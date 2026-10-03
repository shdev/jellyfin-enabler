package server_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
)

// fakeJF ersetzt den Jellyfin-Client. Die Handler-Tests sollen Routing,
// Sitzungsprüfung und Darstellung prüfen, nicht die API-Anbindung.
type fakeJF struct {
	sess      jellyfin.Session
	authErr   error
	users     []jellyfin.User
	usersErr  error
	user      jellyfin.User
	userErr   error
	libs      []jellyfin.Library
	libsErr   error
	written   jellyfin.Access
	writtenID string
	updateErr error
	// writtenBack erzwingt eine Bestätigung, die von der Anfrage abweicht.
	// Sonst könnte kein Test belegen, dass die Antwort den bestätigten
	// Stand trägt und nicht bloß die Anfrage zurückspiegelt.
	writtenBack *jellyfin.Access
	loggedOut   []string
	logoutErr   error
	// SetDisabled: Token, Ziel und Wert des letzten Aufrufs. setDisabled
	// bleibt nil, solange nicht geschaltet wurde.
	setToken    string
	setID       string
	setDisabled *bool
	setErr      error
}

func (f *fakeJF) Authenticate(_ context.Context, _, _ string) (jellyfin.Session, error) {
	return f.sess, f.authErr
}
func (f *fakeJF) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return f.logoutErr
}
func (f *fakeJF) Users(context.Context, string) ([]jellyfin.User, error) {
	return f.users, f.usersErr
}
func (f *fakeJF) User(context.Context, string, string) (jellyfin.User, error) {
	return f.user, f.userErr
}
func (f *fakeJF) Libraries(context.Context, string) ([]jellyfin.Library, error) {
	return f.libs, f.libsErr
}
func (f *fakeJF) UpdateAccess(_ context.Context, _, userID string, a jellyfin.Access) (jellyfin.Access, error) {
	f.writtenID, f.written = userID, a
	if f.updateErr != nil {
		return jellyfin.Access{}, f.updateErr
	}
	if f.writtenBack != nil {
		return *f.writtenBack, nil
	}
	return a, nil
}

func (f *fakeJF) SetDisabled(_ context.Context, token, userID string, disabled bool) (bool, error) {
	f.setToken, f.setID, f.setDisabled = token, userID, &disabled
	if f.setErr != nil {
		return false, f.setErr
	}
	return disabled, nil
}

// aufbau liefert Handler, Fake und Sitzungsspeicher.
func aufbau(t *testing.T) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa", Admin: true}}
	store := session.NewStore(time.Hour)
	return server.New(jf, store, "false"), jf, store
}

// aufbauMit erlaubt es, die Secure-Entscheidung zu prüfen; aufbau bleibt
// die knappe Variante für alle übrigen Tests.
func aufbauMit(t *testing.T, cookieSecure string) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa", Admin: true}}
	store := session.NewStore(time.Hour)
	return server.New(jf, store, cookieSecure), jf, store
}

// angemeldet legt eine Sitzung an und liefert das passende Cookie.
func angemeldet(t *testing.T, store *session.Store) *http.Cookie {
	t.Helper()
	id, err := store.Create("tok", "u-1", "papa")
	if err != nil {
		t.Fatalf("Sitzung nicht anlegbar: %v", err)
	}
	return &http.Cookie{Name: "jfe_session", Value: id}
}

func TestHealthzOhneAnmeldung(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Code = %d, erwartet 200", rec.Code)
	}
}

func TestSeiteOhneSitzungLeitetAufLogin(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Code = %d, erwartet 303", rec.Code)
	}
	if ort := rec.Header().Get("Location"); ort != "/login" {
		t.Errorf("Location = %q, erwartet /login", ort)
	}
}

func TestAPIOhneSitzungGibt401(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/users/u-2/access", strings.NewReader("{}"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Code = %d, erwartet 401", rec.Code)
	}
}

func TestLoginSeiteIstErreichbar(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `name="password"`) {
		t.Error("Anmeldeformular fehlt")
	}
}

func TestLoginSeiteLeitetBeiSitzungAufStart(t *testing.T) {
	h, _, store := aufbau(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(angemeldet(t, store))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("Code = %d, Location = %q — erwartet 303 nach /", rec.Code, rec.Header().Get("Location"))
	}
}

func anmelden(h http.Handler, name, pw string) *httptest.ResponseRecorder {
	form := url.Values{"username": {name}, "password": {pw}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginSetztCookieUndLeitetWeiter(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := anmelden(h, "papa", "geheim")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("Code = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	var gefunden *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jfe_session" {
			gefunden = c
		}
	}
	if gefunden == nil {
		t.Fatal("Sitzungscookie fehlt")
	}
	if !gefunden.HttpOnly {
		t.Error("Cookie muss HttpOnly sein")
	}
	if gefunden.SameSite != http.SameSiteLaxMode {
		t.Error("Cookie muss SameSite=Lax sein")
	}
}

func TestLoginMitFalschenDatenZeigtFehler(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.authErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}
	rec := anmelden(h, "papa", "falsch")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, erwartet 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Anmeldung fehlgeschlagen") {
		t.Errorf("Rumpf ohne Fehlermeldung: %s", rec.Body.String())
	}
}

// Ein Konto ohne Rolle bekommt kein Cookie, und das gerade erzeugte Token
// wird entwertet — sonst bliebe es ungenutzt in Jellyfin gültig.
func TestLoginOhneRolleWirdAbgewiesenUndAbgemeldet(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.sess = jellyfin.Session{Token: "tok-kind", UserID: "u-2", Name: "kind1"}
	rec := anmelden(h, "kind1", "geheim")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Dieses Konto hat keinen Zugriff auf diesen Dienst.") {
		t.Errorf("Rumpf ohne Ablehnung: %s", rec.Body.String())
	}
	if len(jf.loggedOut) != 1 || jf.loggedOut[0] != "tok-kind" {
		t.Errorf("Jellyfin-Abmeldung = %v, erwartet [tok-kind]", jf.loggedOut)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jfe_session" {
			t.Error("ein abgewiesenes Konto darf kein Sitzungscookie bekommen")
		}
	}
}

// Scheitert die Abmeldung, bleibt es trotzdem bei der Ablehnung; der Fehler
// wird nur geloggt.
func TestLoginOhneRolleBleibtAbgewiesenWennAbmeldungScheitert(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.sess = jellyfin.Session{Token: "tok-kind", UserID: "u-2", Name: "kind1"}
	jf.logoutErr = errors.New("Verbindung abgelehnt")
	rec := anmelden(h, "kind1", "geheim")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
}

func TestLogoutEntwertetTokenUndLoeschtCookie(t *testing.T) {
	h, jf, store := aufbau(t)
	c := angemeldet(t, store)
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("Code = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(jf.loggedOut) != 1 || jf.loggedOut[0] != "tok" {
		t.Errorf("Jellyfin-Abmeldung = %v, erwartet [tok]", jf.loggedOut)
	}
	if _, ok := store.Get(c.Value); ok {
		t.Error("Sitzung muss aus dem Speicher verschwinden")
	}
}

// TestStatischeDateienWerdenAusgeliefert deckt die Naht zwischen
// //go:embed, dem Mux-Muster "GET /static/" und http.FileServerFS ab. Der
// Embed-Test in web/embed_test.go belegt nur, dass die Dateien in der
// eingebetteten FS liegen — nicht, dass die Route sie auch ausliefert.
// Ändert jemand das //go:embed-Muster oder das Mux-Muster, liefert die Route
// still 404, die Oberfläche bleibt ungestaltet und der Speichern-Knopf tot,
// ohne dass ein anderer Test das bemerkt — sichtbar würde es nur im Browser.
func TestStatischeDateienWerdenAusgeliefert(t *testing.T) {
	h, _, _ := aufbau(t)
	for _, pfad := range []string{
		"/static/app.css",
		"/static/app.js",
		"/static/icons/icon-192.png",
		"/manifest.webmanifest",
	} {
		t.Run(pfad, func(t *testing.T) {
			rec := holen(t, h, pfad, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("Code = %d, erwartet 200", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Error("Rumpf ist leer")
			}
		})
	}
}

func TestNoStoreAufAllenAntworten(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, erwartet no-store", got)
	}
}

// TestSecureFlagAmSitzungscookie deckt alle drei Zweige von (*Server).secure
// ab. Eine vertauschte Bedingung dort würde das Sitzungscookie im Klartext
// über eine TLS-terminierende Proxy-Strecke schicken — genau das soll das
// Secure-Flag verhindern.
func TestSecureFlagAmSitzungscookie(t *testing.T) {
	cases := []struct {
		name         string
		cookieSecure string
		mitTLS       bool
		erwartet     bool
	}{
		{"explizit true", "true", false, true},
		{"explizit false", "false", true, false},
		{"auto ohne TLS", "auto", false, false},
		{"auto mit TLS", "auto", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := aufbauMit(t, tc.cookieSecure)
			form := url.Values{"username": {"papa"}, "password": {"geheim"}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.mitTLS {
				req.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			for _, c := range rec.Result().Cookies() {
				if c.Name == "jfe_session" {
					if c.Secure != tc.erwartet {
						t.Errorf("Secure = %v, erwartet %v", c.Secure, tc.erwartet)
					}
					return
				}
			}
			t.Fatal("Sitzungscookie fehlt")
		})
	}
}

// TestLoeschCookieHatDieselbenAttribute stellt sicher, dass das Cookie, das
// die Abmeldung löscht, dieselben Attribute trägt wie das gesetzte —
// unterscheiden sie sich, löschen manche Browser das Cookie nicht.
func TestLoeschCookieHatDieselbenAttribute(t *testing.T) {
	h, _, store := aufbauMit(t, "true")
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(angemeldet(t, store))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name != "jfe_session" {
			continue
		}
		if c.MaxAge >= 0 {
			t.Errorf("MaxAge = %d, erwartet negativ zum Löschen", c.MaxAge)
		}
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || !c.Secure || c.Path != "/" {
			t.Errorf("Lösch-Cookie weicht ab: HttpOnly=%v SameSite=%v Secure=%v Path=%q",
				c.HttpOnly, c.SameSite, c.Secure, c.Path)
		}
		return
	}
	t.Fatal("Lösch-Cookie fehlt")
}

func beispielBenutzer() []jellyfin.User {
	return []jellyfin.User{
		{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Disabled: true, Folders: []string{"lib-a"}}},
		{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}},
	}
}

func beispielBibliotheken() []jellyfin.Library {
	return []jellyfin.Library{{ID: "lib-a", Name: "Filme"}, {ID: "lib-b", Name: "Serien"}}
}

func holen(t *testing.T, h http.Handler, pfad string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, pfad, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListeZeigtBenutzerMitStatus(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.users, jf.libs = beispielBenutzer(), beispielBibliotheken()

	rec := holen(t, h, "/", angemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	rumpf := rec.Body.String()
	for _, teil := range []string{"kind1", "papa", "Administrator", "Aus", "Aktiv", `href="/users/u-2"`} {
		if !strings.Contains(rumpf, teil) {
			t.Errorf("Liste ohne %q", teil)
		}
	}
}

func TestZugriffTextInDerListe(t *testing.T) {
	cases := []struct {
		name   string
		access jellyfin.Access
		text   string
	}{
		{"alle künftigen", jellyfin.Access{AllFolders: true}, "Alle Bibliotheken"},
		{"keine", jellyfin.Access{}, "Keine Bibliothek"},
		{"eine von zwei", jellyfin.Access{Folders: []string{"lib-a"}}, "1 von 2 Bibliotheken"},
		{"alle angehakt", jellyfin.Access{Folders: []string{"lib-a", "lib-b"}}, "Alle 2 Bibliotheken (ohne künftige)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, jf, store := aufbau(t)
			jf.libs = beispielBibliotheken()
			jf.users = []jellyfin.User{{ID: "u-2", Name: "kind1", Access: tc.access}}

			rec := holen(t, h, "/", angemeldet(t, store))
			if !strings.Contains(rec.Body.String(), tc.text) {
				t.Errorf("Liste ohne %q", tc.text)
			}
		})
	}
}

func TestListeBeiAbgelaufenemTokenLeitetAufLogin(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.usersErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}

	c := angemeldet(t, store)
	rec := holen(t, h, "/", c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("Code = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, ok := store.Get(c.Value); ok {
		t.Error("die tote Sitzung muss verworfen werden")
	}
}

func TestListeBeiJellyfinAusfallGibt502(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.usersErr = errors.New("Verbindung abgelehnt")

	rec := holen(t, h, "/", angemeldet(t, store))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("Code = %d, erwartet 502", rec.Code)
	}
}

// TestListeBeiJellyfinFehlerZeigtDessenMeldung stellt sicher, dass eine
// konkrete Fehlermeldung von Jellyfin (z. B. eine unvollständige Policy)
// tatsächlich im Rumpf steht — nicht der feste Rückfalltext, der bei jedem
// Ausfall gleich lautet und niemanden auf die wahre Ursache brächte.
func TestListeBeiJellyfinFehlerZeigtDessenMeldung(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.usersErr = &jellyfin.Error{
		Status:  http.StatusBadGateway,
		Message: "Jellyfin hat eine unvollständige Policy geliefert. Es wurde nichts geändert.",
	}

	rec := holen(t, h, "/", angemeldet(t, store))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("Code = %d, erwartet 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Jellyfin hat eine unvollständige Policy geliefert. Es wurde nichts geändert.") {
		t.Errorf("Rumpf = %q, erwartet die konkrete Jellyfin-Meldung statt des Rückfalltexts", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Jellyfin ist gerade nicht erreichbar.") {
		t.Error("der feste Rückfalltext darf eine konkrete Meldung nicht verdecken")
	}
}

// kartenAusschnitt liefert den HTML-Ausschnitt der Karte eines Benutzers.
// Nötig, weil eine Teilstring-Prüfung über die ganze Seite nicht bemerkt,
// wenn Status und Markierung beim falschen Benutzer landen.
func kartenAusschnitt(t *testing.T, rumpf, userID string) string {
	t.Helper()
	start := strings.Index(rumpf, `href="/users/`+userID+`"`)
	if start == -1 {
		t.Fatalf("Karte für %s nicht gefunden", userID)
	}
	rest := rumpf[start:]
	ende := strings.Index(rest, "</a>")
	if ende == -1 {
		t.Fatalf("Karte für %s ist nicht abgeschlossen", userID)
	}
	return rest[:ende]
}

func TestListeOrdnetStatusDemRichtigenBenutzerZu(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.users, jf.libs = beispielBenutzer(), beispielBibliotheken()

	rumpf := holen(t, h, "/", angemeldet(t, store)).Body.String()

	// kind1 (u-2): deaktiviert, kein Administrator.
	kind := kartenAusschnitt(t, rumpf, "u-2")
	if !strings.Contains(kind, "pill-err") {
		t.Error("kind1 muss als deaktiviert dargestellt sein")
	}
	if strings.Contains(kind, "pill-ok") {
		t.Error("kind1 darf nicht als aktiv dargestellt sein")
	}
	if strings.Contains(kind, "Administrator") {
		t.Error("kind1 ist kein Administrator")
	}

	// papa (u-1): aktiv und Administrator.
	papa := kartenAusschnitt(t, rumpf, "u-1")
	if !strings.Contains(papa, "pill-ok") {
		t.Error("papa muss als aktiv dargestellt sein")
	}
	if strings.Contains(papa, "pill-err") {
		t.Error("papa darf nicht als deaktiviert dargestellt sein")
	}
	if !strings.Contains(papa, "Administrator") {
		t.Error("papa muss als Administrator markiert sein")
	}
}

func TestZugriffTextZaehltNurVorhandeneBibliotheken(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken() // lib-a, lib-b
	// In der Policy stehen nur IDs gelöschter Bibliotheken. Ein reiner
	// Zählvergleich meldete hier fälschlich "Alle 2 Bibliotheken".
	jf.users = []jellyfin.User{
		{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Folders: []string{"alt-x", "alt-y"}}},
	}

	rumpf := holen(t, h, "/", angemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, "Keine Bibliothek") {
		t.Errorf("erwartet \"Keine Bibliothek\", bekommen: %s", rumpf)
	}
	if strings.Contains(rumpf, "Alle 2 Bibliotheken") {
		t.Error("gelöschte Bibliotheken dürfen nicht als Zugriff gezählt werden")
	}
}

func TestZugriffTextGemischtVorhandenUndGeloescht(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.users = []jellyfin.User{
		{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Folders: []string{"lib-a", "alt-x"}}},
	}

	rumpf := holen(t, h, "/", angemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, "1 von 2 Bibliotheken") {
		t.Errorf("erwartet \"1 von 2 Bibliotheken\", bekommen: %s", rumpf)
	}
}

func TestDetailseiteZeigtBibliotheken(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Folders: []string{"lib-a"}}}

	rec := holen(t, h, "/users/u-2", angemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	rumpf := rec.Body.String()
	for _, teil := range []string{"kind1", "Filme", "Serien", `data-user-id="u-2"`, "Alle Bibliotheken, auch künftig neue"} {
		if !strings.Contains(rumpf, teil) {
			t.Errorf("Detailseite ohne %q", teil)
		}
	}
	// Filme ist freigegeben, Serien nicht.
	if a := chipFeld(t, rumpf, "lib-a"); !strings.Contains(a, "checked") {
		t.Errorf("die freigegebene Bibliothek ist nicht angehakt: %s", a)
	}
	if b := chipFeld(t, rumpf, "lib-b"); strings.Contains(b, "checked") {
		t.Errorf("die gesperrte Bibliothek darf nicht angehakt sein: %s", b)
	}
}

func TestDetailseiteGrautAdminSchalterAus(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}}

	rumpf := holen(t, h, "/users/u-1", angemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, "Administratoren können nicht deaktiviert werden.") {
		t.Error("Hinweis für Administratoren fehlt")
	}
}

// tagMit liefert das öffnende Tag, das die angegebene Marke enthält.
func tagMit(t *testing.T, rumpf, marke string) string {
	t.Helper()
	pos := strings.Index(rumpf, marke)
	if pos == -1 {
		t.Fatalf("kein Element mit %q gefunden", marke)
	}
	anfang := strings.LastIndex(rumpf[:pos], "<")
	ende := strings.Index(rumpf[pos:], ">")
	if anfang == -1 || ende == -1 {
		t.Fatalf("Element mit %q ist nicht abgeschlossen", marke)
	}
	return rumpf[anfang : pos+ende+1]
}

// eingabeFeld liefert das öffnende Tag des Elements mit der angegebenen id.
// Nötig, weil eine Suche nach "disabled" oder "checked" irgendwo auf der
// Seite auch vom Speichern-Knopf oder von einem Chip stammen könnte.
func eingabeFeld(t *testing.T, rumpf, id string) string {
	t.Helper()
	return tagMit(t, rumpf, `id="`+id+`"`)
}

// chipFeld liefert das öffnende Tag einer Bibliotheks-Checkbox. Die Chips
// tragen keine id, sondern die Bibliotheks-ID im value.
func chipFeld(t *testing.T, rumpf, libID string) string {
	t.Helper()
	return tagMit(t, rumpf, `value="`+libID+`"`)
}

func TestDetailseiteHaktDenRichtigenSchalterAn(t *testing.T) {
	cases := []struct {
		name        string
		disabled    bool
		anErwartet  bool
		ausErwartet bool
	}{
		{"aktiver Benutzer", false, true, false},
		{"deaktivierter Benutzer", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, jf, store := aufbau(t)
			jf.libs = beispielBibliotheken()
			jf.user = jellyfin.User{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Disabled: tc.disabled}}

			rumpf := holen(t, h, "/users/u-2", angemeldet(t, store)).Body.String()
			an := eingabeFeld(t, rumpf, "enabled-on")
			aus := eingabeFeld(t, rumpf, "enabled-off")
			if strings.Contains(an, "checked") != tc.anErwartet {
				t.Errorf("enabled-on = %s, checked erwartet: %v", an, tc.anErwartet)
			}
			if strings.Contains(aus, "checked") != tc.ausErwartet {
				t.Errorf("enabled-off = %s, checked erwartet: %v", aus, tc.ausErwartet)
			}
		})
	}
}

func TestDetailseiteSperrtBeideSchalterBeiAdmin(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}}

	rumpf := holen(t, h, "/users/u-1", angemeldet(t, store)).Body.String()
	for _, id := range []string{"enabled-on", "enabled-off"} {
		if feld := eingabeFeld(t, rumpf, id); !strings.Contains(feld, "disabled") {
			t.Errorf("%s ist bei einem Administrator nicht gesperrt: %s", id, feld)
		}
	}
}

// Gegenstück zum vorigen Test: ohne diesen bliebe unbemerkt, wenn der
// Schalter versehentlich für alle Benutzer gesperrt würde.
func TestDetailseiteSperrtSchalterNichtBeiNormalemBenutzer(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-2", Name: "kind1"}

	rumpf := holen(t, h, "/users/u-2", angemeldet(t, store)).Body.String()
	for _, id := range []string{"enabled-on", "enabled-off"} {
		if feld := eingabeFeld(t, rumpf, id); strings.Contains(feld, "disabled") {
			t.Errorf("%s darf bei einem normalen Benutzer nicht gesperrt sein: %s", id, feld)
		}
	}
}

// Der entscheidende Fall ist die Kombination: "Alle Bibliotheken" graut die
// Chips aus, darf ihnen aber das checked nicht nehmen — sonst wäre die
// frühere Auswahl beim Zurückschalten verloren.
func TestDetailseiteGrautChipsAusOhneDieAuswahlZuVerlieren(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-2", Name: "kind1", Access: jellyfin.Access{
		AllFolders: true,
		Folders:    []string{"lib-a"},
	}}

	rumpf := holen(t, h, "/users/u-2", angemeldet(t, store)).Body.String()

	a := chipFeld(t, rumpf, "lib-a")
	if !strings.Contains(a, "disabled") {
		t.Errorf("lib-a muss ausgegraut sein: %s", a)
	}
	if !strings.Contains(a, "checked") {
		t.Errorf("lib-a muss angehakt bleiben, sonst ist die Auswahl beim Zurückschalten weg: %s", a)
	}

	b := chipFeld(t, rumpf, "lib-b")
	if !strings.Contains(b, "disabled") {
		t.Errorf("lib-b muss ausgegraut sein: %s", b)
	}
	if strings.Contains(b, "checked") {
		t.Errorf("lib-b war nie freigegeben und darf nicht angehakt sein: %s", b)
	}
}

func TestDetailseiteUnbekannterBenutzer(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.userErr = &jellyfin.Error{Status: http.StatusNotFound, Message: "Der Benutzer existiert nicht mehr."}

	rec := holen(t, h, "/users/gibt-es-nicht", angemeldet(t, store))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Code = %d, erwartet 404", rec.Code)
	}
}

func TestListeOrdnetZugriffstextDemRichtigenBenutzerZu(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.users = []jellyfin.User{
		{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}},
		{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Folders: []string{"lib-a"}}},
	}

	rumpf := holen(t, h, "/", angemeldet(t, store)).Body.String()

	papa := kartenAusschnitt(t, rumpf, "u-1")
	if !strings.Contains(papa, "Alle Bibliotheken") {
		t.Errorf("papa-Karte ohne \"Alle Bibliotheken\": %s", papa)
	}
	kind := kartenAusschnitt(t, rumpf, "u-2")
	if !strings.Contains(kind, "1 von 2 Bibliotheken") {
		t.Errorf("kind1-Karte ohne \"1 von 2 Bibliotheken\": %s", kind)
	}
	if strings.Contains(kind, "Alle Bibliotheken") {
		t.Error("der Zugriffstext von papa ist bei kind1 gelandet")
	}
}

func speichern(t *testing.T, h http.Handler, c *http.Cookie, id, rumpf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/users/"+id+"/access", strings.NewReader(rumpf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSpeichernSchreibtUndAntwortetMitBestaetigtemStand(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	c := angemeldet(t, store)
	rec := speichern(t, h, c, "u-2",
		`{"disabled": true, "allFolders": false, "folders": ["lib-a"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	if jf.writtenID != "u-2" {
		t.Errorf("geschrieben für %q, erwartet u-2", jf.writtenID)
	}
	if !jf.written.Disabled || jf.written.AllFolders || len(jf.written.Folders) != 1 {
		t.Errorf("geschriebener Zugriff = %+v", jf.written)
	}
	if jf.written.Folders[0] != "lib-a" {
		t.Errorf("geschriebene Liste = %v, erwartet [lib-a]", jf.written.Folders)
	}
	var antwort struct {
		Disabled   bool     `json:"disabled"`
		AllFolders bool     `json:"allFolders"`
		Folders    []string `json:"folders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &antwort); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v", err)
	}
	if !antwort.Disabled || len(antwort.Folders) != 1 || antwort.Folders[0] != "lib-a" {
		t.Errorf("Antwort = %+v, erwartet den bestätigten Stand", antwort)
	}
	if antwort.AllFolders {
		t.Errorf("antwort.AllFolders = %v, erwartet false", antwort.AllFolders)
	}
	if _, ok := store.Get(c.Value); !ok {
		t.Error("eine erfolgreiche Speicherung darf die Sitzung nicht verwerfen")
	}
}

func TestSpeichernAntwortetMitLeererListeStattNull(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	rec := speichern(t, h, angemeldet(t, store), "u-2",
		`{"disabled": false, "allFolders": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	rumpf := rec.Body.String()
	if !strings.Contains(rumpf, `"folders":[]`) {
		t.Errorf("Antwort enthält nicht \"folders\":[]: %s", rumpf)
	}
	if strings.Contains(rumpf, `"folders":null`) {
		t.Errorf("Antwort enthält \"folders\":null, das darf nicht sein: %s", rumpf)
	}
}

func TestSpeichernLehntUnbekannteBibliothekAb(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	rec := speichern(t, h, angemeldet(t, store), "u-2",
		`{"disabled": false, "allFolders": false, "folders": ["lib-a", "erfunden"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, erwartet 400", rec.Code)
	}
	if jf.writtenID != "" {
		t.Error("bei ungültiger Eingabe darf nichts geschrieben werden")
	}
}

func TestSpeichernKaputtesJSON(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	rec := speichern(t, h, angemeldet(t, store), "u-2", `{kein json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Code = %d, erwartet 400", rec.Code)
	}
}

func TestSpeichernReichtVerbotDurch(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.updateErr = &jellyfin.Error{
		Status:  http.StatusForbidden,
		Message: "Administratoren können nicht deaktiviert werden.",
		Body:    "Administrators cannot be disabled.",
	}

	c := angemeldet(t, store)
	rec := speichern(t, h, c, "u-1", `{"disabled": true, "allFolders": true, "folders": []}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Administratoren können nicht deaktiviert werden.") {
		t.Errorf("Rumpf = %s", rec.Body.String())
	}
	if _, ok := store.Get(c.Value); !ok {
		t.Error("eine Ablehnung durch Jellyfin ist kein Grund, die Sitzung zu verwerfen")
	}
}

func TestSpeichernBeiAbgelaufenemTokenGibt401(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.updateErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}

	c := angemeldet(t, store)
	rec := speichern(t, h, c, "u-2", `{"disabled": false, "allFolders": false, "folders": []}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, erwartet 401", rec.Code)
	}
	if _, ok := store.Get(c.Value); ok {
		t.Error("die tote Sitzung muss verworfen werden")
	}
}

func TestSpeichernBeiJellyfinAusfallGibt502(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.updateErr = errors.New("Verbindung abgelehnt")

	rec := speichern(t, h, angemeldet(t, store), "u-2", `{"disabled": false, "allFolders": false, "folders": []}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("Code = %d, erwartet 502", rec.Code)
	}
}

// Jellyfin darf den Stand abweichend bestätigen. Die Antwort muss dann
// diesen bestätigten Stand tragen, nicht die Anfrage — sonst zeigte die
// Oberfläche eine Änderung an, die der Server nie übernommen hat.
func TestSpeichernAntwortetMitDemBestaetigtenStandNichtMitDerAnfrage(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.writtenBack = &jellyfin.Access{Disabled: false, AllFolders: true, Folders: []string{"lib-b"}}

	rec := speichern(t, h, angemeldet(t, store), "u-2",
		`{"disabled": true, "allFolders": false, "folders": ["lib-a"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	var antwort struct {
		Disabled   bool     `json:"disabled"`
		AllFolders bool     `json:"allFolders"`
		Folders    []string `json:"folders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &antwort); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v", err)
	}
	if antwort.Disabled {
		t.Error("disabled stammt aus der Anfrage statt aus der Bestätigung")
	}
	if !antwort.AllFolders {
		t.Error("allFolders stammt aus der Anfrage statt aus der Bestätigung")
	}
	if len(antwort.Folders) != 1 || antwort.Folders[0] != "lib-b" {
		t.Errorf("Antwort-Ordner = %v, erwartet den bestätigten Stand [lib-b]", antwort.Folders)
	}
}

func TestSpeichernBeiBibliotheksausfallSchreibtNichts(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libsErr = errors.New("Verbindung abgelehnt")

	rec := speichern(t, h, angemeldet(t, store), "u-2",
		`{"disabled": false, "allFolders": false, "folders": []}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("Code = %d, erwartet 502", rec.Code)
	}
	if jf.writtenID != "" {
		t.Error("ohne die Bibliotheksliste darf nichts geschrieben werden")
	}
}

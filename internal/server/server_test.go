package server_test

import (
	"context"
	"crypto/tls"
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
	loggedOut []string
}

func (f *fakeJF) Authenticate(_ context.Context, _, _ string) (jellyfin.Session, error) {
	return f.sess, f.authErr
}
func (f *fakeJF) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return nil
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
	return a, nil
}

// aufbau liefert Handler, Fake und Sitzungsspeicher.
func aufbau(t *testing.T) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa"}}
	store := session.NewStore(time.Hour)
	return server.New(jf, store, "false"), jf, store
}

// aufbauMit erlaubt es, die Secure-Entscheidung zu prüfen; aufbau bleibt
// die knappe Variante für alle übrigen Tests.
func aufbauMit(t *testing.T, cookieSecure string) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa"}}
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

func TestLoginNichtAdminWirdAbgewiesen(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.authErr = jellyfin.ErrNotAdmin
	rec := anmelden(h, "kind1", "geheim")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Administrator") {
		t.Errorf("Rumpf ohne Hinweis auf fehlende Rechte: %s", rec.Body.String())
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

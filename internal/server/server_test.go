package server_test

import (
	"context"
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

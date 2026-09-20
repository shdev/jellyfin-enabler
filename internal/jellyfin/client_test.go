package jellyfin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
)

// captured hält eine Anfrage fest, die der gefälschte Jellyfin-Server
// entgegengenommen hat — damit Tests Header und Rumpf prüfen können.
type captured struct {
	Method string
	Path   string
	Auth   string
	Body   []byte
}

type fakeJellyfin struct {
	t     *testing.T
	mux   *http.ServeMux
	srv   *httptest.Server
	mu    sync.Mutex
	calls []captured
}

func newFake(t *testing.T) *fakeJellyfin {
	t.Helper()
	f := &fakeJellyfin{t: t, mux: http.NewServeMux()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, captured{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		f.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// reply registriert eine feste Antwort für ein Routenmuster.
func (f *fakeJellyfin) reply(pattern string, status int, body string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
}

func (f *fakeJellyfin) handle(pattern string, fn http.HandlerFunc) {
	f.mux.HandleFunc(pattern, fn)
}

func (f *fakeJellyfin) call(i int) captured {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.calls) {
		f.t.Fatalf("Aufruf %d fehlt, es gab nur %d", i, len(f.calls))
	}
	return f.calls[i]
}

func (f *fakeJellyfin) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeJellyfin) client() *jellyfin.Client {
	return jellyfin.New(f.srv.URL, 5*time.Second, "test")
}

const adminLoginAntwort = `{
  "AccessToken": "tok-123",
  "User": {"Id": "u-1", "Name": "papa", "Policy": {"IsAdministrator": true}}
}`

func TestAuthenticateLiefertSitzung(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, adminLoginAntwort)

	sess, err := f.client().Authenticate(context.Background(), "papa", "geheim")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if sess.Token != "tok-123" || sess.UserID != "u-1" || sess.Name != "papa" {
		t.Errorf("Sitzung = %+v, erwartet Token tok-123 / u-1 / papa", sess)
	}
}

func TestAuthenticateSendetClientHeaderUndZugangsdaten(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, adminLoginAntwort)

	if _, err := f.client().Authenticate(context.Background(), "papa", "geheim"); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}

	auth := f.call(0).Auth
	// Jellyfin 10.11 verlangt das MediaBrowser-Format; X-Emby-Authorization
	// greift nur mit EnableLegacyAuthorization.
	if !strings.HasPrefix(auth, "MediaBrowser ") {
		t.Errorf("Authorization = %q, erwartet MediaBrowser-Präfix", auth)
	}
	for _, teil := range []string{`Client="Jellyfin Enabler"`, `Device="Jellyfin Enabler"`, `Version="test"`} {
		if !strings.Contains(auth, teil) {
			t.Errorf("Authorization = %q, erwartet %s", auth, teil)
		}
	}
	if !strings.Contains(auth, `DeviceId="`+jellyfin.DeviceID("papa")+`"`) {
		t.Errorf("Authorization = %q, erwartet stabile DeviceId für papa", auth)
	}
	if strings.Contains(auth, "Token=") {
		t.Errorf("Authorization = %q, bei der Anmeldung darf kein Token stehen", auth)
	}

	var body map[string]string
	if err := json.Unmarshal(f.call(0).Body, &body); err != nil {
		t.Fatalf("Rumpf nicht lesbar: %v", err)
	}
	if body["Username"] != "papa" || body["Pw"] != "geheim" {
		t.Errorf("Rumpf = %v, erwartet Username/Pw", body)
	}
}

func TestDeviceIDIstStabilUndUnterschiedlich(t *testing.T) {
	if jellyfin.DeviceID("papa") != jellyfin.DeviceID("papa") {
		t.Error("DeviceID muss für denselben Namen gleich bleiben")
	}
	if jellyfin.DeviceID("papa") == jellyfin.DeviceID("mama") {
		t.Error("DeviceID muss sich je Name unterscheiden")
	}
	if got := len(jellyfin.DeviceID("papa")); got != 32 {
		t.Errorf("Länge = %d, erwartet 32", got)
	}
}

func TestAuthenticateLehntNichtAdminAbUndMeldetAb(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, `{
	  "AccessToken": "tok-kind",
	  "User": {"Id": "u-2", "Name": "kind1", "Policy": {"IsAdministrator": false}}
	}`)
	f.reply("/Sessions/Logout", http.StatusNoContent, "")

	_, err := f.client().Authenticate(context.Background(), "kind1", "geheim")
	if !errors.Is(err, jellyfin.ErrNotAdmin) {
		t.Fatalf("err = %v, erwartet ErrNotAdmin", err)
	}
	// Das eben erzeugte Token darf nicht zurückbleiben (Spec §Authentifizierung).
	if f.callCount() != 2 {
		t.Fatalf("%d Aufrufe, erwartet 2 (Anmeldung + Abmeldung)", f.callCount())
	}
	if p := f.call(1).Path; p != "/Sessions/Logout" {
		t.Errorf("zweiter Aufruf = %s, erwartet /Sessions/Logout", p)
	}
	if !strings.Contains(f.call(1).Auth, `Token="tok-kind"`) {
		t.Errorf("Abmeldung ohne das erzeugte Token: %q", f.call(1).Auth)
	}
}

func TestAuthenticateFalschePasswortWirdFehler(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusUnauthorized, `Invalid username or password`)

	_, err := f.client().Authenticate(context.Background(), "papa", "falsch")
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	if !jellyfin.IsUnauthorized(err) {
		t.Errorf("err = %v, erwartet 401", err)
	}
}

func TestLogoutSendetToken(t *testing.T) {
	f := newFake(t)
	f.reply("/Sessions/Logout", http.StatusNoContent, "")

	if err := f.client().Logout(context.Background(), "tok-123"); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if !strings.Contains(f.call(0).Auth, `Token="tok-123"`) {
		t.Errorf("Authorization = %q, erwartet Token", f.call(0).Auth)
	}
}

const benutzerListe = `[
  {"Id": "u-1", "Name": "papa", "Policy": {"IsAdministrator": true, "IsDisabled": false, "EnableAllFolders": true, "EnabledFolders": []}},
  {"Id": "u-2", "Name": "kind1", "Policy": {"IsAdministrator": false, "IsDisabled": true, "EnableAllFolders": false, "EnabledFolders": ["lib-a", "lib-b"]}}
]`

func TestUsersLiefertZugriffsdaten(t *testing.T) {
	f := newFake(t)
	f.reply("/Users", http.StatusOK, benutzerListe)

	users, err := f.client().Users(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("%d Benutzer, erwartet 2", len(users))
	}
	// Alphabetisch sortiert, damit die Liste in der Oberfläche stabil ist.
	if users[0].Name != "kind1" || users[1].Name != "papa" {
		t.Errorf("Reihenfolge = %s, %s — erwartet kind1, papa", users[0].Name, users[1].Name)
	}
	kind := users[0]
	if !kind.Access.Disabled {
		t.Error("kind1 muss als deaktiviert gelesen werden")
	}
	if kind.Admin {
		t.Error("kind1 ist kein Administrator")
	}
	if len(kind.Access.Folders) != 2 || kind.Access.Folders[0] != "lib-a" {
		t.Errorf("Folders = %v, erwartet [lib-a lib-b]", kind.Access.Folders)
	}
	if !users[1].Admin || !users[1].Access.AllFolders {
		t.Errorf("papa = %+v, erwartet Administrator mit allen Bibliotheken", users[1])
	}
}

func TestUserLiefertEinzelnenBenutzer(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":{"IsDisabled":false,"EnableAllFolders":false,"EnabledFolders":["lib-a"]}}`)

	u, err := f.client().User(context.Background(), "tok", "u-2")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if u.ID != "u-2" || u.Name != "kind1" || len(u.Access.Folders) != 1 {
		t.Errorf("Benutzer = %+v", u)
	}
}

func TestUserOhnePolicyIstFehler(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-3", http.StatusOK, `{"Id":"u-3","Name":"kaputt"}`)

	// Eine fehlende Policy wird nicht stillschweigend erfunden — sonst
	// schriebe der Dienst später eine leere Policy zurück.
	if _, err := f.client().User(context.Background(), "tok", "u-3"); err == nil {
		t.Fatal("Fehler erwartet, wenn die Policy fehlt")
	}
}

func TestLibrariesLiefertItemIds(t *testing.T) {
	f := newFake(t)
	f.reply("/Library/VirtualFolders", http.StatusOK, `[
	  {"Name": "Serien", "ItemId": "lib-b", "CollectionType": "tvshows"},
	  {"Name": "Filme", "ItemId": "lib-a", "CollectionType": "movies"}
	]`)

	libs, err := f.client().Libraries(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("%d Bibliotheken, erwartet 2", len(libs))
	}
	if libs[0].Name != "Filme" || libs[0].ID != "lib-a" {
		t.Errorf("erste Bibliothek = %+v, erwartet Filme/lib-a (alphabetisch)", libs[0])
	}
}

func TestLesenSendetToken(t *testing.T) {
	f := newFake(t)
	f.reply("/Users", http.StatusOK, `[]`)

	if _, err := f.client().Users(context.Background(), "tok-xyz"); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if !strings.Contains(f.call(0).Auth, `Token="tok-xyz"`) {
		t.Errorf("Authorization = %q, erwartet Token", f.call(0).Auth)
	}
}

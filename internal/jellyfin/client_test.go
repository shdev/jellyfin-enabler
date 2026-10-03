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

// Seit den Semiadmins entscheidet der Server, wer hinein darf. Der Client
// liefert nur das Admin-Flag und lässt das Token unangetastet — sonst hätte
// ein Semiadmin nach der Anmeldung schon kein gültiges Token mehr.
func TestAuthenticateLiefertAdminFlagOhneAbzumelden(t *testing.T) {
	cases := []struct {
		name    string
		antwort string
		admin   bool
	}{
		{"Administrator", adminLoginAntwort, true},
		{"kein Administrator", `{
		  "AccessToken": "tok-kind",
		  "User": {"Id": "u-2", "Name": "kind1", "Policy": {"IsAdministrator": false}}
		}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.reply("/Users/AuthenticateByName", http.StatusOK, tc.antwort)

			sess, err := f.client().Authenticate(context.Background(), "wer", "geheim")
			if err != nil {
				t.Fatalf("unerwarteter Fehler: %v", err)
			}
			if sess.Admin != tc.admin {
				t.Errorf("Admin = %v, erwartet %v", sess.Admin, tc.admin)
			}
			if f.callCount() != 1 {
				t.Errorf("%d Aufrufe, erwartet 1 — Authenticate darf nicht selbst abmelden", f.callCount())
			}
		})
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

// vollePolicy enthält alle 44 UserPolicy-Felder aus der OpenAPI-Spezifikation
// von Jellyfin plus ein erfundenes Feld, wie es ein Plugin oder eine spätere
// Version einführen könnte. Keines davon darf beim Schreiben verloren gehen.
const vollePolicy = `{
  "IsAdministrator": false,
  "IsHidden": true,
  "EnableCollectionManagement": false,
  "EnableSubtitleManagement": false,
  "EnableLyricManagement": false,
  "IsDisabled": false,
  "MaxParentalRating": 10,
  "MaxParentalSubRating": null,
  "BlockedTags": ["horror"],
  "AllowedTags": [],
  "EnableUserPreferenceAccess": true,
  "AccessSchedules": [{"Id": 1, "UserId": "u-2", "DayOfWeek": "Monday", "StartHour": 8.0, "EndHour": 20.0}],
  "BlockUnratedItems": ["Movie"],
  "EnableRemoteControlOfOtherUsers": false,
  "EnableSharedDeviceControl": true,
  "EnableRemoteAccess": true,
  "EnableLiveTvManagement": false,
  "EnableLiveTvAccess": true,
  "EnableMediaPlayback": true,
  "EnableAudioPlaybackTranscoding": true,
  "EnableVideoPlaybackTranscoding": false,
  "EnablePlaybackRemuxing": true,
  "ForceRemoteSourceTranscoding": false,
  "EnableContentDeletion": false,
  "EnableContentDeletionFromFolders": [],
  "EnableContentDownloading": true,
  "EnableSyncTranscoding": true,
  "EnableMediaConversion": false,
  "EnabledDevices": [],
  "EnableAllDevices": true,
  "EnabledChannels": [],
  "EnableAllChannels": false,
  "EnabledFolders": ["lib-a"],
  "EnableAllFolders": false,
  "InvalidLoginAttemptCount": 0,
  "LoginAttemptsBeforeLockout": -1,
  "MaxActiveSessions": 0,
  "EnablePublicSharing": false,
  "BlockedMediaFolders": [],
  "BlockedChannels": [],
  "RemoteClientBitrateLimit": 0,
  "AuthenticationProviderId": "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
  "PasswordResetProviderId": "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
  "SyncPlayAccess": "CreateAndJoinGroups",
  "EinFeldDasDerClientNichtKennt": {"a": 1, "b": [true, null]}
}`

// kompakt normalisiert JSON-Werte, damit Einrückung im Testliteral den
// Vergleich nicht stört — der Encoder sendet kompakt.
func kompakt(t *testing.T, b []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		t.Fatalf("ungültiges JSON %s: %v", b, err)
	}
	return buf.String()
}

// policySchreiber richtet den gefälschten Server so ein, dass er die Policy
// liefert und die geschriebene Policy festhält.
func policySchreiber(t *testing.T, f *fakeJellyfin, policy string) *map[string]json.RawMessage {
	t.Helper()
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":`+policy+`}`)
	geschrieben := map[string]json.RawMessage{}
	f.handle("/Users/u-2/Policy", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&geschrieben); err != nil {
			t.Errorf("geschriebene Policy nicht lesbar: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return &geschrieben
}

func TestUpdateAccessErhaeltAlleUebrigenPolicyFelder(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	_, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{
		Disabled:   true,
		AllFolders: false,
		Folders:    []string{"lib-a", "lib-c"},
	})
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}

	var original map[string]json.RawMessage
	if err := json.Unmarshal([]byte(vollePolicy), &original); err != nil {
		t.Fatalf("Testliteral kaputt: %v", err)
	}
	if len(*geschrieben) != len(original) {
		t.Fatalf("%d Felder geschrieben, erwartet %d", len(*geschrieben), len(original))
	}
	verwaltet := map[string]bool{"IsDisabled": true, "EnableAllFolders": true, "EnabledFolders": true}
	for name, wert := range original {
		if verwaltet[name] {
			continue
		}
		neu, da := (*geschrieben)[name]
		if !da {
			t.Errorf("Feld %s fehlt in der geschriebenen Policy", name)
			continue
		}
		if kompakt(t, wert) != kompakt(t, neu) {
			t.Errorf("Feld %s verändert: %s -> %s", name, kompakt(t, wert), kompakt(t, neu))
		}
	}

	// Die drei verwalteten Felder werden oben übersprungen. Ohne diese Prüfung
	// bliebe eine Vertauschung von IsDisabled und EnableAllFolders unbemerkt:
	// im Betrieb hieße das, "Benutzer deaktivieren" gäbe alle Bibliotheken frei.
	for name, erwartet := range map[string]string{
		"IsDisabled":       "true",
		"EnableAllFolders": "false",
		"EnabledFolders":   `["lib-a","lib-c"]`,
	} {
		if got := kompakt(t, (*geschrieben)[name]); got != erwartet {
			t.Errorf("Feld %s = %s, erwartet %s", name, got, erwartet)
		}
	}
}

func TestUpdateAccessSchreibtDieDreiVerwaltetenFelder(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	neu, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{
		Disabled:   true,
		AllFolders: true,
		Folders:    []string{"lib-a", "lib-c"},
	})
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if got := kompakt(t, (*geschrieben)["IsDisabled"]); got != "true" {
		t.Errorf("IsDisabled = %s, erwartet true", got)
	}
	if got := kompakt(t, (*geschrieben)["EnableAllFolders"]); got != "true" {
		t.Errorf("EnableAllFolders = %s, erwartet true", got)
	}
	// Die Einzelauswahl wird auch bei EnableAllFolders mitgeschrieben, damit
	// sie beim Zurückschalten wieder dasteht (Spec §EnableAllFolders-Semantik).
	if got := kompakt(t, (*geschrieben)["EnabledFolders"]); got != `["lib-a","lib-c"]` {
		t.Errorf("EnabledFolders = %s", got)
	}
	if !neu.Disabled || !neu.AllFolders || len(neu.Folders) != 2 {
		t.Errorf("Rückgabe = %+v, erwartet den geschriebenen Zustand", neu)
	}
}

func TestUpdateAccessSchreibtLeereListeStattNull(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	if _, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{}); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if got := kompakt(t, (*geschrieben)["EnabledFolders"]); got != "[]" {
		t.Errorf("EnabledFolders = %s, erwartet [] statt null", got)
	}
}

// TestUpdateAccessSchreibtNichtBeiAusgeduennterPolicy deckt den Fall ab, dass
// Jellyfin (oder ein Reverse Proxy bei Teilausfall, oder eine geänderte
// DTO-Form nach einem Upgrade) eine Policy ohne IsAdministrator liefert. Ohne
// diese Prüfung schriebe der Dienst eine Policy aus drei Feldern zurück und
// nähme dem Benutzer stillschweigend alle übrigen Rechte.
func TestUpdateAccessSchreibtNichtBeiAusgeduennterPolicy(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":{"IsDisabled": false}}`)

	_, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{Disabled: true})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	var jfErr *jellyfin.Error
	if !errors.As(err, &jfErr) {
		t.Fatalf("err = %T, erwartet *jellyfin.Error", err)
	}
	if jfErr.Status != http.StatusBadGateway {
		t.Errorf("Status = %d, erwartet 502", jfErr.Status)
	}
	for i := 0; i < f.callCount(); i++ {
		if c := f.call(i); c.Method == http.MethodPost && strings.Contains(c.Path, "/Policy") {
			t.Errorf("Aufruf %d ist ein POST auf %s — es hätte nichts geschrieben werden dürfen", i, c.Path)
		}
	}
}

// TestUpdateAccessSchreibtNichtBeiLeererPolicy ist das Gegenstück mit einer
// vollständig leeren Policy — z. B. ein "{}" von einem Reverse Proxy bei
// Teilausfall.
func TestUpdateAccessSchreibtNichtBeiLeererPolicy(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":{}}`)

	_, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{Disabled: true})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	var jfErr *jellyfin.Error
	if !errors.As(err, &jfErr) {
		t.Fatalf("err = %T, erwartet *jellyfin.Error", err)
	}
	if jfErr.Status != http.StatusBadGateway {
		t.Errorf("Status = %d, erwartet 502", jfErr.Status)
	}
	for i := 0; i < f.callCount(); i++ {
		if c := f.call(i); c.Method == http.MethodPost && strings.Contains(c.Path, "/Policy") {
			t.Errorf("Aufruf %d ist ein POST auf %s — es hätte nichts geschrieben werden dürfen", i, c.Path)
		}
	}
}

func TestUpdateAccessReichtVerbotAlsDeutscheMeldungDurch(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"papa","Policy":`+vollePolicy+`}`)
	f.reply("/Users/u-2/Policy", http.StatusForbidden, "Administrators cannot be disabled.")

	_, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{Disabled: true})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	var jfErr *jellyfin.Error
	if !errors.As(err, &jfErr) {
		t.Fatalf("err = %T, erwartet *jellyfin.Error", err)
	}
	if jfErr.Message != "Administratoren können nicht deaktiviert werden." {
		t.Errorf("Message = %q", jfErr.Message)
	}
}

// SetDisabled ist der Weg des Semiadmins. Er darf ausschließlich IsDisabled
// ändern — insbesondere nicht die Bibliotheksfreigaben (Spec §Jellyfin-Client).
func TestSetDisabledAendertNurIsDisabled(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	neu, err := f.client().SetDisabled(context.Background(), "key-abc", "u-2", true)
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if !neu {
		t.Error("Rückgabe = false, erwartet den geschriebenen Wert true")
	}

	var original map[string]json.RawMessage
	if err := json.Unmarshal([]byte(vollePolicy), &original); err != nil {
		t.Fatalf("Testliteral kaputt: %v", err)
	}
	if len(*geschrieben) != len(original) {
		t.Fatalf("%d Felder geschrieben, erwartet %d", len(*geschrieben), len(original))
	}
	for name, wert := range original {
		if name == "IsDisabled" {
			continue
		}
		neu, da := (*geschrieben)[name]
		if !da {
			t.Errorf("Feld %s fehlt in der geschriebenen Policy", name)
			continue
		}
		if kompakt(t, wert) != kompakt(t, neu) {
			t.Errorf("Feld %s verändert: %s -> %s", name, kompakt(t, wert), kompakt(t, neu))
		}
	}
	if got := kompakt(t, (*geschrieben)["IsDisabled"]); got != "true" {
		t.Errorf("IsDisabled = %s, erwartet true", got)
	}
}

// Der API-Key läuft als token-Argument in den vorhandenen Header — beim
// Lesen der Policy wie beim Schreiben.
func TestSetDisabledSendetDenKeyAlsToken(t *testing.T) {
	f := newFake(t)
	policySchreiber(t, f, vollePolicy)

	if _, err := f.client().SetDisabled(context.Background(), "key-abc", "u-2", false); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if f.callCount() != 2 {
		t.Fatalf("%d Aufrufe, erwartet 2 (Policy lesen + schreiben)", f.callCount())
	}
	for i := 0; i < f.callCount(); i++ {
		if c := f.call(i); !strings.Contains(c.Auth, `Token="key-abc"`) {
			t.Errorf("Aufruf %d (%s %s) ohne Key: %q", i, c.Method, c.Path, c.Auth)
		}
	}
}

func TestSetDisabledSchreibtNichtBeiAusgeduennterPolicy(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":{"IsDisabled": false}}`)

	_, err := f.client().SetDisabled(context.Background(), "key-abc", "u-2", true)
	var jfErr *jellyfin.Error
	if !errors.As(err, &jfErr) {
		t.Fatalf("err = %v, erwartet *jellyfin.Error", err)
	}
	if jfErr.Status != http.StatusBadGateway {
		t.Errorf("Status = %d, erwartet 502", jfErr.Status)
	}
	for i := 0; i < f.callCount(); i++ {
		if c := f.call(i); c.Method == http.MethodPost && strings.Contains(c.Path, "/Policy") {
			t.Errorf("Aufruf %d ist ein POST auf %s — es hätte nichts geschrieben werden dürfen", i, c.Path)
		}
	}
}

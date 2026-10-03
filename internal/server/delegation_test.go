package server_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/internal/settings"
)

func zuordnen(t *testing.T, h http.Handler, c *http.Cookie, id, rumpf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/users/"+id+"/delegation", strings.NewReader(rumpf))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type delegationAntwort struct {
	Semiadmin bool     `json:"semiadmin"`
	Users     []string `json:"users"`
}

func delegationLesen(t *testing.T, rec *httptest.ResponseRecorder) delegationAntwort {
	t.Helper()
	var a delegationAntwort
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v (%s)", err, rec.Body.String())
	}
	return a
}

// familieOhne liefert familie() ohne den Benutzer id.
func familieOhne(id string) []jellyfin.User {
	var rest []jellyfin.User
	for _, u := range familie() {
		if u.ID != id {
			rest = append(rest, u)
		}
	}
	return rest
}

// familieGeaendert liefert familie() mit einer Änderung am Benutzer id.
func familieGeaendert(id string, aendern func(*jellyfin.User)) []jellyfin.User {
	users := familie()
	for i := range users {
		if users[i].ID == id {
			aendern(&users[i])
		}
	}
	return users
}

func TestDelegationSpeichertOhneDuplikate(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()

	rec := zuordnen(t, h, angemeldet(t, store), "u-3", `{"semiadmin": true, "users": ["u-4", "u-2", "u-4"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	a := delegationLesen(t, rec)
	if !a.Semiadmin || !reflect.DeepEqual(a.Users, []string{"u-2", "u-4"}) {
		t.Errorf("Antwort = %+v, erwartet semiadmin=true, users=[u-2 u-4]", a)
	}
	if got, ok := zuordnung.Targets("u-3"); !ok || !reflect.DeepEqual(got, []string{"u-2", "u-4"}) {
		t.Errorf("gespeichert = %v, %v — erwartet [u-2 u-4]", got, ok)
	}
}

// semiadmin:false entfernt den Eintrag; users wird dabei ignoriert.
func TestDelegationFalseEntferntEintrag(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()
	if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rec := zuordnen(t, h, angemeldet(t, store), "u-3", `{"semiadmin": false, "users": ["u-4"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	if a := delegationLesen(t, rec); a.Semiadmin {
		t.Errorf("Antwort = %+v, erwartet semiadmin=false", a)
	}
	if !strings.Contains(rec.Body.String(), `"users":[]`) {
		t.Errorf("Antwort = %s, erwartet \"users\":[]", rec.Body.String())
	}
	if _, ok := zuordnung.Targets("u-3"); ok {
		t.Error("der Eintrag muss entfernt sein")
	}
}

// "users": null oder ein fehlendes Feld bei semiadmin:true ist eine leere
// Liste — erlaubt, gespeichert und beantwortet als [].
func TestDelegationOhneListeIstLeer(t *testing.T) {
	for _, rumpf := range []string{`{"semiadmin": true}`, `{"semiadmin": true, "users": null}`} {
		t.Run(rumpf, func(t *testing.T) {
			h, jf, store, zuordnung := aufbauSemi(t)
			jf.users = familie()

			rec := zuordnen(t, h, angemeldet(t, store), "u-3", rumpf)
			if rec.Code != http.StatusOK {
				t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"users":[]`) {
				t.Errorf("Antwort = %s, erwartet \"users\":[]", rec.Body.String())
			}
			got, ok := zuordnung.Targets("u-3")
			if !ok || len(got) != 0 {
				t.Errorf("gespeichert = %v, %v — erwartet [], true", got, ok)
			}
		})
	}
}

// Alle Ablehnungen in der Reihenfolge der Spec (§PUT /api/users/{id}/delegation).
// In keinem Fall darf etwas gespeichert werden.
func TestDelegationLehntAb(t *testing.T) {
	keinAdminMehr := familieGeaendert("u-1", func(u *jellyfin.User) { u.Admin = false })
	cases := []struct {
		name       string
		apiKey     string
		users      []jellyfin.User
		id         string
		rumpf      string
		code       int
		meldung    string
		sitzungWeg bool
	}{
		{"eigenes Konto kein Administrator mehr", testKey, keinAdminMehr, "u-3", `{"semiadmin": true, "users": []}`, http.StatusForbidden, "Dieses Konto ist kein Administrator mehr.", true},
		{"eigenes Konto fehlt", testKey, familieOhne("u-1"), "u-3", `{"semiadmin": true, "users": []}`, http.StatusForbidden, "Dieses Konto ist kein Administrator mehr.", true},
		{"Kontoprüfung vor Key-Prüfung", "", keinAdminMehr, "u-3", `{"semiadmin": true, "users": []}`, http.StatusForbidden, "Dieses Konto ist kein Administrator mehr.", true},
		{"API-Key nicht gesetzt", "", familie(), "u-3", `{"semiadmin": true, "users": []}`, http.StatusConflict, "JELLYFIN_API_KEY ist nicht gesetzt.", false},
		{"Key-Prüfung vor Zielprüfung", "", familie(), "u-9", `{"semiadmin": true, "users": []}`, http.StatusConflict, "JELLYFIN_API_KEY ist nicht gesetzt.", false},
		{"Ziel unbekannt", testKey, familie(), "u-9", `{"semiadmin": true, "users": []}`, http.StatusNotFound, "Der Benutzer existiert nicht mehr.", false},
		{"Ziel ist Administrator", testKey, familie(), "u-1", `{"semiadmin": true, "users": []}`, http.StatusBadRequest, "Administratoren können keine Semiadmins sein.", false},
		{"Eintrag unbekannt", testKey, familie(), "u-3", `{"semiadmin": true, "users": ["u-2", "u-9"]}`, http.StatusBadRequest, "Unbekannter Benutzer: u-9", false},
		{"Eintrag ist Administrator", testKey, familie(), "u-3", `{"semiadmin": true, "users": ["u-1"]}`, http.StatusBadRequest, "Administratoren können nicht geschaltet werden: papa", false},
		{"Eintrag ist der Semiadmin selbst", testKey, familie(), "u-3", `{"semiadmin": true, "users": ["u-3"]}`, http.StatusBadRequest, "Ein Semiadmin kann sich nicht selbst schalten.", false},
		{"kaputtes JSON", testKey, familie(), "u-3", `{kein json`, http.StatusBadRequest, "Die Anfrage war nicht lesbar.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, jf, store, zuordnung := bauen(t, "false", tc.apiKey)
			jf.users = tc.users
			c := angemeldet(t, store)

			rec := zuordnen(t, h, c, tc.id, tc.rumpf)
			if rec.Code != tc.code {
				t.Fatalf("Code = %d, erwartet %d (Rumpf %s)", rec.Code, tc.code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.meldung) {
				t.Errorf("Rumpf = %s, erwartet %q", rec.Body.String(), tc.meldung)
			}
			if n := len(zuordnung.Semiadmins()); n != 0 {
				t.Errorf("%d Einträge gespeichert, erwartet keinen", n)
			}
			if _, ok := store.Get(c.Value); ok == tc.sitzungWeg {
				t.Errorf("Sitzung vorhanden = %v, erwartet %v", ok, !tc.sitzungWeg)
			}
		})
	}
}

func TestDelegationBeiListenausfallSpeichertNichts(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.usersErr = errors.New("Verbindung abgelehnt")

	rec := zuordnen(t, h, angemeldet(t, store), "u-3", `{"semiadmin": true, "users": []}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("Code = %d, erwartet 502", rec.Code)
	}
	if n := len(zuordnung.Semiadmins()); n != 0 {
		t.Errorf("%d Einträge gespeichert, erwartet keinen", n)
	}
}

// Der typische Deployment-Fehler (Verzeichnis gehört nicht 65532): 500 mit
// der Meldung der Spec, der alte Stand bleibt.
func TestDelegationSchreibfehlerGibt500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root greifen Dateirechte nicht")
	}
	dir := t.TempDir()
	zuordnung, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	jf := &fakeJF{users: familie()}
	store := session.NewStore(time.Hour)
	h := server.New(jf, store, "false", zuordnung, testKey)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	rec := zuordnen(t, h, angemeldet(t, store), "u-3", `{"semiadmin": true, "users": ["u-2"]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Code = %d, erwartet 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Einstellungen konnten nicht gespeichert werden.") {
		t.Errorf("Rumpf = %s", rec.Body.String())
	}
	if _, ok := zuordnung.Targets("u-3"); ok {
		t.Error("ein nicht gespeicherter Eintrag darf nicht wirksam sein")
	}
}

func TestDelegationNurFuerAdmins(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()

	rec := zuordnen(t, h, semiadminAngemeldet(t, store), "u-4", `{"semiadmin": true, "users": ["u-2"]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if n := len(zuordnung.Semiadmins()); n != 0 {
		t.Errorf("%d Einträge gespeichert, erwartet keinen", n)
	}
}

func TestDetailseiteZeigtSemiadminAbschnitt(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.libs, jf.users = beispielBibliotheken(), familie()
	jf.user = jellyfin.User{ID: "u-3", Name: "anna"}
	if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rec := holen(t, h, "/users/u-3", angemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	rumpf := rec.Body.String()
	for _, teil := range []string{"Darf andere Benutzer aktivieren und deaktivieren", "Darf schalten:", `id="semiadmin"`} {
		if !strings.Contains(rumpf, teil) {
			t.Errorf("Detailseite ohne %q", teil)
		}
	}
	if box := eingabeFeld(t, rumpf, "semiadmin-box"); !strings.Contains(box, "checked") {
		t.Errorf("Semiadmin-Haken fehlt: %s", box)
	}
	kind := chipFeld(t, rumpf, "u-2")
	if !strings.Contains(kind, "checked") || strings.Contains(kind, "disabled") {
		t.Errorf("kind1 muss gewählt und wählbar sein: %s", kind)
	}
	if ben := chipFeld(t, rumpf, "u-4"); strings.Contains(ben, "checked") {
		t.Errorf("ben ist nicht zugewiesen: %s", ben)
	}
	// Weder der User selbst noch Administratoren sind wählbar.
	for _, id := range []string{"u-3", "u-1"} {
		if strings.Contains(rumpf, `value="`+id+`"`) {
			t.Errorf("Chip für %s darf nicht erscheinen", id)
		}
	}
	if strings.Contains(rumpf, testKey) {
		t.Error("der API-Key steht im HTML")
	}
}

func TestDetailseiteSemiadminChipsGesperrtOhneHaken(t *testing.T) {
	h, jf, store, _ := aufbauSemi(t)
	jf.libs, jf.users = beispielBibliotheken(), familie()
	jf.user = jellyfin.User{ID: "u-3", Name: "anna"}

	rumpf := holen(t, h, "/users/u-3", angemeldet(t, store)).Body.String()
	if box := eingabeFeld(t, rumpf, "semiadmin-box"); strings.Contains(box, "checked") {
		t.Errorf("anna ist kein Semiadmin: %s", box)
	}
	if kind := chipFeld(t, rumpf, "u-2"); !strings.Contains(kind, "disabled") {
		t.Errorf("ohne Haken müssen die Chips gesperrt sein: %s", kind)
	}
}

func TestDetailseiteOhneKeyZeigtHinweis(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-3", Name: "anna"}

	rumpf := holen(t, h, "/users/u-3", angemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, "Semiadmin-Funktion ist aus: JELLYFIN_API_KEY ist nicht gesetzt.") {
		t.Errorf("Hinweis fehlt: %s", rumpf)
	}
	if strings.Contains(rumpf, `id="semiadmin-box"`) {
		t.Error("ohne Key darf es keinen Haken geben")
	}
}

func TestDetailseiteAdminOhneSemiadminAbschnitt(t *testing.T) {
	h, jf, store, _ := aufbauSemi(t)
	jf.libs, jf.users = beispielBibliotheken(), familie()
	jf.user = jellyfin.User{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}}

	rumpf := holen(t, h, "/users/u-1", angemeldet(t, store)).Body.String()
	if strings.Contains(rumpf, `id="semiadmin"`) || strings.Contains(rumpf, "Semiadmin-Funktion ist aus") {
		t.Error("bei Administratoren fehlt der Abschnitt Semiadmin")
	}
}

func TestDetailseiteBeiListenausfallGibt502(t *testing.T) {
	h, jf, store, _ := aufbauSemi(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-3", Name: "anna"}
	jf.usersErr = errors.New("Verbindung abgelehnt")

	if rec := holen(t, h, "/users/u-3", angemeldet(t, store)); rec.Code != http.StatusBadGateway {
		t.Errorf("Code = %d, erwartet 502", rec.Code)
	}
}

func TestListeZeigtSemiadminPille(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users, jf.libs = familie(), beispielBibliotheken()
	if err := zuordnung.Set("u-3", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rumpf := holen(t, h, "/", angemeldet(t, store)).Body.String()
	if anna := kartenAusschnitt(t, rumpf, "u-3"); !strings.Contains(anna, "Semiadmin") {
		t.Errorf("anna-Karte ohne Pille Semiadmin: %s", anna)
	}
	if ben := kartenAusschnitt(t, rumpf, "u-4"); strings.Contains(ben, "Semiadmin") {
		t.Errorf("ben ist kein Semiadmin: %s", ben)
	}
}

// Ohne Key wirkt ein Eintrag nicht — dann zeigt die Liste ihn auch nicht an.
func TestListeOhneKeyZeigtKeineSemiadminPille(t *testing.T) {
	h, jf, store, zuordnung := bauen(t, "false", "")
	jf.users, jf.libs = familie(), beispielBibliotheken()
	if err := zuordnung.Set("u-3", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rumpf := holen(t, h, "/", angemeldet(t, store)).Body.String()
	if anna := kartenAusschnitt(t, rumpf, "u-3"); strings.Contains(anna, "Semiadmin") {
		t.Errorf("ohne Key darf keine Pille erscheinen: %s", anna)
	}
}

package server_test

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/settings"
)

func schalten(t *testing.T, h http.Handler, c *http.Cookie, id, rumpf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/controlled/"+id+"/active", strings.NewReader(rumpf))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// logMitschnitt leitet das Standard-Log für die Dauer des Tests um.
func logMitschnitt(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestSemiadminAnsichtZeigtNurZugewiesene(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users, jf.libs = familie(), beispielBibliotheken()
	// Eine alte oder von Hand bearbeitete Datei: neben kind1 und ben stehen
	// ein Administrator, eine verwaiste ID und anna selbst darin.
	if err := zuordnung.Set("u-3", []string{"u-2", "u-4", "u-1", "u-9", "u-3"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rec := holen(t, h, "/", semiadminAngemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200 (Rumpf %s)", rec.Code, rec.Body.String())
	}
	rumpf := rec.Body.String()
	for _, id := range []string{"u-2", "u-4"} {
		if !strings.Contains(rumpf, `data-user-id="`+id+`"`) {
			t.Errorf("Karte für %s fehlt", id)
		}
	}
	for _, id := range []string{"u-1", "u-9", "u-3"} {
		if strings.Contains(rumpf, `data-user-id="`+id+`"`) {
			t.Errorf("Karte für %s darf nicht erscheinen", id)
		}
	}
	if strings.Contains(rumpf, "/users/") {
		t.Error("die Semiadmin-Ansicht darf nicht auf Detailseiten verlinken")
	}
	if strings.Contains(rumpf, "Bibliothek") || strings.Contains(rumpf, "Filme") {
		t.Error("die Semiadmin-Ansicht darf keine Bibliotheken zeigen")
	}
	if strings.Contains(rumpf, testKey) {
		t.Error("der API-Key steht im HTML")
	}
	// kind1 ist deaktiviert, ben aktiv.
	if !strings.Contains(eingabeFeld(t, rumpf, "off-u-2"), "checked") || strings.Contains(eingabeFeld(t, rumpf, "on-u-2"), "checked") {
		t.Error("kind1 muss auf Aus stehen")
	}
	if !strings.Contains(eingabeFeld(t, rumpf, "on-u-4"), "checked") {
		t.Error("ben muss auf Aktiv stehen")
	}
	// Die Liste kommt über den API-Key, nicht über das Token der Semiadmin.
	if len(jf.usersTokens) == 0 || jf.usersTokens[0] != testKey {
		t.Errorf("Users-Tokens = %v, erwartet den API-Key", jf.usersTokens)
	}
}

func TestSemiadminAnsichtOhneGueltigeZieleIstLeer(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()
	if err := zuordnung.Set("u-3", []string{"u-1", "u-9"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rumpf := holen(t, h, "/", semiadminAngemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, "Dir sind keine Benutzer zugewiesen.") {
		t.Errorf("Hinweis auf leere Liste fehlt: %s", rumpf)
	}
}

func TestSchaltenSchaltetUndLoggt(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()
	if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	buf := logMitschnitt(t)

	rec := schalten(t, h, semiadminAngemeldet(t, store), "u-2", `{"disabled": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"disabled":false`) {
		t.Errorf("Antwort = %s, erwartet {\"disabled\":false}", rec.Body.String())
	}
	if jf.setDisabled == nil || *jf.setDisabled || jf.setID != "u-2" || jf.setToken != testKey {
		t.Errorf("SetDisabled(token=%q, id=%q, disabled=%v), erwartet Key, u-2, false", jf.setToken, jf.setID, jf.setDisabled)
	}
	if !strings.Contains(buf.String(), "semiadmin schaltet: von=anna (u-3) ziel=kind1 (u-2) disabled=false") {
		t.Errorf("Log-Zeile fehlt: %q", buf.String())
	}
	if strings.Contains(buf.String(), testKey) {
		t.Errorf("der API-Key steht im Log: %q", buf.String())
	}
}

func TestSchaltenLehntAb(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		rumpf   string
		code    int
		meldung string
	}{
		{"fremde ID", "u-4", `{"disabled": true}`, http.StatusForbidden, "Dieser Benutzer ist dir nicht zugewiesen."},
		{"eigenes Konto in der eigenen Liste", "u-3", `{"disabled": true}`, http.StatusForbidden, "Dieser Benutzer ist dir nicht zugewiesen."},
		{"unbekanntes Ziel", "u-9", `{"disabled": true}`, http.StatusNotFound, "Der Benutzer existiert nicht mehr."},
		{"Administrator als Ziel", "u-1", `{"disabled": true}`, http.StatusForbidden, "Administratoren können nicht geschaltet werden."},
		{"kaputtes JSON", "u-2", `{kein json`, http.StatusBadRequest, "Die Anfrage war nicht lesbar."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, jf, store, zuordnung := aufbauSemi(t)
			jf.users = familie()
			if err := zuordnung.Set("u-3", []string{"u-2", "u-3", "u-9", "u-1"}); err != nil {
				t.Fatalf("Set: %v", err)
			}
			c := semiadminAngemeldet(t, store)

			rec := schalten(t, h, c, tc.id, tc.rumpf)
			if rec.Code != tc.code {
				t.Fatalf("Code = %d, erwartet %d (Rumpf %s)", rec.Code, tc.code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.meldung) {
				t.Errorf("Rumpf = %s, erwartet %q", rec.Body.String(), tc.meldung)
			}
			if jf.setDisabled != nil {
				t.Error("es darf nichts geschaltet werden")
			}
			if _, ok := store.Get(c.Value); !ok {
				t.Error("eine Ablehnung darf die Sitzung nicht löschen")
			}
		})
	}
}

// Admins nutzen /access; der Semiadmin-Endpunkt ist für sie gesperrt.
func TestSchaltenDurchAdminGibt403(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()
	if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rec := schalten(t, h, angemeldet(t, store), "u-2", `{"disabled": true}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Diese Aktion ist Semiadmins vorbehalten.") {
		t.Errorf("Rumpf = %s", rec.Body.String())
	}
	if jf.setDisabled != nil {
		t.Error("es darf nichts geschaltet werden")
	}
}

// Rollenentzug und Deaktivierung wirken sofort, nicht erst beim nächsten
// Login: die Sitzung wird gelöscht und das Token entwertet.
func TestSemiadminVerliertZugangMittenInDerSitzung(t *testing.T) {
	cases := []struct {
		name   string
		apiKey string
		users  []jellyfin.User
		vorher func(*settings.Store) error
	}{
		{"Eintrag entfernt", testKey, familie(), func(z *settings.Store) error { return z.Remove("u-3") }},
		{"Konto deaktiviert", testKey, familieGeaendert("u-3", func(u *jellyfin.User) { u.Access.Disabled = true }), nil},
		{"Konto gelöscht", testKey, familieOhne("u-3"), nil},
		{"API-Key nicht gesetzt", "", familie(), nil},
	}
	for _, tc := range cases {
		for _, weg := range []string{"Ansicht", "Schalten"} {
			t.Run(tc.name+"/"+weg, func(t *testing.T) {
				h, jf, store, zuordnung := bauen(t, "false", tc.apiKey)
				jf.users = tc.users
				if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
					t.Fatalf("Set: %v", err)
				}
				c := semiadminAngemeldet(t, store)
				if tc.vorher != nil {
					if err := tc.vorher(zuordnung); err != nil {
						t.Fatalf("Vorbereitung: %v", err)
					}
				}

				if weg == "Ansicht" {
					rec := holen(t, h, "/", c)
					if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
						t.Errorf("Code = %d, Location = %q — erwartet 303 nach /login", rec.Code, rec.Header().Get("Location"))
					}
				} else {
					rec := schalten(t, h, c, "u-2", `{"disabled": true}`)
					if rec.Code != http.StatusUnauthorized {
						t.Errorf("Code = %d, erwartet 401", rec.Code)
					}
					if jf.setDisabled != nil {
						t.Error("es darf nichts geschaltet werden")
					}
				}
				if _, ok := store.Get(c.Value); ok {
					t.Error("die Sitzung muss gelöscht sein")
				}
				if len(jf.loggedOut) != 1 || jf.loggedOut[0] != "tok-anna" {
					t.Errorf("Abmeldung = %v, erwartet [tok-anna]", jf.loggedOut)
				}
			})
		}
	}
}

// Ein Fehler des Key-Aufrufs ist kein Grund, die Sitzung des Semiadmins zu
// löschen. 401/403 heißt: Key ungültig → 502 mit Hinweis an den Admin; alles
// andere läuft über die gewohnte Fehlerabbildung.
func TestKeyFehlerLassenDieSitzungBestehen(t *testing.T) {
	keyKaputt := "Interner Zugang nicht verfügbar. Bitte den Administrator informieren."
	fehler := []struct {
		name    string
		err     error
		code    int
		meldung string
	}{
		{"401", &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}, http.StatusBadGateway, keyKaputt},
		{"403", &jellyfin.Error{Status: http.StatusForbidden, Message: "Jellyfin hat die Änderung abgelehnt."}, http.StatusBadGateway, keyKaputt},
		{"500", &jellyfin.Error{Status: http.StatusInternalServerError, Message: "Jellyfin hat die Anfrage abgelehnt."}, http.StatusInternalServerError, "Jellyfin hat die Anfrage abgelehnt."},
		{"Netzwerk", errors.New("Verbindung abgelehnt"), http.StatusBadGateway, "Jellyfin ist gerade nicht erreichbar."},
	}
	for _, tc := range fehler {
		for _, weg := range []string{"Ansicht", "Liste beim Schalten", "Schalten"} {
			t.Run(tc.name+"/"+weg, func(t *testing.T) {
				h, jf, store, zuordnung := aufbauSemi(t)
				jf.users = familie()
				if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
					t.Fatalf("Set: %v", err)
				}
				c := semiadminAngemeldet(t, store)

				var rec *httptest.ResponseRecorder
				switch weg {
				case "Ansicht":
					jf.usersErr = tc.err
					rec = holen(t, h, "/", c)
				case "Liste beim Schalten":
					jf.usersErr = tc.err
					rec = schalten(t, h, c, "u-2", `{"disabled": true}`)
				case "Schalten":
					jf.setErr = tc.err
					rec = schalten(t, h, c, "u-2", `{"disabled": true}`)
				}
				if rec.Code != tc.code {
					t.Errorf("Code = %d, erwartet %d", rec.Code, tc.code)
				}
				if !strings.Contains(rec.Body.String(), tc.meldung) {
					t.Errorf("Rumpf = %s, erwartet %q", rec.Body.String(), tc.meldung)
				}
				if _, ok := store.Get(c.Value); !ok {
					t.Error("ein Key-Fehler darf die Sitzung nicht löschen")
				}
				if len(jf.loggedOut) != 0 {
					t.Errorf("Abmeldung = %v, erwartet keine", jf.loggedOut)
				}
			})
		}
	}
}

// Rollenmatrix: ohne Sitzung, Admin und Semiadmin gegen jede geschützte Route.
func TestRollenmatrix(t *testing.T) {
	zugriff := `{"disabled": false, "allFolders": true, "folders": []}`
	zuordnung := `{"semiadmin": false}`
	schalter := `{"disabled": true}`
	cases := []struct {
		rolle   string
		methode string
		pfad    string
		rumpf   string
		code    int
		ort     string
	}{
		{"ohne", http.MethodGet, "/", "", http.StatusSeeOther, "/login"},
		{"admin", http.MethodGet, "/", "", http.StatusOK, ""},
		{"semiadmin", http.MethodGet, "/", "", http.StatusOK, ""},
		{"ohne", http.MethodGet, "/users/u-2", "", http.StatusSeeOther, "/login"},
		{"admin", http.MethodGet, "/users/u-2", "", http.StatusOK, ""},
		{"semiadmin", http.MethodGet, "/users/u-2", "", http.StatusSeeOther, "/"},
		{"ohne", http.MethodPut, "/api/users/u-2/access", zugriff, http.StatusUnauthorized, ""},
		{"admin", http.MethodPut, "/api/users/u-2/access", zugriff, http.StatusOK, ""},
		{"semiadmin", http.MethodPut, "/api/users/u-2/access", zugriff, http.StatusForbidden, ""},
		{"ohne", http.MethodPut, "/api/users/u-4/delegation", zuordnung, http.StatusUnauthorized, ""},
		{"admin", http.MethodPut, "/api/users/u-4/delegation", zuordnung, http.StatusOK, ""},
		{"semiadmin", http.MethodPut, "/api/users/u-4/delegation", zuordnung, http.StatusForbidden, ""},
		{"ohne", http.MethodPut, "/api/controlled/u-2/active", schalter, http.StatusUnauthorized, ""},
		{"admin", http.MethodPut, "/api/controlled/u-2/active", schalter, http.StatusForbidden, ""},
		{"semiadmin", http.MethodPut, "/api/controlled/u-2/active", schalter, http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.rolle+" "+tc.methode+" "+tc.pfad, func(t *testing.T) {
			h, jf, store, z := aufbauSemi(t)
			jf.users, jf.libs = familie(), beispielBibliotheken()
			jf.user = jellyfin.User{ID: "u-2", Name: "kind1"}
			if err := z.Set("u-3", []string{"u-2"}); err != nil {
				t.Fatalf("Set: %v", err)
			}
			req := httptest.NewRequest(tc.methode, tc.pfad, strings.NewReader(tc.rumpf))
			req.Header.Set("Content-Type", "application/json")
			switch tc.rolle {
			case "admin":
				req.AddCookie(angemeldet(t, store))
			case "semiadmin":
				req.AddCookie(semiadminAngemeldet(t, store))
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("Code = %d, erwartet %d (Rumpf %s)", rec.Code, tc.code, rec.Body.String())
			}
			if tc.ort != "" && rec.Header().Get("Location") != tc.ort {
				t.Errorf("Location = %q, erwartet %q", rec.Header().Get("Location"), tc.ort)
			}
		})
	}
}

// Bei einem Key-Fehler muss der Semiadmin sich abmelden können: GET /login
// leitet bei bestehender Sitzung auf / zurück, ein Rohtext ohne Layout wäre
// eine Sackgasse.
func TestKeyFehlerAnsichtZeigtMeldungMitAbmelden(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()
	if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	jf.usersErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}
	c := semiadminAngemeldet(t, store)

	rec := holen(t, h, "/", c)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("Code = %d, erwartet 502", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Interner Zugang nicht verfügbar. Bitte den Administrator informieren.") {
		t.Errorf("Meldung fehlt: %s", body)
	}
	if !strings.Contains(body, `role="alert"`) {
		t.Errorf("role=\"alert\" fehlt: %s", body)
	}
	if !strings.Contains(body, `action="/logout"`) {
		t.Errorf("Abmelden-Formular fehlt: %s", body)
	}
	if strings.Contains(body, "Dir sind keine Benutzer zugewiesen.") {
		t.Errorf("Leer-Text darf bei einem Fehler nicht erscheinen: %s", body)
	}
	if _, ok := store.Get(c.Value); !ok {
		t.Error("ein Key-Fehler darf die Sitzung nicht löschen")
	}
}

// Das Feld disabled ist Pflicht: ein leerer Rumpf darf nicht still als
// "aktivieren" gelten.
func TestSchaltenOhneFeldDisabledGibt400(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users = familie()
	if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	c := semiadminAngemeldet(t, store)

	rec := schalten(t, h, c, "u-2", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, erwartet 400 (Rumpf %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Die Anfrage war nicht lesbar.") {
		t.Errorf("Rumpf = %s", rec.Body.String())
	}
	if jf.setDisabled != nil {
		t.Error("es darf nichts geschaltet werden")
	}

	rec = schalten(t, h, c, "u-2", `{"disabled":false}`)
	if rec.Code != http.StatusOK || jf.setDisabled == nil || *jf.setDisabled {
		t.Errorf("{\"disabled\":false} muss schalten: Code %d", rec.Code)
	}
}

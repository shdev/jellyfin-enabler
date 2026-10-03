package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/session"
)

// sitzungAusAntwort liefert die Sitzung, die eine Anmeldung angelegt hat.
func sitzungAusAntwort(t *testing.T, rec *httptest.ResponseRecorder, store *session.Store) (session.Session, bool) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jfe_session" {
			return store.Get(c.Value)
		}
	}
	return session.Session{}, false
}

// Die drei Fälle der Login-Entscheidung (Spec §Anmeldung, Sitzung und
// Rollen) plus der Semiadmin bei fehlendem Key, der wie ein Konto ohne Rolle
// behandelt wird.
func TestLoginEntscheidetNachRolle(t *testing.T) {
	cases := []struct {
		name       string
		apiKey     string
		admin      bool
		eintragen  bool
		code       int
		sitzung    bool
		adminRolle bool
	}{
		{"Administrator", testKey, true, false, http.StatusSeeOther, true, true},
		{"Semiadmin", testKey, false, true, http.StatusSeeOther, true, false},
		{"ohne Rolle", testKey, false, false, http.StatusForbidden, false, false},
		{"Semiadmin ohne API-Key", "", false, true, http.StatusForbidden, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, jf, store, zuordnung := bauen(t, "false", tc.apiKey)
			jf.sess = jellyfin.Session{Token: "tok-anna", UserID: "u-3", Name: "anna", Admin: tc.admin}
			if tc.eintragen {
				if err := zuordnung.Set("u-3", []string{"u-2"}); err != nil {
					t.Fatalf("Set: %v", err)
				}
			}

			rec := anmelden(h, "anna", "geheim")
			if rec.Code != tc.code {
				t.Fatalf("Code = %d, erwartet %d", rec.Code, tc.code)
			}
			sess, ok := sitzungAusAntwort(t, rec, store)
			if ok != tc.sitzung {
				t.Fatalf("Sitzung angelegt = %v, erwartet %v", ok, tc.sitzung)
			}
			if ok && sess.Admin != tc.adminRolle {
				t.Errorf("Sitzung.Admin = %v, erwartet %v", sess.Admin, tc.adminRolle)
			}
			if tc.sitzung {
				if len(jf.loggedOut) != 0 {
					t.Errorf("Abmeldung = %v, eine zugelassene Anmeldung darf nicht abmelden", jf.loggedOut)
				}
				return
			}
			if len(jf.loggedOut) != 1 || jf.loggedOut[0] != "tok-anna" {
				t.Errorf("Abmeldung = %v, erwartet [tok-anna]", jf.loggedOut)
			}
			if !strings.Contains(rec.Body.String(), "Dieses Konto hat keinen Zugriff auf diesen Dienst.") {
				t.Errorf("Rumpf ohne Ablehnung: %s", rec.Body.String())
			}
		})
	}
}

// Bisher erzwang Jellyfin die Adminrechte über das Token. Ein Semiadmin hat
// ein gültiges Token — die Admin-Routen muss der Dienst deshalb selbst sperren.
func TestNurAdminSperrtSemiadminAusDenAdminRouten(t *testing.T) {
	h, jf, store, _ := aufbauSemi(t)
	jf.user = jellyfin.User{ID: "u-2", Name: "kind1"}
	jf.libs = beispielBibliotheken()
	c := semiadminAngemeldet(t, store)

	rec := holen(t, h, "/users/u-2", c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("Detailseite: Code = %d, Location = %q — erwartet 303 nach /", rec.Code, rec.Header().Get("Location"))
	}
	if strings.Contains(rec.Body.String(), "kind1") {
		t.Error("die Detailseite darf für einen Semiadmin nicht dargestellt werden")
	}

	rec = speichern(t, h, c, "u-2", `{"disabled": true, "allFolders": true, "folders": []}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Speichern: Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Diese Aktion ist Administratoren vorbehalten.") {
		t.Errorf("Speichern: Rumpf = %s", rec.Body.String())
	}
	if jf.writtenID != "" {
		t.Error("ein Semiadmin darf nichts über /access schreiben")
	}
	if _, ok := store.Get(c.Value); !ok {
		t.Error("nurAdmin darf die Sitzung nicht löschen")
	}
}

func TestStartseiteFuerSemiadminZeigtEigeneAnsicht(t *testing.T) {
	h, jf, store, zuordnung := aufbauSemi(t)
	jf.users, jf.libs = familie(), beispielBibliotheken()
	if err := zuordnung.Set("u-3", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rec := holen(t, h, "/", semiadminAngemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	rumpf := rec.Body.String()
	if !strings.Contains(rumpf, "Dir sind keine Benutzer zugewiesen.") {
		t.Errorf("Semiadmin-Ansicht ohne Hinweis auf die leere Liste: %s", rumpf)
	}
	if strings.Contains(rumpf, `href="/users/`) {
		t.Error("die Semiadmin-Ansicht darf nicht auf Detailseiten verlinken")
	}
}

func TestLoginSeiteFragtNachBenutzername(t *testing.T) {
	h, _, _ := aufbau(t)
	rumpf := holen(t, h, "/login", nil).Body.String()
	if !strings.Contains(rumpf, `for="username">Benutzername</label>`) {
		t.Errorf("Feldbeschriftung Benutzername fehlt: %s", rumpf)
	}
	if strings.Contains(rumpf, ">Administrator</label>") {
		t.Error("die alte Beschriftung Administrator steht noch da")
	}
}

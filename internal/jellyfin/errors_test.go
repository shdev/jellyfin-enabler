package jellyfin

import (
	"fmt"
	"net/http"
	"testing"
)

// Die Rümpfe stammen wörtlich aus Jellyfin.Api/Controllers/UserController.cs,
// Tag v10.11.0, Methode UpdateUserPolicy.
func TestStatusErrorBildetVerbotstexteAb(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		message string
	}{
		{
			"Administrator deaktivieren",
			"Administrators cannot be disabled.",
			"Administratoren können nicht deaktiviert werden.",
		},
		{
			"letzter aktiver Benutzer",
			"There must be at least one enabled user in the system.",
			"Es muss mindestens ein aktiver Benutzer übrig bleiben.",
		},
		{
			"letzter Administrator",
			"There must be at least one user in the system with administrative access.",
			"Es muss mindestens ein Administrator übrig bleiben.",
		},
		{
			"unbekannter Text fällt zurück",
			"Something else entirely",
			"Jellyfin hat die Änderung abgelehnt.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := statusError(http.StatusForbidden, tc.body)
			if err.Message != tc.message {
				t.Errorf("Message = %q, erwartet %q", err.Message, tc.message)
			}
			if err.Body != tc.body {
				t.Errorf("Body = %q, Originaltext muss fürs Log erhalten bleiben", err.Body)
			}
		})
	}
}

func TestStatusErrorSonstigeCodes(t *testing.T) {
	cases := []struct {
		status  int
		message string
	}{
		{http.StatusUnauthorized, "Die Anmeldung ist abgelaufen."},
		{http.StatusNotFound, "Der Benutzer existiert nicht mehr."},
		{http.StatusInternalServerError, "Jellyfin hat die Anfrage abgelehnt."},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			if got := statusError(tc.status, "").Message; got != tc.message {
				t.Errorf("Message = %q, erwartet %q", got, tc.message)
			}
		})
	}
}

func TestIsUnauthorized(t *testing.T) {
	if !IsUnauthorized(statusError(http.StatusUnauthorized, "")) {
		t.Error("401 muss als abgelaufene Anmeldung erkannt werden")
	}
	if IsUnauthorized(statusError(http.StatusForbidden, "")) {
		t.Error("403 ist keine abgelaufene Anmeldung")
	}
	if IsUnauthorized(fmt.Errorf("Netzwerk kaputt")) {
		t.Error("ein beliebiger Fehler ist keine abgelaufene Anmeldung")
	}
}

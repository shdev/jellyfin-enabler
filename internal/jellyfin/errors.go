package jellyfin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Error ist eine Fehlerantwort von Jellyfin. Message ist deutsch und für die
// Oberfläche gedacht, Body der englische Originaltext fürs Log
// (Spec §Fehlerbehandlung).
type Error struct {
	Status  int
	Message string
	Body    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("jellyfin: HTTP %d: %s (Original: %q)", e.Status, e.Message, e.Body)
}

// Unauthorized meldet ein abgelaufenes oder entwertetes Token.
func (e *Error) Unauthorized() bool { return e.Status == http.StatusUnauthorized }

// IsUnauthorized prüft einen beliebigen Fehler darauf, ob die Jellyfin-Sitzung
// abgelaufen ist — dann muss sich der Benutzer neu anmelden.
func IsUnauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Unauthorized()
}

func statusError(status int, body string) *Error {
	switch status {
	case http.StatusUnauthorized:
		return &Error{Status: status, Message: "Die Anmeldung ist abgelaufen.", Body: body}
	case http.StatusForbidden:
		return &Error{Status: status, Message: forbiddenMessage(body), Body: body}
	case http.StatusNotFound:
		return &Error{Status: status, Message: "Der Benutzer existiert nicht mehr.", Body: body}
	}
	return &Error{Status: status, Message: "Jellyfin hat die Anfrage abgelehnt.", Body: body}
}

// forbiddenMessage bildet die englischen Klartexte aus UserController.cs
// (v10.11.0, UpdateUserPolicy) auf deutsche Meldungen ab. Der Statuscode
// allein ist mehrdeutig — es gibt drei Verbotsfälle. Ändert Jellyfin die
// Formulierung, greift die Rückfallmeldung; der Originaltext wird geloggt.
func forbiddenMessage(body string) string {
	switch {
	case strings.Contains(body, "Administrators cannot be disabled"):
		return "Administratoren können nicht deaktiviert werden."
	case strings.Contains(body, "at least one enabled user"):
		return "Es muss mindestens ein aktiver Benutzer übrig bleiben."
	case strings.Contains(body, "administrative access"):
		// Durch den Passthrough von IsAdministrator nicht auslösbar,
		// defensiv abgebildet (Spec §Fehlerbehandlung).
		return "Es muss mindestens ein Administrator übrig bleiben."
	}
	return "Jellyfin hat die Änderung abgelehnt."
}

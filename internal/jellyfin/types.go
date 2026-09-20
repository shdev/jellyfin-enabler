// Package jellyfin spricht mit der Jellyfin-API. Alle Endpunkte und Regeln
// sind gegen Quellcode und OpenAPI-Spezifikation am Tag v10.11.0 geprüft
// (Spec §Verifizierte Grundlagen).
package jellyfin

// Access sind die drei UserPolicy-Felder, die dieser Dienst verwaltet.
// Alle übrigen Felder werden unverändert durchgereicht
// (Spec §Policy-Passthrough).
type Access struct {
	Disabled   bool
	AllFolders bool
	Folders    []string
}

// User ist die Sicht des Dienstes auf einen Jellyfin-Benutzer.
type User struct {
	ID     string
	Name   string
	Admin  bool
	Access Access
}

// Library ist eine Jellyfin-Bibliothek. ID entspricht VirtualFolderInfo.ItemId
// und ist der Wert, der in UserPolicy.EnabledFolders steht.
type Library struct {
	ID   string
	Name string
}

// Session ist das Ergebnis einer erfolgreichen Anmeldung.
type Session struct {
	Token  string
	UserID string
	Name   string
}

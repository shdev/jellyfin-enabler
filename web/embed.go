// Package web bettet alle UI-Assets ins Binary ein — der Dienst besteht aus
// einer einzigen Datei und braucht keinen Internetzugang.
package web

import "embed"

//go:embed templates static
var FS embed.FS

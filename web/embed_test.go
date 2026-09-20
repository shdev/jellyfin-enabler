package web_test

import (
	"io/fs"
	"strings"
	"testing"

	"jellyfinenabler/web"
)

func TestAssetsSindEingebettet(t *testing.T) {
	for _, name := range []string{
		"templates/layout.html",
		"templates/login.html",
		"static/app.css",
		"static/app.js",
		"static/manifest.webmanifest",
	} {
		if _, err := fs.Stat(web.FS, name); err != nil {
			t.Errorf("%s fehlt im Binary: %v", name, err)
		}
	}
}

func TestAppCssEnthaeltDesignTokens(t *testing.T) {
	b, err := fs.ReadFile(web.FS, "static/app.css")
	if err != nil {
		t.Fatalf("app.css nicht lesbar: %v", err)
	}
	// Wenn make css nie lief, ist die Datei leer oder ohne Tokens — das wäre
	// eine Oberfläche ganz ohne Gestaltung.
	if !strings.Contains(string(b), "--accent") {
		t.Error("app.css enthält keine Design-Tokens; make css vergessen?")
	}
}

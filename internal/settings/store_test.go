package settings_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"jellyfinenabler/internal/settings"
)

// pfad liefert einen Dateipfad in einem frischen Testverzeichnis.
func pfad(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "settings.json")
}

func laden(t *testing.T, p string) *settings.Store {
	t.Helper()
	s, err := settings.Load(p)
	if err != nil {
		t.Fatalf("Load(%s): %v", p, err)
	}
	return s
}

func TestLoadFehlendeDateiIstLeer(t *testing.T) {
	s := laden(t, pfad(t))
	if n := len(s.Semiadmins()); n != 0 {
		t.Errorf("%d Einträge, erwartet 0", n)
	}
	if _, ok := s.Targets("u-3"); ok {
		t.Error("ohne Datei darf es keinen Semiadmin geben")
	}
}

// Eine leere Datei entsteht per touch beim Vorbereiten eines Bind-Mounts.
// Sie ist kein kaputtes JSON, sondern ein leerer Stand.
func TestLoadLeereDateiIstLeer(t *testing.T) {
	for _, inhalt := range []string{"", "  \n\t"} {
		p := pfad(t)
		if err := os.WriteFile(p, []byte(inhalt), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := settings.Load(p)
		if err != nil {
			t.Fatalf("Inhalt %q: unerwarteter Fehler: %v", inhalt, err)
		}
		if n := len(s.Semiadmins()); n != 0 {
			t.Errorf("Inhalt %q: %d Einträge, erwartet 0", inhalt, n)
		}
	}
}

func TestRundlaufAusSpeichernUndLaden(t *testing.T) {
	p := pfad(t)
	s := laden(t, p)
	if err := s.Set("u-3", []string{"u-2", "u-4"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("u-5", []string{}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	neu := laden(t, p)
	if got, ok := neu.Targets("u-3"); !ok || !reflect.DeepEqual(got, []string{"u-2", "u-4"}) {
		t.Errorf("Targets(u-3) = %v, %v — erwartet [u-2 u-4], true", got, ok)
	}
	if got, ok := neu.Targets("u-5"); !ok || len(got) != 0 {
		t.Errorf("Targets(u-5) = %v, %v — erwartet [], true", got, ok)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("Dateirechte = %o, erwartet 600", perm)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version": 1`) {
		t.Errorf("Datei ohne Version: %s", b)
	}
}

func TestSetLegtVerzeichnisMit0700An(t *testing.T) {
	p := filepath.Join(t.TempDir(), "neu", "settings.json")
	s := laden(t, p)
	if err := s.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	info, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("Verzeichnisrechte = %o, erwartet 700", perm)
	}
}

// "users": null im Request kommt hier als nil an. Gespeichert werden muss
// eine leere Liste, sonst stünde null in der Datei und Targets lieferte nil.
func TestSetOhneListeSpeichertLeereListe(t *testing.T) {
	p := pfad(t)
	s := laden(t, p)
	if err := s.Set("u-3", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := s.Targets("u-3")
	if !ok {
		t.Fatal("Semiadmin mit leerer Liste muss eingetragen sein")
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Targets = %#v, erwartet leere Liste statt nil", got)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"u-3": []`) || strings.Contains(string(b), "null") {
		t.Errorf("Datei = %s, erwartet \"u-3\": [] ohne null", b)
	}
}

func TestLoadKaputtesJSON(t *testing.T) {
	p := pfad(t)
	if err := os.WriteFile(p, []byte(`{"version": 1, "semiadmins": {`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := settings.Load(p)
	if err == nil {
		t.Fatal("Fehler erwartet — kaputtes JSON darf nicht still überschrieben werden")
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("Fehlermeldung ohne Pfad: %v", err)
	}
}

func TestLoadUnbekannteVersion(t *testing.T) {
	for _, inhalt := range []string{`{"version": 2, "semiadmins": {}}`, `{"semiadmins": {}}`} {
		p := pfad(t)
		if err := os.WriteFile(p, []byte(inhalt), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := settings.Load(p)
		if err == nil {
			t.Errorf("Inhalt %s: Fehler erwartet", inhalt)
			continue
		}
		if !strings.Contains(err.Error(), "Version") {
			t.Errorf("Inhalt %s: Fehlermeldung ohne Hinweis auf die Version: %v", inhalt, err)
		}
	}
}

// SETTINGS_FILE zeigt versehentlich auf das gemountete Verzeichnis statt auf
// eine Datei darin. Das muss beim Start auffallen.
func TestLoadPfadIstVerzeichnis(t *testing.T) {
	if _, err := settings.Load(t.TempDir()); err == nil {
		t.Fatal("Fehler erwartet, wenn der Pfad ein Verzeichnis ist")
	}
}

// Der typische Deployment-Fehler: das Verzeichnis gehört root statt dem
// nonroot-Benutzer. Speichern scheitert, der alte Stand bleibt gültig —
// im Speicher und auf der Platte.
func TestSchreibfehlerLaesstStandUnveraendert(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root greifen Dateirechte nicht")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	s := laden(t, p)
	if err := s.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := s.Set("u-3", []string{"u-4"}); err == nil {
		t.Error("Set muss scheitern, wenn das Verzeichnis nicht beschreibbar ist")
	}
	if err := s.Set("u-5", []string{"u-2"}); err == nil {
		t.Error("Set eines neuen Semiadmins muss ebenso scheitern")
	}
	if err := s.Remove("u-3"); err == nil {
		t.Error("Remove muss scheitern, wenn das Verzeichnis nicht beschreibbar ist")
	}
	if got, ok := s.Targets("u-3"); !ok || !reflect.DeepEqual(got, []string{"u-2"}) {
		t.Errorf("Targets(u-3) = %v, %v — der alte Stand muss gültig bleiben", got, ok)
	}
	if _, ok := s.Targets("u-5"); ok {
		t.Error("ein nicht gespeicherter Eintrag darf nicht im Speicher stehen")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, ok := laden(t, p).Targets("u-3"); !ok || !reflect.DeepEqual(got, []string{"u-2"}) {
		t.Errorf("Datei: Targets(u-3) = %v, %v — erwartet den alten Stand [u-2]", got, ok)
	}
}

func TestRemoveEntferntUndSpeichert(t *testing.T) {
	p := pfad(t)
	s := laden(t, p)
	if err := s.Set("u-3", []string{"u-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Remove("u-3"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := s.Targets("u-3"); ok {
		t.Error("u-3 muss entfernt sein")
	}
	if _, ok := laden(t, p).Targets("u-3"); ok {
		t.Error("u-3 muss auch in der Datei entfernt sein")
	}
	if err := s.Remove("gibt-es-nicht"); err != nil {
		t.Errorf("Remove einer unbekannten ID = %v, erwartet nil", err)
	}
}

// Aufrufer dürfen den Speicherstand weder über die übergebene noch über die
// gelieferte Liste verändern können.
func TestKopienSindUnabhaengig(t *testing.T) {
	s := laden(t, pfad(t))
	eingabe := []string{"u-2"}
	if err := s.Set("u-3", eingabe); err != nil {
		t.Fatalf("Set: %v", err)
	}
	eingabe[0] = "x"
	got, _ := s.Targets("u-3")
	got[0] = "y"
	alle := s.Semiadmins()
	alle["u-3"][0] = "z"
	alle["u-9"] = nil

	if got, _ := s.Targets("u-3"); got[0] != "u-2" {
		t.Errorf("Targets(u-3) = %v, erwartet [u-2]", got)
	}
	if _, ok := s.Targets("u-9"); ok {
		t.Error("ein Eintrag in der Kopie darf nicht im Store landen")
	}
}

// Zwei gleichzeitige Speicherungen dürfen sich nicht gegenseitig
// überschreiben; -race (make test) prüft zusätzlich die Sperren.
func TestParallelerZugriff(t *testing.T) {
	p := pfad(t)
	s := laden(t, p)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("s-%d", i)
			if err := s.Set(id, []string{"u-2"}); err != nil {
				t.Errorf("Set(%s): %v", id, err)
			}
			s.Targets(id)
			s.Semiadmins()
		}(i)
	}
	wg.Wait()
	if n := len(laden(t, p).Semiadmins()); n != 20 {
		t.Errorf("%d Einträge in der Datei, erwartet 20", n)
	}
}

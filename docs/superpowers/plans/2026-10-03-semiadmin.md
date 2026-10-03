# Semiadmins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Nicht privilegierte Jellyfin-User („Semiadmins“) können die ihnen von einem Admin zugewiesenen User aktivieren und deaktivieren, ohne Jellyfin-Adminrechte zu bekommen.
**Architecture:** Ein neues Paket `internal/settings` speichert die Zuordnung Semiadmin → schaltbare User-IDs atomar in einer JSON-Datei. Sitzungen tragen eine Rolle (`Admin bool`); Admin-Routen sind per `nurAdmin` gesperrt, Semiadmins haben eine eigene, schmale Ansicht (`GET /` → `controlled.html`) und einen eigenen Endpunkt (`PUT /api/controlled/{id}/active`), der als einziger den Jellyfin-API-Key benutzt und bei jeder Anfrage die Rolle neu prüft. Admins arbeiten unverändert mit ihrem eigenen Token und pflegen die Zuordnung auf der Detailseite (`PUT /api/users/{id}/delegation`).
**Tech Stack:** Go 1.23 (Modul `jellyfinenabler`, nur Standardbibliothek: `net/http`, `html/template`, `encoding/json`), Tailwind v4 (vorkompiliertes `web/static/app.css`), Vanilla-JS (`web/static/app.js`), Docker (distroless/static nonroot), Make.
**Spec:** docs/superpowers/specs/2026-10-03-semiadmin-design.md

## Global Constraints

- Nur Standardbibliothek, keine neuen Abhängigkeiten; `go.mod` bleibt unverändert.
- Go 1.23: keine APIs, die neuer sind (also kein `t.Context()`, kein `t.Chdir`, kein `os.Root`, kein `b.Loop`).
- `make check` (fmt-check, vet, `go test -race ./...`) muss nach jedem Task grün sein. Vor `make check` jede geänderte Go-Datei mit `gofmt -w <datei>` formatieren.
- Alle UI- und Fehlertexte deutsch und wörtlich wie in diesem Plan (er übernimmt die Texte der Spec wörtlich).
- Der API-Key erscheint nie in Logs, in der JSON-Datei oder im HTML.
- ENV-Namen: `JELLYFIN_API_KEY` (Vorgabe leer) und `SETTINGS_FILE` (Vorgabe `/data/settings.json`).
- Commits als Conventional Commits mit deutschem Betreff, `git add` nur mit expliziten Pfaden, Commit-Nachricht endet mit dem Trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Nicht pushen.
- Nach einem Edit die Datei nicht erneut lesen; das Edit-Ergebnis reicht.
- Symbole (Definitionen, Verwender, Signaturen) per LSP (gopls) auflösen, nicht per grep.
- Bestehender Stil: deutsche Bezeichner und Kommentare (`seite`, `api`, `aufbau`, `angemeldet`), Kommentare begründen das Warum.
- Neue Tailwind-Klassen sind nicht vorgesehen: Templates in Task 5 und 6 benutzen nur Klassen, die bereits in `web/static/app.css` stehen. `make css` läuft einmal in Task 7.

## Review Focus

1. **`SETTINGS_FILE` liegt in einem nicht beschreibbaren Verzeichnis** (Bind-Mount gehört root statt 65532, der typische Deployment-Fehler): Der Start klappt (fehlende Datei = leerer Stand), jedes Speichern einer Zuordnung antwortet 500 „Einstellungen konnten nicht gespeichert werden.“, Speicherstand und Datei bleiben unverändert; zeigt `SETTINGS_FILE` auf ein Verzeichnis, bricht der Start mit Fehlermeldung ab. Tests: Task 2 `TestSchreibfehlerLaesstStandUnveraendert`, `TestLoadPfadIstVerzeichnis`; Task 5 `TestDelegationSchreibfehlerGibt500`.
2. **Leere oder nur aus Leerraum bestehende Settings-Datei** (entsteht per `touch` beim Vorbereiten des Mounts): gilt wie eine fehlende Datei als leerer Stand, kein Startabbruch. Test: Task 2 `TestLoadLeereDateiIstLeer`.
3. **API-Key mit umgebendem Leerraum oder CRLF** (aus `.env` oder Compose-Datei): wird mit `strings.TrimSpace` bereinigt; Anführungszeichen werden nicht entfernt (Spec: dieselben Trim-Regeln wie bisher), ein so verfälschter Key fällt bei der Startprüfung als Warnung auf, ohne dass der Key im Log steht. Tests: Task 1 `TestFromEnvSemiadminWerteWerdenGetrimmt`; Task 4 `TestPruefeAPIKeyWarntOhneKeyImLog`.
4. **`"users": null` oder fehlendes `users` bei `"semiadmin": true`**: gilt als leere Liste, wird als `[]` (nie `null`) gespeichert und geantwortet; der Semiadmin kann sich anmelden und sieht „Dir sind keine Benutzer zugewiesen.“. Tests: Task 2 `TestSetOhneListeSpeichertLeereListe`; Task 5 `TestDelegationOhneListeIstLeer`.
5. **Der Semiadmin steht in seiner eigenen Zielliste** (alte oder von Hand bearbeitete Datei; die 400-Regel von `/delegation` greift dort nicht): Er erscheint nicht in seiner Ansicht, `PUT /api/controlled/<eigene ID>/active` antwortet 403 „Dieser Benutzer ist dir nicht zugewiesen.“ und schaltet nichts. Tests: Task 6 `TestSemiadminAnsichtZeigtNurZugewiesene`, `TestSchaltenLehntAb`.

## Vorbedingung (vor Task 1, durch den Orchestrator)

Die Spec verlangt vor der Umsetzung einen einmaligen Nachweis gegen die echte Jellyfin-Instanz (10.11), dass ein API-Key im MediaBrowser-Header angenommen wird. Mit gesetzten Shell-Variablen `JELLYFIN_URL` und `JELLYFIN_API_KEY`:

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: MediaBrowser Client=\"Jellyfin Enabler\", Device=\"Jellyfin Enabler\", Version=\"dev\", Token=\"$JELLYFIN_API_KEY\"" \
  "$JELLYFIN_URL/Users"
```

Erwartet: `200`. Bei jedem anderen Ergebnis die Umsetzung anhalten und den Benutzer fragen (Spec §Verifikation vor der Umsetzung).

## Dateistruktur

| Datei | Task | Zweck |
|---|---|---|
| `internal/config/config.go`, `config_test.go` | 1 | `APIKey`, `SettingsFile` |
| `internal/settings/store.go`, `store_test.go` (neu) | 2 | Settings-Datei |
| `internal/jellyfin/types.go`, `client.go`, `client_test.go` | 3 | `Session.Admin`, `SetDisabled` |
| `internal/server/server.go`, `server_test.go` | 3, 4, 5, 6 | Rollen, Delegation, Semiadmin-Pfad |
| `internal/session/store.go`, `store_test.go` | 4 | `Session.Admin`, `Create(…, admin bool)` |
| `internal/server/rollen_test.go` (neu) | 4 | Login-Matrix, `nurAdmin` |
| `internal/server/delegation_test.go` (neu) | 5 | `/delegation`, Detailseite, Pille |
| `internal/server/semiadmin_test.go` (neu) | 6 | Semiadmin-Ansicht, Schalten, Rollenmatrix |
| `cmd/server/main.go`, `cmd/server/apikey_test.go` (neu) | 4 | Settings laden, Startprüfung, Verdrahtung |
| `web/templates/login.html`, `controlled.html` (neu) | 4, 6 | Beschriftung, Semiadmin-Ansicht |
| `web/templates/user.html`, `users.html`, `web/static/app.js` | 5, 6 | Abschnitt „Semiadmin“, Pille, Speichern/Schalten |
| `Dockerfile`, `docker-compose.yml`, `.gitignore`, `Makefile`, `README.md`, `web/static/app.css` | 7 | Deployment und Doku |

---

### Task 1: Konfiguration

**Files:**
- Modify: `internal/config/config.go:13-22` (Struct `Config`), `internal/config/config.go:45-50` (Vorgaben in `FromEnv`)
- Test: `internal/config/config_test.go` (Tests am Dateiende anfügen)

**Interfaces:**
- Consumes: nichts
- Produces:
  - `config.Config.APIKey string` — aus `JELLYFIN_API_KEY`, `strings.TrimSpace`, Vorgabe `""`
  - `config.Config.SettingsFile string` — aus `SETTINGS_FILE`, `strings.TrimSpace`, Vorgabe `"/data/settings.json"` (auch wenn der Wert nur aus Leerraum besteht)

- [ ] **Step 1: Fehlschlagenden Test schreiben**

Am Ende von `internal/config/config_test.go` anfügen (Imports `errors`, `testing`, `time`, `config` sind schon vorhanden; neue Imports sind nicht nötig):

```go

func TestFromEnvSemiadminVorgaben(t *testing.T) {
	cfg, err := config.FromEnv(env(map[string]string{"JELLYFIN_URL": "http://jellyfin:8096"}))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	// Leer heißt: Semiadmin-Funktion aus, Verhalten wie bisher.
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, erwartet leer", cfg.APIKey)
	}
	if cfg.SettingsFile != "/data/settings.json" {
		t.Errorf("SettingsFile = %q, erwartet /data/settings.json", cfg.SettingsFile)
	}
}

// Ein API-Key aus einer .env mit Windows-Zeilenenden oder mit Leerraum aus
// der Compose-Datei darf nicht samt Leerraum an Jellyfin gehen — sonst
// scheitert jeder Semiadmin-Aufruf mit 401, ohne dass man den Grund sieht.
func TestFromEnvSemiadminWerteWerdenGetrimmt(t *testing.T) {
	cfg, err := config.FromEnv(env(map[string]string{
		"JELLYFIN_URL":     "http://jellyfin:8096",
		"JELLYFIN_API_KEY": "  key-123\r\n",
		"SETTINGS_FILE":    " ./data/settings.json ",
	}))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.APIKey != "key-123" {
		t.Errorf("APIKey = %q, erwartet key-123", cfg.APIKey)
	}
	if cfg.SettingsFile != "./data/settings.json" {
		t.Errorf("SettingsFile = %q, erwartet ./data/settings.json", cfg.SettingsFile)
	}
}

// Ein SETTINGS_FILE aus reinem Leerraum ist ein Versehen, kein Pfad.
func TestFromEnvLeererSettingsPfadBleibtBeiDerVorgabe(t *testing.T) {
	cfg, err := config.FromEnv(env(map[string]string{
		"JELLYFIN_URL":  "http://jellyfin:8096",
		"SETTINGS_FILE": "   ",
	}))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.SettingsFile != "/data/settings.json" {
		t.Errorf("SettingsFile = %q, erwartet /data/settings.json", cfg.SettingsFile)
	}
}
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/config/
```

Erwartet: Build-Fehler mit `cfg.APIKey undefined (type config.Config has no field or method APIKey)` und `FAIL	jellyfinenabler/internal/config [build failed]`.

- [ ] **Step 3: Minimale Implementierung**

In `internal/config/config.go` den Struct ersetzen.

Alt:

```go
	CookieSecure string
	HTTPTimeout  time.Duration
}
```

Neu:

```go
	CookieSecure string
	HTTPTimeout  time.Duration
	// APIKey ist der Jellyfin-API-Key für den Semiadmin-Pfad. Leer heißt:
	// Semiadmin-Funktion aus (Spec §Konfiguration und Deployment). Er ist ein
	// vollwertiger Admin-Zugang und darf deshalb nie geloggt werden.
	APIKey string
	// SettingsFile ist der Pfad der JSON-Datei mit den Semiadmin-Zuordnungen.
	SettingsFile string
}
```

In `FromEnv` die Vorgaben und das Einlesen ergänzen.

Alt:

```go
	cfg := Config{
		SessionTTL:   12 * time.Hour,
		CookieSecure: "auto",
		HTTPTimeout:  15 * time.Second,
	}
	cfg.JellyfinURL = strings.TrimRight(strings.TrimSpace(getenv("JELLYFIN_URL")), "/")
	if cfg.JellyfinURL == "" {
		return Config{}, ErrNoJellyfinURL
	}
```

Neu:

```go
	cfg := Config{
		SessionTTL:   12 * time.Hour,
		CookieSecure: "auto",
		HTTPTimeout:  15 * time.Second,
		SettingsFile: "/data/settings.json",
	}
	cfg.JellyfinURL = strings.TrimRight(strings.TrimSpace(getenv("JELLYFIN_URL")), "/")
	if cfg.JellyfinURL == "" {
		return Config{}, ErrNoJellyfinURL
	}
	// Getrimmt wie JELLYFIN_URL: ein Zeilenende aus einer .env gehört nicht
	// zum Key. Anführungszeichen bleiben stehen — ein so verfälschter Key
	// fällt bei der Startprüfung in cmd/server als Warnung auf.
	cfg.APIKey = strings.TrimSpace(getenv("JELLYFIN_API_KEY"))
	if f := strings.TrimSpace(getenv("SETTINGS_FILE")); f != "" {
		cfg.SettingsFile = f
	}
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
go test ./internal/config/
make check
```

Erwartet: `ok  	jellyfinenabler/internal/config`; `make check` endet ohne Fehler (alle Pakete `ok`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): JELLYFIN_API_KEY und SETTINGS_FILE einlesen" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: Paket `internal/settings`

**Files:**
- Create: `internal/settings/store.go`
- Test: `internal/settings/store_test.go` (neu)

**Interfaces:**
- Consumes: nichts
- Produces:
  - `func settings.Load(path string) (*settings.Store, error)` — fehlende oder leere Datei = leerer Stand; kaputtes JSON, unbekannte `version` oder unlesbarer Pfad (z. B. Verzeichnis) = Fehler
  - `func (s *Store) Targets(semiadminID string) (targets []string, ok bool)` — Kopie; bei `ok == true` nie `nil`
  - `func (s *Store) Semiadmins() map[string][]string` — Kopie
  - `func (s *Store) Set(semiadminID string, targets []string) error` — speichert sofort; `nil` wird als `[]` gespeichert
  - `func (s *Store) Remove(semiadminID string) error` — speichert sofort; unbekannte ID = `nil` ohne Schreibzugriff
  - Dateiformat `{"version": 1, "semiadmins": {"<id>": ["<id>", …]}}`, eingerückt mit zwei Leerzeichen, Dateirechte 0600, Verzeichnis 0700

- [ ] **Step 1: Fehlschlagenden Test schreiben**

`internal/settings/store_test.go` anlegen:

```go
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
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/settings/
```

Erwartet: Build-Fehler, weil das Paket noch keinen Code hat, z. B. `no non-test Go files in …/internal/settings` oder `undefined: settings.Load`; Endzeile `FAIL	jellyfinenabler/internal/settings [setup failed]` bzw. `[build failed]`.

- [ ] **Step 3: Minimale Implementierung**

`internal/settings/store.go` anlegen:

```go
// Package settings hält die Semiadmin-Zuordnungen in einer JSON-Datei. Sie ist
// die einzige Persistenz des Dienstes und übersteht Neustarts
// (Spec §Settings-Datei und Paket internal/settings).
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// dateiVersion ist die einzige Version des Dateiformats, die dieser Stand
// versteht. Eine andere bricht den Start ab, statt die Datei still zu
// überschreiben.
const dateiVersion = 1

// datei ist das Format auf der Platte. Schlüssel und Listeneinträge sind
// Jellyfin-User-IDs, keine Namen — Namen können sich ändern.
type datei struct {
	Version    int                 `json:"version"`
	Semiadmins map[string][]string `json:"semiadmins"`
}

// Store hält den Stand im Speicher und schreibt jede Änderung sofort.
type Store struct {
	mu         sync.RWMutex
	path       string
	semiadmins map[string][]string
}

// Load liest die Datei. Eine fehlende oder leere Datei gilt als leerer
// Stand; eine leere entsteht typischerweise per touch beim Vorbereiten
// eines Bind-Mounts. Unlesbares JSON, eine unbekannte Version oder ein Pfad,
// der sich nicht lesen lässt (z. B. ein Verzeichnis), sind Fehler.
func Load(path string) (*Store, error) {
	s := &Store{path: path, semiadmins: map[string][]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Settings-Datei %s nicht lesbar: %w", path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return s, nil
	}
	var d datei
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("Settings-Datei %s ist kein gültiges JSON: %w", path, err)
	}
	if d.Version != dateiVersion {
		return nil, fmt.Errorf("Settings-Datei %s hat die unbekannte Version %d (erwartet %d)", path, d.Version, dateiVersion)
	}
	for id, targets := range d.Semiadmins {
		s.semiadmins[id] = kopie(targets)
	}
	return s, nil
}

// kopie liefert eine unabhängige, nie nil-wertige Kopie. nil wird zur
// leeren Liste, damit in der Datei [] statt null steht.
func kopie(ids []string) []string {
	return append([]string{}, ids...)
}

// stand kopiert den Speicherstand. Der Aufrufer hält s.mu.
func (s *Store) stand() map[string][]string {
	m := make(map[string][]string, len(s.semiadmins))
	for id, targets := range s.semiadmins {
		m[id] = kopie(targets)
	}
	return m
}

// Targets liefert die User-IDs, die semiadminID schalten darf. ok ist
// false, wenn semiadminID kein Semiadmin ist.
func (s *Store) Targets(semiadminID string) (targets []string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.semiadmins[semiadminID]
	if !ok {
		return nil, false
	}
	return kopie(t), true
}

// Semiadmins liefert alle Zuordnungen, für die Pillen in der Benutzerliste.
func (s *Store) Semiadmins() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stand()
}

// Set trägt semiadminID mit seiner Zielliste ein und speichert sofort. Der
// Speicherstand wird erst nach erfolgreichem Schreiben ersetzt; schlägt das
// Schreiben fehl, bleibt der alte Stand gültig. Die Schreibsperre gilt über
// das Schreiben hinweg, damit sich zwei gleichzeitige Speicherungen nicht
// gegenseitig überschreiben.
func (s *Store) Set(semiadminID string, targets []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	neu := s.stand()
	neu[semiadminID] = kopie(targets)
	if err := s.schreiben(neu); err != nil {
		return err
	}
	s.semiadmins = neu
	return nil
}

// Remove entfernt semiadminID und speichert sofort. Eine unbekannte ID ist
// kein Fehler; dann wird auch nichts geschrieben.
func (s *Store) Remove(semiadminID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.semiadmins[semiadminID]; !ok {
		return nil
	}
	neu := s.stand()
	delete(neu, semiadminID)
	if err := s.schreiben(neu); err != nil {
		return err
	}
	s.semiadmins = neu
	return nil
}

// schreiben ersetzt die Datei atomar: Temp-Datei im selben Verzeichnis
// (0600), fsync, Rename. Ein Absturz mittendrin hinterlässt so entweder den
// alten oder den neuen Stand, nie eine halbe Datei.
func (s *Store) schreiben(semiadmins map[string][]string) error {
	b, err := json.MarshalIndent(datei{Version: dateiVersion, Semiadmins: semiadmins}, "", "  ")
	if err != nil {
		return err
	}
	verz := filepath.Dir(s.path)
	if err := os.MkdirAll(verz, 0o700); err != nil {
		return fmt.Errorf("Settings-Verzeichnis %s nicht anlegbar: %w", verz, err)
	}
	tmp, err := os.CreateTemp(verz, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("Settings-Datei in %s nicht schreibbar: %w", verz, err)
	}
	// Bei jedem Fehler unten bleibt keine halbe Temp-Datei liegen.
	fertig := false
	defer func() {
		if !fertig {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	fertig = true
	return nil
}
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/settings/store.go internal/settings/store_test.go
go test -race ./internal/settings/
make check
```

Erwartet: `ok  	jellyfinenabler/internal/settings`; `make check` ohne Fehler.

- [ ] **Step 5: Commit**

```bash
git add internal/settings/store.go internal/settings/store_test.go
git commit -m "feat(settings): Semiadmin-Zuordnungen als JSON-Datei speichern" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: Jellyfin-Client — Admin-Flag und `SetDisabled`

**Files:**
- Modify: `internal/jellyfin/types.go:31-35` (`Session`)
- Modify: `internal/jellyfin/client.go:3-18` (Imports), `:23-25` (`ErrNotAdmin` entfällt), `:102-128` (`Authenticate`), `:228-282` (`UpdateAccess` → Policy-Helfer + `SetDisabled`)
- Modify: `internal/server/server.go:23-30` (Interface `Jellyfin`), `:150-166` (`handleLogin`)
- Test: `internal/jellyfin/client_test.go:149-171` (ersetzen) und Tests am Dateiende; `internal/server/server_test.go:20-83` (`fakeJF`, `aufbau`, `aufbauMit`), `:193-203` (ersetzen)

**Interfaces:**
- Consumes: nichts aus früheren Tasks
- Produces:
  - `jellyfin.Session{Token, UserID, Name string; Admin bool}`
  - `func (c *jellyfin.Client) Authenticate(ctx context.Context, username, password string) (jellyfin.Session, error)` — weist Nicht-Admins nicht mehr ab, meldet nicht mehr selbst ab
  - `func (c *jellyfin.Client) SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error)` — ändert nur `IsDisabled`; liefert den geschriebenen Wert; bei fehlendem `IsAdministrator` `*jellyfin.Error{Status: 502}` ohne Schreibzugriff
  - `func (c *jellyfin.Client) policyAendern(ctx context.Context, token, userID string, felder map[string]any) error` (intern, gemeinsamer Helfer)
  - `jellyfin.ErrNotAdmin` entfällt
  - Server-Interface `Jellyfin` zusätzlich: `SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error)`
  - `func (s *Server) abweisen(w http.ResponseWriter, r *http.Request, token string)` — Logout des Tokens (Fehler nur loggen), 403 mit „Dieses Konto hat keinen Zugriff auf diesen Dienst.“
  - Test-Fake `fakeJF` zusätzlich: Felder `logoutErr error`, `setToken string`, `setID string`, `setDisabled *bool`, `setErr error`; Methode `SetDisabled`; `aufbau`/`aufbauMit` liefern eine Admin-Anmeldung (`Admin: true`)

#### Zyklus A: Client

- [ ] **Step 1: Fehlschlagenden Test schreiben**

In `internal/jellyfin/client_test.go` den Test `TestAuthenticateLehntNichtAdminAbUndMeldetAb` (Zeilen 149-171) vollständig ersetzen.

Alt (Anfang und Ende des zu ersetzenden Blocks):

```go
func TestAuthenticateLehntNichtAdminAbUndMeldetAb(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, `{
	  "AccessToken": "tok-kind",
```

… bis einschließlich

```go
	if !strings.Contains(f.call(1).Auth, `Token="tok-kind"`) {
		t.Errorf("Abmeldung ohne das erzeugte Token: %q", f.call(1).Auth)
	}
}
```

Neu:

```go
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
```

Am Ende von `internal/jellyfin/client_test.go` anfügen:

```go

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
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/jellyfin/
```

Erwartet: Build-Fehler mit `sess.Admin undefined (type jellyfin.Session has no field or method Admin)` und `f.client().SetDisabled undefined (type *jellyfin.Client has no field or method SetDisabled)`, Endzeile `FAIL	jellyfinenabler/internal/jellyfin [build failed]`.

- [ ] **Step 3: Minimale Implementierung**

`internal/jellyfin/types.go`, Session ersetzen.

Alt:

```go
// Session ist das Ergebnis einer erfolgreichen Anmeldung.
type Session struct {
	Token  string
	UserID string
	Name   string
}
```

Neu:

```go
// Session ist das Ergebnis einer erfolgreichen Anmeldung. Admin trägt
// Policy.IsAdministrator; ob ein Nicht-Admin hinein darf, entscheidet der
// Server (Spec §Anmeldung, Sitzung und Rollen).
type Session struct {
	Token  string
	UserID string
	Name   string
	Admin  bool
}
```

`internal/jellyfin/client.go`, Imports: `"errors"` und `"log"` entfallen (beide wurden nur von `ErrNotAdmin` bzw. dem Logout in `Authenticate` benutzt).

Alt:

```go
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
```

Neu:

```go
	"encoding/json"
	"fmt"
	"io"
	"net/http"
```

`ErrNotAdmin` entfernen. Alt:

```go
// ErrNotAdmin meldet eine gültige Anmeldung ohne Administratorrechte. Der
// Dienst schreibt Policies und braucht RequiresElevation.
var ErrNotAdmin = errors.New("dieser Benutzer ist kein Administrator")

```

Neu: (leer — die drei Zeilen und die folgende Leerzeile ersatzlos streichen)

`Authenticate`, Ende der Funktion. Alt:

```go
	if err != nil {
		return Session{}, err
	}
	if !res.User.Policy.IsAdministrator {
		// Kein verwaistes Token zurücklassen. Scheitert das, bleibt ein
		// gültiges Admin-fremdes Token bestehen, ohne dass es sonst irgendwo
		// auffiele — deshalb wird ein Fehlschlag geloggt statt verworfen.
		if err := c.Logout(ctx, res.AccessToken); err != nil {
			log.Printf("Token eines abgelehnten Anmeldeversuchs konnte nicht entwertet werden: %v", err)
		}
		return Session{}, ErrNotAdmin
	}
	return Session{Token: res.AccessToken, UserID: res.User.ID, Name: res.User.Name}, nil
}
```

Neu:

```go
	if err != nil {
		return Session{}, err
	}
	// Kein Abweisen mehr an dieser Stelle: Auch Semiadmins melden sich mit
	// einem gewöhnlichen Konto an. Wer ohne Rolle kommt, weist der Server ab
	// und entwertet dabei das Token.
	return Session{
		Token:  res.AccessToken,
		UserID: res.User.ID,
		Name:   res.User.Name,
		Admin:  res.User.Policy.IsAdministrator,
	}, nil
}
```

`UpdateAccess` (Zeilen 228-282, vom Kommentar `// UpdateAccess liest die Policy frisch …` bis zum Dateiende) vollständig ersetzen durch:

```go
// policyAendern liest die Policy frisch, ersetzt genau die Schlüssel aus
// felder und schreibt sie zurück. POST /Users/{id}/Policy ersetzt die Policy
// vollständig — deshalb Read-Modify-Write über die Rohdaten, damit die
// übrigen Felder unangetastet bleiben (Spec §Policy-Passthrough). UpdateAccess
// und SetDisabled gehen beide hier durch, damit sie nicht auseinanderlaufen.
func (c *Client) policyAendern(ctx context.Context, token, userID string, felder map[string]any) error {
	r, err := c.rawUser(ctx, token, userID)
	if err != nil {
		return err
	}
	// Die Policy wird vollständig ersetzt. Bevor das passiert, muss
	// plausibel sein, dass wir wirklich eine vollständige Policy gelesen
	// haben — sonst schriebe ein ausgedünnter Rumpf (Reverse Proxy bei
	// Teilausfall, geänderte DTO-Form nach einem Upgrade) eine Policy aus
	// wenigen Feldern zurück und nähme dem Benutzer stillschweigend
	// Transcoding-Rechte, Zeitfenster und Altersfreigabe.
	// IsAdministrator liefert Jellyfin immer und dieser Dienst schreibt es nie.
	if _, ok := r.Policy["IsAdministrator"]; !ok {
		return &Error{
			Status:  http.StatusBadGateway,
			Message: "Jellyfin hat eine unvollständige Policy geliefert. Es wurde nichts geändert.",
			Body:    fmt.Sprintf("Policy von %s ohne IsAdministrator, %d Felder", userID, len(r.Policy)),
		}
	}
	for name, wert := range felder {
		b, err := json.Marshal(wert)
		if err != nil {
			return err
		}
		r.Policy[name] = b
	}
	path := "/Users/" + url.PathEscape(userID) + "/Policy"
	return c.do(ctx, http.MethodPost, path, token, "", r.Policy, nil)
}

// UpdateAccess schreibt Aktiv-Status und Bibliotheksfreigaben.
func (c *Client) UpdateAccess(ctx context.Context, token, userID string, a Access) (Access, error) {
	// Jellyfin erwartet eine Liste, kein null.
	folders := a.Folders
	if folders == nil {
		folders = []string{}
	}
	err := c.policyAendern(ctx, token, userID, map[string]any{
		"IsDisabled":       a.Disabled,
		"EnableAllFolders": a.AllFolders,
		"EnabledFolders":   folders,
	})
	if err != nil {
		return Access{}, err
	}
	// Jellyfin antwortet auf das Schreiben mit 204 ohne Rumpf. Der
	// Rückgabewert ist deshalb der geschriebene, nicht ein nachgelesener
	// Stand — ein Nachlesen kostete einen zusätzlichen Request, den dieser
	// Entwurf bewusst vermeidet.
	return Access{Disabled: a.Disabled, AllFolders: a.AllFolders, Folders: folders}, nil
}

// SetDisabled ändert ausschließlich IsDisabled; Bibliotheksfreigaben bleiben
// unberührt. Das ist der einzige schreibende Aufruf des Semiadmin-Pfads, der
// ihn mit dem API-Key als token benutzt (Spec §Jellyfin-Client). Rückgabe
// ist wie bei UpdateAccess der geschriebene Wert.
func (c *Client) SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error) {
	if err := c.policyAendern(ctx, token, userID, map[string]any{"IsDisabled": disabled}); err != nil {
		return false, err
	}
	return disabled, nil
}
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/jellyfin/types.go internal/jellyfin/client.go internal/jellyfin/client_test.go
go test ./internal/jellyfin/
```

Erwartet: `ok  	jellyfinenabler/internal/jellyfin`. (Das Paket `internal/server` baut in diesem Moment noch nicht, weil es `jellyfin.ErrNotAdmin` benutzt; das behebt Zyklus B.)

#### Zyklus B: Server-Mindestanpassung

- [ ] **Step 1: Fehlschlagenden Test schreiben**

In `internal/server/server_test.go` den Fake erweitern. Struct-Ende alt:

```go
	writtenBack *jellyfin.Access
	loggedOut   []string
}
```

Neu:

```go
	writtenBack *jellyfin.Access
	loggedOut   []string
	logoutErr   error
	// SetDisabled: Token, Ziel und Wert des letzten Aufrufs. setDisabled
	// bleibt nil, solange nicht geschaltet wurde.
	setToken    string
	setID       string
	setDisabled *bool
	setErr      error
}
```

`Logout` des Fakes alt:

```go
func (f *fakeJF) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return nil
}
```

Neu:

```go
func (f *fakeJF) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return f.logoutErr
}
```

Direkt nach der Methode `UpdateAccess` des Fakes (endet mit `return a, nil` und `}`) einfügen:

```go

func (f *fakeJF) SetDisabled(_ context.Context, token, userID string, disabled bool) (bool, error) {
	f.setToken, f.setID, f.setDisabled = token, userID, &disabled
	if f.setErr != nil {
		return false, f.setErr
	}
	return disabled, nil
}
```

In `aufbau` und `aufbauMit` meldet sich ab jetzt ausdrücklich ein Administrator an. In beiden Funktionen ersetzen (die Zeile kommt zweimal vor):

Alt:

```go
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa"}}
```

Neu:

```go
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa", Admin: true}}
```

`TestLoginNichtAdminWirdAbgewiesen` (Zeilen 193-203) ersetzen. Alt:

```go
func TestLoginNichtAdminWirdAbgewiesen(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.authErr = jellyfin.ErrNotAdmin
	rec := anmelden(h, "kind1", "geheim")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Administrator") {
		t.Errorf("Rumpf ohne Hinweis auf fehlende Rechte: %s", rec.Body.String())
	}
}
```

Neu:

```go
// Ein Konto ohne Rolle bekommt kein Cookie, und das gerade erzeugte Token
// wird entwertet — sonst bliebe es ungenutzt in Jellyfin gültig.
func TestLoginOhneRolleWirdAbgewiesenUndAbgemeldet(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.sess = jellyfin.Session{Token: "tok-kind", UserID: "u-2", Name: "kind1"}
	rec := anmelden(h, "kind1", "geheim")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Dieses Konto hat keinen Zugriff auf diesen Dienst.") {
		t.Errorf("Rumpf ohne Ablehnung: %s", rec.Body.String())
	}
	if len(jf.loggedOut) != 1 || jf.loggedOut[0] != "tok-kind" {
		t.Errorf("Jellyfin-Abmeldung = %v, erwartet [tok-kind]", jf.loggedOut)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jfe_session" {
			t.Error("ein abgewiesenes Konto darf kein Sitzungscookie bekommen")
		}
	}
}

// Scheitert die Abmeldung, bleibt es trotzdem bei der Ablehnung; der Fehler
// wird nur geloggt.
func TestLoginOhneRolleBleibtAbgewiesenWennAbmeldungScheitert(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.sess = jellyfin.Session{Token: "tok-kind", UserID: "u-2", Name: "kind1"}
	jf.logoutErr = errors.New("Verbindung abgelehnt")
	rec := anmelden(h, "kind1", "geheim")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
}
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/server/
```

Erwartet: Build-Fehler `server.go:157:…: undefined: jellyfin.ErrNotAdmin`, Endzeile `FAIL	jellyfinenabler/internal/server [build failed]`.

- [ ] **Step 3: Minimale Implementierung**

`internal/server/server.go`, Interface ergänzen. Alt:

```go
	UpdateAccess(ctx context.Context, token, userID string, a jellyfin.Access) (jellyfin.Access, error)
}
```

Neu:

```go
	UpdateAccess(ctx context.Context, token, userID string, a jellyfin.Access) (jellyfin.Access, error)
	SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error)
}
```

`handleLogin`, Fehlerauswertung ersetzen. Alt:

```go
	sess, err := s.jf.Authenticate(r.Context(), r.PostFormValue("username"), r.PostFormValue("password"))
	switch {
	case errors.Is(err, jellyfin.ErrNotAdmin):
		s.renderLogin(w, http.StatusForbidden, "Nur Administratoren dürfen diesen Dienst verwenden.")
		return
	case err != nil:
		// Benutzer- und Passwortfehler werden bewusst nicht unterschieden.
		log.Printf("Anmeldung fehlgeschlagen: %v", err)
		s.renderLogin(w, http.StatusUnauthorized, "Anmeldung fehlgeschlagen.")
		return
	}
	id, err := s.sessions.Create(sess.Token, sess.UserID, sess.Name)
```

Neu:

```go
	sess, err := s.jf.Authenticate(r.Context(), r.PostFormValue("username"), r.PostFormValue("password"))
	if err != nil {
		// Benutzer- und Passwortfehler werden bewusst nicht unterschieden.
		log.Printf("Anmeldung fehlgeschlagen: %v", err)
		s.renderLogin(w, http.StatusUnauthorized, "Anmeldung fehlgeschlagen.")
		return
	}
	if !sess.Admin {
		s.abweisen(w, r, sess.Token)
		return
	}
	id, err := s.sessions.Create(sess.Token, sess.UserID, sess.Name)
```

Direkt nach dem Ende von `handleLogin` (vor dem Kommentar `// secure entscheidet über das Secure-Flag.`) einfügen:

```go
// abweisen entwertet das gerade erhaltene Token eines Kontos ohne Rolle und
// zeigt die Ablehnung. Scheitert die Abmeldung, bleibt ein gültiges Token
// zurück, ohne dass es sonst irgendwo auffiele — deshalb wird ein
// Fehlschlag geloggt statt verworfen.
func (s *Server) abweisen(w http.ResponseWriter, r *http.Request, token string) {
	if err := s.jf.Logout(r.Context(), token); err != nil {
		log.Printf("Token eines abgelehnten Anmeldeversuchs konnte nicht entwertet werden: %v", err)
	}
	s.renderLogin(w, http.StatusForbidden, "Dieses Konto hat keinen Zugriff auf diesen Dienst.")
}

```

(`errors` bleibt in `server.go` importiert; es wird weiter von `seitenFehler`, `handleUserPage` und `apiFehler` benutzt.)

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/server/server.go internal/server/server_test.go
go test ./internal/server/ ./internal/jellyfin/
make check
```

Erwartet: `ok  	jellyfinenabler/internal/server` und `ok  	jellyfinenabler/internal/jellyfin`; `make check` ohne Fehler (auch `cmd/server` baut, weil `*jellyfin.Client` jetzt `SetDisabled` hat).

- [ ] **Step 5: Commit**

```bash
git add internal/jellyfin/types.go internal/jellyfin/client.go internal/jellyfin/client_test.go internal/server/server.go internal/server/server_test.go
git commit -m "feat(jellyfin): Admin-Flag bei der Anmeldung und SetDisabled" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: Rollen in Sitzung, Anmeldung und Server

**Files:**
- Modify: `internal/session/store.go:14-21` (`Session`), `:38-53` (`Create`)
- Modify: `internal/session/store_test.go` (sieben `Create`-Aufrufe, ein Test am Ende)
- Modify: `internal/server/server.go:5-17` (Imports), `:32-57` (`Server`, `New`, Routen), nach `:116` (`nurAdmin` u. a.), `handleLogin` (Rollenentscheidung), nach `handleIndex` (`handleStart`, `handleControlled`)
- Modify: `web/templates/login.html:8`
- Create: `web/templates/controlled.html`
- Modify: `cmd/server/main.go:16-21` (Imports), `:43-50` (Verdrahtung), Funktion `pruefeAPIKey` am Dateiende
- Test: `internal/server/server_test.go:3-18` (Imports), `:68-93` (Aufbau-Helfer); `internal/server/rollen_test.go` (neu); `cmd/server/apikey_test.go` (neu)

**Interfaces:**
- Consumes:
  - `config.Config.APIKey string`, `config.Config.SettingsFile string` (Task 1)
  - `settings.Load(path string) (*settings.Store, error)`, `(*settings.Store).Targets(semiadminID string) (targets []string, ok bool)`, `(*settings.Store).Set(semiadminID string, targets []string) error` (Task 2)
  - `jellyfin.Session.Admin bool`, `(*jellyfin.Client).Users(ctx context.Context, token string) ([]jellyfin.User, error)` (Task 3)
  - `(*Server).abweisen(w http.ResponseWriter, r *http.Request, token string)` (Task 3)
- Produces:
  - `session.Session{Token, UserID, Name string; Admin bool; Expires time.Time}`
  - `func (s *session.Store) Create(token, userID, name string, admin bool) (string, error)`
  - `func server.New(jf server.Jellyfin, sessions *session.Store, cookieSecure string, semiadmins *settings.Store, apiKey string) http.Handler`
  - `server.Server` zusätzlich: Felder `semiadmins *settings.Store`, `apiKey string`
  - `type sitzungsFn = func(http.ResponseWriter, *http.Request, session.Session)`
  - `func nurAdmin(fn sitzungsFn, abgelehnt http.HandlerFunc) sitzungsFn`; `func zurStartseite(w http.ResponseWriter, r *http.Request)` (303 nach `/`); `func keinAdmin(w http.ResponseWriter, _ *http.Request)` (403 JSON „Diese Aktion ist Administratoren vorbehalten.“)
  - `func (s *Server) semiadminEingetragen(userID string) bool` — API-Key gesetzt und `Targets(userID)` liefert `ok`
  - `func (s *Server) handleStart(w http.ResponseWriter, r *http.Request, sess session.Session)` auf `GET /{$}`
  - `func (s *Server) handleControlled(w http.ResponseWriter, r *http.Request, sess session.Session)` (Gerüst; Task 6 ersetzt den Rumpf)
  - `type controlledRow struct{ ID, Name string; Disabled bool }`, `type controlledData struct{ Title string; Users []controlledRow }`
  - Template `controlled` (Datei `web/templates/controlled.html`)
  - `func pruefeAPIKey(ctx context.Context, jf *jellyfin.Client, key string)` in `cmd/server`
  - Test-Helfer in `internal/server/server_test.go`: `const testKey = "key-123"`; `bauen(t *testing.T, cookieSecure, apiKey string) (http.Handler, *fakeJF, *session.Store, *settings.Store)`; `aufbau(t) (http.Handler, *fakeJF, *session.Store)` (ohne Key); `aufbauMit(t, cookieSecure string) (http.Handler, *fakeJF, *session.Store)` (ohne Key); `aufbauSemi(t) (http.Handler, *fakeJF, *session.Store, *settings.Store)` (mit `testKey`); `angemeldet(t, store) *http.Cookie` (Admin papa, `u-1`, Token `tok`); `semiadminAngemeldet(t, store) *http.Cookie` (anna, `u-3`, Token `tok-anna`, `Admin=false`); `familie() []jellyfin.User` (anna `u-3`, ben `u-4`, kind1 `u-2` deaktiviert, papa `u-1` Admin)
  - Test-Helfer in `internal/server/rollen_test.go`: `sitzungAusAntwort(t *testing.T, rec *httptest.ResponseRecorder, store *session.Store) (session.Session, bool)`

#### Zyklus A: Sitzung

- [ ] **Step 1: Fehlschlagenden Test schreiben**

Alle sieben Aufrufe von `Create` in `internal/session/store_test.go` bekommen `true` als viertes Argument:

```bash
perl -pi -e 's/s\.Create\(("[^"]*", "[^"]*", "[^"]*")\)/s.Create($1, true)/g' internal/session/store_test.go
grep -c 'Create(.*, true)' internal/session/store_test.go
```

Erwartet: `7`.

Am Ende von `internal/session/store_test.go` anfügen:

```go

// Die Rolle wird bei der Anmeldung festgelegt und mit der Sitzung gelesen;
// danach entscheidet der Server allein anhand dieses Felds über die
// Admin-Routen.
func TestCreateMerktSichDieRolle(t *testing.T) {
	s := NewStore(time.Hour)
	admin, err := s.Create("tok", "u-1", "papa", true)
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	semi, err := s.Create("tok-anna", "u-3", "anna", false)
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if sess, _ := s.Get(admin); !sess.Admin {
		t.Error("die Admin-Sitzung muss Admin=true tragen")
	}
	if sess, _ := s.Get(semi); sess.Admin {
		t.Error("die Semiadmin-Sitzung darf nicht Admin=true tragen")
	}
}
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/session/
```

Erwartet: Build-Fehler `too many arguments in call to s.Create` und `sess.Admin undefined (type Session has no field or method Admin)`, Endzeile `FAIL	jellyfinenabler/internal/session [build failed]`.

- [ ] **Step 3: Minimale Implementierung**

`internal/session/store.go`, `Session` ersetzen. Alt:

```go
// Session ist eine angemeldete Sitzung am Dienst. Token ist das
// Jellyfin-Zugriffstoken, mit dem alle Aufrufe laufen.
type Session struct {
	Token   string
	UserID  string
	Name    string
	Expires time.Time
}
```

Neu:

```go
// Session ist eine angemeldete Sitzung am Dienst. Token ist das
// Jellyfin-Zugriffstoken des Benutzers. Admin ist die Rolle zum Zeitpunkt
// der Anmeldung: true für Jellyfin-Administratoren, false für Semiadmins
// (Spec §Anmeldung, Sitzung und Rollen).
type Session struct {
	Token   string
	UserID  string
	Name    string
	Admin   bool
	Expires time.Time
}
```

`Create` ersetzen. Alt:

```go
// Create legt eine Sitzung an und liefert den Cookie-Wert.
func (s *Store) Create(token, userID, name string) (string, error) {
```

Neu:

```go
// Create legt eine Sitzung an und liefert den Cookie-Wert.
func (s *Store) Create(token, userID, name string, admin bool) (string, error) {
```

und im Rumpf von `Create`. Alt:

```go
		Name:    name,
		Expires: s.now().Add(s.ttl),
```

Neu:

```go
		Name:    name,
		Admin:   admin,
		Expires: s.now().Add(s.ttl),
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/session/store.go internal/session/store_test.go
go test ./internal/session/
```

Erwartet: `ok  	jellyfinenabler/internal/session`. (`internal/server` baut erst nach Zyklus B wieder; die Verwender von `Create` hat gopls per findReferences ermittelt: `internal/server/server.go:166` in `handleLogin` und `internal/server/server_test.go:88` in `angemeldet` — beide passt Zyklus B an.)

#### Zyklus B: Server, Login-Seite, Startprüfung

- [ ] **Step 1: Fehlschlagenden Test schreiben**

`internal/server/server_test.go`, Imports ersetzen. Alt:

```go
import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
)
```

Neu:

```go
import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/internal/settings"
)
```

Die Helfer `aufbau`, `aufbauMit` und `angemeldet` (vom Kommentar `// aufbau liefert Handler, Fake und Sitzungsspeicher.` bis zum Ende von `angemeldet`, also bis `return &http.Cookie{Name: "jfe_session", Value: id}` und `}`) vollständig ersetzen durch:

```go
// testKey ist der API-Key der Tests. Er darf in keiner Antwort und in
// keinem Log auftauchen.
const testKey = "key-123"

// bauen ist der gemeinsame Kern aller Aufbau-Helfer: Handler, Fake,
// Sitzungsspeicher und ein Settings-Store in einem frischen Testverzeichnis.
func bauen(t *testing.T, cookieSecure, apiKey string) (http.Handler, *fakeJF, *session.Store, *settings.Store) {
	t.Helper()
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa", Admin: true}}
	store := session.NewStore(time.Hour)
	zuordnung, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("Settings nicht ladbar: %v", err)
	}
	return server.New(jf, store, cookieSecure, zuordnung, apiKey), jf, store, zuordnung
}

// aufbau liefert Handler, Fake und Sitzungsspeicher ohne API-Key, also mit
// abgeschalteter Semiadmin-Funktion — das Verhalten wie vor den Semiadmins.
func aufbau(t *testing.T) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	h, jf, store, _ := bauen(t, "false", "")
	return h, jf, store
}

// aufbauMit erlaubt es, die Secure-Entscheidung zu prüfen; aufbau bleibt
// die knappe Variante für alle übrigen Tests.
func aufbauMit(t *testing.T, cookieSecure string) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	h, jf, store, _ := bauen(t, cookieSecure, "")
	return h, jf, store
}

// aufbauSemi baut mit gesetztem API-Key und liefert den Settings-Store mit,
// damit Tests Semiadmins eintragen können.
func aufbauSemi(t *testing.T) (http.Handler, *fakeJF, *session.Store, *settings.Store) {
	t.Helper()
	return bauen(t, "false", testKey)
}

// angemeldet legt eine Admin-Sitzung für papa (u-1) an.
func angemeldet(t *testing.T, store *session.Store) *http.Cookie {
	t.Helper()
	id, err := store.Create("tok", "u-1", "papa", true)
	if err != nil {
		t.Fatalf("Sitzung nicht anlegbar: %v", err)
	}
	return &http.Cookie{Name: "jfe_session", Value: id}
}

// semiadminAngemeldet legt eine Semiadmin-Sitzung für anna (u-3) an.
func semiadminAngemeldet(t *testing.T, store *session.Store) *http.Cookie {
	t.Helper()
	id, err := store.Create("tok-anna", "u-3", "anna", false)
	if err != nil {
		t.Fatalf("Sitzung nicht anlegbar: %v", err)
	}
	return &http.Cookie{Name: "jfe_session", Value: id}
}

// familie ist die Benutzerliste der Semiadmin-Tests: anna (u-3) ist die
// Semiadmin, ben (u-4) und kind1 (u-2, deaktiviert) sind mögliche Ziele,
// papa (u-1) ist Administrator. Sortiert nach Namen wie Users im Client.
func familie() []jellyfin.User {
	return []jellyfin.User{
		{ID: "u-3", Name: "anna"},
		{ID: "u-4", Name: "ben"},
		{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Disabled: true}},
		{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}},
	}
}
```

`internal/server/rollen_test.go` anlegen:

```go
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
```

`cmd/server/apikey_test.go` anlegen:

```go
package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
)

// mitschneiden leitet das Standard-Log in einen Puffer um.
func mitschneiden(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestPruefeAPIKeyOhneKey(t *testing.T) {
	buf := mitschneiden(t)
	pruefeAPIKey(context.Background(), jellyfin.New("http://127.0.0.1:1", time.Second, "test"), "")
	if !strings.Contains(buf.String(), "Semiadmin-Funktion aus (JELLYFIN_API_KEY nicht gesetzt)") {
		t.Errorf("Log = %q", buf.String())
	}
}

// Ein abgelehnter Key bricht den Start nicht ab, steht aber als Warnung im
// Log — der Key selbst nie. Das deckt auch einen Key mit eingeschleppten
// Anführungszeichen ab, den Jellyfin ablehnt.
func TestPruefeAPIKeyWarntOhneKeyImLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Unauthorized")
	}))
	defer srv.Close()
	buf := mitschneiden(t)

	pruefeAPIKey(context.Background(), jellyfin.New(srv.URL, time.Second, "test"), `"geheimer-key"`)
	if !strings.Contains(buf.String(), "JELLYFIN_API_KEY konnte nicht geprüft werden: ") {
		t.Errorf("Log ohne Warnung: %q", buf.String())
	}
	if strings.Contains(buf.String(), "geheimer-key") {
		t.Errorf("der Key steht im Log: %q", buf.String())
	}
}

func TestPruefeAPIKeyErfolgreichOhneWarnung(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	}))
	defer srv.Close()
	buf := mitschneiden(t)

	pruefeAPIKey(context.Background(), jellyfin.New(srv.URL, time.Second, "test"), "key-123")
	if strings.Contains(buf.String(), "konnte nicht geprüft werden") {
		t.Errorf("Warnung trotz gültigem Key: %q", buf.String())
	}
}
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/server/ ./cmd/server/
```

Erwartet: Build-Fehler in beiden Paketen, u. a. `too many arguments in call to server.New`, `not enough arguments in call to s.sessions.Create` (aus `server.go`) und `undefined: pruefeAPIKey`; Endzeilen `FAIL	jellyfinenabler/internal/server [build failed]` und `FAIL	jellyfinenabler/cmd/server [build failed]`.

- [ ] **Step 3: Minimale Implementierung**

`internal/server/server.go`, Imports. Alt:

```go
	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/web"
)
```

Neu:

```go
	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/internal/settings"
	"jellyfinenabler/web"
)
```

`Server`, `New` und Routen ersetzen. Alt:

```go
type Server struct {
	jf           Jellyfin
	sessions     *session.Store
	cookieSecure string
	tmpl         *template.Template
}

func New(jf Jellyfin, sessions *session.Store, cookieSecure string) http.Handler {
	s := &Server{
		jf:           jf,
		sessions:     sessions,
		cookieSecure: cookieSecure,
		tmpl:         template.Must(template.ParseFS(web.FS, "templates/*.html")),
	}
```

Neu:

```go
type Server struct {
	jf           Jellyfin
	sessions     *session.Store
	cookieSecure string
	tmpl         *template.Template
	// semiadmins hält die Zuordnung Semiadmin → schaltbare User.
	semiadmins *settings.Store
	// apiKey ist der Jellyfin-API-Key; leer heißt Semiadmin-Funktion aus.
	// Er wird ausschließlich im Semiadmin-Pfad benutzt und nie ausgegeben.
	apiKey string
}

// New baut den Handler. semiadmins und apiKey stehen hinten, damit die
// bisherigen Argumente ihre Reihenfolge behalten.
func New(jf Jellyfin, sessions *session.Store, cookieSecure string, semiadmins *settings.Store, apiKey string) http.Handler {
	s := &Server{
		jf:           jf,
		sessions:     sessions,
		cookieSecure: cookieSecure,
		tmpl:         template.Must(template.ParseFS(web.FS, "templates/*.html")),
		semiadmins:   semiadmins,
		apiKey:       apiKey,
	}
```

Routen. Alt:

```go
	mux.HandleFunc("GET /{$}", s.seite(s.handleIndex))
	mux.HandleFunc("GET /users/{id}", s.seite(s.handleUserPage))
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(s.handleUpdateAccess))
```

Neu:

```go
	mux.HandleFunc("GET /{$}", s.seite(s.handleStart))
	mux.HandleFunc("GET /users/{id}", s.seite(nurAdmin(s.handleUserPage, zurStartseite)))
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(nurAdmin(s.handleUpdateAccess, keinAdmin)))
```

Direkt nach dem Ende der Funktion `api` (vor `func writeJSON`) einfügen:

```go
// sitzungsFn ist ein Handler, dessen Sitzung bereits geprüft ist.
type sitzungsFn = func(http.ResponseWriter, *http.Request, session.Session)

// nurAdmin lässt nur Admin-Sitzungen durch. Bisher erzwang Jellyfin die
// Adminrechte über das Token; ein Semiadmin hat aber ein gültiges Token,
// also muss der Dienst das selbst tun (Spec §Anmeldung, Sitzung und Rollen).
// abgelehnt antwortet passend zur Route.
func nurAdmin(fn sitzungsFn, abgelehnt http.HandlerFunc) sitzungsFn {
	return func(w http.ResponseWriter, r *http.Request, sess session.Session) {
		if !sess.Admin {
			abgelehnt(w, r)
			return
		}
		fn(w, r, sess)
	}
}

// zurStartseite ist die Ablehnung auf Seiten: dort wartet die eigene Ansicht.
func zurStartseite(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// keinAdmin ist die Ablehnung in der API.
func keinAdmin(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusForbidden, "Diese Aktion ist Administratoren vorbehalten.")
}

// semiadminEingetragen ist die Rollenprüfung beim Login: Key gesetzt und
// Eintrag in der Settings-Datei. Ob das Konto in Jellyfin noch aktiv ist,
// prüft die Semiadmin-Prüfung bei jeder weiteren Anfrage.
func (s *Server) semiadminEingetragen(userID string) bool {
	if s.apiKey == "" {
		return false
	}
	_, ok := s.semiadmins.Targets(userID)
	return ok
}
```

`handleLogin`, Rollenentscheidung. Alt:

```go
	if !sess.Admin {
		s.abweisen(w, r, sess.Token)
		return
	}
	id, err := s.sessions.Create(sess.Token, sess.UserID, sess.Name)
```

Neu:

```go
	// Drei Fälle (Spec §Anmeldung, Sitzung und Rollen): Jellyfin-Admin,
	// eingetragener Semiadmin bei gesetztem Key, sonst kein Zugang.
	if !sess.Admin && !s.semiadminEingetragen(sess.UserID) {
		s.abweisen(w, r, sess.Token)
		return
	}
	id, err := s.sessions.Create(sess.Token, sess.UserID, sess.Name, sess.Admin)
```

Direkt vor `type libraryRow struct` (also nach dem Ende von `handleIndex`) einfügen:

```go
// handleStart verteilt nach Rolle: Admins sehen die Benutzerliste,
// Semiadmins ihre eigene, schmale Ansicht (Spec §HTTP-Oberfläche).
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request, sess session.Session) {
	if sess.Admin {
		s.handleIndex(w, r, sess)
		return
	}
	s.handleControlled(w, r, sess)
}

// controlledRow ist eine Karte der Semiadmin-Ansicht: Name und Schalter,
// keine Bibliotheken.
type controlledRow struct {
	ID       string
	Name     string
	Disabled bool
}

type controlledData struct {
	Title string
	Users []controlledRow
}

// handleControlled zeigt die Ansicht eines Semiadmins.
func (s *Server) handleControlled(w http.ResponseWriter, r *http.Request, sess session.Session) {
	daten := controlledData{Title: "Jellyfin Zugriff"}
	if err := s.tmpl.ExecuteTemplate(w, "controlled", daten); err != nil {
		log.Printf("Semiadmin-Ansicht nicht darstellbar: %v", err)
	}
}
```

`web/templates/login.html`, Zeile 8. Alt:

```html
          <label class="field-label" for="username">Administrator</label>
```

Neu:

```html
          <label class="field-label" for="username">Benutzername</label>
```

`web/templates/controlled.html` anlegen (Gerüst; Task 6 ersetzt die Datei vollständig):

```html
{{define "controlled"}}{{template "head" .}}
    <header class="mb-6 flex items-center justify-between gap-3">
      <h1 class="text-[17px] font-semibold tracking-tight">Jellyfin <span class="text-accent">Zugriff</span></h1>
      <form method="post" action="/logout">
        <button class="btn btn-ghost btn-sm" type="submit">Abmelden</button>
      </form>
    </header>
    <h2 class="section-title mx-0.5 mb-2.5">Benutzer</h2>
    <div class="card text-sm text-muted">Dir sind keine Benutzer zugewiesen.</div>
{{template "foot" .}}{{end}}
```

`cmd/server/main.go`, Imports. Alt:

```go
	"jellyfinenabler/internal/config"
	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
)
```

Neu:

```go
	"jellyfinenabler/internal/config"
	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/internal/settings"
)
```

Verdrahtung. Alt:

```go
	sessions := session.NewStore(cfg.SessionTTL)
	sessions.StartCleanup(ctx, 10*time.Minute)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(jellyfin.New(cfg.JellyfinURL, cfg.HTTPTimeout, version), sessions, cfg.CookieSecure),
```

Neu:

```go
	sessions := session.NewStore(cfg.SessionTTL)
	sessions.StartCleanup(ctx, 10*time.Minute)

	// Auch ohne API-Key laden: ein vorhandener Stand bleibt erhalten, und ein
	// Fehler in der Datei fällt sofort beim Start auf
	// (Spec §Settings-Datei und Paket internal/settings).
	semiadmins, err := settings.Load(cfg.SettingsFile)
	if err != nil {
		log.Fatalf("Settings-Datei: %v", err)
	}
	jf := jellyfin.New(cfg.JellyfinURL, cfg.HTTPTimeout, version)
	// Nebenläufig, damit ein beim Start noch nicht erreichbares Jellyfin den
	// Listener nicht um HTTP_TIMEOUT verzögert.
	go pruefeAPIKey(ctx, jf, cfg.APIKey)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(jf, sessions, cfg.CookieSecure, semiadmins, cfg.APIKey),
```

Am Ende von `cmd/server/main.go` anfügen:

```go

// pruefeAPIKey meldet beim Start, ob der Semiadmin-Pfad benutzbar ist. Ein
// Fehlschlag bricht nicht ab: Jellyfin ist beim Start oft noch nicht
// erreichbar (Spec §Konfiguration und Deployment). Der Key selbst wird nie
// geloggt; die Fehlermeldung des Clients enthält nur Status und Antworttext.
func pruefeAPIKey(ctx context.Context, jf *jellyfin.Client, key string) {
	if key == "" {
		log.Print("Semiadmin-Funktion aus (JELLYFIN_API_KEY nicht gesetzt)")
		return
	}
	if _, err := jf.Users(ctx, key); err != nil {
		log.Printf("JELLYFIN_API_KEY konnte nicht geprüft werden: %v", err)
	}
}
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/server/server.go internal/server/server_test.go internal/server/rollen_test.go cmd/server/main.go cmd/server/apikey_test.go
go test -race ./internal/server/ ./cmd/server/ ./internal/session/
make check
```

Erwartet: `ok` für alle drei Pakete; `make check` ohne Fehler.

- [ ] **Step 5: Commit**

```bash
git add internal/session/store.go internal/session/store_test.go internal/server/server.go internal/server/server_test.go internal/server/rollen_test.go web/templates/login.html web/templates/controlled.html cmd/server/main.go cmd/server/apikey_test.go
git commit -m "feat(server): Rollen Admin und Semiadmin in Sitzung und Anmeldung" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: Delegation — Zuordnung auf der Admin-Seite

**Files:**
- Modify: `internal/server/server.go` — Imports (`sort`), Routen in `New`, `userRow`, Schleife in `handleIndex`, `userData`, Ende von `handleUserPage`, neue Funktionen am Dateiende
- Modify: `web/templates/users.html:15`, `web/templates/user.html:34-38` (Abschnitt „Semiadmin“ einfügen)
- Modify: `web/static/app.js` (Datei vollständig ersetzen)
- Test: `internal/server/delegation_test.go` (neu)

**Interfaces:**
- Consumes:
  - `(*settings.Store).Targets`, `.Semiadmins() map[string][]string`, `.Set(semiadminID string, targets []string) error`, `.Remove(semiadminID string) error` (Task 2)
  - `server.Server.semiadmins *settings.Store`, `server.Server.apiKey string`, `nurAdmin`, `keinAdmin`, `writeError`, `writeJSON`, `apiFehler`, `seitenFehler` (Task 4 bzw. Bestand)
  - Test-Helfer `bauen`, `aufbau`, `aufbauSemi`, `angemeldet`, `semiadminAngemeldet`, `familie`, `testKey`, `fakeJF` (Task 3/4), `holen`, `eingabeFeld`, `chipFeld`, `kartenAusschnitt`, `beispielBibliotheken` (Bestand in `server_test.go`)
- Produces:
  - Route `PUT /api/users/{id}/delegation` → `func (s *Server) handleDelegation(w http.ResponseWriter, r *http.Request, sess session.Session)`, umschlossen von `s.api(nurAdmin(…, keinAdmin))`
  - `type delegationRequest struct{ Semiadmin bool; Users []string }` und `type delegationResponse struct{ Semiadmin bool; Users []string }`, beide mit den JSON-Tags `semiadmin` und `users` (Antwort-`users` nie `null`, sortiert)
  - `userRow.Semiadmin bool`; `userData.SemiadminAn bool`, `userData.Semiadmin bool`, `userData.Kandidaten []kandidatRow`; `type kandidatRow struct{ ID, Name string; Checked bool }`
  - `func (s *Server) kandidatenFuellen(r *http.Request, sess session.Session, u jellyfin.User, daten *userData) error`
  - HTML: `id="semiadmin"` (Abschnitt), `id="semiadmin-box"` (Checkbox), `id="targets"` (Chips, `name="target"`, `value="<User-ID>"`); Pille `<span class="pill pill-run">Semiadmin</span>` in der Liste
  - JS: globale Funktion `senden(pfad, rumpf)` → `Promise<{ok, status, daten}>` in `web/static/app.js`
  - Test-Helfer in `internal/server/delegation_test.go`: `zuordnen(t *testing.T, h http.Handler, c *http.Cookie, id, rumpf string) *httptest.ResponseRecorder`; `familieOhne(id string) []jellyfin.User`; `familieGeaendert(id string, aendern func(*jellyfin.User)) []jellyfin.User`
  - Fehlertexte (JSON `{"error": …}`): 403 „Dieses Konto ist kein Administrator mehr.“, 409 „JELLYFIN_API_KEY ist nicht gesetzt.“, 404 „Der Benutzer existiert nicht mehr.“, 400 „Administratoren können keine Semiadmins sein.“, 400 „Unbekannter Benutzer: <id>“, 400 „Administratoren können nicht geschaltet werden: <Name>“, 400 „Ein Semiadmin kann sich nicht selbst schalten.“, 400 „Die Anfrage war nicht lesbar.“, 500 „Einstellungen konnten nicht gespeichert werden.“

- [ ] **Step 1: Fehlschlagenden Test schreiben**

`internal/server/delegation_test.go` anlegen:

```go
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
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/server/ -run 'Delegation|Detailseite|Liste'
```

Erwartet: `FAIL`, u. a. `TestDelegationSpeichertOhneDuplikate: … Code = 404` (Route fehlt), `TestDetailseiteZeigtSemiadminAbschnitt: … Detailseite ohne "Darf andere Benutzer aktivieren und deaktivieren"` und `TestListeZeigtSemiadminPille: … anna-Karte ohne Pille Semiadmin`.

- [ ] **Step 3: Minimale Implementierung**

`internal/server/server.go`, Imports. Alt:

```go
	"log"
	"net/http"

	"jellyfinenabler/internal/jellyfin"
```

Neu:

```go
	"log"
	"net/http"
	"sort"

	"jellyfinenabler/internal/jellyfin"
```

Route in `New`. Alt:

```go
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(nurAdmin(s.handleUpdateAccess, keinAdmin)))
```

Neu:

```go
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(nurAdmin(s.handleUpdateAccess, keinAdmin)))
	mux.HandleFunc("PUT /api/users/{id}/delegation", s.api(nurAdmin(s.handleDelegation, keinAdmin)))
```

`userRow`. Alt:

```go
type userRow struct {
	ID       string
	Name     string
	Admin    bool
	Disabled bool
	Access   string
}
```

Neu:

```go
type userRow struct {
	ID       string
	Name     string
	Admin    bool
	Disabled bool
	Access   string
	// Semiadmin zeigt die Pille "Semiadmin".
	Semiadmin bool
}
```

Schleife in `handleIndex`. Alt:

```go
	daten := usersData{Title: "Jellyfin Zugriff"}
	for _, u := range users {
		daten.Users = append(daten.Users, userRow{
			ID:       u.ID,
			Name:     u.Name,
			Admin:    u.Admin,
			Disabled: u.Access.Disabled,
			Access:   zugriffText(u, libs),
		})
	}
```

Neu:

```go
	daten := usersData{Title: "Jellyfin Zugriff"}
	// Die Pille erscheint nur, wenn die Rolle auch wirkt: ohne API-Key
	// bleibt ein Eintrag in der Settings-Datei wirkungslos.
	var eingetragen map[string][]string
	if s.apiKey != "" {
		eingetragen = s.semiadmins.Semiadmins()
	}
	for _, u := range users {
		_, semiadmin := eingetragen[u.ID]
		daten.Users = append(daten.Users, userRow{
			ID:        u.ID,
			Name:      u.Name,
			Admin:     u.Admin,
			Disabled:  u.Access.Disabled,
			Access:    zugriffText(u, libs),
			Semiadmin: semiadmin && !u.Admin,
		})
	}
```

`userData`. Alt:

```go
type userData struct {
	Title     string
	User      jellyfin.User
	Libraries []libraryRow
}
```

Neu:

```go
type userData struct {
	Title     string
	User      jellyfin.User
	Libraries []libraryRow
	// SemiadminAn: der Abschnitt "Semiadmin" ist bedienbar, weil der
	// API-Key gesetzt ist. Bei Administratoren fehlt der Abschnitt ganz.
	SemiadminAn bool
	// Semiadmin: der Benutzer steht in der Settings-Datei.
	Semiadmin  bool
	Kandidaten []kandidatRow
}

// kandidatRow ist ein Chip unter "Darf schalten:".
type kandidatRow struct {
	ID      string
	Name    string
	Checked bool
}
```

Ende von `handleUserPage`. Alt:

```go
	daten := userData{Title: u.Name + " · Jellyfin Zugriff", User: u}
	for _, l := range libs {
		daten.Libraries = append(daten.Libraries, libraryRow{ID: l.ID, Name: l.Name, Checked: freigegeben[l.ID]})
	}
	if err := s.tmpl.ExecuteTemplate(w, "user", daten); err != nil {
```

Neu:

```go
	daten := userData{Title: u.Name + " · Jellyfin Zugriff", User: u}
	for _, l := range libs {
		daten.Libraries = append(daten.Libraries, libraryRow{ID: l.ID, Name: l.Name, Checked: freigegeben[l.ID]})
	}
	if !u.Admin && s.apiKey != "" {
		daten.SemiadminAn = true
		if err := s.kandidatenFuellen(r, sess, u, &daten); err != nil {
			s.seitenFehler(w, r, err)
			return
		}
	}
	if err := s.tmpl.ExecuteTemplate(w, "user", daten); err != nil {
```

Am Ende von `internal/server/server.go` anfügen:

```go

// kandidatenFuellen setzt den Semiadmin-Abschnitt der Detailseite: ob u
// eingetragen ist und welche Nicht-Admins als Ziel in Frage kommen.
// Verwaiste IDs aus der Settings-Datei erscheinen nicht und fallen beim
// nächsten Speichern dieser Zuordnung weg (Spec §Fehlerbehandlung).
func (s *Server) kandidatenFuellen(r *http.Request, sess session.Session, u jellyfin.User, daten *userData) error {
	users, err := s.jf.Users(r.Context(), sess.Token)
	if err != nil {
		return err
	}
	targets, eingetragen := s.semiadmins.Targets(u.ID)
	daten.Semiadmin = eingetragen
	gewaehlt := make(map[string]bool, len(targets))
	for _, id := range targets {
		gewaehlt[id] = true
	}
	for _, k := range users {
		if k.Admin || k.ID == u.ID {
			continue
		}
		daten.Kandidaten = append(daten.Kandidaten, kandidatRow{ID: k.ID, Name: k.Name, Checked: gewaehlt[k.ID]})
	}
	return nil
}

type delegationRequest struct {
	Semiadmin bool     `json:"semiadmin"`
	Users     []string `json:"users"`
}

type delegationResponse struct {
	Semiadmin bool     `json:"semiadmin"`
	Users     []string `json:"users"`
}

// handleDelegation speichert, ob {id} Semiadmin ist und wen er schalten
// darf (Spec §PUT /api/users/{id}/delegation). Die Ablehnungen folgen der
// Reihenfolge der Spec.
func (s *Server) handleDelegation(w http.ResponseWriter, r *http.Request, sess session.Session) {
	var req delegationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Die Anfrage war nicht lesbar.")
		return
	}
	users, err := s.jf.Users(r.Context(), sess.Token)
	if err != nil {
		s.apiFehler(w, r, err)
		return
	}
	nachID := make(map[string]jellyfin.User, len(users))
	for _, u := range users {
		nachID[u.ID] = u
	}

	// Die Settings-Datei ist eine Ressource des Dienstes, die Jellyfin nicht
	// schützt. Ein in Jellyfin inzwischen entmachteter Admin mit alter
	// Sitzung dürfte sonst weiter Zuordnungen ändern.
	if ich, ok := nachID[sess.UserID]; !ok || !ich.Admin {
		if _, id, ok := s.aktuelleSitzung(r); ok {
			s.sessions.Delete(id)
		}
		writeError(w, http.StatusForbidden, "Dieses Konto ist kein Administrator mehr.")
		return
	}
	if s.apiKey == "" {
		writeError(w, http.StatusConflict, "JELLYFIN_API_KEY ist nicht gesetzt.")
		return
	}
	id := r.PathValue("id")
	ziel, ok := nachID[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Der Benutzer existiert nicht mehr.")
		return
	}
	if ziel.Admin {
		writeError(w, http.StatusBadRequest, "Administratoren können keine Semiadmins sein.")
		return
	}

	if !req.Semiadmin {
		// users wird ignoriert.
		if err := s.semiadmins.Remove(id); err != nil {
			log.Printf("Settings-Datei nicht schreibbar: %v", err)
			writeError(w, http.StatusInternalServerError, "Einstellungen konnten nicht gespeichert werden.")
			return
		}
		writeJSON(w, http.StatusOK, delegationResponse{Semiadmin: false, Users: []string{}})
		return
	}

	// Leere Liste (auch "users": null) ist erlaubt: der Semiadmin kann sich
	// anmelden und sieht eine leere Liste.
	targets := []string{}
	gesehen := make(map[string]bool, len(req.Users))
	for _, t := range req.Users {
		u, ok := nachID[t]
		switch {
		case !ok:
			writeError(w, http.StatusBadRequest, "Unbekannter Benutzer: "+t)
			return
		case u.Admin:
			writeError(w, http.StatusBadRequest, "Administratoren können nicht geschaltet werden: "+u.Name)
			return
		case t == id:
			writeError(w, http.StatusBadRequest, "Ein Semiadmin kann sich nicht selbst schalten.")
			return
		}
		if gesehen[t] {
			continue
		}
		gesehen[t] = true
		targets = append(targets, t)
	}
	sort.Strings(targets)
	if err := s.semiadmins.Set(id, targets); err != nil {
		log.Printf("Settings-Datei nicht schreibbar: %v", err)
		writeError(w, http.StatusInternalServerError, "Einstellungen konnten nicht gespeichert werden.")
		return
	}
	writeJSON(w, http.StatusOK, delegationResponse{Semiadmin: true, Users: targets})
}
```

`web/templates/users.html`, Zeile 15. Alt:

```html
            {{if .Admin}}<span class="pill pill-wait">Administrator</span>{{end}}
```

Neu:

```html
            {{if .Admin}}<span class="pill pill-wait">Administrator</span>{{end}}
            {{if .Semiadmin}}<span class="pill pill-run">Semiadmin</span>{{end}}
```

`web/templates/user.html`, Abschnitt nach den Bibliotheken einfügen. Alt:

```html
          {{end}}
        </div>
      </div>

      <div class="flex items-center gap-3">
```

Neu:

```html
          {{end}}
        </div>
      </div>

      {{if not .User.Admin}}
      <div class="h-px bg-edge"></div>

      <div class="grid gap-4" id="semiadmin">
        <h2 class="section-title">Semiadmin</h2>
        {{if .SemiadminAn}}
        <label class="flex cursor-pointer items-center gap-2 text-sm text-text">
          <input type="checkbox" id="semiadmin-box"{{if .Semiadmin}} checked{{end}}>
          Darf andere Benutzer aktivieren und deaktivieren
        </label>
        <div>
          <div class="field-label">Darf schalten:</div>
          <div class="flex flex-wrap gap-2" id="targets">
            {{range .Kandidaten}}
            <label>
              <input class="sr-only" type="checkbox" name="target" value="{{.ID}}"{{if .Checked}} checked{{end}}{{if not $.Semiadmin}} disabled{{end}}>
              <span class="chip">{{.Name}}</span>
            </label>
            {{end}}
          </div>
        </div>
        {{else}}
        <div class="text-xs text-muted">Semiadmin-Funktion ist aus: JELLYFIN_API_KEY ist nicht gesetzt.</div>
        {{end}}
      </div>
      {{end}}

      <div class="flex items-center gap-3">
```

(Alle Klassen stehen bereits in `web/static/app.css`; `make css` ist hier nicht nötig.)

`web/static/app.js` vollständig ersetzen durch:

```js
// Kurze Rückmeldung am unteren Bildschirmrand. art ist "ok", "warn" oder "err".
function toast(text, art = "ok") {
  const region = document.getElementById("toast-region");
  if (!region) return;
  const el = document.createElement("div");
  el.className = `toast toast-${art}`;
  el.innerHTML = '<span class="toast-dot"></span>';
  el.appendChild(document.createTextNode(text));
  region.appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// Schickt einen Teil der Seite per PUT als JSON. Liefert Status und Rumpf;
// wirft nur, wenn der Dienst gar nicht erreichbar ist.
async function senden(pfad, rumpf) {
  const res = await fetch(pfad, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(rumpf),
  });
  let daten = {};
  try {
    daten = await res.json();
  } catch (err) {
    // Kein JSON (z. B. eine Fehlerseite des Reverse Proxys): dann greift
    // beim Aufrufer die Rückfallmeldung.
  }
  return { ok: res.ok, status: res.status, daten };
}

// Sammelt die Änderungen auf der Benutzerseite und schickt sie mit einem
// Klick — statt bei jedem Häkchen einen Request abzusetzen. Zugriff und
// Semiadmin-Zuordnung sind getrennte Endpunkte; gesendet wird nur der Teil,
// der sich geändert hat.
(function () {
  const form = document.getElementById("user-form");
  if (!form) return;

  const saveBtn = document.getElementById("save-btn");
  const hint = document.getElementById("save-hint");
  const allBox = document.getElementById("all-folders");
  const enabledOn = document.getElementById("enabled-on");
  const enabledOff = document.getElementById("enabled-off");
  // Fehlt bei Administratoren und ohne API-Key.
  const semiBox = document.getElementById("semiadmin-box");
  const folderBoxes = () =>
    Array.from(document.querySelectorAll("#libraries input[type=checkbox]"));
  const targetBoxes = () =>
    Array.from(document.querySelectorAll("#targets input[type=checkbox]"));
  const ausgewaehlt = (boxen) =>
    boxen
      .filter((b) => b.checked)
      .map((b) => b.value)
      .sort();

  function lesenZugriff() {
    return {
      disabled: enabledOn ? !enabledOn.checked : false,
      allFolders: allBox.checked,
      folders: ausgewaehlt(folderBoxes()),
    };
  }

  function lesenZuordnung() {
    if (!semiBox) return null;
    return { semiadmin: semiBox.checked, users: ausgewaehlt(targetBoxes()) };
  }

  // Die Stände, die der Server zuletzt bestätigt hat.
  let zugriff = lesenZugriff();
  let zuordnung = lesenZuordnung();

  function mengenAbstand(a, b) {
    const mengeA = new Set(a);
    const mengeB = new Set(b);
    let n = 0;
    for (const id of mengeA) if (!mengeB.has(id)) n++;
    for (const id of mengeB) if (!mengeA.has(id)) n++;
    return n;
  }

  function unterschiedeZugriff(a, b) {
    let n = 0;
    if (a.disabled !== b.disabled) n++;
    if (a.allFolders !== b.allFolders) n++;
    return n + mengenAbstand(a.folders, b.folders);
  }

  function unterschiedeZuordnung(a, b) {
    if (!a || !b) return 0;
    let n = a.semiadmin !== b.semiadmin ? 1 : 0;
    // Ohne Haken ignoriert der Server die Auswahl; dann zählt sie nicht.
    if (b.semiadmin) n += mengenAbstand(a.users, b.users);
    return n;
  }

  // Setzt die Felder auf die bestätigten Stände.
  function anwenden() {
    if (enabledOn) enabledOn.checked = !zugriff.disabled;
    if (enabledOff) enabledOff.checked = zugriff.disabled;
    allBox.checked = zugriff.allFolders;
    const freigegeben = new Set(zugriff.folders);
    folderBoxes().forEach((b) => {
      b.checked = freigegeben.has(b.value);
    });
    if (semiBox && zuordnung) {
      semiBox.checked = zuordnung.semiadmin;
      const ziele = new Set(zuordnung.users);
      targetBoxes().forEach((b) => {
        b.checked = ziele.has(b.value);
      });
    }
  }

  function aktualisieren() {
    // Die Einzelauswahl bleibt sichtbar, ist aber wirkungslos, solange
    // "Alle Bibliotheken" gesetzt ist.
    folderBoxes().forEach((b) => {
      b.disabled = allBox.checked;
    });
    // Die Chips sind nur wählbar, solange der Semiadmin-Haken gesetzt ist.
    if (semiBox) {
      targetBoxes().forEach((b) => {
        b.disabled = !semiBox.checked;
      });
    }
    const anzahl =
      unterschiedeZugriff(zugriff, lesenZugriff()) +
      unterschiedeZuordnung(zuordnung, lesenZuordnung());
    saveBtn.disabled = anzahl === 0;
    saveBtn.textContent = anzahl === 0 ? "Speichern" : `Speichern (${anzahl})`;
    hint.textContent =
      anzahl === 0
        ? "Keine Änderungen"
        : anzahl === 1
          ? "1 Änderung"
          : `${anzahl} Änderungen`;
  }

  // Zurück auf den bestätigten Stand, statt eine Änderung vorzutäuschen. Ein
  // bereits gespeicherter Teil bleibt gespeichert.
  function fehlschlag(text) {
    anwenden();
    aktualisieren();
    toast(text, "err");
  }

  form.addEventListener("change", aktualisieren);

  saveBtn.addEventListener("click", async () => {
    saveBtn.disabled = true;
    const id = form.dataset.userId;
    try {
      if (unterschiedeZugriff(zugriff, lesenZugriff()) > 0) {
        const r = await senden(`/api/users/${id}/access`, lesenZugriff());
        if (r.status === 401) {
          window.location.href = "/login";
          return;
        }
        if (!r.ok) {
          fehlschlag(r.daten.error || "Speichern fehlgeschlagen.");
          return;
        }
        zugriff = {
          disabled: r.daten.disabled,
          allFolders: r.daten.allFolders,
          folders: (r.daten.folders || []).slice().sort(),
        };
      }
      if (unterschiedeZuordnung(zuordnung, lesenZuordnung()) > 0) {
        const r = await senden(`/api/users/${id}/delegation`, lesenZuordnung());
        if (r.status === 401) {
          window.location.href = "/login";
          return;
        }
        if (!r.ok) {
          fehlschlag(r.daten.error || "Speichern fehlgeschlagen.");
          return;
        }
        zuordnung = {
          semiadmin: r.daten.semiadmin,
          users: (r.daten.users || []).slice().sort(),
        };
      }
      anwenden();
      aktualisieren();
      toast("Gespeichert.", "ok");
    } catch (err) {
      fehlschlag("Der Dienst ist nicht erreichbar.");
    }
  });

  aktualisieren();
})();
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/server/server.go internal/server/delegation_test.go
go test -race ./internal/server/
command -v node >/dev/null && node --check web/static/app.js
make check
```

Erwartet: `ok  	jellyfinenabler/internal/server`; `node --check` (falls Node vorhanden) ohne Ausgabe; `make check` ohne Fehler.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/delegation_test.go web/templates/users.html web/templates/user.html web/static/app.js
git commit -m "feat(server): Semiadmin-Zuordnung auf der Detailseite pflegen" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: Semiadmin-Ansicht und delegiertes Schalten

**Files:**
- Modify: `internal/server/server.go` — Route in `New`, Funktion `handleControlled` (aus Task 4) ersetzen, neue Typen und Funktionen dort einfügen
- Modify: `web/templates/controlled.html` (Datei vollständig ersetzen)
- Modify: `web/static/app.js` (Block am Dateiende anfügen)
- Test: `internal/server/server_test.go` (Fake-Methode `Users`, Feld `usersTokens`); `internal/server/semiadmin_test.go` (neu)

**Interfaces:**
- Consumes:
  - `(*settings.Store).Targets(semiadminID string) (targets []string, ok bool)` (Task 2)
  - `Jellyfin.Users(ctx, token string) ([]jellyfin.User, error)`, `Jellyfin.SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error)`, `Jellyfin.Logout(ctx, token string) error` (Task 3)
  - `server.Server.apiKey`, `server.Server.semiadmins`, `handleStart`, `controlledRow`, `controlledData`, Template `controlled` (Task 4)
  - JS-Funktionen `toast(text, art)` und `senden(pfad, rumpf)` (Task 5)
  - Test-Helfer `aufbauSemi`, `bauen`, `angemeldet`, `semiadminAngemeldet`, `familie`, `testKey` (Task 4), `familieOhne`, `familieGeaendert` (Task 5), `holen`, `eingabeFeld`, `beispielBibliotheken` (Bestand)
- Produces:
  - Route `PUT /api/controlled/{id}/active` → `func (s *Server) handleControlledActive(w http.ResponseWriter, r *http.Request, sess session.Session)`, umschlossen von `s.api(…)`
  - `var errEntzogen error`; `type semiadminLage struct{ ich jellyfin.User; users []jellyfin.User; nachID map[string]jellyfin.User; targets map[string]bool }`
  - `func (s *Server) semiadminPruefen(r *http.Request, sess session.Session) (semiadminLage, error)` — liefert `errEntzogen` (Sitzung gelöscht, Token per Logout entwertet) oder den Fehler des Key-Aufrufs (Sitzung bleibt)
  - `func (s *Server) entziehen(r *http.Request, sess session.Session) error`
  - `func keyFehler(err error) (int, string)` — 401/403 → 502 „Interner Zugang nicht verfügbar. Bitte den Administrator informieren.“, sonst Status und Meldung des `*jellyfin.Error`, sonst 502 „Jellyfin ist gerade nicht erreichbar.“
  - `type controlledRequest struct{ Disabled bool }`, `type controlledResponse struct{ Disabled bool }`, JSON-Tag jeweils `disabled`
  - Log-Zeile `semiadmin schaltet: von=<Name> (<UserID>) ziel=<Name> (<ID>) disabled=<bool>`
  - HTML: `id="controlled"` (Liste), je Karte `data-user-id="<ID>"`, Radios `id="on-<ID>"`/`id="off-<ID>"` mit `name="enabled-<ID>"`
  - Fehlertexte: 403 „Diese Aktion ist Semiadmins vorbehalten.“, 403 „Dieser Benutzer ist dir nicht zugewiesen.“, 404 „Der Benutzer existiert nicht mehr.“, 403 „Administratoren können nicht geschaltet werden.“, 401 „Die Anmeldung ist abgelaufen.“, 400 „Die Anfrage war nicht lesbar.“
  - Test-Fake: Feld `usersTokens []string`; Test-Helfer `schalten(t *testing.T, h http.Handler, c *http.Cookie, id, rumpf string) *httptest.ResponseRecorder`, `logMitschnitt(t *testing.T) *bytes.Buffer`

- [ ] **Step 1: Fehlschlagenden Test schreiben**

`internal/server/server_test.go`, Fake erweitern. Struct-Ende alt:

```go
	setDisabled *bool
	setErr      error
}
```

Neu:

```go
	setDisabled *bool
	setErr      error
	// usersTokens hält fest, mit welchem Token Users gerufen wurde.
	usersTokens []string
}
```

Methode `Users` des Fakes. Alt:

```go
func (f *fakeJF) Users(context.Context, string) ([]jellyfin.User, error) {
	return f.users, f.usersErr
}
```

Neu:

```go
func (f *fakeJF) Users(_ context.Context, token string) ([]jellyfin.User, error) {
	f.usersTokens = append(f.usersTokens, token)
	return f.users, f.usersErr
}
```

`internal/server/semiadmin_test.go` anlegen:

```go
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
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
go test ./internal/server/ -run 'Semiadmin|Schalten|KeyFehler|Rollenmatrix'
```

Erwartet: `FAIL`, u. a. `TestSemiadminAnsichtZeigtNurZugewiesene: … Karte für u-2 fehlt`, `TestSchaltenSchaltetUndLoggt: … Code = 404` (Route fehlt) und `TestSemiadminVerliertZugangMittenInDerSitzung/…/Ansicht: … erwartet 303 nach /login`.

- [ ] **Step 3: Minimale Implementierung**

`internal/server/server.go`, Route in `New`. Alt:

```go
	mux.HandleFunc("PUT /api/users/{id}/delegation", s.api(nurAdmin(s.handleDelegation, keinAdmin)))
```

Neu:

```go
	mux.HandleFunc("PUT /api/users/{id}/delegation", s.api(nurAdmin(s.handleDelegation, keinAdmin)))
	mux.HandleFunc("PUT /api/controlled/{id}/active", s.api(s.handleControlledActive))
```

Die Gerüst-Fassung von `handleControlled` aus Task 4 ersetzen. Alt:

```go
// handleControlled zeigt die Ansicht eines Semiadmins.
func (s *Server) handleControlled(w http.ResponseWriter, r *http.Request, sess session.Session) {
	daten := controlledData{Title: "Jellyfin Zugriff"}
	if err := s.tmpl.ExecuteTemplate(w, "controlled", daten); err != nil {
		log.Printf("Semiadmin-Ansicht nicht darstellbar: %v", err)
	}
}
```

Neu:

```go
// errEntzogen meldet eine nicht bestandene Semiadmin-Prüfung. Die Sitzung
// ist dann bereits gelöscht.
var errEntzogen = errors.New("Semiadmin-Rolle entzogen")

// semiadminLage ist das Ergebnis einer bestandenen Semiadmin-Prüfung. Die
// Userliste ist per API-Key geladen; beide Semiadmin-Handler brauchen sie.
type semiadminLage struct {
	ich     jellyfin.User
	users   []jellyfin.User
	nachID  map[string]jellyfin.User
	targets map[string]bool
}

// semiadminPruefen ist die Semiadmin-Prüfung (Spec §Anmeldung, Sitzung und
// Rollen). Sie läuft bei jeder Semiadmin-Anfrage, damit ein inzwischen
// gesperrter oder entmachteter Semiadmin mit seiner alten Sitzung nicht
// weiter schalten kann. Nicht bestanden (errEntzogen) heißt: Key fehlt,
// Eintrag fehlt, oder die per Key erfolgreich geladene Liste kennt das
// Konto nicht oder nur deaktiviert. Scheitert der Key-Aufruf selbst, kommt
// dessen Fehler zurück und die Sitzung bleibt bestehen.
func (s *Server) semiadminPruefen(r *http.Request, sess session.Session) (semiadminLage, error) {
	if s.apiKey == "" {
		return semiadminLage{}, s.entziehen(r, sess)
	}
	targets, ok := s.semiadmins.Targets(sess.UserID)
	if !ok {
		return semiadminLage{}, s.entziehen(r, sess)
	}
	users, err := s.jf.Users(r.Context(), s.apiKey)
	if err != nil {
		return semiadminLage{}, err
	}
	lage := semiadminLage{
		users:   users,
		nachID:  make(map[string]jellyfin.User, len(users)),
		targets: make(map[string]bool, len(targets)),
	}
	for _, u := range users {
		lage.nachID[u.ID] = u
	}
	for _, id := range targets {
		lage.targets[id] = true
	}
	ich, ok := lage.nachID[sess.UserID]
	if !ok || ich.Access.Disabled {
		return semiadminLage{}, s.entziehen(r, sess)
	}
	lage.ich = ich
	return lage, nil
}

// entziehen löscht die Sitzung und entwertet das User-Token in Jellyfin.
// Scheitert die Abmeldung, wird das nur geloggt.
func (s *Server) entziehen(r *http.Request, sess session.Session) error {
	if _, id, ok := s.aktuelleSitzung(r); ok {
		s.sessions.Delete(id)
	}
	if err := s.jf.Logout(r.Context(), sess.Token); err != nil {
		log.Printf("Token eines entzogenen Semiadmins konnte nicht entwertet werden: %v", err)
	}
	log.Printf("Semiadmin-Sitzung beendet: %s (%s)", sess.Name, sess.UserID)
	return errEntzogen
}

// keyFehler übersetzt einen gescheiterten Aufruf mit dem API-Key in Status
// und Meldung. Anders als bei Aufrufen mit dem User-Token wird die Sitzung
// nie gelöscht: 401 oder 403 heißt hier, dass der Key ungültig oder
// widerrufen ist, nicht dass die Anmeldung des Semiadmins abgelaufen ist
// (Spec §Fehlerbehandlung).
func keyFehler(err error) (int, string) {
	var jfErr *jellyfin.Error
	if errors.As(err, &jfErr) {
		if jfErr.Status == http.StatusUnauthorized || jfErr.Status == http.StatusForbidden {
			log.Printf("Jellyfin lehnt JELLYFIN_API_KEY ab: %v", jfErr)
			return http.StatusBadGateway, "Interner Zugang nicht verfügbar. Bitte den Administrator informieren."
		}
		log.Printf("Jellyfin hat abgelehnt: %v", jfErr)
		return jfErr.Status, jfErr.Message
	}
	log.Printf("Jellyfin nicht erreichbar: %v", err)
	return http.StatusBadGateway, "Jellyfin ist gerade nicht erreichbar."
}

// handleControlled zeigt die Ansicht eines Semiadmins: je zugewiesenem
// Nicht-Admin eine Karte mit Aktiv/Aus-Schalter, keine Bibliotheken, keine
// Links auf Detailseiten. Verwaiste IDs, inzwischen zu Administratoren
// gewordene Ziele und das eigene Konto (aus einer alten oder von Hand
// bearbeiteten Datei) werden ausgefiltert.
func (s *Server) handleControlled(w http.ResponseWriter, r *http.Request, sess session.Session) {
	lage, err := s.semiadminPruefen(r, sess)
	if errors.Is(err, errEntzogen) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err != nil {
		code, meldung := keyFehler(err)
		http.Error(w, meldung, code)
		return
	}
	daten := controlledData{Title: "Jellyfin Zugriff"}
	for _, u := range lage.users {
		if !lage.targets[u.ID] || u.Admin || u.ID == sess.UserID {
			continue
		}
		daten.Users = append(daten.Users, controlledRow{ID: u.ID, Name: u.Name, Disabled: u.Access.Disabled})
	}
	if err := s.tmpl.ExecuteTemplate(w, "controlled", daten); err != nil {
		log.Printf("Semiadmin-Ansicht nicht darstellbar: %v", err)
	}
}

type controlledRequest struct {
	Disabled bool `json:"disabled"`
}

type controlledResponse struct {
	Disabled bool `json:"disabled"`
}

// handleControlledActive schaltet einen zugewiesenen User
// (Spec §PUT /api/controlled/{id}/active). Das ist der einzige schreibende
// Aufruf mit dem API-Key; jede Schaltung landet im Log, weil Jellyfins
// Aktivitätslog bei Key-Aufrufen keinen Benutzer nennt.
func (s *Server) handleControlledActive(w http.ResponseWriter, r *http.Request, sess session.Session) {
	if sess.Admin {
		writeError(w, http.StatusForbidden, "Diese Aktion ist Semiadmins vorbehalten.")
		return
	}
	lage, err := s.semiadminPruefen(r, sess)
	if errors.Is(err, errEntzogen) {
		writeError(w, http.StatusUnauthorized, "Die Anmeldung ist abgelaufen.")
		return
	}
	if err != nil {
		code, meldung := keyFehler(err)
		writeError(w, code, meldung)
		return
	}
	var req controlledRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Die Anfrage war nicht lesbar.")
		return
	}
	id := r.PathValue("id")
	// Das eigene Konto ist nie ein Ziel, auch wenn es in der Datei steht.
	if !lage.targets[id] || id == sess.UserID {
		writeError(w, http.StatusForbidden, "Dieser Benutzer ist dir nicht zugewiesen.")
		return
	}
	ziel, ok := lage.nachID[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Der Benutzer existiert nicht mehr.")
		return
	}
	if ziel.Admin {
		writeError(w, http.StatusForbidden, "Administratoren können nicht geschaltet werden.")
		return
	}
	neu, err := s.jf.SetDisabled(r.Context(), s.apiKey, id, req.Disabled)
	if err != nil {
		code, meldung := keyFehler(err)
		writeError(w, code, meldung)
		return
	}
	log.Printf("semiadmin schaltet: von=%s (%s) ziel=%s (%s) disabled=%t", lage.ich.Name, sess.UserID, ziel.Name, ziel.ID, neu)
	writeJSON(w, http.StatusOK, controlledResponse{Disabled: neu})
}
```

`web/templates/controlled.html` vollständig ersetzen durch:

```html
{{define "controlled"}}{{template "head" .}}
    <header class="mb-6 flex items-center justify-between gap-3">
      <h1 class="text-[17px] font-semibold tracking-tight">Jellyfin <span class="text-accent">Zugriff</span></h1>
      <form method="post" action="/logout">
        <button class="btn btn-ghost btn-sm" type="submit">Abmelden</button>
      </form>
    </header>
    <h2 class="section-title mx-0.5 mb-2.5">Benutzer</h2>
    {{if .Users}}
    <div class="grid gap-2" id="controlled">
      {{range .Users}}
      <div class="card flex items-center justify-between gap-3" data-user-id="{{.ID}}">
        <span class="min-w-0 truncate text-[14.5px] font-semibold text-text">{{.Name}}</span>
        <div class="seg" role="radiogroup" aria-label="Konto von {{.Name}}">
          <input class="sr-only" type="radio" name="enabled-{{.ID}}" id="on-{{.ID}}" value="on"{{if not .Disabled}} checked{{end}}>
          <label class="seg-label" for="on-{{.ID}}">Aktiv</label>
          <input class="sr-only" type="radio" name="enabled-{{.ID}}" id="off-{{.ID}}" value="off"{{if .Disabled}} checked{{end}}>
          <label class="seg-label" for="off-{{.ID}}">Aus</label>
        </div>
      </div>
      {{end}}
    </div>
    {{else}}
    <div class="card text-sm text-muted">Dir sind keine Benutzer zugewiesen.</div>
    {{end}}
{{template "foot" .}}{{end}}
```

(Alle Klassen stehen bereits in `web/static/app.css`.)

Am Ende von `web/static/app.js` anfügen:

```js

// Semiadmin-Ansicht: jeder Schalter speichert sofort. Bei einem Fehler
// springt er auf den letzten Stand zurück, ein Toast zeigt die Meldung.
(function () {
  const liste = document.getElementById("controlled");
  if (!liste) return;

  liste.addEventListener("change", async (ev) => {
    const karte = ev.target.closest("[data-user-id]");
    if (!karte) return;
    const id = karte.dataset.userId;
    const an = document.getElementById(`on-${id}`);
    const aus = document.getElementById(`off-${id}`);
    const disabled = aus.checked;
    const zurueck = () => {
      an.checked = disabled;
      aus.checked = !disabled;
    };
    an.disabled = true;
    aus.disabled = true;
    try {
      const r = await senden(
        `/api/controlled/${encodeURIComponent(id)}/active`,
        { disabled },
      );
      if (r.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!r.ok) {
        zurueck();
        toast(r.daten.error || "Schalten fehlgeschlagen.", "err");
        return;
      }
      an.checked = !r.daten.disabled;
      aus.checked = r.daten.disabled;
      toast(r.daten.disabled ? "Deaktiviert." : "Aktiviert.", "ok");
    } catch (err) {
      zurueck();
      toast("Der Dienst ist nicht erreichbar.", "err");
    } finally {
      an.disabled = false;
      aus.disabled = false;
    }
  });
})();
```

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
gofmt -w internal/server/server.go internal/server/server_test.go internal/server/semiadmin_test.go
go test -race ./internal/server/
command -v node >/dev/null && node --check web/static/app.js
make check
```

Erwartet: `ok  	jellyfinenabler/internal/server`; `node --check` (falls Node vorhanden) ohne Ausgabe; `make check` ohne Fehler.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go internal/server/semiadmin_test.go web/templates/controlled.html web/static/app.js
git commit -m "feat(server): Semiadmin-Ansicht und delegiertes Schalten" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: Deployment und Doku

**Files:**
- Modify: `Dockerfile:17-29` (Build- und Laufzeitstufe)
- Modify: `docker-compose.yml` (Datei vollständig ersetzen)
- Modify: `.gitignore` (zwei Einträge am Dateiende)
- Modify: `Makefile:30-35` (Target `run`)
- Modify: `README.md:3-5`, `:26-32`, `:34-41`, `:46-49`, `:57-61` (Einleitung, ENV-Tabelle, Entwicklung, Grenzen, Sicherheitshinweis) und neuer Abschnitt „Semiadmins“ nach der Konfiguration
- Modify (nur falls `make css` eine Änderung erzeugt): `web/static/app.css`
- Test: Shell-Prüfung in Step 1 (kein Go-Test; der Task ändert keinen Go-Code)

**Interfaces:**
- Consumes: ENV `JELLYFIN_API_KEY`, `SETTINGS_FILE` (Task 1); Vorgabe `/data/settings.json` (Task 1); Log-Texte „Semiadmin-Funktion aus (JELLYFIN_API_KEY nicht gesetzt)“ und „JELLYFIN_API_KEY konnte nicht geprüft werden: …“ (Task 4); Fehlertext „Einstellungen konnten nicht gespeichert werden.“ (Task 5); Log-Zeile `semiadmin schaltet: …` (Task 6)
- Produces: Image mit `/data` im Besitz von 65532:65532; Compose mit `JELLYFIN_API_KEY: "${JELLYFIN_API_KEY:-}"` und Volume `jellyfin-enabler-data:/data`; Make-Variable `SETTINGS_FILE ?= ./data/settings.json`; README-Abschnitt „Semiadmins“

- [ ] **Step 1: Fehlschlagenden Test schreiben**

Die Prüfung als Skript im Scratch-Bereich anlegen (wird nicht committet), z. B. `/tmp/semiadmin-deploy-check.sh`:

```bash
#!/bin/sh
# Prüft die Deployment-Änderungen der Semiadmin-Funktion.
set -u
f=0
pruefe() { grep -qF -- "$2" "$1" || { echo "FEHLT in $1: $2"; f=1; }; }
pruefe Dockerfile 'RUN mkdir -p /data'
pruefe Dockerfile 'COPY --from=build --chown=65532:65532 /data /data'
pruefe docker-compose.yml 'JELLYFIN_API_KEY: "${JELLYFIN_API_KEY:-}"'
pruefe docker-compose.yml '- jellyfin-enabler-data:/data'
grep -q '^volumes:' docker-compose.yml || { echo "FEHLT in docker-compose.yml: Top-Level volumes:"; f=1; }
grep -qx '\.env' .gitignore || { echo "FEHLT in .gitignore: .env"; f=1; }
grep -qx 'data/' .gitignore || { echo "FEHLT in .gitignore: data/"; f=1; }
pruefe Makefile 'SETTINGS_FILE ?= ./data/settings.json'
pruefe Makefile 'SETTINGS_FILE=$(SETTINGS_FILE)'
pruefe README.md '| `JELLYFIN_API_KEY` |'
pruefe README.md '| `SETTINGS_FILE` |'
pruefe README.md '## Semiadmins'
pruefe README.md '65532'
pruefe README.md 'vollwertiger Admin-Zugang'
# Der Key darf nirgends als Klartext im Makefile-Rezept stehen.
grep -q 'JELLYFIN_API_KEY=\$(JELLYFIN_API_KEY)' Makefile && { echo "Makefile gibt den Key auf der Kommandozeile aus"; f=1; }
[ $f -eq 0 ] && echo "Deployment-Prüfung: alles da"
exit $f
```

- [ ] **Step 2: Test ausführen, Fehlschlag prüfen**

```bash
sh /tmp/semiadmin-deploy-check.sh
```

Erwartet: mehrere Zeilen `FEHLT in …`, Exit-Code 1.

- [ ] **Step 3: Minimale Implementierung**

`Dockerfile`, Build-Stufe. Alt:

```dockerfile
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /app ./cmd/server
```

Neu:

```dockerfile
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /app ./cmd/server
# Leeres Datenverzeichnis für die Settings-Datei (SETTINGS_FILE, Vorgabe
# /data/settings.json). distroless hat keine Shell; deshalb wird es hier
# angelegt und unten mit Besitzer nonroot (65532) übernommen — sonst könnte
# der Dienst dort nicht schreiben.
RUN mkdir -p /data
```

Laufzeitstufe. Alt:

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /usr/local/bin/app
```

Neu:

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /usr/local/bin/app
COPY --from=build --chown=65532:65532 /data /data
```

`docker-compose.yml` vollständig ersetzen durch:

```yaml
services:
  jellyfin-enabler:
    build: .
    # NAS-Deployment ohne lokalen Build: "build: ." entfernen und stattdessen
    # das veröffentlichte Image ziehen (amd64):
    #   image: ghcr.io/shdev/jellyfin-enabler:latest
    image: jellyfin-enabler-native
    container_name: jellyfin-enabler
    ports:
      # Links der Host-Port, rechts der Port im Container. Wird PORT unten
      # gesetzt, muss die rechte Zahl mitwandern.
      - "8081:8080"
    environment:
      JELLYFIN_URL: "http://jellyfin:8096"
      # Jellyfin-API-Schlüssel für Semiadmins. Der Wert kommt aus einer Datei
      # .env neben dieser Datei (JELLYFIN_API_KEY=…), nie direkt hierher
      # schreiben: der Schlüssel ist ein vollwertiger Admin-Zugang. Leer
      # schaltet die Semiadmin-Funktion aus.
      JELLYFIN_API_KEY: "${JELLYFIN_API_KEY:-}"
      # Hinter einem TLS-terminierenden Reverse Proxy nötig, sonst kommt das
      # Sitzungscookie ohne Secure-Flag:
      # COOKIE_SECURE: "true"
      # SESSION_TTL: "12h"
    volumes:
      # Settings-Datei mit den Semiadmin-Zuordnungen (/data/settings.json).
      # Ein Bind-Mount statt des Volumes muss dem Benutzer 65532 gehören.
      - jellyfin-enabler-data:/data
    restart: unless-stopped
    # Läuft Jellyfin in einem eigenen Compose-Projekt, dessen Netz hier
    # eintragen, damit der Hostname "jellyfin" auflösbar ist:
    # networks:
    #   - jellyfin_default
volumes:
  jellyfin-enabler-data:
# networks:
#   jellyfin_default:
#     external: true
```

`.gitignore`, am Dateiende anfügen:

```gitignore

# Lokale Geheimnisse (JELLYFIN_API_KEY) und Settings-Datei von `make run`
.env
data/
```

`Makefile`, Target `run`. Alt:

```make
JELLYFIN_URL ?= http://localhost:8096

run: build
	PORT=8081 JELLYFIN_URL=$(JELLYFIN_URL) $(BINARY)
```

Neu:

```make
JELLYFIN_URL ?= http://localhost:8096
# Settings-Datei für lokale Läufe; das Verzeichnis ist per .gitignore
# ausgeschlossen. JELLYFIN_API_KEY wird bewusst nicht hier gesetzt: make
# druckt jedes Rezept aus, der Key käme sonst ins Terminal. Er wird aus der
# Umgebung geerbt (export JELLYFIN_API_KEY=… vor make run).
SETTINGS_FILE ?= ./data/settings.json

run: build
	PORT=8081 JELLYFIN_URL=$(JELLYFIN_URL) SETTINGS_FILE=$(SETTINGS_FILE) $(BINARY)
```

`README.md`, Einleitung (Zeilen 3-5). Alt:

```markdown
Kleine Weboberfläche, um Jellyfin-Benutzer zu aktivieren bzw. zu deaktivieren
und ihnen Bibliotheken zuzuordnen. Angemeldet wird sich mit einem
Jellyfin-Administrator; der Dienst speichert selbst keine Zugangsdaten.
```

Neu:

```markdown
Kleine Weboberfläche, um Jellyfin-Benutzer zu aktivieren bzw. zu deaktivieren
und ihnen Bibliotheken zuzuordnen. Angemeldet wird sich mit einem
Jellyfin-Administrator oder als Semiadmin (siehe unten); der Dienst speichert
selbst keine Passwörter.
```

ENV-Tabelle (Zeilen 26-32). Alt:

```markdown
| `COOKIE_SECURE` | `auto` | hinter TLS-Proxy auf `true` setzen |
| `HTTP_TIMEOUT` | `15s` | Zeitlimit für Jellyfin-Aufrufe |
```

Neu:

```markdown
| `COOKIE_SECURE` | `auto` | hinter TLS-Proxy auf `true` setzen |
| `HTTP_TIMEOUT` | `15s` | Zeitlimit für Jellyfin-Aufrufe |
| `JELLYFIN_API_KEY` | leer | Jellyfin-API-Schlüssel für Semiadmins; leer schaltet die Funktion aus. Gehört in `.env`, nicht in `docker-compose.yml` |
| `SETTINGS_FILE` | `/data/settings.json` | Pfad der Settings-Datei mit den Semiadmin-Zuordnungen |

## Semiadmins

Ein Semiadmin ist ein gewöhnliches Jellyfin-Konto, das bestimmte andere
Benutzer aktivieren und deaktivieren darf — typischerweise ein Elternteil für
die Konten der eigenen Kinder. Er meldet sich mit seinem normalen
Jellyfin-Konto an und sieht nur die ihm zugewiesenen Benutzer mit einem
Aktiv/Aus-Schalter; Bibliotheken kann er nicht ändern.

Einrichten:

1. Im Jellyfin-Dashboard unter *API-Schlüssel* einen Schlüssel anlegen.
2. Ihn in eine Datei `.env` neben `docker-compose.yml` schreiben:
   `JELLYFIN_API_KEY=<schlüssel>` — ohne Anführungszeichen und Leerzeichen.
3. Neu starten (`docker compose up -d`). Steht im Log
   „JELLYFIN_API_KEY konnte nicht geprüft werden: …“, ist der Schlüssel
   falsch oder Jellyfin war beim Start nicht erreichbar.
4. Als Administrator die Detailseite des künftigen Semiadmins öffnen, im
   Abschnitt „Semiadmin“ den Haken setzen, die Benutzer wählen, speichern.

Die Zuordnung liegt in `SETTINGS_FILE` (im Container `/data/settings.json`,
benanntes Volume `jellyfin-enabler-data`) und übersteht Neustarts. Wer statt
des benannten Volumes einen Bind-Mount nutzt, muss das Host-Verzeichnis dem
Benutzer 65532 geben (`sudo chown 65532:65532 ./data`), unter dem der Dienst
im Container läuft — sonst scheitert jedes Speichern mit „Einstellungen
konnten nicht gespeichert werden.“

Ohne `JELLYFIN_API_KEY` ist die Funktion aus; eine vorhandene Zuordnung bleibt
in der Datei und wirkt wieder, sobald der Schlüssel gesetzt ist. Jede
Schaltung eines Semiadmins steht im Log des Dienstes
(`semiadmin schaltet: von=… ziel=… disabled=…`), weil Jellyfins
Aktivitätslog bei Aufrufen mit API-Schlüssel keinen Benutzer nennt.
```

Entwicklung (Zeilen 36-41). Alt:

```markdown
make run        # lokal starten, JELLYFIN_URL=… überschreibbar
```

Neu:

```markdown
make run        # lokal starten, JELLYFIN_URL=… und SETTINGS_FILE=… überschreibbar
```

Grenzen (Zeilen 48-49). Alt:

```markdown
Benutzer anlegen oder löschen, Passwörter ändern, Sitzungen beenden. Dafür
gibt es das Jellyfin-Dashboard.
```

Neu:

```markdown
Benutzer anlegen oder löschen, Passwörter ändern, Sitzungen beenden. Dafür
gibt es das Jellyfin-Dashboard. Semiadmins können keine Bibliotheken ändern;
Gruppen und Zeitpläne gibt es nicht, und verwaiste Einträge gelöschter
Benutzer werden nicht automatisch aufgeräumt.
```

Sicherheitshinweis (Zeilen 59-61). Alt:

```markdown
Wer die Oberfläche erreicht und ein Admin-Passwort hat, kann Zugänge
abschalten. Der Dienst gehört ins Heimnetz, hinter Reverse Proxy und TLS —
nicht ungeschützt ins Internet.
```

Neu:

```markdown
Wer die Oberfläche erreicht und ein Admin-Passwort hat, kann Zugänge
abschalten. Der Dienst gehört ins Heimnetz, hinter Reverse Proxy und TLS —
nicht ungeschützt ins Internet.

`JELLYFIN_API_KEY` ist ein vollwertiger Admin-Zugang zu Jellyfin. Er gehört
nur in `.env` (per `.gitignore` ausgeschlossen), nicht in
`docker-compose.yml` und in kein Repository. Der Dienst loggt ihn nie und
benutzt ihn ausschließlich für die Semiadmin-Ansicht und das Schalten. Ist er
abgeflossen: im Jellyfin-Dashboard widerrufen und einen neuen anlegen.
```

Tailwind-Stand prüfen. Tasks 5 und 6 benutzen nur Klassen, die bereits in `web/static/app.css` stehen; `make css` bestätigt das (benötigt Docker, `web/static/app.css` ist versioniert — `git ls-files web/static/app.css` liefert die Datei):

```bash
command -v docker >/dev/null && make css
git status --short web/static/app.css
```

Erwartet: keine Ausgabe von `git status` (Datei unverändert). Erscheint ` M web/static/app.css`, wird die Datei in Step 5 mitcommittet.

- [ ] **Step 4: Test ausführen, Erfolg prüfen**

```bash
sh /tmp/semiadmin-deploy-check.sh
docker compose config --quiet && echo "compose ok"
make check
command -v docker >/dev/null && make image-native
```

Erwartet: `Deployment-Prüfung: alles da`; `compose ok` (ohne `.env` ergibt `${JELLYFIN_API_KEY:-}` einen leeren Wert, keine Warnung); `make check` ohne Fehler; `make image-native` endet mit einem erfolgreich getaggten Image `jellyfin-enabler-native` (nur falls Docker verfügbar).

- [ ] **Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml .gitignore Makefile README.md
# nur falls Step 3 eine Änderung an app.css gezeigt hat:
git status --short web/static/app.css | grep -q '^ M' && git add web/static/app.css
git commit -m "build: Settings-Volume, API-Key und Doku für Semiadmins" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

## Spec-Abdeckung

| Spec-Abschnitt | Task |
|---|---|
| Kontext & Ziel, Erfolgskriterien | 4 (Login), 5 (Zuordnung), 6 (Ansicht/Schalten), 2 (Neustartfest) |
| Entscheidungen (eigene schmale Pfade, Key nur im Semiadmin-Pfad) | 4, 6 |
| Nicht-Ziele | 7 (README); kein Task baut Gruppen, Zeitpläne, CSRF-Umbau oder Aufräumen |
| Konfiguration und Deployment (ENV, Dockerfile, Compose, .gitignore, Makefile, README, Startprüfung) | 1, 4 (Startprüfung), 7 |
| Settings-Datei und Paket `internal/settings` | 2; 500-Antwort in 5; Laden ohne Key in 4 (`main.go`) |
| Anmeldung, Sitzung und Rollen (Login-Entscheidung, Beschriftung, `nurAdmin`) | 3 (Mindestanpassung), 4 |
| Anmeldung, Sitzung und Rollen (Semiadmin-Prüfung) | 6 |
| Anmeldung, Sitzung und Rollen (Admin-Prüfung bei `/delegation`) | 5 |
| Jellyfin-Client (`Session.Admin`, `SetDisabled`, gemeinsamer Helfer, Interface) | 3 |
| HTTP-Oberfläche, `PUT /api/users/{id}/delegation` | 5 |
| HTTP-Oberfläche, `PUT /api/controlled/{id}/active`, Log-Zeile | 6 |
| Frontend `user.html`, `users.html`, `app.js` (Speichern) | 5 |
| Frontend `controlled.html`, `app.js` (Schalter) | 4 (Gerüst), 6 |
| Fehlerbehandlung (Key ungültig, verwaiste IDs, Ziel wird Admin, Datei nicht schreibbar) | 6, 5 (Chips ohne verwaiste IDs), 2/5 (Schreibfehler) |
| Tests | je Task |
| Verifikation vor der Umsetzung | Abschnitt „Vorbedingung“ |
| Risiken & Hinweise | 7 (README), 4/6 (Key nie im Log, Tests) |

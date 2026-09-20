# Jellyfin Enabler — Implementierungsplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein kleiner Go-Webservice, mit dem sich Jellyfin-Benutzer aktivieren bzw. deaktivieren und ihnen Bibliotheken zuordnen lassen.

**Architecture:** Servergerendertes HTML plus eine kleine `app.js`, dahinter ein Jellyfin-API-Client ohne externe Abhängigkeiten. Die `UserPolicy` wird als `map[string]json.RawMessage` durchgereicht, weil `POST /Users/{id}/Policy` die Policy vollständig ersetzt; nur drei Schlüssel werden angefasst. Anmeldung erfolgt mit einem Jellyfin-Admin-Konto, die Sitzung liegt im Arbeitsspeicher.

**Tech Stack:** Go 1.23 (stdlib `net/http`, `html/template`, `embed`), Tailwind CSS v4 (im Docker-Container kompiliert), Docker (distroless), Make.

**Spec:** `docs/superpowers/specs/2026-09-20-jellyfin-enabler-design.md`

## Global Constraints

Diese Vorgaben gelten für **jede** Aufgabe:

- Modulname: `jellyfinenabler`. Go-Version in `go.mod`: `go 1.23`.
- **Keine externen Go-Abhängigkeiten.** `go.mod` enthält keinen einzigen `require`-Eintrag. Wer eine Bibliothek braucht, hat die Aufgabe falsch verstanden.
- Alle Kommentare, UI-Texte, Fehlermeldungen und Commit-Botschaften auf **Deutsch**, mit korrekten Umlauten (`für`, nicht `fuer`).
- Kommentare erklären das **Warum**, nicht das Was, und verweisen wo sinnvoll auf den Spec-Abschnitt, z. B. `(Spec §Policy-Passthrough)`.
- `gofmt` und `go vet` müssen sauber durchlaufen: `make check` ist das Tor.
- Testdateien liegen als `package <name>_test` neben dem Code, außer wo der Plan ausdrücklich einen internen Test verlangt.
- Container-Port ist **8080**, Host-Port **8081**.
- `TAILWIND_VERSION` ist **4.3.3** und muss in `Makefile` und `Dockerfile` identisch sein.
- Jede Commit-Botschaft endet mit der `Co-Authored-By:`-Attributionszeile, die
  das eigene Harness vorgibt — sie nennt das Modell, das den Commit
  tatsächlich geschrieben hat. Die Beispiel-Commits unten zeigen die Zeile des
  Controllers; ein Implementer setzt stattdessen seine eigene ein.
- Das Nachbarprojekt `/Users/sh/projects/yt-dl-web-client` ist die Stilvorlage für Aufbau, Kommentardichte und Teststil. Bei Zweifeln dort nachsehen.

## Dateistruktur

| Datei | Verantwortung |
|---|---|
| `go.mod` | Modul, keine Abhängigkeiten |
| `internal/config/config.go` | Laufzeitkonfiguration aus Umgebungsvariablen |
| `internal/jellyfin/types.go` | `User`, `Library`, `Access`, `Session` |
| `internal/jellyfin/errors.go` | Fehlerabbildung Jellyfin → deutsche Meldungen |
| `internal/jellyfin/client.go` | HTTP-Client: Anmeldung, Lesen, Schreiben |
| `internal/session/store.go` | Cookie-Wert → Sitzung, im Speicher, mit TTL |
| `internal/server/server.go` | Routing, Middleware, Handler, Templates |
| `web/embed.go` | `//go:embed templates static` |
| `web/templates/*.html` | `layout`, `login`, `users`, `user` |
| `web/src/input.css` | Tailwind-Quelle mit Design-Tokens |
| `web/static/app.js` | Dirty-Tracking, Speichern, Toasts |
| `cmd/server/main.go` | Start, `-healthcheck`, sauberes Herunterfahren |
| `cmd/genicons/main.go` | PWA-Icons erzeugen |
| `Makefile`, `Dockerfile`, `docker-compose.yml` | Build und Betrieb |

Abhängigkeitsrichtung: `server` → `jellyfin`, `session`, `config`, `web`. Niemals umgekehrt. `session` kennt `jellyfin` **nicht**.

---

### Task 1: Projektgerüst und Konfiguration

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nichts
- Produces: `config.Config{Port int, JellyfinURL string, SessionTTL time.Duration, CookieSecure string, HTTPTimeout time.Duration}`, `config.FromEnv(getenv func(string) string) (Config, error)`, `config.ErrNoJellyfinURL`

- [ ] **Step 1: Modul anlegen**

```bash
cd /Users/sh/projects/jellyfin-enabler
go mod init jellyfinenabler
```

Danach `go.mod` prüfen: die Zeile muss `go 1.23` lauten. Falls `go mod init` eine höhere Version einträgt, von Hand auf `1.23` ändern.

- [ ] **Step 2: Den fehlschlagenden Test schreiben**

`internal/config/config_test.go`:

```go
package config_test

import (
	"errors"
	"testing"
	"time"

	"jellyfinenabler/internal/config"
)

// env baut ein getenv aus einer Map, damit Tests ohne echte Umgebung laufen.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnvVorgaben(t *testing.T) {
	cfg, err := config.FromEnv(env(map[string]string{"JELLYFIN_URL": "http://jellyfin:8096"}))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, erwartet 8080", cfg.Port)
	}
	if cfg.SessionTTL != 12*time.Hour {
		t.Errorf("SessionTTL = %v, erwartet 12h", cfg.SessionTTL)
	}
	if cfg.CookieSecure != "auto" {
		t.Errorf("CookieSecure = %q, erwartet auto", cfg.CookieSecure)
	}
	if cfg.HTTPTimeout != 15*time.Second {
		t.Errorf("HTTPTimeout = %v, erwartet 15s", cfg.HTTPTimeout)
	}
}

func TestFromEnvOhneJellyfinURL(t *testing.T) {
	_, err := config.FromEnv(env(nil))
	if !errors.Is(err, config.ErrNoJellyfinURL) {
		t.Fatalf("err = %v, erwartet ErrNoJellyfinURL", err)
	}
}

func TestFromEnvSchneidetSchraegstrichAb(t *testing.T) {
	cfg, err := config.FromEnv(env(map[string]string{"JELLYFIN_URL": "http://jellyfin:8096/"}))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.JellyfinURL != "http://jellyfin:8096" {
		t.Errorf("JellyfinURL = %q, erwartet ohne Schrägstrich", cfg.JellyfinURL)
	}
}

func TestFromEnvUeberschreibungen(t *testing.T) {
	cases := []struct {
		name  string
		vars  map[string]string
		check func(config.Config) error
	}{
		{"Port", map[string]string{"PORT": "9000"}, func(c config.Config) error {
			if c.Port != 9000 {
				return errors.New("Port nicht übernommen")
			}
			return nil
		}},
		{"ungültiger Port bleibt bei der Vorgabe", map[string]string{"PORT": "keine Zahl"}, func(c config.Config) error {
			if c.Port != 8080 {
				return errors.New("Vorgabe überschrieben")
			}
			return nil
		}},
		{"SessionTTL", map[string]string{"SESSION_TTL": "30m"}, func(c config.Config) error {
			if c.SessionTTL != 30*time.Minute {
				return errors.New("SessionTTL nicht übernommen")
			}
			return nil
		}},
		{"HTTPTimeout", map[string]string{"HTTP_TIMEOUT": "5s"}, func(c config.Config) error {
			if c.HTTPTimeout != 5*time.Second {
				return errors.New("HTTPTimeout nicht übernommen")
			}
			return nil
		}},
		{"CookieSecure true", map[string]string{"COOKIE_SECURE": "TRUE"}, func(c config.Config) error {
			if c.CookieSecure != "true" {
				return errors.New("CookieSecure nicht übernommen")
			}
			return nil
		}},
		{"CookieSecure false", map[string]string{"COOKIE_SECURE": "0"}, func(c config.Config) error {
			if c.CookieSecure != "false" {
				return errors.New("CookieSecure nicht übernommen")
			}
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.vars["JELLYFIN_URL"] = "http://jellyfin:8096"
			cfg, err := config.FromEnv(env(tc.vars))
			if err != nil {
				t.Fatalf("unerwarteter Fehler: %v", err)
			}
			if err := tc.check(cfg); err != nil {
				t.Error(err)
			}
		})
	}
}
```

- [ ] **Step 3: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/config/...`
Expected: FAIL — das Paket `config` existiert noch nicht.

- [ ] **Step 4: Minimale Implementierung**

`internal/config/config.go`:

```go
// Package config liest die Laufzeitkonfiguration aus Umgebungsvariablen
// (Spec §Build und Deployment).
package config

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port        int
	JellyfinURL string
	SessionTTL  time.Duration
	// CookieSecure ist "auto", "true" oder "false". "auto" setzt das
	// Secure-Flag nur bei direktem TLS; hinter einem TLS-terminierenden
	// Reverse Proxy ist "true" nötig (Spec §Authentifizierung und Sitzung).
	CookieSecure string
	HTTPTimeout  time.Duration
}

// ErrNoJellyfinURL meldet die fehlende Pflichtvariable. Der Dienst startet
// ohne sie nicht, statt später bei jedem Aufruf ins Leere zu laufen.
var ErrNoJellyfinURL = errors.New("JELLYFIN_URL fehlt: Basis-URL des Jellyfin-Servers, z. B. http://jellyfin:8096")

func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:         8080,
		SessionTTL:   12 * time.Hour,
		CookieSecure: "auto",
		HTTPTimeout:  15 * time.Second,
	}
	cfg.JellyfinURL = strings.TrimRight(strings.TrimSpace(getenv("JELLYFIN_URL")), "/")
	if cfg.JellyfinURL == "" {
		return Config{}, ErrNoJellyfinURL
	}
	if n, err := strconv.Atoi(getenv("PORT")); err == nil && n > 0 {
		cfg.Port = n
	}
	if d, err := time.ParseDuration(getenv("SESSION_TTL")); err == nil && d > 0 {
		cfg.SessionTTL = d
	}
	if d, err := time.ParseDuration(getenv("HTTP_TIMEOUT")); err == nil && d > 0 {
		cfg.HTTPTimeout = d
	}
	switch strings.ToLower(getenv("COOKIE_SECURE")) {
	case "true", "1", "yes":
		cfg.CookieSecure = "true"
	case "false", "0", "no":
		cfg.CookieSecure = "false"
	}
	return cfg, nil
}
```

- [ ] **Step 5: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/config/... -v`
Expected: PASS, alle vier Testfunktionen grün.

- [ ] **Step 6: Committen**

```bash
git add go.mod internal/config/
git commit -m "$(printf '%s\n' \
  'feat: Projektgerüst und Konfiguration aus Umgebungsvariablen' \
  '' \
  'JELLYFIN_URL ist Pflicht — ohne sie startet der Dienst gar nicht,' \
  'statt später bei jedem Aufruf ins Leere zu laufen.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 2: Jellyfin-Typen und Fehlerabbildung

**Files:**
- Create: `internal/jellyfin/types.go`
- Create: `internal/jellyfin/errors.go`
- Test: `internal/jellyfin/errors_test.go`

**Interfaces:**
- Consumes: nichts
- Produces:
  - `jellyfin.Access{Disabled bool, AllFolders bool, Folders []string}`
  - `jellyfin.User{ID, Name string, Admin bool, Access Access}`
  - `jellyfin.Library{ID, Name string}`
  - `jellyfin.Session{Token, UserID, Name string}`
  - `jellyfin.Error{Status int, Message, Body string}` mit `Error() string` und `Unauthorized() bool`
  - `jellyfin.IsUnauthorized(err error) bool`
  - paketintern: `statusError(status int, body string) *Error`, `forbiddenMessage(body string) string`

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`internal/jellyfin/errors_test.go` — beachte: **internes** Testpaket, weil `statusError` nicht exportiert ist.

```go
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
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/jellyfin/...`
Expected: FAIL — Paket existiert nicht.

- [ ] **Step 3: Typen schreiben**

`internal/jellyfin/types.go`:

```go
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
```

- [ ] **Step 4: Fehlerabbildung schreiben**

`internal/jellyfin/errors.go`:

```go
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
```

- [ ] **Step 5: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/jellyfin/... -v`
Expected: PASS.

- [ ] **Step 6: Committen**

```bash
git add internal/jellyfin/
git commit -m "$(printf '%s\n' \
  'feat: Jellyfin-Typen und Abbildung der Verbotsmeldungen' \
  '' \
  'Jellyfin antwortet auf alle drei Verbotsfälle mit 403 und einem' \
  'englischen Klartext. Der Statuscode allein ist mehrdeutig, deshalb' \
  'wird der Rumpf ausgewertet; bei unbekanntem Text greift eine' \
  'Rückfallmeldung und das Original landet im Log.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 3: Jellyfin-Client — Grundlage, Anmeldung, Abmeldung

**Files:**
- Create: `internal/jellyfin/client.go`
- Test: `internal/jellyfin/client_test.go`

**Interfaces:**
- Consumes: `Session`, `statusError` aus Task 2
- Produces:
  - `jellyfin.New(baseURL string, timeout time.Duration, version string) *Client`
  - `jellyfin.DeviceID(username string) string`
  - `(*Client).Authenticate(ctx context.Context, username, password string) (Session, error)`
  - `(*Client).Logout(ctx context.Context, token string) error`
  - `jellyfin.ErrNotAdmin`
  - paketintern: `(*Client).do(ctx, method, path, token, deviceID string, body, out any) error`

- [ ] **Step 1: Testhilfe und die fehlschlagenden Tests schreiben**

`internal/jellyfin/client_test.go`:

```go
package jellyfin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"jellyfinenabler/internal/jellyfin"
)

// captured hält eine Anfrage fest, die der gefälschte Jellyfin-Server
// entgegengenommen hat — damit Tests Header und Rumpf prüfen können.
type captured struct {
	Method string
	Path   string
	Auth   string
	Body   []byte
}

type fakeJellyfin struct {
	t     *testing.T
	mux   *http.ServeMux
	srv   *httptest.Server
	mu    sync.Mutex
	calls []captured
}

func newFake(t *testing.T) *fakeJellyfin {
	t.Helper()
	f := &fakeJellyfin{t: t, mux: http.NewServeMux()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, captured{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		f.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// reply registriert eine feste Antwort für ein Routenmuster.
func (f *fakeJellyfin) reply(pattern string, status int, body string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
}

func (f *fakeJellyfin) handle(pattern string, fn http.HandlerFunc) {
	f.mux.HandleFunc(pattern, fn)
}

func (f *fakeJellyfin) call(i int) captured {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.calls) {
		f.t.Fatalf("Aufruf %d fehlt, es gab nur %d", i, len(f.calls))
	}
	return f.calls[i]
}

func (f *fakeJellyfin) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeJellyfin) client() *jellyfin.Client {
	return jellyfin.New(f.srv.URL, 5*time.Second, "test")
}

const adminLoginAntwort = `{
  "AccessToken": "tok-123",
  "User": {"Id": "u-1", "Name": "papa", "Policy": {"IsAdministrator": true}}
}`

func TestAuthenticateLiefertSitzung(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, adminLoginAntwort)

	sess, err := f.client().Authenticate(context.Background(), "papa", "geheim")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if sess.Token != "tok-123" || sess.UserID != "u-1" || sess.Name != "papa" {
		t.Errorf("Sitzung = %+v, erwartet Token tok-123 / u-1 / papa", sess)
	}
}

func TestAuthenticateSendetClientHeaderUndZugangsdaten(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, adminLoginAntwort)

	if _, err := f.client().Authenticate(context.Background(), "papa", "geheim"); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}

	auth := f.call(0).Auth
	// Jellyfin 10.11 verlangt das MediaBrowser-Format; X-Emby-Authorization
	// greift nur mit EnableLegacyAuthorization.
	if !strings.HasPrefix(auth, "MediaBrowser ") {
		t.Errorf("Authorization = %q, erwartet MediaBrowser-Präfix", auth)
	}
	for _, teil := range []string{`Client="Jellyfin Enabler"`, `Device="Jellyfin Enabler"`, `Version="test"`} {
		if !strings.Contains(auth, teil) {
			t.Errorf("Authorization = %q, erwartet %s", auth, teil)
		}
	}
	if !strings.Contains(auth, `DeviceId="`+jellyfin.DeviceID("papa")+`"`) {
		t.Errorf("Authorization = %q, erwartet stabile DeviceId für papa", auth)
	}
	if strings.Contains(auth, "Token=") {
		t.Errorf("Authorization = %q, bei der Anmeldung darf kein Token stehen", auth)
	}

	var body map[string]string
	if err := json.Unmarshal(f.call(0).Body, &body); err != nil {
		t.Fatalf("Rumpf nicht lesbar: %v", err)
	}
	if body["Username"] != "papa" || body["Pw"] != "geheim" {
		t.Errorf("Rumpf = %v, erwartet Username/Pw", body)
	}
}

func TestDeviceIDIstStabilUndUnterschiedlich(t *testing.T) {
	if jellyfin.DeviceID("papa") != jellyfin.DeviceID("papa") {
		t.Error("DeviceID muss für denselben Namen gleich bleiben")
	}
	if jellyfin.DeviceID("papa") == jellyfin.DeviceID("mama") {
		t.Error("DeviceID muss sich je Name unterscheiden")
	}
	if got := len(jellyfin.DeviceID("papa")); got != 32 {
		t.Errorf("Länge = %d, erwartet 32", got)
	}
}

func TestAuthenticateLehntNichtAdminAbUndMeldetAb(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusOK, `{
	  "AccessToken": "tok-kind",
	  "User": {"Id": "u-2", "Name": "kind1", "Policy": {"IsAdministrator": false}}
	}`)
	f.reply("/Sessions/Logout", http.StatusNoContent, "")

	_, err := f.client().Authenticate(context.Background(), "kind1", "geheim")
	if !errors.Is(err, jellyfin.ErrNotAdmin) {
		t.Fatalf("err = %v, erwartet ErrNotAdmin", err)
	}
	// Das eben erzeugte Token darf nicht zurückbleiben (Spec §Authentifizierung).
	if f.callCount() != 2 {
		t.Fatalf("%d Aufrufe, erwartet 2 (Anmeldung + Abmeldung)", f.callCount())
	}
	if p := f.call(1).Path; p != "/Sessions/Logout" {
		t.Errorf("zweiter Aufruf = %s, erwartet /Sessions/Logout", p)
	}
	if !strings.Contains(f.call(1).Auth, `Token="tok-kind"`) {
		t.Errorf("Abmeldung ohne das erzeugte Token: %q", f.call(1).Auth)
	}
}

func TestAuthenticateFalschePasswortWirdFehler(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/AuthenticateByName", http.StatusUnauthorized, `Invalid username or password`)

	_, err := f.client().Authenticate(context.Background(), "papa", "falsch")
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	if !jellyfin.IsUnauthorized(err) {
		t.Errorf("err = %v, erwartet 401", err)
	}
}

func TestLogoutSendetToken(t *testing.T) {
	f := newFake(t)
	f.reply("/Sessions/Logout", http.StatusNoContent, "")

	if err := f.client().Logout(context.Background(), "tok-123"); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if !strings.Contains(f.call(0).Auth, `Token="tok-123"`) {
		t.Errorf("Authorization = %q, erwartet Token", f.call(0).Auth)
	}
}
```

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/jellyfin/...`
Expected: FAIL — `undefined: jellyfin.New`, `undefined: jellyfin.DeviceID` usw.

- [ ] **Step 3: Client schreiben**

`internal/jellyfin/client.go`:

```go
package jellyfin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// clientName taucht in Jellyfin als App- und Gerätename der Sitzung auf.
const clientName = "Jellyfin Enabler"

// ErrNotAdmin meldet eine gültige Anmeldung ohne Administratorrechte. Der
// Dienst schreibt Policies und braucht RequiresElevation.
var ErrNotAdmin = errors.New("dieser Benutzer ist kein Administrator")

type Client struct {
	base    string
	http    *http.Client
	version string
}

func New(baseURL string, timeout time.Duration, version string) *Client {
	return &Client{
		base:    strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		version: version,
	}
}

// DeviceID ist pro Benutzername stabil. Ohne das legte jeder Neustart des
// Containers einen neuen Geräteeintrag in Jellyfin an
// (Spec §Authentifizierung und Sitzung).
func DeviceID(username string) string {
	sum := sha256.Sum256([]byte("jellyfin-enabler|" + username))
	return hex.EncodeToString(sum[:])[:32]
}

// authHeader baut den Authorization-Header im MediaBrowser-Format, das
// Jellyfin 10.11 erwartet. DeviceId wird nur bei der Anmeldung gebraucht;
// danach identifiziert das Token die Sitzung.
func (c *Client) authHeader(token, deviceID string) string {
	parts := []string{
		fmt.Sprintf("Client=%q", clientName),
		fmt.Sprintf("Device=%q", clientName),
		fmt.Sprintf("Version=%q", c.version),
	}
	if deviceID != "" {
		parts = append(parts, fmt.Sprintf("DeviceId=%q", deviceID))
	}
	if token != "" {
		parts = append(parts, fmt.Sprintf("Token=%q", token))
	}
	return "MediaBrowser " + strings.Join(parts, ", ")
}

// do führt einen Aufruf aus. body und out dürfen nil sein.
func (c *Client) do(ctx context.Context, method, path, token, deviceID string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader(token, deviceID))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// Begrenzt lesen: der Rumpf ist ein kurzer Klartext, kein Datenstrom.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return statusError(resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Authenticate(ctx context.Context, username, password string) (Session, error) {
	var res struct {
		AccessToken string `json:"AccessToken"`
		User        struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Policy struct {
				IsAdministrator bool `json:"IsAdministrator"`
			} `json:"Policy"`
		} `json:"User"`
	}
	body := map[string]string{"Username": username, "Pw": password}
	err := c.do(ctx, http.MethodPost, "/Users/AuthenticateByName", "", DeviceID(username), body, &res)
	if err != nil {
		return Session{}, err
	}
	if !res.User.Policy.IsAdministrator {
		// Kein verwaistes Token zurücklassen; Fehler dabei sind nachrangig.
		_ = c.Logout(ctx, res.AccessToken)
		return Session{}, ErrNotAdmin
	}
	return Session{Token: res.AccessToken, UserID: res.User.ID, Name: res.User.Name}, nil
}

func (c *Client) Logout(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodPost, "/Sessions/Logout", token, "", nil, nil)
}
```

- [ ] **Step 4: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/jellyfin/... -v`
Expected: PASS, alle Tests aus Task 2 und Task 3.

- [ ] **Step 5: Committen**

```bash
git add internal/jellyfin/
git commit -m "$(printf '%s\n' \
  'feat: Jellyfin-Client mit An- und Abmeldung' \
  '' \
  'Die DeviceId wird aus dem Benutzernamen abgeleitet, damit Jellyfin' \
  'nicht bei jedem Neustart einen neuen Geräteeintrag anlegt. Meldet' \
  'sich ein Nicht-Administrator an, wird das eben erzeugte Token sofort' \
  'wieder entwertet, statt als verwaiste Sitzung zurückzubleiben.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 4: Jellyfin-Client — Benutzer und Bibliotheken lesen

**Files:**
- Modify: `internal/jellyfin/client.go` (anhängen)
- Modify: `internal/jellyfin/client_test.go` (anhängen)

**Interfaces:**
- Consumes: `(*Client).do` aus Task 3, `User`/`Library`/`Access` aus Task 2
- Produces:
  - `(*Client).Users(ctx context.Context, token string) ([]User, error)`
  - `(*Client).User(ctx context.Context, token, userID string) (User, error)`
  - `(*Client).Libraries(ctx context.Context, token string) ([]Library, error)`
  - paketintern: `rawUser` mit `Policy map[string]json.RawMessage`, `(rawUser).view() (User, error)` und `(*Client).rawUser(ctx, token, userID) (rawUser, error)`

- [ ] **Step 1: Die fehlschlagenden Tests anhängen**

An `internal/jellyfin/client_test.go` anhängen:

```go
const benutzerListe = `[
  {"Id": "u-1", "Name": "papa", "Policy": {"IsAdministrator": true, "IsDisabled": false, "EnableAllFolders": true, "EnabledFolders": []}},
  {"Id": "u-2", "Name": "kind1", "Policy": {"IsAdministrator": false, "IsDisabled": true, "EnableAllFolders": false, "EnabledFolders": ["lib-a", "lib-b"]}}
]`

func TestUsersLiefertZugriffsdaten(t *testing.T) {
	f := newFake(t)
	f.reply("/Users", http.StatusOK, benutzerListe)

	users, err := f.client().Users(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("%d Benutzer, erwartet 2", len(users))
	}
	// Alphabetisch sortiert, damit die Liste in der Oberfläche stabil ist.
	if users[0].Name != "kind1" || users[1].Name != "papa" {
		t.Errorf("Reihenfolge = %s, %s — erwartet kind1, papa", users[0].Name, users[1].Name)
	}
	kind := users[0]
	if !kind.Access.Disabled {
		t.Error("kind1 muss als deaktiviert gelesen werden")
	}
	if kind.Admin {
		t.Error("kind1 ist kein Administrator")
	}
	if len(kind.Access.Folders) != 2 || kind.Access.Folders[0] != "lib-a" {
		t.Errorf("Folders = %v, erwartet [lib-a lib-b]", kind.Access.Folders)
	}
	if !users[1].Admin || !users[1].Access.AllFolders {
		t.Errorf("papa = %+v, erwartet Administrator mit allen Bibliotheken", users[1])
	}
}

func TestUserLiefertEinzelnenBenutzer(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":{"IsDisabled":false,"EnableAllFolders":false,"EnabledFolders":["lib-a"]}}`)

	u, err := f.client().User(context.Background(), "tok", "u-2")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if u.ID != "u-2" || u.Name != "kind1" || len(u.Access.Folders) != 1 {
		t.Errorf("Benutzer = %+v", u)
	}
}

func TestUserOhnePolicyIstFehler(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-3", http.StatusOK, `{"Id":"u-3","Name":"kaputt"}`)

	// Eine fehlende Policy wird nicht stillschweigend erfunden — sonst
	// schriebe der Dienst später eine leere Policy zurück.
	if _, err := f.client().User(context.Background(), "tok", "u-3"); err == nil {
		t.Fatal("Fehler erwartet, wenn die Policy fehlt")
	}
}

func TestLibrariesLiefertItemIds(t *testing.T) {
	f := newFake(t)
	f.reply("/Library/VirtualFolders", http.StatusOK, `[
	  {"Name": "Serien", "ItemId": "lib-b", "CollectionType": "tvshows"},
	  {"Name": "Filme", "ItemId": "lib-a", "CollectionType": "movies"}
	]`)

	libs, err := f.client().Libraries(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("%d Bibliotheken, erwartet 2", len(libs))
	}
	if libs[0].Name != "Filme" || libs[0].ID != "lib-a" {
		t.Errorf("erste Bibliothek = %+v, erwartet Filme/lib-a (alphabetisch)", libs[0])
	}
}

func TestLesenSendetToken(t *testing.T) {
	f := newFake(t)
	f.reply("/Users", http.StatusOK, `[]`)

	if _, err := f.client().Users(context.Background(), "tok-xyz"); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if !strings.Contains(f.call(0).Auth, `Token="tok-xyz"`) {
		t.Errorf("Authorization = %q, erwartet Token", f.call(0).Auth)
	}
}
```

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/jellyfin/...`
Expected: FAIL — `client.Users undefined` usw.

- [ ] **Step 3: Implementierung an `client.go` anhängen**

```go
// rawUser hält die Policy bewusst als Rohdaten. POST /Users/{id}/Policy
// ersetzt die Policy vollständig — würde hier ein Struct mit nur den
// benötigten Feldern stehen, gingen beim Zurückschreiben alle übrigen
// verloren (Spec §Policy-Passthrough).
type rawUser struct {
	ID     string                     `json:"Id"`
	Name   string                     `json:"Name"`
	Policy map[string]json.RawMessage `json:"Policy"`
}

// view dekodiert aus der Rohpolicy die Felder, die die Oberfläche anzeigt —
// ausschließlich lesend.
func (r rawUser) view() (User, error) {
	if r.Policy == nil {
		return User{}, fmt.Errorf("jellyfin: Benutzer %q (%s) hat keine Policy", r.Name, r.ID)
	}
	var p struct {
		IsAdministrator  bool     `json:"IsAdministrator"`
		IsDisabled       bool     `json:"IsDisabled"`
		EnableAllFolders bool     `json:"EnableAllFolders"`
		EnabledFolders   []string `json:"EnabledFolders"`
	}
	b, err := json.Marshal(r.Policy)
	if err != nil {
		return User{}, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return User{}, err
	}
	return User{
		ID:    r.ID,
		Name:  r.Name,
		Admin: p.IsAdministrator,
		Access: Access{
			Disabled:   p.IsDisabled,
			AllFolders: p.EnableAllFolders,
			Folders:    p.EnabledFolders,
		},
	}, nil
}

func (c *Client) Users(ctx context.Context, token string) ([]User, error) {
	var raw []rawUser
	if err := c.do(ctx, http.MethodGet, "/Users", token, "", nil, &raw); err != nil {
		return nil, err
	}
	users := make([]User, 0, len(raw))
	for _, r := range raw {
		u, err := r.view()
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Name) < strings.ToLower(users[j].Name)
	})
	return users, nil
}

func (c *Client) User(ctx context.Context, token, userID string) (User, error) {
	r, err := c.rawUser(ctx, token, userID)
	if err != nil {
		return User{}, err
	}
	return r.view()
}

func (c *Client) rawUser(ctx context.Context, token, userID string) (rawUser, error) {
	var r rawUser
	if err := c.do(ctx, http.MethodGet, "/Users/"+url.PathEscape(userID), token, "", nil, &r); err != nil {
		return rawUser{}, err
	}
	return r, nil
}

func (c *Client) Libraries(ctx context.Context, token string) ([]Library, error) {
	var raw []struct {
		Name   string `json:"Name"`
		ItemID string `json:"ItemId"`
	}
	if err := c.do(ctx, http.MethodGet, "/Library/VirtualFolders", token, "", nil, &raw); err != nil {
		return nil, err
	}
	libs := make([]Library, 0, len(raw))
	for _, r := range raw {
		libs = append(libs, Library{ID: r.ItemID, Name: r.Name})
	}
	sort.Slice(libs, func(i, j int) bool {
		return strings.ToLower(libs[i].Name) < strings.ToLower(libs[j].Name)
	})
	return libs, nil
}
```

Die Importe `net/url` und `sort` ergänzen.

- [ ] **Step 4: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/jellyfin/... -v`
Expected: PASS.

- [ ] **Step 5: Committen**

```bash
git add internal/jellyfin/
git commit -m "$(printf '%s\n' \
  'feat: Benutzer und Bibliotheken aus Jellyfin lesen' \
  '' \
  'Die Policy wird als Rohdaten gehalten und nur für die Anzeige in ein' \
  'typisiertes Struct dekodiert. Fehlt sie ganz, ist das ein Fehler —' \
  'eine erfundene leere Policy würde beim Zurückschreiben Rechte löschen.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 5: Jellyfin-Client — Zugriff schreiben (Policy-Passthrough)

**Das ist die Kernaufgabe des Projekts.** `POST /Users/{id}/Policy` ersetzt die
Policy vollständig. Wird hier geschludert, löscht der Dienst bei jedem
Speichern 41 Policy-Felder des Benutzers.

**Files:**
- Modify: `internal/jellyfin/client.go` (anhängen)
- Modify: `internal/jellyfin/client_test.go` (anhängen)

**Interfaces:**
- Consumes: `(*Client).rawUser`, `(*Client).do`, `Access`
- Produces: `(*Client).UpdateAccess(ctx context.Context, token, userID string, a Access) (Access, error)`

- [ ] **Step 1: Die fehlschlagenden Tests anhängen**

An `internal/jellyfin/client_test.go` anhängen. Die Policy enthält alle 44
Felder aus der OpenAPI-Spezifikation plus ein erfundenes Feld, das der Client
nicht kennt:

```go
// vollePolicy enthält alle 44 UserPolicy-Felder aus der OpenAPI-Spezifikation
// von Jellyfin plus ein erfundenes Feld, wie es ein Plugin oder eine spätere
// Version einführen könnte. Keines davon darf beim Schreiben verloren gehen.
const vollePolicy = `{
  "IsAdministrator": false,
  "IsHidden": true,
  "EnableCollectionManagement": false,
  "EnableSubtitleManagement": false,
  "EnableLyricManagement": false,
  "IsDisabled": false,
  "MaxParentalRating": 10,
  "MaxParentalSubRating": null,
  "BlockedTags": ["horror"],
  "AllowedTags": [],
  "EnableUserPreferenceAccess": true,
  "AccessSchedules": [{"Id": 1, "UserId": "u-2", "DayOfWeek": "Monday", "StartHour": 8.0, "EndHour": 20.0}],
  "BlockUnratedItems": ["Movie"],
  "EnableRemoteControlOfOtherUsers": false,
  "EnableSharedDeviceControl": true,
  "EnableRemoteAccess": true,
  "EnableLiveTvManagement": false,
  "EnableLiveTvAccess": true,
  "EnableMediaPlayback": true,
  "EnableAudioPlaybackTranscoding": true,
  "EnableVideoPlaybackTranscoding": false,
  "EnablePlaybackRemuxing": true,
  "ForceRemoteSourceTranscoding": false,
  "EnableContentDeletion": false,
  "EnableContentDeletionFromFolders": [],
  "EnableContentDownloading": true,
  "EnableSyncTranscoding": true,
  "EnableMediaConversion": false,
  "EnabledDevices": [],
  "EnableAllDevices": true,
  "EnabledChannels": [],
  "EnableAllChannels": false,
  "EnabledFolders": ["lib-a"],
  "EnableAllFolders": false,
  "InvalidLoginAttemptCount": 0,
  "LoginAttemptsBeforeLockout": -1,
  "MaxActiveSessions": 0,
  "EnablePublicSharing": false,
  "BlockedMediaFolders": [],
  "BlockedChannels": [],
  "RemoteClientBitrateLimit": 0,
  "AuthenticationProviderId": "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
  "PasswordResetProviderId": "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
  "SyncPlayAccess": "CreateAndJoinGroups",
  "EinFeldDasDerClientNichtKennt": {"a": 1, "b": [true, null]}
}`

// kompakt normalisiert JSON-Werte, damit Einrückung im Testliteral den
// Vergleich nicht stört — der Encoder sendet kompakt.
func kompakt(t *testing.T, b []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		t.Fatalf("ungültiges JSON %s: %v", b, err)
	}
	return buf.String()
}

// policySchreiber richtet den gefälschten Server so ein, dass er die Policy
// liefert und die geschriebene Policy festhält.
func policySchreiber(t *testing.T, f *fakeJellyfin, policy string) *map[string]json.RawMessage {
	t.Helper()
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"kind1","Policy":`+policy+`}`)
	geschrieben := map[string]json.RawMessage{}
	f.handle("/Users/u-2/Policy", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&geschrieben); err != nil {
			t.Errorf("geschriebene Policy nicht lesbar: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return &geschrieben
}

func TestUpdateAccessErhaeltAlleUebrigenPolicyFelder(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	_, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{
		Disabled:   true,
		AllFolders: false,
		Folders:    []string{"lib-a", "lib-c"},
	})
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}

	var original map[string]json.RawMessage
	if err := json.Unmarshal([]byte(vollePolicy), &original); err != nil {
		t.Fatalf("Testliteral kaputt: %v", err)
	}
	if len(*geschrieben) != len(original) {
		t.Fatalf("%d Felder geschrieben, erwartet %d", len(*geschrieben), len(original))
	}
	verwaltet := map[string]bool{"IsDisabled": true, "EnableAllFolders": true, "EnabledFolders": true}
	for name, wert := range original {
		if verwaltet[name] {
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

	// Die drei verwalteten Felder werden oben übersprungen. Ohne diese Prüfung
	// bliebe eine Vertauschung von IsDisabled und EnableAllFolders unbemerkt:
	// im Betrieb hieße das, "Benutzer deaktivieren" gäbe alle Bibliotheken frei.
	for name, erwartet := range map[string]string{
		"IsDisabled":       "true",
		"EnableAllFolders": "false",
		"EnabledFolders":   `["lib-a","lib-c"]`,
	} {
		if got := kompakt(t, (*geschrieben)[name]); got != erwartet {
			t.Errorf("Feld %s = %s, erwartet %s", name, got, erwartet)
		}
	}
}

func TestUpdateAccessSchreibtDieDreiVerwaltetenFelder(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	neu, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{
		Disabled:   true,
		AllFolders: true,
		Folders:    []string{"lib-a", "lib-c"},
	})
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if got := kompakt(t, (*geschrieben)["IsDisabled"]); got != "true" {
		t.Errorf("IsDisabled = %s, erwartet true", got)
	}
	if got := kompakt(t, (*geschrieben)["EnableAllFolders"]); got != "true" {
		t.Errorf("EnableAllFolders = %s, erwartet true", got)
	}
	// Die Einzelauswahl wird auch bei EnableAllFolders mitgeschrieben, damit
	// sie beim Zurückschalten wieder dasteht (Spec §EnableAllFolders-Semantik).
	if got := kompakt(t, (*geschrieben)["EnabledFolders"]); got != `["lib-a","lib-c"]` {
		t.Errorf("EnabledFolders = %s", got)
	}
	if !neu.Disabled || !neu.AllFolders || len(neu.Folders) != 2 {
		t.Errorf("Rückgabe = %+v, erwartet den geschriebenen Zustand", neu)
	}
}

func TestUpdateAccessSchreibtLeereListeStattNull(t *testing.T) {
	f := newFake(t)
	geschrieben := policySchreiber(t, f, vollePolicy)

	if _, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{}); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if got := kompakt(t, (*geschrieben)["EnabledFolders"]); got != "[]" {
		t.Errorf("EnabledFolders = %s, erwartet [] statt null", got)
	}
}

func TestUpdateAccessReichtVerbotAlsDeutscheMeldungDurch(t *testing.T) {
	f := newFake(t)
	f.reply("/Users/u-2", http.StatusOK, `{"Id":"u-2","Name":"papa","Policy":`+vollePolicy+`}`)
	f.reply("/Users/u-2/Policy", http.StatusForbidden, "Administrators cannot be disabled.")

	_, err := f.client().UpdateAccess(context.Background(), "tok", "u-2", jellyfin.Access{Disabled: true})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	var jfErr *jellyfin.Error
	if !errors.As(err, &jfErr) {
		t.Fatalf("err = %T, erwartet *jellyfin.Error", err)
	}
	if jfErr.Message != "Administratoren können nicht deaktiviert werden." {
		t.Errorf("Message = %q", jfErr.Message)
	}
}
```

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/jellyfin/... -run UpdateAccess`
Expected: FAIL — `client.UpdateAccess undefined`.

- [ ] **Step 3: Implementierung an `client.go` anhängen**

```go
// UpdateAccess liest die Policy frisch, ersetzt genau drei Schlüssel und
// schreibt sie zurück. POST /Users/{id}/Policy ersetzt die Policy vollständig
// — deshalb Read-Modify-Write über die Rohdaten, damit die übrigen 41 Felder
// unangetastet bleiben (Spec §Policy-Passthrough).
func (c *Client) UpdateAccess(ctx context.Context, token, userID string, a Access) (Access, error) {
	r, err := c.rawUser(ctx, token, userID)
	if err != nil {
		return Access{}, err
	}
	if r.Policy == nil {
		return Access{}, fmt.Errorf("jellyfin: Benutzer %s hat keine Policy", userID)
	}
	// Jellyfin erwartet eine Liste, kein null.
	folders := a.Folders
	if folders == nil {
		folders = []string{}
	}
	set := func(name string, wert any) error {
		b, err := json.Marshal(wert)
		if err != nil {
			return err
		}
		r.Policy[name] = b
		return nil
	}
	if err := set("IsDisabled", a.Disabled); err != nil {
		return Access{}, err
	}
	if err := set("EnableAllFolders", a.AllFolders); err != nil {
		return Access{}, err
	}
	if err := set("EnabledFolders", folders); err != nil {
		return Access{}, err
	}
	path := "/Users/" + url.PathEscape(userID) + "/Policy"
	if err := c.do(ctx, http.MethodPost, path, token, "", r.Policy, nil); err != nil {
		return Access{}, err
	}
	return Access{Disabled: a.Disabled, AllFolders: a.AllFolders, Folders: folders}, nil
}
```

- [ ] **Step 4: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/jellyfin/... -v`
Expected: PASS. Insbesondere `TestUpdateAccessErhaeltAlleUebrigenPolicyFelder`.

- [ ] **Step 5: Committen**

```bash
git add internal/jellyfin/
git commit -m "$(printf '%s\n' \
  'feat: Zugriff schreiben, ohne die übrige Policy zu verlieren' \
  '' \
  'POST /Users/{id}/Policy ersetzt die Policy vollständig; ein PATCH' \
  'existiert nicht. Deshalb Read-Modify-Write über map[string]json.RawMessage:' \
  'nur IsDisabled, EnableAllFolders und EnabledFolders werden ersetzt, die' \
  'übrigen 41 Felder gehen byteidentisch zurück — auch solche, die der' \
  'Client gar nicht kennt.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 6: Sitzungsspeicher

**Files:**
- Create: `internal/session/store.go`
- Test: `internal/session/store_test.go`

**Interfaces:**
- Consumes: nichts (kennt `jellyfin` **nicht**)
- Produces:
  - `session.Session{Token, UserID, Name string, Expires time.Time}`
  - `session.NewStore(ttl time.Duration) *Store`
  - `(*Store).Create(token, userID, name string) (string, error)` — liefert den Cookie-Wert
  - `(*Store).Get(id string) (Session, bool)`
  - `(*Store).Delete(id string)`
  - `(*Store).Cleanup()`
  - `(*Store).StartCleanup(ctx context.Context, every time.Duration)`

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`internal/session/store_test.go` — **internes** Testpaket, damit die Uhr
austauschbar ist, ohne dafür eine öffentliche Schnittstelle zu bauen:

```go
package session

import (
	"context"
	"testing"
	"time"
)

// bei baut einen Store mit fester Uhr.
func bei(ttl time.Duration, jetzt *time.Time) *Store {
	s := NewStore(ttl)
	s.now = func() time.Time { return *jetzt }
	return s
}

func TestCreateLiefertUnterschiedlicheIDs(t *testing.T) {
	s := NewStore(time.Hour)
	a, err := s.Create("tok", "u-1", "papa")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	b, err := s.Create("tok", "u-1", "papa")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if a == b {
		t.Error("zwei Anmeldungen dürfen nicht dieselbe ID bekommen")
	}
	if len(a) < 32 {
		t.Errorf("ID = %q, zu kurz für ein Sitzungsgeheimnis", a)
	}
}

func TestGetLiefertSitzung(t *testing.T) {
	s := NewStore(time.Hour)
	id, _ := s.Create("tok-123", "u-1", "papa")

	sess, ok := s.Get(id)
	if !ok {
		t.Fatal("Sitzung nicht gefunden")
	}
	if sess.Token != "tok-123" || sess.UserID != "u-1" || sess.Name != "papa" {
		t.Errorf("Sitzung = %+v", sess)
	}
}

func TestGetUnbekanntesCookie(t *testing.T) {
	s := NewStore(time.Hour)
	if _, ok := s.Get("gibt-es-nicht"); ok {
		t.Error("unbekanntes Cookie darf keine Sitzung liefern")
	}
	if _, ok := s.Get(""); ok {
		t.Error("leeres Cookie darf keine Sitzung liefern")
	}
}

func TestGetNachAblauf(t *testing.T) {
	jetzt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	s := bei(time.Hour, &jetzt)
	id, _ := s.Create("tok", "u-1", "papa")

	jetzt = jetzt.Add(59 * time.Minute)
	if _, ok := s.Get(id); !ok {
		t.Error("Sitzung darf vor Ablauf der TTL nicht verschwinden")
	}
	jetzt = jetzt.Add(2 * time.Minute)
	if _, ok := s.Get(id); ok {
		t.Error("Sitzung muss nach Ablauf der TTL ungültig sein")
	}
}

func TestDeleteEntferntSitzung(t *testing.T) {
	s := NewStore(time.Hour)
	id, _ := s.Create("tok", "u-1", "papa")
	s.Delete(id)
	if _, ok := s.Get(id); ok {
		t.Error("gelöschte Sitzung darf nicht mehr gefunden werden")
	}
	s.Delete("gibt-es-nicht") // darf nicht in Panik geraten
}

func TestCleanupGibtSpeicherFrei(t *testing.T) {
	jetzt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	s := bei(time.Hour, &jetzt)
	alt, _ := s.Create("tok", "u-1", "papa")
	jetzt = jetzt.Add(2 * time.Hour)
	neu, _ := s.Create("tok", "u-2", "mama")

	s.Cleanup()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, da := s.items[alt]; da {
		t.Error("abgelaufene Sitzung muss entfernt werden")
	}
	if _, da := s.items[neu]; !da {
		t.Error("gültige Sitzung darf nicht entfernt werden")
	}
}

func TestStartCleanupEndetMitContext(t *testing.T) {
	s := NewStore(time.Hour)
	ctx, abbrechen := context.WithCancel(context.Background())
	s.StartCleanup(ctx, time.Millisecond)
	abbrechen()
	// Kein Test auf Zeitverhalten — es geht darum, dass der Aufruf nicht
	// blockiert und der Context die Schleife beendet.
	time.Sleep(5 * time.Millisecond)
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/session/...`
Expected: FAIL — Paket existiert nicht.

- [ ] **Step 3: Implementierung schreiben**

`internal/session/store.go`:

```go
// Package session hält die Anmeldungen am Dienst im Arbeitsspeicher. Ein
// Neustart bedeutet: neu anmelden — es liegt bewusst kein Token auf der
// Platte (Spec §Authentifizierung und Sitzung).
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Session ist eine angemeldete Sitzung am Dienst. Token ist das
// Jellyfin-Zugriffstoken, mit dem alle Aufrufe laufen.
type Session struct {
	Token   string
	UserID  string
	Name    string
	Expires time.Time
}

type Store struct {
	mu    sync.RWMutex
	items map[string]Session
	ttl   time.Duration
	now   func() time.Time
}

func NewStore(ttl time.Duration) *Store {
	return &Store{
		items: make(map[string]Session),
		ttl:   ttl,
		now:   time.Now,
	}
}

// Create legt eine Sitzung an und liefert den Cookie-Wert.
func (s *Store) Create(token, userID, name string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = Session{
		Token:   token,
		UserID:  userID,
		Name:    name,
		Expires: s.now().Add(s.ttl),
	}
	return id, nil
}

func (s *Store) Get(id string) (Session, bool) {
	if id == "" {
		return Session{}, false
	}
	s.mu.RLock()
	sess, ok := s.items[id]
	s.mu.RUnlock()
	if !ok || !s.now().Before(sess.Expires) {
		return Session{}, false
	}
	return sess, true
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}

// Cleanup entfernt abgelaufene Sitzungen. Get erkennt sie ohnehin als
// ungültig; das hier gibt nur den Speicher frei.
func (s *Store) Cleanup() {
	jetzt := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.items {
		if !jetzt.Before(sess.Expires) {
			delete(s.items, id)
		}
	}
}

// StartCleanup räumt im Hintergrund auf, bis der Context endet.
func (s *Store) StartCleanup(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Cleanup()
			}
		}
	}()
}
```

- [ ] **Step 4: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/session/... -v && go test ./... && go vet ./...`
Expected: PASS, alles grün.

- [ ] **Step 5: Committen**

```bash
git add internal/session/
git commit -m "$(printf '%s\n' \
  'feat: Sitzungsspeicher mit Ablauf im Arbeitsspeicher' \
  '' \
  'Cookie-Werte kommen aus crypto/rand. Sitzungen liegen bewusst nicht' \
  'auf der Platte: ein Neustart bedeutet neu anmelden, dafür bleibt kein' \
  'Jellyfin-Token liegen.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

> **Planänderung gegenüber der Spec (bewusst, in Task 7–8 umgesetzt):**
> An- und Abmeldung laufen als klassische Formulare über `POST /login` und
> `POST /logout` statt über `POST /api/login` / `POST /api/logout`. Damit
> funktioniert die Anmeldung ohne JavaScript, und es braucht keine
> JSON-Fehlerbehandlung im Frontend. JSON bleibt allein für das Speichern
> (`PUT /api/users/{id}/access`). Die Spec ist entsprechend nachgezogen.

---

### Task 7: Build-Werkzeug und Design-System

**Files:**
- Create: `Makefile`
- Create: `web/embed.go`
- Create: `web/src/input.css` (aus dem Nachbarprojekt kopiert und angepasst)
- Create: `web/templates/layout.html`
- Create: `web/templates/login.html`
- Create: `web/static/manifest.webmanifest`
- Create: `web/static/app.js` (zunächst nur ein Platzhalter mit der Toast-Funktion)
- Generated: `web/static/app.css` (durch `make css`)
- Test: `web/embed_test.go`

**Interfaces:**
- Consumes: nichts
- Produces: `web.FS` (`embed.FS` mit `templates` und `static`), Templates `head`, `foot`, `login`; JS-Funktion `toast(text, art)` in `app.js`

- [ ] **Step 1: Verzeichnisse anlegen und das Design-System übernehmen**

```bash
cd /Users/sh/projects/jellyfin-enabler
mkdir -p web/src web/static/icons web/templates
cp /Users/sh/projects/yt-dl-web-client/web/src/input.css web/src/input.css
```

Danach `web/src/input.css` an drei Stellen anpassen:

1. Die beiden `@source`-Zeilen oben ersetzen durch:

```css
@source "../templates";
@source "../static/app.js";
```

2. Die Komponenten `.job-card`, `.bar` und `.bar-fill` aus `@layer components` **löschen** — die gehören zum Download-Projekt.

3. Direkt nach der `.chip`-Regel ergänzen:

```css
  /* Ausgegraut, solange "Alle Bibliotheken" gesetzt ist: die Einzelauswahl
     bleibt sichtbar, ist aber wirkungslos (Spec §EnableAllFolders-Semantik). */
  input:disabled + .chip {
    opacity: 0.5;
    cursor: not-allowed;
  }
```

Alles andere — Tokens, Hell/Dunkel-Blöcke, `@theme inline`, `@layer base`, `.card`, `.btn`, `.pill`, `.seg`, `.field-label`, `.input`, `.select`, `.section-title`, `.toast*` — bleibt unverändert.

- [ ] **Step 2: Makefile schreiben**

`Makefile`:

```makefile
BINARY := bin/app
IMAGE := jellyfin-enabler
# Lokales Native-Image (docker compose / make start) — eigener Name, damit
# das veröffentlichte amd64-Image nicht überschrieben wird.
NATIVE_IMAGE := $(IMAGE)-native
GHCR_USER ?= shdev
TAG ?= latest
# Muss mit ARG TAILWIND_VERSION im Dockerfile übereinstimmen.
TAILWIND_VERSION := 4.3.3
# Muss zum ports-Mapping in docker-compose.yml passen.
HOST_PORT ?= 8081

.PHONY: build test check fmt-check vet run image image-native push check-ghcr-user up down start stop css css-watch clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/server

test:
	go test ./...

check: fmt-check vet test

fmt-check:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

# Lokaler Lauf gegen den echten Jellyfin-Server; URL überschreibbar:
#   make run JELLYFIN_URL=http://nas:8096
JELLYFIN_URL ?= http://localhost:8096

run: build
	PORT=8081 JELLYFIN_URL=$(JELLYFIN_URL) $(BINARY)

image:
	docker build --platform linux/amd64 -t $(IMAGE) .

image-native:
	docker build -t $(NATIVE_IMAGE) .

push: check-ghcr-user image
	docker tag $(IMAGE) ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG)
	docker push ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG)

check-ghcr-user:
	@test -n "$(GHCR_USER)" || { echo "GHCR_USER fehlt: make push GHCR_USER=<github-user>"; exit 1; }

up:
	docker compose up -d --build

down:
	docker compose down

stop: down

# Image bauen, Container starten und die UI öffnen, sobald der Dienst
# antwortet (max. 60 s).
start: up
	@ok=0; for i in $$(seq 1 60); do \
	  curl -fsS -o /dev/null http://localhost:$(HOST_PORT)/healthz && { ok=1; break; }; \
	  sleep 1; \
	done; \
	test $$ok -eq 1 || { echo "Dienst antwortet nicht auf http://localhost:$(HOST_PORT)"; exit 1; }
	@command -v open >/dev/null 2>&1 \
	  && open "http://localhost:$(HOST_PORT)/" \
	  || xdg-open "http://localhost:$(HOST_PORT)/"

clean:
	rm -rf bin

# Tailwind-CSS via Docker kompilieren — kein lokales npm nötig.
# Der Symlink nach /node_modules ist nötig, weil der v4-Resolver das Paket
# "tailwindcss" vom Verzeichnis der Input-Datei aufwärts sucht.
TAILWIND_RUN = npm install -g @tailwindcss/cli@$(TAILWIND_VERSION) >/dev/null \
	&& ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules \
	&& cd /work && tailwindcss -i web/src/input.css -o web/static/app.css

css:
	docker run --rm -v $(CURDIR):/work -v jellyfin-enabler-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --minify"

css-watch:
	docker run --rm -it -v $(CURDIR):/work -v jellyfin-enabler-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --watch"
```

- [ ] **Step 3: Templates und Assets anlegen**

`web/templates/layout.html`:

```html
{{define "head"}}<!doctype html>
<html lang="de">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <meta name="theme-color" media="(prefers-color-scheme: dark)" content="#0f1116">
  <meta name="theme-color" media="(prefers-color-scheme: light)" content="#f6f7f9">
  <meta name="mobile-web-app-capable" content="yes">
  <meta name="apple-mobile-web-app-capable" content="yes">
  <meta name="apple-mobile-web-app-status-bar-style" content="default">
  <meta name="apple-mobile-web-app-title" content="Jellyfin">
  <title>{{.Title}}</title>
  <link rel="manifest" href="/manifest.webmanifest">
  <link rel="apple-touch-icon" href="/static/icons/apple-touch-icon.png">
  <link rel="icon" type="image/png" sizes="192x192" href="/static/icons/icon-192.png">
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="mx-auto max-w-[720px] px-4 py-7 sm:px-6">
{{end}}

{{define "foot"}}  </div>
  <div class="toast-region" id="toast-region" role="status" aria-live="polite"></div>
  <script src="/static/app.js"></script>
</body>
</html>
{{end}}
```

`web/templates/login.html`:

```html
{{define "login"}}{{template "head" .}}
    <header class="mb-6">
      <h1 class="text-[17px] font-semibold tracking-tight">Jellyfin <span class="text-accent">Zugriff</span></h1>
    </header>
    <section class="card">
      <form class="grid gap-4" method="post" action="/login">
        <div>
          <label class="field-label" for="username">Administrator</label>
          <input class="input" type="text" id="username" name="username" autocomplete="username" autofocus required>
        </div>
        <div>
          <label class="field-label" for="password">Passwort</label>
          <input class="input" type="password" id="password" name="password" autocomplete="current-password" required>
        </div>
        <button class="btn btn-primary" type="submit">Anmelden</button>
        {{if .Error}}<div class="text-sm text-danger">{{.Error}}</div>{{end}}
      </form>
    </section>
{{template "foot" .}}{{end}}
```

`web/static/manifest.webmanifest`:

```json
{
  "name": "Jellyfin Zugriff",
  "short_name": "Zugriff",
  "description": "Jellyfin-Benutzer aktivieren, deaktivieren und Bibliotheken zuordnen",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#0f1116",
  "theme_color": "#0f1116",
  "icons": [
    { "src": "/static/icons/icon-192.png", "sizes": "192x192", "type": "image/png" },
    { "src": "/static/icons/icon-512.png", "sizes": "512x512", "type": "image/png" },
    { "src": "/static/icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
```

`web/static/app.js` — vorerst nur die Toast-Funktion, der Rest folgt in Task 12:

```javascript
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
```

`web/embed.go`:

```go
// Package web bettet alle UI-Assets ins Binary ein — der Dienst besteht aus
// einer einzigen Datei und braucht keinen Internetzugang.
package web

import "embed"

//go:embed templates static
var FS embed.FS
```

- [ ] **Step 4: CSS erzeugen**

Run: `make css`
Expected: `web/static/app.css` entsteht. Braucht ein laufendes Docker. Schlägt das fehl, **hier abbrechen und melden** — ein von Hand geschriebenes `app.css` wäre falsch, weil der Docker-Build es ohnehin neu erzeugt.

- [ ] **Step 5: Embed-Test schreiben und laufen lassen**

`web/embed_test.go`:

```go
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
```

Run: `go test ./web/... -v`
Expected: PASS.

- [ ] **Step 6: Committen**

```bash
git add Makefile web/
git commit -m "$(printf '%s\n' \
  'feat: Design-System, Templates und Build-Werkzeug' \
  '' \
  'Tokens und Komponenten stammen aus yt-dl-web-client, damit beide' \
  'Dienste gleich aussehen; die auftragsspezifischen Klassen sind' \
  'entfernt und eine Regel für ausgegraute Chips kam dazu. Tailwind' \
  'wird wie dort im Container kompiliert, ein lokales npm ist nicht nötig.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 8: Server-Gerüst, An- und Abmeldung

**Files:**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `jellyfin.Session`, `jellyfin.User`, `jellyfin.Library`, `jellyfin.Access`, `jellyfin.ErrNotAdmin`, `jellyfin.IsUnauthorized`, `session.Store`, `web.FS`
- Produces:
  - `server.Jellyfin` (Schnittstelle mit `Authenticate`, `Logout`, `Users`, `User`, `Libraries`, `UpdateAccess`)
  - `server.New(jf Jellyfin, sessions *session.Store, cookieSecure string) http.Handler`
  - Cookie-Name `jfe_session`

- [ ] **Step 1: Die fehlschlagenden Tests schreiben**

`internal/server/server_test.go`:

```go
package server_test

import (
	"context"
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

// fakeJF ersetzt den Jellyfin-Client. Die Handler-Tests sollen Routing,
// Sitzungsprüfung und Darstellung prüfen, nicht die API-Anbindung.
type fakeJF struct {
	sess      jellyfin.Session
	authErr   error
	users     []jellyfin.User
	usersErr  error
	user      jellyfin.User
	userErr   error
	libs      []jellyfin.Library
	libsErr   error
	written   jellyfin.Access
	writtenID string
	updateErr error
	loggedOut []string
}

func (f *fakeJF) Authenticate(_ context.Context, _, _ string) (jellyfin.Session, error) {
	return f.sess, f.authErr
}
func (f *fakeJF) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return nil
}
func (f *fakeJF) Users(context.Context, string) ([]jellyfin.User, error) {
	return f.users, f.usersErr
}
func (f *fakeJF) User(context.Context, string, string) (jellyfin.User, error) {
	return f.user, f.userErr
}
func (f *fakeJF) Libraries(context.Context, string) ([]jellyfin.Library, error) {
	return f.libs, f.libsErr
}
func (f *fakeJF) UpdateAccess(_ context.Context, _, userID string, a jellyfin.Access) (jellyfin.Access, error) {
	f.writtenID, f.written = userID, a
	if f.updateErr != nil {
		return jellyfin.Access{}, f.updateErr
	}
	return a, nil
}

// aufbau liefert Handler, Fake und Sitzungsspeicher.
func aufbau(t *testing.T) (http.Handler, *fakeJF, *session.Store) {
	t.Helper()
	jf := &fakeJF{sess: jellyfin.Session{Token: "tok", UserID: "u-1", Name: "papa"}}
	store := session.NewStore(time.Hour)
	return server.New(jf, store, "false"), jf, store
}

// angemeldet legt eine Sitzung an und liefert das passende Cookie.
func angemeldet(t *testing.T, store *session.Store) *http.Cookie {
	t.Helper()
	id, err := store.Create("tok", "u-1", "papa")
	if err != nil {
		t.Fatalf("Sitzung nicht anlegbar: %v", err)
	}
	return &http.Cookie{Name: "jfe_session", Value: id}
}

func TestHealthzOhneAnmeldung(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Code = %d, erwartet 200", rec.Code)
	}
}

func TestSeiteOhneSitzungLeitetAufLogin(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Code = %d, erwartet 303", rec.Code)
	}
	if ort := rec.Header().Get("Location"); ort != "/login" {
		t.Errorf("Location = %q, erwartet /login", ort)
	}
}

func TestAPIOhneSitzungGibt401(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/users/u-2/access", strings.NewReader("{}"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Code = %d, erwartet 401", rec.Code)
	}
}

func TestLoginSeiteIstErreichbar(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `name="password"`) {
		t.Error("Anmeldeformular fehlt")
	}
}

func TestLoginSeiteLeitetBeiSitzungAufStart(t *testing.T) {
	h, _, store := aufbau(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(angemeldet(t, store))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("Code = %d, Location = %q — erwartet 303 nach /", rec.Code, rec.Header().Get("Location"))
	}
}

func anmelden(h http.Handler, name, pw string) *httptest.ResponseRecorder {
	form := url.Values{"username": {name}, "password": {pw}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginSetztCookieUndLeitetWeiter(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := anmelden(h, "papa", "geheim")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("Code = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	var gefunden *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jfe_session" {
			gefunden = c
		}
	}
	if gefunden == nil {
		t.Fatal("Sitzungscookie fehlt")
	}
	if !gefunden.HttpOnly {
		t.Error("Cookie muss HttpOnly sein")
	}
	if gefunden.SameSite != http.SameSiteLaxMode {
		t.Error("Cookie muss SameSite=Lax sein")
	}
}

func TestLoginMitFalschenDatenZeigtFehler(t *testing.T) {
	h, jf, _ := aufbau(t)
	jf.authErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}
	rec := anmelden(h, "papa", "falsch")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, erwartet 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Anmeldung fehlgeschlagen") {
		t.Errorf("Rumpf ohne Fehlermeldung: %s", rec.Body.String())
	}
}

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

func TestLogoutEntwertetTokenUndLoeschtCookie(t *testing.T) {
	h, jf, store := aufbau(t)
	c := angemeldet(t, store)
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("Code = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(jf.loggedOut) != 1 || jf.loggedOut[0] != "tok" {
		t.Errorf("Jellyfin-Abmeldung = %v, erwartet [tok]", jf.loggedOut)
	}
	if _, ok := store.Get(c.Value); ok {
		t.Error("Sitzung muss aus dem Speicher verschwinden")
	}
}

func TestNoStoreAufAllenAntworten(t *testing.T) {
	h, _, _ := aufbau(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, erwartet no-store", got)
	}
}
```

> **Nachtrag aus dem Review (umgesetzt in `80bb40c`):** Die Tests oben prüfen
> das `Secure`-Flag des Sitzungscookies nicht — alle drei Zweige von `secure()`
> blieben ungetestet, eine vertauschte Bedingung wäre unbemerkt geblieben.
> Ergänzt wurden deshalb eine Hilfsfunktion `aufbauMit(t, cookieSecure)`, ein
> Tabellentest `TestSecureFlagAmSitzungscookie` über vier Fälle (`"true"` ohne
> TLS, `"false"` **mit** TLS, `"auto"` ohne und mit TLS, Letzteres über
> `req.TLS = &tls.ConnectionState{}`) und `TestLoeschCookieHatDieselbenAttribute`.

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/server/...`
Expected: FAIL — Paket existiert nicht.

- [ ] **Step 3: Server schreiben**

`internal/server/server.go`:

```go
// Package server stellt die Oberfläche und die Speicher-API bereit
// (Spec §HTTP-Oberfläche).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/web"
)

const cookieName = "jfe_session"

// Jellyfin ist der Ausschnitt des API-Clients, den die Handler brauchen.
// Als Schnittstelle, damit die Tests ohne echten Server auskommen.
type Jellyfin interface {
	Authenticate(ctx context.Context, username, password string) (jellyfin.Session, error)
	Logout(ctx context.Context, token string) error
	Users(ctx context.Context, token string) ([]jellyfin.User, error)
	User(ctx context.Context, token, userID string) (jellyfin.User, error)
	Libraries(ctx context.Context, token string) ([]jellyfin.Library, error)
	UpdateAccess(ctx context.Context, token, userID string, a jellyfin.Access) (jellyfin.Access, error)
}

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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /static/", http.FileServerFS(web.FS))
	mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	return noStore(mux)
}

// noStore verbietet jedes clientseitige Caching: der Dienst liefert
// ausschließlich Oberfläche und API, beides soll immer frisch sein.
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	b, err := web.FS.ReadFile("static/manifest.webmanifest")
	if err != nil {
		http.Error(w, "Manifest fehlt", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write(b)
}

// aktuelleSitzung liest das Cookie und schlägt die Sitzung nach.
func (s *Server) aktuelleSitzung(r *http.Request) (session.Session, string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return session.Session{}, "", false
	}
	sess, ok := s.sessions.Get(c.Value)
	return sess, c.Value, ok
}

// seite schützt eine Seitenroute: ohne Sitzung geht es zum Anmeldeformular.
func (s *Server) seite(fn func(http.ResponseWriter, *http.Request, session.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, _, ok := s.aktuelleSitzung(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		fn(w, r, sess)
	}
}

// api schützt eine API-Route: ohne Sitzung gibt es 401, damit das Frontend
// den Unterschied zu einem Fehler erkennt.
func (s *Server) api(fn func(http.ResponseWriter, *http.Request, session.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, _, ok := s.aktuelleSitzung(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Die Anmeldung ist abgelaufen.")
			return
		}
		fn(w, r, sess)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("Antwort nicht schreibbar: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

type loginData struct {
	Title string
	Error string
}

func (s *Server) renderLogin(w http.ResponseWriter, code int, meldung string) {
	w.WriteHeader(code)
	if err := s.tmpl.ExecuteTemplate(w, "login", loginData{Title: "Anmelden", Error: meldung}); err != nil {
		log.Printf("Anmeldeseite nicht darstellbar: %v", err)
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.aktuelleSitzung(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, http.StatusOK, "")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, http.StatusBadRequest, "Das Formular war unvollständig.")
		return
	}
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
	if err != nil {
		log.Printf("Sitzung nicht anlegbar: %v", err)
		s.renderLogin(w, http.StatusInternalServerError, "Die Sitzung konnte nicht angelegt werden.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure(r),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// secure entscheidet über das Secure-Flag. "auto" setzt es nur bei direktem
// TLS; hinter einem TLS-terminierenden Proxy ist COOKIE_SECURE=true nötig.
func (s *Server) secure(r *http.Request) bool {
	switch s.cookieSecure {
	case "true":
		return true
	case "false":
		return false
	}
	return r.TLS != nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess, id, ok := s.aktuelleSitzung(r); ok {
		if err := s.jf.Logout(r.Context(), sess.Token); err != nil {
			// Die eigene Abmeldung darf daran nicht scheitern.
			log.Printf("Jellyfin-Abmeldung fehlgeschlagen: %v", err)
		}
		s.sessions.Delete(id)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure(r),
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
```

Damit `TestSeiteOhneSitzungLeitetAufLogin` und `TestAPIOhneSitzungGibt401` grün werden, zusätzlich die beiden noch leeren Routen registrieren — die Inhalte kommen in Task 9 und 11:

```go
	mux.HandleFunc("GET /{$}", s.seite(s.handleIndex))
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(s.handleUpdateAccess))
```

und vorläufig:

```go
func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request, _ session.Session) {
	http.Error(w, "kommt in Task 9", http.StatusNotImplemented)
}

func (s *Server) handleUpdateAccess(w http.ResponseWriter, _ *http.Request, _ session.Session) {
	writeError(w, http.StatusNotImplemented, "kommt in Task 11")
}
```

- [ ] **Step 4: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/server/... -v`
Expected: PASS.

- [ ] **Step 5: Committen**

```bash
git add internal/server/
git commit -m "$(printf '%s\n' \
  'feat: Server-Gerüst mit An- und Abmeldung' \
  '' \
  'Anmeldung als klassisches Formular, damit sie ohne JavaScript' \
  'funktioniert. Nicht-Administratoren werden abgewiesen; beim Abmelden' \
  'wird auch das Jellyfin-Token entwertet, nicht nur das Cookie gelöscht.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 9: Benutzerliste

**Files:**
- Create: `web/templates/users.html`
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go` (anhängen)

**Interfaces:**
- Consumes: `Jellyfin.Users`, `Jellyfin.Libraries`
- Produces: Template `users`, `(*Server).handleIndex`, paketintern `zugriffText(u jellyfin.User, gesamt int) string`, `(*Server).seitenFehler(w, r, err)`

- [ ] **Step 1: Die fehlschlagenden Tests anhängen**

An `internal/server/server_test.go`:

```go
func beispielBenutzer() []jellyfin.User {
	return []jellyfin.User{
		{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Disabled: true, Folders: []string{"lib-a"}}},
		{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}},
	}
}

func beispielBibliotheken() []jellyfin.Library {
	return []jellyfin.Library{{ID: "lib-a", Name: "Filme"}, {ID: "lib-b", Name: "Serien"}}
}

func holen(t *testing.T, h http.Handler, pfad string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, pfad, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListeZeigtBenutzerMitStatus(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.users, jf.libs = beispielBenutzer(), beispielBibliotheken()

	rec := holen(t, h, "/", angemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	rumpf := rec.Body.String()
	for _, teil := range []string{"kind1", "papa", "Administrator", "Aus", "Aktiv", `href="/users/u-2"`} {
		if !strings.Contains(rumpf, teil) {
			t.Errorf("Liste ohne %q", teil)
		}
	}
}

func TestZugriffTextInDerListe(t *testing.T) {
	cases := []struct {
		name   string
		access jellyfin.Access
		text   string
	}{
		{"alle künftigen", jellyfin.Access{AllFolders: true}, "Alle Bibliotheken"},
		{"keine", jellyfin.Access{}, "Keine Bibliothek"},
		{"eine von zwei", jellyfin.Access{Folders: []string{"lib-a"}}, "1 von 2 Bibliotheken"},
		{"alle angehakt", jellyfin.Access{Folders: []string{"lib-a", "lib-b"}}, "Alle 2 Bibliotheken (ohne künftige)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, jf, store := aufbau(t)
			jf.libs = beispielBibliotheken()
			jf.users = []jellyfin.User{{ID: "u-2", Name: "kind1", Access: tc.access}}

			rec := holen(t, h, "/", angemeldet(t, store))
			if !strings.Contains(rec.Body.String(), tc.text) {
				t.Errorf("Liste ohne %q", tc.text)
			}
		})
	}
}

func TestListeBeiAbgelaufenemTokenLeitetAufLogin(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.usersErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}

	c := angemeldet(t, store)
	rec := holen(t, h, "/", c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("Code = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, ok := store.Get(c.Value); ok {
		t.Error("die tote Sitzung muss verworfen werden")
	}
}

func TestListeBeiJellyfinAusfallGibt502(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.usersErr = errors.New("Verbindung abgelehnt")

	rec := holen(t, h, "/", angemeldet(t, store))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("Code = %d, erwartet 502", rec.Code)
	}
}
```

Den Import `"errors"` in der Testdatei ergänzen.

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/server/... -run Liste`
Expected: FAIL — `501 kommt in Task 9`.

- [ ] **Step 3: Template anlegen**

`web/templates/users.html`:

```html
{{define "users"}}{{template "head" .}}
    <header class="mb-6 flex items-center justify-between gap-3">
      <h1 class="text-[17px] font-semibold tracking-tight">Jellyfin <span class="text-accent">Zugriff</span></h1>
      <form method="post" action="/logout">
        <button class="btn btn-ghost btn-sm" type="submit">Abmelden</button>
      </form>
    </header>
    <h2 class="section-title mx-0.5 mb-2.5">Benutzer</h2>
    <div class="grid gap-2">
      {{range .Users}}
      <a class="card flex items-center justify-between gap-3 no-underline" href="/users/{{.ID}}">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="truncate text-[14.5px] font-semibold text-text">{{.Name}}</span>
            {{if .Admin}}<span class="pill pill-wait">Administrator</span>{{end}}
          </div>
          <div class="mt-0.5 text-[12.5px] text-muted">{{.Access}}</div>
        </div>
        {{if .Disabled}}<span class="pill pill-err"><span class="pill-dot"></span>Aus</span>
        {{else}}<span class="pill pill-ok"><span class="pill-dot"></span>Aktiv</span>{{end}}
      </a>
      {{end}}
    </div>
{{template "foot" .}}{{end}}
```

- [ ] **Step 4: Handler schreiben**

In `internal/server/server.go` den Platzhalter `handleIndex` ersetzen und ergänzen:

```go
type userRow struct {
	ID       string
	Name     string
	Admin    bool
	Disabled bool
	Access   string
}

type usersData struct {
	Title string
	Users []userRow
}

// ACHTUNG (Review-Nachtrag, umgesetzt in e256b82): Diese Fassung vergleicht
// nur Zählwerte. Steht in Access.Folders die ID einer gelöschten Bibliothek,
// meldet sie fälschlich "Alle N Bibliotheken". Die gebaute Fassung nimmt
// deshalb libs []jellyfin.Library entgegen und zählt nur Treffer gegen die
// vorhandenen Bibliotheken; handleIndex ruft zugriffText(u, libs) auf.
// zugriffText beschreibt die Bibliotheksfreigabe in einem Satz. "Alle
// Bibliotheken" und "alle angehakt" sind bewusst unterschiedliche Aussagen:
// nur Ersteres schließt künftig neue Bibliotheken ein
// (Spec §EnableAllFolders-Semantik).
func zugriffText(u jellyfin.User, gesamt int) string {
	if u.Access.AllFolders {
		return "Alle Bibliotheken"
	}
	n := len(u.Access.Folders)
	switch {
	case n == 0:
		return "Keine Bibliothek"
	case n >= gesamt && gesamt > 0:
		return fmt.Sprintf("Alle %d Bibliotheken (ohne künftige)", gesamt)
	default:
		return fmt.Sprintf("%d von %d Bibliotheken", n, gesamt)
	}
}

// seitenFehler behandelt einen Fehler auf einer Seitenroute: ein abgelaufenes
// Token führt zurück zum Anmeldeformular, alles andere ist ein Ausfall der
// Gegenstelle.
func (s *Server) seitenFehler(w http.ResponseWriter, r *http.Request, err error) {
	if jellyfin.IsUnauthorized(err) {
		if _, id, ok := s.aktuelleSitzung(r); ok {
			s.sessions.Delete(id)
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	log.Printf("Jellyfin nicht erreichbar: %v", err)
	http.Error(w, "Jellyfin ist gerade nicht erreichbar.", http.StatusBadGateway)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request, sess session.Session) {
	users, err := s.jf.Users(r.Context(), sess.Token)
	if err != nil {
		s.seitenFehler(w, r, err)
		return
	}
	libs, err := s.jf.Libraries(r.Context(), sess.Token)
	if err != nil {
		s.seitenFehler(w, r, err)
		return
	}
	daten := usersData{Title: "Jellyfin Zugriff"}
	for _, u := range users {
		daten.Users = append(daten.Users, userRow{
			ID:       u.ID,
			Name:     u.Name,
			Admin:    u.Admin,
			Disabled: u.Access.Disabled,
			Access:   zugriffText(u, len(libs)),
		})
	}
	if err := s.tmpl.ExecuteTemplate(w, "users", daten); err != nil {
		log.Printf("Benutzerliste nicht darstellbar: %v", err)
	}
}
```

Den Import `"fmt"` ergänzen.

- [ ] **Step 5: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/server/... -v`
Expected: PASS.

- [ ] **Step 6: Committen**

```bash
git add internal/server/ web/templates/users.html
git commit -m "$(printf '%s\n' \
  'feat: Benutzerliste mit Status und Bibliotheksfreigabe' \
  '' \
  '"Alle Bibliotheken" und "alle angehakt" sind zwei verschiedene' \
  'Aussagen — nur Ersteres schließt künftig neue Bibliotheken ein. Die' \
  'Liste benennt das, statt beides gleich darzustellen.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 10: Detailseite eines Benutzers

**Files:**
- Create: `web/templates/user.html`
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go` (anhängen)

**Interfaces:**
- Consumes: `Jellyfin.User`, `Jellyfin.Libraries`
- Produces: Template `user`, `(*Server).handleUserPage`, `libraryRow{ID, Name string, Checked bool}`, `userData{Title string, User jellyfin.User, Libraries []libraryRow}`

- [ ] **Step 1: Die fehlschlagenden Tests anhängen**

```go
func TestDetailseiteZeigtBibliotheken(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-2", Name: "kind1", Access: jellyfin.Access{Folders: []string{"lib-a"}}}

	rec := holen(t, h, "/users/u-2", angemeldet(t, store))
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, erwartet 200", rec.Code)
	}
	rumpf := rec.Body.String()
	for _, teil := range []string{"kind1", "Filme", "Serien", `data-user-id="u-2"`, "Alle Bibliotheken, auch künftig neue"} {
		if !strings.Contains(rumpf, teil) {
			t.Errorf("Detailseite ohne %q", teil)
		}
	}
	// Filme ist freigegeben, Serien nicht.
	if !strings.Contains(rumpf, `value="lib-a" checked`) {
		t.Error("die freigegebene Bibliothek ist nicht angehakt")
	}
	if strings.Contains(rumpf, `value="lib-b" checked`) {
		t.Error("die gesperrte Bibliothek darf nicht angehakt sein")
	}
}

func TestDetailseiteGrautAdminSchalterAus(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-1", Name: "papa", Admin: true, Access: jellyfin.Access{AllFolders: true}}

	rumpf := holen(t, h, "/users/u-1", angemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, "Administratoren können nicht deaktiviert werden.") {
		t.Error("Hinweis für Administratoren fehlt")
	}
	if !strings.Contains(rumpf, "disabled") {
		t.Error("der Aktiv-Schalter muss für Administratoren ausgegraut sein")
	}
}

func TestDetailseiteGrautChipsBeiAllenBibliothekenAus(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.user = jellyfin.User{ID: "u-2", Name: "kind1", Access: jellyfin.Access{AllFolders: true}}

	rumpf := holen(t, h, "/users/u-2", angemeldet(t, store)).Body.String()
	if !strings.Contains(rumpf, `value="lib-a" disabled`) {
		t.Error("die Einzelauswahl muss ausgegraut sein, solange alle Bibliotheken gelten")
	}
}

func TestDetailseiteUnbekannterBenutzer(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.userErr = &jellyfin.Error{Status: http.StatusNotFound, Message: "Der Benutzer existiert nicht mehr."}

	rec := holen(t, h, "/users/gibt-es-nicht", angemeldet(t, store))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Code = %d, erwartet 404", rec.Code)
	}
}
```

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/server/... -run Detailseite`
Expected: FAIL — die Route gibt es noch nicht (404 vom Mux, nicht vom Handler).

- [ ] **Step 3: Template anlegen**

`web/templates/user.html`:

```html
{{define "user"}}{{template "head" .}}
    <header class="mb-6 flex items-center justify-between gap-3">
      <a class="btn btn-ghost btn-sm no-underline" href="/">Zurück</a>
      <h1 class="truncate text-[17px] font-semibold tracking-tight">{{.User.Name}}</h1>
    </header>

    <section class="card grid gap-4" id="user-form" data-user-id="{{.User.ID}}">
      <div>
        <div class="field-label">Konto</div>
        <div class="seg" role="radiogroup" aria-label="Konto">
          <input class="sr-only" type="radio" name="enabled" id="enabled-on" value="on"{{if not .User.Access.Disabled}} checked{{end}}{{if .User.Admin}} disabled{{end}}>
          <label class="seg-label" for="enabled-on">Aktiv</label>
          <input class="sr-only" type="radio" name="enabled" id="enabled-off" value="off"{{if .User.Access.Disabled}} checked{{end}}{{if .User.Admin}} disabled{{end}}>
          <label class="seg-label" for="enabled-off">Aus</label>
        </div>
        {{if .User.Admin}}<div class="mt-1.5 text-xs text-muted">Administratoren können nicht deaktiviert werden.</div>{{end}}
      </div>

      <div class="h-px bg-edge"></div>

      <label class="flex cursor-pointer items-center gap-2 text-sm text-text">
        <input type="checkbox" id="all-folders"{{if .User.Access.AllFolders}} checked{{end}}>
        Alle Bibliotheken, auch künftig neue
      </label>

      <div>
        <div class="field-label">Bibliotheken</div>
        <div class="flex flex-wrap gap-2" id="libraries">
          {{range .Libraries}}
          <label>
            <input class="sr-only" type="checkbox" name="folder" value="{{.ID}}"{{if .Checked}} checked{{end}}{{if $.User.Access.AllFolders}} disabled{{end}}>
            <span class="chip">{{.Name}}</span>
          </label>
          {{end}}
        </div>
      </div>

      <div class="flex items-center gap-3">
        <button class="btn btn-primary" id="save-btn" type="button" disabled>Speichern</button>
        <span class="text-xs text-muted" id="save-hint">Keine Änderungen</span>
      </div>
    </section>
{{template "foot" .}}{{end}}
```

- [ ] **Step 4: Handler schreiben**

In `internal/server/server.go` die Route registrieren:

```go
	mux.HandleFunc("GET /users/{id}", s.seite(s.handleUserPage))
```

und ergänzen:

```go
type libraryRow struct {
	ID      string
	Name    string
	Checked bool
}

type userData struct {
	Title     string
	User      jellyfin.User
	Libraries []libraryRow
}

func (s *Server) handleUserPage(w http.ResponseWriter, r *http.Request, sess session.Session) {
	id := r.PathValue("id")
	u, err := s.jf.User(r.Context(), sess.Token, id)
	if err != nil {
		var jfErr *jellyfin.Error
		if errors.As(err, &jfErr) && jfErr.Status == http.StatusNotFound {
			http.Error(w, jfErr.Message, http.StatusNotFound)
			return
		}
		s.seitenFehler(w, r, err)
		return
	}
	libs, err := s.jf.Libraries(r.Context(), sess.Token)
	if err != nil {
		s.seitenFehler(w, r, err)
		return
	}
	freigegeben := make(map[string]bool, len(u.Access.Folders))
	for _, f := range u.Access.Folders {
		freigegeben[f] = true
	}
	daten := userData{Title: u.Name + " · Jellyfin Zugriff", User: u}
	for _, l := range libs {
		daten.Libraries = append(daten.Libraries, libraryRow{ID: l.ID, Name: l.Name, Checked: freigegeben[l.ID]})
	}
	if err := s.tmpl.ExecuteTemplate(w, "user", daten); err != nil {
		log.Printf("Detailseite nicht darstellbar: %v", err)
	}
}
```

- [ ] **Step 5: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./internal/server/... -v`
Expected: PASS.

- [ ] **Step 6: Committen**

```bash
git add internal/server/ web/templates/user.html
git commit -m "$(printf '%s\n' \
  'feat: Detailseite mit Aktiv-Schalter und Bibliotheks-Chips' \
  '' \
  'Bei Administratoren ist der Aktiv-Schalter ausgegraut, damit der' \
  'sichere 403 von Jellyfin gar nicht erst provoziert wird. Die' \
  'Einzelauswahl bleibt sichtbar, wenn alle Bibliotheken gelten — sie' \
  'steht beim Zurückschalten wieder bereit.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 11: Speichern

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go` (anhängen)

**Interfaces:**
- Consumes: `Jellyfin.Libraries`, `Jellyfin.UpdateAccess`
- Produces: `(*Server).handleUpdateAccess`, JSON-Format `{"disabled": bool, "allFolders": bool, "folders": []string}` für Anfrage und Antwort

- [ ] **Step 1: Die fehlschlagenden Tests anhängen**

```go
func speichern(t *testing.T, h http.Handler, c *http.Cookie, id, rumpf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/users/"+id+"/access", strings.NewReader(rumpf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSpeichernSchreibtUndAntwortetMitBestaetigtemStand(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	rec := speichern(t, h, angemeldet(t, store), "u-2",
		`{"disabled": true, "allFolders": false, "folders": ["lib-a"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, Rumpf %s", rec.Code, rec.Body.String())
	}
	if jf.writtenID != "u-2" {
		t.Errorf("geschrieben für %q, erwartet u-2", jf.writtenID)
	}
	if !jf.written.Disabled || jf.written.AllFolders || len(jf.written.Folders) != 1 {
		t.Errorf("geschriebener Zugriff = %+v", jf.written)
	}
	var antwort struct {
		Disabled   bool     `json:"disabled"`
		AllFolders bool     `json:"allFolders"`
		Folders    []string `json:"folders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &antwort); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v", err)
	}
	if !antwort.Disabled || len(antwort.Folders) != 1 || antwort.Folders[0] != "lib-a" {
		t.Errorf("Antwort = %+v, erwartet den bestätigten Stand", antwort)
	}
}

func TestSpeichernLehntUnbekannteBibliothekAb(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	rec := speichern(t, h, angemeldet(t, store), "u-2",
		`{"disabled": false, "allFolders": false, "folders": ["lib-a", "erfunden"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, erwartet 400", rec.Code)
	}
	if jf.writtenID != "" {
		t.Error("bei ungültiger Eingabe darf nichts geschrieben werden")
	}
}

func TestSpeichernKaputtesJSON(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()

	rec := speichern(t, h, angemeldet(t, store), "u-2", `{kein json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Code = %d, erwartet 400", rec.Code)
	}
}

func TestSpeichernReichtVerbotDurch(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.updateErr = &jellyfin.Error{
		Status:  http.StatusForbidden,
		Message: "Administratoren können nicht deaktiviert werden.",
		Body:    "Administrators cannot be disabled.",
	}

	rec := speichern(t, h, angemeldet(t, store), "u-1", `{"disabled": true, "allFolders": true, "folders": []}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Code = %d, erwartet 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Administratoren können nicht deaktiviert werden.") {
		t.Errorf("Rumpf = %s", rec.Body.String())
	}
}

func TestSpeichernBeiAbgelaufenemTokenGibt401(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.updateErr = &jellyfin.Error{Status: http.StatusUnauthorized, Message: "Die Anmeldung ist abgelaufen."}

	c := angemeldet(t, store)
	rec := speichern(t, h, c, "u-2", `{"disabled": false, "allFolders": false, "folders": []}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, erwartet 401", rec.Code)
	}
	if _, ok := store.Get(c.Value); ok {
		t.Error("die tote Sitzung muss verworfen werden")
	}
}

func TestSpeichernBeiJellyfinAusfallGibt502(t *testing.T) {
	h, jf, store := aufbau(t)
	jf.libs = beispielBibliotheken()
	jf.updateErr = errors.New("Verbindung abgelehnt")

	rec := speichern(t, h, angemeldet(t, store), "u-2", `{"disabled": false, "allFolders": false, "folders": []}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("Code = %d, erwartet 502", rec.Code)
	}
}
```

Den Import `"encoding/json"` in der Testdatei ergänzen.

- [ ] **Step 2: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/server/... -run Speichern`
Expected: FAIL — `501 kommt in Task 11`.

- [ ] **Step 3: Handler schreiben**

Den Platzhalter `handleUpdateAccess` in `internal/server/server.go` ersetzen:

```go
type accessRequest struct {
	Disabled   bool     `json:"disabled"`
	AllFolders bool     `json:"allFolders"`
	Folders    []string `json:"folders"`
}

type accessResponse struct {
	Disabled   bool     `json:"disabled"`
	AllFolders bool     `json:"allFolders"`
	Folders    []string `json:"folders"`
}

func (s *Server) handleUpdateAccess(w http.ResponseWriter, r *http.Request, sess session.Session) {
	var req accessRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Die Anfrage war nicht lesbar.")
		return
	}
	// Nur bekannte Bibliotheken zulassen — sonst landet Unsinn in der Policy.
	libs, err := s.jf.Libraries(r.Context(), sess.Token)
	if err != nil {
		s.apiFehler(w, r, err)
		return
	}
	bekannt := make(map[string]bool, len(libs))
	for _, l := range libs {
		bekannt[l.ID] = true
	}
	for _, f := range req.Folders {
		if !bekannt[f] {
			writeError(w, http.StatusBadRequest, "Unbekannte Bibliothek: "+f)
			return
		}
	}

	neu, err := s.jf.UpdateAccess(r.Context(), sess.Token, r.PathValue("id"), jellyfin.Access{
		Disabled:   req.Disabled,
		AllFolders: req.AllFolders,
		Folders:    req.Folders,
	})
	if err != nil {
		s.apiFehler(w, r, err)
		return
	}
	folders := neu.Folders
	if folders == nil {
		folders = []string{}
	}
	writeJSON(w, http.StatusOK, accessResponse{
		Disabled:   neu.Disabled,
		AllFolders: neu.AllFolders,
		Folders:    folders,
	})
}

// apiFehler übersetzt einen Fehler in eine JSON-Antwort. Die deutschen
// Meldungen kommen aus dem Client, damit die Oberfläche sie unverändert
// anzeigen kann (Spec §Fehlerbehandlung).
func (s *Server) apiFehler(w http.ResponseWriter, r *http.Request, err error) {
	var jfErr *jellyfin.Error
	if errors.As(err, &jfErr) {
		if jfErr.Unauthorized() {
			if _, id, ok := s.aktuelleSitzung(r); ok {
				s.sessions.Delete(id)
			}
		}
		log.Printf("Jellyfin hat abgelehnt: %v", jfErr)
		writeError(w, jfErr.Status, jfErr.Message)
		return
	}
	log.Printf("Jellyfin nicht erreichbar: %v", err)
	writeError(w, http.StatusBadGateway, "Jellyfin ist gerade nicht erreichbar.")
}
```

- [ ] **Step 4: Tests laufen lassen, Erfolg bestätigen**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Committen**

```bash
git add internal/server/
git commit -m "$(printf '%s\n' \
  'feat: Zugriff speichern mit einem Request' \
  '' \
  'Bibliotheks-IDs werden gegen die bekannten Bibliotheken geprüft, bevor' \
  'geschrieben wird. Die Antwort enthält den bestätigten Stand, damit die' \
  'Oberfläche bei einer Ablehnung sichtbar zurückspringt, statt eine' \
  'Änderung vorzutäuschen.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 12: Frontend — Änderungen sammeln und speichern

**Files:**
- Modify: `web/static/app.js`

**Interfaces:**
- Consumes: DOM-Ids aus `user.html` (`user-form`, `save-btn`, `save-hint`, `all-folders`, `libraries`, `enabled-on`, `enabled-off`), Route `PUT /api/users/{id}/access`, Funktion `toast` aus Task 7
- Produces: nichts für andere Aufgaben

Diese Aufgabe hat keinen Go-Test; geprüft wird sie im Rauchtest (Task 16).
Die Oberfläche bleibt ohne JavaScript lesbar — nur Speichern geht dann nicht.

- [ ] **Step 1: `web/static/app.js` um den Seitenteil ergänzen**

Unter die vorhandene `toast`-Funktion setzen:

```javascript
// Sammelt die Änderungen auf der Benutzerseite und schickt sie mit einem
// Klick — statt bei jedem Häkchen einen Request abzusetzen.
(function () {
  const form = document.getElementById("user-form");
  if (!form) return;

  const saveBtn = document.getElementById("save-btn");
  const hint = document.getElementById("save-hint");
  const allBox = document.getElementById("all-folders");
  const enabledOn = document.getElementById("enabled-on");
  const enabledOff = document.getElementById("enabled-off");
  const folderBoxes = () =>
    Array.from(document.querySelectorAll("#libraries input[type=checkbox]"));

  function lesen() {
    return {
      disabled: enabledOn ? !enabledOn.checked : false,
      allFolders: allBox.checked,
      folders: folderBoxes()
        .filter((b) => b.checked)
        .map((b) => b.value)
        .sort(),
    };
  }

  // Der Stand, den der Server zuletzt bestätigt hat.
  let bestaetigt = lesen();

  function unterschiede(a, b) {
    let n = 0;
    if (a.disabled !== b.disabled) n++;
    if (a.allFolders !== b.allFolders) n++;
    const mengeA = new Set(a.folders);
    const mengeB = new Set(b.folders);
    for (const id of mengeA) if (!mengeB.has(id)) n++;
    for (const id of mengeB) if (!mengeA.has(id)) n++;
    return n;
  }

  function anwenden(stand) {
    if (enabledOn) enabledOn.checked = !stand.disabled;
    if (enabledOff) enabledOff.checked = stand.disabled;
    allBox.checked = stand.allFolders;
    const gesetzt = new Set(stand.folders);
    folderBoxes().forEach((b) => {
      b.checked = gesetzt.has(b.value);
    });
  }

  function aktualisieren() {
    // Die Einzelauswahl bleibt sichtbar, ist aber wirkungslos, solange
    // "Alle Bibliotheken" gesetzt ist.
    folderBoxes().forEach((b) => {
      b.disabled = allBox.checked;
    });
    const anzahl = unterschiede(bestaetigt, lesen());
    saveBtn.disabled = anzahl === 0;
    saveBtn.textContent = anzahl === 0 ? "Speichern" : `Speichern (${anzahl})`;
    hint.textContent =
      anzahl === 0
        ? "Keine Änderungen"
        : anzahl === 1
          ? "1 Änderung"
          : `${anzahl} Änderungen`;
  }

  form.addEventListener("change", aktualisieren);

  saveBtn.addEventListener("click", async () => {
    saveBtn.disabled = true;
    try {
      const res = await fetch(`/api/users/${form.dataset.userId}/access`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(lesen()),
      });
      if (res.status === 401) {
        window.location.href = "/login";
        return;
      }
      const daten = await res.json();
      if (!res.ok) {
        // Zurück auf den Serverzustand, statt eine Änderung vorzutäuschen.
        anwenden(bestaetigt);
        aktualisieren();
        toast(daten.error || "Speichern fehlgeschlagen.", "err");
        return;
      }
      bestaetigt = {
        disabled: daten.disabled,
        allFolders: daten.allFolders,
        folders: (daten.folders || []).slice().sort(),
      };
      anwenden(bestaetigt);
      aktualisieren();
      toast("Gespeichert.", "ok");
    } catch (err) {
      anwenden(bestaetigt);
      aktualisieren();
      toast("Der Dienst ist nicht erreichbar.", "err");
    }
  });

  aktualisieren();
})();
```

- [ ] **Step 2: CSS neu erzeugen**

`app.js` wird von Tailwind nach Klassennamen durchsucht (`@source`), deshalb nach jeder Änderung:

Run: `make css`
Expected: `web/static/app.css` wird neu geschrieben.

- [ ] **Step 3: Prüfen, dass nichts kaputt ist**

Run: `make check`
Expected: PASS.

- [ ] **Step 4: Committen**

```bash
git add web/static/
git commit -m "$(printf '%s\n' \
  'feat: Änderungen sammeln und mit einem Klick speichern' \
  '' \
  'Die Seite hält den zuletzt vom Server bestätigten Stand und zählt die' \
  'Abweichungen. Nach einer Ablehnung wird dieser Stand wieder' \
  'hergestellt, damit die Oberfläche nichts anzeigt, was nicht gilt.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 13: Einstiegspunkt

**Files:**
- Create: `cmd/server/main.go`
- Test: `cmd/server/main_test.go`

**Interfaces:**
- Consumes: `config.FromEnv`, `jellyfin.New`, `session.NewStore`, `session.(*Store).StartCleanup`, `server.New`
- Produces: Binary `./cmd/server`, Flag `-healthcheck`, Variable `version` (über `-ldflags -X main.version=…` setzbar)

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`cmd/server/main_test.go`:

```go
package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPruefeMeldetErfolg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("Port nicht ermittelbar: %v", err)
	}
	if code := pruefe(port); code != 0 {
		t.Errorf("Rückgabe = %d, erwartet 0", code)
	}
}

func TestPruefeMeldetFehlerWennNiemandAntwortet(t *testing.T) {
	// Port 1 ist im Container garantiert unbelegt.
	if code := pruefe("1"); code == 0 {
		t.Error("ohne laufenden Dienst muss der Healthcheck fehlschlagen")
	}
}

func TestPruefeOhnePortNutztVorgabe(t *testing.T) {
	// Kein laufender Dienst auf 8080 im Test — es geht nur darum, dass die
	// leere Angabe nicht in Panik gerät.
	_ = pruefe("")
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./cmd/server/...`
Expected: FAIL — `undefined: pruefe`.

- [ ] **Step 3: `cmd/server/main.go` schreiben**

```go
// Kommando jellyfin-enabler: Weboberfläche, um Jellyfin-Benutzer zu
// aktivieren bzw. zu deaktivieren und ihnen Bibliotheken zuzuordnen.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jellyfinenabler/internal/config"
	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/server"
	"jellyfinenabler/internal/session"
)

// version wird beim Build gesetzt: -ldflags "-X main.version=v1".
var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "nur prüfen, ob der laufende Dienst antwortet, und beenden")
	flag.Parse()
	if *healthcheck {
		os.Exit(pruefe(os.Getenv("PORT")))
	}

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("Konfiguration unvollständig: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessions := session.NewStore(cfg.SessionTTL)
	sessions.StartCleanup(ctx, 10*time.Minute)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(jellyfin.New(cfg.JellyfinURL, cfg.HTTPTimeout, version), sessions, cfg.CookieSecure),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		abschluss, abbrechen := context.WithTimeout(context.Background(), 10*time.Second)
		defer abbrechen()
		if err := srv.Shutdown(abschluss); err != nil {
			log.Printf("Herunterfahren: %v", err)
		}
	}()

	log.Printf("Jellyfin Enabler %s lauscht auf :%d, Jellyfin unter %s", version, cfg.Port, cfg.JellyfinURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server beendet: %v", err)
	}
}

// pruefe ist der Docker-Healthcheck: ein GET auf /healthz des eigenen
// Prozesses. Rückgabe 0 heißt gesund.
func pruefe(port string) int {
	if port == "" {
		port = "8080"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Healthcheck fehlgeschlagen:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "Healthcheck: HTTP", resp.StatusCode)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Tests und Build laufen lassen**

Run: `go test ./... && make build`
Expected: PASS, `bin/app` entsteht.

- [ ] **Step 5: Committen**

```bash
git add cmd/server/
git commit -m "$(printf '%s\n' \
  'feat: Einstiegspunkt mit Healthcheck und sauberem Herunterfahren' \
  '' \
  'Das Flag -healthcheck fragt den eigenen Prozess ab, damit der' \
  'Docker-Healthcheck ohne curl im Image auskommt — das distroless-Image' \
  'hat keine Shell und keine Werkzeuge.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 14: PWA-Icons

**Files:**
- Create: `cmd/genicons/main.go` (aus dem Nachbarprojekt portiert)
- Generated: `web/static/icons/icon-192.png`, `icon-512.png`, `apple-touch-icon.png`

**Interfaces:**
- Consumes: nichts
- Produces: die drei PNG-Dateien, auf die `manifest.webmanifest` und `layout.html` verweisen

- [ ] **Step 1: Generator übernehmen**

```bash
mkdir -p cmd/genicons
cp /Users/sh/projects/yt-dl-web-client/cmd/genicons/main.go cmd/genicons/main.go
```

- [ ] **Step 2: Glyphe austauschen**

In `cmd/genicons/main.go` den Paketkommentar anpassen (Download-Pfeil → Kippschalter) und die Funktion `glyphDist` **vollständig** ersetzen durch:

```go
// glyphDist: Kippschalter — eine abgerundete Kapsel, aus der der Knauf
// rechts ausgestanzt ist. Bleibt innerhalb der maskable-Safe-Zone (innere
// 80 %), damit iOS und Android nichts abschneiden.
func glyphDist(x, y float64) float64 {
	kapsel := sdRoundRect(x, y, 0.5, 0.5, 0.26, 0.145, 0.145)
	knauf := math.Hypot(x-0.605, y-0.5) - 0.088
	// max(a, -b) schneidet b aus a heraus.
	return math.Max(kapsel, -knauf)
}
```

Die Hilfsfunktion `sdTriangle` wird damit nicht mehr gebraucht — löschen, sonst meckert `go vet` bzw. der Compiler über ungenutzten Code nicht, wohl aber der Linter über toten Code. Prüfen, ob `math` weiterhin importiert ist.

- [ ] **Step 3: Icons erzeugen**

Run: `go run ./cmd/genicons web/static/icons`
Expected: drei PNG-Dateien in `web/static/icons/`.

- [ ] **Step 4: Prüfen**

Run: `go test ./... && ls -la web/static/icons/`
Expected: PASS, drei Dateien vorhanden.

- [ ] **Step 5: Committen**

```bash
git add cmd/genicons/ web/static/icons/
git commit -m "$(printf '%s\n' \
  'feat: PWA-Icons mit Kippschalter-Glyphe' \
  '' \
  'Generator aus yt-dl-web-client übernommen, nur die Glyphe ist neu.' \
  'Vollflächig ohne Alpha, damit der iOS-Home-Screen keine schwarzen' \
  'Ecken zeigt.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 15: Docker und README

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.dockerignore`
- Create: `README.md`

**Interfaces:**
- Consumes: `make css`-Pipeline, `cmd/server`, Flag `-healthcheck`
- Produces: Image `jellyfin-enabler`, Dienst auf Host-Port 8081

- [ ] **Step 1: `Dockerfile` schreiben**

```dockerfile
# Stufe 1: Tailwind-CSS — erzeugt app.css frisch im Image, damit ein
# veralteter committeter Stand nie ins Binary gelangt.
# Muss mit TAILWIND_VERSION im Makefile übereinstimmen.
FROM node:22-alpine AS css
ARG TAILWIND_VERSION=4.3.3
# Symlink: der v4-Resolver sucht das Paket "tailwindcss" vom Verzeichnis der
# Input-Datei aufwärts — /node_modules liegt auf diesem Pfad.
RUN npm install -g @tailwindcss/cli@${TAILWIND_VERSION} \
 && ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules
WORKDIR /src
COPY web/ web/
RUN tailwindcss -i web/src/input.css -o /out/app.css --minify

# Stufe 2: Go-Build
FROM golang:1.24 AS build
WORKDIR /src
COPY . .
COPY --from=css /out/app.css web/static/app.css
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /app ./cmd/server

# Stufe 3: Laufzeit. distroless/static bringt CA-Zertifikate mit — nötig,
# wenn Jellyfin über HTTPS erreicht wird — und läuft als nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /usr/local/bin/app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app"]
# Exec-Form ohne Shell: das Image hat keine.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["/usr/local/bin/app", "-healthcheck"]
```

- [ ] **Step 2: `docker-compose.yml` schreiben**

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
      - "8081:8080"
    environment:
      JELLYFIN_URL: "http://jellyfin:8096"
      # Hinter einem TLS-terminierenden Reverse Proxy nötig, sonst kommt das
      # Sitzungscookie ohne Secure-Flag:
      # COOKIE_SECURE: "true"
      # SESSION_TTL: "12h"
    restart: unless-stopped
    # Läuft Jellyfin in einem eigenen Compose-Projekt, dessen Netz hier
    # eintragen, damit der Hostname "jellyfin" auflösbar ist:
    # networks:
    #   - jellyfin_default
# networks:
#   jellyfin_default:
#     external: true
```

- [ ] **Step 3: `.dockerignore` schreiben**

```
.git
.remember
.superpowers
bin
docs
.dockerignore
docker-compose.yml
```

- [ ] **Step 4: `README.md` schreiben**

```markdown
# Jellyfin Enabler

Kleine Weboberfläche, um Jellyfin-Benutzer zu aktivieren bzw. zu deaktivieren
und ihnen Bibliotheken zuzuordnen. Angemeldet wird sich mit einem
Jellyfin-Administrator; der Dienst speichert selbst keine Zugangsdaten.

Geprüft gegen **Jellyfin 10.11.0**.

## Betrieb

```bash
docker compose up -d --build
```

Danach erreichbar unter <http://localhost:8081>.

## Konfiguration

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `JELLYFIN_URL` | — (Pflicht) | Basis-URL, z. B. `http://jellyfin:8096` |
| `PORT` | `8080` | Port im Container |
| `SESSION_TTL` | `12h` | Lebensdauer einer Anmeldung |
| `COOKIE_SECURE` | `auto` | hinter TLS-Proxy auf `true` setzen |
| `HTTP_TIMEOUT` | `15s` | Zeitlimit für Jellyfin-Aufrufe |

## Entwicklung

```bash
make check      # gofmt, go vet, Tests
make css        # Tailwind im Container übersetzen
make run        # lokal starten, JELLYFIN_URL=… überschreibbar
make start      # Container bauen, starten und die UI öffnen
```

Design, Makefile und Docker-Aufbau folgen dem Schwesterprojekt
`yt-dl-web-client`.

## Was der Dienst bewusst nicht kann

Benutzer anlegen oder löschen, Passwörter ändern, Sitzungen beenden. Dafür
gibt es das Jellyfin-Dashboard.

## Sicherheitshinweis

Wer die Oberfläche erreicht und ein Admin-Passwort hat, kann Zugänge
abschalten. Der Dienst gehört ins Heimnetz, hinter Reverse Proxy und TLS —
nicht ungeschützt ins Internet.
```

- [ ] **Step 5: Bauen und starten**

Run: `make check && make image-native`
Expected: Image `jellyfin-enabler-native` entsteht.

Run: `docker compose up -d && sleep 3 && curl -fsS http://localhost:8081/healthz && docker compose down`
Expected: `ok`.

- [ ] **Step 6: Committen**

```bash
git add Dockerfile docker-compose.yml .dockerignore README.md
git commit -m "$(printf '%s\n' \
  'feat: Docker-Build, Compose-Setup und README' \
  '' \
  'Dreistufiger Build: Tailwind, Go, distroless. Das Laufzeit-Image hat' \
  'keine Shell, deshalb prüft der Healthcheck über das eigene Binary.' \
  'Host-Port 8081, damit der Dienst neben yt-dl-web laufen kann.' \
  '' \
  'Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>')"
```

---

### Task 16: Rauchtest gegen das echte Jellyfin

**Diese Aufgabe kann kein Subagent allein erledigen** — sie braucht die
laufende Jellyfin-Instanz und ein Admin-Konto. Sie wird gemeinsam mit dem
Menschen durchgeführt und ist die Bedingung dafür, das Projekt als fertig zu
bezeichnen.

**Files:** keine

- [ ] **Step 1: Dienst gegen den echten Server starten**

```bash
make start JELLYFIN_URL=<die echte URL>
```

bzw. `JELLYFIN_URL` in `docker-compose.yml` eintragen und `make start`.

- [ ] **Step 2: Anmelden**

Mit dem Jellyfin-Admin-Konto anmelden. Erwartet: die Benutzerliste erscheint.
Gegenprobe mit einem Nicht-Admin-Konto: Abweisung mit „Nur Administratoren
dürfen diesen Dienst verwenden."

- [ ] **Step 3: Zustand vorher festhalten**

Im Jellyfin-Dashboard die Policy eines Testbenutzers ansehen und notieren:
Transcoding-Rechte, Zugriffszeitfenster, Altersfreigabe, Session-Limit.

- [ ] **Step 4: Deaktivieren und wieder aktivieren**

Testbenutzer auf „Aus" stellen, speichern. Erwartet: Toast „Gespeichert.",
laufende Sitzungen des Benutzers sind beendet. Danach wieder auf „Aktiv".

- [ ] **Step 5: Bibliothek entziehen und zurückgeben**

Eine Bibliothek abwählen, speichern, im Jellyfin-Client des Benutzers prüfen,
dass sie verschwunden ist. Danach zurückgeben.

- [ ] **Step 6: Die entscheidende Gegenprobe**

Im Jellyfin-Dashboard erneut die Policy desselben Benutzers ansehen: **alle in
Schritt 3 notierten Werte müssen unverändert sein.** Sind sie es nicht, ist
der Policy-Passthrough kaputt und Task 5 muss nachgebessert werden.

- [ ] **Step 7: Administrator gegenprüfen**

Die Detailseite eines Administrators öffnen. Erwartet: der Aktiv-Schalter ist
ausgegraut, mit Hinweis.

- [ ] **Step 8: Ergebnis festhalten**

Läuft alles, in `docs/superpowers/plans/` nichts weiter ändern, sondern das
Ergebnis im Abschlussbericht nennen. Bei Abweichungen: systematic-debugging.

---

## Selbstprüfung des Plans

Nach dem Schreiben gegen die Spec geprüft:

- **Spec-Abdeckung:** Verifizierte Grundlagen → Task 2–5. Policy-Passthrough →
  Task 5. `EnableAllFolders`-Semantik → Task 5, 9, 10, 12.
  Authentifizierung/Sitzung → Task 3, 6, 8. HTTP-Oberfläche → Task 8–11.
  Frontend → Task 7, 10, 12. Build/Deployment → Task 7, 13, 15.
  Fehlerbehandlung → Task 2, 9, 11. Verifikation → alle Tasks plus Task 16.
  Keine Lücke gefunden.
- **Abweichung:** An- und Abmeldung laufen über `POST /login` / `POST /logout`
  statt `/api/…`, damit sie ohne JavaScript funktionieren. Die Spec ist
  nachgezogen.
- **Typkonsistenz:** `Access`, `User`, `Library`, `Session` werden in Task 2
  definiert und in Task 3–11 unverändert verwendet. `Jellyfin`-Schnittstelle
  aus Task 8 deckt sich mit den Methoden aus Task 3–5. Die JSON-Felder
  `disabled`/`allFolders`/`folders` sind in Task 11 und Task 12 identisch.

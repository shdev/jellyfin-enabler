// Package config liest die Laufzeitkonfiguration aus Umgebungsvariablen
// (Spec §Build und Deployment).
package config

import (
	"errors"
	"log"
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
	// APIKey ist der Jellyfin-API-Key für den Semiadmin-Pfad. Leer heißt:
	// Semiadmin-Funktion aus (Spec §Konfiguration und Deployment). Er ist ein
	// vollwertiger Admin-Zugang und darf deshalb nie geloggt werden.
	APIKey string
	// SettingsFile ist der Pfad der JSON-Datei mit den Semiadmin-Zuordnungen.
	SettingsFile string
}

// DefaultPort ist der Port, auf dem der Dienst ohne PORT lauscht. Exportiert,
// damit der Healthcheck in cmd/server denselben Wert verwendet.
const DefaultPort = 8080

// PortFromEnv löst PORT nach derselben Regel auf wie FromEnv: alles, was
// keine positive Zahl ist, fällt auf DefaultPort zurück. Exportiert, damit
// der Healthcheck in cmd/server denselben Port trifft, auf dem der Dienst
// tatsächlich lauscht — sonst gilt der Container bei einem unsinnigen PORT
// dauerhaft als ungesund, obwohl er läuft.
func PortFromEnv(getenv func(string) string) int {
	if n, err := strconv.Atoi(getenv("PORT")); err == nil && n > 0 {
		return n
	}
	return DefaultPort
}

// ErrNoJellyfinURL meldet die fehlende Pflichtvariable. Der Dienst startet
// ohne sie nicht, statt später bei jedem Aufruf ins Leere zu laufen.
var ErrNoJellyfinURL = errors.New("JELLYFIN_URL fehlt: Basis-URL des Jellyfin-Servers, z. B. http://jellyfin:8096")

func FromEnv(getenv func(string) string) (Config, error) {
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
	cfg.Port = PortFromEnv(getenv)
	if d, err := time.ParseDuration(getenv("SESSION_TTL")); err == nil && d > 0 {
		cfg.SessionTTL = d
	}
	if d, err := time.ParseDuration(getenv("HTTP_TIMEOUT")); err == nil && d > 0 {
		cfg.HTTPTimeout = d
	}
	roh := getenv("COOKIE_SECURE")
	switch strings.ToLower(roh) {
	case "true", "1", "yes":
		cfg.CookieSecure = "true"
	case "false", "0", "no":
		cfg.CookieSecure = "false"
	case "":
		// Leer ist die Vorgabe und bleibt stillschweigend "auto".
	default:
		// Ein Tippfehler wie "ture" darf das Secure-Flag nicht lautlos
		// abschalten — hinter einem TLS-Proxy ginge das Sitzungscookie
		// sonst im Klartext über jeden Hop.
		log.Printf("COOKIE_SECURE=%q wird nicht verstanden, es gilt die Vorgabe \"auto\".", roh)
	}
	return cfg, nil
}

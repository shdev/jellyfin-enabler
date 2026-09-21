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

// DefaultPort ist der Port, auf dem der Dienst ohne PORT lauscht. Exportiert,
// damit der Healthcheck in cmd/server denselben Wert verwendet.
const DefaultPort = 8080

// ErrNoJellyfinURL meldet die fehlende Pflichtvariable. Der Dienst startet
// ohne sie nicht, statt später bei jedem Aufruf ins Leere zu laufen.
var ErrNoJellyfinURL = errors.New("JELLYFIN_URL fehlt: Basis-URL des Jellyfin-Servers, z. B. http://jellyfin:8096")

func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:         DefaultPort,
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

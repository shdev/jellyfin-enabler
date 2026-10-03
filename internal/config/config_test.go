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

// TestFromEnvUnbekannterCookieSecureBleibtBeiAuto deckt einen Tippfehler wie
// COOKIE_SECURE="ture" ab: der darf nicht lautlos irgendeinen anderen Wert
// ergeben, sondern muss bei der Vorgabe "auto" bleiben (die Warnung selbst
// wird hier nicht geprüft).
func TestFromEnvUnbekannterCookieSecureBleibtBeiAuto(t *testing.T) {
	cfg, err := config.FromEnv(env(map[string]string{
		"JELLYFIN_URL":  "http://jellyfin:8096",
		"COOKIE_SECURE": "ture",
	}))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.CookieSecure != "auto" {
		t.Errorf("CookieSecure = %q, erwartet auto", cfg.CookieSecure)
	}
}

// Die Regel muss dieselbe sein wie in FromEnv — sonst lauscht der Dienst auf
// der Vorgabe, während der Healthcheck einen unsinnigen Port abfragt.
func TestPortFromEnvFaelltBeiUnsinnAufDieVorgabe(t *testing.T) {
	for _, roh := range []string{"", "abc", "0", "-1", " 8080", "8080x"} {
		if got := config.PortFromEnv(func(string) string { return roh }); got != config.DefaultPort {
			t.Errorf("PortFromEnv(%q) = %d, erwartet %d", roh, got, config.DefaultPort)
		}
	}
	if got := config.PortFromEnv(func(string) string { return "9000" }); got != 9000 {
		t.Errorf("PortFromEnv(\"9000\") = %d, erwartet 9000", got)
	}
}

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

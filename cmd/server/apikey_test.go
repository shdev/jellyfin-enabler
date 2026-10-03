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

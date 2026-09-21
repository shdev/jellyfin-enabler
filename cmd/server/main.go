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
		// Denselben Weg wie der Dienst gehen, damit beide auf demselben
		// Port landen — auch wenn PORT unsinnig gesetzt ist.
		os.Exit(pruefe(config.PortFromEnv(os.Getenv)))
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

	// Shutdown schließt zuerst die Listener, erst danach wartet es auf die
	// laufenden Requests. ListenAndServe kehrt deshalb sofort zurück — ohne
	// dieses Signal liefe main aus, während noch bedient wird.
	fertig := make(chan struct{})
	go func() {
		<-ctx.Done()
		abschluss, abbrechen := context.WithTimeout(context.Background(), 10*time.Second)
		defer abbrechen()
		if err := srv.Shutdown(abschluss); err != nil {
			log.Printf("Herunterfahren: %v", err)
		}
		close(fertig)
	}()

	log.Printf("Jellyfin Enabler %s lauscht auf :%d, Jellyfin unter %s", version, cfg.Port, cfg.JellyfinURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server beendet: %v", err)
	}
	<-fertig
}

// healthURL baut die Adresse des eigenen /healthz.
func healthURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
}

// pruefe ist der Docker-Healthcheck: ein GET auf /healthz des eigenen
// Prozesses. Rückgabe 0 heißt gesund.
func pruefe(port int) int {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(healthURL(port))
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

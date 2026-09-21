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

func TestHealthURLNutztOhnePortDieVorgabe(t *testing.T) {
	if got := healthURL(""); got != "http://127.0.0.1:8080/healthz" {
		t.Errorf("healthURL(\"\") = %q", got)
	}
	if got := healthURL("9000"); got != "http://127.0.0.1:9000/healthz" {
		t.Errorf("healthURL(\"9000\") = %q", got)
	}
}

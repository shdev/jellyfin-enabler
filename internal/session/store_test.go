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
	a, err := s.Create("tok", "u-1", "papa", true)
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	b, err := s.Create("tok", "u-1", "papa", true)
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
	id, _ := s.Create("tok-123", "u-1", "papa", true)

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
	id, _ := s.Create("tok", "u-1", "papa", true)

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
	id, _ := s.Create("tok", "u-1", "papa", true)
	s.Delete(id)
	if _, ok := s.Get(id); ok {
		t.Error("gelöschte Sitzung darf nicht mehr gefunden werden")
	}
	s.Delete("gibt-es-nicht") // darf nicht in Panik geraten
}

func TestCleanupGibtSpeicherFrei(t *testing.T) {
	jetzt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	s := bei(time.Hour, &jetzt)
	alt, _ := s.Create("tok", "u-1", "papa", true)
	jetzt = jetzt.Add(2 * time.Hour)
	neu, _ := s.Create("tok", "u-2", "mama", true)

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

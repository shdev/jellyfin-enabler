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

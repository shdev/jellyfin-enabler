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

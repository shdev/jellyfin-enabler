// Package server stellt die Oberfläche und die Speicher-API bereit
// (Spec §HTTP-Oberfläche).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	mux.HandleFunc("GET /{$}", s.seite(s.handleIndex))
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(s.handleUpdateAccess))
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

// zugriffText beschreibt die Bibliotheksfreigabe in einem Satz. Gezählt werden
// ausschließlich Freigaben auf Bibliotheken, die es noch gibt: in der Policy
// können IDs gelöschter Bibliotheken zurückbleiben, und die dürfen nicht als
// Zugriff erscheinen. "Alle Bibliotheken" und "alle angehakt" sind bewusst
// verschiedene Aussagen — nur Ersteres schließt künftig neue ein
// (Spec §EnableAllFolders-Semantik).
func zugriffText(u jellyfin.User, libs []jellyfin.Library) string {
	if u.Access.AllFolders {
		return "Alle Bibliotheken"
	}
	vorhanden := make(map[string]bool, len(libs))
	for _, l := range libs {
		vorhanden[l.ID] = true
	}
	n := 0
	for _, f := range u.Access.Folders {
		if vorhanden[f] {
			n++
		}
	}
	gesamt := len(libs)
	switch {
	case n == 0:
		return "Keine Bibliothek"
	case gesamt == 1:
		return "Die einzige Bibliothek (ohne künftige)"
	case n >= gesamt:
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
			Access:   zugriffText(u, libs),
		})
	}
	if err := s.tmpl.ExecuteTemplate(w, "users", daten); err != nil {
		log.Printf("Benutzerliste nicht darstellbar: %v", err)
	}
}

// handleUpdateAccess ist der Platzhalter für das Speichern der Zugriffsrechte
// (Task 11).
func (s *Server) handleUpdateAccess(w http.ResponseWriter, _ *http.Request, _ session.Session) {
	writeError(w, http.StatusNotImplemented, "kommt in Task 11")
}

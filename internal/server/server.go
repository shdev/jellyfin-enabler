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
	"sort"

	"jellyfinenabler/internal/jellyfin"
	"jellyfinenabler/internal/session"
	"jellyfinenabler/internal/settings"
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
	SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error)
}

type Server struct {
	jf           Jellyfin
	sessions     *session.Store
	cookieSecure string
	tmpl         *template.Template
	// semiadmins hält die Zuordnung Semiadmin → schaltbare User.
	semiadmins *settings.Store
	// apiKey ist der Jellyfin-API-Key; leer heißt Semiadmin-Funktion aus.
	// Er wird ausschließlich im Semiadmin-Pfad benutzt und nie ausgegeben.
	apiKey string
}

// New baut den Handler. semiadmins und apiKey stehen hinten, damit die
// bisherigen Argumente ihre Reihenfolge behalten.
func New(jf Jellyfin, sessions *session.Store, cookieSecure string, semiadmins *settings.Store, apiKey string) http.Handler {
	s := &Server{
		jf:           jf,
		sessions:     sessions,
		cookieSecure: cookieSecure,
		tmpl:         template.Must(template.ParseFS(web.FS, "templates/*.html")),
		semiadmins:   semiadmins,
		apiKey:       apiKey,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /static/", http.FileServerFS(web.FS))
	mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /{$}", s.seite(s.handleStart))
	mux.HandleFunc("GET /users/{id}", s.seite(nurAdmin(s.handleUserPage, zurStartseite)))
	mux.HandleFunc("PUT /api/users/{id}/access", s.api(nurAdmin(s.handleUpdateAccess, keinAdmin)))
	mux.HandleFunc("PUT /api/users/{id}/delegation", s.api(nurAdmin(s.handleDelegation, keinAdmin)))
	mux.HandleFunc("PUT /api/controlled/{id}/active", s.api(s.handleControlledActive))
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

// sitzungsFn ist ein Handler, dessen Sitzung bereits geprüft ist.
type sitzungsFn = func(http.ResponseWriter, *http.Request, session.Session)

// nurAdmin lässt nur Admin-Sitzungen durch. Bisher erzwang Jellyfin die
// Adminrechte über das Token; ein Semiadmin hat aber ein gültiges Token,
// also muss der Dienst das selbst tun (Spec §Anmeldung, Sitzung und Rollen).
// abgelehnt antwortet passend zur Route.
func nurAdmin(fn sitzungsFn, abgelehnt http.HandlerFunc) sitzungsFn {
	return func(w http.ResponseWriter, r *http.Request, sess session.Session) {
		if !sess.Admin {
			abgelehnt(w, r)
			return
		}
		fn(w, r, sess)
	}
}

// zurStartseite ist die Ablehnung auf Seiten: dort wartet die eigene Ansicht.
func zurStartseite(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// keinAdmin ist die Ablehnung in der API.
func keinAdmin(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusForbidden, "Diese Aktion ist Administratoren vorbehalten.")
}

// semiadminEingetragen ist die Rollenprüfung beim Login: Key gesetzt und
// Eintrag in der Settings-Datei. Ob das Konto in Jellyfin noch aktiv ist,
// prüft die Semiadmin-Prüfung bei jeder weiteren Anfrage.
func (s *Server) semiadminEingetragen(userID string) bool {
	if s.apiKey == "" {
		return false
	}
	_, ok := s.semiadmins.Targets(userID)
	return ok
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
	if err != nil {
		// Benutzer- und Passwortfehler werden bewusst nicht unterschieden.
		log.Printf("Anmeldung fehlgeschlagen: %v", err)
		s.renderLogin(w, http.StatusUnauthorized, "Anmeldung fehlgeschlagen.")
		return
	}
	// Drei Fälle (Spec §Anmeldung, Sitzung und Rollen): Jellyfin-Admin,
	// eingetragener Semiadmin bei gesetztem Key, sonst kein Zugang.
	if !sess.Admin && !s.semiadminEingetragen(sess.UserID) {
		s.abweisen(w, r, sess.Token)
		return
	}
	id, err := s.sessions.Create(sess.Token, sess.UserID, sess.Name, sess.Admin)
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

// abweisen entwertet das gerade erhaltene Token eines Kontos ohne Rolle und
// zeigt die Ablehnung. Scheitert die Abmeldung, bleibt ein gültiges Token
// zurück, ohne dass es sonst irgendwo auffiele — deshalb wird ein
// Fehlschlag geloggt statt verworfen.
func (s *Server) abweisen(w http.ResponseWriter, r *http.Request, token string) {
	if err := s.jf.Logout(r.Context(), token); err != nil {
		log.Printf("Token eines abgelehnten Anmeldeversuchs konnte nicht entwertet werden: %v", err)
	}
	s.renderLogin(w, http.StatusForbidden, "Dieses Konto hat keinen Zugriff auf diesen Dienst.")
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
	// Semiadmin zeigt die Pille "Semiadmin".
	Semiadmin bool
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
	var jfErr *jellyfin.Error
	if errors.As(err, &jfErr) {
		log.Printf("Jellyfin hat abgelehnt: %v", jfErr)
		http.Error(w, jfErr.Message, jfErr.Status)
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
	// Die Pille erscheint nur, wenn die Rolle auch wirkt: ohne API-Key
	// bleibt ein Eintrag in der Settings-Datei wirkungslos.
	var eingetragen map[string][]string
	if s.apiKey != "" {
		eingetragen = s.semiadmins.Semiadmins()
	}
	for _, u := range users {
		_, semiadmin := eingetragen[u.ID]
		daten.Users = append(daten.Users, userRow{
			ID:        u.ID,
			Name:      u.Name,
			Admin:     u.Admin,
			Disabled:  u.Access.Disabled,
			Access:    zugriffText(u, libs),
			Semiadmin: semiadmin && !u.Admin,
		})
	}
	if err := s.tmpl.ExecuteTemplate(w, "users", daten); err != nil {
		log.Printf("Benutzerliste nicht darstellbar: %v", err)
	}
}

// handleStart verteilt nach Rolle: Admins sehen die Benutzerliste,
// Semiadmins ihre eigene, schmale Ansicht (Spec §HTTP-Oberfläche).
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request, sess session.Session) {
	if sess.Admin {
		s.handleIndex(w, r, sess)
		return
	}
	s.handleControlled(w, r, sess)
}

// controlledRow ist eine Karte der Semiadmin-Ansicht: Name und Schalter,
// keine Bibliotheken.
type controlledRow struct {
	ID       string
	Name     string
	Disabled bool
}

type controlledData struct {
	Title string
	Users []controlledRow
	// Fehler ersetzt die Benutzerliste, wenn der Key-Aufruf scheitert: Der
	// Semiadmin braucht das normale Layout samt Abmelden-Knopf, weil GET /login
	// bei bestehender Sitzung auf / zurückleitet.
	Fehler string
}

// errEntzogen meldet eine nicht bestandene Semiadmin-Prüfung. Die Sitzung
// ist dann bereits gelöscht.
var errEntzogen = errors.New("Semiadmin-Rolle entzogen")

// semiadminLage ist das Ergebnis einer bestandenen Semiadmin-Prüfung. Die
// Userliste ist per API-Key geladen; beide Semiadmin-Handler brauchen sie.
type semiadminLage struct {
	ich     jellyfin.User
	users   []jellyfin.User
	nachID  map[string]jellyfin.User
	targets map[string]bool
}

// semiadminPruefen ist die Semiadmin-Prüfung (Spec §Anmeldung, Sitzung und
// Rollen). Sie läuft bei jeder Semiadmin-Anfrage, damit ein inzwischen
// gesperrter oder entmachteter Semiadmin mit seiner alten Sitzung nicht
// weiter schalten kann. Nicht bestanden (errEntzogen) heißt: Key fehlt,
// Eintrag fehlt, oder die per Key erfolgreich geladene Liste kennt das
// Konto nicht oder nur deaktiviert. Scheitert der Key-Aufruf selbst, kommt
// dessen Fehler zurück und die Sitzung bleibt bestehen.
func (s *Server) semiadminPruefen(r *http.Request, sess session.Session) (semiadminLage, error) {
	if s.apiKey == "" {
		return semiadminLage{}, s.entziehen(r, sess)
	}
	targets, ok := s.semiadmins.Targets(sess.UserID)
	if !ok {
		return semiadminLage{}, s.entziehen(r, sess)
	}
	users, err := s.jf.Users(r.Context(), s.apiKey)
	if err != nil {
		return semiadminLage{}, err
	}
	lage := semiadminLage{
		users:   users,
		nachID:  make(map[string]jellyfin.User, len(users)),
		targets: make(map[string]bool, len(targets)),
	}
	for _, u := range users {
		lage.nachID[u.ID] = u
	}
	for _, id := range targets {
		lage.targets[id] = true
	}
	ich, ok := lage.nachID[sess.UserID]
	if !ok || ich.Access.Disabled {
		return semiadminLage{}, s.entziehen(r, sess)
	}
	lage.ich = ich
	return lage, nil
}

// entziehen löscht die Sitzung und entwertet das User-Token in Jellyfin.
// Scheitert die Abmeldung, wird das nur geloggt.
func (s *Server) entziehen(r *http.Request, sess session.Session) error {
	if _, id, ok := s.aktuelleSitzung(r); ok {
		s.sessions.Delete(id)
	}
	if err := s.jf.Logout(r.Context(), sess.Token); err != nil {
		log.Printf("Token eines entzogenen Semiadmins konnte nicht entwertet werden: %v", err)
	}
	log.Printf("Semiadmin-Sitzung beendet: %s (%s)", sess.Name, sess.UserID)
	return errEntzogen
}

// keyFehler übersetzt einen gescheiterten Aufruf mit dem API-Key in Status
// und Meldung. Anders als bei Aufrufen mit dem User-Token wird die Sitzung
// nie gelöscht: 401 oder 403 heißt hier, dass der Key ungültig oder
// widerrufen ist, nicht dass die Anmeldung des Semiadmins abgelaufen ist
// (Spec §Fehlerbehandlung).
func keyFehler(err error) (int, string) {
	var jfErr *jellyfin.Error
	if errors.As(err, &jfErr) {
		if jfErr.Status == http.StatusUnauthorized || jfErr.Status == http.StatusForbidden {
			log.Printf("Jellyfin lehnt JELLYFIN_API_KEY ab: %v", jfErr)
			return http.StatusBadGateway, "Interner Zugang nicht verfügbar. Bitte den Administrator informieren."
		}
		log.Printf("Jellyfin hat abgelehnt: %v", jfErr)
		return jfErr.Status, jfErr.Message
	}
	log.Printf("Jellyfin nicht erreichbar: %v", err)
	return http.StatusBadGateway, "Jellyfin ist gerade nicht erreichbar."
}

// handleControlled zeigt die Ansicht eines Semiadmins: je zugewiesenem
// Nicht-Admin eine Karte mit Aktiv/Aus-Schalter, keine Bibliotheken, keine
// Links auf Detailseiten. Verwaiste IDs, inzwischen zu Administratoren
// gewordene Ziele und das eigene Konto (aus einer alten oder von Hand
// bearbeiteten Datei) werden ausgefiltert.
func (s *Server) handleControlled(w http.ResponseWriter, r *http.Request, sess session.Session) {
	lage, err := s.semiadminPruefen(r, sess)
	if errors.Is(err, errEntzogen) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err != nil {
		code, meldung := keyFehler(err)
		w.WriteHeader(code)
		if err := s.tmpl.ExecuteTemplate(w, "controlled", controlledData{Title: "Jellyfin Zugriff", Fehler: meldung}); err != nil {
			log.Printf("Semiadmin-Ansicht nicht darstellbar: %v", err)
		}
		return
	}
	daten := controlledData{Title: "Jellyfin Zugriff"}
	for _, u := range lage.users {
		if !lage.targets[u.ID] || u.Admin || u.ID == sess.UserID {
			continue
		}
		daten.Users = append(daten.Users, controlledRow{ID: u.ID, Name: u.Name, Disabled: u.Access.Disabled})
	}
	if err := s.tmpl.ExecuteTemplate(w, "controlled", daten); err != nil {
		log.Printf("Semiadmin-Ansicht nicht darstellbar: %v", err)
	}
}

type controlledRequest struct {
	Disabled bool `json:"disabled"`
}

type controlledResponse struct {
	Disabled bool `json:"disabled"`
}

// handleControlledActive schaltet einen zugewiesenen User
// (Spec §PUT /api/controlled/{id}/active). Das ist der einzige schreibende
// Aufruf mit dem API-Key; jede Schaltung landet im Log, weil Jellyfins
// Aktivitätslog bei Key-Aufrufen keinen Benutzer nennt.
func (s *Server) handleControlledActive(w http.ResponseWriter, r *http.Request, sess session.Session) {
	if sess.Admin {
		writeError(w, http.StatusForbidden, "Diese Aktion ist Semiadmins vorbehalten.")
		return
	}
	lage, err := s.semiadminPruefen(r, sess)
	if errors.Is(err, errEntzogen) {
		writeError(w, http.StatusUnauthorized, "Die Anmeldung ist abgelaufen.")
		return
	}
	if err != nil {
		code, meldung := keyFehler(err)
		writeError(w, code, meldung)
		return
	}
	var req controlledRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Die Anfrage war nicht lesbar.")
		return
	}
	id := r.PathValue("id")
	// Das eigene Konto ist nie ein Ziel, auch wenn es in der Datei steht.
	if !lage.targets[id] || id == sess.UserID {
		writeError(w, http.StatusForbidden, "Dieser Benutzer ist dir nicht zugewiesen.")
		return
	}
	ziel, ok := lage.nachID[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Der Benutzer existiert nicht mehr.")
		return
	}
	if ziel.Admin {
		writeError(w, http.StatusForbidden, "Administratoren können nicht geschaltet werden.")
		return
	}
	neu, err := s.jf.SetDisabled(r.Context(), s.apiKey, id, req.Disabled)
	if err != nil {
		code, meldung := keyFehler(err)
		writeError(w, code, meldung)
		return
	}
	log.Printf("semiadmin schaltet: von=%s (%s) ziel=%s (%s) disabled=%t", lage.ich.Name, sess.UserID, ziel.Name, ziel.ID, neu)
	writeJSON(w, http.StatusOK, controlledResponse{Disabled: neu})
}

type libraryRow struct {
	ID      string
	Name    string
	Checked bool
}

type userData struct {
	Title     string
	User      jellyfin.User
	Libraries []libraryRow
	// SemiadminAn: der Abschnitt "Semiadmin" ist bedienbar, weil der
	// API-Key gesetzt ist. Bei Administratoren fehlt der Abschnitt ganz.
	SemiadminAn bool
	// Semiadmin: der Benutzer steht in der Settings-Datei.
	Semiadmin  bool
	Kandidaten []kandidatRow
}

// kandidatRow ist ein Chip unter "Darf schalten:".
type kandidatRow struct {
	ID      string
	Name    string
	Checked bool
}

// handleUserPage zeigt die Detailseite eines Benutzers. Ein nicht mehr
// vorhandener Benutzer ist kein Ausfall der Gegenstelle, sondern ein
// gewöhnliches 404 — z. B. wenn er zwischen Liste und Klick gelöscht wurde.
func (s *Server) handleUserPage(w http.ResponseWriter, r *http.Request, sess session.Session) {
	id := r.PathValue("id")
	u, err := s.jf.User(r.Context(), sess.Token, id)
	if err != nil {
		var jfErr *jellyfin.Error
		if errors.As(err, &jfErr) && jfErr.Status == http.StatusNotFound {
			http.Error(w, jfErr.Message, http.StatusNotFound)
			return
		}
		s.seitenFehler(w, r, err)
		return
	}
	libs, err := s.jf.Libraries(r.Context(), sess.Token)
	if err != nil {
		s.seitenFehler(w, r, err)
		return
	}
	freigegeben := make(map[string]bool, len(u.Access.Folders))
	for _, f := range u.Access.Folders {
		freigegeben[f] = true
	}
	daten := userData{Title: u.Name + " · Jellyfin Zugriff", User: u}
	for _, l := range libs {
		daten.Libraries = append(daten.Libraries, libraryRow{ID: l.ID, Name: l.Name, Checked: freigegeben[l.ID]})
	}
	if !u.Admin && s.apiKey != "" {
		daten.SemiadminAn = true
		if err := s.kandidatenFuellen(r, sess, u, &daten); err != nil {
			s.seitenFehler(w, r, err)
			return
		}
	}
	if err := s.tmpl.ExecuteTemplate(w, "user", daten); err != nil {
		log.Printf("Detailseite nicht darstellbar: %v", err)
	}
}

type accessRequest struct {
	Disabled   bool     `json:"disabled"`
	AllFolders bool     `json:"allFolders"`
	Folders    []string `json:"folders"`
}

type accessResponse struct {
	Disabled   bool     `json:"disabled"`
	AllFolders bool     `json:"allFolders"`
	Folders    []string `json:"folders"`
}

func (s *Server) handleUpdateAccess(w http.ResponseWriter, r *http.Request, sess session.Session) {
	var req accessRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Die Anfrage war nicht lesbar.")
		return
	}
	// Nur bekannte Bibliotheken zulassen — sonst landet Unsinn in der Policy.
	libs, err := s.jf.Libraries(r.Context(), sess.Token)
	if err != nil {
		s.apiFehler(w, r, err)
		return
	}
	bekannt := make(map[string]bool, len(libs))
	for _, l := range libs {
		bekannt[l.ID] = true
	}
	for _, f := range req.Folders {
		if !bekannt[f] {
			writeError(w, http.StatusBadRequest, "Unbekannte Bibliothek: "+f)
			return
		}
	}

	neu, err := s.jf.UpdateAccess(r.Context(), sess.Token, r.PathValue("id"), jellyfin.Access{
		Disabled:   req.Disabled,
		AllFolders: req.AllFolders,
		Folders:    req.Folders,
	})
	if err != nil {
		s.apiFehler(w, r, err)
		return
	}
	folders := neu.Folders
	if folders == nil {
		folders = []string{}
	}
	writeJSON(w, http.StatusOK, accessResponse{
		Disabled:   neu.Disabled,
		AllFolders: neu.AllFolders,
		Folders:    folders,
	})
}

// apiFehler übersetzt einen Fehler in eine JSON-Antwort. Die deutschen
// Meldungen kommen aus dem Client, damit die Oberfläche sie unverändert
// anzeigen kann (Spec §Fehlerbehandlung).
func (s *Server) apiFehler(w http.ResponseWriter, r *http.Request, err error) {
	var jfErr *jellyfin.Error
	if errors.As(err, &jfErr) {
		if jfErr.Unauthorized() {
			if _, id, ok := s.aktuelleSitzung(r); ok {
				s.sessions.Delete(id)
			}
		}
		log.Printf("Jellyfin hat abgelehnt: %v", jfErr)
		writeError(w, jfErr.Status, jfErr.Message)
		return
	}
	log.Printf("Jellyfin nicht erreichbar: %v", err)
	writeError(w, http.StatusBadGateway, "Jellyfin ist gerade nicht erreichbar.")
}

// kandidatenFuellen setzt den Semiadmin-Abschnitt der Detailseite: ob u
// eingetragen ist und welche Nicht-Admins als Ziel in Frage kommen.
// Verwaiste IDs aus der Settings-Datei erscheinen nicht und fallen beim
// nächsten Speichern dieser Zuordnung weg (Spec §Fehlerbehandlung).
func (s *Server) kandidatenFuellen(r *http.Request, sess session.Session, u jellyfin.User, daten *userData) error {
	users, err := s.jf.Users(r.Context(), sess.Token)
	if err != nil {
		return err
	}
	targets, eingetragen := s.semiadmins.Targets(u.ID)
	daten.Semiadmin = eingetragen
	gewaehlt := make(map[string]bool, len(targets))
	for _, id := range targets {
		gewaehlt[id] = true
	}
	for _, k := range users {
		if k.Admin || k.ID == u.ID {
			continue
		}
		daten.Kandidaten = append(daten.Kandidaten, kandidatRow{ID: k.ID, Name: k.Name, Checked: gewaehlt[k.ID]})
	}
	return nil
}

// sitzungBeenden löscht die Sitzung und antwortet auf eine Delegation eines
// nicht mehr als Administrator geführten Kontos mit 403.
func (s *Server) sitzungBeenden(w http.ResponseWriter, r *http.Request) {
	if _, id, ok := s.aktuelleSitzung(r); ok {
		s.sessions.Delete(id)
	}
	writeError(w, http.StatusForbidden, "Dieses Konto ist kein Administrator mehr.")
}

type delegationRequest struct {
	Semiadmin bool     `json:"semiadmin"`
	Users     []string `json:"users"`
}

type delegationResponse struct {
	Semiadmin bool     `json:"semiadmin"`
	Users     []string `json:"users"`
}

// handleDelegation speichert, ob {id} Semiadmin ist und wen er schalten
// darf (Spec §PUT /api/users/{id}/delegation). Die Ablehnungen folgen der
// Reihenfolge der Spec.
func (s *Server) handleDelegation(w http.ResponseWriter, r *http.Request, sess session.Session) {
	var req delegationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Die Anfrage war nicht lesbar.")
		return
	}
	users, err := s.jf.Users(r.Context(), sess.Token)
	// Verweigert Jellyfin die Liste mit dem Token des Aufrufers, ist er kein
	// Administrator mehr: wie "kein Administrator mehr" behandeln, statt die
	// Sitzung bei der allgemeinen Fehlerbehandlung zu behalten.
	var jfErr *jellyfin.Error
	if errors.As(err, &jfErr) && jfErr.Status == http.StatusForbidden {
		s.sitzungBeenden(w, r)
		return
	}
	if err != nil {
		s.apiFehler(w, r, err)
		return
	}
	nachID := make(map[string]jellyfin.User, len(users))
	for _, u := range users {
		nachID[u.ID] = u
	}

	// Die Settings-Datei ist eine Ressource des Dienstes, die Jellyfin nicht
	// schützt. Ein in Jellyfin inzwischen entmachteter Admin mit alter
	// Sitzung dürfte sonst weiter Zuordnungen ändern.
	if ich, ok := nachID[sess.UserID]; !ok || !ich.Admin {
		s.sitzungBeenden(w, r)
		return
	}
	if s.apiKey == "" {
		writeError(w, http.StatusConflict, "JELLYFIN_API_KEY ist nicht gesetzt.")
		return
	}
	id := r.PathValue("id")
	ziel, ok := nachID[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Der Benutzer existiert nicht mehr.")
		return
	}
	if ziel.Admin {
		writeError(w, http.StatusBadRequest, "Administratoren können keine Semiadmins sein.")
		return
	}

	if !req.Semiadmin {
		// users wird ignoriert.
		if err := s.semiadmins.Remove(id); err != nil {
			log.Printf("Settings-Datei nicht schreibbar: %v", err)
			writeError(w, http.StatusInternalServerError, "Einstellungen konnten nicht gespeichert werden.")
			return
		}
		writeJSON(w, http.StatusOK, delegationResponse{Semiadmin: false, Users: []string{}})
		return
	}

	// Leere Liste (auch "users": null) ist erlaubt: der Semiadmin kann sich
	// anmelden und sieht eine leere Liste.
	targets := []string{}
	gesehen := make(map[string]bool, len(req.Users))
	for _, t := range req.Users {
		u, ok := nachID[t]
		switch {
		case !ok:
			writeError(w, http.StatusBadRequest, "Unbekannter Benutzer: "+t)
			return
		case u.Admin:
			writeError(w, http.StatusBadRequest, "Administratoren können nicht geschaltet werden: "+u.Name)
			return
		case t == id:
			writeError(w, http.StatusBadRequest, "Ein Semiadmin kann sich nicht selbst schalten.")
			return
		}
		if gesehen[t] {
			continue
		}
		gesehen[t] = true
		targets = append(targets, t)
	}
	sort.Strings(targets)
	if err := s.semiadmins.Set(id, targets); err != nil {
		log.Printf("Settings-Datei nicht schreibbar: %v", err)
		writeError(w, http.StatusInternalServerError, "Einstellungen konnten nicht gespeichert werden.")
		return
	}
	writeJSON(w, http.StatusOK, delegationResponse{Semiadmin: true, Users: targets})
}

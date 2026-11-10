// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web contiene l'interfaccia utente di VeronaTAO, servita al browser.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"modernc.org/sqlite"

	"github.com/davidenegrini/veronatao/internal/domain"
	"github.com/davidenegrini/veronatao/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Version è la versione del programma, mostrata nell'interfaccia.
var Version = "0.1.0"

type session struct {
	userID   int64
	csrf     string
	lastSeen time.Time
	flash    string
	flashErr string
}

type Server struct {
	Store   *store.Store
	DataDir string
	TLS     bool
	// Shutdown viene chiamata per arrestare il programma (ad esempio dopo un
	// ripristino).
	Shutdown func()

	mu       sync.Mutex
	sessions map[string]*session
	tmpl     map[string]*template.Template
}

type ctxKey int

const (
	keyUser ctxKey = iota
	keySession
)

func New(st *store.Store, dataDir string, tls bool) *Server {
	s := &Server{Store: st, DataDir: dataDir, TLS: tls, sessions: map[string]*session{}, tmpl: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"weekdays":     func() [7]string { return domain.WeekdayShort },
		"weekdaysLong": func() [7]string { return domain.WeekdayLong },
		"weekdayLong":  func(d domain.Date) string { return strings.ToLower(domain.WeekdayLong[d.Weekday()]) },
		"mg":           domain.FormatMg,
		"statusLabel":  func(k string) string { return store.StatusLabels[k] },
		"siteLabel":    func(k string) string { return store.SiteLabels[k] },
		"today":        func() string { return domain.Today().String() },
		"inc":          func(i int) int { return i + 1 },
		"add":          func(a, b int) int { return a + b },
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[kv[i].(string)] = kv[i+1]
			}
			return m
		},
	}
	pages, _ := fs.Glob(assets, "templates/*.html")
	for _, p := range pages {
		name := strings.TrimPrefix(p, "templates/")
		if name == "layout.html" {
			continue
		}
		s.tmpl[name] = template.Must(template.New("layout.html").Funcs(funcs).ParseFS(assets, "templates/layout.html", p))
	}
	return s
}

// Handler restituisce il gestore HTTP completo.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setupPost)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginPost)

	user := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h, false)) }
	admin := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h, true)) }

	user("POST /logout", s.logout)
	user("GET /password", s.passwordPage)
	user("POST /password", s.passwordPost)
	user("GET /{$}", s.dashboard)
	user("GET /patients", s.patients)
	user("POST /patients", s.patients)
	user("GET /patients/new", s.patientForm)
	user("POST /patients/new", s.patientSave)
	user("GET /patients/{id}", s.patientPage)
	user("GET /patients/{id}/edit", s.patientForm)
	user("POST /patients/{id}/edit", s.patientSave)
	user("POST /patients/{id}/control", s.controlPost)
	user("GET /patients/{id}/record", s.recordPage)
	user("POST /patients/{id}/treatment", s.treatmentOpen)
	user("POST /patients/{id}/treatment/update", s.treatmentUpdate)
	user("POST /patients/{id}/treatment/close", s.treatmentClose)
	user("POST /patients/{id}/target", s.targetChange)
	user("POST /patients/{id}/status", s.statusChange)
	user("POST /patients/{id}/medications", s.medicationAdd)
	user("POST /patients/{id}/medications/{mid}/end", s.medicationEnd)
	user("POST /patients/{id}/medications/{mid}/delete", s.medicationDelete)
	user("POST /patients/{id}/events", s.eventAdd)
	user("POST /patients/{id}/events/{eid}/delete", s.eventDelete)
	user("POST /patients/{id}/export", s.patientExport)
	user("GET /controls/{id}", s.controlPage)
	user("POST /controls/{id}/print", s.controlPrint)
	user("POST /controls/{id}/delete", s.controlDelete)
	user("POST /controls/{id}/email", s.controlEmail)
	user("POST /patients/{id}/pdf-password", s.pdfPasswordRenew)
	user("GET /documents/{id}", s.documentGet)
	user("GET /stats", s.stats)
	user("POST /stats", s.stats)

	admin("GET /admin", s.adminPage)
	admin("POST /admin/users", s.adminUserCreate)
	admin("POST /admin/users/{id}", s.adminUserUpdate)
	admin("POST /admin/users/{id}/reset", s.adminUserReset)
	admin("POST /admin/drugs", s.adminDrugSave)
	admin("POST /admin/indications", s.adminIndicationSave)
	admin("POST /admin/eventtypes", s.adminEventTypeSave)
	admin("POST /admin/settings", s.adminSettings)
	admin("GET /admin/audit", s.adminAudit)
	admin("POST /admin/audit", s.adminAudit)
	admin("POST /admin/audit.csv", s.adminAuditCSV)
	admin("POST /admin/export.csv", s.adminExportCSV)
	admin("POST /admin/backup/download", s.backupDownload)
	admin("POST /admin/backup/now", s.backupNow)
	admin("POST /admin/backup/verify", s.backupVerify)
	admin("POST /admin/backup/restore", s.backupRestore)
	admin("POST /admin/shutdown", s.adminShutdown)
	admin("POST /admin/email/test", s.adminEmailTest)

	return s.secure(mux)
}

// secure aggiunge gli header di sicurezza e rifiuta le richieste di modifica
// provenienti da un'altra origine.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'; object-src 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if s.TLS {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			h.Set("Cache-Control", "no-store")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameOrigin(r) {
				http.Error(w, "Richiesta non valida", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin accetta solo richieste nate da una pagina di VeronaTAO. Si basa
// su Sec-Fetch-Site, inviato da tutti i browser attuali; in sua assenza
// confronta Origin con l'host richiesto. Le azioni autenticate sono inoltre
// protette dal token del modulo e dal cookie SameSite=Strict.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// ---------- Sessioni ----------

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

const cookieName = "veronatao_session"

func (s *Server) startSession(w http.ResponseWriter, userID int64) {
	token := randomToken(32)
	s.mu.Lock()
	s.sessions[token] = &session{userID: userID, csrf: randomToken(32), lastSeen: time.Now()}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.TLS, SameSite: http.SameSiteStrictMode})
}

func (s *Server) currentSession(r *http.Request) (string, *session) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", nil
	}
	timeout := time.Duration(s.Store.SettingInt("session_minutes", 20)) * time.Minute
	if timeout < time.Minute {
		timeout = 20 * time.Minute
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[c.Value]
	if sess == nil {
		return "", nil
	}
	if time.Since(sess.lastSeen) > timeout {
		delete(s.sessions, c.Value)
		return "", nil
	}
	sess.lastSeen = time.Now()
	return c.Value, sess
}

func (s *Server) endSession(w http.ResponseWriter, token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.TLS, SameSite: http.SameSiteStrictMode})
}

// dropUserSessions chiude tutte le sessioni di un utente (cambio password,
// disattivazione).
func (s *Server) dropUserSessions(userID int64, except *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if v.userID == userID && v != except {
			delete(s.sessions, k)
		}
	}
}

func (s *Server) auth(next http.HandlerFunc, adminOnly bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Store.UserCount() == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		token, sess := s.currentSession(r)
		if sess == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		u, err := s.Store.UserByID(sess.userID)
		if err != nil || !u.Active {
			s.endSession(w, token)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			if subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(sess.csrf)) != 1 {
				http.Error(w, "Sessione non valida: ricaricare la pagina e riprovare.", http.StatusForbidden)
				return
			}
		}
		if u.MustChange && r.URL.Path != "/password" && r.URL.Path != "/logout" {
			http.Redirect(w, r, "/password", http.StatusSeeOther)
			return
		}
		if adminOnly && !u.IsAdmin() {
			http.Error(w, "Funzione riservata all'amministratore", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), keyUser, &u)
		ctx = context.WithValue(ctx, keySession, sess)
		next(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(keyUser).(*store.User)
	return u
}

func sessionOf(r *http.Request) *session {
	x, _ := r.Context().Value(keySession).(*session)
	return x
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func actor(r *http.Request) store.Actor {
	a := store.Actor{IP: clientIP(r)}
	if u := currentUser(r); u != nil {
		a.UserID, a.Username = u.ID, u.Username
	}
	return a
}

// ---------- Rendering ----------

func (s *Server) render(w http.ResponseWriter, r *http.Request, page, title string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Title"] = title
	data["Version"] = Version
	if u := currentUser(r); u != nil {
		data["User"] = u
	}
	if sess := sessionOf(r); sess != nil {
		data["CSRF"] = sess.csrf
		s.mu.Lock()
		if _, ok := data["Flash"]; !ok {
			data["Flash"] = sess.flash
		}
		if _, ok := data["Error"]; !ok {
			data["Error"] = sess.flashErr
		}
		sess.flash, sess.flashErr = "", ""
		s.mu.Unlock()
	}
	t := s.tmpl[page]
	if t == nil {
		http.Error(w, "Pagina non disponibile", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("errore di rendering %s: %v", page, err)
	}
}

func (s *Server) flash(r *http.Request, msg string) {
	if sess := sessionOf(r); sess != nil {
		s.mu.Lock()
		sess.flash = msg
		s.mu.Unlock()
	}
}

// userMessage restituisce il testo da mostrare per un errore. Gli errori
// interni dell'archivio non vengono mai mostrati: finiscono nel log locale.
func userMessage(err error) string {
	var se *sqlite.Error
	if errors.As(err, &se) || errors.Is(err, sql.ErrNoRows) {
		log.Printf("errore interno: %v", err)
		return "Operazione non riuscita per un errore interno. Riprovare; se il problema persiste contattare l'amministratore."
	}
	return err.Error()
}

func (s *Server) flashErr(r *http.Request, err error) {
	if sess := sessionOf(r); sess != nil {
		s.mu.Lock()
		sess.flashErr = userMessage(err)
		s.mu.Unlock()
	}
}

// done imposta il messaggio di esito e torna alla pagina indicata.
func (s *Server) done(w http.ResponseWriter, r *http.Request, err error, okMsg, to string) {
	if err != nil {
		s.flashErr(r, err)
	} else if okMsg != "" {
		s.flash(r, okMsg)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// fail risponde con un errore generico, senza dettagli tecnici né dati
// personali; il dettaglio va nel log locale.
func (s *Server) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		http.Error(w, "Elemento non trovato", http.StatusNotFound)
		return
	}
	log.Printf("errore: %v", err)
	http.Error(w, "Si è verificato un errore. Riprovare; se il problema persiste contattare l'amministratore.", http.StatusInternalServerError)
}

var errNotFound = errors.New("non trovato")

func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// ---------- Password ----------

const minPassword = 10

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	const t, m, p = 3, 64 * 1024, 2
	h := argon2.IDKey([]byte(pw), salt, t, m, p, 32)
	return fmt.Sprintf("argon2id$%d$%d$%d$%s$%s", t, m, p, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h))
}

// HashPassword è usata anche dalla riga di comando per il recupero
// dell'amministratore.
func HashPassword(pw string) string { return hashPassword(pw) }

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "argon2id" {
		return false
	}
	t, _ := strconv.Atoi(parts[1])
	m, _ := strconv.Atoi(parts[2])
	p, _ := strconv.Atoi(parts[3])
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || t <= 0 || m <= 0 || p <= 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash serve a rendere il tempo di risposta del login indipendente
// dall'esistenza dell'utente.
var dummyHash = hashPassword(randomToken(16))

// TempPassword genera una password temporanea facile da trascrivere.
func TempPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:4]) + "-" + string(b[4:8]) + "-" + string(b[8:])
}

func validatePassword(pw, confirm string) error {
	if pw != confirm {
		return errors.New("le due password non coincidono")
	}
	if len([]rune(pw)) < minPassword {
		return fmt.Errorf("la password deve avere almeno %d caratteri", minPassword)
	}
	return nil
}

// ---------- Primo avvio, accesso, cambio password ----------

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if s.Store.UserCount() > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, "setup.html", "Primo avvio", nil)
}

func (s *Server) setupPost(w http.ResponseWriter, r *http.Request) {
	if s.Store.UserCount() > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	fullName := strings.TrimSpace(r.FormValue("full_name"))
	err := validatePassword(r.FormValue("password"), r.FormValue("password2"))
	if err == nil && (username == "" || fullName == "") {
		err = errors.New("nome utente e nome completo sono obbligatori")
	}
	if err != nil {
		s.render(w, r, "setup.html", "Primo avvio", map[string]any{"Error": err.Error(), "Username": username, "FullName": fullName})
		return
	}
	a := store.Actor{Username: username, IP: clientIP(r)}
	id, err := s.Store.CreateUser(a, store.User{Username: username, FullName: fullName, PasswordHash: hashPassword(r.FormValue("password")), Role: "admin"})
	if err != nil {
		s.fail(w, err)
		return
	}
	if h := strings.TrimSpace(r.FormValue("header")); h != "" {
		_ = s.Store.SetSetting("header", h)
	}
	s.startSession(w, id)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.Store.UserCount() == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", "Accesso", nil)
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	u, err := s.Store.UserByUsername(username)
	a := store.Actor{Username: username, IP: clientIP(r)}
	ok := false
	if err != nil {
		checkPassword(dummyHash, password)
	} else {
		a.UserID = u.ID
		locked := u.LockedUntil != "" && u.LockedUntil > store.Now()
		valid := checkPassword(u.PasswordHash, password)
		switch {
		case locked || !u.Active:
		case valid:
			ok = true
		default:
			s.Store.LoginFailed(u.ID, 5, 15*time.Minute)
		}
	}
	if !ok {
		s.Store.Audit(a, "login_failed", "user", a.UserID, 0, "")
		// Messaggio generico: non rivela se l'utente esiste o è bloccato.
		s.render(w, r, "login.html", "Accesso", map[string]any{"Error": "Credenziali non valide, oppure accesso temporaneamente bloccato dopo troppi tentativi."})
		return
	}
	s.Store.LoginSucceeded(u.ID)
	s.Store.Audit(a, "login", "user", u.ID, 0, "")
	s.startSession(w, u.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.Store.Audit(actor(r), "logout", "user", currentUser(r).ID, 0, "")
		s.endSession(w, c.Value)
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) passwordPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "password.html", "Cambio password", nil)
}

func (s *Server) passwordPost(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	err := validatePassword(r.FormValue("password"), r.FormValue("password2"))
	if err == nil && !checkPassword(u.PasswordHash, r.FormValue("current")) {
		err = errors.New("la password attuale non è corretta")
	}
	if err == nil && r.FormValue("password") == r.FormValue("current") {
		err = errors.New("la nuova password deve essere diversa da quella attuale")
	}
	if err != nil {
		s.render(w, r, "password.html", "Cambio password", map[string]any{"Error": err.Error()})
		return
	}
	if err := s.Store.SetPassword(actor(r), u.ID, hashPassword(r.FormValue("password")), false, "password_change"); err != nil {
		s.fail(w, err)
		return
	}
	s.dropUserSessions(u.ID, sessionOf(r))
	s.done(w, r, nil, "Password aggiornata.", "/")
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/davidenegrini/veronatao/internal/backup"
	"github.com/davidenegrini/veronatao/internal/domain"
	"github.com/davidenegrini/veronatao/internal/store"
)

func newServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st, dir, false)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestPasswordHash(t *testing.T) {
	h := hashPassword("una-password-lunga")
	if !checkPassword(h, "una-password-lunga") || checkPassword(h, "altra-password") || checkPassword("non-un-hash", "x") {
		t.Error("verifica della password")
	}
	if !strings.HasPrefix(h, "argon2id$") || strings.Contains(h, "una-password") {
		t.Error("formato dell'hash")
	}
	if validatePassword("corta", "corta") == nil || validatePassword("abbastanza-lunga", "diversa-ma-lunga") == nil {
		t.Error("criteri della password")
	}
}

// Nessuna pagina è raggiungibile senza accesso.
func TestEverythingRequiresLogin(t *testing.T) {
	s, ts := newServer(t)
	if _, err := s.Store.CreateUser(store.Actor{}, store.User{Username: "admin", FullName: "A", PasswordHash: hashPassword("password-di-prova"), Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	c := noRedirect()
	for _, path := range []string{"/", "/patients", "/patients/1", "/patients/1/record", "/patients/1/edit", "/controls/1", "/documents/1", "/stats", "/admin", "/admin/audit", "/password"} {
		resp, err := c.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
			t.Errorf("%s accessibile senza login: %d", path, resp.StatusCode)
		}
	}
	for _, path := range []string{"/patients/1/control", "/admin/users", "/admin/backup/download", "/admin/export.csv", "/controls/1/print", "/patients/1/export"} {
		resp, err := c.PostForm(ts.URL+path, url.Values{})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Errorf("POST %s senza login: %d", path, resp.StatusCode)
		}
	}
	if resp, _ := c.Get(ts.URL + "/setup"); resp.StatusCode != http.StatusSeeOther {
		t.Error("la pagina di primo avvio deve sparire dopo la creazione del primo utente")
	}
}

func TestLoginCSRFRolesAndLockout(t *testing.T) {
	s, ts := newServer(t)
	s.Store.CreateUser(store.Actor{}, store.User{Username: "medico", FullName: "M", PasswordHash: hashPassword("password-di-prova"), Role: "clinician"})
	c := noRedirect()
	login := func(pw string) *http.Response {
		resp, err := c.PostForm(ts.URL+"/login", url.Values{"username": {"medico"}, "password": {pw}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if login("sbagliata").StatusCode != http.StatusOK {
		t.Error("password errata")
	}
	resp := login("password-di-prova")
	if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) == 0 {
		t.Fatal("login non riuscito")
	}
	cookie := resp.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Error("attributi del cookie di sessione")
	}
	do := func(method, path string, form url.Values) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if r := do("GET", "/", nil); r.StatusCode != http.StatusOK || r.Header.Get("Content-Security-Policy") == "" || r.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("pagina iniziale o header di sicurezza", r.StatusCode)
	}
	if r := do("GET", "/admin", nil); r.StatusCode != http.StatusForbidden {
		t.Error("amministrazione accessibile a un non amministratore:", r.StatusCode)
	}
	if r := do("POST", "/patients/new", url.Values{"last_name": {"X"}}); r.StatusCode != http.StatusForbidden {
		t.Error("POST senza token accettato:", r.StatusCode)
	}
	// Richiesta proveniente da un altro sito.
	req, _ := http.NewRequest("POST", ts.URL+"/login", strings.NewReader("username=medico&password=password-di-prova"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if r, _ := c.Do(req); r.StatusCode != http.StatusForbidden {
		t.Error("richiesta da altra origine accettata")
	}
	// Blocco dopo cinque tentativi falliti: anche la password giusta è rifiutata.
	for i := 0; i < 5; i++ {
		login("sbagliata")
	}
	if login("password-di-prova").StatusCode != http.StatusOK {
		t.Error("accesso non bloccato dopo i tentativi falliti")
	}
	var failed int
	s.Store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action = 'login_failed'").Scan(&failed)
	if failed < 6 {
		t.Errorf("tentativi falliti registrati: %d", failed)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	s, _ := newServer(t)
	uid, _ := s.Store.CreateUser(store.Actor{}, store.User{Username: "admin", FullName: "A", PasswordHash: "x", Role: "admin"})
	a := store.Actor{UserID: uid, Username: "admin"}
	s.Store.SavePatient(a, store.Patient{LastName: "VERDI", FirstName: "ANNA", BirthDate: domain.MustDate("1950-01-01")})
	data, err := s.snapshot("passphrase-di-prova")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "VERDI") {
		t.Fatal("il backup contiene dati in chiaro")
	}
	plain, summary, err := s.inspect(data, "passphrase-di-prova")
	if err != nil || !strings.HasPrefix(summary, "1 pazienti") || !strings.HasPrefix(string(plain), "SQLite format 3") {
		t.Fatal(summary, err)
	}
	if _, _, err := s.inspect(data, "passphrase-sbagliata"); err == nil {
		t.Error("passphrase errata accettata")
	}
	bogus, _ := backup.Encrypt([]byte("non è un archivio"), "passphrase-di-prova")
	if _, _, err := s.inspect(bogus, "passphrase-di-prova"); err == nil {
		t.Error("contenuto non valido accettato")
	}
	// Backup automatico: richiede la passphrase, scrive il file, ruota.
	if _, err := s.AutoBackup(a, true); err == nil {
		t.Error("backup automatico senza passphrase")
	}
	s.Store.SetSetting("backup_passphrase", "passphrase-di-prova")
	s.Store.SetSetting("backup_keep", "1")
	p1, err := s.AutoBackup(a, true)
	if err != nil || p1 == "" {
		t.Fatal(err)
	}
	if p, _ := s.AutoBackup(a, false); p != "" {
		t.Error("secondo backup nello stesso giorno senza richiesta esplicita")
	}
	files, _ := filepath.Glob(filepath.Join(s.backupDir(), "*"+backupExt))
	if len(files) != 1 {
		t.Errorf("file di backup: %d", len(files))
	}
	if s.backupWarning() == "" {
		t.Error("atteso avviso: nessuna verifica eseguita")
	}
}

// Le pagine non devono contenere stili o script in linea: la CSP li vieta.
func TestTemplatesHaveNoInlineCode(t *testing.T) {
	re := regexp.MustCompile(`(?i)\sstyle=|<script(?:\s[^>]*)?>[^<]|\son[a-z]+=`)
	entries, _ := assets.ReadDir("templates")
	for _, e := range entries {
		b, _ := assets.ReadFile("templates/" + e.Name())
		if m := re.Find(b); m != nil {
			t.Errorf("%s contiene codice in linea: %q", e.Name(), m)
		}
	}
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/davidenegrini/veronatao/internal/backup"
	"github.com/davidenegrini/veronatao/internal/domain"
	"github.com/davidenegrini/veronatao/internal/mail"
	"github.com/davidenegrini/veronatao/internal/store"
)

// ---------- Statistiche ----------

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	to := domain.Today()
	from := to.AddDays(-364)
	if v, err := domain.ParseDate(r.FormValue("from")); err == nil && !v.IsZero() {
		from = v
	}
	if v, err := domain.ParseDate(r.FormValue("to")); err == nil && !v.IsZero() {
		to = v
	}
	data := map[string]any{"From": from, "To": to}
	if to.Before(from) {
		data["Error"] = "La data di fine precede quella di inizio."
	} else if st, err := s.Store.Stats(from, to); err != nil {
		s.fail(w, err)
		return
	} else {
		b, in, a, ok := st.TTR.Percent()
		data["S"] = st
		data["TTR"] = ttrView{OK: ok, Below: b, In: in, Above: a}
		data["PatientYears"] = strconv.FormatFloat(st.PatientYears(), 'f', 1, 64)
		data["MeanInterval"] = strconv.FormatFloat(st.MeanIntervalD, 'f', 1, 64)
	}
	s.render(w, r, "stats.html", "Statistiche", data)
}

// ---------- Amministrazione ----------

var settingKeys = []string{"header", "doc_title", "template_code", "footer_text", "session_minutes", "max_daily_quarters",
	"weekly_variation_pct", "lost_days", "due_days", "backup_warn_days", "backup_keep", "backup_dir", "port",
	"smtp_host", "smtp_port", "smtp_user", "smtp_from", "smtp_from_name", "email_subject", "email_body"}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{}
	data["Users"], _ = s.Store.Users()
	data["Drugs"], _ = s.Store.Drugs(false)
	data["Indications"], _ = s.Store.Indications(false)
	data["EventTypes"], _ = s.Store.EventTypes(false)
	set := map[string]string{}
	for _, k := range append(append([]string{}, settingKeys...), "print_ttr", "lan_enabled", "email_enabled", "last_backup_at", "last_verify_at") {
		set[k] = s.Store.Setting(k)
	}
	data["Set"] = set
	data["HasPassphrase"] = s.Store.Setting("backup_passphrase") != ""
	data["HasSMTPPassword"] = s.Store.Setting("smtp_password") != ""
	data["BackupDir"] = s.backupDir()
	data["BackupWarning"] = s.backupWarning()
	data["DataDir"] = s.DataDir
	s.render(w, r, "admin.html", "Amministrazione", data)
}

func (s *Server) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	fullName := strings.TrimSpace(r.FormValue("full_name"))
	role := r.FormValue("role")
	if username == "" || fullName == "" || (role != "admin" && role != "clinician" && role != "assistant") {
		s.done(w, r, errors.New("nome utente, nome completo e ruolo sono obbligatori"), "", "/admin")
		return
	}
	temp := TempPassword()
	_, err := s.Store.CreateUser(actor(r), store.User{Username: username, FullName: fullName, PasswordHash: hashPassword(temp), Role: role, MustChange: true})
	if err != nil {
		err = errors.New("impossibile creare l'utente: il nome utente esiste già?")
	}
	s.done(w, r, err, "Utente "+username+" creato. Password temporanea da comunicare di persona: "+temp+" (andrà cambiata al primo accesso).", "/admin")
}

func (s *Server) adminUserUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	active := r.FormValue("active") == "1"
	err := s.Store.UpdateUser(actor(r), id, strings.TrimSpace(r.FormValue("full_name")), r.FormValue("role"), active)
	if err == nil && !active {
		s.dropUserSessions(id, nil)
	}
	s.done(w, r, err, "Utente aggiornato.", "/admin")
}

// adminUserReset è il percorso primario di recupero delle credenziali:
// funziona senza posta elettronica e senza Internet.
func (s *Server) adminUserReset(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	u, err := s.Store.UserByID(id)
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	temp := TempPassword()
	err = s.Store.SetPassword(actor(r), id, hashPassword(temp), true, "password_reset")
	if err == nil {
		s.dropUserSessions(id, sessionOf(r))
	}
	s.done(w, r, err, "Password di "+u.Username+" reimpostata. Password temporanea da comunicare di persona: "+temp, "/admin")
}

func (s *Server) adminDrugSave(w http.ResponseWriter, r *http.Request) {
	d := store.Drug{Ingredient: strings.TrimSpace(r.FormValue("ingredient")), Brand: strings.TrimSpace(r.FormValue("brand")),
		Div: domain.Divisibility(r.FormValue("divisibility")), Active: r.FormValue("active") == "1"}
	d.ID, _ = strconv.ParseInt(r.FormValue("id"), 10, 64)
	mg, err := domain.ParseDecimal(r.FormValue("mg"), 1000)
	d.MgMilli = mg
	switch {
	case err != nil || mg <= 0 || mg%4 != 0:
		err = errors.New("mg per compressa non validi")
	case d.Ingredient == "" || d.Brand == "":
		err = errors.New("principio attivo e nome commerciale sono obbligatori")
	case d.Div != domain.DivQuarter && d.Div != domain.DivHalf && d.Div != domain.DivWhole:
		err = errors.New("divisibilità non valida")
	default:
		err = s.Store.SaveDrug(actor(r), d)
	}
	s.done(w, r, err, "Farmaco salvato.", "/admin")
}

func (s *Server) adminIndicationSave(w http.ResponseWriter, r *http.Request) {
	i := store.Indication{Name: strings.TrimSpace(r.FormValue("name")), Active: r.FormValue("active") == "1"}
	i.ID, _ = strconv.ParseInt(r.FormValue("id"), 10, 64)
	var err error
	if i.Min, err = parseINR(r.FormValue("inr_min")); err == nil {
		if i.Max, err = parseINR(r.FormValue("inr_max")); err == nil {
			i.Target, err = parseINR(r.FormValue("inr_target"))
		}
	}
	if err == nil && (i.Name == "" || i.Max < i.Min) {
		err = errors.New("nome mancante o range non valido")
	}
	if err == nil {
		err = s.Store.SaveIndication(actor(r), i)
	}
	s.done(w, r, err, "Indicazione salvata.", "/admin")
}

func (s *Server) adminEventTypeSave(w http.ResponseWriter, r *http.Request) {
	e := store.EventType{Name: strings.TrimSpace(r.FormValue("name")), Category: r.FormValue("category"), Active: r.FormValue("active") == "1"}
	e.ID, _ = strconv.ParseInt(r.FormValue("id"), 10, 64)
	var err error
	if e.Name == "" || (e.Category != "hemorrhagic" && e.Category != "thrombotic" && e.Category != "other") {
		err = errors.New("nome e categoria sono obbligatori")
	} else {
		err = s.Store.SaveEventType(actor(r), e)
	}
	s.done(w, r, err, "Tipo di evento salvato.", "/admin")
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	numeric := map[string][2]int{"session_minutes": {1, 480}, "max_daily_quarters": {0, 80}, "weekly_variation_pct": {0, 1000},
		"lost_days": {1, 3650}, "due_days": {0, 365}, "backup_warn_days": {1, 365}, "backup_keep": {1, 3650}, "port": {1024, 65535}, "smtp_port": {1, 65535}}
	var changed []string
	set := func(k, v string) error {
		if s.Store.Setting(k) == v {
			return nil
		}
		changed = append(changed, k)
		return s.Store.SetSetting(k, v)
	}
	var err error
	for _, k := range settingKeys {
		v := strings.TrimSpace(strings.ReplaceAll(r.FormValue(k), "\r", ""))
		if lim, ok := numeric[k]; ok {
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n < lim[0] || n > lim[1] {
				err = fmt.Errorf("valore non valido per %s (ammesso da %d a %d)", k, lim[0], lim[1])
				break
			}
		}
		if err = set(k, v); err != nil {
			break
		}
	}
	if err == nil {
		for _, k := range []string{"print_ttr", "lan_enabled", "email_enabled"} {
			v := "0"
			if r.FormValue(k) == "1" {
				v = "1"
			}
			if err = set(k, v); err != nil {
				break
			}
		}
	}
	if pass := r.FormValue("backup_passphrase"); err == nil && pass != "" {
		if len(pass) < backup.MinPassphrase {
			err = fmt.Errorf("la passphrase dei backup deve avere almeno %d caratteri", backup.MinPassphrase)
		} else if pass != r.FormValue("backup_passphrase2") {
			err = errors.New("le due passphrase non coincidono")
		} else if err = s.Store.SetSetting("backup_passphrase", pass); err == nil {
			changed = append(changed, "backup_passphrase")
		}
	}
	if pass := strings.TrimSpace(r.FormValue("smtp_password")); err == nil && pass != "" {
		if err = s.Store.SetSetting("smtp_password", pass); err == nil {
			changed = append(changed, "smtp_password")
		}
	}
	if err == nil && s.emailEnabled() {
		if verr := s.mailConfig().Validate(); verr != nil {
			_ = s.Store.SetSetting("email_enabled", "0")
			err = fmt.Errorf("invio per email non attivato: %s", verr)
		} else if s.Store.Setting("email_subject") == "" || s.Store.Setting("email_body") == "" {
			_ = s.Store.SetSetting("email_enabled", "0")
			err = errors.New("invio per email non attivato: oggetto e testo del messaggio sono obbligatori")
		}
	}
	if len(changed) > 0 {
		// Nel registro finiscono i nomi delle voci modificate, mai passphrase o password.
		s.Store.Audit(actor(r), "update", "settings", 0, 0, strings.Join(changed, ", "))
	}
	s.done(w, r, err, "Impostazioni salvate. Le modifiche a porta e accesso dalla rete locale hanno effetto al prossimo avvio.", "/admin")
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.FormValue("q"))
	rows, err := s.Store.AuditList(0, q, 500)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "audit.html", "Registro delle attività", map[string]any{"Rows": rows, "Q": q})
}

func (s *Server) adminAuditCSV(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.AuditList(0, "", 1000000)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Store.Audit(actor(r), "export", "audit_log", 0, 0, fmt.Sprintf("%d voci", len(rows)))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="registro-attivita.csv"`)
	_, _ = w.Write([]byte("\xef\xbb\xbf"))
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	_ = cw.Write([]string{"id", "data_ora", "utente", "azione", "entita", "id_entita", "id_paziente", "dettagli", "indirizzo"})
	for _, e := range rows {
		_ = cw.Write([]string{strconv.FormatInt(e.ID, 10), e.At, e.Username, e.Action, e.Entity, strconv.FormatInt(e.EntityID, 10), strconv.FormatInt(e.PatientID, 10), e.Details, e.IP})
	}
	cw.Flush()
}

func (s *Server) adminExportCSV(w http.ResponseWriter, r *http.Request) {
	pseudo := r.FormValue("mode") != "nominative"
	name := "controlli-pseudonimizzati.csv"
	if !pseudo {
		name = "controlli-nominativi.csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write([]byte("\xef\xbb\xbf"))
	n, err := s.Store.ExportControlsCSV(w, pseudo)
	if err != nil {
		log.Printf("errore di esportazione: %v", err)
	}
	s.Store.Audit(actor(r), "export", "controls", 0, 0, fmt.Sprintf("%s, %d righe", name, n))
}

func (s *Server) adminShutdown(w http.ResponseWriter, r *http.Request) {
	s.Store.Audit(actor(r), "shutdown", "", 0, 0, "")
	s.render(w, r, "stopped.html", "Programma arrestato", map[string]any{"Message": "VeronaTAO è stato arrestato. Per riprendere, avviarlo di nuovo dal collegamento sul computer che ospita l'archivio."})
	if s.Shutdown != nil {
		go func() { time.Sleep(500 * time.Millisecond); s.Shutdown() }()
	}
}

// ---------- Backup e ripristino ----------

const backupExt = ".vtbk"

func (s *Server) backupDir() string {
	if d := strings.TrimSpace(s.Store.Setting("backup_dir")); d != "" {
		return d
	}
	return filepath.Join(s.DataDir, "backups")
}

func (s *Server) tmpDir() (string, error) {
	d := filepath.Join(s.DataDir, "tmp")
	return d, os.MkdirAll(d, 0o700)
}

// snapshot produce una copia coerente dell'archivio, cifrata con la
// passphrase.
func (s *Server) snapshot(pass string) ([]byte, error) {
	if len(pass) < backup.MinPassphrase {
		return nil, fmt.Errorf("la passphrase deve avere almeno %d caratteri", backup.MinPassphrase)
	}
	dir, err := s.tmpDir()
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, "snapshot-"+randomToken(8)+".db")
	defer os.Remove(tmp)
	if err := s.Store.SnapshotTo(tmp); err != nil {
		return nil, err
	}
	plain, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	return backup.Encrypt(plain, pass)
}

// inspect decifra un backup e ne verifica l'integrità senza ripristinarlo.
// Restituisce i dati decifrati e una descrizione del contenuto.
func (s *Server) inspect(data []byte, pass string) ([]byte, string, error) {
	plain, err := backup.Decrypt(data, pass)
	if err != nil {
		return nil, "", err
	}
	dir, err := s.tmpDir()
	if err != nil {
		return nil, "", err
	}
	tmp := filepath.Join(dir, "verify-"+randomToken(8)+".db")
	defer os.Remove(tmp)
	if err := os.WriteFile(tmp, plain, 0o600); err != nil {
		return nil, "", err
	}
	db, err := sql.Open("sqlite", "file:"+tmp+"?mode=ro")
	if err != nil {
		return nil, "", err
	}
	defer db.Close()
	var res string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&res); err != nil || res != "ok" {
		return nil, "", errors.New("il backup è stato decifrato ma l'archivio al suo interno risulta danneggiato")
	}
	var patients, controls, version int
	var lastAudit sql.NullString
	if err := db.QueryRow("SELECT (SELECT COUNT(*) FROM patients), (SELECT COUNT(*) FROM controls), (SELECT MAX(at) FROM audit_log)").Scan(&patients, &controls, &lastAudit); err != nil {
		return nil, "", errors.New("il backup non contiene un archivio VeronaTAO valido")
	}
	_ = db.QueryRow("PRAGMA user_version").Scan(&version)
	if version > store.SchemaVersion {
		return nil, "", errors.New("il backup proviene da una versione più recente di VeronaTAO")
	}
	return plain, fmt.Sprintf("%d pazienti, %d controlli, ultima attività registrata: %s", patients, controls, lastAudit.String), nil
}

func (s *Server) backupDownload(w http.ResponseWriter, r *http.Request) {
	pass := r.FormValue("passphrase")
	if pass != r.FormValue("passphrase2") {
		s.done(w, r, errors.New("le due passphrase non coincidono"), "", "/admin")
		return
	}
	data, err := s.snapshot(pass)
	if err != nil {
		s.done(w, r, err, "", "/admin")
		return
	}
	s.Store.Audit(actor(r), "backup", "", 0, 0, "copia cifrata scaricata (trasporto o copia fuori sede)")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="veronatao-`+time.Now().Format("20060102-150405")+backupExt+`"`)
	_, _ = w.Write(data)
}

// AutoBackup esegue il backup giornaliero se non è già stato fatto oggi.
// force lo esegue comunque.
func (s *Server) AutoBackup(a store.Actor, force bool) (string, error) {
	pass := s.Store.Setting("backup_passphrase")
	if pass == "" {
		return "", errors.New("passphrase dei backup non impostata")
	}
	today := time.Now().Format("2006-01-02")
	if !force && strings.HasPrefix(s.Store.Setting("last_backup_at"), today) {
		return "", nil
	}
	data, err := s.snapshot(pass)
	if err != nil {
		return "", err
	}
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "veronatao-"+time.Now().Format("20060102-150405")+backupExt)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	_ = s.Store.SetSetting("last_backup_at", store.Now())
	s.Store.Audit(a, "backup", "", 0, 0, filepath.Base(path))
	// Rotazione: si conservano solo i file più recenti.
	files, _ := filepath.Glob(filepath.Join(dir, "veronatao-*"+backupExt))
	sort.Strings(files)
	keep := s.Store.SettingInt("backup_keep", 30)
	for len(files) > keep && keep > 0 {
		_ = os.Remove(files[0])
		files = files[1:]
	}
	return path, nil
}

// RunScheduler esegue il backup automatico e la pulizia delle sessioni.
func (s *Server) RunScheduler(stop <-chan struct{}) {
	tick := time.NewTicker(30 * time.Minute)
	defer tick.Stop()
	for {
		if _, err := s.AutoBackup(store.Actor{Username: "sistema"}, false); err != nil && s.Store.Setting("backup_passphrase") != "" {
			log.Printf("backup automatico non riuscito: %v", err)
		}
		// Le sessioni abbandonate da oltre un giorno vengono eliminate.
		s.mu.Lock()
		for k, v := range s.sessions {
			if time.Since(v.lastSeen) > 24*time.Hour {
				delete(s.sessions, k)
			}
		}
		s.mu.Unlock()
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

func (s *Server) backupWarning() string {
	if s.Store.Setting("backup_passphrase") == "" {
		return "Backup automatico non configurato: impostare la passphrase dei backup in Amministrazione."
	}
	warn := s.Store.SettingInt("backup_warn_days", 7)
	last, err := time.ParseInLocation(store.TS, s.Store.Setting("last_backup_at"), time.Local)
	if err != nil {
		return "Nessun backup automatico ancora eseguito."
	}
	if time.Since(last) > time.Duration(warn)*24*time.Hour {
		return "L'ultimo backup risale al " + last.Format("02/01/2006") + "."
	}
	verified, err := time.ParseInLocation(store.TS, s.Store.Setting("last_verify_at"), time.Local)
	if err != nil {
		return "Nessuna verifica dei backup mai eseguita: un backup mai verificato non è un backup."
	}
	if time.Since(verified) > 183*24*time.Hour {
		return "L'ultima verifica dei backup risale al " + verified.Format("02/01/2006") + " (oltre 6 mesi)."
	}
	return ""
}

func (s *Server) backupNow(w http.ResponseWriter, r *http.Request) {
	path, err := s.AutoBackup(actor(r), true)
	s.done(w, r, err, "Backup eseguito: "+path, "/admin")
}

// uploaded restituisce il file caricato; se assente, l'ultimo backup
// automatico e la passphrase memorizzata.
func (s *Server) uploaded(r *http.Request) (data []byte, pass string, err error) {
	pass = r.FormValue("passphrase")
	f, _, ferr := r.FormFile("file")
	if ferr == nil {
		defer f.Close()
		data, err = io.ReadAll(f)
		return data, pass, err
	}
	files, _ := filepath.Glob(filepath.Join(s.backupDir(), "veronatao-*"+backupExt))
	if len(files) == 0 {
		return nil, "", errors.New("nessun file caricato e nessun backup automatico presente")
	}
	sort.Strings(files)
	data, err = os.ReadFile(files[len(files)-1])
	if pass == "" {
		pass = s.Store.Setting("backup_passphrase")
	}
	return data, pass, err
}

func (s *Server) backupVerify(w http.ResponseWriter, r *http.Request) {
	data, pass, err := s.uploaded(r)
	var summary string
	if err == nil {
		_, summary, err = s.inspect(data, pass)
	}
	if err == nil {
		_ = s.Store.SetSetting("last_verify_at", store.Now())
		s.Store.Audit(actor(r), "backup_verify", "", 0, 0, summary)
	}
	s.done(w, r, err, "Backup verificato: leggibile e integro. Contenuto: "+summary+".", "/admin")
}

// RestorePending è il nome del file che, se presente all'avvio, sostituisce
// l'archivio corrente.
const RestorePending = "restore-pending.db"

func (s *Server) backupRestore(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("confirm") != "RIPRISTINA" {
		s.done(w, r, errors.New("per procedere scrivere RIPRISTINA nel campo di conferma"), "", "/admin")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		s.done(w, r, errors.New("selezionare il file di backup da ripristinare"), "", "/admin")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	var plain []byte
	var summary string
	if err == nil {
		plain, summary, err = s.inspect(data, r.FormValue("passphrase"))
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(s.DataDir, RestorePending), plain, 0o600)
	}
	if err != nil {
		s.done(w, r, err, "", "/admin")
		return
	}
	s.Store.Audit(actor(r), "restore", "", 0, 0, "ripristino predisposto: "+summary)
	s.render(w, r, "stopped.html", "Ripristino predisposto", map[string]any{"Message": "Il backup è stato verificato (" + summary + "). VeronaTAO ora si arresta: al prossimo avvio userà l'archivio ripristinato. L'archivio attuale viene conservato nella cartella dei dati con il nome «prima-del-ripristino»."})
	if s.Shutdown != nil {
		go func() { time.Sleep(500 * time.Millisecond); s.Shutdown() }()
	}
}

// adminEmailTest invia un messaggio di prova, senza alcun dato di pazienti,
// per verificare la configurazione del server di posta.
func (s *Server) adminEmailTest(w http.ResponseWriter, r *http.Request) {
	to := strings.TrimSpace(r.FormValue("to"))
	err := mail.Send(s.mailConfig(), mail.Message{To: to, Subject: "Prova di invio da VeronaTAO",
		Body: "Questo è un messaggio di prova inviato da VeronaTAO per verificare la configurazione del server di posta. Non contiene dati."})
	if err == nil {
		s.Store.Audit(actor(r), "email", "settings", 0, 0, "messaggio di prova inviato")
	}
	s.done(w, r, err, "Messaggio di prova inviato a "+to+": controlla che sia arrivato.", "/admin")
}

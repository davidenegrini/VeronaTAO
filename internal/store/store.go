// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store gestisce l'archivio SQLite di VeronaTAO.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/davidenegrini/veronatao/internal/domain"
)

//go:embed schema.sql
var schemaSQL string

const schemaVersion = 1

// TS è il formato dei timestamp tecnici (non delle date cliniche).
const TS = "2006-01-02 15:04:05"

func Now() string { return time.Now().Format(TS) }

type Store struct {
	DB   *sql.DB
	Path string
}

// Open apre (o crea) l'archivio e applica lo schema.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, Path: path}
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		db.Close()
		return nil, err
	}
	switch {
	case v == 0:
		tx, err := db.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(schemaSQL); err != nil {
			return nil, fmt.Errorf("creazione schema: %w", err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	case v > schemaVersion:
		db.Close()
		return nil, fmt.Errorf("l'archivio è stato creato da una versione più recente di VeronaTAO (schema %d)", v)
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// IntegrityCheck esegue il controllo di integrità di SQLite.
func (s *Store) IntegrityCheck() error {
	var res string
	if err := s.DB.QueryRow("PRAGMA integrity_check").Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return errors.New("archivio danneggiato: " + res)
	}
	return nil
}

// SnapshotTo scrive una copia coerente dell'archivio nel file indicato.
func (s *Store) SnapshotTo(path string) error {
	_, err := s.DB.Exec("VACUUM INTO ?", path)
	return err
}

// execer è soddisfatto sia da *sql.DB sia da *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

func (s *Store) tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- Impostazioni ----------

func (s *Store) Setting(key string) string {
	var v string
	_ = s.DB.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	return v
}

func (s *Store) SettingInt(key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s.Setting(key)))
	if err != nil {
		return def
	}
	return n
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec("INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// ---------- Registro delle attività ----------

// Actor identifica chi compie un'azione.
type Actor struct {
	UserID   int64
	Username string
	IP       string
}

type AuditEntry struct {
	ID        int64
	At        string
	Username  string
	Action    string
	Entity    string
	EntityID  int64
	PatientID int64
	Details   string
	IP        string
}

func audit(x execer, a Actor, action, entity string, entityID, patientID int64, details string) error {
	_, err := x.Exec(`INSERT INTO audit_log (at, user_id, username, action, entity, entity_id, patient_id, details, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, Now(), a.UserID, a.Username, action, entity, entityID, patientID, details, a.IP)
	return err
}

// Audit registra una voce nel registro delle attività.
func (s *Store) Audit(a Actor, action, entity string, entityID, patientID int64, details string) {
	_ = audit(s.DB, a, action, entity, entityID, patientID, details)
}

// AuditList restituisce le voci più recenti, filtrate per paziente (0 = tutti)
// e per testo.
func (s *Store) AuditList(patientID int64, q string, limit int) ([]AuditEntry, error) {
	where, args := "1=1", []any{}
	if patientID > 0 {
		where += " AND patient_id = ?"
		args = append(args, patientID)
	}
	if q != "" {
		where += " AND (username LIKE ? OR action LIKE ? OR entity LIKE ? OR details LIKE ?)"
		p := "%" + q + "%"
		args = append(args, p, p, p, p)
	}
	args = append(args, limit)
	rows, err := s.DB.Query(`SELECT id, at, username, action, entity, entity_id, patient_id, details, ip
		FROM audit_log WHERE `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Username, &e.Action, &e.Entity, &e.EntityID, &e.PatientID, &e.Details, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------- Utenti ----------

type User struct {
	ID             int64
	Username       string
	FullName       string
	PasswordHash   string
	Role           string
	Active         bool
	MustChange     bool
	FailedAttempts int
	LockedUntil    string
	LastLoginAt    string
}

func (u User) IsAdmin() bool { return u.Role == "admin" }

func (u User) RoleLabel() string {
	switch u.Role {
	case "admin":
		return "Amministratore"
	case "clinician":
		return "Medico"
	}
	return "Collaboratore"
}

const userCols = "id, username, full_name, password_hash, role, active, must_change, failed_attempts, locked_until, last_login_at"

func scanUser(r interface{ Scan(...any) error }) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Username, &u.FullName, &u.PasswordHash, &u.Role, &u.Active, &u.MustChange, &u.FailedAttempts, &u.LockedUntil, &u.LastLoginAt)
	return u, err
}

func (s *Store) UserCount() int {
	var n int
	_ = s.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	return n
}

func (s *Store) UserByUsername(name string) (User, error) {
	return scanUser(s.DB.QueryRow("SELECT "+userCols+" FROM users WHERE username = ?", name))
}

func (s *Store) UserByID(id int64) (User, error) {
	return scanUser(s.DB.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func (s *Store) Users() ([]User, error) {
	rows, err := s.DB.Query("SELECT " + userCols + " FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(a Actor, u User) (int64, error) {
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO users (username, full_name, password_hash, role, active, must_change, created_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`, u.Username, u.FullName, u.PasswordHash, u.Role, u.MustChange, Now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return audit(tx, a, "create", "user", id, 0, u.Username+" ("+u.Role+")")
	})
	return id, err
}

// SetPassword imposta la password. action è "password_reset" (da parte di un
// amministratore o da riga di comando) oppure "password_change".
func (s *Store) SetPassword(a Actor, userID int64, hash string, mustChange bool, action string) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE users SET password_hash = ?, must_change = ?, failed_attempts = 0, locked_until = '' WHERE id = ?", hash, mustChange, userID); err != nil {
			return err
		}
		return audit(tx, a, action, "user", userID, 0, "")
	})
}

func (s *Store) UpdateUser(a Actor, id int64, fullName, role string, active bool) error {
	return s.tx(func(tx *sql.Tx) error {
		if !active || role != "admin" {
			var others int
			if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = 1 AND id <> ?", id).Scan(&others); err != nil {
				return err
			}
			if others == 0 {
				return errors.New("deve restare almeno un amministratore attivo")
			}
		}
		if _, err := tx.Exec("UPDATE users SET full_name = ?, role = ?, active = ? WHERE id = ?", fullName, role, active, id); err != nil {
			return err
		}
		return audit(tx, a, "update", "user", id, 0, fmt.Sprintf("ruolo=%s attivo=%t", role, active))
	})
}

func (s *Store) LoginFailed(userID int64, lockAfter int, lockFor time.Duration) {
	_, _ = s.DB.Exec("UPDATE users SET failed_attempts = failed_attempts + 1 WHERE id = ?", userID)
	_, _ = s.DB.Exec("UPDATE users SET locked_until = ?, failed_attempts = 0 WHERE id = ? AND failed_attempts >= ?",
		time.Now().Add(lockFor).Format(TS), userID, lockAfter)
}

func (s *Store) LoginSucceeded(userID int64) {
	_, _ = s.DB.Exec("UPDATE users SET failed_attempts = 0, locked_until = '', last_login_at = ? WHERE id = ?", Now(), userID)
}

// ---------- Anagrafiche di supporto ----------

type Drug struct {
	ID         int64
	Ingredient string
	Brand      string
	MgMilli    int
	Div        domain.Divisibility
	Active     bool
}

func (d Drug) Label() string { return d.Brand + " " + domain.FormatMg(d.MgMilli) + " mg" }

func (s *Store) Drugs(onlyActive bool) ([]Drug, error) {
	q := "SELECT id, ingredient, brand, mg_milli, divisibility, active FROM drugs"
	if onlyActive {
		q += " WHERE active = 1"
	}
	rows, err := s.DB.Query(q + " ORDER BY brand, mg_milli DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Drug
	for rows.Next() {
		var d Drug
		if err := rows.Scan(&d.ID, &d.Ingredient, &d.Brand, &d.MgMilli, &d.Div, &d.Active); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Drug(id int64) (Drug, error) {
	var d Drug
	err := s.DB.QueryRow("SELECT id, ingredient, brand, mg_milli, divisibility, active FROM drugs WHERE id = ?", id).
		Scan(&d.ID, &d.Ingredient, &d.Brand, &d.MgMilli, &d.Div, &d.Active)
	return d, err
}

// SaveDrug crea (ID 0) o aggiorna un farmaco. Concentrazione e divisibilità
// non sono modificabili se il farmaco è già stato usato in un piano.
func (s *Store) SaveDrug(a Actor, d Drug) error {
	return s.tx(func(tx *sql.Tx) error {
		if d.ID == 0 {
			res, err := tx.Exec("INSERT INTO drugs (ingredient, brand, mg_milli, divisibility, active) VALUES (?, ?, ?, ?, ?)",
				d.Ingredient, d.Brand, d.MgMilli, d.Div, d.Active)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			return audit(tx, a, "create", "drug", id, 0, d.Label())
		}
		var used int
		var oldMg int
		var oldDiv string
		if err := tx.QueryRow("SELECT mg_milli, divisibility, (SELECT COUNT(*) FROM controls WHERE drug_id = drugs.id) FROM drugs WHERE id = ?", d.ID).
			Scan(&oldMg, &oldDiv, &used); err != nil {
			return err
		}
		if used > 0 && (oldMg != d.MgMilli || oldDiv != string(d.Div)) {
			return errors.New("concentrazione e divisibilità non sono modificabili per un farmaco già usato: disattivarlo e crearne uno nuovo")
		}
		if _, err := tx.Exec("UPDATE drugs SET ingredient = ?, brand = ?, mg_milli = ?, divisibility = ?, active = ? WHERE id = ?",
			d.Ingredient, d.Brand, d.MgMilli, d.Div, d.Active, d.ID); err != nil {
			return err
		}
		return audit(tx, a, "update", "drug", d.ID, 0, d.Label())
	})
}

type Indication struct {
	ID               int64
	Name             string
	Min, Max, Target domain.INR
	Active           bool
}

func (s *Store) Indications(onlyActive bool) ([]Indication, error) {
	q := "SELECT id, name, inr_min, inr_max, inr_target, active FROM indications"
	if onlyActive {
		q += " WHERE active = 1"
	}
	rows, err := s.DB.Query(q + " ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Indication
	for rows.Next() {
		var i Indication
		if err := rows.Scan(&i.ID, &i.Name, &i.Min, &i.Max, &i.Target, &i.Active); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) SaveIndication(a Actor, i Indication) error {
	return s.tx(func(tx *sql.Tx) error {
		if i.ID == 0 {
			res, err := tx.Exec("INSERT INTO indications (name, inr_min, inr_max, inr_target, active) VALUES (?, ?, ?, ?, ?)", i.Name, i.Min, i.Max, i.Target, i.Active)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			return audit(tx, a, "create", "indication", id, 0, i.Name)
		}
		if _, err := tx.Exec("UPDATE indications SET name = ?, inr_min = ?, inr_max = ?, inr_target = ?, active = ? WHERE id = ?", i.Name, i.Min, i.Max, i.Target, i.Active, i.ID); err != nil {
			return err
		}
		return audit(tx, a, "update", "indication", i.ID, 0, i.Name)
	})
}

type EventType struct {
	ID       int64
	Name     string
	Category string
	Active   bool
}

func CategoryLabel(c string) string {
	switch c {
	case "hemorrhagic":
		return "Emorragico"
	case "thrombotic":
		return "Trombotico"
	}
	return "Altro"
}

func (e EventType) CategoryLabel() string { return CategoryLabel(e.Category) }

func (s *Store) EventTypes(onlyActive bool) ([]EventType, error) {
	q := "SELECT id, name, category, active FROM event_types"
	if onlyActive {
		q += " WHERE active = 1"
	}
	rows, err := s.DB.Query(q + " ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventType
	for rows.Next() {
		var e EventType
		if err := rows.Scan(&e.ID, &e.Name, &e.Category, &e.Active); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SaveEventType(a Actor, e EventType) error {
	return s.tx(func(tx *sql.Tx) error {
		if e.ID == 0 {
			res, err := tx.Exec("INSERT INTO event_types (name, category, active) VALUES (?, ?, ?)", e.Name, e.Category, e.Active)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			return audit(tx, a, "create", "event_type", id, 0, e.Name)
		}
		if _, err := tx.Exec("UPDATE event_types SET name = ?, category = ?, active = ? WHERE id = ?", e.Name, e.Category, e.Active, e.ID); err != nil {
			return err
		}
		return audit(tx, a, "update", "event_type", e.ID, 0, e.Name)
	})
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/davidenegrini/veronatao/internal/domain"
)

// ---------- Pazienti ----------

type Patient struct {
	ID           int64
	LastName     string
	FirstName    string
	BirthDate    domain.Date
	Sex          string
	FiscalCode   string
	Phone        string
	Email        string
	Address      string
	ExternalCode string
	Notes        string
	Status       string
	// EmailAuto: a ogni stampa lo schema viene anche inviato per email.
	EmailAuto bool
	// PDFPassword protegge i PDF inviati per email a questo paziente.
	PDFPassword string
}

func (p Patient) FullName() string { return p.LastName + " " + p.FirstName }
func (p Patient) Age() int         { return p.BirthDate.AgeAt(domain.Today()) }

var StatusLabels = map[string]string{
	"active":           "Attivo",
	"suspended":        "Sospeso",
	"transferred":      "Trasferito",
	"deceased":         "Deceduto",
	"lost_to_followup": "Perso al follow-up",
}

// StatusOrder è l'ordine di presentazione degli stati.
var StatusOrder = []string{"active", "suspended", "transferred", "deceased", "lost_to_followup"}

func (p Patient) StatusLabel() string { return StatusLabels[p.Status] }

const patientCols = "id, last_name, first_name, birth_date, sex, fiscal_code, phone, email, address, external_code, notes, status, email_auto, pdf_password"

func scanPatient(r interface{ Scan(...any) error }) (Patient, error) {
	var p Patient
	err := r.Scan(&p.ID, &p.LastName, &p.FirstName, &p.BirthDate, &p.Sex, &p.FiscalCode, &p.Phone, &p.Email, &p.Address, &p.ExternalCode, &p.Notes, &p.Status, &p.EmailAuto, &p.PDFPassword)
	return p, err
}

func (s *Store) Patient(id int64) (Patient, error) {
	return scanPatient(s.DB.QueryRow("SELECT "+patientCols+" FROM patients WHERE id = ? AND deleted_at = ''", id))
}

func (s *Store) SavePatient(a Actor, p Patient) (int64, error) {
	id := p.ID
	err := s.tx(func(tx *sql.Tx) error {
		if p.ID == 0 {
			res, err := tx.Exec(`INSERT INTO patients (last_name, first_name, birth_date, sex, fiscal_code, phone, email, address, external_code, notes, email_auto, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				p.LastName, p.FirstName, p.BirthDate, p.Sex, p.FiscalCode, p.Phone, p.Email, p.Address, p.ExternalCode, p.Notes, p.EmailAuto, a.UserID, Now(), Now())
			if err != nil {
				return err
			}
			id, _ = res.LastInsertId()
			return audit(tx, a, "create", "patient", id, id, "")
		}
		if _, err := tx.Exec(`UPDATE patients SET last_name = ?, first_name = ?, birth_date = ?, sex = ?, fiscal_code = ?, phone = ?, email = ?, address = ?, external_code = ?, notes = ?, email_auto = ?, updated_at = ?
			WHERE id = ? AND deleted_at = ''`,
			p.LastName, p.FirstName, p.BirthDate, p.Sex, p.FiscalCode, p.Phone, p.Email, p.Address, p.ExternalCode, p.Notes, p.EmailAuto, Now(), p.ID); err != nil {
			return err
		}
		return audit(tx, a, "update", "patient", p.ID, p.ID, "anagrafica")
	})
	return id, err
}

type StatusChange struct {
	Date     domain.Date
	Old, New string
	Reason   string
	Username string
}

func (c StatusChange) OldLabel() string { return StatusLabels[c.Old] }
func (c StatusChange) NewLabel() string { return StatusLabels[c.New] }

func (s *Store) ChangeStatus(a Actor, patientID int64, newStatus string, date domain.Date, reason string) error {
	if _, ok := StatusLabels[newStatus]; !ok {
		return errors.New("stato non valido")
	}
	return s.tx(func(tx *sql.Tx) error {
		var old string
		if err := tx.QueryRow("SELECT status FROM patients WHERE id = ?", patientID).Scan(&old); err != nil {
			return err
		}
		if old == newStatus {
			return nil
		}
		if _, err := tx.Exec("UPDATE patients SET status = ?, updated_at = ? WHERE id = ?", newStatus, Now(), patientID); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO patient_status_changes (patient_id, old_status, new_status, date, reason, user_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			patientID, old, newStatus, date, reason, a.UserID, Now()); err != nil {
			return err
		}
		return audit(tx, a, "update", "patient_status", patientID, patientID, old+" -> "+newStatus)
	})
}

func (s *Store) StatusChanges(patientID int64) ([]StatusChange, error) {
	rows, err := s.DB.Query(`SELECT c.date, c.old_status, c.new_status, c.reason, u.username
		FROM patient_status_changes c JOIN users u ON u.id = c.user_id WHERE c.patient_id = ? ORDER BY c.id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatusChange
	for rows.Next() {
		var c StatusChange
		if err := rows.Scan(&c.Date, &c.Old, &c.New, &c.Reason, &c.Username); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PatientRow è una riga dell'elenco pazienti con i dati riassuntivi.
type PatientRow struct {
	Patient
	Indication   string
	DrugLabel    string
	WeeklyMg     string
	LastINR      domain.INR
	LastINRDate  domain.Date
	LastPos      domain.Position
	LastControl  domain.Date
	NextDate     domain.Date
	HasTreatment bool
}

// DaysToNext restituisce i giorni mancanti al prossimo controllo (negativo se
// scaduto).
func (r PatientRow) DaysToNext() int { return domain.Today().DaysUntil(r.NextDate) }

// DaysSinceINR restituisce i giorni trascorsi dall'ultima misurazione.
func (r PatientRow) DaysSinceINR() int { return r.LastINRDate.DaysUntil(domain.Today()) }

// PatientRows restituisce l'elenco dei pazienti. q filtra per cognome, nome,
// codice o data di nascita; status vuoto = tutti.
func (s *Store) PatientRows(q, status string) ([]PatientRow, error) {
	where, args := "p.deleted_at = ''", []any{}
	for _, word := range strings.Fields(q) {
		where += " AND (p.last_name LIKE ? OR p.first_name LIKE ? OR p.external_code LIKE ? OR p.fiscal_code LIKE ? OR p.birth_date = ?)"
		like := word + "%"
		bd := word
		if parts := strings.Split(word, "/"); len(parts) == 3 && len(parts[2]) == 4 {
			pad := func(v string) string {
				if len(v) == 1 {
					return "0" + v
				}
				return v
			}
			bd = parts[2] + "-" + pad(parts[1]) + "-" + pad(parts[0])
		}
		args = append(args, like, like, like, like, bd)
	}
	if status != "" {
		where += " AND p.status = ?"
		args = append(args, status)
	}
	rows, err := s.DB.Query(`
		SELECT p.id, p.last_name, p.first_name, p.birth_date, p.sex, p.fiscal_code, p.phone, p.email, p.address, p.external_code, p.notes, p.status,
		       t.id IS NOT NULL, COALESCE(i.name, ''),
		       COALESCE(d.brand, ''), COALESCE(d.mg_milli, 0),
		       CASE WHEN c.drug_id IS NULL THEN -1 ELSE c.q_mon + c.q_tue + c.q_wed + c.q_thu + c.q_fri + c.q_sat + c.q_sun END,
		       c.date, c.next_date,
		       ci.inr, ci.date, tp.inr_min, tp.inr_max
		FROM patients p
		LEFT JOIN treatments t ON t.patient_id = p.id AND t.closed_at = ''
		LEFT JOIN indications i ON i.id = t.indication_id
		LEFT JOIN controls c ON c.id = (SELECT id FROM controls WHERE patient_id = p.id AND deleted_at = '' ORDER BY date DESC, id DESC LIMIT 1)
		LEFT JOIN controls cp ON cp.id = (SELECT id FROM controls WHERE patient_id = p.id AND deleted_at = '' AND drug_id IS NOT NULL ORDER BY date DESC, id DESC LIMIT 1)
		LEFT JOIN drugs d ON d.id = COALESCE(cp.drug_id, t.drug_id)
		LEFT JOIN controls ci ON ci.id = (SELECT id FROM controls WHERE patient_id = p.id AND deleted_at = '' AND inr IS NOT NULL ORDER BY date DESC, id DESC LIMIT 1)
		LEFT JOIN target_periods tp ON tp.treatment_id = ci.treatment_id AND tp.valid_from <= ci.date AND (tp.valid_to = '' OR tp.valid_to >= ci.date)
		WHERE `+where+`
		ORDER BY p.last_name, p.first_name, p.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PatientRow
	for rows.Next() {
		var r PatientRow
		var brand string
		var mg, weekQ int
		var cDate, cNext, ciDate sql.NullString
		var inr, tmin, tmax sql.NullInt64
		var wq sql.NullInt64
		if err := rows.Scan(&r.ID, &r.LastName, &r.FirstName, &r.BirthDate, &r.Sex, &r.FiscalCode, &r.Phone, &r.Email, &r.Address, &r.ExternalCode, &r.Notes, &r.Status,
			&r.HasTreatment, &r.Indication, &brand, &mg, &wq, &cDate, &cNext, &inr, &ciDate, &tmin, &tmax); err != nil {
			return nil, err
		}
		weekQ = -1
		if wq.Valid {
			weekQ = int(wq.Int64)
		}
		if brand != "" {
			r.DrugLabel = Drug{Brand: brand, MgMilli: mg}.Label()
			if weekQ >= 0 {
				r.WeeklyMg = domain.FormatMg(domain.Quarters(weekQ).MgMilli(mg))
			}
		}
		r.LastControl, _ = domain.ParseDate(cDate.String)
		r.NextDate, _ = domain.ParseDate(cNext.String)
		if inr.Valid {
			r.LastINR = domain.INR(inr.Int64)
			r.LastINRDate, _ = domain.ParseDate(ciDate.String)
			if tmin.Valid {
				switch {
				case r.LastINR < domain.INR(tmin.Int64):
					r.LastPos = domain.PosBelow
				case r.LastINR > domain.INR(tmax.Int64):
					r.LastPos = domain.PosAbove
				default:
					r.LastPos = domain.PosInRange
				}
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- Percorsi di trattamento ----------

type Treatment struct {
	ID           int64
	PatientID    int64
	IndicationID int64
	Indication   string
	Diagnosis    string
	DrugID       int64
	OpenedAt     domain.Date
	ClosedAt     domain.Date
	SineDie      bool
	PlannedEnd   domain.Date
	CloseReason  string
	CloseNotes   string
}

func (t Treatment) Open() bool { return t.ClosedAt.IsZero() }

func (t Treatment) Duration() string {
	if t.SineDie {
		return "Sine die"
	}
	if !t.PlannedEnd.IsZero() {
		return "fino al " + t.PlannedEnd.IT()
	}
	return ""
}

var CloseReasons = []string{"Termine programmato", "Passaggio ad altro anticoagulante", "Sospensione per evento avverso", "Decesso", "Trasferimento", "Altro"}

func (s *Store) Treatments(patientID int64) ([]Treatment, error) {
	rows, err := s.DB.Query(`SELECT t.id, t.patient_id, t.indication_id, i.name, t.diagnosis, t.drug_id, t.opened_at, t.closed_at, t.sine_die, t.planned_end, t.close_reason, t.close_notes
		FROM treatments t JOIN indications i ON i.id = t.indication_id WHERE t.patient_id = ? ORDER BY t.opened_at DESC, t.id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Treatment
	for rows.Next() {
		var t Treatment
		if err := rows.Scan(&t.ID, &t.PatientID, &t.IndicationID, &t.Indication, &t.Diagnosis, &t.DrugID, &t.OpenedAt, &t.ClosedAt, &t.SineDie, &t.PlannedEnd, &t.CloseReason, &t.CloseNotes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// OpenTreatment apre un nuovo percorso con il suo range INR iniziale.
// I percorsi dello stesso paziente non possono sovrapporsi.
func (s *Store) OpenTreatment(a Actor, t Treatment, target domain.TargetPeriod) (int64, error) {
	if t.OpenedAt.IsZero() {
		return 0, errors.New("data di inizio mancante")
	}
	if target.Min <= 0 || target.Max < target.Min || (target.Target != 0 && (target.Target < target.Min || target.Target > target.Max)) {
		return 0, errors.New("range INR non valido")
	}
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow("SELECT COUNT(*) FROM treatments WHERE patient_id = ? AND (closed_at = '' OR closed_at >= ?)", t.PatientID, t.OpenedAt).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errors.New("esiste già un percorso aperto o sovrapposto a questa data")
		}
		res, err := tx.Exec(`INSERT INTO treatments (patient_id, indication_id, diagnosis, drug_id, opened_at, sine_die, planned_end, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.PatientID, t.IndicationID, t.Diagnosis, t.DrugID, t.OpenedAt, t.SineDie, t.PlannedEnd, a.UserID, Now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO target_periods (treatment_id, valid_from, inr_min, inr_max, inr_target, reason, user_id, created_at)
			VALUES (?, ?, ?, ?, ?, 'Apertura del percorso', ?, ?)`, id, t.OpenedAt, target.Min, target.Max, target.Target, a.UserID, Now()); err != nil {
			return err
		}
		return audit(tx, a, "create", "treatment", id, t.PatientID, "")
	})
	return id, err
}

// UpdateTreatment aggiorna diagnosi, indicazione, farmaco e durata del
// percorso aperto.
func (s *Store) UpdateTreatment(a Actor, t Treatment) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE treatments SET indication_id = ?, diagnosis = ?, drug_id = ?, sine_die = ?, planned_end = ? WHERE id = ? AND closed_at = ''",
			t.IndicationID, t.Diagnosis, t.DrugID, t.SineDie, t.PlannedEnd, t.ID); err != nil {
			return err
		}
		return audit(tx, a, "update", "treatment", t.ID, t.PatientID, "")
	})
}

func (s *Store) CloseTreatment(a Actor, id, patientID int64, date domain.Date, reason, notes string) error {
	return s.tx(func(tx *sql.Tx) error {
		var opened domain.Date
		if err := tx.QueryRow("SELECT opened_at FROM treatments WHERE id = ? AND patient_id = ? AND closed_at = ''", id, patientID).Scan(&opened); err != nil {
			return errors.New("percorso non trovato o già chiuso")
		}
		if date.IsZero() || date.Before(opened) {
			return errors.New("la data di chiusura non può precedere l'apertura")
		}
		var later int
		if err := tx.QueryRow("SELECT COUNT(*) FROM controls WHERE treatment_id = ? AND deleted_at = '' AND date > ?", id, date).Scan(&later); err != nil {
			return err
		}
		if later > 0 {
			return errors.New("esistono controlli successivi alla data di chiusura indicata")
		}
		if _, err := tx.Exec("UPDATE treatments SET closed_at = ?, close_reason = ?, close_notes = ? WHERE id = ?", date, reason, notes, id); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE target_periods SET valid_to = ? WHERE treatment_id = ? AND valid_to = ''", date, id); err != nil {
			return err
		}
		return audit(tx, a, "update", "treatment", id, patientID, "chiusura: "+reason)
	})
}

type TargetRow struct {
	domain.TargetPeriod
	Reason   string
	Username string
}

func (s *Store) Targets(treatmentID int64) ([]TargetRow, error) {
	rows, err := s.DB.Query(`SELECT tp.valid_from, tp.valid_to, tp.inr_min, tp.inr_max, tp.inr_target, tp.reason, u.username
		FROM target_periods tp JOIN users u ON u.id = tp.user_id WHERE tp.treatment_id = ? ORDER BY tp.valid_from DESC`, treatmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TargetRow
	for rows.Next() {
		var t TargetRow
		if err := rows.Scan(&t.From, &t.To, &t.Min, &t.Max, &t.Target, &t.Reason, &t.Username); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PatientTargets restituisce tutti i range del paziente, di ogni percorso.
func (s *Store) PatientTargets(patientID int64) ([]domain.TargetPeriod, error) {
	rows, err := s.DB.Query(`SELECT tp.valid_from, tp.valid_to, tp.inr_min, tp.inr_max, tp.inr_target
		FROM target_periods tp JOIN treatments t ON t.id = tp.treatment_id WHERE t.patient_id = ? ORDER BY tp.valid_from`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TargetPeriod
	for rows.Next() {
		var t domain.TargetPeriod
		if err := rows.Scan(&t.From, &t.To, &t.Min, &t.Max, &t.Target); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ChangeTarget chiude il range corrente il giorno prima di from e ne apre uno
// nuovo. La motivazione è obbligatoria.
func (s *Store) ChangeTarget(a Actor, treatmentID, patientID int64, from domain.Date, min, max, target domain.INR, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("la motivazione della modifica è obbligatoria")
	}
	if min <= 0 || max < min || (target != 0 && (target < min || target > max)) {
		return errors.New("range INR non valido")
	}
	return s.tx(func(tx *sql.Tx) error {
		var curFrom domain.Date
		if err := tx.QueryRow("SELECT valid_from FROM target_periods WHERE treatment_id = ? AND valid_to = ''", treatmentID).Scan(&curFrom); err != nil {
			return errors.New("nessun range corrente per questo percorso")
		}
		if from.IsZero() || !from.After(curFrom) {
			return errors.New("la data di decorrenza deve essere successiva a quella del range corrente (" + curFrom.IT() + ")")
		}
		if _, err := tx.Exec("UPDATE target_periods SET valid_to = ? WHERE treatment_id = ? AND valid_to = ''", from.AddDays(-1), treatmentID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO target_periods (treatment_id, valid_from, inr_min, inr_max, inr_target, reason, user_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, treatmentID, from, min, max, target, reason, a.UserID, Now()); err != nil {
			return err
		}
		return audit(tx, a, "update", "target", treatmentID, patientID, fmt.Sprintf("%s-%s dal %s: %s", min, max, from, reason))
	})
}

// ---------- Controlli e piani ----------

type Control struct {
	ID          int64
	PatientID   int64
	TreatmentID int64
	Date        domain.Date
	Time        string
	HasINR      bool
	INR         domain.INR
	Site        string
	DrugID      int64 // 0 = controllo senza piano
	Week        domain.Week
	FirstDay    *domain.Quarters
	PlanEnd     domain.Date
	NextDate    domain.Date
	Source      string
	Comment     string
	UserID      int64
	UserName    string
	// Campi derivati in lettura.
	DrugLabel  string
	MgMilli    int
	HasHeparin bool
}

func (c Control) HasPlan() bool { return c.DrugID != 0 }

// WeeklyMg restituisce la dose settimanale dello schema base, in mg.
func (c Control) WeeklyMg() string {
	if !c.HasPlan() {
		return ""
	}
	return domain.FormatMg(c.Week.Total().MgMilli(c.MgMilli))
}

func (c Control) SourceLabel() string {
	if !c.HasPlan() {
		return ""
	}
	if c.Source == "copied_from_previous" {
		return "copiata"
	}
	return "manuale"
}

var SiteLabels = map[string]string{"lab": "Laboratorio", "poct": "POCT", "home": "Autocontrollo"}

const controlSelect = `SELECT c.id, c.patient_id, c.treatment_id, c.date, c.time, c.inr, c.site, c.drug_id,
	c.q_mon, c.q_tue, c.q_wed, c.q_thu, c.q_fri, c.q_sat, c.q_sun, c.first_day_q, c.plan_end, c.next_date,
	c.dose_source, c.comment, c.user_id, u.full_name, COALESCE(d.brand, ''), COALESCE(d.mg_milli, 0),
	EXISTS (SELECT 1 FROM plan_days pd WHERE pd.control_id = c.id AND pd.hep_drug <> '')
	FROM controls c JOIN users u ON u.id = c.user_id LEFT JOIN drugs d ON d.id = c.drug_id`

func scanControl(r interface{ Scan(...any) error }) (Control, error) {
	var c Control
	var inr, drug, first sql.NullInt64
	var brand string
	err := r.Scan(&c.ID, &c.PatientID, &c.TreatmentID, &c.Date, &c.Time, &inr, &c.Site, &drug,
		&c.Week[0], &c.Week[1], &c.Week[2], &c.Week[3], &c.Week[4], &c.Week[5], &c.Week[6], &first, &c.PlanEnd, &c.NextDate,
		&c.Source, &c.Comment, &c.UserID, &c.UserName, &brand, &c.MgMilli, &c.HasHeparin)
	if err != nil {
		return c, err
	}
	if inr.Valid {
		c.HasINR, c.INR = true, domain.INR(inr.Int64)
	}
	c.DrugID = drug.Int64
	if first.Valid {
		q := domain.Quarters(first.Int64)
		c.FirstDay = &q
	}
	if brand != "" {
		c.DrugLabel = Drug{Brand: brand, MgMilli: c.MgMilli}.Label()
	}
	return c, nil
}

// Controls restituisce i controlli del paziente, dal più recente.
func (s *Store) Controls(patientID int64) ([]Control, error) {
	rows, err := s.DB.Query(controlSelect+" WHERE c.patient_id = ? AND c.deleted_at = '' ORDER BY c.date DESC, c.id DESC", patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Control
	for rows.Next() {
		c, err := scanControl(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Control(id int64) (Control, error) {
	return scanControl(s.DB.QueryRow(controlSelect+" WHERE c.id = ? AND c.deleted_at = ''", id))
}

// PlanDays restituisce tutti i giorni scritti da un controllo, compresi quelli
// poi sostituiti da un controllo successivo.
func (s *Store) PlanDays(controlID int64) ([]domain.PlanDay, error) {
	rows, err := s.DB.Query("SELECT date, quarters, hep_drug, hep_dose, hep_unit, hep_times FROM plan_days WHERE control_id = ? ORDER BY date", controlID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlanDay
	for rows.Next() {
		var d domain.PlanDay
		if err := rows.Scan(&d.Date, &d.Quarters, &d.Heparin.Drug, &d.Heparin.Dose, &d.Heparin.Unit, &d.Heparin.Times); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveControl registra un controllo e, se presente, il piano giorno per
// giorno, in un'unica transazione. I giorni di piani precedenti dalla data
// del nuovo piano in poi vengono marcati come sostituiti, non cancellati.
func (s *Store) SaveControl(a Actor, c Control, days []domain.PlanDay) (int64, error) {
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		var openID int64
		var opened domain.Date
		if err := tx.QueryRow("SELECT id, opened_at FROM treatments WHERE patient_id = ? AND closed_at = ''", c.PatientID).Scan(&openID, &opened); err != nil {
			return errors.New("il paziente non ha un percorso di trattamento aperto")
		}
		if c.Date.Before(opened) {
			return errors.New("la data del controllo precede l'apertura del percorso (" + opened.IT() + ")")
		}
		var last sql.NullString
		if err := tx.QueryRow("SELECT MAX(date) FROM controls WHERE patient_id = ? AND deleted_at = ''", c.PatientID).Scan(&last); err != nil {
			return err
		}
		if last.Valid && last.String >= c.Date.String() {
			return errors.New("esiste già un controllo in data uguale o successiva: i controlli si inseriscono in ordine cronologico")
		}
		if !c.HasINR && !c.HasPlan() {
			return errors.New("inserire almeno il valore di INR o il piano")
		}
		var inr, drug, first any
		if c.HasINR {
			inr = int(c.INR)
		}
		if c.FirstDay != nil {
			first = int(*c.FirstDay)
		}
		if c.HasPlan() {
			drug = c.DrugID
			var div domain.Divisibility
			if err := tx.QueryRow("SELECT divisibility FROM drugs WHERE id = ?", c.DrugID).Scan(&div); err != nil {
				return errors.New("farmaco non trovato")
			}
			if len(days) == 0 || !days[0].Date.Equal(c.Date) || !days[len(days)-1].Date.Equal(c.PlanEnd) || len(days) != c.Date.DaysUntil(c.PlanEnd)+1 {
				return errors.New("i giorni del piano non coprono esattamente il periodo indicato")
			}
			for i, d := range days {
				if !d.Date.Equal(c.Date.AddDays(i)) {
					return errors.New("giorni del piano non consecutivi")
				}
				if !d.Quarters.ValidFor(div) {
					return fmt.Errorf("dose del %s non somministrabile con questa compressa", d.Date.IT())
				}
			}
		}
		res, err := tx.Exec(`INSERT INTO controls (patient_id, treatment_id, date, time, inr, site, drug_id,
			q_mon, q_tue, q_wed, q_thu, q_fri, q_sat, q_sun, first_day_q, plan_end, next_date, dose_source, comment, user_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.PatientID, openID, c.Date, c.Time, inr, c.Site, drug,
			c.Week[0], c.Week[1], c.Week[2], c.Week[3], c.Week[4], c.Week[5], c.Week[6], first, c.PlanEnd, c.NextDate, c.Source, c.Comment, a.UserID, Now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if c.HasPlan() {
			if _, err := tx.Exec("UPDATE plan_days SET superseded_by = ? WHERE patient_id = ? AND superseded_by IS NULL AND date >= ?", id, c.PatientID, c.Date); err != nil {
				return err
			}
			for _, d := range days {
				if _, err := tx.Exec(`INSERT INTO plan_days (control_id, patient_id, date, quarters, hep_drug, hep_dose, hep_unit, hep_times)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, c.PatientID, d.Date, d.Quarters, d.Heparin.Drug, d.Heparin.Dose, d.Heparin.Unit, d.Heparin.Times); err != nil {
					return err
				}
			}
		}
		details := "controllo del " + c.Date.String()
		if c.HasPlan() {
			details += fmt.Sprintf(", piano fino al %s (%d giorni)", c.PlanEnd, len(days))
		}
		return audit(tx, a, "create", "control", id, c.PatientID, details)
	})
	return id, err
}

// DeleteControl annulla l'ultimo controllo del paziente (correzione di un
// errore di inserimento). Nulla viene cancellato fisicamente: il controllo è
// marcato come annullato e i giorni che aveva sostituito tornano in vigore.
func (s *Store) DeleteControl(a Actor, id, patientID int64, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("la motivazione dell'annullamento è obbligatoria")
	}
	return s.tx(func(tx *sql.Tx) error {
		var lastID int64
		if err := tx.QueryRow("SELECT id FROM controls WHERE patient_id = ? AND deleted_at = '' ORDER BY date DESC, id DESC LIMIT 1", patientID).Scan(&lastID); err != nil {
			return err
		}
		if lastID != id {
			return errors.New("si può annullare solo l'ultimo controllo inserito")
		}
		if _, err := tx.Exec("UPDATE controls SET deleted_at = ?, delete_reason = ? WHERE id = ?", Now(), reason, id); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE plan_days SET superseded_by = ? WHERE control_id = ? AND superseded_by IS NULL", id, id); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE plan_days SET superseded_by = NULL WHERE superseded_by = ? AND control_id <> ?", id, id); err != nil {
			return err
		}
		return audit(tx, a, "delete", "control", id, patientID, reason)
	})
}

// HeparinNames restituisce i nomi di eparina già usati in questa
// installazione, per l'autocompletamento. Non esiste un elenco precaricato.
func (s *Store) HeparinNames() []string {
	return s.distinct("SELECT DISTINCT hep_drug FROM plan_days WHERE hep_drug <> '' ORDER BY hep_drug")
}

func (s *Store) MedicationNames() []string {
	return s.distinct("SELECT DISTINCT name FROM medications ORDER BY name")
}

func (s *Store) distinct(q string) []string {
	rows, err := s.DB.Query(q)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// ---------- Farmaci concomitanti ----------

type Medication struct {
	ID          int64
	PatientID   int64
	Name        string
	Dosage      string
	StartDate   domain.Date
	EndDate     domain.Date
	Interfering bool
	Direction   string
	Notes       string
}

func (m Medication) Ongoing() bool { return m.EndDate.IsZero() }

func (m Medication) DirectionLabel() string {
	switch m.Direction {
	case "increases_inr":
		return "aumenta l'INR"
	case "decreases_inr":
		return "riduce l'INR"
	case "undefined":
		return "direzione non definita"
	}
	return ""
}

func (s *Store) Medications(patientID int64) ([]Medication, error) {
	rows, err := s.DB.Query(`SELECT id, patient_id, name, dosage, start_date, end_date, interfering, direction, notes
		FROM medications WHERE patient_id = ? AND deleted_at = '' ORDER BY end_date <> '', start_date DESC, id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Medication
	for rows.Next() {
		var m Medication
		if err := rows.Scan(&m.ID, &m.PatientID, &m.Name, &m.Dosage, &m.StartDate, &m.EndDate, &m.Interfering, &m.Direction, &m.Notes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddMedication(a Actor, m Medication) error {
	if strings.TrimSpace(m.Name) == "" || m.StartDate.IsZero() {
		return errors.New("nome del farmaco e data di inizio sono obbligatori")
	}
	if m.Interfering && m.Direction == "" {
		return errors.New("per un farmaco dichiarato interferente va indicata la direzione")
	}
	if !m.Interfering {
		m.Direction = ""
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO medications (patient_id, name, dosage, start_date, end_date, interfering, direction, notes, user_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.PatientID, strings.TrimSpace(m.Name), m.Dosage, m.StartDate, m.EndDate, m.Interfering, m.Direction, m.Notes, a.UserID, Now())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		return audit(tx, a, "create", "medication", id, m.PatientID, m.Name)
	})
}

// EndMedication registra la sospensione: valorizza la data di fine, non
// cancella la riga.
func (s *Store) EndMedication(a Actor, id, patientID int64, end domain.Date) error {
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec("UPDATE medications SET end_date = ? WHERE id = ? AND patient_id = ? AND start_date <= ?", end, id, patientID, end)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errors.New("la data di fine non può precedere l'inizio")
		}
		return audit(tx, a, "update", "medication", id, patientID, "sospeso dal "+end.String())
	})
}

func (s *Store) softDelete(a Actor, table, entity string, id, patientID int64, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("la motivazione è obbligatoria")
	}
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE "+table+" SET deleted_at = ?, delete_reason = ? WHERE id = ? AND patient_id = ?", Now(), reason, id, patientID); err != nil {
			return err
		}
		return audit(tx, a, "delete", entity, id, patientID, reason)
	})
}

func (s *Store) DeleteMedication(a Actor, id, patientID int64, reason string) error {
	return s.softDelete(a, "medications", "medication", id, patientID, reason)
}

// ---------- Eventi clinici ----------

type Event struct {
	ID          int64
	PatientID   int64
	Date        domain.Date
	TypeID      int64
	TypeName    string
	Category    string
	Description string
	Outcome     string
}

func (e Event) CategoryLabel() string { return CategoryLabel(e.Category) }

func (s *Store) Events(patientID int64) ([]Event, error) {
	rows, err := s.DB.Query(`SELECT e.id, e.patient_id, e.date, e.type_id, t.name, t.category, e.description, e.outcome
		FROM events e JOIN event_types t ON t.id = e.type_id WHERE e.patient_id = ? AND e.deleted_at = '' ORDER BY e.date DESC, e.id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.PatientID, &e.Date, &e.TypeID, &e.TypeName, &e.Category, &e.Description, &e.Outcome); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AddEvent(a Actor, e Event) error {
	if e.Date.IsZero() || e.TypeID == 0 {
		return errors.New("data e tipo dell'evento sono obbligatori")
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec("INSERT INTO events (patient_id, date, type_id, description, outcome, user_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			e.PatientID, e.Date, e.TypeID, e.Description, e.Outcome, a.UserID, Now())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		return audit(tx, a, "create", "event", id, e.PatientID, "")
	})
}

func (s *Store) DeleteEvent(a Actor, id, patientID int64, reason string) error {
	return s.softDelete(a, "events", "event", id, patientID, reason)
}

// ---------- Documenti stampati ----------

type Printed struct {
	ID           int64
	PatientID    int64
	ControlID    int64
	SHA256       string
	PeriodFrom   domain.Date
	PeriodTo     domain.Date
	TemplateCode string
	Username     string
	CreatedAt    string
}

func (s *Store) SavePrinted(a Actor, p Printed, pdf []byte) (int64, error) {
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO printed_documents (patient_id, control_id, sha256, period_from, period_to, template_code, pdf, user_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.PatientID, p.ControlID, p.SHA256, p.PeriodFrom, p.PeriodTo, p.TemplateCode, pdf, a.UserID, Now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return audit(tx, a, "print", "control", p.ControlID, p.PatientID, "documento "+fmt.Sprint(id)+" sha256 "+p.SHA256)
	})
	return id, err
}

func (s *Store) PrintedList(patientID int64) ([]Printed, error) {
	rows, err := s.DB.Query(`SELECT d.id, d.patient_id, d.control_id, d.sha256, d.period_from, d.period_to, d.template_code, u.username, d.created_at
		FROM printed_documents d JOIN users u ON u.id = d.user_id WHERE d.patient_id = ? ORDER BY d.id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Printed
	for rows.Next() {
		var p Printed
		if err := rows.Scan(&p.ID, &p.PatientID, &p.ControlID, &p.SHA256, &p.PeriodFrom, &p.PeriodTo, &p.TemplateCode, &p.Username, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PrintedPDF(id int64) (patientID int64, pdf []byte, err error) {
	err = s.DB.QueryRow("SELECT patient_id, pdf FROM printed_documents WHERE id = ?", id).Scan(&patientID, &pdf)
	return
}

// HeparinOngoing restituisce i pazienti con eparina prevista da oggi in poi
// nei giorni di piano in vigore.
func (s *Store) HeparinOngoing(today domain.Date) map[int64]bool {
	out := map[int64]bool{}
	rows, err := s.DB.Query("SELECT DISTINCT patient_id FROM plan_days WHERE superseded_by IS NULL AND hep_drug <> '' AND date >= ?", today)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// ---------- Invio per email ----------

// SetPDFPassword imposta la password che protegge i PDF inviati al paziente.
// La password non finisce nel registro delle attività.
func (s *Store) SetPDFPassword(a Actor, patientID int64, password string) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE patients SET pdf_password = ?, updated_at = ? WHERE id = ?", password, Now(), patientID); err != nil {
			return err
		}
		return audit(tx, a, "update", "patient", patientID, patientID, "password dei PDF inviati per email")
	})
}

type EmailSend struct {
	DocumentID int64
	Recipient  string
	OK         bool
	Error      string
	Username   string
	CreatedAt  string
}

// LogEmailSend annota un tentativo di invio, riuscito o fallito.
func (s *Store) LogEmailSend(a Actor, patientID, documentID int64, recipient string, sendErr error) {
	msg, details := "", "documento "+fmt.Sprint(documentID)+" inviato a "+recipient
	if sendErr != nil {
		msg = sendErr.Error()
		details = "invio del documento " + fmt.Sprint(documentID) + " a " + recipient + " non riuscito"
	}
	_ = s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO email_sends (patient_id, document_id, recipient, ok, error, user_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			patientID, documentID, recipient, sendErr == nil, msg, a.UserID, Now()); err != nil {
			return err
		}
		return audit(tx, a, "email", "document", documentID, patientID, details)
	})
}

func (s *Store) EmailSends(patientID int64) ([]EmailSend, error) {
	rows, err := s.DB.Query(`SELECT e.document_id, e.recipient, e.ok, e.error, u.username, e.created_at
		FROM email_sends e JOIN users u ON u.id = e.user_id WHERE e.patient_id = ? ORDER BY e.id DESC LIMIT 50`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmailSend
	for rows.Next() {
		var e EmailSend
		if err := rows.Scan(&e.DocumentID, &e.Recipient, &e.OK, &e.Error, &e.Username, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastPrinted restituisce l'ultimo documento stampato per un controllo.
func (s *Store) LastPrinted(controlID int64) (id int64, err error) {
	err = s.DB.QueryRow("SELECT id FROM printed_documents WHERE control_id = ? ORDER BY id DESC LIMIT 1", controlID).Scan(&id)
	return
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/davidenegrini/veronatao/internal/domain"
)

type fixture struct {
	s   *Store
	a   Actor
	pid int64
}

func setup(t *testing.T) fixture {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	uid, err := s.CreateUser(Actor{}, User{Username: "medico", FullName: "Dott. Prova", PasswordHash: "x", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	a := Actor{UserID: uid, Username: "medico"}
	pid, err := s.SavePatient(a, Patient{LastName: "ROSSI", FirstName: "MARIA", BirthDate: domain.MustDate("1940-12-09")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenTreatment(a, Treatment{PatientID: pid, IndicationID: 1, DrugID: 1, OpenedAt: domain.MustDate("2020-01-01"), SineDie: true},
		domain.TargetPeriod{Min: 200, Max: 300, Target: 250}); err != nil {
		t.Fatal(err)
	}
	return fixture{s, a, pid}
}

func (f fixture) plan(t *testing.T, date, next string, week domain.Week, inr int) (int64, error) {
	t.Helper()
	c := Control{PatientID: f.pid, Date: domain.MustDate(date), NextDate: domain.MustDate(next), DrugID: 1, Week: week, Site: "lab", Source: "manual"}
	if inr > 0 {
		c.HasINR, c.INR = true, domain.INR(inr)
	}
	c.PlanEnd = c.NextDate.AddDays(-1)
	return f.s.SaveControl(f.a, c, domain.BuildDays(c.Date, c.PlanEnd, week, nil))
}

func activeDays(t *testing.T, f fixture) map[string]int {
	t.Helper()
	rows, err := f.s.DB.Query("SELECT date, quarters FROM plan_days WHERE patient_id = ? AND superseded_by IS NULL", f.pid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var q int
		if err := rows.Scan(&d, &q); err != nil {
			t.Fatal(err)
		}
		out[d] = q
	}
	return out
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	f := setup(t)
	if _, err := f.s.DB.Exec("UPDATE audit_log SET details = 'x'"); err == nil {
		t.Error("UPDATE su audit_log accettato")
	}
	if _, err := f.s.DB.Exec("DELETE FROM audit_log"); err == nil {
		t.Error("DELETE su audit_log accettato")
	}
	var n int
	f.s.DB.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&n)
	if n < 3 {
		t.Errorf("voci di registro attese per utente, paziente e percorso: %d", n)
	}
}

func TestSchemaHasNoTimestampDates(t *testing.T) {
	if regexp.MustCompile(`(?i)\b(timestamp|datetime|timestamptz)\b`).MatchString(schemaSQL) {
		t.Error("lo schema contiene colonne con ora o fuso: le date cliniche devono essere solo AAAA-MM-GG")
	}
}

// Per ogni giorno della settimana di inizio, e attraverso cambio d'ora,
// fine anno e 29 febbraio, ciò che è salvato coincide giorno per giorno con
// lo schema inserito.
func TestPlanDaysMatchWeekEndToEnd(t *testing.T) {
	week := domain.Week{1, 2, 3, 4, 5, 6, 7}
	starts := []string{"2026-03-23", "2026-03-24", "2026-03-25", "2026-03-26", "2026-03-27", "2026-03-28", "2026-03-29",
		"2026-10-20", "2026-12-20", "2028-02-20"}
	for _, start := range starts {
		f := setup(t)
		id, err := f.plan(t, start, domain.MustDate(start).AddDays(45).String(), week, 250)
		if err != nil {
			t.Fatal(start, err)
		}
		days, err := f.s.PlanDays(id)
		if err != nil || len(days) != 45 {
			t.Fatal(start, len(days), err)
		}
		for i, d := range days {
			want := domain.MustDate(start).AddDays(i)
			if !d.Date.Equal(want) || d.Quarters != week[want.Weekday()] {
				t.Errorf("inizio %s: giorno %s salvato come %s con %d quarti", start, want, d.Date, d.Quarters)
			}
		}
		// La griglia usata da schermo e stampa riporta gli stessi valori nella colonna giusta.
		seen := 0
		for _, w := range domain.CalendarWeeks(days, domain.MustDate(start).AddDays(45)) {
			for col, cell := range w {
				if cell.Day != nil {
					seen++
					if cell.Day.Quarters != week[col] {
						t.Errorf("griglia: %s in colonna %d con %d quarti", cell.Date, col, cell.Day.Quarters)
					}
				}
			}
		}
		if seen != 45 {
			t.Errorf("giorni in griglia: %d", seen)
		}
	}
}

func TestNewPlanSupersedesAndDeleteRestores(t *testing.T) {
	f := setup(t)
	a := domain.Week{4, 4, 4, 4, 4, 4, 4}
	b := domain.Week{2, 2, 2, 2, 2, 2, 2}
	if _, err := f.plan(t, "2026-01-01", "2026-01-29", a, 250); err != nil {
		t.Fatal(err)
	}
	// Il paziente torna prima del previsto: il nuovo piano sostituisce i giorni restanti.
	id2, err := f.plan(t, "2026-01-15", "2026-02-12", b, 310)
	if err != nil {
		t.Fatal(err)
	}
	days := activeDays(t, f)
	if len(days) != 14+28 || days["2026-01-14"] != 4 || days["2026-01-15"] != 2 || days["2026-02-11"] != 2 {
		t.Fatalf("giorni in vigore dopo il secondo piano: %d %v", len(days), days["2026-01-15"])
	}
	var total int
	f.s.DB.QueryRow("SELECT COUNT(*) FROM plan_days").Scan(&total)
	if total != 28+28 {
		t.Errorf("nessun giorno deve essere cancellato fisicamente: %d", total)
	}
	// Annullamento: serve la motivazione, vale solo per l'ultimo controllo.
	if err := f.s.DeleteControl(f.a, id2, f.pid, " "); err == nil {
		t.Error("annullamento senza motivazione accettato")
	}
	if err := f.s.DeleteControl(f.a, id2-1, f.pid, "errore"); err == nil {
		t.Error("annullato un controllo che non è l'ultimo")
	}
	if err := f.s.DeleteControl(f.a, id2, f.pid, "errore di inserimento"); err != nil {
		t.Fatal(err)
	}
	days = activeDays(t, f)
	if len(days) != 28 || days["2026-01-20"] != 4 {
		t.Errorf("il piano precedente non è tornato in vigore: %d", len(days))
	}
	if cs, _ := f.s.Controls(f.pid); len(cs) != 1 {
		t.Errorf("controlli visibili: %d", len(cs))
	}
}

func TestControlValidation(t *testing.T) {
	f := setup(t)
	w := domain.Week{4, 4, 4, 4, 4, 4, 4}
	if _, err := f.plan(t, "2026-03-10", "2026-03-20", w, 250); err != nil {
		t.Fatal(err)
	}
	if _, err := f.plan(t, "2026-03-10", "2026-03-20", w, 250); err == nil {
		t.Error("due controlli nella stessa data")
	}
	if _, err := f.plan(t, "2026-03-01", "2026-03-05", w, 250); err == nil {
		t.Error("controllo precedente all'ultimo accettato")
	}
	// Giorni che non coprono il periodo.
	c := Control{PatientID: f.pid, Date: domain.MustDate("2026-04-01"), NextDate: domain.MustDate("2026-04-11"), PlanEnd: domain.MustDate("2026-04-10"), DrugID: 1, Week: w, Site: "lab", Source: "manual"}
	days := domain.BuildDays(c.Date, c.PlanEnd, w, nil)
	if _, err := f.s.SaveControl(f.a, c, days[:5]); err == nil {
		t.Error("piano con giorni mancanti accettato")
	}
	// Granularità: Sintrom 1 mg (id 3) non è divisibile.
	c.DrugID = 3
	days[2].Quarters = 3
	if _, err := f.s.SaveControl(f.a, c, days); err == nil {
		t.Error("tre quarti di una compressa non divisibile accettati")
	}
	// Né INR né piano.
	if _, err := f.s.SaveControl(f.a, Control{PatientID: f.pid, Date: domain.MustDate("2026-04-01"), Site: "lab", Source: "manual"}, nil); err == nil {
		t.Error("controllo vuoto accettato")
	}
	// Senza percorso aperto.
	trs, _ := f.s.Treatments(f.pid)
	if err := f.s.CloseTreatment(f.a, trs[0].ID, f.pid, domain.MustDate("2026-03-31"), "Altro", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.plan(t, "2026-04-01", "2026-04-11", w, 250); err == nil {
		t.Error("controllo senza percorso aperto accettato")
	}
}

func TestZeroDoseIsStoredAsRow(t *testing.T) {
	f := setup(t)
	id, err := f.plan(t, "2026-05-04", "2026-05-11", domain.Week{4, 0, 4, 0, 4, 0, 4}, 0)
	if err != nil {
		t.Fatal(err)
	}
	days, _ := f.s.PlanDays(id)
	if len(days) != 7 || days[1].Quarters != 0 || days[1].Quarters.Words() != "NIENTE" {
		t.Errorf("il giorno a dose zero deve essere una riga esplicita: %+v", days)
	}
}

func TestTreatmentsAndTargets(t *testing.T) {
	f := setup(t)
	tr := Treatment{PatientID: f.pid, IndicationID: 1, DrugID: 1, OpenedAt: domain.MustDate("2021-01-01")}
	tg := domain.TargetPeriod{Min: 200, Max: 300}
	if _, err := f.s.OpenTreatment(f.a, tr, tg); err == nil {
		t.Error("secondo percorso aperto accettato")
	}
	trs, _ := f.s.Treatments(f.pid)
	if err := f.s.ChangeTarget(f.a, trs[0].ID, f.pid, domain.MustDate("2026-01-06"), 250, 350, 0, ""); err == nil {
		t.Error("modifica del range senza motivazione accettata")
	}
	if err := f.s.ChangeTarget(f.a, trs[0].ID, f.pid, domain.MustDate("2026-01-06"), 250, 350, 0, "nuova indicazione"); err != nil {
		t.Fatal(err)
	}
	periods, _ := f.s.PatientTargets(f.pid)
	if len(periods) != 2 || periods[0].To.String() != "2026-01-05" {
		t.Fatalf("periodi: %+v", periods)
	}
	if domain.PositionOf(320, domain.MustDate("2026-01-05"), periods) != domain.PosAbove || domain.PositionOf(320, domain.MustDate("2026-01-06"), periods) != domain.PosInRange {
		t.Error("il valore va confrontato con il range vigente alla sua data")
	}
	if err := f.s.CloseTreatment(f.a, trs[0].ID, f.pid, domain.MustDate("2026-06-30"), "Termine programmato", ""); err != nil {
		t.Fatal(err)
	}
	tr.OpenedAt = domain.MustDate("2026-06-30")
	if _, err := f.s.OpenTreatment(f.a, tr, tg); err == nil {
		t.Error("percorso sovrapposto al precedente accettato")
	}
	tr.OpenedAt = domain.MustDate("2026-07-01")
	if _, err := f.s.OpenTreatment(f.a, tr, tg); err != nil {
		t.Error("percorso successivo rifiutato:", err)
	}
}

func TestMedicationDirectionRequired(t *testing.T) {
	f := setup(t)
	m := Medication{PatientID: f.pid, Name: "Farmaco X", StartDate: domain.MustDate("2026-01-01"), Interfering: true}
	if err := f.s.AddMedication(f.a, m); err == nil {
		t.Error("interferente senza direzione accettato")
	}
	m.Direction = "increases_inr"
	if err := f.s.AddMedication(f.a, m); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DB.Exec("UPDATE medications SET direction = ''"); err == nil {
		t.Error("il vincolo dell'archivio deve impedire interferenza senza direzione")
	}
	ms, _ := f.s.Medications(f.pid)
	if err := f.s.EndMedication(f.a, ms[0].ID, f.pid, domain.MustDate("2026-02-01")); err != nil {
		t.Fatal(err)
	}
	if ms, _ = f.s.Medications(f.pid); len(ms) != 1 || ms[0].Ongoing() {
		t.Error("la sospensione valorizza la data di fine e conserva la riga")
	}
}

func TestExposureDays(t *testing.T) {
	d := domain.MustDate
	from, to, today := d("2026-01-01"), d("2026-12-31"), d("2026-06-30")
	cases := []struct {
		opened, closed string
		want           int
	}{
		{"2026-01-01", "2026-01-10", 10}, // chiuso, interamente nel periodo
		{"2025-12-01", "2026-01-10", 10}, // iniziato prima del periodo
		{"2026-06-01", "", 30},           // aperto: fino a oggi
		{"2025-01-01", "2025-12-31", 0},  // tutto prima
		{"2027-01-01", "", 0},            // tutto dopo
	}
	for _, c := range cases {
		closed, _ := domain.ParseDate(c.closed)
		if got := ExposureDays(d(c.opened), closed, from, to, today); got != c.want {
			t.Errorf("%s..%s: %d, atteso %d", c.opened, c.closed, got, c.want)
		}
	}
}

func TestStatsAndExport(t *testing.T) {
	f := setup(t)
	w := domain.Week{4, 4, 4, 4, 4, 4, 4}
	f.plan(t, "2026-01-01", "2026-01-11", w, 250)
	f.plan(t, "2026-01-11", "2026-01-21", w, 280)
	if err := f.s.AddEvent(f.a, Event{PatientID: f.pid, Date: domain.MustDate("2026-01-05"), TypeID: 2}); err != nil {
		t.Fatal(err)
	}
	st, err := f.s.Stats(domain.MustDate("2026-01-01"), domain.MustDate("2026-12-31"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Controls != 2 || st.INRInRange != 2 || st.TTR.In != 10 || st.Hemorrhagic != 1 || st.ExposureDays == 0 {
		t.Errorf("statistiche: %+v", st)
	}
	var sb strings.Builder
	n, err := f.s.ExportControlsCSV(&sb, true)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if strings.Contains(sb.String(), "ROSSI") || strings.Contains(sb.String(), "1940-12-09") || !strings.Contains(sb.String(), "P00001;1940") {
		t.Errorf("l'estrazione pseudonimizzata contiene identificativi diretti:\n%s", sb.String())
	}
	sb.Reset()
	f.s.ExportControlsCSV(&sb, false)
	if !strings.Contains(sb.String(), "ROSSI") {
		t.Error("estrazione nominativa senza nome")
	}
}

// Un archivio creato dalla versione precedente viene aggiornato senza
// perdere dati.
func TestMigrationFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	// Si ricostruisce un archivio alla versione 1 con un paziente.
	old := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaSQL + `; PRAGMA user_version = 1;
		INSERT INTO users (username, full_name, password_hash, role, created_at) VALUES ('u', 'U', 'x', 'admin', '2026-01-01 00:00:00');
		INSERT INTO patients (last_name, first_name, birth_date, created_by, created_at, updated_at) VALUES ('BIANCHI', 'LUCA', '1950-01-01', 1, 'x', 'x');`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err = Open(old)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Patient(1)
	if err != nil || p.LastName != "BIANCHI" || p.EmailAuto || p.PDFPassword != "" {
		t.Fatalf("paziente dopo l'aggiornamento: %+v %v", p, err)
	}
	if s.Setting("smtp_port") != "587" || s.Setting("email_enabled") != "0" {
		t.Error("impostazioni di posta non create, o invio attivo di default")
	}
	var v int
	s.DB.QueryRow("PRAGMA user_version").Scan(&v)
	if v != SchemaVersion {
		t.Errorf("versione dello schema: %d", v)
	}
}

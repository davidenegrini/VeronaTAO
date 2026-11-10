// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/davidenegrini/veronatao/internal/domain"
)

// PatientTTR calcola il tempo in range del paziente nel periodo indicato
// (date zero = nessun limite).
func (s *Store) PatientTTR(patientID int64, from, to domain.Date) (domain.TTR, error) {
	ms, err := s.measurements(patientID)
	if err != nil {
		return domain.TTR{}, err
	}
	targets, err := s.PatientTargets(patientID)
	if err != nil {
		return domain.TTR{}, err
	}
	return domain.Rosendaal(ms, targets, from, to), nil
}

func (s *Store) measurements(patientID int64) ([]domain.Measurement, error) {
	rows, err := s.DB.Query("SELECT date, inr FROM controls WHERE patient_id = ? AND deleted_at = '' AND inr IS NOT NULL ORDER BY date", patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Measurement
	for rows.Next() {
		var m domain.Measurement
		if err := rows.Scan(&m.Date, &m.Value); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Count è una voce di distribuzione.
type Count struct {
	Label string
	N     int
}

// Stats raccoglie gli indicatori retrospettivi di un periodo. Il software
// espone i numeri e non li interpreta.
type Stats struct {
	From, To       domain.Date
	ByStatus       []Count
	ByIndication   []Count
	NewPatients    int
	Controls       int
	INRCount       int
	INRInRange     int
	INRWithTarget  int
	TTR            domain.TTR
	TTRPatients    int
	ExposureDays   int
	Hemorrhagic    int
	Thrombotic     int
	OtherEvents    int
	MeanIntervalD  float64
	IntervalsCount int
}

func (s Stats) PatientYears() float64 { return float64(s.ExposureDays) / 365.25 }

func rate(n int, py float64) string {
	if py <= 0 {
		return "n.d."
	}
	return strconv.FormatFloat(float64(n)*100/py, 'f', 1, 64)
}

// Tassi per 100 anni-paziente.
func (s Stats) HemorrhagicRate() string { return rate(s.Hemorrhagic, s.PatientYears()) }
func (s Stats) ThromboticRate() string  { return rate(s.Thrombotic, s.PatientYears()) }

func (s Stats) InRangePct() string {
	if s.INRWithTarget == 0 {
		return "n.d."
	}
	return strconv.Itoa((s.INRInRange*100+s.INRWithTarget/2)/s.INRWithTarget) + "%"
}

// ExposureDays restituisce i giorni di un percorso compresi nel periodo
// [from, to] (estremi inclusi). Un percorso aperto è considerato fino a
// today.
func ExposureDays(opened, closed, from, to, today domain.Date) int {
	end := closed
	if end.IsZero() {
		end = today
	}
	if end.After(to) {
		end = to
	}
	start := opened
	if start.Before(from) {
		start = from
	}
	if end.Before(start) {
		return 0
	}
	return start.DaysUntil(end) + 1
}

func (s *Store) Stats(from, to domain.Date) (Stats, error) {
	st := Stats{From: from, To: to}
	today := domain.Today()

	counts := func(q string, label func(string) string, args ...any) ([]Count, error) {
		rows, err := s.DB.Query(q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Count
		for rows.Next() {
			var c Count
			if err := rows.Scan(&c.Label, &c.N); err != nil {
				return nil, err
			}
			if label != nil {
				c.Label = label(c.Label)
			}
			out = append(out, c)
		}
		return out, rows.Err()
	}
	var err error
	if st.ByStatus, err = counts("SELECT status, COUNT(*) FROM patients WHERE deleted_at = '' GROUP BY status ORDER BY COUNT(*) DESC",
		func(k string) string { return StatusLabels[k] }); err != nil {
		return st, err
	}
	if st.ByIndication, err = counts(`SELECT i.name, COUNT(*) FROM treatments t JOIN indications i ON i.id = t.indication_id
		JOIN patients p ON p.id = t.patient_id WHERE p.deleted_at = '' AND t.opened_at <= ? AND (t.closed_at = '' OR t.closed_at >= ?)
		GROUP BY i.name ORDER BY COUNT(*) DESC`, nil, to, from); err != nil {
		return st, err
	}
	_ = s.DB.QueryRow("SELECT COUNT(*) FROM patients WHERE deleted_at = '' AND substr(created_at, 1, 10) BETWEEN ? AND ?", from, to).Scan(&st.NewPatients)

	// Tempo di esposizione: giorni coperti dai percorsi nel periodo.
	rows, err := s.DB.Query(`SELECT t.opened_at, t.closed_at FROM treatments t JOIN patients p ON p.id = t.patient_id WHERE p.deleted_at = ''`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var o, c domain.Date
		if err := rows.Scan(&o, &c); err != nil {
			rows.Close()
			return st, err
		}
		st.ExposureDays += ExposureDays(o, c, from, to, today)
	}
	rows.Close()

	// Eventi nel periodo, per categoria.
	ev, err := counts(`SELECT t.category, COUNT(*) FROM events e JOIN event_types t ON t.id = e.type_id JOIN patients p ON p.id = e.patient_id
		WHERE e.deleted_at = '' AND p.deleted_at = '' AND e.date BETWEEN ? AND ? GROUP BY t.category`, nil, from, to)
	if err != nil {
		return st, err
	}
	for _, c := range ev {
		switch c.Label {
		case "hemorrhagic":
			st.Hemorrhagic = c.N
		case "thrombotic":
			st.Thrombotic = c.N
		default:
			st.OtherEvents += c.N
		}
	}

	// Misurazioni e range di tutti i pazienti, raggruppati in memoria.
	targets := map[int64][]domain.TargetPeriod{}
	rows, err = s.DB.Query(`SELECT t.patient_id, tp.valid_from, tp.valid_to, tp.inr_min, tp.inr_max, tp.inr_target
		FROM target_periods tp JOIN treatments t ON t.id = tp.treatment_id`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var pid int64
		var t domain.TargetPeriod
		if err := rows.Scan(&pid, &t.From, &t.To, &t.Min, &t.Max, &t.Target); err != nil {
			rows.Close()
			return st, err
		}
		targets[pid] = append(targets[pid], t)
	}
	rows.Close()

	type ctl struct {
		date   domain.Date
		hasINR bool
		inr    domain.INR
	}
	byPatient := map[int64][]ctl{}
	rows, err = s.DB.Query(`SELECT c.patient_id, c.date, c.inr FROM controls c JOIN patients p ON p.id = c.patient_id
		WHERE c.deleted_at = '' AND p.deleted_at = '' ORDER BY c.patient_id, c.date`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var pid int64
		var c ctl
		var inr *int64
		if err := rows.Scan(&pid, &c.date, &inr); err != nil {
			rows.Close()
			return st, err
		}
		if inr != nil {
			c.hasINR, c.inr = true, domain.INR(*inr)
		}
		byPatient[pid] = append(byPatient[pid], c)
	}
	rows.Close()

	var intervalSum int
	for pid, cs := range byPatient {
		var ms, inPeriod []domain.Measurement
		for i, c := range cs {
			within := !c.date.Before(from) && !c.date.After(to)
			if within {
				st.Controls++
				if i > 0 {
					intervalSum += cs[i-1].date.DaysUntil(c.date)
					st.IntervalsCount++
				}
			}
			if c.hasINR {
				m := domain.Measurement{Date: c.date, Value: c.inr}
				ms = append(ms, m)
				if within {
					st.INRCount++
					inPeriod = append(inPeriod, m)
				}
			}
		}
		in, tot := domain.InRangeCount(inPeriod, targets[pid])
		st.INRInRange += in
		st.INRWithTarget += tot
		t := domain.Rosendaal(ms, targets[pid], from, to)
		if t.Days() > 0 {
			st.TTRPatients++
			st.TTR.Below += t.Below
			st.TTR.In += t.In
			st.TTR.Above += t.Above
		}
	}
	if st.IntervalsCount > 0 {
		st.MeanIntervalD = float64(intervalSum) / float64(st.IntervalsCount)
	}
	return st, nil
}

// ExportControlsCSV scrive tutti i controlli in CSV. In modalità
// pseudonimizzata il paziente è sostituito da un codice progressivo e gli
// identificativi diretti sono omessi (resta il solo anno di nascita).
func (s *Store) ExportControlsCSV(w io.Writer, pseudonymized bool) (int, error) {
	rows, err := s.DB.Query(`SELECT p.id, p.last_name, p.first_name, p.birth_date, p.sex, p.fiscal_code, p.external_code, p.status,
		i.name, c.date, c.inr, COALESCE(d.brand, ''), COALESCE(d.mg_milli, 0), c.drug_id IS NOT NULL,
		c.q_mon, c.q_tue, c.q_wed, c.q_thu, c.q_fri, c.q_sat, c.q_sun, c.plan_end, c.next_date,
		COALESCE((SELECT inr_min FROM target_periods tp WHERE tp.treatment_id = c.treatment_id AND tp.valid_from <= c.date AND (tp.valid_to = '' OR tp.valid_to >= c.date)), 0),
		COALESCE((SELECT inr_max FROM target_periods tp WHERE tp.treatment_id = c.treatment_id AND tp.valid_from <= c.date AND (tp.valid_to = '' OR tp.valid_to >= c.date)), 0),
		EXISTS (SELECT 1 FROM plan_days pd WHERE pd.control_id = c.id AND pd.hep_drug <> '')
		FROM controls c JOIN patients p ON p.id = c.patient_id JOIN treatments t ON t.id = c.treatment_id JOIN indications i ON i.id = t.indication_id
		LEFT JOIN drugs d ON d.id = c.drug_id
		WHERE c.deleted_at = '' AND p.deleted_at = '' ORDER BY p.id, c.date`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	head := []string{"codice"}
	if !pseudonymized {
		head = append(head, "cognome", "nome", "data_nascita", "codice_fiscale", "codice_esterno")
	} else {
		head = append(head, "anno_nascita")
	}
	head = append(head, "sesso", "stato", "indicazione", "data_controllo", "inr", "range_min", "range_max", "farmaco",
		"dose_settimanale_mg", "lun", "mar", "mer", "gio", "ven", "sab", "dom", "fine_piano", "prossimo_controllo", "eparina")
	_ = cw.Write(head)
	pseudo := map[int64]int{}
	n := 0
	for rows.Next() {
		var pid int64
		var last, first, sex, cf, ext, status, ind, brand string
		var birth, date, planEnd, next domain.Date
		var inr *int64
		var mg, tmin, tmax int
		var hasPlan, hep bool
		var w domain.Week
		if err := rows.Scan(&pid, &last, &first, &birth, &sex, &cf, &ext, &status, &ind, &date, &inr, &brand, &mg, &hasPlan,
			&w[0], &w[1], &w[2], &w[3], &w[4], &w[5], &w[6], &planEnd, &next, &tmin, &tmax, &hep); err != nil {
			return n, err
		}
		var rec []string
		if pseudonymized {
			if _, ok := pseudo[pid]; !ok {
				pseudo[pid] = len(pseudo) + 1
			}
			rec = []string{fmt.Sprintf("P%05d", pseudo[pid]), strconv.Itoa(birth.Year())}
		} else {
			rec = []string{strconv.FormatInt(pid, 10), last, first, birth.String(), cf, ext}
		}
		inrS := ""
		if inr != nil {
			inrS = domain.INR(*inr).String()
		}
		opt := func(v int) string {
			if v == 0 {
				return ""
			}
			return domain.INR(v).String()
		}
		rec = append(rec, sex, StatusLabels[status], ind, date.String(), inrS, opt(tmin), opt(tmax))
		if hasPlan {
			rec = append(rec, Drug{Brand: brand, MgMilli: mg}.Label(), domain.FormatMg(w.Total().MgMilli(mg)))
			for _, q := range w {
				rec = append(rec, strconv.Itoa(int(q)))
			}
		} else {
			rec = append(rec, "", "", "", "", "", "", "", "", "")
		}
		hepS := "no"
		if hep {
			hepS = "si"
		}
		rec = append(rec, planEnd.String(), next.String(), hepS)
		_ = cw.Write(rec)
		n++
	}
	cw.Flush()
	if err := rows.Err(); err != nil {
		return n, err
	}
	return n, cw.Error()
}

// PatientExport raccoglie tutti i dati di un paziente in forma leggibile,
// per l'esercizio dei diritti dell'interessato.
func (s *Store) PatientExport(patientID int64) (map[string]any, error) {
	p, err := s.Patient(patientID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"esportato_il": Now(),
		"paziente": map[string]any{
			"cognome": p.LastName, "nome": p.FirstName, "data_nascita": p.BirthDate.String(), "sesso": p.Sex,
			"codice_fiscale": p.FiscalCode, "telefono": p.Phone, "email": p.Email, "indirizzo": p.Address,
			"codice_esterno": p.ExternalCode, "note": p.Notes, "stato": p.StatusLabel(),
		},
	}
	trs, err := s.Treatments(patientID)
	if err != nil {
		return nil, err
	}
	var tl []map[string]any
	for _, t := range trs {
		tgs, _ := s.Targets(t.ID)
		var ranges []map[string]any
		for _, g := range tgs {
			ranges = append(ranges, map[string]any{"dal": g.From.String(), "al": g.To.String(), "range": g.Label(), "motivazione": g.Reason})
		}
		tl = append(tl, map[string]any{"indicazione": t.Indication, "diagnosi": t.Diagnosis, "apertura": t.OpenedAt.String(),
			"chiusura": t.ClosedAt.String(), "motivo_chiusura": t.CloseReason, "range_inr": ranges})
	}
	out["percorsi"] = tl
	cs, err := s.Controls(patientID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Date.Before(cs[j].Date) })
	var cl []map[string]any
	for _, c := range cs {
		m := map[string]any{"data": c.Date.String(), "prossimo_controllo": c.NextDate.String(), "commento": c.Comment, "operatore": c.UserName}
		if c.HasINR {
			m["inr"] = c.INR.String()
		}
		if c.HasPlan() {
			m["farmaco"] = c.DrugLabel
			m["dose_settimanale_mg"] = c.WeeklyMg()
			days, _ := s.PlanDays(c.ID)
			var dl []map[string]any
			for _, d := range days {
				dm := map[string]any{"data": d.Date.String(), "quarti_di_compressa": int(d.Quarters)}
				if d.Heparin.Present() {
					dm["eparina"] = fmt.Sprintf("%s %s %s x %d/die", d.Heparin.Drug, d.Heparin.Dose, d.Heparin.Unit, d.Heparin.Times)
				}
				dl = append(dl, dm)
			}
			m["giorni"] = dl
		}
		cl = append(cl, m)
	}
	out["controlli"] = cl
	meds, _ := s.Medications(patientID)
	var ml []map[string]any
	for _, m := range meds {
		ml = append(ml, map[string]any{"farmaco": m.Name, "posologia": m.Dosage, "dal": m.StartDate.String(), "al": m.EndDate.String(),
			"interferente": m.Interfering, "direzione": m.DirectionLabel(), "note": m.Notes})
	}
	out["farmaci_concomitanti"] = ml
	evs, _ := s.Events(patientID)
	var el []map[string]any
	for _, e := range evs {
		el = append(el, map[string]any{"data": e.Date.String(), "tipo": e.TypeName, "descrizione": e.Description, "esito": e.Outcome})
	}
	out["eventi"] = el
	return out, nil
}

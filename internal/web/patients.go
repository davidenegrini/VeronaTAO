// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/davidenegrini/veronatao/internal/domain"
	"github.com/davidenegrini/veronatao/internal/mail"
	"github.com/davidenegrini/veronatao/internal/pdf"
	"github.com/davidenegrini/veronatao/internal/store"
)

// ---------- Pagina iniziale ed elenco ----------

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.PatientRows("", "active")
	if err != nil {
		s.fail(w, err)
		return
	}
	dueDays := s.Store.SettingInt("due_days", 7)
	lostDays := s.Store.SettingInt("lost_days", 90)
	hep := s.Store.HeparinOngoing(domain.Today())
	var due, out, noINR, noTreatment, heparin []store.PatientRow
	for _, p := range rows {
		if !p.HasTreatment {
			noTreatment = append(noTreatment, p)
			continue
		}
		if !p.NextDate.IsZero() && p.DaysToNext() <= dueDays {
			due = append(due, p)
		}
		if p.LastPos == domain.PosBelow || p.LastPos == domain.PosAbove {
			out = append(out, p)
		}
		if p.LastINRDate.IsZero() || p.DaysSinceINR() > lostDays {
			noINR = append(noINR, p)
		}
		if hep[p.ID] {
			heparin = append(heparin, p)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].NextDate.Before(due[j].NextDate) })
	s.render(w, r, "dashboard.html", "Inizio", map[string]any{
		"Due": due, "OutOfRange": out, "NoINR": noINR, "NoTreatment": noTreatment, "Heparin": heparin,
		"DueDays": dueDays, "LostDays": lostDays, "ActiveCount": len(rows), "BackupWarning": s.backupWarning(),
	})
}

func (s *Server) patients(w http.ResponseWriter, r *http.Request) {
	// La ricerca arriva via POST: nessun dato personale finisce nell'URL.
	q := strings.TrimSpace(r.FormValue("q"))
	status := r.FormValue("status")
	if r.Method == http.MethodGet {
		status = "active"
	}
	rows, err := s.Store.PatientRows(q, status)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "patients.html", "Pazienti", map[string]any{"Rows": rows, "Q": q, "Status": status, "Statuses": store.StatusOrder})
}

func (s *Server) patientForm(w http.ResponseWriter, r *http.Request) {
	var p store.Patient
	if id := pathID(r, "id"); id != 0 {
		var err error
		if p, err = s.Store.Patient(id); err != nil {
			s.fail(w, errNotFound)
			return
		}
	}
	s.render(w, r, "patient_form.html", "Anagrafica", map[string]any{"P": p})
}

func (s *Server) patientSave(w http.ResponseWriter, r *http.Request) {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	p := store.Patient{ID: pathID(r, "id"), LastName: strings.ToUpper(f("last_name")), FirstName: strings.ToUpper(f("first_name")), Sex: f("sex"),
		FiscalCode: strings.ToUpper(f("fiscal_code")), Phone: f("phone"), Email: f("email"), Address: f("address"), ExternalCode: f("external_code"), Notes: f("notes"), EmailAuto: r.FormValue("email_auto") == "1"}
	var err error
	p.BirthDate, err = domain.ParseDate(f("birth_date"))
	switch {
	case err != nil || p.BirthDate.IsZero():
		err = errors.New("data di nascita mancante o non valida")
	case p.BirthDate.After(domain.Today()):
		err = errors.New("la data di nascita è nel futuro")
	case p.LastName == "" || p.FirstName == "":
		err = errors.New("cognome e nome sono obbligatori")
	case p.Sex != "" && p.Sex != "M" && p.Sex != "F":
		err = errors.New("sesso non valido")
	case p.Email != "" && !mail.ValidAddress(p.Email):
		err = errors.New("indirizzo email non valido")
	case p.EmailAuto && p.Email == "":
		err = errors.New("per l'invio automatico per email serve l'indirizzo del paziente")
	}
	if err != nil {
		s.render(w, r, "patient_form.html", "Anagrafica", map[string]any{"P": p, "Error": err.Error()})
		return
	}
	id, err := s.Store.SavePatient(actor(r), p)
	if err != nil {
		s.fail(w, err)
		return
	}
	to := fmt.Sprintf("/patients/%d", id)
	if p.ID == 0 {
		to += "/record"
	}
	s.done(w, r, nil, "Anagrafica salvata.", to)
}

// ---------- Schermata del controllo ----------

type controlForm struct {
	Date, Time, INR, Site string
	InrOnly               bool
	DrugID                int64
	WeeklyMg              string
	Q                     [7]string
	FirstDay              string
	NextDate              string
	Comment               string
	Source                string
}

func readControlForm(r *http.Request) controlForm {
	f := controlForm{
		Date: r.FormValue("date"), Time: strings.TrimSpace(r.FormValue("time")), INR: strings.TrimSpace(r.FormValue("inr")),
		Site: r.FormValue("site"), InrOnly: r.FormValue("inr_only") == "1", WeeklyMg: strings.TrimSpace(r.FormValue("weekly_mg")),
		FirstDay: strings.TrimSpace(r.FormValue("first_day")), NextDate: r.FormValue("next_date"),
		Comment: strings.TrimSpace(r.FormValue("comment")), Source: r.FormValue("source"),
	}
	f.DrugID, _ = strconv.ParseInt(r.FormValue("drug_id"), 10, 64)
	for i := range f.Q {
		f.Q[i] = strings.TrimSpace(r.FormValue("q" + strconv.Itoa(i)))
	}
	return f
}

type controlRow struct {
	store.Control
	Pos domain.Position
}

type ttrView struct {
	OK               bool
	Below, In, Above int
}

func (s *Server) ttr12(patientID int64, asOf domain.Date) ttrView {
	t, err := s.Store.PatientTTR(patientID, asOf.AddDays(-365), asOf)
	if err != nil {
		return ttrView{}
	}
	b, in, a, ok := t.Percent()
	return ttrView{OK: ok, Below: b, In: in, Above: a}
}

// patientContext raccoglie i dati comuni alle schermate del paziente.
type patientContext struct {
	P         store.Patient
	T         *store.Treatment
	Target    *domain.TargetPeriod
	Targets   []domain.TargetPeriod
	Controls  []controlRow
	PrevPlan  *store.Control
	Drugs     []store.Drug
	TTR       ttrView
	Treatment string
}

func (s *Server) loadPatient(id int64) (*patientContext, error) {
	p, err := s.Store.Patient(id)
	if err != nil {
		return nil, errNotFound
	}
	pc := &patientContext{P: p}
	trs, err := s.Store.Treatments(id)
	if err != nil {
		return nil, err
	}
	for i := range trs {
		if trs[i].Open() {
			pc.T = &trs[i]
		}
	}
	if pc.Targets, err = s.Store.PatientTargets(id); err != nil {
		return nil, err
	}
	if t, ok := domain.TargetAt(pc.Targets, domain.Today()); ok && pc.T != nil {
		pc.Target = &t
	}
	cs, err := s.Store.Controls(id)
	if err != nil {
		return nil, err
	}
	for i, c := range cs {
		row := controlRow{Control: c}
		if c.HasINR {
			row.Pos = domain.PositionOf(c.INR, c.Date, pc.Targets)
		}
		pc.Controls = append(pc.Controls, row)
		if pc.PrevPlan == nil && c.HasPlan() {
			pc.PrevPlan = &cs[i]
		}
	}
	if pc.Drugs, err = s.Store.Drugs(true); err != nil {
		return nil, err
	}
	pc.TTR = s.ttr12(id, domain.Today())
	return pc, nil
}

func (s *Server) patientPage(w http.ResponseWriter, r *http.Request) {
	pc, err := s.loadPatient(pathID(r, "id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Store.Audit(actor(r), "view", "patient", pc.P.ID, pc.P.ID, "scheda controlli")
	f := controlForm{Date: domain.Today().String(), Site: "lab", Source: "manual"}
	if pc.PrevPlan != nil {
		f.DrugID = pc.PrevPlan.DrugID
	} else if pc.T != nil {
		f.DrugID = pc.T.DrugID
	}
	s.renderPatient(w, r, pc, f, nil)
}

func (s *Server) renderPatient(w http.ResponseWriter, r *http.Request, pc *patientContext, f controlForm, errs []string) {
	data := map[string]any{"PC": pc, "F": f, "Errors": errs, "Chart": inrChart(pc.Controls, pc.Targets)}
	if f.DrugID != 0 {
		if d, err := s.Store.Drug(f.DrugID); err == nil {
			var w domain.Week
			ok := true
			for i, q := range f.Q {
				n, err := strconv.Atoi(q)
				if err != nil || n < 0 {
					ok = false
					break
				}
				w[i] = domain.Quarters(n)
			}
			if ok {
				data["FormTotalMg"] = domain.FormatMg(w.Total().MgMilli(d.MgMilli))
			}
		}
	}
	s.render(w, r, "patient.html", pc.P.FullName(), data)
}

// parseControl valida i campi del controllo. Restituisce errori tecnici di
// inserimento, mai valutazioni cliniche.
func (s *Server) parseControl(f controlForm, pc *patientContext) (store.Control, store.Drug, []string) {
	var errs []string
	c := store.Control{PatientID: pc.P.ID, Time: f.Time, Site: f.Site, Comment: f.Comment, Source: "manual"}
	var drug store.Drug
	if pc.T == nil {
		return c, drug, []string{"Il paziente non ha un percorso di trattamento aperto."}
	}
	c.TreatmentID = pc.T.ID
	if _, ok := store.SiteLabels[c.Site]; !ok {
		c.Site = "lab"
	}
	var err error
	if c.Date, err = domain.ParseDate(f.Date); err != nil || c.Date.IsZero() {
		errs = append(errs, "Data del controllo mancante o non valida.")
	} else if c.Date.After(domain.Today()) {
		errs = append(errs, "La data del controllo è nel futuro.")
	}
	if f.Time != "" {
		if len(f.Time) != 5 || f.Time[2] != ':' {
			errs = append(errs, "Ora non valida (formato HH:MM).")
		}
	}
	if f.INR != "" {
		v, err := domain.ParseDecimal(f.INR, 100)
		if err != nil || v < 50 || v > 2000 {
			errs = append(errs, "Valore di INR non valido: atteso un numero tra 0,5 e 20, con al massimo due decimali.")
		} else {
			c.HasINR, c.INR = true, domain.INR(v)
		}
	}
	if f.InrOnly {
		if !c.HasINR {
			errs = append(errs, "Per un controllo senza piano va inserito il valore di INR.")
		}
		if f.NextDate != "" {
			if c.NextDate, err = domain.ParseDate(f.NextDate); err != nil {
				errs = append(errs, "Data del prossimo controllo non valida.")
			} else if !c.Date.IsZero() && !c.NextDate.After(c.Date) {
				errs = append(errs, "Il prossimo controllo deve essere successivo alla data del controllo.")
			}
		}
		return c, drug, errs
	}
	if drug, err = s.Store.Drug(f.DrugID); err != nil || !drug.Active {
		errs = append(errs, "Selezionare il farmaco.")
		return c, drug, errs
	}
	c.DrugID = drug.ID
	c.MgMilli, c.DrugLabel = drug.MgMilli, drug.Label()
	for i, q := range f.Q {
		n, err := strconv.Atoi(q)
		switch {
		case err != nil || n < 0:
			errs = append(errs, domain.WeekdayLong[i]+": inserire la dose in quarti di compressa (numero intero, 0 = niente).")
		case !domain.Quarters(n).ValidFor(drug.Div):
			errs = append(errs, fmt.Sprintf("%s: %d quarti non sono somministrabili con %s (%s).", domain.WeekdayLong[i], n, drug.Label(), drug.Div.Label()))
		default:
			c.Week[i] = domain.Quarters(n)
		}
	}
	if f.FirstDay != "" {
		n, err := strconv.Atoi(f.FirstDay)
		if err != nil || n < 0 || !domain.Quarters(n).ValidFor(drug.Div) {
			errs = append(errs, "Dose del primo giorno non valida.")
		} else {
			q := domain.Quarters(n)
			c.FirstDay = &q
		}
	}
	if c.NextDate, err = domain.ParseDate(f.NextDate); err != nil || c.NextDate.IsZero() {
		errs = append(errs, "Indicare la data del prossimo controllo: il piano copre i giorni fino al giorno precedente.")
	} else if !c.Date.IsZero() && !c.NextDate.After(c.Date) {
		errs = append(errs, "Il prossimo controllo deve essere successivo alla data del controllo.")
	} else if !c.Date.IsZero() && c.Date.DaysUntil(c.NextDate) > 366 {
		errs = append(errs, "Il piano non può superare un anno.")
	} else {
		c.PlanEnd = c.NextDate.AddDays(-1)
	}
	if f.Source == "copied_from_previous" && pc.PrevPlan != nil && pc.PrevPlan.Week == c.Week && pc.PrevPlan.DrugID == c.DrugID {
		c.Source = "copied_from_previous"
	}
	return c, drug, errs
}

// readDays legge i giorni dal calendario se presenti nel modulo, altrimenti li
// genera dallo schema settimanale.
func readDays(r *http.Request, c store.Control, drug store.Drug) ([]domain.PlanDay, []string) {
	days := domain.BuildDays(c.Date, c.PlanEnd, c.Week, c.FirstDay)
	if len(days) == 0 || r.FormValue("dq_"+days[0].Date.String()) == "" {
		return days, nil
	}
	var errs []string
	for i := range days {
		d := &days[i]
		key := d.Date.String()
		n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("dq_" + key)))
		if err != nil || n < 0 {
			errs = append(errs, d.Date.IT()+": dose non valida.")
		} else if !domain.Quarters(n).ValidFor(drug.Div) {
			errs = append(errs, fmt.Sprintf("%s: %d quarti non sono somministrabili con %s.", d.Date.IT(), n, drug.Label()))
		} else {
			d.Quarters = domain.Quarters(n)
		}
		h := domain.Heparin{Drug: strings.TrimSpace(r.FormValue("hd_" + key)), Dose: strings.TrimSpace(r.FormValue("hv_" + key)), Unit: r.FormValue("hu_" + key)}
		h.Times, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("ht_" + key)))
		if !h.Present() {
			continue
		}
		_, derr := domain.ParseDecimal(h.Dose, 100)
		switch {
		case h.Drug == "" || derr != nil:
			errs = append(errs, d.Date.IT()+": per l'eparina indicare farmaco e dose (numero).")
		case h.Unit != "mg" && h.Unit != "UI":
			errs = append(errs, d.Date.IT()+": per l'eparina indicare l'unità (mg oppure UI).")
		case h.Times < 1 || h.Times > 4:
			errs = append(errs, d.Date.IT()+": per l'eparina indicare il numero di somministrazioni al giorno (da 1 a 4).")
		}
		d.Heparin = h
	}
	return days, errs
}

// consistencyWarnings esegue le verifiche di coerenza dell'inserimento che
// richiedono una conferma esplicita. Sono confronti aritmetici con soglie
// impostate nella configurazione: non esprimono giudizi clinici e non
// suggeriscono azioni.
func (s *Server) consistencyWarnings(c store.Control, drug store.Drug, days []domain.PlanDay, prev *store.Control) []string {
	var out []string
	if max := s.Store.SettingInt("max_daily_quarters", 16); max > 0 {
		var over []string
		for _, d := range days {
			if int(d.Quarters) > max {
				over = append(over, d.Date.IT())
			}
		}
		if len(over) > 0 {
			out = append(out, fmt.Sprintf("Dose giornaliera superiore al massimo configurato (%d quarti) nei giorni: %s.", max, strings.Join(over, ", ")))
		}
	}
	if prev != nil && prev.DrugID == c.DrugID {
		limit := s.Store.SettingInt("weekly_variation_pct", 30)
		if pct, ok := domain.VariationPercent(prev.Week.Total(), c.Week.Total()); ok && limit > 0 && pct > limit {
			out = append(out, fmt.Sprintf("Dose settimanale precedente %s mg, nuova %s mg: variazione del %d%%, superiore alla soglia configurata del %d%%.",
				prev.WeeklyMg(), domain.FormatMg(c.Week.Total().MgMilli(drug.MgMilli)), pct, limit))
		}
	}
	if len(days) > 84 {
		out = append(out, fmt.Sprintf("Il piano copre %d giorni, più di 12 settimane.", len(days)))
	}
	return out
}

func (s *Server) controlPost(w http.ResponseWriter, r *http.Request) {
	pc, err := s.loadPatient(pathID(r, "id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	f := readControlForm(r)
	action := r.FormValue("action")

	switch action {
	case "edit":
		s.renderPatient(w, r, pc, f, nil)
		return
	case "copy":
		// Copia di dati già inseriti dal medico, non una proposta del software.
		if pc.PrevPlan == nil {
			s.renderPatient(w, r, pc, f, []string{"Non esiste uno schema precedente da copiare."})
			return
		}
		f.DrugID = pc.PrevPlan.DrugID
		for i, q := range pc.PrevPlan.Week {
			f.Q[i] = strconv.Itoa(int(q))
		}
		f.WeeklyMg = pc.PrevPlan.WeeklyMg()
		f.Source = "copied_from_previous"
		s.renderPatient(w, r, pc, f, nil)
		return
	case "distribute":
		// Ripartizione aritmetica uniforme di un totale indicato dal medico.
		var errs []string
		drug, err := s.Store.Drug(f.DrugID)
		if err != nil {
			errs = append(errs, "Selezionare il farmaco.")
		} else if mg, err := domain.ParseDecimal(f.WeeklyMg, 1000); err != nil {
			errs = append(errs, "Dose settimanale in mg mancante o non valida.")
		} else if total, err := domain.WeeklyQuartersFromMg(mg, drug.MgMilli); err != nil {
			errs = append(errs, "Con "+drug.Label()+" "+err.Error()+".")
		} else if week, err := domain.DistributeWeekly(total, drug.Div); err != nil {
			errs = append(errs, "Con "+drug.Label()+" "+err.Error()+".")
		} else {
			for i, q := range week {
				f.Q[i] = strconv.Itoa(int(q))
			}
			f.Source = "manual"
		}
		s.renderPatient(w, r, pc, f, errs)
		return
	}

	c, drug, errs := s.parseControl(f, pc)
	if len(errs) > 0 && (action == "preview" || f.InrOnly) {
		s.renderPatient(w, r, pc, f, errs)
		return
	}
	if f.InrOnly {
		id, err := s.Store.SaveControl(actor(r), c, nil)
		if err != nil {
			s.renderPatient(w, r, pc, f, []string{userMessage(err)})
			return
		}
		s.done(w, r, nil, "Controllo registrato.", fmt.Sprintf("/controls/%d", id))
		return
	}
	var days []domain.PlanDay
	var warnings []string
	if len(errs) == 0 {
		var dayErrs []string
		days, dayErrs = readDays(r, c, drug)
		errs = append(errs, dayErrs...)
		warnings = s.consistencyWarnings(c, drug, days, pc.PrevPlan)
	}
	if action == "save" && len(errs) == 0 && (len(warnings) == 0 || r.FormValue("confirm") == "1") {
		id, err := s.Store.SaveControl(actor(r), c, days)
		if err == nil {
			s.done(w, r, nil, "Controllo e piano salvati.", fmt.Sprintf("/controls/%d", id))
			return
		}
		errs = append(errs, userMessage(err))
	} else if action == "save" && len(errs) == 0 {
		errs = append(errs, "Per salvare occorre confermare esplicitamente le segnalazioni qui sotto.")
	}
	var periodQ domain.Quarters
	for _, d := range days {
		periodQ += d.Quarters
	}
	s.render(w, r, "preview.html", pc.P.FullName(), map[string]any{
		"PC": pc, "F": f, "C": c, "Drug": drug, "Errors": errs, "Warnings": warnings,
		"Weeks": domain.CalendarWeeks(days, c.NextDate), "Week": c.Week, "DayCount": len(days),
		"PeriodMg": domain.FormatMg(periodQ.MgMilli(drug.MgMilli)), "HeparinNames": s.Store.HeparinNames(),
	})
}

// ---------- Controllo salvato, stampa, annullamento ----------

func (s *Server) controlPage(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.Control(pathID(r, "id"))
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	pc, err := s.loadPatient(c.PatientID)
	if err != nil {
		s.fail(w, err)
		return
	}
	days, err := s.Store.PlanDays(c.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Store.Audit(actor(r), "view", "control", c.ID, c.PatientID, "")
	var periodQ domain.Quarters
	for _, d := range days {
		periodQ += d.Quarters
	}
	data := map[string]any{
		"PC": pc, "C": c, "Weeks": domain.CalendarWeeks(days, c.NextDate), "Week": c.Week, "DayCount": len(days),
		"IsLast": len(pc.Controls) > 0 && pc.Controls[0].ID == c.ID, "EmailEnabled": s.emailEnabled(),
	}
	if c.HasINR {
		data["Pos"] = domain.PositionOf(c.INR, c.Date, pc.Targets)
	}
	if c.HasPlan() {
		data["PeriodMg"] = domain.FormatMg(periodQ.MgMilli(c.MgMilli))
	}
	s.render(w, r, "control.html", pc.P.FullName(), data)
}

func (s *Server) controlPrint(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.Control(pathID(r, "id"))
	if err != nil || !c.HasPlan() {
		s.fail(w, errNotFound)
		return
	}
	id, pc, err := s.printDocument(r, c)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Invio automatico, se il paziente lo ha chiesto e la posta è configurata.
	// Un invio non riuscito non blocca la stampa: l'esito compare nella scheda.
	if pc.P.EmailAuto && s.emailEnabled() {
		if err := s.sendDocument(r, pc.P, id); err != nil {
			s.flashErr(r, fmt.Errorf("schema stampato, ma invio per email non riuscito: %s", userMessage(err)))
		} else {
			s.flash(r, "Schema stampato e inviato per email a "+pc.P.Email+".")
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/documents/%d", id), http.StatusSeeOther)
}

// printDocument genera lo schema per il paziente, lo conserva con la sua
// impronta e ne restituisce l'identificativo.
func (s *Server) printDocument(r *http.Request, c store.Control) (int64, *patientContext, error) {
	pc, err := s.loadPatient(c.PatientID)
	if err != nil {
		return 0, nil, err
	}
	days, err := s.Store.PlanDays(c.ID)
	if err != nil || len(days) == 0 {
		return 0, nil, errNotFound
	}
	sch := pdf.Schedule{
		HeaderLines:  strings.Split(strings.ReplaceAll(s.Store.Setting("header"), "\r", ""), "\n"),
		Title:        s.Store.Setting("doc_title"),
		PatientName:  pc.P.FullName(),
		BirthDate:    pc.P.BirthDate.IT(),
		ExternalCode: pc.P.ExternalCode,
		Phone:        pc.P.Phone,
		INRDate:      strings.TrimSpace(c.Date.IT() + " " + c.Time),
		WeeklyMg:     c.WeeklyMg(),
		DrugLabel:    c.DrugLabel,
		Days:         days,
		ControlDate:  c.NextDate,
		Comment:      c.Comment,
		Doctor:       c.UserName,
		FooterText:   s.Store.Setting("footer_text"),
		TemplateCode: s.Store.Setting("template_code"),
	}
	if c.HasINR {
		sch.INR = c.INR.String()
	}
	if !c.NextDate.IsZero() {
		sch.NextControl = strings.ToLower(domain.WeekdayLong[c.NextDate.Weekday()]) + " " + c.NextDate.IT()
	}
	for _, t := range mustTreatments(s, c.PatientID) {
		if t.ID == c.TreatmentID {
			sch.Diagnosis = t.Indication
			if t.Diagnosis != "" {
				sch.Diagnosis = t.Diagnosis
			}
		}
	}
	if t, ok := domain.TargetAt(pc.Targets, c.Date); ok {
		sch.TargetLabel = t.Label()
	}
	if s.Store.Setting("print_ttr") == "1" {
		if t := s.ttr12(c.PatientID, c.Date); t.OK {
			sch.TTRLabel = strconv.Itoa(t.In) + "% (ultimi 12 mesi)"
		}
	}
	for _, prev := range pc.Controls {
		if prev.Date.Before(c.Date) && len(sch.History) < 3 {
			h := pdf.HistoryRow{Date: prev.Date.IT(), WeeklyMg: prev.WeeklyMg()}
			if prev.HasINR {
				h.INR = prev.INR.String()
			}
			sch.History = append(sch.History, h)
		}
	}
	doc, err := pdf.Render(sch)
	if err != nil {
		return 0, nil, err
	}
	sum := sha256.Sum256(doc)
	id, err := s.Store.SavePrinted(actor(r), store.Printed{PatientID: c.PatientID, ControlID: c.ID, SHA256: hex.EncodeToString(sum[:]),
		PeriodFrom: days[0].Date, PeriodTo: days[len(days)-1].Date, TemplateCode: sch.TemplateCode}, doc)
	return id, pc, err
}

// ---------- Invio per email del PDF protetto ----------

func (s *Server) emailEnabled() bool { return s.Store.Setting("email_enabled") == "1" }

func (s *Server) mailConfig() mail.Config {
	return mail.Config{Host: s.Store.Setting("smtp_host"), Port: s.Store.SettingInt("smtp_port", 587), Username: s.Store.Setting("smtp_user"),
		Password: s.Store.Setting("smtp_password"), From: s.Store.Setting("smtp_from"), FromName: s.Store.Setting("smtp_from_name")}
}

// sendDocument invia al paziente un documento già stampato, come PDF cifrato
// con la password del paziente. Il messaggio non contiene dati clinici: né
// l'oggetto né il testo nominano il paziente o la terapia. Ogni tentativo è
// annotato.
func (s *Server) sendDocument(r *http.Request, p store.Patient, documentID int64) error {
	if !s.emailEnabled() {
		return errors.New("l'invio per email non è attivo: lo configura un amministratore")
	}
	if !mail.ValidAddress(p.Email) {
		return errors.New("il paziente non ha un indirizzo email valido in anagrafica")
	}
	pid, doc, err := s.Store.PrintedPDF(documentID)
	if err != nil || pid != p.ID {
		return errNotFound
	}
	if p.PDFPassword == "" {
		p.PDFPassword = TempPassword()
		if err := s.Store.SetPDFPassword(actor(r), p.ID, p.PDFPassword); err != nil {
			return err
		}
	}
	protected, err := mail.ProtectPDF(doc, p.PDFPassword)
	if err == nil {
		err = mail.Send(s.mailConfig(), mail.Message{To: p.Email, Subject: s.Store.Setting("email_subject"), Body: s.Store.Setting("email_body"),
			AttachmentName: "schema-terapia.pdf", Attachment: protected})
	}
	s.Store.LogEmailSend(actor(r), p.ID, documentID, p.Email, err)
	return err
}

// controlEmail invia per email lo schema del controllo: l'ultimo documento
// stampato, oppure uno nuovo se non è ancora stato stampato.
func (s *Server) controlEmail(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.Control(pathID(r, "id"))
	if err != nil || !c.HasPlan() {
		s.fail(w, errNotFound)
		return
	}
	p, err := s.Store.Patient(c.PatientID)
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	back := fmt.Sprintf("/controls/%d", c.ID)
	docID, err := s.Store.LastPrinted(c.ID)
	if err != nil {
		if docID, _, err = s.printDocument(r, c); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.done(w, r, s.sendDocument(r, p, docID), "Schema inviato per email a "+p.Email+" come PDF protetto da password.", back)
}

func (s *Server) pdfPasswordRenew(w http.ResponseWriter, r *http.Request) {
	err := s.Store.SetPDFPassword(actor(r), pathID(r, "id"), TempPassword())
	s.done(w, r, err, "Nuova password dei PDF generata: va consegnata di persona al paziente. I PDF già inviati restano apribili con la password precedente.", recordURL(r))
}

func mustTreatments(s *Server, patientID int64) []store.Treatment {
	t, _ := s.Store.Treatments(patientID)
	return t
}

func (s *Server) documentGet(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	pid, doc, err := s.Store.PrintedPDF(id)
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	s.Store.Audit(actor(r), "view", "document", id, pid, "")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="schema-%d.pdf"`, id))
	_, _ = w.Write(doc)
}

func (s *Server) controlDelete(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.Control(pathID(r, "id"))
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	if err := s.Store.DeleteControl(actor(r), c.ID, c.PatientID, r.FormValue("reason")); err != nil {
		s.done(w, r, err, "", fmt.Sprintf("/controls/%d", c.ID))
		return
	}
	s.done(w, r, nil, "Controllo annullato.", fmt.Sprintf("/patients/%d", c.PatientID))
}

// ---------- Cartella: percorsi, range, stato, farmaci, eventi ----------

func (s *Server) recordPage(w http.ResponseWriter, r *http.Request) {
	pc, err := s.loadPatient(pathID(r, "id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	id := pc.P.ID
	s.Store.Audit(actor(r), "view", "patient", id, id, "cartella")
	data := map[string]any{"PC": pc, "CloseReasons": store.CloseReasons, "Statuses": store.StatusOrder, "MedNames": s.Store.MedicationNames()}
	data["Treatments"], _ = s.Store.Treatments(id)
	if pc.T != nil {
		data["TargetRows"], _ = s.Store.Targets(pc.T.ID)
	}
	data["Indications"], _ = s.Store.Indications(true)
	data["StatusChanges"], _ = s.Store.StatusChanges(id)
	data["Medications"], _ = s.Store.Medications(id)
	data["Events"], _ = s.Store.Events(id)
	data["EventTypes"], _ = s.Store.EventTypes(true)
	data["Printed"], _ = s.Store.PrintedList(id)
	data["Audit"], _ = s.Store.AuditList(id, "", 40)
	data["EmailSends"], _ = s.Store.EmailSends(id)
	data["EmailEnabled"] = s.emailEnabled()
	s.render(w, r, "record.html", pc.P.FullName(), data)
}

func recordURL(r *http.Request) string { return fmt.Sprintf("/patients/%d/record", pathID(r, "id")) }

func parseINR(v string) (domain.INR, error) {
	if strings.TrimSpace(v) == "" {
		return 0, nil
	}
	n, err := domain.ParseDecimal(v, 100)
	return domain.INR(n), err
}

func (s *Server) treatmentFromForm(r *http.Request) (store.Treatment, error) {
	t := store.Treatment{PatientID: pathID(r, "id"), Diagnosis: strings.TrimSpace(r.FormValue("diagnosis")), SineDie: r.FormValue("sine_die") == "1"}
	t.IndicationID, _ = strconv.ParseInt(r.FormValue("indication_id"), 10, 64)
	t.DrugID, _ = strconv.ParseInt(r.FormValue("drug_id"), 10, 64)
	var err error
	if t.PlannedEnd, err = domain.ParseDate(r.FormValue("planned_end")); err != nil {
		return t, err
	}
	if t.SineDie {
		t.PlannedEnd = domain.Date{}
	}
	if t.IndicationID == 0 || t.DrugID == 0 {
		return t, errors.New("indicazione e farmaco sono obbligatori")
	}
	return t, nil
}

func (s *Server) treatmentOpen(w http.ResponseWriter, r *http.Request) {
	t, err := s.treatmentFromForm(r)
	var target domain.TargetPeriod
	if err == nil {
		t.OpenedAt, err = domain.ParseDate(r.FormValue("opened_at"))
	}
	if err == nil {
		target.Min, err = parseINR(r.FormValue("inr_min"))
	}
	if err == nil {
		target.Max, err = parseINR(r.FormValue("inr_max"))
	}
	if err == nil {
		target.Target, err = parseINR(r.FormValue("inr_target"))
	}
	if err == nil {
		_, err = s.Store.OpenTreatment(actor(r), t, target)
	}
	s.done(w, r, err, "Percorso di trattamento aperto.", recordURL(r))
}

func (s *Server) treatmentUpdate(w http.ResponseWriter, r *http.Request) {
	t, err := s.treatmentFromForm(r)
	if err == nil {
		t.ID, _ = strconv.ParseInt(r.FormValue("treatment_id"), 10, 64)
		err = s.Store.UpdateTreatment(actor(r), t)
	}
	s.done(w, r, err, "Percorso aggiornato.", recordURL(r))
}

func (s *Server) treatmentClose(w http.ResponseWriter, r *http.Request) {
	tid, _ := strconv.ParseInt(r.FormValue("treatment_id"), 10, 64)
	date, err := domain.ParseDate(r.FormValue("closed_at"))
	if err == nil {
		err = s.Store.CloseTreatment(actor(r), tid, pathID(r, "id"), date, r.FormValue("close_reason"), strings.TrimSpace(r.FormValue("close_notes")))
	}
	s.done(w, r, err, "Percorso chiuso.", recordURL(r))
}

func (s *Server) targetChange(w http.ResponseWriter, r *http.Request) {
	tid, _ := strconv.ParseInt(r.FormValue("treatment_id"), 10, 64)
	from, err := domain.ParseDate(r.FormValue("valid_from"))
	var min, max, target domain.INR
	if err == nil {
		min, err = parseINR(r.FormValue("inr_min"))
	}
	if err == nil {
		max, err = parseINR(r.FormValue("inr_max"))
	}
	if err == nil {
		target, err = parseINR(r.FormValue("inr_target"))
	}
	if err == nil {
		err = s.Store.ChangeTarget(actor(r), tid, pathID(r, "id"), from, min, max, target, r.FormValue("reason"))
	}
	s.done(w, r, err, "Range INR aggiornato.", recordURL(r))
}

func (s *Server) statusChange(w http.ResponseWriter, r *http.Request) {
	date, err := domain.ParseDate(r.FormValue("date"))
	if err == nil && date.IsZero() {
		date = domain.Today()
	}
	if err == nil {
		err = s.Store.ChangeStatus(actor(r), pathID(r, "id"), r.FormValue("status"), date, strings.TrimSpace(r.FormValue("reason")))
	}
	s.done(w, r, err, "Stato aggiornato.", recordURL(r))
}

func (s *Server) medicationAdd(w http.ResponseWriter, r *http.Request) {
	m := store.Medication{PatientID: pathID(r, "id"), Name: r.FormValue("name"), Dosage: strings.TrimSpace(r.FormValue("dosage")),
		Interfering: r.FormValue("interfering") == "1", Direction: r.FormValue("direction"), Notes: strings.TrimSpace(r.FormValue("notes"))}
	var err error
	if m.StartDate, err = domain.ParseDate(r.FormValue("start_date")); err == nil {
		err = s.Store.AddMedication(actor(r), m)
	}
	s.done(w, r, err, "Farmaco registrato.", recordURL(r))
}

func (s *Server) medicationEnd(w http.ResponseWriter, r *http.Request) {
	end, err := domain.ParseDate(r.FormValue("end_date"))
	if err == nil && end.IsZero() {
		err = errors.New("indicare la data di sospensione")
	}
	if err == nil {
		err = s.Store.EndMedication(actor(r), pathID(r, "mid"), pathID(r, "id"), end)
	}
	s.done(w, r, err, "Sospensione registrata.", recordURL(r))
}

func (s *Server) medicationDelete(w http.ResponseWriter, r *http.Request) {
	err := s.Store.DeleteMedication(actor(r), pathID(r, "mid"), pathID(r, "id"), r.FormValue("reason"))
	s.done(w, r, err, "Voce annullata.", recordURL(r))
}

func (s *Server) eventAdd(w http.ResponseWriter, r *http.Request) {
	e := store.Event{PatientID: pathID(r, "id"), Description: strings.TrimSpace(r.FormValue("description")), Outcome: strings.TrimSpace(r.FormValue("outcome"))}
	e.TypeID, _ = strconv.ParseInt(r.FormValue("type_id"), 10, 64)
	var err error
	if e.Date, err = domain.ParseDate(r.FormValue("date")); err == nil {
		err = s.Store.AddEvent(actor(r), e)
	}
	s.done(w, r, err, "Evento registrato.", recordURL(r))
}

func (s *Server) eventDelete(w http.ResponseWriter, r *http.Request) {
	err := s.Store.DeleteEvent(actor(r), pathID(r, "eid"), pathID(r, "id"), r.FormValue("reason"))
	s.done(w, r, err, "Voce annullata.", recordURL(r))
}

// patientExport produce tutti i dati del paziente in formato leggibile
// (diritto di accesso e portabilità).
func (s *Server) patientExport(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	data, err := s.Store.PatientExport(id)
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	s.Store.Audit(actor(r), "export", "patient", id, id, "esportazione completa dei dati del paziente")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="paziente-%d.json"`, id))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

// ---------- Grafico dell'INR ----------

// inrChart disegna l'andamento dell'INR con la banda del range impostato.
// L'SVG contiene solo numeri calcolati qui, nessun testo proveniente
// dall'utente.
func inrChart(cs []controlRow, targets []domain.TargetPeriod) template.HTML {
	var ms []domain.Measurement
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].HasINR {
			ms = append(ms, domain.Measurement{Date: cs[i].Date, Value: cs[i].INR})
		}
	}
	if len(ms) < 2 {
		return ""
	}
	if len(ms) > 40 {
		ms = ms[len(ms)-40:]
	}
	const W, H, L, R, T, B = 760.0, 220.0, 34.0, 10.0, 10.0, 24.0
	first, last := ms[0].Date, ms[len(ms)-1].Date
	span := float64(first.DaysUntil(last))
	if span < 1 {
		span = 1
	}
	maxV := 400.0
	for _, m := range ms {
		if float64(m.Value)+50 > maxV {
			maxV = float64(m.Value) + 50
		}
	}
	minV := 100.0
	x := func(d domain.Date) float64 { return L + float64(first.DaysUntil(d))/span*(W-L-R) }
	y := func(v float64) float64 {
		if v < minV {
			v = minV
		}
		return T + (maxV-v)/(maxV-minV)*(H-T-B)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %.0f %.0f" role="img" aria-label="Andamento INR">`, W, H)
	for _, t := range targets {
		from, to := t.From, t.To
		if to.IsZero() || to.After(last) {
			to = last
		}
		if from.Before(first) {
			from = first
		}
		if to.Before(from) {
			continue
		}
		fmt.Fprintf(&b, `<rect class="band" x="%.1f" y="%.1f" width="%.1f" height="%.1f"/>`, x(from), y(float64(t.Max)), x(to)-x(from), y(float64(t.Min))-y(float64(t.Max)))
	}
	for v := 100.0; v <= maxV; v += 100 {
		fmt.Fprintf(&b, `<line class="grid" x1="%.0f" y1="%.1f" x2="%.0f" y2="%.1f"/><text class="axis" x="4" y="%.1f">%.0f</text>`, L, y(v), W-R, y(v), y(v)+4, v/100)
	}
	var pts []string
	for _, m := range ms {
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(m.Date), y(float64(m.Value))))
	}
	fmt.Fprintf(&b, `<polyline class="line" points="%s"/>`, strings.Join(pts, " "))
	for _, m := range ms {
		fmt.Fprintf(&b, `<circle class="dot" cx="%.1f" cy="%.1f" r="3.5"><title>%s: %s</title></circle>`, x(m.Date), y(float64(m.Value)), m.Date.IT(), m.Value)
	}
	fmt.Fprintf(&b, `<text class="axis" x="%.0f" y="%.0f">%s</text><text class="axis end" x="%.0f" y="%.0f">%s</text></svg>`, L, H-6, first.IT(), W-R, H-6, last.IT())
	return template.HTML(b.String())
}

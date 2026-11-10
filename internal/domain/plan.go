// SPDX-License-Identifier: AGPL-3.0-or-later

package domain

// Heparin descrive una somministrazione di eparina in un giorno. Tutti i
// valori sono inseriti dal medico: il software non calcola né propone nulla.
type Heparin struct {
	Drug  string // nome del farmaco, testo libero
	Dose  string // valore numerico come scritto dal medico, es. "4000"
	Unit  string // "mg" oppure "UI"
	Times int    // somministrazioni al giorno
}

func (h Heparin) Present() bool { return h.Drug != "" || h.Dose != "" }

// PlanDay è un singolo giorno del piano terapeutico.
type PlanDay struct {
	Date     Date
	Quarters Quarters
	Heparin  Heparin
}

// BuildDays genera una riga per ogni giorno del periodo (estremi inclusi)
// applicando lo schema settimanale. Se firstDay non è nil, sostituisce la
// dose del primo giorno.
func BuildDays(start, end Date, week Week, firstDay *Quarters) []PlanDay {
	var days []PlanDay
	EachDay(start, end, func(d Date) {
		days = append(days, PlanDay{Date: d, Quarters: week[d.Weekday()]})
	})
	if firstDay != nil && len(days) > 0 {
		days[0].Quarters = *firstDay
	}
	return days
}

// DeviatesFromWeek indica se il giorno si discosta dallo schema settimanale.
func (p PlanDay) DeviatesFromWeek(week Week) bool {
	return p.Quarters != week[p.Date.Weekday()]
}

// VariationPercent restituisce lo scostamento percentuale assoluto (arrotondato
// per difetto) tra due dosi settimanali. ok è false se manca il termine di
// confronto.
func VariationPercent(previous, current Quarters) (pct int, ok bool) {
	if previous <= 0 {
		return 0, false
	}
	diff := int(current - previous)
	if diff < 0 {
		diff = -diff
	}
	return diff * 100 / int(previous), true
}

// CalendarCell è una cella della griglia lunedì-domenica. Date è zero per le
// celle esterne al periodo; Day è nil se il giorno non ha dose (ad esempio il
// giorno del controllo successivo).
type CalendarCell struct {
	Date    Date
	Day     *PlanDay
	Control bool
}

// CalendarWeeks dispone i giorni in settimane lunedì-domenica. Le celle
// esterne al periodo restano con data zero. Se control non è zero, il giorno
// del prossimo controllo viene incluso e marcato.
func CalendarWeeks(days []PlanDay, control Date) [][7]CalendarCell {
	if len(days) == 0 {
		return nil
	}
	first, last := days[0].Date, days[len(days)-1].Date
	if !control.IsZero() && control.After(last) {
		last = control
	}
	byDate := make(map[string]*PlanDay, len(days))
	for i := range days {
		byDate[days[i].Date.String()] = &days[i]
	}
	var weeks [][7]CalendarCell
	for monday := first.Monday(); !monday.After(last); monday = monday.AddDays(7) {
		var w [7]CalendarCell
		for i := 0; i < 7; i++ {
			d := monday.AddDays(i)
			if d.Before(first) || d.After(last) {
				continue
			}
			p := byDate[d.String()]
			isControl := !control.IsZero() && d.Equal(control)
			if p == nil && !isControl {
				continue
			}
			w[i] = CalendarCell{Date: d, Day: p, Control: isControl}
		}
		weeks = append(weeks, w)
	}
	return weeks
}

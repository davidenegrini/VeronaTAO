// SPDX-License-Identifier: AGPL-3.0-or-later

package domain

import (
	"sort"
	"strconv"
)

// INR è un valore di INR in centesimi (3,07 -> 307).
type INR int

func (v INR) String() string { return FormatScaled(int(v), 2) }

// Position è la posizione di un INR rispetto al range impostato dal medico.
type Position int

const (
	PosUnknown Position = iota
	PosBelow
	PosInRange
	PosAbove
)

func (p Position) Label() string {
	switch p {
	case PosBelow:
		return "sotto il range"
	case PosInRange:
		return "in range"
	case PosAbove:
		return "sopra il range"
	}
	return ""
}

// TargetPeriod è un range INR impostato dal medico, valido da From a To
// (To zero = tuttora valido).
type TargetPeriod struct {
	From, To         Date
	Min, Max, Target INR
}

func (t TargetPeriod) covers(d Date) bool {
	return !d.Before(t.From) && (t.To.IsZero() || !d.After(t.To))
}

// Label restituisce la forma "2,5 (2-3)" usata in stampa.
func (t TargetPeriod) Label() string {
	short := func(v INR) string {
		if v%100 == 0 {
			return strconv.Itoa(int(v) / 100)
		}
		if v%10 == 0 {
			return FormatScaled(int(v)/10, 1)
		}
		return v.String()
	}
	r := short(t.Min) + "-" + short(t.Max)
	if t.Target > 0 {
		return short(t.Target) + " (" + r + ")"
	}
	return r
}

// TargetAt restituisce il range vigente alla data indicata.
func TargetAt(periods []TargetPeriod, d Date) (TargetPeriod, bool) {
	for _, p := range periods {
		if p.covers(d) {
			return p, true
		}
	}
	return TargetPeriod{}, false
}

// PositionOf confronta un INR con il range vigente in quella data.
func PositionOf(v INR, d Date, periods []TargetPeriod) Position {
	t, ok := TargetAt(periods, d)
	if !ok {
		return PosUnknown
	}
	switch {
	case v < t.Min:
		return PosBelow
	case v > t.Max:
		return PosAbove
	}
	return PosInRange
}

// Measurement è una misurazione di INR.
type Measurement struct {
	Date  Date
	Value INR
}

// MaxTTRGapDays è l'intervallo massimo tra due misurazioni entro cui si
// interpola. Oltre 56 giorni (8 settimane) l'ipotesi di andamento lineare
// dell'INR non è più considerata attendibile e l'intervallo viene escluso
// dal calcolo, come da convenzione comune nell'applicazione del metodo.
const MaxTTRGapDays = 56

// TTR è il risultato del calcolo del tempo in range, in giorni.
type TTR struct {
	Below, In, Above int
}

func (t TTR) Days() int { return t.Below + t.In + t.Above }

// Percent restituisce le tre percentuali (sotto, in range, sopra) arrotondate
// in modo che la somma sia 100. ok è false se non ci sono giorni valutabili.
func (t TTR) Percent() (below, in, above int, ok bool) {
	n := t.Days()
	if n == 0 {
		return 0, 0, 0, false
	}
	below = (t.Below*100 + n/2) / n
	above = (t.Above*100 + n/2) / n
	in = 100 - below - above
	if in < 0 {
		in = 0
	}
	return below, in, above, true
}

// Rosendaal calcola il tempo in range con interpolazione lineare tra
// misurazioni consecutive (Rosendaal et al., 1993). Ogni giorno
// dell'intervallo è valutato rispetto al range vigente in quel giorno, così
// un cambio di target a metà intervallo è gestito correttamente. I giorni
// privi di range impostato e gli intervalli oltre MaxTTRGapDays sono esclusi.
// from e to, se non zero, limitano il periodo considerato.
func Rosendaal(ms []Measurement, periods []TargetPeriod, from, to Date) TTR {
	sorted := append([]Measurement(nil), ms...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
	var res TTR
	for i := 1; i < len(sorted); i++ {
		a, b := sorted[i-1], sorted[i]
		gap := a.Date.DaysUntil(b.Date)
		if gap <= 0 || gap > MaxTTRGapDays {
			continue
		}
		for k := 1; k <= gap; k++ {
			d := a.Date.AddDays(k)
			if (!from.IsZero() && d.Before(from)) || (!to.IsZero() && d.After(to)) {
				continue
			}
			t, ok := TargetAt(periods, d)
			if !ok {
				continue
			}
			// Valore interpolato moltiplicato per gap, per restare su interi.
			v := int(a.Value)*gap + int(b.Value-a.Value)*k
			switch {
			case v < int(t.Min)*gap:
				res.Below++
			case v > int(t.Max)*gap:
				res.Above++
			default:
				res.In++
			}
		}
	}
	return res
}

// InRangeCount conta le misurazioni entro il range vigente alla loro data.
func InRangeCount(ms []Measurement, periods []TargetPeriod) (in, total int) {
	for _, m := range ms {
		switch PositionOf(m.Value, m.Date, periods) {
		case PosInRange:
			in++
			total++
		case PosBelow, PosAbove:
			total++
		}
	}
	return
}

// Class restituisce un identificativo breve della posizione, usato
// dall'interfaccia.
func (p Position) Class() string {
	switch p {
	case PosBelow:
		return "below"
	case PosInRange:
		return "in"
	case PosAbove:
		return "above"
	}
	return ""
}

// Arrow restituisce il simbolo che indica la direzione dello scostamento.
func (p Position) Arrow() string {
	switch p {
	case PosBelow:
		return "↓"
	case PosAbove:
		return "↑"
	}
	return ""
}

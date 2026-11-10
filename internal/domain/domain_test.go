// SPDX-License-Identifier: AGPL-3.0-or-later

package domain

import (
	"testing"
	"time"
)

func TestWords(t *testing.T) {
	want := []string{"NIENTE", "UN quarto", "MEZZA", "TRE quarti", "UNA", "UNA + un quarto",
		"UNA e MEZZA", "UNA + tre quarti", "DUE", "DUE + un quarto", "DUE e MEZZA",
		"DUE + tre quarti", "TRE", "TRE + un quarto", "TRE e MEZZA", "TRE + tre quarti", "QUATTRO"}
	for q, w := range want {
		if got := Quarters(q).Words(); got != w {
			t.Errorf("%d quarti: %q, atteso %q", q, got, w)
		}
	}
	if Quarters(40).Words() != "10" {
		t.Errorf("40 quarti: %q", Quarters(40).Words())
	}
}

func TestDivisibility(t *testing.T) {
	for q := 0; q <= 16; q++ {
		if !Quarters(q).ValidFor(DivQuarter) {
			t.Errorf("quarter %d", q)
		}
		if Quarters(q).ValidFor(DivHalf) != (q%2 == 0) {
			t.Errorf("half %d", q)
		}
		if Quarters(q).ValidFor(DivWhole) != (q%4 == 0) {
			t.Errorf("whole %d", q)
		}
	}
	if Quarters(-1).ValidFor(DivQuarter) {
		t.Error("dose negativa accettata")
	}
}

func TestMg(t *testing.T) {
	// Schema reale: 4|4|4|3|4|4|3 di Coumadin 5 mg = 32,5 mg.
	w := Week{4, 4, 4, 3, 4, 4, 3}
	if got := FormatMg(w.Total().MgMilli(5000)); got != "32,5" {
		t.Errorf("totale %s", got)
	}
	if FormatMg(Quarters(1).MgMilli(5000)) != "1,25" || FormatMg(Quarters(8).MgMilli(4000)) != "8" {
		t.Error("conversione mg")
	}
	q, err := WeeklyQuartersFromMg(32500, 5000)
	if err != nil || q != 26 {
		t.Errorf("da mg: %d %v", q, err)
	}
	if _, err := WeeklyQuartersFromMg(32000, 5000); err == nil {
		t.Error("32 mg non è un numero intero di quarti da 1,25")
	}
}

func TestParseDecimal(t *testing.T) {
	cases := map[string]int{"3,07": 307, "2.5": 250, "2": 200, "0,9": 90, " 3,1 ": 310}
	for in, want := range cases {
		if got, err := ParseDecimal(in, 100); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "3,075", "-1", "1,2,3"} {
		if _, err := ParseDecimal(bad, 100); err == nil {
			t.Errorf("%q accettato", bad)
		}
	}
	if FormatScaled(307, 2) != "3,07" || FormatScaled(200, 2) != "2,00" {
		t.Error("FormatScaled")
	}
}

func TestDistributeWeekly(t *testing.T) {
	for total := 0; total <= 80; total++ {
		w, err := DistributeWeekly(Quarters(total), DivQuarter)
		if err != nil || w.Total() != Quarters(total) {
			t.Fatalf("totale %d: %v %v", total, w, err)
		}
		min, max := w[0], w[0]
		for _, q := range w {
			if q < min {
				min = q
			}
			if q > max {
				max = q
			}
		}
		if max-min > 1 {
			t.Errorf("totale %d non uniforme: %v", total, w)
		}
	}
	w, _ := DistributeWeekly(26, DivQuarter)
	if w.Total() != 26 {
		t.Error(w)
	}
	if _, err := DistributeWeekly(26, DivWhole); err == nil {
		t.Error("26 quarti non ottenibili con compressa intera")
	}
	w, err := DistributeWeekly(20, DivWhole)
	if err != nil || w.Total() != 20 {
		t.Error(w, err)
	}
	for _, q := range w {
		if !q.ValidFor(DivWhole) {
			t.Error("granularità non rispettata", w)
		}
	}
}

func TestDateBasics(t *testing.T) {
	// 10/11/2026 è un martedì (verificato sullo schema reale).
	d := MustDate("2026-11-10")
	if d.Weekday() != 1 || d.Monday().String() != "2026-11-09" {
		t.Error("giorno della settimana", d.Weekday(), d.Monday())
	}
	if d.IT() != "10/11/2026" || d.DayMonth() != "10 nov" {
		t.Error(d.IT(), d.DayMonth())
	}
	if MustDate("2024-02-28").AddDays(1).String() != "2024-02-29" ||
		MustDate("2024-02-29").AddDays(1).String() != "2024-03-01" ||
		MustDate("2025-02-28").AddDays(1).String() != "2025-03-01" {
		t.Error("bisestile")
	}
	if MustDate("2026-12-31").AddDays(1).String() != "2027-01-01" {
		t.Error("fine anno")
	}
	if MustDate("1940-12-09").AgeAt(MustDate("2026-11-10")) != 85 {
		t.Error("età")
	}
	if _, err := ParseDate("2026-02-30"); err == nil {
		t.Error("30 febbraio accettato")
	}
}

// Il conteggio dei giorni non deve dipendere dal fuso della macchina né
// dall'ora legale: si prova con più fusi attraverso i due cambi d'ora.
func TestDSTAndTimezones(t *testing.T) {
	saved := time.Local
	defer func() { time.Local = saved }()
	for _, name := range []string{"Europe/Rome", "America/New_York", "Pacific/Auckland", "UTC"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skip("fusi orari non disponibili")
		}
		time.Local = loc
		for _, c := range []struct{ from, to string }{
			{"2026-03-23", "2026-04-05"}, // 29 marzo 2026: inizio ora legale
			{"2026-10-19", "2026-11-01"}, // 25 ottobre 2026: fine ora legale
			{"2026-12-21", "2027-01-10"},
		} {
			from, to := MustDate(c.from), MustDate(c.to)
			n, prev := 0, Date{}
			EachDay(from, to, func(d Date) {
				if n > 0 && prev.DaysUntil(d) != 1 {
					t.Errorf("%s: salto tra %s e %s", name, prev, d)
				}
				if d.Weekday() != (from.Weekday()+n)%7 {
					t.Errorf("%s: giorno della settimana errato per %s", name, d)
				}
				prev = d
				n++
			})
			if n != from.DaysUntil(to)+1 {
				t.Errorf("%s: %d giorni tra %s e %s", name, n, c.from, c.to)
			}
		}
	}
}

func TestBuildDaysEveryWeekday(t *testing.T) {
	week := Week{1, 2, 3, 4, 5, 6, 7}
	for off := 0; off < 7; off++ {
		start := MustDate("2026-11-09").AddDays(off) // lunedì + off
		end := start.AddDays(34)
		days := BuildDays(start, end, week, nil)
		if len(days) != 35 {
			t.Fatalf("giorni: %d", len(days))
		}
		for _, d := range days {
			if d.Quarters != week[d.Date.Weekday()] {
				t.Errorf("inizio %s, giorno %s: %d", start, d.Date, d.Quarters)
			}
		}
		weeks := CalendarWeeks(days, end.AddDays(1))
		seen := 0
		for _, w := range weeks {
			for i, c := range w {
				if c.Date.IsZero() {
					continue
				}
				if c.Date.Weekday() != i {
					t.Errorf("cella in colonna sbagliata: %s in %d", c.Date, i)
				}
				if c.Day != nil {
					seen++
				}
				if c.Control != c.Date.Equal(end.AddDays(1)) {
					t.Errorf("marcatura controllo errata per %s", c.Date)
				}
			}
		}
		if seen != 35 {
			t.Errorf("celle con dose: %d", seen)
		}
		if off > 0 && !weeks[0][0].Date.IsZero() {
			t.Error("la cella prima dell'inizio deve restare vuota")
		}
	}
}

func TestBuildDaysFirstDayAndZero(t *testing.T) {
	first := Quarters(5)
	days := BuildDays(MustDate("2026-11-10"), MustDate("2026-12-14"), Week{4, 4, 4, 3, 4, 4, 3}, &first)
	if len(days) != 35 || days[0].Quarters != 5 || days[1].Quarters != 4 || days[2].Quarters != 3 {
		t.Fatal(days[:3])
	}
	if !days[0].DeviatesFromWeek(Week{4, 4, 4, 3, 4, 4, 3}) || days[1].DeviatesFromWeek(Week{4, 4, 4, 3, 4, 4, 3}) {
		t.Error("scostamento dallo schema")
	}
	zero := BuildDays(MustDate("2026-11-10"), MustDate("2026-11-10"), Week{}, nil)
	if len(zero) != 1 || zero[0].Quarters.Words() != "NIENTE" {
		t.Error("dose zero")
	}
	if len(BuildDays(MustDate("2026-11-10"), MustDate("2026-11-09"), Week{}, nil)) != 0 {
		t.Error("periodo vuoto")
	}
}

func TestVariation(t *testing.T) {
	if p, ok := VariationPercent(26, 26); !ok || p != 0 {
		t.Error(p, ok)
	}
	if p, ok := VariationPercent(26, 260); !ok || p != 900 {
		t.Error(p, ok)
	}
	if p, _ := VariationPercent(20, 14); p != 30 {
		t.Error(p)
	}
	if _, ok := VariationPercent(0, 26); ok {
		t.Error("primo piano: nessun confronto")
	}
}

var std = []TargetPeriod{{From: MustDate("2020-01-01"), Min: 200, Max: 300, Target: 250}}

func ms(pairs ...any) []Measurement {
	var out []Measurement
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Measurement{MustDate(pairs[i].(string)), INR(pairs[i+1].(int))})
	}
	return out
}

func TestRosendaal(t *testing.T) {
	check := func(name string, got TTR, below, in, above int) {
		t.Helper()
		if got != (TTR{below, in, above}) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	check("entrambe in range", Rosendaal(ms("2026-01-01", 250, "2026-01-11", 280), std, Date{}, Date{}), 0, 10, 0)
	check("entrambe sopra", Rosendaal(ms("2026-01-01", 350, "2026-01-11", 400), std, Date{}, Date{}), 0, 0, 10)
	check("entrambe sotto", Rosendaal(ms("2026-01-01", 150, "2026-01-11", 180), std, Date{}, Date{}), 10, 0, 0)
	// Da 2,0 a 4,0 in 10 giorni: +0,2 al giorno; in range fino al giorno 5 (3,0).
	check("una dentro una fuori", Rosendaal(ms("2026-01-01", 200, "2026-01-11", 400), std, Date{}, Date{}), 0, 5, 5)
	// Da 1,0 a 4,0 in 30 giorni: +0,1 al giorno; giorni 1-9 sotto, 10-20 in range, 21-30 sopra.
	check("attraversamento", Rosendaal(ms("2026-01-01", 100, "2026-01-31", 400), std, Date{}, Date{}), 9, 11, 10)
	check("oltre 56 giorni", Rosendaal(ms("2026-01-01", 250, "2026-02-27", 250), std, Date{}, Date{}), 0, 0, 0)
	check("56 giorni esatti", Rosendaal(ms("2026-01-01", 250, "2026-02-26", 250), std, Date{}, Date{}), 0, 56, 0)
	check("una sola misura", Rosendaal(ms("2026-01-01", 250), std, Date{}, Date{}), 0, 0, 0)
	check("ordine sparso", Rosendaal(ms("2026-01-11", 280, "2026-01-01", 250), std, Date{}, Date{}), 0, 10, 0)
	check("finestra", Rosendaal(ms("2026-01-01", 250, "2026-01-11", 280), std, MustDate("2026-01-07"), Date{}), 0, 5, 0)

	// Cambio di target a metà: INR costante 3,2; range 2-3 fino al 5/1, poi 2,5-3,5.
	changed := []TargetPeriod{
		{From: MustDate("2020-01-01"), To: MustDate("2026-01-05"), Min: 200, Max: 300},
		{From: MustDate("2026-01-06"), Min: 250, Max: 350},
	}
	check("cambio target", Rosendaal(ms("2026-01-01", 320, "2026-01-11", 320), changed, Date{}, Date{}), 0, 6, 4)

	b, in, a, ok := TTR{3, 95, 2}.Percent()
	if !ok || b != 3 || in != 95 || a != 2 {
		t.Error(b, in, a)
	}
	if _, _, _, ok := (TTR{}).Percent(); ok {
		t.Error("nessun dato deve dare ok=false")
	}
}

func TestPositionAndLabel(t *testing.T) {
	d := MustDate("2026-11-10")
	if PositionOf(307, d, std) != PosAbove || PositionOf(300, d, std) != PosInRange ||
		PositionOf(200, d, std) != PosInRange || PositionOf(189, d, std) != PosBelow {
		t.Error("posizione")
	}
	if PositionOf(250, MustDate("2019-01-01"), std) != PosUnknown {
		t.Error("senza range vigente")
	}
	if std[0].Label() != "2,5 (2-3)" {
		t.Error(std[0].Label())
	}
	if (TargetPeriod{Min: 250, Max: 350}).Label() != "2,5-3,5" {
		t.Error((TargetPeriod{Min: 250, Max: 350}).Label())
	}
	in, tot := InRangeCount(ms("2026-01-01", 250, "2026-01-11", 350, "2019-01-01", 250), std)
	if in != 1 || tot != 2 {
		t.Error(in, tot)
	}
}

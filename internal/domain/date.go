// SPDX-License-Identifier: AGPL-3.0-or-later

// Package domain contiene i tipi e i calcoli puri di VeronaTAO.
// Non dipende dal database né dall'interfaccia web.
package domain

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Date è una data clinica: solo anno, mese e giorno, senza ora e senza fuso.
// Internamente è la mezzanotte UTC, così l'aritmetica sui giorni non è mai
// influenzata dall'ora legale. Il valore zero significa "data assente".
type Date struct{ t time.Time }

const isoLayout = "2006-01-02"

// NewDate costruisce una data da anno, mese, giorno.
func NewDate(y int, m time.Month, d int) Date {
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// ParseDate legge una data nel formato AAAA-MM-GG. La stringa vuota produce
// la data zero.
func ParseDate(s string) (Date, error) {
	if s == "" {
		return Date{}, nil
	}
	t, err := time.Parse(isoLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("data non valida: %q", s)
	}
	return Date{t}, nil
}

// MustDate è ParseDate per valori noti a priori (test, costanti).
func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// Today restituisce la data odierna secondo l'orologio locale della macchina.
func Today() Date {
	n := time.Now()
	return NewDate(n.Year(), n.Month(), n.Day())
}

func (d Date) IsZero() bool { return d.t.IsZero() }

// String restituisce AAAA-MM-GG, o stringa vuota per la data zero.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format(isoLayout)
}

// IT restituisce GG/MM/AAAA.
func (d Date) IT() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format("02/01/2006")
}

var monthsShort = [...]string{"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"}

// DayMonth restituisce la forma breve usata nel calendario, es. "10 nov".
func (d Date) DayMonth() string {
	return fmt.Sprintf("%02d %s", d.t.Day(), monthsShort[d.t.Month()-1])
}

func (d Date) Year() int { return d.t.Year() }

func (d Date) AddDays(n int) Date { return Date{d.t.AddDate(0, 0, n)} }

// DaysUntil restituisce il numero di giorni da d a o (positivo se o è dopo).
func (d Date) DaysUntil(o Date) int { return int(o.t.Sub(d.t).Hours() / 24) }

func (d Date) Before(o Date) bool { return d.t.Before(o.t) }
func (d Date) After(o Date) bool  { return d.t.After(o.t) }
func (d Date) Equal(o Date) bool  { return d.t.Equal(o.t) }

// Weekday restituisce il giorno della settimana con lunedì = 0 e domenica = 6.
func (d Date) Weekday() int { return (int(d.t.Weekday()) + 6) % 7 }

// Monday restituisce il lunedì della settimana che contiene d.
func (d Date) Monday() Date { return d.AddDays(-d.Weekday()) }

// AgeAt restituisce gli anni compiuti alla data indicata.
func (d Date) AgeAt(on Date) int {
	a := on.t.Year() - d.t.Year()
	if on.t.Month() < d.t.Month() || (on.t.Month() == d.t.Month() && on.t.Day() < d.t.Day()) {
		a--
	}
	return a
}

// WeekdayShort e WeekdayLong sono le etichette dei giorni, lunedì per primo.
var (
	WeekdayShort = [7]string{"LUN", "MAR", "MER", "GIO", "VEN", "SAB", "DOM"}
	WeekdayLong  = [7]string{"Lunedì", "Martedì", "Mercoledì", "Giovedì", "Venerdì", "Sabato", "Domenica"}
)

// EachDay è l'unico punto del codice che itera su un intervallo di date
// (estremi inclusi). Se to precede from non viene visitato alcun giorno.
func EachDay(from, to Date, fn func(Date)) {
	if from.IsZero() || to.IsZero() {
		return
	}
	for d := from; !d.After(to); d = d.AddDays(1) {
		fn(d)
	}
}

// Value e Scan memorizzano la data come testo AAAA-MM-GG; la data assente è
// la stringa vuota.
func (d Date) Value() (driver.Value, error) { return d.String(), nil }

func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case string:
		p, err := ParseDate(v)
		*d = p
		return err
	case []byte:
		p, err := ParseDate(string(v))
		*d = p
		return err
	}
	return fmt.Errorf("tipo non supportato per Date: %T", src)
}

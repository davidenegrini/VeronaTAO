// SPDX-License-Identifier: AGPL-3.0-or-later

package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Quarters è una dose di anticoagulante orale espressa in quarti di compressa.
// È sempre un intero: i milligrammi sono un valore derivato.
type Quarters int

// Divisibility indica in quante parti la compressa può essere divisa.
type Divisibility string

const (
	DivQuarter Divisibility = "quarter"
	DivHalf    Divisibility = "half"
	DivWhole   Divisibility = "whole"
)

// Step restituisce la frazione minima somministrabile, in quarti.
func (d Divisibility) Step() int {
	switch d {
	case DivHalf:
		return 2
	case DivWhole:
		return 4
	}
	return 1
}

func (d Divisibility) Label() string {
	switch d {
	case DivHalf:
		return "divisibile a metà"
	case DivWhole:
		return "non divisibile"
	}
	return "divisibile in quarti"
}

// ValidFor verifica che la dose sia somministrabile con quella compressa.
// È un vincolo tecnico, non clinico.
func (q Quarters) ValidFor(d Divisibility) bool {
	return q >= 0 && int(q)%d.Step() == 0
}

// MgMilli restituisce la dose in millesimi di milligrammo, data la
// concentrazione della compressa in millesimi di mg.
func (q Quarters) MgMilli(tabletMgMilli int) int { return int(q) * tabletMgMilli / 4 }

// FormatMg scrive millesimi di mg come numero italiano, es. 32500 -> "32,5".
func FormatMg(milli int) string {
	s := strconv.FormatFloat(float64(milli)/1000, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return strings.Replace(s, ".", ",", 1)
}

// ParseDecimal legge un numero con virgola o punto e lo restituisce
// moltiplicato per scale (es. scale 100 per l'INR, 1000 per i mg), senza
// passare da float. Rifiuta più decimali di quanti scale ne consenta.
func ParseDecimal(s string, scale int) (int, error) {
	s = strings.TrimSpace(strings.Replace(s, ",", ".", 1))
	if s == "" {
		return 0, errors.New("valore mancante")
	}
	digits := len(strconv.Itoa(scale)) - 1
	intPart, frac, _ := strings.Cut(s, ".")
	if len(frac) > digits {
		return 0, fmt.Errorf("troppi decimali: %q", s)
	}
	for len(frac) < digits {
		frac += "0"
	}
	if intPart == "" {
		intPart = "0"
	}
	n, err := strconv.Atoi(intPart + frac)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("numero non valido: %q", s)
	}
	return n, nil
}

// FormatScaled scrive un intero scalato con un numero fisso di decimali e la
// virgola, es. FormatScaled(307, 2) -> "3,07".
func FormatScaled(v, decimals int) string {
	div := 1
	for i := 0; i < decimals; i++ {
		div *= 10
	}
	return fmt.Sprintf("%d,%0*d", v/div, decimals, v%div)
}

var wholeWords = [...]string{"", "UNA", "DUE", "TRE", "QUATTRO", "CINQUE", "SEI", "SETTE", "OTTO"}

func wholeWord(n int) string {
	if n < len(wholeWords) {
		return wholeWords[n]
	}
	return strconv.Itoa(n)
}

// Words restituisce la dose a parole per la stampa consegnata al paziente.
// La dose zero è sempre "NIENTE": la cella non resta mai vuota.
func (q Quarters) Words() string {
	if q <= 0 {
		return "NIENTE"
	}
	whole, rem := int(q)/4, int(q)%4
	if whole == 0 {
		switch rem {
		case 1:
			return "UN quarto"
		case 2:
			return "MEZZA"
		default:
			return "TRE quarti"
		}
	}
	w := wholeWord(whole)
	switch rem {
	case 1:
		return w + " + un quarto"
	case 2:
		return w + " e MEZZA"
	case 3:
		return w + " + tre quarti"
	}
	return w
}

// Week è lo schema settimanale, da lunedì (indice 0) a domenica (indice 6).
type Week [7]Quarters

func (w Week) Total() Quarters {
	var t Quarters
	for _, q := range w {
		t += q
	}
	return t
}

// String restituisce la forma compatta dello storico, es. "4 | 4 | 4 | 3 | 4 | 4 | 3".
func (w Week) String() string {
	p := make([]string, 7)
	for i, q := range w {
		p[i] = strconv.Itoa(int(q))
	}
	return strings.Join(p, " | ")
}

// DistributeWeekly ripartisce una dose settimanale totale sui sette giorni nel
// modo più uniforme possibile. È una pura operazione aritmetica: il totale lo
// indica il medico, che può poi modificare ogni singolo giorno.
func DistributeWeekly(total Quarters, d Divisibility) (Week, error) {
	var w Week
	step := d.Step()
	if total < 0 {
		return w, errors.New("la dose settimanale non può essere negativa")
	}
	if int(total)%step != 0 {
		return w, errors.New("la dose settimanale non è ottenibile con questa compressa")
	}
	units := int(total) / step
	base, rem := units/7, units%7
	for i := 0; i < 7; i++ {
		u := base
		// I giorni con l'unità in più sono distanziati in modo regolare.
		if (i+1)*rem/7 > i*rem/7 {
			u++
		}
		w[i] = Quarters(u * step)
	}
	return w, nil
}

// WeeklyQuartersFromMg converte una dose settimanale in mg (millesimi) in
// quarti di compressa. Restituisce errore se non corrisponde a un numero
// intero di quarti.
func WeeklyQuartersFromMg(mgMilli, tabletMgMilli int) (Quarters, error) {
	if tabletMgMilli <= 0 {
		return 0, errors.New("farmaco non valido")
	}
	n := mgMilli * 4
	if n%tabletMgMilli != 0 {
		return 0, errors.New("la dose in mg non corrisponde a un numero intero di quarti di compressa")
	}
	return Quarters(n / tabletMgMilli), nil
}

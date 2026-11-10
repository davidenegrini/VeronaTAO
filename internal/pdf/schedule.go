// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pdf produce lo schema posologico stampato per il paziente.
// È separato dal resto dell'applicazione: riceve una struttura dati già
// pronta e restituisce un PDF. Non contiene testi clinici propri: ogni testo
// rivolto al paziente proviene dal medico o dalle impostazioni dello studio.
package pdf

import (
	"bytes"
	"math"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/davidenegrini/veronatao/internal/domain"
)

type HistoryRow struct {
	Date, INR, WeeklyMg string
}

// Schedule è tutto ciò che serve per stampare lo schema.
type Schedule struct {
	HeaderLines  []string
	Title        string
	PatientName  string
	BirthDate    string
	ExternalCode string
	Phone        string
	Diagnosis    string
	TargetLabel  string
	TTRLabel     string // vuoto = non stampato
	INRDate      string
	INR          string
	WeeklyMg     string
	NextControl  string
	DrugLabel    string
	Days         []domain.PlanDay
	ControlDate  domain.Date // giorno del prossimo controllo, zero se assente
	Comment      string
	History      []HistoryRow
	Doctor       string
	FooterText   string
	TemplateCode string
}

const (
	pageW, pageH = 210.0, 297.0
	margin       = 12.0
	rowH         = 10.0
	labelW       = 12.0
	dateW        = 15.0
	weeksPerRow  = 3
	blocksPage   = 2
)

type renderer struct {
	p  *fpdf.Fpdf
	tr func(string) string
	s  Schedule
}

// Render produce il PDF dello schema.
func Render(s Schedule) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(margin, margin, margin)
	p.SetAutoPageBreak(false, 0)
	p.SetTitle(s.Title, true)
	p.SetCreator("VeronaTAO", true)
	r := &renderer{p: p, tr: p.UnicodeTranslatorFromDescriptor(""), s: s}

	weeks := domain.CalendarWeeks(s.Days, s.ControlDate)
	perPage := weeksPerRow * blocksPage
	pages := (len(weeks) + perPage - 1) / perPage
	if pages == 0 {
		pages = 1
	}
	for pg := 0; pg < pages; pg++ {
		p.AddPage()
		var y float64
		if pg == 0 {
			y = r.fullHeader()
		} else {
			y = r.shortHeader()
		}
		from := pg * perPage
		for b := 0; b < blocksPage; b++ {
			lo := from + b*weeksPerRow
			if lo >= len(weeks) {
				break
			}
			hi := lo + weeksPerRow
			if hi > len(weeks) {
				hi = len(weeks)
			}
			y = r.block(y, weeks[lo:hi]) + 2
		}
		if pg == 0 {
			r.bottom(y + 3)
		}
		r.footer(pg+1, pages)
	}
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *renderer) text(x, y float64, style string, size float64, s string) {
	r.p.SetFont("Helvetica", style, size)
	r.p.Text(x, y, r.tr(s))
}

func (r *renderer) centered(y float64, style string, size float64, s string) {
	r.p.SetFont("Helvetica", style, size)
	s = r.tr(s)
	r.p.Text((pageW-r.p.GetStringWidth(s))/2, y, s)
}

// fit accorcia il testo finché entra nella larghezza indicata.
func (r *renderer) fit(s string, w float64) string {
	t := r.tr(s)
	if r.p.GetStringWidth(t) <= w {
		return s
	}
	rs := []rune(s)
	for len(rs) > 1 {
		rs = rs[:len(rs)-1]
		if r.p.GetStringWidth(r.tr(string(rs)+"…")) <= w {
			return string(rs) + "…"
		}
	}
	return string(rs)
}

// wrap spezza il testo in righe che entrano nella larghezza indicata, con il
// font corrente. Le righe restituite sono già convertite per la stampa.
func (r *renderer) wrap(s string, w float64) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if line != "" && r.p.GetStringWidth(r.tr(try)) > w {
				out = append(out, r.tr(line))
				line = word
			} else {
				line = try
			}
		}
		out = append(out, r.tr(line))
	}
	return out
}

func (r *renderer) field(x, y float64, label, value string, big bool) {
	r.text(x, y, "", 10, label)
	size := 11.0
	if big {
		size = 13
	}
	r.p.SetFont("Helvetica", "B", size)
	lw := 34.0
	r.p.Text(x+lw, y, r.tr(r.fit(value, pageW-margin-x-lw)))
}

func (r *renderer) fullHeader() float64 {
	s := r.s
	y := margin + 4
	for i, line := range s.HeaderLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		style, size := "", 9.5
		if i == 0 {
			style, size = "B", 11
		}
		r.centered(y, style, size, line)
		y += 4.6
	}
	y += 5
	r.centered(y, "B", 14, s.Title)
	y += 9

	left, right := margin, 112.0
	r.field(left, y, "Nome", s.PatientName, true)
	y += 6.5
	r.field(left, y, "Data di nascita", s.BirthDate, false)
	if s.ExternalCode != "" {
		r.field(right, y, "Codice", s.ExternalCode, false)
	}
	y += 6
	if s.Phone != "" {
		r.field(left, y, "Telefono", s.Phone, false)
		y += 6
	}
	r.field(left, y, "Diagnosi principale", s.Diagnosis, false)
	y += 6
	r.field(left, y, "Range INR", s.TargetLabel, false)
	if s.TTRLabel != "" {
		r.field(right, y, "Tempo in range", s.TTRLabel, false)
	}
	y += 6
	inr := s.INR
	if inr == "" {
		inr = "-"
	}
	r.field(left, y, "Data controllo", s.INRDate, false)
	r.field(right, y, "INR", inr, true)
	y += 6.5
	r.field(left, y, "Dose settimanale", s.WeeklyMg+" mg", false)
	r.field(right, y, "Prossimo controllo", s.NextControl, true)
	y += 8

	r.text(margin, y, "B", 10.5, "RIPARTIZIONE GIORNALIERA DEL FARMACO ANTICOAGULANTE")
	r.p.SetFont("Helvetica", "B", 10.5)
	d := r.tr("(" + s.DrugLabel + ")")
	r.p.Text(pageW-margin-r.p.GetStringWidth(d), y, d)
	return y + 2.5
}

func (r *renderer) shortHeader() float64 {
	y := margin + 5
	r.text(margin, y, "B", 12, r.s.PatientName+"  -  nato/a il "+r.s.BirthDate)
	y += 6
	r.text(margin, y, "", 10, r.s.Title+" (segue)  -  "+r.s.DrugLabel)
	return y + 4
}

// block disegna fino a tre settimane affiancate, righe da lunedì a domenica.
func (r *renderer) block(y float64, weeks [][7]domain.CalendarCell) float64 {
	p := r.p
	weekW := (pageW - 2*margin - labelW) / weeksPerRow
	doseW := weekW - dateW
	p.SetDrawColor(0, 0, 0)
	p.SetLineWidth(0.2)
	for day := 0; day < 7; day++ {
		ry := y + float64(day)*rowH
		p.Rect(margin, ry, labelW, rowH, "D")
		style := ""
		if day == 6 {
			style = "B"
		}
		r.text(margin+1.5, ry+rowH/2+1.4, style, 10, domain.WeekdayShort[day])
		for w := 0; w < weeksPerRow; w++ {
			x := margin + labelW + float64(w)*weekW
			p.Rect(x, ry, dateW, rowH, "D")
			p.Rect(x+dateW, ry, doseW, rowH, "D")
			if w >= len(weeks) {
				continue
			}
			c := weeks[w][day]
			if c.Date.IsZero() {
				continue
			}
			r.text(x+1.2, ry+rowH/2+1.4, "", 9.5, c.Date.DayMonth())
			if c.Day != nil {
				r.doseCell(x+dateW, ry, doseW, *c.Day)
			} else if c.Control {
				r.text(x+dateW+1.5, ry+rowH/2+1.6, "B", 11, "CONTROLLO")
			}
		}
	}
	p.SetLineWidth(0.5)
	p.Rect(margin, y, pageW-2*margin, 7*rowH, "D")
	p.SetLineWidth(0.2)
	return y + 7*rowH
}

func (r *renderer) doseCell(x, y, w float64, d domain.PlanDay) {
	hasHep := d.Heparin.Present()
	avkW := w
	if hasHep {
		avkW = w * 0.56
	}
	r.p.SetFont("Helvetica", "B", 9.5)
	words := d.Quarters.Words()
	if d.Quarters == 0 {
		// Dose zero: sempre scritta per esteso, mai cella vuota.
		r.text(x+1.5, y+rowH/2+1.5, "B", 10.5, words)
	} else {
		size := 9.5
		for r.p.GetStringWidth(r.tr(words)) > avkW-2.5 && size > 6.5 {
			size -= 0.5
			r.p.SetFont("Helvetica", "B", size)
		}
		r.p.Text(x+1.5, y+3.9, r.tr(words))
		r.pictogram(x+1.5, y+5.0, 4.4, avkW-3, d.Quarters)
	}
	if hasHep {
		hx := x + avkW
		r.p.Line(hx, y+1, hx, y+rowH-1)
		r.p.SetFont("Helvetica", "B", 7)
		r.p.Text(hx+1, y+3.3, r.tr(r.fit(d.Heparin.Drug, w-avkW-2)))
		r.p.SetFont("Helvetica", "", 7)
		r.p.Text(hx+1, y+6.2, r.tr(r.fit(strings.TrimSpace(d.Heparin.Dose+" "+d.Heparin.Unit), w-avkW-2)))
		n := d.Heparin.Times
		if n > 4 {
			n = 4
		}
		for i := 0; i < n; i++ {
			r.syringe(hx+1+float64(i)*5.6, y+8.3)
		}
	}
}

// pictogram disegna le compresse: un cerchio diviso in quattro spicchi, con
// riempiti (pieno contro vuoto, leggibile in bianco e nero) quelli da
// assumere.
func (r *renderer) pictogram(x, y, diam, maxW float64, q domain.Quarters) {
	p := r.p
	rad := diam / 2
	n := (int(q) + 3) / 4
	step := diam + 0.9
	if float64(n)*step > maxW && n > 0 {
		step = maxW / float64(n)
		if step < diam*0.6 {
			step = diam * 0.6
		}
	}
	left := int(q)
	p.SetFillColor(70, 70, 70)
	p.SetLineWidth(0.15)
	for i := 0; i < n; i++ {
		cx, cy := x+rad+float64(i)*step, y+rad
		fill := left
		if fill > 4 {
			fill = 4
		}
		left -= fill
		if fill == 4 {
			p.Circle(cx, cy, rad, "FD")
			continue
		}
		for s := 0; s < fill; s++ {
			pts := []fpdf.PointType{{X: cx, Y: cy}}
			for k := 0; k <= 8; k++ {
				a := -math.Pi/2 + (float64(s)+float64(k)/8)*math.Pi/2
				pts = append(pts, fpdf.PointType{X: cx + rad*math.Cos(a), Y: cy + rad*math.Sin(a)})
			}
			p.Polygon(pts, "F")
		}
		p.Circle(cx, cy, rad, "D")
		p.Line(cx-rad, cy, cx+rad, cy)
		p.Line(cx, cy-rad, cx, cy+rad)
	}
	p.SetLineWidth(0.2)
}

// syringe disegna una piccola siringa stilizzata.
func (r *renderer) syringe(x, y float64) {
	p := r.p
	p.SetLineWidth(0.2)
	p.Line(x, y-0.7, x, y+0.7)           // impugnatura dello stantuffo
	p.Line(x, y, x+1.0, y)               // asta
	p.Rect(x+1.0, y-0.65, 2.4, 1.3, "D") // corpo
	p.Line(x+3.4, y, x+4.9, y)           // ago
}

func (r *renderer) bottom(y float64) {
	p, s := r.p, r.s
	boxW, boxH := 92.0, 22.0
	p.SetLineWidth(0.2)
	p.Rect(margin, y, boxW, boxH, "D")
	r.text(margin+1.5, y+4.2, "", 9.5, "Commenti")
	p.SetFont("Helvetica", "B", 10)
	lines := r.wrap(s.Comment, boxW-3)
	if len(lines) > 4 {
		lines = lines[:4]
	}
	for i, l := range lines {
		p.Text(margin+1.5, y+9+float64(i)*4.3, l)
	}

	hx := margin + boxW + 8
	r.text(hx, y+4, "B", 10, "Dati relativi agli ultimi controlli")
	r.text(hx, y+9, "", 9, "Data")
	r.text(hx+30, y+9, "", 9, "INR")
	r.text(hx+50, y+9, "", 9, "Dose settimanale")
	for i, h := range s.History {
		ly := y + 13.5 + float64(i)*4.4
		r.text(hx, ly, "B", 9.5, h.Date)
		r.text(hx+30, ly, "B", 9.5, h.INR)
		if h.WeeklyMg != "" {
			r.text(hx+50, ly, "B", 9.5, h.WeeklyMg+" mg")
		}
	}
	y += boxH + 5
	r.text(hx, y, "", 10, "Il medico")
	r.text(hx+20, y, "B", 10.5, s.Doctor)
	y += 5
	if strings.TrimSpace(s.FooterText) != "" {
		p.SetFont("Helvetica", "", 8)
		{
			for _, l := range r.wrap(s.FooterText, pageW-2*margin) {
				if y > pageH-margin-1 {
					return
				}
				p.Text(margin, y, l)
				y += 3.6
			}
		}
	}
}

func (r *renderer) footer(page, pages int) {
	t := r.s.TemplateCode
	if pages > 1 {
		t += "  -  pagina " + strconv.Itoa(page) + " di " + strconv.Itoa(pages)
	}
	r.centered(pageH-margin+3, "", 7.5, t)
}

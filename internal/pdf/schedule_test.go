// SPDX-License-Identifier: AGPL-3.0-or-later

package pdf

import (
	"bytes"
	"os"
	"testing"

	"github.com/davidenegrini/veronatao/internal/domain"
)

func sample(end string) Schedule {
	first := domain.Quarters(5)
	days := domain.BuildDays(domain.MustDate("2026-11-10"), domain.MustDate(end), domain.Week{4, 4, 4, 3, 4, 4, 3}, &first)
	if len(days) > 5 {
		days[3].Quarters = 0
		days[3].Heparin = domain.Heparin{Drug: "Enoxaparina", Dose: "4000", Unit: "UI", Times: 2}
		days[4].Quarters = 7
		days[4].Heparin = domain.Heparin{Drug: "Enoxaparina", Dose: "4000", Unit: "UI", Times: 1}
		days[5].Quarters = 10
	}
	return Schedule{
		HeaderLines: []string{"Studio medico di prova", "Via Roma 1 - Verona", "Tel. 045 000000"},
		Title:       "Scheda personale per il dosaggio della terapia anticoagulante",
		PatientName: "ROSSI MARIO", BirthDate: "01/01/1940", ExternalCode: "X1", Phone: "045 111111",
		Diagnosis: "Fibrillazione atriale parossistica", TargetLabel: "2,5 (2-3)", TTRLabel: "95%",
		INRDate: "10/11/2026", INR: "3,07", WeeklyMg: "32,5", NextControl: "martedì 15/12/2026", DrugLabel: "Coumadin 5 mg",
		Days: days, ControlDate: domain.MustDate(end).AddDays(1),
		Comment: "Testo scritto dal medico, con lettere accentate: è così perché più giù.",
		History: []HistoryRow{{"29/09/2026", "2,53", "32,5"}, {"31/08/2026", "1,89", "32,5"}, {"21/07/2026", "2,85", "32,5"}},
		Doctor:  "Dott. Bianchi", FooterText: "Testo libero a piè di pagina scritto dallo studio.", TemplateCode: "VTAO-01 - Rev. 1",
	}
}

func TestRender(t *testing.T) {
	for name, end := range map[string]string{"una-pagina": "2026-12-14", "piu-pagine": "2027-02-01", "un-giorno": "2026-11-10"} {
		s := sample(end)
		b, err := Render(s)
		if err != nil {
			t.Fatal(name, err)
		}
		if !bytes.HasPrefix(b, []byte("%PDF")) {
			t.Fatal(name, "non è un PDF")
		}
		if dir := os.Getenv("VTAO_PDF_OUT"); dir != "" {
			_ = os.WriteFile(dir+"/"+name+".pdf", b, 0o600)
		}
	}
}

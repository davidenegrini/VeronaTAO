// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"os"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/davidenegrini/veronatao/internal/domain"
	"github.com/davidenegrini/veronatao/internal/pdf"
)

func samplePDF(t *testing.T) []byte {
	t.Helper()
	days := domain.BuildDays(domain.MustDate("2026-11-10"), domain.MustDate("2026-12-14"), domain.Week{4, 4, 4, 3, 4, 4, 3}, nil)
	b, err := pdf.Render(pdf.Schedule{Title: "Schema", PatientName: "ROSSI MARIA", BirthDate: "09/12/1940", Days: days, DrugLabel: "Coumadin 5 mg", TemplateCode: "T"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decrypt(enc []byte, password string) ([]byte, error) {
	conf := model.NewDefaultConfiguration()
	conf.UserPW = password
	conf.Offline = true
	var out bytes.Buffer
	err := api.Decrypt(context.Background(), bytes.NewReader(enc), &out, conf)
	return out.Bytes(), err
}

func TestProtectPDF(t *testing.T) {
	plain := samplePDF(t)
	if !bytes.Contains(plain, []byte("ROSSI")) && !bytes.Contains(plain, []byte("/Title")) {
		t.Log("il PDF di prova è compresso: si verifica comunque la cifratura")
	}
	enc, err := ProtectPDF(plain, "abcd-efgh-jkmn")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(enc, []byte("%PDF")) || !bytes.Contains(enc, []byte("/Encrypt")) {
		t.Fatal("il risultato non è un PDF cifrato")
	}
	// AES-256: versione 5 del gestore di sicurezza, chiave di 256 bit.
	if !bytes.Contains(enc, []byte("/V 5")) || !bytes.Contains(enc, []byte("AESV3")) {
		t.Error("cifratura attesa AES a 256 bit (V 5, AESV3)")
	}
	if dec, err := decrypt(enc, "abcd-efgh-jkmn"); err != nil || len(dec) == 0 {
		t.Error("con la password giusta il PDF deve aprirsi:", err)
	}
	if _, err := decrypt(enc, "password-sbagliata"); err == nil {
		t.Error("il PDF si apre con la password sbagliata")
	}
	if _, err := decrypt(enc, ""); err == nil {
		t.Error("il PDF si apre senza password")
	}
	if _, err := ProtectPDF(plain, "corta"); err == nil {
		t.Error("password troppo corta accettata")
	}
	if dir := os.Getenv("VTAO_PDF_OUT"); dir != "" {
		_ = os.WriteFile(dir+"/protetto.pdf", enc, 0o600)
	}
}

func TestValidAddress(t *testing.T) {
	for _, ok := range []string{"mario.rossi@example.it", "a+b@studio.example"} {
		if !ValidAddress(ok) {
			t.Errorf("%q rifiutato", ok)
		}
	}
	for _, bad := range []string{"", "senza-chiocciola", "a@b.it, c@d.it", "a@b.it\r\nBcc: x@y.it", "Mario <a@b.it>", "a b@c.it"} {
		if ValidAddress(bad) {
			t.Errorf("%q accettato", bad)
		}
	}
}

var cfg = Config{Host: "127.0.0.1", Port: 587, Username: "studio@example.it", Password: "segreta", From: "studio@example.it", FromName: "Studio medico"}

func TestBuildMessage(t *testing.T) {
	att := []byte("%PDF-finto contenuto cifrato")
	raw, err := Build(cfg, Message{To: "paziente@example.it", Subject: "Documento dal suo medico\r\nBcc: altri@example.it", Body: "Riga uno.\nRiga due è così.", AttachmentName: "schema-terapia.pdf", Attachment: att})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("un a capo nell'oggetto ha introdotto un'intestazione")
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if !strings.HasPrefix(subj, "Documento dal suo medico") || msg.Header.Get("To") != "<paziente@example.it>" {
		t.Error("intestazioni:", subj, msg.Header.Get("To"))
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var parts [][]byte
	var names []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		parts = append(parts, b)
		names = append(names, p.FileName())
	}
	if len(parts) != 2 || !strings.Contains(string(parts[0]), "Riga due è così.") || !bytes.Equal(parts[1], att) || names[1] != "schema-terapia.pdf" {
		t.Errorf("parti del messaggio: %d %q", len(parts), names)
	}
	if _, err := Build(cfg, Message{To: "non valido"}); err == nil {
		t.Error("destinatario non valido accettato")
	}
	if _, err := Build(Config{}, Message{To: "a@b.it"}); err == nil {
		t.Error("configurazione vuota accettata")
	}
}

// fakeSMTP è un server di posta minimo, senza cifratura, per i test.
func fakeSMTP(t *testing.T, starttls bool) (port int, received chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	received = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { conn.Write([]byte(s + "\r\n")) }
		say("220 prova ESMTP")
		var log strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			log.WriteString(line)
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				if starttls {
					say("250-prova\r\n250-STARTTLS\r\n250 AUTH PLAIN")
				} else {
					say("250-prova\r\n250 AUTH PLAIN")
				}
			case strings.HasPrefix(cmd, "AUTH"):
				say("235 ok")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				say("250 ok")
			case cmd == "DATA":
				say("354 vai")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					log.WriteString(l)
				}
				say("250 accettato")
			case cmd == "QUIT":
				say("221 ciao")
				received <- log.String()
				return
			default:
				say("502 non supportato")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, received
}

func TestSend(t *testing.T) {
	port, received := fakeSMTP(t, false)
	c := cfg
	c.Port, c.insecure = port, true
	if err := Send(c, Message{To: "paziente@example.it", Subject: "Documento", Body: "Testo", AttachmentName: "schema-terapia.pdf", Attachment: []byte("%PDF-x")}); err != nil {
		t.Fatal(err)
	}
	got := <-received
	for _, want := range []string{"MAIL FROM:<studio@example.it>", "RCPT TO:<paziente@example.it>", "AUTH PLAIN", `filename="schema-terapia.pdf"`} {
		if !strings.Contains(got, want) {
			t.Errorf("manca %q nella sessione SMTP", want)
		}
	}
}

// Senza cifratura della connessione l'invio deve essere rifiutato prima di
// trasmettere credenziali o messaggio.
func TestSendRefusesUnencryptedServer(t *testing.T) {
	port, received := fakeSMTP(t, false)
	c := cfg
	c.Port = port
	err := Send(c, Message{To: "paziente@example.it", Subject: "Documento", Body: "Testo", Attachment: []byte("%PDF-x")})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatal("invio su connessione in chiaro non rifiutato:", err)
	}
	select {
	case got := <-received:
		if strings.Contains(got, "AUTH") || strings.Contains(got, "MAIL FROM") {
			t.Error("credenziali o messaggio trasmessi in chiaro")
		}
	default:
	}
}

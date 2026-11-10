// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mail protegge un PDF con password e lo invia come allegato tramite
// un server SMTP scelto dallo studio. È l'unica parte di VeronaTAO che può
// comunicare con l'esterno, ed è inattiva finché non viene configurata.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func init() {
	// La libreria PDF non deve creare file di configurazione sul computer.
	model.ConfigPath = "disable"
}

// MinPassword è la lunghezza minima della password del PDF.
const MinPassword = 8

// ProtectPDF restituisce una copia del PDF cifrata con AES a 256 bit: per
// aprirla serve la password. La stampa resta consentita.
func ProtectPDF(pdf []byte, password string) ([]byte, error) {
	if len(password) < MinPassword {
		return nil, fmt.Errorf("la password del PDF deve avere almeno %d caratteri", MinPassword)
	}
	owner := make([]byte, 24)
	if _, err := rand.Read(owner); err != nil {
		return nil, err
	}
	conf := model.NewAESConfiguration(password, hex.EncodeToString(owner), 256)
	conf.Offline = true
	var out bytes.Buffer
	if err := api.Encrypt(context.Background(), bytes.NewReader(pdf), &out, conf); err != nil {
		return nil, fmt.Errorf("cifratura del PDF non riuscita: %w", err)
	}
	return out.Bytes(), nil
}

// Config descrive il server SMTP dello studio.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // indirizzo del mittente
	FromName string
	// insecure consente l'invio senza TLS: usato solo dai test automatici.
	insecure bool
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" || c.Port <= 0 {
		return errors.New("server SMTP non configurato")
	}
	if _, err := netmail.ParseAddress(c.From); err != nil {
		return errors.New("indirizzo del mittente non valido")
	}
	return nil
}

type Message struct {
	To             string
	Subject        string
	Body           string
	AttachmentName string
	Attachment     []byte
}

// ValidAddress verifica che l'indirizzo sia un singolo indirizzo email
// senza caratteri di controllo.
func ValidAddress(addr string) bool {
	if strings.ContainsAny(addr, "\r\n,;<> ") || !strings.Contains(addr, "@") {
		return false
	}
	a, err := netmail.ParseAddress(addr)
	return err == nil && a.Address == addr
}

func oneLine(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
}

func b64lines(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.String()
}

// Build compone il messaggio in formato MIME.
func Build(c Config, m Message) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !ValidAddress(m.To) {
		return nil, errors.New("indirizzo email del destinatario non valido")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domain := c.From[strings.LastIndex(c.From, "@")+1:]
	boundary := "vtao-" + hex.EncodeToString(id)
	from := (&netmail.Address{Name: oneLine(c.FromName), Address: c.From}).String()
	name := strings.NewReplacer(`"`, "", "\r", "", "\n", "", "/", "-", `\`, "-").Replace(m.AttachmentName)

	var b bytes.Buffer
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\r\n", args...) }
	w("From: %s", from)
	w("To: <%s>", m.To)
	w("Subject: %s", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	w("Date: %s", time.Now().Format(time.RFC1123Z))
	w("Message-ID: <%s@%s>", hex.EncodeToString(id), domain)
	w("MIME-Version: 1.0")
	w(`Content-Type: multipart/mixed; boundary="%s"`, boundary)
	w("")
	w("--%s", boundary)
	w("Content-Type: text/plain; charset=utf-8")
	w("Content-Transfer-Encoding: base64")
	w("")
	b.WriteString(b64lines([]byte(strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n"))))
	if len(m.Attachment) > 0 {
		w("--%s", boundary)
		w(`Content-Type: application/pdf; name="%s"`, name)
		w("Content-Transfer-Encoding: base64")
		w(`Content-Disposition: attachment; filename="%s"`, name)
		w("")
		b.WriteString(b64lines(m.Attachment))
	}
	w("--%s--", boundary)
	return b.Bytes(), nil
}

// Send invia il messaggio. La connessione è sempre cifrata: TLS diretto sulla
// porta 465, STARTTLS obbligatorio sulle altre. Se il server non offre la
// cifratura l'invio viene rifiutato, così né le credenziali né il messaggio
// viaggiano mai in chiaro.
func Send(c Config, m Message) error {
	raw, err := Build(c, m)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsConf := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if c.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConf)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("impossibile collegarsi al server di posta %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("il server di posta non risponde correttamente: %w", err)
	}
	defer cl.Close()
	if c.Port != 465 && !c.insecure {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("il server di posta non offre una connessione cifrata (STARTTLS): invio annullato")
		}
		if err := cl.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("connessione cifrata con il server di posta non riuscita: %w", err)
		}
	}
	if c.Username != "" {
		var auth smtp.Auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
		if c.insecure {
			auth = insecureAuth{c.Username, c.Password}
		}
		if err := cl.Auth(auth); err != nil {
			return fmt.Errorf("il server di posta ha rifiutato le credenziali: %w", err)
		}
	}
	if err := cl.Mail(c.From); err != nil {
		return fmt.Errorf("mittente rifiutato dal server di posta: %w", err)
	}
	if err := cl.Rcpt(m.To); err != nil {
		return fmt.Errorf("destinatario rifiutato dal server di posta: %w", err)
	}
	wc, err := cl.Data()
	if err != nil {
		return fmt.Errorf("invio rifiutato dal server di posta: %w", err)
	}
	if _, err := wc.Write(raw); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("invio rifiutato dal server di posta: %w", err)
	}
	return cl.Quit()
}

// insecureAuth è l'autenticazione PLAIN senza controllo del TLS, per i soli
// test automatici contro un server locale.
type insecureAuth struct{ user, pass string }

func (a insecureAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}
func (a insecureAuth) Next([]byte, bool) ([]byte, error) { return nil, nil }

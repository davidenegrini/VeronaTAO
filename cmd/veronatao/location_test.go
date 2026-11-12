// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateLocation(t *testing.T) {
	base := t.TempDir()
	dir, err := validateLocation(`  "` + filepath.Join(base, "nuova", "cartella") + `" `)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Error("la cartella deve essere creata")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("il file di prova non è stato rimosso")
	}
	for _, bad := range []string{"", "   ", "relativa/cartella", `\\server\condivisa\dati`, "//server/condivisa",
		filepath.Join(base, "OneDrive - Studio", "dati"), filepath.Join(base, "Dropbox", "x"), filepath.Join(base, "Google Drive")} {
		if _, err := validateLocation(bad); err == nil {
			t.Errorf("%q accettata", bad)
		}
	}
	// Nomi che contengono per caso una parola simile non vengono rifiutati.
	if _, err := validateLocation(filepath.Join(base, "archivio-omega")); err != nil {
		t.Error(err)
	}
	// Cartella non scrivibile: un file al posto della cartella.
	file := filepath.Join(base, "file")
	os.WriteFile(file, []byte("x"), 0o600)
	if _, err := validateLocation(filepath.Join(file, "sotto")); err == nil {
		t.Error("percorso non creabile accettato")
	}
}

func TestResolveDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ProgramData", home)
	def := defaultDataDir()

	if dir, _ := resolveDataDir("/altrove", true, true, true); dir != "/altrove" {
		t.Error("l'opzione -dati ha la precedenza:", dir)
	}
	// Senza archivio e senza possibilità di chiedere: cartella predefinita.
	if dir, _ := resolveDataDir("", false, false, true); dir != def {
		t.Error(dir)
	}
	// Un'installazione esistente continua a funzionare senza domande.
	os.MkdirAll(def, 0o700)
	os.WriteFile(filepath.Join(def, dbFile), []byte("x"), 0o600)
	if dir, _ := resolveDataDir("", false, true, true); dir != def {
		t.Error("archivio esistente nella cartella predefinita non riconosciuto:", dir)
	}
	// La posizione scelta in precedenza vince sulla cartella predefinita.
	chosen := filepath.Join(home, "scelta")
	if err := writeLocation(def, chosen); err != nil {
		t.Fatal(err)
	}
	if dir, _ := resolveDataDir("", false, true, true); dir != chosen {
		t.Error("posizione scelta non riletta:", dir)
	}
}

func TestLocationPage(t *testing.T) {
	def := filepath.Join(t.TempDir(), "predefinita")
	chosen := make(chan string, 1)
	ts := httptest.NewServer(locationHandler(def, chosen))
	defer ts.Close()

	body := func(resp *http.Response, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b := make([]byte, 1<<16)
		n, _ := resp.Body.Read(b)
		return string(b[:n])
	}
	// La cartella consigliata è già pronta: basta un clic, senza scrivere nulla.
	page := body(http.Get(ts.URL + "/"))
	if !strings.Contains(page, "Usa la cartella consigliata") || !strings.Contains(page, `type="hidden" name="dir" value="`+def+`"`) || strings.Contains(page, "<details open") {
		t.Error("pagina di scelta: cartella consigliata non proposta")
	}
	page = body(http.PostForm(ts.URL+"/", url.Values{"dir": {`\\server\dati`}}))
	if !strings.Contains(page, "condivisione di rete") || !strings.Contains(page, "<details open") {
		t.Error("condivisione di rete non rifiutata, o sezione della scelta manuale non riaperta")
	}
	if readLocation(def) != "" {
		t.Error("una scelta rifiutata non deve essere registrata")
	}
	// Richiesta da un altro sito.
	req, _ := http.NewRequest("POST", ts.URL+"/", strings.NewReader("dir=/tmp/x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Error("richiesta da altra origine accettata")
	}
	// Scelta valida, con un archivio già presente.
	target := filepath.Join(t.TempDir(), "archivio")
	os.MkdirAll(target, 0o700)
	os.WriteFile(filepath.Join(target, dbFile), []byte("x"), 0o600)
	if page := body(http.PostForm(ts.URL+"/", url.Values{"dir": {target}})); !strings.Contains(page, "archivio esistente") {
		t.Error("archivio esistente non segnalato")
	}
	if got := <-chosen; got != target || readLocation(def) != target {
		t.Error("scelta non registrata:", got, readLocation(def))
	}
}

// Il clic su "Usa la cartella consigliata" basta da solo a completare la scelta.
func TestLocationRecommended(t *testing.T) {
	def := filepath.Join(t.TempDir(), "predefinita")
	chosen := make(chan string, 1)
	ts := httptest.NewServer(locationHandler(def, chosen))
	defer ts.Close()
	resp, err := http.PostForm(ts.URL+"/", url.Values{"dir": {def}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := <-chosen; got != def || readLocation(def) != def {
		t.Error("cartella consigliata non registrata:", got)
	}
	if st, err := os.Stat(def); err != nil || !st.IsDir() {
		t.Error("cartella consigliata non creata")
	}
}

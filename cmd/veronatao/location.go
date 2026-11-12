// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// La posizione dell'archivio non può stare nell'archivio stesso: è scritta in
// un piccolo file di testo nella cartella predefinita del programma.
const locationFile = "posizione-archivio.txt"

const dbFile = "veronatao.db"

func readLocation(defaultDir string) string {
	b, err := os.ReadFile(filepath.Join(defaultDir, locationFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff"))
}

func writeLocation(defaultDir, dir string) error {
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(defaultDir, locationFile), []byte(dir+"\r\n"), 0o600)
}

func hasArchive(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, dbFile))
	return err == nil
}

// cloudFolders sono i nomi delle cartelle sincronizzate con servizi in rete:
// lì l'archivio uscirebbe dal computer e potrebbe corrompersi durante la
// sincronizzazione.
var cloudFolders = []string{"onedrive", "dropbox", "google drive", "googledrive", "icloud", "pcloud", "nextcloud"}

// validateLocation verifica che la cartella scelta sia utilizzabile e la
// crea se manca. Restituisce il percorso ripulito.
func validateLocation(dir string) (string, error) {
	dir = strings.TrimSpace(strings.Trim(strings.TrimSpace(dir), `"`))
	if dir == "" {
		return "", errors.New("indicare una cartella")
	}
	if strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, "//") {
		return "", errors.New("la cartella non può essere una condivisione di rete: l'archivio deve stare su un disco di questo computer (le altre postazioni si collegano dal browser)")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("indicare il percorso completo della cartella, ad esempio C:\\VeronaTAO-dati")
	}
	dir = filepath.Clean(dir)
	// Si confronta l'inizio di ogni elemento del percorso ("OneDrive - Studio").
	for _, part := range strings.FieldsFunc(strings.ToLower(dir), func(r rune) bool { return r == '/' || r == '\\' }) {
		for _, name := range cloudFolders {
			if !strings.HasPrefix(part, name) {
				continue
			}
			return "", errors.New("la cartella sembra sincronizzata con un servizio in rete (OneDrive, Dropbox e simili): i dati dei pazienti uscirebbero dal computer e l'archivio potrebbe danneggiarsi. Scegliere una cartella locale non sincronizzata")
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("impossibile creare la cartella: %v", err)
	}
	probe := filepath.Join(dir, ".prova-scrittura")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return "", fmt.Errorf("la cartella non è scrivibile: %v", err)
	}
	_ = os.Remove(probe)
	return dir, nil
}

// resolveDataDir stabilisce dove si trova l'archivio. In ordine: l'opzione
// -dati, la posizione scelta in precedenza, un archivio già presente nella
// cartella predefinita; altrimenti, al primo avvio, lo si chiede all'utente.
func resolveDataDir(flagDir string, flagSet, ask, noBrowser bool) (string, error) {
	def := defaultDataDir()
	if flagSet {
		return flagDir, nil
	}
	if dir := readLocation(def); dir != "" {
		return dir, nil
	}
	if hasArchive(def) || !ask {
		return def, nil
	}
	return askLocation(def, noBrowser)
}

var locationPage = template.Must(template.New("p").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Primo avvio · VeronaTAO</title>
<style>
body{font-family:"Segoe UI",system-ui,Arial,sans-serif;background:#f3f5f8;color:#1a2330;margin:0;line-height:1.45}
header{background:#1d4f7a;color:#fff;padding:.6rem 1rem;font-weight:700;font-size:1.2rem}
main{max-width:640px;margin:2rem auto;background:#fff;border:1px solid #d5dbe3;border-radius:6px;padding:1.2rem}
input{font:inherit;width:100%;box-sizing:border-box;padding:.4rem;border:1px solid #aab4c0;border-radius:4px}
button,a.b{font:inherit;display:inline-block;margin-top:.8rem;padding:.4rem 1rem;border:1px solid #1d4f7a;border-radius:4px;background:#1d4f7a;color:#fff;text-decoration:none;cursor:pointer}
.err{background:#fde8e6;border:1px solid #a12622;color:#a12622;padding:.5rem .8rem;border-radius:5px}
.ok{background:#e4f4ea;border:1px solid #1f7a45;color:#1f7a45;padding:.5rem .8rem;border-radius:5px}
li{margin:.3rem 0}.m{color:#5d6b7c;font-size:.92rem}code{background:#eef1f5;padding:0 .25rem}
</style>{{if .Done}}<meta http-equiv="refresh" content="3;url=/">{{end}}</head><body>
<header>VeronaTAO</header><main>
{{if .Done}}
<h1>Posizione dell'archivio impostata</h1>
<p class="ok">{{if .Existing}}Trovato un archivio esistente: verrà usato quello.{{else}}Verrà creato un nuovo archivio.{{end}}<br>Cartella: <code>{{.Dir}}</code></p>
<p>Tra qualche secondo si apre la pagina successiva.</p><a class="b" href="/">Continua</a>
{{else}}
<h1>Dove conservare l'archivio?</h1>
<p>Scegli la cartella di questo computer in cui VeronaTAO terrà i dati dei pazienti, i backup automatici e il registro.</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/">
<label for="dir">Cartella dell'archivio</label>
<input id="dir" name="dir" value="{{.Dir}}" required autofocus spellcheck="false">
<button>Usa questa cartella</button>
</form>
<ul class="m">
<li>Se non hai esigenze particolari, lascia la cartella proposta.</li>
<li>Deve essere su un disco di questo computer, meglio se cifrato. Non usare cartelle di rete né cartelle sincronizzate (OneDrive, Dropbox e simili).</li>
<li>Se la cartella contiene già un archivio di VeronaTAO, verrà usato quello: nulla viene sovrascritto.</li>
<li>Se la cartella non esiste viene creata.</li>
</ul>
{{end}}
</main></body></html>`))

// locationHandler mostra la pagina di scelta e, a scelta valida, la registra
// e la comunica sul canale.
func locationHandler(def string, chosen chan<- string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"Dir": def}
		switch {
		case r.URL.Path != "/":
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		case r.Method == http.MethodPost:
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				http.Error(w, "Richiesta non valida", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
			data["Dir"] = r.FormValue("dir")
			dir, err := validateLocation(r.FormValue("dir"))
			if err == nil {
				err = writeLocation(def, dir)
			}
			if err != nil {
				data["Error"] = err.Error()
				break
			}
			data["Dir"], data["Done"], data["Existing"] = dir, true, hasArchive(dir)
			_ = locationPage.Execute(w, data)
			select {
			case chosen <- dir:
			default:
			}
			return
		}
		_ = locationPage.Execute(w, data)
	})
}

// askLocation chiede all'utente, nel browser, dove tenere l'archivio. Ascolta
// solo sul computer locale e termina appena la scelta è fatta.
func askLocation(def string, noBrowser bool) (string, error) {
	addr := net.JoinHostPort("127.0.0.1", "8756")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Un'altra copia del programma sta già mostrando la pagina.
		if !noBrowser {
			openBrowser("http://localhost:8756/")
		}
		return "", errAlreadyRunning
	}
	chosen := make(chan string, 1)
	srv := &http.Server{Handler: locationHandler(def, chosen), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("scelta della posizione: %v", err)
		}
	}()
	fmt.Println("VeronaTAO: scegli la cartella dell'archivio su http://localhost:8756/")
	if !noBrowser {
		openBrowser("http://localhost:8756/")
	}
	dir := <-chosen
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return dir, nil
}

var errAlreadyRunning = errors.New("già in esecuzione")

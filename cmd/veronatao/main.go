// SPDX-License-Identifier: AGPL-3.0-or-later

// VeronaTAO: registro clinico per la terapia anticoagulante orale.
//
// Il programma è un unico eseguibile: custodisce l'archivio in una cartella
// locale e mostra l'interfaccia nel browser. Non contatta alcun servizio
// esterno.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/davidenegrini/veronatao/internal/store"
	"github.com/davidenegrini/veronatao/internal/web"
)

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "VeronaTAO")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".veronatao")
	}
	return "veronatao-dati"
}

func main() {
	dataDir := flag.String("dati", defaultDataDir(), "cartella che contiene l'archivio")
	noBrowser := flag.Bool("senza-browser", false, "non aprire il browser all'avvio")
	resetUser := flag.String("reimposta-password", "", "nome utente di cui reimpostare la password (recupero dell'amministratore)")
	showVersion := flag.Bool("versione", false, "mostra la versione ed esce")
	flag.Parse()

	if *showVersion {
		fmt.Println("VeronaTAO", web.Version)
		return
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		fatal(*dataDir, err)
	}
	if logFile, err := os.OpenFile(filepath.Join(*dataDir, "veronatao.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(logFile)
	}
	dbPath := filepath.Join(*dataDir, "veronatao.db")
	restored, err := applyPendingRestore(*dataDir, dbPath)
	if err != nil {
		fatal(*dataDir, err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		fatal(*dataDir, err)
	}
	defer st.Close()
	if restored != "" {
		// Il ripristino resta annotato anche nel registro dell'archivio ripristinato.
		st.Audit(store.Actor{Username: "sistema"}, "restore", "", 0, 0, "archivio ripristinato da backup; archivio precedente conservato come "+restored)
	}

	if *resetUser != "" {
		resetPassword(st, *dataDir, *resetUser)
		return
	}

	lan := st.Setting("lan_enabled") == "1"
	port := st.SettingInt("port", 8756)
	host := "127.0.0.1"
	scheme := "http"
	if lan {
		host, scheme = "0.0.0.0", "https"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	localURL := fmt.Sprintf("%s://localhost:%d/", scheme, port)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Con ogni probabilità VeronaTAO è già in esecuzione: basta aprire il browser.
		log.Printf("porta %d occupata: %v", port, err)
		if !*noBrowser {
			openBrowser(localURL)
		}
		return
	}

	srv := web.New(st, *dataDir, lan)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.Shutdown = stop

	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	stopSched := make(chan struct{})
	go srv.RunScheduler(stopSched)

	go func() {
		var err error
		if lan {
			var cert, key string
			if cert, key, err = ensureCertificate(*dataDir); err == nil {
				err = httpSrv.ServeTLS(ln, cert, key)
			}
		} else {
			err = httpSrv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server arrestato: %v", err)
			stop()
		}
	}()
	log.Printf("VeronaTAO %s in ascolto su %s (dati in %s)", web.Version, addr, *dataDir)
	fmt.Println("VeronaTAO è in esecuzione:", localURL)
	if !*noBrowser {
		openBrowser(localURL)
	}

	<-ctx.Done()
	close(stopSched)
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
	log.Printf("VeronaTAO arrestato")
}

// applyPendingRestore sostituisce l'archivio con quello predisposto dalla
// funzione di ripristino, conservando una copia di quello precedente, di cui
// restituisce il nome.
func applyPendingRestore(dataDir, dbPath string) (string, error) {
	pending := filepath.Join(dataDir, web.RestorePending)
	if _, err := os.Stat(pending); err != nil {
		return "", nil
	}
	old := "prima-del-ripristino-" + time.Now().Format("20060102-150405") + ".db"
	if _, err := os.Stat(dbPath); err == nil {
		// Si apre e si chiude l'archivio corrente per consolidare il file WAL.
		if st, err := store.Open(dbPath); err == nil {
			_, _ = st.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			st.Close()
		}
		if err := os.Rename(dbPath, filepath.Join(dataDir, old)); err != nil {
			return "", fmt.Errorf("impossibile mettere da parte l'archivio corrente: %w", err)
		}
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	}
	if err := os.Rename(pending, dbPath); err != nil {
		return "", fmt.Errorf("impossibile attivare l'archivio ripristinato: %w", err)
	}
	log.Printf("archivio ripristinato da backup")
	return old, nil
}

// resetPassword è il recupero di emergenza: va eseguito sul computer che
// ospita l'archivio, quando nessun amministratore riesce più ad accedere.
func resetPassword(st *store.Store, dataDir, username string) {
	u, err := st.UserByUsername(username)
	if err != nil {
		fatal(dataDir, fmt.Errorf("utente %q non trovato", username))
	}
	temp := web.TempPassword()
	if err := st.SetPassword(store.Actor{Username: "riga di comando"}, u.ID, web.HashPassword(temp), true, "password_reset"); err != nil {
		fatal(dataDir, err)
	}
	out := filepath.Join(dataDir, "password-temporanea.txt")
	msg := fmt.Sprintf("Utente: %s\r\nPassword temporanea: %s\r\nVa cambiata al primo accesso. Eliminare questo file dopo l'uso.\r\n", u.Username, temp)
	_ = os.WriteFile(out, []byte(msg), 0o600)
	fmt.Print(msg)
	fmt.Println("La password temporanea è stata scritta anche in", out)
}

func fatal(dataDir string, err error) {
	log.Printf("errore irreversibile: %v", err)
	fmt.Fprintln(os.Stderr, "VeronaTAO non può avviarsi:", err)
	_ = os.WriteFile(filepath.Join(dataDir, "errore-di-avvio.txt"), []byte(time.Now().Format(time.RFC3339)+" "+err.Error()+"\r\n"), 0o600)
	os.Exit(1)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

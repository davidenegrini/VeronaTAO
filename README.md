# VeronaTAO

Registro clinico open source per la gestione della terapia anticoagulante orale (TAO) con
antagonisti della vitamina K, pensato per lo studio del medico di medicina generale e per
l'ambulatorio.

VeronaTAO archivia i controlli INR, conserva lo schema posologico **deciso dal medico**
giorno per giorno, calcola indicatori retrospettivi (TTR, tassi di evento) e stampa lo schema
da consegnare al paziente, con la dose scritta a parole e il pittogramma delle compresse.

> **VeronaTAO non è un dispositivo medico.** Non calcola, non propone e non suggerisce
> dosaggi; non segnala interazioni né rischi; non contiene contenuti clinici propri. Ogni dose
> è inserita dal medico. Non è marcato CE e **non è conforme ai requisiti FCSA** per i
> programmi di gestione della TAO: ne persegue soltanto la compatibilità dei dati e degli
> indicatori. Vedi [REGULATORY.md](REGULATORY.md).

## Caratteristiche

- **Un solo programma, nessuna dipendenza.** Un eseguibile che contiene tutto: archivio
  (SQLite), interfaccia (si usa dal browser), generatore di PDF. Non serve installare
  database, server web o altro.
- **I dati restano sul computer.** Nessun servizio cloud, nessuna risorsa caricata
  dall'esterno. Funziona su una macchina isolata dalla rete. L'unica funzione che usa
  Internet è l'invio dello schema per email, facoltativo e spento finché non lo si configura.
- **Uso singolo o in rete locale.** Di norma il programma risponde solo al computer su cui
  gira. Attivando l'accesso dalla rete locale, altre postazioni dello studio lo usano dal
  browser, in HTTPS, senza installare nulla.
- **Schermata del controllo** in tre fasce: dati del paziente e del percorso, storico dei
  controlli, nuovo controllo (INR, schema settimanale in quarti di compressa, primo giorno,
  prossimo controllo, commento).
- **Calendario giorno per giorno** per le modifiche fini e per l'eparina (farmaco, dose,
  unità, somministrazioni al giorno).
- **Copia dello schema precedente** e **ripartizione aritmetica** di una dose settimanale
  indicata dal medico.
- **Stampa per il paziente** in A4: griglia lunedì-domenica, dose a parole, «NIENTE» esplicito
  nei giorni senza compressa, pittogramma leggibile in bianco e nero, giorno del «CONTROLLO».
- **Invio per email, facoltativo**: lo schema parte come PDF cifrato (AES a 256 bit) con una
  password diversa per ogni paziente, tramite il server di posta dello studio (anche Gmail).
- **Verifiche di coerenza dell'inserimento** (dose giornaliera oltre il massimo configurato,
  variazione settimanale oltre la soglia configurata, piano oltre 12 settimane): chiedono una
  conferma, non suggeriscono azioni.
- **Indicatori**: TTR con metodo Rosendaal, percentuale di INR in range, eventi per 100
  anni-paziente, intervallo tra controlli.
- **Registro delle attività** di sola aggiunta: accessi, consultazioni, modifiche, stampe,
  estrazioni, backup.
- **Backup cifrati** automatici giornalieri, verifica del backup senza ripristino, ripristino
  guidato. Lo stesso file cifrato serve per portare l'archivio in un'altra sede.

## Installazione su Windows

1. Scarica `VeronaTAO-Setup.exe` dalla pagina dei rilasci ed eseguilo (oppure copia
   `VeronaTAO.exe` in una cartella a scelta: funziona anche senza installazione).
2. Avvia VeronaTAO dal collegamento: si apre il browser sulla pagina di primo avvio, dove
   crei l'utente amministratore.
3. In **Amministrazione → Impostazioni** imposta subito la **passphrase dei backup** e
   l'intestazione dello studio per le stampe.

L'archivio si trova in `C:\ProgramData\VeronaTAO` (file `veronatao.db`). Il programma resta
in esecuzione in background finché non lo arresti da *Amministrazione → Arresta VeronaTAO* o
spegni il computer; riaprire il collegamento riapre semplicemente il browser.

### Più postazioni nello stesso studio

1. Sul computer che ospita l'archivio: *Amministrazione → Impostazioni → Consenti l'accesso
   dagli altri computer della rete locale*, poi arresta e riavvia VeronaTAO.
2. Consenti il programma nel firewall di Windows per le reti private (l'installer lo propone).
3. Dalle altre postazioni apri nel browser `https://NOME-DEL-COMPUTER:8756`.

La connessione è cifrata con un certificato generato sul posto (autofirmato): al primo
collegamento il browser mostra un avviso, da accettare una volta per postazione. Il computer
che ospita l'archivio deve essere acceso. Non esporre mai VeronaTAO su Internet.

### Spostare l'archivio in un'altra sede

*Amministrazione → Backup e ripristino → Scarica una copia cifrata*, porta il file nell'altra
sede e lì usa *Ripristina da un file di backup*. **Deve restare in uso una sola copia alla
volta**: dati inseriti in due copie diverse non possono essere riuniti.

### Inviare lo schema per email

1. *Amministrazione → Impostazioni → Invio dello schema per email*: indica server SMTP,
   porta, utente, password, mittente, oggetto e testo del messaggio, poi attiva l'invio e
   provalo con *Invia messaggio di prova*.
   **Con Gmail**: server `smtp.gmail.com`, porta `587`, utente il proprio indirizzo; come
   password serve una «password per le app» creata nell'account Google (richiede la verifica
   in due passaggi), non la password dell'account.
2. Nell'anagrafica del paziente inserisci l'indirizzo email e, se il paziente lo chiede,
   spunta l'invio automatico a ogni stampa. In alternativa usa il pulsante *Invia per email*
   nella pagina del controllo.
3. La password per aprire il PDF è generata dal programma, una per paziente, e si legge nella
   *Cartella* del paziente: va consegnata **di persona** (ad esempio scritta sul primo schema
   cartaceo), mai per email.

La connessione al server di posta è sempre cifrata: se il server non lo consente l'invio viene
rifiutato. Oggetto e testo del messaggio non devono contenere dati del paziente. Ogni
tentativo di invio, riuscito o no, è annotato nella cartella e nel registro delle attività.
Prima di attivare la funzione leggi la sezione dedicata in [COMPLIANCE.md](COMPLIANCE.md).

### Password dimenticata

Un amministratore reimposta la password da *Amministrazione → Utenti*: viene generata una
password temporanea da comunicare di persona. Se nessun amministratore riesce più ad accedere,
sul computer che ospita l'archivio si esegue, da Prompt dei comandi:

```
"C:\Program Files\VeronaTAO\VeronaTAO.exe" -reimposta-password NOMEUTENTE
```

La password temporanea viene scritta in `C:\ProgramData\VeronaTAO\password-temporanea.txt`
(da eliminare dopo l'uso). L'operazione è annotata nel registro delle attività.

## Sicurezza e privacy in breve

Chi installa VeronaTAO è titolare del trattamento dei dati. Il programma offre account
personali, password con hash Argon2id, blocco dopo tentativi falliti, scadenza della sessione,
registro delle attività e backup cifrati. La protezione dei dati sul disco è affidata alla
**cifratura del disco** del computer (BitLocker o «Crittografia dispositivo» di Windows), che
va attivata. La checklist completa è in [COMPLIANCE.md](COMPLIANCE.md).

## Compilazione dai sorgenti

Serve soltanto [Go](https://go.dev) 1.26 o successivo; non occorre un compilatore C.

```
go test ./...
./packaging/build.sh        # produce dist/VeronaTAO.exe e dist/veronatao-linux-amd64
```

Opzioni da riga di comando: `-dati CARTELLA`, `-senza-browser`, `-reimposta-password UTENTE`,
`-versione`.

### Struttura

| Cartella | Contenuto |
|---|---|
| `cmd/veronatao` | avvio del programma, certificato locale, ripristino |
| `internal/domain` | date cliniche, dosi in quarti, calendario, TTR: calcoli puri, senza database |
| `internal/store` | archivio SQLite, schema, registro delle attività, statistiche |
| `internal/pdf` | schema stampato per il paziente |
| `internal/backup` | cifratura dei backup (AES-256-GCM, chiave derivata con Argon2id) |
| `internal/mail` | cifratura del PDF e invio tramite SMTP |
| `internal/web` | interfaccia: pagine, accessi, amministrazione |
| `packaging` | script di compilazione e installer Windows (Inno Setup) |

### Dipendenze

Tutte incorporate nell'eseguibile, nessuna caricata a runtime, tutte con licenze permissive
(MIT, BSD, Apache-2.0) compatibili con AGPL-3.0:

| Modulo | Uso | Licenza |
|---|---|---|
| `modernc.org/sqlite` (con `libc`, `mathutil`, `memory`) | database SQLite in puro Go | BSD-3-Clause |
| `github.com/go-pdf/fpdf` | generazione PDF | MIT |
| `golang.org/x/crypto`, `golang.org/x/sys` | Argon2id, chiamate di sistema | BSD-3-Clause |
| `github.com/pdfcpu/pdfcpu` | cifratura AES-256 del PDF inviato per email | Apache-2.0 |
| `golang.org/x/image`, `golang.org/x/text`, `github.com/hhrutter/tiff` | indirette (pdfcpu) | BSD-3-Clause |
| `github.com/xdg-go/stringprep` | indiretta (pdfcpu) | Apache-2.0 |
| `go.yaml.in/yaml/v3`, `github.com/clipperhouse/uax29`, `github.com/mattn/go-runewidth` | indirette (pdfcpu) | MIT |
| `github.com/google/uuid`, `github.com/remyoudompheng/bigfft` | indirette | BSD-3-Clause |
| `github.com/dustin/go-humanize`, `github.com/mattn/go-isatty`, `github.com/ncruces/go-strftime` | indirette | MIT |

## Limiti noti della versione 0.1

- Nessun import da file dei valori INR: l'inserimento è manuale.
- Un controllo salvato non si modifica: si annulla (solo l'ultimo, con motivazione) e si
  reinserisce.
- Stampe solo in italiano.
- Fusione di pazienti duplicati, gruppi di studio, estrazioni configurabili e statistiche per
  operatore non sono ancora presenti.
- Le comorbidità sono un campo di testo libero nell'anagrafica.

## Licenza

Codice: [AGPL-3.0-or-later](LICENSE). Documentazione: CC-BY-4.0. Il software è fornito senza
alcuna garanzia. Per contribuire leggi [CONTRIBUTING.md](CONTRIBUTING.md).

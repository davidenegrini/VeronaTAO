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

1. Scarica `VeronaTAO-Setup.exe` dalla pagina dei rilasci (*Releases*) ed eseguilo (oppure copia
   `VeronaTAO.exe` in una cartella a scelta: funziona anche senza installazione).
2. Avvia VeronaTAO dal collegamento: si apre il browser. Al primo avvio conferma la **cartella
   dell'archivio** con il pulsante *Usa la cartella consigliata* (oppure scegline un'altra),
   poi crei l'utente amministratore.
3. In **Amministrazione → Impostazioni** imposta subito la **passphrase dei backup** e
   l'intestazione dello studio per le stampe.

L'archivio (file `veronatao.db`) si trova nella cartella scelta al primo avvio, che per
impostazione predefinita è `C:\ProgramData\VeronaTAO`; la cartella in uso è indicata in
*Amministrazione → Backup e ripristino*. Il programma resta
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

### Scegliere o cambiare la cartella dell'archivio

Al primo avvio VeronaTAO propone una cartella consigliata, che va bene nella quasi totalità
dei casi e si conferma con un clic; chi ha esigenze particolari può indicarne un'altra. La
cartella deve stare su un disco
del computer, meglio se cifrato: non sono accettate condivisioni di rete né cartelle
sincronizzate (OneDrive, Dropbox e simili), dove i dati uscirebbero dal computer e l'archivio
potrebbe danneggiarsi. Se la cartella indicata contiene già un archivio, viene usato quello.

La scelta è annotata nel file `C:\ProgramData\VeronaTAO\posizione-archivio.txt`. Per
cambiare cartella in seguito: arresta VeronaTAO, sposta l'intera cartella dell'archivio nella
nuova posizione, elimina quel file e riavvia: il programma chiede di nuovo la cartella e
ritrova l'archivio spostato.

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

### Aggiornare a una nuova versione

Esegui il nuovo `VeronaTAO-Setup.exe` sopra l'installazione esistente: l'installer arresta il
programma se è in esecuzione, sostituisce l'eseguibile e non tocca l'archivio, che viene
adeguato da solo al primo avvio. Prima di aggiornare esegui un backup da *Amministrazione*.
Per verificare l'integrità del file scaricato confronta la sua impronta SHA-256 con quella
pubblicata in `SHA256SUMS.txt`:

```powershell
Get-FileHash VeronaTAO-Setup.exe -Algorithm SHA256
```

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

Servono soltanto [Go](https://go.dev/dl/) 1.26 o successivo e Git; non occorre un compilatore
C. L'eseguibile e l'installer già compilati si trovano anche tra gli artefatti del workflow
«CI» su GitHub (*Actions* → ultimo run su `main` → *Artifacts*).

### Su Windows

Da PowerShell:

```powershell
git clone https://github.com/davidenegrini/VeronaTAO.git
cd VeronaTAO
go test ./...
go build -trimpath -ldflags "-s -w -H windowsgui -X github.com/davidenegrini/veronatao/internal/web.Version=0.1.0" -o dist\VeronaTAO.exe .\cmd\veronatao
```

Il risultato è `dist\VeronaTAO.exe`: con un doppio clic si apre il browser sulla pagina di
primo avvio. L'opzione `-H windowsgui` evita la finestra nera del terminale; per vedere i
messaggi durante una prova basta toglierla.

Per l'installer serve [Inno Setup 6](https://jrsoftware.org/isdl.php); dalla stessa cartella:

```powershell
& "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" /DAppVersion=0.1.0 packaging\veronatao.iss
```

Produce `dist\VeronaTAO-Setup.exe`.

L'eseguibile non è firmato digitalmente: Windows SmartScreen può segnalarlo come «app non
riconosciuta» (*Ulteriori informazioni → Esegui comunque*). Per una prova che non tocchi
l'archivio vero: `dist\VeronaTAO.exe -dati C:\prova-veronatao`.

### Su Linux o macOS

```
go test ./...
./packaging/build.sh        # produce dist/VeronaTAO.exe e dist/veronatao-linux-amd64
```

Lo script compila anche l'eseguibile per Windows.

### Opzioni da riga di comando

`-dati CARTELLA` (usa quella cartella, ignorando la posizione scelta al primo avvio),
`-senza-browser`, `-reimposta-password UTENTE`, `-versione`.

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

## Pubblicare una versione

1. Aggiorna `CHANGELOG.md` con una sezione `## [X.Y.Z] - data`, e `softwareVersion` e
   `releaseDate` in `publiccode.yml`.
2. Dopo il merge su `main`, crea e invia il tag `vX.Y.Z`.
3. Il workflow «CI» esegue i test, compila l'installer, lo prova su Windows (installazione,
   primo avvio, aggiornamento con il programma in esecuzione, disinstallazione) e pubblica la
   release con `VeronaTAO-Setup.exe`, `VeronaTAO.exe` e le impronte `SHA256SUMS.txt`. Le
   versioni 0.x sono marcate come preliminari.

Per segnalare un problema di sicurezza vedi [SECURITY.md](SECURITY.md); le modifiche di ogni
versione sono in [CHANGELOG.md](CHANGELOG.md).

## Licenza

Codice: [AGPL-3.0-or-later](LICENSE). Documentazione: CC-BY-4.0. Il software è fornito senza
alcuna garanzia. Per contribuire leggi [CONTRIBUTING.md](CONTRIBUTING.md).

# Protezione dei dati: guida per chi installa VeronaTAO

Chi installa e usa VeronaTAO è **titolare del trattamento** dei dati dei propri pazienti. Gli
autori del software non trattano alcun dato: non esiste un servizio gestito, il programma non
comunica con l'esterno. Questa guida non sostituisce la valutazione del titolare né il parere
del suo responsabile della protezione dei dati.

## Che cosa fa il software (art. 25 e 32 GDPR)

| Misura | Come è realizzata |
|---|---|
| Dati solo in locale | archivio in un file sul computer; nessuna connessione verso Internet, nessuna risorsa esterna, nessuna telemetria |
| Accesso | account personali (gli account condivisi non sono previsti), password di almeno 10 caratteri con hash Argon2id, blocco di 15 minuti dopo 5 tentativi falliti, scadenza della sessione per inattività (20 minuti, configurabile) |
| Tracciabilità | registro di sola aggiunta: accessi riusciti e falliti, consultazione delle schede, inserimenti, annullamenti, stampe, estrazioni, backup, ripristini, reimpostazioni di password |
| Integrità | nessuna cancellazione fisica dei dati clinici: le voci si annullano con motivazione; i documenti stampati sono conservati con impronta SHA-256 |
| Disponibilità | backup automatico giornaliero cifrato, verifica del backup, ripristino guidato |
| Rete locale | se attivata, connessione HTTPS; di norma il programma risponde solo al computer su cui gira |
| Diritti degli interessati | esportazione completa dei dati di un paziente (scheda → Cartella → Esporta), estrazione pseudonimizzata |

## Minimizzazione: che cosa non viene raccolto, e perché

- **Origine etnica.** Nei programmi con algoritmo posologico è una variabile del calcolo. Qui
  non esiste alcun calcolo, quindi il dato non avrebbe uso; è inoltre una categoria particolare
  (art. 9).
- **Allegati e documenti esterni.** Non si possono caricare file: lettere e referti si
  riassumono nel testo dell'evento.
- **Esami di laboratorio diversi dall'INR.** Non previsti in questa versione.
- **Elenco precaricato di farmaci.** I farmaci concomitanti sono testo libero.

## Cifratura dei dati sul disco

L'archivio non è cifrato dal programma: la protezione è affidata alla **cifratura dell'intero
disco** (BitLocker su Windows Pro, «Crittografia dispositivo» su Windows Home dove
disponibile). La cifratura dei singoli campi è stata scartata perché impedirebbe la ricerca per
cognome, e perché il rischio realistico è il furto del computer o di una copia, non l'accesso
al database da parte di chi possiede già le credenziali. I backup, che sono le copie destinate
a lasciare il computer, sono invece sempre cifrati dal programma.

Se il disco non può essere cifrato, il rischio va valutato e documentato dal titolare.

## Checklist di installazione

- [ ] Cifratura del disco attiva sul computer che ospita l'archivio
- [ ] Account Windows protetto da password; blocco schermo automatico
- [ ] Un account VeronaTAO per ogni persona; ruolo amministratore solo a chi serve
- [ ] Passphrase dei backup impostata e conservata anche fuori dal computer (senza passphrase
      un backup non è recuperabile)
- [ ] Cartella dei backup su un supporto diverso dal disco principale (disco esterno o di rete)
- [ ] Copia fuori sede periodica (vedi sotto)
- [ ] Prima prova di ripristino eseguita e registrata
- [ ] Se si usa la rete locale: rete dello studio protetta, firewall limitato alle reti
      private, VeronaTAO mai raggiungibile da Internet
- [ ] Registro dei trattamenti del titolare aggiornato

## Backup e prova di ripristino

La perdita dell'archivio è il rischio più concreto. Un backup mai provato non è un backup.

- Il backup automatico parte ogni giorno all'avvio o durante l'uso, se la passphrase è
  impostata. La pagina iniziale avvisa se l'ultimo backup è troppo vecchio o se l'ultima
  verifica risale a più di sei mesi.
- **Copia fuori sede.** Almeno una copia cifrata va tenuta in un luogo diverso dallo studio.
  Se la copia è affidata a un fornitore (anche un servizio di archiviazione in rete), questi
  diventa responsabile del trattamento e serve un accordo ai sensi dell'art. 28.
- **Prova semestrale.** Ogni sei mesi: *Amministrazione → Verifica l'ultimo backup
  automatico* e, almeno una volta l'anno, un ripristino completo su un computer di prova.

Modulo da compilare e conservare:

| Data | Eseguita da | File verificato | Esito (pazienti, controlli) | Ripristino completo? | Note |
|---|---|---|---|---|---|
| | | | | | |

## Conservazione e cancellazione

Il programma non cancella dati clinici. Le norme sulla conservazione della documentazione
sanitaria possono prevalere sul diritto alla cancellazione: una richiesta di cancellazione va
valutata dal titolare caso per caso. La dismissione di un'installazione richiede la
cancellazione sicura della cartella dei dati e delle copie di backup.

## Estrazioni e comunicazione a terzi

Il programma produce il file e ne annota la produzione nel registro, nulla di più. Comunicare
dati a terzi, anche pseudonimizzati, richiede una base giuridica propria, di cui risponde il
titolare. L'estrazione pseudonimizzata sostituisce il paziente con un codice progressivo e
omette nome, data di nascita (resta l'anno), codice fiscale e codici esterni; non è un dato
anonimo.

## Violazioni dei dati

Il furto o lo smarrimento del computer o di una copia non cifrata è una violazione da valutare
ai sensi degli artt. 33 e 34. Il registro delle attività aiuta a ricostruire che cosa è stato
consultato e da chi.

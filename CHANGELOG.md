# Registro delle modifiche

Le versioni seguono la numerazione semantica. Finché la versione inizia con 0 il programma è
da considerare in fase pilota.

## [0.1.0] - 2026-11-12

Prima versione pilota. **Da usare in affiancamento al sistema già in uso**, ricontrollando a
mano lo schema stampato: il programma non ha ancora una storia d'uso reale.

VeronaTAO non è un dispositivo medico: non calcola né suggerisce dosaggi, non è marcato CE e
non è conforme ai requisiti FCSA (vedi `REGULATORY.md`).

### Funzioni

- Programma unico per Windows, con archivio locale e interfaccia nel browser; nessun servizio
  esterno. Uso da un solo computer oppure, a scelta, da più postazioni della rete locale in
  HTTPS.
- Al primo avvio: scelta della cartella dell'archivio, con una cartella consigliata
  confermabile con un clic.
- Anagrafica dei pazienti, percorsi di trattamento, range INR con storico delle modifiche.
- Schermata del controllo: INR, schema settimanale in quarti di compressa, primo giorno,
  prossimo controllo, commento; copia dello schema precedente; ripartizione aritmetica di una
  dose settimanale indicata dal medico.
- Calendario giorno per giorno con conferma obbligatoria, modifica dei singoli giorni ed
  eparina.
- Stampa dello schema per il paziente in PDF: dose a parole, pittogramma, giorno del
  controllo, ultimi tre controlli.
- Invio facoltativo dello schema per email, come PDF cifrato con una password per paziente.
- Farmaci concomitanti, eventi clinici, stato del paziente.
- Indicatori: TTR (Rosendaal), INR in range, eventi per 100 anni-paziente.
- Account personali, registro delle attività di sola aggiunta, estrazioni CSV nominative e
  pseudonimizzate, esportazione dei dati di un paziente.
- Backup automatici cifrati, verifica e ripristino; copia cifrata per il trasporto
  dell'archivio in un'altra sede.

### Limiti noti

- Nessuna importazione di pazienti o di valori INR da altri programmi: l'inserimento è
  manuale, in ordine cronologico.
- Un controllo salvato non si modifica: si annulla l'ultimo, con motivazione, e si reinserisce.
- Stampe solo in italiano.
- L'eseguibile non è firmato digitalmente: Windows può mostrare l'avviso «app non
  riconosciuta».
- Un disco di rete mappato con una lettera non viene riconosciuto come tale nella scelta
  della cartella dell'archivio: non va usato.
- L'invio per email non è stato provato con tutti i fornitori di posta: usare il messaggio di
  prova prima di attivarlo.

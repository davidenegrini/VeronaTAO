# Contribuire a VeronaTAO

Grazie per l'interesse. Prima di proporre una modifica leggi `REGULATORY.md`: il perimetro del
progetto è stretto per scelta.

## Contributi che non possono essere accettati

Non vengono accolte modifiche che introducano:

- calcolo, proposta o suggerimento di dosaggi, in qualsiasi forma;
- punteggi o stratificazioni di rischio;
- elenchi di interazioni farmacologiche o alimentari, controindicazioni, soglie cliniche;
- testi informativi o consigli per il paziente forniti dal software;
- avvisi che raccomandino un'azione clinica;
- affermazioni di conformità ai requisiti FCSA o di marcatura CE;
- connessioni a servizi esterni, telemetria, risorse caricate da Internet (unica eccezione
  ammessa: l'invio per email tramite il server SMTP configurato dallo studio).

Una modifica di questo tipo trasformerebbe il software in un dispositivo medico o ne
tradirebbe l'impostazione di tutela dei dati.

## Regole

- **Developer Certificate of Origin.** Ogni commit porta la riga `Signed-off-by:` (usa
  `git commit -s`), con cui dichiari di avere il diritto di contribuire quel codice
  (<https://developercertificate.org>).
- **Nessun accordo di cessione del copyright.** Ogni autore resta titolare dei propri
  contributi, sotto AGPL-3.0-or-later. La titolarità distribuita è ciò che garantisce che il
  progetto resti aperto.
- Aggiungi il tuo nome al file `AUTHORS`.
- Ogni file sorgente porta l'intestazione `SPDX-License-Identifier`.
- `go vet ./...` e `go test ./...` devono passare. Le modifiche a date, dosi, calendario e
  stampa richiedono test dedicati.
- Le date cliniche usano solo il tipo `domain.Date`; le dosi di anticoagulante orale solo
  `domain.Quarters`. Niente `time.Time` e niente numeri in virgola mobile per questi valori.
- Le pagine non contengono stili o script in linea (lo vieta la Content-Security-Policy).
- Niente dati reali di pazienti in issue, test, schermate o esempi.

## Segnalare un problema di sicurezza

Non aprire una issue pubblica: contatta privatamente i manutentori tramite la funzione di
segnalazione riservata del repository.

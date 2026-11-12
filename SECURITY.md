# Sicurezza

VeronaTAO custodisce dati sanitari: le segnalazioni di vulnerabilità sono prese sul serio.

## Come segnalare

**Non aprire una issue pubblica.** Usa la segnalazione riservata di GitHub: nella pagina del
repository, scheda *Security* → *Report a vulnerability*. La segnalazione resta visibile solo
ai manutentori.

Indica, per quanto possibile: versione di VeronaTAO, sistema operativo, modalità d'uso (solo
locale o rete dello studio), passi per riprodurre il problema e conseguenze osservate. **Non
allegare mai dati reali di pazienti**: né schermate, né archivi, né file di backup.

## Che cosa aspettarsi

Il progetto è mantenuto da volontari, senza tempi di risposta garantiti. Si cercherà di
confermare la ricezione, valutare la segnalazione e, se fondata, pubblicare una correzione e
un avviso che indichi le versioni coinvolte. Chi ha segnalato viene citato nei
ringraziamenti, se lo desidera.

## Versioni seguite

Solo l'ultima versione pubblicata riceve correzioni.

## Che cosa interessa in particolare

- accesso ai dati senza autenticazione o con un account di altri;
- modifica o cancellazione del registro delle attività o dei documenti stampati;
- lettura di un backup o di un PDF protetto senza la relativa passphrase o password;
- dati che lasciano il computer al di fuori dell'invio per email configurato dallo studio;
- errori per cui lo schema stampato o inviato non corrisponde a quanto inserito dal medico.

L'ultimo punto riguarda la sicurezza dei pazienti più che quella informatica, ma va segnalato
con la stessa riservatezza e urgenza.

## Fuori dal perimetro

VeronaTAO non cifra da sé l'archivio sul disco e non va esposto su Internet: sono scelte
documentate in `COMPLIANCE.md`, non vulnerabilità. Lo stesso vale per l'avviso del browser
sul certificato autofirmato usato nella rete locale.

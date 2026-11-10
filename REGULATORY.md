# Posizione regolatoria di VeronaTAO

Questo documento descrive le scelte di progetto e le loro ragioni. **Non è un parere legale.**
Chi distribuisce o mette in servizio VeronaTAO in un contesto organizzato dovrebbe far
verificare questa posizione da un esperto di regolamentazione dei dispositivi medici.

## Che cos'è

VeronaTAO è un **registro clinico**: archivia i dati inseriti dal medico, li organizza, calcola
indicatori retrospettivi e stampa lo schema posologico che il medico ha scritto. Non è un
sistema di supporto alla decisione clinica.

## Le tre regole

**R1 — Nessun dosaggio calcolato.** Il software non suggerisce, calcola, propone o raccomanda
dosaggi di anticoagulante. Non contiene algoritmi di aggiustamento, previsioni della dose di
mantenimento, protocolli di bridging. Ogni dose è inserita dal medico.

**R2 — Nessun alert clinico automatico.** Il software non segnala interazioni, controindicazioni
o rischi e non calcola punteggi di rischio.

**R3 — Nessun contenuto clinico proprio.** Il software non incorpora elenchi di farmaci
interferenti, alimenti, controindicazioni, soglie di rischio o testi informativi per il
paziente. Registra ciò che il medico dichiara e stampa ciò che il medico scrive. Anche il
testo a piè di pagina dello schema stampato è scritto dallo studio, nelle impostazioni.

## Perché

Un software destinato a fornire informazioni usate per prendere decisioni a fini diagnostici o
terapeutici ricade nella Regola 11 dell'Allegato VIII del Regolamento (UE) 2017/745 (MDR) ed è
un dispositivo medico almeno di classe IIa, con gli obblighi conseguenti (marcatura CE con
organismo notificato, sistema di gestione della qualità, ciclo di vita del software, gestione
del rischio, valutazione clinica). Un software che si limita ad archiviare, comunicare e
ricercare dati non è, di norma, qualificato come dispositivo medico (linea guida MDCG 2019-11).

## Che cosa il software fa con i numeri

Sono ammesse soltanto operazioni aritmetiche su valori indicati dal medico:

- ripartire sui sette giorni, in parti il più possibile uguali, una dose settimanale **totale
  scritta dal medico**; il risultato resta modificabile e va confermato;
- convertire quarti di compressa in milligrammi e in parole;
- copiare lo schema dell'ultima prescrizione, cioè dati già inseriti dal medico;
- calcolare indicatori retrospettivi (TTR, percentuali, tassi di evento);
- confrontare un valore con una soglia impostata dal medico o dall'amministratore e indicarne
  la posizione (INR sopra o sotto il range impostato; dose oltre il massimo configurato;
  variazione settimanale oltre la percentuale configurata).

Non esiste nel codice alcuna funzione che riceva un INR e restituisca una dose. Le verifiche di
coerenza riportano un confronto numerico e chiedono conferma: non dicono che cosa fare.

Il range INR associato a ciascuna indicazione e i dati iniziali (farmaci, indicazioni, tipi di
evento) sono valori di comodo modificabili, che precompilano un campo: il medico li verifica e
li conferma.

**Zona di attenzione.** L'indicazione «INR fuori dal range impostato» e la ripartizione
aritmetica sono i punti più vicini al confine. La qualifica di un software dipende anche dalla
destinazione d'uso dichiarata da chi lo distribuisce: descrivere VeronaTAO come strumento che
«aiuta a decidere la dose» o «segnala i pazienti a rischio» ne cambierebbe la natura,
indipendentemente dal codice.

## Chi modifica il software

Chi aggiunge a VeronaTAO un calcolo posologico, un punteggio di rischio, un controllo di
interazioni o contenuti clinici, e lo mette a disposizione di altri, crea un prodotto diverso e
ne diventa **fabbricante** ai sensi del MDR, con tutti gli obblighi che ne derivano. Il progetto
non accetta contributi di questo tipo (vedi `CONTRIBUTING.md`).

## Requisiti FCSA

Il documento FCSA sui programmi informatici per la gestione dei pazienti in trattamento
anticoagulante richiede, tra l'altro, la dichiarazione CE di conformità come dispositivo
medico, e per gli AVK la prescrizione automatica e la previsione della dose di mantenimento. Un
software pienamente conforme a quei requisiti è quindi un dispositivo medico.

**VeronaTAO non è conforme ai requisiti FCSA e non deve essere presentato come tale.** Ne
persegue la compatibilità dei dati e degli indicatori (TTR, eventi per anni-paziente,
estrazione dei dati), così che le informazioni raccolte siano confrontabili con quelle di un
centro. Ogni comunicazione sul progetto deve mantenere questa distinzione.

## Adozione da parte di un ente sanitario

Un'azienda sanitaria che adotti VeronaTAO così com'è usa un registro, non un dispositivo
medico. Se l'ente lo modificasse aggiungendo funzioni che ne fanno un dispositivo medico, e lo
usasse solo al proprio interno, entrerebbe in gioco l'art. 5, paragrafo 5, del MDR sui
dispositivi fabbricati e utilizzati all'interno delle istituzioni sanitarie: un regime
agevolato ma condizionato (tra l'altro: sistema di qualità adeguato, giustificazione
dell'assenza di dispositivi equivalenti sul mercato, documentazione, dichiarazione pubblica,
rispetto dei requisiti generali di sicurezza e prestazione). Non è un'esenzione automatica e va
valutata dall'ente con le proprie strutture competenti.

## Rischi residui presidiati

Anche senza calcolo della dose, un errore del software può tradursi in uno schema sbagliato in
mano al paziente. Le scelte seguenti servono a ridurlo e sono coperte da test automatici:

- date cliniche senza ora e senza fuso, mai influenzate dall'ora legale;
- piano memorizzato giorno per giorno, senza ricorrenze ricalcolate in stampa;
- dose zero sempre scritta «NIENTE», mai cella vuota dentro il periodo;
- eparina stampata nella stessa cella dell'anticoagulante orale;
- dosi in quarti di compressa interi, milligrammi sempre derivati;
- ogni documento stampato conservato con impronta SHA-256.

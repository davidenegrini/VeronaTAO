# SPDX-License-Identifier: AGPL-3.0-or-later
# Prova dell'installer su Windows: installazione, primo avvio con scelta della
# cartella consigliata, aggiornamento con il programma in esecuzione,
# disinstallazione che conserva i dati. Usata dal workflow CI; richiede
# privilegi di amministratore e modifica il computer su cui gira.
$ErrorActionPreference = 'Stop'
$setup = Join-Path $PSScriptRoot '..\dist\VeronaTAO-Setup.exe'
$exe = Join-Path $env:ProgramFiles 'VeronaTAO\VeronaTAO.exe'
$dati = Join-Path $env:ProgramData 'VeronaTAO'
$url = 'http://localhost:8756'

function Installa {
    $p = Start-Process $setup -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw "installer terminato con codice $($p.ExitCode)" }
    if (!(Test-Path $exe)) { throw 'eseguibile non installato' }
}

function Attendi([string]$percorso, [string]$testo) {
    for ($i = 0; $i -lt 40; $i++) {
        try {
            $r = Invoke-WebRequest "$url$percorso" -UseBasicParsing
            if ($r.Content.Contains($testo)) { return }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    throw "la pagina $percorso non mostra «$testo»"
}

Write-Host '1. Installazione'
Installa

Write-Host '2. Primo avvio: viene proposta la cartella consigliata'
Start-Process $exe -ArgumentList '-senza-browser'
Attendi '/' 'Usa la cartella consigliata'
Attendi '/' $dati

Write-Host '3. Scelta della cartella consigliata e pagina di creazione dell''amministratore'
Invoke-WebRequest "$url/" -Method Post -Body @{ dir = $dati } -UseBasicParsing | Out-Null
Attendi '/setup' 'Primo avvio'
if (!(Test-Path (Join-Path $dati 'veronatao.db'))) { throw 'archivio non creato nella cartella consigliata' }

Write-Host '4. Aggiornamento con il programma in esecuzione'
if (!(Get-Process VeronaTAO -ErrorAction SilentlyContinue)) { throw 'il programma dovrebbe essere in esecuzione' }
Installa
if (Get-Process VeronaTAO -ErrorAction SilentlyContinue) { throw 'il programma non è stato arrestato dall''installer' }

Write-Host '5. Dopo l''aggiornamento il programma riparte e ritrova l''archivio senza domande'
Start-Process $exe -ArgumentList '-senza-browser'
Attendi '/setup' 'Primo avvio'

Write-Host '6. Disinstallazione: il programma sparisce, i dati restano'
Start-Process (Join-Path $env:ProgramFiles 'VeronaTAO\unins000.exe') -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait
for ($i = 0; $i -lt 60 -and (Test-Path $exe); $i++) { Start-Sleep -Milliseconds 500 }
if (Test-Path $exe) { throw 'eseguibile ancora presente dopo la disinstallazione' }
if (Get-Process VeronaTAO -ErrorAction SilentlyContinue) { throw 'programma ancora in esecuzione dopo la disinstallazione' }
if (!(Test-Path (Join-Path $dati 'veronatao.db'))) { throw 'la disinstallazione ha rimosso l''archivio' }

Write-Host 'Prova dell''installer superata.'

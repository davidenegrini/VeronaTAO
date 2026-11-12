#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Compila VeronaTAO per Windows e Linux nella cartella dist/.
# Non serve alcun compilatore C: l'eseguibile è autosufficiente.
set -eu
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
LDFLAGS="-s -w -X github.com/davidenegrini/veronatao/internal/web.Version=${VERSION}"
mkdir -p dist
# -H windowsgui: nessuna finestra nera all'avvio; il programma si usa dal browser.
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "${LDFLAGS} -H windowsgui" -o dist/VeronaTAO.exe ./cmd/veronatao
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "${LDFLAGS}" -o dist/veronatao-linux-amd64 ./cmd/veronatao
ls -l dist

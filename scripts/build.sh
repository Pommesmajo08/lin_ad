#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/linad-server ./cmd/server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/linad-client-linux-amd64 ./cmd/client
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/linad-client-windows-amd64.exe ./cmd/client

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/linad-client-gui-linux-amd64 ./cmd/client-gui
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w -H windowsgui' -o dist/linad-client-gui-windows-amd64.exe ./cmd/client-gui

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/linad-linux-amd64 ./cmd/linux-client
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/linad-linux-arm64 ./cmd/linux-client
for linad_arch in amd64 arm64; do
  tar -czf "dist/linad-linux-provisioner-$linad_arch.tar.gz" "dist/linad-linux-$linad_arch" scripts/install-linux.sh scripts/uninstall-linux.sh packaging/linux docs/linux-provisioning.md docs/jobs.md docs/installation.md
done

# Desktop packages include both GUI and CLI, with per-user installers.
tar -czf dist/linad-client-linux-amd64.tar.gz dist/linad-client-linux-amd64 dist/linad-client-gui-linux-amd64 scripts/install-client-linux.sh packaging/client docs/installation.md
python3 - <<'PYZIP'
from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED
with ZipFile('dist/linad-client-windows-amd64.zip', 'w', ZIP_DEFLATED) as archive:
    for name in ['dist/linad-client-windows-amd64.exe', 'dist/linad-client-gui-windows-amd64.exe', 'scripts/install-client.ps1', 'install-windows.cmd', 'docs/installation.md']:
        archive.write(name)
PYZIP

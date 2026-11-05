# GitHub Releases bauen

Die Workflow-Datei `.github/workflows/release.yml` startet beim Push eines Versionstags `vMAJOR.MINOR.PATCH` oder manuell mit einem bereits vorhandenen Tag. Alle Pakete werden aus genau diesem Tag gebaut. Die Windows-Stufe baut die Credential-Provider-DLL mit dem Windows SDK und prüft Broker/SAM/Profile; erst danach verpackt die Linux-Stufe diese geprüfte DLL. Linux/Go-Tests, Installer-Downloadtests, Debian-PAM-Integration, Server-Containerprüfung und der native Windows-Installer-Test müssen erfolgreich sein, bevor veröffentlicht wird.

```sh
git tag v0.1.0
git push origin v0.1.0
```

In GitHub unter **Actions → Release installers** den Lauf beobachten. Die Veröffentlichungsphase erzeugt zuerst einen Entwurf mit allen Assets und veröffentlicht ihn erst nach vollständigem Upload. Bereits vorhandene Releases werden nicht überschrieben. Fehlgeschlagene Entwürfe vor einem erneuten Lauf prüfen und gegebenenfalls löschen. Voraussetzung sind aktivierte GitHub Actions und die erlaubte `contents: write`-Berechtigung des Release-Jobs. Es wird kein persönlicher Token im Repository benötigt.

Pre-Release-Tags wie `v0.1.0-beta.1` werden als Vorabversion veröffentlicht und nicht über `latest` ausgewählt. Ein festes `--version` kann sie explizit installieren. Öffentliche Release-Assets funktionieren anonym; für private Repositories unterstützt der Starter `GH_TOKEN` mit Leserechten über die GitHub-API.

Assets:

- `install.sh`, `install.ps1`, `install-windows.cmd`: kleine Download-Installer
- `linad-server-docker.tar.gz`: Server-Image plus Installer
- `linad-linux-provisioner-amd64.tar.gz`, `linad-linux-provisioner-arm64.tar.gz`
- `linad-client-linux-amd64.tar.gz`, `linad-client-windows-amd64.zip`
- `linad-windows-agent-amd64.zip`, `install-agent-windows.cmd`: Windows-Geräteagent als SYSTEM-Dienst
- `installation.md`, `SHA256SUMS`

Lokale Vorbereitung ohne Veröffentlichung:

```sh
sh scripts/build-windows-logon.sh
sh scripts/build.sh
python3 scripts/test-release-bootstrap.py
python3 scripts/test-container.py --export-image
python3 scripts/prepare-release.py
(cd release && sha256sum -c SHA256SUMS)
```

`release/` ist ignoriert und enthält nur die zur Veröffentlichung vorgesehenen Dateien. Die Prüfsummen verwenden reine Dateinamen ohne `dist/`-Präfix, damit die Starter dieselbe Manifestdatei verwenden können. Prüfsummen ersetzen keine unabhängige Code-Signatur.

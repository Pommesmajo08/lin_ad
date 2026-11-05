# Linad installieren

## Direkt aus GitHub Releases installieren

Ein veröffentlichtes Release enthält die kleinen Starter `install.sh`, `install.ps1` und `install-windows.cmd` sowie sämtliche Installationspakete und `SHA256SUMS`. Kein Repository-Checkout, Go oder manuelles Entpacken ist auf dem Ziel nötig.

**Linux:** Einmal den Starter herunterladen (`curl` wird zum Erstdownload benötigt):

```sh
curl --proto '=https' --proto-redir '=https' -fL https://github.com/Pommesmajo08/lin_ad/releases/latest/download/install.sh -o linad-install.sh
```

Anschließend den gewünschten Weg wählen:

```sh
sh linad-install.sh client           # GUI/CLI, ohne sudo
sudo sh linad-install.sh device      # Ubuntu/Debian-Systemanbindung
sudo sh linad-install.sh server      # Docker-Server
```

Ohne Komponente öffnet sich eine Auswahl. Architektur und Paket werden automatisch gewählt. Der Starter löst `latest` einmal auf einen festen Tag auf, lädt Paket und Prüfsummen **derselben Version**, prüft SHA-256, entpackt temporär und führt den Paket-Installer aus. Anschließend werden temporäre Dateien entfernt. Die SHA-Prüfung erkennt beschädigte Downloads; sie ist keine unabhängige digitale Signatur. Download und Weiterleitungen bleiben HTTPS.

Fehlende Voraussetzungen können auf Debian/Ubuntu bei Root-Installation über APT ergänzt werden; eine unterbrochene Paketinstallation wird nicht automatisch repariert. Vorhandene Docker-Installationen werden nicht ersetzt. Unter Ubuntu kann der Starter ein fehlendes Compose-v2-Paket ergänzen; wenn Compose v2 auf einer anderen Distribution nicht verfügbar ist, nennt er diese Voraussetzung. Mit `--skip-dependencies` werden Systempakete nicht installiert.

Optionen wie Serveradresse und Geräte-ID werden nach `--` weitergegeben; der Enrollment-Token wird weiterhin verdeckt abgefragt:

```sh
sudo sh linad-install.sh device -- --server https://directory.example --device-id DEVICE_ID
sudo sh linad-install.sh device --version v0.1.0 -- --server https://directory.example --device-id DEVICE_ID
```

**Updates:** denselben Aufruf erneut ausführen. `latest` lädt die aktuelle veröffentlichte stabile Version; `--version TAG` hält eine feste Version. Bestehende Geräteschlüssel, Konten und Serverdaten werden durch die Paket-Installer weiterverwendet. Laufenden GUI-Client vorher beenden. Enrollment und Passwortabfragen bleiben bewusst interaktiv.

**Windows:** [install-windows.cmd herunterladen](https://github.com/Pommesmajo08/lin_ad/releases/latest/download/install-windows.cmd) und doppelt anklicken. Der Starter lädt `install.ps1`, prüft und entpackt das Windows-Paket und richtet die GUI/CLI für den aktuellen Benutzer ein. Alternativ `install.ps1` herunterladen und in PowerShell ausführen:

```powershell
powershell -NoProfile -File .\install.ps1
powershell -NoProfile -File .\install.ps1 -Version v0.1.0
```

Lokale/unternehmensweite PowerShell-Ausführungsrichtlinien werden nicht umgangen. Diese Download-Adressen funktionieren erst, sobald ein vollständiges Release veröffentlicht ist. Für private Repositories den Starter zunächst im angemeldeten Browser von der Release-Seite herunterladen und `GH_TOKEN` mit Leserechten auf die Repository-Inhalte im aktuellen Terminal bereitstellen. Die Starter laden dann über die GitHub-API. Das Token wird nicht in Installationsdateien oder Download-URLs gespeichert. Für den authentifizierten Linux-Download wird zusätzlich Python 3 benötigt (Root-Installer können es als Voraussetzung ergänzen).

Bei Root-Installation unter Linux nur diese Variable gezielt durchreichen:

```sh
sudo --preserve-env=GH_TOKEN sh install.sh device
```

Unter Windows übernehmen `install-windows.cmd` und `install.ps1` dieselbe Umgebungsvariable. Nach der Installation `GH_TOKEN` aus dem Terminal entfernen. Keine persönlichen Tokens in kopierte Installationsbefehle, Skripte oder Enrollment-Dialoge einfügen.

## Windows-Geräte zentral verwalten

Zusätzlich zur Windows-GUI gibt es einen eigenen **Windows-Geräteagenten** für Hintergrundjobs als SYSTEM-Dienst. In der Webverwaltung ein Windows-Gerät registrieren, **Windows-Agent einrichten** wählen und `install-agent-windows.cmd` aus dem Release herunterladen. In einer Administrator-PowerShell:

```powershell
.\install-agent-windows.cmd -Server https://directory.example -DeviceID DEVICE_ID
```

Alternativ `install-agent-windows.cmd` aus dem Release **als Administrator** starten. Der Agent unterstützt Inventar, Windows-Updates und eigene PowerShell-Jobs. Updates verwenden den vorhandenen Geräteschlüssel. Details zu Einrichtung, TLS, Dienst und Wiederherstellung: [Windows-Geräteagent](windows-agent.md).

Für die Windows-Nutzeranmeldung zusätzlich `-EnableLogon` verwenden. Die [Windows-Anmeldeanleitung](windows-logon.md) beschreibt Aktivierung, Gerätefreigaben, Tests und Wiederherstellung.

## Welches Paket brauche ich?

| Ziel | Paket | Start |
|---|---|---|
| Server auf einem Linux-Docker-Host | `linad-server-docker.tar.gz` | `sudo sh scripts/install-server.sh` |
| Ubuntu/Debian an Linad anbinden (PAM, sudo, Jobs) | `linad-linux-provisioner-amd64.tar.gz` oder `arm64` | `sudo sh scripts/install-linux.sh` |
| Verwaltung unter Linux, ohne Systemanbindung | `linad-client-linux-amd64.tar.gz` | `sh scripts/install-client-linux.sh` |
| Windows-Gerätejobs als SYSTEM-Dienst | `linad-windows-agent-amd64.zip` | `powershell -NoProfile -File .\scripts\install-windows-agent.ps1` |
| Verwaltung unter Windows | `linad-client-windows-amd64.zip` | `powershell -NoProfile -File .\scripts\install-client.ps1` |

Pakete jeweils in einen eigenen Ordner **vollständig entpacken**, dann im entpackten Ordner den Befehl ausführen. Der GUI-Client verwaltet Linad; für Linux-Systemanmeldung und Root-Jobs ist zusätzlich der Geräte-Provisioner erforderlich. Windows-Systemanmeldung ist weiterhin nicht enthalten.

## 1. Server

Voraussetzungen: Linux, Docker Engine, Docker Compose v2, `openssl` und `curl`. Der Installer installiert keine Systempakete automatisch. Zuerst optional prüfen:

```sh
tar -xzf linad-server-docker.tar.gz
sudo sh scripts/install-server.sh --check
sudo sh scripts/install-server.sh
```

Der Installer lädt das mitgelieferte Server-Image und startet den Server. Konfiguration und zufälliges Bootstrap-Passwort liegen unter `/etc/linad-server`, außerhalb des Repositorys. Die Secret-Datei ist nur für Root und die Container-UID lesbar; die Compose-Datei verweist direkt auf sie. `LINAD_ADMIN_SECRET_FILE` muss für diesen Installationsweg nicht gesetzt werden.

Im Netzwerk `http://SERVER-IP:8080` oder lokal `http://127.0.0.1:8080` öffnen. Benutzername: `admin`. Das Passwort auf dem Docker-Host anzeigen:

```sh
sudo cat /etc/linad-server/admin-password
```

Danach im eigenen Konto ändern. Das Bootstrap-Secret setzt bei späteren Starts **kein vorhandenes Passwort zurück**. Entfernte Geräte können direkt `http://SERVER-IP:8080` verwenden. HTTPS kann über einen Reverse-Proxy ergänzt werden.

```sh
sudo docker compose -f /etc/linad-server/compose.yaml ps
sudo docker compose -f /etc/linad-server/compose.yaml logs --tail 100
```

Für Updates das neue Paket entpacken und denselben Installer erneut ausführen. Passwortdatei und Compose-Anpassungen bleiben erhalten. Das Volume `lin_ad_directory-data` enthält die Datenbank; regelmäßig sichern. Der Installer verwendet den Compose-Projektnamen `lin_ad`, passend zum Standard-Checkout. Bei einer bisherigen Installation mit **anderem Projektnamen, anderen Volumes oder Ports** die bestehende Compose-Installation weiterverwenden und nur das neue Image laden; es erfolgt keine automatische Datenmigration. Keine parallelen Server auf derselben Datenbank starten.

Bestehende Compose-Dateien werden bei Updates beibehalten. Falls der Server bisher nur lokal erreichbar war, in `/etc/linad-server/compose.yaml` die Portfreigabe `127.0.0.1:8080:8080` durch `8080:8080` ersetzen und `sudo docker compose -f /etc/linad-server/compose.yaml up -d` ausführen.

Mit `--config-dir /ABSOLUTER/PFAD` ist ein anderer externer Konfigurationsordner möglich. `--no-start` legt nur die Konfiguration an. Der Installer verwendet ausdrücklich den lokalen Docker-Daemon unter `/var/run/docker.sock`; entfernte Docker-Kontexte werden nicht verwendet.

## Speicherverbrauch und OOM-Diagnose

Das Server-Image setzt standardmäßig `GOMEMLIMIT=512MiB`. Dies ist ein weiches Go-Speicherbudget: Es beeinflusst die Speicherbereinigung, begrenzt aber weder den gesamten Prozess-RSS noch die VM hart. Bei direkt gestarteten Servern kann derselbe Wert als Umgebungsvariable gesetzt werden. Ein expliziter Wert in Compose überschreibt den Image-Standard.

Um bei einer VM mit ausreichend RAM einen einzelnen Container hart auf 1 GiB zu begrenzen, kann unter dem vorhandenen Dienst `directory` ergänzt werden (vorhandene Secrets, Volumes und Portfreigaben beibehalten):

```yaml
    mem_limit: 1g
    environment:
      LINAD_ADMIN_PASSWORD_FILE: /run/secrets/admin_password
      GOMEMLIMIT: 512MiB
```

Danach `sudo docker compose -f /etc/linad-server/compose.yaml up -d` ausführen. Die harte Grenze kann bei tatsächlich höherem Speicherbedarf den Container beenden; sie schützt die übrige VM vor diesem Container. Bei großen Verzeichnissen beide Werte anpassen. `restart: unless-stopped` startet den Container anschließend neu.

Für die Eingrenzung: `sudo docker stats --no-stream`, `free -h` und `sudo journalctl -k -b | tail -100` zeigen Containerverbrauch, VM-Speicher und mögliche OOM-Killer-Meldungen. `sudo docker inspect --format '{{.State.OOMKilled}}' CONTAINERNAME` zeigt, ob der aktuelle Containerzustand einen OOM-Abbruch meldet; nach automatischem Neustart kann diese Information bereits überschrieben sein. Hyper-V-Hostspeicher und dynamischer VM-Speicher müssen zusätzlich auf dem Host geprüft werden.

## 2. Ubuntu-/Debian-Gerät

In der Webverwaltung unter **Geräte** ein Linux-Gerät registrieren und Anmeldezugriff für Benutzer/Gruppen vergeben. **Linux provisionieren** zeigt Geräte-ID und den einmaligen, 15 Minuten gültigen Enrollment-Token.

```sh
tar -xzf linad-linux-provisioner-amd64.tar.gz
sudo sh scripts/install-linux.sh
```

Der Assistent fragt Serveradresse, Geräte-ID und optional eine CA-Datei ab. Er prüft die Serververbindung, bevor der Token eingelöst oder Dateien installiert werden. Den Token erst bei der verdeckten Abfrage eingeben. Eine offene lokale Root-/Administratorsitzung für den ersten Anmeldetest beibehalten.

Alle Werte können auch als Optionen übergeben werden:

```sh
sudo sh scripts/install-linux.sh --server https://directory.example --device-id DEVICE_ID --check
sudo sh scripts/install-linux.sh --server https://directory.example --device-id DEVICE_ID
```

`--check` verändert keine Dateien, Konten oder PAM-Konfiguration und löst keinen Token ein. Für eigene Zertifikate `--ca-file /pfad/ca.pem` verwenden. HTTP ist standardmäßig erlaubt; `--allow-http` ist nicht mehr erforderlich. Hilfe: `--help`.

**Update:** Neues Paket entpacken und `sudo sh scripts/install-linux.sh` erneut starten. Geräteschlüssel werden wiederverwendet, kein neuer Token nötig. Ein laufender Job wird beim Dienstneustart abgewartet; das kann bis zu 30 Minuten dauern.

```sh
sudo /usr/local/libexec/linad-linux doctor
sudo systemctl status linad-linux
sudo journalctl -u linad-linux --tail 100
```

`doctor` prüft Konfigurationsrechte, Serververbindung und Geräteschlüssel und zeigt die Anzahl freigegebener Konten. Es verändert keine lokalen Konten. Bei null Freigaben zuerst die Anmeldeberechtigungen im Webinterface ergänzen. Weitere Details, Wiederherstellung und Deinstallation: [Linux-Provisionierung](linux-provisioning.md).

Bei unterbrochenem dpkg bricht die Vorabprüfung ab. Zuerst auf dem Zielgerät die Paketinstallation abschließen (`sudo dpkg --configure -a`, gegebenenfalls `sudo apt-get --fix-broken install`), dann erneut installieren. Der Installer führt keine automatische Paketdatenbank-Reparatur aus.

## 3. GUI und CLI

### Linux

```sh
tar -xzf linad-client-linux-amd64.tar.gz
sh scripts/install-client-linux.sh
```

Ohne `sudo`, als der Benutzer, der den Client verwenden soll. Danach **Linad** im Anwendungsmenü öffnen. Die GUI und CLI liegen unter `~/.local/bin`. In der GUI Serveradresse eingeben und anmelden. `xdg-open` und ein Standardbrowser werden für das automatische Öffnen benötigt; alternativ kann der Client mit `--no-browser` gestartet und seine lokale Startadresse im Browser geöffnet werden.

Updates: laufenden Client über **Client beenden** schließen, neues Paket entpacken und erneut installieren. Entfernen: `sh scripts/install-client-linux.sh --uninstall`. Der Linux-Provisionierungsdienst wird dadurch nicht verändert.

### Windows (AMD64)

ZIP über **Alle extrahieren** entpacken und **install-windows.cmd** doppelt anklicken. Alternativ PowerShell im entpackten Ordner öffnen:

```powershell
powershell -NoProfile -File .\scripts\install-client.ps1
```

Installation erfolgt ohne Administratorrechte nach `%LOCALAPPDATA%\Programs\Linad`; im Startmenü erscheint **Linad**. Alternativ kann die GUI-EXE unter `dist` direkt gestartet werden, falls die lokale PowerShell-Richtlinie Skripte blockiert. Unternehmensrichtlinien bleiben bestehen. Der Installer ist in dieser Linux-Buildumgebung nicht nativ unter Windows getestet.

Vor Updates den Client über **Client beenden** schließen. Neues Paket entpacken und erneut installieren. Deinstallation:

```powershell
powershell -NoProfile -File .\scripts\install-client.ps1 -Uninstall
```

Keine Passwörter oder Server-Sitzungstoken werden vom Installer gespeichert. Die Serverauswahl erfolgt wie bisher beim Programmstart.

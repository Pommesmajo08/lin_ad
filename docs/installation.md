# Linad installieren

## Welches Paket brauche ich?

| Ziel | Paket | Start |
|---|---|---|
| Server auf einem Linux-Docker-Host | `linad-server-docker.tar.gz` | `sudo sh scripts/install-server.sh` |
| Ubuntu/Debian an Linad anbinden (PAM, sudo, Jobs) | `linad-linux-provisioner-amd64.tar.gz` oder `arm64` | `sudo sh scripts/install-linux.sh` |
| Verwaltung unter Linux, ohne Systemanbindung | `linad-client-linux-amd64.tar.gz` | `sh scripts/install-client-linux.sh` |
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

Lokal `http://127.0.0.1:8080` öffnen. Benutzername: `admin`. Das Passwort auf dem Docker-Host anzeigen:

```sh
sudo cat /etc/linad-server/admin-password
```

Danach im eigenen Konto ändern. Das Bootstrap-Secret setzt bei späteren Starts **kein vorhandenes Passwort zurück**. Vor der Anbindung entfernter Geräte HTTPS über einen eigenen Reverse-Proxy einrichten.

```sh
sudo docker compose -f /etc/linad-server/compose.yaml ps
sudo docker compose -f /etc/linad-server/compose.yaml logs --tail 100
```

Für Updates das neue Paket entpacken und denselben Installer erneut ausführen. Passwortdatei und Compose-Anpassungen bleiben erhalten. Das Volume `lin_ad_directory-data` enthält die Datenbank; regelmäßig sichern. Der Installer verwendet den Compose-Projektnamen `lin_ad`, passend zum Standard-Checkout. Bei einer bisherigen Installation mit **anderem Projektnamen, anderen Volumes oder Ports** die bestehende Compose-Installation weiterverwenden und nur das neue Image laden; es erfolgt keine automatische Datenmigration. Keine parallelen Server auf derselben Datenbank starten.

Mit `--config-dir /ABSOLUTER/PFAD` ist ein anderer externer Konfigurationsordner möglich. `--no-start` legt nur die Konfiguration an. Der Installer verwendet ausdrücklich den lokalen Docker-Daemon unter `/var/run/docker.sock`; entfernte Docker-Kontexte werden nicht verwendet.

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

`--check` verändert keine Dateien, Konten oder PAM-Konfiguration und löst keinen Token ein. Für eigene Zertifikate `--ca-file /pfad/ca.pem` verwenden. Bei explizit gewünschtem unverschlüsseltem HTTP ist `--allow-http` erforderlich. Hilfe: `--help`.

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

# lin_ad

Ein eigener Verzeichnisdienst in Go mit Server-API, GUI-/CLI-Clients für Linux und Windows sowie einem Linux-Provisionierungsdienst für Debian/Ubuntu. Enthalten sind Benutzer/Gruppen, Darkmode, Kontensperren, Passwörter, Geräteverwaltung und gerätebezogene Linux-Anmeldung über PAM. Windows-Systemanmeldung, LDAP/Kerberos und Hochverfügbarkeit sind noch nicht implementiert. Keine AD-Kompatibilität.

Die Linux-Installation ist in [docs/linux-provisioning.md](docs/linux-provisioning.md) beschrieben. Sie erstellt reguläre lokale Konten für die pro Gerät freigegebenen Benutzer; Administratoren erhalten zusätzlich sudo mit Linad-Passwort. Der Server ist bei jeder neuen verwalteten Anmeldung erforderlich; es gibt keine Offline-Passwortkopien.

## Einfach installieren

Die [Installationsanleitung](docs/installation.md) führt durch Server, Linux-Systemanbindung sowie GUI/CLI unter Linux und Windows. Die Pakete enthalten passende Installer; für Linux-Geräte gibt es einen Assistenten, `--check` und eine `doctor`-Diagnose.

## Bauen und testen

Go 1.27 oder neuer und Python 3 zum Erstellen des Windows-ZIP-Pakets; keine externen Go-Abhängigkeiten. Docker mit Compose für Containerbetrieb.

```sh
go test -race ./...
go vet ./...
sh scripts/build.sh
docker build -t linad-server:local .
```

Ergebnisse unter `dist/` (jeweils amd64):

| Datei | Variante |
|---|---|
| `linad-server` | Linux-Server für Docker |
| `linad-client-linux-amd64` | Linux ohne GUI (CLI) |
| `linad-client-windows-amd64.exe` | Windows ohne GUI (CLI) |
| `linad-client-gui-linux-amd64` | Linux mit GUI im Browser |
| `linad-client-gui-windows-amd64.exe` | Windows mit GUI im Browser, ohne Konsolenfenster |
| `linad-linux-amd64` / `linad-linux-arm64` | Root-Provisionierungsdienst und PAM-Helfer |
| `linad-linux-provisioner-amd64.tar.gz` / `linad-linux-provisioner-arm64.tar.gz` | Linux-Installationspaket mit Dienst, Installer und Anleitung |

Zusätzlich entstehen GUI/CLI-Installationspakete `linad-client-linux-amd64.tar.gz` und `linad-client-windows-amd64.zip`. `python3 scripts/test-container.py --export-image` erstellt das geprüfte Docker-Installationspaket `linad-server-docker.tar.gz`.

Windows wird crosskompiliert; ein nativer Windows-Lauf ist separat zu testen. Ein exportiertes Image kann mit `docker load -i dist/linad-server-image.tar` importiert werden.

Das Dockerfile verpackt den zuvor gebauten Linux-Server. Für Änderungen zuerst das Buildskript erneut ausführen.

## Server starten

Lege außerhalb des Repositorys eine Datei mit einem zufälligen Administratorpasswort (mindestens 16 Zeichen, ohne abschließenden Zeilenumbruch) an. Der Container läuft als UID/GID `65532:65532`; diese UID muss die Secret-Datei lesen können (unter Linux beispielsweise Eigentümer `65532:65532`, Modus `0400`). Compose bindet dateibasierte Secrets ein und korrigiert deren Host-Rechte nicht automatisch. Setze `LINAD_ADMIN_SECRET_FILE` auf den absoluten Dateipfad und starte:

```sh
docker compose up -d --build
```

Die Datei wird als Compose-Secret eingebunden. Beim ersten Start entsteht der Benutzer `admin`; spätere Starts verwenden die vorhandene Datenbank. Ändern des Bootstrap-Secrets ändert kein gespeichertes Passwort. Das Volume `directory-data` enthält die Daten und muss gesichert werden. Nur eine Serverinstanz pro Datei betreiben. Daten sind nicht verschlüsselt; Dateisystem und Backups entsprechend schützen.

HTTP ist standardmäßig nur an den Loopback-Port des Hosts gebunden. Für Netzwerkzugriff TLS verwenden: entweder einen HTTPS-Reverse-Proxy oder `LINAD_TLS_CERT` und `LINAD_TLS_KEY` im Server setzen und die Dateien schreibgeschützt in den Container einbinden. Ports und Client-URL passend konfigurieren. Keine Passwörter oder Token über unverschlüsseltes HTTP im Netzwerk senden.

Alternativ lokal `LINAD_ADMIN_PASSWORD` setzen und `go run ./cmd/server` starten. Standard: `127.0.0.1:8080`, Daten: `data/directory.json`. `LINAD_DATA` und `LINAD_LISTEN` überschreiben diese Werte. `GET /healthz` liefert `{"status":"ok"}`.

## Webverwaltung

Nach dem Start die Serveradresse im Browser öffnen: lokal `http://127.0.0.1:8080/`, im Netzwerk ausschließlich die eingerichtete HTTPS-Adresse. Als `admin` anmelden. Die Oberfläche bietet Benutzer- und Gruppenlisten, Anlegen/Löschen, Mitgliedschaften, Sperren/Entsperren, Passwortzurücksetzen und Geräte-Anmelderegeln. Geräte können ausschließlich Administratoren registrieren, ändern und entfernen. Im Kopfbereich ist die Darstellung „System“, „Hell“ oder „Dunkel“ wählbar; nur diese nicht geheime Einstellung wird lokal gespeichert. Normale Benutzer können ihr eigenes Passwort ändern. Administratoren können weitere Administratoren ernennen oder deren Rolle entziehen. Die Rolle verleiht auch sudo auf freigegebenen und aktualisierten Linux-Geräten. Der Bootstrap-Administrator kann weder gesperrt, gelöscht noch zum normalen Benutzer herabgestuft werden. Unter „Meine Gruppen“ sehen alle Benutzer ihre eigenen Gruppenmitgliedschaften.

Sitzungstoken bleiben ausschließlich im Arbeitsspeicher des geöffneten Tabs; ein Neuladen der Seite erfordert eine neue Anmeldung. Passwörter oder Token werden nicht in Local Storage geschrieben. Der Server liefert die Oberfläche selbst aus; kein Node.js oder separater Frontend-Dienst ist nötig. Bestehende Datenbestände werden weiterverwendet, vorhandene Konten sind standardmäßig aktiv.

Eine Kontensperre oder Passwortänderung beendet alle Sitzungen des Kontos sofort. Entsperren stellt alte Sitzungen nicht wieder her. Eine Passwortänderung ist erst nach erfolgreichem Speichern wirksam. Die Gruppe bleibt beim Sperren eines Benutzers unverändert.

## GUI-Client für Linux und Windows

Die GUI-Datei starten (unter Linux gegebenenfalls `chmod +x dist/linad-client-gui-linux-amd64`). Sie öffnet automatisch den Standardbrowser. Unter Linux wird `xdg-open` benötigt; unter Windows wird der Standardbrowser über das Betriebssystem gestartet. Es handelt sich um einen eigenständigen Client mit lokal bereitgestellter Browseroberfläche, nicht um ein natives Desktop-Fenster.

1. Serveradresse eingeben, z. B. `https://directory.example:8443`.
2. Nur für lokale Tests HTTP ausdrücklich erlauben.
3. Mit einem Verzeichnisbenutzer anmelden.
4. Als Administrator Benutzer/Gruppen und Gerätezugriff verwalten; als normaler Benutzer das eigene Passwort ändern und freigegebene Geräte sehen.

Die GUI bindet ausschließlich an `127.0.0.1` auf einem zufälligen Port. API-Aufrufe erfordern zusätzlich einen zufälligen lokalen Zugriffsschlüssel. Dieser steht im Fragment der privaten Startadresse, nicht in einem Server-Token. Diese Adresse nicht teilen. Server-TLS wird vom lokalen Go-Client anhand des System-Zertifikatsspeichers geprüft. Sitzungstoken bleiben im Arbeitsspeicher des Browser-Tabs.

Die Serverauswahl gilt für einen Programmlauf. Zum Wechseln „Client beenden“ anklicken und das Programm neu starten; damit gelangen bestehende Tokens nicht an einen anderen Server. Das Schließen des Browser-Tabs allein beendet den lokalen Client-Prozess nicht.

Optionale Startparameter:

```sh
dist/linad-client-gui-linux-amd64 --server https://directory.example:8443
dist/linad-client-gui-linux-amd64 --server http://127.0.0.1:8080 --allow-http
```

Für automatisierte Tests `--no-browser` verwenden. Der Linux-Prozess gibt seine private Startadresse aus. Der Windows-GUI-Build hat kein Konsolenfenster; die Serverauswahl ist vollständig in der Oberfläche möglich.

## Client ohne GUI (CLI)

Linux: `dist/linad-client-linux-amd64`; Windows: `dist/linad-client-windows-amd64.exe`. Die folgenden Beispiele verwenden Linux-Shell-Syntax. In PowerShell Umgebungsvariablen mit `$env:LINAD_PASSWORD` bzw. `$env:LINAD_TOKEN` setzen.

Setze `LINAD_PASSWORD` sicher auf das Administratorpasswort, dann:

```sh
dist/linad-client-linux-amd64 --server http://127.0.0.1:8080 --allow-http login admin
```

Die Antwort enthält einen geheimen Bearer-Token und das Ablaufdatum. Den Token für weitere Aufrufe in `LINAD_TOKEN` setzen; er wird nicht automatisch auf Datenträger gespeichert. Ausgabe nicht in öffentliche Logs übernehmen. Für HTTPS `--server https://directory.example:8443` verwenden und `--allow-http` weglassen. Der Client prüft die System-Zertifikatskette; eine private CA muss im Betriebssystem vertraut sein.

Weitere Befehle (Serverflags jeweils vor dem Befehl):

```text
me
logout
users
user-add alice           # liest neues Passwort aus LINAD_PASSWORD
user-delete alice
user-disable alice
user-enable alice
user-admin alice         # Administratorrolle und sudo auf freigegebenen Linux-Geräten
user-standard alice      # Administratorrolle entziehen
my-groups                # eigene Gruppenmitgliedschaften
user-password alice     # neues Passwort aus LINAD_NEW_PASSWORD
password-change         # bisheriges aus LINAD_PASSWORD, neues aus LINAD_NEW_PASSWORD
groups
group-add engineering
group-delete engineering
member-add engineering alice
member-delete engineering alice
devices
device-register         # registriert lokalen Hostnamen und OS
device-register office-pc
device-heartbeat DEVICE_ID
device-delete DEVICE_ID
# Die folgenden Befehle sind ebenfalls nur für Administratoren:
device-access DEVICE_ID alice bob     # vollständige Freigabe nur für diese Benutzer
device-access-groups DEVICE_ID team   # vollständige Freigabe nur für diese Gruppen
device-access-all DEVICE_ID           # ausdrücklich alle aktiven Benutzer
device-access DEVICE_ID               # alle Freigaben entfernen
device-enrollment DEVICE_ID           # einmaliger Linux-Installations-Token
device-revoke DEVICE_ID               # Geräte-Schlüssel und offene Tokens widerrufen
```

Benutzer mit Administratorrolle verwalten Benutzer und Gruppen. Normale Benutzer können sich anmelden, `me`, `my-groups`, `logout`, `password-change` sowie `devices` zum Anzeigen freigegebener Geräte verwenden. Alle anderen Gerätebefehle erfordern Administratorrechte. Nach Passwortänderungen neu anmelden und `LINAD_TOKEN` ersetzen. `LINAD_NEW_PASSWORD` ebenfalls sicher setzen und nach Gebrauch entfernen. Benutzerlöschung entfernt Mitgliedschaften und macht bestehende Token unbrauchbar. Sitzungen laufen nach acht Stunden ab; Neustart meldet alle Benutzer ab. Passwort-Hashes verwenden PBKDF2-HMAC-SHA256 mit individuellem Salt und 600.000 Iterationen. Login ist global auf 30 Versuche pro Minute begrenzt; für öffentlichen Betrieb sind zusätzliche Schutzmaßnahmen am Reverse-Proxy und eine Sicherheitsprüfung nötig.

## Geräteverwaltung

Nur Administratoren registrieren und ändern Geräte. Namen sind im Verzeichnis eindeutig. Der Ersteller wird zu Dokumentationszwecken gespeichert, hat aber keinen automatischen Linux-Anmeldezugriff. Normale Benutzer sehen nur Geräte, für die sie eine effektive Freigabe haben.

Pro Gerät erlauben Administratoren einzelne Benutzer, Gruppen oder ausdrücklich alle aktiven Benutzer. Standardmäßig ist kein Benutzer freigegeben. Gesperrte Konten werden immer ausgeschlossen. Beim Löschen eines Benutzers/einer Gruppe werden die entsprechenden Freigabeverweise entfernt; das Gerät bleibt bestehen. Änderungen gelten sofort für neue PAM-Anmeldungen. Die lokale Kontoanlage erfolgt beim nächsten Sync (standardmäßig alle 30 Sekunden).

Die Geräteverwaltung zeigt einen stabilen Geräte-Identifier, Plattform und letzte Rückmeldung. Der Root-Dienst aktualisiert die Rückmeldung bei erfolgreichem Sync. Manuelle Inventarrückmeldungen sind nur für Administratoren möglich und sind kein Nachweis einer laufenden Benutzersitzung. Linux-Geräte lassen sich per einmaligem Enrollment-Token provisionieren; Windows-Geräte bleiben derzeit Inventareinträge. Linux-Anmeldungen und Installation: [Linux-Provisionierung](docs/linux-provisioning.md).

`device-access`, `device-access-groups` und `device-access-all` ersetzen jeweils die vollständige Freigabe. Für kombinierte Benutzer- und Gruppenregeln die Webverwaltung oder den API-Aufruf mit beiden Listen verwenden. Das Entfernen von Freigaben beendet keine bereits laufenden Sitzungen.

## API

JSON unter `/v1`, Authentifizierung mit `Authorization: Bearer TOKEN`:

| Methode | Pfad | Inhalt |
|---|---|---|
| POST | /v1/login | `{"name":"admin","password":"…"}` |
| GET | /v1/me | Eigenes Profil |
| GET | /v1/me/groups | Eigene Gruppenmitgliedschaften |
| POST | /v1/logout | Sitzung beenden |
| GET | /v1/devices | Freigegebene Geräte; Admin sieht alle |
| POST | /v1/devices | Admin: `{"name":"office-pc","os":"linux"}` (auch `windows`) |
| POST | /v1/devices/{id}/heartbeat | Admin: manuelle Inventarrückmeldung |
| DELETE | /v1/devices/{id} | Admin: Gerät entfernen und Schlüssel ungültig machen |
| PUT | /v1/devices/{id}/access | Admin: `{"allow_all":false,"allowed_users":["alice"],"allowed_groups":["team"]}` |
| POST | /v1/devices/{id}/enrollment | Admin: einmaliger Token, 15 Minuten gültig |
| DELETE | /v1/devices/{id}/credential | Admin: Geräte-Schlüssel und Enrollment-Token widerrufen |
| GET / POST | /v1/users | Liste / `{"name":"alice","password":"…"}` |
| DELETE | /v1/users/{name} | Benutzer löschen |
| PATCH | /v1/users/{name} | `{"disabled":true/false,"admin":true/false}` (Admin; mindestens ein Feld) |
| POST | /v1/users/{name}/password | `{"password":"…"}` (Admin setzt Passwort) |
| POST | /v1/password | `{"current_password":"…","new_password":"…"}` (eigenes Passwort) |
| GET / POST | /v1/groups | Liste / `{"name":"engineering"}` |
| DELETE | /v1/groups/{name} | Gruppe löschen |
| PUT / DELETE | /v1/groups/{group}/members/{user} | Mitglied hinzufügen / entfernen |

Namen: Kleinbuchstabe als erstes Zeichen, danach Kleinbuchstaben, Ziffern, Punkt, Unterstrich oder Bindestrich; höchstens 64 Zeichen. Neue Passwörter mindestens 16 Zeichen. Unbekannte JSON-Felder werden abgelehnt.

## Aktualisierung einer bestehenden Installation

```sh
git pull --ff-only
go test ./...
sh scripts/build.sh
docker compose --env-file .env up -d --build
```

In `.env` neben `compose.yaml` den absoluten Pfad setzen, z. B. `LINAD_ADMIN_SECRET_FILE=/etc/linad/admin-password`. Bei Nutzung von `sudo` wird ein vorheriges `export` normalerweise nicht übernommen; die explizite `.env`-Datei vermeidet das. Das bestehende Volume beibehalten und vor Updates sichern. `docker compose down -v` löscht den Datenbestand und ist kein Update-Schritt. Der neue Container liefert nach dem Update die Weboberfläche an `/` aus.

Optionaler Browser-Funktionstest nach dem Build (Python-Paket `playwright` und `/usr/bin/chromium` erforderlich):

```sh
python3 scripts/test-web.py
```

Der Test startet einen temporären Server auf Port 18082 mit eigener Datenbank und prüft Anmeldung, Benutzer/Gruppen, Mitgliedschaften, Kontensperre, Passwortzurücksetzen, eigenen Passwortwechsel und die mobile Anmeldeseite. Produktivdaten werden nicht verwendet.

Der zusätzliche GUI-Test startet den lokalen GUI-Client und prüft dessen Zusammenspiel mit Server und CLI (Port 18083 und zufälliger lokaler GUI-Port):

```sh
python3 scripts/test-gui.py
```

Gerätenamen dürfen zusätzlich mit einer Ziffer beginnen. Geräte-Ersteller werden immer aus der Administratorsitzung abgeleitet.

Die Maschinen-API verwendet `Authorization: Device DEVICE_ID.SECRET` statt Benutzertokens:

| Methode | Pfad | Zweck |
|---|---|---|
| POST | `/v1/enroll` | Einmaligen Enrollment-Token gegen Geräte-Schlüssel tauschen |
| GET | `/v1/machine/users` | Freigegebene Unix-Konten für dieses Gerät; Sync-Rückmeldung |
| POST | `/v1/machine/authenticate` | PAM: Linux-Login, Passwort und Gerätezugriff prüfen |
| POST | `/v1/machine/access` | PAM-Kontophase: Gerätezugriff ohne Passwort prüfen |

Die Maschinen-API liefert keine Passwort-Hashes. Bestehende Verzeichnisse erhalten beim Upgrade automatisch stabile Unix-IDs (reservierter Bereich 60001–64999). Alte Geräte ohne Freigaben sind standardmäßig gesperrt; der frühere Gerätebesitz verleiht keinen Zugriff. Vor dem ersten Upgrade eine Datensicherung erstellen.

## Isolierter Linux/PAM-Integrationstest

```sh
sh scripts/test-linux.sh
```

Der Test benötigt Docker und lädt ein Debian-Testimage mit `pamtester`. Er startet ausschließlich einen wegwerfbaren Container und bindet das Repository schreibgeschützt ein. Im Container prüft er TLS und Enrollment, echte PAM-/`su`-Anmeldungen, lokale Kontenkollisionen, Benutzer-/Gruppenfreigaben, Kontensperren, Passwortänderungen, Hintergrund-Synchronisierung, Schlüsselwiderruf, Offline-Verhalten, Wiederholbarkeit des Installers und Deinstallation. Die PAM-Konfiguration des Hosts wird nicht verändert. Die Datei `tests/linux/integration.py` darf nicht direkt auf einem Arbeitsrechner ausgeführt werden.

Server-Image mit temporärem Datenvolume funktional prüfen und optional exportieren (nach dem Build):

```sh
python3 scripts/test-container.py --export-image
```

Der Test verwendet Port 18081, prüft Administratorpflicht, Freigaben, Enrollment und Persistenz der Geräteidentität nach Neustart und entfernt seinen Container samt Testvolume anschließend wieder.

## Linux-sudo-Update

Für sudo-Rechte sowohl Server als auch das Linux-Provisioner-Paket aktualisieren. Auf bereits eingerichteten Linux-Geräten genügt danach `sudo sh scripts/install-linux.sh`; die Konfiguration bleibt erhalten. Nur eine neue Binärdatei zu kopieren installiert die zusätzlich benötigten PAM-Prüfungen nicht. Siehe [sudo und Update-Anleitung](docs/linux-provisioning.md#administratoren-und-sudo).

Freigegebene Administratoren verwenden ihr Linad-Passwort für `sudo` und `sudo -i`. Rollenentzug, Kontensperre und Gerätewiderruf werden bei neuen sudo-Aufrufen online geprüft. Bereits laufende Root-Shells werden nicht beendet. Normale Benutzer bekommen keine Root-Rechte. Die Maschinen-Passwortprüfung erlaubt bis zu 120 Versuche pro Minute und Gerät; die öffentliche Web-Anmeldung behält ihr eigenes Limit von 30 Versuchen pro Minute.

## Automatisierung und geplante Jobs

Unter **Automatisierung** können Administratoren Linux-Geräten Systeminfo-, Paketlisten- und Paketupdate-Jobs zuweisen. Unterstützt sind sofortige Einreihung, einmalige Startzeiten, wiederkehrende Intervalle, Pausieren, Zuordnungsänderungen, Status und Ausgaben. Der Linux-Dienst arbeitet Aufträge im Hintergrund ab und hält Ergebnisberichte bei Verbindungsproblemen lokal vor.

Server und Linux-Provisioner-Paket aktualisieren; auf bestehenden Geräten `sudo sh scripts/install-linux.sh` erneut als bisheriger lokaler Administrator/Root ausführen. Kein neuer Enrollment-Token nötig. Alte Agenten ohne Job-Worker führen keine Aufträge aus. Windows-Ausführung ist noch nicht enthalten. Eigene Bash-/sh-/Python-Skripte können Administratoren als Custom-Job hinterlegen.

Die vollständige Beschreibung, CLI/API und Wiederanlaufregeln stehen unter [docs/jobs.md](docs/jobs.md). Für Paketupdates bleiben APT-Verifikation und bestehende Konfigurationen erhalten. Es gibt keinen automatischen Neustart und keinen blinden Wiederholungsversuch nach unklarem Ausgang. Aktive Updates dürfen bei einem normalen Agent-Neustart fertig laufen, weshalb das Update des Agenten länger dauern kann.

### Ubuntu 26.04 / sudo-rs testen

Die sudo-Anbindung unterstützt sowohl klassisches sudo als auch sudo-rs ohne `pam_service`-/`pam_login_service`-Defaults. Der Integrationstest kann auf Ubuntu mit sudo-rs ausgeführt werden:

```sh
LINAD_TEST_BASE_IMAGE=ubuntu:26.04 LINAD_TEST_SUDO_PACKAGE=sudo-rs LINAD_TEST_IMAGE=linad-pam-ubuntu-rs:local sh scripts/test-linux.sh
```

Die Tests prüfen echte `sudo`-/`sudo -i`-Aufrufe, sofortigen Rollenentzug, unveränderten lokalen sudo-Zugriff, die Reparatur einer alten Linad-Regel und die Wiederherstellung der PAM-Dateien bei Deinstallation.

## Custom-Jobs mit eigenen Skripten

Unter **Automatisierung → Eigenes Skript (Root)** einen Interpreter wählen und Code einfügen oder eine Datei laden. Unterstützt sind Bash, sh und Python 3 mit maximal 16 KiB UTF-8-Quelltext. Skripte laufen als Root; sie können sofort, einmalig oder wiederkehrend zugeordnet werden. Vorlagen lassen sich bearbeiten, während bereits eingereihte Läufe ihre unveränderliche Skriptkopie behalten. Die damalige Version ist an jeder Ausführung einsehbar.

Server und Linux-Provisioner aktualisieren; anschließend den Linux-Installer auf bestehenden Geräten erneut ausführen. Details zu Rechten, Grenzen, Revisionen, CLI und API: [Eigene Skripte](docs/jobs.md#eigene-skripte).

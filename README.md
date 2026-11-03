# lin_ad

Ein eigener Verzeichnisdienst in Go mit Server-API und CLI für Linux und Windows. Dies ist eine erste Entwicklungsversion: Benutzer, Gruppen, Mitgliedschaften und Anmeldung an der API funktionieren. Eine Webverwaltung, Passwortänderungen und Kontensperren sind enthalten. Windows-Anmeldung, Linux PAM/NSS, LDAP/Kerberos, Gerätebeitritt, weitere Administratoren und Hochverfügbarkeit sind noch nicht implementiert. Keine AD-Kompatibilität.

## Bauen und testen

Go 1.27 oder neuer; keine externen Go-Abhängigkeiten. Docker mit Compose für Containerbetrieb.

```sh
go test -race ./...
go vet ./...
sh scripts/build.sh
docker build -t linad-server:local .
```

Ergebnisse unter `dist/`: Linux-Server, Linux-Client und Windows-Client (amd64). Windows wird crosskompiliert; ein nativer Windows-Lauf ist separat zu testen. Ein exportiertes Image kann mit `docker load -i dist/linad-server-image.tar` importiert werden.

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

Nach dem Start die Serveradresse im Browser öffnen: lokal `http://127.0.0.1:8080/`, im Netzwerk ausschließlich die eingerichtete HTTPS-Adresse. Als `admin` anmelden. Die Oberfläche bietet Benutzer- und Gruppenlisten, Anlegen/Löschen, Mitgliedschaften, Sperren/Entsperren und Passwortzurücksetzen. Normale Benutzer können ihr eigenes Passwort ändern. Der Bootstrap-Administrator kann weder gesperrt noch gelöscht werden.

Sitzungstoken bleiben ausschließlich im Arbeitsspeicher des geöffneten Tabs; ein Neuladen der Seite erfordert eine neue Anmeldung. Passwörter oder Token werden nicht in Local Storage geschrieben. Der Server liefert die Oberfläche selbst aus; kein Node.js oder separater Frontend-Dienst ist nötig. Bestehende Datenbestände werden weiterverwendet, vorhandene Konten sind standardmäßig aktiv.

Eine Kontensperre oder Passwortänderung beendet alle Sitzungen des Kontos sofort. Entsperren stellt alte Sitzungen nicht wieder her. Eine Passwortänderung ist erst nach erfolgreichem Speichern wirksam. Die Gruppe bleibt beim Sperren eines Benutzers unverändert.

## Client

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
user-password alice     # neues Passwort aus LINAD_NEW_PASSWORD
password-change         # bisheriges aus LINAD_PASSWORD, neues aus LINAD_NEW_PASSWORD
groups
group-add engineering
group-delete engineering
member-add engineering alice
member-delete engineering alice
```

Nur `admin` verwaltet Benutzer und Gruppen. Normale Benutzer können sich anmelden, `me`, `logout` und `password-change` verwenden. Nach Passwortänderungen neu anmelden und `LINAD_TOKEN` ersetzen. `LINAD_NEW_PASSWORD` ebenfalls sicher setzen und nach Gebrauch entfernen. Benutzerlöschung entfernt Mitgliedschaften und macht bestehende Token unbrauchbar. Sitzungen laufen nach acht Stunden ab; Neustart meldet alle Benutzer ab. Passwort-Hashes verwenden PBKDF2-HMAC-SHA256 mit individuellem Salt und 600.000 Iterationen. Login ist global auf 30 Versuche pro Minute begrenzt; für öffentlichen Betrieb sind zusätzliche Schutzmaßnahmen am Reverse-Proxy und eine Sicherheitsprüfung nötig.

## API

JSON unter `/v1`, Authentifizierung mit `Authorization: Bearer TOKEN`:

| Methode | Pfad | Inhalt |
|---|---|---|
| POST | /v1/login | `{"name":"admin","password":"…"}` |
| GET | /v1/me | Eigenes Profil |
| POST | /v1/logout | Sitzung beenden |
| GET / POST | /v1/users | Liste / `{"name":"alice","password":"…"}` |
| DELETE | /v1/users/{name} | Benutzer löschen |
| PATCH | /v1/users/{name} | `{"disabled":true}` / `{"disabled":false}` (Admin) |
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

# Automatisierung und geplante Linux-Jobs

Administratoren erstellen Jobs in der Webverwaltung unter **Automatisierung** oder mit dem CLI-Client. Normale Benutzer dürfen weder Jobs anlegen noch deren Ausgaben lesen. Der Server ordnet Ausführungen Geräten zu; der Root-Dienst auf jedem Linux-Gerät holt sie ab. Windows-Jobs und frei eingebbare Shell-Skripte sind in dieser Version nicht enthalten.

## Jobtypen

| Typ | Aktion |
|---|---|
| `system-info` | Hostname, Betriebssystem, Architektur und `/etc/os-release` zurückmelden |
| `apt-refresh` | Paketlisten mit `apt-get update` aktualisieren |
| `apt-upgrade` | Erst Paketlisten aktualisieren, anschließend `apt-get -y upgrade` |

Die Paketaktionen sind für Debian/Ubuntu vorgesehen und laufen als Root. APT-Signaturen und Zertifikatsprüfung bleiben aktiviert. Paketupdates behalten bestehende Konfigurationsdateien über die üblichen dpkg-Optionen, entfernen keine Pakete per `autoremove` und lösen keinen automatischen Neustart aus. Paket-Skripte können Dienste neu starten. Ein fehlgeschlagenes `apt-get update` verhindert den anschließenden Upgrade-Schritt. Paketmanager-Locks werden respektiert; bei konkurrierenden Paketoperationen kann ein Lauf fehlschlagen.

## Geräte vorbereiten

Zuerst den Server, dann das Linux-Provisioner-Paket aktualisieren. Auf bereits eingerichteten Geräten als bisheriger lokaler Administrator/Root:

```sh
sudo sh scripts/install-linux.sh
```

Die vorhandene Konfiguration bleibt erhalten, kein neuer Enrollment-Token ist nötig. Der neue Dienst startet den Job-Worker zusätzlich zur Konto-Synchronisierung. Benutzeranmeldungen und Konto-Sync warten nicht auf längere Paketjobs. Im Gerätefeld der Jobverwaltung kennzeichnet „noch kein Jobabruf“ Geräte, deren neuer Worker noch nicht am Server angefragt hat. Noch nicht provisionierte Geräte können bereits zugeordnet werden; ihre Läufe warten bis zur Einrichtung.

Die Ausführung nutzt die Geräteidentität, unabhängig davon, welche Benutzer interaktiv auf dem Gerät angemeldet oder freigegeben sind. Änderungen an einem bestehenden Zeitplan bleiben organisatorische Gerätekonfiguration, auch wenn das erstellende Administratorkonto später gelöscht oder herabgestuft wird.

## Sofort und geplant ausführen

1. Name und Aktion wählen.
2. Ein oder mehrere Linux-Geräte auswählen.
3. Startzeit leer lassen für sofortige Einreihung oder eine lokale Datum/Uhrzeit eingeben.
4. Wiederholung: `0` für einmalig, z. B. `1440` für alle 24 Stunden oder `10080` für alle 7 Tage. Intervalle beginnen bei 5 Minuten.
5. „Job anlegen“ wählen. Die Ausführung beginnt beim nächsten Geräteabruf, normalerweise innerhalb von 15 Sekunden bei einem erreichbaren, aktuellen Client.

Zeitangaben werden im Browser lokal eingegeben und serverseitig als UTC gespeichert. Wiederholungen sind feste Zeitabstände, keine kalenderbasierten Cron-Termine; Sommerzeitwechsel können die angezeigte lokale Uhrzeit verschieben. Pro Vorlage können maximal 100 Linux-Geräte zugeordnet werden.

Vorlagen lassen sich pausieren, reaktivieren, neu terminieren, anderen Geräten zuordnen und löschen. „Jetzt ausführen“ reiht einen neuen Lauf für die zugeordneten Geräte ein, auch bei pausiertem Zeitplan. Solange für denselben Job/Gerät ein Lauf wartet, läuft oder ungeklärt ist, verhindert der Server eine doppelte Einreihung. Bei einem solchen Konflikt wird der manuelle Aufruf insgesamt abgelehnt.

Pausieren oder Löschen bricht wartende Läufe ab. Entfernte Gerätezuordnungen brechen ebenfalls nur noch nicht gestartete Läufe dieses Geräts ab. Neu zugeordnete Geräte werden beim nächsten geplanten Start oder mit „Jetzt ausführen“ berücksichtigt. Laufende Paketoperationen werden dabei nicht abgebrochen. Gelöschte Vorlagen behalten ihre historischen Ausführungen.

## Ergebnisse und Wiederanlauf

Die Oberfläche zeigt Status, Exit-Code und begrenzte Textausgabe pro Job/Gerät. Der Status aktualisiert sich im geöffneten Tab alle 15 Sekunden; geöffnete Ausgaben bleiben dabei geöffnet. Pro Gerät läuft höchstens ein Job gleichzeitig. Verpasste Zeitplanintervalle bei einem längeren Ausfall werden zu einer Ausführung zusammengefasst, statt sämtliche verpassten Updates nachzuholen.

Der Client speichert Claim und Ergebnis in `/var/lib/linad-linux/jobs.json` (Root:0600). Ein verlorener Abruf wird mit derselben Anfrage-ID wiederholt. Ein verlorener Ergebnis-Upload wird aus der Datei erneut gesendet, ohne den Auftrag noch einmal auszuführen.

Wird der Worker während einer Ausführung unterbrochen, meldet der nächste Start den Lauf als **Unklar** statt ihn zu wiederholen. Auch eine Stunde ohne Ergebnis führt serverseitig zu „Unklar“. Weitere Jobs auf diesem Gerät warten, bis ein verspätetes Ergebnis den Ausgang bestätigt oder ein Administrator nach Geräteprüfung **„Nach Prüfung freigeben“** wählt. Diese Freigabe wiederholt den alten Lauf nicht; sie erlaubt andere wartende/fällige Jobs. Vorher sicherstellen, dass keine alte Paketoperation mehr läuft und der Paketmanager konsistent ist.

Ein gewöhnlicher fehlgeschlagener Job wird nicht sofort automatisch wiederholt; ein aktivierter wiederkehrender Zeitplan bleibt für zukünftige Termine bestehen. Erfolgreiche und fehlgeschlagene Befehle melden ihren Exit-Code; `0` bedeutet Erfolg. Die Ausführung ist auf 30 Minuten begrenzt. Ein Timeout kann eine manuelle Paketmanager-Reparatur erfordern.

Bei einem normalen Dienstneustart wartet der Worker eine bereits gestartete Paketoperation ab, bevor der Dienst endet (bis zum Ausführungslimit). Deshalb kann ein Agent-Update oder eine Deinstallation bis zu etwa 30 Minuten dauern. Ein erzwungener Prozessabbruch, Stromausfall oder Reboot kann dies umgehen und zu einem unklaren Lauf führen.

Der Widerruf des Geräte-Schlüssels verhindert neue Abrufe und Ergebnis-Uploads. Bereits akzeptierte Befehle werden nicht dadurch beendet; Ergebnisse bleiben lokal bis zur Wiederanbindung gespeichert. Den lokalen Job-Zustand nicht löschen, um einen hängenden Lauf zu „reparieren“: erst Gerät und Serverstatus prüfen.

Der Server hält höchstens 200 Vorlagen und 2000 Laufdatensätze; ältere abgeschlossene bzw. ausdrücklich freigegebene Läufe werden bei Bedarf entfernt. Die API zeigt die letzten 200 passenden Ausführungen. Wartende, laufende und ungeklärte Läufe werden nicht automatisch aus der Historie entfernt. Ist die Kapazität mit offenen Läufen belegt, müssen diese abgeschlossen, geprüft oder vor dem Start abgebrochen werden.

## CLI-Beispiele

Wie bei anderen Befehlen vorher Serveradresse und `LINAD_TOKEN` setzen. Alle Jobbefehle erfordern Administratorrechte:

```text
jobs
job-runs
job-create "System prüfen" system-info DEVICE_ID
job-schedule "Tägliche Updates" apt-upgrade "2026-11-04T03:00:00+01:00" 1440 DEVICE_ID
job-run JOB_ID
job-pause JOB_ID
job-resume JOB_ID
job-delete JOB_ID
job-cancel RUN_ID
job-resolve RUN_ID
```

Der letzte Befehl entspricht der ausdrücklichen Freigabe eines zuvor geprüften unklaren Laufs. Zur lokalen Diagnose als Root kann `linad-linux jobs-once` einen Auftrag abholen/ausführen oder einen gespeicherten Ergebnisbericht erneut senden. Der dauerhaft laufende Dienst ist der normale Betriebsweg.

## API

Benutzer-API, nur Administratoren:

| Methode | Pfad | Inhalt |
|---|---|---|
| GET / POST | `/v1/jobs` | Vorlagen lesen / erstellen |
| PATCH | `/v1/jobs/{id}` | `enabled`, `device_ids`, `start_at`, `interval_minutes` ändern |
| POST | `/v1/jobs/{id}/run` | Sofortige Einreihung |
| DELETE | `/v1/jobs/{id}` | Vorlage löschen, wartende Läufe abbrechen |
| GET | `/v1/job-runs` | Historie; Filter `device_id` und `job_id` optional |
| POST | `/v1/job-runs/{id}/cancel` | Nur einen wartenden Lauf abbrechen |
| POST | `/v1/job-runs/{id}/resolve` | Unklaren Lauf nach Prüfung freigeben |

Beispiel für POST `/v1/jobs`:

```json
{"name":"Updates","kind":"apt-upgrade","device_ids":["DEVICE_ID"],"start_at":"2026-11-04T02:00:00Z","interval_minutes":1440,"enabled":true}
```

Maschinen-API mit `Authorization: Device DEVICE_ID.SECRET`:

- POST `/v1/machine/jobs/claim`: `{ "request_id": "64 zufällige Hex-Zeichen" }`; diese ID vor dem Aufruf lokal dauerhaft speichern und bei verlorenem Ergebnis wiederverwenden. Die Antwort enthält `run` oder `null`.
- POST `/v1/machine/jobs/{id}/result`: dieselbe `request_id`, `status`, `exit_code` und `output`. Nur die eigene beanspruchte Ausführung darf gemeldet werden; doppelte identische Berichte werden bestätigt.

Die Geräte führen nur fest implementierte Aktionen aus. Jobnamen und Ausgaben werden als Text angezeigt, nicht als Skript oder HTML ausgeführt.

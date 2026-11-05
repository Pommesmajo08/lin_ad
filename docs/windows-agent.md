# Windows-Geräteagent

Der Agent für Windows 10/11 und Windows Server (AMD64) bindet Geräte an die zentrale Jobverwaltung an. Er läuft als automatischer Windows-Dienst **LinadAgent** unter **LocalSystem**, ohne angemeldeten Benutzer. Der Server muss diese Agent-Version unterstützen; zuerst den Server aktualisieren.

Unterstützt werden Inventarabfragen (`system-info`), geplante oder sofortige Windows-Updates (`windows-update`) sowie eigene PowerShell-Skripte (`custom-script`, Interpreter `powershell`). Benutzeranmeldung, Windows-Domänenbeitritt, lokale Kontensynchronisierung und Desktop-Fernsteuerung/RDP sind keine Bestandteile dieses Agenten. Die vorhandenen Windows-GUI/CLI-Clients bleiben die Verwaltungsoberfläche; für Gerätejobs wird zusätzlich dieser Agent installiert.

## Einrichtung

1. Im Webinterface als Administrator ein Gerät mit Betriebssystem **Windows** registrieren.
2. **Windows-Agent einrichten** auswählen; Geräte-ID und einmaliger Enrollment-Token werden angezeigt.
3. `install-agent-windows.cmd` aus dem aktuellen GitHub Release herunterladen und **Als Administrator ausführen**. Alternativ im Administrator-Terminal mit vorbelegter Adresse/ID:

```powershell
.\install-agent-windows.cmd -Server https://directory.example -DeviceID DEVICE_ID
```

Der Release-Starter lädt das Agent-Paket und prüft dessen SHA-256. Bei privaten Repositories muss `GH_TOKEN` mit Repository-Leserechten im Terminal verfügbar sein. Der Installer fragt fehlende Serveradresse und Geräte-ID ab. Der Starter setzt nur für seinen PowerShell-Prozess `RemoteSigned`; er ändert keine dauerhafte Ausführungsrichtlinie und überschreibt keine Gruppenrichtlinien. Bei direkter Ausführung einer im Browser geladenen PS1 können lokale Richtlinien zusätzlich eine explizite Freigabe der vertrauenswürdigen Datei verlangen.

Offline/mit bereits geladenem Paket `linad-windows-agent-amd64.zip`: vollständig entpacken und aus dem Paketordner starten:

```powershell
powershell -NoProfile -File .\scripts\install-windows-agent.ps1 -Server https://directory.example -DeviceID DEVICE_ID
```

Der Token wird nur in der verdeckten Terminal-Abfrage eingegeben. Er wird weder als Kommandozeilenparameter noch in der Konfiguration gespeichert. Nach der Einlösung speichert der Agent einen eigenen Geräteschlüssel. Für eine private Zertifizierungsstelle kann `-CaFile C:\Pfad\ca.pem` angegeben werden; TLS-/Hostnamenprüfung bleiben aktiv. Der Installer kopiert die CA in den geschützten Zustandsordner. HTTP nur ausdrücklich über `-AllowHttp`; dabei werden Geräteschlüssel und Ergebnisse unverschlüsselt übertragen.

`-Check` prüft Voraussetzungen und Serververbindung ohne Änderungen oder Token-Einlösung; bei bestehenden Installationen wird zusätzlich der gespeicherte Geräteschlüssel geprüft. `-NoStart` installiert den Dienst, startet ihn aber noch nicht. Lokale oder unternehmensweite PowerShell-Richtlinien werden nicht umgangen; insbesondere können GPO-Signaturanforderungen eigene Skripte blockieren.

## Betrieb und Aktualisierung

```powershell
Get-Service LinadAgent
& "$env:ProgramFiles\LinadAgent\linad-windows-agent.exe" doctor
Get-Content "$env:ProgramData\Linad\agent\agent.log" -Tail 100
```

Das EXE liegt unter `%ProgramFiles%\LinadAgent`; Gerätekonfiguration (`client.json`), dauerhaftes Jobjournal (`jobs.json`) und auf zweimal 5 MiB begrenztes Protokoll liegen unter `%ProgramData%\Linad\agent`. Verzeichnisse und sensible Dateien sind ausschließlich für SYSTEM/Administratoren freigegeben. Reparse-Punkte sowie unsichere Datei-ACLs/Owner werden abgelehnt. Zugangsdaten werden nicht in die Umgebung eines Skripts übernommen.

Für Updates denselben Release-Aufruf oder das neue entpackte Paket erneut starten. Vorhandene Konfiguration wird weiterverwendet; kein neuer Token nötig. Beim Stoppen wartet der Dienst einen bereits akzeptierten Job bis zu seinem 30-Minuten-Limit ab. Deshalb kann ein Update bis zu etwa 30 Minuten dauern. Ein erzwungenes Beenden, Reboot oder Stromausfall kann einen unklaren Lauf erzeugen.

Der Dienst fragt alle 15 Sekunden nach Jobs und läuft auch nach Ab-/Anmeldung oder Neustart. Die Windows-Dienstwiederherstellung startet ihn nach einem Prozessfehler neu. Ein verlorener Abruf verwendet dieselbe Anfrage-ID. Ergebnisse werden dauerhaft gespeichert und bei Verbindungsfehlern erneut übertragen, ohne den Auftrag erneut auszuführen. Ein unterbrochener oder abgelaufener Lauf wird als **Unklar** gemeldet und bis zur Administratorprüfung nicht wiederholt.

## Jobs

**Eigene Skripte:** In Automatisierung „Eigenes Skript“, Interpreter **PowerShell (Windows)** wählen. Nur Windows-Geräte werden angeboten. Skripte laufen mit vollen SYSTEM-Rechten und können das gesamte Gerät verändern. Maximale Quelle: 16 KiB; Ausgabe begrenzt; Laufzeit maximal 30 Minuten. PowerShell 5.1 wird aus dem Windows-Systemverzeichnis verwendet, ohne Profile, mit UTF-8-Ausgabe und anfänglichem `$ErrorActionPreference = 'Stop'`. Das Skript kann diese Fehlerbehandlung selbst ändern; mit `exit N` einen expliziten Exit-Code melden. Native Windows-Exitcodes außerhalb des Serverbereichs −1…255 werden in der Ausgabe erhalten und als Fehlercode 1 gemeldet. Die Variablen `LINAD_JOB_ID`, `LINAD_RUN_ID` und `LINAD_DEVICE_ID` sind verfügbar. Verzeichnis und Skriptdatei werden nach dem Lauf entfernt.

Jeder Lauf behält einen unveränderlichen Quelltext-Snapshot und Inhalts-Hash. Änderungen wirken nur auf neu eingereihte Läufe. Ein Windows Job Object bindet den gestarteten PowerShell-Prozess samt Unterprozessen; ein Timeout oder der Verlust des Agent-Prozesses beendet diese Prozessgruppe. Es ist keine Sandbox gegen bösartige SYSTEM-Skripte. Dauerhafte Dienste müssen bewusst als solche eingerichtet werden.

**Windows-Updates:** Der Job verwendet die Windows Update Agent COM-API und die auf dem Gerät eingestellten Updatequellen/WSUS-Richtlinien. Er sucht nicht installierte, nicht ausgeblendete Software-Updates, akzeptiert deren EULAs, lädt sie herunter und installiert sie. Treiber werden nicht gesucht. Er löst keinen automatischen Neustart aus und meldet einen nötigen Neustart in der Ausgabe. Fehler/Teilerfolge werden als fehlgeschlagen gemeldet. Ein Timeout beendet den Linad-Aufrufer, kann jedoch eine bereits an den Windows-Update-Dienst übergebene Operation nicht rückgängig machen. Bei „Unklar“ Gerät/Updatezustand prüfen, bevor weitere Jobs freigegeben werden.

## Schlüsselwechsel und Entfernen

Nach Widerruf des Schlüssels blockiert der Server weitere Abrufe. Bereits gestartete Befehle werden durch den Widerruf nicht beendet. Neuer Token, gleiche Geräte-ID/Serveradresse:

```powershell
powershell -NoProfile -File .\install.ps1 -Component device -Reenroll
```

Entfernen über das entpackte Paket oder den Release-Starter:

```powershell
powershell -NoProfile -File .\scripts\install-windows-agent.ps1 -Uninstall
```

Der Dienst und die installierte EXE werden entfernt; geschützte Konfiguration und Jobjournal bleiben zur Wiederherstellung erhalten. Den Gerätezugang anschließend im Webinterface widerrufen. Keine Journaldateien löschen, um einen ungeklärten Job zu umgehen.

Die Windows-nativen Prozess-, ACL-, Dienst- und Installationstests laufen im Windows-Job des Release-Workflows. Linux kann die Windows-Binärdateien bauen und das Protokoll testen, aber keinen echten Windows-Dienst ausführen.

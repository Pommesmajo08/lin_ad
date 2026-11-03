# Linux-Provisionierung (Debian/Ubuntu)

Der Root-Dienst provisioniert lokal reguläre Unix-Konten und prüft Linad-Passwörter und Gerätefreigaben über PAM online. Er ist für Debian/Ubuntu mit `pam-auth-update`, `useradd`, `groupadd` und systemd vorgesehen. Tests mit echtem PAM laufen in isolierten Debian- und Ubuntu-26.04-Containern (klassisches sudo und sudo-rs 0.2.13); ein realer grafischer Login und distributionsspezifische SSH-Konfigurationen müssen vor dem Rollout auf einem Testsystem geprüft werden.

## Ablauf für den Administrator

1. Server aktualisieren und als Linad-Administrator ein Gerät mit Betriebssystem Linux registrieren.
2. In „Anmeldezugriff“ einzelne Benutzer/Gruppen auswählen oder „Alle aktiven Benutzer“ einschalten. Standardmäßig darf sich niemand anmelden, auch der Linad-Administrator nicht automatisch.
3. „Linux provisionieren“ erzeugt einen einmaligen Token mit 15 Minuten Gültigkeit. Den Token nicht in Tickets/Logs oder Shell-Befehlszeilen ablegen.
4. Das passende Provisioner-Archiv auf dem Linux-Ziel entpacken und als Root installieren. Ein funktionierendes lokales Root-/Administratorkonto und eine offene Root-Sitzung für den ersten Test beibehalten.

```sh
tar -xzf linad-linux-provisioner-amd64.tar.gz
sudo sh scripts/install-linux.sh \
  --server https://directory.example:8443 \
  --device-id DEVICE_ID
```

Der Installer fragt den Enrollment-Token ohne Echo ab. Für eine private CA zusätzlich `--ca-file /pfad/ca.pem` angeben. Das Zertifikat wird nach `/etc/linad-linux/ca.pem` kopiert; TLS- und Hostnamenprüfung bleiben aktiv. HTTP ist mit `--allow-http` auch für entfernte Server erlaubt. Ohne diesen Schalter bleibt HTTPS erforderlich. Bei HTTP werden Passwörter und Geräte-Schlüssel unverschlüsselt übertragen.

Beispiel für einen HTTP-Server im Netzwerk:

```sh
sudo sh scripts/install-linux.sh --server http://192.168.1.10:8080 --device-id DEVICE_ID --allow-http
```

Der Installer legt Konfiguration (`/etc/linad-linux/client.json`, Root:0600), Registry (`/var/lib/linad-linux/accounts.json`, Root:0600), PAM-Profil und systemd-Dienst an. Er synchronisiert zuerst und aktiviert erst danach die PAM-Anbindung über `pam-auth-update`. Bestehende manuell angepasste PAM-Dateien werden nicht mit `--force` überschrieben; in diesem Fall meldet der Installer einen Fehler und den Pfad seiner Sicherung. `--no-pam` installiert zunächst nur Provisionierung und den separaten PAM-Diagnosedienst. `--no-start` ist für Image-Erstellung ohne laufendes systemd gedacht; beim späteren Boot `systemctl enable --now linad-linux` ausführen.

Jeder Linad-Benutzer erhält eine serverseitig stabile, nicht wiederverwendete UID/GID im Bereich 60001–64999. Der Linux-Login enthält einen Namenspräfix und diese UID, z. B. `linad_alice_60002`. Der genaue Name steht im Benutzerprofil und in der Gerätefreigabe. Vor einem Rollout müssen dieser UID-/GID-Bereich und der Namenspräfix `linad_` frei bzw. für Linad reserviert sein; Kollisionen führen zum Abbruch, niemals zur Übernahme vorhandener lokaler Konten.

Nach der ersten Synchronisierung kann man sich mit diesem Linux-Login und dem normalen Linad-Passwort anmelden. Änderungen an den Freigaben werden alle 30 Sekunden provisioniert. Die PAM-Kontoprüfung fragt den Server bei jeder neuen Anmeldung ab, sodass Sperren und Entzug des Gerätezugriffs sofort neue PAM-Anmeldungen blockieren.

## Betrieb

```sh
sudo systemctl status linad-linux
sudo journalctl -u linad-linux
sudo /usr/local/libexec/linad-linux sync
```

Der Dienst speichert keine Benutzerpasswörter oder Offline-Passwort-Hashes. Ohne Verbindung zum Server sind neue Anmeldungen verwalteter Konten gesperrt. Normale lokale Konten, insbesondere Root, bleiben unabhängig davon verfügbar. Freigegebene Linad-Administratoren erhalten über eine eigene sudo-Regel Root-Rechte mit ihrem Linad-Passwort. Normale Benutzer erhalten keine sudo-Rechte. Homeverzeichnisse werden privat mit Modus 0700 angelegt. Gelöschte/gesperrte bzw. nicht mehr freigegebene Konten werden lokal abgelaufen gesetzt, nicht gelöscht; ihre Daten bleiben erhalten. Bereits laufende Sitzungen werden nicht beendet.

Es werden nur PAM-basierte Dienste erfasst, die das aktivierte gemeinsame PAM-Profil verwenden. Der Installer schaltet SSH-Passwortanmeldung nicht automatisch ein. Für SSH müssen `UsePAM yes` und die gewünschte Passwort-/Keyboard-Interactive-Methode in der vorhandenen SSH-Konfiguration aktiviert sein. `sshd -T` kontrollieren. SSH-Schlüssel können bei erlaubtem Konto weiter funktionieren; die PAM-Kontophase muss auch dann aktiv bleiben. Nicht-PAM-basierte Dienste werden von dieser Integration nicht kontrolliert. Ein leerer Name in einer grafischen Benutzerliste ist möglich: dann den vollständigen Linux-Login manuell eingeben.

Zur Diagnose mit dem optionalen Paket `pamtester`:

```sh
sudo pamtester linad LINUX_LOGIN authenticate acct_mgmt
```

Die bestehenden PAM-Passwortänderungsdialoge sind nicht an Linad angebunden; Passwortänderungen erfolgen über Webverwaltung oder Linad-Client.

## Geräte-Schlüssel erneuern oder widerrufen

„Gerätezugang widerrufen“ entzieht dem installierten Dienst sofort seinen Schlüssel. Auch alle noch offenen Enrollment-Tokens werden ungültig. Die Kontoprüfung lehnt weitere verwaltete Anmeldungen ab. Ein neuer Enrollment-Token kann denselben Dienst wieder anbinden:

```sh
sudo /usr/local/libexec/linad-linux enroll \
  --server https://directory.example:8443 --device-id DEVICE_ID \
  --replace --ca-file /etc/linad-linux/ca.pem
sudo systemctl restart linad-linux
```

Die Ersetzung ist nur für dieselbe Serveradresse und Geräte-ID erlaubt. Nach erfolgreicher Token-Einlösung wird der Token serverseitig gelöscht und der vorherige Geräte-Schlüssel ungültig. Das lokale Speichern muss erfolgreich sein; bei einem Speicherfehler einen neuen Token anfordern, nicht einen bereits eingelösten wiederverwenden.

## Deaktivierung und Wiederherstellung

```sh
sudo sh scripts/uninstall-linux.sh
```

Dadurch wird der Dienst gestoppt, verwaltete Konten werden abgelaufen gesetzt und das gemeinsame PAM-Profil wird deaktiviert. Homeverzeichnisse, Registry, Konfiguration und Binärdatei bleiben zur Wiederherstellung erhalten. Soll nur die PAM-Integration kurzfristig entfernt werden, als lokaler Root `pam-auth-update --disable linad` ausführen. Sicherungen der gemeinsamen PAM-Dateien liegen unter `/var/lib/linad-linux/pam-backup-*`; lokale Anpassungen vor einer manuellen Wiederherstellung prüfen.

## Administratoren und sudo

Der normale Linux-Login behält seine stabile UID. Ein Linad-Administrator, der auf diesem Gerät freigegeben und synchronisiert ist, kann Root-Befehle oder eine Root-Shell starten:

```sh
sudo id -u   # Ausgabe: 0
sudo -i
```

Die Passwortabfrage erwartet das **Linad-Passwort**, nicht ein lokales Root-Passwort. Die Regel verwendet weder `NOPASSWD` noch eine dauerhafte UID 0. Voraussetzungen sind das Paket `sudo`, `visudo` und die übliche Einbindung von `/etc/sudoers.d` in `/etc/sudoers`.

Der Dienst verwaltet ausschließlich `/etc/sudoers.d/linad-linux` (Root:0440). Vor dem atomaren Austausch wird die Syntax mit `visudo` geprüft. Fremde sudo-Regeln und lokale Gruppen werden nicht geändert. Eine gleichnamige, nicht vom Dienst verwaltete Datei führt zum Abbruch statt zum Überschreiben.

Die Rollenprüfung ist direkt in den PAM-Stacks `sudo` und `sudo-i` verankert. Sie benötigt keine sudoers-Optionen `pam_service`, `pam_login_service` oder `authenticate`, die sudo-rs nicht unterstützt. Die portable `PASSWD`-Regel erzwingt die Passwortabfrage; `runaspw` wird nur bei bestätigter Unterstützung deaktiviert. Jeder neue sudo-Aufruf fordert eine Authentifizierung und prüft die aktuelle Administratorrolle, Kontensperre, Gerätefreigabe und Geräteidentität online. Ein Rollenentzug blockiert neue von Linad autorisierte sudo-Aufrufe bereits vor dem nächsten Sync. Ein Rollenaufstieg benötigt zunächst einen Sync, damit die lokale Regel angelegt wird. Bereits gestartete Root-Shells und Prozesse bleiben bestehen. Unabhängig von Linad manuell vergebene lokale sudo-Rechte bleiben in der Verantwortung des lokalen Administrators.

### Bestehendes Gerät aktualisieren

Zuerst den Server aktualisieren. Anschließend das neue Provisioner-Paket auf dem Linux-Gerät entpacken und als lokaler Root/Administrator ausführen:

```sh
sudo apt-get install sudo
sudo sh scripts/install-linux.sh
```

Vorhandene Gerätekonfiguration und Schlüssel werden wiederverwendet; kein neuer Enrollment-Token ist nötig. Der Installer ergänzt die markierten sudo-PAM-Prüfblöcke, synchronisiert und startet den Dienst neu. Die ursprünglichen PAM-Einträge bleiben erhalten und werden bei der Deinstallation wieder freigelegt; Sicherungen liegen unter `/var/lib/linad-linux/sudo-pam-backup`. Lokale sudo-Konten bleiben unabhängig von Linad verfügbar. Bereits erzeugte Linad-Regeln mit inkompatiblen Optionen werden dabei ersetzt. Ein Austausch nur der Binärdatei reicht für dieses Update nicht aus.

### sudo-rs auf Ubuntu 26.04

Für sudo-rs ist kein Wechsel auf klassisches sudo erforderlich. Die aktuelle Implementierung verwendet dessen unterstützte sudoers-Syntax und die normalen PAM-Service-Dateien. Wenn ein älterer Installer mit `unknown setting: pam_service` oder `authenticate` abgebrochen ist, das korrigierte Provisioner-Paket entpacken und als bisheriger lokaler Administrator/Root `sh scripts/install-linux.sh` erneut ausführen. Gerätekonfiguration und Enrollment bleiben erhalten.

Verschwindet die erforderliche PAM-Anbindung später durch eine lokale Änderung, entfernt der nächste Sync die von Linad verwaltete sudo-Freigabe und fordert eine erneute Installer-Ausführung. Die gemeinsame PAM-Kontoprüfung kontrolliert bekannte sudo-Service-Namen zusätzlich. Unabhängige lokale sudo-Regeln werden nicht verändert.

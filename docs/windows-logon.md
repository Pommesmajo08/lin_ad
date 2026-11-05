# Windows-Anmeldung mit Linad

Die optionale Anmeldekomponente ergänzt eine **Linad-Anmeldekachel** für die interaktive Windows-Anmeldung und das Entsperren. Der Credential Provider läuft im Windows-Anmeldeprozess; der vorhandene SYSTEM-Dienst prüft Passwort, Kontensperre und Gerätefreigabe online am Linad-Server. Bestehende lokale Anmeldeanbieter werden weder entfernt noch gefiltert.

Die Komponente ist opt-in: Vor einem produktiven Rollout auf einer Windows-Test-VM den tatsächlichen Anmeldebildschirm, Entsperren, Profile und die lokale Wiederherstellung prüfen. Linux kann DLL und Agent crosskompilieren, aber keine echte LogonUI-Sitzung ausführen. Automatisierte Windows-Tests prüfen COM, Online-Broker, SAM-Anmeldung, Profilanlage, Sperren und Wiederherstellung. Sie ersetzen diesen interaktiven VM-Test nicht.

## Einrichtung

1. Server und Windows-Agent aktualisieren.
2. Als Administrator im Webinterface unter **Geräte → Anmeldezugriff** Benutzer/Gruppen für das Windows-Gerät freigeben. Auch Linad-Administratoren benötigen eine Gerätefreigabe; ohne Freigabe wird abgelehnt.
3. Windows-Agent aus dem vollständigen Release-Paket in einer **64-Bit-Administrator-PowerShell** mit zusätzlichem Schalter installieren:

```powershell
.\install-agent-windows.cmd -Server https://directory.example -DeviceID DEVICE_ID -EnableLogon
```

Bei bereits angebundenem Gerät reicht derselbe Installer mit `-EnableLogon`; Konfiguration und Geräteschlüssel werden beibehalten. Für das entpackte Paket:

```powershell
powershell -NoProfile -ExecutionPolicy RemoteSigned -File .\scripts\install-windows-agent.ps1 -EnableLogon
```

Der Installer prüft Server-API, lokale Namenskollisionen und die Ladbarkeit der x64-DLL. Er registriert ausschließlich Linads eigene Provider-/COM-CLSID `{1D066A31-71D4-45F4-9128-37A36B1A4256}`. Der Dienst aktiviert den Anmeldebroker über die geschützte Datei `logon.enabled`. Die DLL wird mit einem Inhalts-Hash im Dateinamen installiert; ein Update überschreibt keine von LogonUI geladene DLL.

In der Webverwaltung lässt sich beim Kopieren des Windows-Einrichtungsbefehls **Windows-Anmeldung mit Linad aktivieren** auswählen. Ohne diesen Schalter installiert der Agent weiterhin nur Gerätejobs. Ein vorhandener aktiver Anmeldedienst bleibt bei Updates aktiv.

Lokale Ausführungs-, Code-Integrity- und Unternehmensrichtlinien bleiben wirksam. Produktive Bereitstellungen müssen deren Anforderungen an Signaturen erfüllen. Die lokalen Builds sind nicht mit einem Unternehmenszertifikat signiert.

## Anmeldung und Konten

Am Anmeldebildschirm **Linad** unter **Anmeldeoptionen** beziehungsweise **Anderer Benutzer** wählen. Eingeben: normaler Linad-Benutzername, zum Beispiel `alice`, und das aktuelle Linad-Passwort. Die Sichtbarkeit/Anordnung der Kachel kann je nach Windows-Version und Anmelderichtlinie variieren und ist Teil des VM-Tests.

Der Dienst legt ausschließlich verwaltete lokale Konten namens `linad_<stabile-ID>` an, zum Beispiel `linad_60002`. Die lokale SID wird geschützt protokolliert; dadurch bleiben Windows-Profil und Dateien bei folgenden Anmeldungen erhalten. Vorhandene gleichnamige Konten oder geänderte SID-/Besitzmarkierungen werden nicht übernommen. Registry: `%ProgramData%\Linad\agent\windows-accounts.json`. Konten nicht manuell umbenennen, löschen oder auf einen anderen Server übertragen.

Windows erhält nach erfolgreicher Online-Prüfung ein zufälliges, kurzlebiges **lokales** Passwort. Das Linad-Passwort wird nicht als SAM-Passwort gesetzt und nicht als Offline-Passwortkopie gespeichert. Ein Konto wird erst für den Anmeldeversuch freigegeben. Nach der Windows-Rückmeldung oder spätestens nach dem kurzen Freigabezeitraum wird das temporäre Passwort ersetzt und das Konto wieder gesperrt. Laufende Sitzungen bleiben davon unberührt. Ohne erreichbaren Linad-Server wird die Linad-Anmeldung abgelehnt.

Zwischen Credential Provider und Dienst wird eine lokale Named Pipe mit SYSTEM-ACL und Ablehnung entfernter Clients verwendet. Vor Übertragung des Passworts prüft der Provider die SYSTEM-Identität des Servers. Die Kommunikation zum Verzeichnis verwendet die Agent-TLS-Konfiguration und den gerätebezogenen Schlüssel. Passwörter und temporäre lokale Geheimnisse werden nicht in das Kontenjournal oder Protokoll geschrieben.

Freigegebene Standardbenutzer sind Mitglieder der lokalen Benutzergruppe; Linad-Administratoren erhalten zusätzlich die lokale Administratorgruppe. Gruppen werden über bekannte SIDs angesprochen und funktionieren auch auf lokalisiertem Windows. Rollenänderungen gelten für neue Windows-Anmeldetokens. Bestehende Administrator-Sitzungen verlieren ihren bereits ausgestellten Token nicht automatisch; nach Rollenentzug abmelden.

Passwörter über Linad-Webverwaltung/Client ändern. Die normalen Windows-Passwortänderungsdialoge verwalten das lokale SAM-Passwort und sind nicht an Linad angebunden. RDP mit NLA, Windows Hello/PIN, Domänenbeitritt und UAC-Dialoge mit erneuter Passworteingabe sind nicht implementiert; diese werden durch die Anmeldekomponente nicht als Linad-Anmeldung angeboten. Lokale Windows-Konten verwenden weiterhin ihre bisherigen Anbieter.

## Deaktivierung und Wiederherstellung

Eine lokale Administratorsitzung für den ersten Test offen halten. Anmeldung separat abschalten, während Gerätejobs erhalten bleiben:

```powershell
powershell -NoProfile -ExecutionPolicy RemoteSigned -File .\scripts\install-windows-agent.ps1 -DisableLogon
```

Das installierte Wiederherstellungsskript funktioniert auch ohne Serververbindung und ohne gültigen Geräteschlüssel:

```powershell
powershell -NoProfile -ExecutionPolicy RemoteSigned -File "$env:ProgramFiles\LinadAgent\recover-windows-logon.ps1"
```

Es entfernt nur Linads Registrierung, stoppt den Anmeldebroker, sperrt die verwalteten Konten und startet den Agenten für Gerätejobs neu. Profile und Konten werden nicht gelöscht. Bereits geladene Provider können bis zum nächsten Anmeldebildschirm/Neustart im Speicher bleiben; alte DLL-Dateien bleiben für diesen Fall erhalten. Die Agent-Deinstallation führt denselben Anmelde-Rückbau aus.

Falls der Anmeldebildschirm selbst nicht nutzbar ist, über ein unabhängiges lokales Administratorkonto beziehungsweise Windows-Wiederherstellung starten. In WinRE kann Linads Registrierung aus der **Offline-SOFTWARE-Hive** entfernt werden. Laufwerksbuchstaben zuerst prüfen; folgendes Beispiel nimmt Windows auf `D:` an:

```cmd
reg load HKLM\LinadRecovery D:\Windows\System32\Config\SOFTWARE
reg delete "HKLM\LinadRecovery\Microsoft\Windows\CurrentVersion\Authentication\Credential Providers\{1D066A31-71D4-45F4-9128-37A36B1A4256}" /f
reg delete "HKLM\LinadRecovery\Classes\CLSID\{1D066A31-71D4-45F4-9128-37A36B1A4256}" /f
reg unload HKLM\LinadRecovery
```

Danach normal starten und das Wiederherstellungsskript ausführen, um auch den Broker und die verwalteten Konten zu deaktivieren. Andere Credential Provider oder die gesamte Authentifizierungsregistrierung nicht löschen.

## Bauen und prüfen

Windows mit Visual Studio C++ Build Tools, Windows SDK und CMake:

```powershell
powershell -NoProfile -File .\scripts\build-windows-logon.ps1
```

Unter Linux kann `sh scripts/build-windows-logon.sh` über MinGW oder den isolierten Docker-Compiler DLL und nativen Testprüfer erzeugen. Anschließend `sh scripts/build.sh` für das vollständige Agent-Paket. Release-Pakete werden nur mit vorhandener DLL vorbereitet; CI verpackt die zuvor auf Windows getestete DLL.

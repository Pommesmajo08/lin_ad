@echo off
powershell -NoProfile -File "%~dp0scripts\install-client.ps1" %*
if errorlevel 1 echo Installation fehlgeschlagen. Bitte die Meldung oben beachten. Die GUI-EXE unter dist kann auch direkt gestartet werden.
pause

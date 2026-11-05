@echo off
setlocal
set "LINAD_SETUP_PS=%TEMP%\linad-setup-%RANDOM%-%RANDOM%.ps1"
powershell -NoProfile -Command "$ErrorActionPreference='Stop'; [Net.ServicePointManager]::SecurityProtocol=[Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12; $token=$env:GH_TOKEN; if (-not $token) {$token=$env:GITHUB_TOKEN}; if ($token) { $h=@{Authorization=('Bearer '+$token);Accept='application/vnd.github+json'}; $r=Invoke-RestMethod -Headers $h -Uri 'https://api.github.com/repos/Pommesmajo08/lin_ad/releases/latest'; $a=@($r.assets | Where-Object {$_.name -eq 'install.ps1'}); if ($a.Count -ne 1 -or $a[0].url -notmatch '^https://api\.github\.com/repos/Pommesmajo08/lin_ad/releases/assets/[0-9]+$') {throw 'Installer fehlt im Release'}; $h.Accept='application/octet-stream'; Invoke-WebRequest -UseBasicParsing -Headers $h -Uri $a[0].url -OutFile $env:LINAD_SETUP_PS } else { Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/Pommesmajo08/lin_ad/releases/latest/download/install.ps1' -OutFile $env:LINAD_SETUP_PS }"
if errorlevel 1 goto failed
powershell -NoProfile -ExecutionPolicy RemoteSigned -File "%LINAD_SETUP_PS%" %*
if errorlevel 1 goto failed
del "%LINAD_SETUP_PS%" >nul 2>&1
pause
exit /b 0
:failed
del "%LINAD_SETUP_PS%" >nul 2>&1
echo Download oder Installation fehlgeschlagen. Bitte die Fehlermeldung oben beachten.
pause
exit /b 1

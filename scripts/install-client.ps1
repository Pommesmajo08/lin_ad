# Per-user installer; no administrator rights or system login integration required.
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\Linad'),
    [switch]$Uninstall
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$menu = Join-Path ([Environment]::GetFolderPath('Programs')) 'Linad.lnk'
if ($Uninstall) {
    foreach ($name in @('linad-client.exe', 'linad-client-gui.exe')) {
        $path = Join-Path $InstallDir $name
        if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path }
    }
    if (Test-Path -LiteralPath $menu) { Remove-Item -LiteralPath $menu }
    Write-Host 'Linad entfernt. Server- und Gerätekonfiguration bleiben erhalten.'
    exit
}
$files = @{
    'linad-client-windows-amd64.exe' = 'linad-client.exe'
    'linad-client-gui-windows-amd64.exe' = 'linad-client-gui.exe'
}
foreach ($name in $files.Keys) {
    if (-not (Test-Path -LiteralPath (Join-Path $root "dist\$name") -PathType Leaf)) {
        throw "Paket unvollständig: dist\$name fehlt. ZIP-Datei zuerst vollständig entpacken."
    }
}
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
foreach ($name in $files.Keys) {
    try {
        Copy-Item -LiteralPath (Join-Path $root "dist\$name") -Destination (Join-Path $InstallDir $files[$name]) -Force
    } catch {
        throw "Client konnte nicht aktualisiert werden. Laufenden Linad-Client über 'Client beenden' schließen und erneut versuchen. $($_.Exception.Message)"
    }
}
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $menu) | Out-Null
$shell = New-Object -ComObject WScript.Shell
$link = $shell.CreateShortcut($menu)
$link.TargetPath = Join-Path $InstallDir 'linad-client-gui.exe'
$link.WorkingDirectory = $InstallDir
$link.Description = 'Linad Verzeichnisverwaltung'
$link.Save()
Write-Host "Linad installiert: $InstallDir"
Write-Host 'Im Startmenü „Linad“ öffnen und Serveradresse eingeben.'
Write-Host 'Für Updates zuerst den laufenden GUI-Client über „Client beenden“ schließen.'

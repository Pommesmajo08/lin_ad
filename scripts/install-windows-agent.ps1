#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$Server,
    [string]$DeviceID,
    [string]$CaFile,
    [switch]$AllowHttp,
    [switch]$NoStart,
    [switch]$Check,
    [switch]$Reenroll,
    [switch]$Uninstall,
    [string]$StateDir = (Join-Path $env:ProgramData 'Linad\agent'),
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'LinadAgent')
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$source = Join-Path $root 'dist\linad-windows-agent-amd64.exe'
$StateDir = [IO.Path]::GetFullPath($StateDir)
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$target = Join-Path $InstallDir 'linad-windows-agent.exe'
$config = Join-Path $StateDir 'client.json'
function Invoke-Agent([string[]]$Arguments) {
    & $source @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'Agent-Prüfung/Einrichtung fehlgeschlagen. Fehlermeldung oben beachten.' }
}
function Assert-NoReparse([string]$Path) {
    $part = [IO.Path]::GetFullPath($Path)
    while ($part) {
        if (Test-Path -LiteralPath $part) {
            if ((Get-Item -LiteralPath $part -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Reparse-Punkt nicht zulässig: $part" }
        }
        $parent = Split-Path -Parent $part
        if ($parent -eq $part) { break }
        $part = $parent
    }
}
function Set-PrivateDirectory([string]$Path) {
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
    $acl = New-Object Security.AccessControl.DirectorySecurity
    $acl.SetOwner((New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')))
    $acl.SetAccessRuleProtection($true,$false)
    foreach ($sid in @('S-1-5-18','S-1-5-32-544')) {
        $identity = New-Object Security.Principal.SecurityIdentifier($sid)
        $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity,'FullControl','ContainerInherit,ObjectInherit','None','Allow')
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
}
Assert-NoReparse $StateDir
Assert-NoReparse $InstallDir
$service = Get-Service -Name LinadAgent -ErrorAction SilentlyContinue
if ($service) {
    $definition = Get-CimInstance Win32_Service -Filter "Name='LinadAgent'"
    if (-not $definition.PathName.StartsWith(('"'+$target+'" service '),[StringComparison]::OrdinalIgnoreCase) -or $definition.StartName -ne 'LocalSystem') { throw 'Vorhandener Dienst LinadAgent gehört nicht zu dieser Installation.' }
}
function Stop-Agent {
    $running = Get-Service -Name LinadAgent -ErrorAction SilentlyContinue
    if ($running -and $running.Status -ne 'Stopped') {
        Write-Host 'Dienst stoppen; ein akzeptierter Job wird abgewartet (bis zu 30 Minuten).'
        $serviceProcessID = (Get-CimInstance Win32_Service -Filter "Name='LinadAgent'").ProcessId
        $running.Stop()
        $running.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped,[TimeSpan]::FromMinutes(35))
        if ($serviceProcessID) {
            $process = Get-Process -Id $serviceProcessID -ErrorAction SilentlyContinue
            if ($process -and -not $process.WaitForExit(30000)) { throw 'Dienst meldet gestoppt, Prozess läuft noch. Update nicht erzwungen.' }
            if ($process) { $process.Dispose() }
        }
        $running.Dispose()
    }
}
if ($Uninstall) {
    Stop-Agent
    if ($service) { & sc.exe delete LinadAgent | Out-Null; if ($LASTEXITCODE) { throw 'Dienst konnte nicht gelöscht werden.' }; $service.Dispose() }
    if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target }
    Write-Host 'Windows-Agent entfernt. Geschützte Konfiguration und Jobjournal bleiben erhalten. Geräteschlüssel im Webinterface widerrufen.'
    return
}
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -and $env:PROCESSOR_ARCHITEW6432 -ne 'AMD64') { throw 'Windows AMD64 erforderlich.' }
if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw 'Agent fehlt. Windows-Agent-Paket vollständig entpacken.' }
if (Test-Path -LiteralPath $config) {
    if (-not $Reenroll) { Invoke-Agent @('doctor','--state-dir',$StateDir) }
} else {
    if (-not $Server) { $Server=Read-Host 'Linad-Serveradresse (https://...)' }
    if (-not $DeviceID) { $DeviceID=Read-Host 'Windows-Geräte-ID aus der Webverwaltung' }
    if (-not $Server -or -not $DeviceID) { throw 'Serveradresse und Geräte-ID sind erforderlich.' }
    $uri = [Uri]$Server
    if (($uri.Scheme -ne 'https' -and -not ($uri.Scheme -eq 'http' -and $AllowHttp)) -or $DeviceID -notmatch '^[a-f0-9]{32}$') { throw 'HTTPS-Adresse und gültige Geräte-ID erforderlich; HTTP nur mit -AllowHttp.' }
    $connection = @('check-connection','--server',$Server,'--device-id',$DeviceID)
    if ($CaFile) { $connection += @('--ca-file',$CaFile) }
    if ($AllowHttp) { $connection += '--allow-http' }
    Invoke-Agent $connection
}
if ($Check) { Write-Host 'Vorabprüfung erfolgreich. Keine Änderungen vorgenommen.'; return }
$marker = Join-Path $StateDir 'linad-agent.marker'
if ((Test-Path -LiteralPath $StateDir) -and -not (Test-Path -LiteralPath $marker) -and @(Get-ChildItem -LiteralPath $StateDir -Force).Count -gt 0) { throw 'Zustandsordner enthält fremde Dateien. Eigenen leeren Ordner verwenden.' }
if ((Test-Path -LiteralPath $InstallDir) -and -not (Test-Path -LiteralPath $target) -and @(Get-ChildItem -LiteralPath $InstallDir -Force).Count -gt 0) { throw 'Installationsordner enthält fremde Dateien.' }
if ($StateDir -eq (Join-Path $env:ProgramData 'Linad\agent')) { Set-PrivateDirectory (Split-Path -Parent $StateDir) }
Set-PrivateDirectory $StateDir
Set-PrivateDirectory $InstallDir
Set-Content -LiteralPath $marker -Value 'Managed by Linad Windows Agent'
if (-not (Test-Path -LiteralPath $config) -or $Reenroll) {
    if ($Reenroll) { Stop-Agent }
    $arguments = @('enroll','--state-dir',$StateDir)
    if ($Server) { $arguments += @('--server',$Server) }
    if ($DeviceID) { $arguments += @('--device-id',$DeviceID) }
    if ($CaFile) {
        $caTarget = Join-Path $StateDir 'ca.pem'
        if ([IO.Path]::GetFullPath($CaFile) -ne $caTarget) { Copy-Item -LiteralPath $CaFile -Destination $caTarget -Force }
        $arguments += @('--ca-file',$caTarget)
    }
    if ($AllowHttp) { $arguments += '--allow-http' }
    if ($Reenroll) { $arguments += '--replace' }
    Invoke-Agent $arguments
}
Invoke-Agent @('doctor','--state-dir',$StateDir)
Stop-Agent
Copy-Item -LiteralPath $source -Destination $target -Force
if (-not $service) {
    $command = '"'+$target+'" service --state-dir "'+$StateDir+'"'
    New-Service -Name LinadAgent -BinaryPathName $command -DisplayName 'Linad Windows-Geräteverwaltung' -Description 'Holt zentrale Gerätejobs ab und führt autorisierte PowerShell-Aufträge aus.' -StartupType Automatic | Out-Null
}
& sc.exe failure LinadAgent reset= 86400 actions= restart/15000/restart/30000/restart/60000 | Out-Null
if ($LASTEXITCODE) { throw 'Dienst-Wiederanlauf konnte nicht konfiguriert werden.' }
if (-not $NoStart) { Start-Service LinadAgent; (Get-Service LinadAgent).WaitForStatus([ServiceProcess.ServiceControllerStatus]::Running,[TimeSpan]::FromSeconds(30)) }
Write-Host "Windows-Agent installiert. Diagnose: & `"$target`" doctor"
Write-Host "Protokoll: $StateDir\agent.log"
Write-Host 'Jobs laufen als SYSTEM. Windows-Anmeldung und lokale Benutzerkonten werden nicht verändert.'

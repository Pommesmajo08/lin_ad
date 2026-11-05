#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$Server,
    [string]$DeviceID,
    [string]$CaFile,
    [switch]$AllowHttp = $true,
    [switch]$NoStart,
    [switch]$Check,
    [switch]$Reenroll,
    [switch]$EnableLogon,
    [switch]$DisableLogon,
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
$logonMarker=Join-Path $StateDir 'logon.enabled'
$logonRequested=$EnableLogon -or (Test-Path -LiteralPath $logonMarker)
if($EnableLogon -and $DisableLogon){throw '-EnableLogon und -DisableLogon schließen sich aus.'}
if($DisableLogon){$logonRequested=$false}
$providerSource=Join-Path $root 'dist\linad-credential-provider.dll'
$providerGuid='{1D066A31-71D4-45F4-9128-37A36B1A4256}'
$providerKey="HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Authentication\Credential Providers\$providerGuid"
$clsidKey="HKLM:\SOFTWARE\Classes\CLSID\$providerGuid"
if($logonRequested -and -not $Uninstall -and -not (Test-Path -LiteralPath $providerSource -PathType Leaf)){throw 'Credential-Provider-DLL fehlt. Vollständiges Windows-Agent-Paket mit Anmeldekomponente verwenden.'}
if($logonRequested -and -not $Uninstall){
    if(-not [Environment]::Is64BitProcess){throw 'Für die Windows-Anmeldung eine 64-Bit-PowerShell verwenden.'}
    if(-not ('Linad.LogonLibraryLoader' -as [type])){
        Add-Type @'
using System;
using System.Runtime.InteropServices;
namespace Linad { public static class LogonLibraryLoader {
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] public static extern IntPtr LoadLibraryW(string path);
 [DllImport("kernel32.dll",CharSet=CharSet.Ansi,SetLastError=true)] public static extern IntPtr GetProcAddress(IntPtr module,string name);
 [DllImport("kernel32.dll")] public static extern bool FreeLibrary(IntPtr module);
 [DllImport("kernel32.dll")] public static extern IntPtr GetCurrentProcess();
 [DllImport("kernel32.dll",SetLastError=true)] public static extern bool IsWow64Process2(IntPtr process,out ushort processMachine,out ushort nativeMachine);
}}
'@
    }
    [ushort]$processMachine=0;[ushort]$nativeMachine=0
    if(-not [Linad.LogonLibraryLoader]::IsWow64Process2([Linad.LogonLibraryLoader]::GetCurrentProcess(),[ref]$processMachine,[ref]$nativeMachine) -or $nativeMachine -ne 0x8664){throw 'Die Anmeldekomponente benötigt natives Windows AMD64, keine ARM64-/x64-Emulation.'}
    $loaded=[Linad.LogonLibraryLoader]::LoadLibraryW($providerSource)
    if($loaded -eq [IntPtr]::Zero){throw 'Credential Provider kann nicht geladen werden. Architektur, Abhängigkeiten oder Code-Integrity-Richtlinie prüfen.'}
    try{if([Linad.LogonLibraryLoader]::GetProcAddress($loaded,'DllGetClassObject') -eq [IntPtr]::Zero){throw 'Ungültige Credential-Provider-DLL.'}}
    finally{[void][Linad.LogonLibraryLoader]::FreeLibrary($loaded)}
}
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
    }
    if($running){$running.Dispose()}
}
if ($Uninstall -or $DisableLogon) {
    $recovery=Join-Path $PSScriptRoot 'recover-windows-logon.ps1'
    if(-not (Test-Path -LiteralPath $recovery)){ $recovery=Join-Path $InstallDir 'recover-windows-logon.ps1' }
    if(Test-Path -LiteralPath $recovery){ & $recovery -StateDir $StateDir -InstallDir $InstallDir -NoRestart }
    elseif(Test-Path -LiteralPath $logonMarker){throw 'Wiederherstellungsskript fehlt; Anmeldung nicht unkontrolliert entfernen.'}
    if($DisableLogon -and -not $Uninstall){if($service){Start-Service LinadAgent};Write-Host 'Windows-Anmeldung deaktiviert; Gerätejobs bleiben eingerichtet.';return}
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
    if (-not $Server) { $Server=Read-Host 'Linad-Serveradresse (http://... oder https://...)' }
    if (-not $DeviceID) { $DeviceID=Read-Host 'Windows-Geräte-ID aus der Webverwaltung' }
    if (-not $Server -or -not $DeviceID) { throw 'Serveradresse und Geräte-ID sind erforderlich.' }
    $uri = [Uri]$Server
    if (($uri.Scheme -notin @('http','https')) -or $DeviceID -notmatch '^[a-f0-9]{32}$') { throw 'HTTP- oder HTTPS-Adresse und gültige Geräte-ID erforderlich.' }
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
if($logonRequested){Invoke-Agent @('check-logon','--state-dir',$StateDir)}
Stop-Agent
Copy-Item -LiteralPath $source -Destination $target -Force
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'recover-windows-logon.ps1') -Destination (Join-Path $InstallDir 'recover-windows-logon.ps1') -Force
if($logonRequested){
    $sourceHash=(Get-FileHash -LiteralPath $providerSource -Algorithm SHA256).Hash.ToLower()
    $hash=$sourceHash.Substring(0,16)
    $providerTarget=Join-Path $InstallDir ("linad-credential-provider-"+$hash+'.dll')
    if(-not (Test-Path -LiteralPath $providerTarget)){Copy-Item -LiteralPath $providerSource -Destination $providerTarget}
    elseif((Get-FileHash -LiteralPath $providerTarget -Algorithm SHA256).Hash.ToLower() -ne $sourceHash){throw 'Installierte Anmelde-DLL stimmt nicht mit ihrem Inhalts-Hash überein. Keine Registrierung geändert.'}
    if(Test-Path -LiteralPath ($clsidKey+'\InprocServer32')){
        $old=(Get-Item -LiteralPath ($clsidKey+'\InprocServer32')).GetValue('')
        if(-not ([string]$old).StartsWith(($InstallDir+'\linad-credential-provider-'),[StringComparison]::OrdinalIgnoreCase)){throw 'CLSID ist bereits durch eine fremde Registrierung belegt.'}
    }
    New-Item -Path ($clsidKey+'\InprocServer32') -Force | Out-Null
    Set-Item -LiteralPath $clsidKey -Value 'Linad Credential Provider'
    Set-Item -LiteralPath ($clsidKey+'\InprocServer32') -Value $providerTarget
    New-ItemProperty -LiteralPath ($clsidKey+'\InprocServer32') -Name ThreadingModel -Value Apartment -PropertyType String -Force | Out-Null
    New-Item -Path $providerKey -Force | Out-Null
    Set-Item -LiteralPath $providerKey -Value 'Linad'
    Set-Content -LiteralPath $logonMarker -Value 'Managed by Linad Windows Logon'
    $markerAcl=Get-Acl -LiteralPath $logonMarker
    $markerAcl.SetOwner((New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')))
    Set-Acl -LiteralPath $logonMarker -AclObject $markerAcl
    Write-Host 'Zusätzliche Linad-Anmeldekomponente aktiviert. Vor einem Rollout Anmeldung/Entsperren auf einer Test-VM prüfen. Lokale Anmeldung bleibt erhalten.'
}
if (-not $service) {
    $command = '"'+$target+'" service --state-dir "'+$StateDir+'"'
    New-Service -Name LinadAgent -BinaryPathName $command -DisplayName 'Linad Windows-Geräteverwaltung' -Description 'Holt zentrale Gerätejobs ab und führt autorisierte PowerShell-Aufträge aus.' -StartupType Automatic | Out-Null
}
& sc.exe failure LinadAgent reset= 86400 actions= restart/15000/restart/30000/restart/60000 | Out-Null
if ($LASTEXITCODE) { throw 'Dienst-Wiederanlauf konnte nicht konfiguriert werden.' }
if (-not $NoStart) { Start-Service LinadAgent; (Get-Service LinadAgent).WaitForStatus([ServiceProcess.ServiceControllerStatus]::Running,[TimeSpan]::FromSeconds(30)) }
Write-Host "Windows-Agent installiert. Diagnose: & `"$target`" doctor"
Write-Host "Protokoll: $StateDir\agent.log"
if($logonRequested){Write-Host 'Linad-Konten werden online gegen die Gerätefreigaben geprüft. Anmeldung ggf. unter Anderer Benutzer → Linad wählen.'}
else{Write-Host 'Jobs laufen als SYSTEM. Windows-Anmeldung ist nicht aktiviert; dafür -EnableLogon verwenden.'}

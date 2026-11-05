#Requires -RunAsAdministrator
[CmdletBinding()]
param(
 [string]$StateDir=(Join-Path $env:ProgramData 'Linad\agent'),
 [string]$InstallDir=(Join-Path $env:ProgramFiles 'LinadAgent'),
 [switch]$NoRestart
)
$ErrorActionPreference='Stop'
$guid='{1D066A31-71D4-45F4-9128-37A36B1A4256}'
# Only Linad's registration is removed. Built-in providers and Windows policy are untouched.
foreach($key in @("HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Authentication\Credential Providers\$guid","HKLM:\SOFTWARE\Classes\CLSID\$guid")){
 if(Test-Path -LiteralPath $key){Remove-Item -LiteralPath $key -Recurse -Force}
}
$marker=Join-Path $StateDir 'logon.enabled'
if(Test-Path -LiteralPath $marker){Remove-Item -LiteralPath $marker -Force}
$service=Get-Service LinadAgent -ErrorAction SilentlyContinue
if($service -and $service.Status -ne 'Stopped'){
 $processID=(Get-CimInstance Win32_Service -Filter "Name='LinadAgent'").ProcessId
 Write-Host 'Linad-Anmeldung deaktiviert. Laufenden Gerätejob vor Dienstneustart abwarten (maximal 30 Minuten).'
 $service.Stop();$service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped,[TimeSpan]::FromMinutes(35))
 if($processID){$process=Get-Process -Id $processID -ErrorAction SilentlyContinue;if($process -and -not $process.WaitForExit(30000)){throw 'Agent-Prozess läuft noch; keine parallele Kontenänderung vorgenommen.'};if($process){$process.Dispose()}}
}
$agent=Join-Path $InstallDir 'linad-windows-agent.exe'
if(Test-Path -LiteralPath $agent){
 & $agent disable-logon --state-dir $StateDir
 if($LASTEXITCODE){throw 'Verwaltete Konten konnten nicht vollständig gesperrt werden. Registry/Agent-Protokoll prüfen.'}
}
if($service -and -not $NoRestart){Start-Service LinadAgent}
if($service){$service.Dispose()}
Write-Host 'Linad-Anmeldung entfernt. Lokale Windows-Anmeldung bleibt unverändert. Bereits geladene Anmeldeoberfläche ggf. durch Ab-/Anmeldung oder Neustart erneuern.'

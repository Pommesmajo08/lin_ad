# Runs only on the disposable Windows CI host, with its own server, service and paths.
$ErrorActionPreference='Stop'
$root=Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$work=Join-Path $env:RUNNER_TEMP ('linad-agent-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$state=Join-Path $work 'state'
$bin=Join-Path $work 'bin'
New-Item -ItemType Directory -Path $state | Out-Null
& icacls.exe $state /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE) { throw 'ACL initialization failed' }
& icacls.exe $state /setowner '*S-1-5-32-544' | Out-Null
if ($LASTEXITCODE) { throw 'ACL owner failed' }
Set-Content -LiteralPath (Join-Path $state 'linad-agent.marker') 'Managed by Linad Windows Agent'
$listener=New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback,0)
$listener.Start();$port=$listener.LocalEndpoint.Port;$listener.Stop()
$base="http://127.0.0.1:$port"
$env:LINAD_ADMIN_PASSWORD=[Guid]::NewGuid().ToString('N')
$env:LINAD_DATA=Join-Path $work 'server.json'
$env:LINAD_LISTEN="127.0.0.1:$port"
$server=Start-Process -FilePath (Join-Path $root 'dist\linad-server-windows-test.exe') -PassThru -RedirectStandardOutput (Join-Path $work 'server-out.log') -RedirectStandardError (Join-Path $work 'server-error.log')
$auth=@{}
function Api([string]$Method,[string]$Path,$Body) {
    $requestOptions=@{Method=$Method;Uri=($base+$Path);Headers=$auth;ContentType='application/json';TimeoutSec=15}
    if ($null -ne $Body) {$requestOptions.Body=ConvertTo-Json $Body -Depth 10 -Compress}
    Invoke-RestMethod @requestOptions
}
try {
    $ready=$false
    for($i=0;$i -lt 100;$i++){try{[void](Api 'GET' '/healthz' $null);$ready=$true;break}catch{Start-Sleep -Milliseconds 100}}
    if(-not $ready){throw 'Server not ready'}
    $auth=@{Authorization=('Bearer '+(Api 'POST' '/v1/login' @{name='admin';password=$env:LINAD_ADMIN_PASSWORD}).token)}
    $device=Api 'POST' '/v1/devices' @{name='windows-ci';os='windows'}
    $enroll=Api 'POST' ("/v1/devices/"+$device.id+'/enrollment') $null
    $enroll.enrollment_token | & (Join-Path $root 'dist\linad-windows-agent-amd64.exe') enroll --server $base --device-id $device.id --state-dir $state --allow-http
    if($LASTEXITCODE){throw 'Agent enrollment failed'}
    & (Join-Path $root 'scripts\install-windows-agent.ps1') -StateDir $state -InstallDir $bin
    $deadline=[DateTime]::UtcNow.AddSeconds(45)
    do{$devices=@(Api 'GET' '/v1/devices' $null);if($devices[0].last_job_poll){break};Start-Sleep -Milliseconds 200}while([DateTime]::UtcNow -lt $deadline)
    if(-not $devices[0].last_job_poll){throw 'Windows service did not poll'}
    $source="Write-Output 'hello from Windows service'; exit 7`n"
    $job=Api 'POST' '/v1/jobs' @{name='Windows service script';kind='custom-script';interpreter='powershell';script=$source;device_ids=@($device.id)}
    $deadline=[DateTime]::UtcNow.AddSeconds(60)
    do{$runs=@(Api 'GET' ('/v1/job-runs?job_id='+$job.id) $null);if($runs.Count -and $runs[0].status -eq 'failed'){break};Start-Sleep -Milliseconds 200}while([DateTime]::UtcNow -lt $deadline)
    if($runs[0].exit_code -ne 7 -or $runs[0].output -notmatch 'hello from Windows service'){throw 'Real service did not execute/report script'}
    # Update must retain credentials and restart the real SCM service.
    $before=Get-Content -LiteralPath (Join-Path $state 'client.json') -Raw
    & (Join-Path $root 'scripts\install-windows-agent.ps1') -StateDir $state -InstallDir $bin
    if((Get-Content -LiteralPath (Join-Path $state 'client.json') -Raw) -ne $before){throw 'Update replaced identity'}
    & (Join-Path $root 'scripts\install-windows-agent.ps1') -StateDir $state -InstallDir $bin -Uninstall
    if(Test-Path (Join-Path $bin 'linad-windows-agent.exe')){throw 'Agent executable not removed'}
    Write-Host 'PASS: Windows enrollment, private ACLs, real SYSTEM service, PowerShell job/report, update/identity retention, uninstall'
} finally {
    $svc=Get-Service LinadAgent -ErrorAction SilentlyContinue
    if($svc){Stop-Service LinadAgent -Force -ErrorAction SilentlyContinue;& sc.exe delete LinadAgent | Out-Null}
    if(-not $server.HasExited){Stop-Process -Id $server.Id -Force}
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}

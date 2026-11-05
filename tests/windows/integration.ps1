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
    # Exercise the real credential provider/broker/SAM path as SYSTEM, without locking CI.
    $loginPassword='Directory-login-'+[Guid]::NewGuid().ToString('N')+'ä'
    [void](Api 'POST' '/v1/users' @{name='signin-user';password=$loginPassword})
    [void](Api 'PUT' ("/v1/devices/"+$device.id+'/access') @{allowed_users=@('signin-user')})
    $providerRoot='HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Authentication\Credential Providers'
    $originalProviders=@(Get-ChildItem -LiteralPath $providerRoot | ForEach-Object {$_.PSChildName})
    & (Join-Path $root 'scripts\install-windows-agent.ps1') -StateDir $state -InstallDir $bin -EnableLogon
    $providerPath=(Get-Item -LiteralPath 'HKLM:\SOFTWARE\Classes\CLSID\{1D066A31-71D4-45F4-9128-37A36B1A4256}\InprocServer32').GetValue('')
    $probe=Join-Path $state 'linad-logon-probe.exe'
    Copy-Item -LiteralPath (Join-Path $root 'dist\linad-logon-probe.exe') -Destination $probe
    $inputFile=Join-Path $state 'logon-test-input'
    [IO.File]::WriteAllText($inputFile,('signin-user'+"`n"+$loginPassword),(New-Object Text.UTF8Encoding($false)))
    function Probe-Logon([bool]$Expected) {
        $quote={param($value) "'"+$value.Replace("'","''")+"'"}
        $code='$global:LASTEXITCODE=$null; $ErrorActionPreference="Continue"; & '+(& $quote $probe)+' '+(& $quote $providerPath)+' '+(& $quote $inputFile)+'; $probeExit=$LASTEXITCODE; $ErrorActionPreference="Stop"; if($null -eq $probeExit){throw "Native logon probe did not execute synchronously; LASTEXITCODE is unset"}; Write-Output ("Native logon probe exit code: "+$probeExit); exit $probeExit'
        $test=Api 'POST' '/v1/jobs' @{name='Windows logon integration';kind='custom-script';interpreter='powershell';script=$code;device_ids=@($device.id)}
        $until=[DateTime]::UtcNow.AddSeconds(120)
        do{$attempts=@(Api 'GET' ('/v1/job-runs?job_id='+$test.id) $null);if($attempts.Count -and $attempts[0].status -in @('succeeded','failed')){break};Start-Sleep -Milliseconds 200}while([DateTime]::UtcNow -lt $until)
        if(-not $attempts.Count -or $attempts[0].status -notin @('succeeded','failed')){throw 'Native Windows logon probe did not reach a terminal job result within 120 seconds'}
        $result=$attempts[0]
        Write-Host ("Native logon result: status=$($result.status); exit_code=$($result.exit_code)`n$($result.output)")
        if($Expected -and ($attempts[0].status -ne 'succeeded' -or $attempts[0].output -notmatch 'PASS: credential provider')){throw ('Native Windows logon test failed: status='+$result.status+'; exit_code='+$result.exit_code+'; '+$result.output)}
        if(-not $Expected -and ($attempts[0].status -ne 'failed' -or $attempts[0].exit_code -ne 15)){throw 'Revoked sign-in was not denied by the credential provider'}
    }
    Probe-Logon $true
    [void](Api 'PUT' ("/v1/devices/"+$device.id+'/access') @{allowed_users=@()})
    Probe-Logon $false
    Remove-Item -LiteralPath $inputFile -Force
    # Update must retain credentials and restart the real SCM service.
    $before=Get-Content -LiteralPath (Join-Path $state 'client.json') -Raw
    & (Join-Path $root 'scripts\install-windows-agent.ps1') -StateDir $state -InstallDir $bin
    if((Get-Content -LiteralPath (Join-Path $state 'client.json') -Raw) -ne $before){throw 'Update replaced identity'}
    & (Join-Path $root 'scripts\install-windows-agent.ps1') -StateDir $state -InstallDir $bin -Uninstall
    if(Test-Path (Join-Path $bin 'linad-windows-agent.exe')){throw 'Agent executable not removed'}
    $remaining=@(Get-ChildItem -LiteralPath $providerRoot | ForEach-Object {$_.PSChildName})
    if(@(Compare-Object $originalProviders $remaining).Count){throw 'Recovery modified other credential providers'}
    Write-Host 'PASS: Windows enrollment, private ACLs, real SYSTEM service, PowerShell job/report, update/identity retention, uninstall'
} finally {
    $svc=Get-Service LinadAgent -ErrorAction SilentlyContinue
    if($svc){Stop-Service LinadAgent -Force -ErrorAction SilentlyContinue;& sc.exe delete LinadAgent | Out-Null}
    # Delete only accounts/profiles whose SID and ownership nonce belong to this CI fixture.
    $registryFile=Join-Path $state 'windows-accounts.json'
    if(Test-Path -LiteralPath $registryFile){
        $registry=Get-Content -LiteralPath $registryFile -Raw | ConvertFrom-Json
        foreach($property in $registry.accounts.PSObject.Properties){
            $entry=$property.Value
            if($entry.sid){
                $sid=New-Object Security.Principal.SecurityIdentifier($entry.sid)
                $local=Get-LocalUser -SID $sid -ErrorAction SilentlyContinue
                if($local -and $local.Description -eq ('Linad '+$registry.device_id+' '+$entry.reservation)){
                    Remove-LocalUser -SID $sid
                    Get-CimInstance Win32_UserProfile -Filter ("SID='"+$entry.sid+"'") | Remove-CimInstance
                }
            }
        }
    }
    if(-not $server.HasExited){Stop-Process -Id $server.Id -Force}
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}

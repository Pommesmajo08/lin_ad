# Standalone GitHub Releases bootstrapper for Windows AMD64, no admin required.
[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\Linad')
)
$ErrorActionPreference = 'Stop'
$repo = 'https://github.com/Pommesmajo08/lin_ad'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -and $env:PROCESSOR_ARCHITEW6432 -ne 'AMD64') {
    throw 'Dieses Paket benötigt Windows AMD64.'
}
$token = $env:GH_TOKEN
if (-not $token) { $token = $env:GITHUB_TOKEN }
$headers = @{}
$release = $null
if ($token) {
    $headers = @{ Authorization = "Bearer $token"; Accept = 'application/vnd.github+json' }
    if ($Version -eq 'latest') { $endpoint = 'latest' } else {
        if ($Version -notmatch '^[a-zA-Z0-9._-]+$') { throw 'Ungültiger Release-Tag.' }
        $endpoint = "tags/$Version"
    }
    $release = Invoke-RestMethod -Headers $headers -Uri "https://api.github.com/repos/Pommesmajo08/lin_ad/releases/$endpoint" -TimeoutSec 120
    $Version = $release.tag_name
} elseif ($Version -eq 'latest') {
    $response = Invoke-WebRequest -UseBasicParsing -Method Head -Uri "$repo/releases/latest" -MaximumRedirection 10
    $resolved = $response.BaseResponse.ResponseUri
    if (-not $resolved) { $resolved = $response.BaseResponse.RequestMessage.RequestUri }
    if (-not $resolved -or -not $resolved.AbsoluteUri.StartsWith("$repo/releases/tag/")) { throw 'Kein veröffentlichter Release-Tag erreichbar.' }
    $Version = $resolved.Segments[-1]
}
if ($Version -notmatch '^[a-zA-Z0-9._-]+$') { throw 'Ungültiger Release-Tag.' }
$work = Join-Path ([IO.Path]::GetTempPath()) ('linad-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
try {
    $package = 'linad-client-windows-amd64.zip'
    $base = "$repo/releases/download/$Version"
    Write-Host "Linad $Version herunterladen ..."
    if ($release) {
        $headers.Accept = 'application/octet-stream'
        foreach ($name in @('SHA256SUMS', $package)) {
            $asset = @($release.assets | Where-Object { $_.name -eq $name })
            if ($asset.Count -ne 1 -or $asset[0].url -notmatch '^https://api\.github\.com/repos/Pommesmajo08/lin_ad/releases/assets/[0-9]+$') { throw "Release-Asset fehlt: $name" }
            Invoke-WebRequest -UseBasicParsing -Headers $headers -Uri $asset[0].url -OutFile (Join-Path $work $name) -TimeoutSec 900
        }
    } else {
        Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $work 'SHA256SUMS') -TimeoutSec 120
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$package" -OutFile (Join-Path $work $package) -TimeoutSec 900
    }
    $matchesForPackage = @(Get-Content -LiteralPath (Join-Path $work 'SHA256SUMS') | Where-Object { $_ -match ('^[a-fA-F0-9]{64}  ' + [Regex]::Escape($package) + '$') })
    if ($matchesForPackage.Count -ne 1) { throw 'Keine eindeutige SHA-256-Prüfsumme für das Paket.' }
    $expected = $matchesForPackage[0].Substring(0,64)
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $work $package)).Hash
    if ($expected -ne $actual) { throw 'Prüfsumme stimmt nicht überein. Installation abgebrochen.' }
    Write-Host 'Prüfsumme stimmt. Paket entpacken ...'
    Expand-Archive -LiteralPath (Join-Path $work $package) -DestinationPath (Join-Path $work 'package')
    & (Join-Path $work 'package\scripts\install-client.ps1') -InstallDir $InstallDir
    Write-Host "Linad $Version installiert. Im Startmenü Linad öffnen."
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force
}

[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
cmake -S (Join-Path $root 'native\windows-logon') -B (Join-Path $root 'dist\logon-build') -A x64
if($LASTEXITCODE){throw 'Windows SDK/C++ compiler configuration failed'}
cmake --build (Join-Path $root 'dist\logon-build') --config Release
if($LASTEXITCODE){throw 'Credential Provider build failed'}
foreach($name in @('linad-credential-provider.dll','linad-logon-probe.exe')){
 Copy-Item -LiteralPath (Join-Path $root "dist\logon-build\Release\$name") -Destination (Join-Path $root "dist\$name") -Force
}
& (Join-Path $root 'dist\linad-logon-probe.exe') (Join-Path $root 'dist\linad-credential-provider.dll')
if($LASTEXITCODE){throw 'Credential Provider COM smoke test failed'}

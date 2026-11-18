param([Parameter(Mandatory)][string]$Zig)

$ErrorActionPreference = 'Stop'
$zigPath = (Resolve-Path -LiteralPath $Zig).Path
$root = Split-Path -Parent $PSScriptRoot
$output = Join-Path $root 'dist'
$null = New-Item -Path $output -ItemType Directory -Force
$oldEnvironment = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED', 'CC')) {
    $oldEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '1'
$env:CC = '"' + $zigPath.Replace('\', '/') + '" cc -target x86_64-windows-gnu'
Push-Location $root
try {
    go build -trimpath -buildmode=c-shared -o (Join-Path $output 'LoGoTSF.dll') ./tsf
    if ($LASTEXITCODE -ne 0) { throw 'Go TSF DLL build failed.' }
} finally {
    Pop-Location
    foreach ($name in $oldEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $oldEnvironment[$name], 'Process')
    }
}

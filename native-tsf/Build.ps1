param([Parameter(Mandatory)][string]$Zig)
$ErrorActionPreference = 'Stop'
$zigPath = (Resolve-Path -LiteralPath $Zig).Path
$root = Split-Path -Parent $PSScriptRoot
$null = New-Item -ItemType Directory -Path (Join-Path $root 'dist') -Force
$previousCgo = $env:CGO_ENABLED
Push-Location $root
try {
    & $zigPath rc /fo dist/ContextIME.res native-tsf/ContextIME.rc
    if ($LASTEXITCODE -ne 0) { throw 'TSF icon resource build failed.' }
    & $zigPath cc -target x86_64-windows-gnu -O2 -s -shared native-tsf/service.c dist/ContextIME.res -lole32 -loleaut32 -ladvapi32 -luser32 -lgdi32 -luuid -o dist/ContextIMETSF.dll
    if ($LASTEXITCODE -ne 0) { throw 'TSF DLL build failed.' }
    $env:CGO_ENABLED = '0'
    go build -trimpath -ldflags '-H=windowsgui' -o dist/ContextIME.exe ./cmd/contextime
    if ($LASTEXITCODE -ne 0) { throw 'MyGo build failed.' }
} finally {
    $env:CGO_ENABLED = $previousCgo
    Pop-Location
}

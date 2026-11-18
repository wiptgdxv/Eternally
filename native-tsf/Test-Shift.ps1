param([Parameter(Mandatory)][string]$Zig)
$ErrorActionPreference = 'Stop'
$compiler = (Resolve-Path -LiteralPath $Zig).Path
$source = Join-Path $PSScriptRoot 'tests\shift.c'
$outputDirectory = Join-Path ([IO.Path]::GetTempPath()) ('Eternally-Shift-Test-' + [Guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $outputDirectory
$executable = Join-Path $outputDirectory 'shift.exe'
try {
    & $compiler cc -target x86_64-windows-gnu -O0 -g0 -Wno-dll-attribute-on-redeclaration $source `
        -lole32 -loleaut32 -ladvapi32 -luser32 -lgdi32 -luuid -o $executable
    if ($LASTEXITCODE -ne 0) { throw 'Native Shift regression test build failed.' }
    & $executable
    if ($LASTEXITCODE -ne 0) { throw "Native Shift regression tests failed with exit code $LASTEXITCODE." }
} finally {
    # Only these known compiler outputs are removed; no recursive deletion.
    foreach ($name in 'shift.exe', 'shift.pdb', 'shift.lib') {
        Remove-Item -LiteralPath (Join-Path $outputDirectory $name) -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $outputDirectory -Force -ErrorAction SilentlyContinue
}

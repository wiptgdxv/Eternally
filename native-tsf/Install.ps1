param([switch]$Register, [switch]$Remove)
$ErrorActionPreference = 'Stop'
$dest = Join-Path $env:ProgramFiles 'ContextIME'
$dll = Join-Path $dest 'ContextIMETSF.dll'
$comKey = 'HKLM:\Software\Classes\CLSID\{A65E6A23-596A-4535-A24D-2762B2679774}\InprocServer32'
$backend = Join-Path $dest 'ContextIME.exe'
$regsvr = Join-Path $env:WINDIR 'System32\regsvr32.exe'
$tip = '0x0804:{A65E6A23-596A-4535-A24D-2762B2679774}{8C597E63-C615-4B1C-AC11-318F3AABD029}'
$old = '0x0804:{8F204C91-2D7A-4B3E-9E1F-6A5C0D8B2E7F}{51489E14-C88B-40FD-A536-9ECE387BD24F}'
$run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
if (-not [Environment]::Is64BitProcess) { throw 'Run this installer from 64-bit PowerShell.' }

if ($Register) {
  try {
    $files = Join-Path (Split-Path -Parent $PSScriptRoot) 'dist'
    if (-not $Remove) {
        $null = New-Item -Path $dest -ItemType Directory -Force
        $target = Get-Item -LiteralPath $dest
        if ($target.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Eternally install directory is a reparse point.' }
        $dllSource = Join-Path $files 'ContextIMETSF.dll'
        $dllHash = (Get-FileHash -LiteralPath $dllSource).Hash
        $dll = Join-Path $dest ('ContextIMETSF-' + $dllHash.Substring(0,16) + '.dll')
        if (-not (Test-Path -LiteralPath $dll)) { Copy-Item -LiteralPath $dllSource -Destination $dll }
        foreach ($name in 'ContextIME.exe') {
            $source = Join-Path $files $name
            $targetFile = Join-Path $dest $name
            if ((Test-Path -LiteralPath $targetFile) -and (Get-FileHash -LiteralPath $source).Hash -eq (Get-FileHash -LiteralPath $targetFile).Hash) { continue }
            Copy-Item -LiteralPath $source -Destination $targetFile -Force
        }
    } else {
        $registered = Get-Item -LiteralPath $comKey -ErrorAction SilentlyContinue
        if (-not $registered) { exit 0 }
        $dll = $registered.GetValue('')
        if (-not (Test-Path -LiteralPath $dll -PathType Leaf) -or (Split-Path -Parent $dll) -ne $dest) { throw 'Unexpected registered DLL path.' }
    }
    $flags = if ($Remove) { '/s /u' } else { '/s' }
    $p = Start-Process -FilePath $regsvr -ArgumentList ($flags + ' "' + $dll + '"') -WindowStyle Hidden -Wait -PassThru
    exit $p.ExitCode
  } catch {
    [IO.File]::WriteAllText((Join-Path $env:LOCALAPPDATA 'ContextIME-native-install-error.txt'),($_ | Out-String))
    exit 1
  }
}

Add-Type @'
using System.Runtime.InteropServices;
public static class ContextNativeProfile {
    [DllImport("input.dll", ExactSpelling=true, CharSet=CharSet.Unicode)]
    public static extern bool InstallLayoutOrTip(string profile, uint flags);
}
'@
if ($Remove) {
    $null = [ContextNativeProfile]::InstallLayoutOrTip($tip,1)
}
$localBackend = Join-Path (Split-Path -Parent $PSScriptRoot) 'dist\ContextIME.exe'
$runningBackends = @(Get-Process ContextIME -ErrorAction SilentlyContinue | Where-Object { $_.Path -in $backend,$localBackend })
$previousBackend = $runningBackends | Select-Object -First 1 -ExpandProperty Path
$runningBackends | ForEach-Object {
    $_.Kill()
    if (-not $_.WaitForExit(5000)) { throw 'Eternally backend did not exit.' }
}
$argsForAdmin = '-NoProfile -ExecutionPolicy Bypass -File "' + $PSCommandPath + '" -Register'
if ($Remove) { $argsForAdmin += ' -Remove' }
try {
    $admin = Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList $argsForAdmin -Verb RunAs -Wait -PassThru
    if ($admin.ExitCode -ne 0) { throw "TSF registration returned exit code $($admin.ExitCode)." }
} catch {
    if ($previousBackend -and (Test-Path -LiteralPath $previousBackend)) {
        Start-Process -FilePath $previousBackend -ArgumentList '--native' -WindowStyle Hidden
    }
    throw
}
if ($Remove) {
    Remove-ItemProperty -LiteralPath $run -Name ContextIME -ErrorAction SilentlyContinue
    Write-Host 'Eternally profile unregistered. Installed files may remain until applications exit.'
    exit 0
}
if (-not [ContextNativeProfile]::InstallLayoutOrTip($tip,0)) { throw 'Windows could not enable the Eternally profile.' }
Start-Process -FilePath $backend -ArgumentList '--native' -WindowStyle Hidden
$enabled = $false
foreach ($language in (Get-WinUserLanguageList)) {
    foreach ($entry in $language.InputMethodTips) {
        if ('0x' + $entry -eq $tip) { $enabled = $true }
    }
}
if (-not $enabled) { throw 'Windows did not save the Eternally profile.' }
New-ItemProperty -LiteralPath $run -Name ContextIME -PropertyType String -Value ('"' + $backend + '" --native') -Force | Out-Null
$null = [ContextNativeProfile]::InstallLayoutOrTip($old,1)
Write-Host 'Eternally installed. Press Win + Space and select Eternally.'

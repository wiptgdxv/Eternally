param([string]$OfficialInstaller)

$ErrorActionPreference = 'Stop'

function Find-MoqiLauncher {
    $default = Join-Path ${env:ProgramFiles(x86)} 'MoqiIM\MoqiLauncher.exe'
    if (Test-Path -LiteralPath $default -PathType Leaf) { return $default }
    $run = Get-ItemPropertyValue -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name MoqiLauncher -ErrorAction SilentlyContinue
    if ($run) {
        $candidate = [Environment]::ExpandEnvironmentVariables($run).Trim('"')
        if ((Split-Path -Leaf $candidate) -eq 'MoqiLauncher.exe' -and (Test-Path -LiteralPath $candidate -PathType Leaf)) { return $candidate }
    }
    return $default
}

function Test-ContextImeEnabled([string]$Tip) {
    foreach ($language in (Get-WinUserLanguageList)) {
        if ($language.LanguageTag -ne 'zh-Hans-CN') { continue }
        foreach ($entry in $language.InputMethodTips) {
            if ($entry -eq $Tip) { return $true }
        }
    }
    return $false
}

$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $argsForAdmin = '-NoProfile -ExecutionPolicy Bypass -File "' + $PSCommandPath + '"'
    if ($OfficialInstaller) { $argsForAdmin += ' -OfficialInstaller "' + (Resolve-Path -LiteralPath $OfficialInstaller).Path + '"' }
    $admin = Start-Process -FilePath powershell.exe -ArgumentList $argsForAdmin -Verb RunAs -Wait -PassThru
    if ($admin.ExitCode -eq 0) {
        $launcher = Find-MoqiLauncher
        Start-Process -FilePath $launcher -WindowStyle Hidden
        Write-Host '按 Win + Space 选择「语境输入」。如果列表暂未刷新，请注销后重新登录。'
    }
    exit $admin.ExitCode
}

try {
    $root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $exe = Join-Path $root 'dist\ContextIME.exe'
    if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { throw '请先构建 dist\ContextIME.exe。' }
    $defaultMoqi = Join-Path ${env:ProgramFiles(x86)} 'MoqiIM'
    $launcher = Find-MoqiLauncher
    if (-not (Test-Path -LiteralPath $launcher -PathType Leaf)) {
        $hash = '7e963b6da980034ae3a9e422f37c8a0200b6d3b2c198598ae2ef3ffbad68918f'
        if (-not $OfficialInstaller) {
            $OfficialInstaller = Join-Path $env:TEMP 'contextime-moqi-1.0.5.exe'
            if (-not (Test-Path -LiteralPath $OfficialInstaller) -or (Get-FileHash -LiteralPath $OfficialInstaller).Hash -ne $hash) {
                Write-Host '正在下载墨奇 1.0.5 的 TSF 前端…'
                if ((Test-Path -LiteralPath $OfficialInstaller) -and (Get-Item -LiteralPath $OfficialInstaller).Length -ge 42204542) {
                    Move-Item -LiteralPath $OfficialInstaller -Destination ($OfficialInstaller + '.' + [guid]::NewGuid() + '.invalid')
                }
                curl.exe -fL --connect-timeout 20 --max-time 900 -C - 'https://github.com/gaboolic/moqi-im-windows/releases/download/1.0.5/moqi-im-windows-setup.exe' -o $OfficialInstaller
                if ($LASTEXITCODE -ne 0) {
                    throw '下载中断。可以重新运行以继续下载，或传入已下载的官方安装包。'
                }
            }
        }
        if ((Get-FileHash -LiteralPath $OfficialInstaller).Hash -ne $hash) { throw '官方安装包校验失败。' }
        $setup = Start-Process -FilePath $OfficialInstaller -ArgumentList '/NORESTART' -Wait -PassThru
        $launcher = Find-MoqiLauncher
        if ($setup.ExitCode -ne 0 -or -not (Test-Path -LiteralPath $launcher)) {
            throw '墨奇安装未完成，请完成后再运行本脚本。'
        }
    }
    $moqi = Split-Path -Parent $launcher
    $defaultEntry = Get-Item -LiteralPath $defaultMoqi -ErrorAction SilentlyContinue
    if ($moqi -ne $defaultMoqi -and $defaultEntry -and
        ($defaultEntry.LinkType -ne 'Junction' -or $defaultEntry.Target -ne $moqi)) {
        throw '墨奇默认目录已被占用，无法为自定义安装建立目录链接。'
    }

    $native32 = Join-Path $env:WINDIR 'SysWOW64\MoqiTextService.dll'
    $native64 = Join-Path $env:WINDIR 'System32\MoqiTextService.dll'
    if (-not (Test-Path -LiteralPath $native64) -or -not (Test-Path -LiteralPath $native32)) {
        throw '墨奇 TSF DLL 尚未就绪。如官方安装提示重启，请重启后再运行。'
    }
    $configPath = Join-Path $moqi 'backends.json'
    $config = [IO.File]::ReadAllText($configPath)
    $entries = @(
        foreach ($entry in (ConvertFrom-Json -InputObject $config)) {
            if ($entry.name) { $entry }
            elseif ($entry.PSObject.Properties['value'] -and $entry.PSObject.Properties['Count']) { $entry.value }
            else { throw '墨奇后端配置中存在无法识别的条目。' }
        }
    )
    $ours = (ConvertFrom-Json -InputObject ([IO.File]::ReadAllText((Join-Path $PSScriptRoot 'backends.json'))))[0]
    $existing = @($entries | Where-Object name -eq $ours.name)
    if ($existing.Count -gt 0 -and ($existing.Count -ne 1 -or $existing[0].command -ne $ours.command -or $existing[0].params -ne $ours.params)) {
        throw '现有 contextime 后端配置与预期不同，请检查后再安装。'
    }
    $dest = Join-Path $moqi 'contextime'
    $null = New-Item -Path (Join-Path $dest 'input_methods\translation') -ItemType Directory -Force
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'contextime\input_methods\translation\ime.json') -Destination (Join-Path $dest 'input_methods\translation\ime.json') -Force
    if (-not $existing) { $entries += $ours }
    $newConfig = ConvertTo-Json -InputObject $entries -Depth 20
    if ($config.Trim() -ne $newConfig.Trim()) {
        Copy-Item -LiteralPath $configPath -Destination ($configPath + '.contextime-' + [guid]::NewGuid() + '.bak')
        [IO.File]::WriteAllText($configPath, $newConfig, [Text.UTF8Encoding]::new($false))
    }

    $backend = Join-Path $dest 'ContextIME.exe'
    Get-Process MoqiLauncher -ErrorAction SilentlyContinue | Where-Object Path -eq $launcher | Stop-Process
    Get-Process ContextIME -ErrorAction SilentlyContinue | Where-Object Path -eq $backend | Stop-Process
    Copy-Item -LiteralPath $exe -Destination $backend -Force
    # The official DLL looks in this directory when loaded by a Windows app.
    if ($moqi -ne $defaultMoqi -and -not $defaultEntry) {
        $null = New-Item -ItemType Junction -Path $defaultMoqi -Target $moqi
    }
    $env:MOQI_PROGRAM_DIR = $moqi
    foreach ($dll in @($native32, $native64)) {
        $regsvr = Join-Path (Split-Path -Parent $dll) 'regsvr32.exe'
        $register = Start-Process -FilePath $regsvr -ArgumentList ('/s "' + $dll + '"') -WindowStyle Hidden -PassThru -Wait
        if ($register.ExitCode -ne 0) { throw ('TSF 注册失败，退出码：' + $register.ExitCode) }
    }
    $languages = Get-WinUserLanguageList
    $chinese = $languages | Where-Object LanguageTag -eq 'zh-Hans-CN' | Select-Object -First 1
    if (-not $chinese) {
        $languages.Add((New-WinUserLanguageList 'zh-Hans-CN')[0])
        $chinese = $languages | Where-Object LanguageTag -eq 'zh-Hans-CN' | Select-Object -First 1
    }
    $tip = '0804:{8F204C91-2D7A-4B3E-9E1F-6A5C0D8B2E7F}{51489E14-C88B-40FD-A536-9ECE387BD24F}'
    if (-not (Test-ContextImeEnabled $tip)) {
        $chinese.InputMethodTips.Add($tip)
        Set-WinUserLanguageList -LanguageList $languages -Force
    }
    if (-not (Test-ContextImeEnabled $tip)) {
        Add-Type -TypeDefinition @'
using System.Runtime.InteropServices;
public static class ContextImeProfile {
    [DllImport("input.dll", ExactSpelling = true, CharSet = CharSet.Unicode)]
    public static extern bool InstallLayoutOrTip(string profile, uint flags);
}
'@
        $null = [ContextImeProfile]::InstallLayoutOrTip("0x$tip", 0)
    }
    if (-not (Test-ContextImeEnabled $tip)) { throw 'TSF 已注册，但 Windows 没有保存语境输入条目。' }
    Write-Host '已注册语境输入。按 Win + Space 切换。如果启动器尚未运行，请注销后重新登录。'
    if ([Console]::UserInteractive) { $null = Read-Host '按回车关闭' }
} catch {
    $logPath = Join-Path $env:LOCALAPPDATA 'ContextIME-install-error.txt'
    [IO.File]::WriteAllText($logPath, ($_ | Out-String))
    Write-Host $_.Exception.Message -ForegroundColor Red
    if ([Console]::UserInteractive) { $null = Read-Host '按回车关闭' }
    exit 1
}

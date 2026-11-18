$ErrorActionPreference = 'Stop'

function Find-Deployer {
    $runKey = 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Run'
    $server = Get-ItemPropertyValue -Path $runKey -Name WeaselServer -ErrorAction SilentlyContinue
    if ($server) {
        $serverPath = [Environment]::ExpandEnvironmentVariables($server).Trim('"')
        $candidate = Join-Path (Split-Path -Parent $serverPath) 'WeaselDeployer.exe'
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate }
    }
    foreach ($base in @($env:ProgramFiles, ${env:ProgramFiles(x86)})) {
        if (-not $base) { continue }
        $rime = Join-Path $base 'Rime'
        if (-not (Test-Path -LiteralPath $rime)) { continue }
        $deployer = Get-ChildItem -LiteralPath $rime -Directory |
            ForEach-Object { Get-Item -LiteralPath (Join-Path $_.FullName 'WeaselDeployer.exe') -ErrorAction SilentlyContinue } |
            Sort-Object LastWriteTime -Descending | Select-Object -First 1
        if ($deployer) { return $deployer.FullName }
    }
}

try {
    $deployer = Find-Deployer
    if (-not $deployer) {
        Write-Host '正在准备小狼毫官方安装包。请在安装窗口完成安装。'
        $installer = Join-Path $env:TEMP 'Lo-Weasel-0.17.4-installer.exe'
        $sha256 = 'cf509534a8f5f8af9c98ed7cbb8f135439f145a8cbe7e50ede42bb5b5ab45c29'
        if (-not (Test-Path -LiteralPath $installer) -or (Get-FileHash -LiteralPath $installer).Hash -ne $sha256) {
            $download = Join-Path $env:TEMP ("Lo-Weasel-" + [guid]::NewGuid() + '.exe')
            curl.exe -fL --connect-timeout 20 --max-time 240 'https://github.com/rime/weasel/releases/download/0.17.4/weasel-0.17.4.0-installer.exe' -o $download
            if ($LASTEXITCODE -ne 0 -or (Get-FileHash -LiteralPath $download).Hash -ne $sha256) {
                throw '小狼毫安装包下载或校验失败，请重试。'
            }
            Move-Item -LiteralPath $download -Destination $installer -Force
        }
        $setup = Start-Process -FilePath $installer -Wait -PassThru
        $deployer = Find-Deployer
        if ($setup.ExitCode -ne 0 -or -not $deployer) { throw '小狼毫安装未完成，请完成安装后再次运行。' }
    }

    $rimeKey = 'HKCU:\Software\Rime\Weasel'
    $configuredDir = Get-ItemPropertyValue -Path $rimeKey -Name RimeUserDir -ErrorAction SilentlyContinue
    $userDir = if ($configuredDir) { [Environment]::ExpandEnvironmentVariables($configuredDir) } else { Join-Path $env:APPDATA 'Rime' }
    $luaDir = Join-Path $userDir 'lua'
    $null = New-Item -ItemType Directory -Path $luaDir -Force
    $luaSource = Join-Path $PSScriptRoot 'rime\lua\lo_follow.lua'
    $luaTarget = Join-Path $luaDir 'lo_follow.lua'
    if ((Test-Path -LiteralPath $luaTarget) -and (Get-FileHash -LiteralPath $luaSource).Hash -ne (Get-FileHash -LiteralPath $luaTarget).Hash) {
        Copy-Item -LiteralPath $luaTarget -Destination ($luaTarget + '.' + [guid]::NewGuid() + '.bak')
    }
    Copy-Item -LiteralPath $luaSource -Destination $luaTarget -Force

    $configured = 0
    # The bundled simplified scheme inherits the Luna Pinyin processors.
    foreach ($schema in @('luna_pinyin')) {
        $file = "$schema.custom.yaml"
        $target = Join-Path $userDir $file
        if (-not (Test-Path -LiteralPath $target)) {
            Copy-Item -LiteralPath (Join-Path $PSScriptRoot "rime\$file") -Destination $target
            $configured++
            continue
        }
        $yaml = [IO.File]::ReadAllText($target)
        if ($yaml -match '(?m)^\s+[^\r\n]+:\s*lua_processor@\*lo_follow\s*(?:#.*)?$') { $configured++; continue }
        $patch = [regex]::Match($yaml, '(?m)^patch:[ \t]*(?:#[^\r\n]*)?\r?$')
        if (-not $patch.Success -or $yaml -match '(?m)^\s+[''"]?engine/processors(?:/[^:''"]*)?[''"]?\s*:') {
            Write-Warning "已保留 $file 的自定义处理器，请按 README 手动合并。"
            continue
        }
        $insertAt = $patch.Index + $patch.Length
        $indent = [regex]::Match($yaml.Substring($insertAt), '(?m)^([ ]+)\S').Groups[1].Value
        if (-not $indent) { $indent = '  ' }
        Copy-Item -LiteralPath $target -Destination ($target + '.' + [guid]::NewGuid() + '.bak')
        $entry = '"engine/processors/@before 0": lua_processor@*lo_follow'
        $yaml = $yaml.Insert($insertAt, "`n$indent$entry")
        [IO.File]::WriteAllText($target, $yaml, [Text.UTF8Encoding]::new($false))
        $configured++
    }
    if (-not $configured) { throw '现有方案需要手动合并，请按 README 操作。' }
    $deploy = Start-Process -FilePath $deployer -ArgumentList '/deploy' -WorkingDirectory (Split-Path -Parent $deployer) -WindowStyle Hidden -PassThru
    if (-not $deploy.WaitForExit(45000)) {
        Stop-Process -InputObject $deploy -ErrorAction SilentlyContinue
        throw '小狼毫部署超时，请从小狼毫托盘菜单重新部署。'
    }
    if ($deploy.ExitCode -ne 0) { throw '小狼毫重新部署失败，请从小狼毫托盘菜单重试。' }
    Start-Process -FilePath (Join-Path $PSScriptRoot 'LoTranslation.exe') -ArgumentList '--preview' -WindowStyle Hidden
    Write-Host '配置已写入。按 Win + Space 切到小狼毫，选朙月拼音，再在记事本输入「你好」。'
} catch {
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
}

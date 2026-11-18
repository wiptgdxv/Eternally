# Go 原生输入法原型

本目录验证 Windows 11 x64 的 TSF 接入和回车规则。输入法逻辑、候选窗和注册逻辑都由 Go 实现。仅 DLL 导出使用 cgo，构建需要 Zig 的 C 工具链。这个原型不依赖小狼毫或旧版翻译助手。

## 构建和注册

在项目目录运行，传入 Zig 0.15.2 的可执行文件路径：

```powershell
.\tsf\Build.ps1 -Zig 'C:\tools\zig\zig.exe'
$dll = (Resolve-Path .\dist\LoGoTSF.dll).Path
& "$env:WINDIR\System32\regsvr32.exe" $dll
```

原型使用独立的输入法标识。Windows 可能需要重新登录后才显示新输入源。此 DLL 尚未签名，目标仅为本机开发，受保护的应用可能不会加载它。

## 操作

1. 在记事本中用 `Win + Space` 选择「语境翻译 Go 原型」。
2. 打 `nihao`，按一次回车。候选「你好」进入同一窗口里的中文草稿，下面显示固定英文 `Hello`。
3. 可以继续打 `shijie` 并按回车，把「世界」追加到草稿。没有未确认的拼音时，再按回车通过 TSF 提交英文。
4. `Esc` 清空草稿，退格先删拼音再删草稿文字。长按回车的重复事件不触发提交。

只支持这两个固定词和示例英文，用于验证架构。当前候选窗为原生调试界面。后续接入随安装包分发的 librime、真实异步翻译、正式候选窗样式，以及使用 MyGo Go 原生 UI 实现设置。

## 注销

```powershell
$dll = (Resolve-Path .\dist\LoGoTSF.dll).Path
& "$env:WINDIR\System32\regsvr32.exe" /u $dll
```

注销前切回原来的输入法，关闭试用时打开的应用。Go 原型 DLL 一旦被应用进程加载，会留到该进程退出。

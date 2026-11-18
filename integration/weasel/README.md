# 语境翻译 · Windows 11

当前的 [Eternally Windows 输入法](../../cmd/contextime/README.md) 使用 MyGo。下文记录小狼毫和 MyGo 浮窗版本的用法。

在原应用里照常使用小狼毫打拼音。选词后中文照常上屏，短暂停顿时，MyGo 在光标附近显示英文。悬停浮窗右下角可复制译文，也可以从系统托盘复制。

## 预览界面

启动 `LoTranslation.exe`，在系统托盘右键点击语境翻译图标，选择「预览浮窗」。示例可独立预览，无需安装输入法。也可以运行 `./LoTranslation.exe --preview`。

## 接入小狼毫

双击 `Setup.cmd` 可以安装小狼毫、配置朙月拼音并重新部署。小狼毫首次安装时，按 Windows 安装窗口的提示操作。若要手动接入，可用以下步骤。

1. 安装 [小狼毫](https://github.com/rime/weasel/releases)，在 Windows 输入法列表中切换到小狼毫。
2. 右键小狼毫托盘图标，打开「用户文件夹」。把本项目的 `rime/lua/lo_follow.lua` 复制到该目录的 `lua` 子目录中。
3. 把 `rime/luna_pinyin.custom.yaml` 复制到用户目录。已有同名配置时，在已有的 `patch:` 下合并 `"engine/processors/@before 0": lua_processor@*lo_follow`，并保留现有配置。小狼毫自带的朙月拼音·简化字会继承此设置。其他独立方案可以把同一项加到对应的 `<方案 ID>.custom.yaml`。
4. 右键小狼毫托盘图标，选择「重新部署」。启动 `LoTranslation.exe`，在记事本里选词输入「你好」，等待英文显示。

托盘中的「跟随翻译」可以暂停。启用时，Rime 选词确认的中文通过本机命名管道传给程序，随后提交到 Lo 使用的微软 Edge 翻译接口。程序不保存原文和译文。移除接入时，从自定义方案里删掉上面的配置项，再重新部署。

## 构建

Windows 11 x64、Go 1.27.3、WebView2 Runtime。Go 依赖包含 MyGo v0.9.0。

```powershell
go build -trimpath -ldflags '-H=windowsgui' -o LoTranslation.exe .
```

当前实现覆盖中译英浮窗与复制。部分应用不向 Win32 提供文本光标坐标，此时浮窗定位到鼠标附近。翻译区域按 Lo 的深色默认样式实现，使用 Windows 系统字体。Rime 扩展只能观察自己的提交和经过其处理的按键，应用内部的鼠标编辑等操作无法完全同步。

视觉和翻译接口参考 [Lo 语境输入法](https://github.com/chengpeng-cp/lo)，拼音和选词由 [小狼毫](https://github.com/rime/weasel) 提供。

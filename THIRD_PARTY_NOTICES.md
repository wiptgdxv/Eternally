# 第三方组件与来源

本文件记录仓库中使用的组件、数据和设计参考。项目自有代码尚未单独指定开源许可证；本说明不改变任何第三方文件的许可。

| 项目 | 用途 | 来源与许可 |
| --- | --- | --- |
| MyGo v0.9.0 | 原生 Go 桌面界面 | [项目](https://github.com/egoist/mygo) · [MIT](https://github.com/egoist/mygo/blob/main/LICENSE) |
| 雾凇拼音／白霜拼音词表 | 本地中文候选数据 | [雾凇](https://github.com/iDvel/rime-ice) · [白霜](https://github.com/gaboolic/rime-frost) · [仓库内 GPLv3](cmd/contextime/words-LICENSE) · [数据来源说明](cmd/contextime/words-SOURCES.md) |
| DM Sans | 介绍页英文字体 | [字体项目](https://github.com/googlefonts/dm-fonts) · [仓库内 SIL OFL 1.1](docs/assets/dm-sans-LICENSE.txt) |
| Lo | 产品交互、翻译接口与早期视觉参考 | [项目及 MIT 许可](https://github.com/chengpeng-cp/lo) |

Go 依赖的版本和校验信息保留在 `go.mod`、`go.sum` 中。依赖源代码由 Go 工具按模块获取，适用其各自许可证；本仓库不将构建出的可执行文件和 DLL 作为源码提交。

## 可选离线模型

离线模型和 ONNX Runtime 由应用按需下载，不随源码仓库提交。

- [Helsinki-NLP/opus-mt-zh-en](https://huggingface.co/Helsinki-NLP/opus-mt-zh-en)：中译英模型，上游标注 CC BY 4.0。
- [Xenova/opus-mt-zh-en](https://huggingface.co/Xenova/opus-mt-zh-en)：用于 Windows 离线推理的量化 ONNX 权重。
- [Microsoft ONNX Runtime](https://github.com/microsoft/onnxruntime)：MIT 许可；应用下载模型包时同时获取运行库的许可与第三方声明。

具体下载源、固定修订和内容校验位于 `internal/translation/offline_windows.go`。

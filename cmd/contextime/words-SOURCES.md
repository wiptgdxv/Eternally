# 内置拼音词库来源

`words.tsv.gz` 是经过筛选与排序的拼音、词语数据，解压后为 UTF-8 TSV 文本，供本地候选生成使用。程序使用 Go embed 将其包含在构建产物中。

来源为 [雾凇拼音 rime-ice](https://github.com/iDvel/rime-ice) 数据，以及本机可用的 [白霜拼音 rime-frost](https://github.com/gaboolic/rime-frost) 所含字表、词表。处理包括筛选词条、整理拼音、排序及 gzip 压缩；所保存的词条可直接通过解压 TSV 检查和编辑。

数据继续适用上游 GPLv3，完整许可证见同目录 `words-LICENSE`。本说明不将第三方词库变更为项目自有代码，也不改变其原有许可。

本次整理未保留可核验的上游精确提交号，因此不标注未经验证的 revision。本来源说明整理于 2026-11-18。

# 闭环 #80: DATA_STANDARD source 标签表述对齐多 provider 现状

- **日期**: 2026-08-03
- **PR**: #277(功能)+ 本文档(归档)
- **背景**: 「现状表述」维度重扫(暂用/临时/当前仅类词)——DATA_STANDARD.md「一致性校验」行「当前仅 futu 写入」过时:`bars.source` 列有多个写入方——CLI mock/file/url 各自默认 `cli-mock`/`cli-file`/`cli-url`(main.go:996/1097/1198,`-source` 可覆盖),futu 系写平台源;dev-up 种子数据即 mock 写入。

## 改动

- doc/DATA_STANDARD.md:逐 provider 说明 source 标签,保留「同键共存可对比」语义

## 验证

- 表述与 main.go source 默认值逐项核对;docs-only CI check-skip 通过

## 备注

- **引擎经验**: 「当前仅 X 写入」类现状限定句是欠账高发点——**provider/写入方演进时限定句最易漏同步**;对账须回源码逐 provider 确认默认 source 标签,不能只信文档。
- **候选池**: 仍枯竭(待老板 7 项)。

# 闭环 #72: 顶层 help futu 行补全子命令列表

- **日期**: 2026-08-03
- **PR**: #261(功能)+ 本文档(归档)
- **背景**: CLI 对账——顶层 help 的 futu 行只列 status/quote,实际 5 个子命令(funds/position/order 漏列;accept-futu-cli 21 项已覆盖、serve 代理表 #63 已列 4 端点),顶层 help 具误导性。

## 改动

- cmd/wbot/main.go 顶层 help futu 行:`status/quote` → `status/quote/funds/position/order`

## 验证

- verify.sh 全绿(gofmt/test/vet/race/staticcheck + 零依赖 accept);新二进制 `wbot -h` 实测;PR #261 CI 6/6 绿
- 环境注:本机 staticcheck 在 `$(go env GOPATH)/bin`(默认 PATH 未含),verify 按契约响亮提示——非 repo 问题

## 备注

- **引擎经验**: 顶层 help 是用户第一入口,「列 2 个漏 3 个」比「不列」(try -h 指引)更具误导性——对账时把 help 文本当契约:顶层行 vs 子命令 -h 输出 vs accept 覆盖三重对照。
- **候选池**: 仍枯竭(待老板 7 项)。

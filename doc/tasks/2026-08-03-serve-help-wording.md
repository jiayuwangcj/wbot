# 闭环 #76: serve 去 "read-only" 误述 + 补 POST /v1/ingest 端点

- **日期**: 2026-08-03
- **PR**: #269(功能)+ 本文档(归档)
- **背景**: #75(dev-up loopback 收敛)后对账继续安全/表述维度——顶层 help 与 serve -h 称 serve 为 "read-only data API",但 serve 有完整写面(POST /v1/backtests、PUT/DELETE /v1/watchlist、PUT /v1/admin/config、POST /v1/ingest),且 serve -h 端点清单**缺 POST /v1/ingest**(2026-08-03 options 链拉取加时未同步 help)。

## 改动

- cmd/wbot/main.go:顶层 help serve 行「Read-only HTTP data API」→「HTTP server: data API + write endpoints + futu proxies + Web UI」;serve -h 首句「read-only data API」→「HTTP data API」+ 补 ingestion API (POST /v1/ingest)
- doc/API.md:标题「API 契约(只读数据接口)」→「API 契约(wbot serve HTTP 接口)」
- doc/README.md:「`wbot serve` 只读数据接口契约」→「HTTP 接口契约」

## 验证

- `wbot serve -h` 实测输出正确(ingestion API 在列);verify.sh 全绿(19 包 + vet + race + staticcheck + CLI smoke);dev-up 自动重启后 ALL 19 CHECKS PASSED;PR #269 CI 5/5

## 备注

- **引擎经验**: 「help 文本三重对照」(顶层 vs 子命令 -h vs accept 覆盖)要延伸到**文档标题/索引级**表述——本欠账在顶层 help、serve -h、API.md 标题、README 索引**四处**同病,同一误述常整链存在,修一处后须全仓库 grep 清零(「Read-only HTTP」已零命中)。serve -h 端点清单与 mux 注册逐项比对可抓「端点新增未同步 help」类欠账(POST /v1/ingest 属此类)。
- **候选池**: 仍枯竭(待老板 7 项)。

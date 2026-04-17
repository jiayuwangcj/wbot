# wbot

`wbot` 是一个面向个人交易的 Go 量化交易机器人项目。

当前版本只做工程自动化基线，不交付业务功能。

## v0 目标（仅流程）

- Go 单体工程（all-in-one）基础可运行
- GitHub Actions CI 全绿
- TDD 标准工作流落地
- PR 通过后支持 auto-merge（需在仓库设置中开启分支保护规则）
- 生成第一份 proposal 文档，作为后续架构演进基线

## 项目约束（已确认）

- 主要语言：Go
- 部署形态：单二进制，支持前台/守护两种运行方式（后续实现）
- 架构：master/agent，多机部署，agent 主动 HTTPS 轮询 master（后续实现）
- 市场：港股/美股，现货 + 期权（后续实现）
- 交易接入：富途 / IBKR 抽象层（后续实现）
- 存储：PostgreSQL（后续可扩展）
- 日志：`zerolog`
- Web：后端 Go API，前端 React 打包后 `go:embed` 内嵌（后续实现）
- 外部通知：Telegram / Discord（后续实现）

## 本地开发

```bash
go test ./... -count=1
go vet ./...
```

## 协作规则（v0）

- 功能/计划/缺陷/发布，统一由 GitHub 留言驱动
- 留言之外的执行动作，默认由 Agent 自动完成
- 文档统一放在 `doc/`，保持 tiny、独立、可双向链接

## 文档入口

- 总览：`doc/README.md`

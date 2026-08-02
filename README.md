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
- 部署形态：单二进制，前台 serve（dev-up/日构建脚本以守护方式启动）
- 架构：master/agent 占位子系统已实现（见 `doc/API.md` Agent federation）；多机 HTTPS 部署待后续
- 市场：港股/美股，现货 + 期权（已接入，见 `doc/FUTU.md`）
- 交易接入：富途已接入（见 `doc/FUTU.md`）；IBKR 抽象层待后续
- 存储：PostgreSQL（后续可扩展）
- 日志：`zerolog`（当前 std log/fmt 输出；结构化日志随 v3 执行路径阶段按需收紧，见 `doc/ROADMAP.md`）
- Web：后端 Go API，前端原生 HTML/CSS/JS 经 `go:embed` 内嵌
- 外部通知：Telegram / Discord（后续实现）

## 本地开发

```bash
go test ./... -count=1
go vet ./...
scripts/verify.sh   # 提交前全量校验：单测 + gofmt + race/staticcheck + 零依赖 accept（≡ CI test job）
scripts/dev-up.sh   # 本地全链冒烟：PG + serve，19 项
```

逐端点验收：`scripts/accept-*.sh`（12 个脚本，135 项检查；零依赖对与 PG 依赖对已在 CI 自动跑），索引见 `doc/ACCEPTANCE.md`。

## 协作规则（v0）

- 功能/计划/缺陷/发布，统一由 GitHub 留言驱动
- 留言之外的执行动作，默认由 Agent 自动完成
- 文档统一放在 `doc/`，保持 tiny、独立、可双向链接

## 文档入口

- 总览：`doc/README.md`

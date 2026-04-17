# （草稿）GitHub Issue 正文 — 复制到「Feature」模板

> **计划已调整**：仓库主线优先级见最新 [`doc/ROADMAP.md`](../ROADMAP.md)（**数据拉取与落地 → 回测**，模拟盘/控制面不优先扩张）。本草稿描述的是**早期已落地的一段垂直切片**，保留作历史参考。

**建议标题**：`[feature] v1 首包：交易域最小模型 + agent/master 骨架 + wbot CLI`

---

## Trigger comment

- **仓库级 GitHub-driven 锚点**（通用 `Driven-By`）：<https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869>（Feature Issue [#8](https://github.com/jiayuwangcj/wbot/issues/8)）
- 若本切片单独开 Issue：在同一 Issue 下留**锚点评论**后，把该评论 URL 填回此处并作为 PR 的 `Driven-By:`。

## Goal（对齐 [[ROADMAP]] v1 的第一块）

在**不做真实网络、不做券商接入**的前提下，落地可测的 **v1 垂直切片**，为后续「master/agent HTTPS 轮询」「模拟盘执行」铺路。

### 范围

1. **`cmd/wbot`**：提供可构建的单二进制入口；至少支持 `version`（或 `-version`）与 help；为后续 `agent` / `master` 子命令预留占位（可为 stub）。
2. **`internal/domain`**：最小交易域类型与约定（例如 `Symbol`、`Side`、可选 `OrderID` / 状态枚举），带简短包注释，全部 **TDD**。
3. **`internal/agent` / `internal/master`**：各一个**小而清晰**的接口或门面（例如「Agent 自报身份」「Master 登记占位」），实现可为 no-op 或内存假实现，**禁止**在本切片内引入真实 HTTP 服务端/客户端（仅限类型与函数签名级别的预留注释）。

### 验收

- `go test ./... -count=1`、`go vet ./...`、本地与 CI 中已存在的 `gofmt` / `staticcheck` 门禁通过。
- `go build -o /dev/null ./cmd/wbot` 成功。
- 新增代码风格与 `internal/bootstrap` 一致：短小、可测、无多余依赖。

### 非目标

- HTTPS、证书、真实轮询间隔、PostgreSQL、富途/IBKR、Web UI。

## Plan（可勾选）

- [ ] domain 包与测试
- [ ] agent/master 骨架与测试
- [ ] CLI 入口与测试（可用 `os/exec` 测 `-h` 或 version，若过重则只测 domain/agent/master）

## 仓库内链回

- 路线图：[`doc/ROADMAP.md`](https://github.com/jiayuwangcj/wbot/blob/main/doc/ROADMAP.md)
- 自动推进说明：[`doc/AUTO_ADVANCE.md`](https://github.com/jiayuwangcj/wbot/blob/main/doc/AUTO_ADVANCE.md)
- 本草稿源文件（复制后可删本段）：`doc/issues/draft-2026-04-17-v1-first-slice.md`

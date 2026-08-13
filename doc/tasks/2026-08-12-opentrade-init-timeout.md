# OpenTrade init 握手无超时 → 连接泄漏打满网关 128 上限(bugfix)

- **id**: `2026-08-12-opentrade-init-timeout`
- **created**: `2026-08-12`
- **updated**: `2026-08-12`

## Goal(2026-08-12 盘中实测发现 + 用户指令强化连接管理)

**根因问题**:futu proto 连接(11111)在网关不响应时**永久阻塞**:`gofutuapi.initConnectSync` 的 `io.ReadFull` 无 read deadline(`FutuApiOption.Timeout` 字段存在但**未接线**,`conn.go:84` dial 硬编码 5s,init 读无 deadline)→ `OpenTrade` 不返回 → 各调用点 `defer tc.Close()` 不执行 → **连接泄漏**。

实测:serve 泄漏 128 个 ESTAB 连接打满 futu-opend-rs 的 128 连接上限(`max connections reached (128), rejecting`)→ 新 proto 连接全被拒(EOF)→ wheel positions 全部失败。serve 重启后恢复,但**未修还会复发**(网关任何一次假死/重启间隙即开始泄漏)。

**用户指令(2026-08-12 09:43,方向性约束,覆盖本任务全部实现)**:

> 「强化连接管理,券商api的网关非常容易封杀,所有失败重试不得超过2次,不能多次重复创建连接,必须连接复用」

即本任务交付物 = **完整连接管理**,三条硬性约束:
1. **所有失败重试 ≤ 2 次**(失败即重试,累计不超过 2 次,防止网关封杀)
2. **不重复创建连接**:禁止每次调用都 OpenTrade+Close(现状 4 调用点全如此,每次 tick 新建连接握手 → 网关易封杀)
3. **必须连接复用**:长连接复用(进程级单例/按 addr 缓存 + 断线重连),复用是核心交付

## 现状(2026-08-12 09:4x 实测)

- `internal/futu/trade.go:41` OpenTrade: `gofutuapi.Open(ctx, gofutuapi.FutuApiOption{Address: addr})`——未传 Timeout,SDK 也不接
- SDK `gofutuapi@v0.0.0-20260515153859`(module cache)conn.go:initConnectSync 的 ReadFull 无 deadline;`FutuApiOption.Timeout`(conn.go:30)定义后未被 connect 使用
- 泄漏面 4 处 OpenTrade 调用:cmd/wbot/wheel_scheduler.go:97(futuPositions,每 tick)、internal/httpapi/futu_client.go:51(futuAccounter,/v1/futu/account 轮询)、cmd/wbot/futu.go:64(CLI)、cmd/wbot/telegram_scheduler.go:65(telegram 下单)
- 网关 futu-opend-rs 1.5.0:proto 11111 连接上限 128,满则 reject

## Constraints

- **不动** `internal/wheelrun/*`、`internal/llmreview/*` 的逻辑(并行任务);`cmd/wbot/telegram_scheduler.go` 的**业务逻辑**不动,但其 OpenTrade 调用点参与连接复用改造
- `futu.OpenTrade(ctx, addr)` 签名可微调(复用方案需要),但**4 处调用点必须改成复用语义**(这是用户指令核心,不是零改动)
- 零新远程依赖:SDK fork 本地化(`go.mod replace` 指向仓库内目录),MIT 许可保留
- 超时语义:init 握手超时(默认 10s)→ 返回明确错误 + **连接必须关闭**(不能留半开连接)
- **失败重试 ≤ 2 次**:任何失败(超时/EOF/网关错误)最多重试 1-2 次,禁止无限/多次重试(网关封杀红线)
- **连接复用**:进程内按 addr 复用 TradeClient;连接断开检测 + 自动重连(重连本身也受重试限制);复用连接必须串行使用(互斥锁),SDK/底层不支持并发请求时不得并发调用
- 测试:mock 无响应 TCP 服务器 → OpenTrade 超时返回;**复用测试**:两次调用只建立一次 TCP 连接;断开后重连;verify.sh 全绿

## 实现要点

1. **fork SDK**:复制 `github.com/qtopie/gofutuapi` module 到仓库内(如 `third_party/gofutuapi/`,去 docs/_data 等非必要文件,保留 gen/ 生成代码与源码);go.mod 加 `replace github.com/qtopie/gofutuapi => ./third_party/gofutuapi`
2. **接线 Timeout**:fork 的 `conn.go` initConnectSync 在读 init 响应前 `conn.rw.SetReadDeadline(now + option.Timeout 或默认 10s)`;dial 超时也走 option.Timeout(替代硬编码 5s)
3. **连接复用层**(本任务核心交付,建议放 `internal/futu/`):
   - `TradeClient` 复用管理器:按 addr 缓存单例连接;`Close` 改为归还/引用计数,连接断开(调用返回 EOF/超时)时标记失效,下次调用重连
   - 所有失败路径**重试 ≤ 2 次**:重试前必须确认旧连接已关闭(不能拿半开连接重试)
   - 复用连接互斥串行使用(必要时),防并发破坏协议
   - 4 处调用点(wheel_scheduler.go futuPositions、httpapi/futu_client.go futuAccounter、cmd/wbot/futu.go、telegram_scheduler.go)全部改用复用入口,不再每次新建
   - 迁移期间旧行为(每次新建)删除,不留双路径
4. **测试**:
   - trade_test.go:mock 无响应 TCP 服务器 → OpenTrade 应在 ~10s 返回错误(允许 10-15s 窗口)
   - 复用测试:mock TCP 服务器统计 accept 次数 → 连续两次调用只 accept 1 次;kill 连接后重试可重建
   - 重试测试:mock 服务器先失败后成功 → 总尝试 ≤ 2
   - gateway 不可达(dial 拒绝)→ 快速错误
5. 自测:`scripts/verify.sh` 全绿(注意 go.mod replace 后 verify 构建路径)

## Links

- 上游: doc/tasks/2026-08-11-wheel-data-link.md(数据链路已合入)、PR #337(option-quote flexInt,同批修复的网关字段类型)
- 实测依据: 2026-08-12 09:35-09:40 盘中(网关假死 8h → 泄漏 128 连接 → max connections rejected → EOF;重启 serve 恢复)
- Branch: `fix/opentrade-init-timeout`(worktree `.claude/worktrees/opentrade-init-timeout`)
- 执行者: codex gpt-5.6-luna(2026-08-12;额度尽时退回 Claude coder)

## State

- **status**: `in_progress`(2026-08-12 09:43 用户指令扩展范围:连接管理强化;09:44 重派 codex)
- 执行者: codex gpt-5.6-luna(fork 已完成一次但因范围扩展重派;额度尽时退回 Claude coder)

## Next

- 实现(超时 + 复用 + 重试 ≤2)→ verify.sh 全绿 → reviewer 评审(bugfix)→ 合入 → 下次发布带上(0.2.3)
- 中间态缓解(已做):serve 重启清连接;网关恢复响应时泄漏停止(仅网关无响应期间泄漏)

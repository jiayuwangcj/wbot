## 一、结论与健康度

综合健康度：**中等偏低（约 55/100）**。

| 维度 | 评价 | 结论 |
|---|---:|---|
| 基础代码与测试 | 75/100 | Go 单测较丰富，错误路径和 race 门禁意识较好 |
| 工程进度治理 | 40/100 | 主分支、开发基线、任务记录和 worktree 严重分散 |
| 发布流程 | 50/100 | 已发布语义版本，但脚本仍保留废止的日构建语义 |
| 安全边界 | 45/100 | 默认 loopback 尚可，但生产 compose 暴露无鉴权写面 |
| #37 开发准备度 | 35/100 | 共享管道已形成，但信号边界、幂等和配置模型尚未就绪 |

### 积极项

- `go test ./... -count=1` 通过，最慢包 `cmd/wbot` 约 106 秒。
- `go vet ./...`、`git diff --check` 通过。
- `internal` 主要风险包覆盖较好：`llmreview` 86.1%、`futu` 83.5%、`wheel` 78.8%、`wheelrun` 78.9%。
- 未扫描到私钥、常见 API key/PAT 形态；敏感配置基本遵守 `~/.wbot/` 约定。
- Futu HTTP 客户端有超时、响应体关闭和限流重试；Wheel runner 对资源释放和单 goroutine 所有权有明确约束。
- LLM 审核错误会 fail-closed，方向正确。

---

# 二、不规范清单

## P0

**本轮未发现可确认的 P0。**

没有发现仓库内真实密钥、默认实盘自动下单、已知数据破坏或稳定必现的无确认交易路径。

但下述 P1-1/P1-2 已非常接近交易安全阻断级，必须在 #37 自动调度前完成。

---

## P1

### P1-1：Telegram 与 Discord 之间存在重复下单竞态

位置：

- [telegram_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler.go:403)
- [discord_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/discord_scheduler.go:652)
- [store.go](/home/jiayu/workspace/github/wbot/internal/wheelstore/store.go:789)

问题：

Telegram 路径执行：

1. `HasAction(CONFIRM)`
2. 调用券商 `PlaceOrder`
3. 再 `AppendAction(CONFIRM)`

整个过程无互斥。Discord 虽有进程内 `confirmMu`，但它只保护 Discord scheduler，不能与 Telegram 路径互斥，也不能跨进程保护。

两个渠道同时确认同一信号时，可以都在 CONFIRM 尚未写入时通过检查并分别下单。更严重的是，下单成功但 `AppendAction` 失败后，代码只记日志，下一次点击仍能再次下单。

结论：这是确定性的 check-then-act 竞态，当前数据库审计记录不能作为交易幂等键。

建议：在下单前建立数据库级原子 claim/idempotency 状态；所有渠道调用同一个确认服务。不能仅增加另一个进程内 mutex。

---

### P1-2：LLM 注入端点的库存补全发生在持久化之后，实际不会更新信号

位置：

- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:206)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:223)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:228)
- [store.go](/home/jiayu/workspace/github/wbot/internal/wheelstore/store.go:541)

问题：

`record` 在第 223 行先 `AppendSignal`，账户现金和持仓到第 228 行以后才获取，随后只修改内存中的 `record.Inventory`，没有更新数据库。

同时，`AppendSignal` 又要求 ALERT 在写入时已有完整库存快照。因此：

- 真正省略库存字段的请求会在账户查询前直接失败；
- 注释所称“库存字段可选、缺省用实时账户补全”并不成立；
- 调用方提交的零值或错误库存会被先持久化，之后的真实账户数据只进入审核上下文，不会修正信号审计记录；
- 推送与人工确认读取的仍是错误/陈旧的持久化快照。

这也是已有评审记录中“持仓 0”问题没有在结构上解决的根因。

建议：先采集并验证完整上下文，再一次性构造不可变信号并落库；不要追加后再修改本地结构。

---

### P1-3：自动交易候选的关键硬约束仍主要由第二个 LLM 判断

位置：

- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:39)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:129)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:161)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:183)

代码只确定性检查方向枚举和 `quantity >= 1`。以下约束主要依赖审核模型理解提示词：

- 期权数量不超过 5；
- 股票数量不超过 1000；
- `premium/current_price > 0`；
- expiry 未过期且和合约一致；
- Delta 符号和范围；
- strike/现价数量级；
- 现金担保 Put、备兑 Call、库存和每日限额。

端点甚至能先把零价格 candidate 标成 `Accepted:true`、`CapabilityStatus:READY`，直到人工确认时才因限价不可用被拒。

#37 每 15 分钟自动生成后，这会把模型输出错误持续写入 READY/ALERT 审计流。LLM 审核应是附加防线，而不是确定性业务校验器。

---

### P1-4：缺 expiry 会静默生成固定的 2026-08-21 合约

位置：[llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:313)

`syntheticOptionCode` 在 expiry 不合法或缺失时固定使用 `260821`。该日期很快会成为过期日期，也可能和真实期权链完全无关。

自动调度后，只要生成模型漏字段，就可能构造不存在或过期的合约。该行为必须改为明确拒绝，不能提供日期 fallback。

---

### P1-5：#37 缺少可执行的幂等、状态与恢复契约

位置：

- [review-plan 任务记录](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-review-plan.md:111)
- [llm-prompt-framework 任务记录](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-llm-prompt-framework.md:40)
- [repository.go](/home/jiayu/workspace/github/wbot/internal/wheelstore/repository.go:8)

计划写了“同标的同方向未处置跳过、价格相似去重、沿用每日限额”，但目前：

- 没有运行轮次/run ID；
- 没有决策输入快照 ID；
- 没有 `(symbol, strategy, time bucket)` 唯一约束；
- 没有生成中的 lease/锁；
- 没有推送成功/PUSHED 持久状态；
- scheduler 重启、双实例或超时重试可能重复生成；
- `SignalRepository` 没有按“未处置/策略来源/决策指纹”查询的能力。

不能仅用内存 map 或查询最近一条信号实现金融决策去重。

---

### P1-6：任务队列不具备机器可恢复性

位置：

- [tasks README](/home/jiayu/workspace/github/wbot/doc/tasks/README.md:3)
- [任务模板](/home/jiayu/workspace/github/wbot/doc/tasks/_template.md:20)
- [wheel-full-rewrite](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-10-wheel-full-rewrite.md:27)
- [slice-e](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-11-wheel-live-slice-e.md:37)

共 196 份任务记录中：

- 只有 52 份严格使用模板规定的 `queued|running|blocked|done`；
- 出现 `draft`、`in_progress`、`coding_done`、`reviewed_pending_merge`、`delivered`、`merged`、`verified`、`slice0-complete` 等至少 13 种额外状态；
- 85 份未命中规范的 status 结构；
- 59 份缺少可识别的 Driven-By/trigger；
- 多份任务头部仍为 queued/running，但尾部已经写 delivered；
- `wheel-full-rewrite` 自 8 月 10 日仍标 running，实际多个切片已经交付；
- `slice-e` 同时存在 queued 和 delivered；
- #37 没有独立规范任务记录，主要散落在 review-plan 和未跟踪的 draft 文件中。

这直接违反“按 status 恢复队列”和“每步更新 State/Next”的约定，manager/dispatcher 无法可靠取任务。

---

### P1-7：开发基线与主基线严重分叉，闭环未完成

当前状态：

- 当前分支 `feat/llm-signal-endpoint` 比本地 `main` 多 59 个提交；
- 比 `origin/main` 多 67 个提交；
- 本地 `main` 与远端 `main` 本身也不一致；
- 24 个 worktree、193 个本地分支；
- 多个已经记录 delivered/merged 的 worktree 仍长期保留；
- 当前工作树有未提交 `go.sum`、工具代码、7 份任务文档和 `tg-test` 二进制。

这不符合“候选评审 → 主会话合入决策 → 合入主基线”的迭代闭环。当前更像长期开发集成分支，导致：

- 发布 tag 与开发实际 HEAD 脱节；
- “已合入”究竟指 main、开发基线还是某个 feature 分支不明确；
- worktree 回收和冲突管理失效；
- #37 基于哪个基线开发没有稳定答案。

---

### P1-8：无鉴权写面在生产 compose 中绑定所有接口

位置：

- [docker-compose.serve.yml](/home/jiayu/workspace/github/wbot/configs/docker-compose.serve.yml:23)
- [docker-compose.serve.yml](/home/jiayu/workspace/github/wbot/configs/docker-compose.serve.yml:51)
- [main.go](/home/jiayu/workspace/github/wbot/cmd/wbot/main.go:323)

compose 使用 host 网络并将服务绑定到 `0.0.0.0:8080`。同一个 mux 上包括：

- admin config PUT；
- watchlist/ingest/backtest 写面；
- LLM signal 注入；
- Futu 账户代理；
- Discord webhook。

除 Discord webhook 自身验签外，其他接口未见认证中间件。文档曾把安全模型定义为“默认 loopback，远程访问需认证代理”，但生产 compose 已突破这个前提。

对于 #37，任意能访问端口的客户端均可写入交易候选；虽然仍需 LLM 和人工确认，但属于不必要的高风险输入面。

---

### P1-9：发布脚本与正式发布规则冲突，且关键 git 失败被吞掉

位置：

- [release.sh](/home/jiayu/workspace/github/wbot/scripts/release.sh:20)
- [release.sh](/home/jiayu/workspace/github/wbot/scripts/release.sh:200)
- [release.sh](/home/jiayu/workspace/github/wbot/scripts/release.sh:225)
- [RELEASE_DAILY.md](/home/jiayu/workspace/github/wbot/doc/RELEASE_DAILY.md:3)

规范已废除日构建，但脚本仍：

- 宣传和支持 `daily-*`；
- 保留会删除远端 Release/tag 的 `republish`；
- 对 `git tag` 和 `git push` 使用 `|| true`；
- publish 不校验当前提交属于 main、工作树干净或 CI 绿色；
- tag 创建/推送失败后仍可能继续创建 Release。

这会产生“Release 资产、tag、开发 HEAD 不一致”的审计风险。当前虽然已有 `v0.2.0`～`v0.2.2`，但最新开发分支比 `origin/main` 多 67 提交，发布批次边界并不清晰。

---

## P2

### P2-1：任务文档含残留冲突标记

位置：[slice-a-shared-pipeline.md](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-slice-a-shared-pipeline.md:37)

存在孤立的 `>>>>>>> feat/shared-pipeline`。说明合入后的文档质量检查未覆盖冲突标记。

---

### P2-2：本地 verify 与 CI 并不等价

位置：

- [verify.sh](/home/jiayu/workspace/github/wbot/scripts/verify.sh:1)
- [ci.yml](/home/jiayu/workspace/github/wbot/.github/workflows/ci.yml:70)

`verify.sh` 声称等价 CI，但前端阶段只做 `npm ci`、类型检查和 build，没有执行 CI 中的 `npm run test`。该欠账其实已在 8 月 11 日任务记录中识别，但尚未处理。

此外，完整 `go test ./...` 中有一个真实等待 105 秒的测试，导致普通测试和 race 测试均被无谓拖慢。

---

### P2-3：测试把生产轮询时间写死，单测耗时 105 秒

位置：

- [telegram_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler.go:480)
- [telegram_scheduler_test.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler_test.go:883)

`TestWatchFillWindowClosePushesPending` 真等 7 次 × 15 秒，单测耗时 105.02 秒。轮询时钟未注入，使全包、race、CI 和本地验证都被拖慢。

建议注入 ticker/sleep/clock，测试使用虚拟时间。

---

### P2-4：HTTP 服务和 scheduler 日志不可结构化检索

大量代码直接使用 `fmt.Fprintf(os.Stderr, "...")`，例如：

- [runner.go](/home/jiayu/workspace/github/wbot/internal/wheelrun/runner.go:120)
- [main.go](/home/jiayu/workspace/github/wbot/cmd/wbot/main.go:318)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:240)

缺少统一：

- level；
- component；
- request/run/signal ID；
- latency；
- outcome/retry 分类；
- LLM model/prompt version；
- scheduler pass 汇总指标。

#37 每 15 分钟运行后，很难区分“未运行、无建议、被确定性校验拒绝、生成失败、审核失败、重复抑制、推送失败”。

---

### P2-5：健康检查只证明数据库可 Ping

位置：

- [httpapi.go](/home/jiayu/workspace/github/wbot/internal/httpapi/httpapi.go:220)
- [docker-compose.serve.yml](/home/jiayu/workspace/github/wbot/configs/docker-compose.serve.yml:53)

`/v1/health` 只检查 PostgreSQL。即使 Futu、生成模型、审核模型、scheduler goroutine、Telegram/Discord 全部失效，容器仍为 healthy。

#37 至少需要额外 readiness/diagnostic 状态：last_started、last_success、last_error、next_run、consecutive_failures、生成/审核依赖状态。

---

### P2-6：strategy 注册表与 LLM 策略计划不一致

位置：

- [strategy.go](/home/jiayu/workspace/github/wbot/internal/strategy/strategy.go:1)
- [strategy.go](/home/jiayu/workspace/github/wbot/internal/strategy/strategy.go:42)
- [llm-prompt-framework.md](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-llm-prompt-framework.md:17)

注册表和 watchlist 契约明确只接受 `wheel`；任务文档一处说 #36 应加入 `llm` 模板，另一处的实际 #36 决策又说“不硬抽统一 Strategy 接口”。

必须先确定：

- LLM 是 watchlist 的正式 strategy 值；
- 还是 wheel binding 上的第二个决策器；
- 或独立 binding/runner。

当前模型无法表达“哪些 symbol 运行 LLM、对应参数和 prompt version”，scheduler 容易退化为扫描全部 wheel binding。

---

### P2-7：LLM 注入接口和主文档没有同步

`cmd/wbot/main.go` 已注册 `/v1/wheel/llm-signal`，但 `doc/API.md`、`doc/WHEEL_STRATEGY.md` 和 serve help 均未完整登记该端点契约。serve help 还写着 Telegram yes 会下“market order”，而实际已强制限价单。

这违反 API、代码与运维文档一致性要求。

---

### P2-8：迁移编号不唯一

位置：[migrations](/home/jiayu/workspace/github/wbot/internal/db/migrations)

同时存在：

- `004_account_snapshots.sql`
- `004_backtest_results_curve.sql`

当前实现按完整文件名字典序记录，技术上可执行，但编号不再代表唯一演进顺序，后续 cherry-pick、排障和人工审计容易误解。

---

### P2-9：持久化推送状态缺失，重启窗口仍可能丢信号

位置：

- [telegram_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler.go:196)
- [discord_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/discord_scheduler.go:144)

两条推送循环启动时都把 cursor 初始化到当前最大 signal ID，已存在但审核尚未完成的信号会在重启后永久跳过。handled 集合也只是内存态。

任务记录已承认该 backlog，但 #37 会显著增加信号与审核重叠窗口，应该升级为前置项。

---

### P2-10：覆盖率虽不低，但关键持久化层本地默认覆盖不足

本次无 PG 环境的覆盖结果：

- `internal/db`：0%
- `internal/watchlist`：7.5%
- `internal/wheelstore`：35.5%
- `internal/ingest`：43.7%

CI 有 PostgreSQL 集成 job，是积极项；但本地默认 `go test ./...` 会跳过大量持久化行为。#37 的去重、lease、幂等和恢复逻辑必须进入 CI 的真实 PG race 测试，不能只用 fake store。

---

# 三、LLM 策略开发前必须先解决

按阻断顺序建议如下：

1. **交易确认幂等化**
   - 建立数据库原子 claim；
   - Telegram/Discord 共用单一确认服务；
   - 下单成功但审计落库失败时有可恢复状态；
   - 加跨渠道并发和双实例 PG race 验收。

2. **重构信号创建边界**
   - 行情、账户、库存、策略配置先完整采集；
   - 确定性校验通过后一次性写入不可变 SignalRecord；
   - 删除“落库后补内存字段”的路径；
   - 保证审核、推送、审计读取同一份快照。

3. **实现确定性 LLM 输出校验器**
   - JSON schema；
   - 数量、价格、expiry、合约、Delta、现金、库存、每日限额；
   - 禁止固定 expiry fallback；
   - 不合格输出写为明确的 generation rejection，不能写 READY/ALERT。

4. **明确 strategy 配置模型**
   - 决定 `llm` 是否是正式 watchlist strategy；
   - 定义 symbol、interval、生成模型、审核模型、prompt version、数量限制；
   - 统一 `strategy`、watchlist、serve 和文档契约。

5. **设计持久化调度状态和幂等键**
   - run ID、time bucket、input snapshot ID、decision fingerprint；
   - pending/running/succeeded/failed/skipped；
   - 单实例 lease 或数据库 advisory lock；
   - 重启恢复和重复 tick 测试。

6. **持久化推送状态**
   - 明确 `PUSHED/REJECT_NOTIFIED` 等状态；
   - 重启不丢审核中的信号；
   - Telegram 与 Discord 各自记录投递结果，避免一个成功掩盖另一个失败。

7. **收敛工程基线**
   - 把已评审功能按顺序真正合入 main；
   - 清理已交付 worktree；
   - 提交或隔离当前脏工作树；
   - 为 #37 创建唯一、规范的任务记录和独立 worktree；
   - 明确基于哪个 main commit 开发。

8. **补可观测与验收**
   - scheduler last-run/next-run/error/counter；
   - fake generation + fake review + PG + 两种通知渠道端到端验收；
   - 市场关闭、模型超时、DB 重启、serve 重启、双实例、重复输出场景；
   - 将前端测试补回本地 verify。

## 最终判断

#37 当前可以进入设计和测试用例阶段，**不建议直接开始 ticker/模型调用实现**。最大的风险不在“如何每 15 分钟调用 DeepSeek”，而在现有信号持久化、确定性校验、交易确认幂等和运行状态恢复尚未形成可靠边界。若先加 scheduler，这些缺陷会从人工偶发路径升级为周期性放大问题。
tokens used
385,178
# wbot 工程全面 Review 报告

审查基线：2026-08-13 当前工作树 `feat/llm-signal-endpoint`，HEAD `f99efd2`。仅做只读评估，未修改文件。

## 一、结论与健康度

综合健康度：**中等偏低（约 55/100）**。

| 维度 | 评价 | 结论 |
|---|---:|---|
| 基础代码与测试 | 75/100 | Go 单测较丰富，错误路径和 race 门禁意识较好 |
| 工程进度治理 | 40/100 | 主分支、开发基线、任务记录和 worktree 严重分散 |
| 发布流程 | 50/100 | 已发布语义版本，但脚本仍保留废止的日构建语义 |
| 安全边界 | 45/100 | 默认 loopback 尚可，但生产 compose 暴露无鉴权写面 |
| #37 开发准备度 | 35/100 | 共享管道已形成，但信号边界、幂等和配置模型尚未就绪 |

### 积极项

- `go test ./... -count=1` 通过，最慢包 `cmd/wbot` 约 106 秒。
- `go vet ./...`、`git diff --check` 通过。
- `internal` 主要风险包覆盖较好：`llmreview` 86.1%、`futu` 83.5%、`wheel` 78.8%、`wheelrun` 78.9%。
- 未扫描到私钥、常见 API key/PAT 形态；敏感配置基本遵守 `~/.wbot/` 约定。
- Futu HTTP 客户端有超时、响应体关闭和限流重试；Wheel runner 对资源释放和单 goroutine 所有权有明确约束。
- LLM 审核错误会 fail-closed，方向正确。

---

# 二、不规范清单

## P0

**本轮未发现可确认的 P0。**

没有发现仓库内真实密钥、默认实盘自动下单、已知数据破坏或稳定必现的无确认交易路径。

但下述 P1-1/P1-2 已非常接近交易安全阻断级，必须在 #37 自动调度前完成。

---

## P1

### P1-1：Telegram 与 Discord 之间存在重复下单竞态

位置：

- [telegram_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler.go:403)
- [discord_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/discord_scheduler.go:652)
- [store.go](/home/jiayu/workspace/github/wbot/internal/wheelstore/store.go:789)

问题：

Telegram 路径执行：

1. `HasAction(CONFIRM)`
2. 调用券商 `PlaceOrder`
3. 再 `AppendAction(CONFIRM)`

整个过程无互斥。Discord 虽有进程内 `confirmMu`，但它只保护 Discord scheduler，不能与 Telegram 路径互斥，也不能跨进程保护。

两个渠道同时确认同一信号时，可以都在 CONFIRM 尚未写入时通过检查并分别下单。更严重的是，下单成功但 `AppendAction` 失败后，代码只记日志，下一次点击仍能再次下单。

结论：这是确定性的 check-then-act 竞态，当前数据库审计记录不能作为交易幂等键。

建议：在下单前建立数据库级原子 claim/idempotency 状态；所有渠道调用同一个确认服务。不能仅增加另一个进程内 mutex。

---

### P1-2：LLM 注入端点的库存补全发生在持久化之后，实际不会更新信号

位置：

- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:206)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:223)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:228)
- [store.go](/home/jiayu/workspace/github/wbot/internal/wheelstore/store.go:541)

问题：

`record` 在第 223 行先 `AppendSignal`，账户现金和持仓到第 228 行以后才获取，随后只修改内存中的 `record.Inventory`，没有更新数据库。

同时，`AppendSignal` 又要求 ALERT 在写入时已有完整库存快照。因此：

- 真正省略库存字段的请求会在账户查询前直接失败；
- 注释所称“库存字段可选、缺省用实时账户补全”并不成立；
- 调用方提交的零值或错误库存会被先持久化，之后的真实账户数据只进入审核上下文，不会修正信号审计记录；
- 推送与人工确认读取的仍是错误/陈旧的持久化快照。

这也是已有评审记录中“持仓 0”问题没有在结构上解决的根因。

建议：先采集并验证完整上下文，再一次性构造不可变信号并落库；不要追加后再修改本地结构。

---

### P1-3：自动交易候选的关键硬约束仍主要由第二个 LLM 判断

位置：

- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:39)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:129)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:161)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:183)

代码只确定性检查方向枚举和 `quantity >= 1`。以下约束主要依赖审核模型理解提示词：

- 期权数量不超过 5；
- 股票数量不超过 1000；
- `premium/current_price > 0`；
- expiry 未过期且和合约一致；
- Delta 符号和范围；
- strike/现价数量级；
- 现金担保 Put、备兑 Call、库存和每日限额。

端点甚至能先把零价格 candidate 标成 `Accepted:true`、`CapabilityStatus:READY`，直到人工确认时才因限价不可用被拒。

#37 每 15 分钟自动生成后，这会把模型输出错误持续写入 READY/ALERT 审计流。LLM 审核应是附加防线，而不是确定性业务校验器。

---

### P1-4：缺 expiry 会静默生成固定的 2026-08-21 合约

位置：[llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:313)

`syntheticOptionCode` 在 expiry 不合法或缺失时固定使用 `260821`。该日期很快会成为过期日期，也可能和真实期权链完全无关。

自动调度后，只要生成模型漏字段，就可能构造不存在或过期的合约。该行为必须改为明确拒绝，不能提供日期 fallback。

---

### P1-5：#37 缺少可执行的幂等、状态与恢复契约

位置：

- [review-plan 任务记录](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-review-plan.md:111)
- [llm-prompt-framework 任务记录](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-llm-prompt-framework.md:40)
- [repository.go](/home/jiayu/workspace/github/wbot/internal/wheelstore/repository.go:8)

计划写了“同标的同方向未处置跳过、价格相似去重、沿用每日限额”，但目前：

- 没有运行轮次/run ID；
- 没有决策输入快照 ID；
- 没有 `(symbol, strategy, time bucket)` 唯一约束；
- 没有生成中的 lease/锁；
- 没有推送成功/PUSHED 持久状态；
- scheduler 重启、双实例或超时重试可能重复生成；
- `SignalRepository` 没有按“未处置/策略来源/决策指纹”查询的能力。

不能仅用内存 map 或查询最近一条信号实现金融决策去重。

---

### P1-6：任务队列不具备机器可恢复性

位置：

- [tasks README](/home/jiayu/workspace/github/wbot/doc/tasks/README.md:3)
- [任务模板](/home/jiayu/workspace/github/wbot/doc/tasks/_template.md:20)
- [wheel-full-rewrite](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-10-wheel-full-rewrite.md:27)
- [slice-e](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-11-wheel-live-slice-e.md:37)

共 196 份任务记录中：

- 只有 52 份严格使用模板规定的 `queued|running|blocked|done`；
- 出现 `draft`、`in_progress`、`coding_done`、`reviewed_pending_merge`、`delivered`、`merged`、`verified`、`slice0-complete` 等至少 13 种额外状态；
- 85 份未命中规范的 status 结构；
- 59 份缺少可识别的 Driven-By/trigger；
- 多份任务头部仍为 queued/running，但尾部已经写 delivered；
- `wheel-full-rewrite` 自 8 月 10 日仍标 running，实际多个切片已经交付；
- `slice-e` 同时存在 queued 和 delivered；
- #37 没有独立规范任务记录，主要散落在 review-plan 和未跟踪的 draft 文件中。

这直接违反“按 status 恢复队列”和“每步更新 State/Next”的约定，manager/dispatcher 无法可靠取任务。

---

### P1-7：开发基线与主基线严重分叉，闭环未完成

当前状态：

- 当前分支 `feat/llm-signal-endpoint` 比本地 `main` 多 59 个提交；
- 比 `origin/main` 多 67 个提交；
- 本地 `main` 与远端 `main` 本身也不一致；
- 24 个 worktree、193 个本地分支；
- 多个已经记录 delivered/merged 的 worktree 仍长期保留；
- 当前工作树有未提交 `go.sum`、工具代码、7 份任务文档和 `tg-test` 二进制。

这不符合“候选评审 → 主会话合入决策 → 合入主基线”的迭代闭环。当前更像长期开发集成分支，导致：

- 发布 tag 与开发实际 HEAD 脱节；
- “已合入”究竟指 main、开发基线还是某个 feature 分支不明确；
- worktree 回收和冲突管理失效；
- #37 基于哪个基线开发没有稳定答案。

---

### P1-8：无鉴权写面在生产 compose 中绑定所有接口

位置：

- [docker-compose.serve.yml](/home/jiayu/workspace/github/wbot/configs/docker-compose.serve.yml:23)
- [docker-compose.serve.yml](/home/jiayu/workspace/github/wbot/configs/docker-compose.serve.yml:51)
- [main.go](/home/jiayu/workspace/github/wbot/cmd/wbot/main.go:323)

compose 使用 host 网络并将服务绑定到 `0.0.0.0:8080`。同一个 mux 上包括：

- admin config PUT；
- watchlist/ingest/backtest 写面；
- LLM signal 注入；
- Futu 账户代理；
- Discord webhook。

除 Discord webhook 自身验签外，其他接口未见认证中间件。文档曾把安全模型定义为“默认 loopback，远程访问需认证代理”，但生产 compose 已突破这个前提。

对于 #37，任意能访问端口的客户端均可写入交易候选；虽然仍需 LLM 和人工确认，但属于不必要的高风险输入面。

---

### P1-9：发布脚本与正式发布规则冲突，且关键 git 失败被吞掉

位置：

- [release.sh](/home/jiayu/workspace/github/wbot/scripts/release.sh:20)
- [release.sh](/home/jiayu/workspace/github/wbot/scripts/release.sh:200)
- [release.sh](/home/jiayu/workspace/github/wbot/scripts/release.sh:225)
- [RELEASE_DAILY.md](/home/jiayu/workspace/github/wbot/doc/RELEASE_DAILY.md:3)

规范已废除日构建，但脚本仍：

- 宣传和支持 `daily-*`；
- 保留会删除远端 Release/tag 的 `republish`；
- 对 `git tag` 和 `git push` 使用 `|| true`；
- publish 不校验当前提交属于 main、工作树干净或 CI 绿色；
- tag 创建/推送失败后仍可能继续创建 Release。

这会产生“Release 资产、tag、开发 HEAD 不一致”的审计风险。当前虽然已有 `v0.2.0`～`v0.2.2`，但最新开发分支比 `origin/main` 多 67 提交，发布批次边界并不清晰。

---

## P2

### P2-1：任务文档含残留冲突标记

位置：[slice-a-shared-pipeline.md](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-slice-a-shared-pipeline.md:37)

存在孤立的 `>>>>>>> feat/shared-pipeline`。说明合入后的文档质量检查未覆盖冲突标记。

---

### P2-2：本地 verify 与 CI 并不等价

位置：

- [verify.sh](/home/jiayu/workspace/github/wbot/scripts/verify.sh:1)
- [ci.yml](/home/jiayu/workspace/github/wbot/.github/workflows/ci.yml:70)

`verify.sh` 声称等价 CI，但前端阶段只做 `npm ci`、类型检查和 build，没有执行 CI 中的 `npm run test`。该欠账其实已在 8 月 11 日任务记录中识别，但尚未处理。

此外，完整 `go test ./...` 中有一个真实等待 105 秒的测试，导致普通测试和 race 测试均被无谓拖慢。

---

### P2-3：测试把生产轮询时间写死，单测耗时 105 秒

位置：

- [telegram_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler.go:480)
- [telegram_scheduler_test.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler_test.go:883)

`TestWatchFillWindowClosePushesPending` 真等 7 次 × 15 秒，单测耗时 105.02 秒。轮询时钟未注入，使全包、race、CI 和本地验证都被拖慢。

建议注入 ticker/sleep/clock，测试使用虚拟时间。

---

### P2-4：HTTP 服务和 scheduler 日志不可结构化检索

大量代码直接使用 `fmt.Fprintf(os.Stderr, "...")`，例如：

- [runner.go](/home/jiayu/workspace/github/wbot/internal/wheelrun/runner.go:120)
- [main.go](/home/jiayu/workspace/github/wbot/cmd/wbot/main.go:318)
- [llm_signal.go](/home/jiayu/workspace/github/wbot/internal/httpapi/llm_signal.go:240)

缺少统一：

- level；
- component；
- request/run/signal ID；
- latency；
- outcome/retry 分类；
- LLM model/prompt version；
- scheduler pass 汇总指标。

#37 每 15 分钟运行后，很难区分“未运行、无建议、被确定性校验拒绝、生成失败、审核失败、重复抑制、推送失败”。

---

### P2-5：健康检查只证明数据库可 Ping

位置：

- [httpapi.go](/home/jiayu/workspace/github/wbot/internal/httpapi/httpapi.go:220)
- [docker-compose.serve.yml](/home/jiayu/workspace/github/wbot/configs/docker-compose.serve.yml:53)

`/v1/health` 只检查 PostgreSQL。即使 Futu、生成模型、审核模型、scheduler goroutine、Telegram/Discord 全部失效，容器仍为 healthy。

#37 至少需要额外 readiness/diagnostic 状态：last_started、last_success、last_error、next_run、consecutive_failures、生成/审核依赖状态。

---

### P2-6：strategy 注册表与 LLM 策略计划不一致

位置：

- [strategy.go](/home/jiayu/workspace/github/wbot/internal/strategy/strategy.go:1)
- [strategy.go](/home/jiayu/workspace/github/wbot/internal/strategy/strategy.go:42)
- [llm-prompt-framework.md](/home/jiayu/workspace/github/wbot/doc/tasks/2026-08-12-llm-prompt-framework.md:17)

注册表和 watchlist 契约明确只接受 `wheel`；任务文档一处说 #36 应加入 `llm` 模板，另一处的实际 #36 决策又说“不硬抽统一 Strategy 接口”。

必须先确定：

- LLM 是 watchlist 的正式 strategy 值；
- 还是 wheel binding 上的第二个决策器；
- 或独立 binding/runner。

当前模型无法表达“哪些 symbol 运行 LLM、对应参数和 prompt version”，scheduler 容易退化为扫描全部 wheel binding。

---

### P2-7：LLM 注入接口和主文档没有同步

`cmd/wbot/main.go` 已注册 `/v1/wheel/llm-signal`，但 `doc/API.md`、`doc/WHEEL_STRATEGY.md` 和 serve help 均未完整登记该端点契约。serve help 还写着 Telegram yes 会下“market order”，而实际已强制限价单。

这违反 API、代码与运维文档一致性要求。

---

### P2-8：迁移编号不唯一

位置：[migrations](/home/jiayu/workspace/github/wbot/internal/db/migrations)

同时存在：

- `004_account_snapshots.sql`
- `004_backtest_results_curve.sql`

当前实现按完整文件名字典序记录，技术上可执行，但编号不再代表唯一演进顺序，后续 cherry-pick、排障和人工审计容易误解。

---

### P2-9：持久化推送状态缺失，重启窗口仍可能丢信号

位置：

- [telegram_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/telegram_scheduler.go:196)
- [discord_scheduler.go](/home/jiayu/workspace/github/wbot/cmd/wbot/discord_scheduler.go:144)

两条推送循环启动时都把 cursor 初始化到当前最大 signal ID，已存在但审核尚未完成的信号会在重启后永久跳过。handled 集合也只是内存态。

任务记录已承认该 backlog，但 #37 会显著增加信号与审核重叠窗口，应该升级为前置项。

---

### P2-10：覆盖率虽不低，但关键持久化层本地默认覆盖不足

本次无 PG 环境的覆盖结果：

- `internal/db`：0%
- `internal/watchlist`：7.5%
- `internal/wheelstore`：35.5%
- `internal/ingest`：43.7%

CI 有 PostgreSQL 集成 job，是积极项；但本地默认 `go test ./...` 会跳过大量持久化行为。#37 的去重、lease、幂等和恢复逻辑必须进入 CI 的真实 PG race 测试，不能只用 fake store。

---

# 三、LLM 策略开发前必须先解决

按阻断顺序建议如下：

1. **交易确认幂等化**
   - 建立数据库原子 claim；
   - Telegram/Discord 共用单一确认服务；
   - 下单成功但审计落库失败时有可恢复状态；
   - 加跨渠道并发和双实例 PG race 验收。

2. **重构信号创建边界**
   - 行情、账户、库存、策略配置先完整采集；
   - 确定性校验通过后一次性写入不可变 SignalRecord；
   - 删除“落库后补内存字段”的路径；
   - 保证审核、推送、审计读取同一份快照。

3. **实现确定性 LLM 输出校验器**
   - JSON schema；
   - 数量、价格、expiry、合约、Delta、现金、库存、每日限额；
   - 禁止固定 expiry fallback；
   - 不合格输出写为明确的 generation rejection，不能写 READY/ALERT。

4. **明确 strategy 配置模型**
   - 决定 `llm` 是否是正式 watchlist strategy；
   - 定义 symbol、interval、生成模型、审核模型、prompt version、数量限制；
   - 统一 `strategy`、watchlist、serve 和文档契约。

5. **设计持久化调度状态和幂等键**
   - run ID、time bucket、input snapshot ID、decision fingerprint；
   - pending/running/succeeded/failed/skipped；
   - 单实例 lease 或数据库 advisory lock；
   - 重启恢复和重复 tick 测试。

6. **持久化推送状态**
   - 明确 `PUSHED/REJECT_NOTIFIED` 等状态；
   - 重启不丢审核中的信号；
   - Telegram 与 Discord 各自记录投递结果，避免一个成功掩盖另一个失败。

7. **收敛工程基线**
   - 把已评审功能按顺序真正合入 main；
   - 清理已交付 worktree；
   - 提交或隔离当前脏工作树；
   - 为 #37 创建唯一、规范的任务记录和独立 worktree；
   - 明确基于哪个 main commit 开发。

8. **补可观测与验收**
   - scheduler last-run/next-run/error/counter；
   - fake generation + fake review + PG + 两种通知渠道端到端验收；
   - 市场关闭、模型超时、DB 重启、serve 重启、双实例、重复输出场景；
   - 将前端测试补回本地 verify。

## 最终判断

#37 当前可以进入设计和测试用例阶段，**不建议直接开始 ticker/模型调用实现**。最大的风险不在“如何每 15 分钟调用 DeepSeek”，而在现有信号持久化、确定性校验、交易确认幂等和运行状态恢复尚未形成可靠边界。若先加 scheduler，这些缺陷会从人工偶发路径升级为周期性放大问题。

# Wheel 实时运行切片 G:LLM 审核闸门实时接线

- **id**: `2026-08-11-wheel-live-slice-g`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

切片 E 评审 P1-1 修复(横跨 D/C 的交付缺口):**runner 实时链路接线 LLM 审核闸门**。当前状态:切片 D 只交付了 `internal/llmreview` 客户端(Review 函数)+ wheelstore.AppendAction,但全仓库**无任何代码写入 LLM_REVIEW action**——runner(切片 C)不调 Review 也不 AppendAction;E 的推送闸门(`LatestLLMReview` verdict=APPROVE)因此永远失败,提醒/处置闭环在实况不可达。

接线语义(对齐 plan 切片 D 闸门契约):
- runner 对每个 ALERT 信号,在 `AppendSignal` 后调用 `llmreview.Review`(输入:策略配置/信号/持仓/资金/**产品文字规则**),verdict APPROVE → `AppendAction(LLM_REVIEW, approved)`;REJECT → 落 REJECTED 记录;审核失败(API 不可用)→ **fail-closed** 记录原因,不推送不处置
- 端点 env 可配:`LLM_BASE_URL`/`LLM_API_KEY`/`LLM_MODEL`(切片 D 已定义);LLM key 只从环境变量读,不落库不打印
- E 的推送闸门(telegram_scheduler.go LatestLLMReview)原样消费,不改 telegram 侧

## Constraints

- **不碰**切片 E 文件:`internal/telegram/`、`cmd/wbot/telegram_scheduler.go`、webui;E 的推送闸门是消费方,保持原样
- 只动:`internal/wheelrun/runner.go`(接线点,加 LLMReview 依赖注入接口)+ `cmd/wbot/wheel_scheduler.go`(组装真实 llmreview 客户端,env 读取)+ 各自测试
- 依赖注入:`runner.Dependencies` 加接口(如 `LLMReviewer`),fake 可注入;真实客户端只在 cmd/wbot 组装,env 缺失时该依赖可降级(不接线但记录,不 panic)
- 审核是**硬闸门**:REJECT/失败 → 该信号不进入推送/处置(不依赖 E 存在与否,闸门语义本身完整)
- 遵守 self-documenting-code(注释 ≤1 行)

## Links

- Driven-By: 切片 E 评审 P1-1(推送闸门死路,主会话合入决策:独立切片接线)
- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md` 切片 D 闸门语义 + 切片 E 推送闸门
- Depends-On: 切片 E 修复合入后派单(接线不动 E 文件,但合入顺序 E 先);切片 F 的 accept-wheel-live.sh「fake LLM 端点 env 验证审核记录存在」依赖本切片
- Branch: `feat/slice-g-llm-wiring`(worktree `.claude/worktrees/slice-g-llm-wiring`)

## State

- **status**: `delivered`(已合入基线)
- **last step**: codex CLI(gpt-5.6-luna, max 思考)实现并提交 `7260642`(4 文件 +381):runner.Dependencies 注入 LLMReviewer 接口 + ALERT 后审核接线(APPROVE→LLM_REVIEW / REJECT→REJECTED / 失败 fail-closed / env 缺失降级 nil 不 panic)+ wheel_scheduler env 组装;测试 TestRunOnceLLMGateStates / TestRunOnceAlertReady / TestRunOnceLLMReviewVisibleToLatestLLMReview / TestLLMReviewerFromEnv;自测全绿(build/vet/test/verify.sh/PG 集成)

## 评审结论(2026-08-11,reviewer 有条件批准)

- **结论**:有条件合入;达到可使用阶段;功能类型 **feature**(新行为接线;含切片 E P1-1 推送闸门死路修复成分,按主体判 feature)
- 无 P0;fail-closed/降级/密钥安全/API 兼容(唯一构造点已同步)全部通过;端到端实测(ALERT→fake LLM→APPROVE→LatestLLMReview 可见)
- **P1(合入条件,已修)**:ci.yml db-integration 包列表缺 `./internal/wheelrun/...` → PG 集成测 CI 静默 skip;合入时主会话补行 `0db90db`
- **P2(排期)**:① wheelrun 日志无等级(error/warn/info 规范化)② LLM gate env 三变量缺部署文档 + serve 启动 reviewer nil 无 warn(可并入切片 F 文档段)③ 审核同步阻塞单趟 pass(N×10s),评估并发/独立队列
- **P3**:wheelReviewRules 规则摘要常量与 WHEEL_STRATEGY.md 漂移(建议单一来源);cash_available 恒 nil(资金维度审核不参与,观察);dailyOrders 是否剔除 REJECTED 计数(产品侧确认)

## 合入记录(2026-08-11)

- merge `6d6993f`(Merge branch 'feat/slice-g-llm-wiring',无冲突)+ P1 修复 `0db90db`;合入后 go build/vet + wheelrun/cmd/wbot 测试全绿
- **status: delivered**;切片 F 依赖已解锁(accept-wheel-live.sh 可验证 LLM 审核记录真实存在)

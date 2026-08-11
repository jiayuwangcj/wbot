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

- **status**: `queued`
- **last step**: 主会话收 E 评审报告后决策:LLM 闸门实时接线排独立切片(动切片 C 的 runner.go,不与 E 修复轮混);评审确认 `internal/llmreview` 无任何导入方、wheelrun/wheel_scheduler 无 AppendAction/llmreview 引用(grep 验证)

## Next

切片 E 修复轮合入后:在 worktree `.claude/worktrees/slice-g-llm-wiring`(branch `feat/slice-g-llm-wiring`,基于最新合入 HEAD)实现接线 + 单测(fake LLM 端点,REJECT/失败/APPROVE 三态;PG 集成验证 LatestLLMReview 被 runner 真实写入)→ `scripts/verify.sh` 等价自测 → 独立分支提交(push)→ 报告改动文件/测试结果/遗留问题。

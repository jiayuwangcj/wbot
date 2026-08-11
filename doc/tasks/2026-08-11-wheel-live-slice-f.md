# Wheel 实时运行切片 F:CLI 默认参数档 + 验收 + 文档

- **id**: `2026-08-11-wheel-live-slice-f`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

① CLI 默认档:`watchlist add` wheel 缺 `price_position_curve`/`max_inventory` 时,拉现价生成默认曲线 `[{0.8×P,100},{1.2×P,0}]`,max_inventory=100,其余字段用模板默认;HTTP PUT 保持严格 400(不放松)。② 新 `scripts/accept-wheel-live.sh [bin] [dsn] [base-url]`:自己起临时 serve(`--wheel-run --wheel-interval 10s`,LLM/Telegram 用 fake 端点 env)→ 绑定 wheel → 轮询 wheel_signals 有行(90s)→ 信号 capability ∈ {READY,DATA_BLOCKED} 合法 → watchlist status 同步 → LLM 审核记录存在(fake LLM)→ dismiss 当日静默生效 → 清理;连跑两遍。③ **核对期权报价字段路径(评审 P1-1 显式验收步骤)**:futu 网关恢复后,对 1 个期权合约 `curl /api/quote` dump 原始响应,diff 键名与 `internal/futu/option_quote.go` fixture(bid_price/ask_price/last_price + ex_data),不一致则改 struct + fixture 后重跑测试,并把核对结果记入 task ledger(核对前实时运行器可能 DATA_BLOCKED,运行器诊断日志 requested/answered/bidask_zero 定位形态不匹配)。④ 对账:ACCEPTANCE.md 13→14 脚本、156→新和(逐脚本 `grep -c 'check "'` 实计);文档同步 doc/WHEEL_STRATEGY.md(实时链路+LLM 闸门+Telegram 处置)、doc/API.md(serve flags/新表/动作类型/**动作词表补 LLM_REVIEW(评审 P2,doc/API.md:121)**)、task ledger。

## Constraints

- **不碰**已交付切片的既有逻辑(只在新基线之上加);改 CLI 只动 `cmd/wbot/` 的 watchlist 命令与模板默认值来源。
- HTTP PUT /v1/watchlist/{symbol} 校验保持严格(缺失 required 字段 → 400),默认档只存在于 CLI 侧。
- accept-wheel-live.sh 遵守运维纪律:自清理(symbol/进程)、`check()`/`count()` helper、连跑两遍全过、PID 唯一 symbol 防同秒复用、不进真实网关依赖(fake quoter env 或网关不可用时跑 DATA_BLOCKED 路径)。
- 遵守 self-documenting-code(注释 ≤1 行)。

## Links

- Driven-By: 用户指令 2026-08-11「wheel 策略先实际应用到 futu 模拟盘运行起来,按默认参数即可」(CLI 基于现价生成默认曲线)
- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md` 切片 F
- Branch: `feat/slice-f-default-cli`(worktree `.claude/worktrees/slice-f-default-cli`)
- Depends-On: 切片 B/C/D/E 全部合入

## State

- **status**: `queued`
- **last step**: 主会话已确认 CLI `watchlist add` 现有行为与模板默认值位置;accept 脚本纪律(逐脚本 grep -c 实计、矩阵求和、总表)在 doc/ACCEPTANCE.md 与 memory 双链

## Next

B/C/D/E 全部合入后:在 worktree `.claude/worktrees/slice-f-default-cli`(branch `feat/slice-f-default-cli`,基于最新合入 HEAD)实现 CLI 默认档 + accept-wheel-live.sh + 对账/文档同步 → `scripts/verify.sh` 等价自测 + accept-wheel-live 连跑两遍 → 独立分支提交(push)→ 报告改动文件/测试结果/遗留问题。

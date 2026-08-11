# Wheel 实时运行切片 F:CLI 默认参数档 + 验收 + 文档

- **id**: `2026-08-11-wheel-live-slice-f`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

① CLI 默认档:`watchlist add` wheel 缺 `price_position_curve`/`max_inventory` 时,拉现价生成默认曲线 `[{0.8×P,100},{1.2×P,0}]`,max_inventory=100,其余字段用模板默认;HTTP PUT 保持严格 400(不放松)。② 新 `scripts/accept-wheel-live.sh [bin] [dsn] [base-url]`:自己起临时 serve(`--wheel-run --wheel-interval 10s`,LLM/Telegram 用 fake 端点 env)→ 绑定 wheel → 轮询 wheel_signals 有行(90s)→ 信号 capability ∈ {READY,DATA_BLOCKED} 合法 → watchlist status 同步 → LLM 审核记录存在(fake LLM)→ dismiss 当日静默生效 → 清理;连跑两遍。③ **核对期权报价字段路径(评审 P1-1 显式验收步骤)**:futu 网关恢复后,对 1 个期权合约 `curl /api/quote` dump 原始响应,diff 键名与 `internal/futu/option_quote.go` fixture(bid_price/ask_price/last_price + ex_data),不一致则改 struct + fixture 后重跑测试,并把核对结果记入 task ledger(核对前实时运行器可能 DATA_BLOCKED,运行器诊断日志 requested/answered/bidask_zero 定位形态不匹配)。④ 对账:ACCEPTANCE.md 13→14 脚本、156→新和(逐脚本 `grep -c 'check "'` 实计);文档同步 doc/WHEEL_STRATEGY.md(实时链路+LLM 闸门+Telegram 处置)、doc/API.md(serve flags/新表/动作类型/**动作词表补 LLM_REVIEW(评审 P2,doc/API.md:121)**)、task ledger。⑤ **切片 C 评审 P2 移交(2026-08-11)**:a) `cmd/wbot` 适配器单测——`qualifySymbol` 表驱动(HK/US/SH/SZ,CN 前缀启发式注明)、`parseWheelEnv`、`futuQuoter.Quote` s2c fixture 解析、`dailyOrders` 边界(昨日/今日 ALERT 计数);b) `-wheel-interval 0/负值` 在 flag 解析处 fail-fast(exit 2,与 parseWheelEnv 同模式);c) accept-wheel-live.sh 补「死网关 + `serve -wheel-run` 启动时 `/v1/health` 仍 200、stderr 出现 per-symbol 失败」断言(容灾冒烟)。

**Sol 评估吸收(2026-08-11)**:accept-wheel-live.sh 拆成两个独立验收场景(死网关通常只产 HOLD 而 G 只审核 ALERT,同一场景不能同时稳定证明容灾与闭环):场景 A 死网关 fail-closed(health 200 + 信号 DATA_BLOCKED + 零 LLM 审核);场景 B 完整 fake 报价产生 ALERT 后验证 LLM_REVIEW 记录存在 + dismiss 静默(LLM 用 fake 端点 env)。真实字段核对(③)作为真实环境认证闸门单独记录,不作为离线脚本必过步骤。另吸收切片 G 评审 P2:LLM gate env 三变量(LLM_BASE_URL/LLM_API_KEY/LLM_MODEL)补入 doc/README.md 部署文档;serve 启动时 wheel 开启且 reviewer 为 nil(env 缺失)打一行 warn(提升运维可发现性,避免 ALERT 静默不推送)。

## Constraints

- **不碰**已交付切片的既有逻辑(只在新基线之上加);改 CLI 只动 `cmd/wbot/` 的 watchlist 命令与模板默认值来源。
- HTTP PUT /v1/watchlist/{symbol} 校验保持严格(缺失 required 字段 → 400),默认档只存在于 CLI 侧。
- accept-wheel-live.sh 遵守运维纪律:自清理(symbol/进程)、`check()`/`count()` helper、连跑两遍全过、PID 唯一 symbol 防同秒复用、不进真实网关依赖(fake quoter env 或网关不可用时跑 DATA_BLOCKED 路径);两个验收场景各自独立跑。
- 遵守 self-documenting-code(注释 ≤1 行)。

## Links

- Driven-By: 用户指令 2026-08-11「wheel 策略先实际应用到 futu 模拟盘运行起来,按默认参数即可」(CLI 基于现价生成默认曲线)
- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md` 切片 F
- Branch: `feat/slice-f-default-cli`(worktree `.claude/worktrees/slice-f-default-cli`)
- Depends-On: 切片 B/C/D/E **/G 全部合入**(G 已于 6d6993f 合入,LLM 审核真实写入已就位)

## State

- **status**: `in_progress`(2026-08-11 派单 codex gpt-5.6-luna max)
- **last step**: 主会话已确认 CLI `watchlist add` 现有行为与模板默认值位置;accept 脚本纪律(逐脚本 grep -c 实计、矩阵求和、总表)在 doc/ACCEPTANCE.md 与 memory 双链
- **P2 移交已由 codex 完成(2026-08-11,待 F 收口评审)**:
  - P2-2 适配器单测:`25ea2da`(wheel_scheduler_test.go + runner_test.go)
  - P2-3 interval fail-fast:`ed7332a`(main.go 启动前校验 exit 2 + validateWheelInterval + wheel_interval_test.go)
  - 均由 codex CLI(gpt-5.6-luna)实现并自测全绿,主会话已验收复跑;评审并入本切片交付
  - 另:切片 A 引入的 ingest jsonb 断言 bug(CI db-integration 必挂)已修复合入基线 `bd4a700`(非本切片工作,记录备查)

## Next

实现 CLI 默认档 + accept-wheel-live.sh(两场景)+ LLM env 文档/warn + 对账/文档同步 → `scripts/verify.sh` 等价自测 + accept-wheel-live 连跑两遍 → 独立分支提交(push)→ 报告改动文件/测试结果/遗留问题。真实报价认证(③)作为独立真实环境闸门,网关恢复后执行并记入 ledger。

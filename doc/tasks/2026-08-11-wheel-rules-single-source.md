# Wheel 评审 P3-1:LLM 审核规则摘要单一来源

- **id**: `2026-08-11-wheel-rules-single-source`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

切片 G 评审 **P3-1**(观察/排期):`internal/wheelrun/runner.go:83` 的 `wheelReviewRules` 规则摘要常量与 `doc/WHEEL_STRATEGY.md` 漂移风险(同一产品规则两份维护点)。本次修复:**建立单一来源 + 防漂移测试**。

交付:
1. `doc/WHEEL_STRATEGY.md` 新增独立小节「LLM 审核规则摘要(单一来源)」,内容与常量完全一致(常量是中文文本,文档照录);注释标注该段为 LLM 审核规则唯一维护点
2. `internal/wheelrun/runner.go:83` 常量旁加 ≤1 行注释,注明与文档该段同步(文档为源)
3. `internal/wheelrun/` 新增防漂移单测:读取 `doc/WHEEL_STRATEGY.md`,断言包含 `wheelReviewRules` 全文(空白归一化后比较),防止任一侧独立漂移

## Constraints

- **不碰**切片 F 进行中的文件:`cmd/wbot/`(wheel_scheduler.go 等)、`scripts/accept-wheel-live.sh`、`doc/README.md`、`doc/API.md`;doc/WHEEL_STRATEGY.md 只新增独立小节,不改 F 正在同步的实时链路段落
- 只动:`internal/wheelrun/runner.go`(仅注释)、`internal/wheelrun/<test 文件>`、`doc/WHEEL_STRATEGY.md`(新增小节)
- 不引入新依赖;注释 ≤1 行;测试不依赖网络
- 测试读取文档用相对路径(`../../doc/WHEEL_STRATEGY.md`,包内跑 go test 即 repo root 之上两级)

## Links

- Driven-By: 切片 G 评审 P3-1(规则摘要常量与 WHEEL_STRATEGY.md 漂移,建议单一来源)
- Branch: `chore/wheel-rules-single-source`(worktree `.claude/worktrees/wheel-rules-single-source`)
- 执行者:Claude coder subagent(与 codex 切片 F 并发,文件不重叠)

## State

- **status**: `delivered`(已合入基线)
- **last step**: Claude coder subagent 实现并提交 `11c746e`(3 文件 +31:doc/WHEEL_STRATEGY.md 新增大节「LLM 审核规则摘要(单一来源)」+ runner.go 常量 1 行注释 + rules_ssot_test.go 防漂移测试);自测 5 步全绿(含变异验证:文档删段→红,还原后绿)

## 评审结论(2026-08-11,reviewer 无条件通过)

- 结论:合入;功能类型 **bugfix/维护**(常量值零变化,无行为变化,无需单独发布,随下批合入)
- 无 P0/P1/P2;逐字一致性已验证(常量↔文档字节相等);CI 两 job 均真实执行新测试;提交粒度良好
- **P3 观察×2**:① 包级 helper `normalizeSpace` 与未来同名测试冲突风险(常见惯用法,记录)② 单向防护——只防文档删改,不防常量增改(Goal 部分未达,接受理由:文档为源工作流+行为测试兜底,不划算硬化)

## 合入记录(2026-08-11)

- merge(no-ff,主基线 codex/feat-datacheck-observability)+ build/vet/wheelrun test 全绿
- **status: delivered**

## 验收

- 自测:gofmt + go build ./... + go test ./internal/wheelrun/... 全绿;防漂移测试真实生效(文档删除该段 → 测试红)
- 提交到 `chore/wheel-rules-single-source` 分支,署名 `Co-Authored-By: Claude <noreply@anthropic.com>`,不 push
- 评审:主会话派 reviewer;P3 项,如无发现按低风险合入

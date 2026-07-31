# 按 self-documenting-code rule 整理既有代码注释（分批）

- **id**: `2026-07-31-code-comments-self-doc`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

按用户级规则 `~/.claude/rules/self-documenting-code.mdc`（doc comment ≤ 1 行、内联 ≤ 1 行、复杂设计移 docs + 一行指针）与 `vibe-coding.mdc`（分步迭代、复用存量、完备测例）整理既有代码注释。**分批**：批 1 = `internal/ingest` + `internal/backtest`（13 文件，含最长 9 行块）；批 2 = `internal/{poll,paper,httpregister,db,httpapi}` + `cmd/wbot/main.go`。

## Constraints

- 只改注释，不动代码逻辑/签名/行为；`gofmt` 后零 diff 于逻辑。
- 注释精炼需保留信息（非机械删除）：多行 doc → 1 行要点；设计解释 → 保留要点或指 docs。
- 每个文件整理后 `go build` 通过；每批走 PR + reviewer 评审 + CI 绿后合入。

## Links

- 规则：`~/.claude/rules/self-documenting-code.mdc`、`~/.claude/rules/vibe-coding.mdc`
- 评审规范：`.claude/agents/reviewer.md`（已合入，PR #12）
- Driven-By: 用户指令「按照新的rule编码规范整理已有代码」（2026-07-31）

## State

- **status**: `running`
- **last step**: 盘点完成（21 文件含 2+ 行注释块）；批 1 派单中。

## Next

- 批 1 完成 → verify → PR → reviewer 评审 → merge → 批 2。

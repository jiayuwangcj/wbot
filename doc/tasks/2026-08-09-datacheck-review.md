# datacheck core 独立审查与收口

- **id**: `2026-08-09-datacheck-review`
- **created**: `2026-08-09`
- **updated**: `2026-08-09`

## Goal

独立审查当前未提交的 datacheck core，发现并修复会导致误判、漏拉、重复调度、退出码错误、数据污染或竞态的缺陷；输出可复核的测试证据。

## Constraints

- 审查范围：`internal/datacheck/**`、`cmd/wbot/datacheck*.go`、`cmd/wbot/main.go` 中 datacheck wiring、对应文档/测试。
- 不实现 P1 API/UI，不改 `internal/webui`、`internal/httpapi`，避免扩大 P0。
- 保留 CLI 默认只读、`-repair` 显式写入、serve 默认 17:30 调度的已拍板契约。
- 可直接修复明确缺陷并补测试；不得提交、推送或清理他人改动。

## Links

- Driven-By / trigger: 当前 Codex 任务用户指令（2026-08-09）
- PR / branch: `codex/feat-daily-data-completeness`

## State

- **status**: `done`
- **last step**: Luna 独立审查完成；修复 option coverage 将不同快照的 `MAX(ts)`/`MAX(expiry)` 拼接问题，并补真实 PG 反例测试。主线程复核后 `scripts/verify.sh` 为 `verify: ok`。

## Next

P0 已合格。剩余风险仅为 weekday-only 不识别交易所节假日，已独立排入 `2026-08-09-datacheck-market-calendar.md`（P2）。

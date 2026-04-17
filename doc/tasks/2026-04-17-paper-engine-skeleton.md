# 模拟盘执行骨架（paper）

- **id**: `2026-04-17-paper-engine-skeleton`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`

## Goal

- ROADMAP v1「模拟盘执行骨架」最小切片：`internal/paper` 入队即成交（无券商、无 HTTP）。
- 验收：`go test ./...`、`go vet`、与 CI 一致；`scripts/verify.sh` 通过。

## Constraints

- 不引入 DB / HTTP；不改 domain 既有语义，仅消费 `domain.Order`。

## Links

- Driven-By / trigger: 用户「提交 + 正式迭代」
- PR / branch: main

## State

- **status**: `done`
- **last step**：新增 `paper.Engine`、`Submit` → 生成 ID、即时 `OrderFilled`；单测覆盖无效 symbol、填充、递增 ID。

## Next

- 可选：CLI `paper` 子命令 demo；或将 `Submit` 拆为 New → Fill 两阶段以更贴近真实撮合。

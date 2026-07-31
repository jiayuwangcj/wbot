# CI 标准改为 Ubuntu（去 macOS）

- **id**: `2026-07-31-ci-ubuntu-only`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

CI 门禁标准统一为 **Ubuntu**（与当前开发环境一致），从 test job 矩阵移除 macOS。

## Constraints

- 不改任何 Go 代码；只改 `.github/workflows/ci.yml`、`doc/PLAN_V0.md` 与任务记录。

## Links

- Driven-By: 用户本会话指示「开发在 macOS，但优先在 Ubuntu 上运行；CI 环境标准改为 Ubuntu（当前开发环境）」
- 触发背景: run `30613731989`（`feat(ingest): add HTTPSource...`）在 `test (macos-latest)` 连续两次因 runner 环境故障 `dyld: missing LC_UUID load command` + `signal: abort trap` 挂掉（所有包、含未改动包，加载期即崩；Ubuntu test 与 db-integration 全绿），属 macOS runner 间歇性 infra 故障，与代码无关。

## State

- **status**: `done`
- **last step**: `ci.yml` test job 去矩阵（`runs-on: ubuntu-latest`），gofmt/race/staticcheck 的 `if: matrix.os` 条件移除，ci-summary 文案改 ubuntu；`doc/PLAN_V0.md` 改「Linux/Ubuntu」。随后按用户指示**固定 `ubuntu-24.04`（当前 LTS，与开发环境一致）**，不用 `ubuntu-latest`（避免随 runner 前滚）。

## Next

- 已完成：commit `4cfb4ee`（ubuntu-only）run `30614075769` **绿**；commit `c2d6618`（pin LTS）run `30614192478` **绿**，闭环。

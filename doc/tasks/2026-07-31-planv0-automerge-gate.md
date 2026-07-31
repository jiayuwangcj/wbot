# PLAN_V0 验收补齐：main 分支保护 + auto-merge

- **id**: `2026-07-31-planv0-automerge-gate`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

补齐 [[PLAN_V0]] 最后一项验收「main 分支启用 auto-merge 规则」（检查发现 `Branch not protected`，此前未落实）。

## Constraints

- 按 `doc/GITHUB_SETUP.md` 既有约定执行（必需 checks、1 approval、Allow auto-merge、推荐 squash-only）。

## Links

- [[PLAN_V0]]（验收：本地检查 ✅ / PR checks 全绿 ✅ / auto-merge ❌→✅）
- [[GITHUB_SETUP]]

## State

- **status**: `done`
- **last step**（含后续变更，2026-07-31）:
  - 初始：分支保护 `PUT /branches/main/protection`——`enforce_admins=true`、必需 checks（初设 `ci / test` 等带 workflow 前缀，**后修正为 job 名** `test`/`db-integration`/`governance`，strict）、`required_approving_review_count=1`、禁 force push/删除；仓库 `PATCH` `allow_auto_merge=true`。
  - **变更 A（用户授权自行评审）**：approval 1 → 0。先试 `required_approving_review_count=0`（GitHub 语义异常，PR 仍 BLOCKED），最终 `required_pull_request_reviews=null`（无 review 要求）。
  - **变更 B（context 修正）**：必需 checks 从 `ci / <job>` 改为 `<job>` 名（check run 名不带 workflow 前缀；`ci / test` 写法导致 PR 永远 BLOCKED，PR #11 即因此卡住，修正后 auto-merge 立即合入）。
  - 复核（2026-07-31，gh 认证修复后 GET）：`enforce_admins=true`、contexts `[test, db-integration, governance]`、strict、`reviews=null`、force_pushes=false；分支保护层 `allow_auto_merge=null`（仓库级已开，PR #11 auto-merge 实际生效验证）。
  - 备注：merge 方法字段（squash-only）API 返回 null（token 读取权限限制），为推荐项非验收必需，留待有权限时设置。

## Next

- 闭环（无代码改动，无需 CI）。后续 PR 自动合入门禁已就位。

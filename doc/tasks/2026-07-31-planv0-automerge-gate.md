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
- **last step**: 用 `gh api`（用户已认证 token）设置：
  - 分支保护 `PUT /branches/main/protection`：`enforce_admins=true`、必需 checks `ci / test`、`ci / db-integration`、`ci / governance`（strict）、`required_approving_review_count=1`、禁止 force push/删除。
  - 仓库设置 `PATCH /repos/wbot`：`allow_auto_merge=true`。
  - 验证：`GET /branches/main/protection` 确认 contexts/approvals/enforce_admins 生效。
  - 备注：merge 方法字段（squash-only）API 返回 null（token 读取权限限制），为推荐项非验收必需，留待有权限时设置。

## Next

- 闭环（无代码改动，无需 CI）。后续 PR 自动合入门禁已就位。

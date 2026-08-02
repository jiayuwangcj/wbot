# 闭环 #43: dev-up smoke 扩展到全部 DB 本地端点

- **日期**: 2026-08-03
- **PR**: #206(脚本 + 归档合一)
- **背景**: 「验收覆盖扩展」引擎对账 dev-up(本地验收主门,「本地全可用才提交」)的 10 项检查: 早于 `/v1/account/snapshots`(2026-08-03 新增),且从未覆盖 cluster 之外的 DB 本地只读面(health/runs/bars/status/config)。

## 改动

`scripts/dev-up.sh` smoke 新增 6 项(种子数据后恒 200):

- `GET /v1/health`、`/v1/runs`、`/v1/account/snapshots`、`/v1/admin/status`、`/v1/admin/config` → 200
- `GET /v1/bars?symbol=DEMO.US&timeframe=1d` → 200(种子 DEMO.US)

futu 系端点依赖网关,刻意不入 dev-up(由 scripts/accept-*.sh 覆盖);注释写明分工。

## 验证

- dev-up --force smoke 16/16 全过
- CI 5/5 全绿

## 备注

- **引擎经验**: 「验收覆盖扩展」的第三个对象维度——dev-up smoke 也是验收面: 新端点落库后,dev-up 主门与 accept 脚本一样要同步扩展(否则本地门漏检新端点);futu 系与 DB 本地系的职责分工写进脚本注释,防未来歧义。
- **候选池**: 仍枯竭。

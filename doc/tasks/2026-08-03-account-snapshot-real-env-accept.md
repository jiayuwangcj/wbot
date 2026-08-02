# 闭环 #34: ingest account real-env 分支端到端验收(资产曲线双环境)

- **日期**: 2026-08-03
- **PR**: #188(功能+归档合一, 仅脚本)
- **背景**: 资产曲线验收(闭环 #29/#30)只覆盖 sim env——real 分支(同一只读 funds 查询, 实盘账户)从未实测, API 脚本甚至断言 `env=real` 为空序列(因从未写过 real 快照)。这是 S-account-curve 双环境语义(env 切换联动, Dashboard 资产曲线 real 视图)的验收洞。

## 改动

- `scripts/accept-account-snapshot.sh`(CLI 侧)新增第 5 步:
  - `wbot ingest account -env real` 真实快照 → exit 0 + 输出含 `acc_id/env=real + rows=1`
  - psql 断言 env='real' 表行 rows +1、最新行 total_assets>0
  - 头注释 sim → sim and real(两环境均只读)
- `scripts/accept-account-snapshots-api.sh`(API 侧)第 3 步:env=real 从「空 points」改为「有数据 + 时间递增 + total_assets>0」(real 行由 CLI 脚本写入, 注释注明依赖顺序)

## 验证(实测)

- CLI 脚本 11/11 ×2 连跑稳定: sim 6→7→8, **real 0→1→2**(real 账户解析成功, 首次实盘快照落库)
- API 脚本 6/6: env=real 返回 real 快照点数(时间递增 + 数值合理), 增长检查 before 8 → after 9
- CI 5/5 全绿

## 备注

- **real 快照落库安全性**: 与 `wbot futu funds -env real` 同一只读安全面(FUTU.md §9);账户快照表本就为两环境设计;实测未打印实盘资产值(脚本只断言模式与数值, 不回显)。
- **候选池**: 仍枯竭(同 #32/#33);本步取自动「验收覆盖扩展」——每轮 triage 后对账验收脚本的断言与真实数据分支, 找零覆盖分支补齐。下一步候选: 验收脚本对账继续(如 fresh/real 分支的 bars/option 脚本)。

# 闭环 #35: ingest account -every 循环验收(SIGINT 优雅退出)

- **日期**: 2026-08-03
- **PR**: #190(功能+归档合一, 仅脚本)
- **背景**: DATA_PIPELINE.md 资产快照章承诺「应用内循环」模式(`-every`,与外部 cron 二选一),但验收从未实测该路径——零覆盖分支(「验收覆盖扩展」引擎 #34 的延续)。

## 改动

`scripts/accept-account-snapshot.sh` 新增第 6 步:

- 后台跑 `wbot ingest account -every 2s`,6 秒后 `kill -INT`,断言:
  - 优雅退出 exit 0(进程自身退出码,经 kill -INT + wait 获取)
  - 期间快照 +≥2(立即首拍 + 每 2s 一拍;实测 6s 内 +3)

## 关键坑

**GNU timeout(1) 计时触发后恒返回 124**(即使进程优雅退出)——拿不到进程真实退出码。改用 `kill -INT "$pid"; wait "$pid"` 获取进程自身退出状态。第一版用 `timeout -s INT 6` 断言 exit 0 失败(实测 124),直接验证 kill+wait 得到 exit=0 确认优雅退出逻辑正确,修脚本断言方式而非代码。

## 验证(实测)

- CLI 脚本 13/13 ×2 连跑稳定(sim/real 快照 + -every 循环)
- API 脚本联动 6/6 不变
- 优雅退出语义与源码一致: `ingestRepeatCtx` 用 `signal.NotifyContext(SIGINT)`,RunEveryResilient 返回 `context.Canceled`,`runIngestAccount` 对 `errors.Is(err, context.Canceled) → return 0`(cmd/wbot/ingest_account.go)
- CI 5/5 全绿

## 备注

- **候选池**: 仍枯竭;本轮引擎「验收覆盖扩展」第 2 弹(real 分支 → -every 循环)。其余 accept 脚本(bars-refill 4/4、option 系 4/4+6/6+2/2)已核对无零覆盖分支。
- 下一步候选: 无自主可推进项;等待老板拍板/资源/新需求。

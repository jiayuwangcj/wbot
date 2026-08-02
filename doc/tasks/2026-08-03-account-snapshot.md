# 账户资产快照落库——资产曲线数据层 (S-account-snapshot) — 2026-08-03

状态: ✅ 已合并 (PR #179)

## 背景
AUTO_ADVANCE 根任务循环。候选池枯竭后,归档备注候选「资产曲线(账户历史
快照,大)」取**数据层**为最小步:`wbot futu funds` 的只读资金查询已存在,
缺的是定时快照落库——资产曲线 UI 的地基。此步只做 CLI+表,不做 API/UI
(下一轮)。

另:上一轮归档候选「期权链 ATM/ITM/OTM 标记」在 triage 中发现与老板明确
指令冲突——960710c 已删 options chain 区块(老板:不需看盘工具),该候选
作废(见 memory)。

## 改动
1. **迁移 004_account_snapshots.sql**:env/acc_id/total_assets/cash/
   market_val/frozen_cash/power/captured_at;UNIQUE (env, acc_id,
   captured_at) + 时间索引。**与 ingestion_runs 隔离**——账户数据非行情
   历史,两条线永不混(隐私红线:凭证不入 Config、配置值永不返回 API,
   表只存快照数值)。
2. **cmd/wbot/ingest_account.go**:`wbot ingest account`
   - protobuf TCP funds 只读查询(复用 openTradeClient/resolveAccount,
     与 `futu funds` 同一安全面,sim/real 均只读)
   - INSERT ... ON CONFLICT DO NOTHING 幂等;输出 acc_id/env/总资产/现金/
     市值/购买力 + rows 落库数
   - `-every` 重复快照(ingest.RunEveryResilient,供 cron 定时驱动)
3. **dispatch/usage**:main.go `case "account"` + usageIngest 行。
4. **cmd/wbot/ingest_account_test.go**:dispatch 契约(-h/坏 env/缺 dsn;
   注意 CI 设 WBOT_PG_DSN,测试内 t.Setenv 置空)。
5. **scripts/accept-account-snapshot.sh** 7/7:契约×2 + 真实快照 rows+1 +
   数值合理性;OrbStack 桥接地址参数化(bin/dsn/proto-addr 三参)。

## 验证
- `go test ./...` 全绿;gofmt/vet 干净
- 本地真实快照:acc_id=1907141 env=simulate total_assets=1198286.82
  (与 doc/FUTU.md 实测记录一致),rows=1;`-every 1s` 冒烟 3 次快照后
  SIGINT 正常退出
- 验收脚本连续两遍 7/7(before 1 → after 2 → 3,幂等 +1)
- CI:首轮 db-integration 挂(测试假设 WBOT_PG_DSN 未设,CI 已设)→
  修复 t.Setenv 置空后 5/5 全绿

## 备注
- **幂等语义**:UNIQUE (env, acc_id, captured_at) + ON CONFLICT,同一时刻
  重复快照 rows=0(不重复计数);不同时刻快照各一行,曲线按 captured_at
  排序。
- **env 命名**:表内 env 存 futu.EnvName 输出(`simulate`/`real`,非
  flag 输入 `sim`)——与 /v1/futu/account 输出一致,验收脚本按
  simulate 过滤。
- **OrbStack 地址**:proto 11111 宿主 127.0.0.1 不可达,需桥接 IP
  (192.168.215.2:11111);PG 同理(192.168.215.5)——见
  memory/wbot-ops-context。
- **下一步候选(数据层已备,取 UI 最小步)**:Dashboard 账户资产卡加
  资产曲线(drawSparkline 复用,`account_snapshots` 查询端点);
  DATA_PIPELINE.md 补 `ingest account -every` cron 示例(与既有
  ingest futu cron 同段)。
- 作废候选:期权链 ATM/ITM/OTM 标记(老板:不需看盘工具)。

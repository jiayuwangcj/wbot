# 训练参数实装 00700 + JD 尝试运行(2026-08-14 老板指令)

## Goal

老板指令:「将此参数实装到 0700和JD尝试运行」— 把 #82 B 重训 rank1 候选参数(报告 bt-HK.00700-123-7ecf5509)实装到 watchlist,HK.00700 与 US.JD 各跑一轮观察。

## 实装参数

战术参数(两标的共用,训练自 HK.00700;JD 为跨标的暂借,记录注明):

| 键 | 值 |
| --- | --- |
| move_interval_pct | 0.005 |
| min_premium_per_share | 1.5258919439616279 |
| stock_switch_pct | 0.14451573668691084 |
| trade_gap | 76 |
| min_option_quality | 0.5318450198523592 |
| min_dte | 13 |
| max_dte | 45 |

战略参数:
- **HK.00700**(训练固定):full_position_price 400 / zero_position_price 600 / max_inventory 1200
- **US.JD**(老板 2026-08-14 修正:「JD战略为 24-30,15手」):full_position_price **24** / zero_position_price **30** / max_inventory **1500**(15 手 × 100 股);保留 max_quote_age_seconds=3600(旧配置值)

## 实施

- 经 `PUT /v1/watchlist/{symbol}`(容器内 node fetch,sandbox 外 curl 不通、容器内 wget 不支持 PUT):
  - HK.00700 → 200,config_version=2(全量训练参数,460 港元股价尺度)
  - US.JD → 200,config_version=3(24/30/1500 战略参数)→ **config_version=4**(按比例修正,见下)
- wheelrun 每 pass 从 watchlist 加载(runner.go List→ParseConfig),下个 pass 自动生效,无需重启

### 按比例修正(老板反馈:「最低每股权利金不对,标的不同每手价格不同,没有按比例调整」)

绝对金额/数量参数按股价 × 手规模比例换算(00700 股价 460 / 每手 100 股;JD 股价 ~29 / 每手 100 股,换算系数 29.3/460≈0.064):

| 参数 | 00700 训练值 | JD 按比例值 | 依据 |
| --- | --- | --- | --- |
| min_premium_per_share | 1.53 | **0.10** | 1.53 × 29.3/460;实测 JD put bid 0.26–0.625(均 0.405) |
| min_option_profit | 200 | **13**(美元/手) | 200 × 2900/46000 ≈ 12.6 |
| min_dte | 13 | **5** | JD 仅一个到期日 JD260821(DTE=7),13 会过滤掉全部候选 |
| trade_gap | 76 | **95** | 76 × 1500/1200(按 max_inventory 比例) |

战略参数按老板修正:full_position_price **24** / zero_position_price **30** / max_inventory **1500**(15 手 × 100 股)。

### ⚠ 发现:实盘 option chain 窗口超 futu 30 天限制(DATA_BLOCKED)

- JD v3 PUT 后下个 pass(23:11)报 `GetOptionChain: requested period (2764800 seconds) exceeds 30 days + 1 hour (2595600s) limit` → **DATA_BLOCKED,实盘评估中断**
- 根因:runner.go `OptionChain(ctx, now+MinDTE, now+MaxDTE)` 窗口跨度 = MaxDTE−MinDTE;JD 5..45 → 40 天、00700 13..45 → 32 天,均超 futu 网关 30 天+1h 上限;00700 港股收盘未暴露,明早开盘必炸
- **修复(clamp)**:runner.go 请求窗口跨度 >30 天时裁远端到 begin+30 天(低 DTE 区间优先,候选质量通常更高),打日志;单测覆盖 40 天→30 天 clamp 与 25 天不 clamp 两场景
- 训练配置 max_dte=45 不动,clamp 后 00700 实盘窗口 [13, 43]、JD [5, 35](JD 唯一到期日 DTE=7 在内,不受影响)

## State

- [x] 参数落库(00700 v2 / JD v3 / JD v4 按比例修正)
- [x] option chain 30 天超限修复(clamp + 单测 + verify.sh 全绿,提交中)
- [ ] 部署(serve 重建)+ 下个 pass 验证 JD 新配置跑出(signal > 888,config_version=4)
- [ ] 观察一轮运行(ALERT/HOLD/候选分布)后收口汇报

## Links

- 报告:reports/bt-HK.00700-123-7ecf5509.json(#82 B 重训)
- 前置:doc/tasks/2026-08-14-backtest-premium-return.md
- 旧 JD 配置(v2):price_position_curve 30.25→1100..35.25→600,no_trade_gap 10,max_daily_orders 1(config_version=2,2026-08-14 迁移前格式)

## Next

观察一轮后:JD 战术参数建议后续独立训练(本次为跨标的暂借);00700 港股已收盘,明早开盘后看真实评估(13..45 窗口经 clamp 后 [13,43])。

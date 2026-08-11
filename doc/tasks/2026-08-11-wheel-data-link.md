# wheel 数据链路修复(00700 明早实测阻断 bug)

- **id**: `2026-08-11-wheel-data-link`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

serve 的 wheel-run 从启动起(22:46)全部标的 `current price 0 is not positive`——数据面字段映射对真实 futu 网关从未验证通过(fixture 假数据固化)。修复正股价格 + 期权数据源,明早 9:30 港股开盘以 HK.00700 实测 wheel 的前置条件。

## 已实测事实(2026-08-11 深夜,真实网关 v1.4.106)

1. **正股 `/api/quote`**:字段是 `cur_price`(HK.00700 → 470.8)。**无 `last_price` 字段**。`cmd/wbot/wheel_scheduler.go:72` futuQuoter.Quote 解析 `last_price` → 恒 0 且 err=nil → runner 报 "current price 0 is not positive"(futu 包 client_test.go:100 fixture 用 cur_price 是对的,错在 wheel_scheduler 的局部 struct)
2. **期权 `/api/quote`**:与正股同结构,只有 `cur_price`/`volume`/`update_time`/`security`;**无 bid_price/ask_price/last_price,`option_ex_data` 恒 null**(实测 HK 盘后 + US 盘中均如此,订阅 sub_types=[1,7] 不影响)。`internal/futu/option_quote.go` quotePage 的 bid_price/ask_price/last_price/ex_data 字段全部不存在
3. **`/api/option-quote`(combo,免订阅)**:单合约完整数据——`price`(现价)/`mid`(中间价)/`iv`/`delta`/`gamma`/`vega`/`theta`/`rho`/`open_interest`/`vol`/`strike`/`days_to_expiry`/`contract_size`。实测 HK 盘后 TCH260814C375000:price 106.79, iv 122.07, delta 0.986, theta -0.247;US 盘中 AAPL260814C110000 同字段。**无 bid/ask**(mid 是唯一参考价)。一次一个合约(多腿=组合报价)
4. wheel.Validate 硬条件(wheel.go:325):Bid>0、Ask≥Bid、delta 符号、IV>0、Theta 非 nil、Volume>0、OI>0、LotSize>0、QuoteTime 新鲜
5. 期权链 `/api/option-chain` 正常(实测 HK.00700 85 合约,代码如 TCH260814C375000,market=1)

## Constraints

- **不动** `internal/wheel/wheel.go`、`internal/wheelrun/runner.go`、`cmd/wbot/telegram_scheduler.go`(codex telegram 任务正在改,并行协议)
- `futu.OptionQuotes(ctx, symbols) (map[string]futu.OptionQuoteEx, error)` 签名不变(runner 零改动)
- 零新依赖;fixture 测试同步更新为真实字段;verify.sh 全绿
- 限价语义:Last = 成交价(option-quote 的 price 或快照 cur_price),与 telegram v20 限价行对接
- 期权 Greeks 缺失的合约不得产生假 ALERT(Validate 拒掉即安全,勿放宽校验)

## 实现要点

1. **正股(cur_price,一行)**:`cmd/wbot/wheel_scheduler.go` futuQuoter.Quote 的局部 struct 改 `CurPrice float64 json:"cur_price"`
2. **期权 OptionQuotes 重写**(internal/futu/option_quote.go):
   - 快照层:`/api/quote` 批量(cur_price → Last,volume,update_time → QuoteTime)——立即返回,全链覆盖
   - Greeks 层:对**缺失 Greeks 的合约**逐合约 `/api/option-quote`(3s 限频,实测免订阅),填 Delta/IV/Theta/OI/Bid/Ask
   - Bid/Ask 数据源决策:option-quote 无盘口,mid 同时作 Bid/Ask(mid>0 时)或 price;注意 Validate 要求 Bid>0/Ask≥Bid——设计需自洽并有注释
   - **缓存增量**:包级缓存上次 Greeks,新鲜度阈值内(如 10min)不重拉;首轮全链 85 合约×3s≈255s,与 5min tick 的关系在注释说明;每 tick 只补缺失/过期
   - QuoteTime:option-quote 无时间戳时用快照 update_time
3. **测试**:option_quote_test.go fixture 改真实字段(cur_price/option-quote 结构);补 cur_price 解析 + mid 回填 + 缓存增量测试
4. 自测:`scripts/verify.sh` 全绿;`wbot futu quote` 对比实测(cur_price=470.8)

## Links

- 上游: doc/tasks/2026-08-11-telegram-alert-redesign.md(并行:codex 加 wheel.OptionQuote.Last + runner 映射 Last: q.Last,与本次 OptionQuoteEx.Last=cur_price 对接)
- 实测依据: doc/FUTU.md §10(期权 REST 契约,option-quote 实测路径)
- Branch: `fix/wheel-data-link`(worktree `.claude/worktrees/wheel-data-link`)
- 执行者: Claude 侧 coder(2026-08-11 深夜;codex 单飞 telegram 任务中)

## State

- **status**: `done`(2026-08-11 评审通过、合入 main;PR #333 已 MERGED)
- 评审结论: 无 P0/P1,建议合入;功能类型 bugfix(可及时发);P2:put 腿 option-quote 未实测,9:30 实测显式覆盖;P3:冷启 255s 串行(已注释接受)、闭市期 10min 重拉、ctx 取消 stderr 噪音

## Next

- ✅ 已合入(PR #333);重建 release 后 serve 重启生效
- 明早 9:30 开盘:数据链路验证(cur_price 470.8 区间)→ 候选生成 → ALERT → telegram 按钮闭环;**显式验证 put 腿**(P2)

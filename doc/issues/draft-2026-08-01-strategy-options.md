# （草稿）GitHub Issue 正文 — 策略模块⑫：Covered Call + Cash-Secured Put + 关注标的（watchlist）

**建议标题**：`[feature] 策略模块：Covered Call / Cash-Secured Put 期权策略 + 关注标的 watchlist（HK.00700 模拟盘实测）`

---

## Trigger comment

- 仓库级锚点：<https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869>
- 若单独开 Issue：发帖后在同一 Issue 留锚点评论，把评论 URL 填回此处并作为 PR 的 `Driven-By:`。

## Goal（对齐 `doc/tasks/2026-07-31-web-v1-target.md` 切片⑫）

老板指令（2026-08-01，两次）：

1. 「策略模块，新增 coverdcall 和现金担保 put 策略，增加关注标的，编写策略」——策略模块新增 **Covered Call**（备兑看涨：持有正股 + 卖出看涨）与 **Cash-Secured Put**（现金担保看跌：卖出看跌、现金担保）策略 + **关注标的（watchlist）**功能。
2. 「以 0700 为模拟盘为测试标的」——测试标的 **HK.00700**（腾讯，富途模拟盘）。
3. 扩展（2026-08-01 追加）：watchlist 为**完整形态**——(1) 指定关注标的（增删）；(2) 对每个关注标的应用策略；(3) 策略以**参数化模板**提供；(4) **每标的独立参数**；(5) 配置完成后可在**模拟盘回测**（回测 → 模拟盘验证链路；富途 paper 账户 HK.00700 已就绪）；(6) 策略模块与回测框架（internal/backtest）集成。

### 范围（建议的最小拆法：4 个子切片，各自可测可独立合入）

#### 子切片 ⑫-a：期权数据获取（Chain + 期权 K 线 + 数据方案）

**富途期权行情链路（调研结论，futuapi.com REST 参考 + openapi.futunn.com 官方文档）**：

- futu-opend-rs 网关（REST 22222，实测链路见 `doc/FUTU.md` §7/§8）**提供期权接口**，共 9 个 option 端点，本切片用到：
  - `POST /api/option-chain`（QOT_GET_OPTION_CHAIN 3209）：请求 `owner`/`symbol`（如 `"HK.00700"`，顶层字段自动展开为 Security 对象，**不要**传 `security` 对象）+ `begin_time`/`end_time`（格式 `yyyy-MM-dd`，**跨度最多 30 天**，end 须为今天或未来——**过期链不可查**）+ `condition`（价内/价外过滤）；响应 `option_static_info` 类合约静态信息：`code`、`lot_size`（合约乘数）、`name`、`strike_price`、`strike_time`（行权日）、call/put 类型、`owner`。
  - 期权 K 线：走**通用 `/api/history-kline`**（已实测实现于 `internal/futu/kline.go`，任意 symbol 可用）——`wbot ingest futu -symbol <期权 code>` 直接复用既有管道落库，**bars 表零 schema 变更**。
  - 辅助端点（后续可选）：`/api/option-expiration-date`（3224）、`/api/option-quote`（3255，注意其 body 用 `c2s.multi_legs` 包装，与其他端点不一致）、`/api/option-volatility`（3250）、`/api/risk-free-rate`（20231，官方明示供 Black-Scholes 定价用）。
- **港股期权代码规则**：`HK.{字母代码}{YYMMDD}{C|P}{行权价×1000}`（如 `HK.TCH210429C350000`；字母代码 ≠ 数字代码）。**必须从 option-chain 响应派生，绝不手工拼接**（futu-opend-mcp 为此专门提供 `resolve_option_code` 工具；港股简写格式与美股不同，手工构造必错）。
- **实测优先**：网关版本严格校验（`doc/FUTU.md` 已记录 v1.4.93 BUG-002 类坑：`symbol` 字符串形态被拒、未知字段误报），option-chain 的 `owner` 顶层字段与 option-quote 的 `c2s` 包装均须**实测记录**进 `doc/FUTU.md` §9（沿用 §7/§8 的实测表格模式），契约以实测为准。
- **限频**：option-chain 官方 10 次/30 秒 → `internal/futu/ratelimit.go` 新增 `OptionChainLimit` 档（3s/次，复用全局池）。
- **数据方案（关键决策）**：真实数据 + BS 模拟双路径——
  - 真实路径：chain 取当前合约 → `history-kline` 拉各合约期权 K 线落库。**限制**：过期链不可查、期权合约存续期短（历史只回溯到合约上市日），回测区间受限。
  - 模拟回落路径：`internal/options` 新包，Black-Scholes 定价（`risk-free-rate` 端点或参数化无风险利率；IV 参数化默认值）生成模拟期权价格；数据不可得（权限未开/历史不足）时回测仍可跑通。两路径收敛到同一 `OptionDataSource` 接口供 ⑫-b 消费。

**验收**：

- 单测（mock 网关响应，沿用 `internal/futu/client_test.go` 模式）：chain 解析（code/lot_size/strike/expiry/call_put）、错误路径（ret_type != 0、400、超限）。
- `doc/FUTU.md` §9 记录 option-chain/option-expiration-date 实测契约（含与文档出入）与期权 K 线实测结论。
- 有网关 + 权限时（本地联调）：`wbot futu option-chain -symbol HK.00700` 输出 JSON 合约列表；`wbot ingest futu -symbol <chain code> -timeframe K_DAY` 落库 bars 且重复执行幂等（ON CONFLICT）。
- 无网关/无权限：命令报错可读、exit 非零；集成测自动 skip（沿用 `internal/httpapi/integration_test.go` 模式）。
- BS 模拟路径：确定性单测（同输入同输出），无网关也能全链路跑回测。

#### 子切片 ⑫-b：策略实现（covered call / cash-secured put）+ 回测集成

**回测框架扩展（`internal/backtest`，现状：单 symbol `Run` + `Strategy.OnBar(ctx, bar, *State) (Action, size, error)`，Action 仅 Hold/Buy/Sell，State 仅 Cash/Position/Price，Equity = cash + position×price）**：

- Action 新增 4 态：`ActionSellCall` / `ActionBuyCall` / `ActionSellPut` / `ActionBuyPut`（size = 合约数；正股 Buy/Sell 不变）。
- State 新增 `Options map[string]OptionPosition`；`OptionPosition{Code, Kind(Call|Put), Strike, Expiry, Contracts, AvgPremium}`；`Equity` 纳入期权腿估值（v1 口径见待拍板项 6）。
- **到期结算**（runner 内机械执行，保持确定性）：`bar.Ts ≥ Expiry` → ITM 行权（Call 被 call away：以 **strike** 卖出 `lot×contracts` 股；Put 被行权：以 **strike** 买入、现金扣减 `lot×contracts×strike`），OTM 到期作废移除腿。CSP 开仓时现金储备校验（`Cash ≥ contracts×lot×strike`，不足报错）。
- **期权腿数据**：主 symbol 时间轴不变，期权腿价格经 `State` 提供的 `OptionBars(code)` 查找（runner 注入腿数据表）——**不触碰** blocked 的 multisymbol 对齐设计（`doc/tasks/2026-07-31-backtest-multisymbol-design.md`）。
- 既有 `Run` 签名与行为不变（向后兼容，既有测试全绿）；新入口 `RunWithSeries` 或等价扩展承载腿数据。

**策略模板（`internal/strategy` 包，现仅空 sma 目录，从零建）**：模板注册表 `Templates()`（名称/说明/参数 schema）+ `Factory(name, params) (backtest.Strategy, error)`（参数校验失败报错）：

- `covered-call`：持有正股 + 卖出看涨。参数（建议，最终待拍板）：`strike_pct_otm`（默认 0.03，行权价 = 现价×(1+pct) 就近 chain 档）、`expiry_rule`（`next_expiry` 默认）、`days_to_expiry`（28）、`fee_per_contract`（0）。
- `cash-secured-put`：现金担保 + 卖出看跌。参数：`strike_pct_otm`（默认 0.03）、`expiry_rule`、`days_to_expiry`（28）、`fee_per_contract`（0）。

**验收**（确定性单测，mkBars 风格、手工可核对）：

- CC 行权：到期 close > strike → 股以 strike 被卖出、权利金保留、腿移除；CC OTM 到期：持股保留、权利金保留。
- CSP 行权：到期 close < strike → 以 strike 买入、现金扣减、权利金保留；CSP OTM：腿作废、现金释放、权利金保留。
- 费用（fee_per_contract/正股 fee）、CSP 储备不足报错、未知模板/非法参数报错。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI 绿。

#### 子切片 ⑫-c：watchlist 后端（存储 + API + 模板 + 每标的参数绑定）

- **存储**：migration `003_watchlist.sql`——单表 `watchlist(symbol text PRIMARY KEY, strategy text, params jsonb, added_at timestamptz, updated_at timestamptz)`（strategy/params 可空 = 纯关注不跑策略）。建议 **DB 存储**（结构化 + API 一致 + JSONB 参数），不用 `internal/config`（其模型是白名单字符串 key，专为凭证设计，不适合结构化数据）。
- **API**（`internal/httpapi/watchlist.go`，独立 handler + 独立 `WatchlistStore` 接口，不动既有 `Store`/`AdminHandler`）：
  - `GET /v1/strategies`：策略模板清单 + 参数 schema（⑫-b 注册表导出）。
  - `GET /v1/watchlist`：`[{symbol, strategy, params, added_at, updated_at}]`。
  - `PUT /v1/watchlist/{symbol}`：body `{strategy, params}`；按模板 schema 校验，非法 400。
  - `DELETE /v1/watchlist/{symbol}`：移除。
- **CLI**：`wbot watchlist add|remove|list`（`-symbol -strategy -param k=v` 可重复）+ `wbot backtest` 扩展 `-strategy covered-call|cash-secured-put` 与 `-from-watchlist`（从 DB 读每标的策略与参数）。

**验收**：

- httptest 契约测：CRUD 全路径、未知策略/非法参数 400、JSONB 参数 roundtrip、`GET /v1/strategies` 模板清单与 ⑫-b 注册表一致。
- migration 集成测（有 `WBOT_PG_DSN` 时真 PG：add → list → backtest 读参；无则 skip）。
- CLI 单测（`main_test` 参数用例）：`wbot watchlist add -symbol HK.00700 -strategy covered-call -param strike_pct_otm=0.03` 后可 `list` 与 `backtest -from-watchlist`。
- `doc/API.md` 增 `/v1/strategies`、`/v1/watchlist` 章节；`wbot serve -h` 帮助文本同步（`main_test` 断言）。

#### 子切片 ⑫-d：回测集成验证（HK.00700 模拟盘实测）

- **端到端链路**：watchlist 配置（⑫-c）→ ingest HK.00700 日线 + 期权 chain/期权 K 线（⑫-a）→ `wbot backtest`（⑫-b）→ 确定性结果；富途模拟盘**只读** smoke（`funds`/`positions` 查询，trd_env=0，doc/FUTU.md 交易安全策略允许只读）。
- 数据两态：有网关+权限 → 真实数据；无 → BS 模拟数据（⑫-a 回落）同样可跑通全链路。
- 人工核对：HK.00700 回测输出一行摘要（`final_equity/total_return/max_drawdown/bars`），同输入同输出；结果与手工计算的 CC/CSP 现金流向对得上（抽样核对）。

**验收**：

- 真实数据路径（本地有网关 + HK 期权权限时）：`wbot ingest futu -symbol HK.00700 -timeframe K_DAY` + chain + `wbot backtest -symbol HK.00700 -strategy covered-call -from-watchlist` 输出确定性摘要；重复运行结果一致。
- 模拟路径：无网关时 BS 模拟期权价格下两策略回测可跑通（CI 不依赖网关）。
- CI 全绿（test + db-integration），网关相关集成测自动 skip。

### 非目标

- **真实期权下单/模拟盘自动交易循环**（v3 执行路径；实盘写操作需老板确认——doc/FUTU.md 交易安全策略；期权下单另需账号开期权权限）。
- 多 symbol 组合对齐/组合层（blocked 设计不动；期权腿只做「主 symbol 时间轴 + 腿数据查找」）。
- 美式期权**提前行权**建模（港股期权美式，v1 仅到期行权近似；见待拍板项 5）。
- Greeks/IV 曲面展示、OI 落库、期权策略分析（option-strategy* 端点均属后续）。
- Web UI 管理页面（watchlist 管理前端属 v4 Web 后续切片；本切片仅后端数据面）。
- Schwab/IBKR（挂起）。

### 待拍板项

1. **期权数据不可得/权限未开时的默认路径**：BS 模拟先跑通、实测数据可用后切换（产品建议）vs 挂起等老板开通 HK 期权权限（discussions/21）。另：HK 期权权限是否已开**需老板确认**（股票行情权限 ≠ 期权权限）。
2. **chain 快照是否落库**：产品建议落库 `option_chain_snapshots(code PK, owner, call_put, strike, expiry, lot_size, captured_at)`——回测可复现；不落库则每次回测现拉现用（依赖网关可用性）。
3. **option-chain 请求字段形态**：REST 文档称顶层 `owner`/`symbol`，与 kline 的 `security` 对象形态不同——以 ⑫-a 实测为准（BUG-002 模式），记录进 doc/FUTU.md。
4. **行权结算价口径**：ITM 行权按 **strike** 成交（产品建议，贴近真实）vs 按到期 bar close 近似。
5. **提前行权**：v1 不做（仅到期行权）——确认可接受。
6. **期权腿 mark-to-market 口径**：v1 权利金锁定（简化）vs 内在价值 vs 期权 K 线市值（⑫-a 数据可用后启用）——产品建议：权利金锁定起步，`OptionBars` 可用后升级市值。
7. **watchlist 存储形态**：DB 单表（产品建议）vs DB 双表（watchlist + strategy 分表）vs config 文件。
8. **backtest 执行端点**：CLI 先行（产品建议）vs `POST /v1/watchlist/{symbol}/backtest` API 同步提供。
9. **模拟盘实测范围**：只读查询（funds/positions，产品建议，符合交易安全策略）vs 模拟盘下期权单（属 v3，且需老板确认）。

## 依赖

- ⑫-a：复用 ⑪ 链路（`internal/futu` 客户端、`wbot ingest futu`、限频池、doc/FUTU.md 实测模式）；无新依赖。
- ⑫-b：依赖 ⑫-a 的 `OptionDataSource` 接口与数据方案（可先用 mock 数据并行开发，接口收敛后替换）。
- ⑫-c：独立（不依赖期权数据），可与 ⑫-a/⑫-b 并行；依赖 ⑫-b 的模板注册表（或先定义模板 schema 契约再并行）。
- ⑫-d：依赖 ⑫-a + ⑫-b + ⑫-c 全部。
- 建议派单顺序：**⑫-a → ⑫-b（与 ⑫-c 并行）→ ⑫-d**；并行注意：⑫-b 改 `internal/backtest` + `internal/strategy`，⑫-c 改 `internal/httpapi`（新文件）+ migration——文件面不重叠。

## Plan（可勾选）

- [x] ⑫-a：`internal/futu` option-chain/option-expiration-date 客户端 + `OptionChainLimit` 限频 + CLI `wbot futu option-chain` + `internal/options` BS 模拟 + doc/FUTU.md §9 实测记录
- [x] ⑫-b：`internal/backtest` 期权腿扩展（Action/State/到期结算/腿数据）+ `internal/strategy` 模板注册表（covered-call / cash-secured-put）+ 确定性单测
- [x] ⑫-c：migration 003 + `internal/watchlist` 查询 + `httpapi/watchlist.go`（/v1/strategies、/v1/watchlist CRUD）+ CLI `wbot watchlist` + `backtest -from-watchlist` + API.md
- [x] ⑫-d：HK.00700 端到端（真实 + BS 模拟两态）+ 模拟盘只读 smoke + 集成测 skip 策略 + CI 保持绿

## 仓库内链回

- 目标切片：`doc/tasks/2026-07-31-web-v1-target.md` ⑫（拆解中 → 本草稿落定后更新为「可做/已拆解」）
- 前置链路：`doc/tasks/2026-07-31-futu-integration.md`（⑪-a/b/c 完成）、`doc/FUTU.md`（REST 实测 §7/§8、交易安全策略、限频策略）
- 回测现状：`internal/backtest/`（backtest.go/strategy.go/state.go/constraint.go + 测试）、`doc/BACKTEST.md`、`doc/tasks/2026-07-31-backtest-*`
- 多 symbol 设计（不动）：`doc/tasks/2026-07-31-backtest-multisymbol-design.md`（blocked）
- 复用模式：`internal/futu/`（client/kline/ratelimit + 测试）、`internal/httpapi/admin*.go`（独立 handler + 独立接口模式）、`internal/ingest`（RunIngestion/QueryBars/ON CONFLICT）、`internal/db/migrations/`（001/002）
- 期权接口参考：futuapi.com REST 参考（option-chain 3209 / option-expiration-date 3224 / option-quote 3255 / option-volatility 3250 / risk-free-rate 20231）、openapi.futunn.com Qot_GetOptionChain（owner + 30 天窗口、过期链不可查、HK 期权代码格式、10 req/30s）
- 需求源：老板 2026-08-01 指令（策略 + 测试标的 HK.00700）+ 2026-08-01 扩展（watchlist 完整形态）；老板待办汇总：discussions/21（HK 期权权限确认）

## 状态（2026-08-03）

✅ **已完成并合入**：⑫-a option-chain 客户端 + `ingest futu-option`、
⑫-b `internal/strategy/options.go`（covered-call / cash-secured-put）、
⑫-c migration 003 + watchlist CRUD + `backtest -from-watchlist`、
⑫-d HK.00700 端到端均已落地。闭环记录见 PR #68/#69（⑫-c/⑫-b）、
`doc/tasks/2026-08-03-options-ingest-button.md`（⑫-a ingest futu-option）。

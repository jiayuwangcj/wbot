# （草稿）GitHub Issue 正文 — 富途模拟盘账户页：资金/持仓 Web 展示

**建议标题**：`[feature] 富途模拟盘账户页：/v1/futu/account 资金+持仓代理 + 数据页账户卡片`

---

## Goal（对齐 `doc/tasks/2026-07-31-web-v1-target.md` ⑤「券商持仓显示」——富途侧解除挂起；老板 2026-08-01「模拟盘策略运行」目标的查看侧闭环）

富途 paper 账号已就绪（⑫-d：HK.00700 模拟盘），trade 客户端只读查询（funds/positions）已实现于 `internal/futu/trade.go`（CLI `wbot futu funds|position`，PR #66），但**只有 CLI 无 Web**——浏览器看不到持仓/资金，切命令行才能查。本切片补齐 HTTP 代理 + 页面，用户在工作台内即可看「策略运行在模拟盘上的状态」。

### 范围（建议的最小拆法：2 个子切片）

#### 子切片 A：HTTP 代理 `GET /v1/futu/account`（资金 + 持仓，只读）

- 复用 `futu.TradeClient`（proto，TCP 11111；`OpenTrade` → `Account` → `Funds`/`Positions`，CLI 路径已验证），默认 trd_env=0（模拟盘护栏，doc/FUTU.md 交易安全策略——只读查询安全）。
- 独立窄接口 `FutuAccounter`（模式同 `FutuQuoter`，mock 可测）：`Account(ctx, env, accID) → {funds, positions}`。
- 连接管理（待拍板）：每请求短连接（简单、成本可控）vs 复用长连接（效率高、需互斥/超时）——产品建议：包级复用 + 互斥（单进程，查询低频）；评审确认。
- 响应字段白名单（不泄漏敏感信息）：funds `{total_assets, cash, market_val, available_cash}`；positions `[{symbol, qty, avg_cost, market_val, pl}]`。网关不可达 → 503 + 错误体 `{code, message, action}`（沿用 S5 约定，action 如「确认 OpenD 网关已启动（11111）」）。
- 环境变量沿用 `FUTU_GATEWAY_URL`（`futu.DefaultAddr` 默认）——与 quote 代理一致。

#### 子切片 B：Web 页面（数据页账户卡片 + 持仓表）

- `internal/webui/web/index.html`（或新 account 区域）：资金卡片（复用 quote card 模式）+ 持仓表（symbol/qty/市值/盈亏）；「刷新」按钮；移动断点复用（⑨ 布局）。
- 遵守既有约束：零外部资源、无构建链、多页静态站；PRIVACY 红线（不渲染任何配置值/凭证）。

### 验收（可测）

- httptest（mock `FutuAccounter`）：200 字段齐全；mock 错误 → 503 + 错误体含 `code`/`action`；env 参数非法 → 400。
- 页面源码断言（webui 包测试）：含对 `/v1/futu/account` 的 fetch 引用与持仓表渲染逻辑；全部 html/css/js 无 `http://`/`https://` 外部引用；viewport meta 存在。
- 集成测（有 `FUTU_GATEWAY_URL` + 网关可达时真网关只读 smoke：funds/positions 返回非空；无网关自动 skip——沿用 `futu_quote`/integration 既有模式）。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI（test + db-integration）绿。
- `doc/API.md` 增 `/v1/futu/account` 章节；`serve -h` 同步（`main_test` 断言）。

### 非目标

- **下单 Web 化**（order 保持 CLI；实盘写操作需老板确认——doc/FUTU.md 交易安全策略）。
- Schwab/IBKR 持仓（凭证 blocked，discussions/21 项 3）。
- 订阅式实时推送（v1 页面手动刷新即可）。
- 策略运行状态页（回测结果已有 results.html；连续运行属后续切片）。

### 依赖

- **无外部依赖**（网关本地 + 富途 paper 账号已就绪）。
- 前置复用：`internal/futu/trade.go`（#66）、`/v1/futu/quote` 代理模式（`internal/httpapi/futu_quote.go`，#79）、webui 页面模式（index.html/app.js）、错误体约定（S5 #76）。

## 仓库内链回

- 目标切片：`doc/tasks/2026-07-31-web-v1-target.md` ⑤（挂起中 → 本切片解除富途侧）；ROADMAP v3/v4（持仓数据读取接口；控制面 Web UI）
- 前置链路：`doc/tasks/2026-07-31-futu-integration.md`（⑪ 完成）、`internal/futu/trade.go`（funds/positions 只读）、`doc/FUTU.md`（交易安全策略、限频）
- 复用模式：`internal/httpapi/futu_quote.go`（FutuQuoter 窄接口 + 条件注册）、`internal/webui/web/`（零外部资源、断点）
- 需求源：老板 2026-08-01「模拟盘策略运行」目标（web-v1-target）；产品体验意见 2026-08-02（策略结果可视化、看板趋势）

## Plan（可勾选）

- [x] A：`internal/httpapi/futu_account.go`（FutuAccounter 窄接口 + 代理端点 + 503/400 映射）+ httptest + 集成测 skip 策略
- [x] B：index.html 账户卡片 + 持仓表 + 页面断言
- [x] API.md + serve -h + verify/CI 绿

## 状态（2026-08-03）

✅ **已完成并合入**：A `GET /v1/futu/account`（FutuAccounter 窄接口
+ 503/400 映射 + 集成测 skip）、B 数据页账户卡片 + 持仓表、API.md
+ serve -h 同步均已落地。闭环记录见
`doc/tasks/2026-08-02-futu-account-web.md`。

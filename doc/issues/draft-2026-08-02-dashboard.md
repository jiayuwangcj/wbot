# （草稿）GitHub Issue 正文 — Data 页改 Dashboard:账户聚合 + 订单状态 + Paper/实盘标识 + 视觉优化

**建议标题**：`[feature] Data 页改 Dashboard:账户聚合/子账户明细/订单状态 + Paper 标识 + UI 视觉优化`

---

## Goal（老板 2026-08-02 指令）

1. **删除 Data 页的 bars 区块**（意义不明）。
2. **Data 页改为 Dashboard 切页**：
   - 主要显示**用户当前账户聚合信息**（总资产/现金/市值/购买力）。
   - **各子账户明细**（sim/real 多账户列表）。
   - **右边栏显示当前订单状态**（挂单/今日订单）。
3. **明显位置显示 papermoney 还是实盘，颜色区分**（安全红线可视化）。
4. **视觉优化**：当前皮肤过于简洁、排版乱 — 卡片化布局、清晰分栏、统一间距。

## 现状

- `internal/webui/web/index.html` = Data 页（bars 图表 + futu account + options 块），导航 Data/Watchlist/Results/Admin。
- `GET /v1/futu/account`：funds（聚合）+ positions（单账户，默认 sim 第一个）。
- **缺**：多账户列表端点、订单状态端点（proto `TRD_GETORDERLIST`，gofutuapi 有 `GetOrderList` + `PendingOrderStatuses`）。

## 拆解（切片）

| 切片 | 内容 | 依赖 |
| --- | --- | --- |
| A | 后端：`GET /v1/futu/orders`（proto 订单列表代理，白名单字段：状态/方向/代码/数量/价格/已成交/时间）；`/v1/futu/account` 扩展多账户（`accounts[]` 或 `env` 参数支持多环境） | 无 |
| B | 前端：index.html 去 bars → Dashboard（左：聚合卡 + 子账户表；右：订单状态表）；paper/实盘徽章 + 红绿颜色区分 | A |
| C | 样式：style.css 卡片/分栏/间距统一（复用既有断点体系） | B |

## 验收（老板规则：逐端点验收）

- serve 起本地全链：`/v1/futu/account`（含多账户）、`/v1/futu/orders`、`/v1/futu/options`、watchlist、backtests 全部 200 且字段符合预期。
- Dashboard 页：聚合数据正确、子账户列表正确、订单状态正确显示、paper 徽章可见且颜色区分。
- `go test -race ./...`、`go vet`、`scripts/verify.sh` 全绿后提交。

## 非目标

- 实盘下单入口（需老板确认,doc/FUTU.md 安全策略）。
- 移动端专项适配（响应式断点已预埋）。

## 仓库内链回

- 需求源：老板 2026-08-02 Data 页指令；[[2026-07-31-web-v1-target]]（⑫ 策略模块 Web 化）
- 现状：`internal/webui/web/index.html`、`internal/httpapi/futu_account.go`、`internal/futu/trade.go`（GetOrderList 可用）

## 状态（2026-08-03）

✅ **已完成并合入**：Data 页改 Dashboard（账户聚合卡片 + 订单状态
+ Paper/实盘标识 + 视觉优化 + 30s 自动轮询 + 新鲜度打点）已落地。
闭环记录见 `doc/tasks/2026-08-02-dashboard.md`、
`doc/tasks/2026-08-02-devup-auto-restart.md` 等。

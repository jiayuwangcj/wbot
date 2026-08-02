# watchlist 一键回测 (S-UI-watchlist-backtest) — 2026-08-02

状态: ✅ 已合并 (PR #143, commit ab78042)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):「配置→回测→看结果」
闭环此前缺 UI 侧入口——oneclick-backtest(服务端 POST /v1/backtests +
run-watchlist 复选框)已交付,但 watchlist 行内无法直接对单标的一键回测,
必须手抄代码/策略到 results 页表单。参考富途/IB 工作台补上行级动作。

## 改动
1. **app.js**:
   - `renderWatchlist(items, onEdit, onDelete, onBacktest)` 签名加第四参;
     行操作区加「回测」按钮(位于 编辑/删除 之间),click → onBacktest(item)。
   - `runBacktest(item)`:POST /v1/backtests,body 用该行绑定策略
     (`{symbol, strategy}`,有 params 时 `if (item.params) body.params = item.params`),
     成功后 `location.href = "/ui/results.html#bt-" + res.id` 跳转并打开详情,
     失败走行级错误提示(showError)。
   - initResultsPage 末尾解析 `location.hash.match(/^#bt-(\d+)$/)` →
     `openDetail(Number(bt[1]))`,兼容直接带 hash 进入。
2. 测试: TestWatchlistBacktestJS(11 条契约断言:四参签名、按钮文案、
   回调、body 构造、POST method、hash 跳转、hash 解析)。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh smoke 10/10
- 逐端点验收 8/8:serve 实际吐出的 app.js 契约(5 条)+ 真实 POST
  一键回测(BTEXECOPT.US/covered-call → 201 id=213)+ hash 目标详情
  端点 200 + 无数据标的(HK.00700)503 no_data 错误语义可见
- CI: 5/5 全 pass 首轮绿;PR #143 merged

## 备注
- 验收小坑:本地 serve 的 watchlist 条目(HK.00700/SAVE.US)均无完整
  bars+option_quotes 数据,直接 POST 会 503 no_data——这是真实错误
  语义而非 bug(前端 showError 可见);端到端 201 用 dev-up 预置的
  BTEXECOPT.US(bars 5 fwd + option_quotes 7 fwd 齐备)验证。
- 覆盖表(Data 页)可作为后续「哪些标的有数据」的可视入口,与一键回测
  联动(无数据标的的 503 提示里已含 ingest action 指引)。

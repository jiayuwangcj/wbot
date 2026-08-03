# 排期:watchlist 列表标题记录计数(富途「自选 N」惯例)

## 状态

**✅ 已完成**(2026-08-03)。

## 来源

AUTO_ADVANCE Round 35。候选池巡检(ingest -from/-to、Provider 抽象、外部 cron 文档三候选再次实证全已实现)后按老板 Goal「打磨 UI 不停」评估 UI 候选:回测结果页计数因服务端 `/v1/backtests` 返回纯数组无 total 字段而放弃;watchlist **整表加载**(无分页),标题旁显示记录数即准确总量——落地券商面板惯例(富途「自选 N」)。

## 变更

- `watchlist.html:45`:`<h2>观察列表 <span id="watchlist-count" class="section-tag"></span></h2>`——section-tag 样式复用既有惯例
- `app.js` renderWatchlist:空列表显示空串,否则 `items.length + " 个标的"`;整表加载,数即总量
- `webui_test.go`:HTML 契约断言(want 列表加 `id="watchlist-count"`)+ TestWatchlistSortJS 追加两条 JS 断言(`getElementById("watchlist-count")`、`count.textContent = ...`)

## 验证

- verify.sh 连跑两遍全绿(go fmt / test / vet / race / staticcheck / build)
- E2E(真实 PG + watchlist API 3 条:BTEXEC.US/BTEXECB.US buy-hold + HK.00700 option-watch):`/ui/watchlist.html` HTTP 200,渲染确认计数元素;计数逻辑由单测 pin
- CI 五检查全绿(governance / test / db-integration / check-skip / ci-summary);#315 merge --admin

## 收益

券商面板惯例对齐(富途「自选 N」);列表状态信息一屏可得。空态/非空态契约由测试 pin,防回归。

## 引擎经验

**候选评估要先确认数据形状**:回测页计数候选因服务端无 total(纯数组)当场放弃——「先查 API 形状再选 UI 落点」避免做错功能;watchlist 整表加载是计数候选成立的前提。

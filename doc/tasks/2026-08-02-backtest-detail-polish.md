# 回测结果子视图打磨 (S-UI-backtest) — 2026-08-02

状态: ✅ 已合并 (PR #109, commit 2ce13d6)

## 背景
AUTO_ADVANCE 根任务循环 ⑤ UI 打磨切片:回测页结果子视图。老板长期目标
「参考富途/IB/嘉信把交互做好」。审查发现既有 detail-back/run-ok 导向
均已存在,真实缺口在长回测的展示性能与详情定位。

## 改动
1. **trades 限量渲染** (`app.js` renderTradesTable + `TRADES_LIMIT = 100`):
   长回测 trades 上千条时只渲染最近 100 条,超限显示「共 N 笔交易,仅显示
   最近 100 笔」提示 + 「显示全部」按钮(点击全量重绘)。避免 DOM 爆炸、
   页面过长。
2. **列表选中态** (`app.js` selectResultsRow + `tr.dataset.id` + `style.css`
   `tr.selected`): 打开详情时高亮结果列表中当前行(inset accent 边条),
   从详情返回列表一眼定位。
3. 元素: `results.html` 增 `trades-limit-hint` + `trades-show-all`;
   测试 `webui_test.go` 增 TestAppJSTradesLimitContract + 元素断言。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 7/7(三个静态资源的新元素 + JS 契约)
- 真实回测 run(194, buy-hold)详情 API 结构验证
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- 限流触发(>100 trades)实机未复现: 本地 demo 数据最长 6 bars / mock 固定
  3 bars / covered-call 需期权链数据。该分支为纯前端 10 行逻辑,由契约
  测试断言 slice(-TRADES_LIMIT) + hint/showAll 引用。
- 长数据实测(可选后续): `ingest file` 造 2000 bars + futu-option 链可触发。

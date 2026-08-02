# 闭环 #38: API.md Web UI 表刷新(Dashboard 行 + 补 data.html 行)

- **日期**: 2026-08-03
- **PR**: #196(功能+归档合一, doc-only)
- **背景**: 「文档欠账对账」引擎在 **Web UI 维度**的延伸——之前对账只看 `/v1` 端点章节,未对账 Web UI 表。核对发现两处欠账:
  1. `GET /ui/` 行仍描述 2026-08-02 Dashboard 改造**之前**的旧数据页(bars/runs 查询骨架 + 报价卡 + 账户卡)——bars 查询早已移至 data.html
  2. `GET /ui/data.html` 行**从未列出**(数据页: bars 表单/覆盖总览/期权新鲜度/补数据/拉取期权链/行情明细)

## 改动

`doc/API.md` Web UI 表:

- `GET /ui/` 行重写为 Dashboard 描述: 账户资产卡(`/v1/futu/account`)+ 资产曲线(`/v1/account/snapshots`,悬停/触摸读数)+ 子账户/持仓/订单 + 最近入库(`/v1/runs`)+ Paper/Live 环境切换
- 新增 `GET /ui/data.html` 行: bars 查询表单 + 覆盖总览(`bars_coverage`)+ 期权新鲜度(`options_freshness`)+ 补数据/拉取期权链(`POST /v1/ingest`)+ 行情明细(周期 tab/涨跌幅)
- watchlist/results/admin 行核对无误,保持

## 验证

- grep 断言: 表含 Dashboard 行与 data.html 行;旧「bars/runs 查询骨架」描述已移除
- CI 5/5 全绿;无 Go 改动

## 备注

- **引擎经验**: 「文档欠账对账」要覆盖 API.md 的全部表结构(/v1 端点章节 + Web UI 表),页面演进后描述行需同步;行内功能描述以实际 html id 核对为准(grep `id="..."`)。
- **候选池**: 仍枯竭;下一步候选: 无自主可推进项;等待老板拍板/资源/新需求。

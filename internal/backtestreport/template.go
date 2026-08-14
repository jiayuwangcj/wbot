package backtestreport

import (
	"fmt"
	"html/template"
)

type htmlData struct {
	Report       *Report
	Details      string
	Unfilled     string
	WindowMark   string
	WindowAmount string
	Coverage     string
	Assignment   string
	StopReason   string
}

func percent(v float64) string                 { return fmt.Sprintf("%.2f%%", v*100) }
func amount(v float64, currency string) string { return fmt.Sprintf("%s %.2f", currency, v) }
func percentPtr(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return percent(*v)
}
func amountPtr(v *float64, currency string) string {
	if v == nil {
		return "N/A"
	}
	return amount(*v, currency)
}

var reportTemplate = template.Must(template.New("backtest-report").Funcs(template.FuncMap{
	"percent":    percent,
	"amount":     amount,
	"percentPtr": percentPtr,
	"amountPtr":  amountPtr,
}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
  <meta name="theme-color" content="#090d16">
  <meta property="og:title" content="回测报告 {{.Report.Identity.Symbol}}">
  <meta property="og:description" content="{{.Report.Identity.CapabilityStatus}} · 窗口末估值变动 {{.WindowMark}} · 最大回撤 {{percent .Report.Result.MaxDrawdownPct}}">
  <title>回测报告 {{.Report.Identity.Symbol}}</title>
  <style>
    :root{color-scheme:dark;--bg:#090d16;--panel:#111827;--line:#273449;--text:#edf2f7;--muted:#93a4ba;--cyan:#54d2d2;--amber:#f2bd5c;--red:#ff7d8a}*{box-sizing:border-box}html,body{margin:0;max-width:100%;overflow-x:hidden;background:var(--bg);color:var(--text);font:15px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}body{padding:max(18px,env(safe-area-inset-top)) max(14px,env(safe-area-inset-right)) max(24px,env(safe-area-inset-bottom)) max(14px,env(safe-area-inset-left))}.shell{width:min(920px,100%);margin:auto}header{padding:10px 2px 18px}h1{margin:3px 0;font-size:clamp(24px,8vw,38px);line-height:1.1;overflow-wrap:anywhere}.eyebrow,.label,.meta{color:var(--muted);font-size:12px;letter-spacing:.08em;text-transform:uppercase}.status{display:inline-flex;margin-top:10px;padding:5px 10px;border:1px solid var(--amber);border-radius:999px;color:var(--amber);font-weight:700;overflow-wrap:anywhere}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.card,details{min-width:0;border:1px solid var(--line);border-radius:14px;background:linear-gradient(145deg,#131c2c,#0e1522);box-shadow:0 12px 35px #0004}.card{padding:15px}.value{margin-top:5px;color:var(--cyan);font-size:clamp(20px,6vw,29px);font-weight:750;line-height:1.15;overflow-wrap:anywhere}.sub{margin-top:4px;color:var(--muted);font-size:12px}.risk{margin:16px 0;padding:14px;border-left:3px solid var(--amber);background:#f2bd5c12;border-radius:8px}.risk p{margin:4px 0;overflow-wrap:anywhere}details{margin-top:12px;padding:0 14px}summary{padding:14px 0;cursor:pointer;font-weight:700}dl{display:grid;grid-template-columns:minmax(90px,1fr) minmax(0,2fr);gap:7px 12px;margin:0 0 15px}dt{color:var(--muted)}dd{margin:0;text-align:right;overflow-wrap:anywhere}pre{max-width:100%;margin:0 0 14px;padding:12px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;border-radius:8px;background:#070b12;color:#c9d6e5;font:11px/1.45 ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:430px){body{padding-left:12px;padding-right:12px}.card{padding:12px}.value{font-size:20px}dl{grid-template-columns:1fr}dd{text-align:left;margin-bottom:5px}}
  </style>
</head>
<body><main class="shell">
  <header><div class="eyebrow">schema {{.Report.SchemaVersion}} · {{.Report.ReportID}}</div><h1>{{.Report.Identity.Symbol}} 回测报告</h1><div class="status">{{.Report.Identity.CapabilityStatus}}</div></header>
    <section class="grid" aria-label="核心指标">
      <article class="card"><div class="label">本金</div><div class="value">{{amount .Report.InitialCash .Report.Identity.Currency}}</div><div class="sub">initial_cash</div></article>
      <article class="card"><div class="label">期末权益</div><div class="value">{{amountPtr .Report.Result.FinalEquityAmount .Report.Identity.Currency}}</div><div class="sub">{{.Report.Result.ReturnStatus}}</div></article>
      {{if .Report.Result.PremiumNetReturnPct}}<article class="card"><div class="label">权利金净额口径</div><div class="value">{{percentPtr .Report.Result.PremiumNetReturnPct}}</div><div class="sub">{{amountPtr .Report.Result.PremiumNetReturnAmount .Report.Identity.Currency}} · 权利金收入 − 平仓成本 − 期权/行权费用 · 年化 {{percentPtr .Report.Result.PremiumAnnualizedReturnPct}}</div></article>
      <article class="card"><div class="label">已实现口径</div><div class="value">{{percentPtr .Report.Result.RealizedReturnPct}}</div><div class="sub">{{amountPtr .Report.Result.RealizedReturnAmount .Report.Identity.Currency}} · 含正股已实现盈亏及全部费用 · 年化 {{percentPtr .Report.Result.RealizedAnnualizedReturnPct}}</div></article>{{else}}
      <article class="card"><div class="label">已实现盈利</div><div class="value">{{percentPtr .Report.Result.NetReturnPct}}</div><div class="sub">{{.Report.Result.ReturnStatus}} · 年化 {{percentPtr .Report.Result.AnnualizedReturnPct}}</div></article>{{end}}
      <article class="card"><div class="label">市值标记变动（研究值）</div><div class="value">{{.WindowMark}}</div><div class="sub">{{.WindowAmount}} · 浮盈浮亏依赖战略参数</div></article>
    <article class="card"><div class="label">最大回撤</div><div class="value">{{percent .Report.Result.MaxDrawdownPct}}</div></article>
    <article class="card"><div class="label">未成交率</div><div class="value">{{.Unfilled}}</div><div class="sub">{{.Report.Result.UnfilledCount}} / {{.Report.Result.AttemptCount}} 次尝试</div></article>
    <article class="card"><div class="label">停止原因</div><div class="value">{{.StopReason}}</div></article>
    <article class="card"><div class="label">数据有效覆盖率</div><div class="value">{{.Coverage}}</div><div class="sub">阻塞 {{.Report.DataQuality.BlockedBarCount}} / {{.Report.DataQuality.TotalBarCount}} bars</div></article>
    <article class="card"><div class="label">窗口末未平仓腿</div><div class="value">{{.Report.Terminal.OpenOptionLegCount}}</div><div class="sub">{{.Report.Terminal.SettlementStatus}}</div></article>
  </section>
  <section class="risk" aria-label="风险提示">{{range .Report.Risk}}<p>{{.}}</p>{{end}}</section>
    <details><summary>窗口末持仓与机械结算</summary><dl><dt>估值状态</dt><dd>{{.Report.Terminal.ValuationStatus}}</dd><dt>现金</dt><dd>{{amount .Report.Terminal.CashAmount .Report.Identity.Currency}}</dd><dt>持仓末值</dt><dd>{{amountPtr .Report.Terminal.HoldingsMarketValueAmount .Report.Identity.Currency}}（{{percentPtr .Report.Result.HoldingsMarketValuePct}} of initial_cash）</dd><dt>正股股数 / 市值</dt><dd>{{.Report.Terminal.StockShares}} / {{amount .Report.Terminal.StockMarketValueAmount .Report.Identity.Currency}}（{{percentPtr .Report.Result.StockMarketValuePct}}）</dd><dt>期权持仓末值</dt><dd>{{amountPtr .Report.Terminal.OptionMarketValueAmount .Report.Identity.Currency}}（{{percentPtr .Report.Result.OptionMarketValuePct}} of initial_cash）</dd><dt>已实现 P&amp;L</dt><dd>{{amountPtr .Report.Terminal.RealizedPnLAmount .Report.Identity.Currency}}</dd><dt>未实现 P&amp;L</dt><dd>{{amountPtr .Report.Terminal.UnrealizedPnLAmount .Report.Identity.Currency}}</dd><dt>到期 / 空头到期 / 指派</dt><dd>{{.Report.Terminal.ExpiryCount}} / {{.Report.Terminal.ShortExpiryCount}} / {{.Report.Terminal.AssignmentCount}}</dd><dt>机械指派率</dt><dd>{{.Assignment}}</dd><dt>事件口径</dt><dd>{{.Report.Terminal.EventBasis}}</dd></dl></details>
    <details><summary>数据质量</summary><dl><dt>状态</dt><dd>{{.Report.DataQuality.Status}}</dd><dt>标的 bars 数据源 / 复权</dt><dd>{{range .Report.DataQuality.UnderlyingBars}}{{.Source}}/{{.Adjusted}} ({{.BarCount}} bars) {{else}}not_available{{end}}</dd><dt>期权 snapshot 数据源</dt><dd>{{if .Report.DataQuality.OptionSnapshotSources}}{{.Report.DataQuality.OptionSnapshotSources}}{{else}}not_available{{end}}</dd><dt>snapshot 批次 / 合约行</dt><dd>{{.Report.DataQuality.SnapshotBatchCount}} / {{.Report.DataQuality.SnapshotContractRowCount}}</dd><dt>历史完整到期周期</dt><dd>{{if .Report.DataQuality.HistoricalOptionCycleComplete}}{{.Report.DataQuality.HistoricalOptionCycleComplete}}{{else}}not_applicable{{end}}</dd><dt>阻塞项</dt><dd>{{.Report.DataQuality.BlockedBy}}</dd></dl></details>
    <details><summary>身份、收益与费用审计</summary><dl><dt>数据区间</dt><dd>{{.Report.Identity.DataWindow.From}} — {{.Report.Identity.DataWindow.To}}</dd><dt>运行种子</dt><dd>{{.Report.Identity.RunSeed}}</dd><dt>代码版本</dt><dd>{{.Report.Identity.CodeVersion}}</dd><dt>输入快照</dt><dd>{{.Report.Audit.InputSnapshotHash}}</dd><dt>基线收益</dt><dd>{{percent .Report.Result.BaselineReturnPct}}</dd><dt>{{if .Report.Result.PremiumNetReturnPct}}权利金净收益 / 毛收益（评价口径）{{else}}已实现净 / 毛收益{{end}}</dt><dd>{{percentPtr .Report.Result.NetReturnPct}} / {{percentPtr .Report.Result.GrossReturnPct}}</dd>{{if .Report.Result.PremiumNetReturnPct}}<dt>权利金净额口径</dt><dd>{{amountPtr .Report.Result.PremiumNetReturnAmount .Report.Identity.Currency}}（{{percentPtr .Report.Result.PremiumNetReturnPct}}，年化 {{percentPtr .Report.Result.PremiumAnnualizedReturnPct}}）</dd><dt>已实现口径</dt><dd>{{amountPtr .Report.Result.RealizedReturnAmount .Report.Identity.Currency}}（{{percentPtr .Report.Result.RealizedReturnPct}}，年化 {{percentPtr .Report.Result.RealizedAnnualizedReturnPct}}）</dd>{{end}}<dt>市值标记变动（研究值）</dt><dd>{{percentPtr .Report.Result.WindowMarkToMarketReturnPct}}（{{amountPtr .Report.Result.WindowMarkToMarketAmount .Report.Identity.Currency}}）</dd><dt>总费用</dt><dd>{{amount .Report.Result.CostDrag.TotalFeesAmount .Report.Identity.Currency}}（本金 {{percent .Report.Result.CostDrag.CostDragPct}}，收益率拖累 {{percent .Report.Result.CostDrag.CostDragReturnPct}}）</dd><dt>期权费用</dt><dd>{{amount .Report.Result.CostModel.OptionFeesAmount .Report.Identity.Currency}}（{{.Report.Result.CostModel.Option.Contracts}} 张）</dd><dt>正股 / 行权交割费用</dt><dd>{{amount .Report.Result.CostModel.StockFeesAmount .Report.Identity.Currency}} / {{amount .Report.Result.CostModel.ExerciseDeliveryFeesAmount .Report.Identity.Currency}}</dd><dt>收益归因（已实现）</dt><dd>权利金收入 {{amount .Report.Result.Attribution.PremiumIncomeAmount .Report.Identity.Currency}} − 平仓成本 {{amount .Report.Result.Attribution.OptionCloseCostAmount .Report.Identity.Currency}} + 正股卖出已实现 {{amount .Report.Result.Attribution.StockRealizedPnLAmount .Report.Identity.Currency}} − 费用 {{amount .Report.Result.Attribution.FeesAmount .Report.Identity.Currency}} = 已实现 {{amount .Report.Result.Attribution.RealizedPnLAmount .Report.Identity.Currency}}</dd><dt>未成交机会成本</dt><dd>{{.Report.Result.Attribution.UnfilledAttemptCount}} 次尝试放弃权利金 {{amount .Report.Result.Attribution.UnfilledAttemptPremium .Report.Identity.Currency}}</dd></dl></details>
  <details><summary>完整 JSON（唯一事实源）</summary><pre>{{.Details}}</pre></details>
</main></body></html>
`))

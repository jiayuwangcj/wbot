package backtestreport

import (
	"fmt"
	"html/template"
)

type htmlData struct {
	Report     *Report
	Details    string
	Unfilled   string
	StopReason string
}

func percent(v float64) string                 { return fmt.Sprintf("%.2f%%", v*100) }
func amount(v float64, currency string) string { return fmt.Sprintf("%s %.2f", currency, v) }

var reportTemplate = template.Must(template.New("backtest-report").Funcs(template.FuncMap{
	"percent": percent,
	"amount":  amount,
}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
  <meta name="theme-color" content="#090d16">
  <meta property="og:title" content="回测报告 {{.Report.Identity.Symbol}}">
  <meta property="og:description" content="{{.Report.Identity.CapabilityStatus}} · 净收益 {{percent .Report.Result.NetReturnPct}} · 最大回撤 {{percent .Report.Result.MaxDrawdownPct}}">
  <title>回测报告 {{.Report.Identity.Symbol}}</title>
  <style>
    :root{color-scheme:dark;--bg:#090d16;--panel:#111827;--line:#273449;--text:#edf2f7;--muted:#93a4ba;--cyan:#54d2d2;--amber:#f2bd5c;--red:#ff7d8a}*{box-sizing:border-box}html,body{margin:0;max-width:100%;overflow-x:hidden;background:var(--bg);color:var(--text);font:15px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}body{padding:max(18px,env(safe-area-inset-top)) max(14px,env(safe-area-inset-right)) max(24px,env(safe-area-inset-bottom)) max(14px,env(safe-area-inset-left))}.shell{width:min(920px,100%);margin:auto}header{padding:10px 2px 18px}h1{margin:3px 0;font-size:clamp(24px,8vw,38px);line-height:1.1;overflow-wrap:anywhere}.eyebrow,.label,.meta{color:var(--muted);font-size:12px;letter-spacing:.08em;text-transform:uppercase}.status{display:inline-flex;margin-top:10px;padding:5px 10px;border:1px solid var(--amber);border-radius:999px;color:var(--amber);font-weight:700;overflow-wrap:anywhere}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.card,details{min-width:0;border:1px solid var(--line);border-radius:14px;background:linear-gradient(145deg,#131c2c,#0e1522);box-shadow:0 12px 35px #0004}.card{padding:15px}.value{margin-top:5px;color:var(--cyan);font-size:clamp(20px,6vw,29px);font-weight:750;line-height:1.15;overflow-wrap:anywhere}.sub{margin-top:4px;color:var(--muted);font-size:12px}.risk{margin:16px 0;padding:14px;border-left:3px solid var(--amber);background:#f2bd5c12;border-radius:8px}.risk p{margin:4px 0;overflow-wrap:anywhere}details{margin-top:12px;padding:0 14px}summary{padding:14px 0;cursor:pointer;font-weight:700}dl{display:grid;grid-template-columns:minmax(90px,1fr) minmax(0,2fr);gap:7px 12px;margin:0 0 15px}dt{color:var(--muted)}dd{margin:0;text-align:right;overflow-wrap:anywhere}pre{max-width:100%;margin:0 0 14px;padding:12px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;border-radius:8px;background:#070b12;color:#c9d6e5;font:11px/1.45 ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:430px){body{padding-left:12px;padding-right:12px}.card{padding:12px}.value{font-size:20px}dl{grid-template-columns:1fr}dd{text-align:left;margin-bottom:5px}}
  </style>
</head>
<body><main class="shell">
  <header><div class="eyebrow">schema {{.Report.SchemaVersion}} · {{.Report.ReportID}}</div><h1>{{.Report.Identity.Symbol}} 回测报告</h1><div class="status">{{.Report.Identity.CapabilityStatus}}</div></header>
  <section class="grid" aria-label="核心指标">
    <article class="card"><div class="label">净收益</div><div class="value">{{percent .Report.Result.NetReturnPct}}</div><div class="sub">{{amount .Report.Result.NetReturnAmount .Report.Identity.Currency}}</div></article>
    <article class="card"><div class="label">最大回撤</div><div class="value">{{percent .Report.Result.MaxDrawdownPct}}</div></article>
    <article class="card"><div class="label">未成交率</div><div class="value">{{.Unfilled}}</div><div class="sub">{{.Report.Result.UnfilledCount}} / {{.Report.Result.AttemptCount}} 次尝试</div></article>
    <article class="card"><div class="label">停止原因</div><div class="value">{{.StopReason}}</div></article>
  </section>
  <section class="risk" aria-label="风险提示">{{range .Report.Risk}}<p>{{.}}</p>{{end}}</section>
	  <details><summary>身份与审计</summary><dl><dt>数据区间</dt><dd>{{.Report.Identity.DataWindow.From}} — {{.Report.Identity.DataWindow.To}}</dd><dt>运行种子</dt><dd>{{.Report.Identity.RunSeed}}</dd><dt>代码版本</dt><dd>{{.Report.Identity.CodeVersion}}</dd><dt>输入快照</dt><dd>{{.Report.Audit.InputSnapshotHash}}</dd><dt>基线收益</dt><dd>{{percent .Report.Result.BaselineReturnPct}}</dd><dt>总费用</dt><dd>{{amount .Report.Result.CostModel.TotalFeesAmount .Report.Identity.Currency}}</dd><dt>期权费用</dt><dd>{{amount .Report.Result.CostModel.OptionFeesAmount .Report.Identity.Currency}}</dd></dl></details>
  <details><summary>完整 JSON（唯一事实源）</summary><pre>{{.Details}}</pre></details>
</main></body></html>
`))

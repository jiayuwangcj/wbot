package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func serveGet(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	FileServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesIndex(t *testing.T) {
	rec := serveGet(t, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q; want text/html", ct)
	}
	for _, want := range []string{"<title>wbot · Dashboard</title>", `name="viewport"`, "/ui/style.css", "/ui/admin.html", "/ui/data.html", "/ui/app.js"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("index missing %q: %s", want, rec.Body)
		}
	}
}

func TestServesWatchlistPage(t *testing.T) {
	rec := serveGet(t, "/watchlist.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q; want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · 策略</title>") {
		t.Fatalf("watchlist missing title: %s", rec.Body)
	}
}

func TestServesDataPage(t *testing.T) {
	rec := serveGet(t, "/data.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · 数据</title>") {
		t.Fatalf("data missing title: %s", rec.Body)
	}
}

func TestDataPageContract(t *testing.T) {
	html, err := fs.ReadFile(webFiles, "web/data.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="bars-form"`,
		`id="bars-symbol"`,
		`id="bars-timeframe"`,
		`id="coverage-table"`,
		`id="coverage-empty"`,
		`id="coverage-error"`,
		`id="detail-sparkline"`,
		`id="bars-table"`,
		`id="bars-empty"`,
		`id="detail-error"`,
		`id="data-refresh"`,
	} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("data.html missing %q", want)
		}
	}
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"initDataPage",
		"loadDataCoverage",
		"loadBars",
		"drawSparkline",
		`"/v1/bars?symbol="`,
		`&desc=1`,
		`"/v1/admin/cluster"`,
	} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing data-page logic %q", want)
		}
	}
}

// TestDataNavLinks: 数据 页在五个页面导航中互通(老板 2026-08-02 补需求)。
func TestDataNavLinks(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html", "web/results.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "/ui/data.html") {
			t.Fatalf("%s missing data nav link", path)
		}
	}
}

func TestServesResultsPage(t *testing.T) {
	rec := serveGet(t, "/results.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q; want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · 回测</title>") {
		t.Fatalf("results missing title: %s", rec.Body)
	}
}

func TestServesAdminPlaceholder(t *testing.T) {
	rec := serveGet(t, "/admin.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · Admin</title>") {
		t.Fatalf("admin missing title: %s", rec.Body)
	}
}

func TestServesStaticAssets(t *testing.T) {
	tests := []struct {
		path string
		ct   string
	}{
		{"/style.css", "text/css"},
		{"/app.js", "text/javascript"},
	}
	for _, tt := range tests {
		rec := serveGet(t, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; want 200 (body %s)", tt.path, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.ct) {
			t.Fatalf("%s: content-type = %q; want %s prefix", tt.path, ct, tt.ct)
		}
	}
}

func TestMissingFileIs404(t *testing.T) {
	rec := serveGet(t, "/nope.txt")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestNoExternalURLs(t *testing.T) {
	var files []string
	fs.WalkDir(webFiles, "web", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	for _, path := range files {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, banned := range []string{"http://", "https://", "//"} {
			if strings.Contains(string(data), banned) {
				t.Fatalf("%s contains banned external URL marker %q", path, banned)
			}
		}
	}
}

func TestViewportMetaOnAllPages(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html", "web/results.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), `name="viewport"`) {
			t.Fatalf("%s missing viewport meta", path)
		}
	}
}

// TestThemeToggleOnAllPages: every page carries the theme toggle in the header
// (UI 主题化 — dark mode + persisted manual preference).
func TestThemeToggleOnAllPages(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html", "web/results.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), `id="theme-toggle"`) {
			t.Fatalf("%s missing theme toggle button", path)
		}
	}
}

func TestThemeSystem(t *testing.T) {
	css, err := fs.ReadFile(webFiles, "web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`html[data-theme="dark"]`,
		"@media (prefers-color-scheme: dark)",
		"var(--surface)", // hardcoded #fff replaced by tokens
	} {
		if !strings.Contains(string(css), want) {
			t.Fatalf("style.css missing %q", want)
		}
	}
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`localStorage.getItem("wbot-theme")`, "dataset.theme", "initTheme();", "redrawCharts()", "chartCache"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestResponsiveBreakpoints(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, want := range []string{"@media (min-width: 1024px)", "@media (max-width: 767px)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("style.css missing %q", want)
		}
	}
}

// TestTablesInScrollContainers: every data table must sit inside a .table-scroll
// wrapper so narrow screens scroll in-container instead of overflowing the page.
func TestTablesInScrollContainers(t *testing.T) {
	tableRe := regexp.MustCompile(`id="([a-z0-9-]+-table)"`)
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html", "web/results.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		for _, m := range tableRe.FindAllStringSubmatch(html, -1) {
			pos := strings.Index(html, `id="`+m[1]+`"`)
			if wrap := strings.LastIndex(html[:pos], `class="table-scroll"`); wrap == -1 {
				t.Fatalf("%s: table %q is not inside a .table-scroll container", path, m[1])
			}
		}
	}
}

// TestMobileBreakpointStyles: the 767px media query must cover the mobile
// layout rules (stacked forms, 44px tap targets, scrollable tables).
func TestMobileBreakpointStyles(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	start := strings.Index(css, "@media (max-width: 767px)")
	if start == -1 {
		t.Fatal("style.css missing mobile breakpoint")
	}
	// Find the media query block by matching braces.
	depth, end := 0, len(css)
	for i := start; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				i = len(css)
			}
		}
	}
	mobile := css[start:end]
	for _, want := range []string{
		"flex-direction: column", // forms and fieldsets stack vertically
		"form > button",          // submit buttons full width
		"width: 100%",
		"min-height: 44px", // touch targets >= 44px
		"min-width: 600px", // tables scroll inside .table-scroll
		"display: block",   // nav links stack instead of overflowing
	} {
		if !strings.Contains(mobile, want) {
			t.Fatalf("mobile media query missing %q", want)
		}
	}
}

// TestDashboardPageContract: Data 页已改 Dashboard(老板 2026-08-02):bars/quote
// 区块删除,改为账户聚合 + 子账户明细 + 订单面板 + Paper/实盘徽章。
func TestDashboardPageContract(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`id="env-badge"`,
		`id="env-paper"`,
		`id="env-real"`,
		`id="summary-total-assets"`,
		`id="accounts-table"`,
		`id="positions-table"`,
		`id="orders-table"`,
		`id="runs-table"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("index.html missing %q", want)
		}
	}
	for _, gone := range []string{`id="bars-form"`, `id="quote-card"`, `id="bars-table"`} {
		if strings.Contains(html, gone) {
			t.Fatalf("index.html still contains removed %q (bars/quote deleted)", gone)
		}
	}
}

func TestAppJSQueriesDashboardAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{"fetch(", `"/v1/runs`, `"/v1/futu/account?env=`, `"/v1/futu/orders?env=`} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
	/* /v1/bars 属于数据页(initDataPage);Dashboard 初始化块不得触碰 bars。 */
	start := strings.Index(js, "function initDashboardPage")
	adminMark := strings.Index(js, "/* Admin page:")
	if start == -1 || adminMark == -1 || start > adminMark {
		t.Fatal("cannot locate initDashboardPage block")
	}
	dash := js[start:adminMark]
	if strings.Contains(dash, "/v1/bars") || strings.Contains(dash, "loadBars") {
		t.Fatal("initDashboardPage block still calls /v1/bars (bars 已移至数据页)")
	}
}

// TestAppJSQuoteRemovedFromUI: Dashboard 不再内嵌看盘工具(老板:不需要看盘工具);
// /v1/futu/quote 端点仍在(API 层),但 UI 不再调用。
func TestAppJSQuoteRemovedFromUI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	if strings.Contains(js, `"/v1/futu/quote`) {
		t.Fatalf("app.js still calls /v1/futu/quote (看盘工具已从 UI 移除)")
	}
}

// TestAdminPageSections: 页4 集群节点状态概览(老板 2026-08-02) — 4 张节点卡
// (Process/Database/Pipeline/Data plane) 各带状态徽章;过细列表
// (recent runs、coverage 表)与冗余 status 区已移除。
func TestAdminPageSections(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<section id="cluster"`,
		`<section id="config"`,
		`id="admin-refresh"`,
		`id="cluster-error"`,
		`id="config-error"`,
		`id="config-table"`,
		`id="cluster-cards"`,
		`id="cluster-process-badge"`,
		`id="cluster-db-badge"`,
		`id="cluster-pipeline-badge"`,
		`id="cluster-data-badge"`,
		`id="cluster-data-series"`,
		`id="cluster-data-stale"`,
		`id="cluster-data-newest"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("admin.html missing %q", want)
		}
	}
	for _, gone := range []string{`<section id="status"`, "cluster-pipeline-runs-table", "cluster-coverage-table"} {
		if strings.Contains(html, gone) {
			t.Fatalf("admin.html must not contain %q (过细列表已移除)", gone)
		}
	}
}

// TestAdminPageFreshness: 页4 概览将新鲜度收进 Data plane 节点卡
// (stale 序列计数 + 徽章);freshness 行标记逻辑仍在 app.js(freshnessCell,
// 数据页逐行渲染用),样式保留。
func TestAdminPageFreshness(t *testing.T) {
	html, err := fs.ReadFile(webFiles, "web/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="cluster-data-stale"`, `id="cluster-data-badge"`} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("admin.html missing %q", want)
		}
	}
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"b.fresh",
		"freshness-stale",
		"freshness-unknown",
		"数据过期",
		"无数据",
		"freshnessCell",
		"cluster-data-stale",
	} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing freshness logic %q", want)
		}
	}
	css, err := fs.ReadFile(webFiles, "web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"td.freshness-stale", "td.freshness-unknown", ".badge.warn", ".badge.down"} {
		if !strings.Contains(string(css), want) {
			t.Fatalf("style.css missing freshness style %q", want)
		}
	}
}

func TestTableEmptyConvention(t *testing.T) {
	tableRe := regexp.MustCompile(`id="([a-z0-9-]+-table)"`)
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html", "web/results.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		for _, m := range tableRe.FindAllStringSubmatch(html, -1) {
			emptyID := strings.Replace(m[1], "-table", "-empty", 1)
			if !strings.Contains(html, `id="`+emptyID+`"`) {
				t.Fatalf("%s: table %q has no matching empty element %q", path, m[1], emptyID)
			}
		}
	}
}

func TestAdminPageReadOnly(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<form") {
		t.Fatal("admin.html contains a form; admin page must be read-only")
	}
}

func TestWatchlistPageElements(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/watchlist.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<form id="watchlist-form"`,
		`name="symbol"`,
		`id="strategy-select"`,
		`id="param-fields"`,
		`id="watchlist-table"`,
		`id="watchlist-empty"`,
		`id="watchlist-error"`,
		`id="watchlist-form-error"`,
		`id="watchlist-form-ok"`,
		`/ui/app.js`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("watchlist.html missing %q", want)
		}
	}
}

// TestStrategyCardsSection: options chain 已删(老板 2026-08-02,不需看盘工具),
// 策略页页首为策略说明卡(/v1/strategies schema 渲染)。
func TestStrategyCardsSection(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/watchlist.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<section id="strategies"`,
		`id="strategy-cards"`,
		`id="strategies-error"`,
		`class="strategy-cards"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("watchlist.html missing %q", want)
		}
	}
	for _, gone := range []string{`id="options"`, `id="options-form"`, `id="options-table"`, `id="options-expiry"`} {
		if strings.Contains(html, gone) {
			t.Fatalf("watchlist.html still contains removed %q (options chain 已删)", gone)
		}
	}
}

// TestAppJSOptionsRemoved: UI 不再调用 /v1/futu/options(端点仍在 API 层)。
func TestAppJSOptionsRemoved(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, gone := range []string{`"/v1/futu/options`, "initOptionsChainPage", "renderOptionChain", "renderOptionExpirySelect"} {
		if strings.Contains(js, gone) {
			t.Fatalf("app.js still has options-chain logic %q (已删)", gone)
		}
	}
}

// TestAppJSStrategyCards: 策略卡渲染 + 点击联动编辑表单。
func TestAppJSStrategyCards(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"renderStrategyCards",
		`s.params`,
		"strategy-card",
		"dataset.strategy",
		"strategySelect.value = card.dataset.strategy",
		"p.default",
		"p.description",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing strategy-card logic %q", want)
		}
	}
}

func TestWatchlistNavLinks(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "/ui/watchlist.html") {
			t.Fatalf("%s missing watchlist nav link", path)
		}
	}
}

func TestAppJSQueriesWatchlistAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`"/v1/watchlist`,
		`"/v1/strategies`,
		`method: "PUT"`,
		`method: "DELETE"`,
		"initWatchlistPage",
		"renderStrategyCards",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestAppJSDynamicParamForm(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`p.type === "choice"`,
		`p.type === "number"`,
		`"params." + p.name`,
		"p.choices",
		"p.default",
		"invalid number for ",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing schema-driven param form logic %q", want)
		}
	}
}

func TestAppJSQueriesAdminAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	/* 页4 概览化后 status 端点不再单独拉取(节点信息并入 cluster 卡) */
	for _, want := range []string{`"/v1/admin/cluster`, `"/v1/admin/config`} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
	if strings.Contains(js, `"/v1/admin/status`) {
		t.Fatal("app.js must not query /v1/admin/status (页4: 并入 cluster 概览)")
	}
}

func TestConfigMetadataOnly(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{"c.key", "c.group", "c.set", "c.updated_at"} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing config metadata access %q", want)
		}
	}
	if strings.Contains(js, "c.value") {
		t.Fatal("app.js renders config values (PRIVACY red line)")
	}
}

func TestResultsPageElements(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/results.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`id="backtest-form"`,
		`id="run-symbol"`,
		`id="run-watchlist"`,
		`id="run-strategy-select"`,
		`id="run-param-fields"`,
		`id="run-btn"`,
		`id="run-error"`,
		`id="run-ok"`,
		`id="results-table"`,
		`id="results-empty"`,
		`id="results-error"`,
		`id="detail"`,
		`id="metric-cards"`,
		`id="metric-equity"`,
		`id="metric-total-return"`,
		`id="metric-max-drawdown"`,
		`id="metric-bars"`,
		`id="equity-canvas"`,
		`id="curve-empty"`,
		`id="curve-wrap"`,
		`id="detail-extra"`,
		`id="trades-table"`,
		`id="trades-empty"`,
		`id="trades-limit-hint"`,
		`id="trades-show-all"`,
		`id="detail-params"`,
		`id="detail-back"`,
		`/ui/app.js`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("results.html missing %q", want)
		}
	}
}

func TestResultsNavLinks(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html", "web/results.html", "web/data.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "/ui/results.html") {
			t.Fatalf("%s missing results nav link", path)
		}
	}
}

func TestAppJSQueriesBacktestsAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`"/v1/backtests?limit=50"`,
		`"/v1/backtests/" + id`,
		"initResultsPage",
		"drawEquityCurve",
		"showDetailError",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// TestAppJSTradesLimitContract: 长回测 trades 限量渲染契约(UI 打磨切片):
// 默认只渲染最近 TRADES_LIMIT 条,超限显示提示 + 「显示全部」;打开详情时
// 高亮列表中当前行(tr.dataset.id + .selected)。
func TestAppJSTradesLimitContract(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"const TRADES_LIMIT = 100",
		"renderTradesTable",
		"rows.slice(-TRADES_LIMIT)",
		`"trades-limit-hint"`,
		`"trades-show-all"`,
		"tr.dataset.id = String(item.id)",
		"selectResultsRow",
		`tr.classList.toggle("selected", tr.dataset.id === String(id))`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// TestInteractionFeedbackJS: 交互反馈补全切片(2026-08-02):watchlist 保存
// 成功显式反馈(watchlist-form-ok,编辑/切策略/失败时隐藏) + admin 页刷新
// 按钮重载 cluster/config。
func TestInteractionFeedbackJS(t *testing.T) {
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"watchlist-form-ok"`,
		"formOk.textContent = \"已保存 \" + symbol",
		"formOk.hidden = false",
		"hideOk()",
		`"admin-refresh"`,
		"document.getElementById(\"admin-refresh\").addEventListener(\"click\", loadAll)",
		"const loadAll = () => {",
	} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// TestBacktestRunFormJS: the 回测页 run form (老板 2026-08-02 页3) posts to
// /v1/backtests with {symbol, strategy, params} or {from_watchlist: true},
// then refreshes the list and opens the new single-run detail.
func TestBacktestRunFormJS(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`"run-param-fields"`,
		`{from_watchlist: true}`,
		`method: "POST"`,
		`"/v1/backtests"`,
		"setupBacktestRunForm",
		"renderParamFields(strategyByName(select.value), null, \"run-param-fields\")",
		"symbolInput.disabled = watchlistCheck.checked",
		"openDetail(res.id)",
		"res.runs",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
	if !strings.Contains(js, `{symbol: symbol, strategy: strategy.name, params: collected.params}`) {
		t.Fatal("app.js run form must build {symbol, strategy, params} body")
	}
}

func TestAppJSErrorBodyConvention(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{"body.error", "body.message", "body.action"} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestAppJSEquityCurveDrawing(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`getContext("2d")`,
		"canvas.width",
		"canvas.height",
		"beginPath",
		"moveTo",
		"lineTo",
		"fillText",
		"Math.PI",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing curve drawing primitive %q", want)
		}
	}
	if strings.Contains(js, "d3") || strings.Contains(js, "chart.js") || strings.Contains(js, "echarts") {
		t.Fatal("app.js references an external chart library")
	}
}

func TestResultsCompareElements(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/results.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`id="compare"`,
		`id="compare-btn"`,
		`id="compare-hint"`,
		`id="compare-table"`,
		`id="compare-table-empty"`,
		`id="compare-canvas"`,
		`id="compare-empty"`,
		`id="compare-legend"`,
		`id="compare-curve-wrap"`,
		`id="compare-back"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("results.html missing %q", want)
		}
	}
}

func TestAppJSCompareView(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"row-check",
		"compareSelection",
		"Promise.all",
		"drawMultiCurve",
		"drawCurvePlot",
		"compare-legend",
		"compare-btn",
		"runLabel",
		"CURVE_ALT",
		"请选择恰好两条回测进行对比。",
		"series.find((s) => s.points.length > 0)", /* P1: empty first series (legacy run) must not crash the label loop */
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
	/* P1: compare section is revealed before drawing, so render/fetch errors stay visible. */
	rcStart := strings.Index(js, "function renderCompare")
	reveal := strings.Index(js[rcStart:], "compare.hidden = false")
	draw := strings.Index(js[rcStart:], "drawMultiCurve")
	if reveal == -1 || draw == -1 || reveal > draw {
		t.Fatalf("renderCompare must reveal the compare section before drawing")
	}
}

// TestUICopyLocalized: UI 文案中文化切片(2026-08-02):用户可见文案统一中文,
// 关键交互文案(空状态/按钮/错误提示)断言在位,防回退为英文。
func TestUICopyLocalized(t *testing.T) {
	for _, path := range []string{"web/results.html", "web/watchlist.html", "web/index.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "暂无") {
			t.Fatalf("%s missing localized empty-state copy", path)
		}
	}
	data, err := fs.ReadFile(webFiles, "web/results.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"暂无回测结果", "对比所选回测", "未记录交易", "该回测无权益曲线",
		"请选择恰好两条回测进行对比", "回测详情", "期末权益", "最大回撤", "权益曲线",
		"返回列表",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("results.html missing %q", want)
		}
	}
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"请选择策略", "运行中", "详情"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// TestDashboardAccountBlock: Dashboard 的账户区块(聚合卡 + 子账户表 + 持仓表)。
func TestDashboardAccountBlock(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`id="summary-error"`,
		`id="summary-total-assets"`,
		`id="summary-cash"`,
		`id="summary-market-val"`,
		`id="summary-power"`,
		`id="summary-available-cash"`,
		`id="accounts-table"`,
		`id="positions-table"`,
		`id="positions-empty"`,
		`id="dash-refresh"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("index.html missing %q", want)
		}
	}
}

// TestAutoRefreshJS: Dashboard 自动轮询契约(2026-08-02):30s 定时刷新,
// visibilitychange 隐藏暂停/可见恢复,避免后台持续打 futu 网关。
func TestAutoRefreshJS(t *testing.T) {
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const AUTO_REFRESH_MS = 30000",
		"function startAutoRefresh",
		"function stopAutoRefresh",
		`if (document.visibilityState === "visible") loadDashboard();`,
		"visibilitychange",
		`if (document.visibilityState === "hidden") stopAutoRefresh();`,
	} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestAppJSQueriesFutuAccountAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`"/v1/futu/account?env=`,
		"snap.funds.total_assets",
		"snap.funds.available_cash",
		"positions-table",
		"loadEnvSnap",
		"loadDashboard",
		"dash-refresh",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// TestNumCellSemanticColor: 盈亏/收益率语义色契约(券商 UI 惯例切片):
// numCell 以原始数值着色(>0 → num-up / <0 → num-down),持仓盈亏与回测
// 收益率列均走该路径;CSS 提供 td.num-up/td.num-down 三态色。
func TestNumCellSemanticColor(t *testing.T) {
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"function numCell(v, fmt)",
		`td.className = v > 0 ? "num-up" : v < 0 ? "num-down" : ""`,
		"numCell(p.pl)",
		"numCell(metricOf(item, \"total_return\"), fmtPct)",
	} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
	css, err := fs.ReadFile(webFiles, "web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"td.num-up", "td.num-down"} {
		if !strings.Contains(string(css), want) {
			t.Fatalf("style.css missing %q", want)
		}
	}
}

// TestEnumLocalizationJS: 动态枚举值中文化契约(2026-08-02):静态 HTML 文案
// 已中文化,JS 动态渲染的方向 buy/sell 与 runs 状态仍为英文,此切片加
// sideZh/statusZh 映射并接入三处渲染点;顺带修复 appendRow 对预构建 td
// 元素的字符串化 bug(2a15758 引入:sideTd 元素渲染成 "[object ...]",
// 订单方向列颜色从未生效)。
func TestEnumLocalizationJS(t *testing.T) {
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(js)
	for _, want := range []string{
		`const SIDE_ZH = {buy: "买入", sell: "卖出"}`,
		`const STATUS_ZH = {succeeded: "成功", failed: "失败", running: "运行中"}`,
		`function sideZh(v)`,
		`function statusZh(v)`,
		`|| v`, // 未知值原样透传,不误译
		// 三处接入点:订单方向 / 回测 trades 方向 / runs 状态
		"sideTd.textContent = sideZh(o.side)",
		`actionTd.textContent = sideZh(t.action)`,
		"statusZh(r.status)",
		// appendRow 元素支持(修复字符串化 bug)
		"cell instanceof Node ? cell",
		`String(t.action).toLowerCase() === "buy" ? "side-buy" : "side-sell"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
	// 订单方向 class 判断仍在(本地化不影响语义色)
	if !strings.Contains(s, `sideTd.className = o.side.toLowerCase() === "buy" ? "side-buy" : "side-sell"`) {
		t.Fatal("app.js lost renderOrders side class assignment")
	}
}

// TestResultsSortingJS: 回测结果表表头排序契约(2026-08-02):表头 th
// 带 data-sort 键、RESULTS_SORT_KEYS 取值器覆盖全部数值/字符串列、
// 点击切换升/降序、箭头指示、排序重绘后恢复详情选中高亮。
func TestResultsSortingJS(t *testing.T) {
	html, err := fs.ReadFile(webFiles, "web/results.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-sort="id"`,
		`data-sort="strategy"`,
		`data-sort="symbol"`,
		`data-sort="equity"`,
		`data-sort="total_return"`,
		`data-sort="max_drawdown"`,
		`data-sort="bars"`,
		`data-sort="created_at"`,
		"title=\"点击按此列排序\"",
	} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("results.html missing %q", want)
		}
	}
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(js)
	for _, want := range []string{
		"const RESULTS_SORT_KEYS = {",
		"total_return: (i) => metricOf(i, \"total_return\") ?? -Infinity",
		"created_at: (i) => i.created_at",
		"function sortResults(items)",
		"function renderSortIndicators()",
		"function initResultsSorting(render)",
		"let resultsSortKey = null",
		"resultsSortDir = -resultsSortDir",
		`(resultsSortDir === 1 ? " ↑" : " ↓")`,
		// 排序重绘后恢复选中高亮
		"openDetailId",
		"if (openDetailId !== null) selectResultsRow(openDetailId)",
		"initResultsSorting(render)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// TestBarsCoverageRemoved: bars 区块已随 Dashboard 改造迁出(老板 2026-08-02),
// 旧覆盖度提示逻辑移除 — 查看缓存数据的功能现由独立「数据」页承担(data.html,
// coverage-table + drill-in),Admin cluster 页仍有 freshness 覆盖表。
func TestBarsCoverageRemoved(t *testing.T) {
	html, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), `id="bars-coverage"`) {
		t.Fatalf("index.html still has bars-coverage (bars 已删除)")
	}
	js, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	/* Admin cluster 页仍有 bars_coverage 表格;被删的是 Dashboard 的查询提示逻辑。 */
	for _, gone := range []string{"barsCoverage", "showBarsCoverage", "loadBarsCoverage"} {
		if strings.Contains(string(js), gone) {
			t.Fatalf("app.js still has Dashboard coverage-hint logic %q (bars 已删除)", gone)
		}
	}
}

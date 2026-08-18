package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/httpapi"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/notify"
)

func TestFormatMarketOpenReport(t *testing.T) {
	now := time.Date(2026, 8, 18, 9, 10, 0, 0, time.FixedZone("UTC+8", 8*3600))
	d := marketOpenReportData{
		GeneratedAt: now,
		GatewayOK:   true,
		GatewayDesc: "ok · 行情已登录 · 交易已登录",
		DBOK:        true,
		DBDesc:      "ok (1.2ms)",
		LLMOK:       true,
		LLMDesc:     "已配置 · gpt-5.6",
		Watch: []marketOpenWatchItem{
			{Symbol: "HK.00700", Strategy: "wheel", VersionText: "3", ExecutionStatus: "READY", ParamsSummary: "covered_call_pct=0.3 strike_pct_otm=0.05"},
			{Symbol: "US.AAPL", Strategy: "llm", VersionText: "-", InvalidationReason: "waiting for complete quote snapshot"},
		},
		Account: marketOpenAccount{
			OK: true, Env: "sim", AccID: 123456789,
			AvailableCash: 123456.78, Cash: 200000.5, MarketVal: 150000, TotalAssets: 350000.5,
			Positions: []marketOpenPosition{
				{Symbol: "HK.00700", Qty: 500, MarketVal: 210000, PL: 12345.6},
			},
		},
		Cluster: marketOpenCluster{
			OK: true, Version: "1.2.3", UptimeSeconds: 3661,
			DBLatencyMS: 3.5, PipelineRunning: 1, PipelineSucceeded: 42, PipelineFailed: 2,
			BarsSymbols: 3, BarsStale: 1, OptionsFresh: 2, OptionsStale: 0,
		},
	}
	text := formatMarketOpenReport(d)
	for _, want := range []string{
		"开盘准备状态",
		"时间: 2026-08-18 09:10:00",
		"futu 网关: ✅ ok · 行情已登录 · 交易已登录",
		"DB: ✅ ok (1.2ms)",
		"LLM 审核: ✅ 已配置 · gpt-5.6",
		"Watch 标的 (2)",
		"HK.00700 · 固化策略 · v3 · READY",
		"covered_call_pct=0.3 strike_pct_otm=0.05",
		"US.AAPL · LLM 策略 · v- · 未运行",
		"⚠️ waiting for complete quote snapshot",
		"账户/资金 (sim)",
		"账户: 123456789",
		"可用资金: 123,456.78",
		"现金: 200,000.50 · 市值: 150,000.00 · 总资产: 350,000.50",
		"持仓 1 笔",
		"HK.00700 500 股 市值 210,000.00 盈亏 12,345.60",
		"服务集群(单进程组件)",
		"进程: v1.2.3 · 运行 1h1m0s",
		"DB: ✅ (3.5ms)",
		"数据管道: 运行 1 / 成功 42 / 失败 2",
		"数据面: bars 标的 3(过期 1) · 期权新鲜 2 / 过期 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q\n--- text ---\n%s", want, text)
		}
	}
}

func TestFormatMarketOpenReportDegraded(t *testing.T) {
	now := time.Date(2026, 8, 18, 9, 10, 0, 0, time.UTC)
	d := marketOpenReportData{
		GeneratedAt: now,
		GatewayOK:   false, GatewayDesc: "不可达: connection refused",
		DBOK: false, DBDesc: "不可达: db down",
		LLMOK: false, LLMDesc: "未配置 (LLM_BASE_URL/LLM_API_KEY/LLM_MODEL)",
		WatchErr: "watchlist: list: query: boom",
		Account:  marketOpenAccount{Err: "account fetch failed"},
		Cluster:  marketOpenCluster{Err: "db ping: nil db"},
	}
	text := formatMarketOpenReport(d)
	for _, want := range []string{
		"futu 网关: ❌ 不可达: connection refused",
		"DB: ❌ 不可达: db down",
		"LLM 审核: ❌ 未配置",
		"查询失败: watchlist: list: query: boom",
		"查询失败: account fetch failed",
		"查询失败: db ping: nil db",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("degraded report missing %q\n--- text ---\n%s", want, text)
		}
	}
}

func TestSummarizeParams(t *testing.T) {
	if got := summarizeParams(nil); got != "" {
		t.Fatalf("nil params = %q; want empty", got)
	}
	if got := summarizeParams(map[string]any{}); got != "" {
		t.Fatalf("empty params = %q; want empty", got)
	}
	if got := summarizeParams(map[string]any{"note": ""}); got != "" {
		t.Fatalf("all-empty params = %q; want empty", got)
	}
	got := summarizeParams(map[string]any{
		"strike_pct_otm":   0.05,
		"covered_call_pct": 0.3,
		"hold":             true,
		"note":             "",
		"tags":             []any{"a", "b"},
		"extra":            map[string]any{"x": 1},
	})
	want := "covered_call_pct=0.3 extra={1键} hold=true strike_pct_otm=0.05 tags=[2项]"
	if got != want {
		t.Fatalf("summarizeParams = %q; want %q", got, want)
	}
	long := summarizeParams(map[string]any{"long": strings.Repeat("x", 200)})
	if !strings.HasSuffix(long, "…") || len([]rune(long)) > 161 {
		t.Fatalf("long summary not truncated: %q (%d runes)", long, len([]rune(long)))
	}
}

func TestMarketOpenTextHelpers(t *testing.T) {
	if got := moneyText(123456.78); got != "123,456.78" {
		t.Fatalf("moneyText = %q", got)
	}
	if got := moneyText(-0.5); got != "-0.50" {
		t.Fatalf("moneyText negative = %q", got)
	}
	if got := moneyText(0); got != "0.00" {
		t.Fatalf("moneyText zero = %q", got)
	}
	if got := quantityText(500); got != "500" {
		t.Fatalf("quantityText int = %q", got)
	}
	if got := quantityText(0.5); got != "0.50" {
		t.Fatalf("quantityText frac = %q", got)
	}
	if got := versionText(nil); got != "-" {
		t.Fatalf("versionText nil = %q", got)
	}
	v := 3
	if got := versionText(&v); got != "3" {
		t.Fatalf("versionText 3 = %q", got)
	}
	if got := strategyName("wheel"); got != "固化策略" {
		t.Fatalf("strategyName wheel = %q", got)
	}
	if got := strategyName("llm"); got != "LLM 策略" {
		t.Fatalf("strategyName llm = %q", got)
	}
	if got := strategyName(""); got != "未设置" {
		t.Fatalf("strategyName empty = %q", got)
	}
	if got := statusText(""); got != "未运行" {
		t.Fatalf("statusText empty = %q", got)
	}
	if got := statusText("READY"); got != "READY" {
		t.Fatalf("statusText READY = %q", got)
	}
}

func TestMarketOpenNotifierFromConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WBOT_CONFIG_DIR", dir)
	cfg, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"credentials.telegram.token", "bot-token"},
		{"credentials.telegram.chat_ids", "42, 43"},
	} {
		if err := cfg.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	sender, err := marketOpenNotifierFromConfig()
	if err != nil {
		t.Fatalf("configured notifier: %v", err)
	}
	ms, ok := sender.(notify.MultiSender)
	if !ok || len(ms) != 2 {
		t.Fatalf("sender = %T (%d); want MultiSender of 2", sender, len(ms))
	}
}

func TestMarketOpenNotifierNotConfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WBOT_CONFIG_DIR", dir)
	if _, err := marketOpenNotifierFromConfig(); err != errMarketOpenNotConfigured {
		t.Fatalf("err = %v; want errMarketOpenNotConfigured", err)
	}
}

func TestServeHelpMentionsMarketOpen(t *testing.T) {
	out := serveHelpOutput(t)
	for _, want := range []string{"-prep-at-hk", "-prep-at-us", "market-open preparation status"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serve help missing %s: %q", want, out)
		}
	}
}

// marketOpenClusterStoreStub injects per-method failures so the cluster view's
// OK/Err semantics are testable without a DB (P1-1: a failing sub-query must
// surface as a failure, never a silent all-zero view).
type marketOpenClusterStoreStub struct {
	pingErr   error
	countsErr error
	barsErr   error
	optsErr   error
	counts    ingest.RunCounts
	bars      []ingest.BarCoverage
	opts      []ingest.OptionFreshness
}

func (m marketOpenClusterStoreStub) Ping(context.Context) error { return m.pingErr }
func (m marketOpenClusterStoreStub) RunStatusCounts(context.Context) (ingest.RunCounts, error) {
	return m.counts, m.countsErr
}
func (m marketOpenClusterStoreStub) BarCoverage(context.Context) ([]ingest.BarCoverage, error) {
	return m.bars, m.barsErr
}
func (m marketOpenClusterStoreStub) OptionFreshness(context.Context) ([]ingest.OptionFreshness, error) {
	return m.opts, m.optsErr
}

// TestCollectMarketOpenClusterSubQueryFailureNotSilent: P1-1 回归——子查询
// (RunStatusCounts) 失败必须 OK=false 让渲染走「查询失败」分支,boss 绝不看
// 到「运行 0/成功 0/失败 0」的全零假象。
func TestCollectMarketOpenClusterSubQueryFailureNotSilent(t *testing.T) {
	store := marketOpenClusterStoreStub{countsErr: errors.New("boom")}
	c := collectMarketOpenCluster(context.Background(), store, httpapi.ProcessMeta{Version: "1.2.3", StartedAt: time.Now()})
	if c.OK {
		t.Fatal("cluster OK=true; want false when a sub-query fails")
	}
	if !strings.Contains(c.Err, "pipeline counts") {
		t.Fatalf("Err = %q; want pipeline counts error", c.Err)
	}
	text := formatMarketOpenReport(marketOpenReportData{GeneratedAt: time.Now(), Cluster: c})
	if !strings.Contains(text, "查询失败") || !strings.Contains(text, "pipeline counts") {
		t.Fatalf("report hides sub-query failure:\n%s", text)
	}
	if strings.Contains(text, "数据管道") {
		t.Fatalf("report shows all-zero pipeline when sub-query failed:\n%s", text)
	}
}

// TestCollectMarketOpenClusterAllQueriesSucceed: OK=true 仅在全部子查询成功时
// 置位(与 P1-1 语义互锁,防止过早 OK=true 回归)。
func TestCollectMarketOpenClusterAllQueriesSucceed(t *testing.T) {
	store := marketOpenClusterStoreStub{
		counts: ingest.RunCounts{Running: 1, Succeeded: 42, Failed: 2},
		bars: []ingest.BarCoverage{
			{Symbol: "HK.00700", Timeframe: "1d", MaxTs: time.Now()},
		},
		opts: []ingest.OptionFreshness{
			{Underlying: "HK.00700", Source: "futu", MaxTs: time.Now()},
		},
	}
	c := collectMarketOpenCluster(context.Background(), store, httpapi.ProcessMeta{Version: "1.2.3", StartedAt: time.Now().Add(-time.Hour)})
	if !c.OK {
		t.Fatalf("cluster OK=false; want true when all queries succeed: Err=%q", c.Err)
	}
	if c.PipelineSucceeded != 42 || c.BarsSymbols != 1 || c.OptionsFresh != 1 {
		t.Fatalf("cluster = %+v", c)
	}
}

package backtestreport

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
)

func testInput(attempts, fills, unfilled int64) Input {
	start := time.Date(2024, 1, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	end := start.Add(24 * time.Hour)
	stats := backtest.UnfilledStats{
		AttemptCount: attempts, FillCount: fills, UnfilledCount: unfilled,
		ModelKind: "heuristic", ModelVersion: "heuristic-1.0",
	}
	if attempts > 0 {
		ratio := float64(unfilled) / float64(attempts)
		stats.UnfilledRatio = &ratio
	}
	configVersion := 1
	finalEquity := 10123.0
	holdings := 123.0
	optionValue := 0.0
	realized := 100.0
	unrealized := 23.0
	coverage := 0.0
	cycleComplete := false
	return Input{
		Symbol: "HK.00883", Strategy: "wheel",
		Params:        map[string]any{"full_position_price": 48.0, "zero_position_price": 55.0, "max_inventory": 22000.0},
		ConfigVersion: &configVersion, CodeVersion: "test-sha", RunSeed: 42,
		InitialCash: 10000, Start: start, End: end, BaselineReturnPct: 0.008,
		SourceHash: "sha256-test-source",
		Result: &backtest.Result{Equity: 10123, TotalReturn: 0.0123, MaxDrawdown: 0.05, Bars: 2, Unfilled: stats,
			Fees: backtest.FeeSummary{Included: true, PerTrade: 3, TotalAmount: 9, StockAmount: 3, OptionAmount: 6, ChargedTradeCount: 3},
			Terminal: backtest.TerminalSummary{ValuationStatus: backtest.ValuationComplete, SettlementStatus: backtest.SettlementOpenOptionLegs, CashAmount: 10000,
				OptionMarketValueAmount: &optionValue, HoldingsMarketValueAmount: &holdings, FinalEquityAmount: &finalEquity, OpenOptionLegCount: 1,
				RealizedPnLAmount: &realized, UnrealizedPnLAmount: &unrealized, EventBasis: "mechanical_backtest", PnLStatus: backtest.ValuationComplete},
			DataQuality: backtest.DataQualitySummary{Status: "DATA_BLOCKED", OptionDataRequired: true, TotalBarCount: 2, BlockedBarCount: 2,
				ValidCoverageRatio: &coverage, MissingRequiredFieldCounts: map[string]int64{"bid": 0}, HistoricalOptionCycleComplete: &cycleComplete, BlockedBy: []string{"option_quote_snapshots"}}},
	}
}

func TestBuildSingleRunStructureAndNullRatio(t *testing.T) {
	r, err := Build(testInput(0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != "1.1" || r.ReportKind != "single_run" || !strings.HasPrefix(r.ReportID, "bt-HK.00883-42-") {
		t.Fatalf("identity = %+v", r)
	}
	if r.Identity.DataWindow.From != "2024-01-01T00:00:00Z" || r.Identity.DataWindow.To != "2024-01-02T00:00:00Z" {
		t.Fatalf("window = %+v; want UTC Z", r.Identity.DataWindow)
	}
	if r.Result.UnfilledRatio != nil {
		t.Fatalf("unfilled ratio = %v; want nil", r.Result.UnfilledRatio)
	}
	b, err := JSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	result := raw["result"].(map[string]any)
	if result["net_return_pct"] != nil || result["net_return_amount"] != nil || result["excess_return_pct"] != nil || result["return_status"] != "not_applicable_data_blocked" || result["window_mark_to_market_return_pct"] != 0.0123 {
		t.Fatalf("blocked return projection = %#v; want null net fields and explicit window mark", result)
	}
	if result["unfilled_ratio"] != nil {
		t.Fatalf("JSON unfilled_ratio = %#v; want null", result["unfilled_ratio"])
	}
	model := result["unfilled_model"].(map[string]any)
	components := model["components"].(map[string]any)
	if model["model_kind"] != "heuristic" || model["model_version"] != "heuristic-1.0" || model["order_assumption"] != orderAssumption {
		t.Fatalf("unfilled_model = %#v", model)
	}
	if components["spread_weight"] != 0.55 || components["volume_weight"] != 0.30 || components["oi_weight"] != 0.15 {
		t.Fatalf("components = %#v", components)
	}
	cost := result["cost_model"].(map[string]any)
	if cost["fees_included"] != true || cost["fee_per_trade"] != 3.0 || cost["total_fees_amount"] != 9.0 || cost["stock_fees_amount"] != 3.0 || cost["option_fees_amount"] != 6.0 || cost["charged_trade_count"] != 3.0 {
		t.Fatalf("cost_model = %#v; want the runner fee ledger", cost)
	}
	terminal := raw["terminal"].(map[string]any)
	if terminal["open_option_leg_count"] != 1.0 || terminal["realized_pnl_amount"] != 100.0 || terminal["unrealized_pnl_amount"] != 23.0 || terminal["broker_assignment_count"] != nil {
		t.Fatalf("terminal = %#v; want mechanical P&L and null broker facts", terminal)
	}
	quality := raw["data_quality"].(map[string]any)
	if quality["status"] != "DATA_BLOCKED" || quality["blocked_bar_count"] != 2.0 || quality["valid_coverage_ratio"] != 0.0 || quality["historical_option_cycle_complete"] != false {
		t.Fatalf("data_quality = %#v; want explicit blocked coverage", quality)
	}
}

func TestBuildConfigVersionIsExplicitAndAffectsIdentity(t *testing.T) {
	adHoc := testInput(0, 0, 0)
	adHoc.ConfigVersion = nil
	adHocReport, err := Build(adHoc)
	if err != nil {
		t.Fatal(err)
	}
	if adHocReport.Identity.ConfigVersion != nil {
		t.Fatalf("ad-hoc config_version = %v; want null", *adHocReport.Identity.ConfigVersion)
	}
	one := 1
	two := 2
	v1 := testInput(0, 0, 0)
	v2 := testInput(0, 0, 0)
	v1.ConfigVersion = &one
	v2.ConfigVersion = &two
	r1, err := Build(v1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Build(v2)
	if err != nil {
		t.Fatal(err)
	}
	if r1.ReportID == r2.ReportID || r2.Identity.ConfigVersion == nil || *r2.Identity.ConfigVersion != 2 {
		t.Fatalf("config versions did not produce distinct identities: %s %#v / %s %#v", r1.ReportID, r1.Identity.ConfigVersion, r2.ReportID, r2.Identity.ConfigVersion)
	}
}

func TestBuildAttemptRatioAndDeterministicRendering(t *testing.T) {
	in := testInput(5, 3, 2)
	a, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Result.UnfilledRatio == nil || *a.Result.UnfilledRatio != 0.4 {
		t.Fatalf("ratio = %v; want 0.4", a.Result.UnfilledRatio)
	}
	ja, _ := JSON(a)
	jb, _ := JSON(b)
	if !bytes.Equal(ja, jb) || a.ReportID != b.ReportID {
		t.Fatalf("same input differs: ids %q/%q", a.ReportID, b.ReportID)
	}
	ha, err := HTML(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := HTML(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ha, hb) {
		t.Fatal("same report rendered different HTML bytes")
	}
	for _, want := range []string{"og:title", "og:description", "theme-color", "窗口末估值变动", "数据有效覆盖率", "窗口末未平仓腿", "已实现 P&amp;L", "最大回撤", "未成交率", "停止原因", "总费用", "期权费用", "40.00%", "overflow-x:hidden", "@media(max-width:430px)"} {
		if !bytes.Contains(ha, []byte(want)) {
			t.Fatalf("HTML missing %q", want)
		}
	}
	if bytes.Contains(ha, []byte("净收益")) {
		t.Fatal("DATA_BLOCKED HTML labels a mechanical window mark as net profit")
	}
}

func TestReportIDTracksInputsNotResult(t *testing.T) {
	in := testInput(0, 0, 0)
	a, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Result.Equity++
	in.Result.TotalReturn += 0.0001
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.ReportID != b.ReportID {
		t.Fatalf("result-only change altered report id: %q != %q", a.ReportID, b.ReportID)
	}
	in.SourceHash = "sha256-other-source"
	c, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.ReportID == b.ReportID {
		t.Fatalf("source input change retained report id %q", c.ReportID)
	}
}

func TestBuildESReusesSchemaAndRequiresDistinctTestSeed(t *testing.T) {
	in := ESInput{
		Run:         testInput(0, 0, 0),
		Windows:     Windows{Train: Window{From: "2024-01-01T00:00:00Z", To: "2024-01-02T00:00:00Z"}, Valid: Window{From: "2024-01-02T00:00:00Z", To: "2024-01-03T00:00:00Z"}, Test: Window{From: "2024-01-03T00:00:00Z", To: "2024-01-04T00:00:00Z"}},
		Train:       Train{Algorithm: "ES", AlgorithmVersion: "es-1.0", GenerationCount: 1, PopulationSize: 20, EvaluationCount: 21, Seeds: []int64{1, 2, 3}, StopReason: "early_stop"},
		Generations: []Generation{{Generation: 0, EvaluationCount: 21, TrainBestReturnPct: .1, TrainMeanReturnPct: .05}},
		Candidates:  []Candidate{{Rank: 1, Params: map[string]any{"move_interval_pct": .01}}},
		Reward:      RewardAudit{FunctionVersion: "reward-1.0", HardFailureHandling: "direct failure"},
		SearchSpace: map[string]SearchBound{"move_interval_pct": {Min: .005, Max: .03, Unit: "%"}},
	}
	r, err := BuildES(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.ReportKind != "es_train" || r.Train == nil || r.Identity.Windows == nil || r.Identity.CapabilityStatus != "DATA_BLOCKED" || len(r.Generations) != 1 || len(r.Candidates) != 1 {
		t.Fatalf("ES report = %+v", r)
	}
	in.Train.Seeds = []int64{1, 2, 1}
	if _, err := BuildES(in); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("same train/test seed error = %v", err)
	}
}

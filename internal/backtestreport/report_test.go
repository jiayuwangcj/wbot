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
	return Input{
		Symbol: "HK.00883", Strategy: "wheel",
		Params:        map[string]any{"full_position_price": 48.0, "zero_position_price": 55.0, "max_inventory": 22000.0},
		ConfigVersion: 1, CodeVersion: "test-sha", RunSeed: 42,
		InitialCash: 10000, Start: start, End: end, BaselineReturnPct: 0.008,
		SourceHash: "sha256-test-source",
		Result:     &backtest.Result{Equity: 10123, TotalReturn: 0.0123, MaxDrawdown: 0.05, Bars: 2, Unfilled: stats},
	}
}

func TestBuildSingleRunStructureAndNullRatio(t *testing.T) {
	r, err := Build(testInput(0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != "1.0" || r.ReportKind != "single_run" || !strings.HasPrefix(r.ReportID, "bt-HK.00883-42-") {
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
	for _, want := range []string{"og:title", "og:description", "theme-color", "净收益", "最大回撤", "未成交率", "停止原因", "40.00%", "overflow-x:hidden", "@media(max-width:430px)"} {
		if !bytes.Contains(ha, []byte(want)) {
			t.Fatalf("HTML missing %q", want)
		}
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
	if r.ReportKind != "es_train" || r.Train == nil || r.Identity.Windows == nil || r.Identity.CapabilityStatus != "RESEARCH_ONLY" || len(r.Generations) != 1 || len(r.Candidates) != 1 {
		t.Fatalf("ES report = %+v", r)
	}
	in.Train.Seeds = []int64{1, 2, 1}
	if _, err := BuildES(in); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("same train/test seed error = %v", err)
	}
}

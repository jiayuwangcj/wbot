package backtestes

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func baseParams() map[string]any {
	return map[string]any{"full_position_price": 48.0, "zero_position_price": 55.0, "max_inventory": 22000.0,
		"move_interval_pct": .01, "min_premium_per_share": .2, "stock_switch_pct": .05,
		"trade_gap": 50.0, "min_option_quality": .6, "min_dte": 5.0, "max_dte": 10.0}
}

func TestParseSpaceOnlyTacticalAndConstrainedDTE(t *testing.T) {
	if _, err := ParseSpace(`{"max_inventory":[100,200]}`, baseParams()); err == nil || !strings.Contains(err.Error(), "not a tactical") {
		t.Fatalf("strategic parameter error = %v", err)
	}
	s, err := ParseSpace(`{"move_interval_pct":["0.005","0.03"],"trade_gap":[0,200],"min_dte":[5,10],"max_dte":[5,10]}`, baseParams())
	if err != nil {
		t.Fatal(err)
	}
	p := s.decode(orderedNames(s.Bounds), []float64{.5, .51, 1, 0})
	if p["full_position_price"] != 48.0 {
		t.Fatal("strategic parameter changed")
	}
	if p["trade_gap"].(float64) != 102 || p["max_dte"].(int) < p["min_dte"].(int) {
		t.Fatalf("decoded params = %#v", p)
	}
}

func TestSplitWindowsIsChronologicalWithoutLeakage(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	w, err := SplitWindows(from, from.Add(100*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if w.Train.From != from || w.Train.To != w.Valid.From || w.Valid.To != w.Test.From || !w.Test.To.After(w.Test.From) {
		t.Fatalf("windows overlap or shuffle: %+v", w)
	}
}

func TestValidateWindowBarsRejectsEmptyWindow(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	w, err := SplitWindows(from, from.Add(10*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	barTimes := []time.Time{
		from, from.Add(time.Hour), from.Add(2 * time.Hour),
		from.Add(3 * time.Hour), from.Add(10 * time.Hour),
	}
	err = ValidateWindowBars(w, barTimes)
	if err == nil || !strings.Contains(err.Error(), "数据不足以 60/20/20 切分,需 ≥5 根 bar") || !strings.Contains(err.Error(), "valid=0") {
		t.Fatalf("empty-window error = %v", err)
	}
}

func TestValidateWindowBarsRejectsFewerThanMinimum(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	w, err := SplitWindows(from, from.Add(10*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	barTimes := []time.Time{from, from.Add(7 * time.Hour), from.Add(10 * time.Hour)}
	if err := ValidateWindowBars(w, barTimes); err == nil || !strings.Contains(err.Error(), "total=3") {
		t.Fatalf("short-input error = %v", err)
	}
}

func TestSearchDeterminismBestAtLeastPopulationMeanAndDistinctSeeds(t *testing.T) {
	s, err := ParseSpace(`{"move_interval_pct":[0,1],"trade_gap":[0,20],"min_dte":[5,9],"max_dte":[5,10]}`, baseParams())
	if err != nil {
		t.Fatal(err)
	}
	w, _ := SplitWindows(time.Unix(0, 0).UTC(), time.Unix(1000, 0).UTC())
	cfg := DefaultConfig(77)
	cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience = 16, 6, 102, 3
	eval := func(_ context.Context, p map[string]any, _ Window, seed int64) (Metrics, error) {
		x := number(p["move_interval_pct"])
		return Metrics{NetReturn: 1 - (x-.73)*(x-.73), MaxDrawdown: .01, EffectiveTrades: int(seed%5) + 1}, nil
	}
	a, err := Search(context.Background(), s, w, cfg, eval)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Search(context.Background(), s, w, cfg, eval)
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !reflect.DeepEqual(ja, jb) {
		t.Fatal("same seed produced different trajectory/candidates")
	}
	for _, g := range a.Generations {
		if g.BestScore < g.MeanScore {
			t.Fatalf("generation %d best %v < mean %v", g.Generation, g.BestScore, g.MeanScore)
		}
	}
	if a.TrainSeed == a.ValidSeed || a.TrainSeed == a.TestSeed || a.ValidSeed == a.TestSeed {
		t.Fatalf("derived seeds are not distinct: %+v", a)
	}
}

func TestSearchParallelMatchesSerialBitForBit(t *testing.T) {
	s, err := ParseSpace(`{"move_interval_pct":[0,1],"trade_gap":[0,20],"min_dte":[5,9],"max_dte":[5,10]}`, baseParams())
	if err != nil {
		t.Fatal(err)
	}
	w, _ := SplitWindows(time.Unix(0, 0).UTC(), time.Unix(1000, 0).UTC())
	cfg := DefaultConfig(91)
	cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience = 16, 5, 85, 3
	eval := func(_ context.Context, p map[string]any, _ Window, seed int64) (Metrics, error) {
		x := number(p["move_interval_pct"])
		return Metrics{NetReturn: 1 - (x-.37)*(x-.37), MaxDrawdown: number(p["trade_gap"]) / 1000, TailLoss: float64(seed%7) / 1000, CostPct: number(p["min_dte"]) / 1000}, nil
	}
	parallel, err := Search(context.Background(), s, w, cfg, eval)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := search(context.Background(), s, w, cfg, eval, 1)
	if err != nil {
		t.Fatal(err)
	}
	parallelJSON, err := json.Marshal(parallel)
	if err != nil {
		t.Fatal(err)
	}
	serialJSON, err := json.Marshal(serial)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parallelJSON, serialJSON) {
		t.Fatalf("parallel and serial ES results differ:\nparallel=%s\nserial=%s", parallelJSON, serialJSON)
	}
}

func TestEarlyStop(t *testing.T) {
	s, _ := ParseSpace(`{"move_interval_pct":[0,1]}`, baseParams())
	w, _ := SplitWindows(time.Unix(0, 0).UTC(), time.Unix(1000, 0).UTC())
	cfg := DefaultConfig(3)
	cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience = 16, 20, 340, 3
	r, err := Search(context.Background(), s, w, cfg, func(context.Context, map[string]any, Window, int64) (Metrics, error) {
		return Metrics{NetReturn: .1}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.StopReason != "early_stop" || len(r.Generations) >= cfg.MaxGenerations {
		t.Fatalf("early stop = %q after %d generations", r.StopReason, len(r.Generations))
	}
}

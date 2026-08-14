package main

import (
	"math"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/backtestes"
)

func tuneBaseParams() map[string]any {
	return map[string]any{"full_position_price": 48.0, "zero_position_price": 55.0, "max_inventory": 22000.0}
}

func TestParseTuneSpec(t *testing.T) {
	base := tuneBaseParams()
	spec := `{"spaces":[{"move_interval_pct":["0.005","0.03"]},{"move_interval_pct":[0.01,0.05],"min_option_profit":[100,300]}],"seeds":[42,7]}`
	spaces, seeds, err := parseTuneSpec(spec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 2 || len(seeds) != 2 || seeds[0] != 42 || seeds[1] != 7 {
		t.Fatalf("spaces=%d seeds=%v", len(spaces), seeds)
	}
	if len(spaces[1].Bounds) != 2 || spaces[1].Bounds["min_option_profit"].Min != 100 {
		t.Fatalf("second space bounds = %+v", spaces[1].Bounds)
	}
	if spaces[0].Base["max_inventory"] != 22000.0 {
		t.Fatal("strategic base params lost")
	}
	for _, bad := range []struct {
		name, raw string
	}{
		{"not json", `nope`},
		{"empty spaces", `{"spaces":[],"seeds":[42]}`},
		{"empty seeds", `{"spaces":[{"move_interval_pct":[0.01,0.03]}],"seeds":[]}`},
		{"duplicate seeds", `{"spaces":[{"move_interval_pct":[0.01,0.03]}],"seeds":[42,42]}`},
		{"zero duplicates 42", `{"spaces":[{"move_interval_pct":[0.01,0.03]}],"seeds":[0,42]}`},
		{"strategic param", `{"spaces":[{"max_inventory":[100,200]}],"seeds":[42]}`},
		{"inverted range", `{"spaces":[{"move_interval_pct":[0.05,0.03]}],"seeds":[42]}`},
	} {
		if _, _, err := parseTuneSpec(bad.raw, base); err == nil {
			t.Fatalf("%s: expected error", bad.name)
		}
	}
}

func TestTuneShouldPruneRacingRules(t *testing.T) {
	// 观察窗口内不剪
	if prune, _ := tuneShouldPrune(2, 4, 0.001, 0.02, 0.05, 0.5); prune {
		t.Fatal("pruned before observation window")
	}
	// 全局最优仍低于基线 → 全不剪(无望窗口全部跑完,取最不差)
	if prune, _ := tuneShouldPrune(4, 4, 0.001, 0.02, 0.01, 0.5); prune {
		t.Fatal("pruned while global best is below baseline")
	}
	// 明显差组:历史最优低于基线 → 剪
	if prune, _ := tuneShouldPrune(4, 4, 0.01, 0.02, 0.05, 0.5); !prune {
		t.Fatal("group below baseline survived")
	}
	// 显著落后:0.02 < max(0.01, 0.05*0.5=0.025) → 剪
	if prune, _ := tuneShouldPrune(4, 4, 0.02, 0.01, 0.05, 0.5); !prune {
		t.Fatal("group far behind global best survived")
	}
	// 好组不被误杀:历史最优 == 全局最优
	if prune, _ := tuneShouldPrune(4, 4, 0.05, 0.01, 0.05, 0.5); prune {
		t.Fatal("current best group pruned")
	}
	// 落后但未到 0.5 阈值:0.03 >= 0.025
	if prune, _ := tuneShouldPrune(4, 4, 0.03, 0.01, 0.05, 0.5); prune {
		t.Fatal("group within factor threshold pruned")
	}
}

func TestTunePruneCheckSequentialGroups(t *testing.T) {
	globalBest := math.Inf(-1)
	check := tunePruneCheck(4, 0.5, 0.03, &globalBest)
	// group 1 前 4 代低于基线:不剪,global best 保持低于基线
	for gen := 0; gen < 4; gen++ {
		if !check(&backtestes.PruneProgress{Generation: gen, HistoryBestScore: 0.01}) {
			t.Fatalf("group below baseline pruned at generation %d", gen)
		}
	}
	// group 1 越过基线 → 成为全局最优,不剪
	if !check(&backtestes.PruneProgress{Generation: 5, HistoryBestScore: 0.05}) {
		t.Fatal("improving group pruned")
	}
	if globalBest != 0.05 {
		t.Fatalf("global best = %v; want 0.05", globalBest)
	}
	// group 2 前 3 代窗口内不剪,第 4 代仍显著落后 → 剪
	for gen := 0; gen < 4; gen++ {
		keep := check(&backtestes.PruneProgress{Generation: gen, HistoryBestScore: 0.02})
		if gen < 3 && !keep {
			t.Fatalf("group 2 pruned before window at generation %d", gen)
		}
		if gen == 3 && keep {
			t.Fatal("group 2 behind global best survived the window")
		}
	}
	// group 3 处于阈值内(0.04 >= floor 0.03)→ 不剪
	if !check(&backtestes.PruneProgress{Generation: 3, HistoryBestScore: 0.04}) {
		t.Fatal("decent group pruned")
	}
}

func TestBestTuneGroupPicksHighestMedianScore(t *testing.T) {
	groups := []tuneGroupResult{{spaceIndex: 0, medianScore: 0.1}, {spaceIndex: 1, medianScore: 0.3}, {spaceIndex: 2, medianScore: 0.2}}
	if got := bestTuneGroup(groups); got != 1 {
		t.Fatalf("best index = %d; want 1", got)
	}
	tied := []tuneGroupResult{{spaceIndex: 2, medianScore: 0.3}, {spaceIndex: 0, medianScore: 0.3}, {spaceIndex: 1, medianScore: 0.1}}
	if got := bestTuneGroup(tied); got != 0 {
		t.Fatalf("tie-break index = %d; want first group", got)
	}
}

func TestConvergenceGenerationTracksLastImprovement(t *testing.T) {
	gens := []backtestes.Generation{
		{ValidBestScore: 0.01},
		{ValidBestScore: 0.01},
		{ValidBestScore: 0.03},
		{ValidBestScore: 0.03},
		{ValidBestScore: 0.035},
	}
	if got := convergenceGeneration(gens); got != 5 {
		t.Fatalf("converged at generation %d; want 5", got)
	}
	if got := convergenceGeneration(gens[:1]); got != 1 {
		t.Fatalf("single generation converged at %d; want 1", got)
	}
	if got := convergenceGeneration(nil); got != 0 {
		t.Fatalf("empty converged at %d; want 0", got)
	}
}

func TestHistoryBestRewardIsMaxTrainScore(t *testing.T) {
	gens := []backtestes.Generation{{BestScore: 0.01}, {BestScore: 0.03}, {BestScore: 0.02}}
	if got := historyBestReward(gens); got != 0.03 {
		t.Fatalf("history best = %v; want 0.03", got)
	}
}

func TestTuneGroupSummaryLineMarksPruned(t *testing.T) {
	ratio := 0.05
	pruned := tuneGroupResult{spaceIndex: 2, seed: 7, historyReward: 0.01, medianScore: 0.005, medianReturn: 0.01, medianDrawdown: 0.02, medianUnfilled: &ratio}
	pruned.search = backtestes.Result{StopReason: "pruned"}
	pruned.search.Generations = make([]backtestes.Generation, 4)
	line := tuneGroupSummaryLine(pruned)
	for _, want := range []string{"space=2", "seed=7", "status=pruned", "pruned_at=4", "unfilled_pct=5.00%"} {
		if !strings.Contains(line, want) {
			t.Fatalf("summary line missing %q: %s", want, line)
		}
	}
	ok := tuneGroupSummaryLine(tuneGroupResult{spaceIndex: 0, seed: 42})
	for _, want := range []string{"status=ok", "pruned_at=-", "unfilled_pct=N/A"} {
		if !strings.Contains(ok, want) {
			t.Fatalf("summary line missing %q: %s", want, ok)
		}
	}
}

func TestBacktestTuneFlagValidation(t *testing.T) {
	_, stderr, code := captureRun(t, []string{
		"wbot", "backtest", "-file", "missing.json", "-tune", `{"spaces":[{"move_interval_pct":["0.005","0.03"]}],"seeds":[42]}`,
	})
	if code != 2 || !strings.Contains(stderr, "-tune requires one -dsn symbol") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = captureRun(t, []string{
		"wbot", "backtest", "-file", "missing.json", "-tune", `{"spaces":[{"move_interval_pct":["0.005","0.03"]}],"seeds":[42]}`, "-train", `{"move_interval_pct":["0.005","0.03"]}`,
	})
	if code != 2 || !strings.Contains(stderr, "-tune and -train are mutually exclusive") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	wheelArgs := []string{"-dsn", "postgres://invalid", "-symbol", "HK.00883", "-strategy", "wheel", "-params", `{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}`}
	_, stderr, code = captureRun(t, append(append([]string{"wbot", "backtest"}, wheelArgs...), "-tune", `{"spaces":[{"move_interval_pct":["0.005","0.03"]}],"seeds":[42]}`, "-tune-prune-window", "0"))
	if code != 2 || !strings.Contains(stderr, "-tune-prune-window must be >= 1") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = captureRun(t, append(append([]string{"wbot", "backtest"}, wheelArgs...), "-tune", `{"spaces":[{"move_interval_pct":["0.005","0.03"]}],"seeds":[42]}`, "-tune-prune-factor", "1.5"))
	if code != 2 || !strings.Contains(stderr, "-tune-prune-factor must be in [0,1]") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	// 坏 -tune spec 在连接 DB 前返回
	_, stderr, code = captureRun(t, append(append([]string{"wbot", "backtest"}, wheelArgs...), "-tune", `{"spaces":[],"seeds":[42]}`))
	if code != 2 || !strings.Contains(stderr, "tune spec: spaces 至少需要一个搜索空间") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

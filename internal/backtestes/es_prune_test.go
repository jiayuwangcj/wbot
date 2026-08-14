package backtestes

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestPruneCheckStopsSearchWithReason(t *testing.T) {
	s, _ := ParseSpace(`{"move_interval_pct":[0,1]}`, baseParams())
	w, _ := SplitWindows(time.Unix(0, 0).UTC(), time.Unix(1000, 0).UTC())
	cfg := DefaultConfig(11)
	cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience = 16, 20, 340, 3
	cfg.PruneCheck = func(progress *PruneProgress) bool {
		if progress.Generation+1 >= 2 {
			progress.PrunedReason = "test floor"
			return false
		}
		return true
	}
	r, err := Search(context.Background(), s, w, cfg, func(context.Context, map[string]any, Window, int64) (Metrics, error) {
		return Metrics{NetReturn: .1}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.StopReason != "pruned" || len(r.Generations) != 2 || !strings.Contains(r.StopDetail, "test floor") {
		t.Fatalf("stop = %q/%q after %d generations; want pruned at generation 2", r.StopReason, r.StopDetail, len(r.Generations))
	}
}

func TestPruneCheckProgressIsHistoryMaxAndEarlyStopWhenKept(t *testing.T) {
	s, _ := ParseSpace(`{"move_interval_pct":[0,1]}`, baseParams())
	w, _ := SplitWindows(time.Unix(0, 0).UTC(), time.Unix(1000, 0).UTC())
	cfg := DefaultConfig(13)
	cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience = 16, 20, 340, 3
	progress := []PruneProgress{}
	cfg.PruneCheck = func(p *PruneProgress) bool {
		progress = append(progress, *p)
		return true
	}
	eval := func(_ context.Context, p map[string]any, _ Window, _ int64) (Metrics, error) {
		x := number(p["move_interval_pct"])
		return Metrics{NetReturn: 1 - (x-.73)*(x-.73), MaxDrawdown: .01, EffectiveTrades: 1}, nil
	}
	r, err := Search(context.Background(), s, w, cfg, eval)
	if err != nil {
		t.Fatal(err)
	}
	if r.StopReason != "early_stop" {
		t.Fatalf("never-pruned search stopped with %q; want early_stop", r.StopReason)
	}
	if len(progress) != len(r.Generations) {
		t.Fatalf("hook called %d times for %d generations", len(progress), len(r.Generations))
	}
	best := math.Inf(-1)
	for i, p := range progress {
		if p.Generation != i {
			t.Fatalf("hook generation %d; want %d", p.Generation, i)
		}
		if p.HistoryBestScore != math.Max(best, p.BestScore) {
			t.Fatalf("history best %v is not the running max at generation %d", p.HistoryBestScore, i)
		}
		best = p.HistoryBestScore
		if p.EvaluationCount <= 0 || (i > 0 && p.EvaluationCount <= progress[i-1].EvaluationCount) {
			t.Fatalf("evaluation count not monotonic: %+v", p)
		}
	}
	if best != r.Generations[len(r.Generations)-1].BestScore {
		t.Fatalf("history best %v does not match last generation best %v", best, r.Generations[len(r.Generations)-1].BestScore)
	}
}

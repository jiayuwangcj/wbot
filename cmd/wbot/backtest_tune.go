package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/backtestes"
	"github.com/jiayu/wbot/internal/backtestexec"
	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/strategy"
)

// sampleOutCandidateCount and sampleOutSeedCount are the shared held-out
// protocol: every search's top candidates are replayed on derived test seeds.
const (
	sampleOutCandidateCount = 3
	sampleOutSeedCount      = 5
)

type backtestTuneFlags struct {
	Population, MaxGenerations, Budget, Patience int
	Timeout                                      time.Duration
	Prune                                        bool
	PruneWindow                                  int
	PruneFactor                                  float64
	Report, Push                                 bool
	ReportDir                                    string
}

type testedCandidate struct {
	candidate   backtestes.Candidate
	metrics     []backtestes.Metrics
	outcomes    []*backtestexec.Outcome
	medianScore float64
	spaceIndex  int
}

type tuneGroupResult struct {
	spaceIndex     int
	seed           int64
	search         backtestes.Result
	tested         []testedCandidate
	testSeeds      []int64
	historyReward  float64
	medianScore    float64
	medianReturn   float64
	medianDrawdown float64
	medianUnfilled *float64
	duration       time.Duration
}

type tuneSpec struct {
	Spaces []json.RawMessage `json:"spaces"`
	Seeds  []int64           `json:"seeds"`
}

func parseTuneSpec(raw string, base map[string]any) ([]backtestes.Space, []int64, error) {
	var spec tuneSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, nil, fmt.Errorf("tune spec: %w", err)
	}
	if len(spec.Spaces) == 0 {
		return nil, nil, errors.New("tune spec: spaces 至少需要一个搜索空间")
	}
	if len(spec.Seeds) == 0 {
		return nil, nil, errors.New("tune spec: seeds 至少需要一个种子")
	}
	seen := map[int64]bool{}
	for _, seed := range spec.Seeds {
		effective := seed
		if effective == 0 {
			effective = 42
		}
		if seen[effective] {
			return nil, nil, fmt.Errorf("tune spec: duplicate seed %d (0 等价于 42)", seed)
		}
		seen[effective] = true
	}
	spaces := make([]backtestes.Space, 0, len(spec.Spaces))
	for i, rawSpace := range spec.Spaces {
		s, err := backtestes.ParseSpace(string(rawSpace), base)
		if err != nil {
			return nil, nil, fmt.Errorf("tune spec: space %d: %w", i, err)
		}
		spaces = append(spaces, s)
	}
	return spaces, spec.Seeds, nil
}

// tuneShouldPrune implements the racing rule: after pruneWindow generations a
// space is pruned when its history-best reward is below the higher of the
// buy-hold baseline and the current global best times pruneFactor. Nothing is
// pruned while every group is still below baseline, so a hopeless window still
// completes and the least-bad group becomes the deliverable.
func tuneShouldPrune(generation, window int, historyBest, baseline, globalBest, factor float64) (bool, float64) {
	if generation+1 < window || globalBest < baseline {
		return false, 0
	}
	floor := math.Max(baseline, globalBest*factor)
	return historyBest < floor, floor
}

// tunePruneCheck wires the racing decision into one search. globalBest holds
// the cross-group maximum and is updated after each generation; groups run
// sequentially, so the closure stays deterministic.
func tunePruneCheck(window int, factor, baseline float64, globalBest *float64) backtestes.PruneCheck {
	return func(progress *backtestes.PruneProgress) bool {
		prune, floor := tuneShouldPrune(progress.Generation, window, progress.HistoryBestScore, baseline, *globalBest, factor)
		if prune {
			progress.PrunedReason = fmt.Sprintf("history_best=%.6g < floor=%.6g (baseline=%.6g global_best=%.6g factor=%.2f)", progress.HistoryBestScore, floor, baseline, *globalBest, factor)
		}
		if progress.HistoryBestScore > *globalBest {
			*globalBest = progress.HistoryBestScore
		}
		return !prune
	}
}

func runBacktestTune(dsn, rawSpec string, opts backtestexec.Options, flags backtestTuneFlags) int {
	// The final report's Trajectory consumes CandidateDetails; tune reruns the
	// best params over the full window with tracing on (buildTrajectory).
	opts.TraceCandidates = true
	canonical, err := strategy.CanonicalParams(opts.Params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: tune params: %v\n", err)
		return 2
	}
	spaces, seeds, err := parseTuneSpec(rawSpec, canonical)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: -tune: %v\n", err)
		return 2
	}
	if flags.Prune && flags.PruneWindow < 1 {
		fmt.Fprintln(os.Stderr, "backtest: -tune-prune-window must be >= 1")
		return 2
	}
	if flags.PruneFactor < 0 || flags.PruneFactor > 1 {
		fmt.Fprintln(os.Stderr, "backtest: -tune-prune-factor must be in [0,1]")
		return 2
	}

	database, err := db.Open(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: migrate: %v\n", err)
		return 1
	}
	baseOutcome, err := backtestexec.Run(context.Background(), database, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: tune data probe: %v\n", err)
		return 1
	}
	if err := requireTrainCoverage(baseOutcome, opts.Symbol, opts.Cash); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: tune data probe: %v\n", err)
		return 1
	}
	windows, err := backtestes.SplitWindows(baseOutcome.StartTs, baseOutcome.EndTs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 1
	}
	barTimes := make([]time.Time, len(baseOutcome.Result.EquityCurve))
	for i, point := range baseOutcome.Result.EquityCurve {
		barTimes[i] = point.Ts
	}
	if err := backtestes.ValidateWindowBars(windows, barTimes); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 1
	}
	prepared := make(map[backtestes.Window]*backtestexec.Prepared, 3)
	tunesIVRank := false
	for _, s := range spaces {
		if _, ok := s.Bounds["min_iv_rank"]; ok {
			tunesIVRank = true
			break
		}
	}
	for _, spec := range []struct {
		name   string
		window backtestes.Window
		to     time.Time
	}{
		{name: "train", window: windows.Train, to: windows.Train.To.Add(-time.Nanosecond)},
		{name: "validation", window: windows.Valid, to: windows.Valid.To.Add(-time.Nanosecond)},
		{name: "sample-out", window: windows.Test, to: windows.Test.To},
	} {
		prepareOpts := opts
		prepareOpts.From, prepareOpts.To = spec.window.From, spec.to
		// min_iv_rank needs a 1-year trailing IV history for every window; widen
		// the snapshot query so window-edge batches carry a computable rank.
		if tunesIVRank {
			prepareOpts.QuoteFrom = spec.window.From.Add(-backtest.IVRankWindow)
		}
		p, err := backtestexec.Prepare(context.Background(), database, prepareOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %s preparation: %v\n", spec.name, err)
			return 1
		}
		prepared[spec.window] = p
	}
	evaluator := func(ctx context.Context, params map[string]any, window backtestes.Window, seed int64) (backtestes.Metrics, error) {
		runOpts := opts
		runOpts.Params, runOpts.Seed, runOpts.From = params, seed, window.From
		runOpts.To = window.To.Add(-time.Nanosecond) // half-open windows: no bar can cross a split
		p, ok := prepared[window]
		if !ok {
			return backtestes.Metrics{}, fmt.Errorf("missing prepared window %v", window)
		}
		out, err := p.RunPrepared(ctx, runOpts)
		if err != nil {
			return backtestes.Metrics{}, err
		}
		return trainMetrics(out.Result, opts.Cash), nil
	}

	baseline := baseOutcome.BaselineReturnPct
	globalBestReward := math.Inf(-1)
	reserved := sampleOutCandidateCount * sampleOutSeedCount
	groups := make([]tuneGroupResult, 0, len(spaces)*len(seeds))
	fmt.Printf("tune_start spaces=%d seeds=%d groups=%d prune=%v prune_window=%d prune_factor=%.2f population=%d budget=%d baseline=%.4f\n", len(spaces), len(seeds), len(spaces)*len(seeds), flags.Prune, flags.PruneWindow, flags.PruneFactor, flags.Population, flags.Budget, baseline)
	for si, space := range spaces {
		for _, seed := range seeds {
			cfg := backtestes.DefaultConfig(seed)
			cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience, cfg.Timeout = flags.Population, flags.MaxGenerations, flags.Budget, flags.Patience, flags.Timeout
			if cfg.Budget <= reserved+cfg.Population {
				fmt.Fprintf(os.Stderr, "backtest: -budget must exceed population + %d sample-out evaluations\n", reserved)
				return 2
			}
			cfg.Budget -= reserved
			if flags.Prune {
				cfg.PruneCheck = tunePruneCheck(flags.PruneWindow, flags.PruneFactor, baseline, &globalBestReward)
			}
			started := time.Now()
			search, err := backtestes.Search(context.Background(), space, windows, cfg, evaluator)
			if err != nil {
				fmt.Fprintf(os.Stderr, "backtest: tune space %d seed %d: %v\n", si, seed, err)
				return 1
			}
			if len(search.Candidates) < sampleOutCandidateCount {
				fmt.Fprintf(os.Stderr, "backtest: tune: fewer than three ES candidates (space %d seed %d)\n", si, seed)
				return 1
			}
			tested, testSeeds, err := sampleOutTestCandidates(context.Background(), prepared, opts, si, search.Candidates[:sampleOutCandidateCount], search.TestSeed, windows, cfg.Weights)
			if err != nil {
				fmt.Fprintf(os.Stderr, "backtest: sample-out evaluation (space %d seed %d): %v\n", si, seed, err)
				return 1
			}
			group := tuneGroupResult{
				spaceIndex: si, seed: seed, search: search, tested: tested, testSeeds: testSeeds,
				historyReward: historyBestReward(search.Generations),
				medianScore:   tested[0].medianScore,
				duration:      time.Since(started),
			}
			returns, drawdowns, ratios := make([]float64, len(tested[0].metrics)), make([]float64, len(tested[0].metrics)), []float64{}
			for i, m := range tested[0].metrics {
				returns[i], drawdowns[i] = m.NetReturn, m.MaxDrawdown
				if m.UnfilledRatio != nil {
					ratios = append(ratios, *m.UnfilledRatio)
				}
			}
			group.medianReturn = quantile(returns, .5)
			group.medianDrawdown = quantile(drawdowns, .5)
			if len(ratios) > 0 {
				v := quantile(ratios, .5)
				group.medianUnfilled = &v
			}
			groups = append(groups, group)
			fmt.Println(tuneGroupSummaryLine(group))
		}
	}

	bestIdx := bestTuneGroup(groups)
	best := groups[bestIdx]
	bestParams := best.tested[0].candidate.Params
	fmt.Printf("tune_best space=%d seed=%d sample_out=%.6g ret_pct=%.4f status=%s\n", best.spaceIndex, best.seed, best.medianScore, best.medianReturn, best.search.StopReason)
	if !flags.Report {
		return 0
	}

	// 最终全量报告:最优参数在全窗重跑,作为唯一交付物;中间组不产生报告。
	finalOpts := opts
	finalOpts.Params = bestParams
	finalOutcome, err := backtestexec.Run(context.Background(), database, finalOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: tune final run: %v\n", err)
		return 1
	}
	sampleBaseline := best.tested[0].outcomes[0].BaselineReturnPct
	allTested := make([]testedCandidate, 0, len(groups)*sampleOutCandidateCount)
	for _, group := range groups {
		allTested = append(allTested, group.tested...)
	}
	sort.SliceStable(allTested, func(i, j int) bool { return allTested[i].medianScore > allTested[j].medianScore })
	reportCandidates := reportCandidatesList(allTested, spaces, sampleBaseline)
	for i := range reportCandidates {
		reportCandidates[i].Rank = i + 1
	}
	if len(reportCandidates) == 0 {
		fmt.Println("无可推荐参数: 样本外多 seed 未稳定胜出 buy-hold 基线")
	}
	gens := make([]backtestreport.Generation, len(best.search.Generations))
	for i, g := range best.search.Generations {
		gens[i] = backtestreport.Generation{Generation: g.Generation, EvaluationCount: g.EvaluationCount, TrainBestReturnPct: g.BestReturn, TrainMeanReturnPct: g.MeanReturn,
			TrainMedianReturnPct: g.MedianReturn, TrainStdReturnPct: g.StdReturn, HistoryBestReturnPct: g.HistoryBestReturn, ValidBestReturnPct: g.ValidBestReturn,
			MaxDrawdownPct: g.Best.MaxDrawdown, UnfilledRatio: g.Best.UnfilledRatio, EffectiveTrades: g.Best.EffectiveTrades, PopulationDispersion: g.Dispersion, MutationScale: g.MutationScale}
	}
	searchAudit := make(map[string]backtestreport.SearchBound, len(spaces[best.spaceIndex].Bounds))
	selectedHits := boundaryHits(spaces[best.spaceIndex], tacticalParams(bestParams, spaces[best.spaceIndex]))
	for name, b := range spaces[best.spaceIndex].Bounds {
		unit := b.Unit
		if name == "min_premium_per_share" {
			unit = reportCurrency(opts.Symbol) + "/股"
		}
		if name == "min_option_profit" {
			unit = reportCurrency(opts.Symbol) + "/笔"
		}
		searchAudit[name] = backtestreport.SearchBound{Min: b.Min, Max: b.Max, Unit: unit, HitBoundary: selectedHits[name]}
	}
	allSeeds := append([]int64{best.search.TrainSeed, best.search.ValidSeed}, best.testSeeds...)
	estimateCfg := backtestes.DefaultConfig(best.seed)
	estimateCfg.Population, estimateCfg.MaxGenerations, estimateCfg.Budget, estimateCfg.Patience, estimateCfg.Timeout = flags.Population, flags.MaxGenerations, flags.Budget, flags.Patience, flags.Timeout
	estimate := fmt.Sprintf("tune %d 空间 x %d 种子,每组最多 %d 次回测", len(spaces), len(seeds), min(flags.Budget, backtestes.EstimatedEvaluations(estimateCfg)+reserved))
	weights := backtestes.DefaultConfig(0).Weights
	rep, err := backtestreport.BuildES(backtestreport.ESInput{
		Run: backtestreport.Input{Symbol: opts.Symbol, Strategy: opts.Strategy, Params: bestParams, ConfigVersion: opts.ConfigVersion, CodeVersion: version, RunSeed: opts.Seed,
			InitialCash: opts.Cash, FeePerTrade: opts.Fee, Start: finalOutcome.StartTs, End: finalOutcome.EndTs, BaselineReturnPct: finalOutcome.BaselineReturnPct, SourceHash: finalOutcome.SourceHash, Result: finalOutcome.Result},
		Windows: reportWindows(windows), Train: backtestreport.Train{Algorithm: "ES", AlgorithmVersion: backtestes.AlgorithmVersion, GenerationCount: len(best.search.Generations), PopulationSize: flags.Population,
			EvaluationCount: best.search.EvaluationCount + reserved, Seeds: allSeeds, StopReason: best.search.StopReason, StopDetail: best.search.StopDetail, DurationSec: best.duration.Seconds(), EvaluationEstimate: estimate},
		Generations: gens, Candidates: reportCandidates, Reward: backtestreport.RewardAudit{FunctionVersion: backtestes.RewardVersion,
			Weights:             backtestreport.RewardWeights{LambdaDD: weights.LambdaDD, LambdaTail: weights.LambdaTail, LambdaTurnover: weights.LambdaTurnover},
			HardFailureHandling: "策略层候选 mask 预防硬失败,违规候选不进入评估;未成交已含于净收益,不重复计罚"}, SearchSpace: searchAudit,
		Trajectory: buildTrajectory(finalOutcome.Result, bestParams, opts.ConfigVersion, opts.Symbol, version, finalOutcome.SourceHash), TailLossPct: tailLoss(finalOutcome.Result.EquityCurve),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 1
	}
	jsonPath, htmlPath, err := backtestreport.Write(strings.TrimSpace(flags.ReportDir), rep)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 1
	}
	fmt.Printf("report_id=%s json=%s html=%s\n", rep.ReportID, jsonPath, htmlPath)
	if flags.Push {
		status, err := pushBacktestReport(context.Background(), rep)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: push: %v\n", err)
			return 1
		}
		fmt.Printf("push_status=%s report_id=%s\n", status, rep.ReportID)
	}
	return 0
}

// sampleOutTestCandidates evaluates each candidate on sample-out seeds derived
// from testSeed and ranks them by median reward score. ParallelMap collects in
// task order, so results are bit-for-bit deterministic.
func sampleOutTestCandidates(ctx context.Context, prepared map[backtestes.Window]*backtestexec.Prepared, opts backtestexec.Options, spaceIndex int, candidates []backtestes.Candidate, testSeed int64, windows backtestes.Windows, weights backtestes.RewardWeights) ([]testedCandidate, []int64, error) {
	testSeeds := make([]int64, sampleOutSeedCount)
	for i := range testSeeds {
		testSeeds[i] = backtestes.DeriveSeed(testSeed, fmt.Sprintf("sample-out-%d", i))
	}
	type sampleEvaluation struct {
		outcome *backtestexec.Outcome
		metrics backtestes.Metrics
	}
	tasks := make([]func(context.Context) (sampleEvaluation, error), 0, len(candidates)*len(testSeeds))
	for _, candidate := range candidates {
		for _, sampleSeed := range testSeeds {
			candidateParams, seed, p := candidate.Params, sampleSeed, prepared[windows.Test]
			tasks = append(tasks, func(taskCtx context.Context) (sampleEvaluation, error) {
				runOpts := opts
				runOpts.Params, runOpts.Seed, runOpts.From = candidateParams, seed, windows.Test.From
				runOpts.To = windows.Test.To
				out, err := p.RunPrepared(taskCtx, runOpts)
				if err != nil {
					return sampleEvaluation{}, err
				}
				return sampleEvaluation{outcome: out, metrics: trainMetrics(out.Result, opts.Cash)}, nil
			})
		}
	}
	results, err := backtestes.ParallelMap(ctx, tasks)
	if err != nil {
		return nil, nil, err
	}
	tested := make([]testedCandidate, 0, len(candidates))
	for candidateIndex, candidate := range candidates {
		tc := testedCandidate{candidate: candidate, spaceIndex: spaceIndex}
		start := candidateIndex * len(testSeeds)
		for _, sample := range results[start : start+len(testSeeds)] {
			tc.outcomes = append(tc.outcomes, sample.outcome)
			tc.metrics = append(tc.metrics, sample.metrics)
		}
		scores := make([]float64, len(tc.metrics))
		for i, m := range tc.metrics {
			scores[i] = m.Score(weights)
		}
		tc.medianScore = quantile(scores, .5)
		tested = append(tested, tc)
	}
	sort.SliceStable(tested, func(i, j int) bool { return tested[i].medianScore > tested[j].medianScore })
	return tested, testSeeds, nil
}

// reportCandidatesList keeps only candidates whose sample-out multi-seed
// evidence beats the buy-hold baseline, with per-space boundary-hit audit.
func reportCandidatesList(tested []testedCandidate, spaces []backtestes.Space, baseline float64) []backtestreport.Candidate {
	out := make([]backtestreport.Candidate, 0, len(tested))
	for _, tc := range tested {
		returns, drawdowns, ratios := make([]float64, len(tc.metrics)), make([]float64, len(tc.metrics)), []float64{}
		for i, m := range tc.metrics {
			returns[i], drawdowns[i] = m.NetReturn, m.MaxDrawdown
			if m.UnfilledRatio != nil {
				ratios = append(ratios, *m.UnfilledRatio)
			}
		}
		if !recommendableCandidate(tc.metrics, baseline) {
			continue
		}
		params := tacticalParams(tc.candidate.Params, spaces[tc.spaceIndex])
		var ratio *float64
		if len(ratios) > 0 {
			v := quantile(ratios, .5)
			ratio = &v
		}
		out = append(out, backtestreport.Candidate{Params: params, BoundaryHits: boundaryHits(spaces[tc.spaceIndex], params), VsBaselinePct: quantile(returns, .5) - baseline,
			Stats: backtestreport.CandidateStats{MedianReturnPct: quantile(returns, .5), P10ReturnPct: quantile(returns, .1), P90ReturnPct: quantile(returns, .9), MedianMaxDrawdownPct: quantile(drawdowns, .5), MedianUnfilledRatio: ratio}})
	}
	return out
}

func historyBestReward(gens []backtestes.Generation) float64 {
	best := math.Inf(-1)
	for _, g := range gens {
		if g.BestScore > best {
			best = g.BestScore
		}
	}
	return best
}

// convergenceGeneration is the 1-based generation at which the final
// validation champion emerged (last strict improvement of ValidBestScore).
func convergenceGeneration(gens []backtestes.Generation) int {
	if len(gens) == 0 {
		return 0
	}
	last := 1
	for i := 1; i < len(gens); i++ {
		if gens[i].ValidBestScore > gens[i-1].ValidBestScore {
			last = i + 1
		}
	}
	return last
}

func bestTuneGroup(groups []tuneGroupResult) int {
	best := 0
	for i := 1; i < len(groups); i++ {
		if groups[i].medianScore > groups[best].medianScore {
			best = i
		}
	}
	return best
}

// tuneGroupSummaryLine is the documented per-group row of the tune summary
// table (doc/BACKTEST.md 多空间自动寻优). reward is the train history-best
// score; sample_out is the selected candidate's median sample-out score.
func tuneGroupSummaryLine(g tuneGroupResult) string {
	status, prunedAt := "ok", "-"
	if g.search.StopReason == "pruned" {
		status, prunedAt = "pruned", fmt.Sprintf("%d", len(g.search.Generations))
	}
	unfilled := "N/A"
	if g.medianUnfilled != nil {
		unfilled = fmt.Sprintf("%.2f%%", *g.medianUnfilled*100)
	}
	return fmt.Sprintf("tune_group space=%d seed=%d status=%s reward=%.6g sample_out=%.6g ret_pct=%.4f dd_pct=%.4f unfilled_pct=%s gens=%d converged=%d evals=%d dur_s=%.2f pruned_at=%s", g.spaceIndex, g.seed, status, g.historyReward, g.medianScore, g.medianReturn, g.medianDrawdown, unfilled, len(g.search.Generations), convergenceGeneration(g.search.Generations), g.search.EvaluationCount+sampleOutCandidateCount*sampleOutSeedCount, g.duration.Seconds(), prunedAt)
}

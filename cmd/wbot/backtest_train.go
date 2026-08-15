package main

import (
	"context"
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
	"github.com/jiayu/wbot/internal/wheelstore"
)

type backtestTrainFlags struct {
	Population, MaxGenerations, Budget, Patience int
	Timeout                                      time.Duration
	Report                                       bool
	ReportDir                                    string
	Push                                         bool
	Cache                                        bool
}

func runBacktestTrain(dsn, rawSpace string, opts backtestexec.Options, flags backtestTrainFlags) int {
	// buildTrajectory consumes CandidateDetails for the RL trajectory; ES
	// windows are short, so materializing candidates here costs little.
	opts.TraceCandidates = true
	canonical, err := strategy.CanonicalParams(opts.Params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: train params: %v\n", err)
		return 2
	}
	space, err := backtestes.ParseSpace(rawSpace, canonical)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: -train: %v\n", err)
		return 2
	}
	cfg := backtestes.DefaultConfig(opts.Seed)
	cfg.Population, cfg.MaxGenerations, cfg.Budget, cfg.Patience, cfg.Timeout = flags.Population, flags.MaxGenerations, flags.Budget, flags.Patience, flags.Timeout
	reserved := sampleOutCandidateCount * sampleOutSeedCount
	if cfg.Budget <= reserved+cfg.Population {
		fmt.Fprintf(os.Stderr, "backtest: -budget must exceed population + %d sample-out evaluations\n", reserved)
		return 2
	}
	searchBudget := cfg.Budget
	cfg.Budget -= reserved
	fmt.Printf("预计评估次数=%d (ES最多%d + 样本外%d) population=%d max_generations=%d\n", min(searchBudget, backtestes.EstimatedEvaluations(cfg)+reserved), backtestes.EstimatedEvaluations(cfg), reserved, cfg.Population, cfg.MaxGenerations)

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
		fmt.Fprintf(os.Stderr, "backtest: train data probe: %v\n", err)
		return 1
	}
	if err := requireTrainCoverage(baseOutcome, opts.Symbol, opts.Cash); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: train data probe: %v\n", err)
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

	// Prepare each window before any worker starts. After this point the map is
	// read-only and each Prepared value contains immutable market inputs, so
	// concurrent evaluators never race on lazy map initialization or RunSeed.
	prepared := make(map[backtestes.Window]*backtestexec.Prepared, 3)
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
		if _, tunesIVRank := space.Bounds["min_iv_rank"]; tunesIVRank {
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
	started := time.Now()
	search, err := backtestes.Search(context.Background(), space, windows, cfg, evaluator)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: train: %v\n", err)
		return 1
	}

	if len(search.Candidates) < sampleOutCandidateCount {
		fmt.Fprintln(os.Stderr, "backtest: train: fewer than three ES candidates")
		return 1
	}
	tested, testSeeds, err := sampleOutTestCandidates(context.Background(), prepared, opts, 0, search.Candidates[:sampleOutCandidateCount], search.TestSeed, windows, cfg.Weights)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: sample-out evaluation: %v\n", err)
		return 1
	}
	selected := tested[0]
	medianIndex := medianOutcomeIndex(selected.metrics)
	selectedOutcome := selected.outcomes[medianIndex]
	baseline := selectedOutcome.BaselineReturnPct

	reportCandidates := reportCandidatesList(tested, []backtestes.Space{space}, baseline)
	for i := range reportCandidates {
		reportCandidates[i].Rank = i + 1
	}
	if len(reportCandidates) == 0 {
		fmt.Println("无可推荐参数: 样本外多 seed 未稳定胜出 buy-hold 基线")
	}
	fmt.Printf("train_stop=%s generations=%d evaluations=%d train_seed=%d test_seed=%d sample_out_return=%v\n", search.StopReason, len(search.Generations), search.EvaluationCount+reserved, search.TrainSeed, testSeeds[0], selected.metrics[medianIndex].NetReturn)
	if !flags.Report {
		return 0
	}

	gens := make([]backtestreport.Generation, len(search.Generations))
	for i, g := range search.Generations {
		gens[i] = backtestreport.Generation{Generation: g.Generation, EvaluationCount: g.EvaluationCount, TrainBestReturnPct: g.BestReturn, TrainMeanReturnPct: g.MeanReturn,
			TrainMedianReturnPct: g.MedianReturn, TrainStdReturnPct: g.StdReturn, HistoryBestReturnPct: g.HistoryBestReturn, ValidBestReturnPct: g.ValidBestReturn,
			MaxDrawdownPct: g.Best.MaxDrawdown, UnfilledRatio: g.Best.UnfilledRatio, EffectiveTrades: g.Best.EffectiveTrades, PopulationDispersion: g.Dispersion, MutationScale: g.MutationScale}
	}
	searchAudit := make(map[string]backtestreport.SearchBound, len(space.Bounds))
	selectedHits := boundaryHits(space, tacticalParams(selected.candidate.Params, space))
	for name, b := range space.Bounds {
		unit := b.Unit
		if name == "min_premium_per_share" {
			unit = reportCurrency(opts.Symbol) + "/股"
		}
		if name == "min_option_profit" {
			unit = reportCurrency(opts.Symbol) + "/笔"
		}
		searchAudit[name] = backtestreport.SearchBound{Min: b.Min, Max: b.Max, Unit: unit, HitBoundary: selectedHits[name]}
	}
	allSeeds := append([]int64{search.TrainSeed, search.ValidSeed}, testSeeds...)
	duration := time.Since(started).Seconds()
	estimate := fmt.Sprintf("启动前输出:预计最多 %d 次回测", min(searchBudget, backtestes.EstimatedEvaluations(cfg)+reserved))
	rep, err := backtestreport.BuildES(backtestreport.ESInput{
		Run: backtestreport.Input{Symbol: opts.Symbol, Strategy: opts.Strategy, Params: selected.candidate.Params, ConfigVersion: opts.ConfigVersion, CodeVersion: version, RunSeed: opts.Seed,
			InitialCash: opts.Cash, FeePerTrade: opts.Fee, Start: selectedOutcome.StartTs, End: selectedOutcome.EndTs, BaselineReturnPct: baseline, SourceHash: selectedOutcome.SourceHash, Result: selectedOutcome.Result},
		Windows: reportWindows(windows), Train: backtestreport.Train{Algorithm: "ES", AlgorithmVersion: backtestes.AlgorithmVersion, GenerationCount: len(search.Generations), PopulationSize: cfg.Population,
			EvaluationCount: search.EvaluationCount + reserved, Seeds: allSeeds, StopReason: search.StopReason, StopDetail: search.StopDetail, DurationSec: duration, EvaluationEstimate: estimate},
		Generations: gens, Candidates: reportCandidates, Reward: backtestreport.RewardAudit{FunctionVersion: backtestes.RewardVersion,
			Weights:             backtestreport.RewardWeights{LambdaDD: cfg.Weights.LambdaDD, LambdaTail: cfg.Weights.LambdaTail, LambdaTurnover: cfg.Weights.LambdaTurnover},
			HardFailureHandling: "策略层候选 mask 预防硬失败,违规候选不进入评估;未成交已含于净收益,不重复计罚"}, SearchSpace: searchAudit,
		Trajectory: buildTrajectory(selectedOutcome.Result, selected.candidate.Params, opts.ConfigVersion, opts.Symbol, version, selectedOutcome.SourceHash), TailLossPct: selected.metrics[medianIndex].TailLoss,
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
	if flags.Cache {
		if err := cacheBacktestReport(context.Background(), database, rep, jsonPath, false); err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
		fmt.Printf("cache_symbol=%s approved_state=%s\n", rep.Identity.Symbol, wheelstore.StrategyCacheResearchCandidate)
	}
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

func requireTrainCoverage(outcome *backtestexec.Outcome, symbol string, initialCash float64) error {
	if outcome == nil || outcome.Result == nil {
		return fmt.Errorf("%w: %s (empty training probe)", backtestexec.ErrNoOptionData, symbol)
	}
	quality := outcome.Result.DataQuality
	coverage := 0.0
	if quality.ValidCoverageRatio != nil {
		coverage = *quality.ValidCoverageRatio
	}
	effectiveTrades := trainMetrics(outcome.Result, initialCash).EffectiveTrades
	if coverage <= 0 || effectiveTrades == 0 {
		return fmt.Errorf("%w: %s (valid_coverage=%.2f%% effective_trades=%d; ES requires positive coverage and at least one effective fill)",
			backtestexec.ErrNoOptionData, symbol, coverage*100, effectiveTrades)
	}
	return nil
}

func recommendableCandidate(metrics []backtestes.Metrics, baseline float64) bool {
	if len(metrics) == 0 {
		return false
	}
	returns := make([]float64, len(metrics))
	for i, metric := range metrics {
		if metric.EffectiveTrades == 0 {
			return false
		}
		returns[i] = metric.NetReturn
	}
	return quantile(returns, 0) > baseline
}

func trainMetrics(r *backtest.Result, initialCash float64) backtestes.Metrics {
	filled := 0
	for _, tr := range r.Trades {
		if isChargedTrade(tr) && (tr.Filled || tr.UnfilledModel == "") {
			filled++
		}
	}
	// 评价口径 = 权利金净收益(reward-3.0,2026-08-14 老板指令「仅以权利金为最大
	// 目标」):期权腿净收益(权利金收入 − 平仓 − 期权/行权费),忽略正股价差收益;
	// 正股仅急涨急跌应急操作,盈亏不参与战术寻优。回撤/尾部惩罚保留作风控。
	return backtestes.Metrics{NetReturn: r.Attribution.PremiumNetAmount / initialCash, MaxDrawdown: r.MaxDrawdown, TailLoss: tailLoss(r.EquityCurve), CostPct: r.Fees.TotalAmount / initialCash,
		UnfilledRatio: r.Unfilled.UnfilledRatio, EffectiveTrades: filled}
}

func isChargedTrade(tr backtest.Trade) bool {
	return tr.Action == "buy" || tr.Action == "sell" || strings.HasPrefix(tr.Action, "sell-") || strings.HasPrefix(tr.Action, "buy-")
}

func tailLoss(curve []backtest.EquityPoint) float64 {
	if len(curve) < 2 {
		return 0
	}
	losses := []float64{}
	for i := 1; i < len(curve); i++ {
		if curve[i-1].Equity > 0 {
			v := (curve[i-1].Equity - curve[i].Equity) / curve[i-1].Equity
			if v > 0 {
				losses = append(losses, v)
			}
		}
	}
	if len(losses) == 0 {
		return 0
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(losses)))
	n := max(1, int(math.Ceil(float64(len(losses))*.1)))
	sum := 0.0
	for _, v := range losses[:n] {
		sum += v
	}
	return sum / float64(n)
}
func quantile(in []float64, q float64) float64 {
	if len(in) == 0 {
		return 0
	}
	cp := append([]float64(nil), in...)
	sort.Float64s(cp)
	pos := q * float64(len(cp)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return cp[lo]
	}
	return cp[lo] + (cp[hi]-cp[lo])*(pos-float64(lo))
}
func medianOutcomeIndex(ms []backtestes.Metrics) int {
	vals := make([]float64, len(ms))
	for i, m := range ms {
		vals[i] = m.NetReturn
	}
	med := quantile(vals, .5)
	best, d := 0, math.Inf(1)
	for i, v := range vals {
		if math.Abs(v-med) < d {
			best, d = i, math.Abs(v-med)
		}
	}
	return best
}
func tacticalParams(all map[string]any, space backtestes.Space) map[string]any {
	out := map[string]any{}
	for name := range space.Bounds {
		if v, ok := all[name]; ok {
			out[name] = v
		}
	}
	return out
}
func boundaryHits(space backtestes.Space, params map[string]any) map[string]bool {
	out := map[string]bool{}
	for n, b := range space.Bounds {
		v, ok := params[n]
		if !ok {
			continue
		}
		x := asFloat(v)
		out[n] = math.Abs(x-b.Min) < 1e-12 || math.Abs(x-b.Max) < 1e-12
	}
	return out
}
func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}
func reportWindows(w backtestes.Windows) backtestreport.Windows {
	cv := func(x backtestes.Window) backtestreport.Window {
		return backtestreport.Window{From: x.From.UTC().Format(time.RFC3339Nano), To: x.To.UTC().Format(time.RFC3339Nano)}
	}
	return backtestreport.Windows{Train: cv(w.Train), Valid: cv(w.Valid), Test: cv(w.Test)}
}

func buildTrajectory(r *backtest.Result, params map[string]any, configVersion *int, symbol, codeVersion, dataHash string) []backtestreport.TrajectoryStep {
	out := make([]backtestreport.TrajectoryStep, 0, len(r.Signals))
	peak := 0.0
	for i, s := range r.Signals {
		candidates := make([]map[string]any, 0, len(s.CandidateDetails))
		for _, c := range s.CandidateDetails {
			q := c.Quote
			reason := any(nil)
			if len(c.Reasons) > 0 {
				reason = strings.Join(c.Reasons, "; ")
			}
			candidates = append(candidates, map[string]any{"contract": firstNonEmpty(q.Symbol, q.Code), "is_call": strings.EqualFold(string(q.OptionType), "call"), "expiry": q.Expiry.UTC().Format("2006-01-02"), "strike": q.Strike, "delta": q.Delta, "bid": q.Bid, "ask": q.Ask, "spread_pct": safeSpread(q.Bid, q.Ask), "implied_vol": q.ImpliedVol, "theta": q.Theta, "volume": q.Volume, "open_interest": q.OpenInterest, "lot_size": q.LotSize, "quality": c.Quality, "masked": !c.Accepted, "mask_reason": reason, "raw_score": c.Quality})
		}
		filled := false
		fillModel := r.Unfilled.ModelVersion
		chargedFee := 0.0
		for _, tr := range r.Trades {
			if tr.Ts.Equal(s.Ts) && tr.Symbol == s.CandidateCode {
				filled = tr.Filled
				if tr.UnfilledModel != "" {
					fillModel = tr.UnfilledModel
				}
				if isChargedTrade(tr) && (tr.Filled || tr.UnfilledModel == "") {
					chargedFee = tr.Fee
				}
				break
			}
		}
		equityDelta, incrementalDD := 0.0, 0.0
		if i < len(r.EquityCurve) {
			equity := r.EquityCurve[i].Equity
			if i > 0 {
				equityDelta = equity - r.EquityCurve[i-1].Equity
			}
			if equity > peak {
				peak = equity
			} else if peak > 0 {
				incrementalDD = (peak - equity) / peak
			}
		}
		termination := any(nil)
		if i == len(r.Signals)-1 {
			termination = "end_of_window"
		}
		out = append(out, backtestreport.TrajectoryStep{Step: i, DecisionTime: s.Ts.UTC().Format(time.RFC3339Nano), BarTS: s.Ts.UTC().Format(time.RFC3339Nano),
			StateBefore: map[string]any{"underlying_price": s.UnderlyingPrice, "actual_inventory": s.ActualInventory, "effective_inventory": s.EffectiveInventory, "target_inventory": s.TargetInventory, "cash": s.CashBefore, "strategy_state": params["strategic_state"], "last_filled_price": nil, "bars_since_last_action": nil}, Candidates: candidates,
			Action: map[string]any{"type": trajectoryAction(s), "candidate_index": candidateIndex(candidates, s.CandidateCode), "quantity": s.Quantity}, Fill: map[string]any{"simulated": s.CandidateCode != "", "filled": filled, "unfilled_model_version": fillModel},
			StateAfter: map[string]any{"effective_inventory": s.EffectiveInventory, "cash": s.CashAfter}, RewardAtoms: map[string]any{"equity_delta": equityDelta, "fees": chargedFee, "slippage": 0.0, "incremental_drawdown": incrementalDD, "tail_exposure_delta": 0.0}, Termination: termination,
			Versions: map[string]any{"config": configVersion, "data_hash": dataHash, "code": codeVersion, "fill_model": r.Unfilled.ModelVersion, "symbol": symbol}})
	}
	return out
}
func trajectoryAction(s backtest.SignalTrace) string {
	if s.CandidateCode == "" {
		return "HOLD"
	}
	return "SELL_" + strings.ToUpper(s.Direction)
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func safeSpread(bid, ask float64) float64 {
	if ask <= 0 {
		return 0
	}
	return (ask - bid) / ask
}
func candidateIndex(cs []map[string]any, code string) any {
	for i, c := range cs {
		if c["contract"] == code {
			return i
		}
	}
	return nil
}

func reportCurrency(symbol string) string {
	if strings.HasPrefix(strings.ToUpper(symbol), "HK.") {
		return "HKD"
	}
	if strings.HasPrefix(strings.ToUpper(symbol), "US.") {
		return "USD"
	}
	return "币种"
}

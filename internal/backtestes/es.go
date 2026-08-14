// Package backtestes implements the deterministic evolution strategy used by
// the backtest CLI. It owns only tactical Wheel parameters; callers keep the
// strategic configuration immutable and provide the actual backtest evaluator.
package backtestes

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"github.com/jiayu/wbot/internal/wheel"
)

const (
	AlgorithmVersion = "es-1.0"
	// reward-2.0 evaluates on realized P&L only (2026-08-14 老板指令): unrealized
	// marks depend on the strategic position curve, so strategic choices must
	// not drive the tactical search. reward-1.0 used mark-to-market net return.
	RewardVersion = "reward-2.0"
)

var tacticalOrder = []string{"move_interval_pct", "min_premium_per_share", "min_option_profit", "stock_switch_pct", "trade_gap", "min_option_quality", "min_dte", "max_dte"}

type Bound struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Unit     string  `json:"unit"`
	Discrete bool    `json:"-"`
}

type Space struct {
	Bounds map[string]Bound
	Base   map[string]any
}

// ParseSpace accepts the CLI range object. Values may be JSON numbers or
// decimal strings so the documented ["0.5","3"] spelling stays exact.
func ParseSpace(data string, base map[string]any) (Space, error) {
	var raw map[string][]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return Space{}, fmt.Errorf("train search space: %w", err)
	}
	if len(raw) == 0 {
		return Space{}, errors.New("train search space: at least one tactical range is required")
	}
	allowed := map[string]Bound{
		"move_interval_pct": {Unit: "%"}, "min_premium_per_share": {Unit: "币种/股"}, "min_option_profit": {Unit: "币种/笔"},
		"stock_switch_pct": {Unit: "%"}, "trade_gap": {Unit: "股", Discrete: true},
		"min_option_quality": {Unit: "[0,1]"}, "min_dte": {Unit: "自然日", Discrete: true},
		"max_dte": {Unit: "自然日", Discrete: true},
	}
	bounds := make(map[string]Bound, len(raw))
	for name, pair := range raw {
		proto, ok := allowed[name]
		if !ok {
			return Space{}, fmt.Errorf("train search space: %q is not a tactical parameter", name)
		}
		if len(pair) != 2 {
			return Space{}, fmt.Errorf("train search space: %s wants [min,max]", name)
		}
		lo, err := rangeNumber(pair[0])
		if err != nil {
			return Space{}, fmt.Errorf("train search space: %s min: %w", name, err)
		}
		hi, err := rangeNumber(pair[1])
		if err != nil {
			return Space{}, fmt.Errorf("train search space: %s max: %w", name, err)
		}
		if lo > hi || lo < 0 || math.IsNaN(lo) || math.IsInf(lo, 0) || math.IsNaN(hi) || math.IsInf(hi, 0) {
			return Space{}, fmt.Errorf("train search space: invalid %s range [%v,%v]", name, lo, hi)
		}
		if proto.Discrete && (lo != math.Trunc(lo) || hi != math.Trunc(hi)) {
			return Space{}, fmt.Errorf("train search space: %s bounds must be integers", name)
		}
		if name == "min_option_quality" && hi > 1 {
			return Space{}, errors.New("train search space: min_option_quality must be in [0,1]")
		}
		if (name == "min_dte" || name == "max_dte") && (lo < wheel.MinWheelDTE || hi > wheel.MaxWheelDTE) {
			return Space{}, fmt.Errorf("train search space: %s must be within %d..%d", name, wheel.MinWheelDTE, wheel.MaxWheelDTE)
		}
		proto.Min, proto.Max = lo, hi
		bounds[name] = proto
	}
	if min, ok := bounds["min_dte"]; ok {
		maxHi := number(base["max_dte"])
		if max, exists := bounds["max_dte"]; exists {
			maxHi = max.Max
		}
		if min.Min > maxHi || min.Max > maxHi {
			return Space{}, errors.New("train search space: min_dte cannot exceed max_dte")
		}
	}
	if max, ok := bounds["max_dte"]; ok {
		if _, tunesMin := bounds["min_dte"]; !tunesMin && max.Max < number(base["min_dte"]) {
			return Space{}, errors.New("train search space: max_dte cannot be below fixed min_dte")
		}
	}
	return Space{Bounds: bounds, Base: cloneMap(base)}, nil
}

func rangeNumber(raw json.RawMessage) (float64, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	switch x := v.(type) {
	case float64:
		return x, nil
	case string:
		return strconv.ParseFloat(x, 64)
	default:
		return 0, errors.New("want a number or decimal string")
	}
}

type Windows struct {
	Train Window `json:"train"`
	Valid Window `json:"valid"`
	Test  Window `json:"test"`
}

type Window struct{ From, To time.Time }

const MinSplitBars = 5

// SplitWindows makes a chronological 60/20/20 split. Boundaries are
// half-open in evaluators ([from,to)), except the final test end.
func SplitWindows(from, to time.Time) (Windows, error) {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return Windows{}, errors.New("train windows: invalid data range")
	}
	d := to.Sub(from)
	a := from.Add(d * 3 / 5)
	b := from.Add(d * 4 / 5)
	return Windows{Train: Window{from, a}, Valid: Window{a, b}, Test: Window{b, to}}, nil
}

// ValidateWindowBars ensures the time-based 60/20/20 split has enough input
// to evaluate every window. Train and validation are half-open; the final test
// window includes its end, matching the CLI evaluators.
func ValidateWindowBars(windows Windows, barTimes []time.Time) error {
	counts := [3]int{}
	for _, ts := range barTimes {
		switch {
		case !ts.Before(windows.Train.From) && ts.Before(windows.Train.To):
			counts[0]++
		case !ts.Before(windows.Valid.From) && ts.Before(windows.Valid.To):
			counts[1]++
		case !ts.Before(windows.Test.From) && !ts.After(windows.Test.To):
			counts[2]++
		}
	}
	if len(barTimes) < MinSplitBars || counts[0] == 0 || counts[1] == 0 || counts[2] == 0 {
		return fmt.Errorf("train windows: 数据不足以 60/20/20 切分,需 ≥%d 根 bar 且每个窗口至少 1 根 (当前 total=%d, train=%d, valid=%d, test=%d)",
			MinSplitBars, len(barTimes), counts[0], counts[1], counts[2])
	}
	return nil
}

type RewardWeights struct{ LambdaDD, LambdaTail, LambdaTurnover float64 }

type Metrics struct {
	NetReturn, MaxDrawdown, TailLoss, CostPct float64
	UnfilledRatio                             *float64
	EffectiveTrades                           int
}

func (m Metrics) Score(w RewardWeights) float64 {
	return m.NetReturn - w.LambdaDD*m.MaxDrawdown - w.LambdaTail*m.TailLoss - w.LambdaTurnover*m.CostPct
}

type Evaluator func(context.Context, map[string]any, Window, int64) (Metrics, error)

type Config struct {
	Population, MaxGenerations, Budget, Patience   int
	EliteFraction, ImmigrantFraction, Sigma        float64
	MinAbsoluteImprovement, MinRelativeImprovement float64
	Seed                                           int64
	Timeout                                        time.Duration
	Weights                                        RewardWeights
}

func DefaultConfig(seed int64) Config {
	return Config{Population: 20, MaxGenerations: 40, Budget: 840, Patience: 8,
		EliteFraction: .2, ImmigrantFraction: .1, Sigma: .12,
		MinAbsoluteImprovement: .0001, MinRelativeImprovement: .005, Seed: effectiveSeed(seed), Timeout: 10 * time.Minute,
		Weights: RewardWeights{LambdaDD: .3, LambdaTail: .15, LambdaTurnover: .1}}
}

type Candidate struct {
	Params            map[string]any
	Train, Valid      Metrics
	Score, ValidScore float64
}

type Generation struct {
	Generation, EvaluationCount                                                   int
	BestScore, MeanScore, MedianScore, StdScore, HistoryBestScore, ValidBestScore float64
	BestReturn, MeanReturn, MedianReturn, StdReturn                               float64
	HistoryBestReturn, ValidBestReturn                                            float64
	Best                                                                          Metrics
	Dispersion, MutationScale                                                     float64
}

type Result struct {
	Generations                    []Generation
	Candidates                     []Candidate
	EvaluationCount                int
	StopReason, StopDetail         string
	TrainSeed, ValidSeed, TestSeed int64
}

func EstimatedEvaluations(c Config) int {
	n := c.Population*c.MaxGenerations + c.MaxGenerations
	if c.Budget > 0 && n > c.Budget {
		return c.Budget
	}
	return n
}

func Search(parent context.Context, space Space, windows Windows, cfg Config, eval Evaluator) (Result, error) {
	return search(parent, space, windows, cfg, eval, EvaluationWorkers)
}

func search(parent context.Context, space Space, windows Windows, cfg Config, eval Evaluator, workers int) (Result, error) {
	if eval == nil {
		return Result{}, errors.New("es: nil evaluator")
	}
	if cfg.Population < 16 || cfg.Population > 24 || cfg.MaxGenerations <= 0 || cfg.Budget < cfg.Population+1 || cfg.Patience <= 0 {
		return Result{}, errors.New("es: population must be 16..24 and generations/budget/patience must be positive")
	}
	ctx := parent
	cancel := func() {}
	if cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, cfg.Timeout)
	}
	defer cancel()
	rng := rand.New(rand.NewSource(effectiveSeed(cfg.Seed)))
	names := orderedNames(space.Bounds)
	trainSeed := DeriveSeed(cfg.Seed, "train")
	validSeed := DeriveSeed(cfg.Seed, "valid")
	testSeed := DeriveSeed(cfg.Seed, "test")
	result := Result{TrainSeed: trainSeed, ValidSeed: validSeed, TestSeed: testSeed}
	genes := make([][]float64, cfg.Population)
	for i := range genes {
		genes[i] = randomGene(rng, len(names))
	}
	sigma, stale := cfg.Sigma, 0
	bestValid := math.Inf(-1)
	var global Candidate
	all := make([]Candidate, 0, cfg.Population*cfg.MaxGenerations)
	for generation := 0; generation < cfg.MaxGenerations; generation++ {
		if err := ctx.Err(); err != nil {
			result.StopReason, result.StopDetail = "timeout", err.Error()
			break
		}
		if result.EvaluationCount+cfg.Population+1 > cfg.Budget {
			result.StopReason, result.StopDetail = "budget_exhausted", "总回测预算已用尽"
			break
		}
		pop := make([]Candidate, 0, cfg.Population)
		scores := make([]float64, 0, cfg.Population)
		returns := make([]float64, 0, cfg.Population)
		requests := make([]EvaluationRequest, cfg.Population)
		for i, gene := range genes {
			requests[i] = EvaluationRequest{Params: space.decode(names, gene), Window: windows.Train, Seed: trainSeed}
		}
		metrics, err := evaluateWithWorkers(ctx, eval, requests, workers)
		if err != nil {
			return result, fmt.Errorf("es: train evaluation: %w", err)
		}
		for i, m := range metrics {
			params := requests[i].Params
			c := Candidate{Params: params, Train: m, Score: m.Score(cfg.Weights)}
			pop, all, scores = append(pop, c), append(all, c), append(scores, c.Score)
			returns = append(returns, m.NetReturn)
			result.EvaluationCount++
		}
		sort.SliceStable(pop, func(i, j int) bool { return pop[i].Score > pop[j].Score })
		champ := pop[0]
		validMetrics, err := evaluateWithWorkers(ctx, eval, []EvaluationRequest{{Params: champ.Params, Window: windows.Valid, Seed: validSeed}}, workers)
		if err != nil {
			return result, fmt.Errorf("es: validation evaluation: %w", err)
		}
		vm := validMetrics[0]
		result.EvaluationCount++
		champ.Valid, champ.ValidScore = vm, vm.Score(cfg.Weights)
		all[len(all)-cfg.Population].Valid, all[len(all)-cfg.Population].ValidScore = vm, champ.ValidScore
		threshold := math.Max(cfg.MinAbsoluteImprovement, math.Abs(bestValid)*cfg.MinRelativeImprovement)
		improved := math.IsInf(bestValid, -1) || champ.ValidScore > bestValid+threshold
		if improved {
			bestValid, global, stale, sigma = champ.ValidScore, champ, 0, math.Min(.5, sigma*1.05)
		} else {
			stale++
			sigma = math.Max(.01, sigma*.9)
		}
		mean, median, std := stats(scores)
		meanReturn, medianReturn, stdReturn := stats(returns)
		bestReturn := returns[0]
		for _, v := range returns[1:] {
			if v > bestReturn {
				bestReturn = v
			}
		}
		result.Generations = append(result.Generations, Generation{Generation: generation, EvaluationCount: result.EvaluationCount,
			BestScore: pop[0].Score, MeanScore: mean, MedianScore: median, StdScore: std, HistoryBestScore: global.Score,
			ValidBestScore: bestValid, BestReturn: bestReturn, MeanReturn: meanReturn, MedianReturn: medianReturn, StdReturn: stdReturn,
			HistoryBestReturn: global.Train.NetReturn, ValidBestReturn: global.Valid.NetReturn,
			Best: pop[0].Train, Dispersion: geneDispersion(genes), MutationScale: sigma})
		if stale >= cfg.Patience {
			result.StopReason, result.StopDetail = "early_stop", fmt.Sprintf("验证集连续 %d 代未达到绝对 %.6g 或相对 %.4g 改善", cfg.Patience, cfg.MinAbsoluteImprovement, cfg.MinRelativeImprovement)
			break
		}
		eliteN := max(1, int(math.Ceil(float64(cfg.Population)*cfg.EliteFraction)))
		immigrants := int(math.Round(float64(cfg.Population) * cfg.ImmigrantFraction))
		next := make([][]float64, 0, cfg.Population)
		next = append(next, encode(space, names, global.Params)) // global champion is never lost
		for len(next) < cfg.Population-immigrants {
			parentGene := encode(space, names, pop[rng.Intn(eliteN)].Params)
			child := make([]float64, len(parentGene))
			for j := range child {
				child[j] = clamp01(parentGene[j] + rng.NormFloat64()*sigma)
			}
			next = append(next, child)
		}
		for len(next) < cfg.Population {
			next = append(next, randomGene(rng, len(names)))
		}
		genes = next
	}
	if result.StopReason == "" {
		result.StopReason, result.StopDetail = "max_generations", "达到最大代数"
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	seen := map[string]bool{}
	for _, c := range all {
		key, _ := json.Marshal(c.Params)
		if seen[string(key)] {
			continue
		}
		seen[string(key)] = true
		result.Candidates = append(result.Candidates, c)
		if len(result.Candidates) == 5 {
			break
		}
	}
	return result, nil
}

func DeriveSeed(seed int64, purpose string) int64 {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", effectiveSeed(seed), purpose)))
	v := int64(binary.BigEndian.Uint64(h[:8]) & math.MaxInt64)
	if v == 0 {
		return 1
	}
	return v
}

func (s Space) decode(names []string, gene []float64) map[string]any {
	out := cloneMap(s.Base)
	for i, name := range names {
		b := s.Bounds[name]
		v := b.Min + clamp01(gene[i])*(b.Max-b.Min)
		if b.Discrete {
			v = math.Round(v)
		}
		out[name] = v
	}
	minDTE, maxDTE := int(number(out["min_dte"])), int(number(out["max_dte"]))
	if _, tunesMin := s.Bounds["min_dte"]; tunesMin {
		if b, tunesMax := s.Bounds["max_dte"]; tunesMax {
			idx := indexOf(names, "max_dte")
			lo := math.Max(float64(minDTE), b.Min)
			maxDTE = int(math.Round(lo + clamp01(gene[idx])*(b.Max-lo)))
		} else if maxDTE < minDTE {
			maxDTE = minDTE
		}
	} else if maxDTE < minDTE {
		maxDTE = minDTE
	}
	out["min_dte"], out["max_dte"] = minDTE, maxDTE
	if _, ok := out["trade_gap"]; ok {
		out["trade_gap"] = math.Round(number(out["trade_gap"]))
	}
	return out
}

func encode(s Space, names []string, params map[string]any) []float64 {
	g := make([]float64, len(names))
	for i, name := range names {
		b := s.Bounds[name]
		if b.Max > b.Min {
			g[i] = clamp01((number(params[name]) - b.Min) / (b.Max - b.Min))
		}
	}
	return g
}

func orderedNames(bounds map[string]Bound) []string {
	var out []string
	for _, n := range tacticalOrder {
		if _, ok := bounds[n]; ok {
			out = append(out, n)
		}
	}
	return out
}
func indexOf(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}
func randomGene(r *rand.Rand, n int) []float64 {
	g := make([]float64, n)
	for i := range g {
		g[i] = r.Float64()
	}
	return g
}
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func number(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}
func effectiveSeed(seed int64) int64 {
	if seed == 0 {
		return 42
	}
	return seed
}
func stats(in []float64) (mean, median, std float64) {
	cp := append([]float64(nil), in...)
	for _, v := range cp {
		mean += v
	}
	mean /= float64(len(cp))
	sort.Float64s(cp)
	if len(cp)%2 == 0 {
		median = (cp[len(cp)/2-1] + cp[len(cp)/2]) / 2
	} else {
		median = cp[len(cp)/2]
	}
	for _, v := range cp {
		std += (v - mean) * (v - mean)
	}
	std = math.Sqrt(std / float64(len(cp)))
	return
}
func geneDispersion(genes [][]float64) float64 {
	if len(genes) == 0 || len(genes[0]) == 0 {
		return 0
	}
	total := 0.0
	for j := range genes[0] {
		mean := 0.0
		for _, g := range genes {
			mean += g[j]
		}
		mean /= float64(len(genes))
		for _, g := range genes {
			d := g[j] - mean
			total += d * d
		}
	}
	return math.Sqrt(total / float64(len(genes)*len(genes[0])))
}

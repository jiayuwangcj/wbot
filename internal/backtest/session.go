package backtest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

// Session replays bars in one or more Process calls while keeping portfolio,
// strategy, and accumulator state across calls, so long windows execute chunk
// by chunk (backtestexec chunked path) without loading the whole window at
// once. RunOptionsWithFeeModel is the single-call case: one Process over the
// full window, byte-identical to the pre-session loop.
type Session struct {
	st       *State
	feeModel FeeModel
	seed     int64
	s        Strategy

	bars                int
	peak, maxDD         float64
	maxStockMarketValue float64
	curve               []EquityPoint
	trades              []Trade
	signals             []SignalTrace
	finalClose          float64
	firstBar, lastBar   time.Time

	dq dataQualityAccum
}

// NewSession creates a fresh backtest session. seed 0 selects the backtest
// default (42), matching OptionsData.RunSeed=0. Details: doc/BACKTEST.md
func NewSession(initialCash float64, feeModel FeeModel, seed int64, s Strategy) (*Session, error) {
	if initialCash <= 0 {
		return nil, errors.New("backtest: initial cash must be > 0")
	}
	if err := feeModel.validate(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("backtest: nil strategy")
	}
	if seed == 0 {
		seed = defaultRunSeed
	}
	return &Session{
		st:       &State{Cash: initialCash, Options: map[string]OptionPosition{}, OptPrice: map[string]float64{}},
		feeModel: feeModel,
		seed:     seed,
		s:        s,
		dq:       newDataQualityAccum(),
	}, nil
}

// Process replays one chunk of bars through the session, settling trades at
// each close. opts supplies the chunk's option universe: its quote batches are
// selected per bar and its price bars merge into the session's running OptBars
// so open legs keep their latest mark across chunk boundaries. ownedFrom
// limits which of the chunk's batches count toward DataQuality (the single-run
// path passes zero = count every batch; the chunked path passes each chunk's
// owned start so overlapping lookback regions are counted once). Empty chunks
// are a no-op, as a monthly window can legitimately contain no bars.
func (sess *Session) Process(ctx context.Context, bars []ingest.Bar, opts *OptionsData, ownedFrom time.Time) error {
	if len(bars) == 0 {
		return nil
	}
	if err := ingest.ValidateBars(bars); err != nil {
		return err
	}
	sess.applyOptions(opts)
	if sess.bars == 0 {
		sess.firstBar = bars[0].Ts
	}
	for i, b := range bars {
		if err := ctx.Err(); err != nil {
			return err
		}
		st := sess.st
		st.Pending = nil
		st.Price = b.Close
		// DATA_BLOCKED: this remains bar-time replay with the latest atomic
		// snapshot at or before the bar. Without a trusted event timeline we do
		// not claim event-driven historical execution semantics.
		st.QuoteBatch = latestQuoteBatch(opts, b.Ts)
		st.Quotes, st.ObservedAt, st.SnapshotKey = nil, time.Time{}, ""
		if st.QuoteBatch != nil {
			st.Quotes = st.QuoteBatch.Quotes
			st.ObservedAt, st.SnapshotKey = st.QuoteBatch.ObservedAt, st.QuoteBatch.SnapshotKey
		}
		markOptions(st, b.Ts)
		cashBefore := st.Cash
		act, size, err := sess.s.OnBar(ctx, b, st)
		if err != nil {
			return fmt.Errorf("backtest: bar %d: strategy: %w", sess.bars+i, err)
		}
		if err := settleAction(st, act, size, b, sess.feeModel, sess.seed, &sess.trades); err != nil {
			return fmt.Errorf("backtest: bar %d: %w", sess.bars+i, err)
		}
		sig := makeSignalTrace(b.Ts, sess.s, st, act, size, cashBefore)
		sess.signals = append(sess.signals, sig)
		settleExpired(st, b.Ts, sess.feeModel, &sess.trades)
		eq := st.Equity(b.Close)
		if stockMarketValue := math.Abs(st.Position * b.Close); stockMarketValue > sess.maxStockMarketValue {
			sess.maxStockMarketValue = stockMarketValue
		}
		sess.curve = append(sess.curve, EquityPoint{Ts: b.Ts, Equity: eq})
		if eq > sess.peak {
			sess.peak = eq
		}
		if sess.peak > 0 && (sess.peak-eq)/sess.peak > sess.maxDD {
			sess.maxDD = (sess.peak - eq) / sess.peak
		}
		sess.finalClose = b.Close
		sess.lastBar = b.Ts
		sess.bars++
		sess.dq.addBar(b)
		sess.dq.addSignal(sig)
	}
	sess.dq.addBatches(opts, ownedFrom)
	return nil
}

// Result builds the final backtest Result from the processed bars. bars and
// opts are the full-window inputs for the single-run path (DataQuality via the
// proven summarizeDataQuality); the chunked path passes nil to use the
// accumulator built across chunks.
func (sess *Session) Result(initialCash float64, bars []ingest.Bar, opts *OptionsData) (*Result, error) {
	if sess.bars == 0 {
		return nil, errors.New("backtest: no bars processed")
	}
	terminal := terminalSummary(sess.st, initialCash, sess.finalClose)
	final := sess.st.Equity(sess.finalClose)
	if terminal.FinalEquityAmount != nil {
		final = *terminal.FinalEquityAmount
	}
	unfilled := UnfilledStats{
		AttemptCount:  sess.st.AttemptCount,
		FillCount:     sess.st.FillCount,
		UnfilledCount: sess.st.UnfilledCount,
		ModelKind:     modelKind,
		ModelVersion:  modelVersion,
	}
	fees := summarizeFees(sess.trades, sess.feeModel)
	if sess.st.AttemptCount > 0 {
		ratio := float64(sess.st.UnfilledCount) / float64(sess.st.AttemptCount)
		unfilled.UnfilledRatio = &ratio
	}
	attr := attributionOf(sess.st, fees, unfilled)
	dq := sess.dq.summary()
	if bars != nil {
		dq = summarizeDataQuality(bars, opts, sess.signals)
	}
	return &Result{
		Equity:                    final,
		TotalReturn:               (final - initialCash) / initialCash,
		RealizedReturnAmount:      attr.RealizedPnLAmount,
		RealizedReturnPct:         attr.RealizedPnLAmount / initialCash,
		MaxDrawdown:               sess.maxDD,
		MaxStockMarketValueAmount: sess.maxStockMarketValue,
		Bars:                      sess.bars,
		EquityCurve:               sess.curve,
		Trades:                    sess.trades,
		Signals:                   sess.signals,
		Unfilled:                  unfilled,
		Fees:                      fees,
		Attribution:               attr,
		Terminal:                  terminal,
		DataQuality:               dq,
	}, nil
}

// FirstBar reports the timestamp of the first processed bar.
func (sess *Session) FirstBar() time.Time { return sess.firstBar }

// LastBar reports the timestamp of the last processed bar.
func (sess *Session) LastBar() time.Time { return sess.lastBar }

// Close reports the close of the last processed bar.
func (sess *Session) Close() float64 { return sess.finalClose }

// BarCount reports the total number of processed bars.
func (sess *Session) BarCount() int { return sess.bars }

// applyOptions sets the chunk's chain and merges its option price bars into
// the running history. Overlapping bars (Ts <= the last already stored per
// code) are dropped, so OptBars stays ascending and each code's latest mark is
// available to markOptions after a chunk boundary.
func (sess *Session) applyOptions(opts *OptionsData) {
	if opts == nil {
		return
	}
	sess.st.Chain = opts.Chain
	if sess.st.OptBars == nil {
		sess.st.OptBars = opts.Bars
		return
	}
	for code, newBars := range opts.Bars {
		existing := sess.st.OptBars[code]
		if len(existing) == 0 {
			sess.st.OptBars[code] = newBars
			continue
		}
		last := existing[len(existing)-1].Ts
		i := 0
		for i < len(newBars) && !newBars[i].Ts.After(last) {
			i++
		}
		if i == len(newBars) {
			continue
		}
		merged := make([]ingest.Bar, 0, len(existing)+len(newBars)-i)
		merged = append(merged, existing...)
		merged = append(merged, newBars[i:]...)
		sess.st.OptBars[code] = merged
	}
}

// dataQualityAccum mirrors summarizeDataQuality while tolerating chunked
// inputs: bars/signals arrive per chunk, and quote batches overlap between
// chunks, so batches are counted once by (observed_at, snapshot_key) and only
// inside each chunk's owned range. summary() then emits the same
// DataQualitySummary shape as the single-run summarizeDataQuality.
type dataQualityAccum struct {
	barCounts      map[barSourceKey]int
	totalBars      int
	readyBars      int
	blockedBars    int
	blockedReasons map[string]struct{}
	snapshot       dataQualitySnapshotAccum
}

type barSourceKey struct{ source, adjusted string }

type dataQualitySnapshotAccum struct {
	optsProvided     bool
	present          bool
	batchCount       int
	contractRowCount int
	sources          map[string]struct{}
	missing          map[string]int64
	hkex             map[string]hkexCoverage
	seenBatches      map[quoteBatchKey]struct{}
}

type hkexCoverage struct{ first, last, expiry time.Time }

type quoteBatchKey struct {
	observed int64
	key      string
}

func newDataQualityAccum() dataQualityAccum {
	counts := make(map[string]int64, len(requiredSnapshotFields))
	for _, field := range requiredSnapshotFields {
		counts[field] = 0
	}
	return dataQualityAccum{
		barCounts:      map[barSourceKey]int{},
		blockedReasons: map[string]struct{}{},
		snapshot: dataQualitySnapshotAccum{
			sources:     map[string]struct{}{},
			missing:     counts,
			hkex:        map[string]hkexCoverage{},
			seenBatches: map[quoteBatchKey]struct{}{},
		},
	}
}

func (a *dataQualityAccum) addBar(b ingest.Bar) {
	source := strings.TrimSpace(b.Source)
	if source == "" {
		return
	}
	adjusted := strings.TrimSpace(b.Adjusted)
	if adjusted == "" {
		adjusted = "unspecified"
	}
	a.barCounts[barSourceKey{source: source, adjusted: adjusted}]++
}

func (a *dataQualityAccum) addSignal(s SignalTrace) {
	a.totalBars++
	if strings.EqualFold(s.CapabilityStatus, wheel.CapabilityDataBlocked) {
		a.blockedBars++
	} else {
		a.readyBars++
	}
	for _, reason := range s.BlockedBy {
		if reason != "" {
			a.blockedReasons[reason] = struct{}{}
		}
	}
}

func (a *dataQualityAccum) addBatches(opts *OptionsData, ownedFrom time.Time) {
	if opts == nil {
		return
	}
	a.snapshot.optsProvided = true
	for _, batch := range optionQuoteBatches(opts) {
		if !ownedFrom.IsZero() && batch.ObservedAt.Before(ownedFrom) {
			continue
		}
		key := quoteBatchKey{observed: batch.ObservedAt.UTC().UnixNano(), key: batch.SnapshotKey}
		if _, seen := a.snapshot.seenBatches[key]; seen {
			continue
		}
		a.snapshot.seenBatches[key] = struct{}{}
		a.snapshot.present = true
		a.snapshot.batchCount++
		observed := utcDate(batch.ObservedAt)
		for _, quote := range batch.Quotes {
			a.snapshot.contractRowCount++
			if source := strings.TrimSpace(quote.Source); source != "" {
				a.snapshot.sources[source] = struct{}{}
			}
			countMissingSnapshotFields(a.snapshot.missing, batch, quote)
			if observed.IsZero() || !strings.EqualFold(strings.TrimSpace(quote.Source), "hkex") || quote.Expiry.IsZero() {
				continue
			}
			name := firstQuoteName(quote)
			if name == "" {
				continue
			}
			c := a.snapshot.hkex[name]
			if c.first.IsZero() || observed.Before(c.first) {
				c.first = observed
			}
			if c.last.IsZero() || observed.After(c.last) {
				c.last = observed
			}
			c.expiry = utcDate(quote.Expiry)
			a.snapshot.hkex[name] = c
		}
	}
}

func (a *dataQualityAccum) summary() DataQualitySummary {
	q := DataQualitySummary{
		Status:                     "NOT_APPLICABLE",
		OptionDataRequired:         a.snapshot.optsProvided,
		UnderlyingBars:             summarizeBarProvenanceFromMap(a.barCounts),
		OptionSnapshotSources:      []string{},
		TotalBarCount:              a.totalBars,
		ReadyBarCount:              a.readyBars,
		BlockedBarCount:            a.blockedBars,
		MissingRequiredFieldCounts: a.snapshot.missing,
	}
	if a.totalBars > 0 {
		ratio := float64(a.readyBars) / float64(a.totalBars)
		q.ValidCoverageRatio = &ratio
	}
	if !a.snapshot.optsProvided {
		return q
	}
	cycleComplete := a.snapshot.hkexComplete()
	q.HistoricalOptionCycleComplete = &cycleComplete
	q.Status = wheel.CapabilityDataBlocked
	q.SnapshotBatchCount = a.snapshot.batchCount
	q.SnapshotContractRowCount = a.snapshot.contractRowCount
	blocked := map[string]struct{}{}
	if !cycleComplete {
		blocked["historical_option_cycle"] = struct{}{}
	}
	if a.snapshot.batchCount == 0 {
		blocked["option_quote_snapshots"] = struct{}{}
	}
	sources := make([]string, 0, len(a.snapshot.sources))
	for s := range a.snapshot.sources {
		sources = append(sources, s)
	}
	q.OptionSnapshotSources = sortedUnique(sources)
	for field, count := range a.snapshot.missing {
		if count > 0 {
			blocked["missing_"+field] = struct{}{}
		}
	}
	for _, reason := range sortedKeys(a.blockedReasons) {
		if reason != "" {
			blocked[reason] = struct{}{}
		}
	}
	if cycleComplete && q.ReadyBarCount > 0 {
		// HKEX EOD settlement marks unlock deterministic research returns, not
		// live/executable READY semantics. Gaps remain visible in BlockedBy and
		// ValidCoverageRatio without nulling the entire research window.
		q.Status = "RESEARCH_ONLY"
	}
	q.BlockedBy = sortedKeys(blocked)
	return q
}

func (s *dataQualitySnapshotAccum) hkexComplete() bool {
	for _, c := range s.hkex {
		lead := int(c.expiry.Sub(c.first) / (24 * time.Hour))
		tail := int(c.expiry.Sub(c.last) / (24 * time.Hour))
		if lead >= 10 && tail >= 0 && tail <= 1 {
			return true
		}
	}
	return false
}

func summarizeBarProvenanceFromMap(counts map[barSourceKey]int) []BarProvenance {
	out := make([]BarProvenance, 0, len(counts))
	for k, count := range counts {
		out = append(out, BarProvenance{Source: k.source, Adjusted: k.adjusted, BarCount: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Adjusted < out[j].Adjusted
	})
	return out
}

package backtest

import (
	"time"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

// OptionKind distinguishes call and put legs.
type OptionKind string

const (
	OptionCall OptionKind = "call"
	OptionPut  OptionKind = "put"
)

// OptionContract is one tradable contract's static data (chain entry).
type OptionContract struct {
	Code   string
	Kind   OptionKind
	Strike float64
	Expiry time.Time
}

// OptionPosition is one open option leg; Contracts > 0 = long, < 0 = short.
// AvgPremium is the market price per underlying share; cash settlement and
// mark-to-market both multiply it by Lot.
type OptionPosition struct {
	Code       string
	Kind       OptionKind
	Strike     float64
	Expiry     time.Time
	Lot        int
	Contracts  float64
	AvgPremium float64
	// MarketDelta is the last trusted snapshot delta for this leg. It is
	// optional for the legacy mechanical runner, but WheelStrategy will not
	// invent it from an option close.
	MarketDelta float64
	// Delta is an alternate field name accepted by adapters building typed
	// state; MarketDelta remains the canonical backtest field.
	Delta float64
	// LotSize mirrors wheel.OptionPosition for snapshot-backed callers. Lot is
	// retained for the original mechanical runner API.
	LotSize int
}

// OptionChain maps a contract code to its static data (runner-injected).
type OptionChain map[string]OptionContract

// OptionBars maps a contract code to its bars, ts ascending (runner-injected).
type OptionBars map[string][]ingest.Bar

// OptionsData is the runner-injected option universe: chain + per-code bars.
// RunSeed seeds the unfilled-attempt draws for sell actions (unfilled.go);
// 0 means defaultRunSeed (42), so runs stay deterministic by default.
type OptionsData struct {
	Bars    OptionBars
	Chain   OptionChain
	RunSeed int64
	// QuoteBatches are immutable, atomic Wheel quote observations. A batch is
	// selected by observed_at + snapshot_key; quotes from different batches
	// are never combined.
	QuoteBatches []QuoteSnapshotBatch
	// Snapshots is an alias accepted by callers constructing OptionsData. New
	// code should prefer QuoteBatches.
	Snapshots []QuoteSnapshotBatch
	// QuoteSnapshots is a descriptive alias for integrations that use the
	// persistence vocabulary directly.
	QuoteSnapshots []QuoteSnapshotBatch
}

// QuoteSnapshotBatch is one atomic set of trusted option quotes. The
// underlying price is kept separately because it is common to all contracts
// in the batch and is required to make a Wheel decision.
type QuoteSnapshotBatch struct {
	ObservedAt      time.Time
	SnapshotKey     string
	Underlying      string
	UnderlyingPrice float64
	Quotes          []wheel.OptionQuote
}

// OptionQuoteBatch is a concise compatibility name for QuoteSnapshotBatch.
type OptionQuoteBatch = QuoteSnapshotBatch

// State is a backtest's portfolio state; Run updates Price to each bar's close
// before OnBar, fills OptPrice from open legs, and clears Pending per bar.
type State struct {
	Cash             float64
	Position         float64
	StockAverageCost float64
	Price            float64
	Options          map[string]OptionPosition
	Chain            OptionChain
	OptBars          OptionBars
	OptPrice         map[string]float64
	// Pending is the contract a strategy picked for an option action on the
	// current bar; the runner settles size contracts against it and clears it.
	Pending *OptionPosition
	// QuoteBatch is the one atomic snapshot selected for the current bar.
	// It is nil when no trusted snapshot exists at or before the bar.
	QuoteBatch *QuoteSnapshotBatch
	// Quotes, ObservedAt and SnapshotKey mirror QuoteBatch for adapters that
	// consume state without depending on the batch wrapper.
	Quotes      []wheel.OptionQuote
	ObservedAt  time.Time
	SnapshotKey string
	DailyOrders int
	ExtremeDay  bool
	// Fill accounting (unfilled.go): AttemptCount counts every sell attempt
	// that reaches settlement sampling, FillCount fills, UnfilledCount the
	// rest. Buys, HOLD and DATA_BLOCKED never increment these.
	AttemptCount  int64
	FillCount     int64
	UnfilledCount int64
	// AttemptsByContract numbers sell attempts per contract code (p.Code);
	// the per-contract sequence is the attempt_index passed to the unfilled
	// draw, so a new candidate elsewhere in the run never shifts existing
	// contracts' draws (lazy-initialized on first attempt).
	AttemptsByContract map[string]int64
	// Mechanical expiry accounting is deliberately separate from broker facts.
	// AssignmentCount counts ITM expiries of short legs only; long-leg exercise
	// is an exercise event, not an assignment.
	ExpiryCount      int64
	ShortExpiryCount int64
	AssignmentCount  int64
}

// Equity returns total portfolio value: cash + position at price plus option
// legs marked to their latest option close (OptPrice; 0 when unknown).
func (s *State) Equity(price float64) float64 {
	eq := s.Cash + s.Position*price
	for _, p := range s.Options {
		eq += p.Contracts * float64(p.Lot) * s.OptPrice[p.Code]
	}
	return eq
}

// PriceAt returns code's latest option close at or before ts (false if none).
func (s *State) PriceAt(code string, ts time.Time) (float64, bool) {
	bars := s.OptBars[code]
	best := -1
	for i, b := range bars {
		if b.Ts.After(ts) {
			break
		}
		best = i
	}
	if best < 0 {
		return 0, false
	}
	return bars[best].Close, true
}

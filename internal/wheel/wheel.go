// Package wheel contains the side-effect free domain model for the dynamic
// wheel strategy.  It deliberately has no broker or persistence dependency:
// callers provide a quote snapshot and receive an alert (or a hold).
package wheel

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	StateNormal            = "NORMAL"
	StateCaution           = "CAUTION"
	StatePauseBuy          = "PAUSE_BUY"
	StateExit              = "EXIT"
	StrategicStateNormal   = StateNormal
	StrategicStateCaution  = StateCaution
	StrategicStatePauseBuy = StatePauseBuy
	StrategicStateExit     = StateExit

	NORMAL    = StateNormal
	CAUTION   = StateCaution
	PAUSEBUY  = StatePauseBuy
	PAUSE_BUY = StatePauseBuy
	EXIT      = StateExit

	DirectionPut  = "PUT"
	DirectionCall = "CALL"
	DirectionHold = "HOLD"

	PUT  = DirectionPut
	CALL = DirectionCall
	HOLD = DirectionHold

	ActionAlert               = "ALERT"
	ActionHold                = "HOLD"
	ALERT                     = ActionAlert
	CapabilityReady           = "READY"
	CapabilityDataBlocked     = "DATA_BLOCKED"
	defaultMaxQuoteAgeSeconds = 24 * 60 * 60
	// DefaultLotSize is the market-standard contract multiplier used only as a
	// fallback for position-derived inventory math; candidate quotes carry
	// their live contract_size and are never allowed to fall back (a missing
	// lot on a quote is a data defect and DATA_BLOCKs the candidate).
	DefaultLotSize = 100
)

// candidateLotSize returns the contract multiplier of the first quoted
// candidate that carries one. All contracts of a family share the same lot
// size, so the first valid quote is representative; DefaultLotSize is the
// market default when no candidate quotes a size.
func candidateLotSize(quotes []OptionQuote) int {
	for _, q := range quotes {
		if q.LotSize > 0 {
			return q.LotSize
		}
	}
	return DefaultLotSize
}

// QuoteMaxAge returns the configured freshness window. Keeping the default in
// one place lets loaders include a still-fresh snapshot just before a bounded
// replay's first bar without widening the decision-time freshness rule.
func (c Config) QuoteMaxAge() time.Duration {
	seconds := c.MaxQuoteAgeSeconds
	if seconds <= 0 {
		seconds = defaultMaxQuoteAgeSeconds
	}
	return time.Duration(seconds) * time.Second
}

// The aliases intentionally remain aliases of string. This makes JSON/YAML
// configuration pleasant and does not require callers to cast string values.
type State = string
type StrategicState = string
type Direction = string
type Action = string
type OptionType = string

const (
	Put  OptionType = "put"
	Call OptionType = "call"
)

// PricePoint is one anchor of the user-defined price-to-target-inventory
// curve. Prices must increase and target inventory must not increase.
type PricePoint struct {
	Price           float64 `json:"price" yaml:"price"`
	TargetInventory float64 `json:"target_inventory" yaml:"target_inventory"`
}

// Config is the complete, structured wheel configuration.
// LotSize is deliberately absent: the contract multiplier comes from the
// live quote's contract_size, with a 100-share market default as fallback
// (2026-08-13: 去除可直接从行情拉取的配置参数).
type Config struct {
	Strategy              string       `json:"strategy" yaml:"strategy"`
	PricePositionCurve    []PricePoint `json:"price_position_curve" yaml:"price_position_curve"`
	MaxInventory          float64      `json:"max_inventory" yaml:"max_inventory"`
	MinDTE                int          `json:"min_dte" yaml:"min_dte"`
	MaxDTE                int          `json:"max_dte" yaml:"max_dte"`
	MinOptionQuality      float64      `json:"min_option_quality" yaml:"min_option_quality"`
	MaxDailyOrders        int          `json:"max_daily_orders" yaml:"max_daily_orders"`
	ExtremeMaxDailyOrders int          `json:"extreme_max_daily_orders" yaml:"extreme_max_daily_orders"`
	NoTradeGap            float64      `json:"no_trade_gap" yaml:"no_trade_gap"`
	StrategicState        string       `json:"strategic_state" yaml:"strategic_state"`
	// MaxQuoteAgeSeconds is required freshness in seconds. Zero uses the
	// conservative one-day default so a quote can never be timeless.
	MaxQuoteAgeSeconds int `json:"max_quote_age_seconds,omitempty" yaml:"max_quote_age_seconds,omitempty"`
}

func (c Config) Validate() error {
	if c.Strategy != "wheel" {
		return fmt.Errorf("wheel: strategy must be wheel")
	}
	if len(c.PricePositionCurve) < 2 {
		return fmt.Errorf("wheel: price_position_curve requires at least two points")
	}
	if !finite(c.MaxInventory) || c.MaxInventory <= 0 {
		return fmt.Errorf("wheel: max_inventory must be positive")
	}
	var prev PricePoint
	for i, p := range c.PricePositionCurve {
		if !finite(p.Price) || p.Price <= 0 || !finite(p.TargetInventory) {
			return fmt.Errorf("wheel: invalid curve point %d", i)
		}
		if p.TargetInventory < 0 || p.TargetInventory > c.MaxInventory {
			return fmt.Errorf("wheel: target inventory out of bounds at point %d", i)
		}
		if i > 0 && (p.Price <= prev.Price || p.TargetInventory > prev.TargetInventory) {
			return fmt.Errorf("wheel: curve must have increasing prices and non-increasing targets")
		}
		prev = p
	}
	if c.MinDTE < 5 || c.MaxDTE > 10 || c.MinDTE > c.MaxDTE {
		return fmt.Errorf("wheel: DTE must be a valid range within 5..10")
	}
	if !finite(c.MinOptionQuality) || c.MinOptionQuality < 0 || c.MinOptionQuality > 1 {
		return fmt.Errorf("wheel: min_option_quality must be in [0,1]")
	}
	if c.MaxDailyOrders < 1 || c.MaxDailyOrders > 1 || c.ExtremeMaxDailyOrders < 1 || c.ExtremeMaxDailyOrders > 2 || c.ExtremeMaxDailyOrders < c.MaxDailyOrders {
		return fmt.Errorf("wheel: daily order limits exceed hard limits")
	}
	if !finite(c.NoTradeGap) || c.NoTradeGap < 0 {
		return fmt.Errorf("wheel: no_trade_gap must be non-negative")
	}
	switch c.StrategicState {
	case StateNormal, StateCaution, StatePauseBuy, StateExit:
	default:
		return fmt.Errorf("wheel: unknown strategic state %q", c.StrategicState)
	}
	return nil
}

// ValidateConfig is the functional form used by decoders and HTTP adapters.
func ValidateConfig(c Config) error { return c.Validate() }

func (c Config) TargetInventory(price float64) (float64, error) {
	return InterpolateTargetInventory(c.PricePositionCurve, price)
}

func (c Config) Interpolate(price float64) (float64, error) { return c.TargetInventory(price) }

// InterpolateTargetInventory linearly interpolates between anchors and clamps
// outside the configured range to the nearest endpoint.
func InterpolateTargetInventory(curve []PricePoint, price float64) (float64, error) {
	if len(curve) < 2 || !finite(price) {
		return 0, errors.New("wheel: invalid price curve or current price")
	}
	for i, p := range curve {
		if !finite(p.Price) || p.Price <= 0 || !finite(p.TargetInventory) || p.TargetInventory < 0 {
			return 0, errors.New("wheel: invalid price curve")
		}
		if i == 0 {
			continue
		}
		if p.Price <= curve[i-1].Price || p.TargetInventory > curve[i-1].TargetInventory {
			return 0, errors.New("wheel: invalid price curve")
		}
	}
	if price <= curve[0].Price {
		return curve[0].TargetInventory, nil
	}
	last := curve[len(curve)-1]
	if price >= last.Price {
		return last.TargetInventory, nil
	}
	for i := 1; i < len(curve); i++ {
		if price <= curve[i].Price {
			a, b := curve[i-1], curve[i]
			ratio := (price - a.Price) / (b.Price - a.Price)
			return a.TargetInventory + ratio*(b.TargetInventory-a.TargetInventory), nil
		}
	}
	return last.TargetInventory, nil // unreachable, retained for defensive safety
}

// OptionPosition uses signed contracts (long positive, short negative) and
// standard market delta. A short put therefore contributes positive stock
// delta, while a short call contributes negative stock delta.
type OptionPosition struct {
	Symbol          string     `json:"symbol"`
	SignedContracts float64    `json:"signed_contracts"`
	Contracts       float64    `json:"contracts,omitempty"`
	Strike          float64    `json:"strike,omitempty"`
	MarketDelta     float64    `json:"market_delta"`
	Delta           float64    `json:"delta,omitempty"`
	LotSize         int        `json:"lot_size,omitempty"`
	OptionType      OptionType `json:"option_type,omitempty"`
}

type Position = OptionPosition

func ActualInventory(stockShares, futuresEquivalentShares float64) float64 {
	return stockShares + futuresEquivalentShares
}

func OptionDeltaStock(positions []OptionPosition, defaultLotSize int) float64 {
	var result float64
	for _, p := range positions {
		contracts := p.SignedContracts
		if contracts == 0 {
			contracts = p.Contracts
		}
		delta := p.MarketDelta
		if delta == 0 && p.Delta != 0 {
			delta = p.Delta
		}
		lot := p.LotSize
		if lot <= 0 {
			lot = defaultLotSize
		}
		result += contracts * delta * float64(lot)
	}
	return result
}

func OptionDelta(positions []OptionPosition, defaultLotSize int) float64 {
	return OptionDeltaStock(positions, defaultLotSize)
}

func OptionDeltaInventory(positions []OptionPosition, defaultLotSize int) float64 {
	return OptionDeltaStock(positions, defaultLotSize)
}

func EffectiveInventory(actual, optionDelta float64) float64 { return actual + optionDelta }

type InventorySnapshot struct {
	StockShares             float64 `json:"stock_shares"`
	FuturesEquivalentShares float64 `json:"futures_equivalent_shares"`
	OptionDeltaStock        float64 `json:"option_delta_stock"`
	ActualInventory         float64 `json:"actual_inventory"`
	EffectiveInventory      float64 `json:"effective_inventory"`
}

type Inventory = InventorySnapshot

func CalculateInventory(stockShares, futuresEquivalentShares float64, positions []OptionPosition, lotSize int) InventorySnapshot {
	actual := ActualInventory(stockShares, futuresEquivalentShares)
	delta := OptionDeltaStock(positions, lotSize)
	return InventorySnapshot{StockShares: stockShares, FuturesEquivalentShares: futuresEquivalentShares,
		OptionDeltaStock: delta, ActualInventory: actual, EffectiveInventory: EffectiveInventory(actual, delta)}
}

// OptionQuote is a real-time quote snapshot. Bid/ask, IV, Theta, delta,
// volume and OI are mandatory for an actionable reminder; a daily close is
// not a substitute. Theta is a pointer because zero can be a valid observed
// value and must remain distinguishable from a missing provider field. Last
// is the latest trade and the estimated limit-price anchor when available.
type OptionQuote struct {
	Symbol       string     `json:"symbol"`
	Code         string     `json:"code,omitempty"`
	Underlying   string     `json:"underlying,omitempty"`
	Source       string     `json:"source"`
	OptionType   OptionType `json:"option_type"`
	Type         OptionType `json:"type,omitempty"`
	Expiry       time.Time  `json:"expiry"`
	Strike       float64    `json:"strike"`
	Delta        float64    `json:"delta"`
	MarketDelta  float64    `json:"market_delta,omitempty"`
	Bid          float64    `json:"bid"`
	Ask          float64    `json:"ask"`
	Last         float64    `json:"last,omitempty"`
	ImpliedVol   float64    `json:"implied_vol"`
	Theta        *float64   `json:"theta"`
	Volume       int64      `json:"volume"`
	OpenInterest int64      `json:"open_interest"`
	LotSize      int        `json:"lot_size"`
	QuoteTime    time.Time  `json:"quote_time"`
	CapturedAt   time.Time  `json:"captured_at,omitempty"`
	Timestamp    time.Time  `json:"timestamp,omitempty"`
	Ts           time.Time  `json:"ts,omitempty"`
	IV           float64    `json:"iv,omitempty"`
	OI           int64      `json:"oi,omitempty"`
}

func (q OptionQuote) name() string {
	if q.Symbol != "" {
		return q.Symbol
	}
	return q.Code
}
func (q OptionQuote) optionType() OptionType {
	if q.OptionType != "" {
		return strings.ToLower(string(q.OptionType))
	}
	return strings.ToLower(string(q.Type))
}
func (q OptionQuote) delta() float64 {
	if q.Delta != 0 {
		return q.Delta
	}
	return q.MarketDelta
}
func (q OptionQuote) quoteTime() time.Time {
	if !q.QuoteTime.IsZero() {
		return q.QuoteTime
	}
	if !q.CapturedAt.IsZero() {
		return q.CapturedAt
	}
	if !q.Timestamp.IsZero() {
		return q.Timestamp
	}
	return q.Ts
}
func (q OptionQuote) impliedVol() float64 {
	if q.ImpliedVol != 0 {
		return q.ImpliedVol
	}
	return q.IV
}
func (q OptionQuote) openInterest() int64 {
	if q.OpenInterest != 0 {
		return q.OpenInterest
	}
	return q.OI
}
func (q OptionQuote) Mid() float64    { return (q.Bid + q.Ask) / 2 }
func (q OptionQuote) Spread() float64 { return q.Ask - q.Bid }

// Validate verifies all fields required to consider a quote candidate.
func (q OptionQuote) Validate(asOf time.Time, cfg Config) error {
	if q.name() == "" || strings.TrimSpace(q.Source) == "" || q.Expiry.IsZero() || !finite(q.Strike) || q.Strike <= 0 || !finite(q.delta()) || q.delta() < -1 || q.delta() > 1 {
		return errors.New("wheel: quote missing symbol, source, expiry, strike or delta")
	}
	typ := q.optionType()
	if typ != string(Put) && typ != string(Call) {
		return errors.New("wheel: quote has invalid option type")
	}
	if typ == string(Put) && q.delta() >= 0 {
		return errors.New("wheel: put delta must be negative")
	}
	if typ == string(Call) && q.delta() <= 0 {
		return errors.New("wheel: call delta must be positive")
	}
	if !finite(q.Bid) || !finite(q.Ask) || q.Bid <= 0 || q.Ask <= 0 || q.Ask < q.Bid {
		return errors.New("wheel: quote has missing or inverted bid/ask")
	}
	if !finite(q.impliedVol()) || q.impliedVol() <= 0 || q.Theta == nil || !finite(*q.Theta) || q.Volume <= 0 || q.openInterest() <= 0 || q.LotSize <= 0 {
		return errors.New("wheel: quote missing IV, Theta, liquidity or lot size")
	}
	qt := q.quoteTime()
	if !asOf.IsZero() && qt.IsZero() {
		return errors.New("wheel: quote timestamp is missing")
	}
	if !asOf.IsZero() && qt.After(asOf) {
		return errors.New("wheel: quote is from the future")
	}
	if !asOf.IsZero() && asOf.Sub(qt) > cfg.QuoteMaxAge() {
		return errors.New("wheel: quote is stale")
	}
	if !asOf.IsZero() && !q.Expiry.After(asOf) {
		return errors.New("wheel: option has expired")
	}
	if !asOf.IsZero() {
		asOfDate := time.Date(asOf.UTC().Year(), asOf.UTC().Month(), asOf.UTC().Day(), 0, 0, 0, 0, time.UTC)
		expiryDate := time.Date(q.Expiry.UTC().Year(), q.Expiry.UTC().Month(), q.Expiry.UTC().Day(), 0, 0, 0, 0, time.UTC)
		dte := int(expiryDate.Sub(asOfDate) / (24 * time.Hour))
		if dte < cfg.MinDTE || dte > cfg.MaxDTE {
			return fmt.Errorf("wheel: DTE %d outside [%d,%d]", dte, cfg.MinDTE, cfg.MaxDTE)
		}
	}
	return nil
}

func ValidateQuote(q OptionQuote, asOf time.Time, cfg Config) error { return q.Validate(asOf, cfg) }

// QualityScore returns a deterministic [0,1] score from spread, volume, OI,
// premium/strike, IV, and absolute Theta. Delta is accounted for separately
// by the primary post-trade inventory-distance ranking. Every component is
// bounded and contributes equally.
func QualityScore(q OptionQuote) float64 {
	if q.Bid <= 0 || q.Ask < q.Bid || q.Strike <= 0 || !finite(q.delta()) || !finite(q.impliedVol()) || q.impliedVol() <= 0 || q.Theta == nil || !finite(*q.Theta) || q.Volume <= 0 || q.openInterest() <= 0 {
		return 0
	}
	mid := q.Mid()
	spread := clamp01(1 - q.Spread()/mid)
	volume := q.VolumeFScore()
	oi := q.OIFScore()
	premium := clamp01(mid / q.Strike * 10)
	iv := q.impliedVol() / (q.impliedVol() + 0.30)
	theta := math.Abs(*q.Theta) / (math.Abs(*q.Theta) + 0.05)
	return clamp01((spread + volume + oi + premium + iv + theta) / 6)
}

func QuoteQuality(q OptionQuote) float64    { return QualityScore(q) }
func (q OptionQuote) QualityScore() float64 { return QualityScore(q) }

func (q OptionQuote) VolumeFScore() float64 { return float64(q.Volume) / (float64(q.Volume) + 100) }
func (q OptionQuote) OIFScore() float64 {
	oi := q.openInterest()
	return float64(oi) / (float64(oi) + 1000)
}

func (p OptionPosition) optionType() OptionType {
	return strings.ToLower(string(p.OptionType))
}

// CoveredShares returns shares already committed to outstanding short calls.
// A new call may only consume the uncommitted portion of actual inventory.
func CoveredShares(positions []OptionPosition, lotSize int) float64 {
	var committed float64
	for _, p := range positions {
		if p.optionType() != string(Call) {
			continue
		}
		contracts := p.SignedContracts
		if contracts == 0 {
			contracts = p.Contracts
		}
		if contracts < 0 {
			lot := p.LotSize
			if lot <= 0 {
				lot = lotSize
			}
			committed += -contracts * float64(lot)
		}
	}
	return committed
}

// PutAssignmentShares returns shares already committed by outstanding short
// puts. Max-inventory checks must include these commitments, not only the new
// candidate, or repeated alerts could exceed the configured hard limit.
func PutAssignmentShares(positions []OptionPosition, lotSize int) float64 {
	var committed float64
	for _, p := range positions {
		if p.optionType() != string(Put) {
			continue
		}
		contracts := p.SignedContracts
		if contracts == 0 {
			contracts = p.Contracts
		}
		if contracts < 0 {
			lot := p.LotSize
			if lot <= 0 {
				lot = lotSize
			}
			committed += -contracts * float64(lot)
		}
	}
	return committed
}

// PutAssignmentCash returns the gross cash already committed by outstanding
// short puts. Cash-secured checks must reserve every open assignment, not only
// the newly proposed contract.
func PutAssignmentCash(positions []OptionPosition, lotSize int) float64 {
	var committed float64
	for _, p := range positions {
		if p.optionType() != string(Put) || p.Strike <= 0 {
			continue
		}
		contracts := p.SignedContracts
		if contracts == 0 {
			contracts = p.Contracts
		}
		if contracts < 0 {
			lot := p.LotSize
			if lot <= 0 {
				lot = lotSize
			}
			committed += -contracts * p.Strike * float64(lot)
		}
	}
	return committed
}

type DecisionInput struct {
	CurrentPrice            float64
	AsOf                    time.Time
	StockShares             float64
	FuturesEquivalentShares float64
	Positions               []OptionPosition
	Quotes                  []OptionQuote
	DailyOrders             int
	ExtremeDay              bool
	CashAvailable           float64
	HasCashAvailable        bool
}

type CandidateEvaluation struct {
	Quote               OptionQuote `json:"quote"`
	Direction           Direction   `json:"direction"`
	Quantity            int         `json:"quantity"`
	SignedContracts     int         `json:"signed_contracts"`
	Quality             float64     `json:"quality"`
	PostTradeEffective  float64     `json:"post_trade_effective_inventory"`
	AssignmentInventory float64     `json:"assignment_inventory"`
	Accepted            bool        `json:"accepted"`
	Reasons             []string    `json:"reasons,omitempty"`
}

type Signal struct {
	Action          Action    `json:"action"`
	Direction       Direction `json:"direction"`
	Quantity        int       `json:"quantity"`
	SignedContracts int       `json:"signed_contracts"`
	// ExpectedGain is the conservative gross premium estimate for the
	// proposed short-option alert: executable bid * lot size * quantity.
	// It excludes fees, slippage, assignment losses and tax, and remains zero
	// when the required market data is unavailable.
	ExpectedGain        float64               `json:"expected_gain,omitempty"`
	Quote               *OptionQuote          `json:"quote,omitempty"`
	Quality             float64               `json:"quality"`
	TargetInventory     float64               `json:"target_inventory"`
	InventoryGap        float64               `json:"inventory_gap"`
	ActualInventory     float64               `json:"actual_inventory"`
	OptionDeltaStock    float64               `json:"option_delta_stock"`
	EffectiveInventory  float64               `json:"effective_inventory"`
	PostTradeEffective  float64               `json:"post_trade_effective_inventory"`
	AssignmentInventory float64               `json:"assignment_inventory"`
	Reason              string                `json:"reason"`
	Reasons             []string              `json:"reasons"`
	RejectReasons       []string              `json:"reject_reasons,omitempty"`
	Candidates          []CandidateEvaluation `json:"candidates,omitempty"`
	CapabilityStatus    string                `json:"capability_status"`
	BlockedBy           []string              `json:"blocked_by,omitempty"`
}

type Decision = Signal

// Engine binds one validated configuration to the pure evaluator. It carries
// no broker client and therefore cannot place orders by construction.
type Engine struct{ Config Config }

func NewEngine(cfg Config) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Engine{Config: cfg}, nil
}

func (e Engine) Evaluate(in DecisionInput) (Signal, error) { return Evaluate(e.Config, in) }

func hold(reason string, base Signal) Signal {
	if base.CapabilityStatus == "" {
		base.CapabilityStatus = CapabilityReady
	}
	base.Action, base.Direction, base.Quantity, base.Reason = ActionHold, DirectionHold, 0, reason
	base.Reasons = append(base.Reasons, reason)
	return base
}

// Evaluate performs a pure, deterministic decision. It never calls a trading
// API and has only two possible output actions: ALERT and HOLD.
func Evaluate(cfg Config, in DecisionInput) (Signal, error) {
	if err := cfg.Validate(); err != nil {
		return Signal{Action: ActionHold, Direction: DirectionHold, Reason: err.Error(), Reasons: []string{err.Error()}, CapabilityStatus: CapabilityDataBlocked, BlockedBy: []string{"config"}}, err
	}
	if !finite(in.CurrentPrice) || in.CurrentPrice <= 0 {
		return Signal{Action: ActionHold, Direction: DirectionHold, Reason: "wheel: current price is invalid", Reasons: []string{"wheel: current price is invalid"}, CapabilityStatus: CapabilityDataBlocked, BlockedBy: []string{"current_price"}}, nil
	}
	target, err := cfg.TargetInventory(in.CurrentPrice)
	if err != nil {
		return Signal{Action: ActionHold, Direction: DirectionHold, Reason: err.Error(), Reasons: []string{err.Error()}}, err
	}
	inv := CalculateInventory(in.StockShares, in.FuturesEquivalentShares, in.Positions, candidateLotSize(in.Quotes))
	gap := target - inv.EffectiveInventory
	base := Signal{Action: ActionHold, Direction: DirectionHold, TargetInventory: target, InventoryGap: gap, ActualInventory: inv.ActualInventory, OptionDeltaStock: inv.OptionDeltaStock, EffectiveInventory: inv.EffectiveInventory, CapabilityStatus: CapabilityReady}
	if math.Abs(gap) <= cfg.NoTradeGap {
		return hold("inventory gap is within no-trade band", base), nil
	}
	direction := DirectionPut
	if gap < 0 {
		direction = DirectionCall
	}
	base.Direction = direction
	if in.DailyOrders < 0 {
		return hold("daily order count is invalid", base), nil
	}
	limit := cfg.MaxDailyOrders
	if in.ExtremeDay {
		limit = cfg.ExtremeMaxDailyOrders
	}
	if in.DailyOrders >= limit || limit <= 0 {
		return hold("daily order limit reached", base), nil
	}
	switch cfg.StrategicState {
	case StatePauseBuy, StateExit:
		if direction == DirectionPut {
			return hold("strategic state does not permit new puts", base), nil
		}
	}
	remaining := limit - in.DailyOrders
	qty := int(math.Ceil(math.Abs(gap) / float64(candidateLotSize(in.Quotes))))
	if qty < 1 {
		qty = 1
	}
	if qty > remaining {
		qty = remaining
	}
	if cfg.StrategicState == StateCaution && direction == DirectionPut && qty > 1 {
		qty = (qty + 1) / 2
	}
	if qty < 1 {
		return hold("inventory gap cannot produce an order", base), nil
	}

	validDirectionQuoteCount := 0
	dependencyBlocked := false
	for _, q := range in.Quotes {
		candidate := CandidateEvaluation{Quote: q, Direction: direction, Quantity: qty, SignedContracts: -qty, Quality: QualityScore(q)}
		if err := q.Validate(in.AsOf, cfg); err != nil {
			candidate.Reasons = append(candidate.Reasons, err.Error())
		} else {
			if (direction == DirectionPut && q.optionType() != string(Put)) || (direction == DirectionCall && q.optionType() != string(Call)) {
				candidate.Reasons = append(candidate.Reasons, "wheel: quote direction does not match inventory gap")
			} else {
				validDirectionQuoteCount++
			}
			if len(candidate.Reasons) == 0 && candidate.Quality < cfg.MinOptionQuality {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: option quality %.4f below minimum %.4f", candidate.Quality, cfg.MinOptionQuality))
			}
		}
		if len(candidate.Reasons) == 0 {
			signed := -float64(qty)
			if direction == DirectionCall {
				signed = -float64(qty)
			}
			postDelta := inv.OptionDeltaStock + signed*q.delta()*float64(q.LotSize)
			candidate.PostTradeEffective = EffectiveInventory(inv.ActualInventory, postDelta)
			candidate.AssignmentInventory = inv.ActualInventory
			if direction == DirectionPut {
				candidate.AssignmentInventory += PutAssignmentShares(in.Positions, DefaultLotSize) + float64(qty*q.LotSize)
			} else {
				candidate.AssignmentInventory -= CoveredShares(in.Positions, DefaultLotSize) + float64(qty*q.LotSize)
			}
			if direction == DirectionPut && candidate.AssignmentInventory > cfg.MaxInventory+1e-9 {
				candidate.Reasons = append(candidate.Reasons, "wheel: put assignment exceeds max inventory")
			}
			if direction == DirectionCall && candidate.AssignmentInventory < -1e-9 {
				candidate.Reasons = append(candidate.Reasons, "wheel: call assignment would create a naked short")
			}
			if direction == DirectionPut && !in.HasCashAvailable {
				candidate.Reasons = append(candidate.Reasons, "wheel: cash/margin availability is missing")
				dependencyBlocked = true
			} else if direction == DirectionPut && in.CashAvailable+1e-9 < PutAssignmentCash(in.Positions, DefaultLotSize)+q.Strike*float64(qty*q.LotSize) {
				candidate.Reasons = append(candidate.Reasons, "wheel: cash/margin is insufficient for put assignment")
			}
			if math.Abs(target-candidate.PostTradeEffective) > math.Abs(gap)+1e-9 {
				candidate.Reasons = append(candidate.Reasons, "wheel: trade would increase inventory deviation")
			}
		}
		candidate.Accepted = len(candidate.Reasons) == 0
		base.Candidates = append(base.Candidates, candidate)
		if !candidate.Accepted {
			base.RejectReasons = append(base.RejectReasons, candidate.Reasons...)
		}
	}
	accepted := make([]CandidateEvaluation, 0, len(base.Candidates))
	for _, c := range base.Candidates {
		if c.Accepted {
			accepted = append(accepted, c)
		}
	}
	if len(accepted) == 0 {
		if validDirectionQuoteCount == 0 {
			base.CapabilityStatus = CapabilityDataBlocked
			base.BlockedBy = append(base.BlockedBy, "option_quote_snapshot")
		}
		if dependencyBlocked {
			base.CapabilityStatus = CapabilityDataBlocked
			base.BlockedBy = append(base.BlockedBy, "cash_available")
		}
		return hold("no quote passed validation and risk checks", base), nil
	}
	sort.SliceStable(accepted, func(i, j int) bool {
		iDist, jDist := math.Abs(target-accepted[i].PostTradeEffective), math.Abs(target-accepted[j].PostTradeEffective)
		if iDist != jDist {
			return iDist < jDist
		}
		// In CAUTION, lower-strike puts are the safer increase-inventory
		// choice. This preference is applied only after post-trade risk is
		// equal, preserving the primary inventory objective.
		if cfg.StrategicState == StateCaution && direction == DirectionPut && accepted[i].Quote.Strike != accepted[j].Quote.Strike {
			return accepted[i].Quote.Strike < accepted[j].Quote.Strike
		}
		if accepted[i].Quality != accepted[j].Quality {
			return accepted[i].Quality > accepted[j].Quality
		}
		iSpread, jSpread := accepted[i].Quote.Spread()/accepted[i].Quote.Mid(), accepted[j].Quote.Spread()/accepted[j].Quote.Mid()
		if iSpread != jSpread {
			return iSpread < jSpread
		}
		if !accepted[i].Quote.Expiry.Equal(accepted[j].Quote.Expiry) {
			return accepted[i].Quote.Expiry.Before(accepted[j].Quote.Expiry)
		}
		if accepted[i].Quote.Strike != accepted[j].Quote.Strike {
			return accepted[i].Quote.Strike < accepted[j].Quote.Strike
		}
		return accepted[i].Quote.name() < accepted[j].Quote.name()
	})
	chosen := accepted[0]
	base.Action, base.Quantity, base.SignedContracts, base.Quality = ActionAlert, chosen.Quantity, chosen.SignedContracts, chosen.Quality
	base.ExpectedGain = expectedGain(chosen.Quote, chosen.Quantity)
	base.Quote = &chosen.Quote
	base.PostTradeEffective, base.AssignmentInventory = chosen.PostTradeEffective, chosen.AssignmentInventory
	base.Reason = fmt.Sprintf("%s inventory gap %.2f exceeds no-trade gap %.2f", direction, gap, cfg.NoTradeGap)
	base.Reasons = []string{base.Reason}
	return base, nil
}

func expectedGain(q OptionQuote, quantity int) float64 {
	if quantity <= 0 || q.LotSize <= 0 || !finite(q.Bid) || q.Bid <= 0 {
		return 0
	}
	return q.Bid * float64(q.LotSize) * float64(quantity)
}

// Decide is a convenience wrapper for callers that want an always-safe HOLD
// rather than handling configuration errors separately.
func Decide(cfg Config, in DecisionInput) Signal { s, _ := Evaluate(cfg, in); return s }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Package wheel contains the side-effect free domain model for the dynamic
// wheel strategy.  It deliberately has no broker or persistence dependency:
// callers provide a quote snapshot and receive an alert (or a hold).
package wheel

import (
	"encoding/json"
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
	// DefaultMinOptionProfit is the gross premium floor for one candidate
	// option trade. It is a total premium amount, not a per-share threshold.
	DefaultMinOptionProfit = 200.0
	// DefaultCoveredCallPct keeps covered calls meaningfully out of the money
	// while old persisted configs (which omit the new field) remain usable.
	DefaultCoveredCallPct = 0.05
	// MinWheelDTE/MaxWheelDTE bound the DTE window the wheel may trade.
	// The upper bound was widened 10 → 45 on 2026-08-14: the historical 5..10
	// window was a structural handicap (thin premiums, sparse candidates,
	// zero-candidate days); configs already deployed stay valid.
	MinWheelDTE = 5
	MaxWheelDTE = 45
)

// DTE validation failures are hot-path, expected candidate rejections. Keep
// them as sentinels so Validate does not format one heap-backed error per
// out-of-range contract; callers that need a human-readable signal can still
// use errors.Is and the stable sentinel text.
var (
	ErrDTEBelowMinimum = errors.New("wheel: option DTE is below configured minimum")
	ErrDTEAboveMaximum = errors.New("wheel: option DTE is above configured maximum")
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

// Config is the complete, structured wheel configuration. Strategic price
// anchors are deliberately separate from tactical parameters: callers choose
// the former, while optimizers may tune only the latter.
type Config struct {
	Strategy           string       `json:"strategy" yaml:"strategy"`
	PricePositionCurve []PricePoint `json:"price_position_curve,omitempty" yaml:"price_position_curve,omitempty"`
	FullPositionPrice  float64      `json:"full_position_price" yaml:"full_position_price"`
	ZeroPositionPrice  float64      `json:"zero_position_price" yaml:"zero_position_price"`
	MaxInventory       float64      `json:"max_inventory" yaml:"max_inventory"`
	MoveIntervalPct    float64      `json:"move_interval_pct" yaml:"move_interval_pct"`
	MinPremiumPerShare float64      `json:"min_premium_per_share" yaml:"min_premium_per_share"`
	MinOptionProfit    float64      `json:"min_option_profit" yaml:"min_option_profit"`
	StockSwitchPct     float64      `json:"stock_switch_pct" yaml:"stock_switch_pct"`
	CoveredCallPct     float64      `json:"covered_call_pct" yaml:"covered_call_pct"`
	// ProfitTakePct closes a short leg early once the received credit decays
	// to (1 − profit_take_pct) of its fill premium: the premium captured as a
	// fraction of max profit reaches the threshold and the leg is bought back
	// (releases cash/margin). 0 = hold to expiry (compat default).
	ProfitTakePct float64 `json:"profit_take_pct,omitempty" yaml:"profit_take_pct,omitempty"`
	// PutDeltaMax/CallDeltaMax cap the absolute sell-side delta of candidate
	// contracts; the OTM hard constraint stays the primary gate, delta is the
	// secondary guard (deep-OTM low-premium contracts are pointless). 0 = no
	// delta limit; a large value is equivalent.
	PutDeltaMax  float64 `json:"put_delta_max,omitempty" yaml:"put_delta_max,omitempty"`
	CallDeltaMax float64 `json:"call_delta_max,omitempty" yaml:"call_delta_max,omitempty"`
	// MinIVRank requires the underlying's 1-year IV percentile (DecisionInput
	// IVRank) to reach the threshold before any sell candidate is accepted;
	// below it every candidate is masked → HOLD. 0 = no IV-rank filter.
	MinIVRank              float64         `json:"min_iv_rank,omitempty" yaml:"min_iv_rank,omitempty"`
	TradeGap               float64         `json:"trade_gap" yaml:"trade_gap"`
	MinOptionQuality       float64         `json:"min_option_quality" yaml:"min_option_quality"`
	MinDTE                 int             `json:"min_dte" yaml:"min_dte"`
	MaxDTE                 int             `json:"max_dte" yaml:"max_dte"`
	StrategicState         string          `json:"strategic_state" yaml:"strategic_state"`
	MigrationLossy         bool            `json:"migration_lossy,omitempty" yaml:"migration_lossy,omitempty"`
	MigrationWarningCount  int             `json:"migration_warning_count,omitempty" yaml:"migration_warning_count,omitempty"`
	MigrationWarnings      []string        `json:"migration_warnings,omitempty" yaml:"migration_warnings,omitempty"`
	MigrationOriginalCurve json.RawMessage `json:"migration_original_price_position_curve,omitempty" yaml:"-"`

	// MaxQuoteAgeSeconds is required freshness in seconds. Zero uses the
	// conservative one-day default so a quote can never be timeless.
	MaxQuoteAgeSeconds int `json:"max_quote_age_seconds,omitempty" yaml:"max_quote_age_seconds,omitempty"`
}

func (c Config) Validate() error {
	if c.Strategy != "wheel" {
		return fmt.Errorf("wheel: strategy must be wheel")
	}
	if !finite(c.MaxInventory) || c.MaxInventory <= 0 || math.Trunc(c.MaxInventory) != c.MaxInventory {
		return fmt.Errorf("wheel: max_inventory must be a positive integer")
	}
	if len(c.PricePositionCurve) > 0 {
		if err := validatePricePositionCurve(c.PricePositionCurve, c.MaxInventory); err != nil {
			return err
		}
	} else {
		if !finite(c.FullPositionPrice) || c.FullPositionPrice <= 0 {
			return fmt.Errorf("wheel: full_position_price must be positive")
		}
		if !finite(c.ZeroPositionPrice) || c.ZeroPositionPrice <= c.FullPositionPrice {
			return fmt.Errorf("wheel: zero_position_price must be greater than full_position_price")
		}
	}
	if c.MinDTE < MinWheelDTE || c.MaxDTE > MaxWheelDTE || c.MinDTE > c.MaxDTE {
		return fmt.Errorf("wheel: DTE must be a valid range within %d..%d", MinWheelDTE, MaxWheelDTE)
	}
	if !finite(c.MinOptionQuality) || c.MinOptionQuality < 0 || c.MinOptionQuality > 1 {
		return fmt.Errorf("wheel: min_option_quality must be in [0,1]")
	}
	if !finite(c.MoveIntervalPct) || c.MoveIntervalPct < 0 {
		return fmt.Errorf("wheel: move_interval_pct must be non-negative")
	}
	if !finite(c.MinPremiumPerShare) || c.MinPremiumPerShare < 0 {
		return fmt.Errorf("wheel: min_premium_per_share must be non-negative")
	}
	if !finite(c.MinOptionProfit) || c.MinOptionProfit < 0 {
		return fmt.Errorf("wheel: min_option_profit must be non-negative")
	}
	if !finite(c.StockSwitchPct) || c.StockSwitchPct < 0 {
		return fmt.Errorf("wheel: stock_switch_pct must be non-negative")
	}
	if !finite(c.CoveredCallPct) || c.CoveredCallPct < 0 || c.CoveredCallPct > 1 {
		return fmt.Errorf("wheel: covered_call_pct must be in [0,1]")
	}
	if !finite(c.ProfitTakePct) || c.ProfitTakePct < 0 || c.ProfitTakePct > 0.8 {
		return fmt.Errorf("wheel: profit_take_pct must be in [0,0.8]")
	}
	if !finite(c.PutDeltaMax) || c.PutDeltaMax < 0 || c.PutDeltaMax > 1 {
		return fmt.Errorf("wheel: put_delta_max must be in [0,1]")
	}
	if !finite(c.CallDeltaMax) || c.CallDeltaMax < 0 || c.CallDeltaMax > 1 {
		return fmt.Errorf("wheel: call_delta_max must be in [0,1]")
	}
	if !finite(c.MinIVRank) || c.MinIVRank < 0 || c.MinIVRank > 1 {
		return fmt.Errorf("wheel: min_iv_rank must be in [0,1]")
	}
	if !finite(c.TradeGap) || c.TradeGap < 0 {
		return fmt.Errorf("wheel: trade_gap must be non-negative")
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
	if len(c.PricePositionCurve) > 0 {
		if !finite(price) || price <= 0 || !finite(c.MaxInventory) || c.MaxInventory <= 0 {
			return 0, errors.New("wheel: invalid price curve or current price")
		}
		if err := validatePricePositionCurve(c.PricePositionCurve, c.MaxInventory); err != nil {
			return 0, err
		}
		return InterpolateTargetInventory(c.PricePositionCurve, price)
	}
	if !finite(price) || price <= 0 || !finite(c.FullPositionPrice) || !finite(c.ZeroPositionPrice) || c.FullPositionPrice <= 0 || c.ZeroPositionPrice <= c.FullPositionPrice || !finite(c.MaxInventory) || c.MaxInventory <= 0 {
		return 0, errors.New("wheel: invalid price anchors or current price")
	}
	if price <= c.FullPositionPrice {
		return c.MaxInventory, nil
	}
	if price >= c.ZeroPositionPrice {
		return 0, nil
	}
	ratio := (price - c.FullPositionPrice) / (c.ZeroPositionPrice - c.FullPositionPrice)
	return c.MaxInventory * (1 - ratio), nil
}

func validatePricePositionCurve(curve []PricePoint, maxInventory float64) error {
	if len(curve) < 2 {
		return errors.New("wheel: price_position_curve must contain at least two points")
	}
	for i, p := range curve {
		if !finite(p.Price) || p.Price <= 0 || !finite(p.TargetInventory) || p.TargetInventory < 0 || p.TargetInventory > maxInventory {
			return fmt.Errorf("wheel: price_position_curve point %d is outside price/inventory bounds", i)
		}
		if i > 0 && (p.Price <= curve[i-1].Price || p.TargetInventory > curve[i-1].TargetInventory) {
			return errors.New("wheel: price_position_curve prices must increase and target inventory must not increase")
		}
	}
	return nil
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
	// AvgPremium is the per-share fill premium of the position: the received
	// credit for short legs (the profit_take_pct basis), the paid debit for
	// long legs. Zero when the caller has no fill-basis data; profit taking
	// then cannot be evaluated and stays off for that leg.
	AvgPremium float64 `json:"avg_premium,omitempty"`
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
	asOfDate, minExpiryDate, maxExpiryDate := validationDates(asOf, cfg)
	return q.validate(asOf, cfg, asOfDate, minExpiryDate, maxExpiryDate)
}

func (q OptionQuote) validate(asOf time.Time, cfg Config, asOfDate, minExpiryDate, maxExpiryDate time.Time) error {
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
		expiryDate := calendarDate(q.Expiry)
		if expiryDate.Before(minExpiryDate) {
			return ErrDTEBelowMinimum
		}
		if expiryDate.After(maxExpiryDate) {
			return ErrDTEAboveMaximum
		}
	}
	return nil
}

func validationDates(asOf time.Time, cfg Config) (asOfDate, minExpiryDate, maxExpiryDate time.Time) {
	if asOf.IsZero() {
		return time.Time{}, time.Time{}, time.Time{}
	}
	asOfDate = calendarDate(asOf)
	return asOfDate, asOfDate.AddDate(0, 0, cfg.MinDTE), asOfDate.AddDate(0, 0, cfg.MaxDTE)
}

func calendarDate(ts time.Time) time.Time {
	utc := ts.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
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
	CurrentPrice float64
	AsOf         time.Time
	StockShares  float64
	// StockAverageCost is the running acquisition basis of held stock. A
	// positive value protects direct stock sales and covered-call strikes from
	// realizing a stock loss. Zero preserves compatibility for callers with no
	// stock basis (and is irrelevant when StockShares is zero).
	StockAverageCost        float64
	FuturesEquivalentShares float64
	Positions               []OptionPosition
	Quotes                  []OptionQuote
	// LastEffectiveFillPrice is optional. A zero value means that no prior
	// effective fill is available, so movement-based tactical gates stay off.
	LastEffectiveFillPrice float64
	CashAvailable          float64
	HasCashAvailable       bool
	// IVRank is the underlying's 1-year implied-volatility percentile in
	// [0,1]. Zero means unknown (no history source wired); with min_iv_rank
	// > 0 an unknown rank masks every candidate (fail-closed HOLD).
	IVRank float64
	// ClosePositions extends the short-leg set considered for profit_take_pct
	// buybacks only. It is deliberately excluded from the inventory math:
	// positions held outside the DTE window (final min_dte days before expiry)
	// must stay closable live, yet must not change the effective inventory.
	// nil preserves the Positions-only behavior.
	ClosePositions []OptionPosition
	// Pending lists unfilled orders already resting for this symbol.
	// Candidates whose contract+direction match an entry are excluded:
	// re-alerting the same contract while a prior order is unfilled would
	// ask the user to double up an open position (2026-08-13: LLM gate
	// kept rejecting repeated US.JD P29000 alerts over pending order 206158430256).
	Pending []PendingOrder
}

// PendingOrder is the unfilled-order footprint the strategy needs to avoid
// duplicate alerts. Kept intentionally minimal; runner maps the store row.
type PendingOrder struct {
	Contract  string
	Direction string
	OrderID   string
}

// ReplaceOrder marks a signal as a modify (改单): the previously confirmed
// unfilled order should be cancelled and this signal's candidate placed in
// its stead. It never bypasses review or confirm — the gate and the operator
// decide as with a fresh order (老板指令 2026-08-13).
type ReplaceOrder struct {
	OrderID  string `json:"order_id"`
	Contract string `json:"contract"`
}

type CandidateEvaluation struct {
	Quote               OptionQuote `json:"quote"`
	Direction           Direction   `json:"direction"`
	Quantity            int         `json:"quantity"`
	SignedContracts     int         `json:"signed_contracts"`
	Quality             float64     `json:"quality"`
	ExpectedGain        float64     `json:"expected_gain,omitempty"`
	PostTradeEffective  float64     `json:"post_trade_effective_inventory"`
	AssignmentInventory float64     `json:"assignment_inventory"`
	Accepted            bool        `json:"accepted"`
	Reasons             []string    `json:"reasons,omitempty"`
}

// StockSuggestion is a non-executing tactical suggestion. It is attached to
// a HOLD signal so existing option-alert and human-review gates cannot mistake
// it for an option order.
type StockSuggestion struct {
	Side   string  `json:"side"`
	Shares float64 `json:"shares"`
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
	StockSuggestion     *StockSuggestion      `json:"stock_suggestion,omitempty"`
	// Replace marks this ALERT as a 改单: replace the pending order (same
	// direction, different contract) with the chosen candidate. Set only
	// when exactly one same-direction pending order exists and the chosen
	// contract differs from it; multiple pendings are left alone.
	Replace *ReplaceOrder `json:"replace,omitempty"`
	// ClosePosition marks an ALERT as a buy-to-close of a held short leg
	// (profit_take_pct reached): Quantity is the buy-back contract count,
	// Direction is the held leg's option type. The human confirmation loop
	// is unchanged — the alert is a suggestion, never an order.
	ClosePosition    bool     `json:"close_position,omitempty"`
	CapabilityStatus string   `json:"capability_status"`
	BlockedBy        []string `json:"blocked_by,omitempty"`
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
	// profit_take_pct exits a short leg when the captured premium reaches the
	// threshold. It is a risk-reducing exit, so it runs before every entry
	// gate (gap band, move interval, stock switch, IV rank): a within-band
	// inventory gap must not suppress the close.
	if sig, ok := takeProfitSignal(cfg, in, base); ok {
		return sig, nil
	}
	if math.Abs(gap) <= cfg.TradeGap {
		return hold("inventory gap is within no-trade band", base), nil
	}
	if in.LastEffectiveFillPrice > 0 && cfg.MoveIntervalPct > 0 {
		move := math.Abs(in.CurrentPrice-in.LastEffectiveFillPrice) / in.LastEffectiveFillPrice
		if move < cfg.MoveIntervalPct {
			return hold("price move is below move_interval_pct", base), nil
		}
	}
	if in.LastEffectiveFillPrice > 0 && cfg.StockSwitchPct > 0 {
		move := math.Abs(in.CurrentPrice-in.LastEffectiveFillPrice) / in.LastEffectiveFillPrice
		if move >= cfg.StockSwitchPct {
			side := "BUY"
			if gap < 0 {
				side = "SELL"
			}
			if side == "SELL" && in.StockShares > 0 && in.StockAverageCost <= 0 {
				base.CapabilityStatus = CapabilityDataBlocked
				base.BlockedBy = append(base.BlockedBy, "stock_average_cost")
				return hold("stock_switch_pct reached but stock average cost is unavailable; cannot sell safely", base), nil
			}
			if side == "SELL" && in.StockAverageCost > 0 && in.CurrentPrice+1e-9 < in.StockAverageCost {
				return hold("stock_switch_pct reached but stock price is below average cost; keep stock until recovery", base), nil
			}
			base.StockSuggestion = &StockSuggestion{Side: side, Shares: math.Abs(gap)}
			return hold("stock_switch_pct reached; stock suggestion only", base), nil
		}
	}
	direction := DirectionPut
	if gap < 0 {
		direction = DirectionCall
	}
	base.Direction = direction
	switch cfg.StrategicState {
	case StatePauseBuy, StateExit:
		if direction == DirectionPut {
			return hold("strategic state does not permit new puts", base), nil
		}
	}
	// IV-rank gate: below the threshold (or unknown history) every candidate
	// is masked → HOLD. The exit path above is deliberately unaffected.
	if cfg.MinIVRank > 0 {
		if in.IVRank <= 0 {
			return hold("wheel: IV rank is unavailable (needs one-year underlying IV history); cannot confirm min_iv_rank", base), nil
		}
		if in.IVRank < cfg.MinIVRank {
			return hold(fmt.Sprintf("wheel: IV rank %.4f below min_iv_rank %.4f", in.IVRank, cfg.MinIVRank), base), nil
		}
	}
	validDirectionQuoteCount := 0
	dependencyBlocked := false
	stockCostBlocked := false
	pendingExcluded := 0
	// 改单资格记录:循环中首个带挂单的候选保留其合约与有效性,供选中候选
	// 后按选择排序判定「撤旧挂新」是否真的更优(避免把更优挂单换成更差合约)。
	pendingContract := ""
	var pendingCandidate CandidateEvaluation
	pendingValid := false
	asOfDate, minExpiryDate, maxExpiryDate := validationDates(in.AsOf, cfg)
	for _, q := range in.Quotes {
		// A signal remains one human-reviewed contract suggestion. Removing the
		// daily cap means later valid evaluations are not suppressed; it does not
		// turn one reminder into an unbounded multi-contract order.
		qty := 1
		candidate := CandidateEvaluation{Quote: q, Direction: direction, Quantity: qty, SignedContracts: -qty}
		if hasPendingContract(in.Pending, q.Symbol, string(direction)) {
			// Pending matches imply direction agreement, so the quote still
			// counts as direction-valid: all-candidates-pending must read as
			// HOLD, not DATA_BLOCKED (quotes are fresh, the book is full).
			candidate.Reasons = append(candidate.Reasons, "wheel: contract already has an unfilled pending order")
			pendingExcluded++
			validDirectionQuoteCount++
			// 记录首个挂单候选的选择排序要素与有效性,供改单判定
			// (见下方选中分支)。仅首个即可:改单只对唯一同方向挂单生效。
			if pendingContract == "" {
				signed := -float64(1)
				postDelta := inv.OptionDeltaStock + signed*q.delta()*float64(q.LotSize)
				pendingCandidate = CandidateEvaluation{Quote: q, Direction: direction, Quantity: 1, SignedContracts: -1, Quality: QualityScore(q), PostTradeEffective: EffectiveInventory(inv.ActualInventory, postDelta)}
				pendingValid = q.validate(in.AsOf, cfg, asOfDate, minExpiryDate, maxExpiryDate) == nil
				pendingContract = q.Symbol
			}
		} else if err := q.validate(in.AsOf, cfg, asOfDate, minExpiryDate, maxExpiryDate); err != nil {
			reason := err.Error()
			if errors.Is(err, ErrDTEBelowMinimum) || errors.Is(err, ErrDTEAboveMaximum) {
				reason = dteRejectionReason(q, asOfDate, cfg)
			}
			candidate.Reasons = append(candidate.Reasons, reason)
		} else {
			if (direction == DirectionPut && q.optionType() != string(Put)) || (direction == DirectionCall && q.optionType() != string(Call)) {
				candidate.Reasons = append(candidate.Reasons, "wheel: quote direction does not match inventory gap")
			} else {
				validDirectionQuoteCount++
			}
			// Moneyness and covered-call basis are hard masks. Apply them before
			// quality/premium scoring: ITM contracts otherwise win naturally on
			// their large intrinsic-value premium.
			if len(candidate.Reasons) == 0 && direction == DirectionPut && q.Strike >= in.CurrentPrice {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: sell put must be OTM (strike %.4f < underlying %.4f)", q.Strike, in.CurrentPrice))
			}
			if len(candidate.Reasons) == 0 && direction == DirectionCall && q.Strike <= in.CurrentPrice {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: sell call must be OTM (strike %.4f > underlying %.4f)", q.Strike, in.CurrentPrice))
			}
			if len(candidate.Reasons) == 0 && direction == DirectionCall {
				if in.StockShares > 0 && in.StockAverageCost <= 0 {
					candidate.Reasons = append(candidate.Reasons, "wheel: stock average cost is unavailable; covered call cannot guarantee loss protection")
					stockCostBlocked = true
				}
			}
			if len(candidate.Reasons) == 0 && direction == DirectionCall {
				minimumStrike := in.CurrentPrice * (1 + cfg.CoveredCallPct)
				if in.StockAverageCost > minimumStrike {
					minimumStrike = in.StockAverageCost
				}
				if q.Strike+1e-9 < minimumStrike {
					candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: covered call strike %.4f below protected minimum %.4f", q.Strike, minimumStrike))
				}
			}
			// Delta cap rides on top of the OTM/moneyness hard masks: puts
			// compare the absolute delta (market delta is negative), calls the
			// positive delta. 0 disables the filter.
			if len(candidate.Reasons) == 0 && direction == DirectionPut && cfg.PutDeltaMax > 0 && math.Abs(q.delta()) > cfg.PutDeltaMax {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: sell put delta %.4f exceeds put_delta_max %.4f", math.Abs(q.delta()), cfg.PutDeltaMax))
			}
			if len(candidate.Reasons) == 0 && direction == DirectionCall && cfg.CallDeltaMax > 0 && q.delta() > cfg.CallDeltaMax {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: sell call delta %.4f exceeds call_delta_max %.4f", q.delta(), cfg.CallDeltaMax))
			}
			if len(candidate.Reasons) == 0 {
				candidate.Quality = QualityScore(q)
				candidate.ExpectedGain = expectedGain(q, qty)
			}
			if len(candidate.Reasons) == 0 && candidate.Quality < cfg.MinOptionQuality {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: option quality %.4f below minimum %.4f", candidate.Quality, cfg.MinOptionQuality))
			}
			if len(candidate.Reasons) == 0 && q.Bid < cfg.MinPremiumPerShare {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: premium per share %.4f below minimum %.4f", q.Bid, cfg.MinPremiumPerShare))
			}
			if len(candidate.Reasons) == 0 && candidate.ExpectedGain < cfg.MinOptionProfit {
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("wheel: expected option profit %.2f below minimum %.2f", candidate.ExpectedGain, cfg.MinOptionProfit))
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
		if stockCostBlocked {
			base.CapabilityStatus = CapabilityDataBlocked
			base.BlockedBy = append(base.BlockedBy, "stock_average_cost")
		}
		if pendingExcluded > 0 && validDirectionQuoteCount == pendingExcluded {
			// Every direction-valid quote is already covered by an unfilled
			// order: the book is full, not the data. HOLD quietly instead of
			// asking the user to double an open position.
			return hold("all qualified candidates already have unfilled pending orders", base), nil
		}
		return hold("no quote passed validation and risk checks", base), nil
	}
	sort.SliceStable(accepted, func(i, j int) bool { return betterCandidate(accepted[i], accepted[j], cfg, target, direction) })
	chosen := accepted[0]
	base.Action, base.Quantity, base.SignedContracts, base.Quality = ActionAlert, chosen.Quantity, chosen.SignedContracts, chosen.Quality
	base.ExpectedGain = expectedGain(chosen.Quote, chosen.Quantity)
	base.Quote = &chosen.Quote
	base.PostTradeEffective, base.AssignmentInventory = chosen.PostTradeEffective, chosen.AssignmentInventory
	base.Reason = fmt.Sprintf("%s inventory gap %.2f exceeds trade gap %.2f", direction, gap, cfg.TradeGap)
	base.Reasons = []string{base.Reason}
	if replace := pendingReplaceTarget(in.Pending, direction, chosen.Quote.Symbol); replace != nil {
		// 改单闸门:rest 单已不在报价(过期/失效)、已不满足结构校验、或新
		// 候选严格按选择排序优于 rest 单时才撤旧挂新。更优合约上的 rest
		// 单(被排除的 natural top)绝不换成次优候选,避免策略空转改单。
		if pendingContract == "" || !pendingValid || betterCandidate(chosen, pendingCandidate, cfg, target, direction) {
			base.Replace = replace
			base.Reason = fmt.Sprintf("%s inventory gap %.2f exceeds trade gap %.2f; replace pending %s with %s", direction, gap, cfg.TradeGap, replace.Contract, chosen.Quote.Symbol)
		}
	}
	return base, nil
}

// takeProfitSignal looks for a short leg whose captured premium ratio
// (fill credit − current ask) / fill credit has reached profit_take_pct and
// returns its buy-to-close ALERT. The most profitable leg wins; ties break on
// symbol for determinism. Legs without a fill basis (AvgPremium zero), without
// a priced quote, or already expiring this bar are skipped. The exit never
// depends on inventory-gap direction or IV rank — it is a risk-reducing close.
func takeProfitSignal(cfg Config, in DecisionInput, base Signal) (Signal, bool) {
	if cfg.ProfitTakePct <= 0 {
		return Signal{}, false
	}
	type candidate struct {
		quote     OptionQuote
		pos       OptionPosition
		contracts float64
		ratio     float64
	}
	var best *candidate
	// ClosePositions augments Positions for exits only (held legs outside the
	// DTE window stay closable); same-symbol entries collapse to one leg.
	closeSet := make(map[string]OptionPosition, len(in.Positions)+len(in.ClosePositions))
	for _, p := range in.Positions {
		if p.Symbol != "" {
			closeSet[p.Symbol] = p
		}
	}
	for _, p := range in.ClosePositions {
		if p.Symbol != "" {
			closeSet[p.Symbol] = p
		}
	}
	for _, p := range closeSet {
		contracts := p.SignedContracts
		if contracts == 0 {
			contracts = p.Contracts
		}
		if contracts >= 0 || p.AvgPremium <= 0 {
			continue
		}
		if strings.TrimSpace(p.Symbol) == "" {
			continue // no symbol to match against the quote set
		}
		quote, ok := positionQuote(in.Quotes, p.Symbol)
		if !ok || !finite(quote.Ask) || quote.Ask <= 0 || quote.Ask >= p.AvgPremium {
			continue
		}
		if !in.AsOf.IsZero() && !quote.Expiry.After(in.AsOf) {
			continue // settling this bar; do not race the expiry settlement
		}
		ratio := (p.AvgPremium - quote.Ask) / p.AvgPremium
		if ratio+1e-12 < cfg.ProfitTakePct {
			continue
		}
		if best == nil || ratio > best.ratio || (ratio == best.ratio && p.Symbol < best.pos.Symbol) {
			best = &candidate{quote: quote, pos: p, contracts: contracts, ratio: ratio}
		}
	}
	if best == nil {
		return Signal{}, false
	}
	qty := int(-best.contracts)
	if qty <= 0 {
		return Signal{}, false
	}
	sig := base
	sig.Action, sig.Direction, sig.Quantity, sig.SignedContracts = ActionAlert, Direction(strings.ToUpper(string(best.pos.optionType()))), qty, qty
	sig.Quote = &best.quote
	sig.ClosePosition = true
	sig.Reason = fmt.Sprintf("profit_take_pct %.2f reached: received %.4f, buyback ask %.4f, captured %.1f%% of max profit", cfg.ProfitTakePct, best.pos.AvgPremium, best.quote.Ask, best.ratio*100)
	sig.Reasons = []string{sig.Reason}
	return sig, true
}

// positionQuote finds a quote for the held contract by symbol name.
func positionQuote(quotes []OptionQuote, symbol string) (OptionQuote, bool) {
	for _, q := range quotes {
		if q.name() == symbol {
			return q, true
		}
	}
	return OptionQuote{}, false
}

func dteRejectionReason(q OptionQuote, asOfDate time.Time, cfg Config) string {
	dte := int(calendarDate(q.Expiry).Sub(asOfDate) / (24 * time.Hour))
	return fmt.Sprintf("wheel: DTE %d outside [%d,%d]", dte, cfg.MinDTE, cfg.MaxDTE)
}

// betterCandidate reports whether candidate a strictly outranks b in
// selection: smaller distance to target, then (CAUTION) lower put strike,
// higher quality, tighter relative spread, earlier expiry, lower strike,
// name. Shared by the selection sort and the 改单 gate so one ordering
// governs both.
func betterCandidate(a, b CandidateEvaluation, cfg Config, target float64, direction Direction) bool {
	aDist, bDist := math.Abs(target-a.PostTradeEffective), math.Abs(target-b.PostTradeEffective)
	if aDist != bDist {
		return aDist < bDist
	}
	// In CAUTION, lower-strike puts are the safer increase-inventory
	// choice. This preference is applied only after post-trade risk is
	// equal, preserving the primary inventory objective.
	if cfg.StrategicState == StateCaution && direction == DirectionPut && a.Quote.Strike != b.Quote.Strike {
		return a.Quote.Strike < b.Quote.Strike
	}
	if a.Quality != b.Quality {
		return a.Quality > b.Quality
	}
	aSpread, bSpread := a.Quote.Spread()/a.Quote.Mid(), b.Quote.Spread()/b.Quote.Mid()
	if aSpread != bSpread {
		return aSpread < bSpread
	}
	if !a.Quote.Expiry.Equal(b.Quote.Expiry) {
		return a.Quote.Expiry.Before(b.Quote.Expiry)
	}
	if a.Quote.Strike != b.Quote.Strike {
		return a.Quote.Strike < b.Quote.Strike
	}
	return a.Quote.name() < b.Quote.name()
}

// pendingReplaceTarget returns the single same-direction pending order that a
// better candidate should replace (改单), or nil. Multiple same-direction
// pendings or a pending for the very contract chosen are left alone — the
// former is ambiguous, the latter is already covered (and excluded above).
func pendingReplaceTarget(pending []PendingOrder, direction, chosenContract string) *ReplaceOrder {
	var same []PendingOrder
	for _, p := range pending {
		if strings.EqualFold(p.Direction, direction) {
			same = append(same, p)
		}
	}
	if len(same) != 1 || same[0].Contract == chosenContract || same[0].OrderID == "" {
		return nil
	}
	return &ReplaceOrder{OrderID: same[0].OrderID, Contract: same[0].Contract}
}

// hasPendingContract reports whether an unfilled order for the same contract
// and direction is already resting, so the strategy does not re-alert it.
// Direction is compared case-insensitively (store rows say PUT/CALL).
func hasPendingContract(pending []PendingOrder, contract, direction string) bool {
	for _, p := range pending {
		if p.Contract == contract && strings.EqualFold(p.Direction, direction) {
			return true
		}
	}
	return false
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

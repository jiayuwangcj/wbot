package backtest

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

const (
	ValuationComplete   = "COMPLETE"
	ValuationIncomplete = "INCOMPLETE"

	SettlementAllOptionLegsSettled = "ALL_OPTION_LEGS_SETTLED"
	SettlementOpenOptionLegs       = "OPEN_OPTION_LEGS"
)

// TerminalSummary describes the portfolio at the final bar. Amounts are
// accounting marks, not broker liquidation proceeds. P&L fields are null only
// when an open option leg has no price mark.
type TerminalSummary struct {
	ValuationStatus              string   `json:"valuation_status"`
	SettlementStatus             string   `json:"settlement_status"`
	CashAmount                   float64  `json:"cash_amount"`
	UnderlyingPrice              float64  `json:"underlying_price"`
	StockShares                  float64  `json:"stock_shares"`
	StockAverageCost             *float64 `json:"stock_average_cost"`
	StockMarketValueAmount       float64  `json:"stock_market_value_amount"`
	OptionMarketValueAmount      *float64 `json:"option_market_value_amount"`
	HoldingsMarketValueAmount    *float64 `json:"holdings_market_value_amount"`
	FinalEquityAmount            *float64 `json:"final_equity_amount"`
	OpenOptionLegCount           int64    `json:"open_option_leg_count"`
	GrossOpenOptionContractCount float64  `json:"gross_open_option_contract_count"`
	PnLStatus                    string   `json:"pnl_status"`
	RealizedPnLAmount            *float64 `json:"realized_pnl_amount"`
	UnrealizedPnLAmount          *float64 `json:"unrealized_pnl_amount"`
	ExpiryCount                  int64    `json:"expiry_count"`
	ShortExpiryCount             int64    `json:"short_expiry_count"`
	AssignmentCount              int64    `json:"assignment_count"`
	AssignmentRate               *float64 `json:"assignment_rate"`
	EventBasis                   string   `json:"event_basis"`
	BrokerExpiryCount            *int64   `json:"broker_expiry_count"`
	BrokerAssignmentCount        *int64   `json:"broker_assignment_count"`
}

// PnLAttribution splits the realized P&L into its sources so a report can show
// where the money came from instead of one residual number. The identity
// RealizedPnLAmount = PremiumIncomeAmount − OptionCloseCostAmount +
// StockRealizedPnLAmount − FeesAmount is asserted by the report builder;
// UnfilledAttemptPremium is the premium attempted fills would have collected
// (opportunity cost, never booked into any P&L line).
type PnLAttribution struct {
	PremiumIncomeAmount    float64 `json:"premium_income_amount"`
	OptionCloseCostAmount  float64 `json:"option_close_cost_amount"`
	StockRealizedPnLAmount float64 `json:"stock_realized_pnl_amount"`
	FeesAmount             float64 `json:"fees_amount"`
	RealizedPnLAmount      float64 `json:"realized_pnl_amount"`
	UnfilledAttemptPremium float64 `json:"unfilled_attempt_premium_amount"`
	UnfilledAttemptCount   int64   `json:"unfilled_attempt_count"`
}

func attributionOf(st *State, fees FeeSummary, unfilled UnfilledStats) PnLAttribution {
	return PnLAttribution{
		PremiumIncomeAmount:    st.PremiumIncome,
		OptionCloseCostAmount:  st.OptionCloseCost,
		StockRealizedPnLAmount: st.StockRealizedPnL,
		FeesAmount:             fees.TotalAmount,
		RealizedPnLAmount:      st.PremiumIncome - st.OptionCloseCost + st.StockRealizedPnL - fees.TotalAmount,
		UnfilledAttemptPremium: st.UnfilledAttemptPremium,
		UnfilledAttemptCount:   unfilled.UnfilledCount,
	}
}

// DataQualitySummary is computed from the exact option snapshot batches
// consumed by the run. MissingRequiredFieldCounts is always present and uses
// zero counts when there are no rows, so zero coverage cannot masquerade as a
// complete snapshot.
type DataQualitySummary struct {
	Status                        string           `json:"status"`
	OptionDataRequired            bool             `json:"option_data_required"`
	UnderlyingBars                []BarProvenance  `json:"underlying_bars"`
	OptionSnapshotSources         []string         `json:"option_snapshot_sources"`
	TotalBarCount                 int              `json:"total_bar_count"`
	ReadyBarCount                 int              `json:"ready_bar_count"`
	BlockedBarCount               int              `json:"blocked_bar_count"`
	ValidCoverageRatio            *float64         `json:"valid_coverage_ratio"`
	SnapshotBatchCount            int              `json:"snapshot_batch_count"`
	SnapshotContractRowCount      int              `json:"snapshot_contract_row_count"`
	MissingRequiredFieldCounts    map[string]int64 `json:"missing_required_field_counts"`
	HistoricalOptionCycleComplete *bool            `json:"historical_option_cycle_complete"`
	BlockedBy                     []string         `json:"blocked_by"`
}

// BarProvenance records the actual underlying rows consumed by a run. Adjusted
// uses provider vocabulary (Tencent qfq) while bars.adjust remains the
// repository's canonical fwd value.
type BarProvenance struct {
	Source   string `json:"source"`
	Adjusted string `json:"adjusted"`
	BarCount int    `json:"bar_count"`
}

var requiredSnapshotFields = []string{
	"ask", "bid", "delta", "expiry", "implied_vol", "lot_size",
	"observed_at", "open_interest", "option_type", "snapshot_key",
	"source", "strike", "symbol", "theta", "underlying", "underlying_price", "volume",
}

func terminalSummary(st *State, initialCash, finalPrice float64) TerminalSummary {
	t := TerminalSummary{
		ValuationStatus:        ValuationComplete,
		SettlementStatus:       SettlementAllOptionLegsSettled,
		CashAmount:             st.Cash,
		UnderlyingPrice:        finalPrice,
		StockShares:            st.Position,
		StockMarketValueAmount: st.Position * finalPrice,
		OpenOptionLegCount:     int64(len(st.Options)),
		ExpiryCount:            st.ExpiryCount,
		ShortExpiryCount:       st.ShortExpiryCount,
		AssignmentCount:        st.AssignmentCount,
		EventBasis:             "mechanical_backtest",
		PnLStatus:              ValuationComplete,
	}
	if st.Position != 0 {
		cost := st.StockAverageCost
		t.StockAverageCost = &cost
	}
	if len(st.Options) > 0 {
		t.SettlementStatus = SettlementOpenOptionLegs
	}
	optionValue := 0.0
	unrealized := st.Position * (finalPrice - st.StockAverageCost)
	for _, p := range st.Options {
		t.GrossOpenOptionContractCount += math.Abs(p.Contracts)
		mark, ok := st.OptPrice[p.Code]
		if !ok {
			t.ValuationStatus = ValuationIncomplete
			continue
		}
		lot := p.Lot
		if lot <= 0 {
			lot = p.LotSize
		}
		if lot <= 0 {
			t.ValuationStatus = ValuationIncomplete
			continue
		}
		optionValue += p.Contracts * float64(lot) * mark
		unrealized += p.Contracts * float64(lot) * (mark - p.AvgPremium)
	}
	if st.ShortExpiryCount > 0 {
		rate := float64(st.AssignmentCount) / float64(st.ShortExpiryCount)
		t.AssignmentRate = &rate
	}
	if t.ValuationStatus == ValuationComplete {
		holdings := t.StockMarketValueAmount + optionValue
		finalEquity := st.Cash + holdings
		realized := finalEquity - initialCash - unrealized
		t.OptionMarketValueAmount = &optionValue
		t.HoldingsMarketValueAmount = &holdings
		t.FinalEquityAmount = &finalEquity
		t.RealizedPnLAmount = &realized
		t.UnrealizedPnLAmount = &unrealized
	} else {
		t.PnLStatus = "NOT_APPLICABLE_MISSING_MARK"
	}
	return t
}

func summarizeDataQuality(bars []ingest.Bar, opts *OptionsData, signals []SignalTrace) DataQualitySummary {
	counts := make(map[string]int64, len(requiredSnapshotFields))
	for _, field := range requiredSnapshotFields {
		counts[field] = 0
	}
	q := DataQualitySummary{
		Status:                     "NOT_APPLICABLE",
		OptionDataRequired:         opts != nil,
		UnderlyingBars:             summarizeBarProvenance(bars),
		OptionSnapshotSources:      []string{},
		TotalBarCount:              len(signals),
		MissingRequiredFieldCounts: counts,
	}
	for _, signal := range signals {
		if strings.EqualFold(signal.CapabilityStatus, wheel.CapabilityDataBlocked) {
			q.BlockedBarCount++
		} else {
			q.ReadyBarCount++
		}
	}
	if q.TotalBarCount > 0 {
		ratio := float64(q.ReadyBarCount) / float64(q.TotalBarCount)
		q.ValidCoverageRatio = &ratio
	}
	if opts == nil {
		return q
	}
	batches := optionQuoteBatches(opts)
	cycleComplete := hkexHistoricalOptionCycleComplete(batches)
	q.HistoricalOptionCycleComplete = &cycleComplete
	q.Status = wheel.CapabilityDataBlocked
	q.SnapshotBatchCount = len(batches)
	blocked := map[string]struct{}{}
	if !cycleComplete {
		blocked["historical_option_cycle"] = struct{}{}
	}
	if len(batches) == 0 {
		blocked["option_quote_snapshots"] = struct{}{}
	}
	for _, batch := range batches {
		q.SnapshotContractRowCount += len(batch.Quotes)
		for _, quote := range batch.Quotes {
			if source := strings.TrimSpace(quote.Source); source != "" {
				q.OptionSnapshotSources = append(q.OptionSnapshotSources, source)
			}
			countMissingSnapshotFields(counts, batch, quote)
		}
	}
	q.OptionSnapshotSources = sortedUnique(q.OptionSnapshotSources)
	for _, signal := range signals {
		for _, reason := range signal.BlockedBy {
			if reason != "" {
				blocked[reason] = struct{}{}
			}
		}
	}
	for field, count := range counts {
		if count > 0 {
			blocked["missing_"+field] = struct{}{}
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

// hkexHistoricalOptionCycleComplete proves at least one contract was observed
// from DTE >=10 through DTE <=1 in complete source=hkex EOD batches. This is
// the narrow gate needed by the Wheel's DTE research window (5..MaxWheelDTE);
// it does not claim event-level fills or broker assignment evidence.
func hkexHistoricalOptionCycleComplete(batches []QuoteSnapshotBatch) bool {
	type coverage struct {
		first, last time.Time
		expiry      time.Time
	}
	byContract := make(map[string]coverage)
	for _, batch := range batches {
		observed := utcDate(batch.ObservedAt)
		if observed.IsZero() {
			continue
		}
		for _, quote := range batch.Quotes {
			if !strings.EqualFold(strings.TrimSpace(quote.Source), "hkex") || quote.Expiry.IsZero() {
				continue
			}
			name := firstQuoteName(quote)
			if name == "" {
				continue
			}
			c := byContract[name]
			if c.first.IsZero() || observed.Before(c.first) {
				c.first = observed
			}
			if c.last.IsZero() || observed.After(c.last) {
				c.last = observed
			}
			c.expiry = utcDate(quote.Expiry)
			byContract[name] = c
		}
	}
	for _, c := range byContract {
		lead := int(c.expiry.Sub(c.first) / (24 * time.Hour))
		tail := int(c.expiry.Sub(c.last) / (24 * time.Hour))
		if lead >= 10 && tail >= 0 && tail <= 1 {
			return true
		}
	}
	return false
}

func utcDate(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func summarizeBarProvenance(bars []ingest.Bar) []BarProvenance {
	type key struct{ source, adjusted string }
	counts := make(map[key]int)
	for _, bar := range bars {
		source := strings.TrimSpace(bar.Source)
		if source == "" {
			continue
		}
		adjusted := strings.TrimSpace(bar.Adjusted)
		if adjusted == "" {
			adjusted = "unspecified"
		}
		counts[key{source: source, adjusted: adjusted}]++
	}
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

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func optionQuoteBatches(opts *OptionsData) []QuoteSnapshotBatch {
	if opts == nil {
		return nil
	}
	if len(opts.QuoteBatches) > 0 {
		return opts.QuoteBatches
	}
	if len(opts.Snapshots) > 0 {
		return opts.Snapshots
	}
	return opts.QuoteSnapshots
}

func countMissingSnapshotFields(counts map[string]int64, batch QuoteSnapshotBatch, q wheel.OptionQuote) {
	if batch.ObservedAt.IsZero() {
		counts["observed_at"]++
	}
	if strings.TrimSpace(batch.SnapshotKey) == "" {
		counts["snapshot_key"]++
	}
	if batch.UnderlyingPrice <= 0 {
		counts["underlying_price"]++
	}
	if strings.TrimSpace(batch.Underlying) == "" {
		counts["underlying"]++
	}
	if strings.TrimSpace(firstQuoteName(q)) == "" {
		counts["symbol"]++
	}
	optionType := q.OptionType
	if optionType == "" {
		optionType = q.Type
	}
	if optionType != wheel.Call && optionType != wheel.Put {
		counts["option_type"]++
	}
	if q.Expiry.IsZero() {
		counts["expiry"]++
	}
	if q.Strike <= 0 {
		counts["strike"]++
	}
	if q.Delta == 0 && q.MarketDelta == 0 {
		counts["delta"]++
	}
	if q.Bid <= 0 {
		counts["bid"]++
	}
	if q.Ask <= 0 {
		counts["ask"]++
	}
	if q.ImpliedVol <= 0 && q.IV <= 0 {
		counts["implied_vol"]++
	}
	if q.Theta == nil {
		counts["theta"]++
	}
	if q.Volume <= 0 {
		counts["volume"]++
	}
	if q.OpenInterest <= 0 && q.OI <= 0 {
		counts["open_interest"]++
	}
	if q.LotSize <= 0 {
		counts["lot_size"]++
	}
	if strings.TrimSpace(q.Source) == "" {
		counts["source"]++
	}
}

func firstQuoteName(q wheel.OptionQuote) string {
	if q.Symbol != "" {
		return q.Symbol
	}
	return q.Code
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func applyStockPosition(st *State, delta, price float64) {
	old := st.Position
	next := old + delta
	switch {
	case delta == 0:
		return
	case old == 0 || sameSign(old, delta):
		st.StockAverageCost = (math.Abs(old)*st.StockAverageCost + math.Abs(delta)*price) / math.Abs(next)
	case math.Abs(delta) < math.Abs(old):
		// Partial close: the remaining shares keep their original basis.
	case math.Abs(delta) == math.Abs(old):
		st.StockAverageCost = 0
	default:
		// A trade crossing through zero opens the residual at this trade price.
		st.StockAverageCost = price
	}
	st.Position = next
}

func mergeOptionPosition(old, delta OptionPosition) OptionPosition {
	next := delta
	next.Contracts = old.Contracts + delta.Contracts
	switch {
	case next.Contracts == 0:
		next.AvgPremium = 0
	case sameSign(old.Contracts, delta.Contracts):
		next.AvgPremium = (math.Abs(old.Contracts)*old.AvgPremium + math.Abs(delta.Contracts)*delta.AvgPremium) / math.Abs(next.Contracts)
	case math.Abs(delta.Contracts) < math.Abs(old.Contracts):
		next.AvgPremium = old.AvgPremium
	default:
		next.AvgPremium = delta.AvgPremium
	}
	return next
}

func sameSign(a, b float64) bool { return (a > 0 && b > 0) || (a < 0 && b < 0) }

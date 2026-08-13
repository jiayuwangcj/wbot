package wheelrun

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// collectOptionQuotes grows the live quote set from ATM outwards. Positions
// are already filtered against the complete chain, so reducing this set only
// changes the data fetch, never the inventory calculation.
func (r *Runner) collectOptionQuotes(
	ctx context.Context,
	underlying string,
	contracts []futu.OptionContract,
	price float64,
	cfg wheel.Config,
	stockShares float64,
	positions []wheel.OptionPosition,
	now time.Time,
) ([]futu.OptionContract, map[string]futu.OptionQuoteEx, error) {
	quotes := make(map[string]futu.OptionQuoteEx)
	direction, needed := desiredOptionType(cfg, price, stockShares, positions)
	if !needed {
		return nil, quotes, nil
	}

	selected := make([]futu.OptionContract, 0)
	seen := make(map[string]struct{})
	for _, level := range atmExpansionLevels(contracts, price, maxATMExpansionRadius) {
		symbols := make([]string, 0, len(level))
		for _, contract := range level {
			// Only the required direction is fetched: the opposite leg is
			// rejected by Evaluate anyway, and every contract costs a
			// rate-limited greeks request (2026-08-12: 42 mixed legs ≈ 4min).
			if contract.Symbol == "" || !strings.EqualFold(string(contract.OptionType), string(direction)) {
				continue
			}
			if _, ok := seen[contract.Symbol]; ok {
				continue
			}
			seen[contract.Symbol] = struct{}{}
			selected = append(selected, contract)
			symbols = append(symbols, contract.Symbol)
		}
		if len(symbols) == 0 {
			continue
		}
		page, err := r.deps.Quoter.OptionQuotes(ctx, symbols)
		if err != nil {
			return nil, nil, err
		}
		for key, quote := range page {
			if quote.Symbol == "" {
				quote.Symbol = key
			}
			quotes[key] = quote
			quotes[quote.Symbol] = quote
		}
		quoteAsOf := r.now()
		if quoteAsOf.IsZero() {
			quoteAsOf = now
		}
		if qualityCandidateCount(underlying, selected, quotes, direction, cfg, quoteAsOf) >= minQualityCandidates {
			break
		}
	}
	return selected, quotes, nil
}

// desiredOptionType mirrors only the direction prerequisite already computed
// by wheel.Evaluate. Evaluate remains the sole authority for the final signal,
// quantity, candidate rejection reasons, and risk checks.
func desiredOptionType(cfg wheel.Config, price, stockShares float64, positions []wheel.OptionPosition) (wheel.OptionType, bool) {
	target, err := cfg.TargetInventory(price)
	if err != nil {
		return "", false
	}
	inv := wheel.CalculateInventory(stockShares, 0, positions, wheel.DefaultLotSize)
	gap := target - inv.EffectiveInventory
	if math.Abs(gap) <= cfg.NoTradeGap {
		return "", false
	}
	if gap < 0 {
		return wheel.Call, true
	}
	return wheel.Put, true
}

func qualityCandidateCount(underlying string, contracts []futu.OptionContract, quotes map[string]futu.OptionQuoteEx, direction wheel.OptionType, cfg wheel.Config, asOf time.Time) int {
	count := 0
	for _, quote := range assembleQuotes(underlying, contracts, quotes) {
		if !strings.EqualFold(string(quote.OptionType), string(direction)) {
			continue
		}
		if quote.Validate(asOf, cfg) != nil || wheel.QualityScore(quote) < cfg.MinOptionQuality {
			continue
		}
		count++
	}
	return count
}

func (r *Runner) enqueueQuoteSnapshots(underlying string, price float64, contracts []futu.OptionContract, quotes map[string]futu.OptionQuoteEx, observedAt time.Time) {
	if r.snapshots == nil || len(contracts) == 0 {
		return
	}
	records := makeQuoteSnapshotRecords(underlying, price, contracts, quotes, observedAt)
	r.snapshots.enqueue(snapshotBatch{underlying: underlying, records: records})
}

func makeQuoteSnapshotRecords(underlying string, price float64, contracts []futu.OptionContract, quotes map[string]futu.OptionQuoteEx, observedAt time.Time) []wheelstore.QuoteSnapshotRecord {
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	key := fmt.Sprintf("wheel-%s-%d", strings.ReplaceAll(underlying, ".", "_"), time.Now().UnixNano())
	records := make([]wheelstore.QuoteSnapshotRecord, 0, len(contracts))
	for _, contract := range contracts {
		quote := quotes[contract.Symbol]
		symbol := quote.Symbol
		if symbol == "" {
			symbol = contract.Symbol
		}
		lot := quote.LotSize
		if lot <= 0 {
			lot = contract.LotSize
		}
		optionType := strings.ToUpper(contract.OptionType)
		if optionType == "" {
			optionType = "CALL"
		}
		records = append(records, wheelstore.QuoteSnapshotRecord{
			Symbol:          symbol,
			Underlying:      underlying,
			OptionType:      optionType,
			Strike:          contract.Strike,
			Expiry:          contract.Expiry,
			Source:          "futu",
			SnapshotKey:     key,
			UnderlyingPrice: positiveFloat(price),
			Delta:           nonZeroFloat(quote.Delta),
			Bid:             positiveFloat(quote.Bid),
			Ask:             positiveFloat(quote.Ask),
			IV:              positiveFloat(quote.ImpliedVol),
			Theta:           copyFloat(quote.Theta),
			Volume:          positiveInt(quote.Volume),
			OpenInterest:    positiveInt(quote.OpenInterest),
			LotSize:         positiveInt(int64(lot)),
			ObservedAt:      observedAt,
		})
	}
	return records
}

func positiveFloat(v float64) *float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func nonZeroFloat(v float64) *float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func positiveInt(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func copyFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return nil
	}
	return &x
}

package backtest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// OptionsDataFromQuoteSnapshots converts trusted immutable Wheel observations
// to runner data. Rows are grouped only by (observed_at, snapshot_key), which
// preserves snapshot atomicity. Missing provider fields remain zero in the
// domain quote and are consequently rejected by wheel.Evaluate; no legacy
// option close is used as a substitute for a Greek or a market side.
func OptionsDataFromQuoteSnapshots(rows []wheelstore.QuoteSnapshotRecord) (*OptionsData, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("backtest: no option quote snapshots")
	}
	type batchKey struct {
		observed int64
		key      string
	}
	batches := make(map[batchKey]*QuoteSnapshotBatch)
	for _, r := range rows {
		if r.ObservedAt.IsZero() || strings.TrimSpace(r.SnapshotKey) == "" {
			return nil, fmt.Errorf("backtest: snapshot %s: observed_at and snapshot_key are required", r.Symbol)
		}
		k := batchKey{observed: r.ObservedAt.UTC().UnixNano(), key: r.SnapshotKey}
		b := batches[k]
		if b == nil {
			b = &QuoteSnapshotBatch{ObservedAt: r.ObservedAt.UTC(), SnapshotKey: r.SnapshotKey, Underlying: r.Underlying}
			if r.UnderlyingPrice != nil {
				b.UnderlyingPrice = *r.UnderlyingPrice
			}
			batches[k] = b
		} else if b.Underlying != r.Underlying {
			return nil, fmt.Errorf("backtest: snapshot %s: mixed underlyings in atomic batch", r.SnapshotKey)
		} else if r.UnderlyingPrice != nil && b.UnderlyingPrice != 0 && *r.UnderlyingPrice != b.UnderlyingPrice {
			return nil, fmt.Errorf("backtest: snapshot %s: conflicting underlying prices", r.SnapshotKey)
		} else if b.UnderlyingPrice == 0 && r.UnderlyingPrice != nil {
			b.UnderlyingPrice = *r.UnderlyingPrice
		}
		if len(b.Quotes) > 0 && !strings.EqualFold(strings.TrimSpace(b.Quotes[0].Source), strings.TrimSpace(r.Source)) {
			return nil, fmt.Errorf("backtest: snapshot %s: mixed sources in atomic batch", r.SnapshotKey)
		}
		q := optionQuoteFromSnapshot(r)
		b.Quotes = append(b.Quotes, q)
	}
	out := make([]QuoteSnapshotBatch, 0, len(batches))
	for _, b := range batches {
		sort.SliceStable(b.Quotes, func(i, j int) bool { return quoteName(b.Quotes[i]) < quoteName(b.Quotes[j]) })
		out = append(out, *b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ObservedAt.Equal(out[j].ObservedAt) {
			return out[i].ObservedAt.Before(out[j].ObservedAt)
		}
		return out[i].SnapshotKey < out[j].SnapshotKey
	})
	data := &OptionsData{Chain: OptionChain{}, Bars: OptionBars{}, QuoteBatches: out, Snapshots: out, QuoteSnapshots: out}
	for _, b := range out {
		for _, q := range b.Quotes {
			name := quoteName(q)
			kind := OptionKind(strings.ToLower(string(q.OptionType)))
			if kind != OptionCall && kind != OptionPut {
				continue
			}
			data.Chain[name] = OptionContract{Code: name, Kind: kind, Strike: q.Strike, Expiry: q.Expiry}
			// The adapter defines the snapshot mark semantics. Live providers
			// supply an observed bid; source=hkex supplies the explicitly
			// RESEARCH_ONLY EOD settlement projection documented in BACKTEST.md.
			// Neither path falls back to a legacy option OHLC close here.
			if q.Bid > 0 {
				data.Bars[name] = append(data.Bars[name], ingest.Bar{Ts: b.ObservedAt, Open: q.Bid, High: q.Bid, Low: q.Bid, Close: q.Bid, Volume: q.Volume})
			}
		}
	}
	return data, nil
}

// OptionsDataFromSnapshots is the short form used by backtest callers.
func OptionsDataFromSnapshots(rows []wheelstore.QuoteSnapshotRecord) (*OptionsData, error) {
	return OptionsDataFromQuoteSnapshots(rows)
}

func quoteName(q wheel.OptionQuote) string {
	if q.Symbol != "" {
		return q.Symbol
	}
	return q.Code
}

func optionQuoteFromSnapshot(r wheelstore.QuoteSnapshotRecord) wheel.OptionQuote {
	q := wheel.OptionQuote{
		Symbol: r.Symbol, Code: r.Symbol, Underlying: r.Underlying, Source: r.Source,
		OptionType: wheel.OptionType(strings.ToLower(r.OptionType)), Type: wheel.OptionType(strings.ToLower(r.OptionType)),
		Expiry: r.Expiry, Strike: r.Strike,
		QuoteTime: r.ObservedAt.UTC(), CapturedAt: r.ObservedAt.UTC(), Timestamp: r.ObservedAt.UTC(), Ts: r.ObservedAt.UTC(),
	}
	if r.Delta != nil {
		q.Delta = *r.Delta
		q.MarketDelta = *r.Delta
	}
	if r.Bid != nil {
		q.Bid = *r.Bid
	}
	if r.Ask != nil {
		q.Ask = *r.Ask
	}
	if r.IV != nil {
		q.ImpliedVol = *r.IV
		q.IV = *r.IV
	}
	q.Theta = r.Theta
	if r.Volume != nil {
		q.Volume = *r.Volume
	}
	if r.OpenInterest != nil {
		q.OpenInterest = *r.OpenInterest
		q.OI = *r.OpenInterest
	}
	if r.LotSize != nil {
		q.LotSize = int(*r.LotSize)
	}
	return q
}

// OptionsDataFromQuotes builds the runner's option universe from option_quotes
// rows: chain metadata from the first row per contract, bars per contract in ts
// order (rows are expected symbol+ts ascending, like QueryOptionQuotes).
func OptionsDataFromQuotes(rows []ingest.OptionQuoteRow) (*OptionsData, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("backtest: no option quote rows")
	}
	data := &OptionsData{Chain: OptionChain{}, Bars: OptionBars{}}
	seen := map[string]bool{}
	for _, r := range rows {
		kind := OptionKind(r.OptionType)
		if kind != OptionCall && kind != OptionPut {
			return nil, fmt.Errorf("backtest: option %s: unknown option_type %q", r.Symbol, r.OptionType)
		}
		if !seen[r.Symbol] {
			seen[r.Symbol] = true
			data.Chain[r.Symbol] = OptionContract{Code: r.Symbol, Kind: kind, Strike: r.Strike, Expiry: r.Expiry}
		}
		data.Bars[r.Symbol] = append(data.Bars[r.Symbol], ingest.Bar{
			Ts: r.Ts, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return data, nil
}

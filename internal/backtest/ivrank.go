package backtest

import (
	"sort"
	"time"

	"github.com/jiayu/wbot/internal/wheel"
)

// IVRankWindow is the trailing history used for the underlying IV percentile
// (industry: IV Rank over one year). Exporting it lets callers widen their
// snapshot query by the same window (backtestexec.quoteRangeStart).
const IVRankWindow = 365 * 24 * time.Hour

// minIVRankObservations is the minimum window size before a rank is trusted;
// with fewer observations the rank is unknown (0) and min_iv_rank masks.
const minIVRankObservations = 20

// attachIVRanks computes each batch's underlying IV rank in place: the
// percentile of the batch's median IV within the same underlying's trailing
// one-year history (rank = fraction of window observations strictly below
// today's IV). Strict comparison keeps a flat IV plateau at rank 0 instead of
// 1.0 — a constant low-IV regime must not sail through a min_iv_rank gate.
// batches must be sorted ascending by ObservedAt. Deterministic: medians and
// ranks depend only on the input rows.
func attachIVRanks(batches []QuoteSnapshotBatch) {
	groups := map[string][]int{}
	order := make([]string, 0, len(batches))
	for i := range batches {
		u := batches[i].Underlying
		if _, ok := groups[u]; !ok {
			order = append(order, u)
		}
		groups[u] = append(groups[u], i)
	}
	for _, u := range order {
		idxs := groups[u]
		medians := make([]float64, len(idxs))
		for i, idx := range idxs {
			medians[i] = medianIV(batches[idx].Quotes)
		}
		for i, idx := range idxs {
			windowStart := batches[idx].ObservedAt.Add(-IVRankWindow)
			var window, below float64
			for j := 0; j <= i; j++ {
				if batches[idxs[j]].ObservedAt.Before(windowStart) {
					continue
				}
				window++
				if medians[j] < medians[i] {
					below++
				}
			}
			if window >= minIVRankObservations {
				batches[idx].IVRank = below / window
			}
		}
	}
}

// medianIV returns the median positive implied volatility across the batch's
// quotes; 0 when the batch carries no usable IV.
func medianIV(quotes []wheel.OptionQuote) float64 {
	values := make([]float64, 0, len(quotes))
	for _, q := range quotes {
		iv := q.ImpliedVol
		if iv == 0 {
			iv = q.IV
		}
		if iv > 0 {
			values = append(values, iv)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	return values[len(values)/2]
}

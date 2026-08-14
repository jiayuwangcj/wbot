package backtest

import (
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/wheel"
)

func ivBatch(ts time.Time, underlying string, ivs []float64) QuoteSnapshotBatch {
	b := QuoteSnapshotBatch{ObservedAt: ts, Underlying: underlying, SnapshotKey: ts.Format("2006-01-02")}
	for _, iv := range ivs {
		theta := -0.10
		b.Quotes = append(b.Quotes, wheel.OptionQuote{
			Symbol: "Q", Source: "test", OptionType: "put", Strike: 90, Expiry: ts.AddDate(0, 0, 7),
			Delta: -0.30, Bid: 1, Ask: 1.10, ImpliedVol: iv, Theta: &theta,
			Volume: 100, OpenInterest: 500, LotSize: 100, QuoteTime: ts,
		})
	}
	return b
}

func TestAttachIVRanksPercentile(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var batches []QuoteSnapshotBatch
	for day := 0; day < 21; day++ {
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "U.US", []float64{0.10 + 0.01*float64(day)}))
	}
	// A low-IV day after a rising run lands at the bottom of the 22-day window.
	batches = append(batches, ivBatch(base.Add(21*24*time.Hour), "U.US", []float64{0.05}))
	attachIVRanks(batches)
	if got := batches[0].IVRank; got != 0 {
		t.Fatalf("first batch rank = %v; want 0 (history insufficient)", got)
	}
	if got, want := batches[20].IVRank, 20.0/21.0; got != want { // strictly below 0.30: 20 observations
		t.Fatalf("day-20 rank = %v; want %v", got, want)
	}
	if got := batches[21].IVRank; got != 0 { // nothing strictly below 0.05
		t.Fatalf("lowest-IV day rank = %v; want 0 (strict percentile)", got)
	}
}

func TestAttachIVRanksFlatPlateauStaysAtZero(t *testing.T) {
	// Constant IV: strict percentile means no observation is strictly below
	// today's, so the rank must be 0 — a flat low-IV regime cannot cross a
	// min_iv_rank gate by being its own 100th percentile.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var batches []QuoteSnapshotBatch
	for day := 0; day < 21; day++ {
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "U.US", []float64{0.20}))
	}
	attachIVRanks(batches)
	for i, b := range batches {
		if b.IVRank != 0 {
			t.Fatalf("flat plateau batch %d rank = %v; want 0", i, b.IVRank)
		}
	}
}

func TestAttachIVRanksPlateauThenRise(t *testing.T) {
	// 19-day plateau at 0.10 then a rise: the plateau days stay at rank 0
	// (nothing strictly below), the first rise day ranks against the plateau.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var batches []QuoteSnapshotBatch
	for day := 0; day < 19; day++ {
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "U.US", []float64{0.10}))
	}
	batches = append(batches, ivBatch(base.Add(19*24*time.Hour), "U.US", []float64{0.20}))
	batches = append(batches, ivBatch(base.Add(20*24*time.Hour), "U.US", []float64{0.30}))
	attachIVRanks(batches)
	if got := batches[18].IVRank; got != 0 {
		t.Fatalf("plateau day rank = %v; want 0", got)
	}
	if got, want := batches[19].IVRank, 19.0/20.0; got != want { // 19 plateau days strictly below 0.20
		t.Fatalf("first rise day rank = %v; want %v", got, want)
	}
	if got, want := batches[20].IVRank, 20.0/21.0; got != want { // plateau + 0.20 strictly below 0.30
		t.Fatalf("second rise day rank = %v; want %v", got, want)
	}
}

func TestAttachIVRanksInsufficientHistoryStaysZero(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var batches []QuoteSnapshotBatch
	for day := 0; day < minIVRankObservations-1; day++ {
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "U.US", []float64{0.20}))
	}
	attachIVRanks(batches)
	for i, b := range batches {
		if b.IVRank != 0 {
			t.Fatalf("batch %d rank = %v; want 0 (window %d < %d)", i, b.IVRank, i+1, minIVRankObservations)
		}
	}
}

func TestAttachIVRanksIsolatesUnderlyings(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var batches []QuoteSnapshotBatch
	for day := 0; day < 21; day++ {
		// A.US climbs, B.US falls: ranks must never mix windows.
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "A.US", []float64{0.10 + 0.01*float64(day)}))
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "B.US", []float64{0.50 - 0.01*float64(day)}))
	}
	attachIVRanks(batches)
	last := batches[len(batches)-1] // day 20, interleaved: B.US comes second
	var aNewest, bNewest float64
	for i := len(batches) - 2; i < len(batches); i++ {
		if batches[i].Underlying == "A.US" {
			aNewest = batches[i].IVRank
		}
	}
	bNewest = last.IVRank
	if got, want := aNewest, 20.0/21.0; got != want {
		t.Fatalf("A newest rank = %v; want %v (strict percentile)", got, want)
	}
	if bNewest != 0 { // B falls: nothing strictly below its own 0.30
		t.Fatalf("B newest rank = %v; want 0", bNewest)
	}
}

func TestAttachIVRanksSkipsBatchesWithoutIV(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var batches []QuoteSnapshotBatch
	for day := 0; day < 21; day++ {
		ivs := []float64{0.20}
		if day == 10 {
			ivs = []float64{0} // no usable IV → median 0
		}
		batches = append(batches, ivBatch(base.Add(time.Duration(day)*24*time.Hour), "U.US", ivs))
	}
	attachIVRanks(batches)
	if got := batches[10].IVRank; got != 0 { // 11-batch window < minIVRankObservations
		t.Fatalf("IV-less batch rank = %v; want 0 (window too small)", got)
	}
	if got, want := batches[20].IVRank, 1.0/21.0; got != want { // only the 0-valued batch strictly below 0.20
		t.Fatalf("newest batch rank = %v; want %v (IV-less observation counts as 0)", got, want)
	}
}

package strategy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// TestTraceCandidatesGating verifies the memory contract: with the default
// TraceCandidates=false the expiry-rejected candidate details are not
// materialized (CandidateDetails stays empty), while trade decisions, realized
// P&L and the equity curve are byte-identical to a TraceCandidates=true run.
// The full-chain audit remains deterministically re-fetchable by re-running
// with the flag (doc/BACKTEST.md).
func TestTraceCandidatesGating(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var rows []wheelstore.QuoteSnapshotRecord
	var bars []ingest.Bar
	for day := 0; day < 3; day++ {
		bid := 2.0
		r := snapshotRow(ts.AddDate(0, 0, day), &bid)
		rows = append(rows, r)
		bars = append(bars, testBar(ts.AddDate(0, 0, day)))
	}
	// One out-of-DTE contract: restoreExpiryRejectedCandidates would add it as
	// an expiry-rejected candidate under tracing, and omit it otherwise.
	outExpiry := ts.AddDate(0, 0, 40)
	bid := 2.0
	late := snapshotRow(ts, &bid)
	late.Symbol = "P130"
	late.Expiry = outExpiry
	rows = append(rows, late)

	data, err := backtest.OptionsDataFromQuoteSnapshots(rows)
	if err != nil {
		t.Fatal(err)
	}

	run := func(trace bool) (*backtest.Result, error) {
		s := &WheelStrategy{Config: wheelBacktestConfig(), TraceCandidates: trace}
		return backtest.RunOptions(context.Background(), bars, 20000, 0, s, data)
	}

	off, err := run(false)
	if err != nil {
		t.Fatal(err)
	}
	on, err := run(true)
	if err != nil {
		t.Fatal(err)
	}

	// Decisions, realized P&L and equity must not depend on tracing.
	if len(off.Trades) != len(on.Trades) {
		t.Fatalf("trace off trades=%d on trades=%d; decisions must match", len(off.Trades), len(on.Trades))
	}
	if off.RealizedReturnAmount != on.RealizedReturnAmount || off.Equity != on.Equity {
		t.Fatalf("off realized=%v final=%v; on realized=%v final=%v", off.RealizedReturnAmount, off.Equity, on.RealizedReturnAmount, on.Equity)
	}
	if len(off.EquityCurve) != len(on.EquityCurve) {
		t.Fatalf("curve lengths differ: off=%d on=%d", len(off.EquityCurve), len(on.EquityCurve))
	}
	for i := range off.EquityCurve {
		if off.EquityCurve[i].Equity != on.EquityCurve[i].Equity {
			t.Fatalf("curve[%d] equity off=%v on=%v", i, off.EquityCurve[i].Equity, on.EquityCurve[i].Equity)
		}
	}

	// Default off: signals keep only the pruned accepted candidate(s), not the
	// full expiry-restored chain. The memory win is the ~300-contract chain per
	// bar staying out of CandidateDetails, not an empty signal trace.
	if len(off.Signals) != len(on.Signals) {
		t.Fatalf("signal count differs: off=%d on=%d", len(off.Signals), len(on.Signals))
	}
	codes := func(s backtest.SignalTrace) map[string]bool {
		out := map[string]bool{}
		for _, c := range s.CandidateDetails {
			name := c.Quote.Symbol
			if name == "" {
				name = c.Quote.Code
			}
			out[name] = true
		}
		return out
	}
	// Default off: no candidate details resident per signal (the memory win).
	for i := range off.Signals {
		if len(off.Signals[i].CandidateDetails) != 0 {
			t.Fatalf("trace off signal[%d] has %d candidate details; want none resident", i, len(off.Signals[i].CandidateDetails))
		}
	}
	// Tracing on: the audit trail must include the expiry-rejected contract.
	onCodes := codes(on.Signals[0])
	if !onCodes["P95"] || !onCodes["P130"] {
		t.Fatalf("trace on signal[0] candidates = %+v; want P95 accepted + P130 expiry-rejected", on.Signals[0].CandidateDetails)
	}

	// Deterministic re-fetch: two identical trace-on runs marshal identically.
	again, err := run(true)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(on.Signals)
	b, _ := json.Marshal(again.Signals)
	if string(a) != string(b) {
		t.Fatal("trace-on re-run is not byte-identical")
	}
}

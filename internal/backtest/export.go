package backtest

// Result export (draft 2026-08-02): one serializer shared by
// GET /v1/backtests/{id}/export and `wbot backtest -export` — json is the
// detail shape (roundtrip with GET /v1/backtests/{id}), csv the same rows as
// two sections (doc/API.md).

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DetailJSON is one run's full detail: summary fields + equity_curve/trades/signals
// (same shape as GET /v1/backtests/{id}); pre-004 rows (nil trace) come back
// as empty arrays for read compatibility.
type DetailJSON struct {
	ID          int64          `json:"id"`
	Strategy    string         `json:"strategy"`
	Symbol      string         `json:"symbol"`
	Params      map[string]any `json:"params"`
	Metrics     map[string]any `json:"metrics"`
	StartTs     string         `json:"start_ts"`
	EndTs       string         `json:"end_ts"`
	CreatedAt   string         `json:"created_at"`
	EquityCurve []EquityPoint  `json:"equity_curve"`
	Trades      []Trade        `json:"trades"`
	Signals     []SignalTrace  `json:"signals"`
}

// Detail converts one record to the export/detail shape: RFC3339 timestamps,
// nil params/metrics normalized to {}, nil trace to empty arrays.
func Detail(r ResultRecord) DetailJSON {
	if r.Params == nil {
		r.Params = map[string]any{}
	}
	if r.Metrics == nil {
		r.Metrics = map[string]any{}
	}
	d := DetailJSON{
		ID: r.ID, Strategy: r.Strategy, Symbol: r.Symbol,
		Params: r.Params, Metrics: r.Metrics,
		StartTs: r.StartTs.UTC().Format(time.RFC3339), EndTs: r.EndTs.UTC().Format(time.RFC3339),
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
	d.EquityCurve = make([]EquityPoint, len(r.EquityCurve))
	for i, point := range r.EquityCurve {
		point.Ts = point.Ts.UTC()
		d.EquityCurve[i] = point
	}
	d.Trades = make([]Trade, len(r.Trades))
	for i, trade := range r.Trades {
		trade.Ts = trade.Ts.UTC()
		d.Trades[i] = trade
	}
	d.Signals = make([]SignalTrace, len(r.Signals))
	for i, signal := range r.Signals {
		signal.Ts = signal.Ts.UTC()
		if signal.BlockedBy == nil {
			signal.BlockedBy = []string{}
		}
		if signal.SnapshotObservedAt != nil {
			observedAt := signal.SnapshotObservedAt.UTC()
			signal.SnapshotObservedAt = &observedAt
		}
		d.Signals[i] = signal
	}
	return d
}

// Export serializes one run in the given format (csv or json) and returns the
// exact response body with its Content-Type; the json body ends with a newline
// so it is byte-identical to the detail endpoint's (roundtrip contract).
// Unknown format is an error (api contract: format=csv|json, default csv).
func Export(r ResultRecord, format string) ([]byte, string, error) {
	switch format {
	case "json":
		b, err := json.Marshal(Detail(r))
		if err != nil {
			return nil, "", err
		}
		return append(b, '\n'), "application/json", nil
	case "csv":
		return ExportCSV(r), "text/csv; charset=utf-8", nil
	default:
		return nil, "", fmt.Errorf("backtest: export: unsupported format %q (want csv or json)", format)
	}
}

// ExportCSV renders one run as blank-line-separated sections, each with a
// section name line and a header row; rows mirror the JSON arrays 1:1
// (RFC3339 ts, shortest float form).
func ExportCSV(r ResultRecord) []byte {
	detail := Detail(r)
	var b strings.Builder
	cw := csv.NewWriter(&b)
	cw.Write([]string{"equity_curve"})
	cw.Write([]string{"ts", "equity"})
	for _, p := range detail.EquityCurve {
		cw.Write([]string{p.Ts.Format(time.RFC3339), strconv.FormatFloat(p.Equity, 'g', -1, 64)})
	}
	cw.Flush()
	b.WriteString("\n")
	cw.Write([]string{"trades"})
	cw.Write([]string{"ts", "action", "symbol", "size", "price", "cash_after"})
	for _, tr := range detail.Trades {
		cw.Write([]string{
			tr.Ts.Format(time.RFC3339),
			tr.Action,
			tr.Symbol,
			strconv.FormatFloat(tr.Size, 'g', -1, 64),
			strconv.FormatFloat(tr.Price, 'g', -1, 64),
			strconv.FormatFloat(tr.CashAfter, 'g', -1, 64),
		})
	}
	cw.Flush()
	b.WriteString("\n")
	cw.Write([]string{"signals"})
	cw.Write([]string{"ts", "action", "direction", "reason", "capability_status", "blocked_by", "snapshot_key", "snapshot_observed_at", "actual_inventory", "effective_inventory", "option_delta_stock", "candidate_code", "quantity", "candidates"})
	for _, signal := range detail.Signals {
		candidates, _ := json.Marshal(signal.Candidates)
		blockedBy, _ := json.Marshal(signal.BlockedBy)
		observedAt := ""
		if signal.SnapshotObservedAt != nil {
			observedAt = signal.SnapshotObservedAt.Format(time.RFC3339)
		}
		cw.Write([]string{
			signal.Ts.Format(time.RFC3339),
			signal.Action,
			signal.Direction,
			signal.Reason,
			signal.CapabilityStatus,
			string(blockedBy),
			signal.SnapshotKey,
			observedAt,
			strconv.FormatFloat(signal.ActualInventory, 'g', -1, 64),
			strconv.FormatFloat(signal.EffectiveInventory, 'g', -1, 64),
			strconv.FormatFloat(signal.OptionDeltaStock, 'g', -1, 64),
			signal.CandidateCode,
			strconv.FormatFloat(signal.Quantity, 'g', -1, 64),
			string(candidates),
		})
	}
	cw.Flush()
	return []byte(b.String())
}

package backtest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func exportSample() ResultRecord {
	ts := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return ResultRecord{
		ID: 3, Strategy: "buy-hold", Symbol: "DEMO.US",
		Params:    map[string]any{"cash": 10000.0, "fee": 0.0},
		Metrics:   map[string]any{"equity": 10500.0, "bars": 2},
		StartTs:   ts,
		EndTs:     ts.Add(24 * time.Hour),
		CreatedAt: ts.Add(48 * time.Hour),
		EquityCurve: []EquityPoint{
			{Ts: ts, Equity: 10000},
			{Ts: ts.Add(24 * time.Hour), Equity: 10500},
		},
		Trades: []Trade{{Ts: ts, Action: "buy", Symbol: "DEMO.US", Size: 100, Price: 100, CashAfter: 0}},
	}
}

func TestDetailShape(t *testing.T) {
	d := Detail(exportSample())
	if d.ID != 3 || d.Strategy != "buy-hold" || d.StartTs != "2026-08-01T00:00:00Z" || d.CreatedAt != "2026-08-03T00:00:00Z" {
		t.Fatalf("detail = %+v; want id 3 buy-hold with RFC3339 timestamps", d)
	}
	if len(d.EquityCurve) != 2 || len(d.Trades) != 1 {
		t.Fatalf("detail trace = (%d curve, %d trades); want (2, 1)", len(d.EquityCurve), len(d.Trades))
	}
}

func TestDetailNilTraceStaysReadable(t *testing.T) {
	r := exportSample()
	r.EquityCurve, r.Trades = nil, nil
	d := Detail(r)
	if d.EquityCurve == nil || d.Trades == nil || len(d.EquityCurve) != 0 || len(d.Trades) != 0 {
		t.Fatalf("nil trace = %+v; want empty arrays", d.EquityCurve)
	}
}

func TestExportCSVSections(t *testing.T) {
	want := "equity_curve\nts,equity\n2026-08-01T00:00:00Z,10000\n2026-08-02T00:00:00Z,10500\n\ntrades\nts,action,symbol,size,price,cash_after\n2026-08-01T00:00:00Z,buy,DEMO.US,100,100,0\n"
	if got := string(ExportCSV(exportSample())); got != want {
		t.Fatalf("csv = %q; want %q", got, want)
	}
}

func TestExportCSVEmptyCurve(t *testing.T) {
	r := exportSample()
	r.EquityCurve, r.Trades = nil, nil
	got := string(ExportCSV(r))
	if !strings.Contains(got, "equity_curve\nts,equity\n") || !strings.Contains(got, "trades\nts,action,symbol,size,price,cash_after\n") {
		t.Fatalf("empty-trace csv missing section headers: %q", got)
	}
	if strings.Count(got, "\n") != 5 {
		t.Fatalf("empty-trace csv has %d lines; want 5 (2 section names + 2 headers + blank)", strings.Count(got, "\n"))
	}
}

func TestExportFormats(t *testing.T) {
	r := exportSample()
	b, mime, err := Export(r, "json")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "application/json" {
		t.Fatalf("json mime = %q; want application/json", mime)
	}
	var d DetailJSON
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.ID != 3 || len(d.EquityCurve) != 2 {
		t.Fatalf("json export = %+v; want id 3 with 2 curve points", d)
	}
	if _, mime, err := Export(r, "csv"); err != nil || mime != "text/csv; charset=utf-8" {
		t.Fatalf("csv export: mime %q err %v", mime, err)
	}
	if _, _, err := Export(r, "xml"); err == nil {
		t.Fatal("Export(xml) = nil error; want unsupported format error")
	}
}

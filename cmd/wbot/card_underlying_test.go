package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeUnderlyingQuoter serves a canned quote page for the card's display-name
// lookup; err non-nil fails the fetch.
type fakeUnderlyingQuoter struct {
	raw json.RawMessage
	err error
}

func (f fakeUnderlyingQuoter) QuoteRaw(context.Context, string) (json.RawMessage, error) {
	return f.raw, f.err
}

func TestUnderlyingName(t *testing.T) {
	ctx := context.Background()
	raw := json.RawMessage(`{"basic_qot_list":[{"name":"腾讯控股","cur_price":480.2}]}`)

	if got := underlyingName(ctx, fakeUnderlyingQuoter{raw: raw}, "HK.00700"); got != "腾讯控股" {
		t.Errorf("underlyingName = %q, want 腾讯控股", got)
	}
	// Failed fetch degrades to "" so the card never blocks on a stalled gateway.
	if got := underlyingName(ctx, fakeUnderlyingQuoter{err: errors.New("down")}, "HK.00700"); got != "" {
		t.Errorf("underlyingName on fetch error = %q, want empty", got)
	}
	// Unparseable body degrades the same way.
	if got := underlyingName(ctx, fakeUnderlyingQuoter{raw: json.RawMessage(`not-json`)}, "HK.00700"); got != "" {
		t.Errorf("underlyingName on bad body = %q, want empty", got)
	}
	// Empty list degrades.
	if got := underlyingName(ctx, fakeUnderlyingQuoter{raw: json.RawMessage(`{"basic_qot_list":[]}`)}, "HK.00700"); got != "" {
		t.Errorf("underlyingName on empty list = %q, want empty", got)
	}
	// Nil quoter never panics.
	if got := underlyingName(ctx, nil, "HK.00700"); got != "" {
		t.Errorf("underlyingName with nil quoter = %q, want empty", got)
	}
}

func TestCodeFromSymbol(t *testing.T) {
	for in, want := range map[string]string{
		"HK.00700": "00700",
		"US.AAPL":  "AAPL",
		"AAPL":     "AAPL",
	} {
		if got := codeFromSymbol(in); got != want {
			t.Errorf("codeFromSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnderlyingLabel(t *testing.T) {
	if got := underlyingLabel("腾讯控股", "HK.00700"); got != "腾讯控股 · 00700" {
		t.Errorf("underlyingLabel = %q, want 腾讯控股 · 00700", got)
	}
	// Name-less fallback keeps the code so the row is never empty.
	if got := underlyingLabel("", "HK.00700"); got != "00700" {
		t.Errorf("underlyingLabel empty name = %q, want 00700", got)
	}
	if got := underlyingLabel("   ", "US.AAPL"); got != "AAPL" {
		t.Errorf("underlyingLabel blank name = %q, want AAPL", got)
	}
}

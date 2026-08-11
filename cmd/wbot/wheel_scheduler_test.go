package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu"
)

func TestQualifySymbol(t *testing.T) {
	tests := []struct {
		name   string
		market int32
		code   string
		want   string
	}{
		{name: "HK", market: 1, code: "00700", want: "HK.00700"},
		{name: "US", market: 2, code: "AAPL", want: "US.AAPL"},
		{name: "SH", market: 3, code: "600000", want: "SH.600000"},
		{name: "SZ", market: 3, code: "000001", want: "SZ.000001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualifySymbol(tt.market, tt.code); got != tt.want {
				t.Fatalf("qualifySymbol(%d, %q) = %q; want %q", tt.market, tt.code, got, tt.want)
			}
		})
	}
}

func TestParseWheelEnv(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  futu.Env
		ok    bool
	}{
		{name: "empty defaults to sim", input: "", want: futu.EnvSim, ok: true},
		{name: "sim", input: "sim", want: futu.EnvSim, ok: true},
		{name: "simulate with spaces", input: " Simulate ", want: futu.EnvSim, ok: true},
		{name: "real uppercase", input: "REAL", want: futu.EnvReal, ok: true},
		{name: "invalid", input: "paper", ok: false},
		{name: "whitespace invalid", input: "  ", want: futu.EnvSim, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWheelEnv(tt.input)
			if tt.ok {
				if err != nil || got != tt.want {
					t.Fatalf("parseWheelEnv(%q) = (%v, %v); want (%v, nil)", tt.input, got, err, tt.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "unknown -wheel-env") {
				t.Fatalf("parseWheelEnv(%q) error = %v; want unknown-environment error", tt.input, err)
			}
		})
	}
}

func TestFutuQuoterQuote(t *testing.T) {
	oldQuoteLimit, oldSnapshotLimit := futu.QuoteLimit, futu.SnapshotLimit
	futu.QuoteLimit = futu.NewLimiter(time.Microsecond)
	futu.SnapshotLimit = futu.NewLimiter(time.Microsecond)
	t.Cleanup(func() {
		futu.QuoteLimit, futu.SnapshotLimit = oldQuoteLimit, oldSnapshotLimit
	})

	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, `{"ret_type":0,"s2c":{"basic_qot_list":[{"last_price":475.2},{"last_price":999.9}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := (futuQuoter{client: futu.NewClient(srv.URL)}).Quote(context.Background(), "HK.00700")
	if err != nil {
		t.Fatalf("Quote() error: %v", err)
	}
	if got != 475.2 {
		t.Fatalf("Quote() = %v; want 475.2", got)
	}
	if strings.Join(paths, ",") != "/api/subscribe,/api/quote" {
		t.Fatalf("REST paths = %v; want subscribe then quote", paths)
	}
}

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu"
)

// fastFutuLimits shrinks the global futu rate pools for fast CLI tests.
func fastFutuLimits(t *testing.T) {
	t.Helper()
	oldQ, oldK, oldH, oldS, oldG := futu.QuoteLimit, futu.KlineLimit, futu.HistoryPageLimit, futu.SnapshotLimit, futu.BatchGap
	futu.QuoteLimit = futu.NewLimiter(time.Microsecond)
	futu.KlineLimit = futu.NewLimiter(time.Microsecond)
	futu.HistoryPageLimit = futu.NewLimiter(time.Microsecond)
	futu.SnapshotLimit = futu.NewLimiter(time.Microsecond)
	futu.BatchGap = time.Microsecond
	t.Cleanup(func() {
		futu.QuoteLimit, futu.KlineLimit, futu.HistoryPageLimit, futu.SnapshotLimit, futu.BatchGap = oldQ, oldK, oldH, oldS, oldG
	})
}

const ingestFutuBars = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
	{"time":"2026-07-30 00:00:00","is_blank":false,"high_price":475.0,"open_price":466.4,"low_price":462.8,"close_price":471.8,"volume":31791979,"timestamp":1785340800.0},
	{"time":"2026-07-31 00:00:00","is_blank":false,"high_price":479.8,"open_price":470.0,"low_price":462.0,"close_price":475.2,"volume":31100240,"timestamp":1785427200.0}
],"next_req_key":null}}`

func TestIngestFutuDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"help", []string{"wbot", "ingest", "futu", "-h"}, 0},
		{"no symbol", []string{"wbot", "ingest", "futu"}, 2},
		{"bad symbol", []string{"wbot", "ingest", "futu", "-symbol", "00700", "-timeframe", "K_DAY"}, 2},
		{"bad timeframe", []string{"wbot", "ingest", "futu", "-symbol", "HK.00700", "-timeframe", "K_3M"}, 2},
		{"bad from", []string{"wbot", "ingest", "futu", "-symbol", "HK.00700", "-timeframe", "K_DAY", "-from", "nope"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestIngestFutuDryRunSuccess(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history-kline" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, ingestFutuBars)
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "K_DAY", "-dry-run", "-addr", srv.URL})
	if code != 0 {
		t.Fatalf("run() = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"ingest futu: dry-run: 2 bars", "2026-07-29T16:00:00Z", "2026-07-30T16:00:00Z", "timeframe=1d"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dry-run output missing %q: %s", want, stdout)
		}
	}
}

func TestIngestFutuDryRunGatewayDown(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "K_DAY", "-dry-run", "-addr", addr})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "ingest futu:") {
		t.Fatalf("stderr = %q; want ingest futu: prefix", stderr)
	}
}

func TestIngestFutuUsageMentionsTimeframes(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	for _, want := range []string{"K_1M", "K_DAY", "K_MONTH", "-dry-run", "-every", "HK.00700"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("ingest futu usage missing %q: %s", want, stderr)
		}
	}
}

func TestIngestUsageListsFutu(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	if !strings.Contains(stderr, "futu") {
		t.Fatalf("ingest usage missing futu: %s", stderr)
	}
}

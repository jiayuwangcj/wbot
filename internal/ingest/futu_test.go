package ingest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
)

// mustReadAll drains the request body, failing the test on error.
func mustReadAll(r *http.Request) []byte {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	return body
}

// contains reports whether s contains substr.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// fastFutuLimits shrinks the global futu rate pools for fast unit tests.
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

const futuBarsPayload = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
	{"time":"2026-07-29 00:00:00","is_blank":false,"high_price":469.4,"open_price":453.0,"low_price":450.0,"close_price":466.4,"volume":36203193,"timestamp":1785254400.0},
	{"time":"2026-07-30 00:00:00","is_blank":true,"high_price":0.0,"open_price":0.0,"low_price":0.0,"close_price":0.0,"volume":0,"timestamp":1785340800.0},
	{"time":"2026-07-31 00:00:00","is_blank":false,"high_price":479.8,"open_price":470.0,"low_price":462.0,"close_price":475.2,"volume":31100240,"timestamp":1785427200.0}
],"next_req_key":null}}`

func TestFutuSourceBars(t *testing.T) {
	fastFutuLimits(t)
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history-kline" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("request body %q: %v", body, err)
		}
		io.WriteString(w, futuBarsPayload)
	}))
	defer srv.Close()

	src := FutuSource{Client: futu.NewClient(srv.URL)}
	from := time.Unix(1785330000, 0).UTC() // 2026-07-29 05:00:00Z: after the first bar
	to := time.Unix(1785513600, 0).UTC()   // 2026-08-01 00:00:00Z
	bars, err := src.Bars(context.Background(), domain.Symbol("HK.00700"), "1d", from, to)
	if err != nil {
		t.Fatalf("Bars() error: %v", err)
	}
	// blank bar skipped, out-of-range rows dropped, in-range kept
	if len(bars) != 1 {
		t.Fatalf("got %d bars; want 1 (blank + out-of-range dropped)", len(bars))
	}
	if !bars[0].Ts.Equal(time.Unix(1785427200, 0).UTC()) || bars[0].Close != 475.2 || bars[0].Volume != 31100240 {
		t.Fatalf("bar = %+v; want ts=1785427200 close=475.2 volume=31100240", bars[0])
	}
	if got["kl_type"] != float64(2) {
		t.Fatalf("request kl_type = %v; want 2 for timeframe 1d", got["kl_type"])
	}
}

func TestFutuSourceUnboundedRange(t *testing.T) {
	fastFutuLimits(t)
	var begin, end string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		if err := json.Unmarshal(mustReadAll(r), &m); err != nil {
			t.Fatalf("request body: %v", err)
		}
		begin, end = m["begin_time"].(string), m["end_time"].(string)
		io.WriteString(w, futuBarsPayload)
	}))
	defer srv.Close()

	src := FutuSource{Client: futu.NewClient(srv.URL)}
	if _, err := src.Bars(context.Background(), domain.Symbol("HK.00700"), "K_DAY", time.Time{}, time.Time{}); err != nil {
		t.Fatalf("Bars() error: %v", err)
	}
	if begin != "2000-01-01 08:00:00" {
		t.Fatalf("empty from begin_time = %q; want 2000-01-01 08:00:00", begin)
	}
	if end == "" {
		t.Fatal("empty to end_time = empty; want now+24h wall clock")
	}
}

func TestFutuSourceNilClient(t *testing.T) {
	src := FutuSource{}
	_, err := src.Bars(context.Background(), domain.Symbol("HK.00700"), "1d", time.Time{}, time.Time{})
	if err == nil || !contains(err.Error(), "nil client") {
		t.Fatalf("Bars() error = %v; want nil client", err)
	}
}

func TestFutuSourceBadTimeframe(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	src := FutuSource{Client: futu.NewClient(srv.URL)}
	_, err := src.Bars(context.Background(), domain.Symbol("HK.00700"), "K_3M", time.Time{}, time.Time{})
	if err == nil || !contains(err.Error(), "unsupported timeframe") {
		t.Fatalf("Bars() error = %v; want unsupported timeframe", err)
	}
}

func TestFutuSourceGatewayError(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"backend disconnected"}`)
	}))
	defer srv.Close()
	src := FutuSource{Client: futu.NewClient(srv.URL)}
	_, err := src.Bars(context.Background(), domain.Symbol("HK.00700"), "1d", time.Time{}, time.Time{})
	if err == nil || !contains(err.Error(), "ingest: futu source:") || !contains(err.Error(), "503") {
		t.Fatalf("Bars() error = %v; want ingest: futu source: with 503", err)
	}
}

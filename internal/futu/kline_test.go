package futu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fastLimits shrinks the global rate pools so unit tests run quickly.
func fastLimits(t *testing.T) {
	t.Helper()
	oldQ, oldK, oldH, oldS, oldG := QuoteLimit, KlineLimit, HistoryPageLimit, SnapshotLimit, BatchGap
	QuoteLimit = NewLimiter(time.Microsecond)
	KlineLimit = NewLimiter(time.Microsecond)
	HistoryPageLimit = NewLimiter(time.Microsecond)
	SnapshotLimit = NewLimiter(time.Microsecond)
	BatchGap = time.Microsecond
	t.Cleanup(func() {
		QuoteLimit, KlineLimit, HistoryPageLimit, SnapshotLimit, BatchGap = oldQ, oldK, oldH, oldS, oldG
	})
}

func TestParseTimeframe(t *testing.T) {
	tests := []struct {
		in       string
		klType   int
		ingestTF string
		wantErr  bool
	}{
		{"K_1M", 1, "1m", false},
		{"K_5M", 6, "5m", false},
		{"K_15M", 7, "15m", false},
		{"K_30M", 8, "30m", false},
		{"K_60M", 9, "60m", false},
		{"K_DAY", 2, "1d", false},
		{"K_WEEK", 3, "1w", false},
		{"K_MONTH", 4, "1mo", false},
		{"k_day", 2, "1d", false}, // case-insensitive
		{"1d", 2, "1d", false},    // ingest convention accepted as-is
		{"1m", 1, "1m", false},
		{"1mo", 4, "1mo", false},
		{"", 0, "", true},
		{"K_3M", 0, "", true},
		{"1h", 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			kl, tf, err := ParseTimeframe(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTimeframe(%q) = nil error; want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimeframe(%q) error: %v", tt.in, err)
			}
			if kl != tt.klType || tf != tt.ingestTF {
				t.Fatalf("ParseTimeframe(%q) = (%d, %q); want (%d, %q)", tt.in, kl, tf, tt.klType, tt.ingestTF)
			}
		})
	}
}

func klineHandler(t *testing.T, fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history-kline" {
			http.NotFound(w, r)
			return
		}
		fn(w, r)
	}))
}

func TestHistoryKlineSuccess(t *testing.T) {
	fastLimits(t)
	var got map[string]any
	srv := klineHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("request body %q: %v", body, err)
		}
		io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"security":{"market":1,"code":"00700"},"name":"TENCENT","kl_list":[
			{"time":"2026-07-30 00:00:00","is_blank":false,"high_price":475.0,"open_price":466.4,"low_price":462.8,"close_price":471.8,"volume":31791979,"timestamp":1785340800.0},
			{"time":"2026-07-31 00:00:00","is_blank":false,"high_price":479.8,"open_price":470.0,"low_price":462.0,"close_price":475.2,"volume":31100240,"timestamp":1785427200.0}
		],"next_req_key":null}}`)
	})
	defer srv.Close()

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	bars, err := NewClient(srv.URL).HistoryKline(context.Background(), "HK.00700", 2, from, to)
	if err != nil {
		t.Fatalf("HistoryKline() error: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("got %d bars; want 2", len(bars))
	}
	wantTS := time.Unix(1785427200, 0).UTC()
	if !bars[1].Ts.Equal(wantTS) || bars[1].Close != 475.2 || bars[1].High != 479.8 || bars[1].Volume != 31100240 {
		t.Fatalf("bar[1] = %+v; want ts=%v close=475.2", bars[1], wantTS)
	}
	if got["kl_type"] != float64(2) || got["rehab_type"] != float64(0) || got["max_count"] != float64(MaxKlinePage) {
		t.Fatalf("request body = %v; want kl_type=2 rehab_type=0 max_count=%d", got, MaxKlinePage)
	}
	if got["begin_time"] != "2026-07-01 08:00:00" || got["end_time"] != "2026-08-01 07:59:59" {
		t.Fatalf("begin/end = %v/%v; want +08 wall clock", got["begin_time"], got["end_time"])
	}
	sec := got["security"].(map[string]any)
	if sec["market"] != float64(1) || sec["code"] != "00700" {
		t.Fatalf("security = %v; want market=1 code=00700", sec)
	}
	if _, ok := got["next_req_key"]; ok {
		t.Fatalf("unexpected next_req_key in first request: %v", got)
	}
}

func TestHistoryKlinePagination(t *testing.T) {
	fastLimits(t)
	page := 0
	var reqs []map[string]any
	srv := klineHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("request body %q: %v", body, err)
		}
		reqs = append(reqs, m)
		if page == 0 {
			page++
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
				{"time":"2026-07-30 00:00:00","is_blank":false,"high_price":475.0,"open_price":466.4,"low_price":462.8,"close_price":471.8,"volume":1,"timestamp":1785340800.0}
			],"next_req_key":[1,2,3]}}`)
			return
		}
		io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
			{"time":"2026-07-31 00:00:00","is_blank":false,"high_price":479.8,"open_price":470.0,"low_price":462.0,"close_price":475.2,"volume":2,"timestamp":1785427200.0}
		],"next_req_key":null}}`)
	})
	defer srv.Close()

	bars, err := NewClient(srv.URL).HistoryKline(context.Background(), "HK.00700", 2, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("HistoryKline() error: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("got %d bars; want 2", len(bars))
	}
	if len(reqs) != 2 {
		t.Fatalf("got %d requests; want 2 pages", len(reqs))
	}
	if reqs[1]["next_req_key"] == nil {
		t.Fatalf("page 2 request missing next_req_key: %v", reqs[1])
	}
	if reqs[0]["begin_time"] != "2000-01-01 08:00:00" {
		t.Fatalf("empty-from default begin_time = %v; want 2000-01-01 08:00:00", reqs[0]["begin_time"])
	}
}

func TestHistoryKlineHTTPError(t *testing.T) {
	fastLimits(t)
	srv := klineHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"error":"backend down"}`)
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).HistoryKline(context.Background(), "HK.00700", 2, time.Time{}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "backend down") {
		t.Fatalf("HistoryKline() error = %v; want 502 backend down", err)
	}
}

func TestHistoryKlineBusinessError(t *testing.T) {
	fastLimits(t)
	srv := klineHandler(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ret_type":-1,"ret_msg":"no permission for market","err_code":null,"s2c":null}`)
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).HistoryKline(context.Background(), "HK.00700", 2, time.Time{}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "no permission for market") {
		t.Fatalf("HistoryKline() error = %v; want ret_msg surfaced", err)
	}
}

func TestHistoryKline429RetryThenSuccess(t *testing.T) {
	fastLimits(t)
	old := retryBackoff
	retryBackoff = []time.Duration{time.Microsecond, time.Microsecond}
	t.Cleanup(func() { retryBackoff = old })

	attempts := 0
	srv := klineHandler(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":"rate limited"}`)
			return
		}
		io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
			{"time":"2026-07-31 00:00:00","is_blank":false,"high_price":479.8,"open_price":470.0,"low_price":462.0,"close_price":475.2,"volume":31100240,"timestamp":1785427200.0}
		],"next_req_key":null}}`)
	})
	defer srv.Close()

	bars, err := NewClient(srv.URL).HistoryKline(context.Background(), "HK.00700", 2, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("HistoryKline() error: %v", err)
	}
	if len(bars) != 1 || attempts != 3 {
		t.Fatalf("got %d bars after %d attempts; want 1 bar after 3 attempts", len(bars), attempts)
	}
}

func TestHistoryKline429Exhausted(t *testing.T) {
	fastLimits(t)
	old := retryBackoff
	retryBackoff = []time.Duration{time.Microsecond, time.Microsecond}
	t.Cleanup(func() { retryBackoff = old })

	srv := klineHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"rate limited"}`)
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).HistoryKline(context.Background(), "HK.00700", 2, time.Time{}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("HistoryKline() error = %v; want 429 rate limited after retries", err)
	}
}

func TestHistoryKlineBadInput(t *testing.T) {
	fastLimits(t)
	c := NewClient("http://127.0.0.1:1")
	if _, err := c.HistoryKline(context.Background(), "00700", 2, time.Time{}, time.Time{}); err == nil {
		t.Fatal("HistoryKline(bad symbol) = nil error; want error")
	}
	if _, err := c.HistoryKline(context.Background(), "HK.00700", 0, time.Time{}, time.Time{}); err == nil {
		t.Fatal("HistoryKline(kl_type 0) = nil error; want error")
	}
}

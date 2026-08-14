package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
)

const tencentBarsPayload = `{"code":0,"msg":"","data":{"hk00700":{"day":[
  ["2026-08-07","479.000","478.800","483.200","475.400","16319939.000"],
  ["2026-08-10","479.000","481.400","483.600","476.400","15508724.000"]
]}}}`

func fastTencentRequests(t *testing.T) {
	t.Helper()
	oldLimit, oldBackoff := tencentRequestLimit, tencentRetryBackoff
	tencentRequestLimit = futu.NewLimiter(time.Microsecond)
	tencentRetryBackoff = []time.Duration{time.Microsecond, time.Microsecond}
	t.Cleanup(func() {
		tencentRequestLimit, tencentRetryBackoff = oldLimit, oldBackoff
	})
}

func TestParseTencentInstrument(t *testing.T) {
	tests := []struct {
		input, symbol, code, market string
	}{
		{"HK.00700", "HK.00700", "hk00700", "HK"},
		{"00700.HK", "HK.00700", "hk00700", "HK"},
		{"us.jd", "US.JD", "usJD", "US"},
		{"SH.600519", "SH.600519", "sh600519", "SH"},
	}
	for _, tt := range tests {
		got, err := ParseTencentInstrument(tt.input)
		if err != nil {
			t.Fatalf("ParseTencentInstrument(%q): %v", tt.input, err)
		}
		if string(got.Symbol) != tt.symbol || got.ProviderCode != tt.code || got.Market != tt.market {
			t.Fatalf("ParseTencentInstrument(%q) = %+v; want %s/%s/%s", tt.input, got, tt.symbol, tt.code, tt.market)
		}
	}
	if _, err := ParseTencentInstrument("00700"); err == nil || !strings.Contains(err.Error(), "bad symbol") {
		t.Fatalf("bad symbol error = %v", err)
	}
}

func TestTencentSourceBars(t *testing.T) {
	fastTencentRequests(t)
	var gotParam, gotAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParam = r.URL.Query().Get("param")
		gotAgent = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, tencentBarsPayload)
	}))
	defer srv.Close()

	src := TencentSource{Endpoint: srv.URL, Count: 2}
	bars, err := src.Bars(context.Background(), domain.Symbol("HK.00700"), "1d", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if gotParam != "hk00700,day,,,2,qfq" {
		t.Fatalf("param = %q", gotParam)
	}
	if gotAgent != "wbot/tencent-datafill" {
		t.Fatalf("user-agent = %q", gotAgent)
	}
	if len(bars) != 2 {
		t.Fatalf("bars = %d; want 2", len(bars))
	}
	wantTs := time.Date(2026, 8, 7, 0, 0, 0, 0, tencentDateLocation).UTC()
	if !bars[0].Ts.Equal(wantTs) || bars[0].Open != 479 || bars[0].Close != 478.8 || bars[0].High != 483.2 || bars[0].Low != 475.4 || bars[0].Volume != 16319939 {
		t.Fatalf("first bar = %+v; want Tencent field mapping at %v", bars[0], wantTs)
	}
	if err := ValidateBars(bars); err != nil {
		t.Fatalf("ValidateBars: %v", err)
	}
}

func TestTencentSourceQFQKeySortDeduplicateAndFilter(t *testing.T) {
	fastTencentRequests(t)
	payload := `{"code":0,"data":{"hk00700":{"qfqday":[
  ["2026-08-11","481.2","470.8","483.8","469.6","19558079"],
  ["2026-08-10","479","481.4","483.6","476.4","15508724"],
  ["2026-08-10","479","481.4","483.6","476.4","15508724"]
]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, payload) }))
	defer srv.Close()
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, tencentDateLocation).UTC()
	bars, err := (TencentSource{Endpoint: srv.URL}).Bars(context.Background(), "HK.00700", "K_DAY", from, from)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 1 || !bars[0].Ts.Equal(from) {
		t.Fatalf("filtered bars = %+v; want one deduplicated 2026-08-10 row", bars)
	}
}

func TestTencentSourceRetriesServerFailure(t *testing.T) {
	fastTencentRequests(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, tencentBarsPayload)
	}))
	defer srv.Close()
	bars, err := (TencentSource{Endpoint: srv.URL}).Bars(context.Background(), "HK.00700", "day", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(bars) != 2 {
		t.Fatalf("calls/bars = %d/%d; want 3/2", calls.Load(), len(bars))
	}
}

func TestTencentSourceRejectsBadInputsAndPayloads(t *testing.T) {
	fastTencentRequests(t)
	tests := []struct {
		name      string
		source    TencentSource
		timeframe string
		payload   string
		want      string
	}{
		{name: "timeframe", timeframe: "1h", want: "unsupported timeframe"},
		{name: "count", source: TencentSource{Count: TencentMaxBars + 1}, timeframe: "1d", want: "count must be"},
		{name: "provider error", timeframe: "1d", payload: `{"code":7,"msg":"denied","data":{}}`, want: "provider code 7"},
		{name: "missing symbol", timeframe: "1d", payload: `{"code":0,"data":{}}`, want: "response missing"},
		{name: "empty", timeframe: "1d", payload: `{"code":0,"data":{"hk00700":{"day":[]}}}`, want: "no daily bars"},
		{name: "short row", timeframe: "1d", payload: `{"code":0,"data":{"hk00700":{"day":[["2026-08-07"]]}}}`, want: "at least 6 fields"},
		{name: "bad volume", timeframe: "1d", payload: `{"code":0,"data":{"hk00700":{"day":[["2026-08-07","1","1","1","1","1.5"]]}}}`, want: "bad volume"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.source
			if tt.payload != "" {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, tt.payload) }))
				defer srv.Close()
				src.Endpoint = srv.URL
			}
			_, err := src.Bars(context.Background(), "HK.00700", tt.timeframe, time.Time{}, time.Time{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestTencentSourceHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (TencentSource{}).Bars(ctx, "HK.00700", "1d", time.Time{}, time.Time{})
	if err != context.Canceled {
		t.Fatalf("error = %v; want context.Canceled", err)
	}
}

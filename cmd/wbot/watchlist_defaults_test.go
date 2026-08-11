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

func TestApplyWheelDefaultsUsesCurrentPrice(t *testing.T) {
	oldQuoteLimit, oldSnapshotLimit := futu.QuoteLimit, futu.SnapshotLimit
	futu.QuoteLimit = futu.NewLimiter(time.Microsecond)
	futu.SnapshotLimit = futu.NewLimiter(time.Microsecond)
	t.Cleanup(func() {
		futu.QuoteLimit, futu.SnapshotLimit = oldQuoteLimit, oldSnapshotLimit
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, `{"ret_type":0,"s2c":{"basic_qot_list":[{"last_price":250}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("FUTU_GATEWAY_URL", server.URL)

	params := map[string]any{"no_trade_gap": 10}
	if err := applyWheelDefaults(context.Background(), "US.TEST", params); err != nil {
		t.Fatalf("applyWheelDefaults() error: %v", err)
	}
	curve, ok := params["price_position_curve"].([]map[string]any)
	if !ok || len(curve) != 2 {
		t.Fatalf("curve = %#v; want two default points", params["price_position_curve"])
	}
	if curve[0]["price"] != 200.0 || curve[0]["target_inventory"] != 100 ||
		curve[1]["price"] != 300.0 || curve[1]["target_inventory"] != 0 {
		t.Fatalf("curve = %#v; want [{200,100},{300,0}]", curve)
	}
	if params["max_inventory"] != 100 {
		t.Fatalf("max_inventory = %#v; want 100", params["max_inventory"])
	}
	if params["no_trade_gap"] != 10 {
		t.Fatalf("explicit optional param changed: %#v", params["no_trade_gap"])
	}
}

func TestApplyWheelDefaultsDoesNotFetchWhenCurveIsExplicit(t *testing.T) {
	t.Setenv("FUTU_GATEWAY_URL", "http://127.0.0.1:1")
	params := map[string]any{
		"price_position_curve": []map[string]any{
			{"price": 80.0, "target_inventory": 100.0},
			{"price": 120.0, "target_inventory": 0.0},
		},
	}
	if err := applyWheelDefaults(context.Background(), "US.TEST", params); err != nil {
		t.Fatalf("applyWheelDefaults() error: %v", err)
	}
	if params["max_inventory"] != 100 {
		t.Fatalf("max_inventory = %#v; want 100", params["max_inventory"])
	}
}

func TestWatchlistHTTPRequiresExplicitWheelDefaults(t *testing.T) {
	top := serveMuxForTest()
	req := httptest.NewRequest(http.MethodPut, "/v1/watchlist/US.TEST", strings.NewReader(`{"strategy":"wheel","params":{"max_inventory":100}}`))
	rec := httptest.NewRecorder()
	top.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT without price_position_curve = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
}

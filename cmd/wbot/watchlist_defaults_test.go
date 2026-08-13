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
			io.WriteString(w, `{"ret_type":0,"s2c":{"basic_qot_list":[{"cur_price":250}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("FUTU_GATEWAY_URL", server.URL)

	params := map[string]any{"trade_gap": 10}
	if err := applyWheelDefaults(context.Background(), "US.TEST", params); err != nil {
		t.Fatalf("applyWheelDefaults() error: %v", err)
	}
	if params["full_position_price"] != 200.0 || params["zero_position_price"] != 300.0 {
		t.Fatalf("price anchors = %#v/%#v; want 200/300", params["full_position_price"], params["zero_position_price"])
	}
	if params["max_inventory"] != 100 {
		t.Fatalf("max_inventory = %#v; want 100", params["max_inventory"])
	}
	if params["trade_gap"] != 10 {
		t.Fatalf("explicit optional param changed: %#v", params["trade_gap"])
	}
}

func TestApplyWheelDefaultsDoesNotFetchWhenAnchorsAreExplicit(t *testing.T) {
	t.Setenv("FUTU_GATEWAY_URL", "http://127.0.0.1:1")
	params := map[string]any{
		"full_position_price": 80.0,
		"zero_position_price": 120.0,
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
		t.Fatalf("PUT without price anchors = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/strategy"
	"github.com/jiayu/wbot/internal/watchlist"
)

func wheelParams() map[string]any {
	return map[string]any{
		"full_position_price": 400.0,
		"zero_position_price": 550.0,
		"max_inventory":       1200.0,
	}
}

type fakeWatchlistStore struct {
	items       []watchlist.Item
	listErr     error
	upsertErr   error
	deleteErr   error
	delFound    bool
	gotSymbol   string
	gotStrategy string
	gotParams   map[string]any
}

func (f *fakeWatchlistStore) List(context.Context) ([]watchlist.Item, error) {
	return f.items, f.listErr
}
func (f *fakeWatchlistStore) Upsert(_ context.Context, symbol, strategy string, params map[string]any) (watchlist.Item, error) {
	f.gotSymbol, f.gotStrategy, f.gotParams = symbol, strategy, params
	if f.upsertErr != nil {
		return watchlist.Item{}, f.upsertErr
	}
	version := 1
	return watchlist.Item{Symbol: symbol, Strategy: strategy, Params: params, ConfigVersion: &version, ExecutionStatus: "DATA_BLOCKED", InvalidationReason: "waiting for complete quote snapshot", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (f *fakeWatchlistStore) Delete(_ context.Context, symbol string) (bool, error) {
	f.gotSymbol = symbol
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	return f.delFound, nil
}

func TestStrategiesListIncludesLLMAndWheel(t *testing.T) {
	rec := get(t, WatchlistHandler(&fakeWatchlistStore{}), "/v1/strategies")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	var got []strategy.ContractTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "llm" || got[1].Name != "wheel" {
		t.Fatalf("strategies = %+v; want llm and wheel", got)
	}
	var full, zero, max strategy.ContractParam
	for _, p := range got[1].Params {
		switch p.Name {
		case "full_position_price":
			full = p
		case "zero_position_price":
			zero = p
		case "max_inventory":
			max = p
		}
	}
	if full.Type != "number" || !full.Required || zero.Type != "number" || !zero.Required || max.Type != "number" || !max.Required {
		t.Fatalf("required wheel schema = full=%+v zero=%+v max=%+v", full, zero, max)
	}
}

func TestWatchlistListIncludesVersionAndExecutionFields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	version := 3
	fake := &fakeWatchlistStore{items: []watchlist.Item{{Symbol: "HK.00700", Strategy: "wheel", Params: wheelParams(), ConfigVersion: &version, ExecutionStatus: "DATA_BLOCKED", InvalidationReason: "waiting for complete quote snapshot", CreatedAt: now, UpdatedAt: now}}}
	rec := get(t, WatchlistHandler(fake), "/v1/watchlist")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["strategy"] != "wheel" || got[0]["config_version"] != float64(3) || got[0]["execution_status"] != "DATA_BLOCKED" || got[0]["invalidation_reason"] != "waiting for complete quote snapshot" {
		t.Fatalf("item = %v", got)
	}
}

func TestWatchlistPutValidWheel(t *testing.T) {
	fake := &fakeWatchlistStore{}
	rec := put(t, WatchlistHandler(fake), "/v1/watchlist/HK.00700", `{"strategy":"wheel","params":{"full_position_price":400,"zero_position_price":550,"max_inventory":1200}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	if fake.gotSymbol != "HK.00700" || fake.gotStrategy != "wheel" {
		t.Fatalf("store got %q/%q", fake.gotSymbol, fake.gotStrategy)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["config_version"] != float64(1) || got["execution_status"] != "DATA_BLOCKED" {
		t.Fatalf("put body = %v", got)
	}
}

func TestWatchlistPutPreservesPriceCurveAndDropsRetiredKeys(t *testing.T) {
	fake := &fakeWatchlistStore{}
	rec := put(t, WatchlistHandler(fake), "/v1/watchlist/HK.00883", `{"strategy":"wheel","params":{"price_position_curve":[{"price":40,"target_inventory":22000},{"price":48,"target_inventory":11000},{"price":55,"target_inventory":0}],"max_inventory":22000,"no_trade_gap":50,"max_daily_orders":1,"extreme_max_daily_orders":2,"lot_size":100}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	for _, key := range []string{"no_trade_gap", "max_daily_orders", "extreme_max_daily_orders", "lot_size"} {
		if _, ok := fake.gotParams[key]; ok {
			t.Fatalf("persisted legacy key %q in %v", key, fake.gotParams)
		}
	}
	curve, ok := fake.gotParams["price_position_curve"].([]any)
	if !ok || len(curve) != 3 || curve[1].(map[string]any)["price"] != 48.0 || curve[1].(map[string]any)["target_inventory"] != 11000.0 {
		t.Fatalf("preserved price curve = %#v", fake.gotParams["price_position_curve"])
	}
	if _, ok := fake.gotParams["full_position_price"]; ok {
		t.Fatalf("curve config unexpectedly persisted compatibility endpoint: %v", fake.gotParams)
	}
	if _, ok := fake.gotParams["zero_position_price"]; ok {
		t.Fatalf("curve config unexpectedly persisted compatibility endpoint: %v", fake.gotParams)
	}
	if fake.gotParams["trade_gap"] != 50.0 || fake.gotParams["migration_warning_count"] != 3.0 {
		t.Fatalf("canonical migration = %v", fake.gotParams)
	}
	if _, ok := fake.gotParams["migration_lossy"]; ok {
		t.Fatalf("losslessly preserved curve marked lossy: %v", fake.gotParams)
	}
}

func TestWatchlistPutRejectsLegacyNamesAndIncompleteWheel(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, body := range []string{
		`{"strategy":"covered-call"}`,
		`{"strategy":"cash-secured-put"}`,
		`{"strategy":"wheel"}`,
		`{"strategy":"wheel","params":{"full_position_price":400,"max_inventory":1200}}`,
	} {
		rec := put(t, h, "/v1/watchlist/HK.00700", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d; want 400 (%s)", body, rec.Code, rec.Body)
		}
	}
}

func TestWatchlistDeleteAndErrors(t *testing.T) {
	fake := &fakeWatchlistStore{delFound: true}
	rec := httptest.NewRecorder()
	WatchlistHandler(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/watchlist/HK.00700", nil))
	if rec.Code != http.StatusOK || fake.gotSymbol != "HK.00700" {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body)
	}
	if rec = httptest.NewRecorder(); true {
		WatchlistHandler(&fakeWatchlistStore{listErr: errors.New("boom")}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/watchlist", nil))
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
			t.Fatalf("error response status=%d body=%s", rec.Code, rec.Body)
		}
	}
}

func TestWatchlistMethodsAndUnknownPaths(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/strategies"}, {http.MethodDelete, "/v1/watchlist"},
		{http.MethodGet, "/v1/watchlist/HK.00700"}, {http.MethodPost, "/v1/watchlist/HK.00700"},
		{http.MethodGet, "/v1/watchlist/"}, {http.MethodGet, "/v1/unknown"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"strategy":"wheel"}`)))
		want := http.StatusMethodNotAllowed
		if tc.path == "/v1/watchlist/" || tc.path == "/v1/unknown" {
			want = http.StatusNotFound
		}
		if rec.Code != want {
			t.Fatalf("%s %s = %d; want %d", tc.method, tc.path, rec.Code, want)
		}
	}
}

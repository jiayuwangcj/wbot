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
		"price_position_curve": []any{
			map[string]any{"price": 400.0, "target_inventory": 1200.0},
			map[string]any{"price": 550.0, "target_inventory": 0.0},
		},
		"max_inventory": 1200.0,
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

func TestStrategiesListOnlyWheelWithRequiredRiskInputs(t *testing.T) {
	rec := get(t, WatchlistHandler(&fakeWatchlistStore{}), "/v1/strategies")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	var got []strategy.ContractTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "wheel" {
		t.Fatalf("strategies = %+v; want only wheel", got)
	}
	var curve, max strategy.ContractParam
	for _, p := range got[0].Params {
		switch p.Name {
		case "price_position_curve":
			curve = p
		case "max_inventory":
			max = p
		}
	}
	if curve.Type != "curve" || !curve.Required || max.Type != "number" || !max.Required {
		t.Fatalf("required wheel schema = curve=%+v max=%+v", curve, max)
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
	rec := put(t, WatchlistHandler(fake), "/v1/watchlist/HK.00700", `{"strategy":"wheel","params":{"price_position_curve":[{"price":400,"target_inventory":1200},{"price":550,"target_inventory":0}],"max_inventory":1200}}`)
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

func TestWatchlistPutRejectsLegacyNamesAndIncompleteWheel(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, body := range []string{
		`{"strategy":"covered-call"}`,
		`{"strategy":"cash-secured-put"}`,
		`{"strategy":"wheel"}`,
		`{"strategy":"wheel","params":{"price_position_curve":[{"price":400,"target_inventory":1200}],"max_inventory":1200}}`,
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

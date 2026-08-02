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

	"github.com/jiayu/wbot/internal/watchlist"
)

// fakeWatchlistStore is a scriptable WatchlistStore for success and error-path tests.
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
	return watchlist.Item{Symbol: symbol, Strategy: strategy, Params: params, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeWatchlistStore) Delete(_ context.Context, symbol string) (bool, error) {
	f.gotSymbol = symbol
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	return f.delFound, nil
}

// templateParam finds a template's param by name, failing the test when missing.
func templateParam(t *testing.T, tmpl watchlist.Template, name string) watchlist.Param {
	t.Helper()
	for _, p := range tmpl.Params {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("template %s missing param %s", tmpl.Name, name)
	return watchlist.Param{}
}

// TestStrategiesList is the ⑫-c contract: template names + param schema match
// the ⑫-b draft (registry swap after feat/strategy-impl must keep this green).
func TestStrategiesList(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	rec := get(t, h, "/v1/strategies")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got []watchlist.Template
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d; want 3 (body %s)", len(got), rec.Body)
	}
	for i, name := range []string{"buy-hold", "covered-call", "cash-secured-put"} {
		if got[i].Name != name {
			t.Fatalf("templates[%d].name = %q; want %q", i, got[i].Name, name)
		}
	}
	cc := got[1]
	for _, p := range []struct{ name, typ string }{
		{"strike_pct_otm", "number"},
		{"expiry_rule", "choice"},
		{"days_to_expiry", "number"},
		{"fee_per_contract", "number"},
	} {
		if got := templateParam(t, cc, p.name); got.Type != p.typ {
			t.Fatalf("param %s type = %q; want %q", p.name, got.Type, p.typ)
		}
	}
	if d := templateParam(t, cc, "strike_pct_otm").Default; d != 0.03 {
		t.Fatalf("strike_pct_otm default = %v; want 0.03", d)
	}
	if d := templateParam(t, cc, "days_to_expiry").Default; d != 28.0 {
		t.Fatalf("days_to_expiry default = %v; want 28", d)
	}
	rule := templateParam(t, cc, "expiry_rule")
	if rule.Default != "next_expiry" || len(rule.Choices) != 1 || rule.Choices[0] != "next_expiry" {
		t.Fatalf("expiry_rule = %+v; want default next_expiry with single choice", rule)
	}
}

func TestStrategiesMethodNotAllowed(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/v1/strategies", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /v1/strategies: status = %d; want 405 (body %s)", method, rec.Code, rec.Body)
		}
	}
}

func TestWatchlistListEmpty(t *testing.T) {
	rec := get(t, WatchlistHandler(&fakeWatchlistStore{}), "/v1/watchlist")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("body = %q; want []", body)
	}
}

func TestWatchlistListItems(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	fake := &fakeWatchlistStore{items: []watchlist.Item{
		{Symbol: "HK.00700", Strategy: "covered-call", Params: map[string]any{"strike_pct_otm": 0.03}, CreatedAt: now, UpdatedAt: now},
		{Symbol: "HK.09988", Strategy: "cash-secured-put", Params: map[string]any{}, CreatedAt: now, UpdatedAt: now},
	}}
	rec := get(t, WatchlistHandler(fake), "/v1/watchlist")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (body %s)", len(got), rec.Body)
	}
	first := got[0]
	if first["symbol"] != "HK.00700" || first["strategy"] != "covered-call" {
		t.Fatalf("item[0] = %v; want symbol+strategy", first)
	}
	params, ok := first["params"].(map[string]any)
	if !ok {
		t.Fatalf("params = %v; want object", first["params"])
	}
	if pct, ok := params["strike_pct_otm"].(float64); !ok || pct != 0.03 {
		t.Fatalf("strike_pct_otm = %v; want float64 0.03", params["strike_pct_otm"])
	}
	if _, err := time.Parse(time.RFC3339, first["created_at"].(string)); err != nil {
		t.Fatalf("created_at %v not RFC3339: %v", first["created_at"], err)
	}
}

func TestWatchlistPutValid(t *testing.T) {
	fake := &fakeWatchlistStore{}
	h := WatchlistHandler(fake)
	rec := put(t, h, "/v1/watchlist/HK.00700", `{"strategy":"covered-call","params":{"strike_pct_otm":0.03}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if fake.gotSymbol != "HK.00700" || fake.gotStrategy != "covered-call" {
		t.Fatalf("store got symbol %q strategy %q", fake.gotSymbol, fake.gotStrategy)
	}
	if pct, ok := fake.gotParams["strike_pct_otm"].(float64); !ok || pct != 0.03 {
		t.Fatalf("store got params %v; want strike_pct_otm 0.03", fake.gotParams)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["symbol"] != "HK.00700" || got["strategy"] != "covered-call" {
		t.Fatalf("put body = %v; want stored item", got)
	}
}

func TestWatchlistPutWithoutParamsDefaults(t *testing.T) {
	fake := &fakeWatchlistStore{}
	rec := put(t, WatchlistHandler(fake), "/v1/watchlist/HK.00700", `{"strategy":"covered-call"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if len(fake.gotParams) != 0 {
		t.Fatalf("store got params %v; want empty map", fake.gotParams)
	}
}

func TestWatchlistPutValidation400(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, tc := range []struct{ path, body, wantMsg string }{
		{"/v1/watchlist/HK.00700", `{"strategy":"nope"}`, "unknown strategy template"},
		{"/v1/watchlist/HK.00700", `{"strategy":"covered-call","params":{"nope":1}}`, "unknown parameter"},
		{"/v1/watchlist/HK.00700", `{"strategy":"covered-call","params":{"strike_pct_otm":"0.03"}}`, "want number"},
		{"/v1/watchlist/HK.00700", `{"strategy":"covered-call","params":{"expiry_rule":"monthly"}}`, "want one of"},
		{"/v1/watchlist/HK.00700", `{"params":{"strike_pct_otm":0.03}}`, "missing strategy"},
		{"/v1/watchlist/HK.00700", `{"strategy":"covered-call","params":{"strike_pct_otm":0.03,"extra":true}}`, "unknown parameter"},
		{"/v1/watchlist/HK.00700", `not json`, "invalid JSON body"},
		{"/v1/watchlist/%20", `{"strategy":"covered-call"}`, "missing symbol"},
	} {
		rec := put(t, h, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s body %s: status = %d; want 400 (response %s)", tc.path, tc.body, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || !strings.Contains(errBody["error"], tc.wantMsg) {
			t.Fatalf("%s body %s: error %q; want contains %q", tc.path, tc.body, errBody["error"], tc.wantMsg)
		}
	}
}

func TestWatchlistDelete(t *testing.T) {
	fake := &fakeWatchlistStore{delFound: true}
	h := WatchlistHandler(fake)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/watchlist/HK.00700", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if fake.gotSymbol != "HK.00700" {
		t.Fatalf("store got symbol %q; want HK.00700", fake.gotSymbol)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["deleted"] != true || got["symbol"] != "HK.00700" {
		t.Fatalf("delete body = %v; want deleted:true", got)
	}
}

func TestWatchlistDeleteNotFound404(t *testing.T) {
	rec := httptest.NewRecorder()
	WatchlistHandler(&fakeWatchlistStore{}).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/watchlist/HK.00700", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestWatchlistMethodNotAllowed(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/watchlist"},
		{http.MethodDelete, "/v1/watchlist"},
		{http.MethodGet, "/v1/watchlist/HK.00700"},
		{http.MethodPost, "/v1/watchlist/HK.00700"},
		{http.MethodPatch, "/v1/watchlist/HK.00700"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"strategy":"covered-call"}`)))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: status = %d; want 405 (body %s)", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestWatchlistUnknownPath404(t *testing.T) {
	h := WatchlistHandler(&fakeWatchlistStore{})
	for _, path := range []string{"/v1/watchlist/", "/v1/watchlist/a/b", "/v1/watchlistx", "/v1/strategiesx"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: status = %d; want 404 (body %s)", path, rec.Code, rec.Body)
		}
	}
}

func TestWatchlistStoreError500(t *testing.T) {
	fake := &fakeWatchlistStore{listErr: errors.New("boom-list"), upsertErr: errors.New("boom-upsert"), deleteErr: errors.New("boom-delete")}
	h := WatchlistHandler(fake)

	rec := get(t, h, "/v1/watchlist")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("list error leaks detail: %s", rec.Body)
	}

	rec = put(t, h, "/v1/watchlist/HK.00700", `{"strategy":"covered-call"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("put status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("put error leaks detail: %s", rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/watchlist/HK.00700", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("delete error leaks detail: %s", rec.Body)
	}
}

// TestWatchlistComposedHandler mirrors serve's wiring: watchlist endpoints + the
// data API on one top-level mux.
func TestWatchlistComposedHandler(t *testing.T) {
	top := http.NewServeMux()
	wl := WatchlistHandler(&fakeWatchlistStore{})
	top.Handle("/v1/strategies", wl)
	top.Handle("/v1/watchlist", wl)
	top.Handle("/v1/watchlist/", wl)
	top.Handle("/", Handler(&fakeStore{}))

	rec := get(t, top, "/v1/strategies")
	if rec.Code != http.StatusOK {
		t.Fatalf("strategies = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/watchlist")
	if rec.Code != http.StatusOK {
		t.Fatalf("watchlist = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = put(t, top, "/v1/watchlist/HK.00700", `{"strategy":"cash-secured-put"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("watchlist put = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/bars?symbol=DEMO.US&timeframe=1d")
	if rec.Code != http.StatusOK {
		t.Fatalf("bars = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	// GET on a symbol path is 405 (only PUT/DELETE are defined), like admin config keys.
	rec = get(t, top, "/v1/watchlist/nope")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("watchlist symbol get = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

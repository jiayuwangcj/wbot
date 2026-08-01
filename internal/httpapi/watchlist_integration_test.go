package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/db"
)

// TestWatchlistIntegration drives the real PG upsert/read/delete path through
// the HTTP API: PUT → GET list → PUT update → DELETE → 404 on re-delete.
func TestWatchlistIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}

	// Same wiring as serve: watchlist endpoints + data API on one mux.
	top := http.NewServeMux()
	wl := WatchlistHandler(NewDBWatchlistStore(database))
	top.Handle("/v1/strategies", wl)
	top.Handle("/v1/watchlist", wl)
	top.Handle("/v1/watchlist/", wl)
	top.Handle("/", Handler(NewDBStore(database)))
	srv := httptest.NewServer(top)
	defer srv.Close()

	const symbol = "ITEST.00700"
	putURL := srv.URL + "/v1/watchlist/" + symbol

	// PUT valid: params round-trip through JSONB as numbers.
	req, err := http.NewRequest(http.MethodPut, putURL, strings.NewReader(`{"strategy":"covered-call","params":{"strike_pct_otm":0.03}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status = %d; want 200", resp.StatusCode)
	}

	// GET list shows the row with the JSONB params decoded.
	resp, err = http.Get(srv.URL + "/v1/watchlist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d; want 200", resp.StatusCode)
	}
	var got []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range got {
		if it["symbol"] == symbol {
			found = true
			if it["strategy"] != "covered-call" {
				t.Fatalf("strategy = %v; want covered-call", it["strategy"])
			}
			params, ok := it["params"].(map[string]any)
			if !ok {
				t.Fatalf("params = %v; want object", it["params"])
			}
			if pct, ok := params["strike_pct_otm"].(float64); !ok || pct != 0.03 {
				t.Fatalf("strike_pct_otm = %v; want float64 0.03", params["strike_pct_otm"])
			}
		}
	}
	if !found {
		t.Fatalf("symbol %s missing from GET /v1/watchlist: %s", symbol, got)
	}

	// PUT update replaces strategy+params (upsert path).
	req, err = http.NewRequest(http.MethodPut, putURL, strings.NewReader(`{"strategy":"cash-secured-put"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d; want 200", resp.StatusCode)
	}

	// PUT with invalid params rejected without touching the row.
	req, err = http.NewRequest(http.MethodPut, putURL, strings.NewReader(`{"strategy":"covered-call","params":{"nope":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid put status = %d; want 400", resp.StatusCode)
	}

	// DELETE removes; second DELETE is 404.
	for _, want := range []int{http.StatusOK, http.StatusNotFound} {
		req, err := http.NewRequest(http.MethodDelete, putURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("delete status = %d; want %d", resp.StatusCode, want)
		}
	}

	// GET /v1/strategies serves the template contract from real wiring.
	resp, err = http.Get(srv.URL + "/v1/strategies")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("strategies status = %d; want 200", resp.StatusCode)
	}
}

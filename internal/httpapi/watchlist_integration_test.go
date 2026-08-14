package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/db"
)

func TestWatchlistIntegrationVersionedWheelHistory(t *testing.T) {
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
	top := http.NewServeMux()
	top.Handle("/v1/strategies", WatchlistHandler(NewDBWatchlistStore(database)))
	top.Handle("/v1/watchlist", WatchlistHandler(NewDBWatchlistStore(database)))
	top.Handle("/v1/watchlist/", WatchlistHandler(NewDBWatchlistStore(database)))
	srv := httptest.NewServer(top)
	defer srv.Close()

	symbol := fmt.Sprintf("ITEST.WHEEL.%d", os.Getpid())
	putURL := srv.URL + "/v1/watchlist/" + symbol
	params1 := `{"full_position_price":400,"zero_position_price":550,"max_inventory":1200}`
	params2 := `{"full_position_price":400,"zero_position_price":550,"max_inventory":1000}`
	put := func(params string) map[string]any {
		req, err := http.NewRequest(http.MethodPut, putURL, strings.NewReader(`{"strategy":"wheel","params":`+params+`}`))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put status=%d", resp.StatusCode)
		}
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := put(params1)
	if first["config_version"] != float64(1) || first["execution_status"] != "DATA_BLOCKED" || first["invalidation_reason"] != "waiting for complete quote snapshot" {
		t.Fatalf("first item=%v", first)
	}
	second := put(params2)
	if second["config_version"] != float64(2) {
		t.Fatalf("second version=%v; want 2", second["config_version"])
	}

	var count int
	if err := database.QueryRow(`SELECT count(*) FROM wheel_configs WHERE symbol=$1`, symbol).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("config history count=%d; want 2", count)
	}

	resp, err := http.DefaultClient.Do(mustRequest(http.MethodDelete, putURL, ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
	if err := database.QueryRow(`SELECT count(*) FROM wheel_configs WHERE symbol=$1`, symbol).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("config history after delete=%d; want 2", count)
	}

	legacyReq := mustRequest(http.MethodPut, srv.URL+"/v1/watchlist/"+symbol, `{"strategy":"covered-call"}`)
	legacyResp, err := http.DefaultClient.Do(legacyReq)
	if err != nil {
		t.Fatal(err)
	}
	legacyResp.Body.Close()
	if legacyResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("legacy status=%d; want 400", legacyResp.StatusCode)
	}
}

func mustRequest(method, url, body string) *http.Request {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	return req
}

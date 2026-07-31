package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

func TestAdminStatusIntegration(t *testing.T) {
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

	// Same wiring as serve: admin namespace + data API on one mux.
	meta := ProcessMeta{Version: "integration-test", StartedAt: time.Now().Add(-time.Minute).Truncate(time.Second), ListenAddr: "127.0.0.1:8080"}
	top := http.NewServeMux()
	top.Handle("/v1/admin/", AdminHandler(meta, PingerFunc(database.PingContext)))
	top.Handle("/", Handler(NewDBStore(database)))
	srv := httptest.NewServer(top)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	dbStatus, ok := got["db"].(map[string]any)
	if !ok || dbStatus["ok"] != true {
		t.Fatalf("db = %v; want ok:true", got["db"])
	}
	if _, ok := dbStatus["latency_ms"].(float64); !ok {
		t.Fatalf("db = %v; want latency_ms", got["db"])
	}
	if got["version"] != meta.Version || got["listen_addr"] != meta.ListenAddr {
		t.Fatalf("process fields = %v; want injected meta", got)
	}
}

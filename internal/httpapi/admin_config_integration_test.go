package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/db"
)

func TestAdminConfigIntegration(t *testing.T) {
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

	// Same wiring as serve: admin namespace + config endpoints + data API on one mux.
	meta := ProcessMeta{Version: "integration-test", StartedAt: time.Now().Add(-time.Minute).Truncate(time.Second), ListenAddr: "127.0.0.1:8080"}
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := ConfigHandler(store)
	top := http.NewServeMux()
	top.Handle("/v1/admin/", AdminHandler(meta, PingerFunc(database.PingContext)))
	top.Handle("/v1/admin/config", cfg)
	top.Handle("/v1/admin/config/", cfg)
	top.Handle("/", Handler(NewDBStore(database)))
	srv := httptest.NewServer(top)
	defer srv.Close()

	const val = "integration-leak-sentinel-7"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/v1/admin/config/system.listen", strings.NewReader(`{"value":"`+val+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status = %d; want 200", resp.StatusCode)
	}
	var putResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&putResp); err != nil {
		t.Fatal(err)
	}
	if putResp["set"] != true {
		t.Fatalf("put = %v; want set:true", putResp)
	}

	resp, err = http.Get(srv.URL + "/v1/admin/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d; want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), val) {
		t.Fatalf("GET leaks config value: %s", raw)
	}
	var got []config.Entry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range got {
		if e.Key == "system.listen" {
			found = true
			if !e.Set || e.UpdatedAt == nil {
				t.Fatalf("entry = %+v; want set:true with updated_at", e)
			}
		}
	}
	if !found {
		t.Fatalf("system.listen missing from GET list")
	}
}

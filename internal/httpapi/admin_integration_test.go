package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/ingest"
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

// TestAdminClusterIntegration: after `wbot ingest mock`, the symbol×timeframe
// coverage must appear in the data_plane component (single-process component view).
func TestAdminClusterIntegration(t *testing.T) {
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

	ctx := context.Background()
	source := "cluster-test"
	symbol := domain.Symbol("CLUSTER.US")
	tf := "1d"
	if err := ingest.RunMockIngestion(ctx, database, source, symbol, tf, "none"); err != nil {
		t.Fatal(err)
	}

	// Same wiring as serve: admin namespace + cluster + data API on one mux.
	meta := ProcessMeta{Version: "integration-test", StartedAt: time.Now().Add(-time.Minute).Truncate(time.Second), ListenAddr: "127.0.0.1:8080"}
	store := NewDBStore(database)
	top := http.NewServeMux()
	top.Handle("/v1/admin/", AdminHandler(meta, PingerFunc(database.PingContext)))
	top.Handle("/v1/admin/cluster", ClusterHandler(meta, store))
	top.Handle("/", Handler(store))
	srv := httptest.NewServer(top)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/admin/cluster")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var got struct {
		Components struct {
			DB struct {
				OK bool `json:"ok"`
			} `json:"db"`
			Pipeline struct {
				Counts struct {
					Running   int64 `json:"running"`
					Succeeded int64 `json:"succeeded"`
					Failed    int64 `json:"failed"`
				} `json:"counts"`
			} `json:"pipeline"`
			DataPlane struct {
				BarsCoverage []struct {
					Symbol    string `json:"symbol"`
					Timeframe string `json:"timeframe"`
					Count     int64  `json:"count"`
					MinTs     string `json:"min_ts"`
					MaxTs     string `json:"max_ts"`
				} `json:"bars_coverage"`
			} `json:"data_plane"`
		} `json:"components"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Components.DB.OK {
		t.Fatal("db ok = false; want true")
	}
	if got.Components.Pipeline.Counts.Succeeded < 1 {
		t.Fatalf("pipeline counts = %+v; want succeeded >= 1", got.Components.Pipeline.Counts)
	}
	found := false
	for _, c := range got.Components.DataPlane.BarsCoverage {
		if c.Symbol != string(symbol) || c.Timeframe != tf {
			continue
		}
		found = true
		if c.Count != 3 {
			t.Fatalf("coverage count = %d; want 3", c.Count)
		}
		if _, err := time.Parse(time.RFC3339, c.MinTs); err != nil {
			t.Fatalf("min_ts %q not RFC3339: %v", c.MinTs, err)
		}
		if _, err := time.Parse(time.RFC3339, c.MaxTs); err != nil {
			t.Fatalf("max_ts %q not RFC3339: %v", c.MaxTs, err)
		}
	}
	if !found {
		t.Fatalf("coverage missing %s %s (got %+v)", symbol, tf, got.Components.DataPlane.BarsCoverage)
	}
}

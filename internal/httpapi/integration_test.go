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

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/watchlist"
)

func TestHandlerIntegration(t *testing.T) {
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
	source := "httpapi-test"
	if err := ingest.RunMockIngestion(ctx, database, source, domain.Symbol("DEMO.US"), "1d", "none"); err != nil {
		t.Fatal(err)
	}
	const datacheckSymbol = "US.HTTPAPICHECK"
	_, _ = watchlist.Delete(ctx, database, datacheckSymbol)
	if _, err := watchlist.Upsert(ctx, database, datacheckSymbol, "wheel", validWheelParams()); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = watchlist.Delete(ctx, database, datacheckSymbol) }()

	srv := httptest.NewServer(Handler(NewDBStore(database)))
	defer srv.Close()

	// GET /v1/bars: mock feed yields exactly 3 bars for DEMO.US 1d.
	resp, err := http.Get(srv.URL + "/v1/bars?symbol=DEMO.US&timeframe=1d&adjust=none")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bars status = %d; want 200", resp.StatusCode)
	}
	var bars []barJSON
	if err := json.NewDecoder(resp.Body).Decode(&bars); err != nil {
		t.Fatal(err)
	}
	if len(bars) != 3 {
		t.Fatalf("bars len = %d; want 3", len(bars))
	}
	if bars[0].Open == 0 || bars[0].Close == 0 || bars[0].Volume == 0 {
		t.Fatalf("bars[0] = %+v; want non-zero OHLCV fields", bars[0])
	}
	if _, err := time.Parse(time.RFC3339, bars[0].Ts); err != nil {
		t.Fatalf("bars[0] ts %q not RFC3339: %v", bars[0].Ts, err)
	}

	// GET /v1/runs: the httpapi-test run must appear (newest first).
	resp, err = http.Get(srv.URL + "/v1/runs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("runs status = %d; want 200", resp.StatusCode)
	}
	var runs []runJSON
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range runs {
		if r.Source == source {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("runs missing source %q (got %d runs)", source, len(runs))
	}

	// GET /v1/datacheck: real PostgreSQL snapshot reuses the datacheck policy.
	resp, err = http.Get(srv.URL + "/v1/datacheck")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("datacheck status = %d", resp.StatusCode)
	}
	var report datacheck.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	foundDatacheckSymbol := false
	for _, item := range report.Items {
		if item.Symbol == datacheckSymbol {
			foundDatacheckSymbol = true
			break
		}
	}
	if report.Symbols < 1 || report.Total == 0 || !foundDatacheckSymbol {
		t.Fatalf("datacheck report = %+v; want watchlist symbol %s", report, datacheckSymbol)
	}

	// GET /v1/health: the real pool must answer the ping.
	resp, err = http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d; want 200", resp.StatusCode)
	}
	var health healthJSON
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" {
		t.Fatalf("health status field = %q; want ok", health.Status)
	}
}

package ingest

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
)

func TestRunMockIngestionIntegration(t *testing.T) {
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
	source := "mock-test"
	symbol := domain.Symbol("TEST.US")
	tf := "1d"
	if err := RunMockIngestion(ctx, database, source, symbol, tf); err != nil {
		t.Fatal(err)
	}

	var n int
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = $1 AND timeframe = $2`, string(symbol), tf).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("bars count: got %d want 3", n)
	}

	var st string
	err = database.QueryRow(`
SELECT status FROM ingestion_runs WHERE source = $1 ORDER BY id DESC LIMIT 1`, source).Scan(&st)
	if err != nil {
		t.Fatal(err)
	}
	if st != "succeeded" {
		t.Fatalf("run status: got %q want succeeded", st)
	}

	// Second run with identical bars must not fail (ON CONFLICT DO NOTHING).
	if err := RunMockIngestion(ctx, database, source, symbol, tf); err != nil {
		t.Fatal(err)
	}
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = $1 AND timeframe = $2`, string(symbol), tf).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("bars count after repeat: got %d want 3", n)
	}
}

func TestRunFileIngestionIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bars.json")
	payload := `[
{"ts":"2024-06-01T00:00:00Z","open":10,"high":11,"low":9,"close":10.5,"volume":100},
{"ts":"2024-06-02T00:00:00Z","open":10.5,"high":12,"low":10,"close":11,"volume":90}
]`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
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
	source := "file-ingest-test"
	symbol := domain.Symbol("FILE.US")
	tf := "1d"
	if err := RunIngestion(ctx, database, source, symbol, tf, time.Time{}, time.Time{}, FileSource{Path: path}); err != nil {
		t.Fatal(err)
	}

	var n int
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = $1 AND timeframe = $2`, string(symbol), tf).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("bars count: got %d want 2", n)
	}

	var st string
	err = database.QueryRow(`
SELECT status FROM ingestion_runs WHERE source = $1 ORDER BY id DESC LIMIT 1`, source).Scan(&st)
	if err != nil {
		t.Fatal(err)
	}
	if st != "succeeded" {
		t.Fatalf("run status: got %q want succeeded", st)
	}
}

func TestRecentRunsIntegration(t *testing.T) {
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
	source := "status-test"
	symbol := domain.Symbol("STATUS.US")
	tf := "1d"
	if err := RunMockIngestion(ctx, database, source, symbol, tf); err != nil {
		t.Fatal(err)
	}

	runs, err := RecentRuns(ctx, database, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatal("RecentRuns: got 0 runs")
	}
	latest := runs[0]
	if latest.Source != source {
		t.Fatalf("latest run source: got %q want %q", latest.Source, source)
	}
	if latest.Status != "succeeded" {
		t.Fatalf("latest run status: got %q want succeeded", latest.Status)
	}
	if latest.FinishedAt == nil {
		t.Fatal("latest run FinishedAt: got nil, want finished timestamp")
	}

	if _, err := RecentRuns(ctx, database, 0); err == nil {
		t.Fatal("RecentRuns with limit 0: expected error")
	}
}

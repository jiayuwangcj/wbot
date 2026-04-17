package ingest

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"context"
	"os"
	"testing"

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
}

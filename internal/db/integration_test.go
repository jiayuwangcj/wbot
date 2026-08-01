package db

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"os"
	"testing"
)

func TestMigrateUpIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := MigrateUp(database); err != nil {
		t.Fatal(err)
	}
	if err := MigrateUp(database); err != nil {
		t.Fatal("second MigrateUp should be idempotent", err)
	}
	for _, tbl := range []string{"ingestion_runs", "bars", "option_quotes", "backtest_results", "watchlist"} {
		var n int
		err := database.QueryRow(`
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = current_schema() AND table_name = $1`, tbl).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("table %s missing: count=%d", tbl, n)
		}
	}

	// bars carries the data-standard columns (doc/DATA_STANDARD.md) with the
	// PK extended by adjust+source so rehab variants coexist.
	var pk string
	err = database.QueryRow(`
SELECT pg_get_constraintdef(oid) FROM pg_constraint
WHERE conname = 'bars_pkey'`).Scan(&pk)
	if err != nil {
		t.Fatal(err)
	}
	wantPK := "PRIMARY KEY (symbol, timeframe, ts, adjust, source)"
	if pk != wantPK {
		t.Fatalf("bars PK = %q; want %q", pk, wantPK)
	}
}

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
	var n int
	err = database.QueryRow(`
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = current_schema() AND table_name = 'ingestion_runs'`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ingestion_runs missing: count=%d", n)
	}
}

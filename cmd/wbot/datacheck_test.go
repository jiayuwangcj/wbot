package main

import (
	"os"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/db"
)

func TestDataCheckHelpAndValidation(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "datacheck", "-h"})
	if code != 0 || !strings.Contains(stderr, "8 timeframe x 3 adjustment") {
		t.Fatalf("help: code=%d stderr=%q", code, stderr)
	}

	_, stderr, code = captureRun(t, []string{"wbot", "datacheck", "-now", "not-a-time"})
	if code != 2 || !strings.Contains(stderr, "invalid RFC3339") {
		t.Fatalf("bad now: code=%d stderr=%q", code, stderr)
	}
}

func TestDataCheckCLIIntegrationReportsWatchlistGaps(t *testing.T) {
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
	const symbol = "US.DATACHECKCLI"
	cleanup := func() {
		_, _ = database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM watchlist WHERE symbol = $1`, symbol)
	}
	cleanup()
	defer cleanup()
	if _, err := database.Exec(`INSERT INTO watchlist(symbol, strategy) VALUES ($1, 'buy-hold')`, symbol); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := captureRun(t, []string{"wbot", "datacheck", "-dsn", dsn, "-now", "2026-08-07T17:30:00+08:00"})
	if code != 1 || stderr != "" {
		t.Fatalf("code=%d stderr=%q; want incomplete exit 1", code, stderr)
	}
	if !strings.Contains(stdout, symbol+" 1m/none missing") || !strings.Contains(stdout, symbol+" options missing") || !strings.Contains(stdout, "complete=false") {
		t.Fatalf("stdout = %q; want gap details and summary", stdout)
	}
}

func TestParseDailyTime(t *testing.T) {
	hour, minute, err := parseDailyTime("17:30")
	if err != nil || hour != 17 || minute != 30 {
		t.Fatalf("parseDailyTime = %d:%d, %v", hour, minute, err)
	}
	for _, value := range []string{"", "24:00", "17:60", "x:30", "17"} {
		if _, _, err := parseDailyTime(value); err == nil {
			t.Fatalf("parseDailyTime(%q) succeeded; want error", value)
		}
	}
}

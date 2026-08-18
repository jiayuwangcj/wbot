package backtestexec

// DB-backed regression for -chunk determinism (doc/BACKTEST.md): chunk windows
// whose upper bound lands exactly on a bar timestamp must not process that bar
// twice. QueryBars is inclusive [from,to]; non-final chunks read [cur,next) so
// the boundary bar is owned by the next chunk only. Requires WBOT_PG_DSN.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

// insertBoundaryBars writes 1m bars every hour across a window sized so 3h
// chunk bounds hit bar timestamps (hour 3, 6, 9, ...).
func insertBoundaryBars(t *testing.T, ctx context.Context, dsn, symbol string) {
	t.Helper()
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol)
	})
	start := time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)
	for i := 0; i < 11; i++ {
		ts := start.Add(time.Duration(i) * time.Hour)
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1m', $2, 100, 101, 99, 100.5, 1000, 'none', 'futu')`, symbol, ts); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunChunkedBoundaryBarNotDuplicated(t *testing.T) {
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
	const symbol = "BOUNDARY.US"
	insertBoundaryBars(t, ctx, dsn, symbol)

	from := time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)
	to := from.Add(10 * time.Hour)
	o := Options{
		Symbol: symbol, Strategy: "hold", Timeframe: "1m", Adjust: "none",
		From: from, To: to, Limit: 1000, Cash: 100000, Fee: 1,
	}
	single, err := Run(ctx, database, o)
	if err != nil {
		t.Fatal(err)
	}
	if single.Result.Bars != 11 {
		t.Fatalf("single bars = %d; want 11", single.Result.Bars)
	}

	// 3h chunks: non-final bounds at hour 3, 6, 9 — all bar timestamps. A buggy
	// inclusive upper bound would double-process those boundary bars (bars>11).
	chunked, err := RunChunked(ctx, database, o, 3*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if chunked.Result.Bars != 11 {
		t.Fatalf("chunked bars = %d; want 11 (boundary bars double-processed?)", chunked.Result.Bars)
	}
	if chunked.SourceHash != single.SourceHash {
		t.Fatalf("sourceHash differs:\n single=%s\nchunked=%s", single.SourceHash, chunked.SourceHash)
	}
	a, err := json.Marshal(single.Result)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(chunked.Result)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("chunked Result differs from single (boundary bar replayed twice):\n%s\n%s", a, b)
	}
}

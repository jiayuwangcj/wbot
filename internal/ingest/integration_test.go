package ingest

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
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
	if err := RunIngestion(ctx, database, source, symbol, tf, "none", "file", time.Time{}, time.Time{}, FileSource{Path: path}); err != nil {
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

func TestRunFutuIngestionIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, futuBarsPayload)
	}))
	defer srv.Close()

	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	source := "futu-ingest-test"
	symbol := domain.Symbol("US.FUTU")
	tf := "1d"
	src := FutuSource{Client: futu.NewClient(srv.URL)}
	if err := RunIngestion(ctx, database, source, symbol, tf, "none", "futu", time.Time{}, time.Time{}, src); err != nil {
		t.Fatal(err)
	}

	var n int
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = $1 AND timeframe = $2`, string(symbol), tf).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("bars count: got %d want 2 (blank bar skipped)", n)
	}

	// Second run with identical bars must not fail (ON CONFLICT DO NOTHING).
	if err := RunIngestion(ctx, database, source, symbol, tf, "none", "futu", time.Time{}, time.Time{}, src); err != nil {
		t.Fatal(err)
	}
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = $1 AND timeframe = $2`, string(symbol), tf).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("bars count after repeat: got %d want 2", n)
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

func TestRunStatusCountsIntegration(t *testing.T) {
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
	source := "counts-test"
	symbol := domain.Symbol("COUNTS.US")
	tf := "1d"
	if err := RunMockIngestion(ctx, database, source, symbol, tf); err != nil {
		t.Fatal(err)
	}

	counts, err := RunStatusCounts(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Succeeded < 1 {
		t.Fatalf("counts: got %+v; want succeeded >= 1", counts)
	}
	if counts.Running < 0 || counts.Failed < 0 {
		t.Fatalf("counts: got %+v; want non-negative", counts)
	}
}

func TestQueryBarCoverageIntegration(t *testing.T) {
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
	source := "coverage-test"
	symbol := domain.Symbol("COVERAGE.US")
	tf := "1d"
	if err := RunMockIngestion(ctx, database, source, symbol, tf); err != nil {
		t.Fatal(err)
	}

	coverage, err := QueryBarCoverage(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	var found *BarCoverage
	for i := range coverage {
		if coverage[i].Symbol == string(symbol) && coverage[i].Timeframe == tf {
			found = &coverage[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("coverage missing %s %s (got %+v)", symbol, tf, coverage)
	}
	if found.Count != 3 {
		t.Fatalf("coverage count: got %d want 3", found.Count)
	}
	if !found.MinTs.Before(found.MaxTs) {
		t.Fatalf("coverage ts: got %v..%v; want min before max", found.MinTs, found.MaxTs)
	}
}

func TestQueryBarsIntegration(t *testing.T) {
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
	source := "bars-query-test"
	symbol := domain.Symbol("QUERY.US")
	tf := "1d"
	if err := RunMockIngestion(ctx, database, source, symbol, tf); err != nil {
		t.Fatal(err)
	}

	// Full range: all 3 bars, ts ascending.
	bars, err := QueryBars(ctx, database, string(symbol), tf, "none", time.Time{}, time.Time{}, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 3 {
		t.Fatalf("full query: got %d bars want 3", len(bars))
	}
	for i := 1; i < len(bars); i++ {
		if !bars[i].Ts.After(bars[i-1].Ts) {
			t.Fatalf("bar %d ts %v not after previous %v", i, bars[i].Ts, bars[i-1].Ts)
		}
	}

	// Closed range [middle ts, last ts]: 2 bars, both endpoints included.
	from := bars[1].Ts
	to := bars[2].Ts
	got, err := QueryBars(ctx, database, string(symbol), tf, "none", from, to, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("range query: got %d bars want 2", len(got))
	}
	if !got[0].Ts.Equal(from) || !got[len(got)-1].Ts.Equal(to) {
		t.Fatalf("range query: endpoints got %v..%v want %v..%v", got[0].Ts, got[len(got)-1].Ts, from, to)
	}

	// limit=1: only the first bar.
	got, err = QueryBars(ctx, database, string(symbol), tf, "none", time.Time{}, time.Time{}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("limit query: got %d bars want 1", len(got))
	}
}

// TestQueryFreshnessIntegration: QueryFreshness reports max_ts ages per
// symbol×timeframe, and JudgeFreshness with the per-timeframe default
// threshold classifies fresh (2h old, 1d) and stale (100h old, 1d).
func TestQueryFreshnessIntegration(t *testing.T) {
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

	const freshSym = "FRESH.US"
	const staleSym = "STALE.US"
	for _, sym := range []string{freshSym, staleSym} {
		if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, sym); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(sym string, ts time.Time) {
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, 100, 101, 99, 100.5, 100, 'none', 'futu')`, sym, ts); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	insert(freshSym, now.Add(-2*time.Hour))   // 1d default threshold 72h → fresh
	insert(staleSym, now.Add(-100*time.Hour)) // 1d default threshold 72h → stale

	entries, err := QueryFreshness(ctx, database, now)
	if err != nil {
		t.Fatal(err)
	}
	find := func(sym string) *Freshness {
		for i := range entries {
			if entries[i].Symbol == sym {
				return &entries[i]
			}
		}
		t.Fatalf("freshness missing %s (got %+v)", sym, entries)
		return nil
	}
	fresh := find(freshSym)
	if fresh.AgeSeconds != 7200 || !fresh.MaxTs.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("fresh entry = %+v; want age 7200s at %v", fresh, now.Add(-2*time.Hour))
	}
	if got := JudgeFreshness(fresh.MaxTs, now, MaxAgeForTimeframe(fresh.Timeframe)); got != Fresh {
		t.Fatalf("fresh judge = %q; want fresh", got)
	}
	stale := find(staleSym)
	if got := JudgeFreshness(stale.MaxTs, now, MaxAgeForTimeframe(stale.Timeframe)); got != Stale {
		t.Fatalf("stale judge = %q; want stale", got)
	}
	// -max-age style global override: 24h flips the 100h-old entry's verdict.
	if got := JudgeFreshness(stale.MaxTs, now, 24*time.Hour); got != Stale {
		t.Fatalf("stale judge with 24h = %q; want stale", got)
	}
	if got := JudgeFreshness(fresh.MaxTs, now, 24*time.Hour); got != Fresh {
		t.Fatalf("fresh judge with 24h = %q; want fresh", got)
	}
}

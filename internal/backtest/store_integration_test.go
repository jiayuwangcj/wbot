package backtest

// Integration tests for SaveResult/LoadResults (backtest_results table);
// require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestSaveLoadResultsIntegration(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	symbol := "SAVE.US"
	strategy := "buy-hold"
	// Self-cleaning: the local dev DB persists between runs (CI uses a fresh one).
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

	id, err := SaveResult(ctx, database, strategy, symbol,
		map[string]any{"cash": 10000.0, "fee": 0.0, "adjust": "fwd"},
		&Result{Equity: 10500, TotalReturn: 0.05, MaxDrawdown: 0.02, Bars: 5},
		start, end)
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("SaveResult id = %d; want > 0", id)
	}

	recs, err := LoadResults(ctx, database, symbol, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("LoadResults = %d rows; want 1", len(recs))
	}
	r := recs[0]
	if r.ID != id || r.Strategy != strategy || r.Symbol != symbol {
		t.Fatalf("record = %+v; want id=%d strategy=%s symbol=%s", r, id, strategy, symbol)
	}
	if r.Params["cash"] != 10000.0 || r.Params["adjust"] != "fwd" {
		t.Fatalf("params = %v; want cash=10000 adjust=fwd", r.Params)
	}
	if r.Metrics["equity"] != 10500.0 || r.Metrics["total_return"] != 0.05 || r.Metrics["bars"] != float64(5) {
		t.Fatalf("metrics = %v; want equity=10500 total_return=0.05 bars=5", r.Metrics)
	}
	if !r.StartTs.Equal(start) || !r.EndTs.Equal(end) {
		t.Fatalf("ts = %v..%v; want %v..%v", r.StartTs, r.EndTs, start, end)
	}
	if r.CreatedAt.IsZero() {
		t.Fatal("created_at is zero; want default now()")
	}

	// Strategy filter + ordering: a second run with another strategy comes last.
	if _, err := SaveResult(ctx, database, "hold", symbol, map[string]any{}, &Result{Bars: 2}, start, end); err != nil {
		t.Fatal(err)
	}
	recs, err = LoadResults(ctx, database, symbol, strategy, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Strategy != strategy {
		t.Fatalf("strategy-filtered = %+v; want only %s", recs, strategy)
	}
	recs, err = LoadResults(ctx, database, symbol, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Strategy != "hold" {
		t.Fatalf("limit-1 = %+v; want the newest (hold)", recs)
	}
	recs, err = LoadResults(ctx, database, symbol, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) < 2 {
		t.Fatalf("limit-0 = %d rows; want >= 2 (default limit)", len(recs))
	}
}

// TestSaveLoadResultDetailIntegration: the equity_curve/trades trace (migration
// 004) round-trips through SaveResult/LoadResult; stock trades get the symbol
// filled, list rows stay curve-free, and a metrics-only row reads back clean.
func TestSaveLoadResultDetailIntegration(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	symbol := "CURVE.US"
	strategy := "buy-hold"
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	res := &Result{
		Equity: 10500, TotalReturn: 0.05, MaxDrawdown: 0.02, Bars: 2,
		EquityCurve: []EquityPoint{
			{Ts: start, Equity: 10000},
			{Ts: end, Equity: 10500},
		},
		Trades: []Trade{
			{Ts: start, Action: "buy", Size: 100, Price: 100, CashAfter: 0},
			{Ts: end, Action: "sell-call", Symbol: "C105", Size: 1, Price: 2.5, CashAfter: 250},
		},
	}
	id, err := SaveResult(ctx, database, strategy, symbol,
		map[string]any{"cash": 10000.0, "fee": 0.0}, res, start, end)
	if err != nil {
		t.Fatal(err)
	}

	rec, err := LoadResult(ctx, database, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.EquityCurve) != 2 || !rec.EquityCurve[1].Ts.Equal(end) || rec.EquityCurve[1].Equity != 10500 {
		t.Fatalf("equity curve = %+v; want 2 points ending at 10500 on %v", rec.EquityCurve, end)
	}
	if len(rec.Trades) != 2 {
		t.Fatalf("trades = %+v; want 2", rec.Trades)
	}
	// The stock trade's empty symbol is filled with the underlying.
	if rec.Trades[0].Symbol != symbol || rec.Trades[0].Action != "buy" {
		t.Fatalf("trades[0] = %+v; want buy with symbol %s", rec.Trades[0], symbol)
	}
	// Option trades keep their contract code.
	if rec.Trades[1].Symbol != "C105" || rec.Trades[1].Action != "sell-call" {
		t.Fatalf("trades[1] = %+v; want sell-call on C105", rec.Trades[1])
	}

	// The list view stays curve-free (summary only).
	recs, err := LoadResults(ctx, database, symbol, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].EquityCurve != nil || recs[0].Trades != nil {
		t.Fatalf("list row = %+v; want no trace loaded", recs)
	}

	// Metrics-only save (no trace): columns stay NULL, LoadResult reads back clean.
	oldID, err := SaveResult(ctx, database, "hold", symbol, map[string]any{}, &Result{Bars: 1}, start, end)
	if err != nil {
		t.Fatal(err)
	}
	oldRec, err := LoadResult(ctx, database, oldID)
	if err != nil {
		t.Fatal(err)
	}
	if oldRec.EquityCurve != nil || oldRec.Trades != nil {
		t.Fatalf("metrics-only row = %+v; want nil trace", oldRec)
	}

	if _, err := LoadResult(ctx, database, id+1000000); err != ErrResultNotFound {
		t.Fatalf("LoadResult(missing) error = %v; want ErrResultNotFound", err)
	}
}

func TestSaveResultValidation(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	r := &Result{Bars: 1}
	if _, err := SaveResult(ctx, nil, "hold", "X.US", nil, r, start, end); err == nil {
		t.Fatal("SaveResult(nil db) = nil error; want error")
	}
	if _, err := SaveResult(ctx, &sql.DB{}, "", "X.US", nil, r, start, end); err == nil {
		t.Fatal("SaveResult(empty strategy) = nil error; want error")
	}
	if _, err := SaveResult(ctx, &sql.DB{}, "hold", "", nil, r, start, end); err == nil {
		t.Fatal("SaveResult(empty symbol) = nil error; want error")
	}
	if _, err := SaveResult(ctx, &sql.DB{}, "hold", "X.US", nil, nil, start, end); err == nil {
		t.Fatal("SaveResult(nil result) = nil error; want error")
	}
	if _, err := SaveResult(ctx, &sql.DB{}, "hold", "X.US", nil, r, end, start); err == nil {
		t.Fatal("SaveResult(start after end) = nil error; want error")
	}
	if _, err := LoadResults(ctx, nil, "X.US", "", 10); err == nil {
		t.Fatal("LoadResults(nil db) = nil error; want error")
	}
	if _, err := LoadResults(ctx, &sql.DB{}, "", "", 10); err == nil {
		t.Fatal("LoadResults(empty symbol) = nil error; want error")
	}
	if _, err := ListResults(ctx, nil, "", "", "", 10, "", false); err == nil {
		t.Fatal("ListResults(nil db) = nil error; want error")
	}
	if _, err := LoadResult(ctx, nil, 1); err == nil {
		t.Fatal("LoadResult(nil db) = nil error; want error")
	}
	if _, err := LoadResult(ctx, &sql.DB{}, 0); err == nil {
		t.Fatal("LoadResult(id 0) = nil error; want error")
	}
}

// TestListResultsQueryIntegration: q 参数对 symbol/strategy 做 ILIKE 包含
// 匹配(通配符按字面转义),与精确过滤、limit 组合。
func TestListResultsQueryIntegration(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	// Self-cleaning: 只用本测试专属符号,避免污染其他断言。
	base := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	symbols := []string{"ZZSEARCH.US", "ZZFIND.HK", "ZZUNRELATED.US"}
	for _, s := range symbols {
		if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, s); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, s := range symbols {
			if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, s); err != nil {
				t.Logf("cleanup %s: %v", s, err)
			}
		}
	}()
	for i, s := range symbols {
		id, err := SaveResult(ctx, database, "covered-call", s,
			map[string]any{"cash": 10000.0},
			&Result{Equity: 10000.0 + float64(i), TotalReturn: 0, MaxDrawdown: 0, Bars: 1},
			base, base.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.Exec(`DELETE FROM backtest_results WHERE id = $1`, id) })
	}

	// 断言一律用 ZZ* 专属符号前缀:本地 dev 库含历史 covered-call 记录,
	// 共享环境下 count 断言会被污染,专属前缀保证只命中本测试插入的行。
	cases := []struct {
		name      string
		q         string
		wantCount int
	}{
		{"symbol contains", "ZZSEARCH", 1},
		{"case-insensitive", "zzsearch", 1},
		{"hk suffix", "ZZFIND", 1},
		{"no match", "ZZSEARCHNOPE", 0},
	}
	for _, tc := range cases {
		recs, err := ListResults(ctx, database, "", "", tc.q, 0, "", false)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(recs) != tc.wantCount {
			t.Fatalf("%s: len = %d; want %d (got %v)", tc.name, len(recs), tc.wantCount, recs)
		}
	}

	// strategy 命中时每条都应包含 q(全库历史记录不算数,只查专属符号)。
	recs, err := ListResults(ctx, database, "", "", "covered", 0, "", false)
	if err != nil {
		t.Fatal(err)
	}
	zz := map[string]bool{}
	for _, r := range recs {
		if r.Strategy != "covered-call" {
			t.Fatalf("strategy match: unexpected %q %s", r.Strategy, r.Symbol)
		}
		if r.Symbol != "" && len(r.Symbol) > 2 && r.Symbol[:2] == "ZZ" {
			zz[r.Symbol] = true
		}
	}
	for _, s := range symbols {
		if !zz[s] {
			t.Fatalf("strategy match: missing %s in results", s)
		}
	}

	// 通配符按字面匹配:% 不应命中所有行。
	recs, err = ListResults(ctx, database, "", "", "%", 0, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("literal %%: len = %d; want 0", len(recs))
	}

	// 与精确过滤组合 + limit。
	recs, err = ListResults(ctx, database, "ZZSEARCH.US", "", "ZZSEARCH", 1, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Symbol != "ZZSEARCH.US" {
		t.Fatalf("combined: got %v; want exactly ZZSEARCH.US", recs)
	}
}

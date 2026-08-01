package ingest

// Unit + integration tests for the futu-option pipeline (chain -> K-lines ->
// option_quotes, cache-first semantics). Integration parts need WBOT_PG_DSN.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
)

const expirationsPayload = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"date_list":[
	{"strike_time":"2026-07-31","strike_timestamp":1785427200.0,"option_expiry_date_distance":-1,"cycle":1},
	{"strike_time":"2026-08-07","strike_timestamp":1786032000.0,"option_expiry_date_distance":6,"cycle":1},
	{"strike_time":"2026-08-28","strike_timestamp":1787846400.0,"option_expiry_date_distance":27,"cycle":2}
]}}`

const chainTwoExpiriesPayload = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"option_chain":[
	{"strike_time":"2026-08-07","strike_timestamp":1786032000.0,"option":[
		{"call":{"basic":{"security":{"market":1,"code":"TCH260807C335000"},"lot_size":100,"name":"腾讯 260807 335.00 购"},"option_ex_data":{"type":1,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-07","strike_price":335.0}},
		 "put":{"basic":{"security":{"market":1,"code":"TCH260807P335000"},"lot_size":100,"name":"腾讯 260807 335.00 沽"},"option_ex_data":{"type":2,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-07","strike_price":335.0}}}
	]},
	{"strike_time":"2026-08-28","strike_timestamp":1787846400.0,"option":[
		{"call":{"basic":{"security":{"market":1,"code":"TCH260828C750000"},"lot_size":100,"name":"腾讯 260828 750.00 购"},"option_ex_data":{"type":1,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-28","strike_price":750.0}},
		 "put":{"basic":{"security":{"market":1,"code":"TCH260828P750000"},"lot_size":100,"name":"腾讯 260828 750.00 沽"},"option_ex_data":{"type":2,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-28","strike_price":750.0}}}
	]}
]}}`

// klineForCode renders a one-week daily K-line payload for a given option code.
func klineForCode(code string) string {
	// ts 1785427200 = 2026-07-31 UTC, the last trading day of the week.
	return `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
		{"time":"2026-07-27 00:00:00","is_blank":false,"high_price":100.0,"open_price":99.0,"low_price":98.0,"close_price":99.5,"volume":1000,"timestamp":1785081600.0},
		{"time":"2026-07-28 00:00:00","is_blank":false,"high_price":101.0,"open_price":99.5,"low_price":99.0,"close_price":100.5,"volume":1200,"timestamp":1785168000.0},
		{"time":"2026-07-29 00:00:00","is_blank":false,"high_price":102.0,"open_price":100.5,"low_price":100.0,"close_price":101.5,"volume":900,"timestamp":1785254400.0},
		{"time":"2026-07-30 00:00:00","is_blank":false,"high_price":103.0,"open_price":101.5,"low_price":101.0,"close_price":102.5,"volume":1100,"timestamp":1785340800.0},
		{"time":"2026-07-31 00:00:00","is_blank":false,"high_price":104.0,"open_price":102.5,"low_price":102.0,"close_price":103.5,"volume":1300,"timestamp":1785427200.0}
	],"next_req_key":null}}`
}

// optionGatewayServer serves the three option endpoints with canned payloads.
func optionGatewayServer(t *testing.T, kline func(code string) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		switch r.URL.Path {
		case "/api/option-expiration-date":
			body = []byte(expirationsPayload)
		case "/api/option-chain":
			body = []byte(chainTwoExpiriesPayload)
		case "/api/history-kline":
			var req struct {
				Security struct {
					Code string `json:"code"`
				} `json:"security"`
			}
			_ = json.Unmarshal(mustReadAll(r), &req)
			body = []byte(kline(req.Security.Code))
		default:
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, string(body))
	}))
}

func TestRunOptionIngestionValidation(t *testing.T) {
	fastFutuLimits(t)
	ctx := context.Background()
	from := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	c := futu.NewClient("http://127.0.0.1:1")
	if _, err := RunOptionIngestion(ctx, nil, c, "HK.00700", "fwd", from, to, 1); err == nil {
		t.Fatal("RunOptionIngestion(nil db) = nil error; want error")
	}
	if _, err := RunOptionIngestion(ctx, stubDB(), nil, "HK.00700", "fwd", from, to, 1); err == nil {
		t.Fatal("RunOptionIngestion(nil client) = nil error; want error")
	}
	if _, err := RunOptionIngestion(ctx, stubDB(), c, "HK.00700", "bogus", from, to, 1); err == nil {
		t.Fatal("RunOptionIngestion(bad adjust) = nil error; want error")
	}
	if _, err := RunOptionIngestion(ctx, stubDB(), c, "HK.00700", "fwd", to, from, 1); err == nil {
		t.Fatal("RunOptionIngestion(from after to) = nil error; want error")
	}
	if _, err := RunOptionIngestion(ctx, stubDB(), c, "00700", "fwd", from, to, 1); err == nil {
		t.Fatal("RunOptionIngestion(bad symbol) = nil error; want error")
	}
}

func TestFetchOptionRows(t *testing.T) {
	fastFutuLimits(t)
	srv := optionGatewayServer(t, func(code string) string { return klineForCode(code) })
	defer srv.Close()

	expiries, err := futu.NewClient(srv.URL).OptionExpirations(context.Background(), "HK.00700")
	if err != nil {
		t.Fatal(err)
	}
	var future []futu.OptionExpiry
	for _, e := range expiries {
		if e.DistanceDays >= 0 {
			future = append(future, e)
		}
	}
	if len(future) != 2 {
		t.Fatalf("future expiries = %d; want 2 (past expiry filtered)", len(future))
	}
	contracts, err := futu.NewClient(srv.URL).OptionChain(context.Background(), "HK.00700", future[0].Timestamp, future[1].Timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 4 {
		t.Fatalf("contracts = %d; want 4 (2 expiries x call+put)", len(contracts))
	}

	from := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rows, used, err := fetchOptionRows(context.Background(), futu.NewClient(srv.URL), "HK.00700", "fwd", 1, from, to, contracts)
	if err != nil {
		t.Fatalf("fetchOptionRows error: %v", err)
	}
	if used != 4 || len(rows) != 20 {
		t.Fatalf("used=%d rows=%d; want 4 contracts x 5 bars = 20", used, len(rows))
	}
	r0 := rows[0]
	if r0.Symbol != "HK.TCH260807C335000" || r0.OptionType != "call" || r0.Strike != 335.0 {
		t.Fatalf("row[0] = %+v; want call 335 at 08-07", r0)
	}
	if !r0.Expiry.Equal(time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("row[0] expiry = %v; want 2026-08-07 chain expiry", r0.Expiry)
	}
	// First bar is 2026-07-27 in +08 market time = 2026-07-26T16:00:00Z.
	if !r0.Ts.Equal(time.Date(2026, 7, 26, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("row[0] ts = %v; want 2026-07-26T16:00:00Z", r0.Ts)
	}
	last := rows[len(rows)-1]
	if !last.Ts.Equal(time.Unix(1785427200, 0).UTC()) || last.Close != 103.5 {
		t.Fatalf("last row ts/close = %v/%v; want 07-31 103.5", last.Ts, last.Close)
	}
}

func TestOptionCacheHelpersValidation(t *testing.T) {
	ctx := context.Background()
	from := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := OptionQuotesCached(ctx, nil, "HK.00700", from, to, "fwd"); err == nil {
		t.Fatal("OptionQuotesCached(nil db) = nil error; want error")
	}
	if _, _, err := OptionQuotesCached(ctx, stubDB(), "", from, to, "fwd"); err == nil {
		t.Fatal("OptionQuotesCached(empty underlying) = nil error; want error")
	}
	if _, _, err := BarsCached(ctx, nil, "HK.00700", "1d", "fwd", from, to); err == nil {
		t.Fatal("BarsCached(nil db) = nil error; want error")
	}
	if _, _, err := BarsCached(ctx, stubDB(), "HK.00700", "", "fwd", from, to); err == nil {
		t.Fatal("BarsCached(empty timeframe) = nil error; want error")
	}
	if err := UpsertWatchlist(ctx, nil, "HK.00700", "option-watch", nil); err == nil {
		t.Fatal("UpsertWatchlist(nil db) = nil error; want error")
	}
	if err := UpsertWatchlist(ctx, stubDB(), "", "option-watch", nil); err == nil {
		t.Fatal("UpsertWatchlist(empty symbol) = nil error; want error")
	}
}

func TestRunOptionIngestionIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	fastFutuLimits(t)
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}

	srv := optionGatewayServer(t, func(code string) string { return klineForCode(code) })
	defer srv.Close()

	// Self-cleaning: the local dev DB persists between runs (CI uses a fresh one).
	if _, err := database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, "HK.00700"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM watchlist WHERE symbol = $1`, "HK.00700"); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	from := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	stats, err := RunOptionIngestion(ctx, database, futu.NewClient(srv.URL), "HK.00700", "fwd", from, to, 2)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Expiries != 2 || stats.Contracts != 4 || stats.Rows != 20 {
		t.Fatalf("stats = %+v; want expiries=2 contracts=4 rows=20", stats)
	}

	var n int
	err = database.QueryRow(`
SELECT COUNT(*) FROM option_quotes WHERE underlying = $1 AND adjust = $2`, "HK.00700", "fwd").Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Fatalf("option_quotes rows = %d; want 20", n)
	}
	var ot, sym string
	var strike float64
	err = database.QueryRow(`
SELECT option_type, strike, symbol FROM option_quotes
WHERE underlying = $1 AND ts = $2 LIMIT 1`, "HK.00700", time.Unix(1785427200, 0).UTC()).Scan(&ot, &strike, &sym)
	if err != nil {
		t.Fatal(err)
	}
	if ot != "call" && ot != "put" {
		t.Fatalf("option_type = %q; want call or put", ot)
	}
	if strike <= 0 || !strings.HasPrefix(sym, "HK.TCH26") {
		t.Fatalf("row = (%s, %v, %s); want sane strike and HK.TCH26... symbol", ot, strike, sym)
	}

	// Watchlist upsert: second pull must not duplicate it.
	if err := UpsertWatchlist(ctx, database, "HK.00700", "option-watch", map[string]any{"expiries": 2}); err != nil {
		t.Fatal(err)
	}
	var wl int
	err = database.QueryRow(`SELECT COUNT(*) FROM watchlist WHERE symbol = $1`, "HK.00700").Scan(&wl)
	if err != nil {
		t.Fatal(err)
	}
	if wl != 1 {
		t.Fatalf("watchlist rows = %d; want 1 (upsert)", wl)
	}

	// Cache helpers: the window is covered now.
	hit, count, err := OptionQuotesCached(ctx, database, "HK.00700", from, to, "fwd")
	if err != nil {
		t.Fatal(err)
	}
	if !hit || count != 20 {
		t.Fatalf("cache hit = %v count = %d; want true 20", hit, count)
	}
	hit, _, err = OptionQuotesCached(ctx, database, "HK.00700", from, to, "none")
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatal("cache hit for adjust=none; want miss (only fwd was ingested)")
	}

	// Re-run is idempotent: same row count after a second full pull.
	stats, err = RunOptionIngestion(ctx, database, futu.NewClient(srv.URL), "HK.00700", "fwd", from, to, 2)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows != 0 {
		t.Fatalf("re-run stats = %+v; want rows=0 (all ON CONFLICT DO NOTHING)", stats)
	}
}

func TestBarsCachedIntegration(t *testing.T) {
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
	from := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	symbol := domain.Symbol("BARSCACHE.US")
	if err := RunMockIngestion(ctx, database, "barscache-test", symbol, "1d"); err != nil {
		t.Fatal(err)
	}
	// Mock bars sit in 2024-06; the 2026-07 window must be a cache miss.
	hit, _, err := BarsCached(ctx, database, string(symbol), "1d", "none", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatal("BarsCached hit for unrelated window; want miss")
	}
}

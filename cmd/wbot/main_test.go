package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/httpapi"
	"github.com/jiayu/wbot/internal/httpregister"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/master"
	"github.com/jiayu/wbot/internal/watchlist"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"no args", []string{"wbot"}, 2},
		{"help flag", []string{"wbot", "-h"}, 0},
		{"help long", []string{"wbot", "--help"}, 0},
		{"help cmd", []string{"wbot", "help"}, 0},
		{"version flag", []string{"wbot", "-version"}, 0},
		{"version cmd", []string{"wbot", "version"}, 0},
		{"agent poll smoke", []string{"wbot", "agent", "-duration", "1ms", "-interval", "1ms"}, 0},
		{"agent help", []string{"wbot", "agent", "-h"}, 0},
		{"master short run", []string{"wbot", "master", "-duration", "1ms"}, 0},
		{"master tls flag mismatch", []string{"wbot", "master", "-tls-cert", "only.pem"}, 2},
		{"paper submit", []string{"wbot", "paper", "-symbol", "T.US", "-side", "sell"}, 0},
		{"paper bad side", []string{"wbot", "paper", "-side", "maybe"}, 2},
		{"agent bad flag", []string{"wbot", "agent", "-notaflag"}, 2},
		{"unknown", []string{"wbot", "nope"}, 2},
		{"ingest no sub", []string{"wbot", "ingest"}, 2},
		{"ingest help", []string{"wbot", "ingest", "-h"}, 0},
		{"ingest bad sub", []string{"wbot", "ingest", "nope"}, 2},
		{"ingest mock help", []string{"wbot", "ingest", "mock", "-h"}, 0},
		{"ingest mock bad from", []string{"wbot", "ingest", "mock", "-from", "not-a-time"}, 2},
		{"ingest mock bad to", []string{"wbot", "ingest", "mock", "-to", "x"}, 2},
		{"ingest mock bad adjust", []string{"wbot", "ingest", "mock", "-adjust", "splits"}, 2},
		{"ingest file bad from", []string{"wbot", "ingest", "file", "-file", "/dev/null", "-from", "not-a-time"}, 2},
		{"ingest file help", []string{"wbot", "ingest", "file", "-h"}, 0},
		{"ingest file no path", []string{"wbot", "ingest", "file"}, 2},
		{"ingest url help", []string{"wbot", "ingest", "url", "-h"}, 0},
		{"ingest url bad to", []string{"wbot", "ingest", "url", "-url", "http://127.0.0.1:1/bars.json", "-to", "x"}, 2},
		{"ingest url no url", []string{"wbot", "ingest", "url"}, 2},
		{"ingest mock unknown provider", []string{"wbot", "ingest", "mock", "-provider", "nope"}, 2},
		{"ingest file unknown provider", []string{"wbot", "ingest", "file", "-file", "/dev/null", "-provider", "nope"}, 2},
		{"ingest url unknown provider", []string{"wbot", "ingest", "url", "-url", "http://127.0.0.1:1/bars.json", "-provider", "nope"}, 2},
		{"ingest status help", []string{"wbot", "ingest", "status", "-h"}, 0},
		{"ingest freshness help", []string{"wbot", "ingest", "freshness", "-h"}, 0},
		{"ingest freshness bad flag", []string{"wbot", "ingest", "freshness", "-notaflag"}, 2},
		{"ingest bars help", []string{"wbot", "ingest", "bars", "-h"}, 0},
		{"ingest bars bad from", []string{"wbot", "ingest", "bars", "-from", "not-a-time"}, 2},
		{"backtest help", []string{"wbot", "backtest", "-h"}, 0},
		{"backtest both inputs", []string{"wbot", "backtest", "-file", "/dev/null", "-dsn", "postgres://x"}, 2},
		{"backtest dsn no value", []string{"wbot", "backtest", "-dsn"}, 2},
		{"backtest bad strategy", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "nope"}, 2},
		{"backtest bad params json", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "hold", "-params", "{"}, 2},
		{"backtest params with hold", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "hold", "-params", `{"a":1}`}, 2},
		{"backtest covered-call bad param", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "covered-call", "-params", `{"strike_pct_otm":-1}`}, 2},
		{"backtest covered-call unknown param", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "covered-call", "-params", `{"bogus":1}`}, 2},
		{"backtest covered-call needs dsn", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "covered-call"}, 2},
		{"backtest cash-secured-put needs dsn", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "cash-secured-put"}, 2},
		{"backtest bad maxdrawdown high", []string{"wbot", "backtest", "-file", "/dev/null", "-max-drawdown", "1.5"}, 2},
		{"backtest bad maxdrawdown neg", []string{"wbot", "backtest", "-file", "/dev/null", "-max-drawdown", "-0.1"}, 2},
		{"backtest save with file", []string{"wbot", "backtest", "-file", "/dev/null", "-save"}, 2},
		{"backtest multi with file", []string{"wbot", "backtest", "-file", "/dev/null", "-symbols", "A.US,B.US"}, 2},
		{"backtest multi duplicate symbol", []string{"wbot", "backtest", "-dsn", "postgres://x", "-symbols", "A.US,A.US"}, 2},
		{"backtest multi empty entry", []string{"wbot", "backtest", "-dsn", "postgres://x", "-symbols", "A.US,,B.US"}, 2},
		{"backtest multi option strategy", []string{"wbot", "backtest", "-dsn", "postgres://x", "-symbols", "A.US,B.US", "-strategy", "covered-call"}, 2},
		{"backtest multi save unsupported", []string{"wbot", "backtest", "-dsn", "postgres://x", "-symbols", "A.US,B.US", "-save"}, 2},
		{"backtest export with file", []string{"wbot", "backtest", "-file", "/dev/null", "-export", "3"}, 2},
		{"backtest export bad id", []string{"wbot", "backtest", "-export", "-1", "-dsn", "postgres://x"}, 2},
		{"backtest export bad format", []string{"wbot", "backtest", "-export", "3", "-dsn", "postgres://x", "-format", "xml"}, 2},
		{"watchlist no sub", []string{"wbot", "watchlist"}, 2},
		{"watchlist help", []string{"wbot", "watchlist", "-h"}, 0},
		{"watchlist bad sub", []string{"wbot", "watchlist", "nope"}, 2},
		{"watchlist add help", []string{"wbot", "watchlist", "add", "-h"}, 0},
		{"watchlist remove help", []string{"wbot", "watchlist", "remove", "-h"}, 0},
		{"watchlist list help", []string{"wbot", "watchlist", "list", "-h"}, 0},
		{"watchlist add no symbol", []string{"wbot", "watchlist", "add", "-strategy", "covered-call"}, 2},
		{"watchlist add no strategy", []string{"wbot", "watchlist", "add", "-symbol", "HK.00700"}, 2},
		{"watchlist add bad params", []string{"wbot", "watchlist", "add", "-symbol", "HK.00700", "-strategy", "covered-call", "-params", "not-json"}, 2},
		{"watchlist add unknown strategy", []string{"wbot", "watchlist", "add", "-symbol", "HK.00700", "-strategy", "nope"}, 2},
		{"watchlist add unknown param", []string{"wbot", "watchlist", "add", "-symbol", "HK.00700", "-strategy", "covered-call", "-params", `{"nope":1}`}, 2},
		{"watchlist add param type", []string{"wbot", "watchlist", "add", "-symbol", "HK.00700", "-strategy", "covered-call", "-params", `{"strike_pct_otm":"0.03"}`}, 2},
		{"watchlist remove no symbol", []string{"wbot", "watchlist", "remove"}, 2},
		{"serve help", []string{"wbot", "serve", "-h"}, 0},
		{"configyaml help", []string{"wbot", "configyaml", "-h"}, 0},
		{"configyaml missing file", []string{"wbot", "configyaml", "-file", "/nonexistent/config.yaml"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run() = %d; want %d", got, tt.want)
			}
		})
	}
}

// TestRunRequiresDSN: exit-2 cases that assume WBOT_PG_DSN is unset (missing-DSN
// rejection); skipped under db-integration, where the DSN is present by design.
func TestRunRequiresDSN(t *testing.T) {
	if os.Getenv("WBOT_PG_DSN") != "" {
		t.Skip("WBOT_PG_DSN set; missing-DSN exit codes not applicable")
	}
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"ingest mock no dsn", []string{"wbot", "ingest", "mock"}, 2},
		{"ingest file valid provider no dsn", []string{"wbot", "ingest", "file", "-file", "/dev/null", "-provider", "file"}, 2},
		{"ingest mock no dsn with every", []string{"wbot", "ingest", "mock", "-every", "1ms"}, 2},
		{"ingest file no dsn", []string{"wbot", "ingest", "file", "-file", "/dev/null"}, 2},
		{"ingest url no dsn", []string{"wbot", "ingest", "url", "-url", "http://127.0.0.1:1/bars.json"}, 2},
		{"ingest futu no dsn", []string{"wbot", "ingest", "futu", "-symbol", "HK.00700", "-timeframe", "K_DAY"}, 2},
		{"ingest futu-option no dsn", []string{"wbot", "ingest", "futu-option", "-symbol", "HK.00700"}, 2},
		{"ingest status no dsn", []string{"wbot", "ingest", "status"}, 2},
		{"ingest freshness no dsn", []string{"wbot", "ingest", "freshness"}, 2},
		{"ingest bars no dsn", []string{"wbot", "ingest", "bars"}, 2},
		{"ingest bars json no dsn", []string{"wbot", "ingest", "bars", "-json"}, 2},
		{"serve no dsn", []string{"wbot", "serve"}, 2},
		{"backtest no file", []string{"wbot", "backtest"}, 2},
		{"backtest export no dsn", []string{"wbot", "backtest", "-export", "3"}, 2},
		{"watchlist add no dsn", []string{"wbot", "watchlist", "add", "-symbol", "HK.00700", "-strategy", "covered-call"}, 2},
		{"watchlist remove no dsn", []string{"wbot", "watchlist", "remove", "-symbol", "HK.00700"}, 2},
		{"watchlist list no dsn", []string{"wbot", "watchlist", "list"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run() = %d; want %d", got, tt.want)
			}
		})
	}
}

// TestBacktestOptionStrategyIntegration: full CLI path against real PG —
// option_quotes + bars seeded, covered-call run with -params prints the
// deterministic summary (skipped without WBOT_PG_DSN).
func TestBacktestOptionStrategyIntegration(t *testing.T) {
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

	const symbol = "STRATCLI.US"
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	for i, c := range []float64{100, 103, 99, 106, 104} {
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'none', 'futu')`, symbol, day(i), c); err != nil {
			t.Fatal(err)
		}
	}
	quotes := []struct {
		code   string
		opt    string
		strike float64
		expiry int
		closes []float64
	}{
		{"CLIO105C", "call", 105, 2, []float64{3, 2.5, 1}},
		{"CLIO110C", "call", 110, 4, []float64{1, 0.8, 0.5, 0.2}},
	}
	for _, q := range quotes {
		for i, c := range q.closes {
			if _, err := database.Exec(`
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $7, $7, 10, NULL, 'none', 'futu')`,
				q.code, symbol, q.opt, q.strike, day(q.expiry), day(i), c); err != nil {
				t.Fatal(err)
			}
		}
	}

	argv := []string{"wbot", "backtest", "-dsn", dsn, "-symbol", symbol, "-timeframe", "1d", "-adjust", "none",
		"-strategy", "covered-call", "-params", `{"strike_pct_otm":0.05}`}
	// buy 100 @100, sell C105 for 250, OTM void; sell C110 for 20, OTM void:
	// cash 270 + 100*104 (same fixture as internal/strategy's integration test).
	out := captureRunOutput(t, argv)
	for _, want := range []string{"final_equity=10670", "total_return=0.067", "bars=5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q; want containing %q", out, want)
		}
	}
	// Determinism: a second run prints the identical summary line.
	if out2 := captureRunOutput(t, argv); out2 != out {
		t.Fatalf("runs differ: %q vs %q", out2, out)
	}
}

// TestBacktestExportIntegration: CLI -save → -export roundtrips against the
// result API (skipped without WBOT_PG_DSN): exported json == GET detail body
// and exported csv == GET export body, CLI and API from one serializer.
func TestBacktestExportIntegration(t *testing.T) {
	// CSV 断言写死 UTC "Z" 格式,固定时区避免本地 TZ 差异。
	t.Setenv("TZ", "UTC")
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

	const symbol = "EXPORTCLI.US"
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 3, 1+i, 0, 0, 0, 0, time.UTC) }
	for i, c := range []float64{100, 103, 99} {
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'none', 'futu')`, symbol, day(i), c); err != nil {
			t.Fatal(err)
		}
	}

	// buy-hold: one buy at bar 1 close, 3 curve points (equity = 100 * close).
	out := captureRunOutput(t, []string{"wbot", "backtest", "-dsn", dsn, "-symbol", symbol, "-timeframe", "1d",
		"-adjust", "none", "-strategy", "buy-hold", "-save"})
	var id int64
	if _, err := fmt.Sscanf(out, "final_equity=%g total_return=%g max_drawdown=%g bars=%d\nsaved result id=%d",
		new(float64), new(float64), new(float64), new(int), &id); err != nil || id == 0 {
		t.Fatalf("output %q; want saved result id=N (err %v)", out, err)
	}
	idStr := strconv.FormatInt(id, 10)

	// API side: export json is byte-identical to the detail endpoint.
	h := httpapi.BacktestsHandler(httpapi.NewDBBacktestStore(database))
	apiDetail := serveGet(t, h, "/v1/backtests/"+idStr)
	apiJSON := serveGet(t, h, "/v1/backtests/"+idStr+"/export?format=json")
	if apiJSON.Code != http.StatusOK || apiDetail.Code != http.StatusOK || apiJSON.Body.String() != apiDetail.Body.String() {
		t.Fatalf("export json (%d) != detail (%d): %q vs %q", apiJSON.Code, apiDetail.Code, apiJSON.Body, apiDetail.Body)
	}
	apiCSV := serveGet(t, h, "/v1/backtests/"+idStr+"/export")
	if apiCSV.Code != http.StatusOK || apiCSV.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("export csv = %d (%s); want 200 text/csv", apiCSV.Code, apiCSV.Body)
	}

	// CLI side: same bodies as the API (roundtrip contract).
	cliJSON := captureRunOutput(t, []string{"wbot", "backtest", "-dsn", dsn, "-export", idStr, "-format", "json"})
	if cliJSON != apiDetail.Body.String() {
		t.Fatalf("CLI json != API detail: %q vs %q", cliJSON, apiDetail.Body)
	}
	var detail backtest.DetailJSON
	if err := json.Unmarshal([]byte(cliJSON), &detail); err != nil {
		t.Fatalf("unmarshal CLI json: %v", err)
	}
	if detail.ID != id || detail.Symbol != symbol || len(detail.EquityCurve) != 3 || len(detail.Trades) != 1 {
		t.Fatalf("detail = %+v; want id %d symbol %s with 3 curve points and 1 trade", detail, id, symbol)
	}
	cliCSV := captureRunOutput(t, []string{"wbot", "backtest", "-dsn", dsn, "-export", idStr})
	if cliCSV != apiCSV.Body.String() {
		t.Fatalf("CLI csv != API csv: %q vs %q", cliCSV, apiCSV.Body)
	}
	sections := strings.Split(cliCSV, "\n\n")
	if len(sections) != 2 {
		t.Fatalf("csv sections = %d; want 2 (equity_curve + trades): %q", len(sections), cliCSV)
	}
	// Each section = name line + header + data rows; the last row's newline is
	// consumed by the blank-line separator, so 4 lines = 3 curve points and
	// 3 lines = 1 trade.
	if lines := strings.Count(sections[0], "\n"); lines != 4 {
		t.Fatalf("equity section lines = %d; want 4 (name + header + 3 rows): %q", lines, cliCSV)
	}
	if lines := strings.Count(sections[1], "\n"); lines != 3 {
		t.Fatalf("trades section lines = %d; want 3 (name + header + 1 row): %q", lines, cliCSV)
	}
	for _, want := range []string{
		"2024-03-01T00:00:00Z,10000\n2024-03-02T00:00:00Z,10300\n2024-03-03T00:00:00Z,9900",
		"2024-03-01T00:00:00Z,buy,EXPORTCLI.US,100,100,0",
	} {
		if !strings.Contains(cliCSV, want) {
			t.Fatalf("csv missing %q: %q", want, cliCSV)
		}
	}

	// Missing id through the CLI: exit 1 with a readable error.
	if code := run([]string{"wbot", "backtest", "-dsn", dsn, "-export", "99999999"}); code != 1 {
		t.Fatalf("export missing id = %d; want 1", code)
	}
}

func TestConfigYAMLOutput(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	content := "futu:\n  login_account: \"${WBOT_TESTYAML_CLI_ACCOUNT:-acc-default}\"\n  login_region: \"${WBOT_TESTYAML_CLI_REGION:-sh}\"\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureRunOutput(t, []string{"wbot", "configyaml", "-file", cfg})
	want := "FUTU_LOGIN_ACCOUNT=acc-default\nFUTU_LOGIN_REGION=sh\n"
	if out != want {
		t.Fatalf("output = %q; want %q", out, want)
	}
}

func TestParseSymbolList(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr string
	}{
		{"", nil, ""},
		{"A.US", []string{"A.US"}, ""},
		{" A.US , B.US ", []string{"A.US", "B.US"}, ""},
		{"A.US,,B.US", nil, "empty symbol entry"},
		{"A.US,A.US", nil, "duplicate symbol A.US"},
	}
	for _, tt := range tests {
		t.Run("in="+tt.in, func(t *testing.T) {
			got, err := parseSymbolList(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseSymbolList(%q) err = %v; want containing %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSymbolList(%q) err = %v; want nil", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseSymbolList(%q) = %v; want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parseSymbolList(%q) = %v; want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

// TestBacktestMultiSymbolIntegration: full CLI multi-symbol path against real
// PG — two symbols with shifted windows seeded, `-symbols` prints the combined
// summary plus per-symbol lines, and one-symbol `-symbols` stays on the
// single-symbol path (skipped without WBOT_PG_DSN).
func TestBacktestMultiSymbolIntegration(t *testing.T) {
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

	const symA, symB = "MULTIA.US", "MULTIB.US"
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol IN ($1, $2)`, symA, symB); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	seed := func(symbol string, start int, closes []float64) {
		for i, c := range closes {
			if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'none', 'futu')`, symbol, day(start+i), c); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A spans d1..d4, B starts a day later: intersection = d2,d3,d4.
	seed(symA, 0, []float64{100, 110, 121, 133.1})
	seed(symB, 1, []float64{200, 220, 242})

	argv := []string{"wbot", "backtest", "-dsn", dsn, "-symbols", symA + "," + symB,
		"-timeframe", "1d", "-adjust", "none", "-strategy", "buy-hold"}
	// A: 5000 @110 -> 5000*133.1/110 = 6050; B: 5000 @200 -> 5000*242/200 = 6050.
	out := captureRunOutput(t, argv)
	for _, want := range []string{
		"final_equity=12100", "total_return=0.21", "bars=3 symbols=2",
		symA + ": final_equity=6050", symB + ": final_equity=6050",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q; want containing %q", out, want)
		}
	}
	// Determinism: a second run prints the identical summary.
	if out2 := captureRunOutput(t, argv); out2 != out {
		t.Fatalf("runs differ: %q vs %q", out2, out)
	}
	// One-symbol -symbols is the single-symbol path (all-in over the full window).
	outSingle := captureRunOutput(t, []string{"wbot", "backtest", "-dsn", dsn, "-symbols", symA,
		"-timeframe", "1d", "-adjust", "none", "-strategy", "buy-hold"})
	if strings.Contains(outSingle, "symbols=") {
		t.Fatalf("single-symbol -symbols output %q; want single-symbol summary", outSingle)
	}
	for _, want := range []string{"final_equity=13310", "total_return=0.331", "bars=4"} {
		if !strings.Contains(outSingle, want) {
			t.Fatalf("single output %q; want containing %q", outSingle, want)
		}
	}
}

// captureRunOutput runs run() with stdout redirected and returns what it printed.
func captureRunOutput(t *testing.T, argv []string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := run(argv)
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("run(%v) = %d; want 0", argv, code)
	}
	return string(out)
}

func TestServeHelpMentionsAdminEndpoints(t *testing.T) {
	out := serveHelpOutput(t)
	for _, want := range []string{"/v1/admin/status", "/v1/admin/cluster"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("serve help missing %s: %q", want, out)
		}
	}
}

func serveHelpOutput(t *testing.T) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	code := run([]string{"wbot", "serve", "-h"})
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	return string(out)
}

// TestServeReportsActualListenAddr: with -listen 127.0.0.1:0 serve must report the
// bound port (ln.Addr), not the flag value; runs serve as a child process so a live
// /v1/admin/status can be queried (skipped without WBOT_PG_DSN).
func TestServeReportsActualListenAddr(t *testing.T) {
	if os.Getenv("WBOT_SERVE_HELPER") == "1" {
		os.Exit(run([]string{"wbot", "serve", "-listen", "127.0.0.1:0"}))
	}
	if os.Getenv("WBOT_PG_DSN") == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestServeReportsActualListenAddr$")
	cmd.Env = append(os.Environ(), "WBOT_SERVE_HELPER=1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	const prefix = "httpapi: listening on http://"
	var addr string
	var log []string
	for addr == "" {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("serve helper exited before listening (output: %s)", strings.Join(log, " | "))
			}
			log = append(log, line)
			if strings.HasPrefix(line, prefix) {
				addr = strings.TrimPrefix(line, prefix)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("serve helper did not print listen addr (output: %s)", strings.Join(log, " | "))
		}
	}
	if addr == "127.0.0.1:0" {
		t.Fatal("reported the -listen flag value; want the actual bound address")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("http://" + addr + "/v1/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["listen_addr"] != addr {
		t.Fatalf("listen_addr = %v; want %q", got["listen_addr"], addr)
	}
}

func TestServeHelpMentionsAdminStatus(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/admin/status") {
		t.Fatalf("serve help missing /v1/admin/status: %q", out)
	}
}

func TestServeHelpMentionsWatchlist(t *testing.T) {
	out := serveHelpOutput(t)
	for _, want := range []string{"/v1/strategies", "/v1/watchlist"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serve help missing %s: %q", want, out)
		}
	}
}

func TestServeHelpMentionsBacktests(t *testing.T) {
	out := serveHelpOutput(t)
	for _, want := range []string{"/v1/backtests", "/v1/backtests/{id}", "/v1/backtests/{id}/export", "POST /v1/backtests"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serve help missing %s: %q", want, out)
		}
	}
}

func TestServeMuxBacktestRoutes(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/v1/backtests")
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/backtests = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	// Unknown id through the real mux: 404 with the new error body shape.
	rec = serveGet(t, top, "/v1/backtests/999")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/v1/backtests/999 = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
	var errBody struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Action  string `json:"action"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody.Code == "" {
		t.Fatalf("/v1/backtests/999 body %q; want {code,message,action} error", rec.Body)
	}
	// Non-numeric id: 400; non-GET: 405.
	rec = serveGet(t, top, "/v1/backtests/abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("/v1/backtests/abc = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	top.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/backtests", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /v1/backtests = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeMuxWatchlistRoutes(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/v1/strategies")
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/strategies = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "covered-call") {
		t.Fatalf("/v1/strategies missing covered-call: %s", rec.Body)
	}
	rec = serveGet(t, top, "/v1/watchlist")
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/watchlist = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	req := httptest.NewRequest(http.MethodPut, "/v1/watchlist/HK.00700", strings.NewReader(`{"strategy":"covered-call"}`))
	rec = httptest.NewRecorder()
	top.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /v1/watchlist/HK.00700 = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	// GET on a symbol path is 405 (only PUT/DELETE are defined), like admin config keys.
	rec = serveGet(t, top, "/v1/watchlist/nope")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("/v1/watchlist/nope = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

// TestWatchlistCLIIntegration runs the real add → list → remove cycle against
// PostgreSQL (skipped without WBOT_PG_DSN; see .github/workflows/ci.yml db-integration).
func TestWatchlistCLIIntegration(t *testing.T) {
	if os.Getenv("WBOT_PG_DSN") == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	const symbol = "CLI.TEST.00700"

	// Clean a previous crashed run's residue.
	if got := run([]string{"wbot", "watchlist", "remove", "-symbol", symbol}); got != 0 && got != 1 {
		t.Fatalf("cleanup remove = %d; want 0 or 1", got)
	}

	addArgs := []string{"wbot", "watchlist", "add", "-symbol", symbol, "-strategy", "covered-call", "-params", `{"strike_pct_otm":0.03}`}
	if out := captureRunOutput(t, addArgs); !strings.Contains(out, symbol) {
		t.Fatalf("add output missing symbol: %q", out)
	}

	listOut := captureRunOutput(t, []string{"wbot", "watchlist", "list"})
	if !strings.Contains(listOut, symbol) || !strings.Contains(listOut, "covered-call") {
		t.Fatalf("list output missing entry: %q", listOut)
	}

	if got := run([]string{"wbot", "watchlist", "remove", "-symbol", symbol}); got != 0 {
		t.Fatalf("remove = %d; want 0", got)
	}
	if listOut := captureRunOutput(t, []string{"wbot", "watchlist", "list"}); strings.Contains(listOut, symbol) {
		t.Fatalf("list still contains %s after remove: %q", symbol, listOut)
	}
	// Second remove: not on the list anymore.
	if got := run([]string{"wbot", "watchlist", "remove", "-symbol", symbol}); got != 1 {
		t.Fatalf("second remove = %d; want 1", got)
	}
}

func TestServeHelpMentionsAdminConfig(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/admin/config") {
		t.Fatalf("serve help missing /v1/admin/config: %q", out)
	}
}

func TestAgentMasterURL(t *testing.T) {
	mem := master.NewMemory()
	srv := httptest.NewServer(httpregister.Handler(mem))
	defer srv.Close()
	if got := run([]string{"wbot", "agent", "-duration", "5ms", "-interval", "1ms", "-master-url", srv.URL}); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}

func TestMasterTLSMissingFiles(t *testing.T) {
	if got := run([]string{"wbot", "master", "-tls-cert", "/nonexistent/cert.pem", "-tls-key", "/nonexistent/key.pem", "-duration", "1ms"}); got != 1 {
		t.Fatalf("run() = %d; want 1", got)
	}
}

func TestMasterTLSShortRun(t *testing.T) {
	certPath, keyPath := writeTestCertPair(t)
	port := freeTCPPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	argv := []string{"wbot", "master", "-listen", addr, "-tls-cert", certPath, "-tls-key", keyPath, "-duration", "1ms"}
	if got := run(argv); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}

func writeTestCertPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"wbot-test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// serveFakeStore is a no-data httpapi.Store for serve-mux tests (no DB needed).
type serveFakeStore struct{}

func (serveFakeStore) QueryBars(context.Context, string, string, string, time.Time, time.Time, int, bool) ([]ingest.Bar, error) {
	return nil, nil
}

func (serveFakeStore) RecentRuns(context.Context, int) ([]ingest.RunStatus, error) {
	return nil, nil
}

func (serveFakeStore) RunStatusCounts(context.Context) (ingest.RunCounts, error) {
	return ingest.RunCounts{}, nil
}

func (serveFakeStore) BarCoverage(context.Context) ([]ingest.BarCoverage, error) {
	return nil, nil
}

func (serveFakeStore) OptionFreshness(context.Context) ([]ingest.OptionFreshness, error) {
	return nil, nil
}

func (serveFakeStore) AccountSnapshots(context.Context, string, int) ([]ingest.AccountSnapshotRow, error) {
	return nil, nil
}

func (serveFakeStore) LatestOptionQuote(context.Context, string) (*ingest.OptionQuoteRow, error) {
	return nil, nil
}

func (serveFakeStore) Ping(context.Context) error {
	return nil
}

// serveFakeWatchlistStore is a no-data httpapi.WatchlistStore for serve-mux tests.
type serveFakeWatchlistStore struct{}

func (serveFakeWatchlistStore) List(context.Context) ([]watchlist.Item, error) { return nil, nil }
func (serveFakeWatchlistStore) Upsert(context.Context, string, string, map[string]any) (watchlist.Item, error) {
	return watchlist.Item{}, nil
}
func (serveFakeWatchlistStore) Delete(context.Context, string) (bool, error) { return false, nil }

// serveFakeBacktestStore is a no-data httpapi.BacktestStore for serve-mux tests.
type serveFakeBacktestStore struct{}

func (serveFakeBacktestStore) List(context.Context, string, string, string, int, int, string, bool) ([]backtest.ResultRecord, error) {
	return nil, nil
}
func (serveFakeBacktestStore) Get(context.Context, int64) (*backtest.ResultRecord, error) {
	return nil, backtest.ErrResultNotFound
}

// serveFakeBacktestExecutor is a canned httpapi.BacktestExecutor for serve-mux tests.
type serveFakeBacktestExecutor struct{}

func (serveFakeBacktestExecutor) RunOne(context.Context, string, string, map[string]any) (*backtest.ResultRecord, error) {
	ts := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return &backtest.ResultRecord{
		ID: 1, Strategy: "buy-hold", Symbol: "DEMO.US",
		Params:  map[string]any{"cash": 10000.0, "fee": 0.0},
		Metrics: map[string]any{"equity": 12100.0, "total_return": 0.21, "max_drawdown": 0.0, "bars": 3},
		StartTs: ts, EndTs: ts.Add(48 * time.Hour), CreatedAt: ts,
		EquityCurve: []backtest.EquityPoint{}, Trades: []backtest.Trade{},
	}, nil
}

// serveFakeFutuQuoter is a scriptable FutuQuoter for serveMux tests.
type serveFakeFutuQuoter struct {
	s2c json.RawMessage
	err error
}

func (f serveFakeFutuQuoter) Quote(_ context.Context, _ string) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.s2c, nil
}

// serveFakeFutuOrderer is a scriptable FutuOrderer for serveMux tests.
type serveFakeFutuOrderer struct{}

// serveFakeIngestRunner is a no-op IngestRunner for serveMux tests.
type serveFakeIngestRunner struct{}

func (serveFakeIngestRunner) RunBars(_ context.Context, _, _, _ string, _, _ time.Time) error {
	return nil
}

func (serveFakeIngestRunner) RunOptions(_ context.Context, _, _ string) error { return nil }

func (serveFakeFutuOrderer) Orders(_ context.Context, _ futu.Env, _ uint64, _ bool) (httpapi.OrdersSnapshot, error) {
	return httpapi.OrdersSnapshot{Env: "simulate", AccID: 1907141, Orders: []httpapi.OrderJSON{}}, nil
}

// serveFakeFutuAccounter is a scriptable FutuAccounter for serveMux tests.
type serveFakeFutuAccounter struct {
	err error
}

func (f serveFakeFutuAccounter) Account(_ context.Context, _ futu.Env, _ uint64) (httpapi.AccountSnapshot, error) {
	if f.err != nil {
		return httpapi.AccountSnapshot{}, f.err
	}
	return httpapi.AccountSnapshot{
		Env:   "simulate",
		AccID: 1907141,
		Funds: httpapi.FundsJSON{Power: 1198286.822, TotalAssets: 1198286.822, Cash: 318666.822, MarketVal: 879620, AvailableCash: 318666.822},
		Positions: []httpapi.PositionJSON{
			{Symbol: "HK.00700", Qty: 100, AvgCost: 470.0, Price: 475.2, MarketVal: 47520, PL: 520},
		},
	}, nil
}

// serveFakeFutuChainer is a scriptable FutuOptionChainer for serveMux tests.
type serveFakeFutuChainer struct {
	err error
}

func (f serveFakeFutuChainer) Expirations(_ context.Context, _ string) ([]futu.OptionExpiry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []futu.OptionExpiry{{Date: "2026-08-07", Timestamp: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), DistanceDays: 5, Cycle: 1}}, nil
}

func (f serveFakeFutuChainer) Chain(_ context.Context, _ string, begin, end time.Time) ([]futu.OptionContract, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []futu.OptionContract{{Symbol: "HK.TCH260807C335000", Underlying: "HK.00700", OptionType: "call", Strike: 335, Expiry: begin, LotSize: 100}}, nil
}

func serveMuxForTest() http.Handler {
	meta := httpapi.ProcessMeta{Version: "v-test", StartedAt: time.Now(), ListenAddr: "127.0.0.1:8080"}
	return serveMux(meta, httpapi.PingerFunc(func(context.Context) error { return nil }), serveFakeStore{}, serveFakeWatchlistStore{}, serveFakeBacktestStore{}, serveFakeBacktestExecutor{}, serveFakeFutuQuoter{s2c: json.RawMessage(`{"basic_qot_list":[{"cur_price":475.2}]}`)}, serveFakeFutuAccounter{}, serveFakeFutuOrderer{}, serveFakeFutuChainer{}, serveFakeIngestRunner{})
}

// TestServeMuxBacktestExecuteRoute: POST /v1/backtests routes to the execute
// handler (201 with the created detail) while GET stays on the read handler.
func TestServeMuxBacktestExecuteRoute(t *testing.T) {
	top := serveMuxForTest()
	req := httptest.NewRequest(http.MethodPost, "/v1/backtests",
		strings.NewReader(`{"symbol":"DEMO.US","strategy":"buy-hold"}`))
	rec := httptest.NewRecorder()
	top.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/backtests = %d; want 201 (body %s)", rec.Code, rec.Body)
	}
	var detail struct {
		ID      int64          `json:"id"`
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("body %q not JSON: %v", rec.Body, err)
	}
	if detail.ID != 1 || detail.Metrics["equity"] != 12100.0 {
		t.Fatalf("detail = %+v; want id 1 equity 12100", detail)
	}
	// GET on the same path still serves the read handler's list.
	rec = serveGet(t, top, "/v1/backtests")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/backtests = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeMuxFutuQuoteRoute(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/v1/futu/quote?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("quote = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "basic_qot_list") || !strings.Contains(rec.Body.String(), "475.2") {
		t.Fatalf("quote body missing passthrough fields: %s", rec.Body)
	}
	// Missing symbol through the real mux: 400 with the {code,message,action} body.
	rec = serveGet(t, top, "/v1/futu/quote")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing symbol = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
	// Non-GET: 405.
	rec = httptest.NewRecorder()
	top.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/futu/quote?symbol=HK.00700", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v1/futu/quote = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeHelpMentionsFutuQuote(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/futu/quote") {
		t.Fatalf("serve help missing /v1/futu/quote: %q", out)
	}
}

func TestServeMuxFutuAccountRoute(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/v1/futu/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("account = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "total_assets") || !strings.Contains(rec.Body.String(), "HK.00700") {
		t.Fatalf("account body missing whitelisted fields: %s", rec.Body)
	}
	// Bad env through the real mux: 400 with the {code,message,action} body.
	rec = serveGet(t, top, "/v1/futu/account?env=production")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad env = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
	// Non-GET: 405.
	rec = httptest.NewRecorder()
	top.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/futu/account", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v1/futu/account = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeHelpMentionsFutuAccount(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/futu/account") {
		t.Fatalf("serve help missing /v1/futu/account: %q", out)
	}
}

func TestServeMuxFutuOptionsRoute(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/v1/futu/options?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("options = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "expirations") || !strings.Contains(rec.Body.String(), "TCH260807C335000") {
		t.Fatalf("options body missing expirations/contracts: %s", rec.Body)
	}
	// Missing symbol through the real mux: 400 with the {code,message,action} body.
	rec = serveGet(t, top, "/v1/futu/options")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing symbol = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
	// Non-GET: 405.
	rec = httptest.NewRecorder()
	top.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/futu/options?symbol=HK.00700", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v1/futu/options = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeHelpMentionsFutuOptions(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/futu/options") {
		t.Fatalf("serve help missing /v1/futu/options: %q", out)
	}
}

func serveGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServeMuxRootRedirectsToUI(t *testing.T) {
	rec := serveGet(t, serveMuxForTest(), "/")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d; want 301 (body %s)", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/ui/" {
		t.Fatalf("Location = %q; want /ui/", loc)
	}
}

func TestServeMuxRootMethodNotRedirected(t *testing.T) {
	rec := httptest.NewRecorder()
	serveMuxForTest().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeMuxUIServesPages(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/ui/")
	if rec.Code != http.StatusOK {
		t.Fatalf("/ui/: status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("/ui/: content-type = %q; want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · Dashboard</title>") {
		t.Fatalf("/ui/ missing title: %s", rec.Body)
	}
	rec = serveGet(t, top, "/ui/admin.html")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>wbot · Admin</title>") {
		t.Fatalf("/ui/admin.html: status = %d body = %s", rec.Code, rec.Body)
	}
	rec = serveGet(t, top, "/ui/data.html")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>wbot · 数据</title>") {
		t.Fatalf("/ui/data.html: status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestServeMuxUIServesAssets(t *testing.T) {
	top := serveMuxForTest()
	tests := []struct {
		path string
		ct   string
	}{
		{"/ui/style.css", "text/css"},
		{"/ui/app.js", "text/javascript"},
	}
	for _, tt := range tests {
		rec := serveGet(t, top, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; want 200 (body %s)", tt.path, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.ct) {
			t.Fatalf("%s: content-type = %q; want %s prefix", tt.path, ct, tt.ct)
		}
	}
	rec := serveGet(t, top, "/ui/nope.txt")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/ui/nope.txt: status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeMuxAPIRegression(t *testing.T) {
	top := serveMuxForTest()
	tests := []struct {
		path string
		want int
	}{
		{"/v1/bars", http.StatusBadRequest}, // missing symbol/timeframe
		{"/v1/runs", http.StatusOK},
		{"/v1/health", http.StatusOK},
		{"/v1/backtests", http.StatusOK},
		{"/v1/nope", http.StatusNotFound},
	}
	for _, tt := range tests {
		rec := serveGet(t, top, tt.path)
		if rec.Code != tt.want {
			t.Fatalf("%s: status = %d; want %d (body %s)", tt.path, rec.Code, tt.want, rec.Body)
		}
	}
	rec := serveGet(t, top, "/v1/nope")
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
		t.Fatalf("/v1/nope body %q; want JSON error", rec.Body)
	}
}

func TestServeHelpMentionsWebUI(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/ui/") {
		t.Fatalf("serve help missing /ui/: %q", out)
	}
}

// TestBacktestSaveReadAPIIntegration: `wbot backtest -save` persists the run
// (metrics + equity_curve/trades), and the read API returns the same numbers
// the CLI printed (skipped without WBOT_PG_DSN).
func TestBacktestSaveReadAPIIntegration(t *testing.T) {
	if os.Getenv("WBOT_PG_DSN") == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(os.Getenv("WBOT_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}

	const symbol = "BTAPI.US"
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	for i, c := range []float64{100, 110, 121} {
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'none', 'futu')`, symbol, day(i), c); err != nil {
			t.Fatal(err)
		}
	}

	out := captureRunOutput(t, []string{"wbot", "backtest", "-dsn", os.Getenv("WBOT_PG_DSN"),
		"-symbol", symbol, "-timeframe", "1d", "-adjust", "none", "-strategy", "buy-hold", "-save"})
	if !strings.Contains(out, "final_equity=12100") || !strings.Contains(out, "saved result id=") {
		t.Fatalf("backtest output %q; want final_equity=12100 and saved id", out)
	}

	// Serve wiring with real DB stores (same as runServe).
	top := serveMux(httpapi.ProcessMeta{Version: "v-test", StartedAt: time.Now(), ListenAddr: "127.0.0.1:0"},
		httpapi.PingerFunc(database.PingContext), httpapi.NewDBStore(database),
		httpapi.NewDBWatchlistStore(database), httpapi.NewDBBacktestStore(database), httpapi.NewDBBacktestExecutor(database), httpapi.NewFutuQuoter(), httpapi.NewFutuAccounter(), httpapi.NewFutuOrderer(), httpapi.NewFutuOptionChainer(), httpapi.NewIngestRunner(database))

	rec := serveGet(t, top, "/v1/backtests?symbol="+symbol)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var list []struct {
		ID      int64          `json:"id"`
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Metrics["equity"] != 12100.0 {
		t.Fatalf("list = %+v; want one run with equity 12100", list)
	}

	rec = serveGet(t, top, fmt.Sprintf("/v1/backtests/%d", list[0].ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var detail struct {
		EquityCurve []struct {
			Ts     string  `json:"ts"`
			Equity float64 `json:"equity"`
		} `json:"equity_curve"`
		Trades []struct {
			Ts        string  `json:"ts"`
			Action    string  `json:"action"`
			Symbol    string  `json:"symbol"`
			Size      float64 `json:"size"`
			Price     float64 `json:"price"`
			CashAfter float64 `json:"cash_after"`
		} `json:"trades"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	// Equity curve: 10000 → 11000 → 12100 (all-in at 100, marked to close).
	wantCurve := []float64{10000, 11000, 12100}
	if len(detail.EquityCurve) != len(wantCurve) {
		t.Fatalf("equity_curve = %+v; want %v", detail.EquityCurve, wantCurve)
	}
	for i, eq := range wantCurve {
		if detail.EquityCurve[i].Equity != eq {
			t.Fatalf("equity_curve[%d] = %v; want %v", i, detail.EquityCurve[i].Equity, eq)
		}
	}
	if len(detail.Trades) != 1 {
		t.Fatalf("trades = %+v; want exactly the opening buy", detail.Trades)
	}
	tr := detail.Trades[0]
	if tr.Action != "buy" || tr.Symbol != symbol || tr.Size != 100 || tr.Price != 100 || tr.CashAfter != 0 {
		t.Fatalf("trade = %+v; want buy %s 100 @100 cash_after 0", tr, symbol)
	}
}

// TestBacktestExecuteMatchesCLISave: POST /v1/backtests and `wbot backtest
// -save` share the runner path — same input yields the same metrics and the
// same persisted params (acceptance: 同输入同输出, draft 2026-08-02 S4).
func TestBacktestExecuteMatchesCLISave(t *testing.T) {
	if os.Getenv("WBOT_PG_DSN") == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(os.Getenv("WBOT_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}

	const symbol = "BTEXECCLI.US"
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	for i, c := range []float64{100, 110, 121} {
		// adjust 'fwd' matches both the CLI default and the API's default.
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'fwd', 'futu')`, symbol, day(i), c); err != nil {
			t.Fatal(err)
		}
	}

	// CLI -save: baseline output (deterministic; doc/BACKTEST.md).
	cliOut := captureRunOutput(t, []string{"wbot", "backtest", "-dsn", os.Getenv("WBOT_PG_DSN"),
		"-symbol", symbol, "-timeframe", "1d", "-strategy", "buy-hold", "-save"})
	if !strings.Contains(cliOut, "final_equity=12100") || !strings.Contains(cliOut, "saved result id=") {
		t.Fatalf("CLI output %q; want final_equity=12100 and saved id", cliOut)
	}

	// API POST: same fixture, same strategy → identical metrics/params.
	top := serveMux(httpapi.ProcessMeta{Version: "v-test", StartedAt: time.Now(), ListenAddr: "127.0.0.1:0"},
		httpapi.PingerFunc(database.PingContext), httpapi.NewDBStore(database),
		httpapi.NewDBWatchlistStore(database), httpapi.NewDBBacktestStore(database), httpapi.NewDBBacktestExecutor(database), httpapi.NewFutuQuoter(), httpapi.NewFutuAccounter(), httpapi.NewFutuOrderer(), httpapi.NewFutuOptionChainer(), httpapi.NewIngestRunner(database))
	req := httptest.NewRequest(http.MethodPost, "/v1/backtests",
		strings.NewReader(`{"symbol":"`+symbol+`","strategy":"buy-hold"}`))
	rec := httptest.NewRecorder()
	top.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d; want 201 (body %s)", rec.Code, rec.Body)
	}
	var posted struct {
		ID      int64          `json:"id"`
		Params  map[string]any `json:"params"`
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Metrics["equity"] != 12100.0 || posted.Metrics["bars"] != 3.0 {
		t.Fatalf("POST metrics = %v; want the CLI's equity 12100 over 3 bars", posted.Metrics)
	}

	// Both runs are listed; their persisted params match field for field.
	list := serveGet(t, top, "/v1/backtests?symbol="+symbol)
	var rows []struct {
		ID      int64          `json:"id"`
		Params  map[string]any `json:"params"`
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("list = %d rows; want 2 (CLI + API)", len(rows))
	}
	for _, row := range rows {
		if row.Metrics["equity"] != 12100.0 {
			t.Fatalf("row %d metrics = %v; want equity 12100", row.ID, row.Metrics)
		}
		for _, k := range []string{"cash", "fee", "timeframe", "adjust"} {
			if row.Params[k] == nil {
				t.Fatalf("row %d params = %v; missing %q", row.ID, row.Params, k)
			}
		}
		if row.Params["adjust"] != "fwd" || row.Params["timeframe"] != "1d" {
			t.Fatalf("row %d params = %v; want timeframe 1d adjust fwd", row.ID, row.Params)
		}
	}

	// The GET detail of the POSTed run carries the same trace as the CLI save.
	detail := serveGet(t, top, fmt.Sprintf("/v1/backtests/%d", posted.ID))
	if detail.Code != http.StatusOK {
		t.Fatalf("detail = %d; want 200 (body %s)", detail.Code, detail.Body)
	}
	var got struct {
		EquityCurve []struct {
			Equity float64 `json:"equity"`
		} `json:"equity_curve"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.EquityCurve) != 3 || got.EquityCurve[2].Equity != 12100 {
		t.Fatalf("equity_curve = %+v; want 3 points ending 12100", got.EquityCurve)
	}
}

// runOutput runs the CLI and returns its exit code plus stdout (no assertion).
func runOutput(argv []string) (int, string) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	code := run(argv)
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return code, string(out)
}

// TestIngestFreshnessIntegration: `wbot ingest freshness` gates on staleness —
// exit 0 when fresh (per-timeframe default or -max-age override), exit 1 when
// any symbol×timeframe is stale (skipped without WBOT_PG_DSN).
func TestIngestFreshnessIntegration(t *testing.T) {
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

	const freshSym = "FRESHCLI.US"
	const staleSym = "STALECLI.US"
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
	now := time.Now().UTC().Truncate(time.Second)
	insert(freshSym, now.Add(-2*time.Hour))   // 1d default threshold 72h → fresh
	insert(staleSym, now.Add(-100*time.Hour)) // 1d default threshold 72h → stale

	statusLine := func(code int, out string, sym string) string {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.HasPrefix(line, sym+" ") {
				return line
			}
		}
		t.Fatalf("exit %d: output %q missing symbol %s", code, out, sym)
		return ""
	}

	// Mixed data: any stale → exit 1, both statuses printed.
	code, out := runOutput([]string{"wbot", "ingest", "freshness", "-dsn", dsn})
	if code != 1 {
		t.Fatalf("mixed: exit = %d; want 1 (output %q)", code, out)
	}
	if l := statusLine(code, out, freshSym); !strings.HasSuffix(l, " fresh") {
		t.Fatalf("fresh symbol line %q; want suffix ' fresh'", l)
	}
	if l := statusLine(code, out, staleSym); !strings.HasSuffix(l, " stale") {
		t.Fatalf("stale symbol line %q; want suffix ' stale'", l)
	}

	// Global -max-age override covers everything → exit 0.
	code, out = runOutput([]string{"wbot", "ingest", "freshness", "-dsn", dsn, "-max-age", "1000000h"})
	if code != 0 {
		t.Fatalf("-max-age 1000000h: exit = %d; want 0 (output %q)", code, out)
	}
	if l := statusLine(code, out, staleSym); !strings.HasSuffix(l, " fresh") {
		t.Fatalf("-max-age 1000000h: stale symbol line %q; want suffix ' fresh'", l)
	}

	// -max-age 1h flips the 2h-old entry to stale → exit 1.
	code, out = runOutput([]string{"wbot", "ingest", "freshness", "-dsn", dsn, "-max-age", "1h"})
	if code != 1 {
		t.Fatalf("-max-age 1h: exit = %d; want 1 (output %q)", code, out)
	}
	if l := statusLine(code, out, freshSym); !strings.HasSuffix(l, " stale") {
		t.Fatalf("-max-age 1h: fresh symbol line %q; want suffix ' stale'", l)
	}

	// Option quotes join the same judgment: fresh option row (2h old, 4h
	// threshold) under the global 1000000h override; stale option row (100h
	// old) under the default → exit 1.
	const optFreshU = "ZZOPTCLIFRESH.US"
	const optStaleU = "ZZOPTCLISTALE.US"
	for _, u := range []string{optFreshU, optStaleU} {
		if _, err := database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, u); err != nil {
			t.Fatal(err)
		}
	}
	optInsert := func(u string, ts time.Time) {
		if _, err := database.Exec(`
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ($1, $1, 'call', 100, '2026-12-31', $2, 10, 11, 9, 10.5, 100, NULL, 'none', 'futu')`, u, ts); err != nil {
			t.Fatal(err)
		}
	}
	optNow := time.Now().UTC().Truncate(time.Second)
	optInsert(optFreshU, optNow.Add(-2*time.Hour))
	optInsert(optStaleU, optNow.Add(-100*time.Hour))

	code, out = runOutput([]string{"wbot", "ingest", "freshness", "-dsn", dsn})
	if code != 1 {
		t.Fatalf("option stale: exit = %d; want 1 (output %q)", code, out)
	}
	if l := statusLine(code, out, optStaleU); !strings.HasSuffix(l, " stale") {
		t.Fatalf("stale option line %q; want suffix ' stale'", l)
	}
	code, out = runOutput([]string{"wbot", "ingest", "freshness", "-dsn", dsn, "-max-age", "1000000h"})
	if code != 0 {
		t.Fatalf("option -max-age 1000000h: exit = %d; want 0 (output %q)", code, out)
	}
	if l := statusLine(code, out, optFreshU); !strings.HasSuffix(l, " fresh") {
		t.Fatalf("fresh option line %q; want suffix ' fresh'", l)
	}
	if l := statusLine(code, out, optStaleU); !strings.HasSuffix(l, " fresh") {
		t.Fatalf("-max-age 1000000h option line %q; want suffix ' fresh'", l)
	}
}

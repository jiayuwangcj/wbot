package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/futu"
)

// fastFutuLimits shrinks the global futu rate pools for fast CLI tests.
func fastFutuLimits(t *testing.T) {
	t.Helper()
	oldQ, oldK, oldH, oldS, oldG := futu.QuoteLimit, futu.KlineLimit, futu.HistoryPageLimit, futu.SnapshotLimit, futu.BatchGap
	futu.QuoteLimit = futu.NewLimiter(time.Microsecond)
	futu.KlineLimit = futu.NewLimiter(time.Microsecond)
	futu.HistoryPageLimit = futu.NewLimiter(time.Microsecond)
	futu.SnapshotLimit = futu.NewLimiter(time.Microsecond)
	futu.BatchGap = time.Microsecond
	t.Cleanup(func() {
		futu.QuoteLimit, futu.KlineLimit, futu.HistoryPageLimit, futu.SnapshotLimit, futu.BatchGap = oldQ, oldK, oldH, oldS, oldG
	})
}

const ingestFutuPage1 = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
	{"time":"2026-07-30 02:00:00","is_blank":false,"high_price":475.0,"open_price":466.4,"low_price":462.8,"close_price":471.8,"volume":31791979,"timestamp":1785348000.0},
	{"time":"2026-07-30 02:30:00","is_blank":false,"high_price":479.8,"open_price":470.0,"low_price":462.0,"close_price":475.2,"volume":31100240,"timestamp":1785349800.0}
],"next_req_key":["0"]}}`

const ingestFutuPage2 = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
	{"time":"2026-07-30 03:00:00","is_blank":false,"high_price":480.0,"open_price":475.0,"low_price":474.0,"close_price":479.0,"volume":12000000,"timestamp":1785351600.0}
],"next_req_key":null}}`

// ingestFutuDirtyPage has one dirty 1m-derived bar (high 186.9 < open 188.0,
// like the 2016-07-22 gateway data bug) between two valid bars.
const ingestFutuDirtyPage = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"kl_list":[
	{"time":"2026-07-30 02:00:00","is_blank":false,"high_price":475.0,"open_price":466.4,"low_price":462.8,"close_price":471.8,"volume":31791979,"timestamp":1785348000.0},
	{"time":"2026-07-30 02:30:00","is_blank":false,"high_price":186.9,"open_price":188.0,"low_price":184.0,"close_price":187.0,"volume":1000,"timestamp":1785349800.0},
	{"time":"2026-07-30 03:00:00","is_blank":false,"high_price":480.0,"open_price":475.0,"low_price":474.0,"close_price":479.0,"volume":12000000,"timestamp":1785351600.0}
],"next_req_key":null}}`

func TestIngestFutuDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"help", []string{"wbot", "ingest", "futu", "-h"}, 0},
		{"bad symbol", []string{"wbot", "ingest", "futu", "-symbol", "00700"}, 2},
		{"bad timeframe", []string{"wbot", "ingest", "futu", "-timeframe", "K_3M"}, 2},
		{"bad adjust", []string{"wbot", "ingest", "futu", "-adjust", "bogus"}, 2},
		{"bad from", []string{"wbot", "ingest", "futu", "-from", "nope"}, 2},
		{"bad range", []string{"wbot", "ingest", "futu", "-from", "2026-08-14T00:00:00Z", "-to", "2026-08-13T00:00:00Z"}, 2},
		{"empty source", []string{"wbot", "ingest", "futu", "-source", ""}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestIngestFutuDryRunPaginated(t *testing.T) {
	fastFutuLimits(t)
	var first, second string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history-kline" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "next_req_key") {
			second = string(b)
			io.WriteString(w, ingestFutuPage2)
			return
		}
		first = string(b)
		io.WriteString(w, ingestFutuPage1)
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "30m", "-adjust", "none", "-dry-run", "-endpoint", srv.URL})
	if code != 0 {
		t.Fatalf("run() = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"ingest futu: dry-run: 3 bars", "2026-07-29T18:00:00Z", "2026-07-29T19:00:00Z", "symbol=HK.00700", "timeframe=30m", "source=futu", "adjust=none"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dry-run output missing %q: %s", want, stdout)
		}
	}
	for _, want := range []string{`"kl_type":8`, `"rehab_type":0`, `"max_count":1000`, `"begin_time"`, `"security"`} {
		if !strings.Contains(first, want) {
			t.Fatalf("first request body missing %q: %s", want, first)
		}
	}
	if !strings.Contains(second, `"next_req_key":["0"]`) {
		t.Fatalf("second request body missing paging cursor: %s", second)
	}
}

func TestIngestFutuDryRunGatewayDown(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "K_DAY", "-dry-run", "-endpoint", addr})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "ingest futu:") {
		t.Fatalf("stderr = %q; want ingest futu: prefix", stderr)
	}
}

func TestIngestFutuSkipInvalid(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history-kline" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, ingestFutuDirtyPage)
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "30m", "-adjust", "none", "-dry-run", "-skip-invalid", "-endpoint", srv.URL})
	if code != 0 {
		t.Fatalf("run(-skip-invalid) = %d; want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "ingest futu: skipped 1 invalid bar(s), first at 2026-07-29T18:30:00Z") {
		t.Fatalf("stderr missing skip report: %s", stderr)
	}
	if !strings.Contains(stdout, "ingest futu: dry-run: 2 bars") {
		t.Fatalf("stdout = %q; want dry-run with 2 bars", stdout)
	}

	_, stderr, code = captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "30m", "-adjust", "none", "-dry-run", "-endpoint", srv.URL})
	if code != 1 {
		t.Fatalf("run(no flag) = %d; want 1", code)
	}
	if !strings.Contains(stderr, "186.9") || !strings.Contains(stderr, "< open") {
		t.Fatalf("stderr = %q; want high < open error naming the dirty bar", stderr)
	}
}

func TestIngestFutuSkipInvalidWritesToDB(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history-kline" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, ingestFutuDirtyPage)
	}))
	defer srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu",
		"-symbol", "HK.00700", "-timeframe", "30m", "-adjust", "none", "-skip-invalid", "-dsn", dsn, "-endpoint", srv.URL})
	if code != 0 {
		t.Fatalf("run(-skip-invalid) = %d; want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "ingest futu: skipped 1 invalid bar(s), first at 2026-07-29T18:30:00Z") {
		t.Fatalf("stderr missing skip report: %s", stderr)
	}
	if !strings.Contains(stderr, "bars=2") {
		t.Fatalf("stderr missing ok line: %s", stderr)
	}

	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const from, to = "2026-07-29T18:00:00Z", "2026-07-29T19:00:00Z"
	var n int
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = 'HK.00700' AND timeframe = '30m' AND source = 'futu'
AND ts >= $1 AND ts <= $2`, from, to).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("bars written: got %d want 2 (dirty bar must be skipped)", n)
	}
	var dirty int
	err = database.QueryRow(`
SELECT COUNT(*) FROM bars WHERE symbol = 'HK.00700' AND timeframe = '30m' AND source = 'futu'
AND ts = '2026-07-29T18:30:00Z'`).Scan(&dirty)
	if err != nil {
		t.Fatal(err)
	}
	if dirty != 0 {
		t.Fatalf("dirty bar persisted: got %d want 0", dirty)
	}
	if _, err := database.Exec(`
DELETE FROM bars WHERE symbol = 'HK.00700' AND timeframe = '30m' AND source = 'futu'
AND ts >= $1 AND ts <= $2`, from, to); err != nil {
		t.Fatal(err)
	}
}

func TestIngestFutuUsageDocumentsContract(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	for _, want := range []string{"HK.00700", "30m", "K_1M", "K_DAY", "K_MONTH", "next_req_key", "1000", "2015-04-16", "-endpoint", "-dry-run", "-skip-invalid", "none", "rehab_type"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("ingest futu usage missing %q: %s", want, stderr)
		}
	}
}

func TestIngestUsageListsFutu(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	if !strings.Contains(stderr, "futu") {
		t.Fatalf("ingest usage missing futu: %s", stderr)
	}
}

func TestIngestFutuOptionDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"help", []string{"wbot", "ingest", "futu-option", "-h"}, 0},
		{"no symbol", []string{"wbot", "ingest", "futu-option"}, 2},
		{"bad symbol", []string{"wbot", "ingest", "futu-option", "-symbol", "00700"}, 2},
		{"bad adjust", []string{"wbot", "ingest", "futu-option", "-symbol", "HK.00700", "-adjust", "bogus"}, 2},
		{"bad days", []string{"wbot", "ingest", "futu-option", "-symbol", "HK.00700", "-days", "0"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestIngestFutuOptionUsageMentionsAdjust(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "futu-option", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	for _, want := range []string{"-days", "-expiries", "-adjust", "fwd", "Cache-first"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("ingest futu-option usage missing %q: %s", want, stderr)
		}
	}
}

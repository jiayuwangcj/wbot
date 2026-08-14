package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

func hkexZip(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hkexFixture(t *testing.T, class string, date time.Time) ([]byte, []byte) {
	t.Helper()
	business := date.Format("20060102")
	dtop := strings.Join([]string{
		fmt.Sprintf(`"H","DTOP","DCASS","%s","%s195046","SEOCH",1`, business, business),
		fmt.Sprintf(`"01","SOM","STOCK OPTIONS","%s","30","JUL","25",500.00,10228,9406,1293,2649,76,12.40,-1.90,17131,15287,2057,3067,153,9.82,-0.27`, class),
		fmt.Sprintf(`"01","WSO","WEEKLY STOCK OPTIONS","%s","11","JUL","25",510.00,500,400,0,100,5,7.01,-1.48,800,700,0,0,0,5.30,0.41`, class),
		`"T",4,"EOF"`,
	}, "\r\n") + "\r\n"
	rp := strings.Join([]string{
		fmt.Sprintf(`"H","RP006-FINAL","DCASS","%s","%s195046","SEOCH",01`, business, business),
		fmt.Sprintf(`"01","%sSP","SOM","STOCK OPTIONS","%s","TEST UNDERLYING","HKD",501.50,503.00,-1.50,`, class, class),
		fmt.Sprintf(`"01","%s500.00G5","SOM","STOCK OPTIONS","%s","TEST UNDERLYING","HKD",12.40,14.30,-1.90,20.5604`, class, class),
		fmt.Sprintf(`"01","%s500.00S5","SOM","STOCK OPTIONS","%s","TEST UNDERLYING","HKD",9.82,10.09,-0.27,19.4420`, class, class),
		fmt.Sprintf(`"01","%s510.00G5W11","WSO","WEEKLY STOCK OPTIONS","%s","TEST UNDERLYING","HKD",7.01,8.49,-1.48,21.4575`, class, class),
		fmt.Sprintf(`"01","%s510.00S5W11","WSO","WEEKLY STOCK OPTIONS","%s","TEST UNDERLYING","HKD",5.30,4.89,0.41,19.3630`, class, class),
		`"T",5,"EOF"`,
	}, "\r\n") + "\r\n"
	return hkexZip(t, business+"_1_dtop_o_seoch_opt_dtl_all.raw", dtop),
		hkexZip(t, business+"_1_rp006-final_o.raw", rp)
}

func TestParseHKEXDayMapsOfficialSettlementAndProjection(t *testing.T) {
	date := time.Date(2025, 7, 2, 0, 0, 0, 0, time.UTC)
	dtop, rp := hkexFixture(t, "TCH", date)
	day, err := parseHKEXDay(date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100}, dtop, rp)
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Quotes) != 4 {
		t.Fatalf("option quotes = %d; want 4", len(day.Quotes))
	}
	// The zero-turnover weekly put remains an official option_quotes mark but
	// is deliberately excluded from the actionable research projection.
	if len(day.Snapshots) != 3 {
		t.Fatalf("snapshots = %d; want 3 complete rows", len(day.Snapshots))
	}
	wantObserved := time.Date(2025, 7, 2, 16, 0, 0, 0, hkexLocation).UTC()
	if !day.ObservedAt.Equal(wantObserved) || !day.Quotes[0].Ts.Equal(wantObserved) {
		t.Fatalf("observed_at = %v / %v; want %v", day.ObservedAt, day.Quotes[0].Ts, wantObserved)
	}
	call := day.Quotes[0]
	if call.Symbol != "HK.TCH250730C500000" || call.OptionType != "call" || call.Close != 12.40 || call.Open != call.Close || call.Volume != 2649 || call.ImpliedVol == nil || *call.ImpliedVol != 0.205604 {
		t.Fatalf("monthly call = %+v; want official settlement/volume/IV mapping", call)
	}
	if day.Quotes[2].Symbol != "HK.TCH250711C510000" {
		t.Fatalf("weekly symbol = %s; want exact DTOP expiry day", day.Quotes[2].Symbol)
	}
	for _, snap := range day.Snapshots {
		if snap.Source != "hkex" || snap.SnapshotKey != "hkex-eod-20250702-bs-r0" || snap.Bid == nil || snap.Ask == nil || *snap.Bid != *snap.Ask || snap.IV == nil || snap.Delta == nil || snap.Theta == nil || snap.UnderlyingPrice == nil || *snap.UnderlyingPrice != 501.5 || snap.LotSize == nil || *snap.LotSize != 100 {
			t.Fatalf("incomplete EOD projection: %+v", snap)
		}
		if snap.OptionType == "CALL" && *snap.Delta <= 0 {
			t.Fatalf("call delta = %v; want positive", *snap.Delta)
		}
		if snap.OptionType == "PUT" && *snap.Delta >= 0 {
			t.Fatalf("put delta = %v; want negative", *snap.Delta)
		}
		if *snap.Theta >= 0 {
			t.Fatalf("theta = %v; want negative daily theta", *snap.Theta)
		}
	}
	if err := ValidateBars([]Bar{{Ts: call.Ts, Open: call.Open, High: call.High, Low: call.Low, Close: call.Close, Volume: call.Volume}}); err != nil {
		t.Fatalf("settlement OHLC must remain a valid bar: %v", err)
	}
}

func TestHKEXSourceFetchDayURLs404AndRetry(t *testing.T) {
	oldBackoff := hkexRetryBackoff
	hkexRetryBackoff = []time.Duration{time.Microsecond, time.Microsecond}
	t.Cleanup(func() { hkexRetryBackoff = oldBackoff })
	date := time.Date(2025, 7, 2, 0, 0, 0, 0, time.UTC)
	dtop, rp := hkexFixture(t, "TCH", date)
	var calls atomic.Int32
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if !strings.Contains(r.Header.Get("User-Agent"), "wbot-hkex-datafill") || r.Header.Get("Accept") != "*/*" {
			http.Error(w, "missing agent", http.StatusBadRequest)
			return
		}
		if strings.Contains(r.URL.Path, "DTOP") && calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		if strings.Contains(r.URL.Path, "DTOP") {
			_, _ = w.Write(dtop)
			return
		}
		_, _ = w.Write(rp)
	}))
	defer srv.Close()
	src := &HKEXSource{DTOPBase: srv.URL + "/dtop", RP006Base: srv.URL + "/rp", RequestInterval: time.Microsecond}
	day, err := src.FetchDay(context.Background(), date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if day == nil || calls.Load() != 2 || len(paths) != 3 || paths[0] != "/dtop/DTOP_O_20250702.zip" || paths[2] != "/rp/RP006_250702.zip" {
		t.Fatalf("day/calls/paths = %v/%d/%v", day != nil, calls.Load(), paths)
	}

	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }))
	defer notFound.Close()
	_, err = (&HKEXSource{DTOPBase: notFound.URL, RP006Base: notFound.URL, RequestInterval: time.Microsecond}).FetchDay(
		context.Background(), date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100})
	if !errors.Is(err, ErrHKEXNoFile) {
		t.Fatalf("404 error = %v; want ErrHKEXNoFile", err)
	}

	noTrading := hkexZip(t, "no_trading_activities.txt", "No Trading Activity")
	var rpRequested atomic.Bool
	noTradingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "DTOP") {
			_, _ = w.Write(noTrading)
			return
		}
		rpRequested.Store(true)
		http.Error(w, "RP006 must not be fetched", http.StatusInternalServerError)
	}))
	defer noTradingServer.Close()
	_, err = (&HKEXSource{DTOPBase: noTradingServer.URL, RP006Base: noTradingServer.URL, RequestInterval: time.Microsecond}).FetchDay(
		context.Background(), date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100})
	if !errors.Is(err, ErrHKEXNoFile) || rpRequested.Load() {
		t.Fatalf("no-trading archive error/RP request = %v/%v; want ErrHKEXNoFile/false", err, rpRequested.Load())
	}

	emptyRaw := fmt.Sprintf("\"H\",\"DTOP\",\"DCASS\",\"%s\",\"%s194207\",\"SEOCH\",1\n\"T\",0,\"EOF\"\n", date.Format("20060102"), date.Format("20060102"))
	emptyDTOP := hkexZip(t, date.Format("20060102")+"_1_dtop_o_seoch_opt_dtl_all.raw", emptyRaw)
	rpRequested.Store(false)
	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "DTOP") {
			_, _ = w.Write(emptyDTOP)
			return
		}
		rpRequested.Store(true)
		http.Error(w, "RP006 must not be fetched", http.StatusInternalServerError)
	}))
	defer emptyServer.Close()
	_, err = (&HKEXSource{DTOPBase: emptyServer.URL, RP006Base: emptyServer.URL, RequestInterval: time.Microsecond}).FetchDay(
		context.Background(), date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100})
	if !errors.Is(err, ErrHKEXNoFile) || rpRequested.Load() {
		t.Fatalf("empty DTOP error/RP request = %v/%v; want ErrHKEXNoFile/false", err, rpRequested.Load())
	}

	rpUnavailable := hkexZip(t, "rp006.txt", `"No File Available Yet"`)
	partialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "DTOP") {
			_, _ = w.Write(dtop)
			return
		}
		_, _ = w.Write(rpUnavailable)
	}))
	defer partialServer.Close()
	partial, err := (&HKEXSource{DTOPBase: partialServer.URL, RP006Base: partialServer.URL, RequestInterval: time.Microsecond}).FetchDay(
		context.Background(), date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100})
	if err != nil || partial == nil || len(partial.Quotes) != 4 || len(partial.Snapshots) != 0 {
		t.Fatalf("RP006 unavailable day = %v/%v; want 4 official quotes and zero projections", partial, err)
	}
}

func TestHKEXParserRejectsDateAndSettlementMismatch(t *testing.T) {
	date := time.Date(2025, 7, 2, 0, 0, 0, 0, time.UTC)
	dtop, rp := hkexFixture(t, "TCH", date)
	if _, err := parseHKEXDay(date.AddDate(0, 0, 1), HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100}, dtop, rp); err == nil || !strings.Contains(err.Error(), "business date") {
		t.Fatalf("date mismatch error = %v", err)
	}
	// Replacing compressed bytes is not reliable; rebuild a valid mismatched zip.
	dtopRaw, err := zipMember(dtop, "_dtop_o_seoch_opt_dtl_all.raw")
	if err != nil {
		t.Fatal(err)
	}
	rpRaw, err := zipMember(rp, "_rp006-final_o.raw")
	if err != nil {
		t.Fatal(err)
	}
	rpRaw = bytes.Replace(rpRaw, []byte("12.40,14.30"), []byte("12.41,14.30"), 1)
	badRP := hkexZip(t, "20250702_1_rp006-final_o.raw", string(rpRaw))
	badDTOP := hkexZip(t, "20250702_1_dtop_o_seoch_opt_dtl_all.raw", string(dtopRaw))
	if _, err := parseHKEXDay(date, HKEXInstrument{Underlying: "HK.00700", Class: "TCH", LotSize: 100}, badDTOP, badRP); err == nil || !strings.Contains(err.Error(), "settlement mismatch") {
		t.Fatalf("settlement mismatch error = %v", err)
	}
}

func TestInsertHKEXDayIntegrationIdempotent(t *testing.T) {
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
	date := time.Date(2025, 7, 2, 0, 0, 0, 0, time.UTC)
	dtop, rp := hkexFixture(t, "TST", date)
	underlying := "HK.HKEXTEST"
	day, err := parseHKEXDay(date, HKEXInstrument{Underlying: underlying, Class: "TST", LotSize: 100}, dtop, rp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM option_quotes WHERE underlying=$1`, underlying); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying=$1`, underlying); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM option_quotes WHERE underlying=$1`, underlying)
		_, _ = database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying=$1`, underlying)
	})
	partial := *day
	partial.Snapshots = nil
	partial.Quotes = append([]OptionQuoteRow(nil), day.Quotes...)
	for i := range partial.Quotes {
		partial.Quotes[i].ImpliedVol = nil
	}
	q0, s0, err := InsertHKEXDay(context.Background(), database, &partial)
	if err != nil {
		t.Fatal(err)
	}
	q1, s1, err := InsertHKEXDay(context.Background(), database, day)
	if err != nil {
		t.Fatal(err)
	}
	q2, s2, err := InsertHKEXDay(context.Background(), database, day)
	if err != nil {
		t.Fatal(err)
	}
	if q0 != int64(len(day.Quotes)) || s0 != 0 || q1 != int64(len(day.Quotes)) || s1 != int64(len(day.Snapshots)) || q2 != 0 || s2 != 0 {
		t.Fatalf("insert counts partial=%d/%d supplement=%d/%d repeat=%d/%d rows=%d/%d", q0, s0, q1, s1, q2, s2, len(day.Quotes), len(day.Snapshots))
	}
	var quotes, snapshots, distinctDates, ivRows int
	if err := database.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT ts),COUNT(implied_vol) FROM option_quotes WHERE underlying=$1 AND source='hkex'`, underlying).Scan(&quotes, &distinctDates, &ivRows); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM option_quote_snapshots WHERE underlying=$1 AND source='hkex'`, underlying).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if quotes != len(day.Quotes) || snapshots != len(day.Snapshots) || distinctDates != 1 || ivRows != len(day.Quotes) {
		t.Fatalf("persisted counts = %d/%d/%d/%d", quotes, snapshots, distinctDates, ivRows)
	}
	runID, err := BeginHKEXRun(context.Background(), database, "hkex-integration-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM ingestion_runs WHERE id=$1`, runID) })
	if err := FinishHKEXRun(context.Background(), database, runID, true); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM ingestion_runs WHERE id=$1`, runID).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("run status/error = %q/%v", status, err)
	}
}

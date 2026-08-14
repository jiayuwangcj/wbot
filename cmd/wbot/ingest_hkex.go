package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
)

// runIngestHKEX implements the offline-only `wbot ingest hkex` backfill. It
// does not call or alter the live Futu Wheel runner.
func runIngestHKEX(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest hkex", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	runSource := fs.String("source", "cli-hkex", "ingestion run label (stored in ingestion_runs; market rows always source=hkex)")
	symbol := fs.String("symbol", "HK.00700", "market-qualified underlying")
	optionClass := fs.String("class", "TCH", "HKEX option class (HK.00700 = TCH)")
	lotSize := fs.Int64("lot-size", 100, "shares per option contract (HK.00700 = 100)")
	fromRaw := fs.String("from", "", "first business date, YYYY-MM-DD (default: 550 days ago)")
	toRaw := fs.String("to", "", "last business date, YYYY-MM-DD (default: yesterday in Hong Kong)")
	dtopBase := fs.String("dtop-base", ingest.HKEXDefaultDTOPBase, "DTOP archive directory")
	rpBase := fs.String("rp006-base", ingest.HKEXDefaultRP006Base, "RP006 archive directory")
	requestInterval := fs.Duration("request-interval", time.Second, "minimum interval between HTTP requests (official HKEX requires >=1s; shorter is loopback-test only)")
	dryRun := fs.Bool("dry-run", false, "download/parse and print counts without touching PostgreSQL")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest hkex -symbol HK.00700 [flags]\n\n", prog)
		fmt.Fprintln(os.Stderr, "Backfills HKEX official stock-option end-of-day files. DTOP supplies settlement")
		fmt.Fprintln(os.Stderr, "price/turnover/gross OI; RP006-FINAL supplies official IV and underlying price.")
		fmt.Fprintln(os.Stderr, "option_quotes stores OHLC=settlement, adjust=none, source=hkex. For offline Wheel")
		fmt.Fprintln(os.Stderr, "research only, option_quote_snapshots also receives an auditable EOD projection:")
		fmt.Fprintln(os.Stderr, "bid=ask=settlement and delta/theta derived by Black-Scholes with r=0. It is not")
		fmt.Fprintln(os.Stderr, "a historical executable order book and never changes the real-time Futu path.")
		fmt.Fprintln(os.Stderr, "Writes are idempotent by contract+date/source and atomic snapshot batch key.")
		fmt.Fprintln(os.Stderr, "Official requests are serialized at least one second apart with exponential retry;")
		fmt.Fprintln(os.Stderr, "holidays (missing DTOP) are skipped. The default 550-day window normally exceeds")
		fmt.Fprintln(os.Stderr, "300 Hong Kong trading days; a single invocation is capped at two years.")
		fmt.Fprintln(os.Stderr)
		fs.SetOutput(os.Stderr)
		fs.PrintDefaults()
	}

	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if showHelp {
		fs.SetOutput(os.Stderr)
		fs.Usage()
		return 0
	}

	market, _, err := futu.ParseSymbol(strings.TrimSpace(*symbol))
	if err != nil || market != 1 {
		fmt.Fprintf(os.Stderr, "ingest hkex: -symbol must be a market-qualified HK symbol: %v\n", err)
		return 2
	}
	instrument := ingest.HKEXInstrument{
		Underlying: strings.TrimSpace(*symbol),
		Class:      strings.ToUpper(strings.TrimSpace(*optionClass)),
		LotSize:    *lotSize,
	}
	if err := instrument.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "ingest hkex: %v\n", err)
		return 2
	}
	label := strings.TrimSpace(*runSource)
	if label == "" {
		fmt.Fprintln(os.Stderr, "ingest hkex: -source must not be empty")
		return 2
	}
	if *requestInterval <= 0 {
		fmt.Fprintln(os.Stderr, "ingest hkex: -request-interval must be positive")
		return 2
	}
	if *requestInterval < time.Second && (!loopbackHTTPBase(*dtopBase) || !loopbackHTTPBase(*rpBase)) {
		fmt.Fprintln(os.Stderr, "ingest hkex: -request-interval below 1s is allowed only when both endpoints are loopback test servers")
		return 2
	}

	nowHK := time.Now().In(time.FixedZone("HKT", 8*60*60))
	today := time.Date(nowHK.Year(), nowHK.Month(), nowHK.Day(), 0, 0, 0, 0, time.UTC)
	from, err := parseHKEXDate(*fromRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest hkex: -from: %v\n", err)
		return 2
	}
	to, err := parseHKEXDate(*toRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest hkex: -to: %v\n", err)
		return 2
	}
	if to.IsZero() {
		to = today.AddDate(0, 0, -1)
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -549)
	}
	if from.After(to) {
		fmt.Fprintln(os.Stderr, "ingest hkex: -from must not be after -to")
		return 2
	}
	if !to.Before(today) {
		fmt.Fprintln(os.Stderr, "ingest hkex: -to must be before today's Hong Kong date (EOD files publish after close)")
		return 2
	}
	days := int(to.Sub(from)/(24*time.Hour)) + 1
	if days > ingest.HKEXMaxBackfillDays {
		fmt.Fprintf(os.Stderr, "ingest hkex: date range is %d days; maximum is %d\n", days, ingest.HKEXMaxBackfillDays)
		return 2
	}

	src := &ingest.HKEXSource{
		Client:          &http.Client{Timeout: 45 * time.Second},
		DTOPBase:        strings.TrimSpace(*dtopBase),
		RP006Base:       strings.TrimSpace(*rpBase),
		RequestInterval: *requestInterval,
	}
	ctx := context.Background()
	var runID int64
	finishedRun := false
	var dbConn *sql.DB
	if !*dryRun {
		d := strings.TrimSpace(*dsn)
		if d == "" {
			d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
		}
		if d == "" {
			fmt.Fprintln(os.Stderr, "ingest hkex: set -dsn or WBOT_PG_DSN")
			return 2
		}
		dbConn, err = db.Open(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest hkex: open db: %v\n", err)
			return 1
		}
		defer dbConn.Close()
		if err := db.MigrateUp(dbConn); err != nil {
			fmt.Fprintf(os.Stderr, "ingest hkex: migrate: %v\n", err)
			return 1
		}
		runID, err = ingest.BeginHKEXRun(ctx, dbConn, label)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest hkex: %v\n", err)
			return 1
		}
		defer func() {
			if !finishedRun {
				_ = ingest.FinishHKEXRun(context.Background(), dbConn, runID, false)
			}
		}()
	}

	stats := ingest.HKEXBackfillStats{}
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		weekday := date.Weekday()
		if weekday == time.Saturday || weekday == time.Sunday {
			continue
		}
		day, err := src.FetchDay(ctx, date, instrument)
		if errors.Is(err, ingest.ErrHKEXNoFile) {
			stats.MissingDays++
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest hkex: %s: %v\n", date.Format("2006-01-02"), err)
			return 1
		}
		stats.TradingDays++
		stats.QuoteRows += len(day.Quotes)
		stats.SnapshotRows += len(day.Snapshots)
		if len(day.Snapshots) == 0 {
			stats.QuoteOnlyDays++
			fmt.Fprintf(os.Stderr, "ingest hkex: %s: RP006 unavailable; retained %d official DTOP settlement rows without research snapshots\n",
				date.Format("2006-01-02"), len(day.Quotes))
		}
		if !*dryRun {
			quotes, snapshots, err := ingest.InsertHKEXDay(ctx, dbConn, day)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ingest hkex: %s: %v\n", date.Format("2006-01-02"), err)
				return 1
			}
			stats.InsertedQuotes += quotes
			stats.InsertedSnapshots += snapshots
		}
		if stats.TradingDays == 1 || stats.TradingDays%20 == 0 {
			fmt.Fprintf(os.Stderr, "ingest hkex: progress date=%s trading_days=%d quotes=%d snapshots=%d\n",
				date.Format("2006-01-02"), stats.TradingDays, stats.QuoteRows, stats.SnapshotRows)
		}
	}
	if stats.TradingDays == 0 {
		fmt.Fprintln(os.Stderr, "ingest hkex: no published trading-day files in requested range")
		return 1
	}
	if !*dryRun {
		if err := ingest.FinishHKEXRun(ctx, dbConn, runID, true); err != nil {
			fmt.Fprintf(os.Stderr, "ingest hkex: %v\n", err)
			return 1
		}
		finishedRun = true
	}
	mode := "ok"
	if *dryRun {
		mode = "dry-run"
	}
	fmt.Printf("ingest hkex: %s: underlying=%s class=%s from=%s to=%s trading_days=%d missing_days=%d quote_only_days=%d option_quotes=%d snapshots=%d inserted_quotes=%d inserted_snapshots=%d source=hkex adjust=none projection=eod-settlement-bs-r0\n",
		mode, instrument.Underlying, instrument.Class, from.Format("2006-01-02"), to.Format("2006-01-02"),
		stats.TradingDays, stats.MissingDays, stats.QuoteOnlyDays, stats.QuoteRows, stats.SnapshotRows, stats.InsertedQuotes, stats.InsertedSnapshots)
	return 0
}

func parseHKEXDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", raw)
	}
	return t, nil
}

func loopbackHTTPBase(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.TrimSpace(u.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

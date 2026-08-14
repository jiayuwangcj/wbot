package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/ingest"
)

const (
	tencentTimeframe = "1d"
	tencentAdjust    = "fwd"
	tencentSource    = "tencent"
)

type prefetchedTencentBars struct {
	bars []ingest.Bar
}

func (s prefetchedTencentBars) Bars(ctx context.Context, _ domain.Symbol, _ string, _, _ time.Time) ([]ingest.Bar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]ingest.Bar(nil), s.bars...), nil
}

// runIngestTencent implements `wbot ingest tencent`: one-shot qfq daily-bar
// backfill from Tencent Finance into canonical adjust=fwd/source=tencent rows.
func runIngestTencent(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest tencent", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	runSource := fs.String("source", "cli-tencent", "ingestion run label")
	symbol := fs.String("symbol", "HK.00700", "market-qualified symbol (HK.00700 or US.JD)")
	timeframe := fs.String("timeframe", tencentTimeframe, "bar timeframe (Tencent backfill supports 1d only)")
	count := fs.Int("count", ingest.TencentMaxBars, "requested daily bars (1..1000; provider may include the forming bar)")
	includeForming := fs.Bool("include-forming", false, "retain today's Beijing-time forming daily bar (unsafe for idempotent backfill)")
	from := fs.String("from", "", "start of bar range, RFC3339; empty = provider history window")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = latest available day")
	endpoint := fs.String("endpoint", ingest.TencentKlineEndpoint, "Tencent Finance K-line endpoint")
	dryRun := fs.Bool("dry-run", false, "fetch bars and print a summary without touching the database")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest tencent -symbol HK.00700 [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Backfills free Tencent Finance qfq daily K-lines into bars as adjust=fwd, source=tencent.\n")
		fmt.Fprintf(os.Stderr, "Writes are idempotent through the bars primary key; repeated runs do not duplicate a date.\n")
		fmt.Fprintf(os.Stderr, "By default, the newest bar is discarded when its Beijing calendar date is today, preventing\n")
		fmt.Fprintf(os.Stderr, "a partial daily bar from being frozen by idempotent inserts. Run again on the next calendar day\n")
		fmt.Fprintf(os.Stderr, "to ingest the completed bar, or use -include-forming to retain the old partial-bar behavior.\n")
		fmt.Fprintf(os.Stderr, "HK.00700 currently returns 1000+ rows with -count 1000. Tencent US symbols such as\n")
		fmt.Fprintf(os.Stderr, "US.JD currently return only the latest trading day; completed rows are recorded once their\n")
		fmt.Fprintf(os.Stderr, "Beijing date is no longer today, so US history must accumulate daily. Requests are spaced at least one second apart and\n")
		fmt.Fprintf(os.Stderr, "transient failures use exponential backoff. This command never changes the Futu live path.\n\n")
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

	instrument, err := ingest.ParseTencentInstrument(strings.TrimSpace(*symbol))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: %v\n", err)
		return 2
	}
	tf := strings.TrimSpace(*timeframe)
	if tf != "1d" && !strings.EqualFold(tf, "K_DAY") && !strings.EqualFold(tf, "day") {
		fmt.Fprintf(os.Stderr, "ingest tencent: unsupported timeframe %q (want 1d)\n", tf)
		return 2
	}
	tf = tencentTimeframe
	if *count < 1 || *count > ingest.TencentMaxBars {
		fmt.Fprintf(os.Stderr, "ingest tencent: -count must be between 1 and %d\n", ingest.TencentMaxBars)
		return 2
	}
	label := strings.TrimSpace(*runSource)
	if label == "" {
		fmt.Fprintln(os.Stderr, "ingest tencent: -source must not be empty")
		return 2
	}
	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: %v\n", err)
		return 2
	}
	if !fromT.IsZero() && !toT.IsZero() && fromT.After(toT) {
		fmt.Fprintln(os.Stderr, "ingest tencent: -from must not be after -to")
		return 2
	}

	src := ingest.TencentSource{
		Client:         &http.Client{Timeout: 20 * time.Second},
		Endpoint:       strings.TrimSpace(*endpoint),
		Count:          *count,
		IncludeForming: *includeForming,
	}
	ctx := context.Background()
	fetch := func() ([]ingest.Bar, error) {
		bars, err := src.Bars(ctx, instrument.Symbol, tf, fromT, toT)
		if err != nil {
			return nil, err
		}
		if err := ingest.ValidateBars(bars); err != nil {
			return nil, err
		}
		return bars, nil
	}
	printUSLimitation := func(barCount int) {
		if instrument.Market == "US" && barCount <= 1 {
			fmt.Fprintln(os.Stderr, "ingest tencent: 腾讯美股仅当日,历史靠每日积累")
		}
	}
	formingMode := "forming bar excluded when dated today in Beijing"
	if *includeForming {
		formingMode = "forming bar included"
	}

	if *dryRun {
		bars, err := fetch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest tencent: %v\n", err)
			return 1
		}
		printUSLimitation(len(bars))
		fmt.Printf("ingest tencent: dry-run: %d bars, %s .. %s (symbol=%s timeframe=%s source=tencent adjusted=qfq; %s)\n",
			len(bars), bars[0].Ts.Format(time.RFC3339), bars[len(bars)-1].Ts.Format(time.RFC3339), instrument.Symbol, tf, formingMode)
		return 0
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintln(os.Stderr, "ingest tencent: set -dsn or WBOT_PG_DSN")
		return 2
	}
	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: migrate: %v\n", err)
		return 1
	}
	bars, err := fetch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: %v\n", err)
		return 1
	}
	if err := ingest.RunIngestion(ctx, database, label, instrument.Symbol, tf, tencentAdjust, tencentSource, fromT, toT, prefetchedTencentBars{bars: bars}); err != nil {
		fmt.Fprintf(os.Stderr, "ingest tencent: %v\n", err)
		return 1
	}
	printUSLimitation(len(bars))
	fmt.Fprintf(os.Stderr, "ingest tencent: ok (symbol=%s timeframe=%s bars=%d source=tencent adjusted=qfq adjust=fwd; %s)\n",
		instrument.Symbol, tf, len(bars), formingMode)
	return 0
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
)

const (
	futuSource    = "futu"
	futuSymbol    = "HK.00700"
	futuTimeframe = "30m"
)

type prefetchedFutuBars struct {
	bars []ingest.Bar
}

func (s prefetchedFutuBars) Bars(ctx context.Context, _ domain.Symbol, _ string, _, _ time.Time) ([]ingest.Bar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]ingest.Bar(nil), s.bars...), nil
}

// filterInvalidBars drops bars failing per-bar OHLC sanity checks, returning the
// filtered slice, the count skipped and the ts of the first dropped bar.
func filterInvalidBars(bars []ingest.Bar) ([]ingest.Bar, int, time.Time) {
	out := make([]ingest.Bar, 0, len(bars))
	var skipped int
	var first time.Time
	haveFirst := false
	for _, b := range bars {
		if err := ingest.ValidateBar(b); err != nil {
			skipped++
			if !haveFirst {
				first = b.Ts
				haveFirst = true
			}
			continue
		}
		out = append(out, b)
	}
	return out, skipped, first
}

// runIngestFutu implements `wbot ingest futu`: one-shot K-line backfill from the
// futu-opend-rs gateway REST /api/history-kline (paged via next_req_key) into the
// bars table as adjust=none/source=futu, the same raw-price basis as option snapshots.
func runIngestFutu(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest futu", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	endpoint := fs.String("endpoint", "", "Futu gateway REST base URL (default: $FUTU_GATEWAY_URL or built-in default)")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	runSource := fs.String("source", futuSource, "ingestion run label")
	symbol := fs.String("symbol", futuSymbol, "market-qualified symbol (HK.00700 or US.JD)")
	timeframe := fs.String("timeframe", futuTimeframe, "bar timeframe: K_1M K_5M K_15M K_30M K_60M K_DAY K_WEEK K_MONTH (ingest names 1m..1mo also accepted)")
	adjust := fs.String("adjust", futu.AdjustNone, "adjustment: none (raw prices, default), fwd or back; maps to futu rehab_type (doc/DATA_STANDARD.md)")
	from := fs.String("from", "", "start of bar range, RFC3339; empty = earliest available (HK.00700 30m/60m depth: 2015-04-16)")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = latest available bar")
	dryRun := fs.Bool("dry-run", false, "fetch bars and print a summary without touching the database")
	skipInvalid := fs.Bool("skip-invalid", false, "skip individual invalid bars from the source instead of failing the whole batch")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest futu -symbol HK.00700 -timeframe 30m [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Backfills K-lines from the futu-opend-rs gateway REST /api/history-kline (POST, up to 1000 bars per request)\n")
		fmt.Fprintf(os.Stderr, "into bars as adjust=none (raw prices, same basis as option snapshots), source=futu.\n")
		fmt.Fprintf(os.Stderr, "Paging follows the next_req_key cursor until no further pages; requests share the global futu rate pool.\n")
		fmt.Fprintf(os.Stderr, "Measured depth: HK.00700 30m/60m history reaches back to 2015-04-16, so the full window can be fetched.\n")
		fmt.Fprintf(os.Stderr, "Writes are idempotent through the bars primary key; repeated runs do not duplicate a bar.\n")
		fmt.Fprintf(os.Stderr, "This command never changes the Futu live path.\n\n")
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

	sym := strings.TrimSpace(*symbol)
	if _, _, err := futu.ParseSymbol(sym); err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	_, ingestTF, err := futu.ParseTimeframe(strings.TrimSpace(*timeframe))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	_, adjustName, err := futu.ParseAdjust(*adjust)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	label := strings.TrimSpace(*runSource)
	if label == "" {
		fmt.Fprintln(os.Stderr, "ingest futu: -source must not be empty")
		return 2
	}
	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	if !fromT.IsZero() && !toT.IsZero() && fromT.After(toT) {
		fmt.Fprintln(os.Stderr, "ingest futu: -from must not be after -to")
		return 2
	}

	src := ingest.FutuSource{
		Client: futu.NewClient(resolveFutuGateway(strings.TrimSpace(*endpoint))),
		Adjust: adjustName,
	}
	ctx := context.Background()
	fetch := func() ([]ingest.Bar, error) {
		bars, err := src.Bars(ctx, domain.Symbol(sym), ingestTF, fromT, toT)
		if err != nil {
			return nil, err
		}
		if *skipInvalid {
			filtered, skipped, first := filterInvalidBars(bars)
			if skipped > 0 {
				fmt.Fprintf(os.Stderr, "ingest futu: skipped %d invalid bar(s), first at %s\n", skipped, first.Format(time.RFC3339))
			}
			bars = filtered
		}
		if err := ingest.ValidateBars(bars); err != nil {
			return nil, err
		}
		return bars, nil
	}

	if *dryRun {
		bars, err := fetch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
			return 1
		}
		fmt.Printf("ingest futu: dry-run: %d bars, %s .. %s (symbol=%s timeframe=%s source=%s adjust=%s)\n",
			len(bars), bars[0].Ts.Format(time.RFC3339), bars[len(bars)-1].Ts.Format(time.RFC3339), sym, ingestTF, label, adjustName)
		return 0
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintln(os.Stderr, "ingest futu: set -dsn or WBOT_PG_DSN")
		return 2
	}
	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: migrate: %v\n", err)
		return 1
	}
	bars, err := fetch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 1
	}
	if err := ingest.RunIngestion(ctx, database, label, domain.Symbol(sym), ingestTF, adjustName, futuSource, fromT, toT, prefetchedFutuBars{bars: bars}); err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "ingest futu: ok (source=%s symbol=%s timeframe=%s bars=%d adjust=%s)\n", label, sym, ingestTF, len(bars), adjustName)
	return 0
}

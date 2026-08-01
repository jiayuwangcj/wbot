package main

import (
	"context"
	"errors"
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

// runIngestFutu implements `wbot ingest futu`: REST K-lines from the
// futu-opend-rs gateway into the bars table (see doc/FUTU.md §8).
func runIngestFutu(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest futu", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultAddr, "gateway REST base URL")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	source := fs.String("source", "cli-futu", "ingestion source label")
	symbol := fs.String("symbol", "", "market-qualified symbol (e.g. HK.00700)")
	timeframe := fs.String("timeframe", "", "futu K-line name: K_1M K_5M K_15M K_30M K_60M K_DAY K_WEEK K_MONTH (ingest names 1m..1mo also accepted)")
	adjust := fs.String("adjust", futu.AdjustFwd, "adjustment: fwd (前复权, default) or none; maps to futu rehab_type (doc/DATA_STANDARD.md)")
	from := fs.String("from", "", "start of bar range, RFC3339 (e.g. 2024-06-01T00:00:00Z); empty = full history")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = now (includes the forming bar)")
	every := fs.Duration("every", 0, "if >0, repeat ingestion at this interval until SIGINT")
	dryRun := fs.Bool("dry-run", false, "fetch bars and print a summary without touching the database")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest futu -symbol HK.00700 -timeframe K_DAY [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Fetches OHLCV K-lines from the futu-opend-rs gateway (REST 22222, /api/history-kline) and writes one ingestion run.\n")
		fmt.Fprintf(os.Stderr, "With -every, repeats at that interval (duplicate bars are skipped via ON CONFLICT; requests share the global futu rate pool).\n")
		fmt.Fprintf(os.Stderr, "Intraday timeframes over a long range produce many bars: prefer explicit -from/-to (e.g. the last 7 days).\n\n")
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
	if sym == "" {
		fmt.Fprintf(os.Stderr, "ingest futu: -symbol is required (e.g. HK.00700)\n")
		return 2
	}
	if _, _, err := futu.ParseSymbol(sym); err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	_, ingestTF, err := futu.ParseTimeframe(*timeframe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 2
	}
	_, adjustName, err := futu.ParseAdjust(*adjust)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
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

	src := ingest.FutuSource{Client: futu.NewClient(*addr), Adjust: adjustName}
	s := domain.Symbol(sym)
	if *dryRun {
		bars, err := src.Bars(context.Background(), s, ingestTF, fromT, toT)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
			return 1
		}
		if len(bars) == 0 {
			fmt.Fprintf(os.Stderr, "ingest futu: dry-run: no bars (symbol=%s timeframe=%s)\n", sym, ingestTF)
			return 1
		}
		fmt.Printf("ingest futu: dry-run: %d bars, %s .. %s (symbol=%s timeframe=%s adjust=%s)\n",
			len(bars), bars[0].Ts.Format(time.RFC3339), bars[len(bars)-1].Ts.Format(time.RFC3339), sym, ingestTF, adjustName)
		return 0
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest futu: set -dsn or WBOT_PG_DSN\n")
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

	ctx, cancel := ingestRepeatCtx(*every)
	defer cancel()
	err = ingest.RunEveryResilient(ctx, *every, func(ctx context.Context) error {
		if err := ingest.RunIngestion(ctx, database, strings.TrimSpace(*source), s, ingestTF, adjustName, "futu", fromT, toT, src); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ingest futu: ok (source=%s symbol=%s timeframe=%s adjust=%s)\n",
			strings.TrimSpace(*source), sym, ingestTF, adjustName)
		return nil
	}, func(err error) {
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ingest futu: %v\n", err)
		return 1
	}
	return 0
}

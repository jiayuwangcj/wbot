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

// runIngestFutuOption implements `wbot ingest futu-option`: option chain
// daily K-lines plus the underlying's daily bars, cache-first (a second run
// with the data already in DB skips the pull; doc/DATA_STANDARD.md).
func runIngestFutuOption(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest futu-option", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultAddr, "gateway REST base URL")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	source := fs.String("source", "cli-futu-option", "ingestion source label")
	symbol := fs.String("symbol", "", "market-qualified underlying (e.g. HK.00700)")
	days := fs.Int("days", 7, "pull window: the last N days of daily K-lines")
	expiries := fs.Int("expiries", 1, "number of nearest listed expiries to include (<=0 = all)")
	adjust := fs.String("adjust", futu.AdjustFwd, "adjustment: fwd (前复权, default) or none (doc/DATA_STANDARD.md)")
	force := fs.Bool("force", false, "bypass the DB cache check and pull again (ON CONFLICT keeps rows idempotent)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest futu-option -symbol HK.00700 [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Pulls the option chain (nearest -expiries listed expiries, all strikes) from the\n")
		fmt.Fprintf(os.Stderr, "futu-opend-rs gateway REST (22222), stores each contract's daily K-lines in\n")
		fmt.Fprintf(os.Stderr, "option_quotes, the underlying's daily bars in bars, and registers the symbol\n")
		fmt.Fprintf(os.Stderr, "in watchlist. Cache-first: if option_quotes/bars already cover the window for\n")
		fmt.Fprintf(os.Stderr, "-adjust, the pull is skipped (doc/DATA_STANDARD.md).\n\n")
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
		fmt.Fprintf(os.Stderr, "ingest futu-option: -symbol is required (e.g. HK.00700)\n")
		return 2
	}
	if _, _, err := futu.ParseSymbol(sym); err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
		return 2
	}
	_, adjustName, err := futu.ParseAdjust(*adjust)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
		return 2
	}
	if *days <= 0 {
		fmt.Fprintf(os.Stderr, "ingest futu-option: -days must be > 0\n")
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest futu-option: set -dsn or WBOT_PG_DSN\n")
		return 2
	}
	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu-option: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu-option: migrate: %v\n", err)
		return 1
	}

	ctx := context.Background()
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -*days)
	to := now.Add(24 * time.Hour)

	client := futu.NewClient(*addr)
	optHit, optRows, err := ingest.OptionQuotesCached(ctx, database, sym, from, to, adjustName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
		return 1
	}
	barHit, barRows, err := ingest.BarsCached(ctx, database, sym, "1d", adjustName, from, to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
		return 1
	}
	if !*force && optHit && barHit {
		fmt.Printf("ingest futu-option: cache hit: option_quotes=%d bars=%d in window for %s (adjust=%s), skip pull\n",
			optRows, barRows, sym, adjustName)
		return 0
	}
	if *force && optHit && barHit {
		fmt.Printf("ingest futu-option: -force: cache has option_quotes=%d bars=%d, pulling again\n",
			optRows, barRows)
	}

	runSource := strings.TrimSpace(*source)
	if !optHit || *force {
		stats, err := ingest.RunOptionIngestion(ctx, database, client, sym, adjustName, from, to, *expiries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
			return 1
		}
		if stats.Skipped > 0 {
			fmt.Fprintf(os.Stderr, "ingest futu-option: ok (source=%s underlying=%s expiries=%d contracts=%d rows=%d skipped=%d adjust=%s)\n",
				runSource, sym, stats.Expiries, stats.Contracts, stats.Rows, stats.Skipped, adjustName)
		} else {
			fmt.Fprintf(os.Stderr, "ingest futu-option: ok (source=%s underlying=%s expiries=%d contracts=%d rows=%d adjust=%s)\n",
				runSource, sym, stats.Expiries, stats.Contracts, stats.Rows, adjustName)
		}
	}
	if !barHit {
		_, ingestTF, err := futu.ParseTimeframe("K_DAY")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
			return 1
		}
		src := ingest.FutuSource{Client: client, Adjust: adjustName}
		if err := ingest.RunIngestion(ctx, database, runSource+"-bars", domain.Symbol(sym), ingestTF, adjustName, "futu", from, to, src); err != nil {
			fmt.Fprintf(os.Stderr, "ingest futu-option: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "ingest futu-option: ok (source=%s symbol=%s timeframe=%s adjust=%s)\n",
			runSource+"-bars", sym, ingestTF, adjustName)
	}
	return 0
}

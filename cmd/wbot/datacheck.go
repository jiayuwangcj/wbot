package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/futu"
)

// runDataCheck implements the manual completeness gate. It is read-only by
// default; -repair explicitly runs the same repair cycle as the serve scheduler.
func runDataCheck(prog string, argv []string) int {
	fs := flag.NewFlagSet("datacheck", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	jsonOutput := fs.Bool("json", false, "print the complete machine-readable report")
	nowValue := fs.String("now", "", "check time as RFC3339 (default: current time; useful for deterministic operations)")
	repair := fs.Bool("repair", false, "pull each missing/stale item from the Futu gateway, then recheck")
	addr := fs.String("addr", "", "Futu gateway REST base URL for -repair (default: $FUTU_GATEWAY_URL or built-in default)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s datacheck [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Checks every watchlist symbol for the 8 timeframe x 3 adjustment bars matrix and a current option chain.\n")
		fmt.Fprintf(os.Stderr, "Exits 1 when data is missing or stale; exits 0 when the watchlist is complete.\n\n")
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
	now := time.Now()
	if value := strings.TrimSpace(*nowValue); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "datacheck: -now: invalid RFC3339 time %q\n", value)
			return 2
		}
		now = parsed
	}
	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "datacheck: set -dsn or WBOT_PG_DSN\n")
		return 2
	}
	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "datacheck: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "datacheck: migrate: %v\n", err)
		return 1
	}
	ctx := context.Background()
	var report datacheck.Report
	if *repair {
		gatewayAddr := resolveFutuGateway(*addr)
		result, err := (datacheck.Service{
			Loader:   datacheck.DBLoader{DB: database},
			Repairer: futuDataRepairer{db: database, client: futu.NewClient(gatewayAddr)},
			Policy:   datacheck.DefaultPolicy(),
		}).Run(ctx, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "datacheck: repair: %v\n", err)
			return 1
		}
		for _, repairErr := range result.RepairErrors {
			fmt.Fprintf(os.Stderr, "datacheck: repair: %s\n", repairErr)
		}
		report = result.After
	} else {
		symbols, bars, options, err := datacheck.Snapshot(ctx, database)
		if err != nil {
			fmt.Fprintf(os.Stderr, "datacheck: %v\n", err)
			return 1
		}
		report = datacheck.Check(symbols, bars, options, now, datacheck.DefaultPolicy())
	}
	if *jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "datacheck: encode report: %v\n", err)
			return 1
		}
	} else {
		for _, item := range report.Items {
			if item.State == datacheck.StateComplete {
				continue
			}
			name := item.Kind
			if item.Kind == "bars" {
				name = item.Timeframe + "/" + item.Adjust
			}
			fmt.Printf("%s %s %s", item.Symbol, name, item.State)
			if !item.MaxTs.IsZero() {
				fmt.Printf(" max_ts=%s", item.MaxTs.Format(time.RFC3339))
			}
			fmt.Println()
		}
		fmt.Printf("datacheck: symbols=%d required=%d missing=%d stale=%d complete=%t\n",
			report.Symbols, report.Total, report.Missing, report.Stale, report.Complete())
	}
	if !report.Complete() {
		return 1
	}
	return 0
}

// seed-wheel-demo writes deterministic synthetic option snapshots used only
// by local development and acceptance tests. It deliberately bypasses HTTP:
// the product audit API remains read-only and no provider capability is
// implied by source=demo-fixture.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/seed-wheel-demo.go DSN SYMBOL...")
		os.Exit(2)
	}
	database, err := db.Open(os.Args[1])
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fatal(err)
	}

	ctx := context.Background()
	expiry := time.Date(2024, 6, 8, 0, 0, 0, 0, time.UTC)
	fixtures := []struct {
		key   string
		price float64
		at    time.Time
	}{
		{"wheel-demo-20240601", 100.50, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		{"wheel-demo-20240602", 101.25, time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC)},
		{"wheel-demo-20240603", 102.00, time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC)},
	}
	for _, underlying := range os.Args[2:] {
		for _, fixture := range fixtures {
			_, err := database.ExecContext(ctx, `
INSERT INTO option_quote_snapshots
  (symbol, underlying, option_type, strike, expiry, source, snapshot_key,
   underlying_price, delta, bid, ask, iv, theta, volume, open_interest,
   lot_size, observed_at, ingested_at)
VALUES ($1, $2, 'PUT', 95, $3, 'demo-fixture', $4, $5, -0.30, 2.00,
        2.20, 0.25, -0.10, 100, 1000, 100, $6, $6)
ON CONFLICT (underlying, observed_at, snapshot_key, symbol) DO NOTHING`,
				underlying+"-P95", underlying, expiry, fixture.key, fixture.price, fixture.at)
			if err != nil {
				fatal(fmt.Errorf("%s %s: %w", underlying, fixture.key, err))
			}
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "seed-wheel-demo:", err)
	os.Exit(1)
}

package datacheck

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

func TestSnapshotIntegration(t *testing.T) {
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

	const symbol = "US.DATACHECK"
	cleanup := func() {
		_, _ = database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM watchlist WHERE symbol = $1`, symbol)
	}
	cleanup()
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := database.Exec(`INSERT INTO watchlist(symbol, strategy) VALUES ($1, 'wheel')`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO bars(symbol,timeframe,ts,open,high,low,close,volume,adjust,source)
VALUES ($1,'1d',$2,10,11,9,10.5,100,'fwd','datacheck-test')`, symbol, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO option_quotes(symbol,underlying,option_type,strike,expiry,ts,open,high,low,close,volume,adjust,source)
VALUES ('US.DATACHECK260901C10',$1,'call',10,$2,$3,1,1,1,1,1,'fwd','datacheck-test')`,
		symbol, now.AddDate(0, 1, 0), now); err != nil {
		t.Fatal(err)
	}
	// The older snapshot has a later expiry. Snapshot must not combine that
	// expiry with the latest timestamp when aggregating option coverage.
	if _, err := database.Exec(`
INSERT INTO option_quotes(symbol,underlying,option_type,strike,expiry,ts,open,high,low,close,volume,adjust,source)
VALUES ('US.DATACHECK260901P10',$1,'put',10,$2,$3,1,1,1,1,1,'fwd','datacheck-test')`,
		symbol, now.AddDate(0, 2, 0), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	symbols, bars, options, err := Snapshot(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	var option *OptionCoverage
	for i := range options {
		if options[i].Underlying == symbol {
			option = &options[i]
			break
		}
	}
	if option == nil {
		t.Fatalf("option coverage = %+v; want underlying %s", options, symbol)
	}
	if !option.MaxTs.Equal(now) {
		t.Fatalf("option max ts = %s; want %s", option.MaxTs, now)
	}
	wantExpiry := now.AddDate(0, 1, 0)
	gotY, gotM, gotD := option.MaxExpiry.Date()
	wantY, wantM, wantD := wantExpiry.Date()
	if gotY != wantY || gotM != wantM || gotD != wantD {
		t.Fatalf("option max expiry = %s; want latest snapshot expiry date %s", option.MaxExpiry, wantExpiry.Format("2006-01-02"))
	}
	report := Check(symbols, bars, options, now, Policy{Timeframes: []string{"1d"}, Adjusts: []string{"fwd"}, Options: true})
	var found []Item
	for _, item := range report.Items {
		if item.Symbol == symbol {
			found = append(found, item)
		}
	}
	if len(found) != 2 || found[0].State != StateComplete || found[1].State != StateComplete {
		t.Fatalf("items for %s = %+v; want complete bars + options", symbol, found)
	}
}

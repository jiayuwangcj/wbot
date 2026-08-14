package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
)

// runIngestAccount implements `wbot ingest account`: snapshot the account's
// funds (protobuf TCP 11111, same read-only query as `futu funds`) into
// account_snapshots — the data layer of the 资产曲线 (equity curve).
// Repeated runs append points; ON CONFLICT makes same-instant repeats no-ops.
func runIngestAccount(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest account", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultProtoAddr, "gateway OpenD protobuf address (TCP 11111)")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	env := fs.String("env", "sim", "trading environment: sim (paper, default) or real (read-only)")
	accID := fs.Uint64("acc-id", 0, "account id (default: first account of -env)")
	every := fs.Duration("every", 0, "if >0, repeat the snapshot at this interval until SIGINT")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest account [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Queries account funds over the protobuf API (TCP 11111, read-only) and appends one row to account_snapshots (资产曲线数据层).\n")
		fmt.Fprintf(os.Stderr, "With -every, takes a snapshot at that interval (e.g. -every 1h from cron) — the equity curve is built from these rows.\n\n")
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

	e, err := parseFutuEnv(*env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest account: %v\n", err)
		return 2
	}
	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest account: set -dsn or WBOT_PG_DSN\n")
		return 2
	}
	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest account: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest account: migrate: %v\n", err)
		return 1
	}

	tc, ok := openTradeClient(prog, "ingest account", *addr)
	if !ok {
		return 1
	}
	defer tc.Close()
	acc, ok := resolveAccount(prog, "ingest account", tc, e, *accID, "")
	if !ok {
		return 1
	}

	ctx, cancel := ingestRepeatCtx(*every)
	defer cancel()
	err = ingest.RunEveryResilient(ctx, *every, func(ctx context.Context) error {
		return snapshotAccount(ctx, database, tc, acc, e)
	}, func(err error) {
		fmt.Fprintf(os.Stderr, "ingest account: %v\n", err)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ingest account: %v\n", err)
		return 1
	}
	return 0
}

// snapshotAccount queries funds once and appends the row to account_snapshots,
// reporting the captured values and how many rows were inserted (0 = same
// instant already captured — ON CONFLICT no-op).
func snapshotAccount(ctx context.Context, dbq *sql.DB, tc *futu.TradeClient, acc *trdcommon.TrdAcc, env futu.Env) error {
	funds, err := tc.Funds(ctx, acc)
	if err != nil {
		return err
	}
	res, err := dbq.ExecContext(ctx, `
INSERT INTO account_snapshots (env, acc_id, total_assets, cash, market_val, frozen_cash, power, captured_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (env, acc_id, captured_at) DO NOTHING`,
		futu.EnvName(env), acc.GetAccID(),
		funds.GetTotalAssets(), funds.GetCash(), funds.GetMarketVal(), funds.GetFrozenCash(), funds.GetPower(),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	fmt.Printf("ingest account: snapshot acc_id=%d env=%s total_assets=%.2f cash=%.2f market_val=%.2f power=%.2f (rows=%d)\n",
		acc.GetAccID(), futu.EnvName(env), funds.GetTotalAssets(), funds.GetCash(), funds.GetMarketVal(), funds.GetPower(), n)
	return nil
}

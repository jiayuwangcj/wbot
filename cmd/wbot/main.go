package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/agent"
	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/backtestexec"
	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/configyaml"
	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/httpapi"
	"github.com/jiayu/wbot/internal/httpregister"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/master"
	"github.com/jiayu/wbot/internal/paper"
	"github.com/jiayu/wbot/internal/poll"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/webui"
)

// Set at link time: go build -ldflags "-X main.version=v1.2.3"
var version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	if len(argv) < 2 {
		usage(argv)
		return 2
	}
	switch argv[1] {
	case "-h", "-help", "--help", "help":
		usage(argv)
		return 0
	case "-version", "--version", "version":
		fmt.Println(version)
		return 0
	case "agent":
		return runAgent(argv[0], argv[2:])
	case "master":
		return runMaster(argv[0], argv[2:])
	case "paper":
		return runPaper(argv[0], argv[2:])
	case "ingest":
		return runIngest(argv[0], argv[2:])
	case "futu":
		return runFutu(argv[0], argv[2:])
	case "backtest":
		return runBacktest(argv[0], argv[2:])
	case "watchlist":
		return runWatchlist(argv[0], argv[2:])
	case "configyaml":
		return runConfigYAML(argv[0], argv[2:])
	case "serve":
		return runServe(argv[0], argv[2:])
	default:
		usage(argv)
		return 2
	}
}

func runAgent(prog string, argv []string) int {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	id := fs.String("id", "cli-agent", "agent identity registered with the master")
	masterURL := fs.String("master-url", "", "if set, register via HTTP(S) at this base URL (e.g. http://127.0.0.1:8080 or https://...); default is in-process master.Memory")
	interval := fs.Duration("interval", 20*time.Millisecond, "heartbeat interval")
	duration := fs.Duration("duration", 200*time.Millisecond, "run wall-clock time; 0 means until SIGINT")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Runs internal/poll.Run: heartbeats register the agent with the master (in-memory or HTTP).\n\n")
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

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if *duration > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), *duration)
	} else {
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt)
	}
	defer cancel()

	a := agent.Stub{ID: *id}
	var m master.Facade
	if strings.TrimSpace(*masterURL) != "" {
		m = &httpregister.RemoteFacade{
			Client: &httpregister.Client{
				BaseURL:      *masterURL,
				RetryMax:     2,
				RetryBackoff: 50 * time.Millisecond,
			},
			Ctx: ctx,
		}
	} else {
		m = master.NewMemory()
	}
	if err := poll.Run(ctx, *interval, a, m); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		return 1
	}
	return 0
}

func runMaster(prog string, argv []string) int {
	fs := flag.NewFlagSet("master", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	listen := fs.String("listen", "127.0.0.1:0", "TCP listen address (POST /v1/register)")
	tlsCert := fs.String("tls-cert", "", "path to PEM certificate (set with -tls-key for HTTPS)")
	tlsKey := fs.String("tls-key", "", "path to PEM private key (set with -tls-cert for HTTPS)")
	duration := fs.Duration("duration", 0, "run wall-clock; 0 means until SIGINT")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s master [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Serves agent registration over HTTP or HTTPS (in-memory registry).\n\n")
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

	if (*tlsCert != "") != (*tlsKey != "") {
		fmt.Fprintf(os.Stderr, "master: -tls-cert and -tls-key must both be set or both empty\n")
		return 2
	}

	mem := master.NewMemory()
	srv := &http.Server{Handler: httpregister.Handler(mem)}

	var ln net.Listener
	var err error
	scheme := "http"
	if *tlsCert != "" {
		cert, errLoad := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if errLoad != nil {
			fmt.Fprintf(os.Stderr, "master: tls: %v\n", errLoad)
			return 1
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err = tls.Listen("tcp", *listen, tlsCfg)
		scheme = "https"
	} else {
		ln, err = net.Listen("tcp", *listen)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "master: listen: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "master: listening on %s://%s\n", scheme, ln.Addr().String())

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "master: serve: %v\n", err)
		}
	}()

	var (
		runCtx    context.Context
		runCancel context.CancelFunc
	)
	if *duration > 0 {
		runCtx, runCancel = context.WithTimeout(context.Background(), *duration)
	} else {
		runCtx, runCancel = signal.NotifyContext(context.Background(), os.Interrupt)
	}
	defer runCancel()

	<-runCtx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "master: shutdown: %v\n", err)
		return 1
	}
	return 0
}

func runServe(prog string, argv []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	listen := fs.String("listen", "127.0.0.1:8080", "HTTP listen address")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	duration := fs.Duration("duration", 0, "run wall-clock; 0 means until SIGINT")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s serve [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Serves the read-only data API (GET /v1/bars, GET /v1/runs, GET /v1/health, GET /v1/admin/status, GET /v1/admin/cluster, GET/PUT /v1/admin/config), the watchlist API (GET /v1/strategies, GET/PUT/DELETE /v1/watchlist), the backtest results API (GET /v1/backtests, GET /v1/backtests/{id}, GET /v1/backtests/{id}/export, POST /v1/backtests), the Futu proxies (GET /v1/futu/quote live quotes, GET /v1/futu/account funds/positions read-only with simulate env by default, GET /v1/futu/orders order list read-only, GET /v1/futu/options option chain; proto gateway via $FUTU_PROTO_ADDR, REST gateway via $FUTU_GATEWAY_URL), the account snapshot series (GET /v1/account/snapshots 资产曲线; DB-local) and the embedded Web UI (GET / redirects to /ui/).\n\n")
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

	startedAt := time.Now()

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "serve: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "serve: migrate: %v\n", err)
		return 1
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: listen: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "httpapi: listening on http://%s\n", ln.Addr().String())

	// meta must carry the bound address: with -listen 127.0.0.1:0 the port exists only after Listen.
	meta := httpapi.ProcessMeta{Version: version, StartedAt: startedAt, ListenAddr: ln.Addr().String()}
	store := httpapi.NewDBStore(database)
	top := serveMux(meta, httpapi.PingerFunc(database.PingContext), store, httpapi.NewDBWatchlistStore(database), httpapi.NewDBBacktestStore(database), httpapi.NewDBBacktestExecutor(database), httpapi.NewFutuQuoter(), httpapi.NewFutuAccounter(), httpapi.NewFutuOrderer(), httpapi.NewFutuOptionChainer(), httpapi.NewIngestRunner(database))
	srv := &http.Server{Handler: top}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "serve: serve: %v\n", err)
		}
	}()

	var (
		runCtx    context.Context
		runCancel context.CancelFunc
	)
	if *duration > 0 {
		runCtx, runCancel = context.WithTimeout(context.Background(), *duration)
	} else {
		runCtx, runCancel = signal.NotifyContext(context.Background(), os.Interrupt)
	}
	defer runCancel()

	<-runCtx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "serve: shutdown: %v\n", err)
		return 1
	}
	return 0
}

func runPaper(prog string, argv []string) int {
	fs := flag.NewFlagSet("paper", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	symbol := fs.String("symbol", "DEMO.US", "instrument symbol")
	side := fs.String("side", "buy", "buy or sell")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s paper [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "One-shot simulated submit via internal/paper (no network).\n\n")
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

	var s domain.Side
	switch *side {
	case "buy", "BUY", "b", "B":
		s = domain.SideBuy
	case "sell", "SELL", "s", "S":
		s = domain.SideSell
	default:
		fmt.Fprintf(os.Stderr, "paper: unknown side %q (want buy or sell)\n", *side)
		return 2
	}

	e := paper.NewEngine()
	got, err := e.Submit(domain.Order{Symbol: domain.Symbol(*symbol), Side: s})
	if err != nil {
		fmt.Fprintf(os.Stderr, "paper: %v\n", err)
		return 1
	}
	fmt.Printf("%s side=%s status=%d id=%s\n", got.Symbol, *side, got.Status, got.ID)
	return 0
}

func runBacktest(prog string, argv []string) int {
	fs := flag.NewFlagSet("backtest", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	file := fs.String("file", "", "path to JSON array of bars (same format as `ingest bars -json`; mutually exclusive with -dsn)")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN; mutually exclusive with -file)")
	symbol := fs.String("symbol", "DEMO.US", "instrument symbol")
	symbols := fs.String("symbols", "", "comma-separated symbols for a multi-symbol run (e.g. HK.00700,US.AAPL; 2+ symbols require -dsn input)")
	timeframe := fs.String("timeframe", "1d", "bar timeframe (e.g. 1d)")
	adjust := fs.String("adjust", "fwd", "adjustment for -dsn bars: fwd (前复权, default) or none (doc/DATA_STANDARD.md)")
	from := fs.String("from", "", "start of bar range, RFC3339 (e.g. 2024-06-01T00:00:00Z); empty = unbounded")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = unbounded")
	limit := fs.Int("limit", 10000, "maximum number of bars to load")
	cash := fs.Float64("cash", 10000, "initial cash")
	fee := fs.Float64("fee", 0, "per-trade fixed fee (placeholder)")
	strat := fs.String("strategy", "hold", "strategy to run: hold, buy-hold, covered-call or cash-secured-put")
	params := fs.String("params", "", `strategy params as JSON, e.g. {"strike_pct_otm":0.05}; validated against the template schema (option strategies only)`)
	maxDrawdown := fs.Float64("max-drawdown", 0, "max drawdown limit (0..1); exit 1 when exceeded; 0 = no check")
	save := fs.Bool("save", false, "persist this run into backtest_results (requires -dsn input)")
	exportID := fs.Int64("export", 0, "export a saved result to stdout instead of running (positive result id; requires -dsn input)")
	format := fs.String("format", "csv", "export format with -export: csv or json (same output as GET /v1/backtests/{id}/export)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s backtest [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Runs a strategy over bars from a JSON file (-file) or directly from the\n")
		fmt.Fprintf(os.Stderr, "database (-dsn, default $WBOT_PG_DSN) and prints one summary line.\n")
		fmt.Fprintf(os.Stderr, "Option strategies (covered-call, cash-secured-put) read contract prices and\n")
		fmt.Fprintf(os.Stderr, "chain metadata from option_quotes, so they require -dsn input.\n")
		fmt.Fprintf(os.Stderr, "A fixed per-trade fee (-fee, default 0) is deducted from cash on every buy/sell settle.\n")
		fmt.Fprintf(os.Stderr, "With -max-drawdown (0..1), exits 1 when the run's max drawdown exceeds the limit.\n")
		fmt.Fprintf(os.Stderr, "With -save, the run (params+metrics+equity_curve/trades trace) is stored in backtest_results (migrations 003/004).\n")
		fmt.Fprintf(os.Stderr, "With -export <id>, a saved result is written to stdout instead (csv by default, -format csv|json),\n")
		fmt.Fprintf(os.Stderr, "byte-identical to GET /v1/backtests/{id}/export (roundtrip contract, doc/API.md).\n")
		fmt.Fprintf(os.Stderr, "Exactly one of -file and -dsn must be set; -symbol/-symbols/-timeframe/-adjust/-from/-to/-limit apply to -dsn input.\n")
		fmt.Fprintf(os.Stderr, "With -symbols A,B,C (2+ symbols, -dsn input only), the initial cash is split equally into one\n")
		fmt.Fprintf(os.Stderr, "sub-account per symbol, run over the intersection of their bars; the summary is printed per\n")
		fmt.Fprintf(os.Stderr, "symbol plus the combined portfolio (minimal multi-symbol semantic, doc/BACKTEST.md).\n")
		fmt.Fprintf(os.Stderr, "Each JSON element: {\"ts\":\"RFC3339\",\"open\":...,\"high\":...,\"low\":...,\"close\":...,\"volume\":...}\n\n")
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

	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 2
	}
	if *maxDrawdown < 0 || *maxDrawdown > 1 {
		fmt.Fprintf(os.Stderr, "backtest: -max-drawdown must be in [0, 1]\n")
		return 2
	}

	fp := strings.TrimSpace(*file)
	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if fp == "" && d == "" {
		fmt.Fprintf(os.Stderr, "backtest: set -file or -dsn (or WBOT_PG_DSN)\n")
		return 2
	}
	if fp != "" && d != "" {
		fmt.Fprintf(os.Stderr, "backtest: -file and -dsn are mutually exclusive\n")
		return 2
	}

	if *exportID != 0 {
		if fp != "" {
			fmt.Fprintf(os.Stderr, "backtest: -export reads from the database; -file is mutually exclusive\n")
			return 2
		}
		if *exportID < 0 {
			fmt.Fprintf(os.Stderr, "backtest: -export must be a positive result id\n")
			return 2
		}
		return runBacktestExport(d, *exportID, strings.TrimSpace(*format))
	}

	symList, err := parseSymbolList(strings.TrimSpace(*symbols))
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 2
	}
	multi := len(symList) > 1
	if multi && fp != "" {
		fmt.Fprintf(os.Stderr, "backtest: -symbols: multi-symbol runs require -dsn input\n")
		return 2
	}

	stratName := strings.TrimSpace(*strat)
	paramsMap := map[string]any{}
	if ps := strings.TrimSpace(*params); ps != "" {
		if err := json.Unmarshal([]byte(ps), &paramsMap); err != nil {
			fmt.Fprintf(os.Stderr, "backtest: -params: %v\n", err)
			return 2
		}
	}
	// Build is the shared CLI/API validation contract (internal/backtestexec):
	// the API's POST /v1/backtests accepts exactly these strategies and params.
	s, templ, err := backtestexec.Build(stratName, paramsMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 2
	}
	if templ != nil && templ.NeedsOptions && fp != "" {
		fmt.Fprintf(os.Stderr, "backtest: strategy %s reads option_quotes; -file input has no option data (use -dsn)\n", stratName)
		return 2
	}

	if *save && fp != "" {
		fmt.Fprintf(os.Stderr, "backtest: -save requires -dsn input\n")
		return 2
	}
	if multi && templ != nil && templ.NeedsOptions {
		fmt.Fprintf(os.Stderr, "backtest: strategy %s reads option_quotes; not supported for multi-symbol runs (use hold or buy-hold)\n", stratName)
		return 2
	}
	if multi && *save {
		fmt.Fprintf(os.Stderr, "backtest: -save is not supported for multi-symbol runs\n")
		return 2
	}

	btSym := strings.TrimSpace(*symbol)
	if len(symList) == 1 {
		btSym = symList[0]
	}
	btOpts := backtestexec.Options{
		Symbol:    btSym,
		Strategy:  stratName,
		Params:    paramsMap,
		Timeframe: strings.TrimSpace(*timeframe),
		Adjust:    strings.TrimSpace(*adjust),
		From:      fromT,
		To:        toT,
		Limit:     *limit,
		Cash:      *cash,
		Fee:       *fee,
	}

	var (
		res     *backtest.Result
		startTs time.Time
		endTs   time.Time
	)
	var database *sql.DB
	if fp != "" {
		data, err := os.ReadFile(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: read %s: %v\n", fp, err)
			return 1
		}
		bars, err := backtest.ParseBars(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
		res, err = backtest.RunOptions(context.Background(), bars, *cash, *fee, s, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
		startTs, endTs = bars[0].Ts, bars[len(bars)-1].Ts
	} else if multi {
		// Multi-symbol: equal-weight sub-accounts over the intersection of the
		// symbols' bars (shared runner in internal/backtestexec, doc/BACKTEST.md).
		database, err = db.Open(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: open db: %v\n", err)
			return 1
		}
		defer database.Close()

		if err := db.MigrateUp(database); err != nil {
			fmt.Fprintf(os.Stderr, "backtest: migrate: %v\n", err)
			return 1
		}

		mout, err := backtestexec.RunMulti(context.Background(), database, btOpts, symList)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
		mr := mout.Result
		fmt.Printf("final_equity=%v total_return=%v max_drawdown=%v bars=%d symbols=%d\n",
			mr.Equity, mr.TotalReturn, mr.MaxDrawdown, mr.Bars, len(mr.PerSymbol))
		for _, sub := range mr.PerSymbol {
			r := sub.Result
			fmt.Printf("  %s: final_equity=%v total_return=%v max_drawdown=%v bars=%d\n",
				sub.Symbol, r.Equity, r.TotalReturn, r.MaxDrawdown, r.Bars)
		}
		if *maxDrawdown > 0 && mr.MaxDrawdown > *maxDrawdown {
			fmt.Fprintf(os.Stderr, "backtest: max drawdown %v exceeds limit %v\n", mr.MaxDrawdown, *maxDrawdown)
			return 1
		}
		return 0
	} else {
		var err error
		database, err = db.Open(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: open db: %v\n", err)
			return 1
		}
		defer database.Close()

		if err := db.MigrateUp(database); err != nil {
			fmt.Fprintf(os.Stderr, "backtest: migrate: %v\n", err)
			return 1
		}

		// The -dsn run path is the API's execution path (POST /v1/backtests):
		// same runner, same persisted params, same output (doc/API.md).
		outcome, err := backtestexec.Run(context.Background(), database, btOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
		res, startTs, endTs = outcome.Result, outcome.StartTs, outcome.EndTs
	}

	fmt.Printf("final_equity=%v total_return=%v max_drawdown=%v bars=%d\n", res.Equity, res.TotalReturn, res.MaxDrawdown, res.Bars)
	if *save {
		id, err := backtest.SaveResult(context.Background(), database,
			stratName, strings.TrimSpace(*symbol),
			backtestexec.SaveParams(btOpts), res, startTs, endTs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
		fmt.Printf("saved result id=%d\n", id)
	}
	if *maxDrawdown > 0 {
		if err := backtest.CheckMaxDrawdown(res, *maxDrawdown); err != nil {
			fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
			return 1
		}
	}
	return 0
}

// runBacktestExport writes one saved result to stdout in csv or json, using
// the same serializer as GET /v1/backtests/{id}/export (roundtrip: identical
// output on CLI and API; exit 1 when the id has no row).
func runBacktestExport(dsn string, id int64, format string) int {
	if format != "csv" && format != "json" {
		fmt.Fprintf(os.Stderr, "backtest: -format %q (want csv or json)\n", format)
		return 2
	}
	database, err := db.Open(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: open db: %v\n", err)
		return 1
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: migrate: %v\n", err)
		return 1
	}
	rec, err := backtest.LoadResult(context.Background(), database, id)
	if errors.Is(err, backtest.ErrResultNotFound) {
		fmt.Fprintf(os.Stderr, "backtest: result %d not found; run `wbot backtest -save` to persist a run first\n", id)
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: load result %d: %v\n", id, err)
		return 1
	}
	payload, _, err := backtest.Export(*rec, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		return 1
	}
	if _, err := os.Stdout.Write(payload); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: write: %v\n", err)
		return 1
	}
	return 0
}

func runWatchlist(prog string, argv []string) int {
	if len(argv) < 1 {
		usageWatchlist(prog)
		return 2
	}
	switch argv[0] {
	case "-h", "-help", "--help", "help":
		usageWatchlist(prog)
		return 0
	case "add":
		return runWatchlistAdd(prog, argv[1:])
	case "remove":
		return runWatchlistRemove(prog, argv[1:])
	case "list":
		return runWatchlistList(prog, argv[1:])
	default:
		usageWatchlist(prog)
		return 2
	}
}

func runWatchlistAdd(prog string, argv []string) int {
	fs := flag.NewFlagSet("watchlist add", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	symbol := fs.String("symbol", "", "instrument symbol (required, e.g. HK.00700)")
	strategy := fs.String("strategy", "", "strategy template name (required, e.g. covered-call)")
	params := fs.String("params", "", "strategy params as JSON object, e.g. '{\"strike_pct_otm\":0.03}'")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s watchlist add [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Adds or updates one watchlist entry: symbol + strategy + params,\n")
		fmt.Fprintf(os.Stderr, "validated against the strategy template schema (watchlist strategies via GET /v1/strategies).\n\n")
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
	strat := strings.TrimSpace(*strategy)
	if sym == "" {
		fmt.Fprintf(os.Stderr, "watchlist add: -symbol is required\n")
		return 2
	}
	if strat == "" {
		fmt.Fprintf(os.Stderr, "watchlist add: -strategy is required\n")
		return 2
	}
	paramsMap := map[string]any{}
	if s := strings.TrimSpace(*params); s != "" {
		if err := json.Unmarshal([]byte(s), &paramsMap); err != nil {
			fmt.Fprintf(os.Stderr, "watchlist add: -params: %v\n", err)
			return 2
		}
	}
	if err := watchlist.Validate(strat, paramsMap); err != nil {
		fmt.Fprintf(os.Stderr, "watchlist add: %v\n", err)
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "watchlist add: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist add: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "watchlist add: migrate: %v\n", err)
		return 1
	}

	it, err := watchlist.Upsert(context.Background(), database, sym, strat, paramsMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist add: %v\n", err)
		return 1
	}
	paramsJSON, err := json.Marshal(it.Params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist add: params: %v\n", err)
		return 1
	}
	fmt.Printf("watchlist: %s strategy=%s params=%s\n", it.Symbol, it.Strategy, paramsJSON)
	return 0
}

func runWatchlistRemove(prog string, argv []string) int {
	fs := flag.NewFlagSet("watchlist remove", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	symbol := fs.String("symbol", "", "instrument symbol (required, e.g. HK.00700)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s watchlist remove [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Removes one symbol from the watchlist (exits 1 when it is not on the list).\n\n")
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
		fmt.Fprintf(os.Stderr, "watchlist remove: -symbol is required\n")
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "watchlist remove: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist remove: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "watchlist remove: migrate: %v\n", err)
		return 1
	}

	found, err := watchlist.Delete(context.Background(), database, sym)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist remove: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(os.Stderr, "watchlist remove: %s: not on watchlist\n", sym)
		return 1
	}
	fmt.Printf("watchlist: removed %s\n", sym)
	return 0
}

func runWatchlistList(prog string, argv []string) int {
	fs := flag.NewFlagSet("watchlist list", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s watchlist list [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Shows watchlist entries (symbol strategy params), one per line.\n\n")
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

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "watchlist list: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist list: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "watchlist list: migrate: %v\n", err)
		return 1
	}

	items, err := watchlist.List(context.Background(), database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchlist list: %v\n", err)
		return 1
	}
	for _, it := range items {
		paramsJSON, err := json.Marshal(it.Params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "watchlist list: params: %v\n", err)
			return 1
		}
		fmt.Printf("%s %s %s\n", it.Symbol, it.Strategy, paramsJSON)
	}
	return 0
}

func usageWatchlist(prog string) {
	fmt.Fprintf(os.Stderr, "Usage: %s watchlist <subcommand>\n\n", prog)
	fmt.Fprintf(os.Stderr, "Subcommands:\n  add    Add or update a symbol's strategy binding (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  remove Remove a symbol from the watchlist (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  list   Show watchlist entries (symbol strategy params) (-h for flags)\n")
}

func runConfigYAML(prog string, argv []string) int {
	fs := flag.NewFlagSet("configyaml", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	path := fs.String("file", "", "path to config.yaml (default: ~/.wbot/config.yaml)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s configyaml [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Renders ~/.wbot/config.yaml to KEY=VALUE dotenv lines for docker compose --env-file or shell source (see doc/FUTU.md).\n")
		fmt.Fprintf(os.Stderr, "Nested YAML keys flatten to UPPER_SNAKE; ${VAR} and ${VAR:-default} expand from the environment (undefined -> error).\n\n")
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

	p := strings.TrimSpace(*path)
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "configyaml: home dir: %v\n", err)
			return 1
		}
		p = filepath.Join(home, ".wbot", "config.yaml")
	}
	env, err := configyaml.Load(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configyaml: %v\n", err)
		return 1
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val, err := dotenvValue(env[k])
		if err != nil {
			fmt.Fprintf(os.Stderr, "configyaml: %s: %v\n", k, err)
			return 1
		}
		fmt.Printf("%s=%s\n", k, val)
	}
	return 0
}

// dotenvValue quotes a value so the line parses both as docker compose --env-file
// (plain KEY=VALUE, no export) and as shell source; single quotes are rejected.
func dotenvValue(s string) (string, error) {
	if s == "" || !strings.ContainsAny(s, " \t\r\n'\"#") {
		return s, nil
	}
	if strings.ContainsRune(s, '\'') {
		return "", fmt.Errorf("value contains a single quote (unsupported in dotenv)")
	}
	return "'" + s + "'", nil
}

func runIngest(prog string, argv []string) int {
	if len(argv) < 1 {
		usageIngest(prog)
		return 2
	}
	switch argv[0] {
	case "-h", "-help", "--help", "help":
		usageIngest(prog)
		return 0
	case "mock":
		return runIngestMock(prog, argv[1:])
	case "file":
		return runIngestFile(prog, argv[1:])
	case "url":
		return runIngestURL(prog, argv[1:])
	case "futu":
		return runIngestFutu(prog, argv[1:])
	case "futu-option":
		return runIngestFutuOption(prog, argv[1:])
	case "account":
		return runIngestAccount(prog, argv[1:])
	case "status":
		return runIngestStatus(prog, argv[1:])
	case "freshness":
		return runIngestFreshness(prog, argv[1:])
	case "bars":
		return runIngestBars(prog, argv[1:])
	default:
		usageIngest(prog)
		return 2
	}
}

func runIngestMock(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest mock", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	source := fs.String("source", "cli-mock", "ingestion source label")
	symbol := fs.String("symbol", "DEMO.US", "instrument symbol")
	timeframe := fs.String("timeframe", "1d", "bar timeframe (e.g. 1d)")
	from := fs.String("from", "", "start of bar range, RFC3339 (e.g. 2024-06-01T00:00:00Z); empty = unbounded")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = unbounded")
	every := fs.Duration("every", 0, "if >0, repeat ingestion at this interval until SIGINT")
	provider := fs.String("provider", "mock", "ingest provider name")
	adjust := fs.String("adjust", "none", "adjustment: none|fwd|back (doc/DATA_STANDARD.md)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest mock [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Runs a sample ingestion (mock bars) into PostgreSQL.\n")
		fmt.Fprintf(os.Stderr, "With -every, repeats at that interval (duplicate bars are skipped via ON CONFLICT).\n")
		fmt.Fprintf(os.Stderr, "With -from/-to, the range is validated before ingestion (the mock feed is fixed\n")
		fmt.Fprintf(os.Stderr, "demo data and keeps all bars; empty = unbounded).\n")
		fmt.Fprintf(os.Stderr, "With -adjust fwd, seeds 前复权 bars so the 回测页 can run (回测默认 fwd).\n\n")
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

	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest mock: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest mock: %v\n", err)
		return 2
	}

	src, err := ingest.NewProvider(ingestProviderName(*provider, "mock"), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest mock: %v\n", err)
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest mock: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest mock: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest mock: migrate: %v\n", err)
		return 1
	}

	sym := domain.Symbol(*symbol)
	ctx, cancel := ingestRepeatCtx(*every)
	defer cancel()
	_, adjustName, err := futu.ParseAdjust(*adjust)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest mock: %v\n", err)
		return 2
	}
	err = ingest.RunEveryResilient(ctx, *every, func(ctx context.Context) error {
		if err := ingest.RunIngestion(ctx, database, strings.TrimSpace(*source), sym, strings.TrimSpace(*timeframe), adjustName, "mock", fromT, toT, src); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ingest mock: ok (source=%s symbol=%s timeframe=%s adjust=%s)\n", strings.TrimSpace(*source), sym, strings.TrimSpace(*timeframe), adjustName)
		return nil
	}, func(err error) {
		fmt.Fprintf(os.Stderr, "ingest mock: %v\n", err)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ingest mock: %v\n", err)
		return 1
	}
	return 0
}

func runIngestFile(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest file", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	path := fs.String("file", "", "path to JSON array of bars (required; see -h)")
	source := fs.String("source", "cli-file", "ingestion source label")
	symbol := fs.String("symbol", "DEMO.US", "instrument symbol")
	timeframe := fs.String("timeframe", "1d", "bar timeframe (e.g. 1d)")
	from := fs.String("from", "", "start of bar range, RFC3339 (e.g. 2024-06-01T00:00:00Z); empty = unbounded")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = unbounded")
	every := fs.Duration("every", 0, "if >0, repeat ingestion at this interval until SIGINT")
	provider := fs.String("provider", "file", "ingest provider name")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest file [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Loads OHLCV bars from a JSON file and writes one ingestion run.\n")
		fmt.Fprintf(os.Stderr, "With -every, repeats at that interval (duplicate bars are skipped via ON CONFLICT).\n")
		fmt.Fprintf(os.Stderr, "Each element: {\"ts\":\"RFC3339\",\"open\":...,\"high\":...,\"low\":...,\"close\":...,\"volume\":...}\n")
		fmt.Fprintf(os.Stderr, "With -from/-to, only bars inside the closed range [from, to] are kept (RFC3339; empty = unbounded).\n\n")
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

	fp := strings.TrimSpace(*path)
	if fp == "" {
		fmt.Fprintf(os.Stderr, "ingest file: -file is required\n")
		return 2
	}

	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest file: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest file: %v\n", err)
		return 2
	}

	src, err := ingest.NewProvider(ingestProviderName(*provider, "file"), ingest.Config{"path": fp})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest file: %v\n", err)
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest file: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest file: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest file: migrate: %v\n", err)
		return 1
	}

	sym := domain.Symbol(*symbol)
	ctx, cancel := ingestRepeatCtx(*every)
	defer cancel()
	err = ingest.RunEveryResilient(ctx, *every, func(ctx context.Context) error {
		if err := ingest.RunIngestion(ctx, database, strings.TrimSpace(*source), sym, strings.TrimSpace(*timeframe), "none", "file", fromT, toT, src); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ingest file: ok (source=%s symbol=%s timeframe=%s file=%s)\n",
			strings.TrimSpace(*source), sym, strings.TrimSpace(*timeframe), fp)
		return nil
	}, func(err error) {
		fmt.Fprintf(os.Stderr, "ingest file: %v\n", err)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ingest file: %v\n", err)
		return 1
	}
	return 0
}

func runIngestURL(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest url", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	url := fs.String("url", "", "URL of JSON array of bars (required; see -h)")
	source := fs.String("source", "cli-url", "ingestion source label")
	symbol := fs.String("symbol", "DEMO.US", "instrument symbol")
	timeframe := fs.String("timeframe", "1d", "bar timeframe (e.g. 1d)")
	from := fs.String("from", "", "start of bar range, RFC3339 (e.g. 2024-06-01T00:00:00Z); empty = unbounded")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = unbounded")
	every := fs.Duration("every", 0, "if >0, repeat ingestion at this interval until SIGINT")
	provider := fs.String("provider", "url", "ingest provider name")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest url [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Loads OHLCV bars from a JSON URL and writes one ingestion run.\n")
		fmt.Fprintf(os.Stderr, "With -every, repeats at that interval (duplicate bars are skipped via ON CONFLICT).\n")
		fmt.Fprintf(os.Stderr, "Each element: {\"ts\":\"RFC3339\",\"open\":...,\"high\":...,\"low\":...,\"close\":...,\"volume\":...}\n")
		fmt.Fprintf(os.Stderr, "With -from/-to, only bars inside the closed range [from, to] are kept (RFC3339; empty = unbounded).\n\n")
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

	u := strings.TrimSpace(*url)
	if u == "" {
		fmt.Fprintf(os.Stderr, "ingest url: -url is required\n")
		return 2
	}

	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest url: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest url: %v\n", err)
		return 2
	}

	src, err := ingest.NewProvider(ingestProviderName(*provider, "url"), ingest.Config{"url": u})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest url: %v\n", err)
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest url: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest url: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest url: migrate: %v\n", err)
		return 1
	}

	sym := domain.Symbol(*symbol)
	ctx, cancel := ingestRepeatCtx(*every)
	defer cancel()
	err = ingest.RunEveryResilient(ctx, *every, func(ctx context.Context) error {
		if err := ingest.RunIngestion(ctx, database, strings.TrimSpace(*source), sym, strings.TrimSpace(*timeframe), "none", "url", fromT, toT, src); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ingest url: ok (source=%s symbol=%s timeframe=%s url=%s)\n",
			strings.TrimSpace(*source), sym, strings.TrimSpace(*timeframe), u)
		return nil
	}, func(err error) {
		fmt.Fprintf(os.Stderr, "ingest url: %v\n", err)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ingest url: %v\n", err)
		return 1
	}
	return 0
}

func runIngestStatus(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest status", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	limit := fs.Int("limit", 10, "number of most recent runs to show")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest status [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Lists the most recent ingestion runs (read-only).\n\n")
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

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest status: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest status: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest status: migrate: %v\n", err)
		return 1
	}

	runs, err := ingest.RecentRuns(context.Background(), database, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest status: %v\n", err)
		return 1
	}
	for _, r := range runs {
		finished := "-"
		if r.FinishedAt != nil {
			finished = r.FinishedAt.Format(time.RFC3339)
		}
		fmt.Printf("%d %s %s %s %s\n", r.ID, r.Source, r.Status, r.StartedAt.Format(time.RFC3339), finished)
	}
	return 0
}

// runIngestFreshness checks data freshness per symbol×timeframe and exits 1
// when any combination is stale — the cron gate `wbot ingest freshness || alert`
// (doc/DATA_PIPELINE.md); unknown (no data) prints and exits 0.
func runIngestFreshness(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest freshness", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	maxAge := fs.Duration("max-age", 0, "global staleness threshold (e.g. 24h); 0 = per-timeframe defaults (1d → 3d, 1m → 10m, unknown → 24h)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest freshness [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Checks data freshness per symbol×timeframe (bars) plus per underlying\n")
		fmt.Fprintf(os.Stderr, "(option quotes): prints each combination's max_ts, age and status\n")
		fmt.Fprintf(os.Stderr, "(fresh / stale / unknown); exits 1 when any combination is stale,\n")
		fmt.Fprintf(os.Stderr, "0 otherwise (no data → unknown, exit 0).\n\n")
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

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest freshness: set -dsn or WBOT_PG_DSN\n")
		return 2
	}
	if *maxAge < 0 {
		fmt.Fprintf(os.Stderr, "ingest freshness: -max-age must not be negative\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest freshness: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest freshness: migrate: %v\n", err)
		return 1
	}

	now := time.Now()
	entries, err := ingest.QueryFreshness(context.Background(), database, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest freshness: %v\n", err)
		return 1
	}
	anyStale := false
	for _, e := range entries {
		threshold := ingest.MaxAgeForTimeframe(e.Timeframe)
		if *maxAge > 0 {
			threshold = *maxAge
		}
		status := ingest.JudgeFreshness(e.MaxTs, now, threshold)
		if status == ingest.Stale {
			anyStale = true
		}
		maxTs := "-"
		if !e.MaxTs.IsZero() {
			maxTs = e.MaxTs.Format(time.RFC3339)
		}
		fmt.Printf("%s %s %s %ds %s\n", e.Symbol, e.Timeframe, maxTs, e.AgeSeconds, status)
	}
	if len(entries) == 0 {
		fmt.Println("unknown: no bars data")
	}
	// 期权数据并入同一判定(草稿非目标项):按 underlying×source 聚合,
	// 阈值 MaxAgeForOptions(4h),-max-age 全局覆盖;stale 同样使 exit 1。
	opts, err := ingest.QueryOptionFreshness(context.Background(), database, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest freshness: %v\n", err)
		return 1
	}
	for _, o := range opts {
		threshold := ingest.MaxAgeForOptions
		if *maxAge > 0 {
			threshold = *maxAge
		}
		status := ingest.JudgeFreshness(o.MaxTs, now, threshold)
		if status == ingest.Stale {
			anyStale = true
		}
		fmt.Printf("%s option %s %ds %s\n", o.Underlying, o.MaxTs.Format(time.RFC3339), o.AgeSeconds, status)
	}
	if len(entries) == 0 && len(opts) == 0 {
		fmt.Println("unknown: no bars or option data")
	}
	if anyStale {
		fmt.Fprintf(os.Stderr, "ingest freshness: stale data found\n")
		return 1
	}
	return 0
}

func runIngestBars(prog string, argv []string) int {
	fs := flag.NewFlagSet("ingest bars", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	dsn := fs.String("dsn", "", "PostgreSQL DSN (default: $WBOT_PG_DSN)")
	symbol := fs.String("symbol", "DEMO.US", "instrument symbol")
	timeframe := fs.String("timeframe", "1d", "bar timeframe (e.g. 1d)")
	adjust := fs.String("adjust", "fwd", "adjustment: fwd (前复权, default) or none (doc/DATA_STANDARD.md)")
	from := fs.String("from", "", "start of bar range, RFC3339 (e.g. 2024-06-01T00:00:00Z); empty = unbounded")
	to := fs.String("to", "", "end of bar range, RFC3339; empty = unbounded")
	limit := fs.Int("limit", 100, "maximum number of bars to show")
	jsonOut := fs.Bool("json", false, "print bars as a JSON array (same format as `ingest file`)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s ingest bars [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Shows ingested OHLCV bars for a symbol/timeframe/adjust (read-only).\n")
		fmt.Fprintf(os.Stderr, "With -json, output can round-trip into `ingest file -file` for diffing.\n\n")
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

	fromT, err := parseRangeTime("-from", strings.TrimSpace(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest bars: %v\n", err)
		return 2
	}
	toT, err := parseRangeTime("-to", strings.TrimSpace(*to))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest bars: %v\n", err)
		return 2
	}

	d := strings.TrimSpace(*dsn)
	if d == "" {
		d = strings.TrimSpace(os.Getenv("WBOT_PG_DSN"))
	}
	if d == "" {
		fmt.Fprintf(os.Stderr, "ingest bars: set -dsn or WBOT_PG_DSN\n")
		return 2
	}

	database, err := db.Open(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest bars: open db: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := db.MigrateUp(database); err != nil {
		fmt.Fprintf(os.Stderr, "ingest bars: migrate: %v\n", err)
		return 1
	}

	bars, err := ingest.QueryBars(context.Background(), database, strings.TrimSpace(*symbol), strings.TrimSpace(*timeframe), strings.TrimSpace(*adjust), fromT, toT, *limit, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest bars: %v\n", err)
		return 1
	}
	if *jsonOut {
		type barJSON struct {
			Ts     string  `json:"ts"`
			Open   float64 `json:"open"`
			High   float64 `json:"high"`
			Low    float64 `json:"low"`
			Close  float64 `json:"close"`
			Volume int64   `json:"volume"`
		}
		out := make([]barJSON, 0, len(bars))
		for _, b := range bars {
			out = append(out, barJSON{b.Ts.Format(time.RFC3339), b.Open, b.High, b.Low, b.Close, b.Volume})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "ingest bars: json: %v\n", err)
			return 1
		}
		return 0
	}
	for _, b := range bars {
		fmt.Printf("%s %v %v %v %v %d\n", b.Ts.Format(time.RFC3339), b.Open, b.High, b.Low, b.Close, b.Volume)
	}
	return 0
}

// serveMux assembles serve's top-level routes: admin API, watchlist API,
// backtest results API (read + execute), Futu quote/account/options proxies,
// data API, embedded Web UI.
func serveMux(meta httpapi.ProcessMeta, pinger httpapi.Pinger, store httpapi.Store, wstore httpapi.WatchlistStore, bstore httpapi.BacktestStore, bexec httpapi.BacktestExecutor, fquoter httpapi.FutuQuoter, facc httpapi.FutuAccounter, forder httpapi.FutuOrderer, fchain httpapi.FutuOptionChainer, irunner httpapi.IngestRunner) *http.ServeMux {
	top := http.NewServeMux()
	top.Handle("/v1/admin/", httpapi.AdminHandler(meta, pinger))
	// Exact pattern wins over the /v1/admin/ subtree, so cluster/config keep their own handler muxes.
	top.Handle("/v1/admin/cluster", httpapi.ClusterHandler(meta, store))
	if cstore, err := config.OpenDefault(); err != nil {
		fmt.Fprintf(os.Stderr, "httpapi: admin: config: %v\n", err)
	} else {
		cfg := httpapi.ConfigHandler(cstore)
		top.Handle("/v1/admin/config", cfg)
		top.Handle("/v1/admin/config/", cfg)
	}
	// Watchlist endpoints: template catalog + per-symbol strategy bindings (one handler mux).
	wl := httpapi.WatchlistHandler(wstore)
	top.Handle("/v1/strategies", wl)
	top.Handle("/v1/watchlist", wl)
	top.Handle("/v1/watchlist/", wl)
	// Backtest results: saved runs list + detail with equity/trades + csv/json
	// export (one handler mux); the method-specific pattern wins for POST, so
	// the execute endpoint runs one backtest synchronously (manual body or
	// from_watchlist batch).
	bt := httpapi.BacktestsHandler(bstore)
	top.Handle("/v1/backtests", bt)
	top.Handle("/v1/backtests/", bt)
	top.Handle("POST /v1/backtests", httpapi.BacktestExecuteHandler(bexec, wstore))
	// Futu proxies: browsers cannot reach the gateway (loopback) directly.
	top.Handle("/v1/futu/quote", httpapi.FutuQuoteHandler(fquoter))
	top.Handle("/v1/futu/account", httpapi.FutuAccountHandler(facc))
	top.Handle("/v1/futu/orders", httpapi.FutuOrdersHandler(forder))
	top.Handle("/v1/futu/options", httpapi.FutuOptionsHandler(fchain))
	// DB-backed account snapshot series (资产曲线; written by `wbot ingest account`).
	top.Handle("/v1/account/snapshots", httpapi.AccountSnapshotsHandler(store))
	// One-shot bar ingestion (Data 页「补数据」; same pipeline as `wbot ingest futu`).
	top.Handle("/v1/ingest", httpapi.IngestHandler(irunner))
	// Longest pattern wins: GET /{$} beats the / catch-all; other methods still reach the API's JSON 404.
	top.Handle("GET /{$}", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	top.Handle("/ui/", http.StripPrefix("/ui/", webui.FileServer()))
	top.Handle("/", httpapi.Handler(store))
	return top
}

// parseSymbolList splits a -symbols value on commas into trimmed symbols;
// empty entries and duplicates are rejected. Empty value yields nil
// (single-symbol mode).
func parseSymbolList(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		sym := strings.TrimSpace(p)
		if sym == "" {
			return nil, errors.New("-symbols: empty symbol entry")
		}
		if seen[sym] {
			return nil, fmt.Errorf("-symbols: duplicate symbol %s", sym)
		}
		seen[sym] = true
		out = append(out, sym)
	}
	return out, nil
}

// parseRangeTime parses a -from/-to flag value. Empty means unbounded (zero time).
func parseRangeTime(flagName, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: invalid RFC3339 time %q", flagName, s)
	}
	return t, nil
}

func ingestRepeatCtx(every time.Duration) (context.Context, context.CancelFunc) {
	if every <= 0 {
		return context.Background(), func() {}
	}
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// ingestProviderName returns the -provider flag value, falling back to the
// subcommand's default when empty (default inferred from the subcommand).
func ingestProviderName(flagVal, def string) string {
	if s := strings.TrimSpace(flagVal); s != "" {
		return s
	}
	return def
}

func usageIngest(prog string) {
	fmt.Fprintf(os.Stderr, "Usage: %s ingest <subcommand>\n\n", prog)
	fmt.Fprintf(os.Stderr, "Subcommands:\n  mock   Insert a mock ingestion run and sample OHLCV bars (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  file   Load bars from a JSON file (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  url    Load bars from a JSON URL (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  futu   Fetch K-lines from the futu-opend-rs gateway (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  futu-option  Fetch option-chain K-lines + underlying bars, cache-first (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  account  Snapshot account funds into account_snapshots (资产曲线数据层) (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  status Show recent ingestion runs (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  freshness  Check data freshness; exit 1 when any symbol×timeframe is stale (-h for flags)\n")
	fmt.Fprintf(os.Stderr, "  bars   Show ingested bars for a symbol/timeframe (-h for flags)\n")
}

func usage(argv []string) {
	prog := "wbot"
	if len(argv) > 0 && argv[0] != "" {
		prog = argv[0]
	}
	fmt.Fprintf(os.Stdout, "wbot - trading bot (v1 slice)\n\n")
	fmt.Fprintf(os.Stdout, "Usage:\n  %s <command|flag>\n\n", prog)
	fmt.Fprintf(os.Stdout, "Flags:\n  -h, -help, --help    Show help\n  -version, --version Print version\n\n")
	fmt.Fprintf(os.Stdout, "Commands:\n  help, version       Same as flags above\n  agent               poll.Run heartbeat (in-memory or -master-url; try -h)\n  master              HTTP registration server (try -h)\n  paper               One-shot paper.Engine submit (try -h)\n  ingest              Data ingestion (try ingest -h)\n  futu                futu-opend-rs gateway client: status/quote/funds/position/order (try futu -h)\n  backtest            Strategy backtest over a JSON bars file (try -h)\n  watchlist           Watchlist management: add/remove/list (try watchlist -h)\n  configyaml          Render ~/.wbot/config.yaml to dotenv lines (try -h)\n  serve               Read-only HTTP data API (try -h)\n")
}

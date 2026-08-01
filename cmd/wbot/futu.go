package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"

	"github.com/jiayu/wbot/internal/futu"
)

func runFutu(prog string, argv []string) int {
	if len(argv) < 1 {
		usageFutu(prog)
		return 2
	}
	switch argv[0] {
	case "-h", "-help", "--help", "help":
		usageFutu(prog)
		return 0
	case "status":
		return runFutuStatus(prog, argv[1:])
	case "quote":
		return runFutuQuote(prog, argv[1:])
	case "funds":
		return runFutuFunds(prog, argv[1:])
	case "position":
		return runFutuPosition(prog, argv[1:])
	case "order":
		return runFutuOrder(prog, argv[1:])
	default:
		usageFutu(prog)
		return 2
	}
}

// parseFutuEnv maps the -env flag to a trade environment (default simulate).
func parseFutuEnv(s string) (futu.Env, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "sim", "simulate", "paper":
		return futu.EnvSim, nil
	case "real":
		return futu.EnvReal, nil
	}
	return 0, fmt.Errorf("bad -env %q (want sim or real)", s)
}

// warnRed prints msg to stderr, in ANSI red when stderr is a terminal.
func warnRed(msg string) {
	if st, err := os.Stderr.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		msg = "\x1b[31m" + msg + "\x1b[0m"
	}
	fmt.Fprintln(os.Stderr, msg)
}

// openTradeClient connects a protobuf trade client and reports errors as the
// CLI convention requires (exit 1 runtime errors).
func openTradeClient(prog, sub, addr string) (*futu.TradeClient, bool) {
	tc, err := futu.OpenTrade(context.Background(), addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: %s: %v\n", sub, err)
		return nil, false
	}
	return tc, true
}

// resolveAccount looks up the account for env and reports CLI errors.
func resolveAccount(prog, sub string, tc *futu.TradeClient, env futu.Env, accID uint64) (*trdcommon.TrdAcc, bool) {
	acc, err := tc.Account(context.Background(), env, accID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: %s: %v\n", sub, err)
		return nil, false
	}
	return acc, true
}

func runFutuFunds(prog string, argv []string) int {
	fs := flag.NewFlagSet("futu funds", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultProtoAddr, "gateway OpenD protobuf address (TCP 11111)")
	env := fs.String("env", "sim", "trading environment: sim (paper, default) or real (read-only)")
	accID := fs.Uint64("acc-id", 0, "account id (default: first account of -env)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s futu funds [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Queries account funds over the protobuf API (TCP 11111).\nBoth envs are read-only and safe.\n\n")
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
		fmt.Fprintf(os.Stderr, "futu: funds: %v\n", err)
		return 2
	}
	tc, ok := openTradeClient(prog, "funds", *addr)
	if !ok {
		return 1
	}
	defer tc.Close()
	acc, ok := resolveAccount(prog, "funds", tc, e, *accID)
	if !ok {
		return 1
	}
	funds, err := tc.Funds(context.Background(), acc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: funds: %v\n", err)
		return 1
	}
	out := struct {
		AccID       uint64  `json:"acc_id"`
		Env         string  `json:"env"`
		TotalAssets float64 `json:"total_assets"`
		Cash        float64 `json:"cash"`
		MarketVal   float64 `json:"market_val"`
		FrozenCash  float64 `json:"frozen_cash"`
		Power       float64 `json:"power"`
	}{
		AccID:       acc.GetAccID(),
		Env:         futu.EnvName(e),
		TotalAssets: funds.GetTotalAssets(),
		Cash:        funds.GetCash(),
		MarketVal:   funds.GetMarketVal(),
		FrozenCash:  funds.GetFrozenCash(),
		Power:       funds.GetPower(),
	}
	printJSON(out)
	return 0
}

func runFutuPosition(prog string, argv []string) int {
	fs := flag.NewFlagSet("futu position", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultProtoAddr, "gateway OpenD protobuf address (TCP 11111)")
	env := fs.String("env", "sim", "trading environment: sim (paper, default) or real (read-only)")
	accID := fs.Uint64("acc-id", 0, "account id (default: first account of -env)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s futu position [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Queries account positions over the protobuf API (TCP 11111).\nBoth envs are read-only and safe.\n\n")
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
		fmt.Fprintf(os.Stderr, "futu: position: %v\n", err)
		return 2
	}
	tc, ok := openTradeClient(prog, "position", *addr)
	if !ok {
		return 1
	}
	defer tc.Close()
	acc, ok := resolveAccount(prog, "position", tc, e, *accID)
	if !ok {
		return 1
	}
	positions, err := tc.Positions(context.Background(), acc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: position: %v\n", err)
		return 1
	}
	type pos struct {
		Code      string  `json:"code"`
		Name      string  `json:"name"`
		Qty       float64 `json:"qty"`
		CanSell   float64 `json:"can_sell_qty"`
		Price     float64 `json:"price"`
		CostPrice float64 `json:"cost_price"`
		Val       float64 `json:"val"`
		PlVal     float64 `json:"pl_val"`
		PlRatio   float64 `json:"pl_ratio"`
	}
	out := struct {
		AccID     uint64 `json:"acc_id"`
		Env       string `json:"env"`
		Positions []pos  `json:"positions"`
	}{AccID: acc.GetAccID(), Env: futu.EnvName(e)}
	for _, p := range positions {
		out.Positions = append(out.Positions, pos{
			Code: p.GetCode(), Name: p.GetName(), Qty: p.GetQty(), CanSell: p.GetCanSellQty(),
			Price: p.GetPrice(), CostPrice: p.GetCostPrice(), Val: p.GetVal(),
			PlVal: p.GetPlVal(), PlRatio: p.GetPlRatio(),
		})
	}
	printJSON(out)
	return 0
}

// runFutuOrder places an order with the paper-first safety guard: default env
// is simulate; real env requires -live-confirm AND -acc-id (实盘写需老板确认,
// 见 doc/FUTU.md 交易安全策略).
func runFutuOrder(prog string, argv []string) int {
	fs := flag.NewFlagSet("futu order", flag.ContinueOnError)
	var showHelp, liveConfirm, dryRun bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	fs.BoolVar(&liveConfirm, "live-confirm", false, "explicit confirmation for real-env write (安全红线: 老板确认)")
	fs.BoolVar(&dryRun, "dry-run", false, "validate and print the order plan without sending")
	addr := fs.String("addr", futu.DefaultProtoAddr, "gateway OpenD protobuf address (TCP 11111)")
	env := fs.String("env", "sim", "trading environment: sim (paper, default) or real (requires -live-confirm)")
	accID := fs.Uint64("acc-id", 0, "account id (required for -env real)")
	symbol := fs.String("symbol", "", "market-qualified symbol (e.g. HK.00700)")
	side := fs.String("side", "", "buy or sell")
	qty := fs.Float64("qty", 0, "quantity in shares")
	price := fs.Float64("price", 0, "limit price (0 = market order)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s futu order -symbol HK.00700 -side buy -qty 100 [-price 470] [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Places an order over the protobuf API (TCP 11111). Default env is\nsimulate (paper trading); real env is a live write and needs -live-confirm\nplus -acc-id (安全红线, doc/FUTU.md).\n\n")
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
		fmt.Fprintf(os.Stderr, "futu: order: -symbol is required (e.g. HK.00700)\n")
		return 2
	}
	if _, _, err := futu.ParseSymbol(sym); err != nil {
		fmt.Fprintf(os.Stderr, "futu: order: %v\n", err)
		return 2
	}
	s := strings.ToLower(strings.TrimSpace(*side))
	if s != "buy" && s != "sell" {
		fmt.Fprintf(os.Stderr, "futu: order: -side is required (buy or sell)\n")
		return 2
	}
	if *qty <= 0 {
		fmt.Fprintf(os.Stderr, "futu: order: -qty must be > 0\n")
		return 2
	}
	if *price < 0 {
		fmt.Fprintf(os.Stderr, "futu: order: -price must be >= 0 (0 = market order)\n")
		return 2
	}
	e, err := parseFutuEnv(*env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: order: %v\n", err)
		return 2
	}

	// 安全红线: real-env writes need explicit boss confirmation + account.
	if e == futu.EnvReal {
		if !liveConfirm {
			warnRed("futu: order: 拒绝——-env real 是实盘写操作（安全红线：实盘写需老板确认），必须显式加 -live-confirm")
			return 2
		}
		if *accID == 0 {
			warnRed("futu: order: 拒绝——实盘下单必须显式指定 -acc-id（确认账户，安全红线）")
			return 2
		}
		warnRed("futu: order: LIVE CONFIRMED -live-confirm——实盘写已确认（真单不可撤销，需老板在场）")
	}

	if dryRun {
		printJSON(struct {
			DryRun bool    `json:"dry_run"`
			Env    string  `json:"env"`
			AccID  uint64  `json:"acc_id"`
			Symbol string  `json:"symbol"`
			Side   string  `json:"side"`
			Qty    float64 `json:"qty"`
			Price  float64 `json:"price"`
		}{
			DryRun: true, Env: futu.EnvName(e), AccID: *accID, Symbol: sym,
			Side: s, Qty: *qty, Price: *price,
		})
		return 0
	}

	tc, ok := openTradeClient(prog, "order", *addr)
	if !ok {
		return 1
	}
	defer tc.Close()
	acc, ok := resolveAccount(prog, "order", tc, e, *accID)
	if !ok {
		return 1
	}
	orderIDEx, orderID, err := tc.PlaceOrder(context.Background(), acc, futu.OrderRequest{
		Symbol: sym, Side: s, Qty: *qty, Price: *price,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: order: %v\n", err)
		return 1
	}
	printJSON(struct {
		Env       string  `json:"env"`
		AccID     uint64  `json:"acc_id"`
		Symbol    string  `json:"symbol"`
		Side      string  `json:"side"`
		Qty       float64 `json:"qty"`
		OrderID   uint64  `json:"order_id"`
		OrderIDEx string  `json:"order_id_ex"`
	}{
		Env: futu.EnvName(e), AccID: acc.GetAccID(), Symbol: sym, Side: s, Qty: *qty,
		OrderID: orderID, OrderIDEx: orderIDEx,
	})
	return 0
}

// printJSON writes v as indented JSON to stdout (used by all futu subcommands).
func printJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: bad JSON: %v\n", err)
		return
	}
	fmt.Println(string(out))
}

func runFutuStatus(prog string, argv []string) int {
	fs := flag.NewFlagSet("futu status", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultAddr, "gateway REST base URL")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s futu status [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Checks the futu-opend-rs gateway (REST 22222): GET /health plus GET /api/global-state.\n\n")
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

	st, err := futu.NewClient(*addr).Status(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: status: %v\n", err)
		return 1
	}
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: status: %v\n", err)
		return 1
	}
	fmt.Println(string(out))
	return 0
}

func runFutuQuote(prog string, argv []string) int {
	fs := flag.NewFlagSet("futu quote", flag.ContinueOnError)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	addr := fs.String("addr", futu.DefaultAddr, "gateway REST base URL")
	symbol := fs.String("symbol", "", "market-qualified symbol (e.g. HK.00700)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s futu quote -symbol HK.00700 [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Subscribes to the symbol (SubType_Basic) and prints the /api/quote response as JSON.\n\n")
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
		fmt.Fprintf(os.Stderr, "futu: quote: -symbol is required (e.g. HK.00700)\n")
		return 2
	}
	if _, _, err := futu.ParseSymbol(sym); err != nil {
		fmt.Fprintf(os.Stderr, "futu: quote: %v\n", err)
		return 2
	}

	s2c, err := futu.NewClient(*addr).Quote(context.Background(), sym)
	if err != nil {
		fmt.Fprintf(os.Stderr, "futu: quote: %v\n", err)
		return 1
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, s2c, "", "  "); err != nil {
		fmt.Fprintf(os.Stderr, "futu: quote: bad JSON: %v\n", err)
		return 1
	}
	fmt.Println(buf.String())
	return 0
}

func usageFutu(prog string) {
	fmt.Fprintf(os.Stderr, "Usage: %s futu <subcommand>\n\n", prog)
	fmt.Fprintf(os.Stderr, "Subcommands:\n  status    Gateway health + login state (REST GET /health, /api/global-state)\n  quote     Basic quote for a symbol (REST POST /api/subscribe + /api/quote)\n  funds     Account funds (protobuf TCP 11111; -env sim|real, both read-only)\n  position  Account positions (protobuf TCP 11111; -env sim|real, both read-only)\n  order     Place an order (protobuf TCP 11111; default sim; real needs -live-confirm)\n")
}

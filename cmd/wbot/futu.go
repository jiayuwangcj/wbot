package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

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
	default:
		usageFutu(prog)
		return 2
	}
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
	fmt.Fprintf(os.Stderr, "Subcommands:\n  status  Gateway health + login state (GET /health, /api/global-state)\n  quote   Basic quote for a symbol (POST /api/subscribe + /api/quote)\n")
}

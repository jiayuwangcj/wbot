package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/jiayu/wbot/internal/agent"
	"github.com/jiayu/wbot/internal/master"
	"github.com/jiayu/wbot/internal/poll"
)

const version = "0.0.0-dev"

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
		fmt.Println("master: in-process registry only in this slice; see `wbot agent` for poll.Run smoke")
		return 0
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
	id := fs.String("id", "cli-agent", "agent identity registered with the in-process master")
	interval := fs.Duration("interval", 20*time.Millisecond, "heartbeat interval")
	duration := fs.Duration("duration", 200*time.Millisecond, "run wall-clock time; 0 means until SIGINT")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent [flags]\n\n", prog)
		fmt.Fprintf(os.Stderr, "Runs internal/poll.Run against an in-memory master (no network).\n\n")
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
	m := master.NewMemory()
	if err := poll.Run(ctx, *interval, a, m); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		return 1
	}
	return 0
}

func usage(argv []string) {
	prog := "wbot"
	if len(argv) > 0 && argv[0] != "" {
		prog = argv[0]
	}
	fmt.Fprintf(os.Stdout, "wbot - trading bot (v1 slice)\n\n")
	fmt.Fprintf(os.Stdout, "Usage:\n  %s <command|flag>\n\n", prog)
	fmt.Fprintf(os.Stdout, "Flags:\n  -h, -help, --help    Show help\n  -version, --version Print version\n\n")
	fmt.Fprintf(os.Stdout, "Commands:\n  help, version       Same as flags above\n  agent               In-process poll.Run smoke (try -h)\n  master              Note about in-process master.Memory\n")
}

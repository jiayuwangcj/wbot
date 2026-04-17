package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/agent"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/httpregister"
	"github.com/jiayu/wbot/internal/master"
	"github.com/jiayu/wbot/internal/paper"
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
		return runMaster(argv[0], argv[2:])
	case "paper":
		return runPaper(argv[0], argv[2:])
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

func usage(argv []string) {
	prog := "wbot"
	if len(argv) > 0 && argv[0] != "" {
		prog = argv[0]
	}
	fmt.Fprintf(os.Stdout, "wbot - trading bot (v1 slice)\n\n")
	fmt.Fprintf(os.Stdout, "Usage:\n  %s <command|flag>\n\n", prog)
	fmt.Fprintf(os.Stdout, "Flags:\n  -h, -help, --help    Show help\n  -version, --version Print version\n\n")
	fmt.Fprintf(os.Stdout, "Commands:\n  help, version       Same as flags above\n  agent               poll.Run heartbeat (in-memory or -master-url; try -h)\n  master              HTTP registration server (try -h)\n  paper               One-shot paper.Engine submit (try -h)\n")
}

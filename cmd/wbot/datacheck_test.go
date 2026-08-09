package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/db"
)

func TestDataCheckHelpAndValidation(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "datacheck", "-h"})
	if code != 0 || !strings.Contains(stderr, "8 timeframe x 3 adjustment") {
		t.Fatalf("help: code=%d stderr=%q", code, stderr)
	}

	_, stderr, code = captureRun(t, []string{"wbot", "datacheck", "-now", "not-a-time"})
	if code != 2 || !strings.Contains(stderr, "invalid RFC3339") {
		t.Fatalf("bad now: code=%d stderr=%q", code, stderr)
	}
}

func TestDataCheckCLIIntegrationReportsWatchlistGaps(t *testing.T) {
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
	const symbol = "US.DATACHECKCLI"
	cleanup := func() {
		_, _ = database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM watchlist WHERE symbol = $1`, symbol)
	}
	cleanup()
	defer cleanup()
	if _, err := database.Exec(`INSERT INTO watchlist(symbol, strategy) VALUES ($1, 'buy-hold')`, symbol); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := captureRun(t, []string{"wbot", "datacheck", "-dsn", dsn, "-now", "2026-08-07T17:30:00+08:00"})
	if code != 1 || stderr != "" {
		t.Fatalf("code=%d stderr=%q; want incomplete exit 1", code, stderr)
	}
	if !strings.Contains(stdout, symbol+" 1m/none missing") || !strings.Contains(stdout, symbol+" options missing") || !strings.Contains(stdout, "complete=false") {
		t.Fatalf("stdout = %q; want gap details and summary", stdout)
	}
}

func TestParseDailyTime(t *testing.T) {
	hour, minute, err := parseDailyTime("17:30")
	if err != nil || hour != 17 || minute != 30 {
		t.Fatalf("parseDailyTime = %d:%d, %v", hour, minute, err)
	}
	for _, value := range []string{"", "24:00", "17:60", "x:30", "17"} {
		if _, _, err := parseDailyTime(value); err == nil {
			t.Fatalf("parseDailyTime(%q) succeeded; want error", value)
		}
	}
}

func TestSendDataCheckAlertOnlyForAnomalies(t *testing.T) {
	now := time.Date(2026, 8, 9, 17, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	capture := &captureSender{}
	complete := datacheck.RunResult{After: datacheck.Report{Symbols: 1, Items: []datacheck.Item{}}}
	if err := sendDataCheckAlert(context.Background(), capture, complete, nil, now); err != nil {
		t.Fatal(err)
	}
	if len(capture.messages) != 0 {
		t.Fatalf("complete run sent %d messages", len(capture.messages))
	}

	incomplete := datacheck.RunResult{After: datacheck.Report{
		Symbols: 1,
		Missing: 2,
		Items: []datacheck.Item{
			{Symbol: "US.AAPL", Kind: "bars", Timeframe: "1d", Adjust: "fwd", State: datacheck.StateMissing},
			{Symbol: "US.AAPL", Kind: "options", State: datacheck.StateMissing},
		},
	}}
	if err := sendDataCheckAlert(context.Background(), capture, incomplete, nil, now); err != nil {
		t.Fatal(err)
	}
	if len(capture.messages) != 1 {
		t.Fatalf("messages = %d; want 1", len(capture.messages))
	}
	for _, want := range []string{"标的 1 / 缺失 2 / 过期 0", "US.AAPL 1d/fwd: 缺失", "US.AAPL options: 缺失"} {
		if !strings.Contains(capture.messages[0], want) {
			t.Errorf("message %q missing %q", capture.messages[0], want)
		}
	}
}

func TestSendDataCheckAlertReportsRunAndSenderErrors(t *testing.T) {
	now := time.Date(2026, 8, 9, 17, 30, 0, 0, time.UTC)
	capture := &captureSender{err: errors.New("sink unavailable")}
	err := sendDataCheckAlert(context.Background(), capture, datacheck.RunResult{}, errors.New("database unavailable"), now)
	if err == nil || err.Error() != "sink unavailable" {
		t.Fatalf("error = %v", err)
	}
	if len(capture.messages) != 1 || !strings.Contains(capture.messages[0], "调度失败: database unavailable") {
		t.Fatalf("messages = %q", capture.messages)
	}
}

func TestDataCheckNotifierFromEnv(t *testing.T) {
	for _, name := range []string{
		"DATACHECK_TELEGRAM_BOT_TOKEN",
		"DATACHECK_TELEGRAM_CHAT_ID",
		"DATACHECK_DISCORD_WEBHOOK_URL",
	} {
		t.Setenv(name, "")
	}
	if _, err := dataCheckNotifierFromEnv(nil); err == nil {
		t.Fatal("empty notification config succeeded")
	}

	t.Setenv("DATACHECK_TELEGRAM_BOT_TOKEN", "token")
	if _, err := dataCheckNotifierFromEnv(nil); err == nil || !strings.Contains(err.Error(), "CHAT_ID") {
		t.Fatalf("partial Telegram error = %v", err)
	}
	t.Setenv("DATACHECK_TELEGRAM_CHAT_ID", "chat")
	if sender, err := dataCheckNotifierFromEnv(nil); err != nil || sender == nil {
		t.Fatalf("Telegram sender = %T, %v", sender, err)
	}

	t.Setenv("DATACHECK_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("DATACHECK_TELEGRAM_CHAT_ID", "")
	t.Setenv("DATACHECK_DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/id/token")
	if sender, err := dataCheckNotifierFromEnv(nil); err != nil || sender == nil {
		t.Fatalf("Discord sender = %T, %v", sender, err)
	}
}

type captureSender struct {
	messages []string
	err      error
}

func (s *captureSender) Send(_ context.Context, message string) error {
	s.messages = append(s.messages, message)
	return s.err
}

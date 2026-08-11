package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/notify"
)

type futuDataRepairer struct {
	db     *sql.DB
	client *futu.Client
}

func (r futuDataRepairer) RepairBars(ctx context.Context, item datacheck.Item, now time.Time) error {
	from := now.Add(-repairWindow(item.Timeframe))
	to := now.Add(24 * time.Hour)
	source := ingest.FutuSource{Client: r.client, Adjust: item.Adjust}
	return ingest.RunIngestion(ctx, r.db, "datacheck-futu", domain.Symbol(item.Symbol), item.Timeframe, item.Adjust, "futu", from, to, source)
}

func (r futuDataRepairer) RepairOptions(ctx context.Context, symbol string, now time.Time) error {
	from := now.AddDate(0, 0, -7)
	to := now.Add(24 * time.Hour)
	_, err := ingest.RunOptionIngestion(ctx, r.db, r.client, symbol, futu.AdjustFwd, from, to, 1)
	return err
}

func repairWindow(timeframe string) time.Duration {
	switch timeframe {
	case "1w":
		return 60 * 24 * time.Hour
	case "1mo":
		return 180 * 24 * time.Hour
	default:
		return 14 * 24 * time.Hour
	}
}

func parseDailyTime(value string) (int, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("want HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	return hour, minute, nil
}

func startDataCheckScheduler(ctx context.Context, database *sql.DB, hour, minute int, notifier notify.Sender) {
	addr := resolveFutuGateway("")
	service := datacheck.Service{
		Loader:   datacheck.DBLoader{DB: database},
		Repairer: futuDataRepairer{db: database, client: futu.NewClient(addr)},
		Policy:   datacheck.DefaultPolicy(),
	}
	run := func(runCtx context.Context, now time.Time) {
		result, err := service.Run(runCtx, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "datacheck: scheduled run: %v\n", err)
			if notifyErr := sendDataCheckAlert(runCtx, notifier, result, err, now); notifyErr != nil {
				fmt.Fprintf(os.Stderr, "datacheck: notify: %v\n", notifyErr)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "datacheck: scheduled run: before missing=%d stale=%d; after missing=%d stale=%d; repair_errors=%d\n",
			result.Before.Missing, result.Before.Stale, result.After.Missing, result.After.Stale, len(result.RepairErrors))
		for _, repairErr := range result.RepairErrors {
			fmt.Fprintf(os.Stderr, "datacheck: repair: %s\n", repairErr)
		}
		if notifyErr := sendDataCheckAlert(runCtx, notifier, result, nil, now); notifyErr != nil {
			fmt.Fprintf(os.Stderr, "datacheck: notify: %v\n", notifyErr)
		}
	}
	datacheck.RunDaily(ctx, hour, minute, run)
}

func dataCheckNotifierFromEnv(client *http.Client) (notify.Sender, error) {
	token := strings.TrimSpace(os.Getenv("DATACHECK_TELEGRAM_BOT_TOKEN"))
	chatID := strings.TrimSpace(os.Getenv("DATACHECK_TELEGRAM_CHAT_ID"))
	discordURL := strings.TrimSpace(os.Getenv("DATACHECK_DISCORD_WEBHOOK_URL"))

	var senders notify.MultiSender
	if token != "" || chatID != "" {
		if token == "" || chatID == "" {
			return nil, errors.New("telegram requires DATACHECK_TELEGRAM_BOT_TOKEN and DATACHECK_TELEGRAM_CHAT_ID")
		}
		sender, err := notify.NewTelegram(token, chatID, "", client)
		if err != nil {
			return nil, err
		}
		senders = append(senders, sender)
	}
	if discordURL != "" {
		sender, err := notify.NewDiscord(discordURL, client)
		if err != nil {
			return nil, err
		}
		senders = append(senders, sender)
	}
	if len(senders) == 0 {
		return nil, errors.New("set Telegram or Discord datacheck notification environment variables")
	}
	return senders, nil
}

func sendDataCheckAlert(ctx context.Context, notifier notify.Sender, result datacheck.RunResult, runErr error, now time.Time) error {
	if notifier == nil {
		return nil
	}
	if runErr == nil && result.After.Complete() && len(result.RepairErrors) == 0 {
		return nil
	}
	return notifier.Send(ctx, formatDataCheckAlert(result, runErr, now))
}

func formatDataCheckAlert(result datacheck.RunResult, runErr error, now time.Time) string {
	var b strings.Builder
	b.WriteString("wbot 数据齐全告警\n")
	b.WriteString("检查时间: ")
	b.WriteString(now.Format(time.RFC3339))
	b.WriteByte('\n')
	if runErr != nil {
		fmt.Fprintf(&b, "调度失败: %v", runErr)
		return limitMessage(b.String(), 1800)
	}
	fmt.Fprintf(&b, "修复后: 标的 %d / 缺失 %d / 过期 %d\n", result.After.Symbols, result.After.Missing, result.After.Stale)
	if len(result.RepairErrors) > 0 {
		fmt.Fprintf(&b, "修复错误: %d\n", len(result.RepairErrors))
		for i, repairErr := range result.RepairErrors {
			if i == 3 {
				break
			}
			fmt.Fprintf(&b, "- 修复: %s\n", repairErr)
		}
	}
	shown := 0
	for _, item := range result.After.Items {
		if item.State == datacheck.StateComplete {
			continue
		}
		name := item.Kind
		if item.Kind == "bars" {
			name = item.Timeframe + "/" + item.Adjust
		}
		fmt.Fprintf(&b, "- %s %s: %s\n", item.Symbol, name, dataCheckStateLabel(item.State))
		shown++
		if shown == 10 {
			remaining := result.After.Missing + result.After.Stale - shown
			if remaining > 0 {
				fmt.Fprintf(&b, "- 另有 %d 项\n", remaining)
			}
			break
		}
	}
	return limitMessage(strings.TrimSpace(b.String()), 1800)
}

func dataCheckStateLabel(state datacheck.State) string {
	if state == datacheck.StateMissing {
		return "缺失"
	}
	if state == datacheck.StateStale {
		return "过期"
	}
	return string(state)
}

func limitMessage(message string, maxRunes int) string {
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes-1]) + "…"
}

func resolveFutuGateway(flagValue string) string {
	if addr := strings.TrimSpace(flagValue); addr != "" {
		return addr
	}
	if addr := strings.TrimSpace(os.Getenv("FUTU_GATEWAY_URL")); addr != "" {
		return addr
	}
	return futu.DefaultAddr
}

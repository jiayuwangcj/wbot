package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
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

func startDataCheckScheduler(ctx context.Context, database *sql.DB, hour, minute int) {
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
			return
		}
		fmt.Fprintf(os.Stderr, "datacheck: scheduled run: before missing=%d stale=%d; after missing=%d stale=%d; repair_errors=%d\n",
			result.Before.Missing, result.Before.Stale, result.After.Missing, result.After.Stale, len(result.RepairErrors))
		for _, repairErr := range result.RepairErrors {
			fmt.Fprintf(os.Stderr, "datacheck: repair: %s\n", repairErr)
		}
	}
	datacheck.RunDaily(ctx, hour, minute, run)
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

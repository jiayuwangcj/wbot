package datacheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// SnapshotData is the persistence-neutral input to a completeness check.
type SnapshotData struct {
	Symbols []string
	Bars    []ingest.BarCoverage
	Options []OptionCoverage
}

// Loader reads the latest persisted data coverage.
type Loader interface {
	Load(context.Context) (SnapshotData, error)
}

// Repairer fills one bars series or one underlying's option chain.
type Repairer interface {
	RepairBars(context.Context, Item, time.Time) error
	RepairOptions(context.Context, string, time.Time) error
}

// Service checks, repairs each missing/stale item, then checks again.
type Service struct {
	Loader   Loader
	Repairer Repairer
	Policy   Policy
}

// RunResult preserves both snapshots so callers can report what changed.
type RunResult struct {
	Before       Report   `json:"before"`
	After        Report   `json:"after"`
	RepairErrors []string `json:"repair_errors,omitempty"`
}

// Run performs one idempotent completeness/repair cycle. Individual gateway
// failures do not prevent the remaining series from being attempted.
func (s Service) Run(ctx context.Context, now time.Time) (RunResult, error) {
	if s.Loader == nil || s.Repairer == nil {
		return RunResult{}, errors.New("datacheck: service requires loader and repairer")
	}
	policy := s.Policy
	if len(policy.Timeframes) == 0 || len(policy.Adjusts) == 0 {
		policy = DefaultPolicy()
	}
	beforeData, err := s.Loader.Load(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("datacheck: load before repair: %w", err)
	}
	result := RunResult{Before: Check(beforeData.Symbols, beforeData.Bars, beforeData.Options, now, policy)}
	for _, item := range result.Before.Items {
		if item.State == StateComplete {
			continue
		}
		if item.Kind == "options" {
			err = s.Repairer.RepairOptions(ctx, item.Symbol, now)
		} else {
			err = s.Repairer.RepairBars(ctx, item, now)
		}
		if err != nil {
			name := item.Kind
			if item.Kind == "bars" {
				name = item.Timeframe + "/" + item.Adjust
			}
			result.RepairErrors = append(result.RepairErrors, fmt.Sprintf("%s %s: %v", item.Symbol, name, err))
		}
	}
	afterData, err := s.Loader.Load(ctx)
	if err != nil {
		return result, fmt.Errorf("datacheck: load after repair: %w", err)
	}
	result.After = Check(afterData.Symbols, afterData.Bars, afterData.Options, now, policy)
	return result, nil
}

// NextRun returns the next local occurrence of hour:minute, strictly after now
// when today's occurrence has already started.
func NextRun(now time.Time, hour, minute int) time.Time {
	y, m, d := now.Date()
	next := time.Date(y, m, d, hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// RunDaily runs task once per local calendar day at hour:minute until ctx ends.
func RunDaily(ctx context.Context, hour, minute int, task func(context.Context, time.Time)) {
	for {
		now := time.Now()
		next := NextRun(now, hour, minute)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case runAt := <-timer.C:
			task(ctx, runAt)
		}
	}
}

// DBLoader adapts Snapshot to Service.Loader.
type DBLoader struct{ DB *sql.DB }

// Load reads one snapshot from DB.
func (l DBLoader) Load(ctx context.Context) (SnapshotData, error) {
	symbols, bars, options, err := Snapshot(ctx, l.DB)
	return SnapshotData{Symbols: symbols, Bars: bars, Options: options}, err
}

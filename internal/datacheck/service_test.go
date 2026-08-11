package datacheck

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

type snapshotSequence struct {
	values []SnapshotData
	calls  int
}

func (s *snapshotSequence) Load(context.Context) (SnapshotData, error) {
	if s.calls >= len(s.values) {
		return SnapshotData{}, errors.New("unexpected load")
	}
	value := s.values[s.calls]
	s.calls++
	return value, nil
}

type repairRecorder struct {
	bars    []Item
	options []string
}

func (r *repairRecorder) RepairBars(_ context.Context, item Item, _ time.Time) error {
	r.bars = append(r.bars, item)
	return nil
}

func (r *repairRecorder) RepairOptions(_ context.Context, symbol string, _ time.Time) error {
	r.options = append(r.options, symbol)
	return nil
}

func TestServiceRepairsGapsAndRechecks(t *testing.T) {
	now := time.Date(2026, 8, 7, 17, 30, 0, 0, time.UTC)
	policy := Policy{Timeframes: []string{"1d"}, Adjusts: []string{"fwd"}, Options: true}
	loader := &snapshotSequence{values: []SnapshotData{
		{Symbols: []string{"US.AAPL"}},
		{
			Symbols: []string{"US.AAPL"},
			Bars:    []ingest.BarCoverage{{Symbol: "US.AAPL", Timeframe: "1d", Adjust: "fwd", MaxTs: now}},
			Options: []OptionCoverage{{Underlying: "US.AAPL", MaxTs: now, MaxExpiry: now.AddDate(0, 0, 7)}},
		},
	}}
	repairer := &repairRecorder{}
	result, err := (Service{Loader: loader, Repairer: repairer, Policy: policy}).Run(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Before.Complete() || !result.After.Complete() {
		t.Fatalf("result = %+v; want incomplete before and complete after", result)
	}
	if len(repairer.bars) != 1 || len(repairer.options) != 1 {
		t.Fatalf("repairs: bars=%d options=%d; want 1 each", len(repairer.bars), len(repairer.options))
	}
}

func TestNextRun(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	before := time.Date(2026, 8, 7, 16, 0, 0, 0, loc)
	if got, want := NextRun(before, 17, 30), time.Date(2026, 8, 7, 17, 30, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("NextRun before = %s; want %s", got, want)
	}
	after := time.Date(2026, 8, 7, 18, 0, 0, 0, loc)
	if got, want := NextRun(after, 17, 30), time.Date(2026, 8, 8, 17, 30, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("NextRun after = %s; want %s", got, want)
	}
}

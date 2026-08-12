package wheelstore

import (
	"context"
	"time"
)

// SignalRepository is the shared signal pipeline surface. It includes the
// append operations used by producers and the reads used by wheel, review and
// Telegram consumers; Store remains the concrete PostgreSQL implementation.
type SignalRepository interface {
	AppendSignal(context.Context, SignalRecord) (int64, error)
	AppendAction(context.Context, ActionRecord) (int64, error)
	LatestConfig(context.Context, string) (*ConfigRecord, error)
	ListSignals(context.Context, string, string, string, int) ([]SignalRecord, error)
	GetSignal(context.Context, int64) (*SignalRecord, error)
	LatestLLMReview(context.Context, int64) (*ActionRecord, error)
	LatestAction(context.Context, int64, string) (*ActionRecord, error)
	HasAction(context.Context, int64, string) (bool, error)
	QuerySignalsSince(context.Context, string, int64, int) ([]SignalRecord, error)
	MaxSignalID(context.Context) (int64, error)
	Dismiss(context.Context, string, time.Time) error
	IsDismissed(context.Context, string, time.Time) (bool, error)
}

var _ SignalRepository = (*Store)(nil)

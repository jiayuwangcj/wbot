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
	// ListPendingOrders returns the symbol's confirmed-but-unfilled orders;
	// consumers pass the result to the LLM generator/reviewer as an explicit
	// declaration of open exposure (老板指令 2026-08-13).
	ListPendingOrders(context.Context, string) ([]PendingOrder, error)
	Dismiss(context.Context, string, time.Time) error
	IsDismissed(context.Context, string, time.Time) (bool, error)
}

var _ SignalRepository = (*Store)(nil)

// OrderClaimRepository is the DB-backed cross-channel idempotency boundary.
// It is intentionally separate from SignalRepository so producers/reviewers
// do not acquire order-placement capabilities.
type OrderClaimRepository interface {
	ClaimOrder(context.Context, int64, string) (bool, error)
	CompleteOrderClaim(context.Context, int64, uint64, string, map[string]any) error
}

// SchedulerRepository is the persistent dedupe read needed by periodic LLM
// generation.  Store implements it with a DB query, never an in-memory map.
type SchedulerRepository interface {
	HasRecentUndisposedSignal(context.Context, string, time.Time) (bool, error)
	// ListPendingOrders returns every confirmed-but-unfilled order (CONFIRM
	// without FILL/NO/REJECTED) for the symbol. The generator and review gate
	// receive this list as an explicit declaration of open exposure — an empty
	// slice means "queried and none open" (2026-08-13: 701 confirmed 08:07,
	// never filled, yet 702 was issued next tick; the fix is to let the LLM see
	// and judge the open order, not to silently stack exposure).
	ListPendingOrders(context.Context, string) ([]PendingOrder, error)
}

var _ OrderClaimRepository = (*Store)(nil)
var _ SchedulerRepository = (*Store)(nil)

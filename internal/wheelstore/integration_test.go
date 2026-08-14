package wheelstore

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
)

func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateUp(database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func cleanIntegrationWheel(t *testing.T, database *sql.DB, symbol string) {
	t.Helper()
	if _, err := database.Exec(`
DELETE FROM wheel_order_claims
WHERE signal_id IN (SELECT id FROM wheel_signals WHERE symbol = $1)`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
DELETE FROM wheel_signal_actions
WHERE signal_id IN (SELECT id FROM wheel_signals WHERE symbol = $1)`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM wheel_signals WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM option_quote_snapshots WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM wheel_configs WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
}

func TestWheelStoreIntegration(t *testing.T) {
	database := openIntegrationDB(t)
	ctx := context.Background()
	symbol := "WHEELSTORE.TEST"
	cleanIntegrationWheel(t, database, symbol)
	t.Cleanup(func() { cleanIntegrationWheel(t, database, symbol) })

	store := New(database)
	configID, err := store.AppendConfig(ctx, ConfigRecord{
		Symbol: symbol, Version: 1,
		Config: map[string]any{"strategy": "wheel", "params": map[string]any{"max_inventory": 1200}},
		State:  map[string]any{"strategic_state": "NORMAL"},
	})
	if err != nil || configID <= 0 {
		t.Fatalf("AppendConfig id=%d err=%v", configID, err)
	}
	latest, err := store.LatestConfig(ctx, symbol)
	if err != nil || latest.Version != 1 || latest.Config["strategy"] != "wheel" {
		t.Fatalf("LatestConfig=%+v err=%v", latest, err)
	}
	configs, err := store.ListConfigs(ctx, symbol, 10)
	if err != nil || len(configs) != 1 {
		t.Fatalf("ListConfigs=%+v err=%v", configs, err)
	}

	observed := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	complete := validQuote()
	complete.Symbol, complete.Underlying, complete.ObservedAt = symbol, "WHEELSTORE", observed
	complete.SnapshotKey = "batch-complete"
	quoteID, err := store.AppendQuoteSnapshot(ctx, complete)
	if err != nil || quoteID <= 0 {
		t.Fatalf("AppendQuoteSnapshot complete id=%d err=%v", quoteID, err)
	}
	// Incomplete observations are retained for diagnostics and query, but
	// Complete remains false and the signal layer cannot turn them into ALERT.
	incomplete := complete
	incomplete.SnapshotKey = "batch-incomplete"
	incomplete.IV = nil
	incomplete.Delta = nil
	incomplete.ObservedAt = observed.Add(time.Minute)
	if _, err := store.AppendQuoteSnapshot(ctx, incomplete); err != nil {
		t.Fatalf("AppendQuoteSnapshot incomplete: %v", err)
	}
	quotes, err := store.QueryQuoteSnapshots(ctx, symbol, time.Time{}, time.Time{}, 10)
	if err != nil || len(quotes) != 2 {
		t.Fatalf("QueryQuoteSnapshots len=%d err=%v", len(quotes), err)
	}
	if quotes[0].Source != "test" || quotes[0].IngestedAt.IsZero() {
		t.Fatalf("quote metadata=%+v", quotes[0])
	}

	alertID, err := store.AppendSignal(ctx, SignalRecord{
		Symbol: symbol, Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY",
		Inventory: validInventory(), Candidates: []Candidate{{QuoteSnapshotID: quoteID, Direction: "PUT"}},
		Reason: "inventory gap exceeds no-trade gap",
	})
	if err != nil || alertID <= 0 {
		t.Fatalf("AppendSignal ALERT id=%d err=%v", alertID, err)
	}
	holdID, err := store.AppendSignal(ctx, SignalRecord{
		Symbol: symbol, Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "DATA_BLOCKED",
		BlockedBy: []string{"missing_iv"}, RejectionReasons: []string{"incomplete quote"},
		Reason: "required quote fields are missing",
	})
	if err != nil || holdID <= 0 {
		t.Fatalf("AppendSignal HOLD id=%d err=%v", holdID, err)
	}
	if pending, err := store.HasRecentUndisposedSignal(ctx, symbol, time.Now().Add(-time.Hour)); err != nil || !pending {
		t.Fatalf("HasRecentUndisposedSignal = %v, %v; want true", pending, err)
	}
	claimed, err := store.ClaimOrder(ctx, alertID, "telegram:42")
	if err != nil || !claimed {
		t.Fatalf("ClaimOrder first = %v, %v; want true", claimed, err)
	}
	claimed, err = store.ClaimOrder(ctx, alertID, "discord:42")
	if err != nil || claimed {
		t.Fatalf("ClaimOrder second channel = %v, %v; want false", claimed, err)
	}
	if err := store.CompleteOrderClaim(ctx, alertID, 12345, "order-12345", map[string]any{"limit_price": 1.25}); err != nil {
		t.Fatalf("CompleteOrderClaim: %v", err)
	}
	gotAlert, err := store.GetSignal(ctx, alertID)
	if err != nil || gotAlert.Action != "ALERT" || len(gotAlert.Candidates) != 1 {
		t.Fatalf("GetSignal=%+v err=%v", gotAlert, err)
	}
	gotHold, err := store.GetSignal(ctx, holdID)
	if err != nil || gotHold.CapabilityStatus != "DATA_BLOCKED" || len(gotHold.BlockedBy) != 1 {
		t.Fatalf("GetSignal HOLD=%+v err=%v", gotHold, err)
	}
	// The database repeats the repository's invariants so direct/internal SQL
	// cannot persist contradictory audit records.
	for _, tc := range []struct {
		name    string
		action  string
		status  string
		blocked string
	}{
		{"unknown status", "HOLD", "UNKNOWN", `[]`},
		{"ready with blockers", "HOLD", "READY", `["missing_iv"]`},
		{"blocked without blockers", "HOLD", "DATA_BLOCKED", `[]`},
		{"blocked alert", "ALERT", "DATA_BLOCKED", `["missing_iv"]`},
	} {
		t.Run("database rejects "+tc.name, func(t *testing.T) {
			_, err := database.ExecContext(ctx, `
INSERT INTO wheel_signals (symbol, action, config_version, capability_status, blocked_by, reason)
VALUES ($1, $2, 1, $3, $4::jsonb, 'invalid fixture')`, symbol, tc.action, tc.status, tc.blocked)
			if err == nil {
				t.Fatalf("database accepted %s/%s blocked_by=%s", tc.action, tc.status, tc.blocked)
			}
		})
	}
	signals, err := store.ListSignals(ctx, symbol, "", "", 10)
	if err != nil || len(signals) != 2 {
		t.Fatalf("ListSignals len=%d err=%v", len(signals), err)
	}
	// Capability filter narrows to the matching rows only.
	blocked, err := store.ListSignals(ctx, symbol, "", "DATA_BLOCKED", 10)
	if err != nil || len(blocked) != 1 || blocked[0].Action != "HOLD" {
		t.Fatalf("ListSignals capability=DATA_BLOCKED len=%d err=%v rows=%+v", len(blocked), err, blocked)
	}
	ready, err := store.ListSignals(ctx, symbol, "", "READY", 10)
	if err != nil || len(ready) != 1 || ready[0].Action != "ALERT" {
		t.Fatalf("ListSignals capability=READY len=%d err=%v rows=%+v", len(ready), err, ready)
	}

	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: alertID, Action: "CONFIRM", Actor: "operator", Note: "reviewed", Details: map[string]any{"order_id": 12345}}); err != nil {
		t.Fatalf("AppendAction CONFIRM: %v", err)
	}
	// ListPendingOrders surfaces the confirmed-but-unfilled order with the
	// broker order id from the CONFIRM details (2026-08-13: LLM 策略输入)。
	pendingOrders, err := store.ListPendingOrders(ctx, symbol)
	if err != nil || len(pendingOrders) != 1 || pendingOrders[0].SignalID != alertID || pendingOrders[0].OrderID != "12345" {
		t.Fatalf("ListPendingOrders = %+v err=%v; want one order id 12345", pendingOrders, err)
	}
	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: alertID, Action: "FILL", Actor: "operator", Note: "human-reported fill", Details: map[string]any{"contracts": 1}}); err != nil {
		t.Fatalf("AppendAction FILL: %v", err)
	}
	// FILL 解除挂单:同一信号不再被视为 pending。
	pendingOrders, err = store.ListPendingOrders(ctx, symbol)
	if err != nil || len(pendingOrders) != 0 {
		t.Fatalf("ListPendingOrders after FILL = %+v err=%v; want none", pendingOrders, err)
	}
	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: alertID, Action: "LLM_REVIEW", Actor: "llm:test-model", Details: map[string]any{"verdict": "APPROVE", "reasons": []string{"within budget"}}}); err != nil {
		t.Fatalf("AppendAction LLM_REVIEW: %v", err)
	}
	actions, err := store.ListActions(ctx, alertID)
	if err != nil || len(actions) != 3 || actions[2].Action != "LLM_REVIEW" || actions[2].Details["verdict"] != "APPROVE" {
		t.Fatalf("ListActions=%+v err=%v", actions, err)
	}
	// Guard migration 008: unknown actions must be rejected by the CHECK
	// constraint even when written with bare SQL past the Go validation.
	if _, err := database.Exec(`INSERT INTO wheel_signal_actions (signal_id, action, actor) VALUES ($1, 'HACK', 'test')`, alertID); err == nil {
		t.Fatal("database accepted HACK action; CHECK constraint from migration 008 missing")
	}
}

func TestStrategyCacheUpsertIntegration(t *testing.T) {
	database := openIntegrationDB(t)
	ctx := context.Background()
	symbol := "CACHE.IDEMPOTENT"
	if _, err := database.Exec(`DELETE FROM strategy_cache WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM strategy_cache WHERE symbol = $1`, symbol) })
	record := StrategyCacheRecord{
		Symbol: symbol, Market: "US", Currency: "USD", ConfigVersion: 1, ModelVersion: "test-model",
		DataWindow:    StrategyCacheWindow{From: "2025-01-01T00:00:00Z", To: "2025-12-31T00:00:00Z"},
		ApprovedState: StrategyCacheResearchCandidate,
		Payload:       map[string]any{"schema_version": "strategy-cache-1.0", "report_reference": map[string]any{"report_id": "same-report"}},
	}
	store := New(database)
	if err := store.UpsertStrategyCache(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.Payload["return_metrics"] = map[string]any{"net_return_pct": 0.2}
	if err := store.UpsertStrategyCache(ctx, record); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM strategy_cache WHERE symbol = $1`, symbol).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cache rows=%d err=%v; want one upserted row", count, err)
	}
	got, err := store.GetStrategyCache(ctx, symbol)
	if err != nil || got.Payload["return_metrics"] == nil {
		t.Fatalf("GetStrategyCache=%+v err=%v; second write did not overwrite payload", got, err)
	}
	record.Payload["approval_gates"] = map[string]any{"data_gate_passed": true, "sample_out_passed": true, "human_approved": false}
	if err := store.UpsertStrategyCache(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveStrategyCache(ctx, symbol); err != nil {
		t.Fatal(err)
	}
	approved, err := store.GetStrategyCache(ctx, symbol)
	if err != nil || approved.ApprovedState != StrategyCacheApprovedCandidate {
		t.Fatalf("approved cache=%+v err=%v", approved, err)
	}
}

// TestWheelTelegramDispositionIntegration covers migrations 009/010 and the
// Telegram confirm-loop store surface: dismissals (idempotent, per-day),
// LatestLLMReview, QuerySignalsSince/MaxSignalID cursor semantics, and the
// NO/REJECTED action vocabulary.
func TestWheelTelegramDispositionIntegration(t *testing.T) {
	database := openIntegrationDB(t)
	ctx := context.Background()
	symbol := "WHEELSTORE.TG"
	cleanIntegrationWheel(t, database, symbol)
	t.Cleanup(func() { cleanIntegrationWheel(t, database, symbol) })

	store := New(database)
	if _, err := store.AppendConfig(ctx, ConfigRecord{
		Symbol: symbol, Version: 1, Config: map[string]any{"strategy": "wheel"},
	}); err != nil {
		t.Fatal(err)
	}
	inv := validInventory()
	signalID, err := store.AppendSignal(ctx, SignalRecord{
		Symbol: symbol, Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY",
		Inventory: inv, Candidates: []Candidate{{QuoteSnapshotID: 1, Direction: "PUT", Quantity: 1}},
		Reason: "inventory gap exceeds no-trade gap",
	})
	if err != nil || signalID <= 0 {
		t.Fatalf("AppendSignal id=%d err=%v", signalID, err)
	}
	holdID, err := store.AppendSignal(ctx, SignalRecord{
		Symbol: symbol, Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "DATA_BLOCKED",
		BlockedBy: []string{"missing_iv"}, Reason: "incomplete quote",
	})
	if err != nil || holdID <= 0 {
		t.Fatalf("AppendSignal HOLD id=%d err=%v", holdID, err)
	}

	// NO/REJECTED (migration 010) pass the Go validation and the DB CHECK.
	for _, action := range []string{"NO", "REJECTED"} {
		if _, err := store.AppendAction(ctx, ActionRecord{SignalID: signalID, Action: action, Actor: "telegram:42"}); err != nil {
			t.Fatalf("AppendAction %s: %v", action, err)
		}
	}
	// HasAction powers the Telegram yes-path dedup.
	if has, err := store.HasAction(ctx, signalID, "CONFIRM"); err != nil || has {
		t.Fatalf("HasAction(CONFIRM) before confirm = %v, %v; want false", has, err)
	}
	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: signalID, Action: "CONFIRM", Actor: "telegram:42"}); err != nil {
		t.Fatalf("AppendAction CONFIRM: %v", err)
	}
	if has, err := store.HasAction(ctx, signalID, "CONFIRM"); err != nil || !has {
		t.Fatalf("HasAction(CONFIRM) after confirm = %v, %v; want true", has, err)
	}
	if has, err := store.HasAction(ctx, signalID, "NO"); err != nil || !has {
		t.Fatalf("HasAction(NO) = %v, %v; want true", has, err)
	}
	if _, err := store.HasAction(ctx, signalID, "HACK"); err != ErrInvalidOp {
		t.Fatalf("HasAction(HACK) = %v; want ErrInvalidOp", err)
	}
	if _, err := database.Exec(`INSERT INTO wheel_signal_actions (signal_id, action, actor) VALUES ($1, 'HACK', 'test')`, signalID); err == nil {
		t.Fatal("database accepted HACK action; CHECK constraint from migration 010 missing")
	}
	if _, err := store.LatestLLMReview(ctx, signalID); err != ErrNotFound {
		t.Fatalf("LatestLLMReview without review = %v; want ErrNotFound", err)
	}
	reviewID, err := store.AppendAction(ctx, ActionRecord{SignalID: signalID, Action: "LLM_REVIEW", Actor: "llm:test", Details: map[string]any{"verdict": "REJECT"}})
	if err != nil || reviewID <= 0 {
		t.Fatalf("AppendAction LLM_REVIEW id=%d err=%v", reviewID, err)
	}
	review, err := store.LatestLLMReview(ctx, signalID)
	if err != nil || review.Details["verdict"] != "REJECT" {
		t.Fatalf("LatestLLMReview=%+v err=%v", review, err)
	}
	// A newer review wins over an older one.
	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: signalID, Action: "LLM_REVIEW", Actor: "llm:test", Details: map[string]any{"verdict": "APPROVE"}}); err != nil {
		t.Fatal(err)
	}
	review, err = store.LatestLLMReview(ctx, signalID)
	if err != nil || review.Details["verdict"] != "APPROVE" {
		t.Fatalf("LatestLLMReview after second review=%+v err=%v", review, err)
	}

	// Cursor semantics: seeded cursor skips history, later signals appear.
	maxID, err := store.MaxSignalID(ctx)
	if err != nil || maxID != holdID {
		t.Fatalf("MaxSignalID=%d err=%v; want %d", maxID, err, holdID)
	}
	signals, err := store.QuerySignalsSince(ctx, "ALERT", maxID, 10)
	if err != nil || len(signals) != 0 {
		t.Fatalf("QuerySignalsSince after max = %d rows, err=%v", len(signals), err)
	}
	alert2, err := store.AppendSignal(ctx, SignalRecord{
		Symbol: symbol, Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY",
		Inventory: inv, Candidates: []Candidate{{QuoteSnapshotID: 1, Direction: "PUT"}},
		Reason: "gap again",
	})
	if err != nil || alert2 <= holdID {
		t.Fatalf("AppendSignal second alert id=%d err=%v", alert2, err)
	}
	signals, err = store.QuerySignalsSince(ctx, "ALERT", maxID, 10)
	if err != nil || len(signals) != 1 || signals[0].ID != alert2 {
		t.Fatalf("QuerySignalsSince = %+v err=%v; want one row id=%d", signals, err, alert2)
	}

	// Dismissals (migration 009): idempotent, per-symbol per-day.
	today := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	otherDay := today.AddDate(0, 0, 1)
	if err := store.Dismiss(ctx, symbol, today); err != nil {
		t.Fatal(err)
	}
	if err := store.Dismiss(ctx, symbol, today); err != nil {
		t.Fatalf("second dismiss must be a no-op: %v", err)
	}
	if dismissed, err := store.IsDismissed(ctx, symbol, today); err != nil || !dismissed {
		t.Fatalf("IsDismissed(today) = %v, %v; want true", dismissed, err)
	}
	if dismissed, err := store.IsDismissed(ctx, symbol, otherDay); err != nil || dismissed {
		t.Fatalf("IsDismissed(other day) = %v, %v; want false", dismissed, err)
	}
	if dismissed, err := store.IsDismissed(ctx, symbol+"-OTHER", today); err != nil || dismissed {
		t.Fatalf("IsDismissed(other symbol) = %v, %v; want false", dismissed, err)
	}
}

func TestQueryUnderlyingQuoteSnapshotsPreservesAtomicBatchAtLimit(t *testing.T) {
	database := openIntegrationDB(t)
	ctx := context.Background()
	const underlying = "WHEELSTORE.ATOMIC"
	if _, err := database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying = $1`, underlying); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying = $1`, underlying) })

	store := New(database)
	observed := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	for _, fixture := range []struct {
		symbol, kind, key string
		delta             float64
		at                time.Time
	}{
		{underlying + "-OLD", "PUT", "older", -0.2, observed.Add(-time.Minute)},
		{underlying + "-P", "PUT", "latest-atomic", -0.3, observed},
		{underlying + "-C", "CALL", "latest-atomic", 0.3, observed},
	} {
		quote := validQuote()
		quote.Symbol, quote.Underlying, quote.OptionType = fixture.symbol, underlying, fixture.kind
		quote.SnapshotKey, quote.ObservedAt, quote.Delta = fixture.key, fixture.at, f(fixture.delta)
		if _, err := store.AppendQuoteSnapshot(ctx, quote); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.QueryUnderlyingQuoteSnapshots(ctx, underlying, time.Time{}, time.Time{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].SnapshotKey != "latest-atomic" || rows[1].SnapshotKey != "latest-atomic" {
		t.Fatalf("limit=1 returned %+v; want every contract in the latest atomic batch", rows)
	}
}

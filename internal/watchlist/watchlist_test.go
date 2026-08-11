package watchlist

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/db"
)

// TestSetExecutionStatusValidation covers the pure checks that need no DB:
// the status whitelist is enforced before any row is touched.
func TestSetExecutionStatusValidation(t *testing.T) {
	ctx := context.Background()
	if err := SetExecutionStatus(ctx, nil, "HK.00700", "BOGUS", "x"); err == nil ||
		!strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("invalid status err = %v; want invalid status", err)
	}
	if err := SetExecutionStatus(ctx, nil, "", StatusReady, ""); err == nil ||
		!strings.Contains(err.Error(), "empty symbol") {
		t.Fatalf("empty symbol err = %v; want empty symbol", err)
	}
	for _, status := range []string{"ready", "data_blocked", "needs_reconfiguration", "READY", "DATA_BLOCKED", "NEEDS_RECONFIGURATION"} {
		if err := SetExecutionStatus(ctx, nil, "HK.00700", status, ""); err == nil ||
			!strings.Contains(err.Error(), "nil db") {
			t.Fatalf("status %s with nil db err = %v; want nil db", status, err)
		}
	}
}

// TestSetExecutionStatusIntegration: READY clears the invalidation reason
// (NULL), DATA_BLOCKED stores it, and a missing symbol reports sql.ErrNoRows.
func TestSetExecutionStatusIntegration(t *testing.T) {
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
	ctx := context.Background()
	const symbol = "US.WLSTATUS"
	cleanup := func() {
		_, _ = database.Exec(`DELETE FROM watchlist WHERE symbol = $1`, symbol)
	}
	cleanup()
	defer cleanup()

	if _, err := database.Exec(`INSERT INTO watchlist(symbol, strategy, execution_status, invalidation_reason) VALUES ($1, 'wheel', 'DATA_BLOCKED', 'old reason')`, symbol); err != nil {
		t.Fatal(err)
	}
	if err := SetExecutionStatus(ctx, database, symbol, StatusReady, "should be ignored"); err != nil {
		t.Fatalf("SetExecutionStatus(READY) error: %v", err)
	}
	var status, reason sql.NullString
	if err := database.QueryRow(`SELECT execution_status, invalidation_reason FROM watchlist WHERE symbol = $1`, symbol).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status.String != StatusReady || reason.Valid {
		t.Fatalf("after READY: status=%q reason_valid=%v; want READY with NULL reason", status.String, reason.Valid)
	}

	if err := SetExecutionStatus(ctx, database, symbol, StatusDataBlocked, "no complete quote snapshot"); err != nil {
		t.Fatalf("SetExecutionStatus(DATA_BLOCKED) error: %v", err)
	}
	if err := database.QueryRow(`SELECT execution_status, invalidation_reason FROM watchlist WHERE symbol = $1`, symbol).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status.String != StatusDataBlocked || !reason.Valid || reason.String != "no complete quote snapshot" {
		t.Fatalf("after DATA_BLOCKED: status=%q reason=%q; want DATA_BLOCKED with reason", status.String, reason.String)
	}

	err = SetExecutionStatus(ctx, database, symbol+"NOPE", StatusReady, "")
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing symbol err = %v; want sql.ErrNoRows wrapped", err)
	}
}

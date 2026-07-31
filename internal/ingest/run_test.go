package ingest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

func TestRunIngestion_validation(t *testing.T) {
	ctx := context.Background()
	err := RunIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d", time.Time{}, time.Time{}, mockSource{})
	if err == nil {
		t.Fatal("expected error for nil db")
	}
	err = RunIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d", time.Time{}, time.Time{}, nil)
	if err == nil {
		t.Fatal("expected error for nil source")
	}
}

// noConnDriver is a driver that never connects; used only to build a non-nil
// *sql.DB so validation checks past the nil-db guard can be exercised.
type noConnDriver struct{}

func (noConnDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("noConnDriver: not meant to connect")
}

var (
	stubDBOnce sync.Once
	stubDBVal  *sql.DB
)

func stubDB() *sql.DB {
	stubDBOnce.Do(func() {
		sql.Register("wbot-test-noconn", noConnDriver{})
		stubDBVal, _ = sql.Open("wbot-test-noconn", "")
	})
	return stubDBVal
}

func TestRunIngestion_fromAfterTo(t *testing.T) {
	ctx := context.Background()
	from := time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	err := RunIngestion(ctx, stubDB(), "mock", domain.Symbol("X.US"), "1d", from, to, mockSource{})
	if err == nil {
		t.Fatal("expected error for from after to")
	}
	if !strings.Contains(err.Error(), "from after to") {
		t.Fatalf("err = %q; want message mentioning 'from after to'", err)
	}
}

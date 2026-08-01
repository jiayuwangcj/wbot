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
	err := RunIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d", "none", "mock", time.Time{}, time.Time{}, mockSource{})
	if err == nil {
		t.Fatal("expected error for nil db")
	}
	err = RunIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d", "none", "mock", time.Time{}, time.Time{}, nil)
	if err == nil {
		t.Fatal("expected error for nil source")
	}
}

// noConnDriver never connects; it exists so stubDB can bypass the nil-db guard.
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
	err := RunIngestion(ctx, stubDB(), "mock", domain.Symbol("X.US"), "1d", "none", "mock", from, to, mockSource{})
	if err == nil {
		t.Fatal("expected error for from after to")
	}
	if !strings.Contains(err.Error(), "from after to") {
		t.Fatalf("err = %q; want message mentioning 'from after to'", err)
	}
}

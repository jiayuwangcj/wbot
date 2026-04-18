package ingest

import (
	"context"
	"testing"

	"github.com/jiayu/wbot/internal/domain"
)

func TestRunIngestion_validation(t *testing.T) {
	ctx := context.Background()
	err := RunIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d", mockSource{})
	if err == nil {
		t.Fatal("expected error for nil db")
	}
	err = RunIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d", nil)
	if err == nil {
		t.Fatal("expected error for nil source")
	}
}

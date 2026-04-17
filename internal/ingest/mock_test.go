package ingest

import (
	"context"
	"testing"

	"github.com/jiayu/wbot/internal/domain"
)

func TestRunMockIngestion_validation(t *testing.T) {
	ctx := context.Background()
	err := RunMockIngestion(ctx, nil, "mock", domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

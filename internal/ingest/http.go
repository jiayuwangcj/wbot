package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

// HTTPSource fetches a JSON array of bars from URL; symbol/timeframe are not filtered.
type HTTPSource struct {
	URL string
}

// Bars fetches/parses the payload, keeping bars in [from, to]; zero from/to are unbounded.
func (h HTTPSource) Bars(ctx context.Context, _ domain.Symbol, _ string, from, to time.Time) ([]Bar, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if h.URL == "" {
		return nil, errors.New("ingest: http source: empty url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("ingest: http source request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ingest: http source get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ingest: http source: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ingest: http source read: %w", err)
	}
	bars, err := parseBarRecords(data, "http source")
	if err != nil {
		return nil, err
	}
	return filterRange(bars, from, to), nil
}

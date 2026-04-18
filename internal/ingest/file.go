package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

// FileSource reads a JSON array of bars from Path. Symbol and timeframe are not
// filtered from the file; they label rows when RunIngestion writes to bars.
type FileSource struct {
	Path string
}

type fileBarRecord struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

func (f FileSource) Bars(ctx context.Context, _ domain.Symbol, _ string) ([]Bar, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if f.Path == "" {
		return nil, errors.New("ingest: file source: empty path")
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, fmt.Errorf("ingest: file source read: %w", err)
	}
	var recs []fileBarRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("ingest: file source json: %w", err)
	}
	if len(recs) == 0 {
		return nil, errors.New("ingest: file source: empty array")
	}
	out := make([]Bar, 0, len(recs))
	for i, r := range recs {
		if r.Ts == "" {
			return nil, fmt.Errorf("ingest: file source: record %d: empty ts", i)
		}
		ts, err := time.Parse(time.RFC3339Nano, r.Ts)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, r.Ts)
			if err != nil {
				return nil, fmt.Errorf("ingest: file source: record %d ts: %w", i, err)
			}
		}
		out = append(out, Bar{
			Ts: ts.UTC(), Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return out, nil
}

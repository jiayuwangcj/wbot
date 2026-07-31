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

// Bars reads and parses the file, then keeps only bars inside the closed
// interval [from, to]; zero from/to are unbounded.
func (f FileSource) Bars(ctx context.Context, _ domain.Symbol, _ string, from, to time.Time) ([]Bar, error) {
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
	bars, err := parseBarRecords(data, "file source")
	if err != nil {
		return nil, err
	}
	return filterRange(bars, from, to), nil
}

// parseBarRecords parses a JSON array of fileBarRecord into Bars. label names
// the source in error messages (e.g. "file source", "http source").
func parseBarRecords(data []byte, label string) ([]Bar, error) {
	var recs []fileBarRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("ingest: %s json: %w", label, err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("ingest: %s: empty array", label)
	}
	out := make([]Bar, 0, len(recs))
	for i, r := range recs {
		if r.Ts == "" {
			return nil, fmt.Errorf("ingest: %s: record %d: empty ts", label, i)
		}
		ts, err := time.Parse(time.RFC3339Nano, r.Ts)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, r.Ts)
			if err != nil {
				return nil, fmt.Errorf("ingest: %s: record %d ts: %w", label, i, err)
			}
		}
		out = append(out, Bar{
			Ts: ts.UTC(), Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return out, nil
}

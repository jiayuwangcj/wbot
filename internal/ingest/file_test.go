package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

func TestFileSource_Bars(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "bars.json")
	content := `[
  {"ts":"2024-06-01T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":10},
  {"ts":"2024-06-02T00:00:00Z","open":1.5,"high":2.5,"low":1,"close":2,"volume":11}
]`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	src := FileSource{Path: path}
	bars, err := src.Bars(ctx, domain.Symbol("X.US"), "1d", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 {
		t.Fatalf("len %d want 2", len(bars))
	}
	if bars[0].Open != 1 || bars[1].Volume != 11 {
		t.Fatalf("unexpected bars: %+v", bars)
	}
}

func TestFileSource_Bars_emptyPath(t *testing.T) {
	ctx := context.Background()
	_, err := (FileSource{}).Bars(ctx, domain.Symbol("X.US"), "1d", time.Time{}, time.Time{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileSource_Bars_range(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "bars.json")
	content := `[
	  {"ts":"2024-06-01T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":10},
	  {"ts":"2024-06-02T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":11},
	  {"ts":"2024-06-03T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":12},
	  {"ts":"2024-06-04T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":13}
	]`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	src := FileSource{Path: path}
	mustT := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want int
	}{
		{"zero from/to keeps all", time.Time{}, time.Time{}, 4},
		{"closed range includes endpoints", mustT("2024-06-02T00:00:00Z"), mustT("2024-06-03T00:00:00Z"), 2},
		{"from only", mustT("2024-06-03T00:00:00Z"), time.Time{}, 2},
		{"to only", time.Time{}, mustT("2024-06-02T00:00:00Z"), 2},
		{"range outside data is empty", mustT("2024-05-01T00:00:00Z"), mustT("2024-05-02T00:00:00Z"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bars, err := src.Bars(ctx, domain.Symbol("X.US"), "1d", tt.from, tt.to)
			if err != nil {
				t.Fatal(err)
			}
			if len(bars) != tt.want {
				t.Fatalf("len %d want %d: %+v", len(bars), tt.want, bars)
			}
		})
	}
}

func TestFileSource_Bars_badJSON(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := FileSource{Path: path}.Bars(ctx, domain.Symbol("X.US"), "1d", time.Time{}, time.Time{})
	if err == nil {
		t.Fatal("expected error")
	}
}

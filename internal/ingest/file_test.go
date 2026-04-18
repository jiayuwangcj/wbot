package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	bars, err := src.Bars(ctx, domain.Symbol("X.US"), "1d")
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
	_, err := (FileSource{}).Bars(ctx, domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileSource_Bars_badJSON(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := FileSource{Path: path}.Bars(ctx, domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error")
	}
}

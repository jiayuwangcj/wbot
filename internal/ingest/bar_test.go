package ingest

import (
	"strings"
	"testing"
	"time"
)

func TestValidateBars(t *testing.T) {
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	ts := func(i int) time.Time { return base.Add(time.Duration(i) * 24 * time.Hour) }
	bar := func(i int, o, h, l, c float64) Bar {
		return Bar{Ts: ts(i), Open: o, High: h, Low: l, Close: c, Volume: 1000}
	}
	valid := []Bar{
		bar(0, 100, 101, 99.5, 100.5),
		bar(1, 100.5, 102, 100, 101.25),
		bar(2, 101.25, 103, 101, 102),
	}

	tests := []struct {
		name string
		bars []Bar
		want string // substring of expected error; empty means success
	}{
		{"valid", valid, ""},
		{"empty", []Bar{}, "empty"},
		{"high below low", []Bar{bar(0, 100, 99, 101, 100.5), bar(1, 100.5, 102, 100, 101.25)}, "bar 0"},
		{"high below open", []Bar{bar(0, 100, 101, 99.5, 100.5), bar(1, 100.5, 102, 100, 101.25), bar(2, 105, 104, 101, 102)}, "bar 2"},
		{"high below close", []Bar{bar(0, 100, 101, 99.5, 100.5), bar(1, 100.5, 102, 100, 106)}, "bar 1"},
		{"low above open", []Bar{bar(0, 100, 101, 99.5, 100.5), bar(1, 100.5, 105, 103, 101.25)}, "bar 1"},
		{"low above close", []Bar{bar(0, 100, 101, 99.5, 100.5), bar(1, 100.5, 102, 100, 98)}, "bar 1"},
		{"duplicate ts", []Bar{bar(0, 100, 101, 99.5, 100.5), bar(0, 100.5, 102, 100, 101.25)}, "bar 1"},
		{"out of order ts", []Bar{bar(1, 100, 101, 99.5, 100.5), bar(0, 100.5, 102, 100, 101.25)}, "bar 1"},
		{"zero ts", []Bar{{Open: 100, High: 101, Low: 99.5, Close: 100.5}, bar(1, 100.5, 102, 100, 101.25)}, "bar 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBars(tt.bars)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
			if !strings.HasPrefix(err.Error(), "ingest:") {
				t.Fatalf("error %q missing ingest: prefix", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q missing %q", err, tt.want)
			}
		})
	}
}

package main

import (
	"strings"
	"testing"
)

func TestIngestAccountDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"help", []string{"wbot", "ingest", "account", "-h"}, 0},
		{"bad env", []string{"wbot", "ingest", "account", "-env", "bogus"}, 2},
		{"missing dsn", []string{"wbot", "ingest", "account"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestIngestAccountUsageMentionsEvery(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "account", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	for _, want := range []string{"-every", "account_snapshots", "资产曲线", "-acc-id", "-env"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("ingest account usage missing %q: %s", want, stderr)
		}
	}
}

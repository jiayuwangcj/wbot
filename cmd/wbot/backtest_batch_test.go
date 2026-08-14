package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBacktestParamGroupsSupportsRepeatedAndArrayForms(t *testing.T) {
	groups, err := parseBacktestParamGroups([]string{`{"a":1}`, `[{"a":2},{"a":3}]`})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || groups[0]["a"] != float64(1) || groups[2]["a"] != float64(3) {
		t.Fatalf("groups = %#v; want three ordered objects", groups)
	}
	if _, err := parseBacktestParamGroups([]string{"[]"}); err == nil {
		t.Fatal("empty parameter array returned nil error")
	}
}

func TestBacktestFixedParameterBatchPreservesResultOrder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bars.json")
	data := []byte(`[
{"ts":"2026-08-01T00:00:00Z","open":100,"high":101,"low":99,"close":100,"volume":1000},
{"ts":"2026-08-02T00:00:00Z","open":100,"high":103,"low":99,"close":102,"volume":1000}
]`)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runOutput([]string{"wbot", "backtest", "-file", file, "-strategy", "buy-hold", "-params", "{}", "-params", "{}"})
	if code != 0 {
		t.Fatalf("batch exit code = %d; output = %q", code, out)
	}
	if !strings.Contains(out, "fixed_params=2 workers=8") || strings.Count(out, "params[0] params=") != 1 || strings.Count(out, "params[1] params=") != 1 {
		t.Fatalf("batch output = %q; want two ordered summaries", out)
	}
}

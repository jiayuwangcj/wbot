package wheelrun

import (
	"os"
	"strings"
	"testing"
)

// wheelReviewRules is a single source of truth; doc/WHEEL_STRATEGY.md must
// carry it verbatim so neither side can drift on its own.
func TestWheelReviewRulesSingleSource(t *testing.T) {
	doc, err := os.ReadFile("../../doc/WHEEL_STRATEGY.md")
	if err != nil {
		t.Fatalf("read ../../doc/WHEEL_STRATEGY.md: %v", err)
	}
	want := normalizeSpace(wheelReviewRules)
	if !strings.Contains(normalizeSpace(string(doc)), want) {
		t.Fatalf("doc/WHEEL_STRATEGY.md is missing the wheelReviewRules text; keep the doc section and this constant in sync")
	}
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

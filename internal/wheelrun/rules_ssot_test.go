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

func TestWheelReviewRulesCoverRequiredAuditDimensions(t *testing.T) {
	for _, want := range []string{
		"wheel 区间策略", "当前情况", "expected_gain", "预期", "方向反转检查（硬性项）",
		"inventory_gap", "满仓价—清仓价区间", "min_dte/max_dte", "Bid/Ask",
		"Volume/OI", "资金与库存", "系统性错误", "DATA_BLOCKED", "必须 REJECT",
	} {
		if !strings.Contains(wheelReviewRules, want) {
			t.Errorf("wheelReviewRules missing %q", want)
		}
	}
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

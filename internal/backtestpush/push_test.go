package backtestpush

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/discord"
)

type captureSender struct {
	mu       sync.Mutex
	messages []discord.Message
	fail     int
}

func (s *captureSender) CreateMessage(_ context.Context, _ string, message discord.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	if s.fail > 0 {
		s.fail--
		return errors.New("temporary outage")
	}
	return nil
}

func reportFixture() *backtestreport.Report {
	zero := 0.0
	return &backtestreport.Report{
		ReportID: "bt-HK.00700-42-deadbeef",
		Identity: backtestreport.Identity{
			Symbol: "HK.00700", Currency: "HKD", CapabilityStatus: "DATA_BLOCKED",
			DataWindow: backtestreport.Window{From: "2026-08-12T00:00:00Z", To: "2026-08-14T00:00:00Z"},
		},
		Result: backtestreport.MoneyResult{
			NetReturnPct: nil, MaxDrawdownPct: .03,
			CostModel: backtestreport.CostModel{FeesIncluded: true, TotalFeesAmount: 12.5},
		},
		DataQuality: backtest.DataQualitySummary{TotalBarCount: 20, BlockedBarCount: 20, ValidCoverageRatio: &zero},
		Risk:        []string{"RESEARCH_ONLY:只用于研究", "DATA_BLOCKED:历史事实缺失", "不得解释为收益承诺"},
	}
}

func TestMessageCarriesCoreFieldsAndFullRisk(t *testing.T) {
	report := reportFixture()
	message, err := Message(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Embeds) != 1 || len(message.Embeds[0].Fields) != 8 {
		t.Fatalf("embed shape = %#v", message.Embeds)
	}
	embed := message.Embeds[0]
	for _, risk := range report.Risk {
		if !strings.Contains(embed.Description, risk) {
			t.Fatalf("description %q lost risk %q", embed.Description, risk)
		}
	}
	values := map[string]string{}
	for _, field := range embed.Fields {
		values[field.Name] = field.Value
	}
	for _, name := range []string{"标的", "数据窗口", "权利金净额口径", "已实现口径", "有效覆盖率", "费用", "最大回撤", "停止原因"} {
		if values[name] == "" {
			t.Errorf("missing field %q in %#v", name, values)
		}
	}
	if !strings.Contains(values["权利金净额口径"], "DATA_BLOCKED") || !strings.Contains(values["权利金净额口径"], "N/A") || !strings.Contains(values["已实现口径"], "N/A") {
		t.Fatalf("blocked dual returns = %q / %q", values["权利金净额口径"], values["已实现口径"])
	}
	if len(message.Nonce) != 25 || !message.EnforceNonce {
		t.Fatalf("nonce = %q enforce=%v", message.Nonce, message.EnforceNonce)
	}
}

func TestMessageRendersDualReturnMetrics(t *testing.T) {
	report := reportFixture()
	premium, premiumAnnualized := 0.12, 0.18
	realized, realizedAnnualized := 0.03, 0.04
	report.Identity.CapabilityStatus = "RESEARCH_ONLY"
	report.Result.NetReturnPct = &premium
	report.Result.AnnualizedReturnPct = &premiumAnnualized
	report.Result.RealizedReturnPct = &realized
	report.Result.RealizedAnnualizedReturnPct = &realizedAnnualized
	message, err := Message(report)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	for _, field := range message.Embeds[0].Fields {
		fields[field.Name] = field.Value
	}
	if !strings.Contains(fields["权利金净额口径"], "12.00%") || !strings.Contains(fields["权利金净额口径"], "年化 18.00%") || !strings.Contains(fields["已实现口径"], "3.00%") || !strings.Contains(fields["已实现口径"], "年化 4.00%") {
		t.Fatalf("dual return fields = %#v", fields)
	}
}

func TestPushFailureRetriesAndSuccessIsDurablyIdempotent(t *testing.T) {
	stateDir := t.TempDir()
	report := reportFixture()
	sender := &captureSender{fail: 1}

	if _, err := Push(context.Background(), sender, "channel-1", stateDir, report); err == nil {
		t.Fatal("failed Discord send returned nil")
	}
	marker := filepath.Join(stateDir, report.ReportID+".sent")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("failed send marker error = %v; want not-exist", err)
	}
	status, err := Push(context.Background(), sender, "channel-1", stateDir, report)
	if err != nil || status != StatusSent {
		t.Fatalf("retry = %q, %v", status, err)
	}
	status, err = Push(context.Background(), sender, "channel-1", stateDir, report)
	if err != nil || status != StatusAlreadySent {
		t.Fatalf("duplicate = %q, %v", status, err)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("Discord calls = %d; want failed attempt + successful retry only", len(sender.messages))
	}
	if sender.messages[0].Nonce != sender.messages[1].Nonce {
		t.Fatalf("retry nonces differ: %q / %q", sender.messages[0].Nonce, sender.messages[1].Nonce)
	}
}

func TestMessageRejectsRiskInsteadOfTruncating(t *testing.T) {
	report := reportFixture()
	report.Risk = []string{strings.Repeat("风", 4096)}
	if _, err := Message(report); err == nil || !strings.Contains(err.Error(), "complete risk text") {
		t.Fatalf("long risk error = %v", err)
	}
}

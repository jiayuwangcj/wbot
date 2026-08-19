package llmreview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/wheelstore"
)

type fakeGateReviewer struct {
	result ReviewResult
	err    error
}

func (f fakeGateReviewer) Review(context.Context, ReviewRequest) (ReviewResult, error) {
	return f.result, f.err
}

type fakeGateRepo struct {
	action wheelstore.ActionRecord
}

func (f *fakeGateRepo) AppendAction(_ context.Context, a wheelstore.ActionRecord) (int64, error) {
	f.action = a
	return 1, nil
}

func (f *fakeGateRepo) AppendSignal(context.Context, wheelstore.SignalRecord) (int64, error) {
	return 0, errors.New("unused")
}

func (f *fakeGateRepo) LatestConfig(context.Context, string) (*wheelstore.ConfigRecord, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) ListSignals(context.Context, string, string, string, int) ([]wheelstore.SignalRecord, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) GetSignal(context.Context, int64) (*wheelstore.SignalRecord, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) LatestLLMReview(context.Context, int64) (*wheelstore.ActionRecord, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) LatestAction(context.Context, int64, string) (*wheelstore.ActionRecord, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) HasAction(context.Context, int64, string) (bool, error) {
	return false, errors.New("unused")
}

func (f *fakeGateRepo) ListPendingOrders(context.Context, string) ([]wheelstore.PendingOrder, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) QuerySignalsSince(context.Context, string, int64, int) ([]wheelstore.SignalRecord, error) {
	return nil, errors.New("unused")
}

func (f *fakeGateRepo) MaxSignalID(context.Context) (int64, error) {
	return 0, errors.New("unused")
}

func (f *fakeGateRepo) Dismiss(context.Context, string, time.Time) error {
	return errors.New("unused")
}

func (f *fakeGateRepo) IsDismissed(context.Context, string, time.Time) (bool, error) {
	return false, errors.New("unused")
}

func gateDetails(t *testing.T, failureMode bool, verdict string) map[string]any {
	t.Helper()
	repo := &fakeGateRepo{}
	_, _, err := RecordLLMGate(context.Background(), repo, fakeGateReviewer{result: ReviewResult{
		Verdict: verdict,
		Reasons: []string{"delta mismatch"},
		Notes:   "recheck curve",
	}}, "test-model", GateInput{
		SignalID:                   7,
		UnexpectedVerdictIsFailure: failureMode,
		Request:                    ReviewRequest{Symbol: "HK.TCH"},
	})
	if err != nil {
		t.Fatalf("RecordLLMGate: %v", err)
	}
	return repo.action.Details
}

func TestRecordLLMGateWheelModeKeepsNotes(t *testing.T) {
	for _, verdict := range []string{"APPROVE", "REJECT"} {
		t.Run(verdict, func(t *testing.T) {
			details := gateDetails(t, true, verdict)
			if details["notes"] != "recheck curve" {
				t.Fatalf("details missing notes key: %v", details)
			}
		})
	}
}

func TestRecordLLMGateWheelModeDropsNotesOnUnexpected(t *testing.T) {
	details := gateDetails(t, true, "MAYBE")
	if _, ok := details["notes"]; ok {
		t.Fatalf("unexpected verdict must not store notes: %v", details)
	}
	if details["error"] == nil {
		t.Fatalf("unexpected verdict must set error in failure mode: %v", details)
	}
}

func TestRecordLLMGateHTTPModeKeepsNotes(t *testing.T) {
	details := gateDetails(t, false, "APPROVE")
	if details["notes"] != "recheck curve" {
		t.Fatalf("details missing notes key: %v", details)
	}
}

// TestRecordLLMGateReviewErrorFailsFailedDisposition: 审核请求失败(网络/
// DNS/超时)不是模型裁决,必须落 LLM_REVIEW_FAILED 而非 REJECTED——推送器
// 会把 REJECTED 当「模型拒绝」推卡片,用户看到拒绝实际是基础设施故障
// (2026-08-13: signal 741 DNS 超时被硬记 REJECTED 的教训)。
func TestRecordLLMGateReviewErrorFailsFailedDisposition(t *testing.T) {
	repo := &fakeGateRepo{}
	verdict, disposition, err := RecordLLMGate(context.Background(), repo,
		fakeGateReviewer{err: errors.New("dial tcp: lookup api.deepseek.com: i/o timeout")},
		"test-model", GateInput{
			SignalID:                   7,
			UnexpectedVerdictIsFailure: true,
			Request:                    ReviewRequest{Symbol: "HK.TCH"},
		})
	if err != nil {
		t.Fatalf("RecordLLMGate: %v", err)
	}
	if disposition != "LLM_REVIEW_FAILED" {
		t.Fatalf("disposition = %q; want LLM_REVIEW_FAILED (transient error must not impersonate a model rejection)", disposition)
	}
	if verdict != "REJECT" {
		t.Fatalf("verdict = %q; want REJECT (fail-closed)", verdict)
	}
	if repo.action.Action != "LLM_REVIEW_FAILED" {
		t.Fatalf("persisted action = %q; want LLM_REVIEW_FAILED", repo.action.Action)
	}
	if repo.action.Details["error"] == nil {
		t.Fatalf("details must carry the request error: %v", repo.action.Details)
	}
	// 失败分支(402/5xx/超时)不返回 usage,不得落库。
	if _, ok := repo.action.Details["usage"]; ok {
		t.Fatalf("failed review must not store usage: %v", repo.action.Details)
	}
}

// TestRecordLLMGateStoresUsage: 成功审核(含 REJECT)把 usage/cache_hit_rate 写入
// details,命中率按 hit/(hit+miss) 计算,可 SQL 聚合。
func TestRecordLLMGateStoresUsage(t *testing.T) {
	repo := &fakeGateRepo{}
	_, disposition, err := RecordLLMGate(context.Background(), repo,
		fakeGateReviewer{result: ReviewResult{
			Verdict: "APPROVE", Reasons: []string{"ok"},
			Usage: Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheHitTokens: 80, CacheMissTokens: 20},
		}},
		"test-model", GateInput{SignalID: 7, Request: ReviewRequest{Symbol: "HK.TCH"}})
	if err != nil {
		t.Fatalf("RecordLLMGate: %v", err)
	}
	if disposition != "LLM_REVIEW" {
		t.Fatalf("disposition = %q; want LLM_REVIEW", disposition)
	}
	usage, ok := repo.action.Details["usage"].(map[string]any)
	if !ok {
		t.Fatalf("details missing usage: %v", repo.action.Details)
	}
	if usage["prompt_tokens"] != 100 || usage["completion_tokens"] != 20 ||
		usage["cache_hit_tokens"] != 80 || usage["cache_miss_tokens"] != 20 {
		t.Fatalf("usage = %v", usage)
	}
	rate, ok := usage["cache_hit_rate"].(float64)
	if !ok || rate != 0.8 {
		t.Fatalf("cache_hit_rate = %v (%T); want 0.8", usage["cache_hit_rate"], usage["cache_hit_rate"])
	}
}

// TestRecordLLMGateUsageHitRateNullWhenNoCache: 无 cache 报告(hit+miss=0)时
// cache_hit_rate 必须为 null,不能是 0(0 会被误读成 0% 命中)。
func TestRecordLLMGateUsageHitRateNullWhenNoCache(t *testing.T) {
	repo := &fakeGateRepo{}
	_, _, err := RecordLLMGate(context.Background(), repo,
		fakeGateReviewer{result: ReviewResult{
			Verdict: "APPROVE", Reasons: []string{"ok"},
			Usage: Usage{PromptTokens: 100, CompletionTokens: 20},
		}},
		"test-model", GateInput{SignalID: 7, Request: ReviewRequest{Symbol: "HK.TCH"}})
	if err != nil {
		t.Fatalf("RecordLLMGate: %v", err)
	}
	usage, ok := repo.action.Details["usage"].(map[string]any)
	if !ok {
		t.Fatalf("details missing usage: %v", repo.action.Details)
	}
	if usage["cache_hit_rate"] != nil {
		t.Fatalf("cache_hit_rate = %v; want null when hit+miss=0", usage["cache_hit_rate"])
	}
}

// TestRecordLLMGateUsageHitRateOpenAIOnly: OpenAI-shape(只有 cached_tokens、
// 无 miss 字段)时 usageDetailMap 必须按 prompt-hit 推导 miss,命中率 0.6 而非
// 虚报 1.0(评审 P1——lkeap 实测只回 cached_tokens)。
func TestRecordLLMGateUsageHitRateOpenAIOnly(t *testing.T) {
	repo := &fakeGateRepo{}
	_, _, err := RecordLLMGate(context.Background(), repo,
		fakeGateReviewer{result: ReviewResult{
			Verdict: "APPROVE", Reasons: []string{"ok"},
			Usage: Usage{PromptTokens: 100, CompletionTokens: 20, CacheHitTokens: 60}, // miss 缺失
		}},
		"test-model", GateInput{SignalID: 7, Request: ReviewRequest{Symbol: "HK.TCH"}})
	if err != nil {
		t.Fatalf("RecordLLMGate: %v", err)
	}
	usage, ok := repo.action.Details["usage"].(map[string]any)
	if !ok {
		t.Fatalf("details missing usage: %v", repo.action.Details)
	}
	if usage["cache_miss_tokens"] != 40 {
		t.Fatalf("cache_miss_tokens = %v; want 40 (derived prompt-hit)", usage["cache_miss_tokens"])
	}
	rate, ok := usage["cache_hit_rate"].(float64)
	if !ok || rate != 0.6 {
		t.Fatalf("cache_hit_rate = %v (%T); want 0.6 not 1.0", usage["cache_hit_rate"], usage["cache_hit_rate"])
	}
}

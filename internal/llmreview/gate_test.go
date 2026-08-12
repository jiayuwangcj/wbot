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

package llmreview

import (
	"context"
	"fmt"
	"strings"

	"github.com/jiayu/wbot/internal/wheelstore"
)

// GateInput contains the request and audit summary for one persisted signal.
// RulesText and Signal are deliberately supplied by each strategy because
// their generation semantics differ even though the gate disposition is one.
// UnexpectedVerdictIsFailure preserves the wheel gate's legacy error details.
type GateInput struct {
	SignalID                   int64
	Request                    ReviewRequest
	Summary                    map[string]any
	UnexpectedVerdictIsFailure bool
}

// RecordLLMGate reviews one signal and appends exactly one disposition action.
// Only an explicit APPROVE passes; missing reviewers, review errors and
// unexpected verdicts are recorded as REJECTED. The append error is returned
// separately so callers can retain their existing logging behavior.
func RecordLLMGate(ctx context.Context, repo wheelstore.SignalRepository, reviewer Reviewer, model string, input GateInput) (string, string, error) {
	verdict := "REJECT"
	disposition := "REJECTED"
	actor := "llm:unknown"
	if model != "" {
		actor = "llm:" + model
	}
	details := map[string]any{
		"verdict":       verdict,
		"reasons":       []string{},
		"input_summary": input.Summary,
	}

	if reviewer == nil {
		details["reasons"] = []string{"llm reviewer unavailable (set LLM_BASE_URL, LLM_API_KEY, LLM_MODEL)"}
	} else {
		result, err := reviewer.Review(ctx, input.Request)
		if err != nil {
			reason := err.Error()
			details["reasons"] = []string{reason}
			details["error"] = reason
		} else {
			verdict = strings.ToUpper(strings.TrimSpace(result.Verdict))
			reasons := result.Reasons
			if reasons == nil {
				reasons = []string{}
			}
			details["verdict"] = verdict
			details["reasons"] = reasons
			if result.Notes != "" && !input.UnexpectedVerdictIsFailure {
				details["notes"] = result.Notes
			}
			if verdict != "APPROVE" && verdict != "REJECT" {
				message := fmt.Sprintf("unexpected LLM verdict %s", result.Verdict)
				if input.UnexpectedVerdictIsFailure {
					message = fmt.Sprintf("unexpected LLM verdict %q", result.Verdict)
					details["error"] = message
					details["reasons"] = []string{message}
				} else {
					details["reasons"] = append([]string{message}, reasons...)
				}
				details["verdict"] = "REJECT"
				verdict = "REJECT"
			}
		}
	}

	if verdict == "APPROVE" {
		disposition = "LLM_REVIEW"
	} else {
		verdict = "REJECT"
	}
	if repo == nil {
		return verdict, disposition, fmt.Errorf("record LLM gate: nil signal repository")
	}
	_, err := repo.AppendAction(ctx, wheelstore.ActionRecord{
		SignalID: input.SignalID,
		Action:   disposition,
		Actor:    actor,
		Details:  details,
	})
	if err != nil {
		return verdict, disposition, err
	}
	return verdict, disposition, nil
}

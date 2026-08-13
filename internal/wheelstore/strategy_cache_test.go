package wheelstore

import "testing"

func TestApprovedStrategyCacheRequiresAllGates(t *testing.T) {
	record := StrategyCacheRecord{
		Symbol: "HK.00700", Market: "HK", Currency: "HKD", ConfigVersion: 1,
		ModelVersion: "test", DataWindow: StrategyCacheWindow{From: "2025-01-01T00:00:00Z", To: "2025-12-31T00:00:00Z"},
		ApprovedState: StrategyCacheApprovedCandidate,
		Payload: map[string]any{"schema_version": "strategy-cache-1.0", "approval_gates": map[string]any{
			"data_gate_passed": true, "sample_out_passed": true, "human_approved": false,
		}},
	}
	if _, _, err := validateStrategyCache(record); err == nil {
		t.Fatal("APPROVED_CANDIDATE without human approval was accepted")
	}
	record.Payload["approval_gates"].(map[string]any)["human_approved"] = true
	if _, _, err := validateStrategyCache(record); err != nil {
		t.Fatalf("all approval gates should pass: %v", err)
	}
}

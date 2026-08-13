package main

import (
	"encoding/json"
	"testing"

	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/wheelstore"
)

func TestStrategyCacheRecordConsumesReportWithoutTrajectory(t *testing.T) {
	configVersion := 1
	rep := &backtestreport.Report{
		ReportID: "bt-HK.00883-42-feedface", ReportKind: "es_train",
		Identity:    backtestreport.Identity{Symbol: "HK.00883", Market: "HK", Currency: "HKD", ConfigVersion: &configVersion, CodeVersion: "v1.2.3", CapabilityStatus: "RESEARCH_ONLY", DataWindow: backtestreport.Window{From: "2025-01-01T00:00:00Z", To: "2025-12-31T00:00:00Z"}, Config: backtestreport.ReportConfig{Params: map[string]any{"move_interval_pct": 0.01}}},
		Result:      backtestreport.MoneyResult{BaselineReturnPct: 0.04},
		Candidates:  []backtestreport.Candidate{{Rank: 1, Params: map[string]any{"move_interval_pct": 0.02}, VsBaselinePct: 0.06, Stats: backtestreport.CandidateStats{MedianReturnPct: 0.10, P10ReturnPct: 0.05, P90ReturnPct: 0.14, MedianMaxDrawdownPct: 0.03}}},
		Generations: []backtestreport.Generation{{Generation: 1}}, Trajectory: []backtestreport.TrajectoryStep{{Step: 1}},
	}
	r := strategyCacheRecord(rep, "/tmp/report.json", false)
	if r.ApprovedState != wheelstore.StrategyCacheResearchCandidate {
		t.Fatalf("initial approved_state=%s", r.ApprovedState)
	}
	gates := r.Payload["approval_gates"].(map[string]any)
	if gates["data_gate_passed"] != true || gates["sample_out_passed"] != true || gates["human_approved"] != false {
		t.Fatalf("approval gates=%v", gates)
	}
	b, err := json.Marshal(r.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || !json.Valid(b) {
		t.Fatalf("invalid cache payload: %s", b)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["generations"]; ok {
		t.Fatal("cache payload contains generations")
	}
	if _, ok := payload["trajectory"]; ok {
		t.Fatal("cache payload contains trajectory")
	}
}

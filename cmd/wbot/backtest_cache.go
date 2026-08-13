package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/llmstrategy"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// cacheBacktestReport stores only the bounded evidence projection needed by
// llmstrategy. It does not write watchlist or wheel_configs. humanApproved is
// the deliberately explicit interface reserved for a future operator action;
// CLI -cache always passes false in this first version.
func cacheBacktestReport(ctx context.Context, database *sql.DB, rep *backtestreport.Report, jsonPath string, humanApproved bool) error {
	if database == nil || rep == nil {
		return fmt.Errorf("backtest cache: database and report are required")
	}
	record := strategyCacheRecord(rep, jsonPath, humanApproved)
	if err := wheelstore.New(database).UpsertStrategyCache(ctx, record); err != nil {
		return fmt.Errorf("backtest cache: %w", err)
	}
	return nil
}

func strategyCacheRecord(rep *backtestreport.Report, jsonPath string, humanApproved bool) wheelstore.StrategyCacheRecord {
	configVersion := 0
	if rep.Identity.ConfigVersion != nil {
		configVersion = *rep.Identity.ConfigVersion
	}
	bestParams := rep.Identity.Config.Params
	returnMetrics := map[string]any{
		"net_return_pct":      rep.Result.NetReturnPct,
		"baseline_return_pct": rep.Result.BaselineReturnPct,
		"excess_return_pct":   rep.Result.ExcessReturnPct,
		"max_drawdown_pct":    rep.Result.MaxDrawdownPct,
	}
	confidence := map[string]any{"p10_return_pct": nil, "p90_return_pct": nil}
	sampleOutPassed := false
	if rep.ReportKind == "es_train" && len(rep.Candidates) > 0 {
		best := rep.Candidates[0]
		bestParams = best.Params
		returnMetrics = map[string]any{
			"median_return_pct":       best.Stats.MedianReturnPct,
			"vs_baseline_pct":         best.VsBaselinePct,
			"median_max_drawdown_pct": best.Stats.MedianMaxDrawdownPct,
		}
		confidence = map[string]any{"p10_return_pct": best.Stats.P10ReturnPct, "p90_return_pct": best.Stats.P90ReturnPct}
		// The frozen S5 report only emits candidates that pass its held-out
		// threshold. Re-check the serialized result rather than consuming ES
		// internals, so S6 does not alter the report schema.
		sampleOutPassed = best.Stats.P10ReturnPct > rep.Result.BaselineReturnPct
	}
	dataGatePassed := rep.Identity.CapabilityStatus == "RESEARCH_ONLY"
	payload := map[string]any{
		"schema_version":      llmstrategy.StrategyCachePayloadVersion,
		"best_params":         bestParams,
		"return_metrics":      returnMetrics,
		"confidence_interval": confidence,
		"capability_state":    rep.Identity.CapabilityStatus,
		"report_reference":    map[string]any{"report_id": rep.ReportID, "json_path": jsonPath},
		"approval_gates": map[string]any{
			"data_gate_passed":  dataGatePassed,
			"sample_out_passed": sampleOutPassed,
			"human_approved":    humanApproved,
		},
		"product_state": "RESEARCH_ONLY",
	}
	state := wheelstore.StrategyCacheResearchCandidate
	if dataGatePassed && sampleOutPassed && humanApproved {
		state = wheelstore.StrategyCacheApprovedCandidate
	}
	return wheelstore.StrategyCacheRecord{
		Symbol: rep.Identity.Symbol, Market: rep.Identity.Market, Currency: rep.Identity.Currency,
		ConfigVersion: configVersion, Payload: payload, ModelVersion: rep.Identity.CodeVersion,
		DataWindow:    wheelstore.StrategyCacheWindow{From: rep.Identity.DataWindow.From, To: rep.Identity.DataWindow.To},
		ApprovedState: state,
	}
}

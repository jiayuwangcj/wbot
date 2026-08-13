// Package backtestreport builds and renders the versioned single-run report
// defined by doc/BACKTEST_REPORT.md. JSON is the source of truth; HTML is a
// deterministic projection of the same in-memory value.
package backtestreport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
)

const (
	SchemaVersion           = "1.0"
	ParamsDictionaryVersion = "params-1.0"
	orderAssumption         = "卖出期权,按 Bid 价尝试成交,有效时长=bar 内"
)

type Window struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Identity struct {
	Symbol           string       `json:"symbol"`
	Market           string       `json:"market"`
	Currency         string       `json:"currency"`
	ConfigVersion    int          `json:"config_version"`
	CodeVersion      string       `json:"code_version"`
	DataWindow       Window       `json:"data_window"`
	CapabilityStatus string       `json:"capability_status"`
	BlockedBy        []string     `json:"blocked_by"`
	RunSeed          int64        `json:"run_seed"`
	Config           ReportConfig `json:"config"`
}

type ReportConfig struct {
	Params map[string]any `json:"params"`
}

type MoneyResult struct {
	NetReturnPct           float64       `json:"net_return_pct"`
	NetReturnAmount        float64       `json:"net_return_amount"`
	BaselineReturnPct      float64       `json:"baseline_return_pct"`
	BaselineName           string        `json:"baseline_name"`
	ExcessReturnPct        float64       `json:"excess_return_pct"`
	MaxDrawdownPct         float64       `json:"max_drawdown_pct"`
	TailLossPct            *float64      `json:"tail_loss_pct"`
	AttemptCount           int64         `json:"attempt_count"`
	FillCount              int64         `json:"fill_count"`
	UnfilledCount          int64         `json:"unfilled_count"`
	UnfilledRatio          *float64      `json:"unfilled_ratio"`
	UnfilledModel          UnfilledModel `json:"unfilled_model"`
	CostModel              CostModel     `json:"cost_model"`
	ManualNotExecutedCount int64         `json:"manual_not_executed_count"`
	HardViolations         int64         `json:"hard_violations"`
	StockAssignmentRate    *float64      `json:"stock_assignment_rate"`
}

type UnfilledModel struct {
	ModelKind       string             `json:"model_kind"`
	ModelVersion    string             `json:"model_version"`
	OrderAssumption string             `json:"order_assumption"`
	Components      UnfilledComponents `json:"components"`
}

type UnfilledComponents struct {
	SpreadWeight float64 `json:"spread_weight"`
	VolumeWeight float64 `json:"volume_weight"`
	OIWeight     float64 `json:"oi_weight"`
}

type CostModel struct {
	FeesIncluded       bool   `json:"fees_included"`
	SlippageIncluded   bool   `json:"slippage_included"`
	TaxesIncluded      bool   `json:"taxes_included"`
	AssignmentIncluded bool   `json:"assignment_included"`
	Description        string `json:"description"`
}

type Audit struct {
	InputSnapshotHash       string                 `json:"input_snapshot_hash"`
	ParamsDictionaryVersion string                 `json:"params_dictionary_version"`
	StrategyParamsSnapshot  StrategyParamsSnapshot `json:"strategy_params_snapshot"`
}

type StrategyParamsSnapshot struct {
	MigrationLossy bool            `json:"migration_lossy"`
	OriginalJSON   json.RawMessage `json:"original_json"`
}

type Report struct {
	SchemaVersion string      `json:"schema_version"`
	ReportID      string      `json:"report_id"`
	ReportKind    string      `json:"report_kind"`
	Identity      Identity    `json:"identity"`
	Result        MoneyResult `json:"result"`
	Audit         Audit       `json:"audit"`
	Risk          []string    `json:"risk"`
}

// Input contains the CLI inputs that cannot be recovered from Result. Params
// must already be the complete, canonical, redacted strategy configuration.
type Input struct {
	Symbol            string
	Strategy          string
	Params            map[string]any
	ConfigVersion     int
	CodeVersion       string
	RunSeed           int64
	InitialCash       float64
	FeePerTrade       float64
	Start             time.Time
	End               time.Time
	BaselineReturnPct float64
	SourceHash        string
	MigrationLossy    bool
	OriginalJSON      json.RawMessage
	Result            *backtest.Result
}

func Build(in Input) (*Report, error) {
	if strings.TrimSpace(in.Symbol) == "" {
		return nil, errors.New("backtest report: symbol is required")
	}
	if strings.ContainsAny(in.Symbol, `/\`) {
		return nil, errors.New("backtest report: symbol contains a path separator")
	}
	if in.Result == nil {
		return nil, errors.New("backtest report: result is required")
	}
	u := in.Result.Unfilled
	if u.AttemptCount < 0 || u.FillCount < 0 || u.UnfilledCount < 0 || u.AttemptCount != u.FillCount+u.UnfilledCount {
		return nil, errors.New("backtest report: inconsistent unfilled counts")
	}
	if u.AttemptCount == 0 && u.UnfilledRatio != nil {
		return nil, errors.New("backtest report: zero attempts require a null unfilled ratio")
	}
	if u.AttemptCount > 0 {
		want := float64(u.UnfilledCount) / float64(u.AttemptCount)
		if u.UnfilledRatio == nil || math.Abs(*u.UnfilledRatio-want) > 1e-12 {
			return nil, errors.New("backtest report: unfilled ratio does not match counts")
		}
	}
	if in.InitialCash <= 0 {
		return nil, errors.New("backtest report: initial cash must be positive")
	}
	if strings.TrimSpace(in.SourceHash) == "" {
		return nil, errors.New("backtest report: source hash is required")
	}
	if in.Start.IsZero() || in.End.IsZero() || in.End.Before(in.Start) {
		return nil, errors.New("backtest report: invalid data window")
	}
	if in.ConfigVersion <= 0 {
		in.ConfigVersion = 1
	}
	if in.CodeVersion == "" {
		in.CodeVersion = "unknown"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	capability, blocked := capability(in.Result.Signals)
	market, currency := marketCurrency(in.Symbol)
	snapshot := struct {
		Symbol      string         `json:"symbol"`
		Strategy    string         `json:"strategy"`
		Params      map[string]any `json:"params"`
		RunSeed     int64          `json:"run_seed"`
		InitialCash float64        `json:"initial_cash"`
		Fee         float64        `json:"fee"`
		Start       string         `json:"start"`
		End         string         `json:"end"`
		SourceHash  string         `json:"source_hash,omitempty"`
	}{in.Symbol, in.Strategy, in.Params, effectiveSeed(in.RunSeed), in.InitialCash, in.FeePerTrade,
		rfc3339(in.Start), rfc3339(in.End), in.SourceHash}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("backtest report: marshal input snapshot: %w", err)
	}
	digest := sha256.Sum256(snapshotJSON)
	hash := hex.EncodeToString(digest[:])

	r := &Report{
		SchemaVersion: SchemaVersion,
		ReportID:      fmt.Sprintf("bt-%s-%d-%s", in.Symbol, effectiveSeed(in.RunSeed), hash[:8]),
		ReportKind:    "single_run",
		Identity: Identity{
			Symbol: in.Symbol, Market: market, Currency: currency,
			ConfigVersion: in.ConfigVersion, CodeVersion: in.CodeVersion,
			DataWindow:       Window{From: rfc3339(in.Start), To: rfc3339(in.End)},
			CapabilityStatus: capability, BlockedBy: blocked,
			RunSeed: effectiveSeed(in.RunSeed), Config: ReportConfig{Params: in.Params},
		},
		Result: MoneyResult{
			NetReturnPct: in.Result.TotalReturn, NetReturnAmount: in.Result.Equity - in.InitialCash,
			BaselineReturnPct: in.BaselineReturnPct, BaselineName: "buy-hold",
			ExcessReturnPct: in.Result.TotalReturn - in.BaselineReturnPct,
			MaxDrawdownPct:  in.Result.MaxDrawdown,
			AttemptCount:    in.Result.Unfilled.AttemptCount, FillCount: in.Result.Unfilled.FillCount,
			UnfilledCount: in.Result.Unfilled.UnfilledCount, UnfilledRatio: in.Result.Unfilled.UnfilledRatio,
			UnfilledModel: UnfilledModel{
				ModelKind: in.Result.Unfilled.ModelKind, ModelVersion: in.Result.Unfilled.ModelVersion,
				OrderAssumption: orderAssumption,
				Components:      UnfilledComponents{SpreadWeight: backtest.UnfilledSpreadWeight, VolumeWeight: backtest.UnfilledVolumeWeight, OIWeight: backtest.UnfilledOIWeight},
			},
			CostModel: CostModel{FeesIncluded: true, Description: "费用计入;滑点/税费/指派模型未接入"},
		},
		Audit: Audit{InputSnapshotHash: "sha256-" + hash, ParamsDictionaryVersion: ParamsDictionaryVersion,
			StrategyParamsSnapshot: StrategyParamsSnapshot{MigrationLossy: in.MigrationLossy, OriginalJSON: in.OriginalJSON}},
		Risk: risks(),
	}
	return r, nil
}

func JSON(r *Report) ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("backtest report: marshal JSON: %w", err)
	}
	return append(b, '\n'), nil
}

func HTML(r *Report) ([]byte, error) {
	if r == nil {
		return nil, errors.New("backtest report: nil report")
	}
	details, err := JSON(r)
	if err != nil {
		return nil, err
	}
	data := htmlData{Report: r, Details: string(details), Unfilled: "N/A", StopReason: "单次回测完成"}
	if r.Result.UnfilledRatio != nil {
		data.Unfilled = percent(*r.Result.UnfilledRatio)
	}
	var out bytes.Buffer
	if err := reportTemplate.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("backtest report: render HTML: %w", err)
	}
	return out.Bytes(), nil
}

// Write creates dir and overwrites the deterministic report pair for ReportID.
func Write(dir string, r *Report) (jsonPath, htmlPath string, err error) {
	if r == nil || r.ReportID == "" {
		return "", "", errors.New("backtest report: report id is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("backtest report: create directory: %w", err)
	}
	jsonData, err := JSON(r)
	if err != nil {
		return "", "", err
	}
	htmlData, err := HTML(r)
	if err != nil {
		return "", "", err
	}
	jsonPath = filepath.Join(dir, r.ReportID+".json")
	htmlPath = filepath.Join(dir, r.ReportID+".html")
	if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
		return "", "", fmt.Errorf("backtest report: write JSON: %w", err)
	}
	if err := os.WriteFile(htmlPath, htmlData, 0o644); err != nil {
		return "", "", fmt.Errorf("backtest report: write HTML: %w", err)
	}
	return jsonPath, htmlPath, nil
}

func effectiveSeed(seed int64) int64 {
	if seed == 0 {
		return 42
	}
	return seed
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func marketCurrency(symbol string) (string, string) {
	parts := strings.Split(symbol, ".")
	market := "UNKNOWN"
	if len(parts) > 1 {
		if len(parts[0]) == 2 {
			market = strings.ToUpper(parts[0])
		} else {
			market = strings.ToUpper(parts[len(parts)-1])
		}
	}
	switch market {
	case "HK":
		return market, "HKD"
	case "US":
		return market, "USD"
	case "SH", "SZ", "CN":
		return market, "CNY"
	default:
		return market, "UNKNOWN"
	}
}

func capability(signals []backtest.SignalTrace) (string, []string) {
	set := map[string]struct{}{}
	blocked := false
	for _, signal := range signals {
		if signal.CapabilityStatus == "DATA_BLOCKED" {
			blocked = true
		}
		for _, reason := range signal.BlockedBy {
			if reason != "" {
				set[reason] = struct{}{}
			}
		}
	}
	if !blocked {
		return "RESEARCH_ONLY", nil
	}
	out := make([]string, 0, len(set))
	for reason := range set {
		out = append(out, reason)
	}
	sort.Strings(out)
	return "DATA_BLOCKED", out
}

func risks() []string {
	out := []string{"RESEARCH_ONLY:历史事件数据未解锁,本结果只用于研究,不驱动提醒"}
	out = append(out, "DATA_BLOCKED:成交/指派/人工处置事实缺失,未成交率为启发式估算")
	return append(out, "bar-time replay:非事件级回放,不含逐 quote 成交时序")
}

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
	// 1.3:net_return_*/annualized/gross 改为已实现收益口径(2026-08-14 老板指令,
	// 浮盈浮亏依赖战略参数,评价只看已实现);市值标记留在 window_mark_to_market_*。
	SchemaVersion           = "1.3"
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
	ConfigVersion    *int         `json:"config_version"`
	CodeVersion      string       `json:"code_version"`
	DataWindow       Window       `json:"data_window"`
	Windows          *Windows     `json:"windows,omitempty"`
	CapabilityStatus string       `json:"capability_status"`
	BlockedBy        []string     `json:"blocked_by"`
	RunSeed          int64        `json:"run_seed"`
	Config           ReportConfig `json:"config"`
}

type Windows struct {
	Train Window `json:"train"`
	Valid Window `json:"valid"`
	Test  Window `json:"test"`
}

type ReportConfig struct {
	Params map[string]any `json:"params"`
}

type MoneyResult struct {
	ReturnStatus string `json:"return_status"`
	// net_return_* 是评价口径(schema 1.3):已实现收益 = 权利金 − 平仓成本 +
	// 正股已实现 − 费用,与战略参数无关。市值标记见 window_mark_to_market_*。
	NetReturnPct                *float64      `json:"net_return_pct"`
	NetReturnAmount             *float64      `json:"net_return_amount"`
	FinalEquityAmount           *float64      `json:"final_equity_amount"`
	AnnualizedReturnPct         *float64      `json:"annualized_return_pct"`
	GrossReturnPct              *float64      `json:"gross_return_pct"`
	GrossReturnAmount           *float64      `json:"gross_return_amount"`
	StockMarketValuePct         *float64      `json:"stock_market_value_pct"`
	OptionMarketValuePct        *float64      `json:"option_market_value_pct"`
	HoldingsMarketValuePct      *float64      `json:"holdings_market_value_pct"`
	FinalEquityPct              *float64      `json:"final_equity_pct"`
	MaxStockMarketValuePct      *float64      `json:"max_stock_market_value_pct"`
	MaxPositionPct              *float64      `json:"max_position_pct"`
	WindowMarkToMarketReturnPct *float64      `json:"window_mark_to_market_return_pct"`
	WindowMarkToMarketAmount    *float64      `json:"window_mark_to_market_amount"`
	BaselineReturnPct           float64       `json:"baseline_return_pct"`
	BaselineName                string        `json:"baseline_name"`
	ExcessReturnPct             *float64      `json:"excess_return_pct"`
	MaxDrawdownPct              float64       `json:"max_drawdown_pct"`
	TailLossPct                 *float64      `json:"tail_loss_pct"`
	AttemptCount                int64         `json:"attempt_count"`
	FillCount                   int64         `json:"fill_count"`
	UnfilledCount               int64         `json:"unfilled_count"`
	UnfilledRatio               *float64      `json:"unfilled_ratio"`
	UnfilledModel               UnfilledModel `json:"unfilled_model"`
	CostModel                   CostModel     `json:"cost_model"`
	CostDrag                    CostDrag      `json:"cost_drag"`
	CostDragPct                 float64       `json:"cost_drag_pct"`
	CostDragReturnPct           float64       `json:"cost_drag_return_pct"`
	Attribution                 Attribution   `json:"attribution"`
	ManualNotExecutedCount      int64         `json:"manual_not_executed_count"`
	HardViolations              int64         `json:"hard_violations"`
	StockAssignmentRate         *float64      `json:"stock_assignment_rate"`
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
	FeesIncluded               bool                `json:"fees_included"`
	Legacy                     bool                `json:"legacy"`
	FeePerTrade                float64             `json:"fee_per_trade"`
	OptionFeePerContract       float64             `json:"option_fee_per_contract"`
	StockFeePerLot             float64             `json:"stock_fee_per_lot"`
	LotSize                    int                 `json:"lot_size"`
	TotalFeesAmount            float64             `json:"total_fees_amount"`
	StockFeesAmount            float64             `json:"stock_fees_amount"`
	OptionFeesAmount           float64             `json:"option_fees_amount"`
	ExerciseDeliveryFeesAmount float64             `json:"exercise_delivery_fees_amount"`
	OptionContracts            float64             `json:"option_contracts"`
	StockLots                  float64             `json:"stock_lots"`
	ExerciseDeliveryLots       float64             `json:"exercise_delivery_lots"`
	OptionTradeCount           int64               `json:"option_trade_count"`
	StockTradeCount            int64               `json:"stock_trade_count"`
	ExerciseDeliveryTradeCount int64               `json:"exercise_delivery_trade_count"`
	ChargedTradeCount          int64               `json:"charged_trade_count"`
	Option                     OptionCostBreakdown `json:"option"`
	Stock                      StockCostBreakdown  `json:"stock"`
	ExerciseDelivery           StockCostBreakdown  `json:"exercise_delivery"`
	SlippageIncluded           bool                `json:"slippage_included"`
	TaxesIncluded              bool                `json:"taxes_included"`
	AssignmentIncluded         bool                `json:"assignment_included"`
	Description                string              `json:"description"`
}

// OptionCostBreakdown is the structured per-contract option fee ledger.
type OptionCostBreakdown struct {
	FeePerContract float64 `json:"fee_per_contract"`
	Contracts      float64 `json:"contracts"`
	Amount         float64 `json:"amount"`
	TradeCount     int64   `json:"trade_count"`
}

// StockCostBreakdown is the structured per-lot stock fee ledger. Exercise and
// assignment delivery use the same shape and rate as active stock trades.
type StockCostBreakdown struct {
	FeePerLot  float64 `json:"fee_per_lot"`
	LotSize    int     `json:"lot_size"`
	Lots       float64 `json:"lots"`
	Amount     float64 `json:"amount"`
	TradeCount int64   `json:"trade_count"`
}

// Attribution decomposes the realized P&L into its sources. The identity
// realized_pnl_amount = premium_income_amount − option_close_cost_amount +
// stock_realized_pnl_amount − fees_amount is validated at build time, so a
// report can never show a realized total that disagrees with the terminal
// summary. unfilled_attempt_premium_amount is the opportunity cost of the
// liquidity-heuristic unfilled attempts, never booked into the P&L.
type Attribution struct {
	PremiumIncomeAmount    float64 `json:"premium_income_amount"`
	OptionCloseCostAmount  float64 `json:"option_close_cost_amount"`
	StockRealizedPnLAmount float64 `json:"stock_realized_pnl_amount"`
	FeesAmount             float64 `json:"fees_amount"`
	RealizedPnLAmount      float64 `json:"realized_pnl_amount"`
	UnfilledAttemptPremium float64 `json:"unfilled_attempt_premium_amount"`
	UnfilledAttemptCount   int64   `json:"unfilled_attempt_count"`
}

// CostDrag keeps transaction loss auditable next to the net and gross return.
// ReturnPct is the rate loss against initial_cash; gross return is nil when
// the report cannot establish a complete terminal valuation.
type CostDrag struct {
	TotalFeesAmount   float64  `json:"total_fees_amount"`
	CostDragPct       float64  `json:"cost_drag_pct"`
	CostDragReturnPct float64  `json:"cost_drag_return_pct"`
	GrossReturnPct    *float64 `json:"gross_return_pct"`
	GrossReturnAmount *float64 `json:"gross_return_amount"`
}

type Audit struct {
	InputSnapshotHash       string                 `json:"input_snapshot_hash"`
	ParamsDictionaryVersion string                 `json:"params_dictionary_version"`
	StrategyParamsSnapshot  StrategyParamsSnapshot `json:"strategy_params_snapshot"`
	Reward                  *RewardAudit           `json:"reward,omitempty"`
	SearchSpace             map[string]SearchBound `json:"search_space,omitempty"`
	BaselineChanges         []string               `json:"baseline_changes,omitempty"`
}

type RewardAudit struct {
	FunctionVersion     string        `json:"function_version"`
	Weights             RewardWeights `json:"weights"`
	HardFailureHandling string        `json:"hard_failure_handling"`
}

type RewardWeights struct {
	LambdaDD       float64 `json:"lambda_dd"`
	LambdaTail     float64 `json:"lambda_tail"`
	LambdaTurnover float64 `json:"lambda_turnover"`
}

type SearchBound struct {
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
	Unit        string  `json:"unit"`
	HitBoundary bool    `json:"hit_boundary"`
}

type StrategyParamsSnapshot struct {
	MigrationLossy bool            `json:"migration_lossy"`
	OriginalJSON   json.RawMessage `json:"original_json"`
}

type Report struct {
	SchemaVersion string                      `json:"schema_version"`
	ReportID      string                      `json:"report_id"`
	ReportKind    string                      `json:"report_kind"`
	InitialCash   float64                     `json:"initial_cash"`
	Identity      Identity                    `json:"identity"`
	Train         *Train                      `json:"train,omitempty"`
	Result        MoneyResult                 `json:"result"`
	Terminal      backtest.TerminalSummary    `json:"terminal"`
	DataQuality   backtest.DataQualitySummary `json:"data_quality"`
	Generations   []Generation                `json:"generations,omitempty"`
	Candidates    []Candidate                 `json:"candidates"`
	Audit         Audit                       `json:"audit"`
	Risk          []string                    `json:"risk"`
	Trajectory    []TrajectoryStep            `json:"trajectory,omitempty"`
}

type Train struct {
	Algorithm          string  `json:"algorithm"`
	AlgorithmVersion   string  `json:"algorithm_version"`
	GenerationCount    int     `json:"generation_count"`
	PopulationSize     int     `json:"population_size"`
	EvaluationCount    int     `json:"evaluation_count"`
	Seeds              []int64 `json:"seeds"`
	StopReason         string  `json:"stop_reason"`
	StopDetail         string  `json:"stop_detail"`
	DurationSec        float64 `json:"duration_sec"`
	EvaluationEstimate string  `json:"evaluation_estimate"`
}

type Generation struct {
	Generation           int      `json:"generation"`
	EvaluationCount      int      `json:"evaluation_count"`
	TrainBestReturnPct   float64  `json:"train_best_return_pct"`
	TrainMeanReturnPct   float64  `json:"train_mean_return_pct"`
	TrainMedianReturnPct float64  `json:"train_median_return_pct"`
	TrainStdReturnPct    float64  `json:"train_std_return_pct"`
	HistoryBestReturnPct float64  `json:"history_best_return_pct"`
	ValidBestReturnPct   float64  `json:"valid_best_return_pct"`
	MaxDrawdownPct       float64  `json:"max_drawdown_pct"`
	UnfilledRatio        *float64 `json:"unfilled_ratio"`
	EffectiveTrades      int      `json:"effective_trades"`
	PopulationDispersion float64  `json:"population_dispersion"`
	MutationScale        float64  `json:"mutation_scale"`
	DurationSec          float64  `json:"duration_sec"`
}

type Candidate struct {
	Rank          int             `json:"rank"`
	Params        map[string]any  `json:"params"`
	Stats         CandidateStats  `json:"stats"`
	BoundaryHits  map[string]bool `json:"boundary_hits"`
	VsBaselinePct float64         `json:"vs_baseline_pct"`
}

type CandidateStats struct {
	MedianReturnPct      float64  `json:"median_return_pct"`
	P10ReturnPct         float64  `json:"p10_return_pct"`
	P90ReturnPct         float64  `json:"p90_return_pct"`
	MedianMaxDrawdownPct float64  `json:"median_max_drawdown_pct"`
	MedianUnfilledRatio  *float64 `json:"median_unfilled_ratio"`
}

// TrajectoryStep is the frozen RL-ready envelope. Payloads remain structured
// JSON because the state/candidate feature set evolves independently of the
// report projection while these required semantic slots stay stable.
type TrajectoryStep struct {
	Step         int              `json:"step"`
	DecisionTime string           `json:"decision_time"`
	BarTS        string           `json:"bar_ts"`
	StateBefore  map[string]any   `json:"state_before"`
	Candidates   []map[string]any `json:"candidates"`
	Action       map[string]any   `json:"action"`
	Fill         map[string]any   `json:"fill"`
	StateAfter   map[string]any   `json:"state_after"`
	RewardAtoms  map[string]any   `json:"reward_atoms"`
	Termination  any              `json:"termination"`
	Versions     map[string]any   `json:"versions"`
}

// Input contains the CLI inputs that cannot be recovered from Result. Params
// must already be the complete, canonical, redacted strategy configuration.
type Input struct {
	Symbol            string
	Strategy          string
	Params            map[string]any
	ConfigVersion     *int
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
	if math.Abs(in.Result.Fees.TotalAmount-in.Result.Fees.StockAmount-in.Result.Fees.OptionAmount) > 1e-9 {
		return nil, errors.New("backtest report: fee components do not match total")
	}
	a := in.Result.Attribution
	if math.Abs(a.RealizedPnLAmount-(a.PremiumIncomeAmount-a.OptionCloseCostAmount+a.StockRealizedPnLAmount-a.FeesAmount)) > 1e-6*math.Max(1, math.Abs(a.RealizedPnLAmount)) {
		return nil, errors.New("backtest report: attribution identity does not hold")
	}
	if a.UnfilledAttemptCount != in.Result.Unfilled.UnfilledCount {
		return nil, errors.New("backtest report: attribution unfilled count does not match unfilled stats")
	}
	if strings.TrimSpace(in.SourceHash) == "" {
		return nil, errors.New("backtest report: source hash is required")
	}
	if in.Start.IsZero() || in.End.IsZero() || in.End.Before(in.Start) {
		return nil, errors.New("backtest report: invalid data window")
	}
	if in.ConfigVersion != nil && *in.ConfigVersion <= 0 {
		return nil, errors.New("backtest report: config version must be positive when present")
	}
	if in.CodeVersion == "" {
		in.CodeVersion = "unknown"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	quality := normalizeDataQuality(in.Strategy, in.Result.DataQuality, in.Result.Signals)
	terminal := normalizeTerminal(in.Result, in.InitialCash)
	if err := validateTerminal(terminal, in.InitialCash); err != nil {
		return nil, err
	}
	if quality.TotalBarCount != quality.ReadyBarCount+quality.BlockedBarCount {
		return nil, errors.New("backtest report: data quality bar counts are inconsistent")
	}
	capability, blocked := capability(in.Strategy, in.Result.Signals, quality)
	market, currency := marketCurrency(in.Symbol)
	snapshot := struct {
		Symbol        string         `json:"symbol"`
		Strategy      string         `json:"strategy"`
		Params        map[string]any `json:"params"`
		ConfigVersion *int           `json:"config_version"`
		RunSeed       int64          `json:"run_seed"`
		InitialCash   float64        `json:"initial_cash"`
		Fee           float64        `json:"fee"`
		FeeLegacy     bool           `json:"fee_legacy"`
		FeeOption     float64        `json:"fee_option_per_contract"`
		FeeStock      float64        `json:"fee_stock_per_lot"`
		LotSize       int            `json:"lot_size"`
		Start         string         `json:"start"`
		End           string         `json:"end"`
		SourceHash    string         `json:"source_hash,omitempty"`
	}{in.Symbol, in.Strategy, in.Params, in.ConfigVersion, effectiveSeed(in.RunSeed), in.InitialCash, in.FeePerTrade,
		in.Result.Fees.Legacy, in.Result.Fees.OptionPerContract, in.Result.Fees.StockPerLot, in.Result.Fees.LotSize,
		rfc3339(in.Start), rfc3339(in.End), in.SourceHash}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("backtest report: marshal input snapshot: %w", err)
	}
	digest := sha256.Sum256(snapshotJSON)
	hash := hex.EncodeToString(digest[:])

	windowReturnAmount, windowReturnPct := terminalWindowReturn(terminal, in.InitialCash)
	// 评价口径 = 已实现收益(schema 1.3):net_return_* 只含已实现(权利金 − 平仓 −
	// 正股已实现 − 费用),与战略参数无关;市值标记留在 window_mark_to_market_* 审计。
	realizedReturnAmount, realizedReturnPct := in.Result.RealizedReturnAmount, in.Result.RealizedReturnPct
	var netReturnAmount, netReturnPct, excessReturnPct *float64
	returnStatus := "not_applicable_data_blocked"
	if in.Strategy == "wheel" {
		// wheel 评价口径 = 已实现(schema 1.3):net_return_* 只含已实现,与战略参数
		// 无关;市值标记留在 window_mark_to_market_* 审计。
		if capability == "RESEARCH_ONLY" {
			returnStatus = "research_only"
			netReturnAmount, netReturnPct = &realizedReturnAmount, &realizedReturnPct
		}
	} else if windowReturnAmount != nil && windowReturnPct != nil {
		// 非 wheel 基准(hold/buy-hold 等)无战略参数与期权腿,市值收益即其真实
		// 口径,不参与已实现口径切换(该口径作用于 wheel/ES 评价)。
		returnStatus = "complete"
		netReturnAmount, netReturnPct = windowReturnAmount, windowReturnPct
	}
	if netReturnPct != nil {
		excess := *netReturnPct - in.BaselineReturnPct
		excessReturnPct = &excess
	}
	finalEquityAmount := terminal.FinalEquityAmount
	finalEquityPct := ratioPtr(finalEquityAmount, in.InitialCash)
	stockMarketValuePct := ratioValuePtr(terminal.StockMarketValueAmount, in.InitialCash)
	optionMarketValuePct := ratioPtr(terminal.OptionMarketValueAmount, in.InitialCash)
	holdingsMarketValuePct := ratioPtr(terminal.HoldingsMarketValueAmount, in.InitialCash)
	maxStockMarketValue := in.Result.MaxStockMarketValueAmount
	if maxStockMarketValue == 0 {
		maxStockMarketValue = math.Abs(terminal.StockMarketValueAmount)
	}
	maxPositionAmount := maxStockMarketValue
	if maxInventory, ok := nonNegativeNumber(in.Params["max_inventory"]); ok && finite(terminal.UnderlyingPrice) && terminal.UnderlyingPrice != 0 {
		// Keep the actual observed maximum separately, while max_position_pct
		// follows the product contract: configured maximum inventory at the
		// terminal market price, all divided by initial_cash.
		maxPositionAmount = maxInventory * math.Abs(terminal.UnderlyingPrice)
	}
	maxStockMarketValuePct := ratioValuePtr(maxStockMarketValue, in.InitialCash)
	maxPositionPct := ratioValuePtr(maxPositionAmount, in.InitialCash)
	costDragPct := in.Result.Fees.TotalAmount / in.InitialCash
	costDragReturnPct := costDragPct
	var grossReturnAmount, grossReturnPct *float64
	if netReturnAmount != nil && netReturnPct != nil {
		grossAmount := *netReturnAmount + in.Result.Fees.TotalAmount
		grossPct := *netReturnPct + costDragReturnPct
		grossReturnAmount, grossReturnPct = &grossAmount, &grossPct
	}
	annualizedReturnPct := annualizedReturn(in.Start, in.End, netReturnPct)
	costDrag := CostDrag{TotalFeesAmount: in.Result.Fees.TotalAmount, CostDragPct: costDragPct, CostDragReturnPct: costDragReturnPct, GrossReturnPct: grossReturnPct, GrossReturnAmount: grossReturnAmount}
	r := &Report{
		SchemaVersion: SchemaVersion,
		ReportID:      fmt.Sprintf("bt-%s-%d-%s", in.Symbol, effectiveSeed(in.RunSeed), hash[:8]),
		ReportKind:    "single_run",
		InitialCash:   in.InitialCash,
		Identity: Identity{
			Symbol: in.Symbol, Market: market, Currency: currency,
			ConfigVersion: in.ConfigVersion, CodeVersion: in.CodeVersion,
			DataWindow:       Window{From: rfc3339(in.Start), To: rfc3339(in.End)},
			CapabilityStatus: capability, BlockedBy: blocked,
			RunSeed: effectiveSeed(in.RunSeed), Config: ReportConfig{Params: in.Params},
		},
		Result: MoneyResult{
			ReturnStatus: returnStatus, NetReturnPct: netReturnPct, NetReturnAmount: netReturnAmount,
			FinalEquityAmount: finalEquityAmount, AnnualizedReturnPct: annualizedReturnPct,
			GrossReturnPct: grossReturnPct, GrossReturnAmount: grossReturnAmount,
			StockMarketValuePct: stockMarketValuePct, OptionMarketValuePct: optionMarketValuePct, HoldingsMarketValuePct: holdingsMarketValuePct,
			FinalEquityPct: finalEquityPct, MaxStockMarketValuePct: maxStockMarketValuePct, MaxPositionPct: maxPositionPct,
			WindowMarkToMarketReturnPct: windowReturnPct, WindowMarkToMarketAmount: windowReturnAmount,
			BaselineReturnPct: in.BaselineReturnPct, BaselineName: "buy-hold",
			ExcessReturnPct: excessReturnPct,
			MaxDrawdownPct:  in.Result.MaxDrawdown,
			AttemptCount:    in.Result.Unfilled.AttemptCount, FillCount: in.Result.Unfilled.FillCount,
			UnfilledCount: in.Result.Unfilled.UnfilledCount, UnfilledRatio: in.Result.Unfilled.UnfilledRatio,
			UnfilledModel: UnfilledModel{
				ModelKind: in.Result.Unfilled.ModelKind, ModelVersion: in.Result.Unfilled.ModelVersion,
				OrderAssumption: orderAssumption,
				Components:      UnfilledComponents{SpreadWeight: backtest.UnfilledSpreadWeight, VolumeWeight: backtest.UnfilledVolumeWeight, OIWeight: backtest.UnfilledOIWeight},
			},
			CostModel: CostModel{
				FeesIncluded: in.Result.Fees.Included, Legacy: in.Result.Fees.Legacy, FeePerTrade: in.Result.Fees.PerTrade,
				OptionFeePerContract: in.Result.Fees.OptionPerContract, StockFeePerLot: in.Result.Fees.StockPerLot, LotSize: in.Result.Fees.LotSize,
				TotalFeesAmount: in.Result.Fees.TotalAmount, StockFeesAmount: in.Result.Fees.StockAmount,
				OptionFeesAmount: in.Result.Fees.OptionAmount, ExerciseDeliveryFeesAmount: in.Result.Fees.ExerciseDeliveryAmount,
				OptionContracts: in.Result.Fees.OptionContracts, StockLots: in.Result.Fees.StockLots,
				ExerciseDeliveryLots: in.Result.Fees.ExerciseDeliveryLots,
				OptionTradeCount:     in.Result.Fees.OptionTradeCount, StockTradeCount: in.Result.Fees.StockTradeCount,
				ExerciseDeliveryTradeCount: in.Result.Fees.ExerciseDeliveryTradeCount, ChargedTradeCount: in.Result.Fees.ChargedTradeCount,
				Option:             OptionCostBreakdown{FeePerContract: in.Result.Fees.OptionPerContract, Contracts: in.Result.Fees.OptionContracts, Amount: in.Result.Fees.OptionAmount, TradeCount: in.Result.Fees.OptionTradeCount},
				Stock:              StockCostBreakdown{FeePerLot: in.Result.Fees.StockPerLot, LotSize: in.Result.Fees.LotSize, Lots: in.Result.Fees.StockLots, Amount: in.Result.Fees.StockAmount - in.Result.Fees.ExerciseDeliveryAmount, TradeCount: in.Result.Fees.StockTradeCount},
				ExerciseDelivery:   StockCostBreakdown{FeePerLot: in.Result.Fees.StockPerLot, LotSize: in.Result.Fees.LotSize, Lots: in.Result.Fees.ExerciseDeliveryLots, Amount: in.Result.Fees.ExerciseDeliveryAmount, TradeCount: in.Result.Fees.ExerciseDeliveryTradeCount},
				AssignmentIncluded: !in.Result.Fees.Legacy,
				Description:        feeDescription(in.Result.Fees.Included, in.Result.Fees.Legacy),
			},
			CostDrag: costDrag, CostDragPct: costDragPct, CostDragReturnPct: costDragReturnPct,
			Attribution: Attribution{
				PremiumIncomeAmount:    a.PremiumIncomeAmount,
				OptionCloseCostAmount:  a.OptionCloseCostAmount,
				StockRealizedPnLAmount: a.StockRealizedPnLAmount,
				FeesAmount:             a.FeesAmount,
				RealizedPnLAmount:      a.RealizedPnLAmount,
				UnfilledAttemptPremium: a.UnfilledAttemptPremium,
				UnfilledAttemptCount:   a.UnfilledAttemptCount,
			},
		},
		Terminal:    terminal,
		DataQuality: quality,
		Audit: Audit{InputSnapshotHash: "sha256-" + hash, ParamsDictionaryVersion: ParamsDictionaryVersion,
			StrategyParamsSnapshot: StrategyParamsSnapshot{MigrationLossy: in.MigrationLossy, OriginalJSON: in.OriginalJSON}},
		Risk: risks(containsStringFold(quality.OptionSnapshotSources, "hkex")),
	}
	return r, nil
}

var historicalWheelBlockers = []string{
	"expiry_assignment_events",
	"historical_ask",
	"historical_bid",
	"historical_delta",
	"historical_implied_vol",
	"historical_open_interest",
	"historical_option_snapshots",
	"historical_quote_time",
	"historical_theta",
}

func normalizeDataQuality(strategyName string, q backtest.DataQualitySummary, signals []backtest.SignalTrace) backtest.DataQualitySummary {
	if q.MissingRequiredFieldCounts == nil {
		q.MissingRequiredFieldCounts = map[string]int64{}
	}
	if q.UnderlyingBars == nil {
		q.UnderlyingBars = []backtest.BarProvenance{}
	}
	if q.OptionSnapshotSources == nil {
		q.OptionSnapshotSources = []string{}
	}
	if q.TotalBarCount == 0 && len(signals) > 0 {
		q.TotalBarCount = len(signals)
		for _, signal := range signals {
			if signal.CapabilityStatus == "DATA_BLOCKED" {
				q.BlockedBarCount++
			} else {
				q.ReadyBarCount++
			}
		}
	}
	if q.TotalBarCount > 0 && q.ValidCoverageRatio == nil {
		ratio := float64(q.ReadyBarCount) / float64(q.TotalBarCount)
		q.ValidCoverageRatio = &ratio
	}
	if strategyName != "wheel" {
		if q.Status == "" {
			q.Status = "NOT_APPLICABLE"
		}
		return q
	}
	q.OptionDataRequired = true
	if q.HistoricalOptionCycleComplete == nil {
		complete := false
		q.HistoricalOptionCycleComplete = &complete
	}
	if *q.HistoricalOptionCycleComplete && q.ReadyBarCount > 0 {
		q.Status = "RESEARCH_ONLY"
		// Remove the legacy blanket blockers that predated the official HKEX
		// EOD source. Per-bar gaps and any actual missing fields stay visible.
		legacy := make(map[string]struct{}, len(historicalWheelBlockers)+1)
		legacy["historical_option_cycle"] = struct{}{}
		for _, blocker := range historicalWheelBlockers {
			legacy[blocker] = struct{}{}
		}
		set := make(map[string]struct{}, len(q.BlockedBy))
		for _, blocker := range q.BlockedBy {
			if _, old := legacy[blocker]; blocker != "" && !old {
				set[blocker] = struct{}{}
			}
		}
		q.BlockedBy = sortedSet(set)
		return q
	}
	q.Status = "DATA_BLOCKED"
	set := make(map[string]struct{}, len(q.BlockedBy)+len(historicalWheelBlockers)+1)
	for _, blocker := range q.BlockedBy {
		if blocker != "" {
			set[blocker] = struct{}{}
		}
	}
	for _, blocker := range historicalWheelBlockers {
		set[blocker] = struct{}{}
	}
	if q.SnapshotBatchCount == 0 {
		set["option_quote_snapshots"] = struct{}{}
	}
	q.BlockedBy = sortedSet(set)
	return q
}

func normalizeTerminal(r *backtest.Result, initialCash float64) backtest.TerminalSummary {
	t := r.Terminal
	if t.ValuationStatus != "" {
		return t
	}
	// Compatibility for callers constructing Result directly. The aggregate
	// final equity is known, while holdings and P&L decomposition are not.
	finalEquity := r.Equity
	t.ValuationStatus = backtest.ValuationComplete
	t.SettlementStatus = "NOT_APPLICABLE"
	t.FinalEquityAmount = &finalEquity
	t.EventBasis = "not_applicable"
	t.PnLStatus = "NOT_APPLICABLE"
	return t
}

func terminalWindowReturn(t backtest.TerminalSummary, initialCash float64) (*float64, *float64) {
	if t.ValuationStatus != backtest.ValuationComplete || t.FinalEquityAmount == nil || initialCash <= 0 {
		return nil, nil
	}
	amount := *t.FinalEquityAmount - initialCash
	pct := amount / initialCash
	return &amount, &pct
}

func validateTerminal(t backtest.TerminalSummary, initialCash float64) error {
	if t.AssignmentCount < 0 || t.ShortExpiryCount < t.AssignmentCount || t.ExpiryCount < t.ShortExpiryCount {
		return errors.New("backtest report: terminal expiry/assignment counts are inconsistent")
	}
	if t.ValuationStatus != backtest.ValuationComplete {
		return nil
	}
	if t.FinalEquityAmount != nil && t.HoldingsMarketValueAmount != nil && math.Abs(*t.FinalEquityAmount-t.CashAmount-*t.HoldingsMarketValueAmount) > 1e-8 {
		return errors.New("backtest report: terminal cash and holdings do not match final equity")
	}
	if t.FinalEquityAmount != nil && t.RealizedPnLAmount != nil && t.UnrealizedPnLAmount != nil && math.Abs(*t.RealizedPnLAmount+*t.UnrealizedPnLAmount-(*t.FinalEquityAmount-initialCash)) > 1e-8 {
		return errors.New("backtest report: terminal realized and unrealized P&L do not reconcile")
	}
	return nil
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func feeDescription(included, legacy bool) string {
	if included {
		if legacy {
			return "兼容固定费用已从每笔主动成交扣除;未成交/HOLD/机械到期事件不收费;滑点/税费/行权交割费未接入"
		}
		return "期权按张、正股及行权交割按手扣费;未成交/HOLD/OTM到期事件不收费;滑点/税费未接入"
	}
	return "费用未计入;滑点/税费/行权交割费用模型未接入"
}

func ratioPtr(value *float64, denominator float64) *float64 {
	if value == nil {
		return nil
	}
	return ratioValuePtr(*value, denominator)
}

func ratioValuePtr(value, denominator float64) *float64 {
	if denominator <= 0 {
		return nil
	}
	ratio := value / denominator
	return &ratio
}

func nonNegativeNumber(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, finite(number) && number >= 0
}

func annualizedReturn(start, end time.Time, returnPct *float64) *float64 {
	if returnPct == nil || !end.After(start) || !finite(*returnPct) || 1+*returnPct <= 0 {
		return nil
	}
	days := end.Sub(start).Hours() / 24
	if days <= 0 {
		return nil
	}
	annualized := math.Pow(1+*returnPct, 365/days) - 1
	if !finite(annualized) {
		return nil
	}
	return &annualized
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

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
	data := htmlData{
		Report: r, Details: string(details), Unfilled: "N/A",
		WindowMark:   percentPtr(r.Result.WindowMarkToMarketReturnPct),
		WindowAmount: amountPtr(r.Result.WindowMarkToMarketAmount, r.Identity.Currency),
		Coverage:     percentPtr(r.DataQuality.ValidCoverageRatio),
		Assignment:   percentPtr(r.Terminal.AssignmentRate),
		StopReason:   "单次回测完成",
	}
	if r.Train != nil {
		data.StopReason = r.Train.StopReason
	}
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

func capability(strategyName string, signals []backtest.SignalTrace, quality backtest.DataQualitySummary) (string, []string) {
	set := map[string]struct{}{}
	completeResearch := strategyName == "wheel" && quality.HistoricalOptionCycleComplete != nil && *quality.HistoricalOptionCycleComplete && quality.ReadyBarCount > 0
	blocked := quality.Status == "DATA_BLOCKED" || (strategyName == "wheel" && !completeResearch)
	for _, reason := range quality.BlockedBy {
		if reason != "" {
			set[reason] = struct{}{}
		}
	}
	for _, signal := range signals {
		if signal.CapabilityStatus == "DATA_BLOCKED" && !completeResearch {
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

func risks(hkexProjection bool) []string {
	out := []string{"RESEARCH_ONLY:历史事件数据未解锁,本结果只用于研究,不驱动提醒"}
	if hkexProjection {
		out = append(out, "HKEX EOD:日终结算价投影不是可执行 bid/ask,Delta/Theta 为模型派生")
	}
	out = append(out, "DATA_BLOCKED:成交/指派/人工处置事实缺失,未成交率为启发式估算")
	out = append(out, "窗口末估值变动仅为机械账面 mark,不是可执行收益;真实券商到期/指派字段为 null")
	return append(out, "bar-time replay:非事件级回放,不含逐 quote 成交时序")
}

func containsStringFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

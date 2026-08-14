// Package llmsignal owns the deterministic boundary between an LLM decision
// and the shared persisted signal pipeline.  Model output is untrusted data:
// all hard constraints run before the immutable ALERT record is appended.
package llmsignal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/wheelstore"
)

const (
	DefaultOptionMaxQuantity = 5
	DefaultStockMaxQuantity  = 1000
	DefaultMaxDailySignals   = 5
)

var ErrRejected = errors.New("llmsignal: generation rejected")

type Decision struct {
	Symbol       string  `json:"symbol"`
	Direction    string  `json:"direction"`
	Quantity     int     `json:"quantity"`
	Contract     string  `json:"contract"`
	Strike       float64 `json:"strike"`
	Expiry       string  `json:"expiry"`
	CurrentPrice float64 `json:"current_price"`
	Premium      float64 `json:"premium"`
	Delta        float64 `json:"delta"`
	IV           float64 `json:"iv"`
	OpenInterest int64   `json:"open_interest"`
	Reason       string  `json:"reason"`
	Notes        string  `json:"notes"`
}

type Position struct {
	Symbol string  `json:"symbol"`
	Qty    float64 `json:"qty"`
}

type Context struct {
	Positions       []Position
	CashAvailable   *float64
	Inventory       wheelstore.InventorySnapshot
	ObservedOptions map[string]ObservedOption
	// PendingOrders is the symbol's confirmed-but-unfilled orders. It is a
	// required declaration: nil means "not queried" and is rejected, an empty
	// slice means "queried, none open" (老板指令 2026-08-13: 未成交订单必须
	// 传入策略与审核综合考虑;未明确传入时拒绝)。
	PendingOrders []wheelstore.PendingOrder
}

type ObservedOption struct {
	Strike       float64 `json:"strike"`
	Expiry       string  `json:"expiry"`
	Premium      float64 `json:"premium"`
	Delta        float64 `json:"delta"`
	IV           float64 `json:"iv"`
	OpenInterest int64   `json:"open_interest"`
}

type Policy struct {
	OptionMaxQuantity int
	StockMaxQuantity  int
	MaxDailySignals   int
	LotSize           int
}

func (p Policy) normalized() Policy {
	if p.OptionMaxQuantity <= 0 {
		p.OptionMaxQuantity = DefaultOptionMaxQuantity
	}
	if p.StockMaxQuantity <= 0 {
		p.StockMaxQuantity = DefaultStockMaxQuantity
	}
	if p.MaxDailySignals <= 0 {
		p.MaxDailySignals = DefaultMaxDailySignals
	}
	if p.LotSize <= 0 {
		p.LotSize = 100
	}
	return p
}

type Service struct {
	Store    wheelstore.SignalRepository
	Reviewer llmreview.Reviewer
	Model    string
	Now      func() time.Time
}

type Result struct {
	SignalID    int64
	Verdict     string
	Disposition string
	Decision    Decision
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Submit validates a fully collected decision+account snapshot, appends one
// immutable ALERT, then invokes the additive LLM review gate.
func (s *Service) Submit(ctx context.Context, d Decision, account Context, policy Policy) (Result, error) {
	if s == nil || s.Store == nil {
		return Result{}, errors.New("llmsignal: signal store is required")
	}
	policy = policy.normalized()
	normalized, err := Normalize(d)
	if err != nil {
		return Result{}, err
	}
	if err := s.validate(ctx, normalized, account, policy); err != nil {
		return Result{}, err
	}
	record := buildRecord(normalized, account.Inventory)
	id, err := s.Store.AppendSignal(ctx, record)
	if err != nil {
		return Result{}, fmt.Errorf("llmsignal: append signal: %w", err)
	}
	decision := decisionMap(normalized)
	rules := optionRules
	if isStock(normalized.Direction) {
		rules = stockRules
	}
	verdict, disposition, gateErr := llmreview.RecordLLMGate(ctx, s.Store, s.Reviewer, strings.TrimSpace(s.Model), llmreview.GateInput{
		SignalID: id,
		Request: llmreview.ReviewRequest{
			StrategyConfig: map[string]any{"strategy": "llm", "policy": policy},
			Signal:         decision, Positions: account.Positions, CashAvailable: account.CashAvailable,
			CurrentPrice: normalized.CurrentPrice, RulesText: rules, Symbol: normalized.Symbol,
			Inventory: account.Inventory, ObservedOptions: account.ObservedOptions,
			PendingOrders: account.PendingOrders, AsOf: s.now().UTC().Format(time.RFC3339),
		},
		Summary: map[string]any{"signal_id": id, "decision": decision},
	})
	return Result{SignalID: id, Verdict: verdict, Disposition: disposition, Decision: normalized}, gateErr
}

// RecordGenerationRejection persists deterministic rejection evidence without
// creating an actionable READY/ALERT signal.
func (s *Service) RecordGenerationRejection(ctx context.Context, symbol string, cause error) (int64, error) {
	if s == nil || s.Store == nil {
		return 0, errors.New("llmsignal: signal store is required")
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		symbol = "UNKNOWN"
	}
	reason := "LLM generation rejected"
	if cause != nil {
		reason += ": " + cause.Error()
	}
	return s.Store.AppendSignal(ctx, wheelstore.SignalRecord{
		Symbol: symbol, Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "DATA_BLOCKED",
		BlockedBy: []string{"llm_generation_validation"}, RejectionReasons: []string{reason}, Reason: reason,
		Strategy: "llm",
	})
}

func Normalize(d Decision) (Decision, error) {
	d.Symbol = strings.TrimSpace(d.Symbol)
	d.Direction = strings.ToUpper(strings.TrimSpace(d.Direction))
	d.Contract = strings.TrimSpace(d.Contract)
	d.Expiry = strings.TrimSpace(d.Expiry)
	d.Reason = strings.TrimSpace(d.Reason)
	d.Notes = strings.TrimSpace(d.Notes)
	if d.Symbol == "" {
		return d, reject("symbol is required")
	}
	if d.Direction != "PUT" && d.Direction != "CALL" && d.Direction != "BUY" && d.Direction != "SELL" {
		return d, reject("direction %q unsupported", d.Direction)
	}
	if isStock(d.Direction) {
		d.Contract = d.Symbol
		if d.Premium <= 0 {
			d.Premium = d.CurrentPrice
		}
		return d, nil
	}
	if d.Expiry == "" && d.Contract != "" {
		d.Expiry = ExpiryFromOptionCode(d.Contract)
	}
	if d.Expiry == "" {
		return d, reject("expiry is required; fallback dates are forbidden")
	}
	if d.Contract == "" {
		if d.Strike <= 0 {
			return d, reject("contract or strike is required")
		}
		code, err := SyntheticOptionCode(d.Symbol, d.Direction, d.Strike, d.Expiry)
		if err != nil {
			return d, err
		}
		d.Contract = code
	}
	parsedStrike, side, err := parseOptionCode(d.Contract)
	if err != nil {
		return d, reject("contract: %v", err)
	}
	if d.Strike <= 0 {
		d.Strike = parsedStrike
	}
	if side != d.Direction[:1] {
		return d, reject("contract side does not match direction")
	}
	return d, nil
}

func (s *Service) validate(ctx context.Context, d Decision, account Context, p Policy) error {
	if account.Inventory.CurrentPrice == nil || account.Inventory.ActualInventory == nil || account.Inventory.OptionDeltaStock == nil || account.Inventory.EffectiveInventory == nil || account.Inventory.TargetInventory == nil || account.Inventory.InventoryGap == nil {
		return reject("complete inventory snapshot is required")
	}
	// 挂单声明是强制输入(老板指令 2026-08-13):nil 表示调用方未查询未成交
	// 订单——即使此刻没有挂单也必须显式传空切片,审核才可能综合判断;同
	// 合约同方向已有挂单时新决策构成确定性重复动作,无需交给模型。
	// 方向不同(如挂单 BUY 与决策 SELL)必须留给 LLM 综合判断——正股方向
	// contract 规范化为 symbol,只看合约会把反向决策误判为重复(272 教训)。
	if account.PendingOrders == nil {
		return reject("pending orders snapshot is required")
	}
	for _, po := range account.PendingOrders {
		if po.Contract != "" && po.Contract == d.Contract && po.Direction == d.Direction {
			return reject("contract %q already has a pending order", d.Contract)
		}
	}
	if d.Quantity < 1 {
		return reject("quantity must be >= 1")
	}
	if d.CurrentPrice <= 0 || math.IsNaN(d.CurrentPrice) || math.IsInf(d.CurrentPrice, 0) {
		return reject("current_price must be positive")
	}
	if d.Premium <= 0 || math.IsNaN(d.Premium) || math.IsInf(d.Premium, 0) {
		return reject("premium/limit price must be positive")
	}
	if d.Reason == "" {
		return reject("reason is required")
	}
	stockQty := positionQty(account.Positions, d.Symbol)
	if isStock(d.Direction) {
		if d.Quantity > p.StockMaxQuantity {
			return reject("stock quantity %d exceeds limit %d", d.Quantity, p.StockMaxQuantity)
		}
		if d.Premium/d.CurrentPrice < .5 || d.Premium/d.CurrentPrice > 1.5 {
			return reject("stock limit price is inconsistent with current price")
		}
		if d.Direction == "SELL" && stockQty < float64(d.Quantity) {
			return reject("SELL quantity exceeds owned inventory")
		}
		if d.Direction == "BUY" && (account.CashAvailable == nil || *account.CashAvailable < float64(d.Quantity)*d.Premium) {
			return reject("BUY is not cash covered")
		}
	} else {
		if d.Quantity > p.OptionMaxQuantity {
			return reject("option quantity %d exceeds limit %d", d.Quantity, p.OptionMaxQuantity)
		}
		expiry, err := parseExpiry(d.Expiry)
		if err != nil {
			return reject("expiry: %v", err)
		}
		nowDay := time.Date(s.now().UTC().Year(), s.now().UTC().Month(), s.now().UTC().Day(), 0, 0, 0, 0, time.UTC)
		if expiry.Before(nowDay) {
			return reject("expiry is past")
		}
		contractExpiry := ExpiryFromOptionCode(d.Contract)
		ce, err := parseExpiry(contractExpiry)
		if err != nil || !ce.Equal(expiry) {
			return reject("expiry does not match contract")
		}
		parsedStrike, _, _ := parseOptionCode(d.Contract)
		if math.Abs(parsedStrike-d.Strike) > .0001 {
			return reject("strike does not match contract")
		}
		if d.Strike/d.CurrentPrice < .1 || d.Strike/d.CurrentPrice > 10 {
			return reject("strike is inconsistent with current price")
		}
		if d.Direction == "PUT" && !(d.Delta < 0 && d.Delta >= -1) {
			return reject("PUT delta must be in [-1,0)")
		}
		if d.Direction == "CALL" && !(d.Delta > 0 && d.Delta <= 1) {
			return reject("CALL delta must be in (0,1]")
		}
		if d.IV <= 0 || d.OpenInterest <= 0 {
			return reject("iv and open_interest must be positive")
		}
		if len(account.ObservedOptions) == 0 {
			return reject("collected option snapshot is empty")
		}
		observed, ok := account.ObservedOptions[d.Contract]
		if !ok {
			return reject("contract was not present in the collected option snapshot")
		}
		if len(observed.Expiry) < 10 || len(d.Expiry) < 10 || math.Abs(observed.Strike-d.Strike) > .0001 || observed.Expiry[:10] != d.Expiry[:10] ||
			math.Abs(observed.Premium-d.Premium) > .0001 || math.Abs(observed.Delta-d.Delta) > .0001 ||
			math.Abs(observed.IV-d.IV) > .0001 || observed.OpenInterest != d.OpenInterest {
			return reject("decision fields do not match the collected option snapshot")
		}
		if d.Direction == "PUT" {
			required := float64(d.Quantity*p.LotSize) * d.Strike
			if account.CashAvailable == nil || *account.CashAvailable < required {
				return reject("PUT is not cash secured")
			}
		} else if stockQty < float64(d.Quantity*p.LotSize) {
			return reject("CALL is not covered by stock inventory")
		}
	}
	// Daily quota counts only ALERTs that passed the LLM gate: a rejected
	// ALERT never places an order, so it must not consume the quota (same
	// semantics as wheelrun.dailyOrders; 2026-08-13: 5 rejected ALERTs
	// exhausted the day's quota and blocked a valid generated decision).
	start := time.Date(s.now().UTC().Year(), s.now().UTC().Month(), s.now().UTC().Day(), 0, 0, 0, 0, time.UTC)
	signals, err := s.Store.ListSignals(ctx, d.Symbol, "ALERT", "", 1000)
	if err != nil {
		return reject("daily limit check failed: %v", err)
	}
	n := 0
	for _, sig := range signals {
		if sig.CreatedAt.Before(start) {
			continue
		}
		approved, err := s.Store.HasAction(ctx, sig.ID, "LLM_REVIEW")
		if err != nil {
			continue // best-effort: store failure skips the signal, count stays conservative
		}
		if approved {
			n++
		}
	}
	if n >= p.MaxDailySignals {
		return reject("daily signal limit %d reached", p.MaxDailySignals)
	}
	return nil
}

func buildRecord(d Decision, inv wheelstore.InventorySnapshot) wheelstore.SignalRecord {
	return wheelstore.SignalRecord{Symbol: d.Symbol, Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY", BlockedBy: []string{}, Strategy: "llm", Inventory: inv,
		Candidates: []wheelstore.Candidate{wheelstore.AsCompactCandidate(wheelstore.Candidate{Direction: d.Direction, Quantity: d.Quantity, Accepted: true,
			Quote: &wheelstore.Quote{Symbol: d.Contract, OptionType: strings.ToLower(d.Direction), Strike: d.Strike, Expiry: d.Expiry, Bid: d.Premium, Ask: d.Premium, Last: d.Premium, Delta: d.Delta, ImpliedVol: d.IV, OpenInterest: d.OpenInterest}})},
		Reason: d.Reason}
}

func decisionMap(d Decision) map[string]any {
	return map[string]any{"symbol": d.Symbol, "direction": d.Direction, "quantity": d.Quantity, "contract": d.Contract, "strike": d.Strike, "expiry": d.Expiry, "current_price": d.CurrentPrice, "premium": d.Premium, "delta": d.Delta, "iv": d.IV, "open_interest": d.OpenInterest, "reason": d.Reason}
}
func isStock(direction string) bool { return direction == "BUY" || direction == "SELL" }
func positionQty(ps []Position, symbol string) float64 {
	var n float64
	for _, p := range ps {
		if p.Symbol == symbol {
			n += p.Qty
		}
	}
	return n
}
func reject(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRejected, fmt.Sprintf(format, args...))
}

func parseExpiry(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if len(v) >= 10 {
		v = v[:10]
	}
	return time.Parse("2006-01-02", v)
}

// SyntheticOptionCode refuses missing/malformed expiry; there is no date fallback.
func SyntheticOptionCode(symbol, direction string, strike float64, expiry string) (string, error) {
	t, err := parseExpiry(expiry)
	if err != nil {
		return "", reject("valid expiry is required: %v", err)
	}
	if strike <= 0 {
		return "", reject("strike must be positive")
	}
	return "HK." + optionPrefix(symbol) + t.Format("060102") + direction[:1] + strconv.FormatInt(int64(math.Round(strike*1000)), 10), nil
}

func parseOptionCode(code string) (float64, string, error) {
	rest := strings.TrimPrefix(code, "HK.")
	if rest == code {
		return 0, "", fmt.Errorf("missing HK. prefix")
	}
	idx := strings.LastIndexAny(rest, "CP")
	if idx < 6 || idx+1 >= len(rest) {
		return 0, "", fmt.Errorf("missing C/P marker or strike")
	}
	v, err := strconv.ParseInt(rest[idx+1:], 10, 64)
	if err != nil || v <= 0 {
		return 0, "", fmt.Errorf("bad strike")
	}
	return float64(v) / 1000, rest[idx : idx+1], nil
}

func ExpiryFromOptionCode(code string) string {
	rest := strings.TrimPrefix(code, "HK.")
	if rest == code {
		return ""
	}
	idx := strings.LastIndexAny(rest, "CP")
	if idx < 6 {
		return ""
	}
	t, err := time.Parse("060102", rest[idx-6:idx])
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02T00:00:00Z")
}

func optionPrefix(symbol string) string {
	switch strings.TrimPrefix(strings.TrimSpace(symbol), "HK.") {
	case "00700":
		return "TCH"
	case "00883":
		return "CNC"
	case "09988":
		return "ALB"
	default:
		return "OPT"
	}
}

const optionRules = `审核 LLM 期权决策。确定性代码已检查数量、正价格、合约/到期日/行权价、Delta、资金和库存；你仍须从经济理由、数据一致性和系统性风险独立复核。仅全部通过时 APPROVE。

未成交订单(2026-08-13 老板指令):pending_orders 是本标的确认未成交的挂单列表,空数组表示明确无挂单;若该字段缺失(未提供)必须 REJECT。存在挂单时,必须综合评估新决策是否与挂单构成重复敞口、方向叠加或冲突(如同合约重复动作、同方向叠加仓位),并核对其合约/数量/权利金;认为新决策会放大未成交订单风险时必须 REJECT。确定性代码已拒绝同合约重复挂单,你仍须检查不同合约间的叠加风险。

数据范围(缺失即不存在,不得因缺字段拒绝):current_date 是审核基准时间,到期日 DTE 按它计算;signal 与 observed_options 只提供 strike/expiry/premium(=last 成交参考)/delta/iv/open_interest,无 bid/ask/volume/theta;llm 策略参数范围见 strategy_config.policy(数量上限与 lot),无 max_inventory/DTE 硬参数,合约范围由快照过滤保证;inventory 提供 effective/target/gap 可复核库存一致性。signal.direction 语义:PUT/CALL 即卖出对应期权(期权只卖不买),BUY/SELL 为正股买卖。inventory 的 target 是当前库存快照镜像(target=effective,gap=0),不是策略目标参数,不构成硬约束;方向一致性检查针对与现有持仓相反或造成裸露的动作,不得仅因交易偏离 target 拒绝(2026-08-13 信号 674:备兑卖出 CALL 被以"制造库存缺口"误拒)。`

const stockRules = `审核 LLM 正股决策。确定性代码已检查数量、正限价、现金或库存覆盖；你仍须从经济理由、价格一致性和系统性风险独立复核。仅全部通过时 APPROVE。

未成交订单(2026-08-13 老板指令):pending_orders 是本标的确认未成交的挂单列表,空数组表示明确无挂单;若该字段缺失(未提供)必须 REJECT。存在挂单时,必须综合评估新决策是否与挂单构成重复敞口、方向叠加或冲突,并核对其方向/数量/限价;认为新决策会放大未成交订单风险时必须 REJECT。确定性代码已拒绝同合约重复挂单,你仍须检查不同方向间的叠加风险。

数据范围(缺失即不存在,不得因缺字段拒绝):current_date 是审核基准时间;strategy_config.policy 是 llm 策略全部参数(数量上限),无 max_inventory/DTE 硬参数;inventory 提供 effective/target/gap 可复核库存一致性。inventory 的 target 是当前库存快照镜像(target=effective,gap=0),不是策略目标参数,不构成硬约束;方向一致性检查针对与现有持仓相反或造成裸露的动作,不得仅因交易偏离 target 拒绝。`

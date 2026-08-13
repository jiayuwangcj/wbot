// Package llmstrategy periodically generates decisions for watchlist strategy
// "llm" and feeds the shared llmsignal persistence/review pipeline.
package llmstrategy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/llmsignal"
	"github.com/jiayu/wbot/internal/watchlist"
)

const Model = "deepseek-v4-flash"

type Option struct {
	Contract     string  `json:"contract"`
	Direction    string  `json:"direction"`
	Strike       float64 `json:"strike"`
	Expiry       string  `json:"expiry"`
	Premium      float64 `json:"premium"`
	Delta        float64 `json:"delta"`
	IV           float64 `json:"iv"`
	OpenInterest int64   `json:"open_interest"`
}
type Snapshot struct {
	Symbol           string               `json:"symbol"`
	CurrentPrice     float64              `json:"current_price"`
	CashAvailable    *float64             `json:"cash_available"`
	OptionDeltaStock float64              `json:"option_delta_stock"`
	Positions        []llmsignal.Position `json:"positions"`
	Options          []Option             `json:"options"`
}

type Market interface {
	Snapshot(context.Context, string, map[string]any, time.Time) (Snapshot, error)
}
type Watchlist interface {
	List(context.Context) ([]watchlist.Item, error)
}
type DedupeStore interface {
	HasRecentUndisposedSignal(context.Context, string, time.Time) (bool, error)
}
type Submitter interface {
	Submit(context.Context, llmsignal.Decision, llmsignal.Context, llmsignal.Policy) (llmsignal.Result, error)
	RecordGenerationRejection(context.Context, string, error) (int64, error)
}
type Generator interface {
	Generate(context.Context, Snapshot) (llmsignal.Decision, error)
}

type Runner struct {
	Watchlist  Watchlist
	Dedupe     DedupeStore
	Market     Market
	Generator  Generator
	Submitter  Submitter
	Now        func() time.Time
	MarketOpen func(string, time.Time) bool
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) RunOnce(ctx context.Context) error {
	if r.Watchlist == nil || r.Dedupe == nil || r.Market == nil || r.Generator == nil || r.Submitter == nil {
		return errors.New("llmstrategy: incomplete dependencies")
	}
	items, err := r.Watchlist.List(ctx)
	if err != nil {
		return fmt.Errorf("llmstrategy: watchlist: %w", err)
	}
	now := r.now()
	failed := 0
	total := 0
	for _, it := range items {
		if it.Strategy != "llm" {
			continue
		}
		total++
		if r.MarketOpen != nil && !r.MarketOpen(it.Symbol, now) {
			continue
		}
		pending, err := r.Dedupe.HasRecentUndisposedSignal(ctx, it.Symbol, now.Add(-15*time.Minute))
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "llmstrategy: %s: dedupe: %v\n", it.Symbol, err)
			continue
		}
		if pending {
			continue
		}
		snap, err := r.Market.Snapshot(ctx, it.Symbol, it.Params, now)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "llmstrategy: %s: snapshot: %v\n", it.Symbol, err)
			continue
		}
		decision, err := r.Generator.Generate(ctx, snap)
		if err != nil {
			failed++
			_, _ = r.Submitter.RecordGenerationRejection(ctx, it.Symbol, err)
			fmt.Fprintf(os.Stderr, "llmstrategy: %s: generation: %v\n", it.Symbol, err)
			continue
		}
		// Market fields are authoritative: the model decides direction,
		// quantity and contract only; every price field is injected from the
		// live snapshot, so the model cannot fabricate quotes (老板指令
		// 2026-08-13). A contract missing from the snapshot is rejected here,
		// before Submit, and recorded as a generation rejection.
		decision.Symbol = it.Symbol
		decision.CurrentPrice = snap.CurrentPrice
		if isStockDirection(decision.Direction) {
			decision.Premium = snap.CurrentPrice // 正股限价 = 现价
		} else if o, ok := snapshotOption(snap.Options, decision.Contract); ok {
			decision.Strike, decision.Expiry, decision.Premium, decision.Delta, decision.IV, decision.OpenInterest = o.Strike, o.Expiry, o.Premium, o.Delta, o.IV, o.OpenInterest
		} else {
			failed++
			err := fmt.Errorf("contract %q not in market snapshot", decision.Contract)
			_, _ = r.Submitter.RecordGenerationRejection(ctx, it.Symbol, err)
			fmt.Fprintf(os.Stderr, "llmstrategy: %s: submit: %v\n", it.Symbol, err)
			continue
		}
		account := llmsignal.Context{Positions: snap.Positions, CashAvailable: snap.CashAvailable, ObservedOptions: map[string]llmsignal.ObservedOption{}}
		actual := positionQty(snap.Positions, it.Symbol)
		optionDelta := snap.OptionDeltaStock
		zero := 0.0
		account.Inventory.CurrentPrice = &decision.CurrentPrice
		account.Inventory.ActualInventory = &actual
		account.Inventory.OptionDeltaStock = &optionDelta
		effective := actual + optionDelta
		account.Inventory.EffectiveInventory = &effective
		account.Inventory.TargetInventory = &effective
		account.Inventory.InventoryGap = &zero
		for _, o := range snap.Options {
			account.ObservedOptions[o.Contract] = llmsignal.ObservedOption{Strike: o.Strike, Expiry: o.Expiry, Premium: o.Premium, Delta: o.Delta, IV: o.IV, OpenInterest: o.OpenInterest}
		}
		policy := policyFromParams(it.Params)
		if _, err := r.Submitter.Submit(ctx, decision, account, policy); err != nil {
			failed++
			if errors.Is(err, llmsignal.ErrRejected) {
				_, _ = r.Submitter.RecordGenerationRejection(ctx, it.Symbol, fmt.Errorf("%w; decision=%s", err, compactDecision(decision)))
			}
			fmt.Fprintf(os.Stderr, "llmstrategy: %s: submit: %v\n", it.Symbol, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("llmstrategy: %d of %d bindings failed", failed, total)
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("llmstrategy: interval must be positive")
	}
	run := func() {
		if err := r.RunOnce(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			run()
		}
	}
}

// compactDecision renders the rejected decision for the audit record; without
// it a rejection shows only the validation message and the model's actual
// output is unrecoverable.
func compactDecision(d llmsignal.Decision) string {
	b, err := json.Marshal(d)
	if err != nil {
		return fmt.Sprintf("<unmarshalable: %v>", err)
	}
	s := string(b)
	if len(s) > 800 {
		s = s[:800]
	}
	return s
}

func isStockDirection(d string) bool {
	return d == "BUY" || d == "SELL"
}

func snapshotOption(opts []Option, contract string) (Option, bool) {
	for _, o := range opts {
		if o.Contract == contract {
			return o, true
		}
	}
	return Option{}, false
}

func positionQty(ps []llmsignal.Position, s string) float64 {
	var n float64
	for _, p := range ps {
		if p.Symbol == s {
			n += p.Qty
		}
	}
	return n
}
func intParam(m map[string]any, k string, d int) int {
	if v, ok := m[k].(float64); ok && v > 0 {
		return int(v)
	}
	return d
}
func policyFromParams(m map[string]any) llmsignal.Policy {
	return llmsignal.Policy{OptionMaxQuantity: intParam(m, "option_max_quantity", 5), StockMaxQuantity: intParam(m, "stock_max_quantity", 1000), MaxDailySignals: intParam(m, "max_daily_signals", 5), LotSize: 100}
}

// Client is the fixed-model OpenAI-compatible generation client.  The prompt
// skeleton and JSON contract are immutable; only the snapshot slot varies.
type Client struct {
	baseURL, apiKey string
	http            *http.Client
	tools           ToolExecutor
	maxRounds       int
}

// ToolExecutor is the read-only capability boundary exposed to the model.
// Implementations must only query market and account data; trading or any
// other state-changing operation must never be implemented here.
type ToolExecutor interface {
	Quote(context.Context, string, Snapshot) (any, error)
	OptionChain(context.Context, string, int, int, int, Snapshot) (any, error)
	OptionQuote(context.Context, string, Snapshot) (any, error)
	Account(context.Context, string, Snapshot) (any, error)
}

// ClientOption customizes the agent without changing the Generator interface.
type ClientOption func(*Client)

// WithToolExecutor supplies live read-only quote/account tools. When omitted,
// the client safely serves tool calls from the initial snapshot.
func WithToolExecutor(executor ToolExecutor) ClientOption {
	return func(c *Client) {
		if executor != nil {
			c.tools = executor
		}
	}
}

func NewClient(baseURL, apiKey string, opts ...ClientOption) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("llmstrategy: LLM_BASE_URL and LLM_API_KEY are required")
	}
	c := &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: 180 * time.Second}, tools: snapshotToolExecutor{}, maxRounds: 5}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c, nil
}

const generationPrompt = `你是 wbot 的交易候选生成器。初始输入是完整 snapshot；如果需要补充信息，可以调用系统提供的只读行情/账户工具，绝不能请求下单或任何写操作。最终只能从已观察的真实合约中选择一个，或对正股给出 BUY/SELL。你只负责决策方向、数量与合约选择；所有价格字段(current_price/premium/strike/expiry/delta/iv/open_interest)由系统按行情注入，你无需输出(输出了也会被忽略)。期权仅卖出 PUT/CALL，正股限价由系统设为现价。数量必须大于 0 才可操作。理由必须具体说明现价/行权价、权利金、到期日与风险。不调用工具时，只输出一个严格 JSON 对象，字段为 symbol,direction,quantity,contract,reason,notes。无法形成安全决策时仍输出 JSON，但 quantity=0，让确定性校验拒绝。`

var agentTools = []map[string]any{
	toolDefinition("quote", "查询正股实时报价(现价/涨跌)", map[string]any{"symbol": stringProperty("正股代码")}, []string{"symbol"}),
	toolDefinition("option_chain", "查询期权链(合约、行权价、到期日、bid/ask/last)", map[string]any{"symbol": stringProperty("正股代码"), "min_dte": integerProperty("最小到期天数"), "max_dte": integerProperty("最大到期天数"), "max_strikes": integerProperty("最多行权价数")}, []string{"symbol"}),
	toolDefinition("option_quote", "查询指定期权合约报价(delta/iv/open_interest)", map[string]any{"contract": stringProperty("期权合约代码")}, []string{"contract"}),
	toolDefinition("account", "查询账户资金及指定标的正股/期权持仓", map[string]any{"symbol": stringProperty("正股代码")}, []string{"symbol"}),
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func integerProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": 0}
}
func toolDefinition(name, description string, properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}}
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type agentMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
}

func (c *Client) Generate(ctx context.Context, s Snapshot) (llmsignal.Decision, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return llmsignal.Decision{}, err
	}
	totalCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	messages := []agentMessage{{Role: "system", Content: generationPrompt}, {Role: "user", Content: string(raw)}}
	for round := 0; round < c.maxRounds; round++ {
		message, err := c.complete(totalCtx, messages)
		if err != nil {
			return llmsignal.Decision{}, err
		}
		messages = append(messages, message)
		if len(message.ToolCalls) == 0 {
			var d llmsignal.Decision
			if err := json.Unmarshal([]byte(message.Content), &d); err != nil {
				return d, fmt.Errorf("llmstrategy: decision JSON: %w", err)
			}
			return d, nil
		}
		for _, call := range message.ToolCalls {
			result, toolErr := executeTool(totalCtx, c.tools, call.Function.Name, call.Function.Arguments, s)
			content := marshalToolResult(result, toolErr)
			messages = append(messages, agentMessage{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: content})
		}
	}
	return llmsignal.Decision{}, fmt.Errorf("llmstrategy: agent exceeded %d rounds", c.maxRounds)
}

// complete uses the OpenAI-compatible native tools/tool_calls protocol. This
// is supported by the configured DeepSeek-compatible gateway and avoids a
// second, ambiguous JSON protocol in assistant content.
func (c *Client) complete(ctx context.Context, messages []agentMessage) (agentMessage, error) {
	payload := map[string]any{"model": Model, "messages": messages, "tools": agentTools, "tool_choice": "auto", "response_format": map[string]string{"type": "json_object"}}
	body, _ := json.Marshal(payload)
	roundCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(roundCtx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agentMessage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return agentMessage{}, fmt.Errorf("llmstrategy: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return agentMessage{}, fmt.Errorf("llmstrategy: status %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var chat struct {
		Choices []struct {
			Message agentMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&chat); err != nil {
		return agentMessage{}, fmt.Errorf("llmstrategy: decode: %w", err)
	}
	if len(chat.Choices) == 0 {
		return agentMessage{}, errors.New("llmstrategy: response has no choices")
	}
	return chat.Choices[0].Message, nil
}

func marshalToolResult(result any, err error) string {
	var value any = map[string]any{"ok": true, "result": result}
	if err != nil {
		value = map[string]any{"ok": false, "error": err.Error()}
	}
	b, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return `{"ok":false,"error":"tool result is not JSON serializable"}`
	}
	return string(b)
}

type snapshotToolExecutor struct{}

type toolArguments struct {
	Symbol     string `json:"symbol"`
	Contract   string `json:"contract"`
	MinDTE     int    `json:"min_dte"`
	MaxDTE     int    `json:"max_dte"`
	MaxStrikes int    `json:"max_strikes"`
}

func executeTool(ctx context.Context, executor ToolExecutor, name, arguments string, s Snapshot) (any, error) {
	var args toolArguments
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("invalid %s arguments: %w", name, err)
	}
	switch name {
	case "quote":
		return executor.Quote(ctx, args.Symbol, s)
	case "option_chain":
		return executor.OptionChain(ctx, args.Symbol, args.MinDTE, args.MaxDTE, args.MaxStrikes, s)
	case "option_quote":
		return executor.OptionQuote(ctx, args.Contract, s)
	case "account":
		return executor.Account(ctx, args.Symbol, s)
	default:
		return nil, fmt.Errorf("unknown read-only tool %q", name)
	}
}

func (snapshotToolExecutor) Quote(_ context.Context, symbol string, s Snapshot) (any, error) {
	if symbol != s.Symbol {
		return nil, fmt.Errorf("symbol %q is outside the current snapshot", symbol)
	}
	return map[string]any{"symbol": s.Symbol, "current_price": s.CurrentPrice}, nil
}

func (snapshotToolExecutor) OptionChain(_ context.Context, symbol string, _, _ int, maxStrikes int, s Snapshot) (any, error) {
	if symbol != s.Symbol {
		return nil, fmt.Errorf("symbol %q is outside the current snapshot", symbol)
	}
	opts := s.Options
	if maxStrikes > 0 && len(opts) > maxStrikes {
		opts = opts[:maxStrikes]
	}
	return opts, nil
}

func (snapshotToolExecutor) OptionQuote(_ context.Context, contract string, s Snapshot) (any, error) {
	if option, ok := snapshotOption(s.Options, contract); ok {
		return option, nil
	}
	return nil, fmt.Errorf("contract %q is outside the current snapshot", contract)
}

func (snapshotToolExecutor) Account(_ context.Context, symbol string, s Snapshot) (any, error) {
	if symbol != s.Symbol {
		return nil, fmt.Errorf("symbol %q is outside the current snapshot", symbol)
	}
	positions := make([]llmsignal.Position, 0, len(s.Positions))
	for _, position := range s.Positions {
		if position.Symbol == symbol {
			positions = append(positions, position)
		}
	}
	return map[string]any{"symbol": s.Symbol, "cash_available": s.CashAvailable, "positions": positions, "option_delta_stock": s.OptionDeltaStock}, nil
}

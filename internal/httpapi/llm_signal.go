package httpapi

// LLM 策略信号注入端点:POST /v1/wheel/llm-signal
//
// LLM 策略(watchlist strategy "llm" 的决策入口,2026-08-12 盘中用户指令):
// 由大模型决定发起下单策略——调用方(真实 LLM 决策引擎或盘中验证脚本)把
// 决策结果 POST 到这里,serve 把它落成 wheel_signals ALERT,经过 LLM 审核
// 闸门(APPROVE 才允许推送),再由 telegram scheduler 推送人工确认按钮,
// yes → 模拟盘下单。
//
// 与现有 wheel 策略完全隔离:不改 Evaluate/Validate/runner 任何逻辑;
// 审核规则 llmSignalRules 独立于 wheelReviewRules。APPROVE/REJECT 语义
// 与 wheel 链路一致(fail-closed:审核失败按 REJECTED 记录,不推送)。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// LLMSignalStore is the write surface the LLM-signal endpoint needs: append a
// signal and record the LLM-gate disposition. wheelstore.Store implements it.
type LLMSignalStore interface {
	AppendSignal(ctx context.Context, r wheelstore.SignalRecord) (int64, error)
	AppendAction(ctx context.Context, r wheelstore.ActionRecord) (int64, error)
}

// LLMReviewer audits the injected decision before it can reach Telegram;
// nil (env missing) fails closed: the signal is recorded REJECTED and never
// pushed. llmreview.Client implements it.
type LLMReviewer interface {
	Review(ctx context.Context, req llmreview.ReviewRequest) (llmreview.ReviewResult, error)
}

// LLMSignalPath is the LLM-strategy decision injection endpoint
// (POST /v1/wheel/llm-signal).
const LLMSignalPath = "/v1/wheel/llm-signal"

// llmSignalRules is the audit rule set for LLM-generated decisions. It stays
// deliberately short: the decision arrives already shaped by the model, so the
// gate checks coherence, contract sanity, quantity bounds and data consistency
// instead of re-deriving a strategy. 与 doc/WHEEL_STRATEGY.md 的 LLM 审核规则
// 分开(该规则服务 wheel 区间策略,这里服务 LLM 直出决策)。
const llmSignalRules = `你是 wbot 模拟盘的下单审核闸门。审核一个 LLM 策略信号:该信号由大模型决策引擎直接提出,动作是卖出一个期权合约(收取权利金),人工确认后才会在模拟盘下单。
逐项审核:
1. 方向与语义:direction 必须是 PUT(卖 put 建仓/增强)或 CALL(卖 call 备兑/减仓),与 decision_reason 描述的动作一致;任何矛盾必须 REJECT。
2. 购入理由(硬性项):decision_reason 必须说明购入/卖出该合约的经济理由——现价与行权价的关系(put 行权价接近或低于现价、call 行权价高于现价)、权利金水平、到期日考量;理由缺失、空泛(如"看多""不错")或与方向矛盾必须 REJECT。
3. 合约:strike/expiry/contract 代码必须自洽(到期日合理、行权价与标的现价量级匹配);明显错误代码或过期合约必须 REJECT。
4. 数量:quantity 必须 ≥1,且不超过模拟盘验证规模(>5 张必须 REJECT)。
5. 数据一致性:提供的 premium/delta/iv/oi 若非零必须符号与量级合理(delta 方向正确:put 为负、call 为正);矛盾必须 REJECT。
6. 系统性:字段矛盾、或任何无法核实的情形一律 REJECT,不得以任何理由放宽。
输出 JSON:{"verdict":"APPROVE" 或 "REJECT","reasons":["..."],"notes":"可选"}`

// LLMSignalHandler serves POST /v1/wheel/llm-signal. Body:
//
//	{"symbol":"HK.00700","direction":"PUT","quantity":1,
//	 "contract":"HK.TCH260821P460000","current_price":459,
//	 "premium":11.45,"delta":-0.47,"iv":0.404,"open_interest":249,
//	 "expiry":"2026-08-21T00:00:00Z","reason":"...","notes":"..."}
//
// Only symbol/direction/quantity are required; contract defaults to a
// synthetic code when absent (strike required then). Response:
//
//	{"signal_id":247,"llm_verdict":"APPROVE","approved":true}
func LLMSignalHandler(store LLMSignalStore, reviewer LLMReviewer, model string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(LLMSignalPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
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
			// Optional inventory context (ALERT persistence requires a complete
			// snapshot; absent fields default to no-position/identity so the
			// LLM strategy carries no inventory claim — the llm rules gate does
			// not check the gap, only direction/contract/quantity coherence).
			ActualInventory    *float64 `json:"actual_inventory"`
			OptionDeltaStock   *float64 `json:"option_delta_stock"`
			EffectiveInventory *float64 `json:"effective_inventory"`
			TargetInventory    *float64 `json:"target_inventory"`
			InventoryGap       *float64 `json:"inventory_gap"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "bad JSON body: " + err.Error(), Action: "send a valid llm-signal JSON document"})
			return
		}
		req.Symbol = strings.TrimSpace(req.Symbol)
		req.Direction = strings.ToUpper(strings.TrimSpace(req.Direction))
		req.Contract = strings.TrimSpace(req.Contract)
		if req.Symbol == "" {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "symbol is required", Action: "set symbol, e.g. HK.00700"})
			return
		}
		if req.Direction != "PUT" && req.Direction != "CALL" {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: fmt.Sprintf("direction %q unsupported", req.Direction), Action: "use PUT or CALL"})
			return
		}
		if req.Quantity < 1 {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "quantity must be >= 1", Action: "set quantity to a positive contract count"})
			return
		}
		// Build the candidate quote the telegram alert renderer expects
		// (firstCandidate/alertMessage contract): direction, quantity,
		// accepted and quote{symbol,option_type,strike,expiry,bid,ask,last,
		// delta,implied_vol,open_interest}.
		optType := "put"
		if req.Direction == "CALL" {
			optType = "call"
		}
		code := req.Contract
		strike := req.Strike
		if code == "" {
			if strike <= 0 {
				writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "contract or strike is required", Action: "set the full option code (HK.TCH260821P460000) or strike"})
				return
			}
			code = syntheticOptionCode(req.Symbol, req.Direction, strike, req.Expiry)
		} else if strike <= 0 {
			var err error
			strike, err = strikeFromOptionCode(code)
			if err != nil {
				writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "contract: " + err.Error(), Action: "use the gateway option code shape HK.<PFX><YYMMDD><C|P><strike*1000>"})
				return
			}
		}
		if req.Expiry == "" {
			req.Expiry = expiryFromOptionCode(code)
		}
		decision := map[string]any{
			"symbol":        req.Symbol,
			"direction":     req.Direction,
			"quantity":      req.Quantity,
			"contract":      code,
			"strike":        strike,
			"expiry":        req.Expiry,
			"current_price": req.CurrentPrice,
			"premium":       req.Premium,
			"delta":         req.Delta,
			"iv":            req.IV,
			"open_interest": req.OpenInterest,
			"reason":        req.Reason,
		}
		bid := req.Premium
		ask := req.Premium
		candidate := map[string]any{
			"direction": req.Direction,
			"quantity":  req.Quantity,
			"accepted":  true,
			"quote": map[string]any{
				"symbol":        code,
				"option_type":   optType,
				"strike":        strike,
				"expiry":        req.Expiry,
				"bid":           bid,
				"ask":           ask,
				"last":          req.Premium,
				"delta":         req.Delta,
				"implied_vol":   req.IV,
				"open_interest": req.OpenInterest,
			},
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = fmt.Sprintf("LLM 决策:卖出 %s %s %d 张", req.Symbol, code, req.Quantity)
		}
		record := wheelstore.SignalRecord{
			Symbol:           req.Symbol,
			Action:           "ALERT",
			ConfigVersion:    1,
			CapabilityStatus: "READY",
			BlockedBy:        []string{},
			Inventory: wheelstore.InventorySnapshot{
				CurrentPrice:       fptr(req.CurrentPrice),
				ActualInventory:    req.ActualInventory,
				OptionDeltaStock:   req.OptionDeltaStock,
				EffectiveInventory: req.EffectiveInventory,
				TargetInventory:    req.TargetInventory,
				InventoryGap:       req.InventoryGap,
			},
			Candidates: []map[string]any{candidate},
			Reason:     reason,
		}
		id, err := store.AppendSignal(r.Context(), record)
		if err != nil {
			writeErrorBody(w, http.StatusInternalServerError, errorJSON{Code: "store_error", Message: "append signal: " + err.Error(), Action: "check the wheel_signals table and retry"})
			return
		}
		verdict, disposition := reviewLLMSignal(r.Context(), store, reviewer, model, id, req.Symbol, decision, reason)
		response := map[string]any{
			"signal_id":   id,
			"llm_verdict": verdict,
			"approved":    verdict == "APPROVE",
			"symbol":      req.Symbol,
			"direction":   req.Direction,
			"quantity":    req.Quantity,
			"contract":    code,
			"reason":      reason,
		}
		if req.Notes != "" {
			response["notes"] = req.Notes
		}
		if disposition != "" {
			response["disposition"] = disposition
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	})
	return mux
}

// reviewLLMSignal runs the audit gate and records the disposition. It never
// blocks the response on a missing reviewer: nil reviewer means the signal is
// recorded REJECTED (fail-closed, matching the wheel gate) and reported as
// such. Returns (verdict, disposition) where disposition is the recorded
// wheel_signal_actions action name, "" when not recorded.
func reviewLLMSignal(ctx context.Context, store LLMSignalStore, reviewer LLMReviewer, model string, signalID int64, symbol string, decision map[string]any, reason string) (string, string) {
	verdict := "REJECT"
	disposition := "REJECTED"
	actor := "llm:unknown"
	if model != "" {
		actor = "llm:" + model
	}
	details := map[string]any{
		"verdict":       verdict,
		"reasons":       []string{},
		"input_summary": map[string]any{"signal_id": signalID, "decision": decision},
	}
	if reviewer == nil {
		details["reasons"] = []string{"llm reviewer unavailable (set LLM_BASE_URL, LLM_API_KEY, LLM_MODEL)"}
	} else {
		result, err := reviewer.Review(ctx, llmreview.ReviewRequest{
			StrategyConfig: map[string]any{"strategy": "llm", "quantity": decision["quantity"], "direction": decision["direction"]},
			Signal:         decision,
			Positions:      nil,
			CashAvailable:  nil,
			RulesText:      llmSignalRules,
			Symbol:         symbol,
		})
		if err != nil {
			details["reasons"] = []string{err.Error()}
			details["error"] = err.Error()
		} else {
			verdict = strings.ToUpper(strings.TrimSpace(result.Verdict))
			reasons := result.Reasons
			if reasons == nil {
				reasons = []string{}
			}
			details["verdict"] = verdict
			details["reasons"] = reasons
			if result.Notes != "" {
				details["notes"] = result.Notes
			}
			if verdict != "APPROVE" && verdict != "REJECT" {
				details["verdict"] = "REJECT"
				details["reasons"] = append([]string{"unexpected LLM verdict " + result.Verdict}, reasons...)
				verdict = "REJECT"
			}
		}
	}
	// Only an explicit APPROVE is a pass; anything else stays REJECTED.
	if verdict == "APPROVE" {
		disposition = "LLM_REVIEW"
	} else {
		verdict = "REJECT"
	}
	_, _ = storeAppendAction(ctx, store, wheelstore.ActionRecord{
		SignalID: signalID,
		Action:   disposition,
		Actor:    actor,
		Details:  details,
	})
	return verdict, disposition
}

// storeAppendAction appends one action through the store; the signature is
// split out so the nil-receiver guard reads clearly (the real store never nil
// in production).
func storeAppendAction(ctx context.Context, store LLMSignalStore, action wheelstore.ActionRecord) (int64, error) {
	if store == nil {
		return 0, errors.New("llm-signal: store is nil")
	}
	return store.AppendAction(ctx, action)
}

// syntheticOptionCode builds a gateway-style option code for the common
// 00700/00883/09988 chains when the caller supplies only symbol+strike:
// HK.<pfx><YYMMDD><C|P><strike*1000>. Unknown prefixes fall back to "OPT".
func syntheticOptionCode(symbol, direction string, strike float64, expiry string) string {
	pfx := optionPrefix(symbol)
	ymd := "260821"
	if len(expiry) >= 10 {
		ymd = expiry[2:4] + expiry[5:7] + expiry[8:10]
	} else if len(expiry) == 8 && !strings.Contains(expiry, "-") {
		ymd = expiry
	}
	return "HK." + pfx + ymd + direction[0:1] + strconv.FormatInt(int64(strike*1000), 10)
}

// strikeFromOptionCode parses the trailing numeric field of a gateway option
// code (strike encoded ×1000, e.g. HK.TCH260821P460000 → 460).
func strikeFromOptionCode(code string) (float64, error) {
	// shape: HK.<pfx><YYMMDD><C|P><digits>
	rest := strings.TrimPrefix(code, "HK.")
	if rest == code {
		return 0, fmt.Errorf("missing HK. prefix")
	}
	idx := strings.LastIndexAny(rest, "CP")
	if idx < 0 || idx+1 >= len(rest) {
		return 0, fmt.Errorf("missing C/P side marker")
	}
	num := rest[idx+1:]
	if num == "" {
		return 0, fmt.Errorf("missing strike")
	}
	v, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad strike %q", num)
	}
	return float64(v) / 1000, nil
}

// expiryFromOptionCode extracts YYMMDD (formatted 2006-01-02T00:00:00Z) from a
// gateway option code; empty when unparseable (renderer then shows "-").
func expiryFromOptionCode(code string) string {
	rest := strings.TrimPrefix(code, "HK.")
	if rest == code {
		return ""
	}
	idx := strings.LastIndexAny(rest, "CP")
	if idx < 8 || idx > len(rest)-2 {
		return ""
	}
	ymd := rest[idx-6 : idx]
	if len(ymd) != 6 {
		return ""
	}
	if t, err := time.Parse("060102", ymd); err == nil {
		return t.Format("2006-01-02T00:00:00Z")
	}
	return ""
}

// optionPrefix maps the common HK stock symbols to their option-chain prefix
// (TCH=00700, CNC=00883, ALB=09988); unknown symbols get "OPT" so the
// synthetic code stays parseable.
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

func fptr(v float64) *float64 { return &v }

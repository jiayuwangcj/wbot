package httpapi

// LLM strategy injection is a thin HTTP adapter around internal/llmsignal.
// It collects live quote + account/inventory before the service constructs
// and persists the immutable SignalRecord.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/llmsignal"
	"github.com/jiayu/wbot/internal/wheelstore"
)

const LLMSignalPath = "/v1/wheel/llm-signal"

type llmSignalRequest struct {
	llmsignal.Decision
	ActualInventory    *float64 `json:"actual_inventory"`
	OptionDeltaStock   *float64 `json:"option_delta_stock"`
	EffectiveInventory *float64 `json:"effective_inventory"`
	TargetInventory    *float64 `json:"target_inventory"`
	InventoryGap       *float64 `json:"inventory_gap"`
}

// LLMSignalHandler serves POST /v1/wheel/llm-signal.  liveQuoter is optional
// only for isolated tests; serve always supplies it and its current price wins
// over the untrusted request field.
func LLMSignalHandler(store wheelstore.SignalRepository, reviewer llmreview.Reviewer, model string, accounter FutuAccounter, liveQuoter ...FutuQuoter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(LLMSignalPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req llmSignalRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "bad JSON body: " + err.Error(), Action: "send a valid llm-signal JSON document"})
			return
		}
		req.Symbol = strings.TrimSpace(req.Symbol)
		if req.Symbol == "" {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "symbol is required", Action: "set symbol"})
			return
		}

		// P1-2: acquire market and account state before constructing/persisting.
		if len(liveQuoter) > 0 && liveQuoter[0] != nil {
			qctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			raw, err := liveQuoter[0].Quote(qctx, req.Symbol)
			cancel()
			if err != nil {
				writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "upstream_unavailable", Message: "current quote: " + err.Error(), Action: "retry after the quote gateway recovers"})
				return
			}
			price, err := currentPriceFromQuote(raw)
			if err != nil {
				writeErrorBody(w, http.StatusBadGateway, errorJSON{Code: "upstream_error", Message: err.Error(), Action: "check the quote gateway response"})
				return
			}
			req.CurrentPrice = price
		}
		normalized, err := llmsignal.Normalize(req.Decision)
		if err != nil {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "generation_rejected", Message: err.Error(), Action: "correct the decision fields and retry"})
			return
		}
		req.Decision = normalized

		var account llmsignal.Context
		if accounter == nil {
			writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "account_unavailable", Message: "account context is required", Action: "configure the Futu account gateway"})
			return
		}
		actx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		accountSymbol := req.Symbol
		if req.Direction == "PUT" || req.Direction == "CALL" {
			accountSymbol = req.Contract
		}
		snap, err := accounter.AccountForSymbol(actx, futu.EnvSim, accountSymbol)
		cancel()
		if err != nil {
			writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "account_unavailable", Message: "account context: " + err.Error(), Action: "retry after the trade gateway recovers"})
			return
		}
		cash := snap.Funds.AvailableCash
		account.CashAvailable = &cash
		account.Positions = make([]llmsignal.Position, 0, len(snap.Positions))
		actual := 0.0
		for _, p := range snap.Positions {
			account.Positions = append(account.Positions, llmsignal.Position{Symbol: p.Symbol, Qty: p.Qty})
			if p.Symbol == req.Symbol {
				actual += p.Qty
			}
		}
		// Option accounts do not contain the underlying stock used for covered
		// CALL validation. Merge the stock account before freezing inventory.
		if accountSymbol != req.Symbol {
			stockCtx, stockCancel := context.WithTimeout(r.Context(), 5*time.Second)
			stockSnap, stockErr := accounter.AccountForSymbol(stockCtx, futu.EnvSim, req.Symbol)
			stockCancel()
			if stockErr != nil {
				writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "account_unavailable", Message: "stock inventory context: " + stockErr.Error(), Action: "retry after the trade gateway recovers"})
				return
			}
			for _, p := range stockSnap.Positions {
				account.Positions = append(account.Positions, llmsignal.Position{Symbol: p.Symbol, Qty: p.Qty})
				if p.Symbol == req.Symbol {
					actual += p.Qty
				}
			}
		}
		// Explicit derived values are accepted only after the authoritative
		// actual inventory is known. Existing option positions are valued with
		// the same live option quote source as wheel; missing quote capability
		// fails closed rather than persisting a zero delta fiction.
		optionDelta := 0.0
		account.ObservedOptions = map[string]llmsignal.ObservedOption{}
		if oq, ok := firstOptionQuoter(liveQuoter); ok {
			family := optionFamily(req.Contract)
			var held []string
			qty := map[string]float64{}
			for _, p := range account.Positions {
				if family != "" && strings.HasPrefix(strings.TrimPrefix(p.Symbol, "HK."), family) {
					held = append(held, p.Symbol)
					qty[p.Symbol] += p.Qty
				}
			}
			if req.Direction == "PUT" || req.Direction == "CALL" {
				held = append(held, req.Contract)
			}
			if len(held) > 0 {
				qctx, qcancel := context.WithTimeout(r.Context(), 45*time.Second)
				quotes, qerr := oq.OptionQuotes(qctx, held)
				qcancel()
				if qerr != nil {
					writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "upstream_unavailable", Message: "option inventory quotes: " + qerr.Error(), Action: "retry after option quotes recover"})
					return
				}
				for code, q := range quotes {
					lot := q.LotSize
					if lot <= 0 {
						lot = 100
					}
					optionDelta += qty[code] * q.Delta * float64(lot)
					if code == req.Contract {
						premium := q.Bid
						if premium <= 0 {
							premium = q.Last
						}
						account.ObservedOptions[code] = llmsignal.ObservedOption{Strike: req.Strike, Expiry: req.Expiry, Premium: premium, Delta: q.Delta, IV: q.ImpliedVol, OpenInterest: q.OpenInterest}
					}
				}
			}
		}
		effective := actual + optionDelta
		target := effective
		gap := 0.0
		account.Inventory = wheelstore.InventorySnapshot{CurrentPrice: fptr(req.CurrentPrice), ActualInventory: &actual, OptionDeltaStock: &optionDelta, EffectiveInventory: &effective, TargetInventory: &target, InventoryGap: &gap}

		svc := &llmsignal.Service{Store: store, Reviewer: reviewer, Model: model}
		result, err := svc.Submit(r.Context(), normalized, account, llmsignal.Policy{})
		if err != nil {
			if errors.Is(err, llmsignal.ErrRejected) {
				writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "generation_rejected", Message: err.Error(), Action: "correct the decision fields and retry"})
				return
			}
			writeErrorBody(w, http.StatusInternalServerError, errorJSON{Code: "store_error", Message: err.Error(), Action: "check persistence and retry"})
			return
		}
		resp := map[string]any{"signal_id": result.SignalID, "llm_verdict": result.Verdict, "approved": result.Verdict == "APPROVE", "symbol": result.Decision.Symbol, "direction": result.Decision.Direction, "quantity": result.Decision.Quantity, "contract": result.Decision.Contract, "reason": result.Decision.Reason}
		if result.Decision.Notes != "" {
			resp["notes"] = result.Decision.Notes
		}
		if result.Disposition != "" {
			resp["disposition"] = result.Disposition
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

func currentPriceFromQuote(raw json.RawMessage) (float64, error) {
	var pg struct {
		BasicQotList []struct {
			CurPrice float64 `json:"cur_price"`
		} `json:"basic_qot_list"`
	}
	if err := json.Unmarshal(raw, &pg); err != nil {
		return 0, fmt.Errorf("current quote: bad response: %w", err)
	}
	if len(pg.BasicQotList) == 0 || pg.BasicQotList[0].CurPrice <= 0 {
		return 0, errors.New("current quote: missing positive price")
	}
	return pg.BasicQotList[0].CurPrice, nil
}

func fptr(v float64) *float64 { return &v }

func firstOptionQuoter(q []FutuQuoter) (LLMOptionQuoter, bool) {
	if len(q) == 0 || q[0] == nil {
		return nil, false
	}
	v, ok := q[0].(LLMOptionQuoter)
	return v, ok
}
func optionFamily(code string) string {
	rest := strings.TrimPrefix(code, "HK.")
	idx := strings.LastIndexAny(rest, "CP")
	if idx < 7 {
		return ""
	}
	return rest[:idx-6]
}

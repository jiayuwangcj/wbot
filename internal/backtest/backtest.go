package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// Result summarizes one backtest run; EquityCurve/Trades are the deterministic
// per-bar trace (draft 2026-08-02 S1: same input, same trace).
type Result struct {
	Equity      float64
	TotalReturn float64
	MaxDrawdown float64
	Bars        int
	EquityCurve []EquityPoint
	Trades      []Trade
}

// EquityPoint is one bar's portfolio equity at the bar timestamp.
type EquityPoint struct {
	Ts     time.Time `json:"ts"`
	Equity float64   `json:"equity"`
}

// Trade is one settled trade event: fills at the bar close (stock at close,
// option at per-contract premium), expiry events exercise ITM legs at the
// strike or void OTM legs. Symbol is the option contract code for legs and is
// filled with the underlying symbol by SaveResult for stock trades.
type Trade struct {
	Ts        time.Time `json:"ts"`
	Action    string    `json:"action"` // buy/sell/*-call/*-put/exercise-call/exercise-put/expire-otm
	Symbol    string    `json:"symbol"`
	Size      float64   `json:"size"`  // shares for stock/exercise, contracts for option fills
	Price     float64   `json:"price"` // close, premium, or strike
	CashAfter float64   `json:"cash_after"`
}

// buyTol tolerates one-ulp float error in BuyHold's all-in size (cash/close).
const buyTol = 1e-9

// Run replays bars ascending, calling the strategy once per bar and settling
// trades at the close; no option leg data. details: doc/BACKTEST.md
func Run(ctx context.Context, bars []ingest.Bar, initialCash float64, feePerTrade float64, s Strategy) (*Result, error) {
	return RunOptions(ctx, bars, initialCash, feePerTrade, s, nil)
}

// RunOptions replays bars like Run with an injected option universe (chain +
// per-contract bars); option legs settle mechanically at expiry and Equity
// marks them to their latest option close. nil opts = Run.
func RunOptions(ctx context.Context, bars []ingest.Bar, initialCash float64, feePerTrade float64, s Strategy, opts *OptionsData) (*Result, error) {
	if len(bars) == 0 {
		return nil, errors.New("backtest: empty bars")
	}
	if initialCash <= 0 {
		return nil, errors.New("backtest: initial cash must be > 0")
	}
	if feePerTrade < 0 {
		return nil, errors.New("backtest: negative fee")
	}
	if s == nil {
		return nil, errors.New("backtest: nil strategy")
	}
	if err := ingest.ValidateBars(bars); err != nil {
		return nil, err
	}

	st := &State{
		Cash:     initialCash,
		Options:  map[string]OptionPosition{},
		OptPrice: map[string]float64{},
	}
	if opts != nil {
		st.Chain = opts.Chain
		st.OptBars = opts.Bars
	}
	var (
		peak, maxDD float64
		curve       []EquityPoint
		trades      []Trade
	)
	for i, b := range bars {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		st.Pending = nil
		st.Price = b.Close
		markOptions(st, b.Ts)
		act, size, err := s.OnBar(ctx, b, st)
		if err != nil {
			return nil, fmt.Errorf("backtest: bar %d: strategy: %w", i, err)
		}
		if err := settleAction(st, act, size, b, feePerTrade, &trades); err != nil {
			return nil, fmt.Errorf("backtest: bar %d: %w", i, err)
		}
		settleExpired(st, b.Ts, &trades)
		eq := st.Equity(b.Close)
		curve = append(curve, EquityPoint{Ts: b.Ts, Equity: eq})
		if eq > peak {
			peak = eq
		}
		if peak > 0 && (peak-eq)/peak > maxDD {
			maxDD = (peak - eq) / peak
		}
	}

	final := st.Equity(bars[len(bars)-1].Close)
	return &Result{
		Equity:      final,
		TotalReturn: (final - initialCash) / initialCash,
		MaxDrawdown: maxDD,
		Bars:        len(bars),
		EquityCurve: curve,
		Trades:      trades,
	}, nil
}

// markOptions fills OptPrice with each open leg's latest close at or before ts.
func markOptions(st *State, ts time.Time) {
	for code := range st.Options {
		if price, ok := st.PriceAt(code, ts); ok {
			st.OptPrice[code] = price
		}
	}
}

// settleAction books one bar's trade: stock trades at the close, option trades
// at the pending contract's latest close (size = contracts).
func settleAction(st *State, act Action, size float64, b ingest.Bar, feePerTrade float64, trades *[]Trade) error {
	switch act {
	case ActionHold:
	case ActionBuy:
		if size < 0 || size*b.Close > st.Cash+buyTol {
			return fmt.Errorf("buy %v shares at close %v exceeds cash %v", size, b.Close, st.Cash)
		}
		st.Cash -= size*b.Close + feePerTrade
		st.Position += size
		*trades = append(*trades, Trade{Ts: b.Ts, Action: "buy", Size: size, Price: b.Close, CashAfter: st.Cash})
	case ActionSell:
		if size < 0 || size > st.Position+buyTol {
			return fmt.Errorf("sell %v shares exceeds position %v", size, st.Position)
		}
		st.Cash += size*b.Close - feePerTrade
		st.Position -= size
		*trades = append(*trades, Trade{Ts: b.Ts, Action: "sell", Size: size, Price: b.Close, CashAfter: st.Cash})
	case ActionSellCall, ActionBuyCall, ActionSellPut, ActionBuyPut:
		return settleOptionTrade(st, act, size, b, trades)
	default:
		return fmt.Errorf("unknown action %s", act)
	}
	return nil
}

// settleOptionTrade books one option action: size contracts of the pending
// contract at its latest close, enforcing the CSP cash reserve on short puts.
func settleOptionTrade(st *State, act Action, size float64, b ingest.Bar, trades *[]Trade) error {
	if size <= 0 {
		return fmt.Errorf("%s size %v; want > 0 contracts", act, size)
	}
	p := st.Pending
	if p == nil {
		return fmt.Errorf("%s without a pending option (set State.Pending before the action)", act)
	}
	want := OptionCall
	if act == ActionSellPut || act == ActionBuyPut {
		want = OptionPut
	}
	if p.Kind != want {
		return fmt.Errorf("%s pending kind %q; want %q", act, p.Kind, want)
	}
	if p.Code == "" || p.Strike <= 0 || p.Expiry.IsZero() || p.Lot <= 0 {
		return fmt.Errorf("%s pending option incomplete: code=%q strike=%v expiry=%v lot=%d", act, p.Code, p.Strike, p.Expiry, p.Lot)
	}
	price, ok := st.PriceAt(p.Code, b.Ts)
	if !ok {
		return fmt.Errorf("%s %s: no option price data at or before %s", act, p.Code, b.Ts.Format(time.RFC3339))
	}
	sign := 1.0
	if act == ActionSellCall || act == ActionSellPut {
		sign = -1
	}
	contracts := sign * size
	// Sells receive the premium, buys pay it; direction rides on the sign.
	flow := -contracts * p.AvgPremium
	if flow < 0 && st.Cash+flow < -buyTol {
		return fmt.Errorf("%s %v contracts costs %v; exceeds cash %v", act, size, -flow, st.Cash)
	}
	if act == ActionSellPut {
		reserve := size * float64(p.Lot) * p.Strike
		if st.Cash+flow < reserve {
			return fmt.Errorf("sell-put %v contracts needs cash reserve %v (strike %v x lot %v), have %v",
				size, reserve, p.Strike, p.Lot, st.Cash+flow)
		}
	}
	pos := OptionPosition{
		Code: p.Code, Kind: p.Kind, Strike: p.Strike, Expiry: p.Expiry,
		Lot: p.Lot, Contracts: contracts, AvgPremium: p.AvgPremium,
	}
	if old, ok := st.Options[p.Code]; ok {
		net := old.Contracts*old.AvgPremium + contracts*p.AvgPremium
		pos.Contracts += old.Contracts
		if pos.Contracts != 0 {
			pos.AvgPremium = net / pos.Contracts
		}
	}
	st.Cash += flow
	*trades = append(*trades, Trade{Ts: b.Ts, Action: act.String(), Symbol: p.Code, Size: size, Price: p.AvgPremium, CashAfter: st.Cash})
	if pos.Contracts == 0 {
		delete(st.Options, p.Code)
	} else {
		st.Options[p.Code] = pos
	}
	st.OptPrice[p.Code] = price
	st.Pending = nil
	return nil
}

// settleExpired exercises ITM legs (call: shares out at strike, put: shares in
// at strike) and voids OTM legs once their expiry date has passed; the leg is
// always removed. ITM exercise and OTM void both land in the trade ledger.
func settleExpired(st *State, ts time.Time, trades *[]Trade) {
	for code, p := range st.Options {
		if ts.Before(p.Expiry) {
			continue
		}
		shares := p.Contracts * float64(p.Lot)
		switch p.Kind {
		case OptionCall:
			if st.Price > p.Strike {
				st.Position += shares
				st.Cash -= shares * p.Strike
				*trades = append(*trades, Trade{Ts: ts, Action: "exercise-call", Symbol: code, Size: shares, Price: p.Strike, CashAfter: st.Cash})
			} else {
				*trades = append(*trades, Trade{Ts: ts, Action: "expire-otm", Symbol: code, CashAfter: st.Cash})
			}
		case OptionPut:
			if st.Price < p.Strike {
				st.Position -= shares
				st.Cash += shares * p.Strike
				*trades = append(*trades, Trade{Ts: ts, Action: "exercise-put", Symbol: code, Size: shares, Price: p.Strike, CashAfter: st.Cash})
			} else {
				*trades = append(*trades, Trade{Ts: ts, Action: "expire-otm", Symbol: code, CashAfter: st.Cash})
			}
		}
		delete(st.Options, code)
	}
}

// barRecord is the JSON wire format of one bar, matching `ingest bars -json`.
type barRecord struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

// ParseBars parses a JSON array of bars in `ingest bars -json` format; Run checks sanity/order.
func ParseBars(data []byte) ([]ingest.Bar, error) {
	var recs []barRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("backtest: parse bars: json: %w", err)
	}
	out := make([]ingest.Bar, 0, len(recs))
	for i, r := range recs {
		ts, err := time.Parse(time.RFC3339Nano, r.Ts)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, r.Ts)
			if err != nil {
				return nil, fmt.Errorf("backtest: parse bars: record %d ts: %w", i, err)
			}
		}
		out = append(out, ingest.Bar{
			Ts: ts.UTC(), Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return out, nil
}

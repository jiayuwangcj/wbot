package strategy

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
)

// base holds the shared validated params of the option strategies.
type base struct {
	strikePctOTM   float64
	expiryRule     string
	daysToExpiry   float64
	feePerContract float64
	lot            int
}

// baseFrom builds base from a validated params map.
func baseFrom(p map[string]any) base {
	return base{
		strikePctOTM:   p["strike_pct_otm"].(float64),
		expiryRule:     p["expiry_rule"].(string),
		daysToExpiry:   p["days_to_expiry"].(float64),
		feePerContract: p["fee_per_contract"].(float64),
		lot:            int(math.Round(p["lot_size"].(float64))),
	}
}

// hasShort reports an open short leg of kind (the strategy's own position).
func hasShort(st *backtest.State, kind backtest.OptionKind) bool {
	for _, p := range st.Options {
		if p.Kind == kind && p.Contracts < 0 {
			return true
		}
	}
	return false
}

// pick returns the best contract of kind on the selected expiry: strike
// nearest to price×(1∓pct) (tie: lower), only contracts with price data; ok is
// false when the chain offers nothing usable.
func (s *base) pick(bar ingest.Bar, st *backtest.State, kind backtest.OptionKind) (backtest.OptionContract, float64, bool) {
	expiry, ok := s.pickExpiry(bar.Ts, st)
	if !ok {
		return backtest.OptionContract{}, 0, false
	}
	target := bar.Close * (1 + s.strikePctOTM)
	if kind == backtest.OptionPut {
		target = bar.Close * (1 - s.strikePctOTM)
	}
	var best backtest.OptionContract
	var bestPrice float64
	found := false
	for _, c := range st.Chain {
		if c.Kind != kind || !c.Expiry.Equal(expiry) {
			continue
		}
		price, has := st.PriceAt(c.Code, bar.Ts)
		if !has {
			continue
		}
		if !found || closer(c.Strike, best.Strike, target) {
			best, bestPrice, found = c, price, true
		}
	}
	return best, bestPrice, found
}

// pickExpiry selects the expiry per expiry_rule: next_expiry = earliest future
// expiry; days = closest to days_to_expiry days ahead (tie: earlier).
func (s *base) pickExpiry(ts time.Time, st *backtest.State) (time.Time, bool) {
	var best time.Time
	var bestDiff float64
	found := false
	for _, c := range st.Chain {
		if !c.Expiry.After(ts) {
			continue
		}
		switch s.expiryRule {
		case "next_expiry":
			if !found || c.Expiry.Before(best) {
				best, found = c.Expiry, true
			}
		case "days":
			diff := math.Abs(daysUntil(c.Expiry, ts) - s.daysToExpiry)
			if !found || diff < bestDiff || (diff == bestDiff && c.Expiry.Before(best)) {
				best, bestDiff, found = c.Expiry, diff, true
			}
		}
	}
	return best, found
}

// daysUntil returns whole UTC-midnight days from ts to expiry.
func daysUntil(expiry, ts time.Time) float64 {
	e := time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
	return e.Sub(t).Hours() / 24
}

// closer reports whether strike a is nearer to target than b (tie: lower).
func closer(a, b, target float64) bool {
	da, db := math.Abs(a-target), math.Abs(b-target)
	if da != db {
		return da < db
	}
	return a < b
}

// premium returns the per-contract premium at price minus the contract fee.
func (s *base) premium(price float64) float64 {
	return price*float64(s.lot) - s.feePerContract
}

// CoveredCall holds lot shares of the underlying and sells one call per cycle,
// buying back the lot when it was exercised away. details: doc/BACKTEST.md
type CoveredCall struct {
	base
}

// OnBar: cover the lot, hold while a short call is open, else sell a new one.
func (s *CoveredCall) OnBar(_ context.Context, bar ingest.Bar, st *backtest.State) (backtest.Action, float64, error) {
	if st.Position < float64(s.lot) {
		return backtest.ActionBuy, float64(s.lot) - st.Position, nil
	}
	if hasShort(st, backtest.OptionCall) {
		return backtest.ActionHold, 0, nil
	}
	cc, price, ok := s.pick(bar, st, backtest.OptionCall)
	if !ok {
		return backtest.ActionHold, 0, nil
	}
	st.Pending = &backtest.OptionPosition{
		Code: cc.Code, Kind: cc.Kind, Strike: cc.Strike, Expiry: cc.Expiry,
		Lot: s.lot, AvgPremium: s.premium(price),
	}
	return backtest.ActionSellCall, 1, nil
}

// CashSecuredPut sells puts backed by cash (cash_reserve×strike×lot per
// contract at open); assigned stock is unwound at the next bar's close.
type CashSecuredPut struct {
	base
	cashReserve float64
}

// OnBar: unwind assigned stock, hold while a short put is open, else sell puts
// sized by the cash reserve; errors when cash cannot secure even one contract.
func (s *CashSecuredPut) OnBar(_ context.Context, bar ingest.Bar, st *backtest.State) (backtest.Action, float64, error) {
	if st.Position > 0 {
		return backtest.ActionSell, st.Position, nil
	}
	if hasShort(st, backtest.OptionPut) {
		return backtest.ActionHold, 0, nil
	}
	pp, price, ok := s.pick(bar, st, backtest.OptionPut)
	if !ok {
		return backtest.ActionHold, 0, nil
	}
	collateral := pp.Strike * float64(s.lot) * s.cashReserve
	contracts := int64(st.Cash / collateral)
	if contracts < 1 {
		return 0, 0, fmt.Errorf("cash-secured-put: cash %v cannot secure 1 contract (collateral %v at strike %v)",
			st.Cash, collateral, pp.Strike)
	}
	st.Pending = &backtest.OptionPosition{
		Code: pp.Code, Kind: pp.Kind, Strike: pp.Strike, Expiry: pp.Expiry,
		Lot: s.lot, AvgPremium: s.premium(price),
	}
	return backtest.ActionSellPut, float64(contracts), nil
}

package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/httpapi"
	"github.com/jiayu/wbot/internal/llmsignal"
	"github.com/jiayu/wbot/internal/llmstrategy"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/wheelstore"
)

type llmWatchlist struct{ db *sql.DB }

func (w llmWatchlist) List(ctx context.Context) ([]watchlist.Item, error) {
	return watchlist.List(ctx, w.db)
}

type llmMarket struct {
	quoter   futuQuoter
	client   *futu.Client
	accounts httpapi.FutuAccounter
}

func (m llmMarket) Snapshot(ctx context.Context, symbol string, params map[string]any, now time.Time) (llmstrategy.Snapshot, error) {
	price, err := m.quoter.Quote(ctx, symbol)
	if err != nil {
		return llmstrategy.Snapshot{}, err
	}
	if price <= 0 {
		return llmstrategy.Snapshot{}, fmt.Errorf("non-positive current price")
	}
	minDTE, maxDTE := intLLMParam(params, "min_dte", 5), intLLMParam(params, "max_dte", 10)
	contracts, err := m.client.OptionChain(ctx, symbol, now.AddDate(0, 0, minDTE), now.AddDate(0, 0, maxDTE))
	if err != nil {
		return llmstrategy.Snapshot{}, err
	}
	// Bound the expensive live-Greeks requests while preserving both sides and
	// the closest strikes the model can reasonably select.
	sort.Slice(contracts, func(i, j int) bool { return math.Abs(contracts[i].Strike-price) < math.Abs(contracts[j].Strike-price) })
	if len(contracts) > 12 {
		contracts = contracts[:12]
	}
	codes := make([]string, 0, len(contracts))
	for _, c := range contracts {
		codes = append(codes, c.Symbol)
	}
	quotes, err := m.quoter.OptionQuotes(ctx, codes)
	if err != nil {
		return llmstrategy.Snapshot{}, err
	}
	stockSnap, err := m.accounts.AccountForSymbol(ctx, futu.EnvSim, symbol)
	if err != nil {
		return llmstrategy.Snapshot{}, err
	}
	positions := make([]llmsignal.Position, 0, len(stockSnap.Positions))
	for _, p := range stockSnap.Positions {
		positions = append(positions, llmsignal.Position{Symbol: p.Symbol, Qty: p.Qty})
	}
	cash := stockSnap.Funds.AvailableCash
	var optionPositions []httpapi.PositionJSON
	if len(contracts) > 0 {
		if optionSnap, e := m.accounts.AccountForSymbol(ctx, futu.EnvSim, contracts[0].Symbol); e == nil {
			if optionSnap.Funds.AvailableCash < cash {
				cash = optionSnap.Funds.AvailableCash
			}
			for _, p := range optionSnap.Positions {
				positions = append(positions, llmsignal.Position{Symbol: p.Symbol, Qty: p.Qty})
				optionPositions = append(optionPositions, p)
			}
		}
	}
	family := ""
	if len(contracts) > 0 {
		family = llmOptionFamily(contracts[0].Symbol)
	}
	for _, p := range optionPositions {
		if family != "" && strings.HasPrefix(strings.TrimPrefix(p.Symbol, "HK."), family) {
			if _, ok := quotes[p.Symbol]; !ok {
				page, e := m.quoter.OptionQuotes(ctx, []string{p.Symbol})
				if e != nil {
					return llmstrategy.Snapshot{}, e
				}
				for code, q := range page {
					quotes[code] = q
				}
			}
		}
	}
	optionDelta := 0.0
	for _, p := range optionPositions {
		q, ok := quotes[p.Symbol]
		if !ok {
			continue
		}
		lot := q.LotSize
		if lot <= 0 {
			lot = 100
		}
		optionDelta += p.Qty * q.Delta * float64(lot)
	}
	out := llmstrategy.Snapshot{Symbol: symbol, CurrentPrice: price, CashAvailable: &cash, OptionDeltaStock: optionDelta, Positions: positions, Options: make([]llmstrategy.Option, 0, len(contracts))}
	for _, c := range contracts {
		q, ok := quotes[c.Symbol]
		if !ok {
			continue
		}
		premium := q.Bid
		if premium <= 0 {
			premium = q.Last
		}
		out.Options = append(out.Options, llmstrategy.Option{Contract: c.Symbol, Direction: strings.ToUpper(c.OptionType), Strike: c.Strike, Expiry: c.Expiry.Format("2006-01-02T00:00:00Z"), Premium: premium, Delta: q.Delta, IV: q.ImpliedVol, OpenInterest: q.OpenInterest})
	}
	return out, nil
}

func llmOptionFamily(code string) string {
	rest := strings.TrimPrefix(code, "HK.")
	idx := strings.LastIndexAny(rest, "CP")
	if idx < 7 {
		return ""
	}
	return rest[:idx-6]
}

func intLLMParam(m map[string]any, key string, def int) int {
	if v, ok := m[key].(float64); ok && v > 0 {
		return int(v)
	}
	return def
}

func startLLMStrategyScheduler(ctx context.Context, database *sql.DB, interval time.Duration, reviewerModel string) {
	client, err := llmstrategy.NewClient(os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "llmstrategy: disabled: %v\n", err)
		return
	}
	reviewer, _ := llmReviewerFromEnv()
	store := wheelstore.New(database)
	svc := &llmsignal.Service{Store: store, Reviewer: reviewer, Model: reviewerModel}
	futuClient := futu.NewClient(resolveFutuGateway(""))
	runner := &llmstrategy.Runner{Watchlist: llmWatchlist{database}, Dedupe: store, Market: llmMarket{quoter: futuQuoter{client: futuClient}, client: futuClient, accounts: httpapi.NewFutuAccounter()}, Generator: client, Submitter: svc}
	if err := runner.Run(ctx, interval); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "llmstrategy: runner: %v\n", err)
	}
}

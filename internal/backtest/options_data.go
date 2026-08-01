package backtest

import (
	"fmt"

	"github.com/jiayu/wbot/internal/ingest"
)

// OptionsDataFromQuotes builds the runner's option universe from option_quotes
// rows: chain metadata from the first row per contract, bars per contract in ts
// order (rows are expected symbol+ts ascending, like QueryOptionQuotes).
func OptionsDataFromQuotes(rows []ingest.OptionQuoteRow) (*OptionsData, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("backtest: no option quote rows")
	}
	data := &OptionsData{Chain: OptionChain{}, Bars: OptionBars{}}
	seen := map[string]bool{}
	for _, r := range rows {
		kind := OptionKind(r.OptionType)
		if kind != OptionCall && kind != OptionPut {
			return nil, fmt.Errorf("backtest: option %s: unknown option_type %q", r.Symbol, r.OptionType)
		}
		if !seen[r.Symbol] {
			seen[r.Symbol] = true
			data.Chain[r.Symbol] = OptionContract{Code: r.Symbol, Kind: kind, Strike: r.Strike, Expiry: r.Expiry}
		}
		data.Bars[r.Symbol] = append(data.Bars[r.Symbol], ingest.Bar{
			Ts: r.Ts, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return data, nil
}

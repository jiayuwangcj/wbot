// Package wheelrun adapts broker state into the pure wheel decision inputs.
// It holds no broker client and no gofutuapi dependency: callers inject a
// TradePositions implementation and receive wheel-ready values back.
package wheelrun

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/wheel"
)

// PositionSide mirrors trdcommon.PositionSide (0=long, 1=short, -1=unknown)
// so a futu.TradeClient adapter can copy the enum value unchanged.
const (
	SideLong  = 0
	SideShort = 1
)

// Position is one broker position in the futu-neutral shape. Qty is always
// positive; the side gives the sign (short positions are negative on input).
type Position struct {
	Symbol string  // market-qualified, e.g. HK.TCH260807C335000
	Code   string  // bare code, e.g. TCH260807C335000 or 00700
	Qty    float64 // shares (stocks) or contracts (options)
	Side   int     // PositionSide: 0 long, 1 short, -1 unknown
}

// TradePositions is the injectable position source for the runner (fakes in
// tests, a futu.TradeClient adapter in production). acc is opaque so wheelrun
// stays free of the gofutuapi trdcommon types.
type TradePositions interface {
	Positions(ctx context.Context, acc any) ([]Position, error)
}

// filterPositions keeps only the positions that belong to the underlying
// currently being evaluated. Stock positions are already market-qualified by
// the production TradePositions adapter, so their Symbol must equal symbol.
// Option positions are identified by their code and must also be present in
// the current option chain. The second result contains option positions that
// look valid (or explicitly unsupported) but cannot be attributed to this
// chain; callers should log those positions and continue fail-closed.
func filterPositions(symbol string, positions []Position, contractSymbols []string) (matched, unassignedOptions []Position) {
	chainCodes := make(map[string]struct{}, len(contractSymbols)*2)
	for _, contractSymbol := range contractSymbols {
		if contractSymbol == "" {
			continue
		}
		chainCodes[contractSymbol] = struct{}{}
		chainCodes[bareSecurityCode(contractSymbol)] = struct{}{}
	}

	matched = make([]Position, 0, len(positions))
	for _, p := range positions {
		code := p.Code
		if code == "" {
			code = p.Symbol
		}
		_, _, _, optionErr := parseOptionCode(code)
		if optionErr == nil {
			if optionCodeInChain(p, chainCodes) {
				matched = append(matched, p)
			} else {
				unassignedOptions = append(unassignedOptions, p)
			}
			continue
		}
		if errors.Is(optionErr, errUnsupportedOption) {
			unassignedOptions = append(unassignedOptions, p)
			continue
		}
		if p.Symbol == symbol {
			matched = append(matched, p)
		}
	}
	return matched, unassignedOptions
}

func optionCodeInChain(p Position, chainCodes map[string]struct{}) bool {
	for _, candidate := range []string{p.Code, p.Symbol} {
		if candidate == "" {
			continue
		}
		if _, ok := chainCodes[candidate]; ok {
			return true
		}
		if _, ok := chainCodes[bareSecurityCode(candidate)]; ok {
			return true
		}
	}
	return false
}

func bareSecurityCode(symbol string) string {
	if _, code, ok := strings.Cut(symbol, "."); ok {
		return code
	}
	return symbol
}

// optionCodeRE splits UNDERLYING + YYMMDD + C/P + strike×1000, the convention
// documented in doc/DATA_STANDARD.md (e.g. TCH260807C335000); a market prefix
// like "HK." is stripped before matching.
var optionCodeRE = regexp.MustCompile(`^([A-Za-z]+)(\d{2})(\d{2})(\d{2})([CP])(\d+)$`)

// suspiciousOptionRE matches digit-led codes that still carry a C/P suffix —
// likely SH/SZ ETF option shapes this mapping does not support yet.
var suspiciousOptionRE = regexp.MustCompile(`^[^A-Za-z].*[CP]\d+$`)

// errUnsupportedOption marks a code that looks like an option but whose
// underlying does not start with a letter (e.g. SH/SZ ETF options).
var errUnsupportedOption = errors.New("wheelrun: unsupported option code: underlying must start with a letter")

// parseOptionCode parses an option code into strike, expiry (UTC midnight)
// and a wheel.OptionType ("call"/"put") ready for wheel.OptionPosition.
// Digit-led codes with an option suffix fail with errUnsupportedOption instead
// of a generic parse error, so callers never mistake them for stock codes.
func parseOptionCode(code string) (strike float64, expiry time.Time, typ wheel.OptionType, err error) {
	for _, pre := range []string{"HK.", "US.", "SH.", "SZ."} {
		code = strings.TrimPrefix(code, pre)
	}
	m := optionCodeRE.FindStringSubmatch(code)
	if m == nil {
		if suspiciousOptionRE.MatchString(code) {
			return 0, time.Time{}, "", fmt.Errorf("%w: %q", errUnsupportedOption, code)
		}
		return 0, time.Time{}, "", fmt.Errorf("not an option code %q (want UNDERLYING+YYMMDD+C/P+strike×1000)", code)
	}
	expiry, err = time.Parse("060102", m[2]+m[3]+m[4])
	if err != nil {
		return 0, time.Time{}, "", fmt.Errorf("bad expiry in option code %q", code)
	}
	strike, err = strconv.ParseFloat(m[6], 64)
	if err != nil {
		return 0, time.Time{}, "", fmt.Errorf("bad strike in option code %q", code)
	}
	strike /= 1000
	typ = wheel.Call
	if m[5] == "P" {
		typ = wheel.Put
	}
	return strike, expiry, typ, nil
}

// PositionsInput maps broker positions to wheel DecisionInput stock shares
// and option positions (pure). Codes that parse as options become option
// positions; everything else counts as stock. wheel.OptionPosition has no
// expiry field, so LotSize/Delta/Expiry stay zero — the runner fills quotes
// (with parseOptionCode's expiry) from OptionQuotes. Suspicious option-shaped
// codes and negative qtys fail instead of silently entering the inventory.
func PositionsInput(positions []Position) (stockShares float64, opts []wheel.OptionPosition, err error) {
	opts = make([]wheel.OptionPosition, 0, len(positions))
	for _, p := range positions {
		code := p.Code
		sym := p.Symbol
		if code == "" {
			code = sym
		}
		if code == "" {
			return 0, nil, errors.New("wheelrun: position without code")
		}
		if sym == "" {
			sym = code
		}
		signed, err := signedQty(p)
		if err != nil {
			return 0, nil, err
		}
		if strike, _, typ, perr := parseOptionCode(code); perr == nil {
			opts = append(opts, wheel.OptionPosition{
				Symbol:          sym,
				SignedContracts: signed,
				Strike:          strike,
				OptionType:      typ,
			})
			continue
		} else if errors.Is(perr, errUnsupportedOption) {
			return 0, nil, fmt.Errorf("wheelrun: position %s: %w", sym, perr)
		}
		stockShares += signed
	}
	return stockShares, opts, nil
}

// signedQty applies the PositionSide sign (long +, short −); an unknown side
// or a negative qty is an error so a mis-adapted position cannot silently
// flip or shrink the inventory.
func signedQty(p Position) (float64, error) {
	if p.Qty < 0 {
		return 0, fmt.Errorf("wheelrun: negative qty %v for %s", p.Qty, p.Symbol)
	}
	switch p.Side {
	case SideLong:
		return p.Qty, nil
	case SideShort:
		return -p.Qty, nil
	default:
		return 0, fmt.Errorf("wheelrun: unknown position side %d for %s", p.Side, p.Symbol)
	}
}

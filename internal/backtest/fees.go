package backtest

import (
	"fmt"
	"math"
	"strings"
)

const (
	// DefaultOptionFeePerContract is the HK option fee used by the CLI when
	// the type-specific fee model is selected.
	DefaultOptionFeePerContract = 21.0
	// DefaultStockFeePerLot is the HK stock fee used by the CLI when the
	// type-specific fee model is selected.
	DefaultStockFeePerLot = 70.0
	// DefaultLotSize is the number of shares in one stock lot for the fee
	// model. It is deliberately separate from the option contract multiplier.
	DefaultLotSize = 100
)

// FeeModel describes the fee schedule used by a replay. OptionPerContract is
// charged once per option contract, while StockPerLot is charged once per
// stock lot. Legacy=true preserves the historical fixed-per-filled-trade
// contract exposed by Run and RunOptions; in that mode mechanical exercise
// events remain free for backwards compatibility.
type FeeModel struct {
	OptionPerContract float64
	StockPerLot       float64
	LotSize           int
	LegacyPerTrade    float64
	Legacy            bool
}

// DefaultFeeModel returns the current Hong Kong type-specific schedule.
func DefaultFeeModel() FeeModel {
	return FeeModel{OptionPerContract: DefaultOptionFeePerContract, StockPerLot: DefaultStockFeePerLot, LotSize: DefaultLotSize}
}

// LegacyFeeModel adapts the historical fixed fee to the new replay engine.
func LegacyFeeModel(feePerTrade float64) FeeModel {
	return FeeModel{LegacyPerTrade: feePerTrade, LotSize: DefaultLotSize, Legacy: true}
}

// HKFeeModel constructs a type-specific schedule. A zero lot size selects the
// market default of 100 shares per lot.
func HKFeeModel(optionPerContract, stockPerLot float64, lotSize int) FeeModel {
	if lotSize == 0 {
		lotSize = DefaultLotSize
	}
	return FeeModel{OptionPerContract: optionPerContract, StockPerLot: stockPerLot, LotSize: lotSize}
}

func (m FeeModel) normalized() FeeModel {
	if m.LotSize == 0 {
		m.LotSize = DefaultLotSize
	}
	return m
}

func (m FeeModel) validate() error {
	m = m.normalized()
	if m.Legacy {
		if math.IsNaN(m.LegacyPerTrade) || math.IsInf(m.LegacyPerTrade, 0) || m.LegacyPerTrade < 0 {
			return errorsNegativeFee
		}
		return nil
	}
	if math.IsNaN(m.OptionPerContract) || math.IsInf(m.OptionPerContract, 0) || m.OptionPerContract < 0 ||
		math.IsNaN(m.StockPerLot) || math.IsInf(m.StockPerLot, 0) || m.StockPerLot < 0 {
		return errorsNegativeFee
	}
	if m.LotSize <= 0 {
		return fmt.Errorf("backtest: lot size must be > 0")
	}
	return nil
}

var errorsNegativeFee = fmt.Errorf("backtest: negative fee")

func (m FeeModel) fee(action string, size float64) float64 {
	if size == 0 {
		return 0
	}
	if m.Legacy {
		if isActiveTrade(action) {
			return m.LegacyPerTrade
		}
		return 0
	}
	m = m.normalized()
	if isStockTrade(action) || isExerciseTrade(action) {
		return stockLots(size, m.LotSize) * m.StockPerLot
	}
	if isOptionTrade(action) {
		return math.Abs(size) * m.OptionPerContract
	}
	return 0
}

func stockLots(shares float64, lotSize int) float64 {
	if shares == 0 || lotSize <= 0 {
		return 0
	}
	// A partial stock lot still consumes one fee unit. Normal Hong Kong stock
	// fills are lot-aligned; ceil keeps fractional benchmark fills from
	// accidentally becoming free.
	return math.Ceil(math.Abs(shares) / float64(lotSize))
}

func isStockTrade(action string) bool { return action == "buy" || action == "sell" }

func isOptionTrade(action string) bool {
	return strings.HasPrefix(action, "buy-") || strings.HasPrefix(action, "sell-")
}

func isExerciseTrade(action string) bool {
	return action == "exercise-call" || action == "exercise-put" || action == "exercise-buyin"
}

func isActiveTrade(action string) bool { return isStockTrade(action) || isOptionTrade(action) }

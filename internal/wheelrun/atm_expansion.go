package wheelrun

import (
	"math"
	"sort"
	"strings"

	"github.com/jiayu/wbot/internal/futu"
)

const (
	// maxATMExpansionRadius bounds the live path to the ATM strike plus ten
	// levels on each side. A chain can still be retained in storage separately.
	maxATMExpansionRadius = 10
	minQualityCandidates  = 2
)

type strikeLevel struct {
	strike    float64
	contracts []futu.OptionContract
}

// atmExpansionLevels groups calls/puts and expiries by strike and orders the
// groups ATM, one strike below, one above, two below, two above, and so on.
// The returned levels are the only contracts the realtime quote path needs.
func atmExpansionLevels(contracts []futu.OptionContract, price float64, radius int) [][]futu.OptionContract {
	levels := strikeLevels(contracts)
	if len(levels) == 0 {
		return nil
	}
	if radius < 0 {
		radius = 0
	}
	center := 0
	centerDistance := math.Abs(levels[0].strike - price)
	for i := 1; i < len(levels); i++ {
		distance := math.Abs(levels[i].strike - price)
		if distance < centerDistance || distance == centerDistance && levels[i].strike < levels[center].strike {
			center, centerDistance = i, distance
		}
	}

	out := make([][]futu.OptionContract, 0, 2*radius+1)
	for distance := 0; distance <= radius; distance++ {
		if i := center - distance; i >= 0 {
			out = append(out, levels[i].contracts)
		}
		if distance > 0 {
			if i := center + distance; i < len(levels) {
				out = append(out, levels[i].contracts)
			}
		}
		if center-distance < 0 && center+distance >= len(levels) {
			break
		}
	}
	return out
}

func strikeLevels(contracts []futu.OptionContract) []strikeLevel {
	ordered := append([]futu.OptionContract(nil), contracts...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Strike != ordered[j].Strike {
			return ordered[i].Strike < ordered[j].Strike
		}
		if !ordered[i].Expiry.Equal(ordered[j].Expiry) {
			return ordered[i].Expiry.Before(ordered[j].Expiry)
		}
		left, right := strings.ToLower(ordered[i].OptionType), strings.ToLower(ordered[j].OptionType)
		if left != right {
			return left < right
		}
		return ordered[i].Symbol < ordered[j].Symbol
	})

	levels := make([]strikeLevel, 0, len(ordered))
	for _, contract := range ordered {
		if len(levels) == 0 || levels[len(levels)-1].strike != contract.Strike {
			levels = append(levels, strikeLevel{strike: contract.Strike})
		}
		last := &levels[len(levels)-1]
		last.contracts = append(last.contracts, contract)
	}
	return levels
}

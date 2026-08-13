package backtest

// Unfilled-attempt model (adjudication doc/BACKTEST_REPORT.md §3, plan §二):
// a market sell of the pending option is attempted at the Bid side of the
// current atomic quote and is valid for the bar. Limit price is undefined, so
// the model explains only whether the attempt fills, never at what price.
//
// The unfilled probability is a liquidity heuristic over relative spread,
// proximal volume and open interest; the weights are calibration defaults and
// changing any of them requires a model_version bump.

import (
	"encoding/binary"
	"hash/fnv"
	"time"
)

const (
	modelKind    = "heuristic"
	modelVersion = "heuristic-1.0"
	// defaultRunSeed seeds the attempt draws when OptionsData.RunSeed is 0.
	defaultRunSeed = 42
	// Calibration weights (versioned under modelVersion).
	wSpread   = 0.55
	wVol      = 0.30
	wOI       = 0.15
	failFloor = 0.05
	failCap   = 0.95
)

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }

// failProb returns the unfilled probability of one sell attempt from the
// contract's market: relative spread (ask-bid)/mid, proximal volume and open
// interest. Missing or zero vol/oi count as 0 (component → 1, high fail);
// a missing two-sided quote (mid <= 0) is treated as maximally wide so the
// spread component also goes to 1 (total clamps at failCap).
func failProb(bid, ask float64, vol, oi int64) float64 {
	liquidity := 0.0 // the contract's spread := clamp01(1 - relativeSpread)
	if mid := (bid + ask) / 2; mid > 0 {
		liquidity = clamp01(1 - (ask-bid)/mid)
	}
	p := wSpread*(1-liquidity) + wVol*(100/(float64(vol)+100)) + wOI*(1000/(float64(oi)+1000))
	return clamp(p, failFloor, failCap)
}

// attemptDraw derives the uniform draw of one sell attempt from (runSeed,
// symbol, contract, barTs, attemptIndex). attemptIndex is the contract's own
// attempt sequence (State.AttemptsByContract), so a new candidate or attempt
// elsewhere in the run never shifts existing orders' outcomes (same seed,
// same trace; plan §二).
func attemptDraw(runSeed int64, symbol, contract string, barTs time.Time, attemptIndex int64) float64 {
	h := fnv.New64a()
	h.Write([]byte(symbol))
	h.Write([]byte{0})
	h.Write([]byte(contract))
	h.Write([]byte{0})
	h.Write([]byte(barTs.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte{0})
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(attemptIndex))
	h.Write(buf[:])
	mixed := mix64(h.Sum64() ^ uint64(runSeed))
	return float64(mixed>>11) * (1.0 / float64(uint64(1)<<53))
}

// mix64 is the splitmix64 finalizer: bijective avalanche for the fnv state.
func mix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// unfilledModelLabel is the Trade.UnfilledModel value on unfilled attempts.
func unfilledModelLabel() string { return modelKind + "/" + modelVersion }

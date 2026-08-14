package backtestreport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ESInput extends the single-run projection without changing any S4 field.
// Run is the selected candidate's sample-out result.
type ESInput struct {
	Run         Input
	Windows     Windows
	Train       Train
	Generations []Generation
	Candidates  []Candidate
	Reward      RewardAudit
	SearchSpace map[string]SearchBound
	Trajectory  []TrajectoryStep
	TailLossPct float64
}

func BuildES(in ESInput) (*Report, error) {
	if in.Train.Algorithm != "ES" || in.Train.AlgorithmVersion == "" {
		return nil, errors.New("backtest report: es_train requires ES algorithm metadata")
	}
	if len(in.Train.Seeds) < 3 || !uniqueSeeds(in.Train.Seeds) {
		return nil, errors.New("backtest report: es_train requires distinct train and test seeds")
	}
	if len(in.Generations) == 0 {
		return nil, errors.New("backtest report: es_train requires generations")
	}
	r, err := Build(in.Run)
	if err != nil {
		return nil, err
	}
	r.ReportKind = "es_train"
	r.Identity.Windows = &in.Windows
	// Training artifacts cannot become READY or drive configuration writes.
	// A historical data blocker is stronger than RESEARCH_ONLY and must not be
	// hidden merely because the report contains an ES projection.
	if r.Identity.CapabilityStatus != "DATA_BLOCKED" {
		r.Identity.CapabilityStatus = "RESEARCH_ONLY"
	}
	r.Train, r.Generations, r.Candidates, r.Trajectory = &in.Train, in.Generations, in.Candidates, in.Trajectory
	r.Result.TailLossPct = &in.TailLossPct
	r.Audit.Reward, r.Audit.SearchSpace = &in.Reward, in.SearchSpace
	r.Audit.BaselineChanges = []string{"buy-hold 基线按封存 test 窗口独立重算"}
	snapshot := struct {
		BaseHash           string                 `json:"base_hash"`
		Windows            Windows                `json:"windows"`
		Algorithm          string                 `json:"algorithm"`
		AlgorithmVersion   string                 `json:"algorithm_version"`
		PopulationSize     int                    `json:"population_size"`
		Seeds              []int64                `json:"seeds"`
		EvaluationEstimate string                 `json:"evaluation_estimate"`
		Space              map[string]SearchBound `json:"search_space"`
	}{r.Audit.InputSnapshotHash, in.Windows, in.Train.Algorithm, in.Train.AlgorithmVersion, in.Train.PopulationSize, in.Train.Seeds, in.Train.EvaluationEstimate, in.SearchSpace}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("backtest report: marshal ES snapshot: %w", err)
	}
	digest := sha256.Sum256(b)
	hash := hex.EncodeToString(digest[:])
	r.Audit.InputSnapshotHash = "sha256-" + hash
	r.ReportID = fmt.Sprintf("bt-%s-%d-%s", r.Identity.Symbol, r.Identity.RunSeed, hash[:8])
	return r, nil
}

func uniqueSeeds(seeds []int64) bool {
	seen := make(map[int64]bool, len(seeds))
	for _, seed := range seeds {
		if seen[seed] {
			return false
		}
		seen[seed] = true
	}
	return true
}

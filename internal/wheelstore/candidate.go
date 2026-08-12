package wheelstore

import "encoding/json"

type candidateWireMode uint8

const (
	wireSparse candidateWireMode = iota
	wireCompact
	wireFull
)

// Quote is the provider-neutral quote DTO embedded in a persisted signal
// candidate. Timestamps stay strings because signal JSON already exposes them
// as RFC3339 values and LLM signals may omit them.
type Quote struct {
	Symbol       string   `json:"symbol"`
	Code         string   `json:"code,omitempty"`
	Underlying   string   `json:"underlying,omitempty"`
	Source       string   `json:"source"`
	OptionType   string   `json:"option_type"`
	Type         string   `json:"type,omitempty"`
	Expiry       string   `json:"expiry"`
	Strike       float64  `json:"strike"`
	Delta        float64  `json:"delta"`
	MarketDelta  float64  `json:"market_delta,omitempty"`
	Bid          float64  `json:"bid"`
	Ask          float64  `json:"ask"`
	Last         float64  `json:"last"`
	ImpliedVol   float64  `json:"implied_vol"`
	Theta        *float64 `json:"theta,omitempty"`
	Volume       int64    `json:"volume,omitempty"`
	OpenInterest int64    `json:"open_interest"`
	LotSize      int      `json:"lot_size,omitempty"`
	QuoteTime    string   `json:"quote_time,omitempty"`
	CapturedAt   string   `json:"captured_at,omitempty"`
	Timestamp    string   `json:"timestamp,omitempty"`
	Ts           string   `json:"ts,omitempty"`
	IV           float64  `json:"iv,omitempty"`
	OI           int64    `json:"oi,omitempty"`
	wireMode     candidateWireMode
}

// Candidate is the common signal candidate DTO consumed by both the wheel
// runner and the LLM injection path. The wire mode is internal: wheel
// candidates retain the complete domain JSON while injected candidates retain
// their intentionally compact JSON shape.
type Candidate struct {
	// Symbol and QuoteSnapshotID retain the small diagnostic candidate shape
	// accepted by earlier repository callers; actionable candidates use Quote.
	Symbol              string   `json:"symbol,omitempty"`
	QuoteSnapshotID     int64    `json:"quote_snapshot_id,omitempty"`
	Quote               *Quote   `json:"quote,omitempty"`
	Direction           string   `json:"direction"`
	Quantity            int      `json:"quantity"`
	SignedContracts     int      `json:"signed_contracts"`
	Quality             float64  `json:"quality"`
	PostTradeEffective  float64  `json:"post_trade_effective_inventory"`
	AssignmentInventory float64  `json:"assignment_inventory"`
	Accepted            bool     `json:"accepted"`
	Reasons             []string `json:"reasons,omitempty"`
	wireMode            candidateWireMode
}

// AsCompactCandidate marks a candidate produced by the LLM injection path.
// The returned value keeps the old direction/quantity/accepted plus quote
// field set when it is persisted or exposed by the audit API.
func AsCompactCandidate(c Candidate) Candidate {
	c.wireMode = wireCompact
	if c.Quote != nil {
		q := *c.Quote
		q.wireMode = wireCompact
		c.Quote = &q
	}
	return c
}

// AsFullCandidate marks a candidate converted from wheel.CandidateEvaluation.
// It keeps zero-valued domain fields that the previous wheel JSON serializer
// emitted deliberately (for example theta:null and accepted:false).
func AsFullCandidate(c Candidate) Candidate {
	c.wireMode = wireFull
	if c.Quote != nil {
		q := *c.Quote
		q.wireMode = wireFull
		c.Quote = &q
	}
	return c
}

type sparseQuoteJSON struct {
	Symbol       string   `json:"symbol,omitempty"`
	Code         string   `json:"code,omitempty"`
	Underlying   string   `json:"underlying,omitempty"`
	Source       string   `json:"source,omitempty"`
	OptionType   string   `json:"option_type,omitempty"`
	Type         string   `json:"type,omitempty"`
	Expiry       string   `json:"expiry,omitempty"`
	Strike       float64  `json:"strike,omitempty"`
	Delta        float64  `json:"delta,omitempty"`
	MarketDelta  float64  `json:"market_delta,omitempty"`
	Bid          float64  `json:"bid,omitempty"`
	Ask          float64  `json:"ask,omitempty"`
	Last         float64  `json:"last,omitempty"`
	ImpliedVol   float64  `json:"implied_vol,omitempty"`
	Theta        *float64 `json:"theta,omitempty"`
	Volume       int64    `json:"volume,omitempty"`
	OpenInterest int64    `json:"open_interest,omitempty"`
	LotSize      int      `json:"lot_size,omitempty"`
	QuoteTime    string   `json:"quote_time,omitempty"`
	CapturedAt   string   `json:"captured_at,omitempty"`
	Timestamp    string   `json:"timestamp,omitempty"`
	Ts           string   `json:"ts,omitempty"`
	IV           float64  `json:"iv,omitempty"`
	OI           int64    `json:"oi,omitempty"`
}

type compactQuoteJSON struct {
	Symbol       string  `json:"symbol"`
	OptionType   string  `json:"option_type"`
	Strike       float64 `json:"strike"`
	Expiry       string  `json:"expiry"`
	Bid          float64 `json:"bid"`
	Ask          float64 `json:"ask"`
	Last         float64 `json:"last"`
	Delta        float64 `json:"delta"`
	ImpliedVol   float64 `json:"implied_vol"`
	OpenInterest int64   `json:"open_interest"`
}

type fullQuoteJSON struct {
	Symbol       string   `json:"symbol"`
	Code         string   `json:"code,omitempty"`
	Underlying   string   `json:"underlying,omitempty"`
	Source       string   `json:"source"`
	OptionType   string   `json:"option_type"`
	Type         string   `json:"type,omitempty"`
	Expiry       string   `json:"expiry"`
	Strike       float64  `json:"strike"`
	Delta        float64  `json:"delta"`
	MarketDelta  float64  `json:"market_delta,omitempty"`
	Bid          float64  `json:"bid"`
	Ask          float64  `json:"ask"`
	Last         float64  `json:"last,omitempty"`
	ImpliedVol   float64  `json:"implied_vol"`
	Theta        *float64 `json:"theta"`
	Volume       int64    `json:"volume"`
	OpenInterest int64    `json:"open_interest"`
	LotSize      int      `json:"lot_size"`
	QuoteTime    string   `json:"quote_time"`
	CapturedAt   string   `json:"captured_at,omitempty"`
	Timestamp    string   `json:"timestamp,omitempty"`
	Ts           string   `json:"ts,omitempty"`
	IV           float64  `json:"iv,omitempty"`
	OI           int64    `json:"oi,omitempty"`
}

func (q Quote) MarshalJSON() ([]byte, error) {
	switch q.wireMode {
	case wireCompact:
		return json.Marshal(compactQuoteJSON{
			Symbol: q.Symbol, OptionType: q.OptionType, Strike: q.Strike,
			Expiry: q.Expiry, Bid: q.Bid, Ask: q.Ask, Last: q.Last,
			Delta: q.Delta, ImpliedVol: q.ImpliedVol, OpenInterest: q.OpenInterest,
		})
	case wireFull:
		return json.Marshal(fullQuoteJSON{
			Symbol: q.Symbol, Code: q.Code, Underlying: q.Underlying, Source: q.Source,
			OptionType: q.OptionType, Type: q.Type, Expiry: q.Expiry, Strike: q.Strike,
			Delta: q.Delta, MarketDelta: q.MarketDelta, Bid: q.Bid, Ask: q.Ask,
			Last: q.Last, ImpliedVol: q.ImpliedVol, Theta: q.Theta, Volume: q.Volume,
			OpenInterest: q.OpenInterest, LotSize: q.LotSize, QuoteTime: q.QuoteTime,
			CapturedAt: q.CapturedAt, Timestamp: q.Timestamp, Ts: q.Ts, IV: q.IV, OI: q.OI,
		})
	default:
		return json.Marshal(sparseQuoteJSON{
			Symbol: q.Symbol, Code: q.Code, Underlying: q.Underlying, Source: q.Source,
			OptionType: q.OptionType, Type: q.Type, Expiry: q.Expiry, Strike: q.Strike,
			Delta: q.Delta, MarketDelta: q.MarketDelta, Bid: q.Bid, Ask: q.Ask,
			Last: q.Last, ImpliedVol: q.ImpliedVol, Theta: q.Theta, Volume: q.Volume,
			OpenInterest: q.OpenInterest, LotSize: q.LotSize, QuoteTime: q.QuoteTime,
			CapturedAt: q.CapturedAt, Timestamp: q.Timestamp, Ts: q.Ts, IV: q.IV, OI: q.OI,
		})
	}
}

func (q *Quote) UnmarshalJSON(data []byte) error {
	type plain Quote
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*q = Quote(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if hasAny(fields, "source", "code", "underlying", "type", "market_delta", "theta", "volume", "lot_size", "quote_time", "captured_at", "timestamp", "ts", "iv", "oi") {
		q.wireMode = wireFull
	} else if hasAny(fields, "symbol", "option_type", "strike", "expiry", "bid", "ask", "last", "delta", "implied_vol", "open_interest") {
		q.wireMode = wireCompact
	}
	return nil
}

type sparseCandidateJSON struct {
	Symbol              string   `json:"symbol,omitempty"`
	QuoteSnapshotID     int64    `json:"quote_snapshot_id,omitempty"`
	Quote               *Quote   `json:"quote,omitempty"`
	Direction           string   `json:"direction,omitempty"`
	Quantity            int      `json:"quantity,omitempty"`
	SignedContracts     int      `json:"signed_contracts,omitempty"`
	Quality             float64  `json:"quality,omitempty"`
	PostTradeEffective  float64  `json:"post_trade_effective_inventory,omitempty"`
	AssignmentInventory float64  `json:"assignment_inventory,omitempty"`
	Accepted            bool     `json:"accepted,omitempty"`
	Reasons             []string `json:"reasons,omitempty"`
}

type compactCandidateJSON struct {
	Symbol          string   `json:"symbol,omitempty"`
	QuoteSnapshotID int64    `json:"quote_snapshot_id,omitempty"`
	Quote           *Quote   `json:"quote,omitempty"`
	Direction       string   `json:"direction"`
	Quantity        int      `json:"quantity"`
	Accepted        bool     `json:"accepted"`
	Reasons         []string `json:"reasons,omitempty"`
}

type fullCandidateJSON struct {
	Symbol              string   `json:"symbol,omitempty"`
	QuoteSnapshotID     int64    `json:"quote_snapshot_id,omitempty"`
	Quote               *Quote   `json:"quote"`
	Direction           string   `json:"direction"`
	Quantity            int      `json:"quantity"`
	SignedContracts     int      `json:"signed_contracts"`
	Quality             float64  `json:"quality"`
	PostTradeEffective  float64  `json:"post_trade_effective_inventory"`
	AssignmentInventory float64  `json:"assignment_inventory"`
	Accepted            bool     `json:"accepted"`
	Reasons             []string `json:"reasons,omitempty"`
}

func (c Candidate) MarshalJSON() ([]byte, error) {
	switch c.wireMode {
	case wireCompact:
		return json.Marshal(compactCandidateJSON{
			Symbol: c.Symbol, QuoteSnapshotID: c.QuoteSnapshotID, Quote: c.Quote,
			Direction: c.Direction, Quantity: c.Quantity, Accepted: c.Accepted, Reasons: c.Reasons,
		})
	case wireFull:
		return json.Marshal(fullCandidateJSON{
			Symbol: c.Symbol, QuoteSnapshotID: c.QuoteSnapshotID, Quote: c.Quote,
			Direction: c.Direction, Quantity: c.Quantity, SignedContracts: c.SignedContracts,
			Quality: c.Quality, PostTradeEffective: c.PostTradeEffective,
			AssignmentInventory: c.AssignmentInventory, Accepted: c.Accepted, Reasons: c.Reasons,
		})
	default:
		return json.Marshal(sparseCandidateJSON{
			Symbol: c.Symbol, QuoteSnapshotID: c.QuoteSnapshotID, Quote: c.Quote,
			Direction: c.Direction, Quantity: c.Quantity, SignedContracts: c.SignedContracts,
			Quality: c.Quality, PostTradeEffective: c.PostTradeEffective,
			AssignmentInventory: c.AssignmentInventory, Accepted: c.Accepted, Reasons: c.Reasons,
		})
	}
}

func (c *Candidate) UnmarshalJSON(data []byte) error {
	type plain Candidate
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Candidate(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if hasAny(fields, "signed_contracts", "quality", "post_trade_effective_inventory", "assignment_inventory") {
		c.wireMode = wireFull
	} else if hasAny(fields, "direction", "quantity", "accepted", "quote") {
		c.wireMode = wireCompact
	}
	return nil
}

func hasAny(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

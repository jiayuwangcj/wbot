package wheelstore

import (
	"encoding/json"
	"sort"
	"testing"
)

func jsonKeys(t *testing.T, value any) []string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestCandidateWireModesPreserveExistingShapes(t *testing.T) {
	compact := AsCompactCandidate(Candidate{
		Direction: "PUT", Quantity: 1, Accepted: true,
		Quote: &Quote{
			Symbol: "HK.TCH260821P460000", OptionType: "put", Expiry: "2026-08-21T00:00:00Z",
			Strike: 460, Bid: 11.45, Ask: 11.45, Last: 11.45, Delta: -0.47,
			ImpliedVol: 0.404, OpenInterest: 249,
		},
	})
	if got, want := jsonKeys(t, compact), []string{"accepted", "direction", "quantity", "quote"}; !sameStrings(got, want) {
		t.Fatalf("compact candidate keys=%v want=%v", got, want)
	}
	if got, want := jsonKeys(t, compact.Quote), []string{"ask", "bid", "delta", "expiry", "implied_vol", "last", "open_interest", "option_type", "strike", "symbol"}; !sameStrings(got, want) {
		t.Fatalf("compact quote keys=%v want=%v", got, want)
	}

	full := AsFullCandidate(Candidate{
		Quote:     &Quote{Symbol: "HK.TCH260821P460000", Source: "futu", OptionType: "put", Expiry: "2026-08-21T00:00:00Z", Bid: 1, Ask: 1, Last: 1, ImpliedVol: 0.4, QuoteTime: "2026-08-12T01:02:03Z"},
		Direction: "PUT", Quantity: 1, Accepted: false,
	})
	if got, want := jsonKeys(t, full), []string{"accepted", "assignment_inventory", "direction", "post_trade_effective_inventory", "quality", "quantity", "quote", "signed_contracts"}; !sameStrings(got, want) {
		t.Fatalf("full candidate keys=%v want=%v", got, want)
	}
	if got, want := jsonKeys(t, full.Quote), []string{"ask", "bid", "delta", "expiry", "implied_vol", "last", "lot_size", "open_interest", "option_type", "quote_time", "source", "strike", "symbol", "theta", "volume"}; !sameStrings(got, want) {
		t.Fatalf("full quote keys=%v want=%v", got, want)
	}

	sparse := Candidate{QuoteSnapshotID: 7, Direction: "PUT"}
	if got, want := jsonKeys(t, sparse), []string{"direction", "quote_snapshot_id"}; !sameStrings(got, want) {
		t.Fatalf("sparse candidate keys=%v want=%v", got, want)
	}
}

func TestCandidateUnmarshalRetainsWireMode(t *testing.T) {
	const compactJSON = `{"direction":"PUT","quantity":1,"accepted":true,"quote":{"symbol":"OPT","option_type":"put","strike":100,"expiry":"2026-08-21T00:00:00Z","bid":1,"ask":1.1,"last":1,"delta":-0.3,"implied_vol":0.4,"open_interest":10}}`
	var candidate Candidate
	if err := json.Unmarshal([]byte(compactJSON), &candidate); err != nil {
		t.Fatalf("unmarshal compact candidate: %v", err)
	}
	if got, want := jsonKeys(t, candidate), []string{"accepted", "direction", "quantity", "quote"}; !sameStrings(got, want) {
		t.Fatalf("decoded compact candidate keys=%v want=%v", got, want)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

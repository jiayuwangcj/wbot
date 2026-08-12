package futu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// quoteFixture is the fixture-frozen /api/quote s2c for two option contracts
// with the real gateway's fields only (cur_price/volume/update_time; no
// bid/ask, no ex_data — 实测 2026-08-11, doc/FUTU.md §10).
const quoteFixture = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[
	{"security":{"market":1,"code":"TCH260807C335000"},"cur_price":12.2,"volume":1500,"update_time":"2026-08-07 10:30:00"},
	{"security":{"market":1,"code":"TCH260807P335000"},"cur_price":3.3,"volume":800,"update_time":"2026-08-07 10:31:00"}
]}}`

// greeksResp renders one /api/option-quote s2c item with the real fields
// (price/mid/iv percent/delta/theta/vol/open_interest/contract_size).
func greeksResp(price, mid, iv, delta, theta float64, oi int64, lot int) string {
	payload := map[string]any{
		"ret_type": 0,
		"s2c": map[string]any{
			"option_quote_list": []any{map[string]any{
				"price":         price,
				"mid":           mid,
				"iv":            iv,
				"delta":         delta,
				"theta":         theta,
				"vol":           777,
				"mark_price":    price,
				"open_interest": oi,
				"contract_size": lot,
			}},
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// resetGreeksCache clears the package-level option-quote cache so tests are
// deterministic regardless of run order (cache is shared across tests).
func resetGreeksCache(t *testing.T) {
	t.Helper()
	greeksCacheMu.Lock()
	greeksCache = map[string]greeksEntry{}
	greeksCacheMu.Unlock()
}

func TestOptionQuotePageFlexibleIntegers(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantOI   int64
		wantLot  int
		wantVol  int64
		wantMark float64
	}{
		{
			name:     "floating-point literals from gateway",
			payload:  `{"option_quote_list":[{"open_interest":3204.0,"contract_size":100.0,"vol":1500.0,"mark_price":12.25}]}`,
			wantOI:   3204,
			wantLot:  100,
			wantVol:  1500,
			wantMark: 12.25,
		},
		{
			name:     "integer literals",
			payload:  `{"option_quote_list":[{"open_interest":3204,"contract_size":100,"vol":800,"mark_price":3.35}]}`,
			wantOI:   3204,
			wantLot:  100,
			wantVol:  800,
			wantMark: 3.35,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pg optionQuotePage
			if err := json.Unmarshal([]byte(tt.payload), &pg); err != nil {
				t.Fatalf("json.Unmarshal() error: %v", err)
			}
			if len(pg.OptionQuoteList) != 1 {
				t.Fatalf("option_quote_list length = %d; want 1", len(pg.OptionQuoteList))
			}
			leg := pg.OptionQuoteList[0]
			if got := int64(leg.OpenInterest); got != tt.wantOI {
				t.Errorf("open_interest = %d; want %d", got, tt.wantOI)
			}
			if got := int(leg.LotSize); got != tt.wantLot {
				t.Errorf("contract_size = %d; want %d", got, tt.wantLot)
			}
			if got := int64(leg.Vol); got != tt.wantVol {
				t.Errorf("vol = %d; want %d", got, tt.wantVol)
			}
			if leg.MarkPrice != tt.wantMark {
				t.Errorf("mark_price = %v; want %v", leg.MarkPrice, tt.wantMark)
			}
		})
	}
}

func TestOptionQuotesSuccess(t *testing.T) {
	fastLimits(t)
	resetGreeksCache(t)
	var subBody, quoteBody string
	var legCodes []string
	var legBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/subscribe":
			subBody = string(body)
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			quoteBody = string(body)
			io.WriteString(w, quoteFixture)
		case "/api/option-quote":
			legBodies = append(legBodies, string(body))
			var req struct {
				MultiLegs []struct {
					Security struct {
						Code string `json:"code"`
					} `json:"security"`
				} `json:"multi_legs"`
			}
			json.Unmarshal(body, &req)
			code := ""
			if len(req.MultiLegs) > 0 {
				code = req.MultiLegs[0].Security.Code
			}
			legCodes = append(legCodes, code)
			switch code {
			case "TCH260807C335000":
				// The real gateway emits these integer-valued fields as floats.
				io.WriteString(w, `{"ret_type":0,"s2c":{"option_quote_list":[{"price":12.2,"mid":12.3,"iv":25.0,"delta":0.58,"theta":-0.03,"vol":777.0,"mark_price":12.25,"open_interest":3204.0,"contract_size":100.0}]}}`)
			case "TCH260807P335000":
				// mid 0 → Bid/Ask fall back to the price
				io.WriteString(w, greeksResp(3.3, 0, 24.0, -0.42, -0.02, 1200, 100))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	quotes, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C335000", "TCH260807P335000.HK"})
	if err != nil {
		t.Fatalf("OptionQuotes() error: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("OptionQuotes() = %d quotes; want 2", len(quotes))
	}
	call := quotes["HK.TCH260807C335000"]
	// snapshot fields: Last from cur_price, volume, update_time as QuoteTime
	if call.Last != 12.2 || call.Volume != 1500 {
		t.Fatalf("call snapshot = %+v; want last 12.2 volume 1500", call)
	}
	// greeks fields: Bid/Ask both take mid, IV percent normalized to fraction
	if call.Bid != 12.3 || call.Ask != 12.3 {
		t.Fatalf("call Bid/Ask = %v/%v; want mid 12.3 on both sides", call.Bid, call.Ask)
	}
	if call.ImpliedVol != 0.25 || call.Delta != 0.58 || call.OpenInterest != 3204 || call.LotSize != 100 {
		t.Fatalf("call Greeks = %+v; want iv 0.25 delta 0.58 oi 3204 lot 100", call)
	}
	if call.Theta == nil || *call.Theta != -0.03 {
		t.Fatalf("call Theta = %v; want non-nil -0.03", call.Theta)
	}
	want := time.Date(2026, 8, 7, 10, 30, 0, 0, futuLoc)
	if !call.QuoteTime.Equal(want) {
		t.Fatalf("call QuoteTime = %v; want %v", call.QuoteTime, want)
	}
	put := quotes["HK.TCH260807P335000"]
	if put.Bid != 3.3 || put.Ask != 3.3 {
		t.Fatalf("put Bid/Ask = %v/%v; want price 3.3 fallback when mid is 0", put.Bid, put.Ask)
	}
	if put.Delta != -0.42 || put.OpenInterest != 1200 || put.Last != 3.3 {
		t.Fatalf("put quote = %+v; want delta -0.42 oi 1200 last 3.3", put)
	}

	// one batch subscribe with canonical MARKET.CODE symbols
	var sub map[string]any
	if err := json.Unmarshal([]byte(subBody), &sub); err != nil {
		t.Fatalf("subscribe body %q: %v", subBody, err)
	}
	if syms := sub["symbols"].([]any); len(syms) != 2 || syms[0] != "HK.TCH260807C335000" || syms[1] != "HK.TCH260807P335000" {
		t.Fatalf("subscribe symbols = %v; want canonical pair", syms)
	}
	if sub["sub_types"].([]any)[0] != float64(1) {
		t.Fatalf("subscribe sub_types = %v; want [1] SubType_Basic", sub["sub_types"])
	}
	// one snapshot call carrying every contract in a single security_list
	var qb map[string]any
	if err := json.Unmarshal([]byte(quoteBody), &qb); err != nil {
		t.Fatalf("quote body %q: %v", quoteBody, err)
	}
	if secs := qb["security_list"].([]any); len(secs) != 2 {
		t.Fatalf("quote security_list = %v; want both contracts in one call", secs)
	}
	// one option-quote call per contract, single leg with side 1 qty_ratio 1
	if len(legCodes) != 2 || legCodes[0] != "TCH260807C335000" || legCodes[1] != "TCH260807P335000" {
		t.Fatalf("option-quote legs = %v; want one per contract", legCodes)
	}
	for _, b := range legBodies {
		if !strings.Contains(b, `"side":1`) || !strings.Contains(b, `"qty_ratio":1`) {
			t.Fatalf("option-quote body %s; want single leg side 1 qty_ratio 1", b)
		}
	}
}

func TestOptionQuotesMissingFieldsZero(t *testing.T) {
	fastLimits(t)
	resetGreeksCache(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			// real-field snapshot: no ex_data anywhere, one malformed time
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[
				{"security":{"market":1,"code":"TCH260807C336000"},"cur_price":12.2,"update_time":"not-a-time"},
				{"security":{"market":1,"code":"TCH260807C337000"},"cur_price":3.3}
			]}}`)
		case "/api/option-quote":
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "TCH260807C336000") {
				w.WriteHeader(http.StatusInternalServerError)
				io.WriteString(w, `{"error":"boom"}`)
				return
			}
			// explicit theta 0 must stay a non-nil zero
			io.WriteString(w, greeksResp(3.3, 3.35, 24.0, -0.42, 0, 1200, 100))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	quotes, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C336000", "HK.TCH260807C337000"})
	if err != nil {
		t.Fatalf("OptionQuotes() error: %v", err)
	}
	q := quotes["HK.TCH260807C336000"]
	if q.Bid != 0 || q.Ask != 0 || q.ImpliedVol != 0 || q.Delta != 0 || q.Theta != nil || q.OpenInterest != 0 || q.LotSize != 0 || !q.QuoteTime.IsZero() {
		t.Fatalf("missing fields must stay zero with nil theta, got %+v", q)
	}
	if q.Last != 12.2 {
		t.Fatalf("present field must parse, Last = %v; want 12.2", q.Last)
	}
	zero := quotes["HK.TCH260807C337000"]
	if zero.Theta == nil || *zero.Theta != 0 {
		t.Fatalf("explicit theta 0 must stay a non-nil zero, got %v", zero.Theta)
	}
}

func TestOptionQuotesDiagnostics(t *testing.T) {
	fastLimits(t)
	resetGreeksCache(t)
	old := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = pw
	t.Cleanup(func() { os.Stderr = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			// 2 requested, 1 answered; the answered one's greeks fetch fails
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[
				{"security":{"market":1,"code":"TCH260807C338000"},"cur_price":12.2}
			]}}`)
		case "/api/option-quote":
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"boom"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C338000", "HK.TCH260807C339000"}); err != nil {
		t.Fatalf("OptionQuotes() error: %v", err)
	}
	pw.Close()
	buf, _ := io.ReadAll(pr)
	if got := string(buf); !strings.Contains(got, "option-quotes: requested=2 answered=1 greeks_failed=2") {
		t.Fatalf("stderr = %q; want option-quotes diagnostic", got)
	}
}

func TestOptionQuotesEmptyBatch(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("empty batch must not call the gateway, got %s", r.URL.Path)
	}))
	defer srv.Close()

	quotes, err := NewClient(srv.URL).OptionQuotes(context.Background(), nil)
	if err != nil {
		t.Fatalf("OptionQuotes(nil) error: %v", err)
	}
	if len(quotes) != 0 {
		t.Fatalf("OptionQuotes(nil) = %d quotes; want 0", len(quotes))
	}
}

func TestOptionQuotesBadSymbol(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("bad symbol must fail before any HTTP call, got %s", r.URL.Path)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"XX.TCH260807C335000"})
	if err == nil || !strings.Contains(err.Error(), "unsupported market") {
		t.Fatalf("OptionQuotes(bad symbol) error = %v; want unsupported market", err)
	}
}

func TestOptionQuotesSubscribeError(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"unknown field"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C335000"})
	if err == nil || !strings.Contains(err.Error(), "subscribe") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("OptionQuotes() error = %v; want subscribe HTTP 400", err)
	}
}

func TestOptionQuotesSnapshotFailureFallsBackToGreeks(t *testing.T) {
	fastLimits(t)
	resetGreeksCache(t)
	old := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = pw
	t.Cleanup(func() { os.Stderr = old })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, `{"ret_type":-1,"ret_msg":"no permission for market","err_code":null,"s2c":null}`)
		case "/api/option-quote":
			io.WriteString(w, `{"ret_type":0,"s2c":{"option_quote_list":[{"price":12.2,"mid":12.3,"iv":25.0,"delta":0.58,"theta":-0.03,"vol":1500.0,"mark_price":12.25,"open_interest":3204.0,"contract_size":100.0}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	before := time.Now()
	quotes, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C335000"})
	if err != nil {
		t.Fatalf("OptionQuotes() error: %v", err)
	}
	after := time.Now()
	q := quotes["HK.TCH260807C335000"]
	if q.Last != 12.2 || q.Volume != 1500 || q.Bid != 12.3 || q.Ask != 12.3 || q.OpenInterest != 3204 {
		t.Fatalf("greeks fallback quote = %+v; want price/volume/mid/OI populated", q)
	}
	if q.QuoteTime.IsZero() || q.QuoteTime.Before(before) || q.QuoteTime.After(after) {
		t.Fatalf("greeks fallback QuoteTime = %v; want within [%v, %v]", q.QuoteTime, before, after)
	}
	pw.Close()
	buf, _ := io.ReadAll(pr)
	if got := string(buf); !strings.Contains(got, "snapshot [1/1]: no permission for market") {
		t.Fatalf("stderr = %q; want snapshot warning with progress", got)
	}
}

func TestOptionQuotesGatewayConnectionError(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	_, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C335000"})
	if err == nil || !strings.Contains(err.Error(), "subscribe") {
		t.Fatalf("OptionQuotes() error = %v; want gateway connection failure", err)
	}
}

func TestOptionQuotesCache(t *testing.T) {
	fastLimits(t)
	resetGreeksCache(t)
	oldTTL := greeksTTL
	t.Cleanup(func() { greeksTTL = oldTTL })
	var snapshotCalls, greeksCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			snapshotCalls++
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[
				{"security":{"market":1,"code":"TCH260807C340000"},"cur_price":12.2,"volume":1500,"update_time":"2026-08-07 10:30:00"},
				{"security":{"market":1,"code":"TCH260807P340000"},"cur_price":3.3,"volume":800,"update_time":"2026-08-07 10:31:00"}
			]}}`)
		case "/api/option-quote":
			greeksCalls++
			io.WriteString(w, greeksResp(12.2, 12.3, 25.0, 0.58, -0.03, 2300, 100))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	syms := []string{"HK.TCH260807C340000", "HK.TCH260807P340000"}

	// cold start: full greeks pass, one option-quote per contract
	quotes, err := NewClient(srv.URL).OptionQuotes(context.Background(), syms)
	if err != nil {
		t.Fatalf("cold OptionQuotes() error: %v", err)
	}
	if greeksCalls != 2 || snapshotCalls != 1 {
		t.Fatalf("cold pass: greeks=%d snapshot=%d; want 2 greeks after 1 snapshot", greeksCalls, snapshotCalls)
	}
	if got := quotes["HK.TCH260807C340000"]; got.Bid != 12.3 || got.ImpliedVol != 0.25 {
		t.Fatalf("cold quote = %+v; want mid 12.3 iv 0.25", got)
	}

	// fresh cache (greeksTTL default 10min): the next tick only snapshots
	if _, err := NewClient(srv.URL).OptionQuotes(context.Background(), syms); err != nil {
		t.Fatalf("warm OptionQuotes() error: %v", err)
	}
	if greeksCalls != 2 || snapshotCalls != 2 {
		t.Fatalf("warm pass: greeks=%d snapshot=%d; want no greeks refetch", greeksCalls, snapshotCalls)
	}

	// stale cache (TTL shrunk): the next tick refetches every expired leg
	greeksTTL = time.Nanosecond
	if _, err := NewClient(srv.URL).OptionQuotes(context.Background(), syms); err != nil {
		t.Fatalf("stale OptionQuotes() error: %v", err)
	}
	if greeksCalls != 4 {
		t.Fatalf("stale pass: greeks=%d; want 4 (both legs refetched)", greeksCalls)
	}
}

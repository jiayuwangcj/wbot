package futu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// quoteFixture is the fixture-frozen /api/quote s2c for two option contracts
// (real gateway offline 2026-08-11; verify field paths against it on return).
const quoteFixture = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[
	{"security":{"market":1,"code":"TCH260807C335000"},"bid_price":12.1,"ask_price":12.3,"last_price":12.2,"volume":1500,"update_time":"2026-08-07 10:30:00",
	 "ex_data":{"implied_volatility":0.25,"delta":0.58,"theta":-0.03,"open_interest":2300,"lot_size":100}},
	{"security":{"market":1,"code":"TCH260807P335000"},"bid_price":3.2,"ask_price":3.4,"last_price":3.3,"volume":800,"update_time":"2026-08-07 10:31:00",
	 "ex_data":{"implied_volatility":0.24,"delta":-0.42,"theta":-0.02,"open_interest":1200,"lot_size":100}}
]}}`

func TestOptionQuotesSuccess(t *testing.T) {
	fastLimits(t)
	var subBody, quoteBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/subscribe":
			subBody = string(body)
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			quoteBody = string(body)
			io.WriteString(w, quoteFixture)
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
	if call.Bid != 12.1 || call.Ask != 12.3 || call.Last != 12.2 || call.Volume != 1500 {
		t.Fatalf("call quote = %+v; want bid 12.1 ask 12.3 last 12.2 volume 1500", call)
	}
	if call.ImpliedVol != 0.25 || call.Delta != 0.58 || call.Theta != -0.03 || call.OpenInterest != 2300 || call.LotSize != 100 {
		t.Fatalf("call Greeks = %+v; want iv 0.25 delta 0.58 theta -0.03 oi 2300 lot 100", call)
	}
	want := time.Date(2026, 8, 7, 10, 30, 0, 0, futuLoc)
	if !call.QuoteTime.Equal(want) {
		t.Fatalf("call QuoteTime = %v; want %v", call.QuoteTime, want)
	}
	put := quotes["HK.TCH260807P335000"]
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
}

func TestOptionQuotesMissingFieldsZero(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			// no ex_data, no bid/ask, malformed update_time: all stay zero
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[
				{"security":{"market":1,"code":"TCH260807C335000"},"last_price":12.2,"update_time":"not-a-time"}
			]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	quotes, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C335000"})
	if err != nil {
		t.Fatalf("OptionQuotes() error: %v", err)
	}
	q := quotes["HK.TCH260807C335000"]
	if q.Bid != 0 || q.Ask != 0 || q.ImpliedVol != 0 || q.Delta != 0 || q.Theta != 0 || q.OpenInterest != 0 || q.LotSize != 0 || !q.QuoteTime.IsZero() {
		t.Fatalf("missing fields must stay zero, got %+v", q)
	}
	if q.Last != 12.2 {
		t.Fatalf("present field must parse, Last = %v; want 12.2", q.Last)
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

func TestOptionQuotesQuoteError(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, `{"ret_type":-1,"ret_msg":"no permission for market","err_code":null,"s2c":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).OptionQuotes(context.Background(), []string{"HK.TCH260807C335000"})
	if err == nil || !strings.Contains(err.Error(), "no permission for market") {
		t.Fatalf("OptionQuotes() error = %v; want ret_msg surfaced", err)
	}
}

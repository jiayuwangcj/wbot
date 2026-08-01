package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu"
)

// gatewayLog captures the last /api/option-chain request body (window check).
type gatewayLog struct {
	mu    sync.Mutex
	chain string
}

// mockOptionsGateway runs a scriptable futu REST gateway (expiration + chain).
func mockOptionsGateway(t *testing.T, expBody, chainBody string, log *gatewayLog) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/option-expiration-date":
			io.WriteString(w, expBody)
		case "/api/option-chain":
			if log != nil {
				body, _ := io.ReadAll(r.Body)
				log.mu.Lock()
				log.chain = string(body)
				log.mu.Unlock()
			}
			io.WriteString(w, chainBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sampleExpirations mirrors a measured /api/option-expiration-date s2c
// (2026-07-31 already expired, distance -1; two future expiries).
const sampleExpirations = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"date_list":[
  {"strike_time":"2026-07-31","strike_timestamp":1785427200,"option_expiry_date_distance":-1,"cycle":1},
  {"strike_time":"2026-08-07","strike_timestamp":1786032000,"option_expiry_date_distance":5,"cycle":1},
  {"strike_time":"2026-08-28","strike_timestamp":1787846400,"option_expiry_date_distance":26,"cycle":1}
]}}`

// sampleChain mirrors a measured /api/option-chain s2c: two strikes × call/put.
const sampleChain = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"option_chain":[
  {"strike_time":"2026-08-07","strike_timestamp":1786032000,"option":[
    {"call":{"basic":{"security":{"market":1,"code":"TCH260807C335000"},"lot_size":100},"option_ex_data":{"type":1,"strike_price":335.0,"strike_time":"2026-08-07"}}},
    {"put":{"basic":{"security":{"market":1,"code":"TCH260807P335000"},"lot_size":100},"option_ex_data":{"type":2,"strike_price":335.0,"strike_time":"2026-08-07"}}}
  ]},
  {"strike_time":"2026-08-07","strike_timestamp":1786032000,"option":[
    {"call":{"basic":{"security":{"market":1,"code":"TCH260807C340000"},"lot_size":100},"option_ex_data":{"type":1,"strike_price":340.0,"strike_time":"2026-08-07"}}},
    {"put":{"basic":{"security":{"market":1,"code":"TCH260807P340000"},"lot_size":100},"option_ex_data":{"type":2,"strike_price":340.0,"strike_time":"2026-08-07"}}}
  ]}
]}}`

// fakeFutuChainer is a scriptable FutuOptionChainer for endpoint-level tests.
type fakeFutuChainer struct {
	expErr      error
	chainErr    error
	expirations []futu.OptionExpiry
	contracts   []futu.OptionContract
	gotSymbol   string
	gotWindow   string
}

func (f *fakeFutuChainer) Expirations(_ context.Context, symbol string) ([]futu.OptionExpiry, error) {
	f.gotSymbol = symbol
	if f.expErr != nil {
		return nil, f.expErr
	}
	if f.expirations != nil {
		return f.expirations, nil
	}
	return []futu.OptionExpiry{{Date: "2026-08-07", Timestamp: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), DistanceDays: 5, Cycle: 1}}, nil
}

func (f *fakeFutuChainer) Chain(_ context.Context, symbol string, begin, end time.Time) ([]futu.OptionContract, error) {
	f.gotWindow = begin.Format("2006-01-02") + ".." + end.Format("2006-01-02")
	if f.chainErr != nil {
		return nil, f.chainErr
	}
	if f.contracts != nil {
		return f.contracts, nil
	}
	return []futu.OptionContract{{Symbol: "HK.TCH260807C335000", OptionType: "call", Strike: 335, Expiry: begin, LotSize: 100}}, nil
}

func TestFutuOptionsProxySuccess(t *testing.T) {
	fastFutuLimits(t)
	log := &gatewayLog{}
	gw := mockOptionsGateway(t, sampleExpirations, sampleChain, log)

	h := FutuOptionsHandler(futuOptionChainer{client: futu.NewClient(gw.URL)})
	rec := get(t, h, "/v1/futu/options?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got optionsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Symbol != "HK.00700" || got.Expiry != "2026-08-07" {
		t.Fatalf("symbol/expiry = %q/%q; want HK.00700/2026-08-07 (nearest future)", got.Symbol, got.Expiry)
	}
	if len(got.Expirations) != 3 {
		t.Fatalf("expirations = %d; want 3 (%+v)", len(got.Expirations), got.Expirations)
	}
	first := got.Expirations[0]
	if first.Date != "2026-07-31" || first.DistanceDays != -1 || first.Cycle != 1 {
		t.Fatalf("first expiry = %+v; want expired 2026-07-31", first)
	}
	if want := time.Unix(1786032000, 0).UTC().Format(time.RFC3339); got.Expirations[1].Timestamp != want {
		t.Fatalf("timestamp = %q; want %q", got.Expirations[1].Timestamp, want)
	}
	if len(got.Contracts) != 4 {
		t.Fatalf("contracts = %d; want 4 (%+v)", len(got.Contracts), got.Contracts)
	}
	// Sorted by strike, call before put at the same strike.
	if got.Contracts[0].Strike != 335 || got.Contracts[0].OptionType != "call" || got.Contracts[0].Symbol != "HK.TCH260807C335000" {
		t.Fatalf("contract[0] = %+v; want 335 call first", got.Contracts[0])
	}
	if got.Contracts[1].OptionType != "put" || got.Contracts[1].Symbol != "HK.TCH260807P335000" {
		t.Fatalf("contract[1] = %+v; want 335 put", got.Contracts[1])
	}
	if got.Contracts[2].Strike != 340 || got.Contracts[2].LotSize != 100 {
		t.Fatalf("contract[2] = %+v; want 340 strike with lot size 100", got.Contracts[2])
	}
	// The chain request window must be the single selected expiry (dates only).
	log.mu.Lock()
	chainBody := log.chain
	log.mu.Unlock()
	if !strings.Contains(chainBody, `"begin_time":"2026-08-07"`) || !strings.Contains(chainBody, `"end_time":"2026-08-07"`) {
		t.Fatalf("chain window = %s; want [2026-08-07, 2026-08-07]", chainBody)
	}
}

func TestFutuOptionsExpiryFilter(t *testing.T) {
	fastFutuLimits(t)
	log := &gatewayLog{}
	gw := mockOptionsGateway(t, sampleExpirations, sampleChain, log)

	h := FutuOptionsHandler(futuOptionChainer{client: futu.NewClient(gw.URL)})
	rec := get(t, h, "/v1/futu/options?symbol=HK.00700&expiry=2026-08-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got optionsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Expiry != "2026-08-28" {
		t.Fatalf("expiry = %q; want the requested 2026-08-28", got.Expiry)
	}
	log.mu.Lock()
	chainBody := log.chain
	log.mu.Unlock()
	if !strings.Contains(chainBody, `"begin_time":"2026-08-28"`) || !strings.Contains(chainBody, `"end_time":"2026-08-28"`) {
		t.Fatalf("chain window = %s; want [2026-08-28, 2026-08-28]", chainBody)
	}
}

func TestFutuOptionsAllExpired(t *testing.T) {
	fastFutuLimits(t)
	log := &gatewayLog{}
	gw := mockOptionsGateway(t, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"date_list":[
  {"strike_time":"2026-07-31","strike_timestamp":1785427200,"option_expiry_date_distance":-1,"cycle":1}
]}}`, sampleChain, log)

	h := FutuOptionsHandler(futuOptionChainer{client: futu.NewClient(gw.URL)})
	rec := get(t, h, "/v1/futu/options?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got optionsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Expiry != "" || len(got.Contracts) != 0 {
		t.Fatalf("expiry/contracts = %q/%d; want \"\"/0 (no chain call)", got.Expiry, len(got.Contracts))
	}
	log.mu.Lock()
	chainCalled := log.chain != ""
	log.mu.Unlock()
	if chainCalled {
		t.Fatal("chain was called although every expiry is expired")
	}
}

func TestFutuOptionsBadParams(t *testing.T) {
	h := FutuOptionsHandler(&fakeFutuChainer{})
	for _, path := range []string{
		"/v1/futu/options",
		"/v1/futu/options?symbol=",
		"/v1/futu/options?symbol=00700",
		"/v1/futu/options?symbol=XX.00700",
		"/v1/futu/options?symbol=HK.00700&expiry=0807",
		"/v1/futu/options?symbol=HK.00700&expiry=2026-13-40",
		"/v1/futu/options?symbol=HK.00700&expiry=2026-08-07T00:00:00Z",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d; want 400 (body %s)", path, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", path, rec.Body)
		}
	}
}

func TestFutuOptionsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/futu/options?symbol=HK.00700", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	FutuOptionsHandler(&fakeFutuChainer{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestFutuOptionsGatewayUnreachable(t *testing.T) {
	fastFutuLimits(t)
	gw := mockOptionsGateway(t, sampleExpirations, sampleChain, nil)
	addr := gw.URL
	gw.Close() // closed listener → connection refused

	h := FutuOptionsHandler(futuOptionChainer{client: futu.NewClient(addr)})
	rec := get(t, h, "/v1/futu/options?symbol=HK.00700")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", rec.Code, rec.Body)
	}
	var errBody errorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("body %q not JSON: %v", rec.Body, err)
	}
	if errBody.Code != "dependency_failed" {
		t.Fatalf("code = %q; want dependency_failed", errBody.Code)
	}
	if !strings.Contains(errBody.Action, "gateway container") {
		t.Fatalf("action = %q; want gateway-container hint", errBody.Action)
	}
}

func TestFutuOptionsUpstreamErrorPassthrough(t *testing.T) {
	fastFutuLimits(t)
	tests := []struct {
		name     string
		expErr   error
		chainErr error
		wantMsg  string
	}{
		{"expirations business error", errors.New("no permission for market"), nil, "no permission for market"},
		{"chain timeout", nil, context.DeadlineExceeded, "Futu gateway unreachable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := FutuOptionsHandler(&fakeFutuChainer{expErr: tt.expErr, chainErr: tt.chainErr})
			rec := get(t, h, "/v1/futu/options?symbol=HK.00700")
			want := http.StatusBadGateway
			if tt.chainErr != nil {
				want = http.StatusServiceUnavailable
			}
			if rec.Code != want {
				t.Fatalf("status = %d; want %d (body %s)", rec.Code, want, rec.Body)
			}
			var errBody errorJSON
			if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("body %q not JSON: %v", rec.Body, err)
			}
			if errBody.Message != tt.wantMsg {
				t.Fatalf("message = %q; want %q", errBody.Message, tt.wantMsg)
			}
			if errBody.Code != "dependency_failed" || errBody.Action == "" {
				t.Fatalf("error body = %+v; want dependency_failed code with action", errBody)
			}
		})
	}
}

// TestFutuOptionsLiveGateway hits the real gateway when FUTU_LIVE_TEST=1
// (local verification with a running container; CI never sets it).
func TestFutuOptionsLiveGateway(t *testing.T) {
	if os.Getenv("FUTU_LIVE_TEST") == "" {
		t.Skip("FUTU_LIVE_TEST not set (real Futu gateway verification)")
	}
	h := FutuOptionsHandler(NewFutuOptionChainer())
	rec := get(t, h, "/v1/futu/options?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "expirations") {
		t.Fatalf("live options body missing expirations: %s", rec.Body)
	}
}

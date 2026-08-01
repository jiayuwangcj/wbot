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
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu"
)

// fastFutuLimits shrinks the shared futu rate pools so proxy tests run fast.
func fastFutuLimits(t *testing.T) {
	t.Helper()
	oldQ, oldS := futu.QuoteLimit, futu.SnapshotLimit
	futu.QuoteLimit = futu.NewLimiter(time.Microsecond)
	futu.SnapshotLimit = futu.NewLimiter(time.Microsecond)
	t.Cleanup(func() { futu.QuoteLimit, futu.SnapshotLimit = oldQ, oldS })
}

// fakeFutuQuoter is a scriptable FutuQuoter for endpoint-level tests.
type fakeFutuQuoter struct {
	err error
	s2c json.RawMessage
	got string
}

func (f *fakeFutuQuoter) Quote(_ context.Context, symbol string) (json.RawMessage, error) {
	f.got = symbol
	if f.err != nil {
		return nil, f.err
	}
	if f.s2c != nil {
		return f.s2c, nil
	}
	return json.RawMessage(`{"basic_qot_list":[{"cur_price":475.2}]}`), nil
}

// mockGateway runs a scriptable futu REST gateway (subscribe + quote).
func mockGateway(t *testing.T, quoteBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, quoteBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const sampleQuote = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[{"cur_price":475.2,"open_price":470.0,"high_price":479.8,"low_price":462.0,"volume":31100240,"name":"TENCENT","update_time":"2026-07-31 16:07:51","security":{"market":1,"code":"00700"}}]}}`

func TestFutuQuoteProxyPassthrough(t *testing.T) {
	fastFutuLimits(t)
	gw := mockGateway(t, sampleQuote)

	h := FutuQuoteHandler(futuQuoter{client: futu.NewClient(gw.URL)})
	rec := get(t, h, "/v1/futu/quote?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got struct {
		BasicQotList []struct {
			CurPrice  float64 `json:"cur_price"`
			OpenPrice float64 `json:"open_price"`
			HighPrice float64 `json:"high_price"`
			LowPrice  float64 `json:"low_price"`
			Volume    int64   `json:"volume"`
			Name      string  `json:"name"`
		} `json:"basic_qot_list"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got.BasicQotList) != 1 || got.BasicQotList[0].CurPrice != 475.2 || got.BasicQotList[0].OpenPrice != 470.0 ||
		got.BasicQotList[0].HighPrice != 479.8 || got.BasicQotList[0].LowPrice != 462.0 ||
		got.BasicQotList[0].Volume != 31100240 || got.BasicQotList[0].Name != "TENCENT" {
		t.Fatalf("s2c fields = %+v; want the gateway quote passthrough", got.BasicQotList)
	}
}

func TestFutuQuoteGatewayUnreachable(t *testing.T) {
	fastFutuLimits(t)
	gw := mockGateway(t, sampleQuote)
	addr := gw.URL
	gw.Close() // closed listener → connection refused

	h := FutuQuoteHandler(futuQuoter{client: futu.NewClient(addr)})
	rec := get(t, h, "/v1/futu/quote?symbol=HK.00700")
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
	if errBody.Error != errBody.Message {
		t.Fatalf("error alias %q != message %q", errBody.Error, errBody.Message)
	}
}

func TestFutuQuoteBadSymbol(t *testing.T) {
	h := FutuQuoteHandler(&fakeFutuQuoter{})
	for _, path := range []string{
		"/v1/futu/quote",
		"/v1/futu/quote?symbol=",
		"/v1/futu/quote?symbol=00700",
		"/v1/futu/quote?symbol=XX.00700",
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

func TestFutuQuoteMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/futu/quote?symbol=HK.00700", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	FutuQuoteHandler(&fakeFutuQuoter{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestFutuQuoteUpstreamErrorPassthrough(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{"business error", errors.New("no permission for market"), "no permission for market"},
		{"gateway HTTP error", errors.New("quote HK.00700: HTTP 401: missing bearer token"), "quote HK.00700: HTTP 401: missing bearer token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := FutuQuoteHandler(&fakeFutuQuoter{err: tt.err})
			rec := get(t, h, "/v1/futu/quote?symbol=HK.00700")
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d; want 502 (body %s)", rec.Code, rec.Body)
			}
			var errBody errorJSON
			if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("body %q not JSON: %v", rec.Body, err)
			}
			if errBody.Message != tt.wantMsg {
				t.Fatalf("message = %q; want upstream message %q", errBody.Message, tt.wantMsg)
			}
			if errBody.Code != "dependency_failed" || errBody.Action == "" {
				t.Fatalf("error body = %+v; want dependency_failed code with action", errBody)
			}
		})
	}
}

// TestFutuQuoteLiveGateway hits the real gateway when FUTU_LIVE_TEST=1
// (local verification with a running container; CI never sets it).
func TestFutuQuoteLiveGateway(t *testing.T) {
	if os.Getenv("FUTU_LIVE_TEST") == "" {
		t.Skip("FUTU_LIVE_TEST not set (real Futu gateway verification)")
	}
	h := FutuQuoteHandler(NewFutuQuoter())
	rec := get(t, h, "/v1/futu/quote?symbol=HK.00700")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "basic_qot_list") {
		t.Fatalf("live quote body missing basic_qot_list: %s", rec.Body)
	}
}

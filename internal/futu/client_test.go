package futu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			io.WriteString(w, "ok")
		case "/api/global-state":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"qot_logined":true,"trd_logined":true,"server_ver":1002,"time":1785554255}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	st, err := NewClient(srv.URL).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if st.Health != "ok" || !st.QotLogined || !st.TrdLogined || st.ServerVer != 1002 || st.Time != 1785554255 {
		t.Fatalf("Status() = %+v; want health=ok qot_logined=true trd_logined=true server_ver=1002 time=1785554255", st)
	}
	if st.Addr != srv.URL {
		t.Fatalf("Status().Addr = %q; want %q", st.Addr, srv.URL)
	}
}

func TestStatusGatewayDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	_, err := NewClient(addr).Status(context.Background())
	if err == nil {
		t.Fatal("Status() = nil error; want connection error")
	}
	if !strings.Contains(err.Error(), "health") {
		t.Fatalf("Status() error = %q; want mention of health step", err)
	}
}

func TestStatusHealth503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":"backend disconnected"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "backend disconnected") {
		t.Fatalf("Status() error = %v; want 503 with backend disconnected", err)
	}
}

func TestStatusGlobalStateBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			io.WriteString(w, "ok")
		case "/api/global-state":
			io.WriteString(w, `{"ret_type":-1,"ret_msg":"not logged in","err_code":null,"s2c":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("Status() error = %v; want ret_msg surfaced", err)
	}
}

func TestQuoteSuccess(t *testing.T) {
	var subBody, quoteBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/subscribe":
			subBody = string(body)
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			quoteBody = string(body)
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[{"cur_price":475.2,"high_price":479.8,"low_price":462.0,"open_price":470.0,"volume":31100240,"security":{"market":1,"code":"00700"},"name":"TENCENT","update_time":"2026-07-31 16:07:51"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s2c, err := NewClient(srv.URL).Quote(context.Background(), "HK.00700")
	if err != nil {
		t.Fatalf("Quote() error: %v", err)
	}
	var q struct {
		BasicQotList []struct {
			CurPrice float64 `json:"cur_price"`
			Name     string  `json:"name"`
		} `json:"basic_qot_list"`
	}
	if err := json.Unmarshal(s2c, &q); err != nil {
		t.Fatalf("unmarshal s2c: %v", err)
	}
	if len(q.BasicQotList) != 1 || q.BasicQotList[0].CurPrice != 475.2 || q.BasicQotList[0].Name != "TENCENT" {
		t.Fatalf("s2c = %s; want TENCENT at 475.2", s2c)
	}

	var sub map[string]any
	if err := json.Unmarshal([]byte(subBody), &sub); err != nil {
		t.Fatalf("subscribe body %q: %v", subBody, err)
	}
	if symbols := sub["symbols"].([]any); len(symbols) != 1 || symbols[0] != "HK.00700" {
		t.Fatalf("subscribe symbols = %v; want [HK.00700]", symbols)
	}
	if sub["is_sub_or_un_sub"] != true {
		t.Fatalf("subscribe is_sub_or_un_sub = %v; want true", sub["is_sub_or_un_sub"])
	}
	var qb map[string]any
	if err := json.Unmarshal([]byte(quoteBody), &qb); err != nil {
		t.Fatalf("quote body %q: %v", quoteBody, err)
	}
	sec := qb["security_list"].([]any)[0].(map[string]any)
	if sec["market"] != float64(1) || sec["code"] != "00700" {
		t.Fatalf("quote security_list = %v; want market=1 code=00700", sec)
	}
}

func TestQuoteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"missing bearer token"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).Quote(context.Background(), "HK.00700")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "missing bearer token") {
		t.Fatalf("Quote() error = %v; want 401 with missing bearer token", err)
	}
}

func TestQuoteBusinessError(t *testing.T) {
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

	_, err := NewClient(srv.URL).Quote(context.Background(), "HK.00700")
	if err == nil || !strings.Contains(err.Error(), "no permission for market") {
		t.Fatalf("Quote() error = %v; want ret_msg surfaced", err)
	}
}

func TestQuoteSubscribeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"unknown field"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).Quote(context.Background(), "HK.00700")
	if err == nil || !strings.Contains(err.Error(), "subscribe") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("Quote() error = %v; want subscribe HTTP 400", err)
	}
}

func TestParseSymbol(t *testing.T) {
	tests := []struct {
		symbol   string
		market   int
		code     string
		wantErr  bool
		errMatch string
	}{
		{"HK.00700", 1, "00700", false, ""},
		{"US.AAPL", 11, "AAPL", false, ""},
		{"SH.600000", 21, "600000", false, ""},
		{"SZ.000001", 22, "000001", false, ""},
		{"hk.00700", 1, "00700", false, ""}, // case-insensitive prefix
		{"00700", 0, "", true, "MARKET.CODE"},
		{"HK.", 0, "", true, "MARKET.CODE"},
		{"XX.00700", 0, "", true, "unsupported market"},
		{"HK.00700.MORE", 0, "", true, "MARKET.CODE"},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			market, code, err := ParseSymbol(tt.symbol)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSymbol(%q) = nil error; want error", tt.symbol)
				}
				if !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("parseSymbol(%q) error = %q; want containing %q", tt.symbol, err, tt.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSymbol(%q) error: %v", tt.symbol, err)
			}
			if market != tt.market || code != tt.code {
				t.Fatalf("parseSymbol(%q) = (%d, %q); want (%d, %q)", tt.symbol, market, code, tt.market, tt.code)
			}
		})
	}
}

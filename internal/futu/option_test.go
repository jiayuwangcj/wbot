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

const expirationPayload = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"date_list":[
	{"strike_time":"2026-07-31","strike_timestamp":1785427200.0,"option_expiry_date_distance":-1,"cycle":1},
	{"strike_time":"2026-08-07","strike_timestamp":1786032000.0,"option_expiry_date_distance":6,"cycle":1},
	{"strike_time":"2026-08-28","strike_timestamp":1787846400.0,"option_expiry_date_distance":27,"cycle":2}
]}}`

const chainPayload = `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"option_chain":[
	{"strike_time":"2026-08-07","strike_timestamp":1786032000.0,"option":[
		{"call":{"basic":{"security":{"market":1,"code":"TCH260807C335000"},"lot_size":100,"name":"腾讯 260807 335.00 购"},"option_ex_data":{"type":1,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-07","strike_price":335.0}},
		 "put":{"basic":{"security":{"market":1,"code":"TCH260807P335000"},"lot_size":100,"name":"腾讯 260807 335.00 沽"},"option_ex_data":{"type":2,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-07","strike_price":335.0}}},
		{"call":{"basic":{"security":{"market":1,"code":"TCH260807C750000"},"lot_size":100,"name":"腾讯 260807 750.00 购"},"option_ex_data":{"type":1,"owner":{"market":1,"code":"00700"},"strike_time":"2026-08-07","strike_price":750.0}},
		 "put":null}
	]}
]}}`

func TestOptionExpirations(t *testing.T) {
	fastLimits(t)
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/option-expiration-date" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("request body %q: %v", body, err)
		}
		io.WriteString(w, expirationPayload)
	}))
	defer srv.Close()

	exp, err := NewClient(srv.URL).OptionExpirations(context.Background(), "HK.00700")
	if err != nil {
		t.Fatalf("OptionExpirations() error: %v", err)
	}
	if len(exp) != 3 {
		t.Fatalf("got %d expiries; want 3", len(exp))
	}
	if exp[1].Date != "2026-08-07" || exp[1].DistanceDays != 6 || exp[1].Cycle != 1 {
		t.Fatalf("expiry[1] = %+v; want 2026-08-07 dist=6 cycle=1", exp[1])
	}
	if !exp[1].Timestamp.Equal(time.Unix(1786032000, 0).UTC()) {
		t.Fatalf("expiry[1] ts = %v; want 1786032000 UTC", exp[1].Timestamp)
	}
	owner := got["owner"].(map[string]any)
	if owner["market"] != float64(1) || owner["code"] != "00700" {
		t.Fatalf("owner = %v; want market=1 code=00700", owner)
	}
}

func TestOptionChain(t *testing.T) {
	fastLimits(t)
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/option-chain" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("request body %q: %v", body, err)
		}
		io.WriteString(w, chainPayload)
	}))
	defer srv.Close()

	begin := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	contracts, err := NewClient(srv.URL).OptionChain(context.Background(), "HK.00700", begin, end)
	if err != nil {
		t.Fatalf("OptionChain() error: %v", err)
	}
	if len(contracts) != 3 {
		t.Fatalf("got %d contracts; want 3 (2nd entry has a null put arm)", len(contracts))
	}
	c0 := contracts[0]
	if c0.Symbol != "HK.TCH260807C335000" || c0.OptionType != "call" || c0.Strike != 335.0 {
		t.Fatalf("contract[0] = %+v; want HK.TCH260807C335000 call 335", c0)
	}
	if c0.Underlying != "HK.00700" || c0.LotSize != 100 {
		t.Fatalf("contract[0] underlying/lot = %+v; want HK.00700 / 100", c0)
	}
	if !c0.Expiry.Equal(begin) {
		t.Fatalf("contract[0] expiry = %v; want %v", c0.Expiry, begin)
	}
	if contracts[1].Symbol != "HK.TCH260807P335000" || contracts[1].OptionType != "put" {
		t.Fatalf("contract[1] = %+v; want HK.TCH260807P335000 put", contracts[1])
	}
	if contracts[2].Symbol != "HK.TCH260807C750000" {
		t.Fatalf("contract[2] = %+v; want HK.TCH260807C750000", contracts[2])
	}
	if got["begin_time"] != "2026-08-07" || got["end_time"] != "2026-08-28" {
		t.Fatalf("begin/end = %v/%v; want YYYY-MM-DD window", got["begin_time"], got["end_time"])
	}
}

func TestOptionChainBadWindow(t *testing.T) {
	fastLimits(t)
	c := NewClient("http://127.0.0.1:1")
	begin := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	if _, err := c.OptionChain(context.Background(), "HK.00700", begin, end); err == nil {
		t.Fatal("OptionChain(begin after end) = nil error; want error")
	}
	if _, err := c.OptionChain(context.Background(), "HK.00700", time.Time{}, end); err == nil {
		t.Fatal("OptionChain(zero begin) = nil error; want error")
	}
	if _, err := c.OptionExpirations(context.Background(), "00700"); err == nil {
		t.Fatal("OptionExpirations(bad symbol) = nil error; want error")
	}
}

func TestOptionExpirationsGatewayError(t *testing.T) {
	fastLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"backend disconnected"}`)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL).OptionExpirations(context.Background(), "HK.00700")
	if err == nil || !strings.Contains(err.Error(), "option-expiration-date") || !strings.Contains(err.Error(), "503") {
		t.Fatalf("OptionExpirations() error = %v; want prefixed 503", err)
	}
}

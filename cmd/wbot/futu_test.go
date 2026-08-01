package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureRun captures stdout, stderr and the exit code of run().
func captureRun(t *testing.T, argv []string) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	or, ow, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = ow, ew
	code := run(argv)
	ow.Close()
	ew.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	stdout, err := io.ReadAll(or)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(er)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr), code
}

func TestFutuDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"no sub", []string{"wbot", "futu"}, 2},
		{"help", []string{"wbot", "futu", "-h"}, 0},
		{"bad sub", []string{"wbot", "futu", "nope"}, 2},
		{"status help", []string{"wbot", "futu", "status", "-h"}, 0},
		{"quote help", []string{"wbot", "futu", "quote", "-h"}, 0},
		{"quote no symbol", []string{"wbot", "futu", "quote"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestFutuStatusSuccess(t *testing.T) {
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

	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "status", "-addr", srv.URL})
	if code != 0 {
		t.Fatalf("run() = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"health": "ok"`, `"server_ver": 1002`, `"qot_logined": true`, `"trd_logined": true`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %s: %s", want, stdout)
		}
	}
}

func TestFutuStatusGatewayDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "status", "-addr", addr})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "futu: status:") {
		t.Fatalf("stderr = %q; want futu: status: prefix", stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q; want empty on failure", stdout)
	}
}

func TestFutuStatusHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"backend disconnected"}`)
	}))
	defer srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "futu", "status", "-addr", srv.URL})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "503") || !strings.Contains(stderr, "backend disconnected") {
		t.Fatalf("stderr = %q; want 503 backend disconnected", stderr)
	}
}

func TestFutuQuoteSuccess(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[{"cur_price":475.2,"high_price":479.8,"low_price":462.0,"open_price":470.0,"volume":31100240,"security":{"market":1,"code":"00700"},"name":"TENCENT","update_time":"2026-07-31 16:07:51"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "HK.00700", "-addr", srv.URL})
	if code != 0 {
		t.Fatalf("run() = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"name": "TENCENT"`, `"cur_price": 475.2`, `"security"`, `"code": "00700"`, `"volume": 31100240`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("quote output missing %s: %s", want, stdout)
		}
	}
}

func TestFutuQuoteUnreachable(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "HK.00700", "-addr", addr})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "futu: quote:") {
		t.Fatalf("stderr = %q; want futu: quote: prefix", stderr)
	}
}

func TestFutuQuoteUnauthorized(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"missing bearer token"}`)
	}))
	defer srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "HK.00700", "-addr", srv.URL})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "401") || !strings.Contains(stderr, "missing bearer token") {
		t.Fatalf("stderr = %q; want 401 missing bearer token", stderr)
	}
}

func TestFutuQuoteBadSymbol(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "00700"})
	if code != 2 {
		t.Fatalf("run() = %d; want 2", code)
	}
	if !strings.Contains(stderr, "MARKET.CODE") {
		t.Fatalf("stderr = %q; want MARKET.CODE hint", stderr)
	}
}

func TestFutuUsageMentionsREST(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "futu", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	for _, want := range []string{"status", "quote"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("futu usage missing %s: %s", want, stderr)
		}
	}
}

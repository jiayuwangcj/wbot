package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cmdHKEXZip(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestIngestHKEXDryRun(t *testing.T) {
	dtop := cmdHKEXZip(t, "20250702_1_dtop_o_seoch_opt_dtl_all.raw", strings.Join([]string{
		`"H","DTOP","DCASS","20250702","20250702195046","SEOCH",1`,
		`"01","SOM","STOCK OPTIONS","TCH","30","JUL","25",500.00,10228,9406,1293,2649,76,12.40,-1.90,17131,15287,2057,3067,153,9.82,-0.27`,
		`"T",2,"EOF"`,
	}, "\r\n")+"\r\n")
	rp := cmdHKEXZip(t, "20250702_1_rp006-final_o.raw", strings.Join([]string{
		`"H","RP006-FINAL","DCASS","20250702","20250702195046","SEOCH",01`,
		`"01","TCHSP","SOM","STOCK OPTIONS","TCH","TENCENT HOLDINGS","HKD",501.50,503.00,-1.50,`,
		`"01","TCH500.00G5","SOM","STOCK OPTIONS","TCH","TENCENT HOLDINGS","HKD",12.40,14.30,-1.90,20.5604`,
		`"01","TCH500.00S5","SOM","STOCK OPTIONS","TCH","TENCENT HOLDINGS","HKD",9.82,10.09,-0.27,19.4420`,
		`"T",3,"EOF"`,
	}, "\r\n")+"\r\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/DTOP_O_20250702.zip":
			_, _ = w.Write(dtop)
		case "/RP006_250702.zip":
			_, _ = w.Write(rp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{
		"wbot", "ingest", "hkex", "-dry-run", "-from", "2025-07-02", "-to", "2025-07-02",
		"-dtop-base", srv.URL, "-rp006-base", srv.URL, "-request-interval", "1ms",
	})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{
		"dry-run", "underlying=HK.00700", "class=TCH", "trading_days=1", "option_quotes=2",
		"snapshots=2", "inserted_quotes=0", "source=hkex", "projection=eod-settlement-bs-r0",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "progress date=2025-07-02") {
		t.Fatalf("stderr missing progress: %s", stderr)
	}
}

func TestIngestHKEXValidationAndUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "help", args: []string{"wbot", "ingest", "hkex", "-h"}, want: 0},
		{name: "non HK", args: []string{"wbot", "ingest", "hkex", "-symbol", "US.JD"}, want: 2},
		{name: "class", args: []string{"wbot", "ingest", "hkex", "-class", "?"}, want: 2},
		{name: "lot", args: []string{"wbot", "ingest", "hkex", "-lot-size", "0"}, want: 2},
		{name: "date", args: []string{"wbot", "ingest", "hkex", "-from", "nope"}, want: 2},
		{name: "range", args: []string{"wbot", "ingest", "hkex", "-from", "2025-07-03", "-to", "2025-07-02"}, want: 2},
		{name: "future", args: []string{"wbot", "ingest", "hkex", "-from", "2099-01-01", "-to", "2099-01-01"}, want: 2},
		{name: "interval", args: []string{"wbot", "ingest", "hkex", "-from", "2025-07-02", "-to", "2025-07-02", "-request-interval", "1ms"}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args); got != tt.want {
				t.Fatalf("run(%v)=%d want %d", tt.args, got, tt.want)
			}
		})
	}
	_, usage, code := captureRun(t, []string{"wbot", "ingest", "hkex", "-h"})
	if code != 0 {
		t.Fatalf("help code=%d", code)
	}
	for _, want := range []string{"DTOP", "RP006-FINAL", "settlement", "source=hkex", "Black-Scholes", "research", "idempotent", "one second", "300"} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage missing %q: %s", want, usage)
		}
	}
	_, ingestUsage, code := captureRun(t, []string{"wbot", "ingest", "-h"})
	if code != 0 || !strings.Contains(ingestUsage, "hkex") {
		t.Fatalf("ingest usage code/body=%d/%q", code, ingestUsage)
	}
}

func TestLoopbackHTTPBase(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://127.0.0.1:1234":   true,
		"https://[::1]:1234/x":    true,
		"http://localhost/x":      true,
		"https://www.hkex.com.hk": false,
		"not a url":               false,
	} {
		if got := loopbackHTTPBase(raw); got != want {
			t.Errorf("loopbackHTTPBase(%q)=%v want %v", raw, got, want)
		}
	}
}

func Example_parseHKEXDate() {
	t, _ := parseHKEXDate("2025-07-02")
	fmt.Println(t.Format("2006-01-02"))
	// Output: 2025-07-02
}

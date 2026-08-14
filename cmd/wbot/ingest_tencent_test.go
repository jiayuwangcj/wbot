package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIngestTencentDispatchAndValidation(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"help", []string{"wbot", "ingest", "tencent", "-h"}, 0},
		{"bad symbol", []string{"wbot", "ingest", "tencent", "-symbol", "00700"}, 2},
		{"bad timeframe", []string{"wbot", "ingest", "tencent", "-timeframe", "1h"}, 2},
		{"bad count low", []string{"wbot", "ingest", "tencent", "-count", "0"}, 2},
		{"bad count high", []string{"wbot", "ingest", "tencent", "-count", "1001"}, 2},
		{"bad from", []string{"wbot", "ingest", "tencent", "-from", "nope"}, 2},
		{"bad range", []string{"wbot", "ingest", "tencent", "-from", "2026-08-14T00:00:00Z", "-to", "2026-08-13T00:00:00Z"}, 2},
		{"empty run source", []string{"wbot", "ingest", "tencent", "-source", ""}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestIngestTencentDryRun(t *testing.T) {
	const payload = `{"code":0,"msg":"","data":{"hk00700":{"day":[
  ["2026-08-07","479.000","478.800","483.200","475.400","16319939.000"],
  ["2026-08-10","479.000","481.400","483.600","476.400","15508724.000"]
]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("param"), "hk00700,day") {
			http.Error(w, "bad param", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "ingest", "tencent", "-symbol", "HK.00700", "-count", "2", "-dry-run", "-endpoint", srv.URL})
	if code != 0 {
		t.Fatalf("code = %d; stderr=%s", code, stderr)
	}
	for _, want := range []string{"2 bars", "2026-08-06T16:00:00Z", "2026-08-09T16:00:00Z", "symbol=HK.00700", "source=tencent", "adjusted=qfq"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("stderr = %q; want empty", stderr)
	}
}

func TestIngestTencentUSDryRunWarnsSingleDay(t *testing.T) {
	const payload = `{"code":0,"msg":"","data":{"usJD":{"day":[
  ["2026-08-13","30.83","29.30","31.37","28.86","32093930"]
]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, payload) }))
	defer srv.Close()
	stdout, stderr, code := captureRun(t, []string{"wbot", "ingest", "tencent", "-symbol", "US.JD", "-dry-run", "-endpoint", srv.URL})
	if code != 0 || !strings.Contains(stdout, "1 bars") || !strings.Contains(stderr, "腾讯美股仅当日,历史靠每日积累") {
		t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
}

func TestIngestUsageListsTencent(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "-h"})
	if code != 0 || !strings.Contains(stderr, "tencent") {
		t.Fatalf("code/stderr = %d/%q", code, stderr)
	}
}

func TestIngestTencentUsageDocumentsContract(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "ingest", "tencent", "-h"})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"HK.00700", "US.JD", "-count", "-include-forming", "discarded", "Beijing", "source=tencent", "adjust=fwd", "one second", "exponential backoff"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("usage missing %q: %s", want, stderr)
		}
	}
}

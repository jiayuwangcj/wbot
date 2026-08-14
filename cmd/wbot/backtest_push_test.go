package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/discord"
)

func writeBacktestPushBars(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bars.json")
	data := `[{"ts":"2026-08-12T00:00:00Z","open":100,"high":101,"low":99,"close":100,"volume":1000},{"ts":"2026-08-13T00:00:00Z","open":100,"high":102,"low":99,"close":101,"volume":1100}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBacktestPushRequiresReport(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "backtest", "-file", writeBacktestPushBars(t), "-push"})
	if code != 2 || !strings.Contains(stderr, "-push requires -report") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = captureRun(t, []string{"wbot", "backtest", "-dsn", "postgres://unused", "-export", "1", "-report", "-push"})
	if code != 2 || !strings.Contains(stderr, "-push cannot be combined with -export") {
		t.Fatalf("export: code=%d stderr=%q", code, stderr)
	}
}

func TestBacktestPushMissingConfigKeepsReport(t *testing.T) {
	configDir, reportDir := t.TempDir(), t.TempDir()
	t.Setenv("WBOT_CONFIG_DIR", configDir)
	stdout, stderr, code := captureRun(t, []string{"wbot", "backtest", "-file", writeBacktestPushBars(t), "-symbol", "HK.00700", "-report", "-push", "-report-dir", reportDir})
	if code != 1 || !strings.Contains(stderr, "credentials.discord.bot_token") || !strings.Contains(stderr, "credentials.discord.channel_id") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "report_id=") {
		t.Fatalf("report output lost on push failure: %q", stdout)
	}
	jsonReports, _ := filepath.Glob(filepath.Join(reportDir, "*.json"))
	htmlReports, _ := filepath.Glob(filepath.Join(reportDir, "*.html"))
	if len(jsonReports) != 1 || len(htmlReports) != 1 {
		t.Fatalf("reports after push failure = %v / %v", jsonReports, htmlReports)
	}
}

func TestBacktestPushFailureRetryAndDuplicateCLI(t *testing.T) {
	var (
		mu       sync.Mutex
		requests [][]byte
		auth     []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, body)
		auth = append(auth, r.Header.Get("Authorization"))
		attempt := len(requests)
		mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/channels/channel-7/messages" {
			http.NotFound(w, r)
			return
		}
		if attempt == 1 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"message-1"}`)
	}))
	defer server.Close()

	configDir, reportDir := t.TempDir(), t.TempDir()
	cfg, err := config.Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"credentials.discord.bot_token":  "fake-token",
		"credentials.discord.channel_id": "channel-7",
	} {
		if err := cfg.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WBOT_CONFIG_DIR", configDir)
	t.Setenv("DISCORD_API_BASE_URL", server.URL)
	argv := []string{"wbot", "backtest", "-file", writeBacktestPushBars(t), "-symbol", "HK.00700", "-report", "-push", "-report-dir", reportDir}

	stdout1, stderr1, code1 := captureRun(t, argv)
	if code1 != 1 || !strings.Contains(stdout1, "report_id=") || !strings.Contains(stderr1, "HTTP status 503") {
		t.Fatalf("first push: code=%d stdout=%q stderr=%q", code1, stdout1, stderr1)
	}
	stdout2, stderr2, code2 := captureRun(t, argv)
	if code2 != 0 || stderr2 != "" || !strings.Contains(stdout2, "push_status=sent") {
		t.Fatalf("retry: code=%d stdout=%q stderr=%q", code2, stdout2, stderr2)
	}
	stdout3, stderr3, code3 := captureRun(t, argv)
	if code3 != 0 || stderr3 != "" || !strings.Contains(stdout3, "push_status=already_sent") {
		t.Fatalf("duplicate: code=%d stdout=%q stderr=%q", code3, stdout3, stderr3)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("Discord POSTs = %d; want failed attempt + retry only", len(requests))
	}
	if auth[0] != "Bot fake-token" || auth[1] != "Bot fake-token" {
		t.Fatalf("authorization headers = %q", auth)
	}
	var first, second struct {
		Nonce        string          `json:"nonce"`
		EnforceNonce bool            `json:"enforce_nonce"`
		Embeds       []discord.Embed `json:"embeds"`
	}
	if err := json.Unmarshal(requests[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(requests[1], &second); err != nil {
		t.Fatal(err)
	}
	if first.Nonce == "" || first.Nonce != second.Nonce || !first.EnforceNonce || !second.EnforceNonce {
		t.Fatalf("nonce retry contract = %#v / %#v", first, second)
	}
	if len(second.Embeds) != 1 || len(second.Embeds[0].Fields) != 7 {
		t.Fatalf("embed fields = %#v", second.Embeds)
	}
	reportFiles, _ := filepath.Glob(filepath.Join(reportDir, "*.json"))
	if len(reportFiles) != 1 {
		t.Fatalf("JSON reports = %v", reportFiles)
	}
	raw, err := os.ReadFile(reportFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	var report backtestreport.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	for _, risk := range report.Risk {
		if !strings.Contains(second.Embeds[0].Description, risk) {
			t.Fatalf("Discord description lost risk %q", risk)
		}
	}
}

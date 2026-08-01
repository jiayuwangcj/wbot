package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/httpapi"
	"github.com/jiayu/wbot/internal/httpregister"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/master"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"no args", []string{"wbot"}, 2},
		{"help flag", []string{"wbot", "-h"}, 0},
		{"help long", []string{"wbot", "--help"}, 0},
		{"help cmd", []string{"wbot", "help"}, 0},
		{"version flag", []string{"wbot", "-version"}, 0},
		{"version cmd", []string{"wbot", "version"}, 0},
		{"agent poll smoke", []string{"wbot", "agent", "-duration", "1ms", "-interval", "1ms"}, 0},
		{"agent help", []string{"wbot", "agent", "-h"}, 0},
		{"master short run", []string{"wbot", "master", "-duration", "1ms"}, 0},
		{"master tls flag mismatch", []string{"wbot", "master", "-tls-cert", "only.pem"}, 2},
		{"paper submit", []string{"wbot", "paper", "-symbol", "T.US", "-side", "sell"}, 0},
		{"paper bad side", []string{"wbot", "paper", "-side", "maybe"}, 2},
		{"agent bad flag", []string{"wbot", "agent", "-notaflag"}, 2},
		{"unknown", []string{"wbot", "nope"}, 2},
		{"ingest no sub", []string{"wbot", "ingest"}, 2},
		{"ingest help", []string{"wbot", "ingest", "-h"}, 0},
		{"ingest bad sub", []string{"wbot", "ingest", "nope"}, 2},
		{"ingest mock help", []string{"wbot", "ingest", "mock", "-h"}, 0},
		{"ingest file bad from", []string{"wbot", "ingest", "file", "-file", "/dev/null", "-from", "not-a-time"}, 2},
		{"ingest file help", []string{"wbot", "ingest", "file", "-h"}, 0},
		{"ingest file no path", []string{"wbot", "ingest", "file"}, 2},
		{"ingest url help", []string{"wbot", "ingest", "url", "-h"}, 0},
		{"ingest url bad to", []string{"wbot", "ingest", "url", "-url", "http://127.0.0.1:1/bars.json", "-to", "x"}, 2},
		{"ingest url no url", []string{"wbot", "ingest", "url"}, 2},
		{"ingest status help", []string{"wbot", "ingest", "status", "-h"}, 0},
		{"ingest bars help", []string{"wbot", "ingest", "bars", "-h"}, 0},
		{"ingest bars bad from", []string{"wbot", "ingest", "bars", "-from", "not-a-time"}, 2},
		{"backtest help", []string{"wbot", "backtest", "-h"}, 0},
		{"backtest both inputs", []string{"wbot", "backtest", "-file", "/dev/null", "-dsn", "postgres://x"}, 2},
		{"backtest dsn no value", []string{"wbot", "backtest", "-dsn"}, 2},
		{"backtest bad strategy", []string{"wbot", "backtest", "-file", "/dev/null", "-strategy", "nope"}, 2},
		{"backtest bad maxdrawdown high", []string{"wbot", "backtest", "-file", "/dev/null", "-max-drawdown", "1.5"}, 2},
		{"backtest bad maxdrawdown neg", []string{"wbot", "backtest", "-file", "/dev/null", "-max-drawdown", "-0.1"}, 2},
		{"serve help", []string{"wbot", "serve", "-h"}, 0},
		{"configyaml help", []string{"wbot", "configyaml", "-h"}, 0},
		{"configyaml missing file", []string{"wbot", "configyaml", "-file", "/nonexistent/config.yaml"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run() = %d; want %d", got, tt.want)
			}
		})
	}
}

// TestRunRequiresDSN: exit-2 cases that assume WBOT_PG_DSN is unset (missing-DSN
// rejection); skipped under db-integration, where the DSN is present by design.
func TestRunRequiresDSN(t *testing.T) {
	if os.Getenv("WBOT_PG_DSN") != "" {
		t.Skip("WBOT_PG_DSN set; missing-DSN exit codes not applicable")
	}
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"ingest mock no dsn", []string{"wbot", "ingest", "mock"}, 2},
		{"ingest mock no dsn with every", []string{"wbot", "ingest", "mock", "-every", "1ms"}, 2},
		{"ingest file no dsn", []string{"wbot", "ingest", "file", "-file", "/dev/null"}, 2},
		{"ingest url no dsn", []string{"wbot", "ingest", "url", "-url", "http://127.0.0.1:1/bars.json"}, 2},
		{"ingest futu no dsn", []string{"wbot", "ingest", "futu", "-symbol", "HK.00700", "-timeframe", "K_DAY"}, 2},
		{"ingest status no dsn", []string{"wbot", "ingest", "status"}, 2},
		{"ingest bars no dsn", []string{"wbot", "ingest", "bars"}, 2},
		{"ingest bars json no dsn", []string{"wbot", "ingest", "bars", "-json"}, 2},
		{"serve no dsn", []string{"wbot", "serve"}, 2},
		{"backtest no file", []string{"wbot", "backtest"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestConfigYAMLOutput(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	content := "futu:\n  login_account: \"${WBOT_TESTYAML_CLI_ACCOUNT:-acc-default}\"\n  login_region: \"${WBOT_TESTYAML_CLI_REGION:-sh}\"\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureRunOutput(t, []string{"wbot", "configyaml", "-file", cfg})
	want := "FUTU_LOGIN_ACCOUNT=acc-default\nFUTU_LOGIN_REGION=sh\n"
	if out != want {
		t.Fatalf("output = %q; want %q", out, want)
	}
}

// captureRunOutput runs run() with stdout redirected and returns what it printed.
func captureRunOutput(t *testing.T, argv []string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := run(argv)
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("run(%v) = %d; want 0", argv, code)
	}
	return string(out)
}

func TestServeHelpMentionsAdminEndpoints(t *testing.T) {
	out := serveHelpOutput(t)
	for _, want := range []string{"/v1/admin/status", "/v1/admin/cluster"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("serve help missing %s: %q", want, out)
		}
	}
}

func serveHelpOutput(t *testing.T) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	code := run([]string{"wbot", "serve", "-h"})
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	return string(out)
}

// TestServeReportsActualListenAddr: with -listen 127.0.0.1:0 serve must report the
// bound port (ln.Addr), not the flag value; runs serve as a child process so a live
// /v1/admin/status can be queried (skipped without WBOT_PG_DSN).
func TestServeReportsActualListenAddr(t *testing.T) {
	if os.Getenv("WBOT_SERVE_HELPER") == "1" {
		os.Exit(run([]string{"wbot", "serve", "-listen", "127.0.0.1:0"}))
	}
	if os.Getenv("WBOT_PG_DSN") == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestServeReportsActualListenAddr$")
	cmd.Env = append(os.Environ(), "WBOT_SERVE_HELPER=1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	const prefix = "httpapi: listening on http://"
	var addr string
	var log []string
	for addr == "" {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("serve helper exited before listening (output: %s)", strings.Join(log, " | "))
			}
			log = append(log, line)
			if strings.HasPrefix(line, prefix) {
				addr = strings.TrimPrefix(line, prefix)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("serve helper did not print listen addr (output: %s)", strings.Join(log, " | "))
		}
	}
	if addr == "127.0.0.1:0" {
		t.Fatal("reported the -listen flag value; want the actual bound address")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("http://" + addr + "/v1/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["listen_addr"] != addr {
		t.Fatalf("listen_addr = %v; want %q", got["listen_addr"], addr)
	}
}

func TestServeHelpMentionsAdminStatus(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/admin/status") {
		t.Fatalf("serve help missing /v1/admin/status: %q", out)
	}
}

func TestServeHelpMentionsAdminConfig(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/v1/admin/config") {
		t.Fatalf("serve help missing /v1/admin/config: %q", out)
	}
}

func TestAgentMasterURL(t *testing.T) {
	mem := master.NewMemory()
	srv := httptest.NewServer(httpregister.Handler(mem))
	defer srv.Close()
	if got := run([]string{"wbot", "agent", "-duration", "5ms", "-interval", "1ms", "-master-url", srv.URL}); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}

func TestMasterTLSMissingFiles(t *testing.T) {
	if got := run([]string{"wbot", "master", "-tls-cert", "/nonexistent/cert.pem", "-tls-key", "/nonexistent/key.pem", "-duration", "1ms"}); got != 1 {
		t.Fatalf("run() = %d; want 1", got)
	}
}

func TestMasterTLSShortRun(t *testing.T) {
	certPath, keyPath := writeTestCertPair(t)
	port := freeTCPPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	argv := []string{"wbot", "master", "-listen", addr, "-tls-cert", certPath, "-tls-key", keyPath, "-duration", "1ms"}
	if got := run(argv); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}

func writeTestCertPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"wbot-test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// serveFakeStore is a no-data httpapi.Store for serve-mux tests (no DB needed).
type serveFakeStore struct{}

func (serveFakeStore) QueryBars(context.Context, string, string, string, time.Time, time.Time, int) ([]ingest.Bar, error) {
	return nil, nil
}

func (serveFakeStore) RecentRuns(context.Context, int) ([]ingest.RunStatus, error) {
	return nil, nil
}

func (serveFakeStore) RunStatusCounts(context.Context) (ingest.RunCounts, error) {
	return ingest.RunCounts{}, nil
}

func (serveFakeStore) BarCoverage(context.Context) ([]ingest.BarCoverage, error) {
	return nil, nil
}

func (serveFakeStore) Ping(context.Context) error {
	return nil
}

func serveMuxForTest() http.Handler {
	meta := httpapi.ProcessMeta{Version: "v-test", StartedAt: time.Now(), ListenAddr: "127.0.0.1:8080"}
	return serveMux(meta, httpapi.PingerFunc(func(context.Context) error { return nil }), serveFakeStore{})
}

func serveGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServeMuxRootRedirectsToUI(t *testing.T) {
	rec := serveGet(t, serveMuxForTest(), "/")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d; want 301 (body %s)", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/ui/" {
		t.Fatalf("Location = %q; want /ui/", loc)
	}
}

func TestServeMuxRootMethodNotRedirected(t *testing.T) {
	rec := httptest.NewRecorder()
	serveMuxForTest().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeMuxUIServesPages(t *testing.T) {
	top := serveMuxForTest()
	rec := serveGet(t, top, "/ui/")
	if rec.Code != http.StatusOK {
		t.Fatalf("/ui/: status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("/ui/: content-type = %q; want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · Data</title>") {
		t.Fatalf("/ui/ missing title: %s", rec.Body)
	}
	rec = serveGet(t, top, "/ui/admin.html")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>wbot · Admin</title>") {
		t.Fatalf("/ui/admin.html: status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestServeMuxUIServesAssets(t *testing.T) {
	top := serveMuxForTest()
	tests := []struct {
		path string
		ct   string
	}{
		{"/ui/style.css", "text/css"},
		{"/ui/app.js", "text/javascript"},
	}
	for _, tt := range tests {
		rec := serveGet(t, top, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; want 200 (body %s)", tt.path, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.ct) {
			t.Fatalf("%s: content-type = %q; want %s prefix", tt.path, ct, tt.ct)
		}
	}
	rec := serveGet(t, top, "/ui/nope.txt")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/ui/nope.txt: status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestServeMuxAPIRegression(t *testing.T) {
	top := serveMuxForTest()
	tests := []struct {
		path string
		want int
	}{
		{"/v1/bars", http.StatusBadRequest}, // missing symbol/timeframe
		{"/v1/runs", http.StatusOK},
		{"/v1/health", http.StatusOK},
		{"/v1/nope", http.StatusNotFound},
	}
	for _, tt := range tests {
		rec := serveGet(t, top, tt.path)
		if rec.Code != tt.want {
			t.Fatalf("%s: status = %d; want %d (body %s)", tt.path, rec.Code, tt.want, rec.Body)
		}
	}
	rec := serveGet(t, top, "/v1/nope")
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
		t.Fatalf("/v1/nope body %q; want JSON error", rec.Body)
	}
}

func TestServeHelpMentionsWebUI(t *testing.T) {
	if out := serveHelpOutput(t); !strings.Contains(out, "/ui/") {
		t.Fatalf("serve help missing /ui/: %q", out)
	}
}

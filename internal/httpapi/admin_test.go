package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// fakePinger is a scriptable Pinger for unit tests.
type fakePinger struct {
	err    error
	delay  time.Duration
	gotCtx context.Context
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.gotCtx = ctx
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.err
}

func testMeta() ProcessMeta {
	return ProcessMeta{
		Version:    "v9.9.9-test",
		StartedAt:  time.Now().Add(-2 * time.Second).Truncate(time.Second),
		ListenAddr: "127.0.0.1:8080",
	}
}

func TestAdminStatusOK(t *testing.T) {
	meta := testMeta()
	p := &fakePinger{delay: 5 * time.Millisecond}
	rec := get(t, AdminHandler(meta, p), "/v1/admin/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got["version"] != "v9.9.9-test" {
		t.Fatalf("version = %v; want v9.9.9-test", got["version"])
	}
	if got["pid"] != float64(os.Getpid()) {
		t.Fatalf("pid = %v; want %d", got["pid"], os.Getpid())
	}
	started, err := time.Parse(time.RFC3339, got["started_at"].(string))
	if err != nil || !started.Equal(meta.StartedAt) {
		t.Fatalf("started_at = %v; want %s", got["started_at"], meta.StartedAt.Format(time.RFC3339))
	}
	up, ok := got["uptime_seconds"].(float64)
	if !ok || up < 1.5 || up > 3.5 {
		t.Fatalf("uptime_seconds = %v; want ~2", got["uptime_seconds"])
	}
	if got["listen_addr"] != "127.0.0.1:8080" {
		t.Fatalf("listen_addr = %v; want 127.0.0.1:8080", got["listen_addr"])
	}
	db, ok := got["db"].(map[string]any)
	if !ok || db["ok"] != true {
		t.Fatalf("db = %v; want ok:true", got["db"])
	}
	lat, ok := db["latency_ms"].(float64)
	if !ok || lat <= 0 {
		t.Fatalf("latency_ms = %v; want positive", db["latency_ms"])
	}
	// Ping runs with a ≤3s timeout context.
	deadline, ok := p.gotCtx.Deadline()
	if !ok || time.Until(deadline) > pingTimeout {
		t.Fatalf("ping context deadline = %v; want <= %s", deadline, pingTimeout)
	}
}

func TestAdminStatusDBDown(t *testing.T) {
	meta := testMeta()
	rec := get(t, AdminHandler(meta, &fakePinger{err: errors.New("connection refused")}), "/v1/admin/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	db, ok := got["db"].(map[string]any)
	if !ok || db["ok"] != false {
		t.Fatalf("db = %v; want ok:false", got["db"])
	}
	if _, ok := db["latency_ms"]; ok {
		t.Fatalf("db = %v; want latency_ms omitted when down", got["db"])
	}
	if got["version"] != meta.Version || got["listen_addr"] != meta.ListenAddr {
		t.Fatalf("process fields = %v; want injected meta on db down", got)
	}
}

// TestAdminStatusPingIgnoresCtx: a pinger that ignores ctx and outlives the
// timeout must still report ok:false with latency omitted (review P2, PR #33).
func TestAdminStatusPingIgnoresCtx(t *testing.T) {
	meta := testMeta()
	p := &fakePinger{delay: pingTimeout + 500*time.Millisecond}
	rec := get(t, AdminHandler(meta, p), "/v1/admin/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	db, ok := got["db"].(map[string]any)
	if !ok || db["ok"] != false {
		t.Fatalf("db = %v; want ok:false when ping ignores ctx", got["db"])
	}
	if _, ok := db["latency_ms"]; ok {
		t.Fatalf("db = %v; want latency_ms omitted when timed out", got["db"])
	}
	if got["version"] != meta.Version {
		t.Fatalf("version = %v; want %s on timeout path", got["version"], meta.Version)
	}
}

func TestAdminStatusMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/status", nil)
	rec := httptest.NewRecorder()
	AdminHandler(testMeta(), &fakePinger{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestAdminStatusUnknownPath(t *testing.T) {
	for _, path := range []string{"/v1/admin/", "/v1/admin/nope", "/v1/admin/status/extra"} {
		rec := get(t, AdminHandler(testMeta(), &fakePinger{}), path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d; want 404 (body %s)", path, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", path, rec.Body)
		}
	}
}

// TestAdminComposedHandler mirrors serve's wiring: admin namespace + data API on one mux.
func TestAdminComposedHandler(t *testing.T) {
	top := http.NewServeMux()
	top.Handle("/v1/admin/", AdminHandler(testMeta(), &fakePinger{}))
	top.Handle("/", Handler(&fakeStore{}))

	rec := get(t, top, "/v1/admin/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/bars?symbol=DEMO.US&timeframe=1d")
	if rec.Code != http.StatusOK {
		t.Fatalf("bars = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

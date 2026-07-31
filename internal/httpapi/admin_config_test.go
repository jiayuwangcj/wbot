package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/config"
)

// fakeConfigStore is a scriptable ConfigStore for error-path tests.
type fakeConfigStore struct {
	entries []config.Entry
	listErr error
	setErr  error
	gotKey  string
	gotVal  string
}

func (f *fakeConfigStore) List() ([]config.Entry, error) { return f.entries, f.listErr }
func (f *fakeConfigStore) Set(key, value string) error {
	f.gotKey, f.gotVal = key, value
	return f.setErr
}

func configTestStore(t *testing.T) *config.Store {
	t.Helper()
	s, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func put(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestConfigListAllKeysUnset(t *testing.T) {
	rec := get(t, ConfigHandler(configTestStore(t)), "/v1/admin/config")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got []config.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got) != len(config.WhitelistedKeys) {
		t.Fatalf("len = %d; want %d", len(got), len(config.WhitelistedKeys))
	}
	for i, e := range got {
		if e.Key != config.WhitelistedKeys[i].Name || e.Group != config.WhitelistedKeys[i].Group {
			t.Fatalf("entry[%d] = %+v; want key %q group %q", i, e, config.WhitelistedKeys[i].Name, config.WhitelistedKeys[i].Group)
		}
		if e.Set || e.UpdatedAt != nil {
			t.Fatalf("entry[%d] = %+v; want unset with nil updated_at", i, e)
		}
	}
}

// TestConfigPutThenListSet is the core contract: PUT persists, GET shows
// set:true with updated_at, and neither response ever contains the value.
func TestConfigPutThenListSet(t *testing.T) {
	store := configTestStore(t)
	h := ConfigHandler(store)
	const val = "leak-sentinel-wx-42"

	rec := put(t, h, "/v1/admin/config/credentials.wechat.appid", `{"value":"`+val+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), val) {
		t.Fatalf("PUT response leaks value: %s", rec.Body)
	}
	var putResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &putResp); err != nil {
		t.Fatal(err)
	}
	if putResp["key"] != "credentials.wechat.appid" || putResp["set"] != true {
		t.Fatalf("put body = %v; want key + set:true", putResp)
	}

	rec = get(t, h, "/v1/admin/config")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), val) {
		t.Fatalf("GET response leaks value: %s", rec.Body)
	}
	var got []config.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Key == "credentials.wechat.appid" {
			if !e.Set || e.UpdatedAt == nil {
				t.Fatalf("appid entry = %+v; want set:true with updated_at", e)
			}
			if _, err := time.Parse(time.RFC3339, *e.UpdatedAt); err != nil {
				t.Fatalf("updated_at %q not RFC3339: %v", *e.UpdatedAt, err)
			}
		} else if e.Set {
			t.Fatalf("unexpected set key %s (body %s)", e.Key, rec.Body)
		}
	}
}

func TestConfigPutUnknownKey404(t *testing.T) {
	h := ConfigHandler(configTestStore(t))
	for _, path := range []string{"/v1/admin/config/credentials.wechat.foo", "/v1/admin/config/nope"} {
		rec := put(t, h, path, `{"value":"x"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d; want 404 (body %s)", path, rec.Code, rec.Body)
		}
	}
}

func TestConfigPutEmptyValue400(t *testing.T) {
	h := ConfigHandler(configTestStore(t))
	for _, body := range []string{`{"value":""}`, `{"value":"   "}`, `{}`, `{"other":1}`} {
		rec := put(t, h, "/v1/admin/config/system.listen", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d; want 400 (response %s)", body, rec.Code, rec.Body)
		}
	}
}

func TestConfigPutLongValue400(t *testing.T) {
	h := ConfigHandler(configTestStore(t))
	body := `{"value":"` + strings.Repeat("a", config.MaxValueLen+1) + `"}`
	rec := put(t, h, "/v1/admin/config/system.listen", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
}

func TestConfigPutBadJSON400(t *testing.T) {
	h := ConfigHandler(configTestStore(t))
	for _, body := range []string{"not json", `{"value":123}`} {
		rec := put(t, h, "/v1/admin/config/system.listen", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d; want 400 (response %s)", body, rec.Code, rec.Body)
		}
	}
}

func TestConfigMethodNotAllowed(t *testing.T) {
	h := ConfigHandler(configTestStore(t))
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/admin/config"},
		{http.MethodDelete, "/v1/admin/config"},
		{http.MethodPost, "/v1/admin/config/system.listen"},
		{http.MethodGet, "/v1/admin/config/system.listen"},
		{http.MethodPut, "/v1/admin/config"}, // PUT needs a key
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"value":"x"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: status = %d; want 405 (body %s)", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestConfigStoreError500(t *testing.T) {
	fake := &fakeConfigStore{listErr: errors.New("boom-list"), setErr: errors.New("boom-set")}
	h := ConfigHandler(fake)
	rec := get(t, h, "/v1/admin/config")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("list error leaks detail: %s", rec.Body)
	}
	rec = put(t, h, "/v1/admin/config/system.listen", `{"value":"x"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("set status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("set error leaks detail: %s", rec.Body)
	}
	if fake.gotKey != "system.listen" || fake.gotVal != "x" {
		t.Fatalf("fake store got key %q value %q", fake.gotKey, fake.gotVal)
	}
}

// TestConfigComposedHandler mirrors serve's wiring: admin namespace + config
// endpoints + data API on one top-level mux.
func TestConfigComposedHandler(t *testing.T) {
	top := http.NewServeMux()
	top.Handle("/v1/admin/", AdminHandler(testMeta(), &fakePinger{}))
	cfg := ConfigHandler(configTestStore(t))
	top.Handle("/v1/admin/config", cfg)
	top.Handle("/v1/admin/config/", cfg)
	top.Handle("/", Handler(&fakeStore{}))

	rec := get(t, top, "/v1/admin/config")
	if rec.Code != http.StatusOK {
		t.Fatalf("config list = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = put(t, top, "/v1/admin/config/system.listen", `{"value":"127.0.0.1:8083"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("config put = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/admin/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/bars?symbol=DEMO.US&timeframe=1d")
	if rec.Code != http.StatusOK {
		t.Fatalf("bars = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/admin/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("admin unknown = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

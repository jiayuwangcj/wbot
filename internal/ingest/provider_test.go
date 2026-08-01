package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}

func TestProviderRegisterAndNew(t *testing.T) {
	name := fmt.Sprintf("test-reg-%d", time.Now().UnixNano())
	Register(Provider{Name: name, New: func(cfg Config) (Source, error) {
		return FileSource{Path: cfg["path"]}, nil
	}})
	src, err := NewProvider(name, Config{"path": "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := src.(FileSource)
	if !ok || got.Path != "/tmp/x" {
		t.Fatalf("src = %+v; want FileSource with /tmp/x", src)
	}
}

func TestProviderRegisterDuplicate(t *testing.T) {
	mustPanic(t, func() {
		Register(Provider{Name: "mock", New: func(Config) (Source, error) { return nil, nil }})
	})
	name := fmt.Sprintf("test-dup-%d", time.Now().UnixNano())
	Register(Provider{Name: name, New: func(Config) (Source, error) { return nil, nil }})
	mustPanic(t, func() {
		Register(Provider{Name: name, New: func(Config) (Source, error) { return nil, nil }})
	})
}

func TestProviderRegisterInvalid(t *testing.T) {
	mustPanic(t, func() { Register(Provider{Name: "", New: func(Config) (Source, error) { return nil, nil }}) })
	mustPanic(t, func() { Register(Provider{Name: "empty-new"}) })
}

func TestProviderUnregisteredName(t *testing.T) {
	_, err := NewProvider("no-such-provider", nil)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("err = %v; want not registered", err)
	}
}

func TestBuiltinProvidersConstruct(t *testing.T) {
	mock, err := NewProvider("mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mock.(mockSource); !ok {
		t.Fatalf("mock provider src = %T; want mockSource", mock)
	}

	file, err := NewProvider("file", Config{"path": "/tmp/bars.json"})
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := file.(FileSource); !ok || f.Path != "/tmp/bars.json" {
		t.Fatalf("file provider src = %+v; want FileSource /tmp/bars.json", file)
	}

	url, err := NewProvider("url", Config{"url": "https://example.com/bars.json"})
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := url.(HTTPSource); !ok || h.URL != "https://example.com/bars.json" {
		t.Fatalf("url provider src = %+v; want HTTPSource", url)
	}
}

// TestBuiltinProvidersMatchDirectSources: provider-built sources must behave
// exactly like the plain mock/file/http structs (backward-compat contract).
func TestBuiltinProvidersMatchDirectSources(t *testing.T) {
	ctx := context.Background()
	sym := domain.Symbol("X.US")
	content := `[
	  {"ts":"2024-06-01T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":10},
	  {"ts":"2024-06-02T00:00:00Z","open":1.5,"high":2.5,"low":1,"close":2,"volume":11}
	]`

	dir := t.TempDir()
	path := filepath.Join(dir, "bars.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()

	cases := []struct {
		name string
		via  func() (Source, error)
		dir  Source
	}{
		{"mock", func() (Source, error) { return NewProvider("mock", nil) }, mockSource{}},
		{"file", func() (Source, error) { return NewProvider("file", Config{"path": path}) }, FileSource{Path: path}},
		{"url", func() (Source, error) { return NewProvider("url", Config{"url": srv.URL}) }, HTTPSource{URL: srv.URL}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			src, err := tt.via()
			if err != nil {
				t.Fatal(err)
			}
			via, err := src.Bars(ctx, sym, "1d", time.Time{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			direct, err := tt.dir.Bars(ctx, sym, "1d", time.Time{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(via, direct) {
				t.Fatalf("provider bars %+v; want direct %+v", via, direct)
			}
		})
	}
}

// TestBuiltinProvidersMissingConfig: absent config keys pass through as the
// empty struct, so the source's own error surfaces (same as direct usage).
func TestBuiltinProvidersMissingConfig(t *testing.T) {
	ctx := context.Background()
	sym := domain.Symbol("X.US")
	file, err := NewProvider("file", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Bars(ctx, sym, "1d", time.Time{}, time.Time{}); err == nil || !strings.Contains(err.Error(), "empty path") {
		t.Fatalf("err = %v; want empty path", err)
	}
	url, err := NewProvider("url", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := url.Bars(ctx, sym, "1d", time.Time{}, time.Time{}); err == nil || !strings.Contains(err.Error(), "empty url") {
		t.Fatalf("err = %v; want empty url", err)
	}
}

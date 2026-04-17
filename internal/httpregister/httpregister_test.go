package httpregister

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/master"
)

func TestHandlerRegisterTwice(t *testing.T) {
	m := master.NewMemory()
	srv := httptest.NewServer(Handler(m))
	defer srv.Close()

	ctx := context.Background()
	c := Client{BaseURL: srv.URL}

	new1, err := c.Register(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !new1 {
		t.Fatalf("first register: want new=true")
	}
	new2, err := c.Register(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if new2 {
		t.Fatalf("second register: want new=false")
	}
	ids := m.Agents()
	if len(ids) != 1 || ids[0] != "a1" {
		t.Fatalf("agents = %v, want [a1]", ids)
	}
}

func TestRegisterRetries503(t *testing.T) {
	m := master.NewMemory()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		Handler(m).ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	c := Client{BaseURL: srv.URL, RetryMax: 3, RetryBackoff: time.Millisecond}

	newID, err := c.Register(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !newID {
		t.Fatalf("want new=true after retries")
	}
	if n < 3 {
		t.Fatalf("server got %d requests, want >= 3", n)
	}
}

func TestRegisterNoRetryOn400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()

	ctx := context.Background()
	c := Client{BaseURL: srv.URL, RetryMax: 3, RetryBackoff: time.Millisecond}

	_, err := c.Register(ctx, "a1")
	if err == nil {
		t.Fatal("want error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %v, want HTTP 400", err)
	}
}

func TestHandlerWrongPath(t *testing.T) {
	m := master.NewMemory()
	srv := httptest.NewServer(Handler(m))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/nope", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

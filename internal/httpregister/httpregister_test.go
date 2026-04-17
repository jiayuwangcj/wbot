package httpregister

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
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

func TestRegisterHTTPS(t *testing.T) {
	m := master.NewMemory()
	srv := httptest.NewTLSServer(Handler(m))
	defer srv.Close()

	ctx := context.Background()
	c := Client{BaseURL: srv.URL, HTTP: srv.Client()}

	newID, err := c.Register(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !newID {
		t.Fatalf("want new=true")
	}
}

func TestHandlerListAgents(t *testing.T) {
	m := master.NewMemory()
	_ = m.Register("z9")
	_ = m.Register("a1")
	srv := httptest.NewServer(Handler(m))
	defer srv.Close()

	resp, err := http.Get(srv.URL + agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got AgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got.Agents)
	if len(got.Agents) != 2 || got.Agents[0] != "a1" || got.Agents[1] != "z9" {
		t.Fatalf("agents = %v, want [a1 z9]", got.Agents)
	}
}

func TestClientListAgents(t *testing.T) {
	m := master.NewMemory()
	srv := httptest.NewServer(Handler(m))
	defer srv.Close()
	ctx := context.Background()
	c := Client{BaseURL: srv.URL}
	if _, err := c.Register(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	ids, err := c.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("ListAgents = %v, want [c1]", ids)
	}
}

func TestRemoteFacadeAgents(t *testing.T) {
	m := master.NewMemory()
	srv := httptest.NewServer(Handler(m))
	defer srv.Close()
	ctx := context.Background()
	c := Client{BaseURL: srv.URL}
	if _, err := c.Register(ctx, "rf1"); err != nil {
		t.Fatal(err)
	}
	rf := RemoteFacade{Client: &c, Ctx: ctx}
	got := rf.Agents()
	if len(got) != 1 || got[0] != "rf1" {
		t.Fatalf("Agents = %v, want [rf1]", got)
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

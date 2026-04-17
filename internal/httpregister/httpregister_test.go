package httpregister

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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

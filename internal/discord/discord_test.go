package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testBotToken = "bottest-token"

// fakeDCServer records create-message payloads and asserts Bot authorization.
type fakeDCServer struct {
	mu      sync.Mutex
	sends   []map[string]any
	authErr string
	status  int
}

func (f *fakeDCServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != "Bot "+testBotToken {
		f.authErr = got
		http.Error(w, "bad auth", http.StatusUnauthorized)
		return
	}
	var p map[string]any
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.sends = append(f.sends, p)
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	w.Write([]byte(`{"id":"m1"}`))
}

func (f *fakeDCServer) lastSend(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		t.Fatal("no create message received")
	}
	return f.sends[len(f.sends)-1]
}

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(testBotToken, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateMessagePostsEmbedAndButtons(t *testing.T) {
	fake := &fakeDCServer{}
	c := newTestClient(t, fake)

	msg := Message{
		Embeds: []Embed{{Title: "信号 #7", Description: "**test**", Color: ColorApprove}},
		Components: [][]Button{{
			{Type: 2, Style: 3, Label: "✅ 下单", CustomID: "wheel:7:yes"},
			{Type: 2, Style: 4, Label: "❌ 拒绝", CustomID: "wheel:7:no"},
		}},
	}
	if err := c.CreateMessage(context.Background(), "chan-1", msg); err != nil {
		t.Fatal(err)
	}
	payload := fake.lastSend(t)
	embeds, _ := payload["embeds"].([]any)
	embed, _ := embeds[0].(map[string]any)
	if embed["color"] != float64(ColorApprove) || embed["title"] != "信号 #7" {
		t.Fatalf("embed = %#v", embed)
	}
	rows, _ := payload["components"].([]any)
	buttons, _ := rows[0].([]any)
	first, _ := buttons[0].(map[string]any)
	if first["custom_id"] != "wheel:7:yes" || first["style"] != float64(3) {
		t.Fatalf("button = %#v", first)
	}
	if fake.authErr != "" {
		t.Fatalf("authorization = %q; want Bot token", fake.authErr)
	}
}

func TestCreateMessageHTTPError(t *testing.T) {
	fake := &fakeDCServer{status: http.StatusForbidden}
	c := newTestClient(t, fake)
	err := c.CreateMessage(context.Background(), "chan-1", Message{Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v; want HTTP 403", err)
	}
}

func TestNewRejectsBadTokenAndBaseURL(t *testing.T) {
	if _, err := New("", "", nil); err == nil {
		t.Fatal("empty token accepted")
	}
	if _, err := New("tok", "ftp://x", nil); err == nil {
		t.Fatal("non-http base URL accepted")
	}
}

package discord

import (
	"context"
	"encoding/json"
	"io"
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
		Embeds: []Embed{{
			Author: &EmbedAuthor{Name: "Wheel Bot"}, Title: "信号 #7", Description: "**test**", Color: ColorApprove,
			Footer: &EmbedFooter{Text: "配置 v1"},
		}},
		Nonce: "report-nonce", EnforceNonce: true,
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
	author, _ := embed["author"].(map[string]any)
	footer, _ := embed["footer"].(map[string]any)
	if author["name"] != "Wheel Bot" || footer["text"] != "配置 v1" {
		t.Fatalf("embed author/footer = %#v / %#v", author, footer)
	}
	rows, _ := payload["components"].([]any)
	row, _ := rows[0].(map[string]any)
	if row["type"] != float64(1) {
		t.Fatalf("action row = %#v; want type 1", row)
	}
	buttons, _ := row["components"].([]any)
	first, _ := buttons[0].(map[string]any)
	if first["type"] != float64(2) || first["custom_id"] != "wheel:7:yes" || first["style"] != float64(3) {
		t.Fatalf("button = %#v", first)
	}
	if fake.authErr != "" {
		t.Fatalf("authorization = %q; want Bot token", fake.authErr)
	}
	if payload["nonce"] != "report-nonce" || payload["enforce_nonce"] != true {
		t.Fatalf("nonce fields = %#v / %#v", payload["nonce"], payload["enforce_nonce"])
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

func TestRegisterGlobalCommandsIsRepeatable(t *testing.T) {
	var mu sync.Mutex
	var payloads [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/applications/app-1/commands" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bot "+testBotToken {
			t.Errorf("authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		payloads = append(payloads, body)
		mu.Unlock()
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	c, err := New(testBotToken, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	commands := []ApplicationCommand{{Name: "ask", Description: "向智能助手提问", Options: []ApplicationCommandOption{{Type: 3, Name: "question", Description: "要问的问题", Required: true}}}}
	for i := 0; i < 2; i++ {
		if err := c.RegisterGlobalCommands(context.Background(), "app-1", commands); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 2 || string(payloads[0]) != string(payloads[1]) {
		t.Fatalf("registration payloads = %q", payloads)
	}
}

func TestEditInteractionReply(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/webhooks/app-1/tok/messages/@original" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(`{"id":"m1"}`))
	}))
	defer server.Close()
	c, err := New(testBotToken, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.EditInteractionReply(context.Background(), "app-1", "tok", "回答"); err != nil {
		t.Fatal(err)
	}
	if got["content"] != "回答" {
		t.Fatalf("payload = %#v", got)
	}
}

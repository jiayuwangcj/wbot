package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramSend(t *testing.T) {
	var got struct {
		ChatID                string `json:"chat_id"`
		Text                  string `json:"text"`
		DisableWebPagePreview bool   `json:"disable_web_page_preview"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/bottest-token/sendMessage" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender, err := NewTelegram("test-token", "chat-42", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), "数据缺失"); err != nil {
		t.Fatal(err)
	}
	if got.ChatID != "chat-42" || got.Text != "数据缺失" || !got.DisableWebPagePreview {
		t.Fatalf("payload = %+v", got)
	}
}

func TestDiscordSend(t *testing.T) {
	var got struct {
		Content string `json:"content"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender, err := NewDiscord(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), "repair failed"); err != nil {
		t.Fatal(err)
	}
	if got.Content != "repair failed" {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestSenderErrorsNeverExposeCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	telegram, err := NewTelegram("super-secret-token", "private-chat", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	discord, err := NewDiscord(server.URL+"/super-secret-webhook", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, sender := range []Sender{telegram, discord} {
		err := sender.Send(context.Background(), "message")
		if err == nil {
			t.Fatal("Send succeeded; want closed-server error")
		}
		for _, secret := range []string{"super-secret-token", "private-chat", "super-secret-webhook", server.URL} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error %q exposes %q", err, secret)
			}
		}
	}
}

func TestMultiSenderContinuesAfterFailure(t *testing.T) {
	var calls []string
	senders := MultiSender{
		senderFunc(func(context.Context, string) error {
			calls = append(calls, "first")
			return errors.New("first failed")
		}),
		senderFunc(func(context.Context, string) error {
			calls = append(calls, "second")
			return nil
		}),
	}
	if err := senders.Send(context.Background(), "message"); err == nil || err.Error() != "first failed" {
		t.Fatalf("error = %v", err)
	}
	if strings.Join(calls, ",") != "first,second" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestConstructorsRejectIncompleteConfig(t *testing.T) {
	if _, err := NewTelegram("", "chat", "", nil); err == nil {
		t.Fatal("NewTelegram accepted missing token")
	}
	if _, err := NewDiscord("not-a-url", nil); err == nil {
		t.Fatal("NewDiscord accepted invalid URL")
	}
}

type senderFunc func(context.Context, string) error

func (f senderFunc) Send(ctx context.Context, message string) error { return f(ctx, message) }

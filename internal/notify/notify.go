// Package notify sends small operational messages to optional external sinks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	telegramAPI = "https://api.telegram.org"
)

// Sender delivers a plain-text operational message.
type Sender interface {
	Send(context.Context, string) error
}

// MultiSender delivers to every configured sink. One failure never prevents
// the remaining sinks from being attempted.
type MultiSender []Sender

func (m MultiSender) Send(ctx context.Context, message string) error {
	var errs []error
	for _, sender := range m {
		if sender == nil {
			continue
		}
		if err := sender.Send(ctx, message); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Telegram sends messages through Telegram's sendMessage bot endpoint.
type Telegram struct {
	token   string
	chatID  string
	baseURL string
	client  *http.Client
}

// NewTelegram validates Telegram configuration. baseURL is optional and exists
// for offline tests; production callers should pass an empty string.
func NewTelegram(token, chatID, baseURL string, client *http.Client) (*Telegram, error) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(chatID) == "" {
		return nil, errors.New("telegram: bot token and chat id are required")
	}
	if baseURL == "" {
		baseURL = telegramAPI
	}
	if _, err := validHTTPURL(baseURL); err != nil {
		return nil, errors.New("telegram: invalid API base URL")
	}
	return &Telegram{token: token, chatID: chatID, baseURL: strings.TrimRight(baseURL, "/"), client: httpClient(client)}, nil
}

func (t *Telegram) Send(ctx context.Context, message string) error {
	payload := struct {
		ChatID                string `json:"chat_id"`
		Text                  string `json:"text"`
		DisableWebPagePreview bool   `json:"disable_web_page_preview"`
	}{ChatID: t.chatID, Text: message, DisableWebPagePreview: true}
	endpoint := t.baseURL + "/bot" + url.PathEscape(t.token) + "/sendMessage"
	return postJSON(ctx, t.client, endpoint, payload, "telegram")
}

// Discord sends messages through an incoming webhook.
type Discord struct {
	webhookURL string
	client     *http.Client
}

// NewDiscord validates an incoming webhook URL without exposing it in errors.
func NewDiscord(webhookURL string, client *http.Client) (*Discord, error) {
	parsed, err := validHTTPURL(webhookURL)
	if err != nil {
		return nil, errors.New("discord: invalid webhook URL")
	}
	localTest := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost"
	if !localTest && !strings.Contains(parsed.Path, "/api/webhooks/") {
		return nil, errors.New("discord: invalid webhook URL")
	}
	return &Discord{webhookURL: webhookURL, client: httpClient(client)}, nil
}

func (d *Discord) Send(ctx context.Context, message string) error {
	payload := struct {
		Content string `json:"content"`
	}{Content: message}
	return postJSON(ctx, d.client, d.webhookURL, payload, "discord")
}

func validHTTPURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("invalid HTTP URL")
	}
	return parsed, nil
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any, provider string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%s: encode request", provider)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: create request", provider)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// net/http errors may contain the URL, which contains the Telegram token
		// or full Discord webhook. Never wrap that error into a user-visible one.
		return fmt.Errorf("%s: request failed", provider)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP status %d", provider, resp.StatusCode)
	}
	return nil
}

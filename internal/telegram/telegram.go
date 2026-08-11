// Package telegram is a minimal Telegram Bot API client for the wheel
// confirm loop: sendMessage with inline keyboards, getUpdates long polling
// with offset advancement, and answerCallbackQuery. The Bot API base URL is
// overridable for offline tests; the token is never included in errors.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.telegram.org"

// Button is one inline keyboard button; Data is the app-routed callback string.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

// Client talks to one bot token over the Bot API.
type Client struct {
	token      string
	baseURL    string
	client     *http.Client
	retryDelay time.Duration // getUpdates error backoff (tests shorten it)
}

// New validates the token and optional base URL (empty = public API).
func New(token, baseURL string, client *http.Client) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("telegram: bot token is required")
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("telegram: invalid API base URL")
	}
	return &Client{token: strings.TrimSpace(token), baseURL: strings.TrimRight(baseURL, "/"), client: httpClient(client), retryDelay: 5 * time.Second}, nil
}

// SendMessage posts text with an optional inline keyboard to chatID.
func (c *Client) SendMessage(ctx context.Context, chatID, text string, buttons []Button) error {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	if len(buttons) > 0 {
		payload["reply_markup"] = map[string]any{"inline_keyboard": [][]Button{buttons}}
	}
	return c.post(ctx, "/sendMessage", payload, "sendMessage")
}

// Update is one getUpdates result (only callback_query is routed).
type Update struct {
	UpdateID      int64          `json:"update_id"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// CallbackQuery is an inline-button press.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

// User is the Telegram account that pressed the button.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Message is the message the keyboard was attached to (reply routing).
type Message struct {
	Chat Chat `json:"chat"`
}

// Chat is the conversation the button press happened in.
type Chat struct {
	ID int64 `json:"id"`
}

// Poll runs getUpdates long polling until ctx is cancelled. The offset
// advances across the loop; each update goes to handler in order (handler
// errors go to onError and never stop the loop); getUpdates errors sleep 5s
// and retry. Callers run it in one goroutine and never block serve.
func (c *Client) Poll(ctx context.Context, handler func(context.Context, Update) error, onError func(error)) error {
	var offset int64
	for {
		updates, err := c.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if onError != nil {
				onError(err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryDelay):
			}
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if handler != nil {
				if err := handler(ctx, u); err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}
}

// getUpdates fetches new updates with the long-poll timeout.
func (c *Client) getUpdates(ctx context.Context, offset int64) ([]Update, error) {
	endpoint := c.baseURL + "/bot" + c.token + "/getUpdates?timeout=50"
	if offset > 0 {
		endpoint += fmt.Sprintf("&offset=%d", offset)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("telegram: getUpdates: create request")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: getUpdates: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram: getUpdates: HTTP status %d", resp.StatusCode)
	}
	var out struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("telegram: getUpdates: decode response")
	}
	if !out.OK {
		return nil, errors.New("telegram: getUpdates: ok=false")
	}
	return out.Result, nil
}

// AnswerCallbackQuery acknowledges a button press, optionally showing a toast.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	payload := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		payload["text"] = text
	}
	return c.post(ctx, "/answerCallbackQuery", payload, "answerCallbackQuery")
}

// ParseChatIDs splits a comma-separated chat id whitelist into a set
// (empty entries are skipped; a malformed id is an error so misconfiguration
// is loud instead of silently locking everyone out).
func ParseChatIDs(raw string) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("telegram: invalid chat id %q", p)
		}
		out[id] = true
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, op string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: %s: encode request", op)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: %s: create request", op)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		// net/http errors may contain the URL, which contains the bot token;
		// never surface it (same rule as internal/notify).
		return fmt.Errorf("telegram: %s: request failed", op)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram: %s: HTTP status %d", op, resp.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return fmt.Errorf("telegram: %s: decode response", op)
	}
	if !out.OK {
		return fmt.Errorf("telegram: %s: ok=false", op)
	}
	return nil
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	// The default client timeout must exceed the getUpdates long-poll timeout.
	return &http.Client{Timeout: 65 * time.Second}
}

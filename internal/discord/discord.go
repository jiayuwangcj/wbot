// Package discord is a minimal Discord Bot API client for the wheel confirm
// loop: channel messages with embeds and button components, plus Ed25519
// verification of interactions (POST /v1/discord/interactions). The API base
// URL is overridable for offline tests; the bot token never appears in errors.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://discord.com/api/v10"

// Button is one message component button; CustomID is the app-routed callback
// string (the same wheel:<id>:<action> convention as Telegram).
type Button struct {
	Type     int    `json:"type"`  // 2 = button
	Style    int    `json:"style"` // 1 primary, 2 secondary, 3 success, 4 danger
	Label    string `json:"label"`
	CustomID string `json:"custom_id,omitempty"`
}

// EmbedField is one embed row (Discord requires both name and value).
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Embed is a rich embed; Color is a decimal RGB int (0xRRGGBB).
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// Message is one channel message (create-message payload).
type Message struct {
	Content    string     `json:"content,omitempty"`
	Embeds     []Embed    `json:"embeds,omitempty"`
	Components [][]Button `json:"components,omitempty"`
}

// Wheel embed state colors (状态色:ALERT 红 / APPROVE 绿 / 拒绝灰).
const (
	ColorAlert    = 0xE74C3C
	ColorApprove  = 0x2ECC71
	ColorRejected = 0x95A5A6
)

// Client talks to one bot token over the Discord API.
type Client struct {
	token   string
	baseURL string
	client  *http.Client
}

// New validates the bot token and optional API base URL (empty = public API).
func New(token, baseURL string, client *http.Client) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("discord: bot token is required")
	}
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("discord: invalid API base URL")
	}
	return &Client{token: strings.TrimSpace(token), baseURL: strings.TrimRight(baseURL, "/"), client: httpClient(client)}, nil
}

// CreateMessage posts msg to a channel with Bot authorization.
func (c *Client) CreateMessage(ctx context.Context, channelID string, msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("discord: create message: encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/channels/"+channelID+"/messages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: create message: create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		// net/http errors may contain the API URL; never surface it (same rule
		// as internal/telegram and internal/notify).
		return fmt.Errorf("discord: create message: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: create message: HTTP status %d", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

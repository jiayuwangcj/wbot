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

// EmbedAuthor is the compact author line rendered above an embed title.
type EmbedAuthor struct {
	Name string `json:"name"`
}

// EmbedFooter is the compact footer line rendered below an embed body.
type EmbedFooter struct {
	Text string `json:"text"`
}

// Embed is a rich embed; Color is a decimal RGB int (0xRRGGBB).
type Embed struct {
	Author      *EmbedAuthor `json:"author,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// Message is one channel message (create-message payload).
type Message struct {
	Content      string     `json:"content,omitempty"`
	Embeds       []Embed    `json:"embeds,omitempty"`
	Components   [][]Button `json:"components,omitempty"`
	Nonce        string     `json:"nonce,omitempty"`
	EnforceNonce bool       `json:"enforce_nonce,omitempty"`
}

// ApplicationCommand describes a global CHAT_INPUT command.
type ApplicationCommand struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Options     []ApplicationCommandOption `json:"options,omitempty"`
}

// ApplicationCommandOption describes one slash-command argument.
type ApplicationCommandOption struct {
	Type        int    `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// actionRow is Discord's required top-level wrapper for message components.
// Message retains [][]Button as its public construction shape so existing
// callers do not need to change when the wire representation is corrected.
type actionRow struct {
	Type       int      `json:"type"`
	Components []Button `json:"components"`
}

// MarshalJSON wraps each button row in a Discord Action Row. Buttons created
// without an explicit component type default to type 2 (button).
func (m Message) MarshalJSON() ([]byte, error) {
	type messageJSON struct {
		Content      string      `json:"content,omitempty"`
		Embeds       []Embed     `json:"embeds,omitempty"`
		Components   []actionRow `json:"components,omitempty"`
		Nonce        string      `json:"nonce,omitempty"`
		EnforceNonce bool        `json:"enforce_nonce,omitempty"`
	}
	wire := messageJSON{Content: m.Content, Embeds: m.Embeds, Nonce: m.Nonce, EnforceNonce: m.EnforceNonce}
	for _, row := range m.Components {
		buttons := append([]Button(nil), row...)
		for i := range buttons {
			if buttons[i].Type == 0 {
				buttons[i].Type = 2
			}
		}
		wire.Components = append(wire.Components, actionRow{Type: 1, Components: buttons})
	}
	return json.Marshal(wire)
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

// ClearMessageComponents strips all component rows (buttons) from an existing
// message — used after any confirm button is pressed so the decision is
// visually consumed. The PATCH body is a raw literal because Message's
// omitempty would drop an empty components slice, which is exactly the signal
// Discord needs to remove buttons.
func (c *Client) ClearMessageComponents(ctx context.Context, channelID, messageID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.baseURL+"/channels/"+channelID+"/messages/"+messageID,
		strings.NewReader(`{"components":[]}`))
	if err != nil {
		return fmt.Errorf("discord: clear components: create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: clear components: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: clear components: HTTP status %d", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// DeleteInteractionReply removes the interaction's original reply — the
// ephemeral "已记录,正在下单" in-progress message is deleted once the async
// order outcome lands (老板指令 2026-08-13: 处理中消息在有异步结果后一律
// 删除,避免污染聊天记录; app_id/token 是 Discord 的 webhook 寻址约定)。
func (c *Client) DeleteInteractionReply(ctx context.Context, appID, interactionToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/webhooks/"+appID+"/"+interactionToken+"/messages/@original", nil)
	if err != nil {
		return fmt.Errorf("discord: delete interaction reply: create request")
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: delete interaction reply: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: delete interaction reply: HTTP status %d", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// EditInteractionReply replaces the original deferred interaction response.
func (c *Client) EditInteractionReply(ctx context.Context, appID, interactionToken, content string) error {
	body, err := json.Marshal(Message{Content: content})
	if err != nil {
		return fmt.Errorf("discord: edit interaction reply: encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.baseURL+"/webhooks/"+appID+"/"+interactionToken+"/messages/@original", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: edit interaction reply: create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: edit interaction reply: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: edit interaction reply: HTTP status %d", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// RegisterGlobalCommands bulk-overwrites an application's global commands.
// Repeating the same PUT is idempotent according to Discord's bulk endpoint.
func (c *Client) RegisterGlobalCommands(ctx context.Context, appID string, commands []ApplicationCommand) error {
	body, err := json.Marshal(commands)
	if err != nil {
		return fmt.Errorf("discord: register commands: encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/applications/"+appID+"/commands", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: register commands: create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: register commands: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: register commands: HTTP status %d", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
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

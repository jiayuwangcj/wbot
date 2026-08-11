// Package futu is a minimal REST client for the futu-opend-rs gateway (port 22222).
// Legacy mode needs no auth for read endpoints; see doc/FUTU.md for deployment.
package futu

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

// DefaultAddr is the loopback REST endpoint of the futu-opend-rs gateway.
const DefaultAddr = "http://127.0.0.1:22222"

// marketCode maps the symbol prefix to the Futu market enum (1=HK, 11=US, 21=SH, 22=SZ).
var marketCode = map[string]int{"HK": 1, "US": 11, "SH": 21, "SZ": 22}

// Client talks to the gateway REST API (legacy mode: read endpoints carry no auth).
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// retryBackoff is the pause before each HTTP 429 retry attempt (1s, then 2s).
var retryBackoff = []time.Duration{time.Second, 2 * time.Second}

// NewClient returns a Client for baseURL with a sane timeout.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Status is the gateway health + login state reported by `wbot futu status`.
type Status struct {
	Addr       string `json:"addr"`
	Health     string `json:"health"`
	ServerVer  int    `json:"server_ver"`
	QotLogined bool   `json:"qot_logined"`
	TrdLogined bool   `json:"trd_logined"`
	Time       int64  `json:"time"`
}

// envelope is the common REST response wrapper (ret_type 0 = success).
type envelope struct {
	RetType int    `json:"ret_type"`
	RetMsg  string `json:"ret_msg"`
	// Gateway endpoints encode err_code inconsistently as a number or string.
	ErrCode any             `json:"err_code"`
	S2C     json.RawMessage `json:"s2c"`
}

// Status checks GET /health and GET /api/global-state of the gateway.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	st.Addr = c.BaseURL
	health, err := c.get(ctx, "/health")
	if err != nil {
		return st, fmt.Errorf("health: %w", err)
	}
	st.Health = health
	body, err := c.get(ctx, "/api/global-state")
	if err != nil {
		return st, fmt.Errorf("global-state: %w", err)
	}
	var env envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return st, fmt.Errorf("global-state: bad JSON: %w", err)
	}
	if env.RetType != 0 {
		return st, fmt.Errorf("global-state: %s", env.RetMsg)
	}
	var gs struct {
		QotLogined bool  `json:"qot_logined"`
		TrdLogined bool  `json:"trd_logined"`
		ServerVer  int   `json:"server_ver"`
		Time       int64 `json:"time"`
	}
	if err := json.Unmarshal(env.S2C, &gs); err != nil {
		return st, fmt.Errorf("global-state: bad s2c: %w", err)
	}
	st.ServerVer = gs.ServerVer
	st.QotLogined = gs.QotLogined
	st.TrdLogined = gs.TrdLogined
	st.Time = gs.Time
	return st, nil
}

// Quote subscribes to symbol (SubType_Basic) and returns the /api/quote s2c body.
func (c *Client) Quote(ctx context.Context, symbol string) (json.RawMessage, error) {
	market, code, err := ParseSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if _, err := c.post(ctx, "/api/subscribe", map[string]any{
		// canonical MARKET.CODE (code-first input like "00700.HK" is rejected
		// by the gateway's string parser — 实测 2026-08-02)
		"symbols":          []string{marketPrefix(market) + code},
		"sub_types":        []int{1}, // SubType_Basic
		"is_sub_or_un_sub": true,
	}); err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", symbol, err)
	}
	if err := SnapshotLimit.Wait(ctx); err != nil {
		return nil, err
	}
	s2c, err := c.post(ctx, "/api/quote", map[string]any{
		"security_list": []map[string]any{{"market": market, "code": code}},
	})
	if err != nil {
		return nil, fmt.Errorf("quote %s: %w", symbol, err)
	}
	return s2c, nil
}

// ParseSymbol splits "HK.00700" (MARKET.CODE) or "00700.HK" (CODE.MARKET,
// the code-first form common in CN/HK tooling and used by demo data) into the
// Futu market enum and bare code. Resolution is market-first: when both sides
// could be a market (e.g. "US.US") the prefix wins.
func ParseSymbol(symbol string) (int, string, error) {
	pre, code, ok := strings.Cut(symbol, ".")
	if !ok || code == "" || strings.Contains(code, ".") {
		return 0, "", fmt.Errorf("bad symbol %q (want MARKET.CODE e.g. HK.00700)", symbol)
	}
	if market, ok := marketCode[strings.ToUpper(pre)]; ok {
		return market, code, nil
	}
	// CODE.MARKET suffix form: prefix is not a market, try the suffix.
	if market, ok := marketCode[strings.ToUpper(code)]; ok {
		return market, pre, nil
	}
	return 0, "", fmt.Errorf("unsupported market %q (want HK/US/SH/SZ)", pre)
}

// get performs a GET (rate-limited by QuoteLimit) and returns the trimmed body.
func (c *Client) get(ctx context.Context, path string) (string, error) {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", httpError(resp.StatusCode, body)
	}
	return strings.TrimSpace(string(body)), nil
}

// post sends a JSON body (rate-limited) and returns the s2c payload after
// ret_type validation; HTTP 429 (rate limited) retries with backoff, then fails.
func (c *Client) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	enc, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryBackoff[attempt-1]):
			}
		}
		if err := QuoteLimit.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(enc))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = httpError(resp.StatusCode, data)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, httpError(resp.StatusCode, data)
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("bad JSON: %w", err)
		}
		if env.RetType != 0 {
			if env.RetMsg != "" {
				return nil, errors.New(env.RetMsg)
			}
			return nil, fmt.Errorf("ret_type=%d", env.RetType)
		}
		return env.S2C, nil
	}
	return nil, fmt.Errorf("rate limited (HTTP 429): %w", lastErr)
}

// httpError renders a non-200 response into a readable error (the gateway sends {"error": ...}).
func httpError(code int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("HTTP %d", code)
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	return fmt.Errorf("HTTP %d: %s", code, msg)
}

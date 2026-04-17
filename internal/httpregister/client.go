package httpregister

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

// Client posts registration requests to a master HTTP endpoint.
type Client struct {
	// BaseURL is the scheme+host (and optional port), e.g. "http://127.0.0.1:8080".
	// Trailing slashes are trimmed.
	BaseURL string
	// HTTP is used when non-nil; otherwise http.DefaultClient.
	HTTP *http.Client
	// RetryMax is extra attempts after the first failure on transient errors (0 = none).
	RetryMax int
	// RetryBackoff is the wait before each retry; if zero, 50ms is used.
	RetryBackoff time.Duration
}

// HTTPError is a non-OK HTTP response from the register endpoint.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("httpregister: HTTP %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("httpregister: HTTP %d", e.StatusCode)
}

var errNoRetry = errors.New("httpregister: not retryable")

// Register POSTs {"id": id} to BaseURL/v1/register and returns whether the ID
// was newly recorded. Context deadlines apply to each attempt; transient
// failures are retried when RetryMax > 0.
func (c *Client) Register(ctx context.Context, id string) (bool, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		newID, err := c.registerOnce(ctx, id)
		if err == nil {
			return newID, nil
		}
		lastErr = err
		if attempt >= c.RetryMax || !retryableRegister(ctx, err) {
			break
		}
		backoff := c.RetryBackoff
		if backoff <= 0 {
			backoff = 50 * time.Millisecond
		}
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return false, ctx.Err()
		case <-t.C:
		}
	}
	return false, lastErr
}

func (c *Client) registerOnce(ctx context.Context, id string) (bool, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return false, fmt.Errorf("%w: empty BaseURL", errNoRetry)
	}
	u := base + registerPath
	body, err := json.Marshal(RegisterRequest{ID: id})
	if err != nil {
		return false, fmt.Errorf("%w: %v", errNoRetry, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("%w: %v", errNoRetry, err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	var out RegisterResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return false, fmt.Errorf("%w: %v", errNoRetry, err)
	}
	return out.New, nil
}

// ListAgents GETs /v1/agents and returns registered agent IDs (order is unspecified).
func (c *Client) ListAgents(ctx context.Context) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: empty BaseURL", errNoRetry)
	}
	u := base + agentsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNoRetry, err)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	var out AgentsResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%w: %v", errNoRetry, err)
	}
	return out.Agents, nil
}

func retryableRegister(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, errNoRetry) {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		if he.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return he.StatusCode >= 500
	}
	return true
}

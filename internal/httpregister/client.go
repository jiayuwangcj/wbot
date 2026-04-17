package httpregister

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client posts registration requests to a master HTTP endpoint.
type Client struct {
	// BaseURL is the scheme+host (and optional port), e.g. "http://127.0.0.1:8080".
	// Trailing slashes are trimmed.
	BaseURL string
	// HTTP is used when non-nil; otherwise http.DefaultClient.
	HTTP *http.Client
}

// Register POSTs {"id": id} to BaseURL/v1/register and returns whether the ID
// was newly recorded. Context deadlines apply to the HTTP round trip.
func (c *Client) Register(ctx context.Context, id string) (bool, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return false, fmt.Errorf("httpregister: empty BaseURL")
	}
	u := base + registerPath
	body, err := json.Marshal(RegisterRequest{ID: id})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return false, err
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
		return false, fmt.Errorf("httpregister: %s", strings.TrimSpace(string(b)))
	}
	var out RegisterResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return false, err
	}
	return out.New, nil
}

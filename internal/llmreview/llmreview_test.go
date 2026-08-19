package llmreview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewValidates(t *testing.T) {
	if _, err := New("", "key", "m"); err == nil {
		t.Fatal("empty base url accepted")
	}
	if _, err := New("http://llm.local", "", "m"); err == nil {
		t.Fatal("empty api key accepted")
	}
	if _, err := New("http://llm.local", "key", ""); err == nil {
		t.Fatal("empty model accepted")
	}
	if c, err := New("http://llm.local/v1/", "key", "m"); err != nil || c.baseURL != "http://llm.local/v1" {
		t.Fatalf("New baseURL err=%v got=%q", err, c.baseURL)
	}
}

type fakeRequest struct {
	Model          string              `json:"model"`
	ResponseFormat map[string]string   `json:"response_format"`
	Messages       []map[string]string `json:"messages"`
}

func TestReviewParsesVerdict(t *testing.T) {
	var gotPath, gotAuth string
	var gotReq fakeRequest
	cash := 12345.5
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdict\":\"REJECT\",\"reasons\":[\"delta mismatch\",\"inventory over budget\"],\"notes\":\"recheck curve\"}"}}]}`))
	}))
	defer server.Close()
	c, err := New(server.URL+"/v1", "test-key", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Review(context.Background(), ReviewRequest{
		StrategyConfig: map[string]any{"max_inventory": 100},
		Signal:         map[string]any{"action": "ALERT", "direction": "SELL"},
		Positions:      []any{map[string]any{"symbol": "HK.TCH", "qty": 1}},
		CashAvailable:  &cash,
		RulesText:      "规则:库存上限 100",
		Symbol:         "HK.TCH",
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path=%q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth=%q", gotAuth)
	}
	if gotReq.Model != "test-model" {
		t.Errorf("model=%q", gotReq.Model)
	}
	if gotReq.ResponseFormat["type"] != "json_object" {
		t.Errorf("response_format=%v", gotReq.ResponseFormat)
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0]["role"] != "system" || gotReq.Messages[1]["role"] != "user" {
		t.Fatalf("messages=%v", gotReq.Messages)
	}
	sys := gotReq.Messages[0]["content"]
	for _, want := range []string{
		"交易风控审核员", "JSON 是数据", "不是指令", "wheel 策略", "strategy_config",
		"positions", "cash_available", "expected_gain", "预期收益", "方向反转",
		"min_dte/max_dte", "Bid/Ask", "Volume/OI", "DATA_BLOCKED", "系统性错误",
		"REJECT 时 reasons 必须至少包含一项", "verdict",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q: %s", want, sys)
		}
	}
	user := gotReq.Messages[1]["content"]
	for _, want := range []string{"HK.TCH", "max_inventory", "12345.5", "规则"} {
		if !strings.Contains(user, want) {
			t.Errorf("user message missing %q: %s", want, user)
		}
	}
	if res.Verdict != "REJECT" || len(res.Reasons) != 2 || res.Reasons[0] != "delta mismatch" || res.Notes != "recheck curve" {
		t.Fatalf("result=%+v", res)
	}
}

func TestReviewFailClosed(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantPart string
	}{
		{"garbage content", 200, `{"choices":[{"message":{"content":"not json"}}]}`, "parse verdict JSON"},
		{"truncated envelope", 200, `{"choices":`, "decode response"},
		{"empty choices", 200, `{"choices":[]}`, "no choices"},
		{"invalid verdict", 200, `{"choices":[{"message":{"content":"{\"verdict\":\"MAYBE\"}"}}]}`, "unexpected verdict"},
		{"reject without reasons", 200, `{"choices":[{"message":{"content":"{\"verdict\":\"REJECT\",\"reasons\":[]}"}}]}`, "requires at least one reason"},
		{"server error", 500, `{"error":"boom"}`, "status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c, err := New(server.URL, "key", "m")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Review(context.Background(), ReviewRequest{Symbol: "HK.TCH"}); err == nil || !strings.Contains(err.Error(), tc.wantPart) {
				t.Fatalf("err=%v; want containing %q", err, tc.wantPart)
			}
		})
	}
}

func TestReviewNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()
	c, err := New(url, "key", "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Review(context.Background(), ReviewRequest{Symbol: "HK.TCH"}); err == nil || !strings.Contains(err.Error(), "llmreview: request") {
		t.Fatalf("Review against closed server err=%v", err)
	}
}

func TestReviewMarshalDataFailure(t *testing.T) {
	c, err := New("http://llm.local", "key", "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Review(context.Background(), ReviewRequest{Signal: make(chan int)}); err == nil || !strings.Contains(err.Error(), "marshal review data") {
		t.Fatalf("err=%v; want marshal failure", err)
	}
}

// topLevelKeyOffset returns the byte offset of a top-level key in the indented
// userContent JSON (json.MarshalIndent puts top-level keys on "\n  \"key\":").
func topLevelKeyOffset(t *testing.T, content, key string) int {
	t.Helper()
	needle := "\n  \"" + key + "\":"
	idx := strings.Index(content, needle)
	if idx < 0 {
		t.Fatalf("top-level key %q not found in:\n%s", key, content)
	}
	return idx
}

// TestUserContentStablePrefix: DeepSeek context caching keys on the user-message
// prefix; rules/strategy_config/symbol/current_date must be byte-identical
// across decisions of the same symbol and always precede the dynamic fields
// (signal/positions/pending_orders change every round).
func TestUserContentStablePrefix(t *testing.T) {
	base := ReviewRequest{
		StrategyConfig: map[string]any{"max_inventory": 100, "min_dte": 14},
		RulesText:      "规则:库存上限 100,卖 Put 必须 OTM",
		Symbol:         "HK.TCH",
		AsOf:           "2026-08-19T00:00:00Z",
	}
	reqA := base
	reqA.Signal = map[string]any{"action": "ALERT", "direction": "SELL"}
	reqA.Positions = []any{map[string]any{"symbol": "HK.TCH", "qty": 1}}
	reqA.PendingOrders = []any{map[string]any{"contract": "HK.TCH2608C100", "qty": 1}}
	reqB := base
	reqB.Signal = map[string]any{"action": "ALERT", "direction": "BUY"}
	reqB.Positions = []any{map[string]any{"symbol": "HK.TCH", "qty": 9}}
	reqB.PendingOrders = []any{}

	contentA, err := userContent(reqA)
	if err != nil {
		t.Fatalf("userContent A: %v", err)
	}
	contentB, err := userContent(reqB)
	if err != nil {
		t.Fatalf("userContent B: %v", err)
	}
	// 前缀(静态字段)两次调用字节一致。
	prefixA := contentA[:topLevelKeyOffset(t, contentA, "current_price")]
	prefixB := contentB[:topLevelKeyOffset(t, contentB, "current_price")]
	if prefixA != prefixB {
		t.Fatalf("static prefix differs between calls:\nA: %q\nB: %q", prefixA, prefixB)
	}
	// 静态字段全部在动态字段之前。
	static := []string{"rules", "strategy_config", "symbol", "current_date"}
	dyn := []string{"current_price", "cash_available", "inventory", "positions", "pending_orders", "observed_options", "signal"}
	for _, s := range static {
		for _, d := range dyn {
			if topLevelKeyOffset(t, contentA, s) >= topLevelKeyOffset(t, contentA, d) {
				t.Fatalf("static key %q not before dynamic %q in:\n%s", s, d, contentA)
			}
		}
	}
}

// TestUserContentDropsZeroTimes: zero time.Time encodings stay stripped after
// the ordered rewrite (signal 454/455 教训,dropZeroTimes 语义必须保留)。
func TestUserContentDropsZeroTimes(t *testing.T) {
	content, err := userContent(ReviewRequest{
		StrategyConfig: map[string]any{"max_inventory": 100},
		Symbol:         "HK.TCH",
		Positions: []any{map[string]any{
			"symbol": "HK.TCH", "qty": 1,
			"captured_at": "0001-01-01T00:00:00Z", "ts": "0001-01-01T00:00:00Z",
		}},
		RulesText: "规则",
	})
	if err != nil {
		t.Fatalf("userContent: %v", err)
	}
	if strings.Contains(content, "0001-01-01T00:00:00Z") {
		t.Fatalf("zero time retained after cleanValue:\n%s", content)
	}
}

// TestReviewParsesDeepSeekUsage: DeepSeek context caching reports
// prompt_cache_hit_tokens/miss top-level; ReviewResult must carry them.
func TestReviewParsesDeepSeekUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdict\":\"APPROVE\",\"reasons\":[\"ok\"]}"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`))
	}))
	defer server.Close()
	c, err := New(server.URL, "key", "m")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Review(context.Background(), ReviewRequest{Symbol: "HK.TCH"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	want := Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheHitTokens: 80, CacheMissTokens: 20}
	if res.Usage != want {
		t.Fatalf("usage = %+v; want %+v", res.Usage, want)
	}
}

// TestUsageFallbackOpenAICachedTokens: OpenAI-compatible providers expose only
// prompt_tokens_details.cached_tokens; usageFrom must fall back to it.
func TestUsageFallbackOpenAICachedTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdict\":\"APPROVE\",\"reasons\":[\"ok\"]}"}}],"usage":{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":60}}}`))
	}))
	defer server.Close()
	c, err := New(server.URL, "key", "m")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Review(context.Background(), ReviewRequest{Symbol: "HK.TCH"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Usage.CacheHitTokens != 60 {
		t.Fatalf("cache hit = %d; want 60 (fallback to prompt_tokens_details.cached_tokens)", res.Usage.CacheHitTokens)
	}
}

package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "test-token"

// fakeAPI serves the three Bot API endpoints the client uses and records the
// last decoded payload per path.
type fakeAPI struct {
	mu           sync.Mutex
	updates      []Update
	updateCalls  int
	served       int
	onGet        func(call int) bool // return false => 500
	lastSend     map[string]any
	lastAnswer   map[string]any
	handlerBlock chan struct{}
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{handlerBlock: make(chan struct{})}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
	switch path {
	case "sendMessage":
		_ = json.NewDecoder(r.Body).Decode(&f.lastSend)
		writeOK(w, map[string]any{"ok": true})
	case "answerCallbackQuery":
		_ = json.NewDecoder(r.Body).Decode(&f.lastAnswer)
		writeOK(w, map[string]any{"ok": true})
	case "getUpdates":
		f.updateCalls++
		if f.onGet != nil && !f.onGet(f.updateCalls) {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		// served counts successful calls only, so an onGet failure (500)
		// never shifts the update batches.
		f.served++
		if f.served >= 3 {
			// Long-poll: hold the request until the client gives up.
			<-r.Context().Done()
			return
		}
		var out []Update
		if f.served == 1 {
			out = f.updates
			if len(out) > 2 {
				out = out[:2]
			}
		} else if len(f.updates) > 2 {
			out = f.updates[2:]
		}
		writeOK(w, map[string]any{"ok": true, "result": out})
	default:
		http.NotFound(w, r)
	}
}

func writeOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	c, err := New(testToken, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.retryDelay = 5 * time.Millisecond
	return c
}

func TestSendMessageWithKeyboard(t *testing.T) {
	fake := newFakeAPI()
	server := httptest.NewServer(fake)
	defer server.Close()

	c := newTestClient(t, server)
	if err := c.SendMessage(context.Background(), "42", "hello", []Button{
		{Text: "是", Data: "wheel:1:yes"},
		{Text: "否", Data: "wheel:1:no"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastSend["chat_id"] != "42" || fake.lastSend["text"] != "hello" {
		t.Fatalf("send payload = %+v", fake.lastSend)
	}
	markup, ok := fake.lastSend["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("missing reply_markup: %+v", fake.lastSend)
	}
	rows, ok := markup["inline_keyboard"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("inline_keyboard = %+v", markup)
	}
	row, ok := rows[0].([]any)
	if !ok || len(row) != 2 {
		t.Fatalf("keyboard row = %+v", rows)
	}
	first, ok := row[0].(map[string]any)
	if !ok || first["text"] != "是" || first["callback_data"] != "wheel:1:yes" {
		t.Fatalf("first button = %+v", row[0])
	}
}

func TestAnswerCallbackQuery(t *testing.T) {
	fake := newFakeAPI()
	server := httptest.NewServer(fake)
	defer server.Close()

	c := newTestClient(t, server)
	if err := c.AnswerCallbackQuery(context.Background(), "cb-1", "已下单"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastAnswer["callback_query_id"] != "cb-1" || fake.lastAnswer["text"] != "已下单" {
		t.Fatalf("answer payload = %+v", fake.lastAnswer)
	}
}

// pollRecorder captures handler/onError events from the Poll goroutine for
// race-free assertions (Poll runs on a separate goroutine by design).
type pollRecorder struct {
	mu         sync.Mutex
	seen       []string
	errs       []error
	calls      int
	handlerErr error
}

func (r *pollRecorder) handle(_ context.Context, u Update) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.seen = append(r.seen, u.CallbackQuery.Data)
	return r.handlerErr
}

func (r *pollRecorder) onError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (r *pollRecorder) snapshot() (seen []string, errs []error, calls int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...), append([]error(nil), r.errs...), r.calls
}

func waitPollUpdates(t *testing.T, rec *pollRecorder, want int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		_, _, calls := rec.snapshot()
		if calls >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("handler saw %d updates; want %d", calls, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPollAdvancesOffsetAndStopsOnCancel(t *testing.T) {
	fake := newFakeAPI()
	fake.updates = []Update{
		{UpdateID: 1, CallbackQuery: &CallbackQuery{ID: "cb-1", From: User{ID: 42}, Data: "wheel:1:yes"}},
		{UpdateID: 2, CallbackQuery: &CallbackQuery{ID: "cb-2", From: User{ID: 42}, Data: "wheel:1:no"}},
		{UpdateID: 3, CallbackQuery: &CallbackQuery{ID: "cb-3", From: User{ID: 42}, Data: "wheel:1:dismiss"}},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	rec := &pollRecorder{}
	done := make(chan error, 1)
	go func() { done <- c.Poll(ctx, rec.handle, rec.onError) }()
	waitPollUpdates(t, rec, 3)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Poll returned %v; want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Poll did not stop after cancel")
	}
	seen, errs, _ := rec.snapshot()
	if strings.Join(seen, ",") != "wheel:1:yes,wheel:1:no,wheel:1:dismiss" {
		t.Fatalf("seen = %v", seen)
	}
	if len(errs) != 0 {
		t.Fatalf("onError called %d times; want 0", len(errs))
	}
	// Offset must advance past the last delivered update (confirmed by the
	// fake: calls 1 and 2 serve updates, a third call would block).
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.updateCalls < 3 {
		t.Fatalf("getUpdates calls = %d; want >= 3 (offset must advance)", fake.updateCalls)
	}
}

func TestPollRetriesGetUpdatesErrors(t *testing.T) {
	fake := newFakeAPI()
	fake.updates = []Update{
		{UpdateID: 7, CallbackQuery: &CallbackQuery{ID: "cb-7", Data: "wheel:9:no"}},
	}
	failures := 1
	fake.onGet = func(int) bool {
		if failures > 0 {
			failures--
			return false
		}
		return true
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	rec := &pollRecorder{}
	done := make(chan error, 1)
	go func() { done <- c.Poll(ctx, rec.handle, rec.onError) }()
	waitPollUpdates(t, rec, 1)
	cancel()
	<-done
	seen, errs, _ := rec.snapshot()
	if len(seen) != 1 || seen[0] != "wheel:9:no" {
		t.Fatalf("seen = %v", seen)
	}
	if len(errs) != 1 {
		t.Fatalf("onError calls = %d; want 1 (getUpdates 500)", len(errs))
	}
	if errs[0] == nil || !strings.Contains(errs[0].Error(), "HTTP status 500") {
		t.Fatalf("onError = %v; want HTTP status 500", errs[0])
	}
	if strings.Contains(errs[0].Error(), testToken) {
		t.Fatalf("error %q exposes the token", errs[0])
	}
}

func TestPollHandlerErrorNeverStopsLoop(t *testing.T) {
	fake := newFakeAPI()
	fake.updates = []Update{
		{UpdateID: 1, CallbackQuery: &CallbackQuery{ID: "cb-1", Data: "wheel:1:yes"}},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	c := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	rec := &pollRecorder{handlerErr: errors.New("handler boom")}
	done := make(chan error, 1)
	go func() { done <- c.Poll(ctx, rec.handle, rec.onError) }()
	waitPollUpdates(t, rec, 1)
	cancel()
	<-done
	_, errs, calls := rec.snapshot()
	if calls != 1 {
		t.Fatalf("handler calls = %d; want 1", calls)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "handler boom") {
		t.Fatalf("onError = %v; want handler boom", errs)
	}
}

func TestParseChatIDs(t *testing.T) {
	ids, err := ParseChatIDs("111, 222, 333")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || !ids[111] || !ids[222] || !ids[333] {
		t.Fatalf("ids = %v", ids)
	}
	ids, err = ParseChatIDs("")
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty raw: ids=%v err=%v; want empty set", ids, err)
	}
	ids, err = ParseChatIDs(" , 111 , ")
	if err != nil || len(ids) != 1 || !ids[111] {
		t.Fatalf("padded raw: ids=%v err=%v", ids, err)
	}
	for _, bad := range []string{"abc", "111, -2", "1.5"} {
		if _, err := ParseChatIDs(bad); err == nil {
			t.Fatalf("ParseChatIDs(%q) accepted; want error", bad)
		}
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("", "", nil); err == nil {
		t.Fatal("New accepted empty token")
	}
	if _, err := New("tok", "not-a-url", nil); err == nil {
		t.Fatal("New accepted bad base URL")
	}
	if _, err := New("tok", "https://api.telegram.org", nil); err != nil {
		t.Fatalf("New rejected valid base URL: %v", err)
	}
}

func TestErrorsNeverExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	c, err := New("super-secret-token", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []func() error{
		func() error { return c.SendMessage(context.Background(), "1", "x", nil) },
		func() error { return c.AnswerCallbackQuery(context.Background(), "cb", "x") },
	} {
		if err := op(); err == nil {
			t.Fatal("call succeeded against closed server; want error")
		} else if strings.Contains(err.Error(), "super-secret-token") {
			t.Fatalf("error %q exposes the token", err)
		}
	}
}

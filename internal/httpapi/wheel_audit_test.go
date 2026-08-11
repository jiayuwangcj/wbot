package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/wheelstore"
)

type fakeWheelAuditStore struct {
	configs []wheelstore.ConfigRecord
	signals []wheelstore.SignalRecord
	actions []wheelstore.ActionRecord
	err     error

	configSymbol string
	configLimit  int
	signalSymbol string
	signalAction string
	signalLimit  int
	actionID     int64
}

func (f *fakeWheelAuditStore) ListWheelConfigs(_ context.Context, symbol string, limit int) ([]wheelstore.ConfigRecord, error) {
	f.configSymbol, f.configLimit = symbol, limit
	if f.err != nil {
		return nil, f.err
	}
	return f.configs, nil
}

func (f *fakeWheelAuditStore) ListWheelSignals(_ context.Context, symbol, action string, limit int) ([]wheelstore.SignalRecord, error) {
	f.signalSymbol, f.signalAction, f.signalLimit = symbol, action, limit
	if f.err != nil {
		return nil, f.err
	}
	return f.signals, nil
}

func (f *fakeWheelAuditStore) ListWheelSignalActions(_ context.Context, signalID int64) ([]wheelstore.ActionRecord, error) {
	f.actionID = signalID
	if f.err != nil {
		return nil, f.err
	}
	return f.actions, nil
}

func wheelAuditRequest(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestWheelAuditHandlerReadRoutesAndFilters(t *testing.T) {
	created := time.Date(2026, 8, 10, 9, 30, 0, 123, time.FixedZone("CST", 8*60*60))
	price := 475.2
	fake := &fakeWheelAuditStore{
		configs: []wheelstore.ConfigRecord{{ID: 7, Symbol: "HK.00700", Version: 3, Config: map[string]any{"strategy": "wheel"}, State: map[string]any{}, CreatedAt: created}},
		signals: []wheelstore.SignalRecord{{ID: 8, Symbol: "HK.00700", Action: "ALERT", ConfigVersion: 3, CapabilityStatus: "READY", Inventory: wheelstore.InventorySnapshot{CurrentPrice: &price}, Reason: "gap", CreatedAt: created}},
		actions: []wheelstore.ActionRecord{{ID: 9, SignalID: 8, Action: "CONFIRM", Actor: "operator", Details: nil, CreatedAt: created}},
	}
	h := WheelAuditHandler(fake)

	rec := wheelAuditRequest(t, h, http.MethodGet, "/v1/wheel/configs?symbol=HK.00700&limit=7")
	if rec.Code != http.StatusOK || fake.configSymbol != "HK.00700" || fake.configLimit != 7 {
		t.Fatalf("configs status=%d filters=(%q,%d) body=%s", rec.Code, fake.configSymbol, fake.configLimit, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"created_at":"2026-08-10T01:30:00Z"`) {
		t.Fatalf("config timestamp is not normalized RFC3339 UTC: %s", rec.Body)
	}

	rec = wheelAuditRequest(t, h, http.MethodGet, "/v1/wheel/signals?symbol=HK.00700&action=alert&limit=4")
	if rec.Code != http.StatusOK || fake.signalSymbol != "HK.00700" || fake.signalAction != "ALERT" || fake.signalLimit != 4 {
		t.Fatalf("signals status=%d filters=(%q,%q,%d) body=%s", rec.Code, fake.signalSymbol, fake.signalAction, fake.signalLimit, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"blocked_by":[]`) || !strings.Contains(rec.Body.String(), `"candidates":[]`) || !strings.Contains(rec.Body.String(), `"rejection_reasons":[]`) {
		t.Fatalf("nil signal arrays must be []: %s", rec.Body)
	}

	rec = wheelAuditRequest(t, h, http.MethodGet, "/v1/wheel/signals/8/actions")
	if rec.Code != http.StatusOK || fake.actionID != 8 {
		t.Fatalf("actions status=%d id=%d body=%s", rec.Code, fake.actionID, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"details":{}`) || !strings.Contains(rec.Body.String(), `"created_at":"2026-08-10T01:30:00Z"`) {
		t.Fatalf("nil action details/timestamp not normalized: %s", rec.Body)
	}
}

func TestWheelAuditHandlerEmptyCollectionsAndValidation(t *testing.T) {
	h := WheelAuditHandler(&fakeWheelAuditStore{})
	for _, path := range []string{"/v1/wheel/configs", "/v1/wheel/signals", "/v1/wheel/signals/999/actions"} {
		rec := wheelAuditRequest(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("%s status=%d body=%q; want 200 []", path, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{
		"/v1/wheel/configs?limit=0",
		"/v1/wheel/signals?action=SELL",
		"/v1/wheel/signals/not-an-id/actions",
		"/v1/wheel/signals/0/actions",
	} {
		rec := wheelAuditRequest(t, h, http.MethodGet, path)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
			t.Fatalf("%s status=%d body=%s; want structured 400", path, rec.Code, rec.Body)
		}
	}
}

func TestWheelAuditHandlerMethodsUnknownAndStoreFailure(t *testing.T) {
	h := WheelAuditHandler(&fakeWheelAuditStore{err: errors.New("database down")})
	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, "/v1/wheel/configs", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/wheel/signals/1/actions", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/wheel/unknown", http.StatusNotFound},
		{http.MethodGet, "/v1/wheel/configs", http.StatusInternalServerError},
	} {
		rec := wheelAuditRequest(t, h, tc.method, tc.path)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":`) {
			t.Fatalf("%s %s status=%d body=%s; want %d JSON error", tc.method, tc.path, rec.Code, rec.Body, tc.status)
		}
	}
}

func TestWheelAuditHandlerNilStoreFailsClosed(t *testing.T) {
	h := WheelAuditHandler(nil)
	rec := wheelAuditRequest(t, h, http.MethodGet, "/v1/wheel/signals")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("nil store status=%d body=%s; want structured 500", rec.Code, rec.Body)
	}
	for _, path := range []string{"/v1/wheel/configs?limit=bad", "/v1/wheel/signals?action=SELL", "/v1/wheel/signals/nope/actions"} {
		rec := wheelAuditRequest(t, h, http.MethodGet, path)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
			t.Fatalf("nil store invalid request %s status=%d body=%s; want 400", path, rec.Code, rec.Body)
		}
	}
}

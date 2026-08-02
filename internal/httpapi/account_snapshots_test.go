package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// fakeSnapshotLister returns a canned series for one env; records the query.
type fakeSnapshotLister struct {
	gotEnv   string
	gotLimit int
	rows     []ingest.AccountSnapshotRow
	err      error
}

func (f *fakeSnapshotLister) AccountSnapshots(ctx context.Context, env string, limit int) ([]ingest.AccountSnapshotRow, error) {
	f.gotEnv, f.gotLimit = env, limit
	return f.rows, f.err
}

func TestAccountSnapshotsHandler(t *testing.T) {
	base := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	rows := []ingest.AccountSnapshotRow{
		{CapturedAt: base, TotalAssets: 100, Cash: 40, MarketVal: 60},
		{CapturedAt: base.Add(24 * time.Hour), TotalAssets: 110, Cash: 45, MarketVal: 65},
	}

	t.Run("ok: default env simulate, default limit, chronological points", func(t *testing.T) {
		f := &fakeSnapshotLister{rows: rows}
		r := httptest.NewRequest(http.MethodGet, "/v1/account/snapshots", nil)
		w := httptest.NewRecorder()
		AccountSnapshotsHandler(f).ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; want 200", w.Code)
		}
		if f.gotEnv != "simulate" || f.gotLimit != 120 {
			t.Fatalf("query(env=%q limit=%d); want simulate/120", f.gotEnv, f.gotLimit)
		}
		var body accountSnapshotsJSON
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Env != "simulate" || len(body.Points) != 2 {
			t.Fatalf("body env=%q points=%d; want simulate/2", body.Env, len(body.Points))
		}
		if body.Points[0].CapturedAt != base.Format("2006-01-02T15:04:05Z07:00") ||
			body.Points[0].TotalAssets != 100 {
			t.Fatalf("points[0] not chronological: %+v", body.Points[0])
		}
	})

	t.Run("env=real and limit passthrough", func(t *testing.T) {
		f := &fakeSnapshotLister{rows: rows}
		r := httptest.NewRequest(http.MethodGet, "/v1/account/snapshots?env=real&limit=7", nil)
		w := httptest.NewRecorder()
		AccountSnapshotsHandler(f).ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d; want 200", w.Code)
		}
		if f.gotEnv != "real" || f.gotLimit != 7 {
			t.Fatalf("query(env=%q limit=%d); want real/7", f.gotEnv, f.gotLimit)
		}
	})

	t.Run("bad env", func(t *testing.T) {
		f := &fakeSnapshotLister{}
		r := httptest.NewRequest(http.MethodGet, "/v1/account/snapshots?env=bogus", nil)
		w := httptest.NewRecorder()
		AccountSnapshotsHandler(f).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; want 400", w.Code)
		}
		if f.gotEnv != "" {
			t.Fatalf("store queried on bad env: %q", f.gotEnv)
		}
	})

	t.Run("bad limit", func(t *testing.T) {
		f := &fakeSnapshotLister{}
		r := httptest.NewRequest(http.MethodGet, "/v1/account/snapshots?limit=0", nil)
		w := httptest.NewRecorder()
		AccountSnapshotsHandler(f).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; want 400", w.Code)
		}
	})

	t.Run("store error → 500", func(t *testing.T) {
		f := &fakeSnapshotLister{err: errors.New("boom")}
		r := httptest.NewRequest(http.MethodGet, "/v1/account/snapshots", nil)
		w := httptest.NewRecorder()
		AccountSnapshotsHandler(f).ServeHTTP(w, r)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d; want 500", w.Code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		f := &fakeSnapshotLister{}
		r := httptest.NewRequest(http.MethodPost, "/v1/account/snapshots", nil)
		w := httptest.NewRecorder()
		AccountSnapshotsHandler(f).ServeHTTP(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d; want 405", w.Code)
		}
	})
}

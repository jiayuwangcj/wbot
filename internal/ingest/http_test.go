package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

func TestHTTPSource_Bars(t *testing.T) {
	body := `[
	  {"ts":"2024-06-01T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":10},
	  {"ts":"2024-06-02T00:00:00+02:00","open":1.5,"high":2.5,"low":1,"close":2,"volume":11}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	bars, err := (HTTPSource{URL: srv.URL}).Bars(context.Background(), domain.Symbol("X.US"), "1d")
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 {
		t.Fatalf("len %d want 2", len(bars))
	}
	if bars[0].Open != 1 || bars[0].Close != 1.5 || bars[0].Volume != 10 {
		t.Fatalf("unexpected bars[0]: %+v", bars[0])
	}
	if bars[1].Volume != 11 || bars[1].High != 2.5 {
		t.Fatalf("unexpected bars[1]: %+v", bars[1])
	}
	// Ts must be normalized to UTC (2024-06-02T00:00:00+02:00 == 22:00 UTC).
	want, err := time.Parse(time.RFC3339, "2024-06-01T22:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !bars[1].Ts.Equal(want) || bars[1].Ts.Location() != time.UTC {
		t.Fatalf("Ts = %v (%v), want %v in UTC", bars[1].Ts, bars[1].Ts.Location(), want)
	}
}

func TestHTTPSource_Bars_non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := (HTTPSource{URL: srv.URL}).Bars(context.Background(), domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %q does not mention status code", err)
	}
}

func TestHTTPSource_Bars_badJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := (HTTPSource{URL: srv.URL}).Bars(context.Background(), domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPSource_Bars_canceledCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (HTTPSource{URL: srv.URL}).Bars(ctx, domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPSource_Bars_emptyURL(t *testing.T) {
	_, err := (HTTPSource{}).Bars(context.Background(), domain.Symbol("X.US"), "1d")
	if err == nil {
		t.Fatal("expected error")
	}
}

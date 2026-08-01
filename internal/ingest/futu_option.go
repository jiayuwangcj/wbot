package ingest

// Option ingestion: expirations + chain for an underlying, then daily K-lines
// per contract into option_quotes (one transaction). Cache-first semantics
// (doc/DATA_STANDARD.md): check OptionQuotesCached/BarsCached before pulling.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
)

// OptionQuoteRow is one option_quotes row (chain metadata denormalized).
type OptionQuoteRow struct {
	Symbol     string // e.g. HK.TCH260807C335000
	Underlying string // e.g. HK.00700
	OptionType string // "call" or "put"
	Strike     float64
	Expiry     time.Time
	Ts         time.Time
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Volume     int64
	ImpliedVol *float64 // nil = unknown (gateway REST does not expose IV in v1)
}

// OptionIngestStats reports one futu-option pull (Rows = actually inserted;
// Skipped = chain contracts the gateway cannot serve, e.g. cache-warmth gaps).
type OptionIngestStats struct {
	Expiries  int
	Contracts int
	Rows      int
	Skipped   int
}

// OptionQuotesCached reports whether option_quotes already covers the
// underlying in [from, to] for adjust (true + row count = cache hit).
func OptionQuotesCached(ctx context.Context, db *sql.DB, underlying string, from, to time.Time, adjust string) (bool, int64, error) {
	if db == nil {
		return false, 0, errors.New("ingest: option cache: nil db")
	}
	if underlying == "" || adjust == "" {
		return false, 0, errors.New("ingest: option cache: empty underlying or adjust")
	}
	var n int64
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM option_quotes
WHERE underlying = $1 AND adjust = $2 AND ts >= $3 AND ts <= $4`,
		underlying, adjust, from, to).Scan(&n)
	if err != nil {
		return false, 0, fmt.Errorf("ingest: option cache: %w", err)
	}
	return n > 0, n, nil
}

// BarsCached reports whether bars already cover symbol/timeframe in [from, to]
// for adjust (true + row count = cache hit).
func BarsCached(ctx context.Context, db *sql.DB, symbol, timeframe, adjust string, from, to time.Time) (bool, int64, error) {
	if db == nil {
		return false, 0, errors.New("ingest: bars cache: nil db")
	}
	if symbol == "" || timeframe == "" || adjust == "" {
		return false, 0, errors.New("ingest: bars cache: empty symbol, timeframe or adjust")
	}
	var n int64
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM bars
WHERE symbol = $1 AND timeframe = $2 AND adjust = $3 AND ts >= $4 AND ts <= $5`,
		symbol, timeframe, adjust, from, to).Scan(&n)
	if err != nil {
		return false, 0, fmt.Errorf("ingest: bars cache: %w", err)
	}
	return n > 0, n, nil
}

// sqlExecutor is the minimal DB surface UpsertWatchlist needs (*sql.DB or *sql.Tx).
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// UpsertWatchlist registers or refreshes symbol in watchlist (strategy+params).
func UpsertWatchlist(ctx context.Context, db sqlExecutor, symbol, strategy string, params map[string]any) error {
	if db == nil {
		return errors.New("ingest: watchlist: nil db")
	}
	if symbol == "" || strategy == "" {
		return errors.New("ingest: watchlist: empty symbol or strategy")
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("ingest: watchlist: params: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO watchlist (symbol, strategy, params)
VALUES ($1, $2, $3::jsonb)
ON CONFLICT (symbol) DO UPDATE
SET strategy = EXCLUDED.strategy, params = EXCLUDED.params, updated_at = now()`,
		symbol, strategy, string(encoded))
	if err != nil {
		return fmt.Errorf("ingest: watchlist: upsert %s: %w", symbol, err)
	}
	return nil
}

// RunOptionIngestion pulls the underlying's listed future expiries and chain
// (maxExpiries <= 0 = all), then one daily K-line per contract in [from, to],
// writing option_quotes rows in one transaction. Adjust follows
// doc/DATA_STANDARD.md (futu rehab_type). details: doc/FUTU.md §10
func RunOptionIngestion(ctx context.Context, db *sql.DB, c *futu.Client, underlying, adjust string, from, to time.Time, maxExpiries int) (*OptionIngestStats, error) {
	if db == nil {
		return nil, errors.New("ingest: futu-option: nil db")
	}
	if c == nil {
		return nil, errors.New("ingest: futu-option: nil client")
	}
	rehabType, adjust, err := futu.ParseAdjust(adjust)
	if err != nil {
		return nil, fmt.Errorf("ingest: futu-option: %w", err)
	}
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.New("ingest: futu-option: need from <= to")
	}
	if _, _, err := futu.ParseSymbol(underlying); err != nil {
		return nil, fmt.Errorf("ingest: futu-option: %w", err)
	}

	expiries, err := c.OptionExpirations(ctx, underlying)
	if err != nil {
		return nil, fmt.Errorf("ingest: futu-option: %w", err)
	}
	window := expiries[:0]
	for _, e := range expiries {
		if e.DistanceDays >= 0 {
			window = append(window, e)
		}
	}
	if len(window) == 0 {
		return nil, fmt.Errorf("ingest: futu-option: %s: no future expiries listed", underlying)
	}
	if maxExpiries > 0 && len(window) > maxExpiries {
		window = window[:maxExpiries]
	}
	contracts, err := c.OptionChain(ctx, underlying, window[0].Timestamp, window[len(window)-1].Timestamp)
	if err != nil {
		return nil, fmt.Errorf("ingest: futu-option: %w", err)
	}
	if len(contracts) == 0 {
		return nil, fmt.Errorf("ingest: futu-option: %s: empty chain for %d expiries", underlying, len(window))
	}

	rows, used, skipped, err := fetchOptionRows(ctx, c, underlying, adjust, rehabType, from, to, contracts)
	if err != nil {
		return nil, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	const insertRow = `
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'futu')
ON CONFLICT (symbol, ts, adjust, source) DO NOTHING`
	inserted := 0
	for _, r := range rows {
		res, err := tx.ExecContext(ctx, insertRow,
			r.Symbol, r.Underlying, r.OptionType, r.Strike, r.Expiry, r.Ts,
			r.Open, r.High, r.Low, r.Close, r.Volume, r.ImpliedVol, adjust)
		if err != nil {
			return nil, fmt.Errorf("ingest: futu-option: insert %s: %w", r.Symbol, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			inserted += int(n)
		}
	}
	if err := UpsertWatchlist(ctx, tx, underlying, "option-watch", map[string]any{
		"expiries": len(window),
		"adjust":   adjust,
		"from":     from.UTC().Format(time.RFC3339),
		"to":       to.UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &OptionIngestStats{Expiries: len(window), Contracts: used, Rows: inserted, Skipped: skipped}, nil
}

// fetchOptionRows pulls each contract's daily K-lines (skipping blanks and
// contracts without data) and returns validated rows plus the used count.
func fetchOptionRows(ctx context.Context, c *futu.Client, underlying, adjust string, rehabType int, from, to time.Time, contracts []futu.OptionContract) (rows []OptionQuoteRow, used, skipped int, err error) {
	klType, _, err := futu.ParseTimeframe("K_DAY")
	if err != nil {
		return nil, 0, 0, err
	}
	for _, ct := range contracts {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, ctx.Err()
		}
		kbars, err := c.HistoryKline(ctx, ct.Symbol, klType, rehabType, from, to)
		if err != nil {
			// Gateway cache-warmth gap (实测 2026-08-01): chain-listed contracts
			// the gateway cannot serve yet; skip instead of failing the whole pull.
			if strings.Contains(err.Error(), "security not found in cache") {
				skipped++
				continue
			}
			return nil, 0, 0, fmt.Errorf("ingest: futu-option: kline %s: %w", ct.Symbol, err)
		}
		bars := make([]Bar, 0, len(kbars))
		for _, k := range kbars {
			if k.IsBlank {
				continue
			}
			bars = append(bars, Bar{Ts: k.Ts, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
		}
		if len(bars) == 0 {
			continue
		}
		if err := ValidateBars(bars); err != nil {
			return nil, 0, 0, fmt.Errorf("ingest: futu-option: %s: %w", ct.Symbol, err)
		}
		used++
		for _, b := range filterRange(bars, from, to) {
			rows = append(rows, OptionQuoteRow{
				Symbol: ct.Symbol, Underlying: underlying, OptionType: ct.OptionType,
				Strike: ct.Strike, Expiry: ct.Expiry,
				Ts: b.Ts, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume,
			})
		}
	}
	return rows, used, skipped, nil
}

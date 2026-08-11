package ingest

// Option ingestion: expirations + chain for an underlying, then daily K-lines
// per contract into option_quotes (one transaction). Cache-first semantics
// (doc/DATA_STANDARD.md): check OptionQuotesCached/BarsCached before pulling.

import (
	"context"
	"database/sql"
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

// QueryOptionQuotes returns an underlying's option_quotes rows in [from, to]
// (zero from/to unbounded), symbol then ts ascending; chain metadata is per row.
func QueryOptionQuotes(ctx context.Context, db *sql.DB, underlying, adjust string, from, to time.Time, limit int) ([]OptionQuoteRow, error) {
	if db == nil {
		return nil, errors.New("ingest: query option quotes: nil db")
	}
	if underlying == "" {
		return nil, errors.New("ingest: query option quotes: empty underlying")
	}
	if adjust == "" {
		return nil, errors.New("ingest: query option quotes: empty adjust")
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, errors.New("ingest: query option quotes: from after to")
	}
	if limit <= 0 {
		return nil, errors.New("ingest: query option quotes: invalid limit")
	}

	conds := []string{"underlying = $1", "adjust = $2"}
	args := []any{underlying, adjust}
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, fmt.Sprintf("ts >= $%d", len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, fmt.Sprintf("ts <= $%d", len(args)))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
SELECT symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol
FROM option_quotes WHERE %s ORDER BY symbol ASC, ts ASC LIMIT $%d`,
		strings.Join(conds, " AND "), len(args))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ingest: query option quotes: query: %w", err)
	}
	defer rows.Close()

	var out []OptionQuoteRow
	for rows.Next() {
		var r OptionQuoteRow
		if err := rows.Scan(&r.Symbol, &r.Underlying, &r.OptionType, &r.Strike, &r.Expiry, &r.Ts,
			&r.Open, &r.High, &r.Low, &r.Close, &r.Volume, &r.ImpliedVol); err != nil {
			return nil, fmt.Errorf("ingest: query option quotes: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: query option quotes: rows: %w", err)
	}
	return out, nil
}

// QueryLatestOptionQuote returns the most recent option_quotes row for one
// contract symbol (nil, nil when none). Backs the /v1/futu/options premium
// field: the daily-close premium (权利金) is the stored contract daily K close
// (doc/FUTU.md §10, P3a 2026-08-03); implied_vol stays nil (P3).
func QueryLatestOptionQuote(ctx context.Context, db *sql.DB, symbol string) (*OptionQuoteRow, error) {
	if db == nil {
		return nil, errors.New("ingest: query latest option quote: nil db")
	}
	if symbol == "" {
		return nil, errors.New("ingest: query latest option quote: empty symbol")
	}
	row := db.QueryRowContext(ctx, `
SELECT symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol
FROM option_quotes WHERE symbol = $1 ORDER BY ts DESC LIMIT 1`, symbol)
	var r OptionQuoteRow
	if err := row.Scan(&r.Symbol, &r.Underlying, &r.OptionType, &r.Strike, &r.Expiry, &r.Ts,
		&r.Open, &r.High, &r.Low, &r.Close, &r.Volume, &r.ImpliedVol); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("ingest: query latest option quote: %w", err)
	}
	return &r, nil
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

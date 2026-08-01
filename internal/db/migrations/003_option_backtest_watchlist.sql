-- Data-standard columns (doc/DATA_STANDARD.md): adjust (none/fwd/back) and
-- source (data platform: futu/...). The bars PK gains both so rehab variants
-- and multi-platform rows coexist and stay comparable.
ALTER TABLE bars
    ADD COLUMN IF NOT EXISTS adjust text NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'futu';

ALTER TABLE bars DROP CONSTRAINT IF EXISTS bars_pkey;
ALTER TABLE bars ADD PRIMARY KEY (symbol, timeframe, ts, adjust, source);

-- Option quotes: per-contract daily OHLCV (chain metadata denormalized per row).
-- implied_vol is nullable: the gateway REST exposes IV only via
-- /api/option-quote (combo, one contract per request, snapshot-rate-limited);
-- the v1 pipeline does not populate it (doc/FUTU.md §9).
CREATE TABLE IF NOT EXISTS option_quotes (
	symbol text NOT NULL,
	underlying text NOT NULL,
	option_type text NOT NULL,
	strike double precision NOT NULL,
	expiry date NOT NULL,
	ts timestamptz NOT NULL,
	open double precision NOT NULL,
	high double precision NOT NULL,
	low double precision NOT NULL,
	close double precision NOT NULL,
	volume bigint NOT NULL,
	implied_vol double precision,
	adjust text NOT NULL DEFAULT 'none',
	source text NOT NULL DEFAULT 'futu',
	PRIMARY KEY (symbol, ts, adjust, source)
);

CREATE INDEX IF NOT EXISTS idx_option_quotes_underlying_ts ON option_quotes (underlying, ts DESC);
CREATE INDEX IF NOT EXISTS idx_option_quotes_expiry ON option_quotes (expiry);

-- Backtest results: one row per run; params/metrics are free-form JSONB.
CREATE TABLE IF NOT EXISTS backtest_results (
	id bigserial PRIMARY KEY,
	strategy text NOT NULL,
	symbol text NOT NULL,
	params jsonb NOT NULL DEFAULT '{}'::jsonb,
	metrics jsonb NOT NULL,
	start_ts timestamptz NOT NULL,
	end_ts timestamptz NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backtest_results_symbol_created ON backtest_results (symbol, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_backtest_results_strategy ON backtest_results (strategy);

-- Watchlist: tracked symbols per strategy with their pull parameters.
CREATE TABLE IF NOT EXISTS watchlist (
	symbol text PRIMARY KEY,
	strategy text NOT NULL,
	params jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

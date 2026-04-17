-- OHLCV bars (historical / market data pipeline v1).
CREATE TABLE IF NOT EXISTS bars (
	symbol text NOT NULL,
	timeframe text NOT NULL,
	ts timestamptz NOT NULL,
	open double precision NOT NULL,
	high double precision NOT NULL,
	low double precision NOT NULL,
	close double precision NOT NULL,
	volume bigint NOT NULL,
	PRIMARY KEY (symbol, timeframe, ts)
);

CREATE INDEX IF NOT EXISTS idx_bars_symbol_tf_ts ON bars (symbol, timeframe, ts DESC);

-- Per-symbol daily silence for the Telegram confirm loop: a dismissed symbol
-- is skipped by the push poll for the whole dismiss_date (UTC calendar day),
-- regardless of how many ALERT signals arrive.

CREATE TABLE IF NOT EXISTS wheel_signal_dismissals (
	symbol       TEXT NOT NULL,
	dismiss_date DATE NOT NULL,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (symbol, dismiss_date)
);

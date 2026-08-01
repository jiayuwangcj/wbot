-- Backtest result detail (draft 2026-08-02 S1): per-bar equity curve and
-- trade-event ledger (fills at close, option exercise/void at expiry).
-- Nullable so pre-004 rows stay readable and metrics-only saves stay valid.
ALTER TABLE backtest_results
    ADD COLUMN IF NOT EXISTS equity_curve jsonb,
    ADD COLUMN IF NOT EXISTS trades jsonb;

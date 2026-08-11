-- Persist the deterministic Wheel decision journal with each backtest run.
-- Rows created before this migration remain readable with a NULL trace.
ALTER TABLE backtest_results
	ADD COLUMN IF NOT EXISTS signals JSONB;

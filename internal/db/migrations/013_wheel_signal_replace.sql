-- Signals may carry a 改单 (replace) intent: the chosen candidate should
-- replace a previously confirmed unfilled order (same direction, different
-- contract). Stored as JSON so review, push cards and the confirm executor
-- can read the replaced order id without string parsing.

ALTER TABLE wheel_signals
	ADD COLUMN IF NOT EXISTS replace jsonb;

-- Close-position ALERTs (profit_take_pct buy-back of a held short leg) carry
-- their own order facts: the sell candidate pipeline never applies — a close
-- is a buy of the held leg, re-selling it would be an inverse add (资金安全级,
-- 2026-08-15 评审 P1-B). close_position marks the signal, close_qty the
-- buy-back contract count and close_quote the priced leg (ask/last) that the
-- push card and the confirm executor read directly.

ALTER TABLE wheel_signals
	ADD COLUMN IF NOT EXISTS close_position boolean NOT NULL DEFAULT false,
	ADD COLUMN IF NOT EXISTS close_qty integer NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS close_quote jsonb;

-- Extend the wheel_signal_actions audit vocabulary with LLM_REVIEW so the
-- pre-order LLM gate records its verdict in the same operator audit trail.
-- Constraint was created unnamed in 005, so Postgres named it
-- wheel_signal_actions_action_check; drop and re-add with the new value.

ALTER TABLE wheel_signal_actions
	DROP CONSTRAINT IF EXISTS wheel_signal_actions_action_check;
ALTER TABLE wheel_signal_actions
	ADD CONSTRAINT wheel_signal_actions_action_check
	CHECK (action IN ('CONFIRM', 'IGNORE', 'FILL', 'NOTE', 'LLM_REVIEW'));

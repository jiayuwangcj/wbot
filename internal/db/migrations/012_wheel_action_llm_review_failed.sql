-- Extend the wheel_signal_actions audit vocabulary with LLM_REVIEW_FAILED:
-- the LLM gate records this disposition when the review request itself failed
-- (network/DNS/timeout), not as a model verdict. Introduced by #56 but the
-- constraint (from 010) predates it, so inserts were rejected with
-- "violates check constraint". Drop and re-add with the new value.

ALTER TABLE wheel_signal_actions
	DROP CONSTRAINT IF EXISTS wheel_signal_actions_action_check;
ALTER TABLE wheel_signal_actions
	ADD CONSTRAINT wheel_signal_actions_action_check
	CHECK (action IN ('CONFIRM', 'IGNORE', 'FILL', 'NOTE', 'LLM_REVIEW', 'LLM_REVIEW_FAILED', 'NO', 'REJECTED'));

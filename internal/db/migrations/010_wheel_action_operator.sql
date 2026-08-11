-- Extend the wheel_signal_actions audit vocabulary with the Telegram confirm
-- loop's operator dispositions: NO (continue waiting) and REJECTED (an order
-- was refused, reason in note). Drop and re-add the 008 constraint.

ALTER TABLE wheel_signal_actions
	DROP CONSTRAINT IF EXISTS wheel_signal_actions_action_check;
ALTER TABLE wheel_signal_actions
	ADD CONSTRAINT wheel_signal_actions_action_check
	CHECK (action IN ('CONFIRM', 'IGNORE', 'FILL', 'NOTE', 'LLM_REVIEW', 'NO', 'REJECTED'));

-- Tighten Wheel signal capability invariants for databases that already
-- applied migration 005 before the READY/DATA_BLOCKED contract was finalized.
-- Legacy contradictory rows fail closed as HOLD/DATA_BLOCKED and retain an
-- explicit migration blocker instead of being silently treated as READY.

UPDATE wheel_signals
SET action = 'HOLD',
    capability_status = 'DATA_BLOCKED',
    blocked_by = CASE
      WHEN CASE WHEN jsonb_typeof(blocked_by) = 'array' THEN jsonb_array_length(blocked_by) > 0 ELSE FALSE END
        THEN blocked_by || '["legacy_invalid_alert_capability"]'::jsonb
      ELSE '["legacy_invalid_alert_capability"]'::jsonb
    END
WHERE action = 'ALERT'
  AND capability_status IS DISTINCT FROM 'READY';

UPDATE wheel_signals
SET capability_status = 'DATA_BLOCKED',
    blocked_by = CASE
      WHEN CASE WHEN jsonb_typeof(blocked_by) = 'array' THEN jsonb_array_length(blocked_by) > 0 ELSE FALSE END
        THEN blocked_by
      ELSE '["legacy_capability_status"]'::jsonb
    END
WHERE capability_status IS NULL
   OR capability_status NOT IN ('READY', 'DATA_BLOCKED');

UPDATE wheel_signals
SET capability_status = 'DATA_BLOCKED'
WHERE capability_status = 'READY'
  AND blocked_by <> '[]'::jsonb;

UPDATE wheel_signals
SET blocked_by = '["legacy_unspecified_dependency"]'::jsonb
WHERE capability_status = 'DATA_BLOCKED'
  AND CASE WHEN jsonb_typeof(blocked_by) = 'array' THEN jsonb_array_length(blocked_by) = 0 ELSE TRUE END;

UPDATE wheel_signals
SET action = 'HOLD',
    blocked_by = blocked_by || '["legacy_invalid_alert_capability"]'::jsonb
WHERE action = 'ALERT'
  AND capability_status <> 'READY';

ALTER TABLE wheel_signals
  ALTER COLUMN capability_status DROP DEFAULT;
ALTER TABLE wheel_signals
  DROP CONSTRAINT IF EXISTS wheel_signals_capability_status_check,
  DROP CONSTRAINT IF EXISTS wheel_signals_capability_blockers_check,
  DROP CONSTRAINT IF EXISTS wheel_signals_alert_ready_check;
ALTER TABLE wheel_signals
  ADD CONSTRAINT wheel_signals_capability_status_check
    CHECK (capability_status IN ('READY', 'DATA_BLOCKED')),
  ADD CONSTRAINT wheel_signals_capability_blockers_check CHECK (
    CASE capability_status
      WHEN 'READY' THEN blocked_by = '[]'::jsonb
      WHEN 'DATA_BLOCKED' THEN
        CASE WHEN jsonb_typeof(blocked_by) = 'array'
          THEN jsonb_array_length(blocked_by) > 0
          ELSE FALSE
        END
      ELSE FALSE
    END
  ),
  ADD CONSTRAINT wheel_signals_alert_ready_check
    CHECK (action <> 'ALERT' OR capability_status = 'READY');

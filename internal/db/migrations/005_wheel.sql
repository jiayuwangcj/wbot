-- Dynamic Wheel persistence (P0-C).
-- These tables are deliberately an audit log.  There is no order id, broker
-- request, or execution path here: an operator must act on a signal manually.

CREATE TABLE IF NOT EXISTS wheel_configs (
	 id              BIGSERIAL PRIMARY KEY,
	 symbol          TEXT NOT NULL,
	 version         INTEGER NOT NULL CHECK (version > 0),
	 config          JSONB NOT NULL,
	 state           JSONB NOT NULL DEFAULT '{}'::jsonb,
	 created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	 UNIQUE (symbol, version)
);

CREATE INDEX IF NOT EXISTS wheel_configs_symbol_created_idx
	ON wheel_configs (symbol, created_at DESC);

-- A quote snapshot is immutable and every observation is retained.  Numeric
-- fields are nullable so an incomplete provider response can be retained for
-- diagnostics without ever being mistaken for an actionable quote.
CREATE TABLE IF NOT EXISTS option_quote_snapshots (
	 id             BIGSERIAL PRIMARY KEY,
	 symbol         TEXT NOT NULL,
	 underlying     TEXT NOT NULL,
	 option_type    TEXT NOT NULL CHECK (option_type IN ('PUT', 'CALL')),
	 strike         DOUBLE PRECISION NOT NULL CHECK (strike > 0),
	 expiry         DATE NOT NULL,
	 source         TEXT NOT NULL,
	 snapshot_key   TEXT NOT NULL,
	 underlying_price DOUBLE PRECISION,
	 delta          DOUBLE PRECISION,
	 bid            DOUBLE PRECISION,
	 ask            DOUBLE PRECISION,
	 iv             DOUBLE PRECISION,
	 theta          DOUBLE PRECISION,
	 volume         BIGINT,
	 open_interest  BIGINT,
	 lot_size       BIGINT,
	 observed_at    TIMESTAMPTZ NOT NULL,
	 ingested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
	CHECK (bid IS NULL OR bid > 0),
	CHECK (ask IS NULL OR ask >= 0),
	CHECK (iv IS NULL OR iv >= 0),
	CHECK (delta IS NULL OR (option_type = 'PUT' AND delta BETWEEN -1 AND 0) OR (option_type = 'CALL' AND delta BETWEEN 0 AND 1)),
	 CHECK (volume IS NULL OR volume >= 0),
	 CHECK (open_interest IS NULL OR open_interest >= 0),
	 CHECK (lot_size IS NULL OR lot_size > 0)
);

CREATE INDEX IF NOT EXISTS option_quote_snapshots_contract_time_idx
	ON option_quote_snapshots (symbol, observed_at DESC);
CREATE INDEX IF NOT EXISTS option_quote_snapshots_underlying_expiry_idx
	ON option_quote_snapshots (underlying, expiry, observed_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS option_quote_snapshots_batch_contract_idx
	ON option_quote_snapshots (underlying, observed_at, snapshot_key, symbol);

CREATE TABLE IF NOT EXISTS wheel_signals (
	 id                 BIGSERIAL PRIMARY KEY,
	 symbol             TEXT NOT NULL,
	 action             TEXT NOT NULL CHECK (action IN ('ALERT', 'HOLD')),
	 config_version     INTEGER NOT NULL CHECK (config_version > 0),
	 capability_status  TEXT NOT NULL,
	 blocked_by         JSONB NOT NULL DEFAULT '[]'::jsonb,
	 current_price      DOUBLE PRECISION,
	 actual_inventory   DOUBLE PRECISION,
	 option_delta_stock DOUBLE PRECISION,
	 effective_inventory DOUBLE PRECISION,
	 target_inventory   DOUBLE PRECISION,
	 inventory_gap      DOUBLE PRECISION,
	 inventory_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
	 candidates         JSONB NOT NULL DEFAULT '[]'::jsonb,
	 rejection_reasons  JSONB NOT NULL DEFAULT '[]'::jsonb,
	 reason             TEXT NOT NULL,
	 created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
	 FOREIGN KEY (symbol, config_version)
	   REFERENCES wheel_configs (symbol, version)
);

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

CREATE INDEX IF NOT EXISTS wheel_signals_symbol_created_idx
	ON wheel_signals (symbol, created_at DESC);
CREATE INDEX IF NOT EXISTS wheel_signals_action_created_idx
	ON wheel_signals (action, created_at DESC);

CREATE TABLE IF NOT EXISTS wheel_signal_actions (
	 id          BIGSERIAL PRIMARY KEY,
	 signal_id   BIGINT NOT NULL REFERENCES wheel_signals(id) ON DELETE RESTRICT,
	 action      TEXT NOT NULL CHECK (action IN ('CONFIRM', 'IGNORE', 'FILL', 'NOTE')),
	 actor       TEXT NOT NULL,
	 note        TEXT NOT NULL DEFAULT '',
	 details     JSONB NOT NULL DEFAULT '{}'::jsonb,
	 created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS wheel_signal_actions_signal_created_idx
	ON wheel_signal_actions (signal_id, created_at ASC);

-- Old watchlist parameters cannot be guessed into a price curve or inventory
-- limit. Preserve them for audit, but make every legacy row require explicit
-- reconfiguration before it can be used by the Wheel flow.
ALTER TABLE watchlist
	ADD COLUMN IF NOT EXISTS config_version INTEGER,
	ADD COLUMN IF NOT EXISTS execution_status TEXT,
	ADD COLUMN IF NOT EXISTS invalidation_reason TEXT;
ALTER TABLE watchlist
	ALTER COLUMN config_version DROP NOT NULL,
	ALTER COLUMN execution_status DROP NOT NULL,
	ALTER COLUMN invalidation_reason DROP NOT NULL,
	ALTER COLUMN execution_status DROP DEFAULT,
	ALTER COLUMN invalidation_reason DROP DEFAULT;
ALTER TABLE watchlist
	DROP CONSTRAINT IF EXISTS watchlist_execution_status_check;
ALTER TABLE watchlist
	ADD CONSTRAINT watchlist_execution_status_check
	CHECK (execution_status IS NULL OR execution_status IN ('DATA_BLOCKED', 'NEEDS_RECONFIGURATION', 'READY'));
ALTER TABLE watchlist
	DROP CONSTRAINT IF EXISTS watchlist_config_version_check;
ALTER TABLE watchlist
	ADD CONSTRAINT watchlist_config_version_check
	CHECK (config_version IS NULL OR config_version > 0);
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1 FROM pg_constraint WHERE conname = 'watchlist_config_version_fkey'
	) THEN
		ALTER TABLE watchlist
			ADD CONSTRAINT watchlist_config_version_fkey
			FOREIGN KEY (symbol, config_version)
			REFERENCES wheel_configs (symbol, version);
	END IF;
END $$;
UPDATE watchlist
	SET execution_status = 'NEEDS_RECONFIGURATION',
	    invalidation_reason = 'legacy strategy parameters require explicit Wheel configuration'
	WHERE config_version IS NULL;

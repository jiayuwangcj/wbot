-- Cross-channel order idempotency and LLM scheduler recovery state.
-- A claim is inserted before the broker call.  It is deliberately retained
-- even when the caller cannot append CONFIRM afterwards: retrying an
-- uncertain broker request is more dangerous than requiring manual recovery.

CREATE TABLE IF NOT EXISTS wheel_order_claims (
	 signal_id   BIGINT PRIMARY KEY REFERENCES wheel_signals(id) ON DELETE RESTRICT,
	 actor       TEXT NOT NULL,
	 order_id    BIGINT,
	 order_id_ex TEXT NOT NULL DEFAULT '',
	 details     JSONB NOT NULL DEFAULT '{}'::jsonb,
	 claimed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
	 placed_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS wheel_order_claims_placed_idx
	ON wheel_order_claims (placed_at DESC) WHERE placed_at IS NOT NULL;

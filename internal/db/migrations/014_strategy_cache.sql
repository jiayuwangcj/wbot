-- Versioned research evidence consumed by the LLM strategy prompt.  This is
-- deliberately separate from wheel_configs/watchlist: caching a report never
-- publishes a trading configuration.

CREATE TABLE IF NOT EXISTS strategy_cache (
	 symbol          TEXT PRIMARY KEY,
	 market          TEXT NOT NULL,
	 currency        TEXT NOT NULL,
	 config_version  INTEGER NOT NULL CHECK (config_version > 0),
	 payload         JSONB NOT NULL,
	 model_version   TEXT NOT NULL,
	 data_window     JSONB NOT NULL,
	 approved_state  TEXT NOT NULL CHECK (approved_state IN ('RESEARCH_CANDIDATE', 'APPROVED_CANDIDATE')),
	 created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	 updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	 CHECK (approved_state <> 'APPROVED_CANDIDATE' OR payload @> '{"approval_gates":{"data_gate_passed":true,"sample_out_passed":true,"human_approved":true}}'::jsonb)
);

CREATE INDEX IF NOT EXISTS strategy_cache_state_updated_idx
	ON strategy_cache (approved_state, updated_at DESC);

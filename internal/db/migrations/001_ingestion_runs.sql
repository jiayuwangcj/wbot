-- Ingestion job metadata (data pipeline v1 placeholder).
CREATE TABLE IF NOT EXISTS ingestion_runs (
	id bigserial PRIMARY KEY,
	source text NOT NULL,
	started_at timestamptz NOT NULL DEFAULT now(),
	finished_at timestamptz,
	status text NOT NULL DEFAULT 'running'
);

CREATE INDEX IF NOT EXISTS idx_ingestion_runs_source ON ingestion_runs (source);

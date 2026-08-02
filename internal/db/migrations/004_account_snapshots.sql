-- Account asset snapshots: periodic funds snapshots backing the equity curve
-- (资产曲线). Written by `wbot ingest account`; deliberately NOT part of
-- ingestion_runs — account data is not ingestion history, and the two never
-- mix (doc/PRIVACY.md: no credentials, no config values in the API).
CREATE TABLE IF NOT EXISTS account_snapshots (
    id            BIGSERIAL PRIMARY KEY,
    env           TEXT NOT NULL,              -- "sim" | "real" (futu.EnvName)
    acc_id        BIGINT NOT NULL,            -- gateway account id
    total_assets  DOUBLE PRECISION NOT NULL,  -- 总资产
    cash          DOUBLE PRECISION NOT NULL,  -- 现金
    market_val    DOUBLE PRECISION NOT NULL,  -- 市值
    frozen_cash   DOUBLE PRECISION NOT NULL,  -- 冻结资金
    power         DOUBLE PRECISION NOT NULL,  -- 购买力
    captured_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (env, acc_id, captured_at)
);
CREATE INDEX IF NOT EXISTS account_snapshots_env_acc_time_idx
    ON account_snapshots (env, acc_id, captured_at);

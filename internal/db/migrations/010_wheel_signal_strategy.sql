-- Signal origin: which strategy produced the signal (llm = 大模型策略,
-- wheel = 固化 wheel 规则策略). Push cards label the source so the operator
-- can tell at a glance whether an order came from the LLM strategy or the
-- fixed rules (老板指令 2026-08-13: 单子未标明是大模型策略还是固化策略生成的).

ALTER TABLE wheel_signals ADD COLUMN IF NOT EXISTS strategy TEXT NOT NULL DEFAULT 'wheel';

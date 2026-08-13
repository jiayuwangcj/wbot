export type JsonValue = null | boolean | number | string | JsonValue[] | JsonObject;

export interface JsonObject {
  [key: string]: JsonValue;
}

export type Environment = "sim" | "real";
export type WheelState = "NORMAL" | "CAUTION" | "PAUSE_BUY" | "EXIT";
export type WheelAction = "ALERT" | "HOLD";
export type CapabilityStatus = "READY" | "DATA_BLOCKED" | string;

export interface WheelCurvePoint {
  price: number;
  target_inventory: number;
}

// WheelParams 契约(2026-08-13 收敛):只有价格范围 + 最大持仓必须提供;
// 其余参数后端有默认值,可省略。lot_size 已整体去除(运行时从行情
// contract_size 实时拉取,兜底 100)。
export interface WheelParams {
  full_position_price: number;
  zero_position_price: number;
  max_inventory: number;
  move_interval_pct?: number;
  min_premium_per_share?: number;
  stock_switch_pct?: number;
  trade_gap?: number;
  min_dte?: number;
  max_dte?: number;
  min_option_quality?: number;
  max_quote_age_seconds?: number;
  strategic_state?: WheelState;
}

export interface StrategyParam {
  name: string;
  type: string;
  required?: boolean;
  default?: JsonValue;
  choices?: string[];
}

export interface StrategyDescriptor {
  name: string;
  description: string;
  params: StrategyParam[];
}

export interface Funds {
  power: number;
  total_assets: number;
  cash: number;
  market_val: number;
  available_cash: number;
}

export interface Position {
  symbol: string;
  qty: number;
  avg_cost: number;
  price: number;
  market_val: number;
  pl: number;
}

export interface FutuAccount {
  env: string;
  acc_id: number;
  funds: Funds;
  positions: Position[];
}

export interface Order {
  create_time: string;
  symbol: string;
  side: string;
  status: string;
  qty: number;
  price: number;
  fill_qty: number;
}

export interface FutuOrders {
  env: string;
  acc_id: number;
  orders: Order[];
}

export interface AccountSnapshotPoint {
  captured_at: string;
  total_assets: number;
  cash: number;
  market_val: number;
}

export interface AccountSnapshots {
  env: string;
  limit: number;
  points: AccountSnapshotPoint[];
}

export interface IngestionRun {
  id: number;
  source: string;
  status: string;
  started_at: string;
  finished_at: string | null;
}

export interface Bar {
  ts: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface BarCoverage {
  symbol: string;
  timeframe: string;
  adjust: string;
  count: number;
  min_ts: string;
  max_ts: string;
  max_ts_age_seconds: number;
  fresh: string;
}

export interface OptionFreshness {
  underlying: string;
  source: string;
  max_ts: string;
  max_ts_age_seconds: number;
  fresh: string;
}

export interface ClusterResponse {
  components: {
    process: {
      version: string;
      pid: number;
      started_at: string;
      uptime_seconds: number;
      listen_addr: string;
    };
    db: {
      ok: boolean;
      latency_ms: number;
    };
    pipeline: {
      counts: {
        running: number;
        succeeded: number;
        failed: number;
      };
      recent_runs: IngestionRun[];
    };
    data_plane: {
      bars_coverage: BarCoverage[];
      options_freshness: OptionFreshness[];
    };
  };
}

export interface DatacheckItem {
  symbol: string;
  kind: string;
  timeframe: string;
  adjust: string;
  state: string;
  max_ts?: string;
  max_expiry?: string;
  age_seconds?: number;
}

export interface DatacheckReport {
  symbols: number;
  total: number;
  complete: number;
  missing: number;
  stale: number;
  checked_at: string;
  items: DatacheckItem[];
}

export interface AdminConfigEntry {
  key: string;
  group: string;
  set: boolean;
  updated_at: string | null;
}

export interface WatchlistItem {
  symbol: string;
  strategy: "wheel" | string;
  params: WheelParams;
  config_version: number | null;
  execution_status: string;
  invalidation_reason: string;
  created_at: string;
  updated_at: string;
}

export interface WheelInventory {
  current_price?: number;
  actual_inventory?: number;
  option_delta_stock?: number;
  effective_inventory?: number;
  target_inventory?: number;
  inventory_gap?: number;
}

export interface WheelCandidate {
  direction?: string;
  quantity?: number;
  quality?: number;
  accepted?: boolean;
  quote?: Record<string, unknown>;
  reasons?: string[];
}

export interface WheelSignal {
  id: number;
  created_at: string;
  symbol: string;
  action: WheelAction;
  capability_status: CapabilityStatus;
  blocked_by: string[];
  config_version: number;
  inventory: WheelInventory;
  candidates: WheelCandidate[];
  rejection_reasons: string[];
  reason: string;
}

export interface SignalAction {
  created_at: string;
  action: string;
  actor: string;
  note: string;
}

export interface WheelConfigVersion {
  symbol: string;
  version: number;
  created_at: string;
  config: Record<string, unknown>;
  state: Record<string, unknown>;
}

export interface EquityPoint {
  ts: string;
  equity: number;
}

export interface BacktestTrade {
  ts: string;
  action: string;
  symbol: string;
  size: number;
  price: number;
  cash_after: number;
}

export interface SignalTrace {
  ts: string;
  action: string;
  capability_status: CapabilityStatus;
  blocked_by: string[];
  snapshot_key: string;
  snapshot_observed_at: string;
  direction: string;
  inventory: WheelInventory;
  candidates: WheelCandidate[];
  size: number;
  reason: string;
}

export interface BacktestSummary {
  id: number;
  strategy: string;
  symbol: string;
  params: Record<string, unknown>;
  metrics: Record<string, number | string | null>;
  start_ts: string;
  end_ts: string;
  created_at: string;
}

export interface BacktestDetail extends BacktestSummary {
  equity_curve: EquityPoint[];
  trades: BacktestTrade[];
  signals: SignalTrace[];
}

export interface BacktestQuery {
  symbol?: string;
  strategy?: string;
  q?: string;
  offset?: number;
  limit?: number;
  sort?: string;
  order?: "asc" | "desc";
}

export interface BacktestRequest {
  symbol?: string;
  strategy?: "wheel";
  params?: WheelParams;
  from_watchlist?: boolean;
}

export interface IngestRequest {
  kind: "bar" | "option";
  symbol: string;
  timeframe?: string;
  adjust?: string;
  from?: string;
  to?: string;
}

export interface ApiEndpoints {
  strategies: { response: StrategyDescriptor[] };
  futuAccount: { response: FutuAccount };
  accountSnapshots: { response: AccountSnapshots };
  futuOrders: { response: FutuOrders };
  runs: { response: IngestionRun[] };
  bars: { response: Bar[] };
  datacheck: { response: DatacheckReport };
  ingest: { response: { status: string } };
  adminConfig: { response: AdminConfigEntry[] };
  adminConfigWrite: { response: { key: string; set: boolean } };
  adminCluster: { response: ClusterResponse };
  watchlist: { response: WatchlistItem[] };
  watchlistWrite: { response: WatchlistItem };
  wheelConfigs: { response: WheelConfigVersion[] };
  wheelSignals: { response: WheelSignal[] };
  wheelSignalActions: { response: SignalAction[] };
  backtests: { response: BacktestSummary[] };
  backtestWrite: { response: BacktestDetail | { runs: BacktestDetail[] } };
  backtest: { response: BacktestDetail };
}

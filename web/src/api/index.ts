import { fetchJSON } from "./client";
import type {
  AccountSnapshots,
  AdminConfigEntry,
  ApiEndpoints,
  BacktestDetail,
  BacktestQuery,
  BacktestRequest,
  BacktestSummary,
  Bar,
  ClusterResponse,
  DatacheckReport,
  Environment,
  FutuAccount,
  FutuOrders,
  IngestRequest,
  IngestionRun,
  SignalAction,
  StrategyDescriptor,
  WatchlistItem,
  WheelConfigVersion,
  WheelSignal,
} from "./types";

const queryString = (values: Record<string, string | number | undefined>): string => {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  const query = params.toString();
  return query ? `?${query}` : "";
};

export const getStrategies = (): Promise<ApiEndpoints["strategies"]["response"]> => fetchJSON<StrategyDescriptor[]>("/v1/strategies");

export const getFutuAccount = (env: Environment): Promise<ApiEndpoints["futuAccount"]["response"]> => fetchJSON<FutuAccount>(`/v1/futu/account?env=${env}`);

export const getAccountSnapshots = (env: Environment, limit = 120): Promise<AccountSnapshots> => fetchJSON<AccountSnapshots>(`/v1/account/snapshots?env=${env}&limit=${limit}`);

export const getFutuOrders = (env: Environment): Promise<FutuOrders> => fetchJSON<FutuOrders>(`/v1/futu/orders?env=${env}`);

export const getRuns = (limit = 10): Promise<IngestionRun[]> => fetchJSON<IngestionRun[]>(`/v1/runs?limit=${limit}`);

export const getBars = (query: { symbol: string; timeframe: string; adjust: string; from?: string; to?: string; limit?: number; desc?: boolean }): Promise<Bar[]> => fetchJSON<Bar[]>(`/v1/bars${queryString({
  symbol: query.symbol,
  timeframe: query.timeframe,
  adjust: query.adjust,
  from: query.from,
  to: query.to,
  limit: query.limit,
  desc: query.desc ? 1 : undefined,
})}`);

export const getDatacheck = (): Promise<DatacheckReport> => fetchJSON<DatacheckReport>("/v1/datacheck");

export const postIngest = (request: IngestRequest): Promise<{ status: string }> => fetchJSON<{ status: string }>("/v1/ingest", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(request),
});

export const getAdminConfig = (): Promise<AdminConfigEntry[]> => fetchJSON<AdminConfigEntry[]>("/v1/admin/config");

export const putAdminConfig = (key: string, value: string): Promise<{ key: string; set: boolean }> => fetchJSON<{ key: string; set: boolean }>(`/v1/admin/config/${encodeURIComponent(key)}`, {
  method: "PUT",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ value }),
});

export const getAdminCluster = (): Promise<ClusterResponse> => fetchJSON<ClusterResponse>("/v1/admin/cluster");

export const getWatchlist = (): Promise<WatchlistItem[]> => fetchJSON<WatchlistItem[]>("/v1/watchlist");

export const saveWatchlist = (symbol: string, body: { strategy: "wheel"; params: Record<string, unknown> }): Promise<WatchlistItem> => fetchJSON<WatchlistItem>(`/v1/watchlist/${encodeURIComponent(symbol)}`, {
  method: "PUT",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
});

export const deleteWatchlist = (symbol: string): Promise<unknown> => fetchJSON<unknown>(`/v1/watchlist/${encodeURIComponent(symbol)}`, { method: "DELETE" });

export const getWheelConfigs = (symbol?: string, limit = 50): Promise<WheelConfigVersion[]> => fetchJSON<WheelConfigVersion[]>(`/v1/wheel/configs${queryString({ symbol, limit })}`);

export const getWheelSignals = (query: { symbol?: string; action?: string; capability?: string; limit?: number } = {}): Promise<WheelSignal[]> => fetchJSON<WheelSignal[]>(`/v1/wheel/signals${queryString(query)}`);

export const getWheelSignalActions = (id: number): Promise<SignalAction[]> => fetchJSON<SignalAction[]>(`/v1/wheel/signals/${id}/actions`);

export const getBacktests = (query: BacktestQuery = {}): Promise<BacktestSummary[]> => fetchJSON<BacktestSummary[]>(`/v1/backtests${queryString({ ...query, order: query.order })}`);

export const postBacktest = (request: BacktestRequest): Promise<BacktestDetail | { runs: BacktestDetail[] }> => fetchJSON<BacktestDetail | { runs: BacktestDetail[] }>("/v1/backtests", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(request),
});

export const getBacktest = (id: number): Promise<BacktestDetail> => fetchJSON<BacktestDetail>(`/v1/backtests/${id}`);

export const backtestExportURL = (id: number, format: "csv" | "json"): string => `/v1/backtests/${id}/export?format=${format}`;

import type { WheelCurvePoint, WheelParams } from "../../api/types";

// Backtest detail signal rows; the API returns flat fields (backtest.SignalTrace),
// while the shared type nests them under inventory — decode both shapes at runtime.

export interface SignalRow {
  ts: string;
  action: string;
  capability_status: string;
  blocked_by: string[];
  snapshot_key: string;
  snapshot_observed_at: string;
  direction: string;
  actual_inventory: number | null;
  effective_inventory: number | null;
  option_delta_stock: number | null;
  candidate_code: string;
  quantity: number;
  reason: string;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function asNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
}

function firstOf(values: Array<number | null>): number | null {
  for (const value of values) {
    if (value !== null) return value;
  }
  return null;
}

export function toSignalRow(signal: unknown): SignalRow {
  const raw = asRecord(signal);
  const inventory = asRecord(raw.inventory);
  const blocked = Array.isArray(raw.blocked_by) ? raw.blocked_by.filter((value): value is string => typeof value === "string") : [];
  return {
    ts: asString(raw.ts),
    action: asString(raw.action),
    capability_status: asString(raw.capability_status),
    blocked_by: blocked,
    snapshot_key: asString(raw.snapshot_key),
    snapshot_observed_at: asString(raw.snapshot_observed_at),
    direction: asString(raw.direction),
    actual_inventory: firstOf([asNumber(raw.actual_inventory), asNumber(inventory.actual_inventory)]),
    effective_inventory: firstOf([asNumber(raw.effective_inventory), asNumber(inventory.effective_inventory)]),
    option_delta_stock: firstOf([asNumber(raw.option_delta_stock), asNumber(inventory.option_delta_stock)]),
    candidate_code: asString(raw.candidate_code),
    quantity: firstOf([asNumber(raw.quantity), asNumber(raw.size)]) ?? 0,
    reason: asString(raw.reason),
  };
}

// Wheel runs saved from the watchlist wrap params under strategy_params;
// manual runs keep them flat. Invalid shapes fall back to the form defaults.

export function toRerunWheelParams(value: unknown): Partial<WheelParams> | null {
  const raw = asRecord(value);
  const nested = asRecord(raw.strategy_params);
  const source = Object.keys(nested).length > 0 ? nested : raw;
  const curveRaw = source.price_position_curve;
  if (!Array.isArray(curveRaw)) return null;
  const curve: WheelCurvePoint[] = [];
  for (const point of curveRaw) {
    const record = asRecord(point);
    const price = asNumber(record.price);
    const targetInventory = asNumber(record.target_inventory);
    if (price === null || targetInventory === null) return null;
    curve.push({ price, target_inventory: targetInventory });
  }
  const result: Partial<WheelParams> = { price_position_curve: curve };
  const maxInventory = asNumber(source.max_inventory);
  if (maxInventory !== null) result.max_inventory = maxInventory;
  // lot_size is no longer accepted (live contract_size, 2026-08-13); the
  // remaining legacy keys stay harmless for old data, formValues collapses the
  // curve to its price range endpoints.
  const minDte = asNumber(source.min_dte);
  if (minDte !== null) result.min_dte = minDte;
  const maxDte = asNumber(source.max_dte);
  if (maxDte !== null) result.max_dte = maxDte;
  const minOptionQuality = asNumber(source.min_option_quality);
  if (minOptionQuality !== null) result.min_option_quality = minOptionQuality;
  const maxDailyOrders = asNumber(source.max_daily_orders);
  if (maxDailyOrders !== null) result.max_daily_orders = maxDailyOrders;
  const extremeMaxDailyOrders = asNumber(source.extreme_max_daily_orders);
  if (extremeMaxDailyOrders !== null) result.extreme_max_daily_orders = extremeMaxDailyOrders;
  const noTradeGap = asNumber(source.no_trade_gap);
  if (noTradeGap !== null) result.no_trade_gap = noTradeGap;
  return result;
}

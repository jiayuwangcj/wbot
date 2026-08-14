import { WHEEL_STATES } from "../../components/WheelForm";
import type { WheelParams, WheelState } from "../../api/types";

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
  let fullPrice = asNumber(source.full_position_price);
  let zeroPrice = asNumber(source.zero_position_price);
  const curveRaw = source.price_position_curve;
  if ((fullPrice === null || zeroPrice === null) && Array.isArray(curveRaw) && curveRaw.length >= 2) {
	fullPrice = asNumber(asRecord(curveRaw[0]).price);
	zeroPrice = asNumber(asRecord(curveRaw[curveRaw.length - 1]).price);
  }
  if (fullPrice === null || zeroPrice === null) return null;
  const result: Partial<WheelParams> = { full_position_price: fullPrice, zero_position_price: zeroPrice };
  const maxInventory = asNumber(source.max_inventory);
  if (maxInventory !== null) result.max_inventory = maxInventory;
  const moveInterval = asNumber(source.move_interval_pct);
  if (moveInterval !== null) result.move_interval_pct = moveInterval;
  const minPremium = asNumber(source.min_premium_per_share);
  if (minPremium !== null) result.min_premium_per_share = minPremium;
  const minOptionProfit = asNumber(source.min_option_profit);
  if (minOptionProfit !== null) result.min_option_profit = minOptionProfit;
  const stockSwitch = asNumber(source.stock_switch_pct);
  if (stockSwitch !== null) result.stock_switch_pct = stockSwitch;
  const tradeGap = firstOf([asNumber(source.trade_gap), asNumber(source.no_trade_gap)]);
  if (tradeGap !== null) result.trade_gap = tradeGap;
  const minDte = asNumber(source.min_dte);
  if (minDte !== null) result.min_dte = minDte;
  const maxDte = asNumber(source.max_dte);
  if (maxDte !== null) result.max_dte = maxDte;
  const minOptionQuality = asNumber(source.min_option_quality);
  if (minOptionQuality !== null) result.min_option_quality = minOptionQuality;
  const strategicState = asString(source.strategic_state);
  if (WHEEL_STATES.includes(strategicState as WheelState)) result.strategic_state = strategicState as WheelState;
  return result;
}

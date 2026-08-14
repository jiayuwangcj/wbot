import { describe, expect, it } from "vitest";
import { toRerunWheelParams, toSignalRow } from "./trace";

describe("toSignalRow", () => {
  it("reads the flat backend fields", () => {
    const row = toSignalRow({
      ts: "2026-08-10T00:00:00Z",
      action: "HOLD",
      capability_status: "DATA_BLOCKED",
      blocked_by: ["bars", "options"],
      snapshot_key: "k1",
      snapshot_observed_at: "2026-08-10T00:00:05Z",
      direction: "sell",
      actual_inventory: 1.5,
      effective_inventory: 2,
      option_delta_stock: 0.5,
      candidate_code: "HK.00700 2500000",
      quantity: 3,
      reason: "no quote",
    });
    expect(row).toMatchObject({
      ts: "2026-08-10T00:00:00Z",
      action: "HOLD",
      capability_status: "DATA_BLOCKED",
      blocked_by: ["bars", "options"],
      snapshot_key: "k1",
      snapshot_observed_at: "2026-08-10T00:00:05Z",
      direction: "sell",
      actual_inventory: 1.5,
      effective_inventory: 2,
      option_delta_stock: 0.5,
      candidate_code: "HK.00700 2500000",
      quantity: 3,
      reason: "no quote",
    });
  });

  it("falls back to the nested inventory shape", () => {
    const row = toSignalRow({
      ts: "2026-08-10T00:00:00Z",
      inventory: { actual_inventory: 5, effective_inventory: 6, option_delta_stock: 7 },
      size: 9,
    });
    expect(row.actual_inventory).toBe(5);
    expect(row.effective_inventory).toBe(6);
    expect(row.option_delta_stock).toBe(7);
    expect(row.quantity).toBe(9);
  });

  it("defaults missing values and keeps non-string blocked_by", () => {
    const row = toSignalRow({});
    expect(row.ts).toBe("");
    expect(row.capability_status).toBe("");
    expect(row.blocked_by).toEqual([]);
    expect(row.actual_inventory).toBeNull();
    expect(row.quantity).toBe(0);
    expect(row.snapshot_key).toBe("");
  });
});

describe("toRerunWheelParams", () => {
  it("unwraps strategy_params saved from the watchlist", () => {
    const params = toRerunWheelParams({
      strategy_params: {
        full_position_price: 400,
        zero_position_price: 500,
        max_inventory: 100,
        move_interval_pct: 0.018,
        min_premium_per_share: 1.2,
        min_option_profit: 250,
        stock_switch_pct: 0.03,
        trade_gap: 50,
        min_dte: 5,
        max_dte: 10,
        min_option_quality: 0.6,
        strategic_state: "CAUTION",
      },
    });
    expect(params).not.toBeNull();
    expect(params?.full_position_price).toBe(400);
    expect(params?.zero_position_price).toBe(500);
    expect(params?.max_inventory).toBe(100);
    expect(params?.min_option_profit).toBe(250);
    expect(params?.strategic_state).toBe("CAUTION");
  });

  it("reads flat manual-run params", () => {
    const params = toRerunWheelParams({
      full_position_price: 400,
      zero_position_price: 500,
      max_inventory: 300,
      min_dte: 5,
      max_dte: 10,
      min_option_quality: 0.6,
      trade_gap: 50,
      strategic_state: "NORMAL",
    });
    expect(params?.max_inventory).toBe(300);
  });

  it("omits unknown fields and rejects non-wheel shapes", () => {
    const params = toRerunWheelParams({ full_position_price: 400, zero_position_price: 500, max_inventory: 100, strategic_state: "BOGUS" });
    expect(params?.strategic_state).toBeUndefined();
    expect(toRerunWheelParams({})).toBeNull();
    expect(toRerunWheelParams({ price_position_curve: [{ price: 400 }, { price: 500 }] })?.full_position_price).toBe(400);
    expect(toRerunWheelParams({ price_position_curve: [{ price: "x" }] })).toBeNull();
  });
});

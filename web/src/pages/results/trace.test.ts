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
  const curve = [
    { price: 400, target_inventory: 100 },
    { price: 500, target_inventory: 0 },
  ];

  it("unwraps strategy_params saved from the watchlist", () => {
    const params = toRerunWheelParams({
      strategy_params: {
        price_position_curve: curve,
        max_inventory: 100,
        lot_size: 100,
        min_dte: 5,
        max_dte: 10,
        min_option_quality: 0.6,
        max_daily_orders: 1,
        extreme_max_daily_orders: 2,
        no_trade_gap: 50,
        strategic_state: "CAUTION",
      },
    });
    expect(params).not.toBeNull();
    expect(params?.price_position_curve).toEqual(curve);
    expect(params?.max_inventory).toBe(100);
    expect(params?.strategic_state).toBe("CAUTION");
  });

  it("reads flat manual-run params", () => {
    const params = toRerunWheelParams({
      price_position_curve: curve,
      max_inventory: 300,
      lot_size: 100,
      min_dte: 5,
      max_dte: 10,
      min_option_quality: 0.6,
      max_daily_orders: 1,
      extreme_max_daily_orders: 2,
      no_trade_gap: 50,
      strategic_state: "NORMAL",
    });
    expect(params?.max_inventory).toBe(300);
  });

  it("omits unknown fields and rejects non-wheel shapes", () => {
    const params = toRerunWheelParams({ price_position_curve: curve, max_inventory: 100, strategic_state: "BOGUS" });
    expect(params?.strategic_state).toBeUndefined();
    expect(toRerunWheelParams({})).toBeNull();
    expect(toRerunWheelParams({ price_position_curve: [{ price: "x" }] })).toBeNull();
  });
});

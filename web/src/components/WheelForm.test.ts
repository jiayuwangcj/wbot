import { describe, expect, it } from "vitest";
import { DEFAULT_WHEEL_VALUES, validateWheelParams, type WheelFormValues } from "./WheelForm";

function validValues(): WheelFormValues {
  return {
    ...DEFAULT_WHEEL_VALUES,
    price_position_curve: [
      { price: 400, target_inventory: 1200 },
      { price: 550, target_inventory: 0 },
    ],
    max_inventory: 1200,
  };
}

describe("WheelForm contract", () => {
  it("returns the complete wheel request shape", () => {
    expect(validateWheelParams(validValues())).toEqual(expect.objectContaining({
      max_inventory: 1200,
      lot_size: 100,
      min_dte: 5,
      max_dte: 10,
      min_option_quality: 0.6,
      max_daily_orders: 1,
      extreme_max_daily_orders: 2,
      no_trade_gap: 50,
      strategic_state: "NORMAL",
    }));
  });

  it("preserves the fifteen source validation messages", () => {
    const cases: Array<[string, (values: WheelFormValues) => void]> = [
      ["最大库存必须大于 0", (v) => { v.max_inventory = 0; }],
      ["合约乘数必须是正整数", (v) => { v.lot_size = 1.5; }],
      ["DTE 必须是 5 到 10 之间的有效范围", (v) => { v.min_dte = 4; }],
      ["最低期权质量必须在 0 到 1 之间", (v) => { v.min_option_quality = 2; }],
      ["正常日最多张数固定为 1", (v) => { v.max_daily_orders = 2; }],
      ["极端日最多张数必须在 1 到 2 之间", (v) => { v.extreme_max_daily_orders = 3; }],
      ["不交易缺口必须不小于 0", (v) => { v.no_trade_gap = -1; }],
      ["至少需要两个价格锚点", (v) => { v.price_position_curve = [{ price: 1, target_inventory: 1 }]; }],
      ["曲线第 1 行必须填写有效数字", (v) => { v.price_position_curve[0] = { price: null, target_inventory: 1 }; }],
      ["曲线价格必须大于 0", (v) => { v.price_position_curve[0] = { price: 0, target_inventory: 1 }; }],
      ["曲线价格必须严格递增", (v) => { v.price_position_curve[1] = { price: 400, target_inventory: 0 }; }],
      ["曲线目标库存必须单调不增", (v) => { v.price_position_curve[1] = { price: 550, target_inventory: 1300 }; }],
      ["曲线目标库存必须位于 0 与最大库存之间", (v) => { v.price_position_curve[0] = { price: 400, target_inventory: -1 }; }],
      ["战略状态无效", (v) => { v.strategic_state = "UNKNOWN"; }],
    ];
    for (const [message, mutate] of cases) {
      const values = validValues();
      mutate(values);
      expect(() => validateWheelParams(values)).toThrow(message);
    }
    expect(() => validateWheelParams({ ...validValues(), max_inventory: null })).toThrow("最大库存 必须是有效数字");
  });
});

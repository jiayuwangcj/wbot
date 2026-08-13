import { describe, expect, it } from "vitest";
import { DEFAULT_WHEEL_VALUES, validateWheelParams, type WheelFormValues } from "./WheelForm";

function validValues(): WheelFormValues {
  return {
    ...DEFAULT_WHEEL_VALUES,
    full_position_price: 400,
    zero_position_price: 550,
    max_inventory: 1200,
	move_interval_pct: 1.8,
	stock_switch_pct: 2.5,
  };
}

describe("WheelForm contract", () => {
  it("returns the complete wheel request shape", () => {
    expect(validateWheelParams(validValues())).toEqual(expect.objectContaining({
      max_inventory: 1200,
      full_position_price: 400,
      zero_position_price: 550,
      move_interval_pct: 0.018,
      min_premium_per_share: 0,
      stock_switch_pct: 0.025,
      trade_gap: 50,
      min_dte: 5,
      max_dte: 10,
      min_option_quality: 0.6,
      strategic_state: "NORMAL",
    }));
  });

  it("validates the new strategic and tactical fields", () => {
    const cases: Array<[string, (values: WheelFormValues) => void]> = [
	  ["满仓价格必须大于 0", (v) => { v.full_position_price = 0; }],
	  ["清仓价格必须大于满仓价格", (v) => { v.zero_position_price = 400; }],
      ["最大库存必须是正整数", (v) => { v.max_inventory = 1.5; }],
	  ["再次出手价差必须不小于 0", (v) => { v.move_interval_pct = -1; }],
	  ["最低每股权利金必须不小于 0", (v) => { v.min_premium_per_share = -1; }],
	  ["正股切换阈值必须不小于 0", (v) => { v.stock_switch_pct = -1; }],
	  ["免交易库存差必须不小于 0", (v) => { v.trade_gap = -1; }],
      ["DTE 必须是 5 到 10 之间的有效范围", (v) => { v.min_dte = 4; }],
      ["最低期权质量必须在 0 到 1 之间", (v) => { v.min_option_quality = 2; }],
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

import { describe, expect, it } from "vitest";
import { DEFAULT_WHEEL_VALUES, validateWheelParams, type WheelFormValues } from "./WheelForm";

function validValues(): WheelFormValues {
  return { ...DEFAULT_WHEEL_VALUES, price_low: 400, price_high: 550, max_inventory: 1200 };
}

describe("WheelForm contract", () => {
  it("turns the price range into a two-point curve at max inventory", () => {
    expect(validateWheelParams(validValues())).toEqual({
      price_position_curve: [
        { price: 400, target_inventory: 1200 },
        { price: 550, target_inventory: 0 },
      ],
      max_inventory: 1200,
    });
  });

  it("validates the three user inputs", () => {
    const cases: Array<[string, (values: WheelFormValues) => void]> = [
      ["最大库存必须大于 0", (v) => { v.max_inventory = 0; }],
      ["最大库存 必须是有效数字", (v) => { v.max_inventory = null; }],
      ["价格必须大于 0", (v) => { v.price_low = 0; }],
      ["价格上限必须大于价格下限", (v) => { v.price_high = 400; }],
      ["价格下限 必须是有效数字", (v) => { v.price_low = null; }],
      ["价格上限 必须是有效数字", (v) => { v.price_high = null; }],
    ];
    for (const [message, mutate] of cases) {
      const values = validValues();
      mutate(values);
      expect(() => validateWheelParams(values)).toThrow(message);
    }
  });
});

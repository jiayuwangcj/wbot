import { describe, expect, it } from "vitest";
import { formatMoney, formatRunStatus, formatSide, formatTime, semanticClass } from "./format";

describe("format helpers", () => {
  it("formats money with two tabular decimal places", () => {
    expect(formatMoney(1234567.8)).toBe("1,234,567.80");
    expect(formatMoney(null)).toBe("—");
  });

  it("localizes stable enum values and keeps unknown values", () => {
    expect(formatSide("buy")).toBe("买入");
    expect(formatSide("sell")).toBe("卖出");
    expect(formatRunStatus("running")).toBe("运行中");
    expect(formatRunStatus("future")).toBe("future");
  });

  it("formats ISO time and semantic signs", () => {
    expect(formatTime("2026-08-11T01:02:03Z")).toMatch(/^2026-08-11 /);
    expect(formatTime("not-a-time")).toBe("not-a-time");
    expect(semanticClass(1)).toBe("num-up");
    expect(semanticClass(-1)).toBe("num-down");
    expect(semanticClass(0)).toBe("");
  });
});

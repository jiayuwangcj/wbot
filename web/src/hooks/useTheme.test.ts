import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useTheme } from "./useTheme";

describe("useTheme", () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  it("persists and applies the three theme modes", () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe("light");
    act(() => result.current.toggleTheme());
    expect(result.current.theme).toBe("dark");
    expect(window.localStorage.getItem("wbot-theme")).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    act(() => result.current.toggleTheme());
    expect(result.current.theme).toBe("dark-contrast");
    act(() => result.current.setTheme("light"));
    expect(document.documentElement.dataset.theme).toBe("light");
  });
});

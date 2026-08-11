import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAutoRefresh } from "./useAutoRefresh";

describe("useAutoRefresh", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
  });

  it("pauses while hidden and clears the timer on unmount", () => {
    const refresh = vi.fn();
    const { unmount } = renderHook(() => useAutoRefresh(refresh, 30));
    act(() => vi.advanceTimersByTime(30));
    expect(refresh).toHaveBeenCalledTimes(1);
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    act(() => vi.advanceTimersByTime(100));
    expect(refresh).toHaveBeenCalledTimes(1);
    unmount();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    act(() => vi.advanceTimersByTime(100));
    expect(refresh).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});

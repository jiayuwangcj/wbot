import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useAsyncData } from "./useAsyncData";

describe("useAsyncData", () => {
  it("exposes data and a repeatable refresh", async () => {
    const loader = vi.fn(async () => "ready");
    const { result } = renderHook(() => useAsyncData(loader));
    await waitFor(() => expect(result.current.data).toBe("ready"));
    expect(result.current.loading).toBe(false);
    await act(async () => result.current.refresh());
    expect(loader).toHaveBeenCalledTimes(2);
  });

  it("keeps the thrown error in the error slot", async () => {
    const { result } = renderHook(() => useAsyncData(async () => { throw new Error("boom"); }));
    await waitFor(() => expect(result.current.error?.message).toBe("boom"));
    expect(result.current.data).toBeNull();
  });
});

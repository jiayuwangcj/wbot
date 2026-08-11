import { describe, expect, it, vi } from "vitest";
import { ApiError, fetchJSON } from "./client";

describe("fetchJSON", () => {
  it("reads the message and action error contract", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ code: "invalid_request", message: "bad input", action: "retry later" }), { status: 400 })));
    await expect(fetchJSON("/v1/test")).rejects.toEqual(expect.objectContaining({
      name: "ApiError",
      message: "bad input · retry later",
      code: "invalid_request",
      action: "retry later",
      status: 400,
    } satisfies Partial<ApiError>));
  });

  it("normalizes network and non-json failures", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("offline"); }));
    await expect(fetchJSON("/v1/test")).rejects.toMatchObject({ message: "cannot reach the server", code: "network_error" });
    vi.stubGlobal("fetch", vi.fn(async () => new Response("not-json", { status: 200 })));
    await expect(fetchJSON("/v1/test")).rejects.toMatchObject({ message: "unexpected server response" });
  });
});

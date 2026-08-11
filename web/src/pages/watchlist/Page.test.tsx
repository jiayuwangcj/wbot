import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WatchlistItem, WheelConfigVersion, WheelSignal } from "../../api/types";
import { WatchlistPage } from "./Page";

const apiMocks = vi.hoisted(() => ({
  deleteWatchlist: vi.fn(),
  getWatchlist: vi.fn(),
  getWheelConfigs: vi.fn(),
  getWheelSignalActions: vi.fn(),
  getWheelSignals: vi.fn(),
  postBacktest: vi.fn(),
  saveWatchlist: vi.fn(),
}));

vi.mock("../../api", () => apiMocks);

const params = {
  price_position_curve: [
    { price: 100, target_inventory: 100 },
    { price: 120, target_inventory: 0 },
  ],
  max_inventory: 100,
  lot_size: 100,
  min_dte: 5,
  max_dte: 10,
  min_option_quality: 0.6,
  max_daily_orders: 1,
  extreme_max_daily_orders: 2,
  no_trade_gap: 50,
  strategic_state: "NORMAL" as const,
};

const watchlistItem: WatchlistItem = {
  symbol: "HK.00700",
  strategy: "wheel",
  params,
  config_version: 3,
  execution_status: "READY",
  invalidation_reason: "",
  created_at: "2026-08-11T01:00:00Z",
  updated_at: "2026-08-11T02:00:00Z",
};

const signal: WheelSignal = {
  id: 7,
  created_at: "2026-08-11T02:00:00Z",
  symbol: "HK.00700",
  action: "HOLD",
  capability_status: "DATA_BLOCKED",
  blocked_by: ["option_quotes"],
  config_version: 3,
  inventory: { current_price: 110, actual_inventory: 20, effective_inventory: 18, target_inventory: 0, inventory_gap: -18 },
  candidates: [{ direction: "PUT", quantity: 1, quality: 0.8, accepted: false, quote: { strike: 100, expiry: "2026-09-01" }, reasons: ["盘口缺失"] }],
  rejection_reasons: ["实时数据不足"],
  reason: "等待完整报价",
};

const config: WheelConfigVersion = {
  symbol: "HK.00700",
  version: 3,
  created_at: "2026-08-11T02:00:00Z",
  config: { strategy: "wheel", params, audit: "immutable" },
  state: { strategic_state: "NORMAL" },
};

beforeEach(() => {
  apiMocks.getWatchlist.mockResolvedValue([watchlistItem]);
  apiMocks.getWheelSignals.mockResolvedValue([signal]);
  apiMocks.getWheelConfigs.mockResolvedValue([config]);
  apiMocks.getWheelSignalActions.mockResolvedValue([]);
  apiMocks.deleteWatchlist.mockResolvedValue({});
  apiMocks.postBacktest.mockResolvedValue({ ...signal, id: 101 });
  apiMocks.saveWatchlist.mockResolvedValue(watchlistItem);
});

afterEach(() => {
  window.location.hash = "";
});

describe("WatchlistPage", () => {
  it("keeps the watchlist count, audit fields, and empty-state contracts visible", async () => {
    render(<WatchlistPage />);

    expect(await screen.findByText("1 个标的")).toBeInTheDocument();
    expect(screen.getByText("READY · 未登记原因")).toBeInTheDocument();
    expect(screen.getAllByText("实际 / 有效 / 目标").length).toBeGreaterThan(0);
    expect(screen.getByText("wheel · 曲线 2 锚点 · 最大库存 100")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /新增观察标的/ })).toBeInTheDocument();
  });

  it("preserves all three legacy empty-state messages", async () => {
    apiMocks.getWatchlist.mockResolvedValue([]);
    apiMocks.getWheelSignals.mockResolvedValue([]);
    apiMocks.getWheelConfigs.mockResolvedValue([]);
    render(<WatchlistPage />);

    expect(await screen.findByText("观察列表暂无标的。")).toBeInTheDocument();
    expect(await screen.findByText("尚无 Wheel 信号记录；实时供应商未解锁时这是正常状态。")).toBeInTheDocument();
    expect(await screen.findByText("暂无配置版本记录。")).toBeInTheDocument();
  });

  it("opens signal and config details from independent deep links", async () => {
    window.location.hash = "#signal-7";
    render(<WatchlistPage />);

    expect(await screen.findByText("现价 110 · 实际 20 · 期权Δ — · 有效 18 · 目标 0 · 缺口 -18")).toBeInTheDocument();
    expect(screen.getByText("候选: PUT · 1 张 · 质量 80% · 拒绝 · strike 100 · 2026-09-01 · (盘口缺失)")).toBeInTheDocument();
  });

  it("shows the frozen Wheel editor in a drawer with the hidden wheel strategy", async () => {
    render(<WatchlistPage />);
    fireEvent.click(await screen.findByRole("button", { name: /打开编辑器/ }));

    expect(screen.getByText("添加观察标的")).toBeInTheDocument();
    expect(screen.getByDisplayValue("wheel")).toHaveAttribute("type", "hidden");
    expect(screen.getByText("价格必须严格递增，目标库存必须单调不增且位于 0 与最大库存之间。")).toBeInTheDocument();
    const form = document.getElementById("watchlist-wheel-form");
    if (!form) throw new Error("missing Wheel form");
    fireEvent.submit(form);
    expect(await screen.findByText("symbol is required")).toBeInTheDocument();
  });

  it("opens a config version from its independent deep link", async () => {
    window.location.hash = "#config-HK.00700-v3";
    render(<WatchlistPage />);

    expect(await screen.findByText(/config: \{/)).toBeInTheDocument();
    expect(screen.getByText(/state: \{/)).toBeInTheDocument();
  });

  it("loads manual action records without changing the signal table", async () => {
    render(<WatchlistPage />);
    fireEvent.click(await screen.findByRole("button", { name: "人工记录" }));

    await waitFor(() => expect(apiMocks.getWheelSignalActions).toHaveBeenCalledWith(7));
    expect(await screen.findByText("HK.00700 / signal #7：尚无人工处置记录。")).toBeInTheDocument();
  });
});

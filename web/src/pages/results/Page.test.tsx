import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BacktestDetail, BacktestSummary } from "../../api/types";
import type { LinePoint } from "../../components/Chart/LineChart";
import type { CompareSeries } from "./CompareChart";
import { ResultsPage } from "./Page";

const apiMocks = vi.hoisted(() => ({
  getBacktests: vi.fn(),
  getBacktest: vi.fn(),
  postBacktest: vi.fn(),
  backtestExportURL: (id: number, format: "csv" | "json") => `/v1/backtests/${id}/export?format=${format}`,
}));

vi.mock("../../api", () => apiMocks);

vi.mock("../../components/Chart/LineChart", () => ({
  LineChart: ({ data, onPointChange, ariaLabel }: {
    data: readonly LinePoint[];
    onPointChange?: (point: LinePoint | null, index: number) => void;
    ariaLabel?: string;
  }) => <div aria-label={ariaLabel} data-testid="line-chart" onMouseEnter={() => onPointChange?.(data[0] ?? null, 0)} role="img" />,
}));

vi.mock("./CompareChart", () => ({
  CompareChart: ({ series }: { series: readonly CompareSeries[] }) => <div aria-label={`compare-${series.length}`} data-testid="compare-chart" role="img" />,
}));

function summary(id: number, symbol = "HK.00700", strategy = "wheel", metrics: Record<string, number | string | null> = {}): BacktestSummary {
  return {
    id,
    strategy,
    symbol,
    params: {},
    metrics,
    start_ts: "2026-08-10T00:00:00Z",
    end_ts: "2026-08-10T01:00:00Z",
    created_at: "2026-08-10T02:00:00Z",
  };
}

function detail(id: number, overrides: Partial<BacktestDetail> = {}): BacktestDetail {
  return { ...summary(id), equity_curve: [], trades: [], signals: [], ...overrides };
}

const wheelParams = {
  price_position_curve: [
    { price: 400, target_inventory: 100 },
    { price: 500, target_inventory: 0 },
  ],
  max_inventory: 100,
  lot_size: 100,
  min_dte: 5,
  max_dte: 10,
  min_option_quality: 0.6,
  max_daily_orders: 1,
  extreme_max_daily_orders: 2,
  no_trade_gap: 50,
  strategic_state: "NORMAL",
} as const;

const flatSignal = {
  ts: "2026-08-10T00:00:00Z",
  action: "HOLD",
  capability_status: "DATA_BLOCKED",
  blocked_by: ["bars"],
  snapshot_key: "snap-1",
  snapshot_observed_at: "2026-08-10T00:00:05Z",
  direction: "sell",
  actual_inventory: 1.5,
  effective_inventory: 2,
  option_delta_stock: 0.5,
  candidate_code: "HK.00700 2500000",
  quantity: 3,
  reason: "no quote",
};

const rowCheckboxes = (): HTMLInputElement[] => {
  const table = screen.getAllByRole("table")[0] as HTMLElement;
  const body = table.querySelector(".ant-table-tbody") as HTMLElement;
  return Array.from(body.querySelectorAll("tr:not(.ant-table-measure-row) input[type=checkbox]"));
};

beforeEach(() => {
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
  apiMocks.getBacktests.mockResolvedValue([]);
  apiMocks.getBacktest.mockRejectedValue(new Error("unexpected"));
  apiMocks.postBacktest.mockRejectedValue(new Error("unexpected"));
});

describe("ResultsPage list", () => {
  it("renders rows with formatted metrics", async () => {
    apiMocks.getBacktests.mockResolvedValue([
      summary(1, "HK.00700", "wheel", { equity: 12345.6, total_return: 0.123, max_drawdown: -0.05, bars: 120 }),
      summary(2, "US.AAPL", "buy-hold", {}),
    ]);
    render(<ResultsPage />);
    expect(await screen.findByText("HK.00700")).toBeInTheDocument();
    expect(screen.getByText("12,346")).toBeInTheDocument();
    expect(screen.getByText("12.30%")).toBeInTheDocument();
    expect(screen.getByText("-5.00%")).toBeInTheDocument();
    expect(screen.getByText("120")).toBeInTheDocument();
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("shows the default empty state", async () => {
    render(<ResultsPage />);
    expect(await screen.findByText("暂无回测结果。可在上方运行,或使用 wbot backtest -save 导入。")).toBeInTheDocument();
  });

  it("searches the server after a debounce and shows the mismatch empty text", async () => {
    apiMocks.getBacktests.mockResolvedValue([summary(1), summary(2, "US.AAPL", "buy-hold")]);
    render(<ResultsPage />);
    await screen.findByText("HK.00700");
    fireEvent.change(screen.getByLabelText("搜索回测"), { target: { value: "ZZZ" } });
    expect(await screen.findByText("无匹配「ZZZ」的回测结果。")).toBeInTheDocument();
    await waitFor(() => expect(apiMocks.getBacktests).toHaveBeenLastCalledWith(expect.objectContaining({ q: "ZZZ" })));
    expect(apiMocks.getBacktests).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 0, limit: 50 }));
  });

  it("sorts through the server on header clicks", async () => {
    apiMocks.getBacktests.mockResolvedValue([summary(1), summary(2, "US.AAPL", "buy-hold")]);
    render(<ResultsPage />);
    await screen.findByText("HK.00700");
    const header = screen.getByRole("columnheader", { name: /ID/ });
    fireEvent.click(header);
    await waitFor(() => expect(apiMocks.getBacktests).toHaveBeenLastCalledWith(expect.objectContaining({ sort: "id", order: "asc" })));
    fireEvent.click(header);
    await waitFor(() => expect(apiMocks.getBacktests).toHaveBeenLastCalledWith(expect.objectContaining({ sort: "id", order: "desc" })));
  });

  it("pages the list via server offset", async () => {
    const many = Array.from({ length: 50 }, (_, index) => summary(index + 1, `SYM${index}`));
    apiMocks.getBacktests.mockResolvedValue(many);
    render(<ResultsPage />);
    await screen.findByText("SYM0");
    fireEvent.click(screen.getByTitle("Next Page").querySelector("button") as HTMLButtonElement);
    await waitFor(() => expect(apiMocks.getBacktests).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 50 })));
  });
});

describe("ResultsPage compare", () => {
  it("selects exactly two rows and rejects a third with a hint", async () => {
    apiMocks.getBacktests.mockResolvedValue([summary(1), summary(2, "US.AAPL"), summary(3, "US.MSFT")]);
    render(<ResultsPage />);
    await screen.findByText("HK.00700");
    const checkboxes = rowCheckboxes();
    expect(checkboxes).toHaveLength(3);
    const compareButton = screen.getByRole("button", { name: "对比所选回测" });
    expect(compareButton).toBeDisabled();
    fireEvent.click(checkboxes[0] as HTMLInputElement);
    expect(compareButton).toBeDisabled();
    fireEvent.click(checkboxes[1] as HTMLInputElement);
    expect(compareButton).toBeEnabled();
    fireEvent.click(checkboxes[2] as HTMLInputElement);
    expect(screen.getByText("请选择恰好两条回测进行对比。")).toBeInTheDocument();
    expect(checkboxes[2]).not.toBeChecked();
    expect(compareButton).toBeEnabled();
  });

  it("opens the compare view with metrics, curve and legend", async () => {
    apiMocks.getBacktests.mockResolvedValue([summary(1), summary(2, "US.AAPL")]);
    apiMocks.getBacktest.mockImplementation(async (id: number) => detail(id, {
      symbol: id === 1 ? "HK.00700" : "US.AAPL",
      metrics: { equity: id === 1 ? 1000 : 1200 },
      equity_curve: [
        { ts: "2026-08-10T00:00:00Z", equity: 1000 },
        { ts: "2026-08-10T01:00:00Z", equity: 1200 },
      ],
    }));
    render(<ResultsPage />);
    await screen.findByText("HK.00700");
    const checkboxes = rowCheckboxes();
    fireEvent.click(checkboxes[0] as HTMLInputElement);
    fireEvent.click(checkboxes[1] as HTMLInputElement);
    fireEvent.click(screen.getByRole("button", { name: "对比所选回测" }));
    expect(await screen.findByText("对比回测")).toBeInTheDocument();
    expect(screen.getByText("期末权益")).toBeInTheDocument();
    expect(screen.getByText("1,000")).toBeInTheDocument();
    expect(screen.getByText("1,200")).toBeInTheDocument();
    expect(screen.getAllByText("#1 wheel HK.00700").length).toBeGreaterThan(0);
    expect(screen.getAllByText("#2 wheel US.AAPL").length).toBeGreaterThan(0);
    expect(screen.getByTestId("compare-chart")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "返回列表" }));
    expect(screen.queryByText("对比回测")).not.toBeInTheDocument();
  });
});

describe("ResultsPage detail", () => {
  it("opens via the #bt- deep link and highlights the row", async () => {
    window.location.hash = "#bt-7";
    apiMocks.getBacktests.mockResolvedValue([summary(7), summary(8, "US.AAPL")]);
    apiMocks.getBacktest.mockResolvedValue(detail(7, { metrics: { equity: 50000 } }));
    render(<ResultsPage />);
    expect(await screen.findByText("回测详情 #7")).toBeInTheDocument();
    expect(screen.getByText("50,000")).toBeInTheDocument();
    const table = screen.getAllByRole("table")[0] as HTMLElement;
    expect(table.querySelector("tr.selected")).not.toBeNull();
  });

  it("reads the curve on hover and shows the old-data empty text", async () => {
    window.location.hash = "#bt-7";
    apiMocks.getBacktest.mockImplementation(async (id: number) => (id === 7 ? detail(7, { equity_curve: [{ ts: "2026-08-10T00:00:00Z", equity: 50000 }] }) : detail(id, {})));
    render(<ResultsPage />);
    fireEvent.mouseEnter(await screen.findByTestId("line-chart"));
    expect(await screen.findByText("2026-08-10T00:00:00 · 50,000")).toBeInTheDocument();
    window.location.hash = "#bt-8";
    expect(await screen.findByText("该回测无权益曲线(旧数据)。")).toBeInTheDocument();
  });

  it("limits trades to the recent 100 and expands on demand", async () => {
    window.location.hash = "#bt-8";
    const trades = Array.from({ length: 120 }, (_, index) => ({
      ts: "2026-08-10T00:00:00Z",
      action: index % 2 === 0 ? "buy" : "sell",
      symbol: `S${index}`,
      size: index + 1,
      price: 10 + index,
      cash_after: 10000 - index,
    }));
    apiMocks.getBacktest.mockResolvedValue(detail(8, { trades }));
    render(<ResultsPage />);
    expect(await screen.findByText("共 120 笔交易,仅显示最近 100 笔。")).toBeInTheDocument();
    expect(screen.queryByText("S0")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "显示全部" }));
    expect(screen.queryByText("共 120 笔交易,仅显示最近 100 笔。")).not.toBeInTheDocument();
    expect(await screen.findByText("S0")).toBeInTheDocument();
    expect(screen.getAllByText("买入").length).toBeGreaterThan(0);
  });

  it("renders the signal trace with capability and inventory columns", async () => {
    window.location.hash = "#bt-9";
    apiMocks.getBacktest.mockResolvedValue({ ...detail(9, { params: { max_inventory: 100 } }), signals: [flatSignal] } as unknown as BacktestDetail);
    render(<ResultsPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "信号轨迹" }));
    expect(await screen.findByText("DATA_BLOCKED · bars")).toBeInTheDocument();
    expect(screen.getByText("实际 1.5 / 有效 2 / 期权Δ 0.5")).toBeInTheDocument();
    expect(screen.getByText("HK.00700 2500000")).toBeInTheDocument();
    expect(screen.getByText("no quote")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "参数" }));
    expect(await screen.findByText(/"max_inventory": 100/)).toBeInTheDocument();
  });

  it("keeps the selection after returning to the list", async () => {
    window.location.hash = "#bt-7";
    apiMocks.getBacktests.mockResolvedValue([summary(7)]);
    apiMocks.getBacktest.mockResolvedValue(detail(7, { metrics: { equity: 50000 } }));
    render(<ResultsPage />);
    await screen.findByText("回测详情 #7");
    fireEvent.click(screen.getByRole("button", { name: "返回列表" }));
    expect(screen.queryByText("回测详情 #7")).not.toBeInTheDocument();
    const table = screen.getAllByRole("table")[0] as HTMLElement;
    expect(table.querySelector("tr.selected")).not.toBeNull();
  });
});

describe("ResultsPage run form", () => {
  it("reruns a wheel run by filling the form", async () => {
    window.location.hash = "#bt-5";
    apiMocks.getBacktest.mockResolvedValue(detail(5, { strategy: "wheel", params: { ...wheelParams } }));
    render(<ResultsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "重新运行" }));
    await waitFor(() => expect((screen.getByLabelText("回测代码") as HTMLInputElement).value).toBe("HK.00700"));
    await waitFor(() => expect((screen.getByLabelText("最大库存") as HTMLInputElement).value).toBe("100"));
    expect(screen.getByRole("checkbox", { name: "使用观察列表全部标的" })).not.toBeChecked();
  });

  it("reports the rerun error for non-wheel strategies", async () => {
    window.location.hash = "#bt-6";
    apiMocks.getBacktest.mockResolvedValue(detail(6, { strategy: "buy-hold", params: {} }));
    render(<ResultsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "重新运行" }));
    expect(await screen.findByText("该回测不是 Wheel 策略，无法回填。")).toBeInTheDocument();
  });

  it("submits a single run and opens the new detail", async () => {
    window.location.hash = "#bt-5";
    apiMocks.getBacktest.mockResolvedValue(detail(5, { strategy: "wheel", params: { ...wheelParams } }));
    render(<ResultsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "重新运行" }));
    await waitFor(() => expect((screen.getByLabelText("最大库存") as HTMLInputElement).value).toBe("100"));
    apiMocks.postBacktest.mockResolvedValue(detail(9, { metrics: { equity: 9000 } }));
    apiMocks.getBacktest.mockResolvedValue(detail(9, { metrics: { equity: 9000 } }));
    fireEvent.click(screen.getByRole("button", { name: "运行回测" }));
    expect(await screen.findByText("回测 #9 完成,已打开详情。")).toBeInTheDocument();
    expect(await screen.findByText("回测详情 #9")).toBeInTheDocument();
    expect(apiMocks.postBacktest).toHaveBeenCalledWith(expect.objectContaining({ symbol: "HK.00700", strategy: "wheel" }));
  });

  it("requires a symbol for single runs", async () => {
    window.location.hash = "#bt-5";
    apiMocks.getBacktest.mockResolvedValue(detail(5, { strategy: "wheel", params: { ...wheelParams } }));
    render(<ResultsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "重新运行" }));
    await waitFor(() => expect((screen.getByLabelText("最大库存") as HTMLInputElement).value).toBe("100"));
    fireEvent.change(screen.getByLabelText("回测代码"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "运行回测" }));
    expect(await screen.findByText("symbol is required (或勾选使用观察列表全部标的)")).toBeInTheDocument();
  });

  it("runs from the whole watchlist with the checkbox flow", async () => {
    render(<ResultsPage />);
    await screen.findByText("暂无回测结果。可在上方运行,或使用 wbot backtest -save 导入。");
    fireEvent.click(screen.getByRole("checkbox", { name: "使用观察列表全部标的" }));
    expect(screen.getByLabelText("回测代码")).toBeDisabled();
    apiMocks.postBacktest.mockResolvedValue({ runs: [detail(10), detail(11)] });
    const runButtons = screen.getAllByRole("button", { name: "运行回测" });
    const enabled = runButtons.find((button) => button.closest("fieldset") === null);
    expect(enabled).toBeDefined();
    fireEvent.click(enabled as HTMLElement);
    expect(await screen.findByText("完成:2 条回测已保存,见下方列表。")).toBeInTheDocument();
    expect(apiMocks.postBacktest).toHaveBeenCalledWith({ from_watchlist: true });
    await waitFor(() => expect(apiMocks.getBacktests).toHaveBeenCalled());
  });
});

describe("ResultsPage export links", () => {
  it("keeps the direct export URLs identical to the old UI", async () => {
    window.location.hash = "#bt-3";
    apiMocks.getBacktest.mockResolvedValue(detail(3, {}));
    render(<ResultsPage />);
    const csv = await screen.findByRole("link", { name: "CSV" });
    const json = screen.getByRole("link", { name: "JSON" });
    expect(csv).toHaveAttribute("href", "/v1/backtests/3/export?format=csv");
    expect(json).toHaveAttribute("href", "/v1/backtests/3/export?format=json");
  });
});

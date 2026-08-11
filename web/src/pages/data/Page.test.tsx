import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { KlinePoint } from "../../components/Chart/KlineChart";
import type { Bar, BarCoverage, ClusterResponse, DatacheckReport, OptionFreshness } from "../../api/types";
import { formatTime } from "../../lib/format";
import { DataPage } from "./Page";

const apiMocks = vi.hoisted(() => ({
  getAdminCluster: vi.fn(),
  getDatacheck: vi.fn(),
  getBars: vi.fn(),
  postIngest: vi.fn(),
}));

vi.mock("../../api", () => apiMocks);

vi.mock("../../components/Chart/KlineChart", () => ({
  KlineChart: ({ data, ariaLabel }: { data: readonly KlinePoint[]; ariaLabel?: string }) => (
    <div aria-label={ariaLabel} data-testid="kline-chart" data-count={data.length} role="img" />
  ),
}));

const coverageRows: BarCoverage[] = [
  { symbol: "HK.00700", timeframe: "1d", adjust: "fwd", count: 100, min_ts: "2026-01-01T00:00:00Z", max_ts: "2026-08-11T08:00:00Z", max_ts_age_seconds: 180, fresh: "ok" },
  { symbol: "US.AAPL", timeframe: "1d", adjust: "fwd", count: 50, min_ts: "2026-02-01T00:00:00Z", max_ts: "2026-08-10T00:00:00Z", max_ts_age_seconds: 40000, fresh: "stale" },
  { symbol: "HK.09988", timeframe: "5m", adjust: "none", count: 0, min_ts: "", max_ts: "", max_ts_age_seconds: 0, fresh: "unknown" },
];

const freshnessRows: OptionFreshness[] = [
  { underlying: "HK.00700", source: "futu-option", max_ts: "2026-08-11T07:00:00Z", max_ts_age_seconds: 3600, fresh: "ok" },
  { underlying: "US.AAPL", source: "futu-option", max_ts: "2026-08-09T00:00:00Z", max_ts_age_seconds: 200000, fresh: "stale" },
];

function clusterReport(): ClusterResponse {
  return {
    components: {
      process: { version: "v0.1.0", pid: 1, started_at: "2026-08-11T00:00:00Z", uptime_seconds: 3600, listen_addr: "127.0.0.1:8080" },
      db: { ok: true, latency_ms: 1 },
      pipeline: { counts: { running: 0, succeeded: 1, failed: 0 }, recent_runs: [] },
      data_plane: { bars_coverage: coverageRows, options_freshness: freshnessRows },
    },
  };
}

const completeReport = {
  symbols: 2,
  total: 48,
  missing: 0,
  stale: 0,
  checked_at: "2026-08-11T08:30:00Z",
  items: [],
} as unknown as DatacheckReport;

const warnReport = {
  symbols: 2,
  total: 48,
  missing: 2,
  stale: 1,
  checked_at: "2026-08-11T08:30:00Z",
  items: [
    { symbol: "US.AAPL", kind: "bars", timeframe: "1d", adjust: "fwd", state: "missing" },
    { symbol: "HK.09988", kind: "options", state: "missing" },
    { symbol: "HK.00700", kind: "bars", timeframe: "1d", adjust: "fwd", state: "stale", max_ts: "2026-08-10T00:00:00Z" },
  ],
} as unknown as DatacheckReport;

const barsFixture: Bar[] = [
  { ts: "2026-08-11T08:00:00Z", open: 105, high: 112, low: 104, close: 110, volume: 1200 },
  { ts: "2026-08-10T08:00:00Z", open: 99, high: 102, low: 98, close: 100, volume: 800 },
];

beforeEach(() => {
  apiMocks.getAdminCluster.mockImplementation(async () => clusterReport());
  apiMocks.getDatacheck.mockImplementation(async () => completeReport);
  apiMocks.getBars.mockImplementation(async () => []);
  apiMocks.postIngest.mockImplementation(async () => ({ status: "ok" }));
});

function metricCard(label: string): Element {
  const heading = screen.getByText(label);
  const card = heading.closest(".metric-card");
  if (!card) throw new Error(`metric card for ${label} not found`);
  return card;
}

describe("DataPage datacheck hero", () => {
  it("shows ok status, metric cards and checked-at stamp", async () => {
    render(<DataPage />);

    const status = await screen.findByText("完整");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(metricCard("标的")).toHaveTextContent("2");
    expect(metricCard("检查项")).toHaveTextContent("48");
    expect(metricCard("完整项")).toHaveTextContent("48");
    expect(metricCard("缺失")).toHaveTextContent("0");
    expect(metricCard("过期")).toHaveTextContent("0");
    expect(screen.getByText(`检查于 ${formatTime("2026-08-11T08:30:00Z")}`)).toBeInTheDocument();
    expect(screen.getByText("当前数据完整。")).toBeInTheDocument();
  });

  it("shows 未配置 with the original guidance copy when no symbols configured", async () => {
    apiMocks.getDatacheck.mockImplementation(async () => ({ symbols: 0, total: 0, missing: 0, stale: 0, checked_at: "", items: [] } as unknown as DatacheckReport));
    render(<DataPage />);

    expect(await screen.findByText("未配置")).toBeInTheDocument();
    expect(screen.getByText("自选列表为空；添加标的后将自动检查行情矩阵。")).toBeInTheDocument();
  });

  it("shows 需关注 with sorted detail rows and 缺失/过期 states", async () => {
    apiMocks.getDatacheck.mockImplementation(async () => warnReport);
    render(<DataPage />);

    expect(await screen.findByText("需关注")).toBeInTheDocument();
    const hero = screen.getByText("数据齐全").closest(".ant-card");
    if (!hero) throw new Error("datacheck hero card not found");
    const rows = hero.querySelectorAll(".ant-table-tbody > tr.ant-table-row");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("HK.09988");
    expect(rows[0]).toHaveTextContent("期权");
    expect(rows[0]).toHaveTextContent("缺失");
    expect(rows[1]).toHaveTextContent("US.AAPL");
    expect(rows[1]).toHaveTextContent("K 线");
    expect(rows[2]).toHaveTextContent("HK.00700");
    expect(rows[2]).toHaveTextContent("过期");
    expect(rows[2]).toHaveTextContent(formatTime("2026-08-10T00:00:00Z"));
  });

  it("exposes datacheck errors with role=alert", async () => {
    apiMocks.getDatacheck.mockRejectedValue(new Error("datacheck broken"));
    render(<DataPage />);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("datacheck broken");
  });
});

describe("DataPage coverage and options tables", () => {
  it("renders coverage rows with age, freshness cells and max_ts desc order", async () => {
    render(<DataPage />);

    await screen.findByText("HK.00700");
    const card = screen.getByText("已缓存数据").closest(".ant-card");
    if (!card) throw new Error("coverage card not found");
    const rows = card.querySelectorAll(".ant-table-tbody > tr.ant-table-row");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("HK.00700");
    expect(rows[0]).toHaveTextContent("3 分钟前");
    expect(rows[0]).toHaveTextContent("正常");
    expect(rows[1]).toHaveTextContent("US.AAPL");
    expect(rows[1]).toHaveTextContent("11 小时前");
    expect(rows[1]).toHaveTextContent("数据过期");
    expect(rows[2]).toHaveTextContent("HK.09988");
    expect(rows[2]).toHaveTextContent("无数据");
  });

  it("opens detail on coverage row click with row title", async () => {
    render(<DataPage />);

    const row = (await screen.findByText("HK.00700")).closest("tr");
    if (!row) throw new Error("coverage row not found");
    expect(row).toHaveAttribute("title", "点击查看 HK.00700 1d (fwd)");
    fireEvent.click(row);

    expect(await screen.findByText("HK.00700 · 1d · fwd")).toBeInTheDocument();
    expect(apiMocks.getBars).toHaveBeenCalledWith({ symbol: "HK.00700", timeframe: "1d", adjust: "fwd", limit: 100, desc: true });
  });

  it("renders the options freshness tab with pull button", async () => {
    render(<DataPage />);

    fireEvent.click(screen.getByRole("tab", { name: "期权新鲜度" }));
    const pull = (await screen.findAllByRole("button", { name: "拉取期权链" }))[0];
    if (!pull) throw new Error("options pull button not found");
    fireEvent.click(pull);

    expect(apiMocks.postIngest).toHaveBeenCalledWith({ kind: "option", symbol: "HK.00700" });
  });
});

describe("DataPage bars form and detail panel", () => {
  it("loads recent 100 bars on submit and shows 最近 100 根 tag", async () => {
    render(<DataPage />);

    await userEvent.type(await screen.findByPlaceholderText("代码,如 HK.00700 / US.AAPL"), "US.AAPL");
    await userEvent.click(screen.getByRole("button", { name: "加载 K 线" }));

    expect(apiMocks.getBars).toHaveBeenCalledWith({ symbol: "US.AAPL", timeframe: "1d", adjust: "fwd", limit: 100, desc: true });
    expect(screen.getByText("最近 100 根")).toBeInTheDocument();
  });

  it("loads closed-range RFC3339 bars with limit 1000 and switches tag to 指定区间", async () => {
    render(<DataPage />);

    await userEvent.type(await screen.findByPlaceholderText("代码,如 HK.00700 / US.AAPL"), "US.AAPL");
    fireEvent.change(screen.getByLabelText("起始日期"), { target: { value: "2026-08-01" } });
    fireEvent.change(screen.getByLabelText("结束日期"), { target: { value: "2026-08-05" } });
    await userEvent.click(screen.getByRole("button", { name: "加载 K 线" }));

    expect(apiMocks.getBars).toHaveBeenCalledWith({
      symbol: "US.AAPL",
      timeframe: "1d",
      adjust: "fwd",
      from: "2026-08-01T00:00:00Z",
      to: "2026-08-05T23:59:59Z",
      limit: 1000,
      desc: true,
    });
    expect(screen.getByText("指定区间")).toBeInTheDocument();
  });

  it("ignores empty symbol submits", async () => {
    render(<DataPage />);

    await userEvent.click(await screen.findByRole("button", { name: "加载 K 线" }));
    expect(apiMocks.getBars).not.toHaveBeenCalled();
  });

  it("shows detail metrics, bars table with relative change and ascending chart feed", async () => {
    apiMocks.getBars.mockImplementation(async () => barsFixture);
    render(<DataPage />);

    fireEvent.click((await screen.findByText("HK.00700")).closest("tr")!);

    expect(await screen.findByText("HK.00700 · 1d · fwd")).toBeInTheDocument();
    expect(metricCard("K 线数")).toHaveTextContent("2");
    expect(metricCard("首收盘")).toHaveTextContent("100");
    expect(metricCard("末收盘")).toHaveTextContent("110");
    expect(metricCard("区间涨跌")).toHaveTextContent("+10.00%");
    expect(screen.getByTestId("kline-chart")).toHaveAttribute("data-count", "2");
    const detailCard = screen.getByText("行情明细").closest(".ant-card");
    if (!detailCard) throw new Error("detail card not found");
    const rows = detailCard.querySelectorAll(".ant-table-tbody > tr.ant-table-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("2026-08-11 08:00");
    expect(rows[0]).toHaveTextContent("+10.00%");
    expect(rows[1]).toHaveTextContent("—");
  });

  it("switches timeframe via detail tabs and reloads", async () => {
    render(<DataPage />);

    fireEvent.click((await screen.findByText("HK.00700")).closest("tr")!);
    await screen.findByText("HK.00700 · 1d · fwd");
    fireEvent.click(screen.getByRole("tab", { name: "5m" }));

    expect(apiMocks.getBars).toHaveBeenLastCalledWith({ symbol: "HK.00700", timeframe: "5m", adjust: "fwd", limit: 100, desc: true });
  });

  it("shows 该周期暂无 K 线 when a symbol has no bars", async () => {
    render(<DataPage />);

    fireEvent.click((await screen.findByText("HK.00700")).closest("tr")!);

    expect(await screen.findByText("该周期暂无 K 线。")).toBeInTheDocument();
  });

  it("shows load-failure notice with the original copy", async () => {
    apiMocks.getBars.mockRejectedValue(new Error("cannot reach the server"));
    render(<DataPage />);

    fireEvent.click((await screen.findByText("HK.00700")).closest("tr")!);

    expect(await screen.findByText("加载失败,请检查代码与周期。")).toBeInTheDocument();
    expect(screen.getByText("cannot reach the server")).toBeInTheDocument();
  });
});

describe("DataPage ingest actions", () => {
  it("refills bars with incremental ingest and refreshes coverage", async () => {
    render(<DataPage />);

    const refill = (await screen.findAllByRole("button", { name: "补数据" }))[0];
    if (!refill) throw new Error("refill button not found");
    fireEvent.click(refill);

    expect(apiMocks.postIngest).toHaveBeenCalledWith({ kind: "bar", symbol: "HK.00700", timeframe: "1d", adjust: "fwd", from: "2026-08-11T08:00:00Z" });
    await waitFor(() => expect(apiMocks.getAdminCluster).toHaveBeenCalledTimes(2));
    // 防误触:点击补数据不触发该行 drill-in(旧版 stopPropagation 语义保留)
    expect(apiMocks.getBars).not.toHaveBeenCalled();
    expect(screen.queryByText("HK.00700 · 1d · fwd")).not.toBeInTheDocument();
  });

  it("shows 拉取中… while refilling and restores the button on failure with the error", async () => {
    let resolveIngest: (value: { status: string }) => void = () => undefined;
    apiMocks.postIngest.mockImplementation(() => new Promise((resolve) => { resolveIngest = resolve; }));
    render(<DataPage />);

    const refill = (await screen.findAllByRole("button", { name: "补数据" }))[0];
    if (!refill) throw new Error("refill button not found");
    fireEvent.click(refill);
    expect(await screen.findByText("拉取中…")).toBeInTheDocument();

    resolveIngest({ status: "ok" });
    await waitFor(() => expect(screen.getAllByRole("button", { name: "补数据" })).toHaveLength(3));

    const failing = screen.getAllByRole("button", { name: "补数据" })[0];
    if (!failing) throw new Error("refill button not found");
    apiMocks.postIngest.mockRejectedValue(new Error("gateway down"));
    fireEvent.click(failing);
    expect(await screen.findByText("gateway down")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "补数据" }).length).toBeGreaterThan(0);
  });
});

describe("DataPage refresh lifecycle", () => {
  it("stamps 更新于 after manual refresh", async () => {
    render(<DataPage />);

    fireEvent.click(await screen.findByRole("button", { name: /刷新覆盖/ }));

    expect(await screen.findByText(/更新于 \d{2}:\d{2}:\d{2}/)).toBeInTheDocument();
  });

  it("shows first-load errors, recovers on poll and stays silent on poll errors", async () => {
    apiMocks.getAdminCluster.mockRejectedValueOnce(new Error("cannot reach the server"));
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout"] });
    try {
      render(<DataPage />);
      await act(async () => {});
      expect(screen.getByText("cannot reach the server")).toBeInTheDocument();

      apiMocks.getAdminCluster.mockResolvedValue(clusterReport());
      await act(async () => { vi.advanceTimersByTime(30_000); });
      await act(async () => {});
      expect(screen.queryByText("cannot reach the server")).not.toBeInTheDocument();

      apiMocks.getAdminCluster.mockRejectedValueOnce(new Error("silent poll failure"));
      await act(async () => { vi.advanceTimersByTime(30_000); });
      await act(async () => {});
      expect(screen.queryByText("silent poll failure")).not.toBeInTheDocument();
      expect(screen.getByText("HK.00700")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("refreshes the open detail after a refill of the same symbol", async () => {
    apiMocks.getBars.mockImplementation(async () => barsFixture);
    render(<DataPage />);

    fireEvent.click((await screen.findByText("HK.00700")).closest("tr")!);
    await screen.findByText("HK.00700 · 1d · fwd");
    const before = apiMocks.getBars.mock.calls.length;

    fireEvent.click((await screen.findAllByRole("button", { name: "补数据" }))[0]!);
    await waitFor(() => expect(apiMocks.getBars.mock.calls.length).toBeGreaterThan(before));
    expect(apiMocks.getBars).toHaveBeenLastCalledWith({ symbol: "HK.00700", timeframe: "1d", adjust: "fwd", limit: 100, desc: true });
  });
});

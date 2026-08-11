import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LinePoint } from "../../components/Chart/LineChart";
import { formatNumber, formatTime } from "../../lib/format";
import { DashboardPage } from "./Page";

const apiMocks = vi.hoisted(() => ({
  getAccountSnapshots: vi.fn(),
  getFutuAccount: vi.fn(),
  getFutuOrders: vi.fn(),
  getRuns: vi.fn(),
}));

vi.mock("../../api", () => apiMocks);

vi.mock("../../components/Chart/LineChart", () => ({
  LineChart: ({ data, onPointChange, ariaLabel }: {
    data: readonly LinePoint[];
    onPointChange?: (point: LinePoint | null, index: number) => void;
    ariaLabel?: string;
  }) => <div aria-label={ariaLabel} data-testid="line-chart" onMouseEnter={() => onPointChange?.(data[1] ?? null, 1)} role="img" />,
}));

const snapshotPoints = [
  { captured_at: "2026-08-11T01:02:03Z", total_assets: 1234.5, cash: 1000, market_val: 234.5 },
  { captured_at: "2026-08-11T02:02:03Z", total_assets: 6789, cash: 5000, market_val: 1789 },
] as const;

const hoveredPoint = snapshotPoints[1];

beforeEach(() => {
  apiMocks.getAccountSnapshots.mockImplementation(async (env: string) => ({ env, limit: 120, points: snapshotPoints }));
  apiMocks.getFutuAccount.mockImplementation(async (env: string) => ({
    env,
    acc_id: env === "sim" ? 1 : 2,
    funds: { power: 1000, total_assets: 1234.5, cash: 1000, market_val: 234.5, available_cash: 900 },
    positions: [],
  }));
  apiMocks.getFutuOrders.mockResolvedValue({ env: "sim", acc_id: 1, orders: [] });
  apiMocks.getRuns.mockResolvedValue([]);
});

describe("DashboardPage", () => {
  it("shows the asset value when the curve reports a hovered point", async () => {
    render(<DashboardPage />);

    const chart = await screen.findByTestId("line-chart");
    fireEvent.mouseEnter(chart);

    expect(await screen.findByText(`${formatTime(hoveredPoint.captured_at)} · ${formatNumber(hoveredPoint.total_assets)}`)).toBeInTheDocument();
  });
});

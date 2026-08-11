import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { createChart, type IChartApi, type ISeriesApi, type LineData, type Time } from "lightweight-charts";
import { ChartBase } from "../../components/Chart/ChartBase";
import { cssColor } from "../../lib/themeTokens";
import { useTheme } from "../../hooks/useTheme";
import type { EquityPoint } from "../../api/types";

export interface CompareSeries {
  label: string;
  color: string;
  points: readonly EquityPoint[];
}

export interface CompareChartProps {
  series: readonly CompareSeries[];
  height?: number;
}

function chartOptions() {
  return {
    autoSize: true,
    layout: { background: { color: cssColor("--surface") }, textColor: cssColor("--muted") },
    grid: { vertLines: { color: cssColor("--border") }, horzLines: { color: cssColor("--border") } },
    rightPriceScale: { borderColor: cssColor("--border") },
    timeScale: { borderColor: cssColor("--border") },
  };
}

// One chart instance holding one line series per run; setData swaps series
// data and applyOptions repaints theme colors without losing zoom state.
export function CompareChart({ series, height = 300 }: CompareChartProps): ReactNode {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRefs = useRef<Map<string, ISeriesApi<"Line">>>(new Map());
  const { theme } = useTheme();

  useEffect(() => {
    if (!containerRef.current) return undefined;
    const chart = createChart(containerRef.current, chartOptions());
    chartRef.current = chart;
    return () => {
      chart.remove();
      chartRef.current = null;
      seriesRefs.current = new Map();
    };
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    const seen = new Set<string>();
    for (const item of series) {
      seen.add(item.label);
      const points: LineData<Time>[] = item.points.map((point) => ({ time: Math.floor(Date.parse(point.ts) / 1000) as Time, value: point.equity }));
      let lineSeries = seriesRefs.current.get(item.label);
      if (!lineSeries) {
        lineSeries = chart.addLineSeries({ color: item.color, lineWidth: 2 });
        seriesRefs.current.set(item.label, lineSeries);
      }
      lineSeries.setData(points);
    }
    for (const [label, lineSeries] of seriesRefs.current) {
      if (!seen.has(label)) {
        chart.removeSeries(lineSeries);
        seriesRefs.current.delete(label);
      }
    }
    chart.timeScale().fitContent();
  }, [series]);

  useEffect(() => {
    chartRef.current?.applyOptions(chartOptions());
  }, [theme]);

  return <ChartBase ariaLabel="Equity curves overlay" className="dashboard-chart" height={height} ref={containerRef} />;
}

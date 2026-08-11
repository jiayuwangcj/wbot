import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { createChart, type CandlestickData, type IChartApi, type ISeriesApi, type Time } from "lightweight-charts";
import { ChartBase } from "./ChartBase";
import { cssColor } from "../../lib/themeTokens";
import { useTheme } from "../../hooks/useTheme";

export interface KlinePoint {
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
}

export interface KlineChartProps {
  data: readonly KlinePoint[];
  ariaLabel?: string;
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

export function KlineChart({ data, ariaLabel = "K 线图", height = 360 }: KlineChartProps): ReactNode {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const { theme } = useTheme();

  useEffect(() => {
    if (!containerRef.current) return undefined;
    const chart = createChart(containerRef.current, chartOptions());
    const series = chart.addCandlestickSeries({
      upColor: cssColor("--ok"),
      downColor: cssColor("--down"),
      borderUpColor: cssColor("--ok"),
      borderDownColor: cssColor("--down"),
      wickUpColor: cssColor("--ok"),
      wickDownColor: cssColor("--down"),
    });
    chartRef.current = chart;
    seriesRef.current = series;
    return () => {
      chart.remove();
      chartRef.current = null;
      seriesRef.current = null;
    };
  }, []);

  useEffect(() => {
    const series = seriesRef.current;
    const chart = chartRef.current;
    if (!series || !chart) return;
    const seriesData: CandlestickData<Time>[] = data.map((point) => ({
      time: Math.floor(Date.parse(point.time) / 1000) as Time,
      open: point.open,
      high: point.high,
      low: point.low,
      close: point.close,
    }));
    series.setData(seriesData);
    chart.timeScale().fitContent();
  }, [data]);

  useEffect(() => {
    const chart = chartRef.current;
    const series = seriesRef.current;
    if (!chart || !series) return;
    chart.applyOptions(chartOptions());
    series.applyOptions({
      upColor: cssColor("--ok"),
      downColor: cssColor("--down"),
      borderUpColor: cssColor("--ok"),
      borderDownColor: cssColor("--down"),
      wickUpColor: cssColor("--ok"),
      wickDownColor: cssColor("--down"),
    });
  }, [theme]);

  return <ChartBase ariaLabel={ariaLabel} className="detail-chart" height={height} ref={containerRef} />;
}

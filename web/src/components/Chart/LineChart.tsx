import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import { createChart, type IChartApi, type ISeriesApi, type LineData, type Time } from "lightweight-charts";
import { ChartBase } from "./ChartBase";
import { cssColor } from "../../lib/themeTokens";
import { useTheme } from "../../hooks/useTheme";

export interface LinePoint {
  time: string;
  value: number;
  label?: string;
}

export interface LineChartProps {
  data: readonly LinePoint[];
  ariaLabel?: string;
  height?: number;
  onPointChange?: (point: LinePoint | null, index: number) => void;
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

export function LineChart({ data, ariaLabel = "权益曲线", height = 280, onPointChange }: LineChartProps): ReactNode {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<"Line"> | null>(null);
  const { theme } = useTheme();
  const pointRef = useRef(data);
  pointRef.current = data;
  const onPointChangeRef = useRef(onPointChange);
  onPointChangeRef.current = onPointChange;
  const [selectedIndex, setSelectedIndex] = useState(-1);

  useEffect(() => {
    if (!containerRef.current) return undefined;
    const chart = createChart(containerRef.current, chartOptions());
    const series = chart.addLineSeries({
      color: cssColor("--accent"),
      lineWidth: 2,
      crosshairMarkerVisible: true,
    });
    chartRef.current = chart;
    seriesRef.current = series;
    const onMove = (param: Parameters<IChartApi["subscribeCrosshairMove"]>[0] extends (event: infer E) => void ? E : never): void => {
      const time = param.time;
      if (time === undefined) {
        onPointChangeRef.current?.(null, -1);
        return;
      }
      const index = pointRef.current.findIndex((point) => Math.floor(Date.parse(point.time) / 1000) === Number(time));
      onPointChangeRef.current?.(index >= 0 ? pointRef.current[index] ?? null : null, index);
    };
    chart.subscribeCrosshairMove(onMove);
    return () => {
      chart.unsubscribeCrosshairMove(onMove);
      chart.remove();
      chartRef.current = null;
      seriesRef.current = null;
    };
  }, []);

  useEffect(() => {
    const series = seriesRef.current;
    const chart = chartRef.current;
    if (!series || !chart) return;
    const seriesData: LineData<Time>[] = data.map((point) => ({ time: Math.floor(Date.parse(point.time) / 1000) as Time, value: point.value }));
    series.setData(seriesData);
    chart.timeScale().fitContent();
  }, [data]);

  useEffect(() => {
    const chart = chartRef.current;
    const series = seriesRef.current;
    if (!chart || !series) return;
    chart.applyOptions(chartOptions());
    series.applyOptions({ color: cssColor("--accent") });
  }, [theme]);

  const onKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>): void => {
    if (data.length === 0 || (event.key !== "ArrowLeft" && event.key !== "ArrowRight")) return;
    event.preventDefault();
    const next = event.key === "ArrowLeft" ? Math.max(0, selectedIndex <= 0 ? 0 : selectedIndex - 1) : Math.min(data.length - 1, selectedIndex < 0 ? 0 : selectedIndex + 1);
    setSelectedIndex(next);
    onPointChange?.(data[next] ?? null, next);
  }, [data, onPointChange, selectedIndex]);
  const selected = selectedIndex >= 0 ? data[selectedIndex] : undefined;
  return (
    <>
      <ChartBase ariaLabel={ariaLabel} className="dashboard-chart" height={height} onKeyDown={onKeyDown} ref={containerRef} tabIndex={0} />
      {selected ? <div aria-live="polite" className="chart-readout">{selected.label ?? selected.time} · {selected.value.toLocaleString("en-US", { maximumFractionDigits: 2 })}</div> : null}
    </>
  );
}

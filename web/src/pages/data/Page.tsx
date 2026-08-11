import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { Alert, Button, Card, Empty, Input, Select, Table, Tabs, Typography } from "antd";
import type { TableColumnsType } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { getAdminCluster, getBars, getDatacheck, postIngest } from "../../api";
import type { Bar, BarCoverage, ClusterResponse, DatacheckItem, DatacheckReport, OptionFreshness } from "../../api/types";
import { DataTable } from "../../components/DataTable";
import { KlineChart, type KlinePoint } from "../../components/Chart/KlineChart";
import { useAutoRefresh } from "../../hooks/useAutoRefresh";
import { formatClock, formatNumber, formatTime } from "../../lib/format";

const TIMEFRAMES = ["1m", "5m", "15m", "30m", "60m", "1d", "1w", "1mo"] as const;

const ADJUST_OPTIONS = [
  { value: "fwd", label: "前复权 fwd" },
  { value: "none", label: "不复权 none" },
  { value: "back", label: "后复权 back" },
];

type RefillState = "idle" | "busy" | "done";

interface CoverageRow extends BarCoverage {
  key: string;
}

interface FreshnessRow extends OptionFreshness {
  key: string;
}

interface BarRow extends Bar {
  key: string;
  change: number | null;
}

interface DatacheckRow {
  key: string;
  symbol: string;
  kind: string;
  timeframe: string;
  adjust: string;
  state: string;
  latest: string;
}

function formatAge(seconds: number): string {
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))} 分钟前`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)} 小时前`;
  return `${Math.round(seconds / 86400)} 天前`;
}

function freshnessText(fresh: string): string {
  if (fresh === "stale") return "数据过期";
  if (fresh === "unknown") return "无数据";
  return "正常";
}

function freshnessClass(fresh: string): string {
  if (fresh === "stale") return "freshness-stale";
  if (fresh === "unknown") return "freshness-unknown";
  return "";
}

function toError(caught: unknown): Error {
  return caught instanceof Error ? caught : new Error("unexpected server response");
}

function itemLatestTs(item: DatacheckItem): string {
  const raw = item as unknown as Record<string, unknown>;
  return typeof raw.max_ts === "string" ? raw.max_ts : "";
}

export function DataPage(): ReactNode {
  const [cluster, setCluster] = useState<ClusterResponse | null>(null);
  const [datacheck, setDatacheck] = useState<DatacheckReport | null>(null);
  const [topError, setTopError] = useState<Error | null>(null);
  const [datacheckError, setDatacheckError] = useState<Error | null>(null);
  const [coverageError, setCoverageError] = useState<Error | null>(null);
  const [optionsError, setOptionsError] = useState<Error | null>(null);
  const [initialLoading, setInitialLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [refillStates, setRefillStates] = useState<Record<string, RefillState>>({});
  const [symbol, setSymbol] = useState("");
  const [timeframe, setTimeframe] = useState("1d");
  const [adjust, setAdjust] = useState("fwd");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const [rangeTag, setRangeTag] = useState("最近 100 根");
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailSymbol, setDetailSymbol] = useState("");
  const [detailTimeframe, setDetailTimeframe] = useState("1d");
  const [detailAdjust, setDetailAdjust] = useState("fwd");
  const [detailBars, setDetailBars] = useState<Bar[]>([]);
  const [detailError, setDetailError] = useState<Error | null>(null);
  const [detailLoadFailed, setDetailLoadFailed] = useState(false);

  const refreshCluster = useCallback(async (): Promise<void> => {
    const data = await getAdminCluster();
    setCluster(data);
    setTopError(null);
    setUpdatedAt(formatClock());
  }, []);

  const refreshDatacheck = useCallback(async (): Promise<void> => {
    try {
      const report = await getDatacheck();
      setDatacheck(report);
      setDatacheckError(null);
    } catch (caught) {
      setDatacheckError(toError(caught));
    }
  }, []);

  const refreshAll = useCallback(async (): Promise<void> => {
    await Promise.all([refreshCluster(), refreshDatacheck()]);
  }, [refreshCluster, refreshDatacheck]);

  useEffect(() => {
    let mounted = true;
    void refreshAll()
      .catch((caught) => {
        if (mounted) setTopError(toError(caught));
      })
      .finally(() => {
        if (mounted) setInitialLoading(false);
      });
    return () => {
      mounted = false;
    };
  }, [refreshAll]);

  useAutoRefresh(() => {
    void refreshAll().catch(() => {});
  });

  const handleManualRefresh = async (): Promise<void> => {
    setRefreshing(true);
    try {
      await refreshAll();
    } catch (caught) {
      setTopError(toError(caught));
    } finally {
      setRefreshing(false);
    }
  };

  const loadBars = useCallback(async (symbolValue: string, timeframeValue: string, adjustValue: string, from = "", to = ""): Promise<void> => {
    setDetailSymbol(symbolValue);
    setDetailTimeframe(timeframeValue);
    setDetailAdjust(adjustValue);
    setSymbol(symbolValue);
    setTimeframe(timeframeValue);
    setAdjust(adjustValue);
    setDetailOpen(true);
    setDetailError(null);
    setDetailLoadFailed(false);
    const hasRange = from !== "" || to !== "";
    const query: { symbol: string; timeframe: string; adjust: string; from?: string; to?: string; limit: number; desc: boolean } = {
      symbol: symbolValue,
      timeframe: timeframeValue,
      adjust: adjustValue,
      limit: hasRange ? 1000 : 100,
      desc: true,
    };
    if (hasRange) {
      query.from = from;
      query.to = to;
    }
    try {
      const bars = await getBars(query);
      setDetailBars(bars);
    } catch (caught) {
      setDetailLoadFailed(true);
      setDetailError(toError(caught));
    }
  }, []);

  const handleSubmit = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    const symbolValue = symbol.trim();
    if (symbolValue === "") return;
    const from = fromDate !== "" ? `${fromDate}T00:00:00Z` : "";
    const to = toDate !== "" ? `${toDate}T23:59:59Z` : "";
    const hasRange = from !== "" || to !== "";
    setRangeTag(hasRange ? "指定区间" : "最近 100 根");
    void loadBars(symbolValue, timeframe, adjust, from, to);
  };

  const handleTimeframeChange = (timeframeValue: string): void => {
    if (detailSymbol === "") return;
    void loadBars(detailSymbol, timeframeValue, detailAdjust);
  };

  const coverageKey = (row: BarCoverage): string => `${row.symbol}|${row.timeframe}|${row.adjust}`;
  const freshnessKey = (row: OptionFreshness): string => `${row.underlying}|${row.source}`;

  const handleRefill = async (row: BarCoverage): Promise<void> => {
    const key = coverageKey(row);
    setCoverageError(null);
    setRefillStates((current) => ({ ...current, [key]: "busy" }));
    try {
      await postIngest({ kind: "bar", symbol: row.symbol, timeframe: row.timeframe, adjust: row.adjust, from: row.max_ts });
      setRefillStates((current) => ({ ...current, [key]: "done" }));
      await refreshAll();
      if (detailOpen && detailSymbol === row.symbol) void loadBars(row.symbol, row.timeframe, row.adjust);
    } catch (caught) {
      setCoverageError(toError(caught));
    } finally {
      setRefillStates((current) => ({ ...current, [key]: "idle" }));
    }
  };

  const handlePullOptions = async (row: OptionFreshness): Promise<void> => {
    const key = freshnessKey(row);
    setOptionsError(null);
    setRefillStates((current) => ({ ...current, [key]: "busy" }));
    try {
      await postIngest({ kind: "option", symbol: row.underlying });
      setRefillStates((current) => ({ ...current, [key]: "done" }));
      await refreshAll();
    } catch (caught) {
      setOptionsError(toError(caught));
    } finally {
      setRefillStates((current) => ({ ...current, [key]: "idle" }));
    }
  };

  const refillLabel = (key: string, idleLabel: string): string => {
    if (refillStates[key] === "busy") return "拉取中…";
    if (refillStates[key] === "done") return "已更新";
    return idleLabel;
  };

  const coverageRows: CoverageRow[] = useMemo(() => {
    const rows = (cluster?.components.data_plane.bars_coverage ?? []).map((row) => ({ ...row, key: coverageKey(row) }));
    rows.sort((a, b) => b.max_ts.localeCompare(a.max_ts));
    return rows;
  }, [cluster]);

  const freshnessRows: FreshnessRow[] = useMemo(() => {
    const rows = (cluster?.components.data_plane.options_freshness ?? []).map((row) => ({ ...row, key: freshnessKey(row) }));
    rows.sort((a, b) => b.max_ts.localeCompare(a.max_ts));
    return rows;
  }, [cluster]);

  const report = datacheck;
  const symbolsCount = report?.symbols ?? 0;
  const missingCount = report?.missing ?? 0;
  const staleCount = report?.stale ?? 0;
  const completeCount = report ? report.total - missingCount - staleCount : 0;
  const statusState = symbolsCount === 0 ? "idle" : missingCount === 0 && staleCount === 0 ? "ok" : "warn";
  const statusLabel = symbolsCount === 0 ? "未配置" : missingCount === 0 && staleCount === 0 ? "完整" : "需关注";

  const datacheckRows: DatacheckRow[] = useMemo(() => {
    if (!report) return [];
    const timeframeOrder = ["1m", "5m", "15m", "30m", "60m", "1d", "1w", "1mo"];
    const adjustOrder = ["none", "fwd", "back"];
    const rows = report.items
      .filter((item) => item.state === "missing" || item.state === "stale")
      .map((item) => ({
        key: `${item.symbol}|${item.kind}|${item.timeframe}|${item.adjust}`,
        symbol: item.symbol,
        kind: item.kind,
        timeframe: item.timeframe,
        adjust: item.adjust,
        state: item.state,
        latest: itemLatestTs(item),
      }));
    rows.sort((a, b) => {
      const priority = (state: string): number => (state === "missing" ? 0 : 1);
      return priority(a.state) - priority(b.state)
        || a.symbol.localeCompare(b.symbol)
        || a.kind.localeCompare(b.kind)
        || timeframeOrder.indexOf(a.timeframe) - timeframeOrder.indexOf(b.timeframe)
        || adjustOrder.indexOf(a.adjust) - adjustOrder.indexOf(b.adjust);
    });
    return rows;
  }, [report]);

  const coverageColumns: TableColumnsType<CoverageRow> = [
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: (a, b) => a.symbol.localeCompare(b.symbol) },
    { title: "周期", dataIndex: "timeframe", key: "timeframe", sorter: (a, b) => a.timeframe.localeCompare(b.timeframe) },
    { title: "复权", dataIndex: "adjust", key: "adjust" },
    { title: "数量", dataIndex: "count", key: "count", align: "right", className: "number-cell", sorter: (a, b) => a.count - b.count },
    { title: "最早", dataIndex: "min_ts", key: "min_ts", sorter: (a, b) => a.min_ts.localeCompare(b.min_ts), render: formatTime },
    { title: "最新", dataIndex: "max_ts", key: "max_ts", defaultSortOrder: "descend", sortDirections: ["ascend", "descend"], sorter: (a, b) => a.max_ts.localeCompare(b.max_ts), render: formatTime },
    { title: "年龄", dataIndex: "max_ts_age_seconds", key: "max_ts_age_seconds", align: "right", className: "number-cell", render: formatAge },
    { title: "新鲜度", dataIndex: "fresh", key: "fresh", onCell: (row) => ({ className: freshnessClass(row.fresh) }), render: freshnessText },
    {
      title: "操作",
      key: "action",
      render: (_value: unknown, row: CoverageRow) => {
        const key = coverageKey(row);
        return (
          <Button type="link" size="small" disabled={refillStates[key] === "busy"} onClick={() => void handleRefill(row)}>
            {refillLabel(key, "补数据")}
          </Button>
        );
      },
    },
  ];

  const freshnessColumns: TableColumnsType<FreshnessRow> = [
    { title: "标的", dataIndex: "underlying", key: "underlying", sorter: (a, b) => a.underlying.localeCompare(b.underlying) },
    { title: "来源", dataIndex: "source", key: "source", sorter: (a, b) => a.source.localeCompare(b.source) },
    { title: "最新", dataIndex: "max_ts", key: "max_ts", defaultSortOrder: "descend", sortDirections: ["ascend", "descend"], sorter: (a, b) => a.max_ts.localeCompare(b.max_ts), render: formatTime },
    { title: "年龄", dataIndex: "max_ts_age_seconds", key: "max_ts_age_seconds", align: "right", className: "number-cell", render: formatAge },
    { title: "新鲜度", dataIndex: "fresh", key: "fresh", onCell: (row) => ({ className: freshnessClass(row.fresh) }), render: freshnessText },
    {
      title: "操作",
      key: "action",
      render: (_value: unknown, row: FreshnessRow) => {
        const key = freshnessKey(row);
        return (
          <Button type="link" size="small" disabled={refillStates[key] === "busy"} onClick={() => void handlePullOptions(row)}>
            {refillLabel(key, "拉取期权链")}
          </Button>
        );
      },
    },
  ];

  const datacheckColumns: TableColumnsType<DatacheckRow> = [
    { title: "代码", dataIndex: "symbol", key: "symbol" },
    { title: "类型", dataIndex: "kind", key: "kind", render: (value: string) => (value === "options" ? "期权" : "K 线") },
    { title: "周期", dataIndex: "timeframe", key: "timeframe", render: (value: string) => value || "—" },
    { title: "复权", dataIndex: "adjust", key: "adjust", render: (value: string) => value || "—" },
    { title: "状态", dataIndex: "state", key: "state", onCell: (row) => ({ className: row.state === "missing" ? "state-down" : "state-warn" }), render: (value: string) => (value === "missing" ? "缺失" : "过期") },
    { title: "最新", dataIndex: "latest", key: "latest", render: (value: string) => (value ? formatTime(value) : "—") },
  ];

  const barRows: BarRow[] = detailBars.map((bar, index) => {
    const prev = detailBars[index + 1];
    return { ...bar, key: `${bar.ts}-${index}`, change: prev ? (bar.close - prev.close) / prev.close : null };
  });

  const barColumns: TableColumnsType<BarRow> = [
    { title: "时间", dataIndex: "ts", key: "ts", render: (value: string) => value.slice(0, 16).replace("T", " ") },
    { title: "开", dataIndex: "open", key: "open", align: "right", className: "number-cell", render: formatNumber },
    { title: "高", dataIndex: "high", key: "high", align: "right", className: "number-cell", render: formatNumber },
    { title: "低", dataIndex: "low", key: "low", align: "right", className: "number-cell", render: formatNumber },
    { title: "收", dataIndex: "close", key: "close", align: "right", className: "number-cell", render: formatNumber },
    {
      title: "涨跌幅",
      dataIndex: "change",
      key: "change",
      align: "right",
      className: "number-cell",
      render: (value: number | null) => {
        if (value === null) return <Typography.Text type="secondary">—</Typography.Text>;
        const text = `${value >= 0 ? "+" : ""}${(value * 100).toFixed(2)}%`;
        return <span className={value >= 0 ? "num-up" : "num-down"}>{text}</span>;
      },
    },
    { title: "量", dataIndex: "volume", key: "volume", align: "right", className: "number-cell" },
  ];

  const firstBar = detailBars[detailBars.length - 1];
  const lastBar = detailBars[0];
  const change = firstBar && lastBar ? (lastBar.close - firstBar.close) / firstBar.close : null;
  const changeText = change === null ? "—" : `${change >= 0 ? "+" : ""}${(change * 100).toFixed(2)}%`;
  const changeClass = change === null ? "" : change >= 0 ? " up" : " down";
  const chartData: KlinePoint[] = [...detailBars].reverse().map((bar) => ({ time: bar.ts, open: bar.open, high: bar.high, low: bar.low, close: bar.close }));

  const coverageEmpty = <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无已缓存行情数据。" />;
  const datacheckEmpty = statusState === "ok" ? <span className="notice ok">当前数据完整。</span> : <span>自选列表为空；添加标的后将自动检查行情矩阵。</span>;

  return (
    <>
      <div className="page-heading">
        <div>
          <Typography.Title level={1}>数据</Typography.Title>
          <Typography.Paragraph>行情数据齐全度与已缓存覆盖一览;数据经 serve 代理拉取,浏览器不直连行情网关。</Typography.Paragraph>
        </div>
      </div>
      <div className="data-toolbar">
        <form className="bars-form" onSubmit={handleSubmit}>
          <Input value={symbol} onChange={(event) => setSymbol(event.target.value)} placeholder="代码,如 HK.00700 / US.AAPL" aria-label="代码" />
          <Select value={timeframe} onChange={setTimeframe} options={TIMEFRAMES.map((tf) => ({ value: tf, label: tf }))} aria-label="周期" />
          <Select value={adjust} onChange={setAdjust} options={ADJUST_OPTIONS} aria-label="复权" />
          <input type="date" value={fromDate} onChange={(event) => setFromDate(event.target.value)} title="起始日期(留空=最近 100 根)" aria-label="起始日期" />
          <input type="date" value={toDate} onChange={(event) => setToDate(event.target.value)} title="结束日期(留空=最近 100 根)" aria-label="结束日期" />
          <Button type="primary" htmlType="submit">加载 K 线</Button>
        </form>
        <div className="data-toolbar-status">
          <Typography.Text className="section-tag">{rangeTag}</Typography.Text>
          <Button icon={<ReloadOutlined />} loading={refreshing} onClick={() => void handleManualRefresh()}>{refreshing ? "刷新中…" : "刷新覆盖"}</Button>
          {updatedAt ? <Typography.Text type="secondary">更新于 {updatedAt}</Typography.Text> : null}
        </div>
      </div>
      {topError ? <Alert type="error" showIcon message={topError.message} style={{ marginBottom: 20 }} /> : null}
      <div className="data-grid">
        <div className="data-main">
          <Card
            className="dashboard-card datacheck-hero"
            title={(
              <div className="dashboard-card-header">
                <span>数据齐全</span>
                <span className={`section-tag ${statusState}`} aria-live="polite">{report ? statusLabel : "—"}</span>
              </div>
            )}
          >
            {report?.checked_at ? <Typography.Paragraph type="secondary">检查于 {formatTime(report.checked_at)}</Typography.Paragraph> : null}
            {datacheckError ? <Alert role="alert" type="error" showIcon message={datacheckError.message} /> : null}
            {report ? (
              <>
                <div className="metric-grid">
                  <div className="metric-card"><h3>标的</h3><p className="metric-value">{report.symbols}</p></div>
                  <div className="metric-card"><h3>检查项</h3><p className="metric-value">{report.total}</p></div>
                  <div className="metric-card"><h3>完整项</h3><p className="metric-value up">{completeCount}</p></div>
                  <div className="metric-card"><h3>缺失</h3><p className="metric-value down">{missingCount}</p></div>
                  <div className="metric-card"><h3>过期</h3><p className="metric-value warn">{staleCount}</p></div>
                </div>
                <DataTable className="table-card" columns={datacheckColumns} data={datacheckRows} loading={initialLoading} emptyText={datacheckEmpty} rowKey="key" scrollX={640} />
              </>
            ) : null}
          </Card>
          <Card className="dashboard-card" title="覆盖与新鲜度">
            <Tabs
              items={[
                {
                  key: "coverage",
                  label: "已缓存数据",
                  children: (
                    <>
                      <Typography.Paragraph type="secondary">点击一行查看该标的的行情明细;过期数据可点「补数据」经网关拉取。</Typography.Paragraph>
                      {coverageError ? <Alert type="error" showIcon message={coverageError.message} /> : null}
                      <Table<CoverageRow>
                        className="table-card"
                        columns={coverageColumns}
                        dataSource={coverageRows}
                        loading={initialLoading}
                        locale={{ emptyText: coverageEmpty }}
                        onRow={(row) => ({ title: `点击查看 ${row.symbol} ${row.timeframe} (${row.adjust})`, onClick: () => void loadBars(row.symbol, row.timeframe, row.adjust) })}
                        rowClassName={() => "coverage-row"}
                        rowKey="key"
                        scroll={{ x: 860 }}
                        size="middle"
                        pagination={false}
                      />
                    </>
                  ),
                },
                {
                  key: "options",
                  label: "期权新鲜度",
                  children: (
                    <>
                      <Typography.Paragraph type="secondary">按标的×来源聚合期权行情的最新时间(阈值 4 小时);过期数据可点「拉取期权链」经网关拉取。</Typography.Paragraph>
                      {optionsError ? <Alert type="error" showIcon message={optionsError.message} /> : null}
                      <DataTable className="table-card" columns={freshnessColumns} data={freshnessRows} loading={initialLoading} emptyText="暂无期权行情数据。" rowKey="key" scrollX={700} />
                    </>
                  ),
                },
              ]}
            />
          </Card>
        </div>
        <div className="data-side">
          <Card
            className="dashboard-card"
            title={(
              <div className="dashboard-card-header">
                <span>行情明细</span>
                <span className="section-tag">{detailOpen ? `${detailSymbol} · ${detailTimeframe} · ${detailAdjust}` : "—"}</span>
              </div>
            )}
          >
            {detailError ? <Alert type="error" showIcon message={detailError.message} /> : null}
            {!detailOpen ? (
              <Typography.Paragraph type="secondary">从左侧覆盖表选择,或在上方输入代码后加载。</Typography.Paragraph>
            ) : (
              <>
                <Tabs size="small" activeKey={detailTimeframe} onChange={handleTimeframeChange} items={TIMEFRAMES.map((tf) => ({ key: tf, label: tf }))} />
                <div className="metric-grid">
                  <div className="metric-card"><h3>K 线数</h3><p className="metric-value">{detailBars.length > 0 ? detailBars.length : "—"}</p></div>
                  <div className="metric-card"><h3>首收盘</h3><p className="metric-value">{firstBar ? formatNumber(firstBar.close) : "—"}</p></div>
                  <div className="metric-card"><h3>末收盘</h3><p className="metric-value">{lastBar ? formatNumber(lastBar.close) : "—"}</p></div>
                  <div className="metric-card"><h3>区间涨跌</h3><p className={`metric-value${changeClass}`}>{changeText}</p></div>
                </div>
                <KlineChart ariaLabel="K 线图" data={chartData} height={360} />
                {!detailLoadFailed && detailBars.length > 0 ? (
                  <DataTable className="table-card" columns={barColumns} data={barRows} rowKey="key" scrollX={680} />
                ) : null}
                {detailLoadFailed ? (
                  <Typography.Paragraph type="secondary">加载失败,请检查代码与周期。</Typography.Paragraph>
                ) : detailBars.length === 0 ? (
                  <Typography.Paragraph type="secondary">该周期暂无 K 线。</Typography.Paragraph>
                ) : null}
              </>
            )}
          </Card>
        </div>
      </div>
    </>
  );
}

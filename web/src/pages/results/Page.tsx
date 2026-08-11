import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Key, ReactNode } from "react";
import { Alert, Button, Card, Checkbox, Col, Empty, Input, Row, Space, Spin, Statistic, Table, Tabs, Tag, Typography } from "antd";
import type { InputRef, TableColumnsType, TableProps } from "antd";
import { backtestExportURL, getBacktest, getBacktests, postBacktest } from "../../api";
import type { BacktestDetail, BacktestSummary, EquityPoint, WheelParams } from "../../api/types";
import { LineChart, type LinePoint } from "../../components/Chart/LineChart";
import { DataTable } from "../../components/DataTable";
import { WheelForm } from "../../components/WheelForm";
import { useAsyncData } from "../../hooks/useAsyncData";
import { useAutoRefresh } from "../../hooks/useAutoRefresh";
import { formatPercent, formatSide, formatTime } from "../../lib/format";
import { cssColor } from "../../lib/themeTokens";
import { CompareChart, type CompareSeries } from "./CompareChart";
import { toRerunWheelParams, toSignalRow, type SignalRow } from "./trace";

const PAGE_SIZE = 50;
const TRADES_LIMIT = 100;
const EMPTY_LIST_TEXT = "暂无回测结果。可在上方运行,或使用 wbot backtest -save 导入。";
const COMPARE_HINT = "请选择恰好两条回测进行对比。";
const COMPARE_COLORS = [cssColor("--accent"), cssColor("--accent-2")];

interface ListRow extends BacktestSummary {}

interface TradeRow {
  key: string;
  ts: string;
  action: string;
  symbol: string;
  size: number;
  price: number;
  cash_after: number;
}

interface SignalTableRow extends SignalRow {
  key: string;
}

interface CompareRow {
  key: string;
  label: string;
  a: string;
  b: string;
}

function metricOf(item: Pick<BacktestSummary, "metrics">, key: string): number | null {
  const raw = item.metrics[key];
  if (typeof raw === "number" && Number.isFinite(raw)) return raw;
  if (typeof raw === "string") {
    const parsed = Number(raw);
    if (Number.isFinite(parsed)) return parsed;
  }
  return null;
}

function metricMoney(value: number | null): string {
  return value === null ? "—" : value.toLocaleString("en-US", { maximumFractionDigits: 0 });
}

function inventoryNumber(value: number | null): string {
  return value === null ? "—" : value.toLocaleString("en-US", { maximumFractionDigits: 2 });
}

function runLabel(run: Pick<BacktestSummary, "id" | "strategy" | "symbol">): string {
  return `#${run.id} ${run.strategy} ${run.symbol}`;
}

const COMPARE_METRICS: ReadonlyArray<{ key: string; label: string; render: (value: number | null) => string }> = [
  { key: "equity", label: "期末权益", render: metricMoney },
  { key: "total_return", label: "总收益率", render: formatPercent },
  { key: "max_drawdown", label: "最大回撤", render: formatPercent },
  { key: "bars", label: "K 线数", render: (value) => (value === null ? "—" : String(value)) },
];

export function ResultsPage(): ReactNode {
  const [watchlistChecked, setWatchlistChecked] = useState(false);
  const [runSymbol, setRunSymbol] = useState("");
  const [rerunValues, setRerunValues] = useState<Partial<WheelParams> | null>(null);
  const [runBusy, setRunBusy] = useState(false);
  const [runOk, setRunOk] = useState<string | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [serverQ, setServerQ] = useState("");
  const [sortKey, setSortKey] = useState("");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [page, setPage] = useState(1);
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [compareHint, setCompareHint] = useState<string | null>(null);
  const [compare, setCompare] = useState<readonly [BacktestDetail, BacktestDetail] | null>(null);
  const [compareError, setCompareError] = useState<Error | null>(null);
  const [compareLoading, setCompareLoading] = useState(false);
  const [detail, setDetail] = useState<BacktestDetail | null>(null);
  const [detailError, setDetailError] = useState<Error | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [openDetailId, setOpenDetailId] = useState<number | null>(null);
  const [tradesShowAll, setTradesShowAll] = useState(false);
  const [curvePoint, setCurvePoint] = useState<LinePoint | null>(null);
  const runCardRef = useRef<HTMLDivElement>(null);
  const runSymbolRef = useRef<InputRef>(null);
  const detailRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const searchTimerRef = useRef<number | null>(null);

  const listState = useAsyncData<BacktestSummary[]>(() => getBacktests({
    ...(serverQ !== "" ? { q: serverQ } : {}),
    ...(sortKey !== "" ? { sort: sortKey, order: sortDir } : {}),
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
  }), [serverQ, sortKey, sortDir, page]);
  useAutoRefresh(listState.refresh);

  const debouncedSearch = useCallback((value: string): void => {
    if (searchTimerRef.current !== null) window.clearTimeout(searchTimerRef.current);
    searchTimerRef.current = window.setTimeout(() => {
      setServerQ(value.trim());
      setPage(1);
    }, 250);
  }, []);
  useEffect(() => () => {
    if (searchTimerRef.current !== null) window.clearTimeout(searchTimerRef.current);
  }, []);

  const openDetailIdRef = useRef<number | null>(null);

  const openDetail = useCallback(async (id: number): Promise<void> => {
    if (openDetailIdRef.current === id) return;
    openDetailIdRef.current = id;
    setOpenDetailId(id);
    setDetail(null);
    setDetailError(null);
    setDetailLoading(true);
    setTradesShowAll(false);
    setCurvePoint(null);
    try {
      setDetail(await getBacktest(id));
    } catch (caught) {
      setDetailError(caught instanceof Error ? caught : new Error("unexpected server response"));
    } finally {
      setDetailLoading(false);
      detailRef.current?.scrollIntoView();
    }
  }, []);

  useEffect(() => {
    const applyHash = (): void => {
      const match = window.location.hash.match(/^#bt-(\d+)$/);
      if (match?.[1] !== undefined) {
        const id = Number(match[1]);
        if (Number.isInteger(id) && id > 0) void openDetail(id);
      }
    };
    applyHash();
    window.addEventListener("hashchange", applyHash);
    return () => window.removeEventListener("hashchange", applyHash);
  }, [openDetail]);

  const handleRunSingle = async (params: WheelParams): Promise<void> => {
    const symbol = runSymbol.trim();
    if (!symbol) throw new Error("symbol is required (或勾选使用观察列表全部标的)");
    setRunError(null);
    setRunOk(null);
    setRunBusy(true);
    try {
      const response = await postBacktest({ symbol, strategy: "wheel", params });
      if ("runs" in response) {
        setRunOk(`完成:${response.runs.length} 条回测已保存,见下方列表。`);
        listRef.current?.scrollIntoView();
      } else {
        setRunOk(`回测 #${response.id} 完成,已打开详情。`);
        await openDetail(response.id);
      }
      void listState.refresh();
    } finally {
      setRunBusy(false);
    }
  };

  const handleRunWatchlist = async (): Promise<void> => {
    setRunError(null);
    setRunOk(null);
    setRunBusy(true);
    try {
      const response = await postBacktest({ from_watchlist: true });
      if (!("runs" in response)) throw new Error("unexpected server response");
      setRunOk(`完成:${response.runs.length} 条回测已保存,见下方列表。`);
      listRef.current?.scrollIntoView();
      void listState.refresh();
    } catch (caught) {
      setRunError(caught instanceof Error ? caught.message : "unexpected server response");
    } finally {
      setRunBusy(false);
    }
  };

  const handleRerun = (): void => {
    if (!detail) return;
    setWatchlistChecked(false);
    setRunSymbol(detail.symbol);
    setRunError(null);
    setRunOk(null);
    if (detail.strategy === "wheel") {
      setRerunValues(toRerunWheelParams(detail.params));
    } else {
      setRerunValues(null);
      setRunError("该回测不是 Wheel 策略，无法回填。");
    }
    runCardRef.current?.scrollIntoView({ behavior: "smooth" });
    runSymbolRef.current?.focus();
  };

  const openCompare = async (): Promise<void> => {
    if (selectedIds.length !== 2) {
      setCompareHint(COMPARE_HINT);
      return;
    }
    setCompareHint(null);
    setCompareError(null);
    setCompareLoading(true);
    try {
      const [first, second] = await Promise.all(selectedIds.map((id) => getBacktest(id)));
      if (first === undefined || second === undefined) throw new Error("unexpected server response");
      setCompare([first, second]);
    } catch (caught) {
      setCompareError(caught instanceof Error ? caught : new Error("unexpected server response"));
    } finally {
      setCompareLoading(false);
    }
  };

  const closeDetail = (): void => {
    openDetailIdRef.current = null;
    setDetail(null);
    setDetailError(null);
    listRef.current?.scrollIntoView();
  };

  const closeCompare = (): void => {
    setCompare(null);
    setCompareError(null);
    listRef.current?.scrollIntoView();
  };

  const items = listState.data ?? [];
  const trimmedQuery = searchInput.trim().toLowerCase();
  const displayItems = useMemo(() => {
    if (trimmedQuery === "") return items;
    return items.filter((item) => item.symbol.toLowerCase().includes(trimmedQuery) || item.strategy.toLowerCase().includes(trimmedQuery));
  }, [items, trimmedQuery]);
  const listEmptyText = trimmedQuery === "" ? EMPTY_LIST_TEXT : `无匹配「${searchInput.trim()}」的回测结果。`;
  const rows: ListRow[] = displayItems.map((item) => item);

  const sortOrderFor = (key: string): "ascend" | "descend" | null => (sortKey === key ? (sortDir === "asc" ? "ascend" : "descend") : null);
  const columns: TableColumnsType<ListRow> = [
    { title: "ID", dataIndex: "id", key: "id", sorter: true, sortOrder: sortOrderFor("id"), width: 80 },
    { title: "策略", dataIndex: "strategy", key: "strategy", sorter: true, sortOrder: sortOrderFor("strategy") },
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: true, sortOrder: sortOrderFor("symbol") },
    { title: "权益", key: "equity", align: "right", className: "number-cell", sorter: true, sortOrder: sortOrderFor("equity"), render: (_value: unknown, record: ListRow) => metricMoney(metricOf(record, "equity")) },
    { title: "收益", key: "total_return", align: "right", className: "number-cell", sorter: true, sortOrder: sortOrderFor("total_return"), render: (_value: unknown, record: ListRow) => formatPercent(metricOf(record, "total_return")) },
    { title: "最大回撤", key: "max_drawdown", align: "right", className: "number-cell", sorter: true, sortOrder: sortOrderFor("max_drawdown"), render: (_value: unknown, record: ListRow) => formatPercent(metricOf(record, "max_drawdown")) },
    { title: "K 线", key: "bars", align: "right", className: "number-cell", sorter: true, sortOrder: sortOrderFor("bars"), render: (_value: unknown, record: ListRow) => {
      const bars = metricOf(record, "bars");
      return bars === null ? "—" : String(bars);
    } },
    { title: "创建时间", dataIndex: "created_at", key: "created_at", sorter: true, sortOrder: sortOrderFor("created_at"), render: formatTime },
    { title: "详情", key: "actions", render: (_value: unknown, record: ListRow) => <Button size="small" type="link" onClick={() => void openDetail(record.id)}>详情</Button> },
  ];

  const handleTableChange: TableProps<ListRow>["onChange"] = (pagination, _filters, sorter): void => {
    if (typeof pagination.current === "number" && pagination.current !== page) {
      setPage(pagination.current);
      return;
    }
    const single = Array.isArray(sorter) ? sorter[0] : sorter;
    if (!single) return;
    const key = typeof single.columnKey === "string" ? single.columnKey : "";
    if (key === "") return;
    setPage(1);
    if (single.order === undefined || single.order === null) {
      setSortKey("");
      return;
    }
    setSortKey(key);
    setSortDir(single.order === "ascend" ? "asc" : "desc");
  };

  const handleSelectionChange = (keys: readonly Key[]): void => {
    const next = keys.map((key) => Number(key));
    if (next.length > 2) {
      setCompareHint(COMPARE_HINT);
      return;
    }
    setCompareHint(null);
    setSelectedIds(next);
  };

  const curvePoints: EquityPoint[] = detail?.equity_curve ?? [];
  const lineData: LinePoint[] = curvePoints.map((point) => ({ time: point.ts, value: point.equity }));
  const handleCurvePointChange = useCallback((point: LinePoint | null): void => {
    setCurvePoint(point);
  }, []);
  const detailMetrics = detail ? [
    { label: "期末权益", value: metricMoney(metricOf(detail, "equity")) },
    { label: "总收益率", value: formatPercent(metricOf(detail, "total_return")) },
    { label: "最大回撤", value: formatPercent(metricOf(detail, "max_drawdown")) },
    { label: "K 线数", value: metricOf(detail, "bars") === null ? "—" : String(metricOf(detail, "bars")) },
  ] : [];

  const allTrades = detail?.trades ?? [];
  const visibleTrades = tradesShowAll ? allTrades : allTrades.slice(-TRADES_LIMIT);
  const tradeRows: TradeRow[] = visibleTrades.map((trade, index) => ({ key: `${trade.ts}-${index}`, ...trade }));
  const tradeColumns: TableColumnsType<TradeRow> = [
    { title: "时间", dataIndex: "ts", key: "ts", render: formatTime },
    { title: "方向", dataIndex: "action", key: "action", render: (value: string) => <span className={value.toLowerCase() === "buy" ? "side-buy" : "side-sell"}>{formatSide(value)}</span> },
    { title: "代码", dataIndex: "symbol", key: "symbol" },
    { title: "数量", dataIndex: "size", key: "size", align: "right", className: "number-cell" },
    { title: "价格", dataIndex: "price", key: "price", align: "right", className: "number-cell" },
    { title: "剩余现金", dataIndex: "cash_after", key: "cash_after", align: "right", className: "number-cell" },
  ];

  const signalRows: SignalTableRow[] = (detail?.signals ?? []).map((signal, index) => ({ key: String(index), ...toSignalRow(signal) }));
  const signalColumns: TableColumnsType<SignalTableRow> = [
    { title: "时间", dataIndex: "ts", key: "ts", render: formatTime },
    { title: "动作", dataIndex: "action", key: "action" },
    { title: "能力状态", key: "capability", render: (_value: unknown, record: SignalTableRow) => `${record.capability_status || "READY"}${record.blocked_by.length > 0 ? ` · ${record.blocked_by.join(", ")}` : ""}` },
    { title: "原子快照", key: "snapshot", render: (_value: unknown, record: SignalTableRow) => (record.snapshot_key !== "" ? record.snapshot_key + (record.snapshot_observed_at !== "" ? ` · ${formatTime(record.snapshot_observed_at)}` : "") : "—") },
    { title: "方向", dataIndex: "direction", key: "direction", render: (value: string) => value || "—" },
    { title: "库存", key: "inventory", render: (_value: unknown, record: SignalTableRow) => `实际 ${inventoryNumber(record.actual_inventory)} / 有效 ${inventoryNumber(record.effective_inventory)} / 期权Δ ${inventoryNumber(record.option_delta_stock)}` },
    { title: "候选", dataIndex: "candidate_code", key: "candidate_code", render: (value: string) => value || "—" },
    { title: "数量", dataIndex: "quantity", key: "quantity", align: "right", className: "number-cell" },
    { title: "原因", dataIndex: "reason", key: "reason", render: (value: string) => value || "—" },
  ];

  const detailTabs = detail ? [
    {
      key: "trades",
      label: "交易记录",
      children: (
        <>
          <DataTable className="table-card" columns={tradeColumns} data={tradeRows} emptyText="未记录交易。" rowKey="key" scrollX={760} />
          {allTrades.length > TRADES_LIMIT && !tradesShowAll ? (
            <Space style={{ marginTop: 12 }}>
              <Typography.Text type="secondary">{`共 ${allTrades.length} 笔交易,仅显示最近 ${TRADES_LIMIT} 笔。`}</Typography.Text>
              <Button type="link" onClick={() => setTradesShowAll(true)}>显示全部</Button>
            </Space>
          ) : null}
        </>
      ),
    },
    {
      key: "signals",
      label: "信号轨迹",
      children: <DataTable className="table-card" columns={signalColumns} data={signalRows} emptyText="该回测未保存逐 bar 信号轨迹（旧数据）。" rowKey="key" scrollX={1080} />,
    },
    {
      key: "params",
      label: "参数",
      children: <pre>{JSON.stringify(detail.params ?? {}, null, 2)}</pre>,
    },
  ] : [];

  const compareRows: CompareRow[] = compare ? COMPARE_METRICS.map((metric) => ({
    key: metric.key,
    label: metric.label,
    a: metric.render(metricOf(compare[0], metric.key)),
    b: metric.render(metricOf(compare[1], metric.key)),
  })).concat({ key: "params", label: "参数", a: JSON.stringify(compare[0].params ?? {}), b: JSON.stringify(compare[1].params ?? {}) }) : [];
  const compareColumns: TableColumnsType<CompareRow> = compare ? [
    { title: "指标", dataIndex: "label", key: "label" },
    { title: runLabel(compare[0]), dataIndex: "a", key: "a" },
    { title: runLabel(compare[1]), dataIndex: "b", key: "b" },
  ] : [];
  const compareSeries: CompareSeries[] = compare ? compare.map((run, index) => ({
    label: runLabel(run),
    color: COMPARE_COLORS[index] ?? cssColor("--accent"),
    points: run.equity_curve ?? [],
  })) : [];
  const compareHasCurves = compareSeries.some((series) => series.points.length > 0);

  return (
    <>
      <div className="page-heading">
        <div>
          <Typography.Title level={1}>回测</Typography.Title>
          <Typography.Paragraph>运行 Wheel 回测并审计权益曲线、交易与逐 bar 信号轨迹；CSV/JSON 导出与 CLI 同一 serializer。</Typography.Paragraph>
        </div>
      </div>
      <div className="dashboard-stack">
        <Card className="dashboard-card" ref={runCardRef} title="启动回测">
          <Typography.Paragraph type="secondary">Wheel 只生成回测提醒，不会自动下单；缺少同一原子快照中的 bid/ask、Delta、IV、OI、Theta、volume、lot size 或 freshness 时明确记录 DATA_BLOCKED/HOLD。</Typography.Paragraph>
          <Space wrap>
            <Input ref={runSymbolRef} aria-label="回测代码" disabled={watchlistChecked} onChange={(event) => setRunSymbol(event.target.value)} placeholder="HK.00700 / US.AAPL" style={{ width: 240 }} value={runSymbol} />
            <Checkbox checked={watchlistChecked} onChange={(event) => {
              setWatchlistChecked(event.target.checked);
              setRunError(null);
            }}>使用观察列表全部标的</Checkbox>
          </Space>
          <Typography.Paragraph style={{ marginTop: 12 }}>策略：<Tag>wheel</Tag> · 仅提醒</Typography.Paragraph>
          <fieldset disabled={watchlistChecked || runBusy} style={{ border: 0, margin: 0, minWidth: 0, padding: 0 }}>
            <WheelForm {...(rerunValues !== null ? { initialValues: rerunValues } : {})} formId="backtest-form" onSubmit={handleRunSingle} submitLabel="运行回测" />
          </fieldset>
          {watchlistChecked ? <Button disabled={runBusy} onClick={() => void handleRunWatchlist()} style={{ marginTop: 16 }} type="primary">{runBusy ? "运行中…" : "运行回测"}</Button> : null}
          {runOk !== null ? <Alert closable message={runOk} onClose={() => setRunOk(null)} showIcon style={{ marginTop: 16 }} type="success" /> : null}
          {runError !== null ? <Alert closable message={runError} onClose={() => setRunError(null)} showIcon style={{ marginTop: 16 }} type="error" /> : null}
        </Card>
        <Card className="dashboard-card" extra={<Button disabled={selectedIds.length !== 2} loading={compareLoading} onClick={() => void openCompare()} type="primary">对比所选回测</Button>} ref={listRef} title="回测结果">
          <Space direction="vertical" style={{ width: "100%" }}>
            <Input allowClear aria-label="搜索回测" onChange={(event) => {
              setSearchInput(event.target.value);
              debouncedSearch(event.target.value);
            }} placeholder="搜索代码/策略(全库,如 00700 或 wheel)" value={searchInput} />
            {compareHint !== null ? <Typography.Text type="warning">{compareHint}</Typography.Text> : null}
            {listState.error !== null ? <Alert message={listState.error.message} showIcon type="error" /> : null}
            <Table<ListRow>
              className="table-card"
              columns={columns}
              dataSource={rows}
              loading={listState.loading}
              locale={{ emptyText: <Empty description={listEmptyText} image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
              onChange={handleTableChange}
              pagination={{
                current: page,
                pageSize: PAGE_SIZE,
                showSizeChanger: false,
                total: (page - 1) * PAGE_SIZE + displayItems.length + (displayItems.length === PAGE_SIZE ? 1 : 0),
              }}
              rowClassName={(record) => (record.id === openDetailId ? "selected" : "")}
              rowKey="id"
              rowSelection={{
                onChange: handleSelectionChange,
                selectedRowKeys: selectedIds,
              }}
              scroll={{ x: 1000 }}
            />
          </Space>
        </Card>
        {compare !== null ? (
          <Card className="dashboard-card" extra={<Button onClick={closeCompare}>返回列表</Button>} title="对比回测">
            {compareError !== null ? <Alert message={compareError.message} showIcon style={{ marginBottom: 16 }} type="error" /> : null}
            <DataTable className="table-card" columns={compareColumns} data={compareRows} rowKey="key" />
            {compareHasCurves ? (
              <Card className="dashboard-card" style={{ marginTop: 16 }} title="权益曲线叠加">
                <CompareChart series={compareSeries} />
                <div className="legend">
                  {compareSeries.map((series) => (
                    <span className="legend-item" key={series.label}>
                      <span className="legend-swatch" style={{ background: series.color }} />
                      <span>{series.label}</span>
                    </span>
                  ))}
                </div>
              </Card>
            ) : <Alert message="无权益曲线可叠加(旧数据)。" showIcon style={{ marginTop: 16 }} type="info" />}
          </Card>
        ) : null}
        {detail !== null || detailError !== null || detailLoading ? (
          <Card className="dashboard-card" extra={detail !== null ? (
            <Space wrap>
              <a download href={backtestExportURL(detail.id, "csv")}>CSV</a>
              <span aria-hidden="true"> · </span>
              <a download href={backtestExportURL(detail.id, "json")}>JSON</a>
              <Button onClick={handleRerun} size="small">重新运行</Button>
              <Button onClick={closeDetail} size="small">返回列表</Button>
            </Space>
          ) : null} ref={detailRef} title={detail !== null ? `回测详情 #${detail.id}` : "回测详情"}>
            {detailError !== null ? <Alert message={detailError.message} showIcon type="error" /> : null}
            {detailLoading && detail === null ? <Space><Spin /><Typography.Text type="secondary">加载详情…</Typography.Text></Space> : null}
            {detail !== null ? (
              <>
                <Row gutter={[16, 16]}>
                  {detailMetrics.map((metric) => (
                    <Col key={metric.label} lg={6} sm={12} xs={24}><Card className="dashboard-stat-card"><Statistic title={metric.label} value={metric.value} /></Card></Col>
                  ))}
                </Row>
                <Card className="dashboard-card" style={{ marginTop: 16 }} title="权益曲线">
                  {lineData.length > 0 ? (
                    <>
                      <LineChart ariaLabel="回测权益曲线" data={lineData} onPointChange={handleCurvePointChange} />
                      {curvePoint !== null ? <div aria-live="polite" className="chart-readout">{curvePoint.time.slice(0, 19)} · {metricMoney(curvePoint.value)}</div> : null}
                    </>
                  ) : <div className="dashboard-chart-empty">该回测无权益曲线(旧数据)。</div>}
                </Card>
                <Tabs items={detailTabs} style={{ marginTop: 16 }} />
              </>
            ) : null}
          </Card>
        ) : null}
      </div>
    </>
  );
}

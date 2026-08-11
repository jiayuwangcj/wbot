import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button, Card, Col, Row, Segmented, Statistic, Tabs, Tag, Typography } from "antd";
import type { TableColumnsType } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { getAccountSnapshots, getFutuAccount, getFutuOrders } from "../../api";
import type { AccountSnapshotPoint, Environment, FutuAccount, FutuOrders, IngestionRun, Position, Order } from "../../api/types";
import { DataTable } from "../../components/DataTable";
import { LineChart, type LinePoint } from "../../components/Chart/LineChart";
import { useAsyncData } from "../../hooks/useAsyncData";
import { useAutoRefresh } from "../../hooks/useAutoRefresh";
import { formatClock, formatMoney, formatNumber, formatRunStatus, formatSide, formatTime, semanticClass } from "../../lib/format";
import { getRuns } from "../../api";

interface EnvState {
  account: FutuAccount | null;
  snapshots: AccountSnapshotPoint[];
  error: Error | null;
  loading: boolean;
}

interface PositionRow extends Position {
  key: string;
}

interface OrderRow extends Order {
  key: string;
}

interface RunRow extends IngestionRun {
  key: string;
}

interface AccountRow {
  key: string;
  environment: string;
  accountId: string;
  total: string;
  cash: string;
  marketVal: string;
  availableCash: string;
  power: string;
  positions: string;
  status: string;
}

const ENVIRONMENTS: Environment[] = ["sim", "real"];

function emptyEnvState(): EnvState {
  return { account: null, snapshots: [], error: null, loading: true };
}

function environmentLabel(environment: Environment): string {
  return environment === "sim" ? "模拟盘" : "实盘";
}

function DashboardTable({ account, orders, runs, accountError, ordersError, runsError, loading }: {
  account: FutuAccount | null;
  orders: FutuOrders | null;
  runs: IngestionRun[] | null;
  accountError: Error | null;
  ordersError: Error | null;
  runsError: Error | null;
  loading: boolean;
}): ReactNode {
  const positionRows: PositionRow[] = (account?.positions ?? []).map((position) => ({ ...position, key: position.symbol }));
  const orderRows: OrderRow[] = (orders?.orders ?? []).map((order, index) => ({ ...order, key: `${order.create_time}-${order.symbol}-${index}` }));
  const runRows: RunRow[] = (runs ?? []).map((run) => ({ ...run, key: String(run.id) }));
  const positionColumns: TableColumnsType<PositionRow> = [
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: (a, b) => a.symbol.localeCompare(b.symbol) },
    { title: "数量", dataIndex: "qty", key: "qty", align: "right", className: "number-cell", sorter: (a, b) => a.qty - b.qty, render: formatNumber },
    { title: "成本价", dataIndex: "avg_cost", key: "avg_cost", align: "right", className: "number-cell", render: formatNumber },
    { title: "现价", dataIndex: "price", key: "price", align: "right", className: "number-cell", render: formatNumber },
    { title: "市值", dataIndex: "market_val", key: "market_val", align: "right", className: "number-cell", sorter: (a, b) => a.market_val - b.market_val, defaultSortOrder: "descend", render: formatMoney },
    { title: "盈亏", dataIndex: "pl", key: "pl", align: "right", className: "number-cell", sorter: (a, b) => a.pl - b.pl, render: (value: number) => <span className={semanticClass(value)}>{formatMoney(value)}</span> },
  ];
  const orderColumns: TableColumnsType<OrderRow> = [
    { title: "时间", dataIndex: "create_time", key: "create_time", sorter: (a, b) => a.create_time.localeCompare(b.create_time), defaultSortOrder: "descend", render: formatTime },
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: (a, b) => a.symbol.localeCompare(b.symbol) },
    { title: "方向", dataIndex: "side", key: "side", render: (value: string) => <span className={value.toLowerCase() === "buy" ? "side-buy" : "side-sell"}>{formatSide(value)}</span> },
    { title: "状态", dataIndex: "status", key: "status" },
    { title: "数量", dataIndex: "qty", key: "qty", align: "right", className: "number-cell", render: formatNumber },
    { title: "价格", dataIndex: "price", key: "price", align: "right", className: "number-cell", render: formatNumber },
    { title: "已成交", dataIndex: "fill_qty", key: "fill_qty", align: "right", className: "number-cell", render: formatNumber },
  ];
  const runColumns: TableColumnsType<RunRow> = [
    { title: "ID", dataIndex: "id", key: "id" },
    { title: "来源", dataIndex: "source", key: "source" },
    { title: "状态", dataIndex: "status", key: "status", render: formatRunStatus },
    { title: "开始", dataIndex: "started_at", key: "started_at", render: formatTime },
    { title: "结束", dataIndex: "finished_at", key: "finished_at", render: (value: string | null) => value === null ? "运行中" : formatTime(value) },
  ];
  const positionPanel = accountError ? <Alert type="error" showIcon message={accountError.message} action={accountError instanceof Error ? "请检查 Futu 网关" : undefined} /> : <DataTable className="table-card" columns={positionColumns} data={positionRows} emptyText="当前环境无持仓。" loading={loading} rowKey="key" scrollX={720} />;
  const orderPanel = ordersError ? <Alert type="error" showIcon message={ordersError.message} action="请检查 Futu 网关" /> : <DataTable className="table-card" columns={orderColumns} data={orderRows} emptyText="暂无挂单。" loading={loading} rowKey="key" scrollX={820} />;
  const runPanel = runsError ? <Alert type="error" showIcon message={runsError.message} /> : <DataTable className="table-card" columns={runColumns} data={runRows} emptyText="尚无入库记录。" loading={loading} rowKey="key" scrollX={650} />;
  return <Tabs items={[{ key: "positions", label: "持仓", children: positionPanel }, { key: "orders", label: "当前订单", children: orderPanel }, { key: "runs", label: "最近入库", children: runPanel }]} />;
}

export function DashboardPage(): ReactNode {
  const [environment, setEnvironment] = useState<Environment>("sim");
  const [envState, setEnvState] = useState<Record<Environment, EnvState>>({ sim: emptyEnvState(), real: emptyEnvState() });
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const orders = useAsyncData<FutuOrders>(() => getFutuOrders(environment), [environment]);
  const runs = useAsyncData<IngestionRun[]>(getRuns, []);

  const refreshAccounts = useCallback(async (): Promise<void> => {
    setEnvState((current) => ({ sim: { ...current.sim, loading: true }, real: { ...current.real, loading: true } }));
    const accountResults = await Promise.allSettled(ENVIRONMENTS.map((env) => getFutuAccount(env)));
    const snapshotResults = await Promise.allSettled(ENVIRONMENTS.map((env) => getAccountSnapshots(env)));
    setEnvState((current) => {
      const next = { ...current };
      ENVIRONMENTS.forEach((env, index) => {
        const accountResult = accountResults[index];
        const snapshotResult = snapshotResults[index];
        const account = accountResult?.status === "fulfilled" ? accountResult.value : null;
        const accountError = accountResult?.status === "rejected" ? (accountResult.reason instanceof Error ? accountResult.reason : new Error("cannot reach the server")) : null;
        const snapshots = snapshotResult?.status === "fulfilled" ? snapshotResult.value.points : [];
        next[env] = { account, snapshots, error: accountError, loading: false };
      });
      return next;
    });
    setUpdatedAt(formatClock());
  }, []);

  const refreshAll = useCallback(async (): Promise<void> => {
    await Promise.all([refreshAccounts(), orders.refresh(), runs.refresh()]);
  }, [orders.refresh, refreshAccounts, runs.refresh]);

  useEffect(() => {
    void refreshAccounts();
  }, [refreshAccounts]);
  useAutoRefresh(refreshAll);

  const current = envState[environment];
  const selectedOrders = orders.data;
  const bothFailed = ENVIRONMENTS.every((env) => envState[env].error !== null);
  const lineData: LinePoint[] = current.snapshots.map((point) => ({ time: point.captured_at, value: point.total_assets, label: `${formatTime(point.captured_at)} · 总资产` }));
  const summaryCards = current.account ? [
    ["总资产", current.account.funds.total_assets],
    ["现金", current.account.funds.cash],
    ["证券市值", current.account.funds.market_val],
    ["购买力", current.account.funds.power],
    ["可用资金", current.account.funds.available_cash],
  ] as const : [];
  const curveReadout = useMemo(() => lineData.length >= 2 ? `${formatClock(new Date(lineData[0]?.time ?? ""))} → ${formatClock(new Date(lineData.at(-1)?.time ?? ""))} · ${lineData.length} 点` : "", [lineData]);
  const accountRows: AccountRow[] = ENVIRONMENTS.map((env) => {
    const item = envState[env];
    const account = item.account;
    return {
      key: env,
      environment: environmentLabel(env),
      accountId: account ? String(account.acc_id) : "—",
      total: account ? formatMoney(account.funds.total_assets) : "—",
      cash: account ? formatMoney(account.funds.cash) : "—",
      marketVal: account ? formatMoney(account.funds.market_val) : "—",
      availableCash: account ? formatMoney(account.funds.available_cash) : "—",
      power: account ? formatMoney(account.funds.power) : "—",
      positions: account ? String(account.positions.length) : "—",
      status: item.error ? "不可用" : account ? "可用" : "加载中",
    };
  });
  const accountColumns: TableColumnsType<AccountRow> = [
    { title: "环境", dataIndex: "environment", key: "environment" },
    { title: "账户 ID", dataIndex: "accountId", key: "accountId" },
    { title: "总资产", dataIndex: "total", key: "total", align: "right", className: "number-cell" },
    { title: "现金", dataIndex: "cash", key: "cash", align: "right", className: "number-cell" },
    { title: "市值", dataIndex: "marketVal", key: "marketVal", align: "right", className: "number-cell" },
    { title: "可用资金", dataIndex: "availableCash", key: "availableCash", align: "right", className: "number-cell" },
    { title: "购买力", dataIndex: "power", key: "power", align: "right", className: "number-cell" },
    { title: "持仓数", dataIndex: "positions", key: "positions", align: "right", className: "number-cell" },
    { title: "状态", dataIndex: "status", key: "status" },
  ];
  const handleManualRefresh = async (): Promise<void> => {
    setRefreshing(true);
    try {
      await refreshAll();
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <>
      <div className="page-heading">
        <div>
          <Typography.Title level={1}>账户总览</Typography.Title>
          <Typography.Paragraph>把资产变化放在第一视觉层，持仓、订单与入库记录收纳在同一处审计。</Typography.Paragraph>
        </div>
        <Tag color={environment === "sim" ? "green" : "red"}>{environment === "sim" ? "PAPER · 模拟盘" : "REAL · 实盘"}</Tag>
      </div>
      <div className="dashboard-toolbar">
        <div className="dashboard-toolbar-left">
          <Segmented<Environment> options={[{ label: "Paper 模拟盘", value: "sim" }, { label: "实盘 Live", value: "real" }]} value={environment} onChange={setEnvironment} />
          <Typography.Text type="secondary">{updatedAt ? `更新于 ${updatedAt}` : "等待首次刷新"}</Typography.Text>
        </div>
        <div className="dashboard-toolbar-right">
          <Button icon={<ReloadOutlined />} loading={refreshing} onClick={() => void handleManualRefresh()}>{refreshing ? "刷新中…" : "刷新"}</Button>
        </div>
      </div>
      {bothFailed ? <Alert closable showIcon type="error" message="Futu 网关不可用" description={`模拟盘与实盘均查询失败（${envState.sim.error?.message ?? "unknown"}）。请检查网关容器状态后刷新。`} style={{ marginBottom: 20 }} /> : null}
      <div className="dashboard-stack">
        <Row gutter={[16, 16]}>
          {summaryCards.map(([label, value]) => <Col key={label} xs={24} sm={12} lg={Math.floor(24 / 5)}><Card className="dashboard-stat-card"><Statistic title={label} value={formatMoney(value)} /></Card></Col>)}
          {summaryCards.length === 0 ? <Col span={24}><Card className="dashboard-card"><Alert type={current.error ? "error" : "info"} showIcon message={current.error?.message ?? "加载中…"} /></Card></Col> : null}
        </Row>
        <Card className="dashboard-card" title={<div className="dashboard-card-header"><span>资产曲线 · {environmentLabel(environment)}</span><Typography.Text type="secondary">{curveReadout}</Typography.Text></div>}>
          {lineData.length >= 2 ? <LineChart ariaLabel="账户资产曲线（总资产历史快照）" data={lineData} /> : <div className="dashboard-chart-empty">暂无历史快照；可运行 <code>wbot ingest account</code> 开始记录（支持 -every 定时）。</div>}
        </Card>
        <Card className="dashboard-card" title="子账户明细">
          <DataTable className="table-card" columns={accountColumns} data={accountRows} rowKey="key" scrollX={900} />
        </Card>
        <Card className="dashboard-card" title={`${environmentLabel(environment)}交易与数据审计`}>
          <Typography.Paragraph type="secondary">挂单列表为只读，来源 `/v1/futu/orders`；页面不会创建、修改或撤销订单。</Typography.Paragraph>
          <DashboardTable account={current.account} orders={selectedOrders} runs={runs.data} accountError={current.error} ordersError={orders.error} runsError={runs.error} loading={current.loading || orders.loading || runs.loading} />
        </Card>
      </div>
    </>
  );
}

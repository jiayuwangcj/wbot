import { useCallback, useEffect, useRef, useState } from "react";
import type { Key, ReactNode } from "react";
import { Alert, Button, Card, Drawer, Empty, Form, Input, Select, Space, Table, Tag, Typography } from "antd";
import type { InputRef, TableColumnsType } from "antd";
import { DeleteOutlined, EditOutlined, ExperimentOutlined, PlusOutlined, ReloadOutlined, SearchOutlined } from "@ant-design/icons";
import {
  deleteWatchlist,
  getWheelConfigs,
  getWheelSignalActions,
  getWheelSignals,
  getWatchlist,
  postBacktest,
  saveWatchlist,
} from "../../api";
import type { SignalAction, WatchlistItem, WheelCandidate, WheelConfigVersion, WheelParams, WheelSignal } from "../../api/types";
import { DataTable } from "../../components/DataTable";
import { WheelForm } from "../../components/WheelForm";
import { useAsyncData } from "../../hooks/useAsyncData";
import { useAutoRefresh } from "../../hooks/useAutoRefresh";
import { formatClock, formatNumber, formatTime } from "../../lib/format";
import "./watchlist.css";

interface SignalFilter {
  symbol: string;
  action: string;
  capability: string;
}

interface ConfigFilter {
  symbol: string;
}

interface ActionState {
  signal: WheelSignal;
  actions: SignalAction[] | null;
  loading: boolean;
  error: Error | null;
}

const EMPTY_SIGNAL_FILTER: SignalFilter = { symbol: "", action: "", capability: "" };
const EMPTY_CONFIG_FILTER: ConfigFilter = { symbol: "" };

function toError(value: unknown): Error {
  return value instanceof Error ? value : new Error("cannot reach the server");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function numberValue(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function formatJson(value: unknown, spacing?: number): string {
  return JSON.stringify(value, null, spacing) ?? "null";
}

function statusColor(status: string): string {
  if (status === "READY") return "green";
  if (status === "DATA_BLOCKED") return "orange";
  if (status === "ALERT") return "blue";
  if (status === "HOLD") return "gold";
  return "default";
}

function inventorySummary(inventory: WheelSignal["inventory"]): string {
  return [inventory.actual_inventory, inventory.effective_inventory, inventory.target_inventory]
    .map((value) => formatNumber(value))
    .join(" / ");
}

function candidateSummary(candidate: WheelCandidate): string {
  const parts: string[] = [];
  if (candidate.direction) parts.push(candidate.direction);
  if (candidate.quantity !== undefined) parts.push(`${candidate.quantity} 张`);
  if (candidate.quality !== undefined && Number.isFinite(candidate.quality)) parts.push(`质量 ${Math.round(candidate.quality * 100)}%`);
  parts.push(candidate.accepted ? "接受" : "拒绝");
  const quote = candidate.quote ?? {};
  const code = stringValue(quote.code) ?? stringValue(quote.symbol);
  const expiry = stringValue(quote.expiry);
  const strike = numberValue(quote.strike);
  const delta = numberValue(quote.delta);
  const bid = numberValue(quote.bid);
  const ask = numberValue(quote.ask);
  const iv = numberValue(quote.iv);
  if (code) parts.push(code);
  if (strike !== null) parts.push(`strike ${formatNumber(strike)}`);
  if (expiry) parts.push(expiry.slice(0, 10));
  if (delta !== null) parts.push(`Δ ${delta.toFixed(2)}`);
  if (bid !== null && ask !== null) parts.push(`bid/ask ${formatNumber(bid)}/${formatNumber(ask)}`);
  if (iv !== null) parts.push(`IV ${Math.round(iv * 100)}%`);
  const reasons = candidate.reasons ?? [];
  if (reasons.length > 0) parts.push(`(${reasons.join("、")})`);
  return `候选: ${parts.join(" · ")}`;
}

function SignalDetail({ signal }: { signal: WheelSignal }): ReactNode {
  const inventory = signal.inventory;
  return (
    <div className="watchlist-detail">
      <Typography.Paragraph>
        现价 {formatNumber(inventory.current_price)} · 实际 {formatNumber(inventory.actual_inventory)} · 期权Δ {formatNumber(inventory.option_delta_stock)} · 有效 {formatNumber(inventory.effective_inventory)} · 目标 {formatNumber(inventory.target_inventory)} · 缺口 {formatNumber(inventory.inventory_gap)}
      </Typography.Paragraph>
      {signal.blocked_by.length > 0 ? <Typography.Paragraph>阻塞依赖: {signal.blocked_by.join("、")}</Typography.Paragraph> : null}
      {signal.rejection_reasons.length > 0 ? <Typography.Paragraph>拒绝原因: {signal.rejection_reasons.join("；")}</Typography.Paragraph> : null}
      {signal.candidates.map((candidate, index) => <Typography.Paragraph key={`${signal.id}-candidate-${index}`}>{candidateSummary(candidate)}</Typography.Paragraph>)}
      {signal.reason ? <Typography.Paragraph>原因: {signal.reason}</Typography.Paragraph> : null}
    </div>
  );
}

function configParams(config: Record<string, unknown>): Record<string, unknown> {
  const params = config.params;
  return isRecord(params) ? params : config;
}

function configSummary(config: WheelConfigVersion): string {
  const params = configParams(config.config);
  const curve = params.price_position_curve;
  const anchors = Array.isArray(curve) ? curve.length : "?";
  const maxInventory = numberValue(params.max_inventory);
  return `wheel · 曲线 ${anchors} 锚点 · 最大库存 ${maxInventory === null ? "?" : formatNumber(maxInventory)}`;
}

function configState(config: WheelConfigVersion): string {
  const params = configParams(config.config);
  return stringValue(params.strategic_state) ?? stringValue(config.config.strategic_state) ?? "—";
}

function ConfigDetail({ config }: { config: WheelConfigVersion }): ReactNode {
  return (
    <div className="watchlist-detail watchlist-config-detail">
      <pre>config: {formatJson(config.config, 2)}</pre>
      <pre>state: {formatJson(config.state, 2)}</pre>
    </div>
  );
}

function parseSignalHash(hash: string): number | null {
  const match = /^#signal-(\d+)$/.exec(hash);
  const value = match?.[1];
  if (value === undefined) return null;
  const id = Number(value);
  return Number.isSafeInteger(id) ? id : null;
}

function parseConfigHash(hash: string): string | null {
  const match = /^#config-(.+)-v(\d+)$/.exec(hash);
  const symbol = match?.[1];
  const version = match?.[2];
  if (symbol === undefined || version === undefined || !Number.isSafeInteger(Number(version))) return null;
  return `${symbol}#${Number(version)}`;
}

function ErrorAlert({ error }: { error: Error | null }): ReactNode {
  return error ? <Alert className="watchlist-error" showIcon type="error" message={error.message} /> : null;
}

export function WatchlistPage(): ReactNode {
  const watchlist = useAsyncData<WatchlistItem[]>(getWatchlist, []);
  const [signalDraft, setSignalDraft] = useState<SignalFilter>(EMPTY_SIGNAL_FILTER);
  const [signalFilter, setSignalFilter] = useState<SignalFilter>(EMPTY_SIGNAL_FILTER);
  const [configDraft, setConfigDraft] = useState<ConfigFilter>(EMPTY_CONFIG_FILTER);
  const [configFilter, setConfigFilter] = useState<ConfigFilter>(EMPTY_CONFIG_FILTER);
  const signals = useAsyncData<WheelSignal[]>(() => {
    const query: { symbol?: string; action?: string; capability?: string; limit: number } = { limit: 50 };
    if (signalFilter.symbol) query.symbol = signalFilter.symbol;
    if (signalFilter.action) query.action = signalFilter.action;
    if (signalFilter.capability) query.capability = signalFilter.capability;
    return getWheelSignals(query);
  }, [signalFilter]);
  const configs = useAsyncData<WheelConfigVersion[]>(() => getWheelConfigs(configFilter.symbol || undefined, 50), [configFilter]);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [expandedSignalKeys, setExpandedSignalKeys] = useState<readonly Key[]>([]);
  const [expandedConfigKeys, setExpandedConfigKeys] = useState<readonly Key[]>([]);
  const [actionState, setActionState] = useState<ActionState | null>(null);
  const actionRequestRef = useRef(0);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingItem, setEditingItem] = useState<WatchlistItem | null>(null);
  const [editorSymbol, setEditorSymbol] = useState("");
  const [formRevision, setFormRevision] = useState(0);
  const [saving, setSaving] = useState(false);
  const [symbolError, setSymbolError] = useState<string | null>(null);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [backtestLoading, setBacktestLoading] = useState<string | null>(null);
  const [listActionError, setListActionError] = useState<Error | null>(null);
  const symbolInputRef = useRef<InputRef>(null);
  const lastAppliedDeepLinkHashRef = useRef<string | null>(null);

  const refreshAll = useCallback(async (): Promise<void> => {
    await Promise.all([watchlist.refresh(), signals.refresh(), configs.refresh()]);
    setUpdatedAt(formatClock());
  }, [configs.refresh, signals.refresh, watchlist.refresh]);

  useAutoRefresh(refreshAll);

  useEffect(() => {
    if (watchlist.data !== null || signals.data !== null || configs.data !== null) setUpdatedAt(formatClock());
  }, [configs.data, signals.data, watchlist.data]);

  const applyDeepLinks = useCallback((): void => {
    const hash = window.location.hash;
    if (lastAppliedDeepLinkHashRef.current === hash) return;
    lastAppliedDeepLinkHashRef.current = hash;
    const signalId = parseSignalHash(window.location.hash);
    const configKey = parseConfigHash(window.location.hash);
    setExpandedSignalKeys(signalId === null ? [] : [signalId]);
    setExpandedConfigKeys(configKey === null ? [] : [configKey]);
  }, []);

  useEffect(() => {
    applyDeepLinks();
    window.addEventListener("hashchange", applyDeepLinks);
    return () => window.removeEventListener("hashchange", applyDeepLinks);
  }, [applyDeepLinks]);

  useEffect(() => {
    if (!drawerOpen) return undefined;
    const form = document.getElementById("watchlist-wheel-form");
    if (!form) return undefined;
    const guardSymbol = (event: Event): void => {
      if (editorSymbol.trim()) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      setSymbolError("symbol is required");
    };
    form.addEventListener("submit", guardSymbol, true);
    return () => form.removeEventListener("submit", guardSymbol, true);
  }, [drawerOpen, editorSymbol, formRevision]);

  const scrollToEditor = useCallback((): void => {
    const editor = document.getElementById("editor");
    if (editor && typeof editor.scrollIntoView === "function") editor.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  const openCreate = useCallback((): void => {
    setEditingItem(null);
    setEditorSymbol("");
    setSymbolError(null);
    setSaveMessage(null);
    setFormRevision((value) => value + 1);
    setDrawerOpen(true);
  }, []);

  const beginEdit = useCallback((item: WatchlistItem): void => {
    setEditingItem(item);
    setEditorSymbol(item.symbol);
    setSymbolError(null);
    setSaveMessage(null);
    setFormRevision((value) => value + 1);
    setDrawerOpen(true);
    scrollToEditor();
  }, [scrollToEditor]);

  const closeEditor = useCallback((): void => {
    setDrawerOpen(false);
    setEditingItem(null);
    setEditorSymbol("");
    setSymbolError(null);
    setSaveMessage(null);
  }, []);

  const handleSave = useCallback(async (params: WheelParams): Promise<void> => {
    const symbol = editorSymbol.trim();
    if (!symbol) {
      setSymbolError("symbol is required");
      throw new Error("symbol is required");
    }
    setSymbolError(null);
    setSaving(true);
    setSaveMessage(null);
    try {
      const bodyParams: Record<string, unknown> = { ...params };
      await saveWatchlist(symbol, { strategy: "wheel", params: bodyParams });
      setSaveMessage(`已保存 ${symbol}(wheel)。`);
      setEditingItem(null);
      setEditorSymbol("");
      setFormRevision((value) => value + 1);
      symbolInputRef.current?.focus();
      await watchlist.refresh();
    } finally {
      setSaving(false);
    }
  }, [editorSymbol, watchlist.refresh]);

  const handleDelete = useCallback(async (item: WatchlistItem): Promise<void> => {
    if (!window.confirm(`从观察列表移除 ${item.symbol}?`)) return;
    try {
      await deleteWatchlist(item.symbol);
      await watchlist.refresh();
    } catch (caught: unknown) {
      setListActionError(toError(caught));
    }
  }, [watchlist.refresh]);

  const handleBacktest = useCallback(async (item: WatchlistItem): Promise<void> => {
    setListActionError(null);
    setBacktestLoading(item.symbol);
    try {
      const result = await postBacktest({ symbol: item.symbol, strategy: "wheel", params: item.params });
      if (!("id" in result)) throw new Error("回测响应缺少 id");
      window.location.href = `/ui/results.html#bt-${result.id}`;
    } catch (caught: unknown) {
      setListActionError(toError(caught));
    } finally {
      setBacktestLoading(null);
    }
  }, []);

  const loadActions = useCallback(async (signal: WheelSignal): Promise<void> => {
    const request = actionRequestRef.current + 1;
    actionRequestRef.current = request;
    setActionState({ signal, actions: null, loading: true, error: null });
    try {
      const actions = await getWheelSignalActions(signal.id);
      if (request === actionRequestRef.current) setActionState({ signal, actions, loading: false, error: null });
    } catch (caught: unknown) {
      if (request === actionRequestRef.current) setActionState({ signal, actions: null, loading: false, error: toError(caught) });
    }
  }, []);

  const jumpToConfigVersion = useCallback((signal: WheelSignal): void => {
    setConfigDraft({ symbol: signal.symbol });
    setConfigFilter({ symbol: signal.symbol });
    const section = document.getElementById("wheel-configs");
    if (section && typeof section.scrollIntoView === "function") section.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  const toggleSignal = useCallback((id: number): void => {
    setExpandedSignalKeys((current) => current.includes(id) ? current.filter((key) => key !== id) : [...current, id]);
  }, []);

  const toggleConfig = useCallback((key: string): void => {
    setExpandedConfigKeys((current) => current.includes(key) ? current.filter((value) => value !== key) : [...current, key]);
  }, []);

  const watchlistColumns: TableColumnsType<WatchlistItem> = [
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: (left, right) => left.symbol.localeCompare(right.symbol) },
    { title: "策略", dataIndex: "strategy", key: "strategy", sorter: (left, right) => left.strategy.localeCompare(right.strategy) },
    { title: "配置版本", dataIndex: "config_version", key: "config_version", render: (_value: unknown, item) => item.config_version === null ? "—" : `v${item.config_version}` },
    {
      title: "能力状态 · 原因",
      key: "execution_status",
      render: (_value: unknown, item) => {
        const status = item.execution_status || "UNKNOWN";
        const reason = item.invalidation_reason || "未登记原因";
        return <Tag title={reason} color={statusColor(status)}>{status} · {reason}</Tag>;
      },
    },
    { title: "参数", dataIndex: "params", key: "params", render: (_value: unknown, item) => <code className="watchlist-json">{formatJson(item.params)}</code> },
    { title: "更新时间", dataIndex: "updated_at", key: "updated_at", sorter: (left, right) => left.updated_at.localeCompare(right.updated_at), defaultSortOrder: "descend", render: (value: string) => formatTime(value) },
    {
      title: "操作",
      key: "actions",
      render: (_value: unknown, item) => (
        <Space size="small" wrap>
          <Button icon={<EditOutlined />} onClick={() => beginEdit(item)} size="small" type="link">编辑</Button>
          <Button disabled={backtestLoading !== null} icon={<ExperimentOutlined />} loading={backtestLoading === item.symbol} onClick={() => void handleBacktest(item)} size="small" type="link">回测</Button>
          <Button danger icon={<DeleteOutlined />} onClick={() => void handleDelete(item)} size="small" type="link">删除</Button>
        </Space>
      ),
    },
  ];

  const signalColumns: TableColumnsType<WheelSignal> = [
    { title: "时间", dataIndex: "created_at", key: "created_at", sorter: (left, right) => left.created_at.localeCompare(right.created_at), defaultSortOrder: "descend", render: (value: string) => formatTime(value) },
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: (left, right) => left.symbol.localeCompare(right.symbol) },
    { title: "动作", dataIndex: "action", key: "action", sorter: (left, right) => left.action.localeCompare(right.action), render: (value: string) => <Tag color={statusColor(value)}>{value}</Tag> },
    {
      title: "能力状态 · 阻塞依赖",
      key: "capability_status",
      sorter: (left, right) => left.capability_status.localeCompare(right.capability_status),
      render: (_value: unknown, signal) => <div className="watchlist-capability"><Tag color={statusColor(signal.capability_status || "UNKNOWN")}>{signal.capability_status || "UNKNOWN"}</Tag>{signal.blocked_by.length > 0 ? <Typography.Text type="secondary">· {signal.blocked_by.join(", ")}</Typography.Text> : null}</div>,
    },
    {
      title: "配置",
      dataIndex: "config_version",
      key: "config_version",
      sorter: (left, right) => left.config_version - right.config_version,
      render: (value: number, signal) => <Button onClick={() => jumpToConfigVersion(signal)} size="small" title="查看该版本配置" type="link">v{value}</Button>,
    },
    { title: "实际 / 有效 / 目标", key: "inventory", className: "number-cell", sorter: (left, right) => (left.inventory.effective_inventory ?? Number.NEGATIVE_INFINITY) - (right.inventory.effective_inventory ?? Number.NEGATIVE_INFINITY), render: (_value: unknown, signal) => <span className="watchlist-inventory">{inventorySummary(signal.inventory)}</span> },
    { title: "原因", dataIndex: "reason", key: "reason", render: (value: string) => value || "—" },
    {
      title: "审计",
      key: "audit",
      render: (_value: unknown, signal) => (
        <Space size="small" wrap>
          <Button onClick={() => toggleSignal(signal.id)} size="small" type="link">{expandedSignalKeys.includes(signal.id) ? "收起" : "详情"}</Button>
          <Button onClick={() => void loadActions(signal)} size="small" type="link">人工记录</Button>
        </Space>
      ),
    },
  ];

  const configColumns: TableColumnsType<WheelConfigVersion> = [
    { title: "代码", dataIndex: "symbol", key: "symbol", sorter: (left, right) => left.symbol.localeCompare(right.symbol) },
    { title: "版本", dataIndex: "version", key: "version", className: "number-cell", sorter: (left, right) => left.version - right.version, render: (value: number) => `v${value}` },
    { title: "保存时间", dataIndex: "created_at", key: "created_at", sorter: (left, right) => left.created_at.localeCompare(right.created_at), defaultSortOrder: "descend", render: (value: string) => formatTime(value) },
    { title: "配置摘要", key: "summary", render: (_value: unknown, config) => configSummary(config) },
    { title: "战略状态", key: "strategic_state", render: (_value: unknown, config) => configState(config) },
    {
      title: "详情",
      key: "detail",
      render: (_value: unknown, config) => {
        const key = `${config.symbol}#${config.version}`;
        return <Button onClick={() => toggleConfig(key)} size="small" type="link">{expandedConfigKeys.includes(key) ? "收起" : "详情"}</Button>;
      },
    },
  ];

  const handleManualRefresh = async (): Promise<void> => {
    setRefreshing(true);
    try {
      await refreshAll();
    } finally {
      setRefreshing(false);
    }
  };

  const listItems = watchlist.data ?? [];
  const signalItems = signals.data ?? [];
  const configItems = configs.data ?? [];
  const wheelForm = editingItem ? (
    <WheelForm key={`edit-${formRevision}`} formId="watchlist-wheel-form" initialValues={editingItem.params} onSubmit={handleSave} submitLabel={saving ? "保存中…" : "保存"} />
  ) : (
    <WheelForm key={`new-${formRevision}`} formId="watchlist-wheel-form" onSubmit={handleSave} submitLabel={saving ? "保存中…" : "保存"} />
  );

  return (
    <div className="watchlist-page">
      <div className="page-heading watchlist-heading">
        <div>
          <Typography.Title level={1}>策略工作台</Typography.Title>
          <Typography.Paragraph>以信号审计为主视觉，配置、观察列表与人工处置记录保持可追溯。</Typography.Paragraph>
        </div>
        <div className="watchlist-heading-actions">
          <Typography.Text type="secondary">{updatedAt ? `更新于 ${updatedAt}` : "等待首次刷新"}</Typography.Text>
          <Button icon={<ReloadOutlined />} loading={refreshing} onClick={() => void handleManualRefresh()}>{refreshing ? "刷新中…" : "刷新"}</Button>
        </div>
      </div>

      <Card className="watchlist-strategy-card" onClick={() => { scrollToEditor(); openCreate(); }} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); scrollToEditor(); openCreate(); } }} role="button" tabIndex={0}>
        <div className="watchlist-strategy-card-heading">
          <div>
            <Typography.Title level={3}>动态 Wheel · 仅提醒</Typography.Title>
            <Typography.Paragraph>按价格—目标库存曲线管理仓位缺口；策略只扫描 Put/Call 并记录 ALERT 或 HOLD，不调用交易 API。</Typography.Paragraph>
          </div>
          <Tag color="blue">wheel</Tag>
        </div>
        <dl className="watchlist-strategy-facts">
          <div><dt>状态</dt><dd>NORMAL / CAUTION / PAUSE_BUY / EXIT</dd></div>
          <div><dt>安全边界</dt><dd>报价不完整或过期时保持 HOLD</dd></div>
        </dl>
        <Typography.Text type="secondary">点击策略卡片打开 Wheel 编辑器</Typography.Text>
      </Card>

      <section className="watchlist-section watchlist-primary-section" id="wheel-audit">
        <div className="watchlist-section-heading">
          <div>
            <Typography.Title level={2}>最近 Wheel 信号</Typography.Title>
            <Typography.Paragraph type="secondary">只读审计：区分策略风控 HOLD 与 DATA_BLOCKED HOLD；人工动作只展示审计记录，不会触发下单。</Typography.Paragraph>
          </div>
          <Tag>只读审计</Tag>
        </div>
        <Card className="watchlist-audit-card">
          <Form className="watchlist-filter" layout="inline" onFinish={() => setSignalFilter({ ...signalDraft, symbol: signalDraft.symbol.trim() })}>
            <Form.Item label="代码">
              <Input placeholder="留空查看全部" value={signalDraft.symbol} onChange={(event) => setSignalDraft((current) => ({ ...current, symbol: event.target.value }))} />
            </Form.Item>
            <Form.Item label="动作">
              <Select<string> allowClear placeholder="全部" value={signalDraft.action || null} onChange={(value) => setSignalDraft((current) => ({ ...current, action: value ?? "" }))} options={[{ label: "ALERT", value: "ALERT" }, { label: "HOLD", value: "HOLD" }]} />
            </Form.Item>
            <Form.Item label="能力状态">
              <Select<string> allowClear placeholder="全部" value={signalDraft.capability || null} onChange={(value) => setSignalDraft((current) => ({ ...current, capability: value ?? "" }))} options={[{ label: "READY · 可提醒", value: "READY" }, { label: "DATA_BLOCKED · 数据阻塞", value: "DATA_BLOCKED" }]} />
            </Form.Item>
            <Button htmlType="submit" icon={<SearchOutlined />} type="primary">刷新信号</Button>
          </Form>
          <ErrorAlert error={signals.error} />
          <Table<WheelSignal>
            className="watchlist-table-card watchlist-signal-table"
            columns={signalColumns}
            dataSource={signalItems}
            expandable={{
              expandedRowKeys: expandedSignalKeys,
              expandedRowRender: (signal) => <SignalDetail signal={signal} />,
              onExpandedRowsChange: (keys) => setExpandedSignalKeys(keys),
              showExpandColumn: false,
            }}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚无 Wheel 信号记录；实时供应商未解锁时这是正常状态。" /> }}
            loading={signals.loading}
            pagination={false}
            rowKey="id"
            scroll={{ x: 1280 }}
            size="middle"
          />
          {actionState ? <Card className="watchlist-actions-card" size="small" title={`${actionState.signal.symbol} / signal #${actionState.signal.id} · 人工处置记录`}>
            {actionState.loading ? <Typography.Text type="secondary">加载中…</Typography.Text> : actionState.error ? <Alert showIcon type="error" message={actionState.error.message} /> : actionState.actions?.length === 0 ? <Typography.Paragraph>{actionState.signal.symbol} / signal #{actionState.signal.id}：尚无人工处置记录。</Typography.Paragraph> : <Typography.Paragraph>{actionState.signal.symbol} / signal #{actionState.signal.id}：{actionState.actions?.map((action) => `${formatTime(action.created_at)} ${action.action} by ${action.actor}${action.note ? ` · ${action.note}` : ""}`).join("；")}</Typography.Paragraph>}
          </Card> : null}
        </Card>
      </section>

      <section className="watchlist-section" id="list">
        <div className="watchlist-section-heading">
          <div>
            <Typography.Title level={2}>观察列表 {listItems.length > 0 ? <Typography.Text className="watchlist-count">{listItems.length} 个标的</Typography.Text> : null}</Typography.Title>
            <Typography.Paragraph type="secondary">已绑定 Wheel 策略的标的与能力状态。</Typography.Paragraph>
          </div>
          <Button icon={<PlusOutlined />} onClick={() => { scrollToEditor(); openCreate(); }} type="primary">新增观察标的</Button>
        </div>
        <ErrorAlert error={watchlist.error} />
        {listActionError ? <Alert className="watchlist-error" showIcon type="error" message={listActionError.message} /> : null}
        <DataTable className="watchlist-table-card" columns={watchlistColumns} data={listItems} emptyText="观察列表暂无标的。" loading={watchlist.loading} rowKey="symbol" scrollX={1250} />
      </section>

      <section className="watchlist-section" id="wheel-configs">
        <div className="watchlist-section-heading">
          <div>
            <Typography.Title level={2}>Wheel 配置版本</Typography.Title>
            <Typography.Paragraph type="secondary">配置保存后即生成不可变版本，信号通过 config_version 引用当时的配置；这里可查每个版本保存时的完整配置与战略状态。</Typography.Paragraph>
          </div>
          <Tag>只读审计</Tag>
        </div>
        <Card className="watchlist-audit-card">
          <Form className="watchlist-filter" layout="inline" onFinish={() => setConfigFilter({ symbol: configDraft.symbol.trim() })}>
            <Form.Item label="代码"><Input placeholder="留空查看全部" value={configDraft.symbol} onChange={(event) => setConfigDraft({ symbol: event.target.value })} /></Form.Item>
            <Button htmlType="submit" icon={<SearchOutlined />}>刷新配置</Button>
          </Form>
          <ErrorAlert error={configs.error} />
          <Table<WheelConfigVersion>
            className="watchlist-table-card watchlist-config-table"
            columns={configColumns}
            dataSource={configItems}
            expandable={{
              expandedRowKeys: expandedConfigKeys,
              expandedRowRender: (config) => <ConfigDetail config={config} />,
              onExpandedRowsChange: (keys) => setExpandedConfigKeys(keys),
              showExpandColumn: false,
            }}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无配置版本记录。" /> }}
            loading={configs.loading}
            pagination={false}
            rowKey={(config) => `${config.symbol}#${config.version}`}
            scroll={{ x: 920 }}
            size="middle"
          />
        </Card>
      </section>

      <section className="watchlist-section" id="editor">
        <Card className="watchlist-editor-trigger">
          <div>
            <Typography.Title level={2}>编辑观察标的</Typography.Title>
            <Typography.Paragraph type="secondary">Wheel 参数收纳在 Drawer 中，保存后生成新的不可变配置版本。</Typography.Paragraph>
          </div>
          <Button icon={<PlusOutlined />} onClick={openCreate} type="primary">打开编辑器</Button>
        </Card>
      </section>

      <Drawer className="watchlist-editor-drawer" destroyOnClose onClose={closeEditor} open={drawerOpen} styles={{ wrapper: { maxWidth: "100vw" } }} title={editingItem ? `编辑 ${editingItem.symbol}` : "添加观察标的"} width={640}>
        <div className="watchlist-drawer-content">
          <Typography.Paragraph type="secondary">策略类型固定为 wheel，仅生成提醒与审计记录。</Typography.Paragraph>
          <label className="watchlist-symbol-label" htmlFor="watchlist-symbol">代码</label>
          <Input id="watchlist-symbol" name="symbol" placeholder="HK.00700" ref={symbolInputRef} value={editorSymbol} onChange={(event) => { setEditorSymbol(event.target.value); setSymbolError(null); setSaveMessage(null); }} />
          <input aria-hidden="true" name="strategy" readOnly type="hidden" value="wheel" />
          {symbolError ? <Alert className="watchlist-error" message={symbolError} showIcon type="error" /> : null}
          {saveMessage ? <Alert className="watchlist-save-message" message={saveMessage} showIcon type="success" /> : null}
          {wheelForm}
        </div>
      </Drawer>
    </div>
  );
}

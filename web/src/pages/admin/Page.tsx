import { useCallback, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button, Card, Col, Descriptions, Form, Input, Row, Select, Space, Steps, Tabs, Tag, Typography } from "antd";
import type { TableColumnsType } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { getAdminCluster, getAdminConfig, putAdminConfig } from "../../api";
import type { AdminConfigEntry, ClusterResponse } from "../../api/types";
import { DataTable } from "../../components/DataTable";
import { useAutoRefresh } from "../../hooks/useAutoRefresh";
import { formatClock, formatTime } from "../../lib/format";

type BadgeTone = "ok" | "warn" | "down" | "idle";

const BADGE_COLOR: Record<BadgeTone, string> = { ok: "green", warn: "orange", down: "red", idle: "default" };

const TELEGRAM_KEYS = ["credentials.telegram.token", "credentials.telegram.chat_ids"] as const;

function toError(caught: unknown): Error {
  return caught instanceof Error ? caught : new Error("unexpected server response");
}

export function isSecretKey(key: string): boolean {
  return key.startsWith("credentials.");
}

function StatusBadge({ tone, children }: { tone: BadgeTone; children: ReactNode }): ReactNode {
  return <Tag color={BADGE_COLOR[tone]}>{children}</Tag>;
}

function ClusterCard({ title, badge, items }: { title: string; badge: ReactNode; items: Array<[string, string]> }): ReactNode {
  return (
    <Col xs={24} sm={12} xl={6}>
      <Card className="dashboard-card" title={<Space size={8}>{badge}{title}</Space>}>
        <Descriptions column={1} size="small">
          {items.map(([label, value]) => <Descriptions.Item key={label} label={label}>{value}</Descriptions.Item>)}
        </Descriptions>
      </Card>
    </Col>
  );
}

function ClusterCards({ cluster }: { cluster: ClusterResponse }): ReactNode {
  const { process, db, pipeline, data_plane: dataPlane } = cluster.components;
  const counts = pipeline.counts;
  const coverage = dataPlane.bars_coverage;
  const stale = coverage.filter((bar) => bar.fresh === "stale").length;
  const unknown = coverage.filter((bar) => bar.fresh === "unknown").length;
  let newest = "";
  for (const bar of coverage) {
    if (bar.max_ts && bar.max_ts > newest) newest = bar.max_ts;
  }
  const [pipelineTone, pipelineLabel]: [BadgeTone, string] = counts.failed > 0 ? ["warn", "有失败"] : counts.running > 0 ? ["ok", "进行中"] : counts.succeeded > 0 ? ["ok", "正常"] : ["idle", "空闲"];
  const [dataTone, dataLabel]: [BadgeTone, string] = stale > 0 ? ["warn", "部分过期"] : coverage.length === 0 ? ["idle", "无数据"] : ["ok", "正常"];
  const latency = typeof db.latency_ms === "number" ? `${db.latency_ms} ms` : "n/a";
  const staleText = stale + (unknown > 0 ? ` (+${unknown} 无数据)` : "");
  const newestText = newest === "" ? "无数据" : newest.slice(0, 16);
  return (
    <Row gutter={[16, 16]}>
      <ClusterCard title="进程" badge={<StatusBadge tone="ok">运行中</StatusBadge>} items={[
        ["版本", process.version],
        ["PID", String(process.pid)],
        ["运行时长", `${Math.round(process.uptime_seconds)} s`],
        ["监听地址", process.listen_addr],
      ]} />
      <ClusterCard title="数据库" badge={db.ok ? <StatusBadge tone="ok">正常</StatusBadge> : <StatusBadge tone="down">故障</StatusBadge>} items={[
        ["状态", db.ok ? "ok" : "down"],
        ["延迟", latency],
      ]} />
      <ClusterCard title="数据管道" badge={<StatusBadge tone={pipelineTone}>{pipelineLabel}</StatusBadge>} items={[
        ["运行中", String(counts.running)],
        ["成功", String(counts.succeeded)],
        ["失败", String(counts.failed)],
      ]} />
      <ClusterCard title="数据平面" badge={<StatusBadge tone={dataTone}>{dataLabel}</StatusBadge>} items={[
        ["缓存序列", String(coverage.length)],
        ["过期序列", staleText],
        ["最新 K 线", newestText],
      ]} />
    </Row>
  );
}

function ConfigPanel({ config, loading, error, onSaved }: {
  config: AdminConfigEntry[] | null;
  loading: boolean;
  error: Error | null;
  onSaved: () => Promise<boolean>;
}): ReactNode {
  const keys = config ?? [];
  const [selectedKey, setSelectedKey] = useState("");
  const [value, setValue] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<Error | null>(null);
  const [saveOk, setSaveOk] = useState(false);

  useEffect(() => {
    if (keys.length > 0 && !keys.some((entry) => entry.key === selectedKey)) {
      setSelectedKey(keys[0]?.key ?? "");
    }
  }, [keys, selectedKey]);

  const handleSubmit = async (): Promise<void> => {
    setSaveOk(false);
    if (!value.trim()) {
      setSaveError(new Error("值不能为空"));
      return;
    }
    setSaveError(null);
    setSaving(true);
    try {
      await putAdminConfig(selectedKey, value);
      setValue("");
      setSaveOk(true);
      await onSaved();
    } catch (caught) {
      setSaveError(toError(caught));
    } finally {
      setSaving(false);
    }
  };

  const columns: TableColumnsType<AdminConfigEntry> = [
    { title: "键", dataIndex: "key", key: "key" },
    { title: "分组", dataIndex: "group", key: "group" },
    { title: "已设置", dataIndex: "set", key: "set", render: (set: boolean) => set ? "是" : "否" },
    { title: "更新时间", dataIndex: "updated_at", key: "updated_at", render: (value: string | null) => value === null ? "未设置" : formatTime(value) },
  ];

  return (
    <>
      <Typography.Paragraph type="secondary">仅元数据:配置值只写不读——设置时输入,永不展示/回显(凭证、监听地址)。</Typography.Paragraph>
      {error ? <Alert type="error" showIcon message={error.message} style={{ marginBottom: 16 }} /> : null}
      {keys.length > 0 ? (
        <>
          <Form layout="inline" onFinish={() => void handleSubmit()} style={{ marginBottom: 16 }}>
            <Form.Item label="键">
              <Select style={{ minWidth: 320 }} value={selectedKey} onChange={setSelectedKey} options={keys.map((entry) => ({ value: entry.key, label: entry.key }))} />
            </Form.Item>
            <Form.Item label="值">
              <Input aria-label="配置值" autoComplete="off" placeholder="仅写入,不会回显" type={isSecretKey(selectedKey) ? "password" : "text"} value={value} onChange={(event) => setValue(event.target.value)} style={{ minWidth: 260 }} />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={saving}>{saving ? "保存中…" : "设置"}</Button>
            </Form.Item>
          </Form>
          {saveError ? <Alert type="error" showIcon message={saveError.message} style={{ marginBottom: 16 }} /> : null}
          {saveOk ? <Typography.Text type="success" style={{ display: "block", marginBottom: 16 }}>已保存。</Typography.Text> : null}
        </>
      ) : null}
      <DataTable className="table-card config-table" columns={columns} data={keys} rowKey="key" emptyText="无配置键。" loading={loading && config === null} />
    </>
  );
}

function telegramStatusText(entries: AdminConfigEntry[]): string {
  const byKey = new Map(entries.map((entry) => [entry.key, entry] as const));
  const token = byKey.get(TELEGRAM_KEYS[0]);
  const ids = byKey.get(TELEGRAM_KEYS[1]);
  if (token?.set && ids?.set) return "已配置:提醒将推送到白名单 chat_ids;重启 serve --telegram-run 生效。";
  if (token?.set) return "token 已配置,还差 chat_ids。";
  if (ids?.set) return "chat_ids 已配置,还差 token。";
  return "未配置:按上面三步填入 token 与 chat_ids。";
}

function TelegramKeyForm({ configKey, label, ariaLabel, placeholder, secret, buttonText, onSaved }: {
  configKey: string;
  label: string;
  ariaLabel: string;
  placeholder: string;
  secret: boolean;
  buttonText: string;
  onSaved: () => Promise<boolean>;
}): ReactNode {
  const [value, setValue] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [ok, setOk] = useState(false);

  const handleSubmit = async (): Promise<void> => {
    setOk(false);
    if (!value.trim()) {
      setError(new Error("值不能为空"));
      return;
    }
    setError(null);
    setSaving(true);
    try {
      await putAdminConfig(configKey, value);
      setValue("");
      setOk(true);
      await onSaved();
    } catch (caught) {
      setError(toError(caught));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <Form layout="inline" onFinish={() => void handleSubmit()}>
        <Form.Item label={label}>
          <Input aria-label={ariaLabel} autoComplete="off" placeholder={placeholder} type={secret ? "password" : "text"} value={value} onChange={(event) => setValue(event.target.value)} style={{ minWidth: 260 }} />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>{saving ? "保存中…" : buttonText}</Button>
        </Form.Item>
      </Form>
      {error ? <Alert type="error" showIcon message={error.message} style={{ marginBottom: 8 }} /> : null}
      {ok ? <Typography.Text type="success">已保存。</Typography.Text> : null}
    </div>
  );
}

function TelegramPanel({ config, onSaved }: {
  config: AdminConfigEntry[] | null;
  onSaved: () => Promise<boolean>;
}): ReactNode {
  const entries = config ?? [];
  const telegramRows = TELEGRAM_KEYS
    .map((key) => entries.find((entry) => entry.key === key))
    .filter((entry): entry is AdminConfigEntry => entry !== undefined);
  const columns: TableColumnsType<AdminConfigEntry> = [
    { title: "键", dataIndex: "key", key: "key" },
    { title: "已配置", dataIndex: "set", key: "set", render: (set: boolean) => set ? "是" : "否" },
    { title: "更新时间", dataIndex: "updated_at", key: "updated_at", render: (value: string | null) => value === null ? "未设置" : formatTime(value) },
  ];
  return (
    <>
      <Typography.Paragraph type="secondary">wheel 实时提醒:ALERT 信号推送 + 按钮处置(是,下单 / 否,等待机会 / 今日不再提醒)。token 与 chat_ids 落盘 <code>~/.wbot/wbot.conf</code>,只写不读——值不回显。</Typography.Paragraph>
      <Steps current={-1} size="small" direction="vertical" items={[
        { title: "创建机器人", description: <>在 <a href="https://t.me/BotFather" rel="noopener" target="_blank">@BotFather</a> 创建机器人,复制 token。</> },
        { title: "获取 chat id", description: <>给机器人发一条消息,把消息里的 chat id(如 <code>12345678</code>)填入下方 chat_ids。</> },
        { title: "重启生效", description: <>保存后重启 <code>serve --telegram-run</code> 生效。</> },
      ]} />
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={12}>
          <TelegramKeyForm configKey={TELEGRAM_KEYS[0]} label="Bot token" ariaLabel="Bot token" placeholder="仅写入,不会回显" secret buttonText="保存 token" onSaved={onSaved} />
        </Col>
        <Col xs={24} lg={12}>
          <TelegramKeyForm configKey={TELEGRAM_KEYS[1]} label="chat_ids(逗号分隔,可多个)" ariaLabel="chat_ids" placeholder="仅写入,不会回显" secret={false} buttonText="保存 chat_ids" onSaved={onSaved} />
        </Col>
      </Row>
      <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>{telegramStatusText(entries)}</Typography.Paragraph>
      <DataTable className="table-card telegram-table" columns={columns} data={telegramRows} rowKey="key" emptyText="无配置键。" />
    </>
  );
}

export function AdminPage(): ReactNode {
  const [cluster, setCluster] = useState<ClusterResponse | null>(null);
  const [config, setConfig] = useState<AdminConfigEntry[] | null>(null);
  const [clusterError, setClusterError] = useState<Error | null>(null);
  const [configError, setConfigError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);

  const loadCluster = useCallback(async (): Promise<boolean> => {
    try {
      setCluster(await getAdminCluster());
      setClusterError(null);
      return true;
    } catch (caught) {
      setClusterError(toError(caught));
      return false;
    }
  }, []);

  const loadConfig = useCallback(async (): Promise<boolean> => {
    try {
      setConfig(await getAdminConfig());
      setConfigError(null);
      return true;
    } catch (caught) {
      setConfigError(toError(caught));
      return false;
    }
  }, []);

  const loadAll = useCallback(async (): Promise<void> => {
    setLoading(true);
    const [clusterOk, configOk] = await Promise.all([loadCluster(), loadConfig()]);
    setLoading(false);
    if (clusterOk && configOk) setUpdatedAt(formatClock());
  }, [loadCluster, loadConfig]);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);
  useAutoRefresh(loadAll);

  const handleRefresh = async (): Promise<void> => {
    setRefreshing(true);
    try {
      await loadAll();
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <>
      <div className="page-heading">
        <div>
          <Typography.Title level={1}>集群管理</Typography.Title>
          <Typography.Paragraph>serve 进程 / PostgreSQL / 行情管道 / 数据平面 各节点总览;详细 bars 覆盖见「数据」页。</Typography.Paragraph>
        </div>
      </div>
      <div className="dashboard-toolbar">
        <Typography.Text type="secondary">{updatedAt ? `更新于 ${updatedAt}` : "等待首次刷新"}</Typography.Text>
        <Button icon={<ReloadOutlined />} loading={refreshing} onClick={() => void handleRefresh()}>{refreshing ? "刷新中…" : "刷新"}</Button>
      </div>
      {clusterError ? <Alert type="error" showIcon message={clusterError.message} style={{ marginBottom: 16 }} /> : null}
      {cluster ? <ClusterCards cluster={cluster} /> : null}
      <Card className="dashboard-card" style={{ marginTop: 20 }}>
        <Tabs items={[
          { key: "config", label: "配置", forceRender: true, children: <ConfigPanel config={config} error={configError} loading={loading} onSaved={loadConfig} /> },
          { key: "telegram", label: "Telegram 向导", forceRender: true, children: <TelegramPanel config={config} onSaved={loadConfig} /> },
        ]} />
      </Card>
    </>
  );
}

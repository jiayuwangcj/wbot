import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { formatTime } from "../../lib/format";
import { AdminPage, isSecretKey } from "./Page";
import type { AdminConfigEntry, ClusterResponse } from "../../api/types";

const apiMocks = vi.hoisted(() => ({
  getAdminCluster: vi.fn(),
  getAdminConfig: vi.fn(),
  putAdminConfig: vi.fn(),
}));

vi.mock("../../api", () => apiMocks);

const cluster: ClusterResponse = {
  components: {
    process: { version: "v0.9.0", pid: 1234, started_at: "2026-08-11T00:00:00Z", uptime_seconds: 3600.5, listen_addr: "127.0.0.1:8080" },
    db: { ok: true, latency_ms: 3.2 },
    pipeline: { counts: { running: 1, succeeded: 10, failed: 0 }, recent_runs: [] },
    data_plane: {
      bars_coverage: [
        { symbol: "SPY", timeframe: "1d", adjust: "fwd", count: 250, min_ts: "2025-08-11T00:00:00Z", max_ts: "2026-08-11T00:00:00Z", max_ts_age_seconds: 3600, fresh: "stale" },
        { symbol: "QQQ", timeframe: "1d", adjust: "fwd", count: 250, min_ts: "2025-08-11T00:00:00Z", max_ts: "2026-08-10T00:00:00Z", max_ts_age_seconds: 60, fresh: "fresh" },
      ],
      options_freshness: [],
    },
  },
};

const configKeys: AdminConfigEntry[] = [
  { key: "credentials.futu.token", group: "credentials", set: true, updated_at: "2026-08-11T01:02:03Z" },
  { key: "credentials.telegram.token", group: "credentials", set: true, updated_at: "2026-08-11T02:02:03Z" },
  { key: "credentials.telegram.chat_ids", group: "credentials", set: false, updated_at: null },
  { key: "server.listen", group: "server", set: false, updated_at: null },
];

beforeEach(() => {
  apiMocks.getAdminCluster.mockResolvedValue(cluster);
  apiMocks.getAdminConfig.mockResolvedValue(configKeys);
  apiMocks.putAdminConfig.mockResolvedValue({ key: "x", set: true });
});

describe("AdminPage", () => {
  it("renders the four cluster cards with status badges", async () => {
    render(<AdminPage />);

    const processCard = (await screen.findByText("v0.9.0")).closest(".ant-card") as HTMLElement;
    expect(within(processCard).getByText("运行中")).toBeInTheDocument();
    expect(within(processCard).getByText("1234")).toBeInTheDocument();
    expect(within(processCard).getByText("3601 s")).toBeInTheDocument();
    expect(within(processCard).getByText("127.0.0.1:8080")).toBeInTheDocument();

    const dbCard = screen.getByText("数据库").closest(".ant-card") as HTMLElement;
    expect(within(dbCard).getByText("正常")).toBeInTheDocument();
    expect(within(dbCard).getByText("ok")).toBeInTheDocument();
    expect(within(dbCard).getByText("3.2 ms")).toBeInTheDocument();

    const pipelineCard = screen.getByText("数据管道").closest(".ant-card") as HTMLElement;
    expect(within(pipelineCard).getByText("进行中")).toBeInTheDocument();
    expect(within(pipelineCard).getByText("10")).toBeInTheDocument();
    expect(within(pipelineCard).getByText("0")).toBeInTheDocument();

    const dataCard = screen.getByText("数据平面").closest(".ant-card") as HTMLElement;
    expect(within(dataCard).getByText("部分过期")).toBeInTheDocument();
    expect(within(dataCard).getByText("2")).toBeInTheDocument();
    expect(within(dataCard).getByText("2026-08-11T00:00")).toBeInTheDocument();
  });

  it("shows warn badges for failed pipelines and stale data with unknown suffix", async () => {
    apiMocks.getAdminCluster.mockResolvedValue({
      ...cluster,
      components: {
        ...cluster.components,
        pipeline: { counts: { running: 0, succeeded: 2, failed: 1 }, recent_runs: [] },
        data_plane: {
          bars_coverage: [
            { symbol: "SPY", timeframe: "1d", adjust: "fwd", count: 250, min_ts: "2025-08-11T00:00:00Z", max_ts: "2026-08-11T00:00:00Z", max_ts_age_seconds: 40000, fresh: "stale" },
            { symbol: "QQQ", timeframe: "1d", adjust: "fwd", count: 250, min_ts: "2025-08-11T00:00:00Z", max_ts: "2026-08-10T00:00:00Z", max_ts_age_seconds: 40000, fresh: "unknown" },
          ],
          options_freshness: [],
        },
      },
    });
    render(<AdminPage />);

    const pipelineCard = (await screen.findByText("数据管道")).closest(".ant-card") as HTMLElement;
    expect(within(pipelineCard).getByText("有失败")).toBeInTheDocument();

    const dataCard = screen.getByText("数据平面").closest(".ant-card") as HTMLElement;
    expect(within(dataCard).getByText("部分过期")).toBeInTheDocument();
    expect(within(dataCard).getByText("1 (+1 无数据)")).toBeInTheDocument();
  });

  it("shows the database card as down with n/a latency when the ping fails", async () => {
    apiMocks.getAdminCluster.mockResolvedValue({
      ...cluster,
      components: { ...cluster.components, db: { ok: false } },
    });
    render(<AdminPage />);

    const dbCard = (await screen.findByText("数据库")).closest(".ant-card") as HTMLElement;
    expect(within(dbCard).getByText("故障")).toBeInTheDocument();
    expect(within(dbCard).getByText("down")).toBeInTheDocument();
    expect(within(dbCard).getByText("n/a")).toBeInTheDocument();
  });

  it("renders the config metadata table and never echoes values", async () => {
    render(<AdminPage />);

    const configTable = (await screen.findByText("credentials.futu.token")).closest("table") as HTMLElement;
    expect(configTable.textContent).toContain("credentials");
    expect(configTable.textContent).toContain("是");
    expect(configTable.textContent).toContain(formatTime("2026-08-11T01:02:03Z"));
    expect(configTable.textContent).toContain("否");
    expect(configTable.textContent).toContain("未设置");
    expect(configTable.textContent).not.toContain("super-secret-value");

    const valueInput = screen.getByLabelText("配置值") as HTMLInputElement;
    expect(valueInput.value).toBe("");
    expect(document.body.textContent).not.toContain("super-secret-value");
  });

  it("writes a config value without echoing it back and clears the input", async () => {
    render(<AdminPage />);

    const input = (await screen.findByLabelText("配置值")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "secret-value-123" } });
    expect(input.value).toBe("secret-value-123");
    fireEvent.click(screen.getByRole("button", { name: /设\s*置/ }));

    expect(await screen.findByText("已保存。")).toBeInTheDocument();
    expect(apiMocks.putAdminConfig).toHaveBeenCalledWith("credentials.futu.token", "secret-value-123");
    expect((screen.getByLabelText("配置值") as HTMLInputElement).value).toBe("");
    expect(document.body.textContent).not.toContain("secret-value-123");
  });

  it("rejects empty config values without calling the API", async () => {
    render(<AdminPage />);

    await screen.findByLabelText("配置值");
    fireEvent.click(screen.getByRole("button", { name: /设\s*置/ }));

    expect(await screen.findByText("值不能为空")).toBeInTheDocument();
    expect(apiMocks.putAdminConfig).not.toHaveBeenCalled();
  });

  it("uses a password input for credentials keys and text for others", async () => {
    render(<AdminPage />);

    const input = (await screen.findByLabelText("配置值")) as HTMLInputElement;
    expect(input.type).toBe("password");
    expect(isSecretKey("credentials.futu.token")).toBe(true);
    expect(isSecretKey("server.listen")).toBe(false);
  });

  it("shows the telegram wizard steps with the BotFather link", async () => {
    render(<AdminPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Telegram 向导" }));

    expect(await screen.findByText("创建机器人")).toBeInTheDocument();
    expect(screen.getByText("获取 chat id")).toBeInTheDocument();
    expect(screen.getByText("重启生效")).toBeInTheDocument();
    const botFather = screen.getByRole("link", { name: "@BotFather" });
    expect(botFather).toHaveAttribute("href", "https://t.me/BotFather");
    expect(botFather).toHaveAttribute("rel", "noopener");
  });

  it("shows all four telegram wizard states from metadata", async () => {
    const cases: Array<[AdminConfigEntry[], string]> = [
      [[
        { key: "credentials.telegram.token", group: "credentials", set: true, updated_at: "2026-08-11T02:02:03Z" },
        { key: "credentials.telegram.chat_ids", group: "credentials", set: true, updated_at: "2026-08-11T03:02:03Z" },
      ], "已配置:提醒将推送到白名单 chat_ids;重启 serve --telegram-run 生效。"],
      [[{ key: "credentials.telegram.token", group: "credentials", set: true, updated_at: "2026-08-11T02:02:03Z" }], "token 已配置,还差 chat_ids。"],
      [[{ key: "credentials.telegram.chat_ids", group: "credentials", set: true, updated_at: "2026-08-11T03:02:03Z" }], "chat_ids 已配置,还差 token。"],
      [[], "未配置:按上面三步填入 token 与 chat_ids。"],
    ];
    for (const [entries, expected] of cases) {
      apiMocks.getAdminConfig.mockResolvedValue(entries);
      const { unmount } = render(<AdminPage />);
      expect(await screen.findByText(expected)).toBeInTheDocument();
      unmount();
    }
  });

  it("saves telegram token and chat_ids without echoing values", async () => {
    render(<AdminPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Telegram 向导" }));

    const tokenInput = (await screen.findByLabelText("Bot token")) as HTMLInputElement;
    expect(tokenInput.type).toBe("password");
    fireEvent.change(tokenInput, { target: { value: "tg-token-abc" } });
    fireEvent.click(screen.getByRole("button", { name: "保存 token" }));
    expect(await screen.findByText("已保存。")).toBeInTheDocument();
    expect(apiMocks.putAdminConfig).toHaveBeenCalledWith("credentials.telegram.token", "tg-token-abc");
    expect((screen.getByLabelText("Bot token") as HTMLInputElement).value).toBe("");
    expect(document.body.textContent).not.toContain("tg-token-abc");

    const idsInput = screen.getByLabelText("chat_ids") as HTMLInputElement;
    expect(idsInput.type).toBe("text");
    fireEvent.change(idsInput, { target: { value: "111,222" } });
    fireEvent.click(screen.getByRole("button", { name: "保存 chat_ids" }));
    expect(await screen.findByText("已保存。")).toBeInTheDocument();
    expect(apiMocks.putAdminConfig).toHaveBeenCalledWith("credentials.telegram.chat_ids", "111,222");
    expect((screen.getByLabelText("chat_ids") as HTMLInputElement).value).toBe("");
    expect(document.body.textContent).not.toContain("111,222");
  });

  it("rejects empty telegram values without calling the API", async () => {
    render(<AdminPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Telegram 向导" }));

    await screen.findByLabelText("Bot token");
    fireEvent.click(screen.getByRole("button", { name: "保存 token" }));

    expect(await screen.findByText("值不能为空")).toBeInTheDocument();
    expect(apiMocks.putAdminConfig).not.toHaveBeenCalled();
  });

  it("refreshes cluster and config on button click and stamps the update time", async () => {
    render(<AdminPage />);

    await screen.findByText("v0.9.0");
    expect(screen.getByText(/^更新于 \d{2}:\d{2}:\d{2}$/)).toBeInTheDocument();
    expect(apiMocks.getAdminCluster).toHaveBeenCalledTimes(1);
    expect(apiMocks.getAdminConfig).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: /刷新/ }));
    await screen.findByText(/^更新于 \d{2}:\d{2}:\d{2}$/);
    expect(apiMocks.getAdminCluster).toHaveBeenCalledTimes(2);
    expect(apiMocks.getAdminConfig).toHaveBeenCalledTimes(2);
  });
});

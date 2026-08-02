# （草稿）GitHub Issue 正文 — 运维工具化：本地联调脚本 + 日构建刷新脚本化 + Futu 网关地址双语义修复

**建议标题**：`[feature] 运维工具化：dev-up 一键联调环境 + release republish 刷新 + FUTU_PROTO_ADDR 独立配置`

---

## Goal（老板 2026-08-02 反馈：**运维脚本需要整理好，避免每次临时写；日构建的意义就是稳定这些工具**）

8-02 部署 vdaily-20260802 时暴露三个问题，全部靠临场探测/临时命令解决——这些必须沉淀为脚本与文档：

1. **本地联调无脚本**：PG/OpenD 地址每次探测（127.0.0.1 不通 → 试多个地址 → 发现 OrbStack 下 bridge 容器 IP 可达），watchlist 演示数据手工填充，serve 手工起、smoke 手工 curl。
2. **日构建刷新无流程**：tag 落后 main 时，需手工 `gh release delete` + 删远端 tag + 重建 + 下载校验部署——全是临时命令。
3. **FUTU_GATEWAY_URL 双语义缺陷**：`internal/httpapi/futu_account.go`（proto TCP 11111）与 `futu_quote.go`/`futu_options.go`（REST 22222）读**同一 env**，设置 REST 地址后 account 端点必 503。

### 范围（3 个子切片）

#### 子切片 A：`scripts/dev-up.sh` 本地联调一键脚本

- 自动发现地址：`docker inspect` 解析 PG（含 wbot_test 的容器）与 futu-opend 容器 IP；无容器时给出可读错误与 docker compose 提示。
- 动作（子命令或顺序执行）：`dev-up.sh [up|down|status]`——`up` = 检查 PG/OpenD → 填充演示 watchlist（若空：HK.00700 covered-call、SAVE.US cash-secured-put）→ 起 `wbot serve -listen 0.0.0.0:8080`（后台）→ smoke test（health/backtests/watchlist/options 状态码断言）。
- 网络注意事项（OrbStack 实测 2026-08-02）：host 网络容器在 127.0.0.1 **不可达**；bridge 容器 IP 可达（192.168.215.x）；OpenD REST `http://<ip>:22222`、proto `<ip>:11111`。
- 验收：全新环境 clone 后 `scripts/dev-up.sh up` 一条命令起完整体验环境，输出各端点状态；`down` 干净停止。

#### 子切片 B：`scripts/release.sh republish` 子命令（日构建刷新）

- `republish --version daily-YYYYMMDD`：删除旧 release（`gh release delete`）与远端 tag → 从当前 main 重建 publish（复用既有 build/publish 逻辑）→ 输出新 tag commit 与校验和。
- 部署校验步骤（下载 → SHA256SUMS 校验 → 替换 `~/.wbot/releases/` 二进制）沉淀为 `scripts/deploy-daily.sh` 或并入 republish 文档化流程。
- 验收：一条命令完成 tag 刷新；`scripts/verify.sh` 绿；CI 绿。

#### 子切片 C：FUTU_GATEWAY_URL 双语义修复

- `internal/httpapi/futu_account.go`：新增独立 env `FUTU_PROTO_ADDR`（默认 `127.0.0.1:11111`），account 读它；`FUTU_GATEWAY_URL` 仅 REST（quote/options）使用。
- 或统一解析：`FUTU_GATEWAY_URL` 带 `http://` scheme → REST，裸 host:port → proto（**待拍板**，产品建议：独立变量更清晰）。
- 验收：同一部署（REST + proto 双地址）下 account/quote/options 全 200；httptest 覆盖；`doc/API.md` 环境变量章节同步。

### 非目标

- 告警/监控体系（另有产品体验意见项）。
- CI 侧部署自动化（本地 operator 场景优先；远端部署属后续）。

### 依赖

- **无外部依赖**。前置复用：`scripts/release.sh`（既有 build/publish）、`~/.wbot/config.yaml` DSN 约定、futu-opend 容器（既有 compose）。

## 仓库内链回

- 需求源：老板 2026-08-02 反馈（运维脚本沉淀）；RELEASE_DAILY.md（每日流程）、AUTO_ADVANCE（operator 巡检）
- 现状：`scripts/release.sh`（build/publish）、`internal/httpapi/futu_account.go`/`futu_quote.go`/`futu_options.go`、`configs/docker-compose*.yml`、`~/.wbot/releases/daily-20260802/`

## 状态（2026-08-03）

✅ **已完成并合入**：A `scripts/dev-up.sh`（自动发现 PG/网关地址 +
md5 变化自动重启）、B `scripts/release.sh republish`、C
FUTU_GATEWAY_URL 双语义（REST `http://` / proto 裸 host:port）均已
落地。闭环记录见 `doc/tasks/2026-08-02-devup-auto-restart.md`。

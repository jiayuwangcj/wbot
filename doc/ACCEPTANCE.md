# 验收体系总表（ACCEPTANCE）

验收按「提交前本地全可用 + 逐端点验收 + 运维沉淀」规则分层组织（见 [[tasks/README]] 与各闭环归档）：

| 层 | 入口 | 覆盖 | 何时跑 |
| --- | --- | --- | --- |
| 单测 + 静态 | `scripts/verify.sh`（= CI `test` job） | 全部 Go 包单测、gofmt、契约测试 + 零依赖 accept（paper/agent-federation） | 每次提交前；CI 自动 |
| 本地全链冒烟 | `scripts/dev-up.sh`（19 项） | `wbot serve` 全部 DB 本地 HTTP 端点（health/runs/bars/account/snapshots/admin·status/config/watchlist 等）+ CLI ingest file/url/status/bars 三维补漏 | 每次 dev 环境启动 |
| 逐端点 e2e | `scripts/accept-*.sh`（12 个，135 项） | 各子系统 CLI/HTTP 真实契约（含真实网关/真实 PG） | 每个闭环提交前，连跑两遍；**零依赖对与 PG 依赖对已在 CI 自动跑**（#52/#53/#56/#57） |

**原则**：dev-up 只冒烟不逐端点验收；accept 脚本只覆盖不冒烟的部分（如 futu 系依赖网关，刻意不入 dev-up，由 accept 覆盖）。CLI 面按「verify.sh 有无冒烟 + dev-up 有无覆盖 + accept 有无脚本」三维对账（#47/#49/#50 经验）。

## accept 脚本一览

| 脚本 | 覆盖 | 检查数 | 前置 / 运行 | CI |
| --- | --- | --- | --- | --- |
| `accept-agent-federation.sh` | master/agent 联邦（register/agents/错误契约 + `wbot agent` e2e 自注册） | 11 | 无 PG 无网关。`scripts/accept-agent-federation.sh` | ✅ test job |
| `accept-paper.sh` | `wbot paper` CLI 契约（side 别名/非法 side/空 symbol） | 12 | 纯本地。`scripts/accept-paper.sh` | ✅ test job |
| `accept-option-freshness.sh` | option_quotes 新鲜度判定（CLI exit 门禁） | 6 | go + psql 或 docker。`scripts/accept-option-freshness.sh [bin] [dsn]` | ✅ db-integration |
| `accept-bars-refill.sh` | `wbot ingest` bars 补数据端到端（201 + 幂等落库） | 4 | serve + PG。`scripts/accept-bars-refill.sh [base-url]` |
| `accept-options-ingest.sh` | 期权链拉取端到端（错误契约 + 真实 201 + 幂等） | 4 | serve + PG。`scripts/accept-options-ingest.sh [base-url]` |
| `accept-options-cluster.sh` | cluster 端点 options_freshness 字段 | 2 | serve + PG。`scripts/accept-options-cluster.sh [base-url] [dsn]` | ✅ db-integration |
| `accept-account-snapshot.sh` | `wbot ingest account` 快照落库（sim/real 双 env + `-every` 循环优雅退出） | 15 | 网关 OpenD + PG。`scripts/accept-account-snapshot.sh [bin] [dsn] [proto-addr]` |
| `accept-account-snapshots-api.sh` | GET /v1/account/snapshots（契约/400/real 通道/快照增长） | 7 | serve + PG + 网关。`scripts/accept-account-snapshots-api.sh [base-url] [bin] [dsn] [proto-addr]` |
| `accept-backtest.sh` | backtest 双面（CLI -dsn/-save/-export + GET detail/export；四条字节一致等价 + from_watchlist） | 21 | serve + PG + 种子 bars。`scripts/accept-backtest.sh [base-url] [bin] [dsn] [symbol]` | ✅ db-integration |
| `accept-watchlist.sh` | `wbot watchlist` CLI（add/remove/list + buy-hold + 写面→读面联动） | 16 | serve + PG。`scripts/accept-watchlist.sh [bin] [dsn] [base-url]` | ✅ db-integration |
| `accept-futu-data.sh` | futu 数据面 HTTP（quote/orders/account） | 15 | serve + 网关可达。`scripts/accept-futu-data.sh [base-url]` |
| `accept-futu-cli.sh` | `wbot futu` CLI（status/quote/funds/position/order + 安全红线） | 21 | 网关可达。`scripts/accept-futu-cli.sh [bin] [rest-addr] [proto-addr]` |

地址参数缺省取 dev-up 已导出的环境变量（`$FUTU_GATEWAY_URL` / `$FUTU_PROTO_ADDR` / `$WBOT_PG_DSN`）；OrbStack 桥接地址实测见 [[FUTU]]。

## 覆盖矩阵（子系统 × 面）

| 子系统 | CLI | HTTP | 说明 |
| --- | --- | --- | --- |
| serve | — | dev-up 19 + 各 accept | DB 本地端点 dev-up 冒烟；futu 系 accept-futu-data |
| ingest | accept-bars-refill / accept-options-ingest / accept-account-snapshot | 同上（POST /v1/ingest） | 含 `-every` 循环 |
| futu | accept-futu-cli 21 | accept-futu-data 15 | order 只测 -dry-run 与校验/红线拒绝路径，**绝不下真单**（写操作 + 账户状态变更，刻意无自动脚本） |
| backtest | accept-backtest 21 | 同上（GET detail/export） | 四条字节一致等价 + from_watchlist 实测 |
| paper | accept-paper 12 | — | 纯本地无网络 |
| watchlist | accept-watchlist 16 | dev-up（PUT/GET /v1/watchlist）+ 联动断言 | 独立 symbol 不留痕 |
| agent/master | accept-agent-federation 11 | 同上 | 无 PG 依赖 |
| configyaml / admin | dev-up（admin·status/config） | 同上 | 配置写面「只写不读」语义见 [[PRIVACY]] |

**对账纪律**（每轮 AUTO_ADVANCE 巡检）：① 端点清单 grep 二进制全部 HTTP 面（含独立子命令）对照 API.md；② CLI 子命令按 verify.sh/dev-up/accept 三维核对；③ 验收脚本断言 vs 真实数据分支找零覆盖分支（「验收覆盖扩展」引擎）。经验与盲区案例见各闭环归档（#40-#50）。

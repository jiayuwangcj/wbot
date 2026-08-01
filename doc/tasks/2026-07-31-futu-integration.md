# 富途接入：OpenD 容器部署（compose）+ Go proto 客户端 + 行情入管道

- **id**: `2026-07-31-futu-integration`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

老板指令（2026-07-31）：**先接入富途 API**——OpenD 用容器方式运行部署，docker-compose 记住配置（configs/）；Go 用 proto 接口连接 OpenD 客户端，参考/可直接使用 qtopie/gofutuapi。行情数据接入现有 ingest 管道。

拆三小步（每步独立可测、独立 PR）：

- **⑪-a OpenD 容器 compose + 部署文档**：新增 `configs/docker-compose.futu.yml`（服务 futu-opend：`ostai/futuopend:latest`，映射 11111，volume 持久化 `/root/.com.futunn.FutuOpenD`，凭证经环境变量注入）+ `doc/FUTU.md`（拉取/启动/首登短信验证码/连通性验证步骤）。
  - 验收：`docker compose -f configs/docker-compose.futu.yml config` 通过；`-f ... up -d` 后 `nc -zv 127.0.0.1 11111` 可达（有账号时）；doc/FUTU.md 覆盖无账号时的编排校验路径；reviewer 扫描无凭证值泄漏。
- **⑪-b Go 客户端接入**：依赖引入 `github.com/qtopie/gofutuapi`（go.mod 兼容性决策见 Constraints）+ `internal/broker/futu` 包（连接 OpenD、行情查询）+ CLI 命令 `wbot futu`（如 `wbot futu quote -symbol HK.00700`、`wbot futu status`）。
  - 验收：`go build ./...` + `go test ./internal/broker/...`；无 OpenD 时命令优雅报错（exit 非零 + 可读信息）；有 OpenD 时 `wbot futu status` 打印连接与账号信息。
- **⑪-c 数据管道接入**：`internal/broker/futu`（或 `internal/ingest`）实现 `ingest.Source` 接口（`Bars(ctx, symbol, timeframe, from, to)`），复用 `RunIngestion` / `RunEveryResilient` 落库；CLI `wbot ingest futu -symbol ... -timeframe ... [-from -to -every]`。
  - 验收：K 线条数/字段与 OpenD 返回一致；重复执行 ON CONFLICT 幂等（复用既有机制）；与 `ingest file|url` 输出格式一致。

## Constraints

- **PRIVACY 红线**：凭证值（富途账号、密码/MD5、短信验证码）一律不入库、不入 commit/PR；compose 用 `${FUTU_LOGIN_ACCOUNT:?}` / `${FUTU_LOGIN_PWD_MD5:?}` 强制注入，真实值放 `~/.wbot/env.sh`（doc/PRIVACY.md）；doc/FUTU.md 只给变量名与格式示例（`echo -n 'xxx' | md5sum`），不给真实值。
- **go.mod 兼容性**：qtopie/gofutuapi 的 go.mod 要求 **go 1.24.4 + google.golang.org/protobuf v1.36.6**；本仓库 go 1.22.0。⑪-b 开工前须决策：a) 升本仓库 go.mod 至 1.24.x（连带确认 CI runner Go 版本）；或 b) 依赖 GOTOOLCHAIN=auto 自动下载。此决策记录到本文件 State。
- OpenD 无官方 docker 镜像，社区镜像为准（ostai/futuopend 为调研推荐）；镜像 tag 固定（不追 latest 亦可，可选）。
- 不改 bars/ingestion_runs schema；`verify.sh` 无 PG 仍通过；不实现交易下单（⑪ 仅行情）。
- OpenD 行情权限依赖富途账号开通情况（美股/港股），账号未开通的市场命令报错属预期，文档注明。

## Links

- 目标切片：`doc/tasks/2026-07-31-web-v1-target.md` ⑪（富途优先，可做）
- 凭证/账号需求：老板反馈主题 [discussions/21「需要老板处理的」](https://github.com/jiayuwangcj/wbot/discussions/21)（挂起项：富途账号 + 密码 + 首登短信验证码）
- 参考：qtopie/gofutuapi（GitHub，MIT，proto 生成代码已入库，`gofutuapi.Open(ctx, FutuApiOption{Address: "localhost:11111"})` + `client.NewClient`）；OpenD 端口 11111（API）/ 22222（telnet 控制）；数据目录 `~/.com.futunn.FutuOpenD`
- [[PRIVACY]]、[[ROADMAP]] v1 数据管道

## State

- **status**: `done`（⑪-a/b/c 全部完成：网关上线 PR #58、客户端 PR #59、数据管道 PR #61）
- **last step**: 2026-07-31 dispatcher 调研完成：OpenD 社区镜像 `ostai/futuopend`（FUTU_LOGIN_ACCOUNT/FUTU_LOGIN_PWD_MD5/FUTU_PORT=11111/SERVER_PORT=8000 验证码 WebSocket；持久化目录 `/root/.com.futunn.FutuOpenD`）；qtopie/gofutuapi 可用（38 commits、MIT、未归档、约 2 个月前有推送；连接/行情/交易 API 形态已确认，proto 用 buf 生成、gen/ 已提交）；**风险：其 go.mod 要求 go 1.24.4，本仓库 go 1.22.0**。

## Next

- ⑪-a：派 coder（worktree `.claude/worktrees/futu-opend`，分支 `feat/futu-opend`）产出 compose + doc/FUTU.md → verify（compose config 校验）→ reviewer（重点：凭证泄漏扫描）→ 合入。
- 无（⑪ 完成）。后续：交易命令（默认模拟盘 trd_env=0，实盘写需老板确认——doc/FUTU.md 交易安全策略）；K 线全量拉取页数上限/流式（P3 排期）。
- ⑪-c 依赖 ⑪-b 连接层真实可用。

# datacheck 后续优先级与交付拆分

- **id**: `2026-08-09-datacheck-priority-plan`
- **created**: `2026-08-09`
- **updated**: `2026-08-09`

## Goal

基于当前 `codex/feat-daily-data-completeness` 的真实状态，把数据齐全主线拆成可独立验收、无交叉返工的 P0/P1/P2，并按顺序持续交付。

## Constraints

- 数据完整性继续高于回测、模拟盘和新券商扩张（`doc/ROADMAP.md`）。
- 当前未提交改动属于 P0，不与后续切片混成一个不可审查的大提交。
- HTTP 只读观察面与自动 repair 写面分离；不新增浏览器 repair 按钮。
- 不接真实交易、不写真实账户；浏览器验收只使用隔离临时 PG/DEMO 数据。
- 每个切片必须有单测；涉及 DB 的切片必须有真实 PG 集成测试；收口必须 `scripts/verify.sh` 全绿。

## Priority

| 优先级 | 切片 | 依赖 | 验收出口 |
| --- | --- | --- | --- |
| P0 | datacheck core 收口与独立审查 | 当前工作区 | 独立 review 无 P0/P1；verify + 真 PG + Chrome headless；临时环境清理；可提交 |
| P1 | datacheck 只读可观测 API + Data 页摘要 | P0 | `GET /v1/datacheck` + UI 缺失/过期摘要；无 repair 写端点；API/Web/PG 测试 |
| P2 | 交易所节假日日历抽象 | P0 | 港/美/沪深 calendar 可注入；节假日不误拉；周末与 DST 回归测试 |
| P3 | 缺失报告外部通知 | P1 | Telegram/Discord adapter；默认关闭；无凭证不影响单体 |

## State

- **status**: `done`
- **last step**: P0-P3 与最终 UI 收口全部完成；真实 PG、Mac Chrome 桌面/390px、全量 verify 均通过，临时服务/容器/浏览器资料已清理。

## Next

后续仅需按年度更新内建交易日历；通知凭证由部署方按需配置，不影响默认单体运行。

## Final Evidence

- `scripts/verify.sh`: `verify: ok`（含全仓测试、race、staticcheck）。
- 真实 PostgreSQL 16：`internal/datacheck`、`internal/httpapi`、`cmd/wbot` 集成测试 PASS；共享数据库残留数据下 fixture 仍隔离。
- Mac Chrome 151：桌面 1440×1000 实际截图通过；移动端 DevTools 390×844 中 `innerWidth = documentElement.scrollWidth = 390`，工具栏、5 个指标与异常表均在视口内。
- 浏览器 fixture：`symbols=1 / total=25 / complete=0 / missing=24 / stale=1`；缺失优先、中文状态色、检查时间与读取失败状态均可见。
- 验收期间额外修复：回测详情/JSON/CSV 时间统一 UTC；`umask=0077` 权限测试稳定；真实 PG integration 不再依赖数据库只有一条期权 coverage。
- 审计结果：本计划与 Web v1 总目标均归档为 done；无其他结构化 `running` / `queued` task。

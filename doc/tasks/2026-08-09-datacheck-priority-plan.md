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

- **status**: `running`
- **last step**: P0、P1 已完成；P2 已完成官方 2026 交易日历、离线注入与时间边界测试。P3 进入实现。

## Next

实现 P3 Telegram/Discord 外部通知：默认关闭、无凭证零影响、仅 repair 后仍异常或调度失败时发送。

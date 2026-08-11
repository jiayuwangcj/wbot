# Dynamic Wheel replacement rewrite

- **id**: `2026-08-10-wheel-full-rewrite`
- **created**: `2026-08-10`
- **updated**: `2026-08-10`

## Goal

以单一、结构化、仅提醒的动态 Wheel 库存策略作为产品契约：用户显式提供价格—目标库存曲线和最大库存；系统记录状态、实际/有效库存、完整 quote snapshot、版本配置和信号；输出只有 `ALERT`/`HOLD`，任何处置都必须由人工完成。

## Constraints

- `/v1/strategies` 和 `/v1/watchlist` 只接受/返回 `wheel`；旧配置不猜测、不静默转换。
- `price_position_curve`、`max_inventory` 是 required；曲线价格严格递增，目标库存单调不增并受最大库存约束。
- 每个信号引用不可变 `config_version`，保存完整库存 snapshot、候选、拒绝理由、`capability_status` 和 `blocked_by`。
- 缺失 Delta、bid/ask、IV、OI、Theta、volume、lot size、source 或新鲜度时只能 `DATA_BLOCKED/HOLD`。
- 不调用交易 API；`CONFIRM`/`IGNORE`/`FILL`/`NOTE` 只写人工审计记录。
- 共享工作区内仅修改分配的文档；历史 issue/task 保持原样。

## Links

- Driven-By / trigger: attached `wbot_wheel_strategy_spec_v1.docx` and replacement decision
- Design: `doc/WHEEL_STRATEGY.md`
- API contract: `doc/API.md`
- Backtest contract: `doc/BACKTEST.md`

## State

- **status**: `running`
- **last step**: P0-A/P0-B/P0-C/P0-D 与只读审计面已完成；Mac Chrome 桌面/390px、动态详情、真实 PostgreSQL、Wheel-only acceptance 均已取得证据。Luna 终审发现的累计 Put 现金、原子批次 limit/lookback、capability 持久化、方向缺失和批量/重跑 UI 问题均已修复并补回归。
- **product status**: 领域/注册表/持久化/只读审计/UI 边界 `READY`；bar-time 最新完整原子 snapshot 回放 `READY`（研究/验证）；真实供应商 adapter、实时提醒和历史事件回测 `DATA_BLOCKED`；人工写动作与期货腿仍未解锁。

## Execution plan

| Priority | Slice | Depends on | Acceptance | Current status |
| --- | --- | --- | --- | --- |
| P0-A | Wheel 领域核心：曲线、实际/Delta/有效库存、状态、方向、候选质量、交易后与指派风控 | none | `go test ./internal/wheel`；缺字段必为 HOLD | `READY` |
| P0-B | 单一 `wheel` 注册表、required schema、watchlist/API 校验 | P0-A | `/v1/strategies` 只有 wheel；非法/旧名明确拒绝；JSON round-trip | `READY` |
| P0-C | 版本配置、quote snapshot、signal/action audit 和 repository | P0-A | migration + repository tests；不完整 snapshot 不出 ALERT；无交易写路径 | `READY`（真实 PostgreSQL integration passed） |
| P0-D | 策略页结构化曲线编辑器、库存/状态/质量配置和验证反馈 | P0-B | 桌面/移动浏览器不可保存非法曲线；证据入账 | `READY`（Mac Chrome desktop/390px） |
| P1-A | 事件回测适配器、信号日志、到期/指派/机会成本指标 | P0-A, P0-C | 完整历史 snapshot/事件样本端到端可复现；最大库存违规为 0 | `DATA_BLOCKED`（当前只有 bar-time 最新 snapshot 回放） |
| P1-B | 5–10 DTE、质量、覆盖率参数面与滚动稳定性报告 | P1-A | 100%/固定/随机漏 30%/50%，seed 可复现 | `RESEARCH_ONLY` |
| P1-C | 期货等价 Delta 与保证金模型 | P0-A | 乘数、币种、保证金边界测试 | `INTEGRATION_BLOCKED` |
| P2 | 多标的组合视图、手工宏观/基本面状态切换 | P0-D, P1-A | 无预测器；配置和信号有审计轨迹 | `pending` |

## Non-executable work register

| Item | Current status | Blocking reason | Retained work | Enable gate | Forbidden fallback |
| --- | --- | --- | --- | --- | --- |
| 真实行情 adapter / 实时 Wheel 提醒 | `DATA_BLOCKED` | 尚无经过验收的供应商 adapter 能提供同一时点完整 bid/ask、Delta、IV、OI、Theta、volume、lot size 和 freshness | domain input、完整 snapshot schema、缺字段 HOLD、信号 reason、不可变 repository 已落库 | 可信只读 adapter、真实采样、原子 snapshot、断线/限流/陈旧测试和零交易调用审计通过 | 不用日线 close、固定 Delta/IV/OI/Theta、默认流动性或拼接不同时间数据 |
| 当前 bar-time snapshot 回放 | `READY`（研究/验证） | bars 运行器已实现按 bar 选择 `observed_at <= bar.ts` 的最新原子批次，但没有 quote/成交事件顺序 | deterministic fixture、同输入同 trace、未来数据不泄漏和 stale → HOLD 回归 | 保持当前确定性回归即为启用证据 | 不把它当实时执行或事件驱动历史回测 |
| 历史事件回测 | `DATA_BLOCKED` | 历史覆盖不足以还原逐 quote/成交事件、盘口/Greeks 和人工事实 | snapshot schema、bar-time trace 和机械到期结算已保留 | snapshot/事件/人工审计覆盖目标日期/DTE，trace 可复现且最大库存违规为零 | 不用 OHLC 猜 bid/ask/Greeks，不把 bar-time 回放冒充事件回测 |
| 配置/信号 HTTP 产品面 | `READY`（只读）/ `INTEGRATION_BLOCKED`（写动作） | 配置、信号和人工动作的只读 serve 路由已具备；身份、授权和人工动作写入闭环尚未齐 | 三个 read-only API、窄化 store、schema/repository 和 append-only 约束已落库 | PG/API/browser 只读验收；写动作另需身份与授权设计 | 不开放匿名写入，不覆盖版本或信号历史，不把查看按钮冒充处置能力 |
| 人工操作闭环 | `INTEGRATION_BLOCKED` | actor 身份、权限边界和 UI 审计尚未验收 | append-only action 表和人工动作类型 | 身份/权限/审计及浏览器流程通过 | 不把 CONFIRM 当下单，不提供自动确认 |
| 期货库存/保证金 | `INTEGRATION_BLOCKED` | 乘数、实时 Delta、币种和券商规则未接入 | position 模型预留 futures equivalent | 账户与合约元数据来源、边界测试验收 | 不估算保证金，不把缺失等价库存当零 |
| 覆盖率与参数面 | `RESEARCH_ONLY` | 依赖事件回测和足够历史 snapshot | 可复现 seed/coverage 接口设计 | P1-A 和数据覆盖闸门完成 | 研究结果不写 ALERT、不改战略配置 |
| 极端日第二张触发器 | `INTEGRATION_BLOCKED` | 每日 2 张硬上限已有，但显著二次价格/库存偏移尚无可信事件输入和审计定义 | `ExtremeDay` 领域输入与硬上限 | 事件来源、阈值归属、审计回放和普通日不可升级测试通过 | 不因波动率高或人工默认值自动放宽到 2 张 |
| 浏览器编辑/回测验证 | `READY`（当前只读/回测面） | Mac Chrome 桌面、390px 移动和动态详情已验证；人工写动作仍由独立身份闸门阻塞 | 结构化编辑器、非法曲线反馈、信号审计和结果 trace UI | 保持 Chrome/CDP 动态断言与回归；写动作另行验收 | 没有浏览器证据不得扩展为人工处置 READY |
| 实时/自动执行 | `OUT_OF_SCOPE` | 产品边界是人工提醒，不提供实时执行器 | 无交易 client、无自动确认开关 | 永久不启用 | 不留隐式交易路径、实时执行器或降级开关 |

## Contract checklist

- 配置必须包含 `strategy: wheel`、`price_position_curve`、`max_inventory`；其余字段按 schema 校验并版本化。
- `actual_inventory = stock_shares + futures_equivalent_shares`；`effective_inventory = actual_inventory + option_delta_stock`；价格曲线插值确定 target 和 gap。
- `gap > no_trade_gap` 只扫描 Put，`gap < -no_trade_gap` 只扫描 Call，其余为 `HOLD`；战略状态进一步限制方向。
- 候选必须包含 `expiry/strike/delta/bid/ask/implied_vol/theta/volume/open_interest/lot_size/observed_at/source`；缺失、过期、倒挂、零流动性或质量不达标都只能 HOLD。
- `ALERT` 仅在能力状态 `READY`、完整 inventory snapshot、至少一个候选且全部风险门槛通过时生成；否则记录 `HOLD`、缺失字段和 reason。
- 旧 watchlist 行保留原 JSON 作为审计证据并标记 `NEEDS_RECONFIGURATION`；用户保存完整 Wheel 配置后才允许建立新版本。

## Verification ledger

| Slice | Commit | Unit / integration | Browser | Capability status | Evidence / next gate |
| --- | --- | --- | --- | --- | --- |
| planning baseline | working tree | `git diff --check` passed | N/A | design only | 本文档/API/BACKTEST 已统一产品边界 |
| P0-A domain core | working tree | `go test ./internal/wheel` passed | N/A | `READY` | 曲线、库存、四状态、候选质量和不完整报价回归 |
| P0-B registry/watchlist schema | working tree | `go test ./internal/strategy ./internal/httpapi ./cmd/wbot` passed；真实 PG integration passed | Mac Chrome desktop passed | `READY`（服务端/UI） | 不恢复旧模板；人工写动作另受身份闸门约束 |
| P0-C persistence boundary | working tree | `WBOT_PG_DSN=... go test ./internal/wheelstore ./internal/watchlist ./internal/httpapi ./internal/backtest ./internal/datacheck ./cmd/wbot -count=1` passed | read-only HTTP 200 / write 405 passed | `READY`（repository/read-only） | 真实 provider adapter 与人工动作 HTTP 身份面仍是启用闸门 |
| backtest signal journal | working tree | migration 006 + Save/Load/JSON/CSV round-trip tests | N/A | `READY`（bar-time trace） | 保存完整 `strategy_params`；不声称包含人工动作或事件级成交事实 |
| full regression | working tree | `go test ./... -count=1`、`go vet ./...`、关键包 race、真实 PG integration、`node --check`、shell syntax、`git diff --check` 全部 passed | Mac Chrome passed | `READY`（当前切片） | 发布前保持同组回归 |
| bar-time latest snapshot replay | working tree | strategy/backtest tests passed | N/A | `READY`（研究/验证） | 明确不是事件驱动；保持最新原子批次选择和 stale → HOLD |
| Wheel read-only audit API/UI | working tree | handler/mux/Web UI/PG tests passed；实际 GET 200、POST 405 | Mac Chrome desktop passed，无 JS exception | `READY`（只读） | configs/signals/actions 只读；人工动作写入仍 `INTEGRATION_BLOCKED` |
| capability/snapshot signal trace | working tree | wheel/strategy/backtest/export/PG round-trip passed | Mac Chrome desktop + 390px；动态详情 5 rows、3 READY、2 DATA_BLOCKED、无页面级横向溢出 | `READY`（bar-time audit） | 保存 blocked_by 与原子 snapshot 身份；不等价于事件驱动回测 |
| Luna P1 correctness audit | working tree | 累计 Put reserve、方向缺失、批次 limit/lookback、signal DB CHECK、稳定空数组均有回归 | batch form valid；rerun 回填 symbol/curve/max inventory | `READY` | migration 007 已在真实 PG 应用；无 P0 遗留 |
| Wheel-only acceptance/CI | working tree | `accept-watchlist` 16/16；`accept-backtest` 21/21；CI/dev-up 不再以简单策略作产品种子 | HTTP/CLI round-trip passed | `READY`（fixture） | `source=demo-fixture` 只用于测试，不宣称真实 provider |
| P1-A event backtest | pending | pending | pending | `DATA_BLOCKED` | 先取得完整历史 snapshot/事件覆盖，再跑固定样本 |

## Next

1. 取得真实供应商 adapter 的字段映射、原子性/新鲜度/断线/限流证据；证据齐全前实时提醒维持 `DATA_BLOCKED/HOLD`。
2. 保持 P0-D 桌面/移动和 configs/signals/actions 只读 HTTP 回归；人工写动作仍需身份、权限和审计设计，在证据齐全前维持 `INTEGRATION_BLOCKED`。
3. 取得覆盖目标日期/DTE 的完整历史 snapshot 与 quote/成交事件，才能解锁事件回测和参数研究；bar-time replay 继续标为研究/验证能力。
4. 继续维持人工处置和期货腿阻塞；不得把 `CONFIRM` 当下单、把缺失期货等价库存当零或估算保证金。实时/自动执行永久 `OUT_OF_SCOPE`。
5. 发布前复扫五份主文档与 acceptance：旧策略术语只能出现在迁移/拒绝测试，CLI 默认 `hold`/产品只接受 `wheel` 的边界必须保持。

迁移说明：历史资料中的旧策略名称只用于识别 legacy 行和审计，不构成当前产品 schema、CLI 示例或 `/v1/strategies`/`/v1/watchlist` 合法值。

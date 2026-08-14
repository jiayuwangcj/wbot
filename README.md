# wbot

`wbot` 是一个面向个人交易的 Go 数据与策略工作台。当前产品策略只有动态 Wheel：用户定义满仓价、清仓价和最大库存，系统根据有效库存与完整期权快照生成 `ALERT` 或 `HOLD`，最终处置由人工完成。系统不自动下单。

## 当前边界

- `/v1/strategies` 和 `/v1/watchlist` 只提供结构化 `wheel` 配置。
- 新配置必须显式包含 `full_position_price`、`zero_position_price` 与 `max_inventory`；旧曲线可兼容读取，转换后只写新键并保留有损迁移审计。
- 信号必须带配置版本、实际/有效库存、完整 snapshot、候选、拒绝理由和 capability status。
- `GET /v1/wheel/configs`、`GET /v1/wheel/signals`、`GET /v1/wheel/signals/{id}/actions` 提供只读审计；没有匿名写动作。
- 缺少 bid/ask、Delta、IV、OI、Theta、volume、lot size 或新鲜度不满足时，安全结果是 `DATA_BLOCKED/HOLD`。
- 提醒只供人工确认、忽略或成交回填；系统没有交易 API 写路径。

当前已完成并有测试证据的部分包括 Wheel 领域决策、唯一策略注册表、watchlist 校验/版本配置、不可变快照 schema、signal/action repository、确定性的 bar-time 回放适配器，以及 HKEX 官方港股期权日终回填。实时供应商 adapter、实时提醒、事件级历史回测、人工操作 HTTP/UI 闭环和期货腿仍受能力闸门阻塞；详见 [Wheel 策略基线](doc/WHEEL_STRATEGY.md)、[API 契约](doc/API.md)、[回测契约](doc/BACKTEST.md) 和 [当前任务](doc/tasks/2026-08-14-backtest-hkex-datafill.md)。

`wbot backtest` 的 CLI 默认策略实际是 `hold`，仅作为内部基准；显式指定 `-strategy wheel` 才运行 Wheel。产品 API、`/v1/strategies` 和 `/v1/watchlist` 只接受 `wheel`，不把 `hold`/`buy-hold` 暴露为产品策略。

当前回测按 bars 的时间轴运行：每根 bar 选择 `observed_at <= bar.ts` 的最新完整原子 quote snapshot；同一时点按 `futu` → `hkex` → 其他来源，再按 snapshot key 稳定选择。HKEX 路径把官方日终结算价投影为 `bid=ask`，并用官方 IV/标的结算价计算研究 Greeks，只能输出 `RESEARCH_ONLY`，不代表历史可成交盘口，也不接入实时提醒。它不是按 quote/成交事件驱动的历史执行回放；没有完整快照时只能 `DATA_BLOCKED/HOLD`。

## 工程约束

- 主要语言：Go；部署为单二进制，前台 `serve`。
- 存储：PostgreSQL；Web 为 Go `embed` 的原生 HTML/CSS/JS。
- 市场数据：港股/美股现货与期权；供应商接入必须经过可替换 adapter 和快照完整性闸门。
- 交易接入保持只读边界；自动交易属于 `OUT_OF_SCOPE`。

## 本地开发

```bash
go test ./... -count=1
go vet ./...
scripts/verify.sh
scripts/dev-up.sh
```

逐端点验收脚本见 `scripts/accept-*.sh`，总表见 [doc/ACCEPTANCE.md](doc/ACCEPTANCE.md)。提交前同时运行：

```bash
git diff --check
```

## 文档入口

- [文档索引](doc/README.md)
- [Wheel 策略](doc/WHEEL_STRATEGY.md)
- [API](doc/API.md)
- [回测](doc/BACKTEST.md)
- [数据管道](doc/DATA_PIPELINE.md)
- [Futu/OpenD](doc/FUTU.md)

# 回测 P0 阻碍清除

**State**: complete（2026-08-14）

## Goal

让 HK.00700 与 US.JD 的固定参数 Wheel 回测报告忠实反映可用数据、实际费用、原始参数版本和窗口末持仓；数据不足时必须输出 `DATA_BLOCKED`，不得把窗口末估值写成可执行的收益承诺。

## Constraints

- 只处理回测、报告与固定参数加载，不修改实时 Wheel、下单、Telegram 或 Discord scheduler。
- 历史数据不得用 OHLC、固定 Greeks、默认盘口或跨时点拼接补造。
- 数据库只读；本任务不会写行情表。富途若没有历史完整快照能力，不实现伪回填工具。
- 提交前运行 `scripts/verify.sh`；关键契约用确定性单测覆盖。

## Links

- 开发任务：`/tmp/sol-backtest-p0-dev.md`
- 真实回测检查：`/tmp/sol-fixed-backtest-check.log`
- 报告 schema 裁决：`~/.claude/plans/mutable-nibbling-music.md`
- 富途官方历史 K 线：<https://openapi.futunn.com/futu-api-doc/en/quote/request-history-kline.html>
- 富途官方期权快照：<https://openapi.futunn.com/futu-api-doc/quote/get-option-quote.html>
- 富途官方期权链：<https://openapi.futunn.com/futu-api-doc/en/quote/get-option-chain.html>

## P0-1 历史数据面裁决（2026-08-14）

结论：**富途/OpenD 当前接口不能为 HK.00700 或 US.JD 回填覆盖完整到期周期的历史原子期权 snapshot**，因此本任务不新增回填写库工具，报告必须保持 `DATA_BLOCKED`。

证据：

1. 实际 `futu-opend-rs` 网关 `GET /health` 返回 `ok`，`/api/global-state` 返回 `qot_logined=true`、`server_ver=1002`；不是网关离线造成的误判。
2. `GetHistoryKL`/`/api/history-kline` 支持按时间分页拉 OHLCV K 线，但返回结构不含同一时点 bid/ask、Delta、IV、Theta、OI 或完整盘口；期权日 K 只能保留价格/成交量，不能转换成 Wheel snapshot。
3. `GetOptionQuote`/`/api/option-quote` 请求只含组合腿，没有历史时间参数；返回的是调用时点快照。当前网关适配器也只缓存实时结果，不能查询过去时点。
4. `GetOptionChain` 的 `start/end` 是**到期日范围**，官方明确说明只返回静态合约信息；它不是历史报价接口，也不能恢复已过去时点的动态 Greeks/盘口。
5. 富途的“期权标的历史统计”等接口只提供标的级成交量、持仓量或 Put/Call 比率，不能恢复逐合约原子 snapshot。

具体缺口：`historical_bid`、`historical_ask`、`historical_delta`、`historical_implied_vol`、`historical_theta`、`historical_open_interest`、`historical_quote_time`，以及真实成交/到期/指派事件。已有实时采样只能从采集启用后向未来积累；完整到期周期达标前，固定参数报告不可用于参数裁决。

报告验收：新增数据质量卡，至少包含总 bar、阻塞 bar、有效覆盖率、snapshot 批次/合约行数、必需字段缺失计数、历史周期是否完整及阻塞原因。零 snapshot 也应生成全程 HOLD 的 `DATA_BLOCKED` 报告，而不是伪造数据或只返回运行错误。

## State

- [x] P0-1：核对官方接口与实际网关，裁决为无法历史回填；禁止伪数据
- [x] P0-1：报告数据质量卡与零 snapshot 全程 HOLD 的 `DATA_BLOCKED` 路径
- [x] P0-2：期权成交费用实际扣账并在报告输出
- [x] P0-3：多点曲线无损执行；`-from-watchlist` 只读加载真实 `config_version`，ad-hoc 报告显式 `null`
- [x] P0-4：窗口末持仓、已实现/未实现 P&L、机械到期/指派统计；真实券商事实显式 `null`
- [x] 全量 `scripts/verify.sh`

## 真实只读验收（2026-08-14）

验收程序使用单 PostgreSQL 连接并先执行 `SET default_transaction_read_only = on`；未执行 migration、未调用 `SaveResult`，仅查询生产 watchlist/bars/snapshot 并在 `/tmp` 生成报告文件。

| 标的 | 配置 | bar 窗口 | snapshot | 覆盖 | 报告结论 |
| --- | --- | --- | --- | --- | --- |
| HK.00700 | v1 | 5458 bars，2004-06-15..2026-08-12 | 45 批 / 457 合约行 | READY 1、BLOCKED 5457，0.0183% | schema 1.1；`DATA_BLOCKED`；`net_return_pct=null` |
| US.JD | **v2** | 9 bars，2026-07-31..2026-08-12 | 88 批 / 359 合约行 | READY 0、BLOCKED 9，0% | schema 1.1；`DATA_BLOCKED`；`net_return_pct=null` |

US.JD 报告身份为 `config_version=2`，完整保留 30.25/31.25/32.25/33.25/34.25/35.25 六个曲线锚点，`migration_lossy=false`。实际数据质量计数显示：US.JD 缺 Delta/IV/Theta 各 14 行、OI 15 行、volume 47 行；HK.00700 缺 Delta/IV/Theta/OI 各 24 行、volume 122 行。两者 `historical_option_cycle_complete=false`，没有伪造历史字段或收益。

## 条件性 P1

- 已随 P0 完成数据质量卡；单次报告同输入同 ID/同 JSON/HTML 字节由确定性测试覆盖。
- 未扩展为 ≥3 fold walk-forward：当前历史期权周期与有效 bar 覆盖仍为 `DATA_BLOCKED`，增加 fold 只会重复零/近零有效样本，不能产生可信验证证据。
- 未实现 Discord `-push`：任务约束不修改 Discord scheduler，且当前报告没有可发布的有效收益结论；应在独立 S7 切片实现 report ID 幂等推送。

## Verify

`scripts/verify.sh` 全绿：frontend build、gofmt、`go test ./...`、`go vet ./...`、race、staticcheck 与 CLI smoke 均通过。

## Next

从实时采集开始累计至少一个完整 DTE/到期周期；覆盖与字段完整性闸门通过后，再做 ≥3 fold walk-forward 和 Discord 报告推送。发布前由独立 reviewer 按 feature 类型复核 schema 1.1、终局 P&L 口径与真实只读验收结果。

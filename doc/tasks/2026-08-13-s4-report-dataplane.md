# S4 报告数据面 + 基础 CLI

**State**: codex 已交付(提交 4785f0d,2026-08-13,署名 gpt-5.6-luna;verify.sh 全绿 + 验收 11/11)→ reviewer 评审中

## 交付记录(2026-08-13,codex 4785f0d)

- CLI:runBacktest 加 -report/-report-dir(默认 ./reports),输出 {report_id}.json/.html;report_id = bt-{symbol}-{run_seed}-{hash8} 确定性,同 ID 覆盖写
- 新包 internal/backtestreport/:report.go(schema 1.0 single_run 构造 + unfilled_model 形状映射 + attempt=0 → ratio null)、template.go(Go html/template 确定性渲染,430/390px 响应式,og 元数据)、report_test.go
- CLI 汇总行未成交指标;doc/BACKTEST.md 补 -seed/Result.Unfilled/Trade.Filled
- scripts/accept-backtest-report.sh(11/11,含同输入两次字节一致、汇总可复算)挂入 verify.sh
- 验收总表实计:15 脚本 191 项;环境无 Chromium,430px 走查未做浏览器截图
- PG 集成测按既有规则跳过(未配置 DSN)
**分支/worktree**: feat/s4-report-dataplane @ .claude/worktrees/s4-report-dataplane(基线 26445c4 开发线,含 S1+S3)
**执行**: codex(gpt-5.6-luna,max 思考);额度尽退 Claude coder

## Goal

单次回测输出版本化 JSON(单一事实源)+ 确定性 HTML 报告(Go html/template 渲染,禁 LLM 发挥),CLI 直接产出;尚不接 Discord(那是 S7)。JSON schema 唯一事实源 = **doc/BACKTEST_REPORT.md(schema_version 1.0)**,结构不得自行发明。

## 契约(schema 1.0,详见 doc/BACKTEST_REPORT.md)

- **report_id** = `bt-{symbol}-{run_seed}-{输入哈希前8位}`,由 symbol+run_seed+输入确定性生成;**重复执行同 ID 幂等**(同输入同输出,覆盖写,可复算)
- **single_run 报告** = 顶层 schema_version/report_id/report_kind="single_run" + identity + result + audit + risk(必填)+ trajectory(可含);train/generations/candidates 为 es_train 专属,single_run 可省略
- **identity**: symbol/market/currency/config_version/code_version/data_window/capability_status/blocked_by/run_seed/config.params(脱敏完整输入参数)
- **result**: net_return_pct+amount、max_drawdown_pct、attempt_count/fill_count/unfilled_count/unfilled_ratio(分母 0 → null,不得报 0%)、baseline 字段、cost_model、manual_not_executed_count、hard_violations
- **audit**: input_snapshot_hash/params_dictionary_version/strategy_params_snapshot(含 migration_lossy/original_json)
- **risk**: RESEARCH_ONLY/DATA_BLOCKED 等风险文案列表
- 百分比一律小数(0.0123 = 1.23%);金额同时给 amount 与 return_pct;时间 RFC3339 UTC Z
- **unfilled_model 形状映射(P2⑤)**:S3 落库字段为扁平(Trade.UnfilledModel="heuristic-1.0" 等),报告 §3 要求对象形状 `{model_kind, model_version, order_assumption, components{spread_weight,volume_weight,oi_weight}}`——映射在报告构造层做(S3 已导出的常量/权重;order_assumption 文案按 §3)

## 改动面

- **`cmd/wbot/main.go` runBacktest**: 新增 `-report`(布尔,产出 JSON+HTML)与 `-report-dir`(默认 `./reports`,不存在则创建);输出 `{report_id}.json` + `{report_id}.html`;同 ID 重跑覆盖写。**S2 不碰此区,本片独占**
- **internal/backtest/**(或新 internal/backtestreport/ 包,按现有布局定): Result→报告 JSON 构造(schema 1.0 single_run;含 unfilled_model 形状映射);HTML 渲染 = Go `html/template`,模板内嵌(go:embed 或 const),**禁止大模型生成 HTML**
- **CLI 汇总行加未成交指标(P2⑥)**: 现有汇总输出加 `未成交 N/M (P%)` 或 `未成交 N/A(无成交尝试)`(无尝试时不得报 0%)
- **文档(P2②)**: doc/BACKTEST.md 补 `-seed`、Result.Unfilled、Trade.Filled 说明(增量,勿大改)
- **验收脚本**: scripts/accept-backtest-report.sh(JSON 存在+schema 键齐全、HTML 存在+含关键字段、同输入两次渲染字节一致、未成交口径可复算),连跑通过后记入 ACCEPTANCE.md 对账

## HTML 投影约束(§9,验收硬性)

- Go html/template 渲染本 JSON(或内存同构结构);首屏 = 状态/净收益/回撤/未成交率/停止原因,明细折叠卡片
- iPhone 16 Pro Max **430px** 验收,同时覆盖 **390px**;正文不得横向滚动
- 视觉参考: internal/webui/web/dist/reports/demo.html(深色主题/2×2 指标卡/折叠明细——仅样式参考,模板必须 Go 实现,不得复用 demo 手写 HTML)
- og:title/og:description/theme-color 元数据(§9 + Discord 实测,S7 推送用;og:url 可不写死域名)

## Verify(验收)

- 同输入两次运行:JSON 字节一致、HTML 字节一致(确定性)
- 汇总关键数字可由 JSON 复算(验收脚本对账:net_return/max_drawdown/attempt/fill/unfilled)
- unfilled_model 形状断言:attempt=0 时 unfilled_ratio=null 且对象形状仍齐;attempt>0 时 ratio 可算
- 430/390px 走查(人工,浏览器打开 HTML)
- gofmt/vet/test/race/staticcheck + verify.sh 全绿;独立分支提交,署名 codex(实际编写模型)

## Links

- schema 唯一事实源: doc/BACKTEST_REPORT.md
- 主任务记录: doc/tasks/2026-08-13-backtest-toolchain.md
- S3 任务记录(未成交字段/映射来源): doc/tasks/2026-08-13-s3-unfilled-model.md
- 端到端验收命令(全切片合入后): `wbot backtest -symbol HK.00883 -params '{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}' -report`

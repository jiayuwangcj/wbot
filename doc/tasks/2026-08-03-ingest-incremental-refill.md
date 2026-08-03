# 排期:补数据增量拉取(/v1/ingest from/to 暴露)

## 状态

**✅ 已完成**(2026-08-03)。

## 来源

AUTO_ADVANCE Round 36。候选池(ingest -from/-to、Provider 抽象、外部 cron 文档)实证枯竭后启用**新对账维度:后端能力 vs API/前端暴露面**——CLI `wbot ingest futu -from/-to` 自 Round 21 已支持,但 `/v1/ingest` 无此参数,前端「补数据」每次全量拉 2000-01-01→now(零值 from 在 futu 网关即 2000 起点)。1m/5m 周期补一次可能数分钟(分页 + 限频)。

## 变更

- `internal/httpapi/ingest.go`:`IngestRunner.RunBars` 签名加 `from, to time.Time` 透传 `ingest.RunIngestion`;body 加可选 `from`/`to` RFC3339 字段(kind=bars 专用;与 `parseRangeTime` 同一解析,非法格式/from 晚于 to → 400 `invalid_request`,action 带格式示例;kind=option 忽略)
- `app.js` `ingestBars`:`body` 加 `from: b.max_ts`——覆盖表 max_ts 输出即为 RFC3339(`admin_cluster.go:124`),零转换回传
- 测试:fakeIngestRunner 记录 gotFrom/gotTo;5 个新用例(合法 from / from+to / 非法 from / 非法 to / from after to)+ 透传断言;TestIngestRefillJS 契约断言更新
- `doc/API.md`:body 表补 from/to 行、错误段补三类 400

## 验证

- verify.sh 连跑两遍全绿(期间修 cmd/wbot/main_test.go serveFakeIngestRunner 适配新签名)
- E2E 真实 PG:非法 from `"2026-08-01"` → 400(消息 `invalid from ...: want RFC3339`);from after to → 400;合法 from → 透传至网关调用(本机网关未启 → 503 connection refused,**证明参数解析+透传路径打通**);data.html 200
- CI 五检查全绿;#317 merge --admin

## 收益

补数据从「2000-now 全量重拉」变「max_ts 之后增量」:1m/5m 周期补拉秒级完成;边界重叠根由 ON CONFLICT 幂等(无害)。CLI/API 参数面收敛(同一解析器、同一语义)。

## 引擎经验

**「后端能力 vs API 暴露面」是可持续对账维度**:CLI 有参数、API 没有、前端没有 → 三步缺口任一存在即为候选;对账时先看 CLI 侧 flag(main.go parseRangeTime)与 httpapi handler 的参数,再对照前端调用。**候选评估先查数据形状**(上轮经验)的姊妹篇:**先查现有能力再决定做什么**——这轮零新后端逻辑,只暴露既有能力。

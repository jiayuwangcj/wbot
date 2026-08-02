# Data 页「补数据」+ POST /v1/ingest (S-ingest-refill) — 2026-08-02

状态: ✅ 已合并 (PR #153, commit 301198a)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):Data 覆盖表能看到
stale 标的但没有补数据动作,只能切 CLI `wbot ingest futu`——运营闭环
断点。浏览器不能直连网关(CORS/安全),serve 代理执行。

## 改动
1. **internal/httpapi/ingest.go**(新):
   - POST /v1/ingest,body `{symbol, timeframe, adjust}`;timeframe/
     adjust 缺省 1d/fwd;空 symbol/坏 JSON → 400;非 POST → 405。
   - 复用 internal/ingest.RunIngestion,与 `wbot ingest futu` 同一管线
     (source=http-api);2min 超时 → 504;失败 → 503 `ingest_failed`
     带 CLI 等价提示(action 含完整 `wbot ingest futu -symbol …` 命令)。
   - 接口驱动 `IngestRunner`(同 FutuQuoter 模式);NewIngestRunner 用
     FutuGatewayURL() 取网关地址。
2. **cmd/wbot/main.go**: serveMux 注入 `NewIngestRunner(database)`;
   pattern 无方法限定(handler 内检查 → 405,与 futu 端点一致——
   方法限定 pattern 的 GET 会落 404 catch-all)。
3. **UI(data.html + app.js)**: 覆盖表加「操作」列,每行「补数据」按钮
   (stopPropagation 不触发行 drill-in);ingestBars 置忙「拉取中…」→
   POST → 成功刷新覆盖表并同步明细视图,失败错误可见(coverage-error);
   表头说明文案同步。
4. 测试: httpapi TestIngestHandler(7 例, fake runner)、
   TestIngestHandlerCreatedBody;webui TestIngestRefillJS(9+2);
   main_test serveFakeIngestRunner 接线。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh smoke 10/10
- 逐端点 13/13:UI 契约 9 + GET 405 + 空 symbol 400 + 坏 JSON 400 +
  真实 ingest 503 错误语义可见(本地网关无凭证时,错误体含 CLI 提示)
- CI: 5/5 全 pass 首轮绿;PR #153 merged

## 备注
- 本地 dev 网关(futu-opend-rs 容器)在线但无 futu 凭证时,真实拉取
  返回错误 → 503 错误语义验证即可;真实凭证环境为成功路径(与 CLI
  同一管线,CLI 已验证过)。后续若配好凭证可补一次 201 端到端。
- 明细视图同步:补数据后若 detail 正显示该标的,loadBars 重拉刷新
  (detail-title 含 symbol 判断)。

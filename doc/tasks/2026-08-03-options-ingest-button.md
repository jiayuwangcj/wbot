# Data 页期权新鲜度表加「拉取期权链」按钮 (S-options-ingest-button) — 2026-08-03

状态: ✅ 已合并 (PR #174)

## 背景
AUTO_ADVANCE 根任务循环。闭环 #24 归档备注:「期权 Web 展示边界——
Data 页期权表只读展示,无 drill-in/补数据按钮;如需与 bars 同等的操作
闭环,可后续给 options_freshness 行加『拉取期权链』按钮(走 ingest
futu-option)」。bars 侧有「补数据」按钮(POST /v1/ingest),本闭环按
同一模式补期权侧,数据新鲜度监控对期权在 Web 端可操作。

## 改动
1. **internal/httpapi/ingest.go**:
   - `IngestRunner` 接口 + `ingestRunner.RunOptions`:调 `ingest.
     RunOptionIngestion`(近端 1 个到期、7 天日线窗口,同 CLI `ingest
     futu-option` 默认;ON CONFLICT 幂等)。
   - POST /v1/ingest body 加可选 `kind`:`bars`(默认,原逻辑不动,
     向后兼容)或 `option`(忽略 timeframe)。option 分支响应
     `{kind, symbol, adjust, status}`,错误 action 提示 CLI 命令。
   - **超时**:option 分支 15 分钟(bars 仍 2 分钟)——期权链逐合约
     串行拉取受网关限频,单到期 60+ 合约实测 ~9 分钟(首次实测
     5 分钟不够,`context canceled` 于第 ~30 合约后修正)。
2. **测试**:`fakeIngestRunner` 加 `RunOptions`;TestIngestHandler 加
   option 用例(默认 adjust=fwd / 空 symbol 400 / 失败 503);
   `TestIngestOptionCreatedBody`(kind=option 响应字段 + runner 收到
   option/fwd);main_test.go serveFakeIngestRunner 补方法。
3. **Web**:data.html 期权表加「操作」列 + `options-error` 提示;
   app.js `renderOptionsFreshness` 行尾加「拉取期权链」按钮 →
   `ingestOptions`(POST kind=option,成功刷新覆盖表+期权表,失败
   显示原因,按钮置忙防重复点击);webui_test.go TestDataPageContract
   补断言(options-error / ingestOptions / `{kind: "option", symbol: ...}`)。
4. **文档**:doc/API.md 新增「POST /v1/ingest」章节(此前缺失,本次
   补齐:请求/响应/错误契约/幂等语义 + kind 字段 + 超时说明)。
5. **scripts/accept-options-ingest.sh**(新增,运维沉淀):4 检查——
   空 symbol 400 契约 / 非法 symbol 503 契约 / HK.00700 真实拉取
   201(kind=option) / 数据落库(option_quotes rows/max_ts 拉取前后
   非下降)。

## 验证
- `go test ./... -count=1` 全绿(19 包,含 PG 集成);`gofmt -l .` 干净
- dev-up smoke 10/10(新二进制自动重启)
- 逐端点验收 4/4(scripts/accept-options-ingest.sh):错误路径契约
  2 项 + HK.00700 真实拉取 201 + 数据落库(374 行,近端到期
  2026-08-07,每合约 4-5 根日 K)
- 真实拉取实测:HK.00700 单到期 60+ 合约串行拉取(限频)289s;
  第一次验收 5 分钟超时失败(`context canceled` 于第 ~30 合约)
  → 修正 15 分钟后成功

## 备注
- **期权拉取成本**:受网关快照限频,一次全链拉取 3-9 分钟;按钮
  「拉取中…」置忙,Data 页 30s 轮询不受影响。bars 补数据仍 2 分钟。
- **超时教训**:httpapi 服务端保护超时对期权链要按最坏合约数估算
  (合约数 × 单合约耗时),不能与 bars 同一量级。
- **日线期权数据 × 4h 阈值**:周末/非交易日拉取后 max_ts 停在最后
  交易日(实测 HK.00700 = 周五 07-31),freshness 判定必 stale——
  这是数据粒度与阈值语义的固有不匹配(4h 阈值 × 日线 K),非拉取
  失败;验收以「数据落库」断言而非 fresh。后续可考虑交易日感知
  阈值或按时间帧分级,待拍板(draft 未定)。

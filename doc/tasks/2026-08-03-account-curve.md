# 资产曲线——Dashboard 账户卡曲线 + snapshots 端点 (S-account-curve) — 2026-08-03

状态: ✅ 已合并 (PR #181)

## 背景
AUTO_ADVANCE 根任务循环。资产曲线方向第 2 步(数据层 #179 已备):把
`account_snapshots` 快照画成曲线。参照交易软件惯例(券商 App 账户资产
走势),Dashboard 账户资产卡下加资产曲线;数据层 → 端点 → UI 三段拆分,
本步含端点 + UI。

## 改动
1. **internal/ingest/account.go**:`QueryAccountSnapshots(ctx, db, env,
   limit)` — 最新 N 条(limit≤0 回落 120),反转成 chronological
   (drawSparkline 读 chronological)。
2. **internal/httpapi/account_snapshots.go**:`GET /v1/account/snapshots`
   - env 归一化同 /v1/futu/account(默认 simulate,sim/simulate/paper→
     simulate,real→real,bad→400)
   - limit 1..10000,非法→400;Store 接口 + dbStore 接线
   - 响应 `{env, limit, points:[{captured_at, total_assets, cash,
     market_val}]}` chronological
3. **serveMux 注册** + serve help 文案补端点。
4. **Dashboard 资产曲线**(index.html + app.js):
   - 账户资产卡下 `summary-curve-wrap`(h3 小节 + canvas + 空态提示)
   - `loadSnapSeries(env)` 拉快照序列(失败静默,次要视图不破坏聚合卡)
   - `renderSummaryCurve()`:≥2 点 → drawSparkline(total_assets 时序)+
     时间范围标注(fmtClock 首末点 + 点数);<2 点 → 引导文案
     「可运行 wbot ingest account 开始记录(支持 -every 定时)」
   - env 切换按钮联动刷新曲线(dashEnv 切换后 loadSnapSeries + 重绘)
   - 30s 自动轮询 loadDashboard 已含双 env 序列刷新
5. **测试**:handler 单测 5 场景(fake lister:默认/limit 透传/400×2/
   500/405);webui_test 契约(index.html canvas 三件套 + app.js
   loadSnapSeries/renderSummaryCurve/drawSparkline 断言)。
6. **scripts/accept-account-snapshots-api.sh** 6/6:默认契约
   (simulate/时间递增/total_assets>0)、400×2、real 空点、快照增长 +1
   (ingest account → 端点点数 +1)。

## 验证
- `go test ./...` 全绿;gofmt/vet 干净;node --check app.js
- 本地真实验收(--force 重启 serve——dev-up 内部自建二进制使 md5 比较
  不触发,旧进程 404):端点 6 点 chronological(total_assets=1198286.82),
  real 空点,ingest account 后 5→6;UI embed grep 通过
- CI:首轮 test 挂(漏提交 httpapi_test.go 的 fakeStore 补丁 → 补提后
  5/5 全绿)

## 备注
- **ANSI 噪音**:node console.log 给数字加色码,脚本捕获处 strip_ansi。
- **数据源节奏**:曲线点密度取决于快照频率——`wbot ingest account
  -every 1h`(cron)最自然;DATA_PIPELINE.md 补 cron 示例为后续候选。
- **下一步候选**:DATA_PIPELINE.md 资产快照 cron 示例(文档步);
  之后 UI 对照候选基本收敛,候选池仅剩待老板 7 项 + 微信小程序
  (需凭证)。

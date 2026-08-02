# 中文化死角收尾 (S-UI-chinese-residue) — 2026-08-02

状态: ✅ 已合并 (PR #125, commit 5df1b6e + e5d433d)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨)连续推进:文案中文化
(PR #113)只覆盖了结果/观察/仪表盘三页——admin 页整页仍是英文,且
JS 动态渲染还有多处英文残留。本轮扫描全部 HTML + JS 后一次性收尾。

## 改动
1. **admin.html 全量中文化**:集群节点卡(进程/数据库/数据管道/数据平面)、
   dt(版本/运行时长/监听地址/状态/延迟/运行中/成功/失败/缓存序列/
   过期序列/最新 K 线)、配置节(仅元数据/键/分组/已设置/更新时间/
   无配置键)。
2. **results/watchlist 静态 label**:Symbol → 代码、Strategy → 策略、
   Trades → 交易记录、Params → 参数、Metric → 指标(含两处 fieldset
   legend 静态值)。
3. **app.js 动态残留**(JS 渲染的英文):
   - freshnessCell fresh → 正常(数据过期/无数据早已中文)
   - renderConfig yes/no/not set → 是/否/未设置
   - renderParamFields legend Parameters → 参数
   - renderWatchlist Edit/Delete → 编辑/删除
   - COMPARE_METRICS Equity/Total return/Max drawdown/Bars →
     期末权益/总收益率/最大回撤/K 线数;renderCompare Metric/Params →
     指标/参数
4. 测试: TestAdminPageChinese(中文契约 + 英文残留反向断言)、
   TestJSChineseResidue(动态文案契约)。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 43/43:admin/results/app.js 中文契约 + 英文残留反向 +
  cluster/config 端点契约
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- **CI 门禁教训**:go test 本地全绿但 CI test 挂——gofmt -l 检查
  webui_test.go 未格式化(注释对齐),修复提交 e5d433d 后全绿。以后
  本地 go test 前先 `gofmt -l`。
- config API 顶层是数组(不是 {"keys":...}),契约脚本首版断言错误,
  已修正为 python3 结构断言。
- 中文残留扫描命令:`grep -oE '>[A-Za-z][A-Za-z ]{3,}<' *.html` +
  `grep -nE 'textContent = "[A-Z]' app.js`——HTML 已全干净(仅剩
  wbot/Dashboard/Admin/Data 品牌与导航)。

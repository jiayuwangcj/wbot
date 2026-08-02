# 回测详情页导出 (S-UI-backtest-export) — 2026-08-02

状态: ✅ 已合并 (PR #145, commit c3f24c2)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):回测结果需要汇报/归档,
服务端 GET /v1/backtests/{id}/export(backtest-export 已交付,与 CLI
`wbot backtest -export` 同一 serializer)但 UI 侧无入口——详情页只能
看,拿不到数据文件。补上「配置→回测→看结果→导出」闭环最后一环。

## 改动
1. **results.html**: detail-extra 顶部加 `#detail-export` 导出行
   (muted 小字「导出:」+ CSV/JSON 两个 link 按钮)。
2. **app.js**: `wireExport(id)` 绑定 export-csv/export-json 点击 →
   `location.href = "/v1/backtests/" + id + "/export?format=csv|json"`,
   浏览器直接下载(Content-Disposition attachment,页面不跳转);
   `renderDetail` 每次渲染时重新绑定。
3. 测试: TestBacktestExportJS(11 断言:app.js 6 + results.html 5)。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh smoke 10/10
- 逐端点验收 18/18:serve 实际吐出的契约(11)+ 真实导出下载
  (CSV 200/312 B、JSON 200/763 B,Content-Disposition
  `attachment; filename="backtest-<id>-<strategy>-<date>.<fmt>"` 正确)
- CI: 5/5 全 pass 首轮绿;PR #145 merged

## 备注
- 验收小坑:脚本 `set -u` 下 TMPDIR 未定义会 unbound variable——临时
  文件一律用 `$CLAUDE_JOB_DIR/tmp/` 显式路径。
- 下载走 location.href 直接导航(attachment 不跳页),无需 fetch+blob
  方案,与静态资源下载同款语义。

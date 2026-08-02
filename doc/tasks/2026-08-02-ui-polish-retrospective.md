# UI 打磨会话复盘 (2026-08-02, PR #129-#154)

- **id**: `2026-08-02-ui-polish-retrospective`
- **created**: `2026-08-02`
- **背景**: AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标「参考富途/IB/嘉信
  把交互做好;UI 主题化」。本会话连续 13 个最小可合入闭环(功能 PR +
  docs PR 各一),全部「本地全可用才提交 → 逐端点验收 → 运维沉淀」。

## 交付清单(13 闭环,26 PR)

| # | 功能 | PR | 验收 |
|---|------|----|----|
| 1 | 订单表排序(表头点击,makeTableSorter 三接入) | #129/#130 | 14/14 |
| 2 | 持仓默认市值降序(工厂暴露 renderIndicators) | #131/#132 | 6/6 |
| 3 | 订单默认时间降序 | #133/#134 | 4/4 |
| 4 | Data 覆盖表排序(coverageRows 本地重绘) | #135/#136 | 16/16 |
| 5 | Admin 30s 自动轮询(startAutoRefresh 参数化) | #137/#138 | 9/9 |
| 6 | release deploy 自动清理(--keep 7) | #139/#140 | 7/7 |
| 7 | 回测列表过滤(applyFilter 先过滤后排序) | #141/#142 | 9/9 |
| 8 | watchlist 一键回测(hash 跳详情) | #143/#144 | 8/8 |
| 9 | 回测详情导出 CSV/JSON(wireExport) | #145/#146 | 18/18 |
| 10 | 时间戳本地化(fmtTime,四处时间列) | #147/#148 | 9/9 |
| 11 | 导航当前页高亮(setActiveNav 双向归一) | #149/#150 | 8/8 |
| 12 | 详情「重新运行」(rerunHandler 表单回填) | #151/#152 | 12/12 |
| 13 | Data 补数据 + POST /v1/ingest(接口驱动) | #153/#154 | 13/13 |

全部 CI 5/5 首轮绿;go test 19 包全绿;dev-up smoke 10/10 每轮跑。

## 沉淀的模式(可复用)

- **makeTableSorter 工厂**(#123 起,本会话三接入):`makeTableSorter(tableId,
  getters)` 绑定表头 data-sort;`sorter.state = {key, dir}` 暴露;
  `render`/`renderIndicators` 由调用方注入(注意赋值在 const 声明后,防 TDZ);
  数值列 getter 一律 `?? -Infinity` 沉底,与服务端 NULLS LAST 一致。
- **startAutoRefresh(fn) 参数化**(#137):模块级 autoRefreshFn 记忆,
  可见时才拉、后台停;visibilitychange 恢复时无参调用沿用本页函数。
- **跨闭包桥接 rerunHandler**(#151):init 闭包持有注册表/表单引用,
  模块级 handler 变量由 renderDetail 每次渲染调用并重绑。
- **接口驱动端点**(#153):IngestRunner 同 FutuQuoter 模式——handler 依赖
  接口,测试用 fake runner;serveMux 调用处注入真实实现。
- **验收脚本模式**:写 `$CLAUDE_JOB_DIR/tmp/`(共享 /tmp 会覆盖);
  bash 脚本 mapfile 需显式 `bash script.sh`;契约 grep 用 `grep -qF` 精确
  串;node 单文件内联模拟 UI 逻辑做语义验证。

## 本轮踩坑(已修复并沉淀)

| 坑 | 修复 |
|----|------|
| select 回填不存在的 option 静默失效(buy-hold 非注册表) | strategyByName 守卫 + 降级提示(#151) |
| 导航仅 href 侧归一,直接访问 /ui/index.html 无高亮 | path 与 want 双向归一(#149) |
| set -u 下 TMPDIR 未定义 → unbound variable | 临时文件显式用 $CLAUDE_JOB_DIR/tmp/ |
| 方法限定 pattern("POST /v1/x")的 GET 落 404 catch-all | 无方法限定 + handler 内检查 → 405(#153) |
| 覆盖表时间列 slice(0,16) 原样输出 | fmtTime 本地化(#147) |
| 真实 POST /v1/backtests 503 no_data(标的数据不齐) | 用 dev-up 预置完整组合标的中验证 201(#143) |

## 剩余候选(留档)

- **待老板**:深色主题肉眼验收;富途实盘下单 Web 化(doc/FUTU.md 交易
  安全策略);新鲜度告警推送(等 token)。
- **blocked**: Schwab/IBKR paper(缺凭证);多 symbol 时间对齐(待拍板)。
- **暂缓/低价值**: watchlist/admin 表排序;Data 页自动轮询(克制);
  服务端全库过滤(数据量大再上);ingest Provider 抽象(大工程);
  Data 补数据 201 端到端(需网关凭证)。

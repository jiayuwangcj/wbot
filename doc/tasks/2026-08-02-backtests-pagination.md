# 回测列表 offset 分页 + 「加载更多」 (S-backtests-pagination) — 2026-08-02

状态: ✅ 已合并 (PR #158, commit eaa053d)

## 背景
AUTO_ADVANCE 根任务循环,接 #156(服务端全库搜索)后:列表 hardcode
`limit=50`,数据溢出后老记录不可见(dev 库当时 53 条已溢出)。
补全「过滤 × 排序 × 分页」服务端化最后一环——offset 分页。

## 改动
1. **internal/backtest/store.go**: `ListResults` 追加 `offset` 参数
   (负值 clamp 0,limit<=0 仍 50)。SQL:`LIMIT $n` 参数号取 append
   limit 后的 `len(args)`;**offset > 0 才追加参数与 OFFSET 子句**
   (offset 0 保持历史 SQL 形态)。
2. **internal/httpapi/backtests.go**: `BacktestStore.List` +
   `backtestStore.List` + list handler 透传 `offset`;handler 解析
   `?offset=`:空→0,负/非整数→400 `invalid_request`。
3. **UI(results.html + app.js)**:
   - 表格底部 `#results-more-wrap`「加载更多」按钮,`updateMoreState`
     满 50 倍数才显示(空列表/尾页隐藏)。
   - 点击:拼 `sort/order/q/offset=已加载条数` → `fetchJSON` 追加
     `concat` 渲染;`.finally` 恢复按钮(失败不卡死,错误走
     listError);搜索/排序态与分页同 URL 组合。
   - 搜索框 placeholder 改为「搜索代码/策略(全库…)」。
4. 测试:
   - store 集成:limit=2 两页 q=ZZ 全命中 3 条不重不漏、负 offset
     clamp 0、与 q/精确过滤组合。
   - httpapi 契约 `TestBacktestsListInvalidOffset`(-1/abc/1.5→400
     invalid_request;0→200 passthrough)+ 集成端到端(同 symbol 两
     条,limit=1 两页 id 不同)。
   - webui 契约:results.html `#results-more` + placeholder;app.js
     PAGE_SIZE/满页显示/offset 拼接/concat 追加/「加载中…」。

## 验收
- `go test ./... -count=1` 全绿(19 包,含 WBOT_PG_DSN 集成)
- dev-up.sh --force smoke 10/10
- 逐端点 16/16:UI 契约 6 + 真实 HTTP 9(offset=0 与缺省一致、
  limit=5 两页拼接不重不漏、offset+搜索/排序组合、负值/非整数
  400、越界空页)+ store 集成 + httpapi 集成
- CI: 5/5 全 pass 首轮绿;PR #158 merged

## 备注
- **参数号坑**(本闭环踩到):offset 的 `append` 必须先于 LIMIT 拼接,
  否则 offset=0 时 `LIMIT $len(args)-1` 指向 q 参数(text)→ PG
  "argument of LIMIT must be type bigint"。
- **验收脚本坑**: `curl -sf` 在期望 400 时拿不到错误体 → 用
  `curl -s`;dev 库条数不定 → 分页断言用小 limit 两页拼接而非
  假设满页。
- 前端「加载更多」与「跨页排序/搜索」互斥点:排序/搜索触发的是
  重载(替换),分页触发的是追加;每次重载后 updateMoreState 重算。

# 盈亏/收益率语义色 (S-UI-numcolor) — 2026-08-02

状态: ✅ 已合并 (PR #111, commit 145f5fd)

## 背景
AUTO_ADVANCE 根任务循环 ⑤ UI 打磨连续推进:回测结果子视图(PR #109)之后,
扫描发现 Dashboard 持仓盈亏列与回测收益率列均为纯文本——券商 UI(富途/IB/
嘉信)的基本语义是盈亏正绿负红。orders 表已有 side-buy/side-sell 色,
持仓/收益列缺同一语义。

## 改动
1. **numCell(v, fmt)** (`app.js`): 以**原始数值**着色(>0 → `num-up` 绿 /
   <0 → `num-down` 红),显示用可选 fmt 格式化。关键设计:着色必须用原始
   数值——`fmtPct()` 返回字符串,先格式化再着色会导致 typeof 检查失效、
   色不生效。
2. **接入点**:
   - Dashboard 持仓盈亏列: `numCell(p.pl)`(futu snap 原始数值)
   - 回测结果 total_return 列: `numCell(metricOf(item, "total_return"), fmtPct)`
3. CSS: `td.num-up`/`td.num-down` 用 `--ok`/`--down` 令牌,深色模式自动适配。
4. 测试: `webui_test.go` TestNumCellSemanticColor(JS 契约 + CSS 类名)。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 5/5 + 回测列表 API(total_return 字段)/futu account snap
  (positions 字段)数据路径验证
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- max_drawdown 列保持中性色(负值红色易误导为「亏损警告」,实为风险指标)。
- 实机肉眼验证(headless 只能验契约)留待老板: 深色模式下 num-up/num-down
  与三态令牌一致性。

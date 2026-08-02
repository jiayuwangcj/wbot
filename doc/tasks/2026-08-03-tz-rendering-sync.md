# 闭环 #68: 时间字段渲染实测校正(API.md 12+ 处 Z 字面 vs 全端点 +08 偏移)

- **日期**: 2026-08-03
- **PR**: #254(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE 对账引擎 #65「声明 vs 实际」延续——#66 实测已知 CLI ingest bars 按会话时区渲染(+08),本轮扩展到 HTTP 面:curl 实测 runs/backtests/snapshots/watchlist/bars 全链路,时间字段均按 serve 进程本地时区渲染 RFC3339 偏移(`2026-08-03T05:14:30+08:00`);API.md 12+ 处 `Z` 字面示例与实测不符。

## 改动

- doc/API.md:`/v1/bars` 响应示例改为偏移形态(`2024-06-01T08:00:00+08:00`) + 全局说明「全部端点时间字段按进程本地时区渲染,其余示例 `Z` 仅示意,勿按字面匹配;落库规范见 DATA_STANDARD」
- doc/DATA_STANDARD.md:时间基准节拆两层——「`ts timestamptz` 落库一律 UTC」+「输出面(CLI 打印/HTTP JSON)按进程本地时区渲染偏移(+08,同一瞬时)」

## 验证

- 全端点 curl 实测对照(bars/runs/backtests/snapshots/watchlist);accept 脚本与 Go 测试零 Z 字面输出断言(不敏感);PR #254 CI 5/5 绿

## 备注

- **引擎经验**: 文档示例中的**时间字面**是「声明 vs 实际」高发点——实现按 Go `time.Time.Format(RFC3339)` 渲染带 Location 偏移,文档手写 `Z` 纯属示意。核对时不能只看一处:示例句(64 行)与规范句(24 行)要分开查,全端点逐一 curl 确认无例外(captured_at 注释虽写「RFC3339 UTC」,`Z07:00` 布局在 +08 Location 下实印数字偏移)。
- **候选池**: 仍枯竭(待老板 7 项)。

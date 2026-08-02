# 闭环: doc/issues 草稿状态段悬空引用修复

- **日期**: 2026-08-03
- **PR**: #297(本体)+ 本文档(归档)
- **背景**: 对账引擎新维度「doc/issues 状态段引用的 doc/tasks 归档必须存在」——全量扫描 `grep -rho 'doc/tasks/[0-9a-z-]*\.md' doc/issues/*.md` 对存在性逐条校验,发现 5 处悬空。

## 悬空清单(5 处)

| draft | 原引用(不存在) | 修正为 |
| --- | --- | --- |
| draft-2026-08-02-dashboard.md | `doc/tasks/2026-08-02-dashboard.md` | PR #86 + `2026-08-02-dashboard-autorefresh.md` + `2026-08-03-account-curve.md` |
| draft-2026-08-02-futu-account-web.md | `doc/tasks/2026-08-02-futu-account-web.md` | PR #86 + `2026-08-03-account-curve.md` + `2026-08-03-futu-serve-proxies.md` |
| draft-2026-08-01-strategy-options.md | `doc/tasks/2026-08-01-strategy-options.md` | PR #68/#69 + `2026-08-03-options-ingest-button.md` |
| draft-2026-08-02-backtest-export.md | `doc/tasks/2026-08-02-backtest-export.md` | PR #87 + `2026-08-02-backtest-export-ui.md` |
| draft-2026-08-02-oneclick-backtest.md | `doc/tasks/2026-08-02-oneclick-backtest.md` | PR #75 + `2026-08-02-watchlist-backtest.md` |

## 验证

- verify.sh 连跑两遍全绿(go test 全包 ok)
- 修复后重扫:零 MISSING;docs-only → CI 5/5 绿
- 归档文件存在性核对:`ls doc/tasks/` 对照;PR 号经 commit → pulls API 回查(fcd92da→#69、a7a22bf→#75、f38e73b→#68、export.go→#87、fdc95db→#86)

## 备注

- **引擎经验(引用存在性)**: 状态段引用归档的写法容易顺手写「与文件名同名」的假文件——本批 5 处全是 `draft-XXX.md` 引用同名 `doc/tasks/XXX.md`,而实际归档命名与 draft 不同(或归档分散)。修法不是创建同名归档,而是回查真实闭环记录(commit → PR → 真实归档)。写引用时应先 `ls doc/tasks/` 确认存在,再落笔。
- **sed 陷阱提醒**: `sed -n '/## 状态/,/^$/p'` 遇「标题后紧跟空行」只打印标题,易误判状态段为空——用 `,+8p` 复核。
- **同轮已扫干净面**: BACKTEST.md 指标描述 vs backtest.go 计算(一致);API.md 错误契约(四字段形状/codeForStatus/actionForStatus/503-502 网关分流/catch-all 404)vs httpapi.go(一致)。

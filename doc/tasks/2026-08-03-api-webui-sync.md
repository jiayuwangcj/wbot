# 闭环 #59: API.md Web UI 表同步

- **日期**: 2026-08-03
- **PR**: #236(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 文档对账——#38 刷新 API.md Web UI 表后,又有 #46(Admin 写面)与 #89→960710c(options 链区块加入又删除)两次 UI 演进,表行需重新逐行核对(以实际 html 内容为准)。

## 改动

- **admin.html 行**: 「config 只读」→「status/cluster 只读 + config 写面(config-set-form 键下拉 → PUT /v1/admin/config/{key},值不渲染回显)」——#46 落地后过时
- **孤儿标记清理**: 「slice 12-c」「slice 8-3」全仓库无定义,删除;「S5」类标记有 draft 定义(draft-2026-08-02-backtest-results-api.md),保留
- **watchlist 行核对无欠账**: #89 的 options 链区块已于 960710c(老板指令「不需看盘工具」)移除,行内未提即正确——git 溯源确认(1a94cab 加 → 960710c 删)

## 验证

- docs-only → CI skip 路径 5/5;API.md 无残留 slice 孤儿

## 备注

- **引擎经验**: ①UI 文档对账要「逐行对照实际 html 内容」且用 git 溯源确认功能当前是否还在——「加入过又删除」的功能,文档若提过就是欠账,若没提就是正确;②孤儿标记清理前先验证全仓库无定义(「或验证或改示例」);③可解析引用(S5)与孤儿(slice 8-3)的判别标准是「有无定义指向」。
- **候选池**: 仍枯竭(待老板 7 项 + 微信小程序 blocked)。

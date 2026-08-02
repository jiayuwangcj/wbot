# 闭环 #73: cron 示例 symbol 对齐 DATA_STANDARD 规范格式

- **日期**: 2026-08-03
- **PR**: #263(功能)+ 本文档(归档)
- **背景**: 外部 cron 文档化落地验证(#69 后)——DATA_PIPELINE.md:54 cron 示例命令的 `-url/-symbol/-timeframe/-from/-to` 全部 flag 实测有效;但 symbol `700.HK` 为 MARKET.CODE 反写,DATA_STANDARD 规范为 `HK.00700`(ingest url 不校验格式,两种均可解析)。示例应对齐规范。

## 改动

- doc/DATA_PIPELINE.md cron 示例 `-symbol '700.HK'` → `-symbol 'HK.00700'`

## 验证

- 示例命令实测:flag 解析通过(假 URL 仅网络层失败,证明 flag 面全有效);PR #263 CI 5/5 绿
- 本轮顺带验证干净维度:最近 5 PR 零未处理 review;`-max-age` 文档与实现一致(负数被 flag 层拒绝);Web UI JS 调用 14 个 `/v1/` 端点全部在 serve mux(零死调用)

## 备注

- **引擎经验**: 文档示例(尤其是 cron/脚本类可复制命令)要**实测 flag 面**——假 URL/假数据也能验证「命令能否解析」;示例格式应对齐 DATA_STANDARD 规范,即便实现宽松不校验。
- **候选池**: 仍枯竭(待老板 7 项)。

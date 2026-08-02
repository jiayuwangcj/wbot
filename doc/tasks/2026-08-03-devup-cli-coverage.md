# 闭环 #66: dev-up 补 CLI 三维覆盖缺口

- **日期**: 2026-08-03
- **PR**: #250(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 执行 ACCEPTANCE.md 纪律②「CLI 子命令按 verify.sh/dev-up/accept 三维核对」——ingest file/url/status/bars 三维零覆盖(file/url 写 PG、status/bars 读 PG → 归 dev-up;verify 零依赖、accept 走 HTTP 面均不覆盖 CLI 面)。

## 改动

- scripts/dev-up.sh smoke 加 3 项 check(16→19):ingest file→bars roundtrip(heredoc fixture,CLI.US)、ingest url→bars roundtrip(python3 http.server 就地供数,CLIURL.US)、ingest status 列最近 runs
- 数字同步:README.md / RELEASE_DAILY.md / ACCEPTANCE.md 16→19(ACCEPTANCE 冒烟覆盖说明列补 CLI 三维补漏)
- 实测坑注释进脚本:①ingest file/url 无 -adjust(恒 none,ingest bars 默认 fwd,查询须显式 -adjust none)②ingest bars 按会话时区渲染(+08,grep 日期前缀)

## 验证

- dev-up.sh 本地二连跑 ALL 19 CHECKS PASSED;CI 全量 test 1m53s + db-integration 1m29s + governance 全绿

## 备注

- **引擎经验**: 纪律②的完整执行——先枚举全部 CLI 子命令(源码 dispatch),再按三维逐条 grep 字面命令;零覆盖才算欠账。修缺口时注意「命令的实际行为细节」(adjust 默认值、时区渲染)以实测为准,不能凭文档印象写断言。
- **候选池**: 仍枯竭(待老板 7 项)。

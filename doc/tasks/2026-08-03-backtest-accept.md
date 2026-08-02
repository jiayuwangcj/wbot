# 闭环 #47: backtest 子系统端到端验收

- **日期**: 2026-08-03
- **PR**: #214(功能)+ 本文档(归档)
- **背景**: 「验收覆盖扩展」引擎对账发现——backtest 是核心子系统,但结果详情/导出面(`GET /v1/backtests/{id}`、`/{id}/export`、CLI `-save`/`-export`)只有单测 + dev-up 冒烟(POST 201 状态码),无真实 HTTP 契约的 accept 脚本。#42 清零声明只覆盖**子命令级**(paper 是最后一个无脚本的子命令),backtest **数据面**漏了——#41 引擎经验「凡有真实 HTTP/CLI 契约的子系统都要有自己的 accept 脚本;启动冒烟 ≠ 验收」未贯彻到 HTTP 数据面。

## 改动

新增 `scripts/accept-backtest.sh`(19 项检查,参数 base/bin/dsn/symbol,默认 BTEXEC.US——dev-up 种子唯一带 fwd 1d bars 的 symbol):

- **CLI**: `-dsn` 真数据运行 exit 0 + summary 形状(`final_equity=… total_return=… max_drawdown=… bars=N`);`-save` 落库并打印 `saved result id`;`-export` 不存在 id → exit 1
- **详情**: GET /v1/backtests/{id} 形状(id/strategy/symbol/metrics 键 total_return/max_drawdown/equity/bars + equity_curve≥2 + trades≥1)
- **文档声称的字节一致等价 ×4**(#44 经验延伸: 声称的等价关系不止 verify.sh≡ci.yml,数据面 roundtrip 声称同样要对账):
  - ① POST exec 201 body == GET detail(共享 serializer `backtest.Detail`)
  - ② CLI `-export` csv == GET export 字节一致(API.md roundtrip 契约)
  - ③ GET export?format=json == GET detail(export.go 注释声称)
  - ④ CLI `-export` json == GET export?format=json
- **错误契约**: id=0/abc → 400;99999999 → 404(detail 与 export);format=bogus → 400

## 验证

- dev-up 环境连跑两遍 19/19 稳定
- 实测中修正脚本自身两处断言: metrics 键是 `equity` 非 `final_equity`(CLI 输出字段名 ≠ JSON 字段名);等价③必须比**同一条记录**的 detail(首版误用 POST 记录比较导出)
- verify.sh 全绿 + CI 5/5

## 备注

- **引擎经验**: ①验收扩展对账粒度要分两层——子命令级(CLI 有无脚本)与**数据面级**(HTTP 结果/导出面有无 e2e);②「字节一致/roundtrip」声称也是对账对象,且验收脚本本身能抓住自己的断言错误(比错记录、错字段名),跑两遍的稳定性验证有效。
- **候选池**: 探索引擎本轮全维度扫描(端点对账/dev-up 覆盖/账户卡明细/期权链删除一致性/长任务忙态/AUTO_ADVANCE ①③④⑤/doc/issues 13 draft)后仅此一缺口;此闭环后 backtest 数据面与 CLI 均有 e2e 验收。

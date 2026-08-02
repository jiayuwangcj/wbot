# 闭环 #52: 自包含 accept 脚本并入远程 CI 门

- **日期**: 2026-08-03
- **PR**: #224(功能)+ 本文档(归档)
- **背景**: 12 个 accept 脚本(122 项检查)全部只本地跑,CI 只有单测 + binary smoke——「提交前验收」规则(本地全可用 + 逐端点验收)无远程守卫,依赖 Agent 自觉。其中两个脚本零外部依赖(无网络/DB/网关),完全可提升为 CI 门禁。

## 改动

- **ci.yml** test job 新增步骤「Run self-contained acceptance scripts」:
  - `scripts/accept-paper.sh`(12 项,纯本地)+ `scripts/accept-agent-federation.sh`(11 项,go+curl,in-memory 注册表)
- **verify.sh** 同步加入相同两行调用——保持「verify.sh ≡ ci.yml test」**双向**等价(#44 对账纪律:verify 缺 CI 项要补,CI 新步骤 verify 也要有)

## 验证

- verify.sh 全链本地通过(全包单测 + 新 accept 步骤)
- CI test job 1m47s 含新步骤全过,5/5 绿

## 备注

- **引擎经验**: 验收从「本地纪律」到「远程强制」是分层:零依赖脚本(paper/agent-federation)可提升进 CI;PG 依赖脚本(backtest/watchlist 等)留在本地(CI db-integration 有 postgres service,理论上可扩,但种子数据与真实网关不可得——留作后续);真实网关脚本(futu 系)永不进 CI。下一候选:db-integration job 的 postgres service 已就位,本地 seed 流程(dev-up 种子 bars/options)若脚本化,backtest 验收可远程化。
- **候选池**: 仍枯竭(除上述种子远程化候选——依赖既有种子数据;查看 dev-up 种子机制后评估,本轮不扩 scope)。

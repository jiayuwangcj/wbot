# 闭环 #50: wbot futu CLI 子命令端到端验收

- **日期**: 2026-08-03
- **PR**: #220(功能)+ 本文档(归档)
- **背景**: futu CLI 面(status/quote/funds/position/order)是最后一个零 e2e 覆盖的 CLI 面——只有单测,无冒烟无 accept。`scripts/accept-account-snapshot.sh` 注释声称「与 `wbot futu funds -env real` 同安全面」,暗示 funds 已被覆盖,但 funds CLI 命令本身零 e2e——#48 教训(注释写明分工处即验收盲区)的同类。

## 改动

新增 `scripts/accept-futu-cli.sh`(21 项检查,连真实网关 192.168.215.2):

- **成功形状**: status exit 0 + health=ok;quote basic_qot_list[0].cur_price 数值
- **env 通道**: funds/position 的 simulate/real 双通道均验证(real 走真实账户)
- **错误契约**: quote 缺/非法 symbol → exit 2;funds -env bogus → exit 2;order 缺 symbol / qty=0 / 非法 side → exit 2
- **安全红线**: order -env real 无 -live-confirm → exit 2 拒绝;有 live-confirm 但无 -acc-id → exit 2 拒绝
- **运行时错误**: 网关不可达 → exit 1

order 只测 -dry-run 与校验/红线拒绝路径——真实下单是写操作,刻意不自动执行(accept 脚本绝不下真单)。

## 验证

- dev-up 环境连跑两遍 21/21 稳定
- 实测修正断言: CLI quote 输出为 basic_qot_list 数组形状(与 HTTP 面一致),首版误按扁平 cur_price 断言
- CI 5/5

## 备注

- **引擎经验**: 验收对账不能只看「子系统有无脚本」,要按 CLI 子命令级 × HTTP 端点级双面对账。至此 futu 子系统 CLI + HTTP 双面均有端点级 e2e;全部 CLI 子命令(serve/ingest/futu/backtest/paper/watchlist/agent/configyaml/master)均有 e2e 或冒烟验收。
- **候选池**: 仍枯竭。

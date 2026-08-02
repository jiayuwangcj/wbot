# 闭环 #45: ROADMAP v3 行与 Futu 接入现状同步

- **日期**: 2026-08-03
- **PR**: #210(文档单独 PR,无归档 PR——功能即文档)
- **背景**: 「文档欠账对账」引擎例行对账 ROADMAP: v3「执行路径」行仍写「券商候选: Futu / IBKR / Schwab(凭证齐备后接入)」「持仓数据读取接口缺券商凭证, blocked」——但 Futu 网关接入早已落地(`wbot futu order` sim 默认 real 需 -live-confirm、`position`、`funds`、`ingest account` 快照,2026-08-03 实测,FUTU.md §9)。过时表述会让新读者误判券商侧未开始。

## 改动

`doc/ROADMAP.md` v3 行:

- Futu 已接入(OpenD 网关: CLI 下单/持仓/资金快照,2026-08-03 实测,见 [[FUTU]])
- IBKR / Schwab 待凭证(见 discussions/10)
- 「持仓数据读取接口 blocked」改为「持仓数据 Web 化(微信小程序前置依赖;Futu 持仓已可读,Web 化/实盘下单 Web 化待老板拍板)」——同步 discussions/21 待老板 7 项中的「实盘下单 Web 化」

## 验证

- Doc-only;CI 5/5 全绿

## 备注

- **引擎经验**: ROADMAP 也属对账对象——「状态」与「后续阶段」表都要对照实际实现逐行核对;过时描述会误导新读者对工程成熟度的判断。
- **候选池**: 仍枯竭。

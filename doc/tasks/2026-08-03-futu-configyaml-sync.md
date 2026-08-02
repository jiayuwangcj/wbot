# 闭环 #67: FUTU.md §7 config.yaml「待后续切片」过时表述修正

- **日期**: 2026-08-03
- **PR**: #252(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 对账引擎 #65「声明 vs 实际实现」——FUTU.md:178 称「config.yaml 的 `futu` 配置接入待后续切片」,但 configyaml 自 2026-08-01 起已渲染 `~/.wbot/config.yaml`,§1 完整记录 config.yaml → `tools/config-to-env.sh` → compose env 注入流程;serve 代理地址走 `FUTU_GATEWAY_URL`/`FUTU_PROTO_ADDR` env(§7 表)。「待后续切片」与现状矛盾。

## 改动

- doc/FUTU.md §7 网关地址句:改为「CLI 直跑暂用 `-addr` flag(默认 22222);compose/serve 场景的 `futu` 配置经 config.yaml 注入——configyaml 渲染 + config-to-env.sh → env(见 §1);serve 代理地址见下表 `FUTU_GATEWAY_URL`/`FUTU_PROTO_ADDR`」
- 核实路径:accept-futu-cli.sh 以 `-addr` 传参(默认取 `$FUTU_GATEWAY_URL`);serve 端读 env;configyaml 已实现(4b066a0 起)

## 验证

- grep 全库「待后续切片」零残留;PR #252 CI 5/5 绿(doc-only,check-skip 白名单命中)

## 备注

- **引擎经验**: 「待后续切片」「暂用」「待扩展」等时间性表述是文档欠账高发点——triage 时 grep 关键词定位,再逐条对照「是否已实现」。本闭环 CLI `-addr` 为真实现状,保留;config.yaml 接入已实现,修正。
- **候选池**: 仍枯竭(待老板 7 项)。

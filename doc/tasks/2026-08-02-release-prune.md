# 日构建目录自动清理 (S-ops-release-prune) — 2026-08-02

状态: ✅ 已合并 (PR #139, commit b0e45b2)

## 背景
遗留候选(早于本窗口):`~/.wbot/releases/` 日构建目录每日 1 个持续
堆积,无清理策略(31M/2 目录时已明显,长期必膨胀)。按「运维脚本必须
沉淀」验收规则,清理逻辑进 `release.sh deploy` 本身——部署即维护,
无需额外 cron。

## 改动
1. **scripts/release.sh**:
   - 新参数 `--keep N`(默认 7):deploy 成功后按名序保留最新 N 个
     `daily-*` 目录;`--keep 0` 禁用清理。
   - prune 段:find + sort + mapfile 循环删旧(`daily-YYYYMMDD` 定长
     名序=时间序,无需解析日期);正式版 `v*` 目录永不触碰。
   - usage/help 文本同步。
2. **doc/RELEASE_DAILY.md**: deploy 说明与规则节同步(本地清理自动化,
   GitHub Release 侧仍由 engineering-admin 发起)。

## 验收
- `bash -n` 语法检查
- 沙箱真实 deploy 全链路(下载 linux_amd64 + SHA256SUMS 校验 + 解压 +
  prune)7/7:prune 生效、保留集正确、`--keep 0` 禁用、`v*` 保留
- CI: 5/5 全 pass 首轮绿

## 备注
- 验收小坑:grep "prune" 误命中沙箱路径 `tmp/prune-accept`(deploy 首行
  输出含 `-> <dir>` 绝对路径)——精确模式 `"release: prune"`。
- 验证脚本用 zsh 跑 `mapfile` 报 command not found——脚本是 `#!/bin/bash`,
  沙箱验证必须显式 `bash -c`。
- 保守语义:只删 `daily-*`,正式版与任何非标准目录不动;默认留 7
  (一周日构建),可用 --keep 0 临时关闭。

# 日构建/版本发布流程复盘（2026-08-02）

- **id**: `2026-08-02-release-process-retrospective`
- **created**: `2026-08-02`
- **updated**: `2026-08-02`
- **背景**：用户触发一次「跑版本」，执行链路多次手忙脚乱、来回等待。用户明确要求：**不希望每次都等待我们完成**。

## 发生了什么（按时间线）

1. 当日「跑版本」= 发布日构建 `vdaily-20260802` 并部署到 `~/.wbot/releases/`。当时流程全部靠手工：gh 命令敲、下载解压手写、网络地址现场摸索。
2. **PR 卡 governance**（最痛，浪费约 40 分钟）：PR body 用了中文「驱动:」而非字面 `Driven-By:` → governance 检查 grep 失败 → 修 body 后发现 `gh run rerun --failed` 重放的是旧事件 payload（旧 body）→ 只能 push 空 commit 触发新 synchronize 事件才重新跑过。
3. **直接 push main 被拒**：受保护分支（GH006 + 3 个 required status checks），必须临时改走分支+PR。
4. **网络地址现场发现**：宿主机端口未绑定，PG/OpenD 只能经 OrbStack bridge 容器 IP 直连；没有文档，每轮部署都要 docker inspect 重新摸。
5. **deploy 校验 bug**：手写校验拿 `sha256sum -c SHA256SUMS` 全量比对，但只下载了 linux_amd64 一个产物 → 4 个缺失条目报错 → 部署中断在解压前。
6. 其他小坑：`gh pr edit --body-file` 被 GitHub Projects classic 弃用 bug 卡死（exit 1）；并行作业共享 `/tmp` 互相覆盖；zsh glob 未加引号报错。

## 根因

- **流程没有沉淀成脚本/文档**：发布、部署、验收每一步都依赖现场记忆和临场发挥。
- **PR 规则不强制执行**：body 锚点是事后补救的认知，不是创建时的 checklist。
- **环境事实（容器 IP、DSN）散落在各人脑内**，没有落盘。
- **没有验收闭环**：部署完没人确认「这个产物真的能跑」— 今天补上了。

## 已落地修复（全部实机验证过）

| 问题 | 修复 | 位置 |
|---|---|---|
| 发布/重发全靠手工 | `republish` 子命令：删旧 release/tag → 重打 tag 到 HEAD → 重建（仅限 `vdaily-*`，build 前快速失败） | `scripts/release.sh` |
| 部署全靠手工 | `deploy` 子命令：下载 → 只校验已下载条目（SHA256SUMS 全量含 5 平台）→ 解压 → 清理 tar.gz | `scripts/release.sh` |
| 起环境靠回忆 | `dev-up.sh`：自动发现容器 IP → 起 serve → 幂等种子（回测 fwd bars）→ 10 项验收 smoke | `scripts/dev-up.sh` |
| 网络事实不落盘 | OrbStack 容器 IP/DSN 实测写入 RELEASE_DAILY.md；一律以 dev-up.sh 自动发现为准 | `doc/RELEASE_DAILY.md` |
| PR body 锚点踩坑 | PR 创建时 body 即含字面 `Driven-By:`；创建后不改 body（不重跑 governance），要改就 push 空 commit | [[WORKFLOW_GITHUB_DRIVEN]] |

## 现在的发布协议（用户触发后应自主跑完）

```bash
# 1. 验收（本地环境必须先全绿）
scripts/dev-up.sh            # 10/10 通过后才允许提交远端

# 2. 发布/刷新日构建
scripts/release.sh publish --version daily-YYYYMMDD    # 首次
scripts/release.sh republish --version daily-YYYYMMDD   # 当日已有 tag

# 3. 部署 + 验收产物
scripts/release.sh deploy --version daily-YYYYMMDD
# 产物 serve :8081 跑 10 项验收（rel-accept 模式），通过后停机

# 4. 留痕
# discussions/9 记 tag/URL/部署结果
```

实测耗时：republish ≈ 3 分钟（含 cross-build + gh），deploy ≈ 30 秒，dev-up smoke ≈ 30 秒。用户触发后到全部完成应在 ~10 分钟内，且每一步失败都能从脚本错误信息直接定位，无需等待人肉接力。

## 遗留改进

- 正式版（`v1.x.y`）的发布路径仍建议走 PR + tag；republish 守卫已禁止对正式版误操作。
- 部署目录 `~/.wbot/releases/` 的清理策略（多日累积）未定。

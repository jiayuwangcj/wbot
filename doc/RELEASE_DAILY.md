# 版本发布（合批）与本地部署（Release by Batch）

2026-08-11 用户指令：**废除日版本发布机制**（`daily-YYYYMMDD` tag 流程废止）。发布改为：reviewer 判定交付物是功能迭代（feature）还是 bugfix；**功能迭代在运维空闲时合批发布**，bugfix 可及时发布，始终保持可用；发布遇障碍由运维提发布 bug 需求给开发解决。

## 功能类型判定（reviewer 职责）

评审报告必须包含功能类型判定：

- **bugfix**：修复缺陷、恢复既有契约/可用性，不引入新行为
- **feature**（功能迭代）：新能力、行为变化、契约扩展

## 发布时机

- **bugfix**：可及时发布（保持可用优先）
- **feature**：**运维空闲时合批发布**——多个功能迭代合入主基线后攒批，由 PM 组/主会话与运维商定批次，一次发布
  - 版本号：正式语义化版本 `vX.Y.Z`（`scripts/release.sh publish --version vX.Y.Z`），不再使用 `daily-*` 版本
  - 版本说明登记 Release 模板，指向批内对应 Issue/Discussion/任务记录

## 发布流程

1. **合批条件**：批内功能均通过评审并合入主基线；发布前 CI 绿
2. **发布**：
   ```bash
   scripts/release.sh publish --version vX.Y.Z   # cross-build 到 dist/ + GitHub Release + tag
   ```
   （环境变量 `GH_TOKEN="$(env -u GITHUB_TOKEN gh auth token)"`；发布动作对仓库外影响大，先征得主会话确认）
3. **运维组 operator 部署**（scripted，自动下载 linux_amd64 产物 → 校验 SHA256SUMS（仅校验已下载条目，SHA256SUMS 全量含 5 平台）→ 解压到发布目录）：
   ```bash
   scripts/release.sh deploy --version vX.Y.Z
   # -> ~/.wbot/releases/vX.Y.Z/wbot   (tar.gz 已删，SHA256SUMS 留存)
   # deploy 自动清理旧版本:仅保留最新 7 个目录(--keep N 覆盖)
   ```
4. **发布障碍**：operator 提发布 bug 需求（GitHub issue，标题带 `[bug]`，正文含复现步骤/日志）→ 开发组修复 → 重新发布
5. **留痕**：版本号/tag/URL 与部署结果记录到进度贴（discussions/9）或当日任务记录

## 本地部署环境（OrbStack，2026-08-02 实测）

- **容器地址**：宿主机端口未绑定，须经 OrbStack bridge 容器 IP 直连：
  - `wbot-pg-ci-test` → `192.168.215.5:5432`（库 `wbot_test`，`postgres/postgres`）
  - `futu-opend-rs` → `192.168.215.2`（REST `:22222`，proto `:11111`）
- **实测 DSN**：`postgres://postgres:postgres@192.168.215.5:5432/wbot_test?sslmode=disable`
- **一键起环境**：`scripts/dev-up.sh` 自动发现上述地址、起 serve、幂等种子演示数据（回测需 fwd bars）、跑 25 项验收冒烟；serve 重启用 `--force`
- IP 可能随容器重建变化——一律以 dev-up.sh 的自动发现为准，不要手写进脚本

## 规则

- 版本号走正式语义化 `vX.Y.Z`；`daily-*` 仅指历史日构建产物（审计保留，清理归 engineering-admin）
- 发布前必须：批内功能评审通过 + CI 绿；发布不破坏可用性
- 部署目录 `~/.wbot/releases/` 属敏感目录约定（[[PRIVACY]]）：只放构建产物与配置，不提交仓库

## 关联

- [[ORGS]]（组织架构与并行协议；版本发布（合批）节）、[[ROADMAP]]、[[PRIVACY]]
- 运维组角色：`.claude/agents/ops/operator.md`

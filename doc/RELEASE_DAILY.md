# 日构建与本地部署（Daily Release）

2026-07-31 用户指令：**每日版本计划**——每日 0 点后高优先级完成阻碍性工作，完成后发布日构建 tag，运维拿到日版本后部署到本地（发布目录）。

## 每日流程

1. **0 点后**（北京时间）：PM 组 manager 评审阻碍性工作（CI 红 / 阻断合入 bug / 解除 blocked 的最小改动）——高优先级完成，不因成本时段顺延
2. **阻碍性工作清零后**：主会话/operator 发布日构建：
   ```bash
   scripts/release.sh publish --version daily-YYYYMMDD   # 首次发布，如 daily-20260801
   scripts/release.sh republish --version daily-YYYYMMDD # 当日已有 tag 时：重打 tag 到 HEAD 并替换 release
   ```
   （release.sh 自动 cross-build 到 `dist/` 并创建 GitHub Release + tag；环境变量 `GH_TOKEN="$(env -u GITHUB_TOKEN gh auth token)"`。republish 仅接受 `vdaily-*` 版本，正式版走新版本号）
3. **运维组 operator 部署**（scripted，自动下载 linux_amd64 产物 → 校验 SHA256SUMS（仅校验已下载条目，SHA256SUMS 全量含 5 平台）→ 解压到发布目录）：
   ```bash
   scripts/release.sh deploy --version daily-YYYYMMDD
   # -> ~/.wbot/releases/daily-YYYYMMDD/wbot   (tar.gz 已删，SHA256SUMS 留存)
   # deploy 后自动清理旧日构建:仅保留最新 7 个 daily-* 目录(--keep N 覆盖),
   # 正式版 v* 目录永不触碰
   ```
4. **留痕**：日构建 tag/URL 与部署结果记录到进度贴（discussions/9）或当日任务记录

## 本地部署环境（OrbStack，2026-08-02 实测）

- **容器地址**：宿主机端口未绑定，须经 OrbStack bridge 容器 IP 直连：
  - `wbot-pg-ci-test` → `192.168.215.5:5432`（库 `wbot_test`，`postgres/postgres`）
  - `futu-opend-rs` → `192.168.215.2`（REST `:22222`，proto `:11111`）
- **实测 DSN**：`postgres://postgres:postgres@192.168.215.5:5432/wbot_test?sslmode=disable`
- **一键起环境**：`scripts/dev-up.sh` 自动发现上述地址、起 serve、幂等种子演示数据（回测需 fwd bars）、跑 10 项验收 smoke；serve 重启用 `--force`
- IP 可能随容器重建变化——一律以 dev-up.sh 的自动发现为准，不要手写进脚本

## 规则

- 日构建 tag 格式固定 `daily-YYYYMMDD`（与正式语义化版本 `v1.x.y` 区分）
- 无阻碍性工作/无新提交的凌晨：仍发 tag 或以最近一次日构建为准（PM 判断，留痕说明）
- 正式版本发布（release.sh --version vX.Y.Z）时：日构建 tag 保留；本地 `~/.wbot/releases/` 由 deploy 自动清理（保 7，见上），GitHub 侧 Release 累积过多后由 engineering-admin 发起清理
- 部署目录 `~/.wbot/releases/` 属敏感目录约定（[[PRIVACY]]）：只放构建产物与配置，不提交仓库

## 关联

- [[ORGS]]（组织架构与并行协议；每日版本计划节）、[[ROADMAP]]、[[PRIVACY]]
- 运维组角色：`.claude/agents/ops/operator.md`

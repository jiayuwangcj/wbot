# 日构建与本地部署（Daily Release）

2026-07-31 用户指令：**每日版本计划**——每日 0 点后高优先级完成阻碍性工作，完成后发布日构建 tag，运维拿到日版本后部署到本地（发布目录）。

## 每日流程

1. **0 点后**（北京时间）：PM 组 manager 评审阻碍性工作（CI 红 / 阻断合入 bug / 解除 blocked 的最小改动）——高优先级完成，不因成本时段顺延
2. **阻碍性工作清零后**：主会话/operator 发布日构建：
   ```bash
   scripts/release.sh publish --version daily-YYYYMMDD   # 如 daily-20260801
   ```
   （release.sh 自动 cross-build 到 `dist/` 并创建 GitHub Release + tag；环境变量 `GH_TOKEN="$(env -u GITHUB_TOKEN gh auth token)"`）
3. **运维组 operator 部署**：下载/获取日版本产物 → 部署到本地发布目录：
   ```
   ~/.wbot/releases/daily-YYYYMMDD/
   ```
   （解压 tar.gz/zip、校验 SHA256SUMS、放置 `wbot` 二进制与示例配置；部署细节按实际环境补充）
4. **留痕**：日构建 tag/URL 与部署结果记录到进度贴（discussions/9）或当日任务记录

## 规则

- 日构建 tag 格式固定 `daily-YYYYMMDD`（与正式语义化版本 `v1.x.y` 区分）
- 无阻碍性工作/无新提交的凌晨：仍发 tag 或以最近一次日构建为准（PM 判断，留痕说明）
- 正式版本发布（release.sh --version vX.Y.Z）时：日构建 tag 保留；累积过多后由 engineering-admin 发起清理
- 部署目录 `~/.wbot/releases/` 属敏感目录约定（[[PRIVACY]]）：只放构建产物与配置，不提交仓库

## 关联

- [[ORGS]]（组织架构与并行协议；每日版本计划节）、[[ROADMAP]]、[[PRIVACY]]
- 运维组角色：`.claude/agents/ops/operator.md`

# 外部 cron 续跑（cron-claude-continue）

2026-08-01 文档化：`scripts/cron-claude-continue.sh` 是**进程外守护**——用系统 crontab 周期性拉起 Claude Code headless 会话续接最近会话并推进工程，无需人工驻留终端。

## 用法

```crontab
# 每 5 分钟检查一次（间隔可改）；日志落 ~/.cache
*/5 * * * * /home/jiayu/workspace/github/wbot/scripts/cron-claude-continue.sh >>"$HOME/.cache/wbot-claude-cron.log" 2>&1
```

依赖：PATH 中有 `claude`（npm 全局或 nvm bin）；headless 认证可用（登录态或 ANTHROPIC_AUTH_TOKEN）。

## 行为

- 若本机**存在任何 claude 进程**（交互会话/后台会话/其他项目）→ 退出（避免重复占用）
- 否则 headless 执行 `claude -p --continue --permission-mode bypassPermissions "continue"`——续接最近会话继续推进（跑完即退出，由 cron 反复拉起）

## 与 /loop 的关系

- **/loop**（会话内定时器）：会话存活期间每 N 分钟注入一次推进指令；**会话退出即失效**
- **cron-claude-continue**（进程外）：会话死亡后由 cron 重新拉起——两者互补：loop 保证存活期节奏，cron 保证死后复活
- 本仓库惯例：主会话内用 `/loop`（每小时或每 5 分钟，见 [[ORGS]] 成本时段），cron 作为兜底

## 注意事项

- pgrep 检测为「任何 claude 进程」级别的粗粒度：若其他项目会话活着而本仓库会话已死，需手动 `claude --continue` 或临时移除 pgrep 条件
- `bypassPermissions` 仅适用于可信环境；headless 跑长任务注意 token 预算（见 [[ORGS]] 成本时段）
- 日志排查：`tail -f ~/.cache/wbot-claude-cron.log`

## 关联

- [[AUTO_ADVANCE]]（根循环）、[[ORGS]]（组织架构/成本时段）、[[WORKFLOW_GITHUB_DRIVEN]]

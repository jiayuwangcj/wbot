#!/usr/bin/env bash
# Crontab helper: cd 到本仓库根目录；若本机已有 claude 会话在运行，则退出；否则
# headless 执行 `claude -p --continue` 续接最近会话并继续推进（跑完即退出，由 cron
# 反复拉起）。
#
# 示例 crontab（每 5 分钟检查一次，可按需改间隔与日志路径）：
#   */5 * * * * /home/jiayu/workspace/github/wbot/scripts/cron-claude-continue.sh >>"$HOME/.cache/wbot-claude-cron.log" 2>&1
#
# 依赖：PATH 中可找到 Claude Code 的 `claude`（npm 全局或 nvm bin 均可）；headless
# 认证可用（ANTHROPIC_AUTH_TOKEN 或已登录态）。
#
# 检测说明：只要本机存在任何 claude 进程（交互会话、daemon/后台会话、其他项目会话）
# 即视为「有人在跑」而不拉起，避免重复占用；若本仓库会话已死而其他会话活着，手动
# 用 `claude --continue` 或删掉本脚本的 pgrep 再跑一次即可。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

if ! command -v claude >/dev/null 2>&1; then
  echo "cron-claude-continue: claude not found in PATH" >&2
  exit 1
fi

if pgrep -f 'claude' >/dev/null 2>&1; then
  exit 0
fi

# -p：非交互 print（headless）；--continue：续接最近会话（等同输入 continue）；
# --permission-mode bypassPermissions：headless 下避免授权询问。
exec claude -p --continue --permission-mode bypassPermissions "continue"

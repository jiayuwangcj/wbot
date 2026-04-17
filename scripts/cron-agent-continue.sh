#!/usr/bin/env bash
# Crontab helper: cd 到本仓库根目录；若尚未有针对本仓库的 agent 进程，则执行 agent（headless）。
#
# 示例 crontab（每 5 分钟检查一次，可按需改间隔与日志路径）：
#   */5 * * * * /home/jiayu/workspace/github/wbot/scripts/cron-agent-continue.sh >>"$HOME/.cache/wbot-agent-cron.log" 2>&1
#
# 依赖：PATH 中可找到 Cursor 的 `agent`（常见为 ~/.local/bin/agent）；需要已配置 CURSOR_API_KEY 等认证方式。
#
# 若本机任意时刻只有一个 agent，可在 crontab 里设置：
#   export MATCH_ANY_AGENT=1
# 则仅当不存在名为 agent 的进程时才启动（见下方检测逻辑）。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

if ! command -v agent >/dev/null 2>&1; then
  echo "cron-agent-continue: agent not found in PATH" >&2
  exit 1
fi

if [[ "${MATCH_ANY_AGENT:-0}" == "1" ]]; then
  if pgrep -x agent >/dev/null 2>&1; then
    exit 0
  fi
else
  # 默认：命令行里含本仓库路径的 agent 视为已在本工程运行（需本脚本拉起的进程带 --workspace）。
  if pgrep -a -f '/agent' 2>/dev/null | grep -Fq -- "$PROJECT_ROOT"; then
    exit 0
  fi
fi

# -p：非交互 print；--workspace：固定工作区；--trust：headless 下信任工作区（见 agent --help）。
# 若需「续上上一次会话」而非把 continue 当 prompt，可改为增加 --continue（按 CLI 版本为准）。
exec agent -p --trust --workspace "$PROJECT_ROOT" "continue"

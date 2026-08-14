#!/usr/bin/env bash
# Acceptance: deterministic ES tactical parameter training (no external DB).
# Usage: scripts/accept-backtest-train.sh [wbot-bin]
set -uo pipefail

pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [[ -n "${1:-}" ]]; then WB="$1"; else go build -o "$tmp/wbot" ./cmd/wbot && WB="$tmp/wbot"; fi

go test ./internal/backtestes -run '^TestSearchDeterminismBestAtLeastPopulationMeanAndDistinctSeeds$' -count=1 >/dev/null; deterministic_code=$?
check "同 seed 同 generations/candidates 轨迹" 0 "$deterministic_code"
check "最优个体 score ≥ 种群均值" 0 "$deterministic_code"
check "训练 seed 与验证/测试 seed 隔离" 0 "$deterministic_code"

go test ./internal/backtestes -run '^TestEarlyStop$' -count=1 >/dev/null; early_code=$?
check "早停显著早于最大代数" 0 "$early_code"
go test ./internal/backtestes -run '^TestSplitWindowsIsChronologicalWithoutLeakage$' -count=1 >/dev/null; windows_code=$?
check "walk-forward 时间顺序且无重叠" 0 "$windows_code"

params='{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}'
bad="$($WB backtest -dsn 'postgres://invalid' -symbol HK.00883 -strategy wheel -params "$params" -train '{"max_inventory":[100,200]}' 2>&1)"; bad_code=$?
check "-train 拒绝战略参数" 2 "$bad_code"
check "战略参数错误在连接 DB 前返回" 1 "$(grep -q 'not a tactical parameter' <<<"$bad" && echo 1 || echo 0)"

probe="$($WB backtest -dsn 'postgres://127.0.0.1:1/nope?connect_timeout=1' -symbol HK.00883 -strategy wheel -params "$params" -train '{"move_interval_pct":["0.005","0.03"]}' -population 16 -max-generations 2 -budget 64 -train-timeout 1s 2>&1)"
check "启动 DB 前输出预计评估次数" 1 "$(grep -q '预计评估次数=' <<<"$probe" && echo 1 || echo 0)"

echo
if [[ "$failed" == 0 ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

#!/usr/bin/env bash
# Acceptance: Wheel 只读审计数据面 (wheel_configs / wheel_signals /
#   wheel_signal_actions)。
# 通过产品写面 PUT /v1/watchlist 制造不可变配置版本(重复 PUT 追加版本、
#   历史版本原文保留),验证 configs 列表/过滤与 400 契约、signals 的
#   action/capability 过滤与 400 契约、actions 端点、空集合 [],以及
#   POST 405。dev 环境无真实 snapshot → 信号行按设计为空,故 signals
#   侧验证错误契约与空态。wheel_configs 是 append-only 且无删除端点,
#   故每轮使用时间戳唯一的 symbol 保持断言精确且产物自标识;结束删除
#   watchlist 绑定(配置版本按契约保留为审计证据,不清除)。
#
# Usage: scripts/accept-wheel-audit.sh [base-url]
# Requires: curl, a running `wbot serve` (scripts/dev-up.sh)。
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
# 用 PID 保证同秒内连续执行也拿到唯一 symbol(秒级时间戳会被复用)。
sym="AUDCFG$$.US"
params_a='{"strategy":"wheel","params":{"full_position_price":90,"zero_position_price":130,"max_inventory":100,"trade_gap":10}}'
params_b='{"strategy":"wheel","params":{"full_position_price":90,"zero_position_price":130,"max_inventory":100,"trade_gap":20}}'
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
# count occurrences in a possibly single-line JSON body (grep -c counts lines).
count() { printf '%s\n' "$1" | grep -o "$2" | wc -l; }

# 1. 写面→审计面联动: PUT 创建配置 v1,configs 端点可见原文。
code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$base/v1/watchlist/$sym" -H 'Content-Type: application/json' -d "$params_a")
check "PUT $sym 创建配置 exit 200 (got $code)" 200 "$code"
body=$(curl -s -m 5 "$base/v1/wheel/configs?symbol=$sym")
check "configs 含 v1 行" 1 "$(count "$body" '"version":1')"
check "configs v1 保留完整配置(trade_gap:10)" 1 "$(count "$body" 'trade_gap":10')"

# 2. 版本不可变性: 重复 PUT(改 params)追加 v2,v1 原文不被覆盖。
code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$base/v1/watchlist/$sym" -H 'Content-Type: application/json' -d "$params_b")
check "PUT 改配置追加 v2 exit 200 (got $code)" 200 "$code"
body=$(curl -s -m 5 "$base/v1/wheel/configs?symbol=$sym")
check "configs 同时含 v1 与 v2(版本不可变)" 2 "$(count "$body" '"version":[12]')"
check "v2 保留新配置(trade_gap:20)" 1 "$(count "$body" 'trade_gap":20')"
check "v1 原文仍在(trade_gap:10 不被覆盖)" 1 "$(count "$body" 'trade_gap":10')"

# 3. configs 过滤与 400 契约。
check "configs 无该 symbol → []" 1 "$(curl -s -m 5 "$base/v1/wheel/configs?symbol=NOSUCH.US" | grep -c '^\[\]$')"
code=$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/wheel/configs?limit=0")
check "configs limit=0 → 400 (got $code)" 400 "$code"

# 4. signals 过滤与错误契约(dev 环境信号行为空是设计如此)。
check "signals 无过滤 → 200 []" 1 "$(curl -s -m 5 "$base/v1/wheel/signals" | grep -c '^\[\]$')"
check "signals?action=ALERT → 200 []" 1 "$(curl -s -m 5 "$base/v1/wheel/signals?action=ALERT" | grep -c '^\[\]$')"
check "signals?capability=READY → 200 []" 1 "$(curl -s -m 5 "$base/v1/wheel/signals?capability=READY" | grep -c '^\[\]$')"
code=$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/wheel/signals?action=SELL")
check "signals?action=SELL → 400 (got $code)" 400 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/wheel/signals?capability=STALE")
check "signals?capability=STALE → 400 (got $code)" 400 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/wheel/signals?limit=0")
check "signals limit=0 → 400 (got $code)" 400 "$code"

# 5. actions 端点: 不存在信号 → 200 [];非法 id → 400。
check "signals/99999999/actions → 200 []" 1 "$(curl -s -m 5 "$base/v1/wheel/signals/99999999/actions" | grep -c '^\[\]$')"
code=$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/wheel/signals/abc/actions")
check "signals/abc/actions → 400 (got $code)" 400 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/wheel/signals/0/actions")
check "signals/0/actions → 400 (got $code)" 400 "$code"

# 6. 只读边界: 审计端点不接受写方法。
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base/v1/wheel/signals")
check "POST /v1/wheel/signals → 405 (got $code)" 405 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$base/v1/wheel/configs")
check "DELETE /v1/wheel/configs → 405 (got $code)" 405 "$code"

# 7. 清理: 删除 watchlist 绑定,审计数据按契约保留。
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$base/v1/watchlist/$sym")
check "DELETE watchlist 绑定 exit 200 (got $code)" 200 "$code"
check "绑定删除后 configs 审计仍保留 v1+v2" 2 "$(count "$(curl -s -m 5 "$base/v1/wheel/configs?symbol=$sym")" '"version":[12]')"

printf '\n  \033[1mALL %d CHECKS PASSED\033[0m (%d failed)\n' "$pass" "$failed"
[[ "$failed" -eq 0 ]] || exit 1

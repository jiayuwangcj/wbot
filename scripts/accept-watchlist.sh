#!/usr/bin/env bash
# Acceptance: `wbot watchlist` CLI 子命令 (S-watchlist).
# Verifies the CLI add/remove/list contract against real PG (dev-up 种子
# 环境): 错误契约(缺参/非法 params/非法 strategy → exit 2), add 幂等
# Upsert, remove 不存在 → exit 1, 以及与 HTTP 数据面(GET /v1/watchlist)
# 的写面→读面联动。使用独立 symbol ACCEPT.US,结束清理不留痕
# (dev-up 种子为 BTEXEC.US/BTEXECB.US,不冲突)。
#
# Usage: scripts/accept-watchlist.sh [wbot-binary] [dsn] [base-url]
# Requires: curl, a running `wbot serve` (scripts/dev-up.sh) for the
#   HTTP 联动检查; dsn 缺省 $WBOT_PG_DSN(dev-up 环境为桥接地址)。
set -uo pipefail
bin="${1:-$HOME/.wbot/dev/wbot}"
dsn="${2:-${WBOT_PG_DSN:-}}"
base="${3:-http://127.0.0.1:8080}"
if [[ -z "$dsn" ]]; then
  echo "accept-watchlist: need dsn (arg 2 or \$WBOT_PG_DSN)" >&2
  exit 2
fi
sym="ACCEPT.US"
params_a='{"full_position_price":90,"zero_position_price":130,"max_inventory":100,"trade_gap":10}'
params_b='{"full_position_price":90,"zero_position_price":130,"max_inventory":100,"trade_gap":20}'
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# 1. 错误契约: 缺 -symbol / 缺 -strategy → exit 2。
"$bin" watchlist add -dsn "$dsn" >/dev/null 2>&1
check "add 缺 -symbol → exit 2 (got $?)" 2 "$?"
"$bin" watchlist add -dsn "$dsn" -symbol "$sym" >/dev/null 2>&1
check "add 缺 -strategy → exit 2 (got $?)" 2 "$?"

# 2. 错误契约: 非法 -params JSON / 非法 strategy → exit 2。
"$bin" watchlist add -dsn "$dsn" -symbol "$sym" -strategy wheel -params '{' >/dev/null 2>&1
check "add 非法 -params JSON → exit 2 (got $?)" 2 "$?"
"$bin" watchlist add -dsn "$dsn" -symbol "$sym" -strategy nope >/dev/null 2>&1
check "add 非法 strategy → exit 2 (got $?)" 2 "$?"
"$bin" watchlist add -dsn "$dsn" -symbol "$sym" -strategy covered-call -params "$params_a" >/dev/null 2>&1
check "旧 covered-call 明确拒绝 → exit 2 (got $?)" 2 "$?"
"$bin" watchlist add -dsn "$dsn" -symbol "$sym" -strategy buy-hold >/dev/null 2>&1
check "产品 watchlist 不接受 buy-hold → exit 2 (got $?)" 2 "$?"

# 3. add: 成功 + 输出形状。
out="$( "$bin" watchlist add -dsn "$dsn" -symbol "$sym" -strategy wheel -params "$params_a" 2>&1 )"
rc=$?
check "add ACCEPT.US wheel exit 0 (got $rc)" 0 "$rc"
check "add 输出形状 watchlist: SYM strategy=… params=…" \
  1 "$(echo "$out" | grep -cE "^watchlist: $sym strategy=wheel params=\{")"

# 4. 写面→读面联动: HTTP GET /v1/watchlist 可见该条目。
check "HTTP GET /v1/watchlist 含 $sym (联动)" \
  1 "$(curl -s -m 5 "$base/v1/watchlist" | grep -c "\"symbol\":\"$sym\"")"

# 5. add 幂等: 重复 add 同条目(改 params) → exit 0,策略更新生效。
out="$( "$bin" watchlist add -dsn "$dsn" -symbol "$sym" -strategy wheel -params "$params_b" 2>&1 )"
rc=$?
check "add 重复(幂等 Upsert)exit 0 (got $rc)" 0 "$rc"
check "重复 add 追加新版本并更新 trade_gap 为 20" \
	1 "$(echo "$out" | grep -c 'trade_gap":20')"

# 6. list: 含该条目行(SYM STRAT params)。
check "list 含 $sym 行" \
  1 "$("$bin" watchlist list -dsn "$dsn" 2>/dev/null | grep -c "^$sym wheel ")"

# 7. remove: 成功 + 输出;不存在 → exit 1。
out="$( "$bin" watchlist remove -dsn "$dsn" -symbol "$sym" 2>&1 )"
rc=$?
check "remove $sym exit 0 (got $rc)" 0 "$rc"
check "remove 输出 watchlist: removed $sym" \
  1 "$(echo "$out" | grep -c "^watchlist: removed $sym$")"
"$bin" watchlist remove -dsn "$dsn" -symbol "$sym" >/dev/null 2>&1
check "remove 不存在 → exit 1 (got $?)" 1 "$?"

# 8. 清理验证: HTTP 读面不再含该条目(grep 计数 0 = 不含)。
check "清理后 HTTP /v1/watchlist 不含 $sym" \
  0 "$(curl -s -m 5 "$base/v1/watchlist" | grep -c "\"symbol\":\"$sym\"")"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
  exit 1
fi

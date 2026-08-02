#!/usr/bin/env bash
# Acceptance: `wbot futu` CLI 子命令 (S-futu-cli).
# Verifies the gateway-backed CLI contract (status/quote/funds/position/order)
# against the real gateway: 成功输出形状 + env 通道 + 错误契约 + 下单安全
# 红线(real 需 -live-confirm + -acc-id, 拒绝即 exit 2, 绝不下单)。order
# 只测 -dry-run 与校验/红线拒绝路径——真实下单是写操作,刻意不自动执行。
# (futu CLI 面此前只有单测;accept-account-snapshot.sh 的注释「与 futu funds
# 同安全面」暗示 funds 已覆盖,但 CLI 命令本身零 e2e——#48 教训的同类盲区。)
#
# Usage: scripts/accept-futu-cli.sh [wbot-binary] [rest-addr] [proto-addr]
#   rest-addr:  gateway REST base (default $FUTU_GATEWAY_URL, dev-up 已导出)
#   proto-addr: gateway OpenD protobuf addr (default $FUTU_PROTO_ADDR)
# Requires: curl, node, gateway reachable (scripts/dev-up.sh 起的环境)。
set -uo pipefail
bin="${1:-$HOME/.wbot/dev/wbot}"
rest="${2:-${FUTU_GATEWAY_URL:-}}"
proto="${3:-${FUTU_PROTO_ADDR:-}}"
if [[ -z "$rest" || -z "$proto" ]]; then
  echo "accept-futu-cli: need rest-addr + proto-addr (arg 2/3 or \$FUTU_GATEWAY_URL/\$FUTU_PROTO_ADDR)" >&2
  exit 2
fi
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# 1. status: exit 0 + health=ok。
out="$( "$bin" futu status -addr "$rest" 2>&1 )"
rc=$?
check "futu status exit 0 (got $rc)" 0 "$rc"
check "status health=ok" 1 "$(echo "$out" | grep -c '"health": "ok"')"

# 2. quote: 200 + cur_price 数值形状;错误契约缺 symbol/非法 symbol → exit 2。
out="$( "$bin" futu quote -addr "$rest" -symbol HK.00700 2>&1 )"
rc=$?
check "futu quote HK.00700 exit 0 (got $rc)" 0 "$rc"
check "quote 输出 basic_qot_list[0].cur_price 数值" \
  1 "$(echo "$out" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); process.exit(j.basic_qot_list && typeof j.basic_qot_list[0].cur_price === 'number' ? 0 : 1)" 2>/dev/null && echo 1 || echo 0)"
"$bin" futu quote -addr "$rest" >/dev/null 2>&1
check "quote 缺 -symbol → exit 2 (got $?)" 2 "$?"
"$bin" futu quote -addr "$rest" -symbol bogus >/dev/null 2>&1
check "quote 非法 symbol → exit 2 (got $?)" 2 "$?"

# 3. funds: sim/real 通道 + 数值形状;env 非法 → exit 2。
out="$( "$bin" futu funds -addr "$proto" 2>&1 )"
rc=$?
check "futu funds exit 0 (got $rc)" 0 "$rc"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.env === 'simulate' && j.total_assets > 0 && typeof j.cash === 'number' ? 0 : 1);
" "$out"
check "funds env=simulate + total_assets>0" 0 "$?"
out="$( "$bin" futu funds -addr "$proto" -env real 2>&1 )"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.env === 'real' && j.total_assets > 0 ? 0 : 1);
" "$out"
check "funds -env real 生效 + total_assets>0" 0 "$?"
"$bin" futu funds -addr "$proto" -env bogus >/dev/null 2>&1
check "funds -env bogus → exit 2 (got $?)" 2 "$?"

# 4. position: sim/real 通道 + positions 数组形状。
out="$( "$bin" futu position -addr "$proto" 2>&1 )"
rc=$?
check "futu position exit 0 (got $rc)" 0 "$rc"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.env === 'simulate' && Array.isArray(j.positions) ? 0 : 1);
" "$out"
check "position env=simulate + positions 数组" 0 "$?"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.env === 'real' && Array.isArray(j.positions) ? 0 : 1);
" "$( "$bin" futu position -addr "$proto" -env real 2>&1 )"
check "position -env real 生效" 0 "$?"

# 5. order -dry-run: 输出计划且不下单(dry_run:true)。
out="$( "$bin" futu order -addr "$proto" -dry-run -symbol HK.00700 -side buy -qty 100 2>&1 )"
rc=$?
check "order -dry-run exit 0 (got $rc)" 0 "$rc"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.dry_run === true && j.symbol === 'HK.00700' && j.side === 'buy' ? 0 : 1);
" "$out"
check "dry-run 输出计划(dry_run/symbol/side)" 0 "$?"

# 6. order 校验: 缺 symbol / qty=0 / 非法 side → exit 2(拒绝不下单)。
"$bin" futu order -addr "$proto" -side buy -qty 100 >/dev/null 2>&1
check "order 缺 -symbol → exit 2 (got $?)" 2 "$?"
"$bin" futu order -addr "$proto" -symbol HK.00700 -side buy -qty 0 >/dev/null 2>&1
check "order -qty 0 → exit 2 (got $?)" 2 "$?"
"$bin" futu order -addr "$proto" -symbol HK.00700 -side bogus -qty 100 >/dev/null 2>&1
check "order 非法 -side → exit 2 (got $?)" 2 "$?"

# 7. 安全红线: -env real 无 -live-confirm → exit 2 拒绝;有 confirm 但无 -acc-id → exit 2。
"$bin" futu order -addr "$proto" -env real -symbol HK.00700 -side buy -qty 100 >/dev/null 2>&1
check "order real 无 -live-confirm → exit 2 拒绝 (got $?)" 2 "$?"
"$bin" futu order -addr "$proto" -env real -live-confirm -symbol HK.00700 -side buy -qty 100 >/dev/null 2>&1
check "order real -live-confirm 但无 -acc-id → exit 2 拒绝 (got $?)" 2 "$?"

# 8. 网关不可达 → exit 1(运行时错误)。
"$bin" futu status -addr http://127.0.0.1:1 >/dev/null 2>&1
check "网关不可达 → exit 1 (got $?)" 1 "$?"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
  exit 1
fi

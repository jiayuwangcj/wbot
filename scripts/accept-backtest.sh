#!/usr/bin/env bash
# Acceptance: backtest 子系统 HTTP 数据面 + CLI 运行/保存/导出 (S-backtest).
# Verifies the real (PG-backed) contract of the backtest data plane:
#   - CLI run summary shape, -save persistence (saved result id)
#   - GET /v1/backtests/{id} detail shape (metrics/equity_curve/trades)
#   - 文档声称的字节一致等价 ×3: POST exec 201 body == GET detail;
#     GET export?format=json == GET detail; `wbot backtest -export` == GET export
#     (csv 与 json 双格式 roundtrip, doc/API.md 契约)
#   - 错误契约: 400(非法 id/格式) 404(不存在) CLI -export 不存在 → exit 1
#
# Usage: scripts/accept-backtest.sh [base-url] [wbot-binary] [dsn] [symbol]
# Requires: curl, node, a running `wbot serve` (scripts/dev-up.sh) with seed
#   bars for the symbol (dev-up 种子 BTEXEC.US 自带 fwd 1d bars; DEMO.US 为空,
#   缺数据时 CLI 步骤会 exit 1 失败——与 dev-up 的 POST 冒烟同用 BTEXEC.US).
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
bin="${2:-$HOME/.wbot/dev/wbot}"
dsn="${3:-${WBOT_PG_DSN:-}}"
symbol="${4:-BTEXEC.US}"
params='{"full_position_price":90,"zero_position_price":130,"max_inventory":100,"move_interval_pct":0,"min_premium_per_share":0,"stock_switch_pct":0,"trade_gap":10,"min_dte":5,"max_dte":10,"min_option_quality":0,"strategic_state":"NORMAL"}'
if [[ -z "$dsn" ]]; then
  echo "accept-backtest: need dsn (arg 3 or \$WBOT_PG_DSN)" >&2
  exit 2
fi
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
# node console.log 数字可能带 ANSI 色码,捕获后剥离再比较。
strip_ansi() { sed -E 's/\x1b\[[0-9;]*m//g'; }

# 1. CLI 运行: -dsn 真数据 → exit 0 + summary 行形状。
out="$( "$bin" backtest -dsn "$dsn" -symbol "$symbol" -strategy wheel -params "$params" 2>&1 )"
rc=$?
check "CLI backtest -dsn 运行 exit 0 (got $rc)" 0 "$rc"
check "CLI summary 形状 final_equity/realized_return/mark_return/max_drawdown/bars" \
  1 "$(echo "$out" | grep -cE '^final_equity=.+ realized_return=.+ mark_return=.+ max_drawdown=.+ bars=[0-9]+( 未成交.*)?$')"

# 2. CLI -save: 落库并打印 result id。
out="$( "$bin" backtest -dsn "$dsn" -symbol "$symbol" -strategy wheel -params "$params" -save 2>&1 | strip_ansi )"
rc=$?
check "CLI -save exit 0 (got $rc)" 0 "$rc"
id="$(echo "$out" | grep -oE 'saved result id=[0-9]+' | grep -oE '[0-9]+$')"
check "CLI -save 输出 saved result id (got ${id:-无})" 1 "$([ -n "$id" ] && echo 1 || echo 0)"

# 3. GET /v1/backtests/{id} 详情形状。
body="$(curl -s -m 10 "$base/v1/backtests/$id")"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/backtests/$id")"
check "GET /v1/backtests/$id → 200 (got $code)" 200 "$code"
node -e "
const j = JSON.parse(process.argv[1]);
const m = j.metrics || {};
const ok = j.id === $id && j.strategy === 'wheel' && j.symbol === '$symbol'
  && typeof m.total_return === 'number' && typeof m.max_drawdown === 'number'
  && typeof m.equity === 'number' && typeof m.bars === 'number'
  && Array.isArray(j.equity_curve) && j.equity_curve.length >= 2
  && Array.isArray(j.trades) && j.trades.length >= 1
  && Array.isArray(j.signals) && j.signals.length === m.bars
  && j.signals.every(s => typeof s.capability_status === 'string'
    && Array.isArray(s.blocked_by) && typeof s.snapshot_key === 'string');
process.exit(ok ? 0 : 1);
" "$body"
check "详情形状: Wheel metrics/equity/trades + 每 bar capability/snapshot trace" 0 "$?"

# 4. 等价①: POST exec 201 body 与随后 GET detail 字节一致。
post="$(curl -s -m 30 -X POST "$base/v1/backtests" -H 'Content-Type: application/json' \
  -d "{\"symbol\":\"$symbol\",\"strategy\":\"wheel\",\"params\":$params}")"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 30 -X POST "$base/v1/backtests" \
  -H 'Content-Type: application/json' -d "{\"symbol\":\"$symbol\",\"strategy\":\"wheel\",\"params\":$params}")"
check "POST /v1/backtests → 201 (got $code)" 201 "$code"
pid="$(echo "$post" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); console.log(j.id)" | strip_ansi)"
getd="$(curl -s -m 10 "$base/v1/backtests/$pid")"
check "POST 201 body == GET /v1/backtests/{id} 字节一致" \
  1 "$([[ "$post" == "$getd" ]] && echo 1 || echo 0)"

# 4b. 回测 watchlist 模式: POST from_watchlist → 201 + Wheel runs 数组。
wl="$(curl -s -m 60 -X POST "$base/v1/backtests" -H 'Content-Type: application/json' \
  -d '{"from_watchlist":true}')"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 60 -X POST "$base/v1/backtests" \
  -H 'Content-Type: application/json' -d '{"from_watchlist":true}')"
check "POST from_watchlist → 201 (got $code)" 201 "$code"
check "from_watchlist runs 数组非空且每条 wheel" \
  1 "$(echo "$wl" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); process.exit(Array.isArray(j.runs) && j.runs.length >= 1 && j.runs.every(r => r.strategy === 'wheel') ? 0 : 1)" 2>/dev/null && echo 1 || echo 0)"

# 5. GET /v1/backtests/{id}/export CSV: mime + equity/trades/signals 三节。
csv="$(curl -s -m 10 "$base/v1/backtests/$id/export")"
mime="$(curl -s -o /dev/null -w '%{content_type}' -m 10 "$base/v1/backtests/$id/export")"
check "export csv Content-Type text/csv (got $mime)" "text/csv; charset=utf-8" "$mime"
head_ok=0
echo "$csv" | grep -q '^equity_curve$' && echo "$csv" | grep -q '^ts,equity$' \
  && echo "$csv" | grep -q '^trades$' && echo "$csv" | grep -q '^ts,action,symbol,size,price,cash_after$' \
  && echo "$csv" | grep -q '^signals$' && echo "$csv" | grep -q '^ts,action,direction,reason,capability_status,blocked_by,snapshot_key,snapshot_observed_at,' \
  && head_ok=1
check "export csv 三节(equity_curve/trades/signals)头行" 1 "$head_ok"

# 6. 等价②: CLI -export csv 与 HTTP export 字节一致(roundtrip)。
cli_csv="$("$bin" backtest -dsn "$dsn" -export "$id" -format csv 2>/dev/null)"
check "CLI -export csv == GET export 字节一致" \
  1 "$([[ "$cli_csv" == "$csv" ]] && echo 1 || echo 0)"

# 7. 等价③: export json == GET detail(同一条记录)字节一致。
json_http="$(curl -s -m 10 "$base/v1/backtests/$id/export?format=json")"
check "export?format=json == GET detail 字节一致" \
  1 "$([[ "$json_http" == "$body" ]] && echo 1 || echo 0)"

# 8. 等价④: CLI -export json 与 HTTP export json 字节一致。
cli_json="$("$bin" backtest -dsn "$dsn" -export "$id" -format json 2>/dev/null)"
check "CLI -export json == GET export?format=json 字节一致" \
  1 "$([[ "$cli_json" == "$json_http" ]] && echo 1 || echo 0)"

# 9. CLI -export 不存在的 id → exit 1。
"$bin" backtest -dsn "$dsn" -export 99999999 >/dev/null 2>&1
check "CLI -export 99999999 → exit 1 (got $?)" 1 "$?"

# 10. 错误契约: 非法 id → 400;不存在 → 404。
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/backtests/0")"
check "GET /v1/backtests/0 → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/backtests/abc")"
check "GET /v1/backtests/abc → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/backtests/99999999")"
check "GET /v1/backtests/99999999 → 404 (got $code)" 404 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/backtests/99999999/export")"
check "GET /v1/backtests/99999999/export → 404 (got $code)" 404 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/backtests/$id/export?format=bogus")"
check "export?format=bogus → 400 (got $code)" 400 "$code"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
  exit 1
fi

#!/usr/bin/env bash
# Acceptance: GET /v1/account/snapshots (S-account-curve).
# Verifies the DB-backed asset-curve series endpoint: real data contract
# (env normalize/limit/chronological points from `wbot ingest account` rows),
# error contract (bad env/limit → 400), and that a new snapshot grows the
# series. Companion to scripts/accept-account-snapshot.sh (the CLI writer).
#
# Usage: scripts/accept-account-snapshots-api.sh [base-url] [wbot-binary] [dsn] [proto-addr]
# Requires: curl, node, a running `wbot serve` (scripts/dev-up.sh) and the
#   gateway reachable (for the growth check's new snapshot).
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
bin="${2:-$HOME/.wbot/dev/wbot}"
dsn="${3:-${WBOT_PG_DSN:-}}"
addr="${4:-}"
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# 1. Contract: default env → simulate, chronological points, sane values.
body="$(curl -s -m 5 "$base/v1/account/snapshots")"
node -e "
const j = JSON.parse(process.argv[1]);
const ok = j.env === 'simulate' && Array.isArray(j.points) && j.points.length >= 1;
if (!ok) process.exit(1);
for (let i = 1; i < j.points.length; i++) {
  if (j.points[i].captured_at < j.points[i-1].captured_at) process.exit(1);
}
process.exit(j.points[0].total_assets > 0 ? 0 : 1);
" "$body"
check "默认 env=simulate + points 时间递增 + total_assets>0" 0 "$?"

# 2. Error contract: bad env → 400; bad limit → 400.
code="$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/account/snapshots?env=bogus")"
check "bad env → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/account/snapshots?limit=0")"
check "bad limit → 400 (got $code)" 400 "$code"

# 3. Real env: no snapshots yet (ingest account only ever wrote simulate) → empty points.
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.env === 'real' && j.points.length === 0 ? 0 : 1);
" "$(curl -s -m 5 "$base/v1/account/snapshots?env=real")"
check "env=real → 空 points" 0 "$?"

# 4. Growth: a new `ingest account` snapshot adds exactly one point.
# (node console.log 可能给数字加 ANSI 色码,捕获后剥离再比较。)
strip_ansi() { sed -E 's/\x1b\[[0-9;]*m//g'; }
before="$(curl -s -m 5 "$base/v1/account/snapshots?env=sim" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); console.log(j.points.length)" | strip_ansi)"
if [[ -z "$dsn" ]]; then
  check "增长检查需 dsn(跳过)" 0 0
else
  addr_args=(); [[ -n "$addr" ]] && addr_args=(-addr "$addr")
  "$bin" ingest account "${addr_args[@]}" -dsn "$dsn" >/dev/null 2>&1
  code=$?
  check "ingest account 新快照 → exit 0" 0 "$code"
  after="$(curl -s -m 5 "$base/v1/account/snapshots?env=sim" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); console.log(j.points.length)" | strip_ansi)"
  node -e "
  const b = Number(process.argv[1]), a = Number(process.argv[2]);
  process.exit(a === b + 1 ? 0 : 1);
  " "$before" "$after"
  check "端点系列随快照 +1(before $before → after $after)" 0 "$?"
fi

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

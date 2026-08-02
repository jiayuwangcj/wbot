#!/usr/bin/env bash
# Acceptance: POST /v1/ingest kind=bars refill (S-bars-refill, unlocked).
# Verifies the live endpoint pulls bars through the futu gateway (HK.00700
# 1d fwd; data-landing asserted via bars rows/max_ts before vs after — a
# weekend pull keeps max_ts at the last trading day, so freshness is NOT
# asserted) plus the error contract for empty/invalid symbols. Companion to
# scripts/accept-options-ingest.sh (kind=option).
#
# Usage: scripts/accept-bars-refill.sh [base-url]
# Requires: curl, node (JSON assertions), docker (wbot-pg-ci-test for the
#   bars table), and a running `wbot serve` with the gateway reachable
#   (scripts/dev-up.sh).
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# 1. Contract: empty symbol → 400 invalid_request with the {code,message,action} body.
body="$(curl -s -X POST "$base/v1/ingest" -H 'Content-Type: application/json' -d '{"symbol":""}')"
node -e "
const e = JSON.parse(process.argv[1]);
process.exit(e.code === 'invalid_request' && e.message && e.action ? 0 : 1);
" "$body"
check "空 symbol → 400 契约(code=invalid_request)" 0 "$?"

# 2. Error path: unqualified symbol is rejected inside the runner → 503 ingest_failed.
code="$(curl -s -o /tmp/wbot-bars-refill-err.json -w '%{http_code}' -X POST "$base/v1/ingest" -H 'Content-Type: application/json' -d '{"symbol":"NOPE"}')"
node -e "
const e = JSON.parse(require('fs').readFileSync(process.argv[1]));
process.exit(e.code === 'ingest_failed' && e.message && e.action ? 0 : 1);
" /tmp/wbot-bars-refill-err.json
check "非法 symbol → 503 ingest_failed(HTTP $code)" 0 "$?"
rm -f /tmp/wbot-bars-refill-err.json

# 3. Real refill: HK.00700 1d fwd bars through the gateway (seconds, unlike
#    the option chain). Rows before/after to prove the data landed.
rows_before="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) || '|' || coalesce(max(ts)::text,'') FROM bars WHERE symbol='HK.00700' AND timeframe='1d' AND adjust='fwd'")"
body="$(curl -s -m 120 -X POST "$base/v1/ingest" -H 'Content-Type: application/json' -d '{"symbol":"HK.00700","timeframe":"1d","adjust":"fwd"}')"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.symbol === 'HK.00700' && j.timeframe === '1d' && j.adjust === 'fwd' && j.status === 'ok' ? 0 : 1);
" "$body"
check "POST bars HK.00700 1d fwd → 201 + symbol/timeframe/adjust/status" 0 "$?"
rows_after="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) || '|' || coalesce(max(ts)::text,'') FROM bars WHERE symbol='HK.00700' AND timeframe='1d' AND adjust='fwd'")"
node -e "
const [bc, bt] = process.argv[1].split('|');
const [ac, at] = process.argv[2].split('|');
process.exit(Number(ac) >= Number(bc) && at >= bt && Number(ac) > 0 ? 0 : 1);
" "$rows_before" "$rows_after"
check "数据落库:bars rows/max_ts 非下降(before $rows_before → after $rows_after)" 0 "$?"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

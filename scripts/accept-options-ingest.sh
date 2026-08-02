#!/usr/bin/env bash
# Acceptance: POST /v1/ingest kind=option (S-options-ingest-button).
# Verifies the live endpoint pulls an option chain through the futu gateway
# (HK.00700: real expiry; data-landing asserted via option_quotes rows/max_ts
# before vs after — freshness is NOT asserted because daily K-lines over a
# weekend are stale by the 4h threshold by definition) plus the error
# contract for empty/invalid symbols.
#
# NOTE: the real pull is serial per contract under the gateway snapshot rate
# limit and can take ~3-9 min (15 min service guard); the two error checks
# are fast and run first.
#
# Usage: scripts/accept-options-ingest.sh [base-url]
# Requires: curl, node (JSON assertions), and a running `wbot serve`
#   with the gateway reachable (scripts/dev-up.sh).
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# 1. Contract: empty symbol → 400 invalid_request with the {code,message,action} body.
body="$(curl -s -X POST "$base/v1/ingest" -H 'Content-Type: application/json' -d '{"kind":"option","symbol":""}')"
node -e "
const e = JSON.parse(process.argv[1]);
process.exit(e.code === 'invalid_request' && e.message && e.action ? 0 : 1);
" "$body"
check "空 symbol → 400 契约(code=invalid_request)" 0 "$?"

# 2. Error path: unqualified symbol is rejected inside the runner → 503 ingest_failed.
code="$(curl -s -o /tmp/wbot-opt-ingest-err.json -w '%{http_code}' -X POST "$base/v1/ingest" -H 'Content-Type: application/json' -d '{"kind":"option","symbol":"NOPE"}')"
node -e "
const e = JSON.parse(require('fs').readFileSync(process.argv[1]));
process.exit(e.code === 'ingest_failed' && e.message && e.action ? 0 : 1);
" /tmp/wbot-opt-ingest-err.json
check "非法 symbol → 503 ingest_failed(HTTP $code)" 0 "$?"
rm -f /tmp/wbot-opt-ingest-err.json

# 3. Real pull: HK.00700 option chain through the gateway (can take minutes).
#    Rows before/after (count + max_ts) to prove the data landed; freshness
#    is NOT asserted: daily K-lines over a weekend are stale by the 4h
#    threshold by definition (last trading day is Friday), pull or not.
rows_before="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) || '|' || coalesce(max(ts)::text,'') FROM option_quotes WHERE underlying='HK.00700'")"
body="$(curl -s -m 880 -X POST "$base/v1/ingest" -H 'Content-Type: application/json' -d '{"kind":"option","symbol":"HK.00700"}')"
node -e "
const j = JSON.parse(process.argv[1]);
process.exit(j.kind === 'option' && j.symbol === 'HK.00700' && j.status === 'ok' ? 0 : 1);
" "$body"
check "POST kind=option HK.00700 → 201 + kind/symbol/status" 0 "$?"
rows_after="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) || '|' || coalesce(max(ts)::text,'') FROM option_quotes WHERE underlying='HK.00700'")"
node -e "
const [bc, bt] = process.argv[1].split('|');
const [ac, at] = process.argv[2].split('|');
process.exit(Number(ac) >= Number(bc) && at >= bt && Number(ac) > 0 ? 0 : 1);
" "$rows_before" "$rows_after"
check "数据落库:option_quotes rows/max_ts 非下降(before $rows_before → after $rows_after)" 0 "$?"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

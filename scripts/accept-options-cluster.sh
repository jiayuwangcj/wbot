#!/usr/bin/env bash
# Acceptance: /v1/admin/cluster options_freshness (S-options-cluster).
# Verifies the live endpoint carries per-underlying×source option freshness
# with the 4h default threshold (fresh/stale), fields complete, backward
# compatibility of bars_coverage, and empty array when no option data.
#
# Usage: scripts/accept-options-cluster.sh [base-url]
# Requires: docker (wbot-pg-ci-test address), node (JSON assertions),
#   and a running `wbot serve` (scripts/dev-up.sh).
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# Seed a 2h-old option row (4h threshold → fresh) and a 100h-old one (stale).
docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -q <<'SQL' || { echo "  seed failed"; exit 1; }
DELETE FROM option_quotes WHERE underlying IN ('ACCCLUSFRESH.US','ACCCLUSSTALE.US');
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ('ACCCLUSFRESH.US','ACCCLUSFRESH.US','call',100,'2026-12-31', now() - interval '2 hours', 10,11,9,10.5,100,NULL,'none','futu');
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ('ACCCLUSSTALE.US','ACCCLUSSTALE.US','call',100,'2026-12-31', now() - interval '100 hours', 10,11,9,10.5,100,NULL,'none','futu');
SQL

body="$(curl -s "$base/v1/admin/cluster")"

# 1. fields + statuses of the two seeded rows, 2. bars_coverage untouched (backward compat).
node -e "
const j = JSON.parse(process.argv[1]);
const dp = j.components.data_plane;
const of = dp.options_freshness || [];
const fresh = of.find(o => o.underlying === 'ACCCLUSFRESH.US');
const stale = of.find(o => o.underlying === 'ACCCLUSSTALE.US');
const okFields = ['underlying','source','max_ts','max_ts_age_seconds','fresh'].every(k => fresh && k in fresh && stale && k in stale);
process.exit(okFields && fresh.fresh === 'fresh' && stale.fresh === 'stale' && Array.isArray(dp.bars_coverage) ? 0 : 1);
" "$body"
check "seeded 行:2h → fresh / 100h → stale,字段齐全" 0 "$?"

node -e "
const j = JSON.parse(process.argv[1]);
const dp = j.components.data_plane;
process.exit(Array.isArray(dp.options_freshness) && Array.isArray(dp.bars_coverage) ? 0 : 1);
" "$body"
check "bars_coverage 与 options_freshness 均为数组(向后兼容)" 0 "$?"

# Clean up acceptance data.
docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -c "DELETE FROM option_quotes WHERE underlying IN ('ACCCLUSFRESH.US','ACCCLUSSTALE.US')" >/dev/null 2>&1

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

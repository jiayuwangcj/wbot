#!/usr/bin/env bash
# Acceptance: option_quotes freshness joins the `ingest freshness` gate
# (S-option-freshness). Verifies real CLI exit codes against a live PG:
#   - default threshold: stale option row → exit 1 + rows printed
#   - -max-age global override → exit 0 + the stale row flips to fresh
#
# Usage: scripts/accept-option-freshness.sh [bin] [dsn]
#   bin  optional prebuilt wbot binary (default: build into a temp dir)
#   dsn  optional PostgreSQL DSN (default: $WBOT_PG_DSN, then the
#        wbot-pg-ci-test docker bridge address)
# Requires: go (when bin omitted), plus psql OR docker (for seeding — host
# psql preferred, docker exec fallback for OrbStack dev hosts).
# Test data uses the ACCOPT* prefix and is cleaned up at the end.
set -uo pipefail
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
has() { local hay="$1" pat="$2"; if grep -qE "$pat" <<<"$hay"; then echo 1; else echo 0; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ -n "${1:-}" ]]; then
  WB="$1"
else
  builddir="$(mktemp -d)"
  trap 'rm -rf "$builddir"' EXIT
  go build -o "$builddir/wbot" ./cmd/wbot
  WB="$builddir/wbot"
fi

DSN="${2:-${WBOT_PG_DSN:-}}"
if [[ -z "$DSN" ]]; then
  ip="$(docker inspect wbot-pg-ci-test --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null)" || { echo "  no DSN arg, no WBOT_PG_DSN, and no wbot-pg-ci-test container"; exit 1; }
  DSN="postgres://postgres:postgres@${ip}:5432/wbot_test?sslmode=disable"
fi

# SQL runner: host psql preferred; docker exec fallback (OrbStack dev hosts
# ship docker but not a psql client).
if command -v psql >/dev/null 2>&1; then
  sql() { psql "$DSN" -q "$@"; }
else
  sql() { docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -q "$@"; }
fi

# Seed one 2h-old option row (4h threshold → fresh) and one 100h-old (stale).
sql <<'SQL' >/dev/null || { echo "  seed failed"; exit 1; }
DELETE FROM option_quotes WHERE underlying IN ('ACCOPTFRESH.US','ACCOPTSTALE.US');
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ('ACCOPTFRESH.US','ACCOPTFRESH.US','call',100,'2026-12-31', now() - interval '2 hours', 10,11,9,10.5,100,NULL,'none','futu');
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ('ACCOPTSTALE.US','ACCOPTSTALE.US','call',100,'2026-12-31', now() - interval '100 hours', 10,11,9,10.5,100,NULL,'none','futu');
SQL

out="$("$WB" ingest freshness -dsn "$DSN" 2>&1)"
code=$?
check "默认阈值下 stale 期权 → exit 1" 1 "$code"
check "fresh 期权行输出(ACCOPTFRESH)" 1 "$(has "$out" 'ACCOPTFRESH\.US option .* fresh')"
check "stale 期权行输出(ACCOPTSTALE)" 1 "$(has "$out" 'ACCOPTSTALE\.US option .* stale')"
check "stale 门禁 stderr 提示" 1 "$(has "$out" 'stale data found')"

out2="$("$WB" ingest freshness -dsn "$DSN" -max-age 1000000h 2>&1)"
code2=$?
check "-max-age 全局覆盖 → exit 0" 0 "$code2"
check "覆盖后 stale 期权变 fresh" 1 "$(has "$out2" 'ACCOPTSTALE\.US option .* fresh')"

# Clean up acceptance data.
sql -c "DELETE FROM option_quotes WHERE underlying IN ('ACCOPTFRESH.US','ACCOPTSTALE.US')" >/dev/null 2>&1

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

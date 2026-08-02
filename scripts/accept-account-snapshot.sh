#!/usr/bin/env bash
# Acceptance: `wbot ingest account` snapshot (S-account-snapshot).
# Verifies the CLI queries funds over the gateway protobuf API (sim and
# real env, both read-only) and appends rows to account_snapshots — the
# data layer of the 资产曲线. Repeated runs must append exactly one row
# each (ON CONFLICT keeps same-instant repeats at rows=0); the table lives
# in PG, asserted via docker exec psql (same pattern as accept-bars-refill.sh).
#
# Usage: scripts/accept-account-snapshot.sh [wbot-binary] [dsn] [proto-addr]
#   wbot-binary: path to the wbot binary (default: ~/.wbot/dev/wbot)
#   dsn:         PostgreSQL DSN (default: $WBOT_PG_DSN)
#   proto-addr:  gateway OpenD protobuf addr (default: futu.DefaultProtoAddr;
#                on OrbStack use the bridge IP, e.g. 192.168.215.2:11111)
# Requires: docker (wbot-pg-ci-test), the gateway OpenD running (scripts/dev-up.sh).
set -uo pipefail
bin="${1:-$HOME/.wbot/dev/wbot}"
dsn="${2:-${WBOT_PG_DSN:-}}"
addr="${3:-}"
if [[ -z "$dsn" ]]; then
  echo "set WBOT_PG_DSN or pass the DSN as \$2" >&2
  exit 1
fi
addr_args=(); [[ -n "$addr" ]] && addr_args=(-addr "$addr")
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

# 1. Contract: -h exits 0 and mentions the snapshot table.
out="$("$bin" ingest account -h 2>&1)"; code=$?
check "ingest account -h → exit 0" 0 "$code"
case "$out" in *account_snapshots*资产曲线*) check "-h 文案含 account_snapshots/资产曲线" 0 0;; *) check "-h 文案含 account_snapshots/资产曲线" 0 1;; esac

# 2. Error path: bad -env → exit 2.
"$bin" ingest account -env bogus >/dev/null 2>&1; code=$?
check "bad -env → exit 2" 2 "$code"

# 3. Real snapshot: sim env funds through the gateway, one row appended.
before="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) FROM account_snapshots WHERE env='simulate'")"
out="$("$bin" ingest account "${addr_args[@]}" -dsn "$dsn" 2>&1)"; code=$?
check "ingest account 真实快照 → exit 0" 0 "$code"
case "$out" in
  *"ingest account: snapshot acc_id="*rows=1*) check "快照输出 acc_id/total_assets + rows=1" 0 0 ;;
  *) check "快照输出 acc_id/total_assets + rows=1 (got: $out)" 0 1 ;;
esac
after="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) FROM account_snapshots WHERE env='simulate'")"
node -e "
const b = Number(process.argv[1]), a = Number(process.argv[2]);
process.exit(a === b + 1 && a > 0 ? 0 : 1);
" "$before" "$after"
check "数据落库:sim 快照 rows +1(before $before → after $after)" 0 "$?"

# 4. Values sane: latest sim snapshot has positive total assets and cash+market+power present.
row="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT total_assets, cash, market_val, power FROM account_snapshots WHERE env='simulate' ORDER BY captured_at DESC LIMIT 1")"
node -e "
const [ta, cash, mv, pw] = process.argv[1].split('|').map(Number);
process.exit(ta > 0 && cash >= 0 && mv >= 0 && pw >= 0 ? 0 : 1);
" "$row"
check "最新快照数值合理(total_assets>0, cash/market_val/power ≥0)" 0 "$?"

# 5. Real env snapshot: same read-only funds query, real account, rows+1.
# (Real account id is resolved by -env real; the write side is read-only —
# same safety surface as `wbot futu funds -env real`, see FUTU.md §9.)
r_before="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) FROM account_snapshots WHERE env='real'")"
out="$("$bin" ingest account -env real "${addr_args[@]}" -dsn "$dsn" 2>&1)"; code=$?
check "ingest account -env real 真实快照 → exit 0" 0 "$code"
case "$out" in
  *"ingest account: snapshot acc_id="*env=real*rows=1*) check "real 快照输出 acc_id/env=real + rows=1" 0 0 ;;
  *) check "real 快照输出 acc_id/env=real + rows=1 (got: $out)" 0 1 ;;
esac
r_after="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT count(*) FROM account_snapshots WHERE env='real'")"
node -e "
const b = Number(process.argv[1]), a = Number(process.argv[2]);
process.exit(a === b + 1 && a > 0 ? 0 : 1);
" "$r_before" "$r_after"
check "数据落库:real 快照 rows +1(before $r_before → after $r_after)" 0 "$?"
r_row="$(docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -tA -c "SELECT total_assets FROM account_snapshots WHERE env='real' ORDER BY captured_at DESC LIMIT 1")"
node -e "process.exit(Number(process.argv[1]) > 0 ? 0 : 1)" "$r_row"
check "real 最新快照 total_assets>0" 0 "$?"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

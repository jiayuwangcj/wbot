#!/usr/bin/env bash
# Deterministic CLI -> HKEX ZIP parser -> PostgreSQL -> Wheel report acceptance.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
WB="${1:-./wbot}"
dsn="${2:-${WBOT_PG_DSN:-}}"
if [ -z "$dsn" ]; then
  echo "accept-hkex-datafill: pass DSN or set WBOT_PG_DSN" >&2
  exit 2
fi

port="${HKEX_FAKE_PORT:-18087}"
base="http://127.0.0.1:${port}"
underlying="HK.HKEXACC"
class="TST"
run_source="hkex-accept-$$"
tmp="$(mktemp -d)"
fake_log="$tmp/fake.log"
fake_pid=""
cleanup() {
  if [ -n "$fake_pid" ]; then kill "$fake_pid" 2>/dev/null || true; fi
  psql "$dsn" -v ON_ERROR_STOP=1 -q -c "DELETE FROM option_quote_snapshots WHERE underlying='${underlying}'; DELETE FROM option_quotes WHERE underlying='${underlying}'; DELETE FROM bars WHERE symbol='${underlying}'; DELETE FROM ingestion_runs WHERE source='${run_source}';" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

go run ./scripts/fake-hkex -listen "127.0.0.1:${port}" -class "$class" >"$fake_log" 2>&1 </dev/null &
fake_pid=$!
for _ in $(seq 1 100); do
  curl -sf "$base/health" >/dev/null 2>&1 && break
  sleep 0.1
done
curl -sf "$base/health" >/dev/null

checks=0
check() {
  local name="$1" want="$2" got="$3"
  checks=$((checks+1))
  if [ "$want" != "$got" ]; then
    echo "not ok $checks - $name (want=$want got=$got)" >&2
    exit 1
  fi
  echo "ok $checks - $name"
}

set +e
help="$($WB ingest hkex -h 2>&1)"; help_code=$?
set -e
case "$help" in *DTOP*RP006-FINAL*source=hkex*research*) help_shape=0 ;; *) help_shape=1 ;; esac
check "ingest hkex help/field semantics" "0:0" "$help_code:$help_shape"

psql "$dsn" -v ON_ERROR_STOP=1 -q -c "DELETE FROM option_quote_snapshots WHERE underlying='${underlying}'; DELETE FROM option_quotes WHERE underlying='${underlying}'; DELETE FROM bars WHERE symbol='${underlying}';" >/dev/null
args=(ingest hkex -dsn "$dsn" -source "$run_source" -symbol "$underlying" -class "$class" -lot-size 100 -from 2026-07-01 -to 2026-07-16 -dtop-base "$base" -rp006-base "$base" -request-interval 1ms)
set +e
first="$($WB "${args[@]}" 2>&1)"; first_code=$?
set -e
case "$first" in *trading_days=12*quote_only_days=1*option_quotes=48*snapshots=22*source=hkex*) first_shape=0 ;; *) first_shape=1 ;; esac
check "首次回填 12 日/48 结算行（1 日仅 DTOP）" "0:0" "$first_code:$first_shape"

quote_counts="$(psql "$dsn" -At -v ON_ERROR_STOP=1 -c "SELECT count(*)||':'||count(DISTINCT ts) FROM option_quotes WHERE underlying='${underlying}' AND source='hkex'")"
check "option_quotes 行数/交易日" "48:12" "$quote_counts"

snapshot_count="$(psql "$dsn" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM option_quote_snapshots WHERE underlying='${underlying}' AND source='hkex'")"
check "研究 snapshot 行数" "22" "$snapshot_count"

settlement="$(psql "$dsn" -At -v ON_ERROR_STOP=1 -c "SELECT trim(to_char(close,'FM999990.00'))||':'||trim(to_char(implied_vol,'FM0.0000')) FROM option_quotes WHERE underlying='${underlying}' AND option_type='put' AND strike=490 ORDER BY ts LIMIT 1")"
check "结算价/官方 IV 字段映射" "20.00:0.2500" "$settlement"

projection="$(psql "$dsn" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM option_quote_snapshots WHERE underlying='${underlying}' AND source='hkex' AND bid=ask AND delta IS NOT NULL AND theta IS NOT NULL AND volume>0 AND open_interest>0 AND lot_size=100")"
check "EOD 投影完整且 source=hkex" "22" "$projection"

set +e
second="$($WB "${args[@]}" 2>&1)"; second_code=$?
set -e
case "$second" in *inserted_quotes=0*inserted_snapshots=0*) second_shape=0 ;; *) second_shape=1 ;; esac
check "重复回填零新增" "0:0" "$second_code:$second_shape"

after_counts="$(psql "$dsn" -At -v ON_ERROR_STOP=1 -c "SELECT (SELECT count(*) FROM option_quotes WHERE underlying='${underlying}' AND source='hkex')||':'||(SELECT count(*) FROM option_quote_snapshots WHERE underlying='${underlying}' AND source='hkex')")"
check "幂等后两表计数不变" "48:22" "$after_counts"

psql "$dsn" -v ON_ERROR_STOP=1 -q <<SQL
INSERT INTO bars (symbol,timeframe,ts,open,high,low,close,volume,adjust,source)
SELECT '${underlying}','1d',d + interval '9 hours',
       CASE WHEN d::date='2026-07-16'::date THEN 490 ELSE 500 END,
       CASE WHEN d::date='2026-07-16'::date THEN 490 ELSE 500 END,
       CASE WHEN d::date='2026-07-16'::date THEN 490 ELSE 500 END,
       CASE WHEN d::date='2026-07-16'::date THEN 490 ELSE 500 END,
       100000,'none','hkex-accept'
FROM generate_series('2026-07-01'::timestamptz,'2026-07-16'::timestamptz,interval '1 day') d
WHERE extract(isodow FROM d) <= 5
ON CONFLICT DO NOTHING;
SQL
params='{"full_position_price":480,"zero_position_price":520,"max_inventory":1000,"move_interval_pct":0,"min_premium_per_share":0,"stock_switch_pct":0,"trade_gap":10,"min_dte":5,"max_dte":10,"min_option_quality":0,"strategic_state":"NORMAL"}'
set +e
backtest_out="$($WB backtest -dsn "$dsn" -symbol "$underlying" -timeframe 1d -adjust none -strategy wheel -params "$params" -cash 200000 -from 2026-07-01T00:00:00Z -to 2026-07-16T23:59:59Z -report -report-dir "$tmp/reports" 2>&1)"; backtest_code=$?
set -e
case "$backtest_out" in *"未成交 N/A"*) trades_shape=1 ;; *"未成交 "*) trades_shape=0 ;; *) trades_shape=1 ;; esac
if [ "$backtest_code:$trades_shape" != "0:0" ]; then echo "$backtest_out" >&2; fi
check "HKEX 历史触发 Wheel 成交" "0:0" "$backtest_code:$trades_shape"

report_file=("$tmp"/reports/*.json)
if [ "${#report_file[@]}" -eq 1 ] && grep -q '"capability_status": "RESEARCH_ONLY"' "${report_file[0]}" && grep -q '"historical_option_cycle_complete": true' "${report_file[0]}" && grep -q '"option_snapshot_sources": \[' "${report_file[0]}" && grep -q '"hkex"' "${report_file[0]}" && grep -Eq '"net_return_pct": -?[0-9]' "${report_file[0]}" && grep -Eq '"fill_count": [1-9]' "${report_file[0]}"; then
  report_shape=0
else
  report_shape=1
fi
check "报告 RESEARCH_ONLY/完整周期/非零成交收益" "0" "$report_shape"

echo "ALL $checks CHECKS PASSED"

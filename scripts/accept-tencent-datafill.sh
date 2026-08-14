#!/usr/bin/env bash
# Acceptance: Tencent qfq daily-bar backfill + report provenance on real PG.
#
# Usage: scripts/accept-tencent-datafill.sh [wbot-bin] [dsn]
# Requires: Tencent Finance network access and a real PostgreSQL database.
# When psql is unavailable, the local wbot-pg-ci-test container is used for
# read-only assertions (the DSN must point at that same database).
set -uo pipefail

pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [[ -n "${1:-}" ]]; then WB="$1"; else go build -o "$tmp/wbot" ./cmd/wbot && WB="$tmp/wbot"; fi
dsn="${2:-${WBOT_PG_DSN:-}}"
if [[ -z "$dsn" ]]; then
  echo "accept-tencent-datafill: pass a DSN or set WBOT_PG_DSN" >&2
  exit 2
fi

db_query() {
  local sql="$1"
  if command -v psql >/dev/null 2>&1; then
    psql "$dsn" -v ON_ERROR_STOP=1 -tA -c "$sql"
    return
  fi
  if docker inspect wbot-pg-ci-test >/dev/null 2>&1; then
    docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -v ON_ERROR_STOP=1 -tA -c "$sql"
    return
  fi
  echo "accept-tencent-datafill: need psql or wbot-pg-ci-test" >&2
  return 127
}

help="$($WB ingest tencent -h 2>&1)"; help_code=$?
help_ok=0
[[ "$help_code" == 0 && "$help" == *"source=tencent"* && "$help" == *"adjust=fwd"* && "$help" == *"US.JD"* ]] && help_ok=1
check "腾讯回填 CLI 帮助固定 source=tencent/qfq→fwd 与 US 限制" 1 "$help_ok"

dry="$($WB ingest tencent -symbol HK.00700 -count 1000 -dry-run 2>&1)"; dry_code=$?
dry_count="$(sed -n 's/.*dry-run: \([0-9][0-9]*\) bars.*/\1/p' <<<"$dry" | tail -1)"
dry_ok=0
[[ "$dry_code" == 0 && "${dry_count:-0}" -ge 300 && "$dry" == *"source=tencent adjusted=qfq"* ]] && dry_ok=1
check "真实腾讯 HK.00700 dry-run ≥300 日且标注 tencent/qfq" 1 "$dry_ok"

first="$($WB ingest tencent -dsn "$dsn" -symbol HK.00700 -count 1000 2>&1)"; first_code=$?
first_ok=0
[[ "$first_code" == 0 && "$first" == *"source=tencent adjusted=qfq adjust=fwd"* ]] && first_ok=1
check "真实 PG 首次回填成功并输出 provenance" 1 "$first_ok"

state1="$(db_query "SELECT count(*) || '|' || count(DISTINCT ts) || '|' || coalesce(min(ts)::text,'') || '|' || coalesce(max(ts)::text,'') FROM bars WHERE symbol='HK.00700' AND timeframe='1d' AND adjust='fwd' AND source='tencent'")"
IFS='|' read -r count1 distinct1 min1 max1 <<<"$state1"
landing_ok=0
[[ "${count1:-0}" -ge 300 && "$count1" == "$distinct1" && -n "${min1:-}" && -n "${max1:-}" ]] && landing_ok=1
check "bars 落库 ≥300 且 Tencent symbol+ts 无重复" 1 "$landing_ok"

second="$($WB ingest tencent -dsn "$dsn" -symbol HK.00700 -count 1000 2>&1)"; second_code=$?
state2="$(db_query "SELECT count(*) || '|' || count(DISTINCT ts) FROM bars WHERE symbol='HK.00700' AND timeframe='1d' AND adjust='fwd' AND source='tencent'")"
repeat_ok=0
[[ "$second_code" == 0 && "$state2" == "$count1|$distinct1" ]] && repeat_ok=1
check "重复回填幂等（rows/distinct ts 不增长）" 1 "$repeat_ok"

us="$($WB ingest tencent -symbol US.JD -count 1000 -dry-run 2>&1)"; us_code=$?
us_ok=0
[[ "$us_code" == 0 && "$us" == *"1 bars"* && "$us" == *"腾讯美股仅当日,历史靠每日积累"* ]] && us_ok=1
check "US.JD 如实返回单日并提示历史靠积累" 1 "$us_ok"

mkdir -p "$tmp/reports"
report_out="$($WB backtest -dsn "$dsn" -symbol HK.00700 -timeframe 1d -adjust fwd -strategy hold -limit 10000 -report -report-dir "$tmp/reports" 2>&1)"; report_code=$?
json_path="$(find "$tmp/reports" -maxdepth 1 -name '*.json' -print -quit)"
json_ok=false
if [[ "$report_code" == 0 && -n "$json_path" ]]; then
  json_ok="$(node -e 'const r=require(process.argv[1]),q=r.data_quality,p=q.underlying_bars||[];process.stdout.write(String(q.total_bar_count>=300&&p.some(x=>x.source==="tencent"&&x.adjusted==="qfq"&&x.bar_count>0)));' "$json_path")"
fi
check "回测 JSON data_quality 总覆盖 ≥300 且含 Tencent qfq provenance" true "$json_ok"

html_path="${json_path%.json}.html"
html_ok=0
[[ -f "$html_path" ]] && grep -q 'tencent/qfq' "$html_path" && html_ok=1
check "回测 HTML 数据质量卡显示 tencent/qfq" 1 "$html_ok"

echo
if [[ "$failed" == 0 ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  printf '  dry=%q first=%q second=%q us=%q report=%q state=%q/%q\n' "$dry" "$first" "$second" "$us" "$report_out" "$state1" "$state2"
  exit 1
fi

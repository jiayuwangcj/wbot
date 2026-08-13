#!/usr/bin/env bash
# Acceptance: Wheel live fail-closed and fake LLM/Telegram loops.
# Usage: scripts/accept-wheel-live.sh [wbot-binary] [dsn] [base-url]
set -uo pipefail

bin="${1:-$HOME/.wbot/dev/wbot}"
dsn="${2:-${WBOT_PG_DSN:-}}"
base_arg="${3:-}"
if [[ -z "$dsn" ]]; then
  echo "accept-wheel-live: need dsn (arg 2 or \$WBOT_PG_DSN)" >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ -n "$base_arg" ]]; then
  case "$base_arg" in
    http://*) serve_listen="${base_arg#http://}" ;;
    *) echo "accept-wheel-live: base-url must start with http://" >&2; exit 2 ;;
  esac
else
  serve_listen="127.0.0.1:0"
fi

if command -v psql >/dev/null 2>&1; then
  sql() { psql "$dsn" -Atq "$@"; }
elif command -v docker >/dev/null 2>&1; then
  sql() { docker exec -i wbot-pg-ci-test psql -U postgres -d wbot_test -Atq "$@"; }
else
  echo "accept-wheel-live: psql or docker is required for dismissal verification" >&2
  exit 2
fi

tmp="$(mktemp -d)"
serve_pid=""
fake_pid=""
serve_base=""
sym_a="WLA$$.US"
sym_b="WLB$$.US"
pass=0
failed=0

check() {
  local description="$1" want="$2" got="$3"
  if [[ "$got" == "$want" ]]; then
    pass=$((pass + 1))
    printf '  \033[32mPASS\033[0m %s\n' "$description"
  else
    failed=$((failed + 1))
    printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$description" "$want" "$got"
  fi
}

count() {
  printf '%s\n' "$1" | grep -oE "$2" | wc -l | tr -d ' '
}

stop_serve() {
  if [[ -n "$serve_pid" ]]; then
    kill "$serve_pid" 2>/dev/null || true
    wait "$serve_pid" 2>/dev/null || true
    serve_pid=""
  fi
}

stop_fake() {
  if [[ -n "$fake_pid" ]]; then
    kill "$fake_pid" 2>/dev/null || true
    wait "$fake_pid" 2>/dev/null || true
    fake_pid=""
  fi
}

cleanup() {
  stop_serve
  stop_fake
  "$bin" watchlist remove -dsn "$dsn" -symbol "$sym_a" >/dev/null 2>&1 || true
  "$bin" watchlist remove -dsn "$dsn" -symbol "$sym_b" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

start_serve() {
  local logfile="$1"
  shift
  local telegram_flag=""
  if [[ "${1:-}" == "--telegram-run" ]]; then
    telegram_flag="-telegram-run"
    shift
  fi
  : >"$logfile"
  env "$@" "$bin" serve -listen "$serve_listen" -dsn "$dsn" \
    -datacheck-disable -wheel-run -wheel-interval 10s $telegram_flag >"$logfile" 2>&1 &
  serve_pid=$!
  serve_base=""
  for _ in $(seq 1 30); do
    if ! kill -0 "$serve_pid" 2>/dev/null; then
      break
    fi
    addr="$(sed -n 's/.*httpapi: listening on http:\/\/\(.*\)$/\1/p' "$logfile" | head -n 1)"
    if [[ -n "$addr" ]]; then
      serve_base="http://$addr"
      break
    fi
    sleep 1
  done
}

wait_for_signal() {
  local symbol="$1" max_seconds="$2" body="[]"
  for _ in $(seq 1 "$max_seconds"); do
    body="$(curl -s -m 5 "$serve_base/v1/wheel/signals?symbol=$symbol&limit=10")"
    if [[ "$(count "$body" "\\\"symbol\\\":\\\"$symbol\\\"")" -gt 0 ]]; then
      printf '%s' "$body"
      return 0
    fi
    sleep 1
  done
  printf '%s' "$body"
}

signal_id() {
  printf '%s\n' "$1" | node -e 'const a=JSON.parse(require("fs").readFileSync(0)); process.stdout.write(a[0] && a[0].id ? String(a[0].id) : "")' 2>/dev/null
}

signal_capabilities_valid() {
  printf '%s\n' "$1" | node -e 'const a=JSON.parse(require("fs").readFileSync(0)); process.stdout.write(a.length > 0 && a.every(x => ["READY", "DATA_BLOCKED"].includes(x.capability_status)) ? "1" : "0")' 2>/dev/null
}

watchlist_status() {
  printf '%s\n' "$1" | node -e 'const a=JSON.parse(require("fs").readFileSync(0)); const x=a.find(x => x.symbol === process.argv[1]); process.stdout.write(x && x.execution_status ? x.execution_status : "")' "$2" 2>/dev/null
}

echo "accept-wheel-live: scenario A dead gateway"
start_serve "$tmp/serve-a.log" \
  "FUTU_GATEWAY_URL=http://127.0.0.1:1" \
  "FUTU_PROTO_ADDR=127.0.0.1:1" \
  "LLM_BASE_URL=" "LLM_API_KEY=" "LLM_MODEL=" \
  "TELEGRAM_API_BASE_URL=" "WBOT_CONFIG_DIR="
health_code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$serve_base/v1/health")"
check "场景 A /v1/health 仍为 200" 200 "$health_code"

strict_code="$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$serve_base/v1/watchlist/$sym_a" \
  -H 'Content-Type: application/json' -d '{"strategy":"wheel","params":{"max_inventory":100}}')"
check "HTTP PUT 缺 curve 仍严格 400" 400 "$strict_code"
params_a='{"strategy":"wheel","params":{"full_position_price":80,"zero_position_price":120,"max_inventory":1000,"min_option_quality":0,"trade_gap":0}}'
put_code="$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$serve_base/v1/watchlist/$sym_a" \
  -H 'Content-Type: application/json' -d "$params_a")"
check "场景 A 绑定有效 wheel 配置" 200 "$put_code"
signals_a="$(wait_for_signal "$sym_a" 20)"
check "场景 A 产生 DATA_BLOCKED 信号" 1 "$(count "$signals_a" '"capability_status":"DATA_BLOCKED"')"
check "场景 A capability 只落 DATA_BLOCKED/READY" 1 "$(signal_capabilities_valid "$signals_a")"
watchlist_a="$(curl -s -m 5 "$serve_base/v1/watchlist")"
check "场景 A watchlist status 同步 DATA_BLOCKED" DATA_BLOCKED "$(watchlist_status "$watchlist_a" "$sym_a")"
check "场景 A stderr 有 per-symbol 失败" 1 "$(grep -c "wheelrun: $sym_a:" "$tmp/serve-a.log" 2>/dev/null || true)"
check "场景 A LLM env 缺失时有启动 warning" 1 "$(grep -c 'wheel: WARN LLM reviewer disabled' "$tmp/serve-a.log" 2>/dev/null || true)"
id_a="$(signal_id "$signals_a")"
actions_a='[]'
if [[ -n "$id_a" ]]; then
  actions_a="$(curl -s -m 5 "$serve_base/v1/wheel/signals/$id_a/actions")"
fi
check "场景 A 无 LLM 审核记录" 1 "$(printf '%s\n' "$actions_a" | grep -c '^\[\]$')"
stop_serve
remove_a_rc=0
"$bin" watchlist remove -dsn "$dsn" -symbol "$sym_a" >/dev/null 2>&1 || remove_a_rc=$?
check "场景 A 清理 watchlist 绑定" 0 "$remove_a_rc"

echo "accept-wheel-live: scenario B fake quote + LLM + Telegram"
fake_bin="$tmp/fake-wheel-live"
if ! go build -o "$fake_bin" ./scripts/fake-wheel-live >"$tmp/fake-build.log" 2>&1; then
  echo "accept-wheel-live: fake gateway build failed" >&2
  sed -n '1,120p' "$tmp/fake-build.log" >&2
  exit 1
fi
"$fake_bin" >"$tmp/fake.log" 2>&1 &
fake_pid=$!
fake_base=""
proto_addr=""
for _ in $(seq 1 30); do
  fake_base="$(sed -n 's/^FAKE_BASE_URL=//p' "$tmp/fake.log" | head -n 1)"
  proto_addr="$(sed -n 's/^PROTO_ADDR=//p' "$tmp/fake.log" | head -n 1)"
  if [[ -n "$fake_base" && -n "$proto_addr" ]]; then
    break
  fi
  sleep 1
done
check "场景 B fake gateway 已监听" 1 "$([[ -n "$fake_base" && -n "$proto_addr" ]] && echo 1 || echo 0)"

config_dir="$tmp/wbot-config"
mkdir -p "$config_dir"
printf '%s\n' '{"credentials.telegram.token":{"value":"test-token","updated_at":"2026-08-11T00:00:00Z"},"credentials.telegram.chat_ids":{"value":"1","updated_at":"2026-08-11T00:00:00Z"}}' >"$config_dir/wbot.conf"
chmod 600 "$config_dir/wbot.conf"
start_serve "$tmp/serve-b.log" \
  --telegram-run \
  "FUTU_GATEWAY_URL=$fake_base" \
  "FUTU_PROTO_ADDR=$proto_addr" \
  "LLM_BASE_URL=$fake_base/v1" "LLM_API_KEY=accept-key" "LLM_MODEL=accept-model" \
  "TELEGRAM_API_BASE_URL=$fake_base" "WBOT_CONFIG_DIR=$config_dir"
health_code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$serve_base/v1/health")"
check "场景 B /v1/health 仍为 200" 200 "$health_code"

out_b="$(FUTU_GATEWAY_URL="$fake_base" "$bin" watchlist add -dsn "$dsn" -symbol "$sym_b" -strategy wheel 2>&1)"
add_b_rc=$?
check "CLI wheel 默认档 add 成功" 0 "$add_b_rc"
check "默认 max_inventory=100" 1 "$(printf '%s\n' "$out_b" | grep -c '"max_inventory":100')"
check "默认曲线包含 0.8×P 与目标库存 100" 1 "$(printf '%s\n' "$out_b" | grep -c '"price":80,"target_inventory":100')"
check "默认曲线包含 1.2×P 与目标库存 0" 1 "$(printf '%s\n' "$out_b" | grep -c '"price":120,"target_inventory":0')"

signals_b="$(wait_for_signal "$sym_b" 90)"
check "场景 B wheel_signals 有 ALERT" 1 "$(count "$signals_b" '"action":"ALERT"')"
check "场景 B signal capability 合法" 1 "$(signal_capabilities_valid "$signals_b")"
watchlist_b="$(curl -s -m 5 "$serve_base/v1/watchlist")"
check "场景 B watchlist status 同步 READY" READY "$(watchlist_status "$watchlist_b" "$sym_b")"
id_b="$(signal_id "$signals_b")"
actions_b='[]'
if [[ -n "$id_b" ]]; then
  actions_b="$(curl -s -m 5 "$serve_base/v1/wheel/signals/$id_b/actions")"
fi
check "场景 B 有 LLM_REVIEW 记录" 1 "$(count "$actions_b" '"action":"LLM_REVIEW"')"
check "场景 B fake LLM verdict APPROVE" 1 "$(count "$actions_b" '"verdict":"APPROVE"')"

dismissed="0"
for _ in $(seq 1 45); do
  dismissed="$(sql -c "SELECT count(*) FROM wheel_signal_dismissals WHERE symbol = '$sym_b' AND dismiss_date = CURRENT_DATE;")"
  if [[ "$dismissed" == "1" ]]; then
    break
  fi
  sleep 1
done
check "场景 B Telegram dismiss 当日静默生效" 1 "$([[ "$dismissed" == "1" ]] && echo 1 || echo 0)"
check "场景 B fake Telegram 至少收到提醒" 1 "$(grep -c 'fake-telegram: sendMessage=1' "$tmp/fake.log" 2>/dev/null || true)"
stop_serve
remove_b_rc=0
"$bin" watchlist remove -dsn "$dsn" -symbol "$sym_b" >/dev/null 2>&1 || remove_b_rc=$?
check "场景 B 清理 watchlist 绑定" 0 "$remove_b_rc"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
  exit 1
fi

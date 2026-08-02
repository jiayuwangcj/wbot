#!/usr/bin/env bash
# Acceptance: agent federation (wbot master HTTP contract + wbot agent
# -master-url e2e registration). Verifies real HTTP behavior against a
# live in-memory master:
#   - POST /v1/register: first {new:true}, repeat {new:false}
#   - GET /v1/agents lists registered ids
#   - error contract (plain text, non-S5): 405 wrong method, 415 curl -d
#     default form content-type, 400 JSON header + bad body, 404 unknown path
#   - wbot agent -master-url registers itself and shows up in /v1/agents
#
# Usage: scripts/accept-agent-federation.sh
# Requires: go, curl.
set -uo pipefail
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
has() { local hay="$1" pat="$2"; if grep -qE "$pat" <<<"$hay"; then echo 1; else echo 0; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

builddir="$(mktemp -d)"
WB="$builddir/wbot"
go build -o "$WB" ./cmd/wbot

addr="127.0.0.1:18091"
base="http://$addr"
"$WB" master -listen "$addr" >/dev/null 2>&1 &
mp=$!
cleanup() { kill -INT "$mp" 2>/dev/null; wait "$mp" 2>/dev/null; rm -rf "$builddir"; }
trap cleanup EXIT
sleep 0.5

# --- register contract ---
r1="$(curl -s -X POST "$base/v1/register" -H 'Content-Type: application/json' -d '{"id":"acc-agent-1"}')"
check "首次 register → new=true" 1 "$(has "$r1" '"new":true')"
r2="$(curl -s -X POST "$base/v1/register" -H 'Content-Type: application/json' -d '{"id":"acc-agent-1"}')"
check "重复 register → new=false" 1 "$(has "$r2" '"new":false')"
r3="$(curl -s -X POST "$base/v1/register" -H 'Content-Type: application/json' -d '{"id":"acc-agent-2"}')"
check "第二个 id 登记成功" 1 "$(has "$r3" '"new":true')"

# --- list contract ---
agents="$(curl -s "$base/v1/agents")"
check "agents 列出 acc-agent-1" 1 "$(has "$agents" 'acc-agent-1')"
check "agents 列出 acc-agent-2" 1 "$(has "$agents" 'acc-agent-2')"

# --- error contract (plain-text, non-S5) ---
check "非 POST register → 405" 405 "$(curl -s -o /dev/null -w '%{http_code}' "$base/v1/register")"
check "curl -d 默认 form 头 → 415" 415 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base/v1/register" -d '{"id":"x"}')"
check "JSON 头 + 坏体 → 400" 400 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base/v1/register" -H 'Content-Type: application/json' -d 'not-json')"
check "非 GET agents → 405" 405 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base/v1/agents")"
check "未知路径 → 404" 404 "$(curl -s -o /dev/null -w '%{http_code}' "$base/nope")"

# --- e2e: wbot agent -master-url registers itself ---
"$WB" agent -id acc-e2e -master-url "$base" -duration 2s -interval 100ms >/dev/null 2>&1 &
ap=$!
wait "$ap"
agents2="$(curl -s "$base/v1/agents")"
check "agent -master-url e2e 注册自己" 1 "$(has "$agents2" 'acc-e2e')"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

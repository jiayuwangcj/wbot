#!/usr/bin/env bash
# Acceptance: wbot paper one-shot simulated submit (no network, no DB).
# Verifies real CLI exit codes and output contract:
#   - defaults: DEMO.US buy, exit 0, prints side=/status=/id=
#   - -symbol/-side overrides and side aliases (b/B/s/S)
#   - unknown side → exit 2; invalid symbol → exit 1
#
# Usage: scripts/accept-paper.sh
# Requires: go.
set -uo pipefail
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
has() { local hay="$1" pat="$2"; if grep -qE "$pat" <<<"$hay"; then echo 1; else echo 0; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

builddir="$(mktemp -d)"
trap 'rm -rf "$builddir"' EXIT
go build -o "$builddir/wbot" ./cmd/wbot
WB="$builddir/wbot"

# --- happy path: defaults ---
out="$("$WB" paper 2>&1)"; code=$?
check "默认 → exit 0" 0 "$code"
check "默认 symbol=DEMO.US" 1 "$(has "$out" 'DEMO\.US')"
check "默认 side=buy" 1 "$(has "$out" 'side=buy')"
check "输出含 status=/id=" 1 "$(has "$out" 'status=[0-9]+ id=paper-[0-9]+')"

# --- overrides ---
out2="$("$WB" paper -symbol V.US -side sell 2>&1)"; code2=$?
check "显式 -symbol/-side → exit 0" 0 "$code2"
check "输出含 V.US side=sell" 1 "$(has "$out2" 'V\.US side=sell')"

# --- side aliases ---
out3="$("$WB" paper -side B 2>&1)"; code3=$?
check "别名 B → exit 0" 0 "$code3"
out4="$("$WB" paper -side S 2>&1)"; code4=$?
check "别名 S → exit 0" 0 "$code4"

# --- error contract ---
out5="$("$WB" paper -side bogus 2>&1)"; code5=$?
check "非法 side → exit 2" 2 "$code5"
check "非法 side stderr 提示" 1 "$(has "$out5" 'unknown side "bogus"')"
out6="$("$WB" paper -symbol '' -side buy 2>&1)"; code6=$?
check "空 symbol → exit 1(引擎校验)" 1 "$code6"
check "空 symbol 错误提示" 1 "$(has "$out6" 'invalid symbol')"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

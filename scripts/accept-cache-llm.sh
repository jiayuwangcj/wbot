#!/usr/bin/env bash
# Acceptance: approved strategy-cache evidence is selectively injected into the LLM prompt.
set -uo pipefail

pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test ./internal/llmstrategy -run '^TestRunOnceInjectsOnlyEligibleStrategyCacheSummary$/valid_cache$' -count=1 >/dev/null
check "有效缓存 → prompt 含压缩摘要" 0 "$?"
go test ./internal/llmstrategy -run '^TestRunOnceInjectsOnlyEligibleStrategyCacheSummary$/empty_cache$' -count=1 >/dev/null
check "空缓存 → prompt 不含摘要" 0 "$?"
go test ./internal/llmstrategy -run '^TestRunOnceInjectsOnlyEligibleStrategyCacheSummary$/expired_cache$' -count=1 >/dev/null
check "过期缓存 → prompt 不含摘要" 0 "$?"
go test ./internal/llmstrategy -run '^TestRunOnceInjectsOnlyEligibleStrategyCacheSummary$/unqualified_state$' -count=1 >/dev/null
check "不合格 approved_state → prompt 不含摘要" 0 "$?"

idempotent=0
grep -q 'ON CONFLICT (symbol) DO UPDATE SET' internal/wheelstore/strategy_cache.go &&
  grep -q 'TestStrategyCacheUpsertIntegration' internal/wheelstore/integration_test.go && idempotent=1
check "-cache 同 symbol/report 重复执行为覆盖写" 1 "$idempotent"

echo
if [[ "$failed" == 0 ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi

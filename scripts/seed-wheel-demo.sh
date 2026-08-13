#!/usr/bin/env bash
# Seed deterministic Wheel demo/acceptance inputs: fwd bars, one structured
# watchlist config version, and one complete immutable option snapshot per bar.
# This is synthetic test data only; source=demo-fixture must never be presented
# as a live provider adapter.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="${1:-$HOME/.wbot/dev/wbot}"
dsn="${2:-${WBOT_PG_DSN:-}}"
shift $(( $# >= 2 ? 2 : $# ))
if (( $# == 0 )); then
	symbols=(BTEXEC.US)
else
	symbols=("$@")
fi

if [[ -z "$dsn" ]]; then
	echo "seed-wheel-demo: need dsn (arg 2 or \$WBOT_PG_DSN)" >&2
	exit 2
fi
command -v go >/dev/null || {
	echo "seed-wheel-demo: go is required" >&2
	exit 2
}

params='{"full_position_price":90,"zero_position_price":130,"max_inventory":100,"move_interval_pct":0,"min_premium_per_share":0,"stock_switch_pct":0,"trade_gap":10,"min_dte":5,"max_dte":10,"min_option_quality":0,"strategic_state":"NORMAL"}'

for symbol in "${symbols[@]}"; do
	if [[ ! "$symbol" =~ ^[A-Za-z0-9._-]+$ ]]; then
		echo "seed-wheel-demo: invalid symbol $symbol" >&2
		exit 2
	fi
	"$bin" ingest mock -dsn "$dsn" -symbol "$symbol" -timeframe 1d -adjust fwd >/dev/null
	"$bin" watchlist add -dsn "$dsn" -symbol "$symbol" -strategy wheel -params "$params" >/dev/null
done

(cd "$root" && go run ./scripts/seed-wheel-demo.go "$dsn" "${symbols[@]}")

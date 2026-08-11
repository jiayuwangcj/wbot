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

params='{"price_position_curve":[{"price":90,"target_inventory":100},{"price":130,"target_inventory":0}],"max_inventory":100,"lot_size":100,"min_dte":5,"max_dte":10,"min_option_quality":0,"max_daily_orders":1,"extreme_max_daily_orders":2,"no_trade_gap":10,"strategic_state":"NORMAL"}'

for symbol in "${symbols[@]}"; do
	if [[ ! "$symbol" =~ ^[A-Za-z0-9._-]+$ ]]; then
		echo "seed-wheel-demo: invalid symbol $symbol" >&2
		exit 2
	fi
	"$bin" ingest mock -dsn "$dsn" -symbol "$symbol" -timeframe 1d -adjust fwd >/dev/null
	"$bin" watchlist add -dsn "$dsn" -symbol "$symbol" -strategy wheel -params "$params" >/dev/null
done

(cd "$root" && go run ./scripts/seed-wheel-demo.go "$dsn" "${symbols[@]}")

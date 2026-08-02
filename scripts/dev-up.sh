#!/usr/bin/env bash
# One-shot local dev environment bring-up (OrbStack): auto-discover the
# PostgreSQL and futu-opend gateway addresses, build the wbot binary, start
# serve, seed demo data (idempotent), then run the endpoint acceptance smoke.
#
# Usage:
#   scripts/dev-up.sh [--force] [--no-seed] [--no-smoke]
#
#   --force     restart serve even if it already answers on :8080
#   --no-seed   skip demo data seeding
#   --no-smoke  skip the endpoint acceptance sweep
#
# Environment (kept when already set):
#   WBOT_PG_DSN        — PostgreSQL DSN; auto-discovered from OrbStack bridge
#                        containers (wbot-pg-ci-test / wbot-pg-bridge / wbot-pg-host)
#   FUTU_GATEWAY_URL   — futu REST gateway (http://<bridge-ip>:22222)
#   FUTU_PROTO_ADDR    — futu proto endpoint (<bridge-ip>:11111)
#
# Requires: go, docker (for address discovery), curl.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

listen=":8080"
devdir="${HOME}/.wbot/dev"
bin="${devdir}/wbot"
log="${devdir}/serve.log"
force=0
seed=1
smoke=1

while [[ $# -gt 0 ]]; do
	case "$1" in
	--force) force=1 ;;
	--no-seed) seed=0 ;;
	--no-smoke) smoke=0 ;;
	*)
		echo "unknown option: $1" >&2
		exit 2
		;;
	esac
	shift
done

say() { printf '\033[1;34m[dev-up]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[dev-up] FAIL\033[0m %s\n' "$*" >&2; }

# tcp_ok <host> <port> — bash /dev/tcp probe, no nc needed.
tcp_ok() {
	timeout 2 bash -c "echo > /dev/tcp/$1/$2" >/dev/null 2>&1
}

# container_ip <name> — first bridge network IP of an OrbStack container.
container_ip() {
	docker inspect "$1" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || true
}

# --- 1. address discovery ---------------------------------------------------

if [[ -z "${WBOT_PG_DSN:-}" ]]; then
	pg_ip=""
	for c in wbot-pg-ci-test wbot-pg-bridge wbot-pg-host; do
		ip="$(container_ip "$c")"
		if [[ -n "$ip" ]] && tcp_ok "$ip" 5432; then
			pg_ip="$ip"
			say "postgres: found $c at $ip:5432 (bridge)"
			break
		fi
	done
	if [[ -z "$pg_ip" ]]; then
		fail "no PostgreSQL container reachable. Start one:"
		fail "  docker run -d --name wbot-pg-ci-test -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=wbot_test postgres:16-alpine"
		exit 1
	fi
	export WBOT_PG_DSN="postgres://postgres:postgres@${pg_ip}:5432/wbot_test?sslmode=disable"
fi

if [[ -z "${FUTU_GATEWAY_URL:-}" ]]; then
	futu_ip="$(container_ip futu-opend-rs)"
	if [[ -n "$futu_ip" ]]; then
		export FUTU_GATEWAY_URL="http://${futu_ip}:22222"
		export FUTU_PROTO_ADDR="${futu_ip}:11111"
		say "futu gateway: found futu-opend-rs at $futu_ip (REST 22222 / proto 11111)"
	else
		say "futu gateway: container not found — gateway endpoints will be unavailable"
	fi
fi

say "dsn=${WBOT_PG_DSN}"
[[ -n "${FUTU_GATEWAY_URL:-}" ]] && say "gateway=${FUTU_GATEWAY_URL} proto=${FUTU_PROTO_ADDR:-n/a}"

# --- 2. build ---------------------------------------------------------------

mkdir -p "$devdir"
old_sum=""
if [[ -f "$bin" ]]; then
	old_sum="$(md5sum "$bin" | cut -d' ' -f1)"
fi
say "building $bin"
go build -o "$bin" ./cmd/wbot
new_sum="$(md5sum "$bin" | cut -d' ' -f1)"

# --- 3. serve ---------------------------------------------------------------

base_url="http://127.0.0.1:$(echo "$listen" | sed 's/^://')"
already_up=0
if curl -sf -o /dev/null "${base_url}/v1/admin/cluster" 2>/dev/null; then
	already_up=1
fi

# 二进制内容变化(源码改动)且 serve 已 up → 自动重启,免手动 --force
# (旧行为:serve 一直在跑时复用旧进程,服务端改动验收会误判)。
if [[ "$already_up" == "1" && "$force" == "0" ]]; then
	if [[ -n "$old_sum" && "$old_sum" != "$new_sum" ]]; then
		say "rebuilt binary differs from running serve; restarting"
		force=1
	else
		say "serve already up on $listen (use --force to restart)"
	fi
fi
if [[ "$already_up" == "1" && "$force" == "1" ]]; then
	port="$(echo "$listen" | sed 's/^://')"
	pids="$(ss -tlnp 2>/dev/null | grep -E "[:.]${port}[[:space:]]" | grep -oP 'pid=\K[0-9]+' | sort -u)"
	for p in $pids; do
		kill "$p" && say "stopped old serve (pid $p)"
	done
	sleep 1
fi

if [[ "$already_up" == "0" || "$force" == "1" ]]; then
	say "starting serve on $listen (log: $log)"
	setsid nohup env WBOT_PG_DSN="$WBOT_PG_DSN" FUTU_GATEWAY_URL="${FUTU_GATEWAY_URL:-}" \
		FUTU_PROTO_ADDR="${FUTU_PROTO_ADDR:-}" "$bin" serve -listen "$listen" \
		>"$log" 2>&1 </dev/null &
	disown

	for _ in $(seq 1 30); do
		if curl -sf -o /dev/null "${base_url}/v1/admin/cluster" 2>/dev/null; then
			say "serve ready on $listen"
			break
		fi
		sleep 1
	done
	curl -sf -o /dev/null "${base_url}/v1/admin/cluster" || {
		fail "serve did not become ready; tail of $log:"
		tail -5 "$log" >&2
		exit 1
	}
fi

# --- 4. demo data (idempotent: bars upsert via ON CONFLICT) ------------------

if [[ "$seed" == "1" ]]; then
	say "seeding demo data"
	# 数据页覆盖展示: 不复权 bars (mock)
	"$bin" ingest mock -dsn "$WBOT_PG_DSN" -symbol DEMO.US -timeframe 1d >/dev/null
	"$bin" ingest mock -dsn "$WBOT_PG_DSN" -symbol QUERY.US -timeframe 1d >/dev/null
	# 回测页可跑: 前复权 bars (回测默认 adjust=fwd)
	"$bin" ingest mock -dsn "$WBOT_PG_DSN" -symbol BTEXEC.US -timeframe 1d -adjust fwd >/dev/null
	"$bin" ingest mock -dsn "$WBOT_PG_DSN" -symbol BTEXECB.US -timeframe 1d -adjust fwd >/dev/null
	# 观察列表(回测 watchlist 模式): 全部 fwd 可跑
	for entry in "BTEXEC.US|buy-hold" "BTEXECB.US|buy-hold"; do
		sym="${entry%%|*}"; strat="${entry##*|}"
		curl -sf -X PUT "${base_url}/v1/watchlist/${sym}" \
			-H 'Content-Type: application/json' \
			-d "{\"strategy\":\"${strat}\"}" >/dev/null || true
	done
	say "seeded: DEMO.US / QUERY.US (none) + BTEXEC.US / BTEXECB.US (fwd) + watchlist"
fi

# --- 5. acceptance smoke -----------------------------------------------------

if [[ "$smoke" == "1" ]]; then
	say "running acceptance smoke on $base_url"
	pass=0
	failed=0

	check() {
		local desc="$1" want="$2" got="$3"
		if [[ "$got" == "$want" ]]; then
			pass=$((pass + 1))
			printf '  \033[32mPASS\033[0m %s\n' "$desc"
		else
			failed=$((failed + 1))
			printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$desc" "$want" "$got"
		fi
	}

	check "GET /ui/ (Dashboard)" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/ui/")"
	check "GET /ui/watchlist.html" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/ui/watchlist.html")"
	check "GET /ui/results.html (回测)" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/ui/results.html")"
	check "GET /ui/data.html (数据)" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/ui/data.html")"
	check "GET /ui/admin.html" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/ui/admin.html")"
	check "GET /v1/strategies" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/strategies")"
	check "GET /v1/watchlist" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/watchlist")"
	check "GET /v1/backtests?limit=1" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/backtests?limit=1")"
	check "GET /v1/admin/cluster" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/admin/cluster")"
	check "POST /v1/backtests (回测可跑)" 201 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base_url/v1/backtests" -H 'Content-Type: application/json' -d '{"symbol":"BTEXEC.US","strategy":"buy-hold"}')"

	echo
	if [[ "$failed" == "0" ]]; then
		printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
	else
		printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
		exit 1
	fi
fi

say "done — UI at $base_url/ui/ (DSN in ~/.wbot/dev; serve log: $log)"

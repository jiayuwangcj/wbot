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
# Requires: go, docker (for address discovery), curl, python3 (CLI url 供数).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# 仅绑 loopback(2026-08-03, 安全边界收敛): 默认形态无鉴权,不暴露
# 到局域网; API.md「安全边界」段同声明。base_url/port 提取兼容两格式。
listen="127.0.0.1:8080"
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

port="$(echo "$listen" | sed 's/.*://')"
base_url="http://127.0.0.1:${port}"
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
	port="$(echo "$listen" | sed 's/.*://')"
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
	# 产品演示只使用结构化 Wheel。fixture 同时写入 fwd bars、版本配置和
	# 每根 bar 的完整原子期权 snapshot；source=demo-fixture 明确不是实时源。
	"$root/scripts/seed-wheel-demo.sh" "$bin" "$WBOT_PG_DSN" BTEXEC.US BTEXECB.US
	say "seeded: DEMO.US / QUERY.US (none) + BTEXEC.US / BTEXECB.US (Wheel fixtures)"
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
	# 静态资源缓存语义(2026-08-03): go:embed 文件零 modtime 会使 FileServer
	# 不发 Last-Modified、永不 304;webui 以二进制 mtime 打戳 + no-cache,
	# 浏览器每次重新验证而非全量重下,改前端代码后也绝不拿旧版。
	check "GET /ui/style.css Cache-Control: no-cache" "no-cache" "$(curl -sI "$base_url/ui/style.css" | grep -i '^cache-control:' | tr -d '\r' | awk '{print $2}')"
	check "GET /ui/style.css 有 Last-Modified" 1 "$(curl -sI "$base_url/ui/style.css" | grep -ic '^last-modified:')"
	style_lm="$(curl -sI "$base_url/ui/style.css" | grep -i '^last-modified:' | sed 's/^[Ll]ast-[Mm]odified:[[:space:]]*//' | tr -d '\r')"
	check "条件请求 304 (style.css)" 304 "$(curl -s -o /dev/null -w '%{http_code}' -H "If-Modified-Since: $style_lm" "$base_url/ui/style.css")"
	check "GET /v1/strategies" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/strategies")"
	check "GET /v1/watchlist" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/watchlist")"
	check "GET /v1/wheel/configs" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/wheel/configs?limit=2")"
	check "GET /v1/wheel/signals" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/wheel/signals?limit=2")"
	check "GET /v1/backtests?limit=1" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/backtests?limit=1")"
	check "GET /v1/admin/cluster" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/admin/cluster")"
	check "POST /v1/backtests (Wheel 回测可跑)" 201 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base_url/v1/backtests" -H 'Content-Type: application/json' -d '{"symbol":"BTEXEC.US","strategy":"wheel","params":{"price_position_curve":[{"price":90,"target_inventory":100},{"price":130,"target_inventory":0}],"max_inventory":100,"lot_size":100,"min_dte":5,"max_dte":10,"min_option_quality":0,"max_daily_orders":1,"extreme_max_daily_orders":2,"no_trade_gap":10,"strategic_state":"NORMAL"}}')"
	# DB-local 端点补齐(2026-08-03): 与网关无关,dev-up 种子数据后应恒 200;
	# futu 系端点依赖网关,由 scripts/accept-*.sh 覆盖,不入 dev-up。
	check "GET /v1/health" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/health")"
	check "GET /v1/runs" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/runs")"
	check "GET /v1/bars (DEMO.US 1d)" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/bars?symbol=DEMO.US&timeframe=1d")"
	check "GET /v1/account/snapshots" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/account/snapshots")"
	check "GET /v1/admin/status" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/admin/status")"
	check "GET /v1/admin/config" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$base_url/v1/admin/config")"

	# CLI 三维补漏(2026-08-03, ACCEPTANCE.md 纪律②对账): ingest file/url/status/bars
	# 三维零覆盖。file/url 写 PG、status/bars 读 PG,归 dev-up(verify 零依赖、
	# accept 走 HTTP 面,均不覆盖 CLI 面)。url 用 python3 http.server 就地供数。
	cli_fixture="$devdir/cli-bars.json"
	cat > "$cli_fixture" <<'EOF'
[{"ts":"2026-07-01T00:00:00Z","open":100,"high":105,"low":99,"close":103,"volume":1000},
 {"ts":"2026-07-02T00:00:00Z","open":103,"high":110,"low":102,"close":108,"volume":2000}]
EOF
	# 注: ①ingest file/url 无 -adjust 参数,落库恒 adjust=none;ingest bars 默认
	# -adjust fwd,查询须显式 -adjust none 才能命中(2026-08-03 实测)。
	# ②ingest bars 按会话时区渲染(+08 本机),grep 日期前缀而非 UTC Z 后缀。
	"$bin" ingest file -dsn "$WBOT_PG_DSN" -symbol CLI.US -timeframe 1d -file "$cli_fixture" >/dev/null || true
	check "ingest file→bars 落库 (CLI.US)" 1 "$("$bin" ingest bars -dsn "$WBOT_PG_DSN" -symbol CLI.US -timeframe 1d -adjust none 2>&1 | grep -c '2026-07-01' || true)"
	url_port=18089
	python3 -m http.server "$url_port" --directory "$devdir" >/dev/null 2>&1 &
	url_pid=$!
	sleep 0.5
	"$bin" ingest url -dsn "$WBOT_PG_DSN" -symbol CLIURL.US -timeframe 1d -url "http://127.0.0.1:$url_port/cli-bars.json" >/dev/null || true
	url_rc=$?
	kill "$url_pid" 2>/dev/null || true
	wait "$url_pid" 2>/dev/null || true
	if [[ "$url_rc" -eq 0 ]]; then
		got_url="$("$bin" ingest bars -dsn "$WBOT_PG_DSN" -symbol CLIURL.US -timeframe 1d -adjust none 2>&1 | grep -c '2026-07-01' || true)"
	else
		got_url="0"
	fi
	check "ingest url→bars 落库 (CLIURL.US)" 1 "$got_url"
	check "ingest status 列最近 runs" 0 "$( "$bin" ingest status -dsn "$WBOT_PG_DSN" 2>&1 | grep -qE 'cli-mock|cli-file|cli-url' && echo 0 || echo 1 )"

	echo
	if [[ "$failed" == "0" ]]; then
		printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
	else
		printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
		exit 1
	fi
fi

say "done — UI at $base_url/ui/ (DSN in ~/.wbot/dev; serve log: $log)"

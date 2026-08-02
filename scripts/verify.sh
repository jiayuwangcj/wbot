#!/usr/bin/env bash
# Local pre-push checks aligned with CI (tests/vet + built binary CLI smoke).
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test ./... -count=1
go vet ./...

bin="$(mktemp)"
tmp="$(mktemp -d)"
trap 'rm -f "$bin"; rm -rf "$tmp"' EXIT
go build -o "$bin" ./cmd/wbot

"$bin" -version >/dev/null
"$bin" master -duration 1ms 2>/dev/null
"$bin" agent -duration 1ms -interval 1ms
"$bin" paper -symbol V.US -side buy >/dev/null
"$bin" serve -h >/dev/null 2>&1
"$bin" backtest -h >/dev/null 2>&1
# 与 ci.yml test job 的 CLI smoke 对齐(2026-08-03 对账补齐):
# 未注册 provider → exit 2;configyaml 渲染 dotenv。
"$bin" ingest mock -provider nope >/dev/null 2>&1 && { echo "verify: ingest mock -provider nope should exit non-zero"; exit 1; } || true
cp tools/config.yaml.example "$tmp/config.yaml"
chmod 600 "$tmp/config.yaml"
tools/config-to-env.sh "$tmp/config.yaml" >/dev/null
"$bin" configyaml -file "$tmp/config.yaml" >/dev/null
# 与 ci.yml test job 的自包含 accept 对齐(2026-08-03):
# paper + agent federation 无外部依赖,本地验收即远程验收。
scripts/accept-paper.sh >/dev/null
scripts/accept-agent-federation.sh >/dev/null
echo "verify: ok"

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
cp tools/config.yaml.example "$tmp/config.yaml"
chmod 600 "$tmp/config.yaml"
tools/config-to-env.sh "$tmp/config.yaml" >/dev/null
echo "verify: ok"

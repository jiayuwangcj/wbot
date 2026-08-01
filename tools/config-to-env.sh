#!/usr/bin/env bash
# Render a wbot config.yaml to KEY=VALUE dotenv lines (compose --env-file / shell source; see doc/FUTU.md).
set -euo pipefail
path="${1:-$HOME/.wbot/config.yaml}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
bin="$(mktemp)"
trap 'rm -f "$bin"' EXIT
go build -o "$bin" ./cmd/wbot
"$bin" configyaml -file "$path"

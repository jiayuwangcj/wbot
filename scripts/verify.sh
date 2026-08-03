#!/usr/bin/env bash
# Local pre-push checks ≡ CI test job (gofmt/test/vet/race/staticcheck +
# binary CLI smoke + zero-dependency accept). README.md 本地开发节以此为门。
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# gofmt（与 ci.yml test job "Check gofmt" 步骤一致）
files=$(git ls-files '*.go')
if [ -n "$files" ]; then
  bad=$(gofmt -l $files)
  if [ -n "$bad" ]; then
    echo "gofmt needed on:"
    echo "$bad"
    exit 1
  fi
fi

go test ./... -count=1
go vet ./...
go test -race ./... -count=1

# staticcheck（与 ci.yml test job "Run staticcheck" 步骤一致；@latest 同 CI）
if ! command -v staticcheck >/dev/null 2>&1; then
  echo "verify: staticcheck not found — run: go install honnef.co/go/tools/cmd/staticcheck@latest" >&2
  exit 1
fi
staticcheck ./...

# 交叉编译 release 矩阵(release.sh 五目标一致)——平台专用代码(如 flock
# build tag)回归在此暴露,republish 前必过(2026-08-03 实测 Windows 缺
# syscall.Flock,修复后加此门禁)。
for t in 'linux:amd64' 'linux:arm64' 'darwin:amd64' 'darwin:arm64' 'windows:amd64'; do
  goos=${t%%:*}; goarch=${t##*:}
  GOOS="$goos" GOARCH="$goarch" go build ./cmd/wbot ./internal/futu/
done

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

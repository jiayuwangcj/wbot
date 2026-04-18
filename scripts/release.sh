#!/usr/bin/env bash
# Cross-build wbot CLI archives under ./dist and optionally create a GitHub Release.
#
# Usage:
#   scripts/release.sh build [--version VER] [--dist DIR]
#   scripts/release.sh publish [--version VER] [--dist DIR] [--notes FILE | --generate-notes]
#
# Environment:
#   GH_TOKEN / GITHUB_TOKEN — for gh when publishing non-interactively.
#
# Typical publish (tag created on remote first):
#   git tag -a v1.0.0 -m "v1.0.0" && git push origin v1.0.0
#   scripts/release.sh publish --version v1.0.0
#
# Or let gh create the tag from main:
#   scripts/release.sh publish --version v1.0.0 --generate-notes
#
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

cmd="${1:-}"
if [[ "$cmd" != "build" && "$cmd" != "publish" ]]; then
	echo "usage: $0 build|publish [--version VER] [--dist DIR]" >&2
	echo "  build    — cross-compile; writes tar.gz/zip + SHA256SUMS under dist/" >&2
	echo "  publish  — build then gh release create (needs gh + auth)" >&2
	exit 2
fi
shift

dist="${root}/dist"
rel_version=""
notes_file=""
generate_notes="0"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--dist)
		dist="$2"
		shift 2
		;;
	--version)
		rel_version="$2"
		shift 2
		;;
	--notes)
		notes_file="$2"
		shift 2
		;;
	--generate-notes)
		generate_notes="1"
		shift
		;;
	*)
		echo "unknown option: $1" >&2
		exit 2
		;;
	esac
done

if [[ -z "$rel_version" ]]; then
	rel_version="$(git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")"
fi
version_ldflags="$rel_version"

checksum_write() {
	local dir="$1"
	(
		cd "$dir"
		shopt -s nullglob
		local files=(./*.tar.gz ./*.zip)
		shopt -u nullglob
		if [[ ${#files[@]} -eq 0 ]]; then
			echo "release: no archives to checksum" >&2
			exit 1
		fi
		if command -v sha256sum >/dev/null 2>&1; then
			sha256sum "${files[@]}" >SHA256SUMS
		elif command -v shasum >/dev/null 2>&1; then
			shasum -a 256 "${files[@]}" >SHA256SUMS
		else
			echo "release: need sha256sum or shasum" >&2
			exit 1
		fi
	)
}

rm -rf "$dist"
mkdir -p "$dist"

echo "release: version=$rel_version -> $dist"

export CGO_ENABLED=0

build_target() {
	local goos="$1" goarch="$2" archive_kind="$3"
	local ext=""
	[[ "$goos" == "windows" ]] && ext=".exe"

	local name="wbot_${rel_version}_${goos}_${goarch}"
	local bindir
	bindir="$(mktemp -d "${TMPDIR:-/tmp}/wbot-release.XXXXXX")"

	GOOS="$goos" GOARCH="$goarch" go build -trimpath \
		-ldflags "-s -w -X main.version=${version_ldflags}" \
		-o "${bindir}/wbot${ext}" ./cmd/wbot

	if [[ "$archive_kind" == "zip" ]]; then
		local zout="${dist}/${name}.zip"
		if command -v zip >/dev/null 2>&1; then
			(
				cd "$bindir"
				zip -q "$zout" "wbot${ext}"
			)
		elif command -v python3 >/dev/null 2>&1; then
			python3 - "$zout" "${bindir}/wbot${ext}" <<'PY'
import sys, zipfile
out, exe = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED) as z:
    z.write(exe, arcname="wbot.exe")
PY
		else
			echo "release: need \`zip\` or python3 to build the Windows archive" >&2
			exit 1
		fi
		echo "  wrote ${name}.zip"
	else
		tar -C "$bindir" -czf "${dist}/${name}.tar.gz" "wbot${ext}"
		echo "  wrote ${name}.tar.gz"
	fi
	rm -rf "$bindir"
}

build_target linux amd64 tar
build_target linux arm64 tar
build_target darwin amd64 tar
build_target darwin arm64 tar
build_target windows amd64 zip

checksum_write "$dist"
echo "release: checksums -> ${dist}/SHA256SUMS"

if [[ "$cmd" == "publish" ]]; then
	if ! command -v gh >/dev/null 2>&1; then
		echo "release: publish requires GitHub CLI (https://cli.github.com/)" >&2
		exit 1
	fi

	tag="$rel_version"
	if [[ "$tag" != v* ]]; then
		tag="v${tag}"
	fi

	gh_args=(release create "$tag")
	shopt -s nullglob
	for f in "$dist"/*.tar.gz "$dist"/*.zip "$dist"/SHA256SUMS; do
		[[ -f "$f" ]] || continue
		gh_args+=("$f")
	done
	shopt -u nullglob

	if [[ ${#gh_args[@]} -le 3 ]]; then
		echo "release: no assets to upload" >&2
		exit 1
	fi

	if [[ "$generate_notes" == "1" ]]; then
		gh_args+=(--generate-notes --target "${GITHUB_RELEASE_TARGET:-main}")
	elif [[ -n "$notes_file" ]]; then
		gh_args+=(--notes-file "$notes_file")
	else
		gh_args+=(--notes "Release ${tag}")
	fi

	echo "release: gh ${gh_args[*]}"
	gh "${gh_args[@]}"
	echo "release: published ${tag}"
fi

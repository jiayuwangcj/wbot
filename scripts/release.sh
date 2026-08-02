#!/usr/bin/env bash
# Cross-build wbot CLI archives under ./dist and optionally create a GitHub Release.
#
# Usage:
#   scripts/release.sh build [--version VER] [--dist DIR]
#   scripts/release.sh publish [--version VER] [--dist DIR] [--notes FILE | --generate-notes]
#   scripts/release.sh republish [--version VER] [--dist DIR] [--notes FILE]
#   scripts/release.sh deploy [--version VER] [--dir DIR]
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
# Daily build refresh (doc/RELEASE_DAILY.md):
#   scripts/release.sh publish --version daily-YYYYMMDD      # first publish of the day
#   scripts/release.sh republish --version daily-YYYYMMDD    # re-tag at HEAD + replace release
#   scripts/release.sh deploy --version daily-YYYYMMDD       # ops: fetch + verify + unpack
#
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

cmd="${1:-}"
case "$cmd" in
build | publish | republish | deploy) ;;
*)
	echo "usage: $0 build|publish|republish|deploy [--version VER] [--dist DIR]" >&2
	echo "  build      — cross-compile; writes tar.gz/zip + SHA256SUMS under dist/" >&2
	echo "  publish    — build then gh release create (needs gh + auth)" >&2
	echo "  republish  — daily builds only (vdaily-*): rebuild, delete old release/tag," >&2
	echo "               re-tag at HEAD and recreate the release" >&2
	echo "  deploy     — download release assets, verify SHA256SUMS, unpack wbot into ~/.wbot/releases/" >&2
	exit 2
	;;
esac
shift

dist="${root}/dist"
rel_version=""
notes_file=""
generate_notes="0"
deploy_dir="${HOME}/.wbot/releases"

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
	--dir)
		deploy_dir="$2"
		shift 2
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

# do_build — cross-build all targets + SHA256SUMS into $1 (a fresh dist dir).
do_build() {
	local d="$1"
	rm -rf "$d"
	mkdir -p "$d"
	echo "release: version=$rel_version -> $d"
	build_target linux amd64 tar
	build_target linux arm64 tar
	build_target darwin amd64 tar
	build_target darwin arm64 tar
	build_target windows amd64 zip
	checksum_write "$d"
	echo "release: checksums -> ${d}/SHA256SUMS"
}

# gh_args_for — release assets from a dist dir, as a shell array via stdin
# (printf "%s\0" | mapfile -d ''), because the asset list is dynamic.
gh_args_for() {
	local d="$1"
	shopt -s nullglob
	for f in "$d"/*.tar.gz "$d"/*.zip "$d"/SHA256SUMS; do
		[[ -f "$f" ]] || continue
		printf '%s\0' "$f"
	done
	shopt -u nullglob
}

require_gh() {
	if ! command -v gh >/dev/null 2>&1; then
		echo "release: ${cmd} requires GitHub CLI (https://cli.github.com/)" >&2
		exit 1
	fi
}

tag_of() { # v-prefix normalization (daily-… -> vdaily-…)
	if [[ "$1" != v* ]]; then
		echo "v$1"
	else
		echo "$1"
	fi
}

if [[ "$cmd" == "republish" ]]; then
	# fail fast: validate the tag before paying for a full cross-build
	tag="$(tag_of "$rel_version")"
	if [[ "$tag" != vdaily-* ]]; then
		echo "release: republish is for daily builds (vdaily-*); got $tag" >&2
		echo "  for formal versions, bump and publish a new version instead" >&2
		exit 2
	fi
fi

if [[ "$cmd" != "deploy" ]]; then
	do_build "$dist"
fi

if [[ "$cmd" == "publish" || "$cmd" == "republish" ]]; then
	require_gh
	tag="$(tag_of "$rel_version")"

	if [[ "$cmd" == "republish" ]]; then
		echo "release: republish $tag at $(git rev-parse --short HEAD)"
		gh release delete "$tag" --yes --cleanup-tag >/dev/null 2>&1 || true
		git tag -d "$tag" >/dev/null 2>&1 || true
		git push origin ":refs/tags/$tag" >/dev/null 2>&1 || true
	fi

	# Re-tag at HEAD for both paths: publish (fresh daily tag) and republish.
	git tag -a "$tag" -m "$tag" >/dev/null 2>&1 || true
	git push origin "$tag" >/dev/null 2>&1 || true

	mapfile -d '' -t assets < <(gh_args_for "$dist")
	if [[ ${#assets[@]} -eq 0 ]]; then
		echo "release: no assets to upload" >&2
		exit 1
	fi

	gh_args=(release create "$tag" "${assets[@]}")
	if [[ "$generate_notes" == "1" ]]; then
		gh_args+=(--generate-notes --target "${GITHUB_RELEASE_TARGET:-main}")
	elif [[ -n "$notes_file" ]]; then
		gh_args+=(--notes-file "$notes_file")
	else
		gh_args+=(--notes "Release ${tag}")
	fi

	echo "release: gh ${gh_args[*]:0:6} …"
	gh "${gh_args[@]}"
	echo "release: published ${tag}"
fi

if [[ "$cmd" == "deploy" ]]; then
	require_gh
	tag="$(tag_of "$rel_version")"
	out="${deploy_dir}/${tag#v}"

	echo "release: deploy $tag -> $out"
	rm -rf "$out"
	mkdir -p "$out"
	gh release download "$tag" --dir "$out" --pattern '*linux_amd64*' --pattern 'SHA256SUMS' >/dev/null
	(
		cd "$out"
		shopt -s nullglob
		archive=(./*.tar.gz)
		if [[ ${#archive[@]} -ne 1 ]]; then
			echo "release: expected exactly one linux_amd64 tar.gz, got ${#archive[@]}" >&2
			exit 1
		fi
		# verify only the downloaded archive's entry (SHA256SUMS lists all 5 targets)
		grep -q "$(basename "${archive[0]}")" SHA256SUMS
		(sha256sum -c <(grep -F "$(basename "${archive[0]}")" SHA256SUMS) 2>/dev/null \
			|| shasum -a 256 -c <(grep -F "$(basename "${archive[0]}")" SHA256SUMS)) >/dev/null
		tar -xzf "${archive[0]}"
		chmod +x wbot
		rm -f "${archive[0]}"
		echo "release: deployed wbot to $out (SHA256SUMS verified)"
	)
fi

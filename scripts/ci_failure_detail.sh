#!/usr/bin/env bash
# Fetch GitHub Actions failure detail for this repo (jobs, failed steps, links).
# Uses the GitHub REST API. Optional: GITHUB_TOKEN or GH_TOKEN for higher rate limits,
# private repos, and --download-logs (job log archives require authentication).
#
# Usage:
#   scripts/ci_failure_detail.sh [--branch BRANCH] [--workflow PATH_SUFFIX] [--run RUN_ID]
#   scripts/ci_failure_detail.sh --repo OWNER/REPO ...   # when not in a git clone
#   scripts/ci_failure_detail.sh --download-logs DIR    # save log zip per failed job
#   scripts/ci_failure_detail.sh --fail-on-red          # exit 1 if matched run failed
#
# Exit codes: 0 ok / green, 1 API or missing run / --fail-on-red, 2 usage, 3 run in progress
#
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

usage() {
	sed -n '1,19p' "$0" | sed -e 's/^# \{0,1\}//g'
}

branch="main"
workflow_suffix="ci.yml"
run_id=""
repo_override=""
download_logs=""
fail_on_red="0"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--branch | -b)
		branch="$2"
		shift 2
		;;
	--workflow | -w)
		workflow_suffix="$2"
		shift 2
		;;
	--run | -r)
		run_id="$2"
		shift 2
		;;
	--repo)
		repo_override="$2"
		shift 2
		;;
	--download-logs)
		download_logs="$2"
		shift 2
		;;
	--fail-on-red)
		fail_on_red="1"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

resolve_repo() {
	if [[ -n "$repo_override" ]]; then
		if [[ ! "$repo_override" =~ ^[^/]+/[^/]+$ ]]; then
			echo "ci_failure_detail: --repo must be OWNER/REPO" >&2
			exit 2
		fi
		echo "$repo_override"
		return
	fi
	local url
	url="$(git remote get-url origin 2>/dev/null || true)"
	if [[ -z "$url" ]]; then
		echo "ci_failure_detail: no git remote 'origin' (use --repo OWNER/REPO)" >&2
		exit 2
	fi
	local out
	out="$(python3 -c '
import re, sys
url = sys.argv[1]
m = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?/?$", url)
if not m:
    sys.exit(1)
print(f"{m.group(1)}/{m.group(2)}", end="")
' "$url")" || {
		echo "ci_failure_detail: could not parse owner/repo from: $url" >&2
		exit 2
	}
	echo "$out"
}

github_curl() {
	local url="$1"
	local auth_hdr=()
	if [[ -n "${GITHUB_TOKEN:-}" ]]; then
		auth_hdr=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
	elif [[ -n "${GH_TOKEN:-}" ]]; then
		auth_hdr=(-H "Authorization: Bearer ${GH_TOKEN}")
	fi
	curl -sS "${auth_hdr[@]}" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		"$url"
}

# Python snippets read JSON from stdin (pipe), not from shell heredocs (those steal stdin).
pick_run_py=$(
	cat <<'PY'
import json, os, sys

data = json.load(sys.stdin)
runs = data.get("workflow_runs") or []
workflow_suffix = os.environ["WORKFLOW_SUFFIX"]
run_id = (os.environ.get("RUN_ID_LOOKUP") or "").strip()

if run_id:
    rid = int(run_id)
    for r in runs:
        if r.get("id") == rid:
            print(json.dumps(r))
            sys.exit(0)
    sys.exit(2)

for r in runs:
    path = r.get("path") or ""
    if path.endswith(workflow_suffix):
        print(json.dumps(r))
        sys.exit(0)
sys.exit(3)
PY
)

summarize_run_py=$(
	cat <<'PY'
import json, sys

r = json.load(sys.stdin)
print(
    json.dumps({
        "id": r.get("id"),
        "html_url": r.get("html_url"),
        "head_sha": r.get("head_sha"),
        "head_branch": r.get("head_branch"),
        "status": r.get("status"),
        "conclusion": r.get("conclusion"),
        "name": r.get("name"),
        "path": r.get("path"),
        "created_at": r.get("created_at"),
    })
)
PY
)

print_jobs_py=$(
	cat <<'PY'
import json, sys

jobs = json.load(sys.stdin).get("jobs") or []
failed = [j for j in jobs if j.get("conclusion") == "failure"]
other_bad = [
    j
    for j in jobs
    if j.get("conclusion") not in ("success", "skipped", None)
    and j.get("conclusion") != "failure"
]
print("Jobs:")
for j in jobs:
    c = j.get("conclusion") or "-"
    print(f"  - {j.get('name')}: {c}")
print("")
if not failed and not other_bad:
    print("No failed jobs (all green or skipped).")
    sys.exit(0)
for j in failed + other_bad:
    print(f"=== {j.get('name')} [{j.get('conclusion')}] ===")
    print(f"job_url: {j.get('html_url')}")
    for s in j.get("steps") or []:
        name = s.get("name")
        cr = s.get("conclusion")
        if cr == "failure":
            print(f"  FAILED STEP: {name}")
        elif cr not in ("success", "skipped", None):
            print(f"  step {name}: {cr}")
    print("")
PY
)

failed_job_ids_py=$(
	cat <<'PY'
import json, sys

jobs = json.load(sys.stdin).get("jobs") or []
ids = [str(j["id"]) for j in jobs if j.get("conclusion") == "failure"]
print(" ".join(ids))
PY
)

repo_slug="$(resolve_repo)"
owner="${repo_slug%%/*}"
repo="${repo_slug#*/}"

api_root="https://api.github.com/repos/${owner}/${repo}"

main() {
	local runs_json run_json
	if [[ -n "$run_id" ]]; then
		run_json="$(github_curl "${api_root}/actions/runs/${run_id}")"
		if ! echo "$run_json" | python3 -c 'import json,sys; j=json.load(sys.stdin); sys.exit(0 if j.get("id") else 1)'; then
			local msg
			msg="$(echo "$run_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("message",""))' 2>/dev/null || true)"
			echo "ci_failure_detail: run ${run_id} not found: ${msg:-bad response}" >&2
			exit 1
		fi
	else
		runs_json="$(github_curl "${api_root}/actions/runs?branch=${branch}&event=push&per_page=20")"
		if [[ -z "$runs_json" ]]; then
			echo "ci_failure_detail: empty response from GitHub API (network or rate limit)" >&2
			exit 1
		fi
		set +e
		run_json="$(echo "$runs_json" | WORKFLOW_SUFFIX="$workflow_suffix" RUN_ID_LOOKUP="" python3 -c "$pick_run_py")"
		local pick_status=$?
		set -e
		if [[ "$pick_status" -eq 3 ]] || [[ -z "$run_json" ]]; then
			echo "ci_failure_detail: no workflow run matching path *${workflow_suffix} on branch ${branch}" >&2
			exit 1
		fi
		if [[ "$pick_status" -eq 2 ]]; then
			echo "ci_failure_detail: run id not found in recent runs list (try --run explicitly)" >&2
			exit 1
		fi
	fi

	local summary
	summary="$(echo "$run_json" | python3 -c "$summarize_run_py")"

	local jid status conclusion html_url sha
	jid="$(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
	status="$(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')"
	conclusion="$(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("conclusion") or "")')"
	html_url="$(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin)["html_url"])')"
	sha="$(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("head_sha") or "")')"
	head_br="$(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("head_branch") or "")')"
	if [[ -z "$head_br" ]]; then
		head_br="$branch"
	fi

	echo "Workflow: $(echo "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("name") or "")') (${repo_slug})"
	echo "Run:      ${jid}"
	echo "SHA:      ${sha}"
	echo "Branch:   ${head_br}"
	echo "Status:   ${status}"
	echo "Result:   ${conclusion:-<pending>}"
	echo "URL:      ${html_url}"
	echo ""

	if [[ "$status" != "completed" ]]; then
		echo "ci_failure_detail: run not finished yet (status=${status})." >&2
		exit 3
	fi

	local jobs_json
	jobs_json="$(github_curl "${api_root}/actions/runs/${jid}/jobs")"

	echo "$jobs_json" | python3 -c "$print_jobs_py"

	local failed_ids
	failed_ids="$(echo "$jobs_json" | python3 -c "$failed_job_ids_py")"

	if [[ -n "$download_logs" && -n "$failed_ids" ]]; then
		mkdir -p "$download_logs"
		for fj in $failed_ids; do
			local zip_path="${download_logs}/job-${fj}.zip"
			echo "Downloading logs for job ${fj} -> ${zip_path}"
			local auth_args=()
			if [[ -n "${GITHUB_TOKEN:-}" ]]; then
				auth_args=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
			elif [[ -n "${GH_TOKEN:-}" ]]; then
				auth_args=(-H "Authorization: Bearer ${GH_TOKEN}")
			else
				echo "ci_failure_detail: --download-logs needs GITHUB_TOKEN or GH_TOKEN for job log archive." >&2
				exit 1
			fi
			curl -sS -L "${auth_args[@]}" \
				-H "Accept: application/vnd.github+json" \
				-H "X-GitHub-Api-Version: 2022-11-28" \
				-o "$zip_path" \
				"${api_root}/actions/jobs/${fj}/logs"
			echo "Saved: ${zip_path} (zip of log files; unzip -l ${zip_path})"
		done
	fi

	if [[ "$fail_on_red" == "1" && "$conclusion" == "failure" ]]; then
		exit 1
	fi
	if [[ "$fail_on_red" == "1" && "$conclusion" != "success" ]]; then
		exit 1
	fi
}

main "$@"

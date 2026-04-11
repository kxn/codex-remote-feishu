#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
usage: bash scripts/check/release-readiness.sh --issue <number> [--repo <owner/name>] [--require-closed]

Validates that a release tracker issue is ready to publish:
- has label release:tracker
- has a milestone whose title exactly matches the tracker version
- has all release-check boxes checked
- leaves no open non-release:stretch issues in the milestone
EOF
}

die() {
  echo "release readiness: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

first_nonempty_line() {
  sed '/^[[:space:]]*$/d' | head -n1 | tr -d '\r'
}

derive_track_from_version() {
  local version="$1"
  case "${version}" in
    *-alpha.*) echo "alpha" ;;
    *-beta.*) echo "beta" ;;
    *) echo "production" ;;
  esac
}

extract_heading_section() {
  local body="$1"
  local heading="$2"
  awk -v heading="${heading}" '
    function normalize(line) {
      sub(/^#{2,3}[[:space:]]+/, "", line)
      sub(/[[:space:]]+$/, "", line)
      return line
    }
    /^#{2,3}[[:space:]]+/ {
      if (capture) {
        exit
      }
      if (normalize($0) == heading) {
        capture = 1
        next
      }
    }
    capture {
      print
    }
  ' <<<"${body}"
}

repo="${REPOSITORY:-${GITHUB_REPOSITORY:-}}"
issue_number=""
require_closed=0
gh_bin="${GH_BIN:-gh}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --issue)
      issue_number="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --require-closed)
      require_closed=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "${issue_number}" ]] || { usage; die "--issue is required"; }
require_cmd "${gh_bin}"
require_cmd jq

if [[ -z "${repo}" ]]; then
  repo="$("${gh_bin}" repo view --json nameWithOwner --jq .nameWithOwner)"
fi
[[ -n "${repo}" ]] || die "could not resolve repository"

issue_json="$("${gh_bin}" api "repos/${repo}/issues/${issue_number}")"
repo_json="$("${gh_bin}" api "repos/${repo}")"
state="$(jq -r '.state // ""' <<<"${issue_json}" | tr '[:upper:]' '[:lower:]')"
body="$(jq -r '.body // ""' <<<"${issue_json}" | tr -d '\r')"
milestone_number="$(jq -r '.milestone.number // empty' <<<"${issue_json}")"
milestone_title="$(jq -r '.milestone.title // empty' <<<"${issue_json}")"
default_branch="$(jq -r '.default_branch // empty' <<<"${repo_json}")"

jq -e '.labels[]? | select(.name == "release:tracker")' >/dev/null <<<"${issue_json}" || die "issue #${issue_number} is not labeled release:tracker"
[[ -n "${default_branch}" ]] || die "could not resolve repository default branch"

if [[ "${require_closed}" -eq 1 && "${state}" != "closed" ]]; then
  die "issue #${issue_number} must be closed before release"
fi

version="$(
  extract_heading_section "${body}" "版本号" | first_nonempty_line || true
)"
[[ -n "${version}" ]] || die "tracker issue is missing a 版本号 section"

track="$(
  extract_heading_section "${body}" "发布轨道" | first_nonempty_line || true
)"
if [[ -z "${track}" ]]; then
  track="$(derive_track_from_version "${version}")"
fi
track="${track,,}"

release_ref="$(
  extract_heading_section "${body}" "发布分支" | first_nonempty_line || true
)"
if [[ -z "${release_ref}" ]]; then
  release_ref="${default_branch}"
fi
encoded_release_ref="$(
  jq -rn --arg value "${release_ref}" '$value|@uri'
)"
if ! "${gh_bin}" api "repos/${repo}/branches/${encoded_release_ref}" >/dev/null 2>&1; then
  die "release ref ${release_ref} does not exist in repo ${repo}"
fi

case "${track}" in
  production)
    version_pattern='^v[0-9]+\.[0-9]+\.[0-9]+$'
    ;;
  beta|alpha)
    version_pattern="^v[0-9]+\\.[0-9]+\\.[0-9]+-${track}\\.[0-9]+$"
    ;;
  *)
    die "unsupported release track in tracker issue: ${track}"
    ;;
esac

if ! [[ "${version}" =~ ${version_pattern} ]]; then
  die "version ${version} does not match track ${track}"
fi

[[ -n "${milestone_number}" && -n "${milestone_title}" ]] || die "tracker issue must have a milestone"
if [[ "${milestone_title}" != "${version}" ]]; then
  die "tracker milestone ${milestone_title} does not match tracker version ${version}"
fi

release_checks="$(extract_heading_section "${body}" "发布前检查")"
[[ -n "${release_checks//[[:space:]]/}" ]] || die "tracker issue is missing 发布前检查 items"
if grep -Eq '^[[:space:]]*-[[:space:]]\[[[:space:]]\]' <<<"${release_checks}"; then
  die "tracker issue still has unchecked 发布前检查 items"
fi

milestone_open_json="$("${gh_bin}" api "repos/${repo}/issues?state=open&milestone=${milestone_number}&per_page=100")"
blocking_issues="$(
  jq -r --argjson tracker "${issue_number}" '
    .[]
    | select(.pull_request | not)
    | select(.number != $tracker)
    | select(([.labels[]?.name] | index("release:stretch")) | not)
    | "#\(.number) \(.title)"
  ' <<<"${milestone_open_json}"
)"
stretch_open_count="$(
  jq -r --argjson tracker "${issue_number}" '
    [
      .[]
      | select(.pull_request | not)
      | select(.number != $tracker)
      | select([.labels[]?.name] | index("release:stretch"))
    ] | length
  ' <<<"${milestone_open_json}"
)"

if [[ -n "${blocking_issues}" ]]; then
  die "blocking milestone issues remain:\n${blocking_issues}"
fi

echo "status: ready"
echo "repo: ${repo}"
echo "issue: #${issue_number}"
echo "ref: ${release_ref}"
echo "track: ${track}"
echo "version: ${version}"
echo "milestone: ${milestone_title}"
echo "stretch open: ${stretch_open_count}"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "ref=${release_ref}"
    echo "track=${track}"
    echo "version=${version}"
    echo "milestone=${milestone_title}"
    echo "stretch_open=${stretch_open_count}"
  } >> "${GITHUB_OUTPUT}"
fi

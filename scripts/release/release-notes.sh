#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

version="${VERSION:-}"
latest_tag="${LATEST_TAG:-}"

if [[ -z "${version}" ]]; then
  echo "VERSION is required." >&2
  exit 1
fi

if [[ -n "${latest_tag}" ]]; then
  mapfile -t commits < <(git log --reverse --format='%h%x09%s' "${latest_tag}..HEAD")
else
  mapfile -t commits < <(git log --reverse --format='%h%x09%s')
fi

breaking=()
features=()
fixes=()
maintenance=()

for line in "${commits[@]}"; do
  hash="${line%%$'\t'*}"
  subject="${line#*$'\t'}"
  item="- ${subject} (${hash})"

  if [[ "${subject}" =~ BREAKING[[:space:]]CHANGE ]] || [[ "${subject}" =~ ^[^[:space:]]+(\(.+\))?!: ]]; then
    breaking+=("${item}")
  elif [[ "${subject}" =~ ^feat(\(.+\))?: ]] || [[ "${subject}" =~ ^(Add|Implement|Create|Support)[[:space:]] ]]; then
    features+=("${item}")
  elif [[ "${subject}" =~ ^(fix(\(.+\))?:|Fix|Handle|Correct)[[:space:]] ]]; then
    fixes+=("${item}")
  else
    maintenance+=("${item}")
  fi
done

print_section() {
  local title="$1"
  shift
  if [[ "$#" -eq 0 ]]; then
    return
  fi
  echo "## ${title}"
  printf '%s\n' "$@"
  echo
}

echo "Release ${version}"
echo
if [[ -n "${latest_tag}" ]]; then
  echo "Changes since ${latest_tag}."
else
  echo "Initial release."
fi
echo
echo "## Install"
echo
echo "Latest macOS / Linux install:"
echo
echo '```bash'
echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash"
echo '```'
echo
echo "Pin this version:"
echo
echo '```bash'
echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version ${version}"
echo '```'
echo

print_section "Breaking Changes" "${breaking[@]}"
print_section "Features" "${features[@]}"
print_section "Fixes" "${fixes[@]}"
print_section "Maintenance" "${maintenance[@]}"

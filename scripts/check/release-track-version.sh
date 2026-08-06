#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_PATH="${ROOT_DIR}/scripts/release/next-version.sh"

work_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

repo_dir="${work_dir}/repo"
mkdir -p "${repo_dir}"
cd "${repo_dir}"

git init -q
git config user.name "Codex Remote Tests"
git config user.email "tests@example.com"

run_next_version() {
  local track="$1"
  local bump="${2:-auto}"

  RELEASE_TRACK="${track}" \
  BUMP_OVERRIDE="${bump}" \
  ROOT_DIR_OVERRIDE="${repo_dir}" \
  bash "${SCRIPT_PATH}"
}

assert_eq() {
  local actual="$1"
  local expected="$2"
  local message="$3"

  if [[ "${actual}" != "${expected}" ]]; then
    echo "${message}: expected ${expected}, got ${actual}" >&2
    exit 1
  fi
}

assert_fails() {
  local output_file
  output_file="$(mktemp)"
  if "$@" >"${output_file}" 2>&1; then
    echo "Expected command to fail: $*" >&2
    sed 's/^/  /' "${output_file}" >&2
    rm -f "${output_file}"
    exit 1
  fi
  rm -f "${output_file}"
}

cat <<'EOF' > README.md
test
EOF
git add README.md
git commit -qm "feat: initial release plumbing"

assert_eq "$(run_next_version production auto)" "v0.1.0" "initial production version"
assert_eq "$(run_next_version beta auto)" "v0.1.0-beta.1" "initial beta version"

git tag v0.1.0

cat <<'EOF' >> README.md
beta
EOF
git add README.md
git commit -qm "feat: add prerelease release track"

assert_eq "$(run_next_version alpha auto)" "v0.2.0-alpha.1" "first alpha version after production"
git tag v0.2.0-alpha.1

assert_eq "$(run_next_version alpha auto)" "v0.2.0-alpha.2" "alpha sequence increments on same core"
assert_eq "$(run_next_version beta auto)" "v0.2.0-beta.1" "beta can start from same production core"
git tag v0.2.0-beta.1

assert_eq "$(run_next_version beta auto)" "v0.2.0-beta.2" "beta sequence increments on same core"
assert_eq "$(run_next_version production auto)" "v0.2.0" "production ignores prerelease tags when computing core"

git tag v0.2.0
assert_fails run_next_version production auto

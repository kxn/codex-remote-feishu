#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_PATH="${ROOT_DIR}/scripts/release/verify-release-assets.sh"
work_dir="$(mktemp -d)"

cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

dist_dir="${work_dir}/dist"
mkdir -p "${dist_dir}"
printf 'alpha\n' >"${dist_dir}/alpha.txt"
printf 'beta\n' >"${dist_dir}/beta.txt"

fake_gh="${work_dir}/gh"
cat >"${fake_gh}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" != "release" || "${2:-}" != "view" || "${3:-}" != "dev-latest" ]]; then
  echo "unexpected fake gh call: $*" >&2
  exit 1
fi

count_file="${FAKE_COUNT_FILE:?missing FAKE_COUNT_FILE}"
count=0
if [[ -f "${count_file}" ]]; then
  count="$(<"${count_file}")"
fi
count=$((count + 1))
printf '%s' "${count}" >"${count_file}"

case "${FAKE_SCENARIO:-ready}" in
  transient)
    if [[ "${count}" -lt 3 ]]; then
      echo "release not found" >&2
      exit 1
    fi
    printf '%s\n' beta.txt alpha.txt
    ;;
  missing)
    printf '%s\n' alpha.txt
    ;;
  *)
    printf '%s\n' beta.txt alpha.txt
    ;;
esac
EOF
chmod +x "${fake_gh}"

count_file="${work_dir}/count"
FAKE_SCENARIO=transient FAKE_COUNT_FILE="${count_file}" GH_BIN="${fake_gh}" \
  VERIFY_RELEASE_ASSETS_ATTEMPTS=4 VERIFY_RELEASE_ASSETS_DELAY=0 \
  bash "${SCRIPT_PATH}" dev-latest "${dist_dir}"
if [[ "$(<"${count_file}")" != "3" ]]; then
  echo "expected transient scenario to succeed on third attempt" >&2
  exit 1
fi

missing_output="${work_dir}/missing-output.txt"
if FAKE_SCENARIO=missing FAKE_COUNT_FILE="${work_dir}/missing-count" GH_BIN="${fake_gh}" \
  VERIFY_RELEASE_ASSETS_ATTEMPTS=2 VERIFY_RELEASE_ASSETS_DELAY=0 \
  bash "${SCRIPT_PATH}" dev-latest "${dist_dir}" >"${missing_output}" 2>&1; then
  echo "expected missing asset scenario to fail" >&2
  exit 1
fi
grep -F "missing release asset: beta.txt" "${missing_output}" >/dev/null

echo "release assets verify selftest: ok"

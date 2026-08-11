#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/verify-release-assets.sh <release_tag> <dist_dir>

Verifies that every file directly under dist_dir exists as a GitHub Release
asset. The GitHub Release API can lag briefly after create/upload, so the
asset query and missing-asset check are retried.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -ne 2 ]]; then
  usage >&2
  exit 1
fi

release_tag="$1"
dist_dir="$2"
gh_bin="${GH_BIN:-gh}"
attempts="${VERIFY_RELEASE_ASSETS_ATTEMPTS:-12}"
delay="${VERIFY_RELEASE_ASSETS_DELAY:-5}"

if ! [[ "${attempts}" =~ ^[0-9]+$ ]] || [[ "${attempts}" -lt 1 ]]; then
  echo "VERIFY_RELEASE_ASSETS_ATTEMPTS must be a positive integer" >&2
  exit 1
fi
if ! [[ "${delay}" =~ ^[0-9]+$ ]]; then
  echo "VERIFY_RELEASE_ASSETS_DELAY must be a non-negative integer" >&2
  exit 1
fi
if [[ ! -d "${dist_dir}" ]]; then
  echo "dist dir not found: ${dist_dir}" >&2
  exit 1
fi

mapfile -t expected_assets < <(
  cd "${dist_dir}"
  shopt -s nullglob
  for path in *; do
    [[ -f "${path}" ]] || continue
    printf '%s\n' "${path}"
  done | sort
)

if [[ "${#expected_assets[@]}" -eq 0 ]]; then
  echo "no release artifacts found in ${dist_dir}" >&2
  exit 1
fi

assets_file="$(mktemp)"
error_file="$(mktemp)"
cleanup() {
  rm -f "${assets_file}" "${error_file}"
}
trap cleanup EXIT

missing_assets=()
release_assets=()
last_error=""

for ((attempt = 1; attempt <= attempts; attempt++)); do
  : >"${assets_file}"
  : >"${error_file}"
  missing_assets=()
  release_assets=()

  if "${gh_bin}" release view "${release_tag}" --json assets --jq '.assets[].name' >"${assets_file}" 2>"${error_file}"; then
    mapfile -t release_assets < <(sort "${assets_file}")
    for expected in "${expected_assets[@]}"; do
      if ! printf '%s\n' "${release_assets[@]}" | grep -Fx "${expected}" >/dev/null; then
        missing_assets+=("${expected}")
      fi
    done
    if [[ "${#missing_assets[@]}" -eq 0 ]]; then
      exit 0
    fi
    last_error="missing release asset(s): ${missing_assets[*]}"
  else
    last_error="$(tr -d '\r' <"${error_file}" | tail -n 1)"
    if [[ -z "${last_error}" ]]; then
      last_error="gh release view failed"
    fi
  fi

  if [[ "${attempt}" -lt "${attempts}" ]]; then
    echo "release asset verification retry ${attempt}/${attempts}: ${last_error}" >&2
    sleep "${delay}"
  fi
done

echo "release asset verification failed for ${release_tag}: ${last_error}" >&2
if [[ "${#missing_assets[@]}" -gt 0 ]]; then
  for missing in "${missing_assets[@]}"; do
    echo "missing release asset: ${missing}" >&2
  done
fi
exit 1

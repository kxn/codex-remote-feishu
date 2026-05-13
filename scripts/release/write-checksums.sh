#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/write-checksums.sh <dist_dir>
EOF
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 1
fi

dist_dir="$1"
if [[ ! -d "${dist_dir}" ]]; then
  echo "dist dir not found: ${dist_dir}" >&2
  exit 1
fi

dist_dir="$(cd "${dist_dir}" && pwd)"

checksum_cmd=(sha256sum)
if ! command -v "${checksum_cmd[0]}" >/dev/null 2>&1; then
  checksum_cmd=(shasum -a 256)
fi

(
  cd "${dist_dir}"
  shopt -s nullglob
  checksum_files=()
  while IFS= read -r file; do
    checksum_files+=("${file}")
  done < <(
    for path in *; do
      [[ -f "${path}" ]] || continue
      [[ "${path}" == "checksums.txt" ]] && continue
      printf '%s\n' "${path}"
    done | sort
  )
  if [[ "${#checksum_files[@]}" -eq 0 ]]; then
    echo "no release artifacts found in ${dist_dir}" >&2
    exit 1
  fi
  "${checksum_cmd[@]}" "${checksum_files[@]}" > checksums.txt
)

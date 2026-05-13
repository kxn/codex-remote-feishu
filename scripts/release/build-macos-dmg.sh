#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/build-macos-dmg.sh --app <path> --output <path> [options]

options:
  --volume-name <name>    volume label to show when mounting the dmg
  -h, --help              show this help
EOF
}

require_mac_toolchain() {
  if ! command -v hdiutil >/dev/null 2>&1; then
    echo "hdiutil is required to build the macOS dmg" >&2
    exit 1
  fi
}

app_path=""
output_path=""
volume_name="Codex Remote Installer"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app)
      app_path="${2:-}"
      shift 2
      ;;
    --output)
      output_path="${2:-}"
      shift 2
      ;;
    --volume-name)
      volume_name="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unexpected argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "${app_path}" || -z "${output_path}" ]]; then
  usage >&2
  exit 1
fi

require_mac_toolchain
if [[ ! -d "${app_path}" ]]; then
  echo "app bundle not found: ${app_path}" >&2
  exit 1
fi

app_path="$(cd "${app_path}" && pwd)"
app_name="$(basename "${app_path}")"
app_parent="$(cd "$(dirname "${app_path}")" && pwd)"
src_root=""
staging_root=""

can_use_parent_directly() {
  local parent_dir="$1"
  local expected_entry="$2"
  local entries=()
  while IFS= read -r -d '' entry_path; do
    entries+=("$(basename "${entry_path}")")
  done < <(find "${parent_dir}" -mindepth 1 -maxdepth 1 -print0)

  [[ "${#entries[@]}" -eq 1 && "${entries[0]}" == "${expected_entry}" ]]
}

cleanup() {
  if [[ -n "${staging_root}" ]]; then
    rm -rf "${staging_root}"
  fi
}
trap cleanup EXIT

if can_use_parent_directly "${app_parent}" "${app_name}"; then
  src_root="${app_parent}"
else
  staging_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-dmg-XXXXXX")"
  cp -R "${app_path}" "${staging_root}/"
  src_root="${staging_root}"
fi

mkdir -p "$(dirname "${output_path}")"
rm -f "${output_path}"

hdiutil create \
  -volname "${volume_name}" \
  -srcfolder "${src_root}" \
  -ov \
  -format UDZO \
  "${output_path}"

printf 'built macOS dmg: %s\n' "${output_path}"

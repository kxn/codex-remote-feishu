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

staging_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-dmg-XXXXXX")"
cleanup() {
  rm -rf "${staging_root}"
}
trap cleanup EXIT

cp -R "${app_path}" "${staging_root}/"
mkdir -p "$(dirname "${output_path}")"
rm -f "${output_path}"

hdiutil create \
  -volname "${volume_name}" \
  -srcfolder "${staging_root}" \
  -ov \
  -format UDZO \
  "${output_path}"

printf 'built macOS dmg: %s\n' "${output_path}"

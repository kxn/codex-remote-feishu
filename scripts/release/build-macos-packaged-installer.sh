#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/build-macos-packaged-installer.sh --version <version> --dist-dir <dir> [options]

options:
  --track <track>         release track: production, beta, or alpha
  --output <path>         output dmg path; defaults under <dir>
  --output-app <path>     optional output .app path; defaults to a temporary path
  -h, --help              show this help
EOF
}

infer_track() {
  case "$1" in
    v*.*.*-alpha.*) printf '%s\n' "alpha" ;;
    v*.*.*-beta.*) printf '%s\n' "beta" ;;
    v*.*.*) printf '%s\n' "production" ;;
    *)
      echo "unable to infer release track from version: $1" >&2
      exit 1
      ;;
  esac
}

validate_track() {
  case "$1" in
    production|beta|alpha) ;;
    *)
      echo "unsupported release track: $1" >&2
      exit 1
      ;;
  esac
}

version=""
dist_dir=""
track=""
output_path=""
output_app=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --dist-dir)
      dist_dir="${2:-}"
      shift 2
      ;;
    --track)
      track="${2:-}"
      shift 2
      ;;
    --output)
      output_path="${2:-}"
      shift 2
      ;;
    --output-app)
      output_app="${2:-}"
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

if [[ -z "${version}" || -z "${dist_dir}" ]]; then
  usage >&2
  exit 1
fi

if [[ ! "${version}" =~ ^v[0-9] ]]; then
  version="v${version}"
fi
if [[ -z "${track}" ]]; then
  track="$(infer_track "${version}")"
fi
validate_track "${track}"

if [[ ! -d "${dist_dir}" ]]; then
  echo "dist directory not found: ${dist_dir}" >&2
  exit 1
fi

dist_dir="$(cd "${dist_dir}" && pwd)"
if [[ -z "${output_path}" ]]; then
  output_path="${dist_dir}/codex-remote-feishu_${version#v}_darwin_universal_installer.dmg"
fi

temp_root=""
cleanup() {
  if [[ -n "${temp_root}" ]]; then
    rm -rf "${temp_root}"
  fi
}
trap cleanup EXIT

if [[ -z "${output_app}" ]]; then
  temp_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-macos-packaged-installer-XXXXXX")"
  output_app="${temp_root}/Install Codex Remote.app"
fi

bash "${ROOT_DIR}/scripts/release/build-macos-installer-app.sh" \
  --version "${version}" \
  --track "${track}" \
  --dist-dir "${dist_dir}" \
  --output-app "${output_app}"

bash "${ROOT_DIR}/scripts/release/build-macos-dmg.sh" \
  --app "${output_app}" \
  --output "${output_path}"

output_dir="$(cd "$(dirname "${output_path}")" && pwd)"
if [[ "${output_dir}" == "${dist_dir}" ]]; then
  bash "${ROOT_DIR}/scripts/release/write-checksums.sh" "${dist_dir}"
fi

printf 'built macOS packaged installer: %s\n' "${output_path}"

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/build-windows-nsis.sh --version <version> --dist-dir <dir> [options]

options:
  --track <track>         release track: production, beta, or alpha
  --output <path>         output installer path; defaults under <dir>
  --makensis <path>       makensis binary to use; defaults to PATH lookup
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

detect_python() {
  if command -v python3 >/dev/null 2>&1; then
    printf '%s\n' "python3"
    return
  fi
  if command -v python >/dev/null 2>&1; then
    printf '%s\n' "python"
    return
  fi
  echo "python3 or python is required to extract the Windows release archive" >&2
  exit 1
}

version=""
dist_dir=""
track=""
output_path=""
makensis_bin="${MAKENSIS:-}"

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
    --makensis)
      makensis_bin="${2:-}"
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

dist_dir="$(cd "${dist_dir}" && pwd)"
if [[ -z "${output_path}" ]]; then
  output_path="${dist_dir}/codex-remote-feishu_${version#v}_windows_amd64_installer.exe"
fi

if [[ -z "${makensis_bin}" ]]; then
  if ! makensis_bin="$(command -v makensis 2>/dev/null)"; then
    echo "makensis is required; install NSIS and ensure makensis is on PATH" >&2
    exit 1
  fi
fi

archive_path="${dist_dir}/codex-remote-feishu_${version#v}_windows_amd64.zip"
package_dir="codex-remote-feishu_${version#v}_windows_amd64"
script_path="${ROOT_DIR}/deploy/windows/codex-remote-installer.nsi"
python_bin="$(detect_python)"

if [[ ! -f "${archive_path}" ]]; then
  echo "release archive not found: ${archive_path}" >&2
  exit 1
fi
if [[ ! -f "${script_path}" ]]; then
  echo "NSIS script not found: ${script_path}" >&2
  exit 1
fi

extract_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-nsis-XXXXXX")"
cleanup() {
  rm -rf "${extract_root}"
}
trap cleanup EXIT

"${python_bin}" - "${archive_path}" "${extract_root}" <<'PY'
import pathlib
import sys
import zipfile

archive = pathlib.Path(sys.argv[1])
target = pathlib.Path(sys.argv[2])
with zipfile.ZipFile(archive) as bundle:
    bundle.extractall(target)
PY

payload_binary="${extract_root}/${package_dir}/codex-remote.exe"
if [[ ! -f "${payload_binary}" ]]; then
  echo "release payload binary not found after extraction: ${payload_binary}" >&2
  exit 1
fi

mkdir -p "$(dirname "${output_path}")"

"${makensis_bin}" \
  "-DAPP_VERSION=${version}" \
  "-DRELEASE_TRACK=${track}" \
  "-DPAYLOAD_BINARY=${payload_binary}" \
  "-DOUTPUT_FILE=${output_path}" \
  "${script_path}"

printf 'built Windows NSIS installer: %s\n' "${output_path}"

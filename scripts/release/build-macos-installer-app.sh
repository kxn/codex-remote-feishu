#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/build-macos-installer-app.sh --version <version> --dist-dir <dir> [options]

options:
  --track <track>         release track: production, beta, or alpha
  --output-app <path>     output .app path; defaults under <dir>
  --min-macos <version>   deployment target for the installer app (default: 13.0)
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

bundle_versions_for_app() {
  local version="$1"
  local base=""
  local pre_kind=""
  local pre_num=""
  if [[ "${version}" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)(-(alpha|beta)\.([0-9]+))?$ ]]; then
    base="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
    pre_kind="${BASH_REMATCH[5]:-}"
    pre_num="${BASH_REMATCH[6]:-}"
  else
    echo "unsupported app version format for macOS bundle metadata: ${version}" >&2
    exit 1
  fi

  local bundle_version="${base}"
  case "${pre_kind}" in
    alpha)
      bundle_version="${base}a${pre_num}"
      ;;
    beta)
      bundle_version="${base}b${pre_num}"
      ;;
  esac

  printf '%s\n%s\n' "${base}" "${bundle_version}"
}

require_mac_toolchain() {
  if ! command -v xcrun >/dev/null 2>&1; then
    echo "xcrun is required to build the macOS installer app" >&2
    exit 1
  fi
  if ! command -v lipo >/dev/null 2>&1; then
    echo "lipo is required to build the macOS installer app" >&2
    exit 1
  fi
}

version=""
dist_dir=""
track=""
output_app=""
min_macos="13.0"

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
    --output-app)
      output_app="${2:-}"
      shift 2
      ;;
    --min-macos)
      min_macos="${2:-}"
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
require_mac_toolchain

if [[ ! -d "${dist_dir}" ]]; then
  echo "dist directory not found: ${dist_dir}" >&2
  exit 1
fi

dist_dir="$(cd "${dist_dir}" && pwd)"
if [[ -z "${output_app}" ]]; then
  output_app="${dist_dir}/Install Codex Remote.app"
fi

archive_amd64="${dist_dir}/codex-remote-feishu_${version#v}_darwin_amd64.tar.gz"
archive_arm64="${dist_dir}/codex-remote-feishu_${version#v}_darwin_arm64.tar.gz"
source_dir="${ROOT_DIR}/deploy/macos/InstallerApp/Sources"
plist_template="${ROOT_DIR}/deploy/macos/InstallerApp/Info.plist.template"
app_exec_name="Install Codex Remote"

for required in "${archive_amd64}" "${archive_arm64}" "${plist_template}"; do
  if [[ ! -f "${required}" ]]; then
    echo "required file not found: ${required}" >&2
    exit 1
  fi
done

build_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-macos-installer-XXXXXX")"
cleanup() {
  rm -rf "${build_root}"
}
trap cleanup EXIT

extract_archive() {
  local archive_path="$1"
  local target_root="$2"
  mkdir -p "${target_root}"
  tar -C "${target_root}" -xzf "${archive_path}"
}

extract_archive "${archive_amd64}" "${build_root}/amd64"
extract_archive "${archive_arm64}" "${build_root}/arm64"

payload_amd64="${build_root}/amd64/codex-remote-feishu_${version#v}_darwin_amd64/codex-remote"
payload_arm64="${build_root}/arm64/codex-remote-feishu_${version#v}_darwin_arm64/codex-remote"
for payload in "${payload_amd64}" "${payload_arm64}"; do
  if [[ ! -f "${payload}" ]]; then
    echo "payload binary not found: ${payload}" >&2
    exit 1
  fi
done

app_root="${build_root}/Install Codex Remote.app"
contents_dir="${app_root}/Contents"
macos_dir="${contents_dir}/MacOS"
resources_dir="${contents_dir}/Resources"
payload_dir="${resources_dir}/payload"
mkdir -p "${macos_dir}" "${payload_dir}"

sdk_path="$(xcrun --sdk macosx --show-sdk-path)"
mapfile -t swift_sources < <(find "${source_dir}" -type f -name '*.swift' | sort)
if [[ "${#swift_sources[@]}" -eq 0 ]]; then
  echo "no Swift sources found under ${source_dir}" >&2
  exit 1
fi

xcrun swiftc \
  -sdk "${sdk_path}" \
  -target "x86_64-apple-macos${min_macos}" \
  -framework Cocoa \
  -parse-as-library \
  "${swift_sources[@]}" \
  -o "${build_root}/installer-x86_64"

xcrun swiftc \
  -sdk "${sdk_path}" \
  -target "arm64-apple-macos${min_macos}" \
  -framework Cocoa \
  -parse-as-library \
  "${swift_sources[@]}" \
  -o "${build_root}/installer-arm64"

lipo -create \
  "${build_root}/installer-x86_64" \
  "${build_root}/installer-arm64" \
  -output "${macos_dir}/${app_exec_name}"

chmod +x "${macos_dir}/${app_exec_name}"
cp "${payload_amd64}" "${payload_dir}/codex-remote-darwin-amd64"
cp "${payload_arm64}" "${payload_dir}/codex-remote-darwin-arm64"
chmod +x "${payload_dir}/codex-remote-darwin-amd64" "${payload_dir}/codex-remote-darwin-arm64"
printf '%s\n' "${version}" > "${resources_dir}/installer-version.txt"
printf '%s\n' "${track}" > "${resources_dir}/installer-track.txt"

bundle_version_output="$(bundle_versions_for_app "${version}")"
mapfile -t bundle_versions <<< "${bundle_version_output}"
app_short_version="${bundle_versions[0]}"
app_bundle_version="${bundle_versions[1]}"

python3 - <<'PY' "${plist_template}" "${contents_dir}/Info.plist" "${app_short_version}" "${app_bundle_version}" "${app_exec_name}"
import pathlib
import sys

template = pathlib.Path(sys.argv[1]).read_text()
output = (
    template.replace("__APP_SHORT_VERSION__", sys.argv[3])
    .replace("__APP_BUNDLE_VERSION__", sys.argv[4])
    .replace("__EXECUTABLE_NAME__", sys.argv[5])
)
pathlib.Path(sys.argv[2]).write_text(output)
PY

rm -rf "${output_app}"
mkdir -p "$(dirname "${output_app}")"
cp -R "${app_root}" "${output_app}"
printf 'built macOS installer app: %s\n' "${output_app}"

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/build-artifacts.sh <version> [output_dir] [options]

options:
  --platform <goos/goarch>   build only the selected platform; may be repeated
  --jobs <count>             build up to <count> platforms concurrently
  --current-platform-only    build only the current host platform
  --skip-admin-ui-build      reuse the existing admin UI dist instead of rebuilding it
  -h, --help                 show this help
EOF
}

detect_goos() {
  case "$(uname -s)" in
    Linux) printf '%s\n' "linux" ;;
    Darwin) printf '%s\n' "darwin" ;;
    *)
      echo "unsupported operating system for artifact build: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_goarch() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s\n' "amd64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    *)
      echo "unsupported architecture for artifact build: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

normalize_platform() {
  case "$1" in
    linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64)
      printf '%s\n' "$1"
      ;;
    *)
      echo "unsupported platform: $1" >&2
      exit 1
      ;;
  esac
}

resolve_build_branch() {
  if [[ -n "${CODEX_REMOTE_BUILD_BRANCH:-}" ]]; then
    printf '%s\n' "${CODEX_REMOTE_BUILD_BRANCH}"
    return
  fi
  local branch=""
  if branch="$(git branch --show-current 2>/dev/null)" && [[ -n "${branch}" ]]; then
    printf '%s\n' "${branch}"
    return
  fi
  if [[ -n "${GITHUB_REF_NAME:-}" ]]; then
    printf '%s\n' "${GITHUB_REF_NAME}"
    return
  fi
  printf '%s\n' "dev"
}

resolve_build_flavor() {
  if [[ -n "${CODEX_REMOTE_BUILD_FLAVOR:-}" ]]; then
    printf '%s\n' "${CODEX_REMOTE_BUILD_FLAVOR}"
    return
  fi
  printf '%s\n' "dev"
}

resolve_package_version_label() {
  if [[ -n "${CODEX_REMOTE_PACKAGE_VERSION_LABEL:-}" ]]; then
    printf '%s\n' "${CODEX_REMOTE_PACKAGE_VERSION_LABEL}"
    return
  fi
  printf '%s\n' "${version#v}"
}

resolve_build_jobs() {
  local platform_count="$1"
  local requested="${build_jobs:-}"
  local resolved=""
  if [[ -z "${requested}" && -n "${CODEX_REMOTE_BUILD_JOBS:-}" ]]; then
    requested="${CODEX_REMOTE_BUILD_JOBS}"
  fi
  if [[ -n "${requested}" ]]; then
    resolved="${requested}"
  else
    if command -v nproc >/dev/null 2>&1; then
      resolved="$(nproc)"
    elif command -v getconf >/dev/null 2>&1; then
      resolved="$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '1')"
    else
      resolved="1"
    fi
  fi
  if ! [[ "${resolved}" =~ ^[0-9]+$ ]] || (( resolved < 1 )); then
    echo "invalid build job count: ${resolved}" >&2
    exit 1
  fi
  if (( resolved > platform_count )); then
    resolved="${platform_count}"
  fi
  printf '%s\n' "${resolved}"
}

build_platform_archive() {
  local goos="$1"
  local goarch="$2"
  local package_name="codex-remote-feishu_${package_version_label}_${goos}_${goarch}"
  local work_dir="${work_root}/${goos}-${goarch}"
  local staging_dir="${work_dir}/${package_name}"
  local archive_path=""
  local extension=""

  echo "building ${goos}/${goarch}"
  rm -rf "${work_dir}"
  mkdir -p "${staging_dir}"

  if [[ "${goos}" == "windows" ]]; then
    extension=".exe"
  fi

  CLOUDFLARED_EMBED_ALLOW_DOWNLOAD=0 \
    bash "${ROOT_DIR}/scripts/externalaccess/prepare-cloudflared-embed.sh" "${goos}" "${goarch}"
  bash "${ROOT_DIR}/scripts/managedshim/prepare-vscode-shim-embed.sh" "${goos}" "${goarch}"
  bash "${ROOT_DIR}/scripts/upgradeshim/prepare-upgrade-shim-embed.sh" "${goos}" "${goarch}"

  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -trimpath -ldflags "-X main.version=${version} -X main.branch=${build_branch} -X github.com/kxn/codex-remote-feishu/internal/buildinfo.FlavorValue=${build_flavor}" \
    -o "${staging_dir}/codex-remote${extension}" ./cmd/codex-remote

  cp README.md QUICKSTART.md CHANGELOG.md "${staging_dir}/"
  cp -R deploy "${staging_dir}/"

  if [[ "${goos}" == "windows" ]]; then
    archive_path="${output_dir}/${package_name}.zip"
    (
      cd "${work_dir}"
      zip -qr "${archive_path}" "${package_name}"
    )
  else
    archive_path="${output_dir}/${package_name}.tar.gz"
    tar -C "${work_dir}" -czf "${archive_path}" "${package_name}"
  fi

  rm -rf "${work_dir}"
  echo "finished ${goos}/${goarch}: ${archive_path}"
}

flush_platform_batch() {
  local failed=0
  local index=""
  for index in "${!batch_pids[@]}"; do
    local pid="${batch_pids[$index]}"
    local platform="${batch_platforms[$index]}"
    local log_path="${batch_logs[$index]}"
    if ! wait "${pid}"; then
      failed=1
      echo "platform build failed: ${platform}" >&2
      cat "${log_path}" >&2 || true
      continue
    fi
    cat "${log_path}"
  done
  batch_pids=()
  batch_platforms=()
  batch_logs=()
  return "${failed}"
}

version=""
output_dir="dist"
skip_admin_ui_build=0
requested_platforms=()
build_jobs=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform)
      requested_platforms+=("$(normalize_platform "${2:-}")")
      shift 2
      ;;
    --jobs)
      build_jobs="${2:-}"
      shift 2
      ;;
    --current-platform-only)
      requested_platforms+=("$(detect_goos)/$(detect_goarch)")
      shift
      ;;
    --skip-admin-ui-build)
      skip_admin_ui_build=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      if [[ -z "${version}" ]]; then
        version="$1"
      elif [[ "${output_dir}" == "dist" ]]; then
        output_dir="$1"
      else
        echo "unexpected argument: $1" >&2
        usage >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [[ -z "${version}" ]]; then
  usage >&2
  exit 1
fi

build_branch="$(resolve_build_branch)"
build_flavor="$(resolve_build_flavor)"
package_version_label="$(resolve_package_version_label)"

if [[ "${skip_admin_ui_build}" == "1" ]]; then
  if [[ ! -f "${ROOT_DIR}/internal/app/daemon/adminui/dist/index.html" ]]; then
    echo "admin UI dist is missing; run scripts/web/build-admin-ui.sh first or omit --skip-admin-ui-build" >&2
    exit 1
  fi
else
  bash "${ROOT_DIR}/scripts/web/build-admin-ui.sh"
fi

rm -rf "${output_dir}"
mkdir -p "${output_dir}"
output_dir="$(cd "${output_dir}" && pwd)"
work_root="${output_dir}/.build-work"
log_root="${output_dir}/.build-logs"
mkdir -p "${work_root}" "${log_root}"

default_platforms=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

platforms=()

if [[ "${#requested_platforms[@]}" -gt 0 ]]; then
  for platform in "${requested_platforms[@]}"; do
    read -r goos goarch <<<"${platform//\// }"
    platforms+=("${goos} ${goarch}")
  done
else
  platforms=("${default_platforms[@]}")
fi

build_jobs="$(resolve_build_jobs "${#platforms[@]}")"
batch_pids=()
batch_platforms=()
batch_logs=()

for platform in "${platforms[@]}"; do
  read -r goos goarch <<<"${platform}"
  platform_name="${goos}/${goarch}"
  log_path="${log_root}/${goos}-${goarch}.log"
  (
    set -euo pipefail
    build_platform_archive "${goos}" "${goarch}"
  ) >"${log_path}" 2>&1 &
  batch_pids+=("$!")
  batch_platforms+=("${platform_name}")
  batch_logs+=("${log_path}")
  if (( ${#batch_pids[@]} >= build_jobs )); then
    flush_platform_batch
  fi
done

if (( ${#batch_pids[@]} > 0 )); then
  flush_platform_batch
fi

cp install-release.sh "${output_dir}/codex-remote-feishu-install.sh"
cp install-release.ps1 "${output_dir}/codex-remote-feishu-install.ps1"

checksum_cmd="sha256sum"
if ! command -v "${checksum_cmd}" >/dev/null 2>&1; then
  checksum_cmd="shasum -a 256"
fi

(
  cd "${output_dir}"
  checksum_files=()
  while IFS= read -r file; do
    checksum_files+=("${file}")
  done < <(find . -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' -o -name '*.sh' -o -name '*.ps1' \) | sort)
  if [[ "${#checksum_files[@]}" -eq 0 ]]; then
    echo "no release artifacts found in ${output_dir}" >&2
    exit 1
  fi
  ${checksum_cmd} "${checksum_files[@]}" > checksums.txt
)

rm -rf "${work_root}" "${log_root}"

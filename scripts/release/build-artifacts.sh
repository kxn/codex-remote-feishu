#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/release/build-artifacts.sh <version> [output_dir] [options]

options:
  --platform <goos/goarch>   build only the selected platform; may be repeated
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

version=""
output_dir="dist"
skip_admin_ui_build=0
requested_platforms=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform)
      requested_platforms+=("$(normalize_platform "${2:-}")")
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

default_platforms=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

archives=()
platforms=()

if [[ "${#requested_platforms[@]}" -gt 0 ]]; then
  for platform in "${requested_platforms[@]}"; do
    read -r goos goarch <<<"${platform//\// }"
    platforms+=("${goos} ${goarch}")
  done
else
  platforms=("${default_platforms[@]}")
fi

for platform in "${platforms[@]}"; do
  read -r goos goarch <<<"${platform}"
  package_name="codex-remote-feishu_${package_version_label}_${goos}_${goarch}"
  staging_dir="${output_dir}/${package_name}"
  mkdir -p "${staging_dir}"

  extension=""
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
      cd "${output_dir}"
      zip -qr "$(basename "${archive_path}")" "$(basename "${staging_dir}")"
    )
  else
    archive_path="${output_dir}/${package_name}.tar.gz"
    tar -C "${output_dir}" -czf "${archive_path}" "$(basename "${staging_dir}")"
  fi

  archives+=("${archive_path}")
  rm -rf "${staging_dir}"
done

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

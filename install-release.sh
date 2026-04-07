#!/usr/bin/env bash
set -euo pipefail

REPO="${CODEX_REMOTE_REPO:-kxn/codex-remote-feishu}"
VERSION="${CODEX_REMOTE_VERSION:-}"
BASE_URL="${CODEX_REMOTE_BASE_URL:-}"
INSTALL_ROOT="${CODEX_REMOTE_INSTALL_ROOT:-}"
SKIP_SETUP="${CODEX_REMOTE_SKIP_SETUP:-0}"
DOWNLOAD_ONLY=0

usage() {
  cat <<'EOF'
Usage: install-release.sh [options] [-- install-args...]

Downloads the latest compatible Codex Remote Feishu release package,
extracts it locally, bootstraps the installed binary, starts the local
daemon, and prints the WebSetup URL.

Options:
  --version <vX.Y.Z>     Install a specific version instead of latest
  --repo <owner/name>    GitHub repository to use
  --install-root <dir>   Directory used to store downloaded releases
  --download-only        Download and extract, but do not run codex-remote install
  -h, --help             Show this help

Environment overrides:
  CODEX_REMOTE_VERSION
  CODEX_REMOTE_REPO
  CODEX_REMOTE_BASE_URL
  CODEX_REMOTE_INSTALL_ROOT
  CODEX_REMOTE_SKIP_SETUP=1

Examples:
  curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
  curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
  curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- -- --install-bin-dir /opt/codex-remote/bin
EOF
}

install_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    --install-root)
      INSTALL_ROOT="${2:-}"
      shift 2
      ;;
    --download-only)
      DOWNLOAD_ONLY=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      install_args=("$@")
      break
      ;;
    *)
      install_args+=("$1")
      shift
      ;;
  esac
done

detect_goos() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      echo "Unsupported operating system for curl installer: $(uname -s)" >&2
      echo "Use the packaged archive for your platform instead." >&2
      exit 1
      ;;
  esac
}

detect_goarch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

default_install_root() {
  local goos="$1"
  case "${goos}" in
    darwin)
      printf '%s\n' "${HOME}/Library/Application Support/codex-remote/releases"
      ;;
    *)
      printf '%s\n' "${XDG_DATA_HOME:-${HOME}/.local/share}/codex-remote/releases"
      ;;
  esac
}

resolve_latest_version() {
  local latest_url
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
  if [[ -z "${latest_url}" ]]; then
    echo "Failed to resolve latest release URL." >&2
    exit 1
  fi
  printf '%s\n' "${latest_url##*/}"
}

goos="$(detect_goos)"
goarch="$(detect_goarch)"
if [[ -z "${VERSION}" ]]; then
  VERSION="$(resolve_latest_version)"
fi
if [[ ! "${VERSION}" =~ ^v[0-9] ]]; then
  VERSION="v${VERSION}"
fi
if [[ -z "${INSTALL_ROOT}" ]]; then
  INSTALL_ROOT="$(default_install_root "${goos}")"
fi
mkdir -p "${INSTALL_ROOT}"

asset_name="codex-remote-feishu_${VERSION#v}_${goos}_${goarch}.tar.gz"
if [[ -z "${BASE_URL}" ]]; then
  asset_url="https://github.com/${REPO}/releases/download/${VERSION}/${asset_name}"
else
  asset_url="${BASE_URL%/}/${asset_name}"
fi

curl_args=(-fsSL)
case "${asset_url}" in
  http://127.0.0.1:*|http://localhost:*|https://127.0.0.1:*|https://localhost:*)
    curl_args+=(--noproxy '*')
    ;;
esac

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

archive_path="${tmp_dir}/${asset_name}"
curl "${curl_args[@]}" "${asset_url}" -o "${archive_path}"
tar -xzf "${archive_path}" -C "${tmp_dir}"

package_dir="${tmp_dir}/codex-remote-feishu_${VERSION#v}_${goos}_${goarch}"
if [[ ! -d "${package_dir}" ]]; then
  echo "Downloaded archive did not contain the expected package directory." >&2
  exit 1
fi

target_dir="${INSTALL_ROOT}/${VERSION}"
rm -rf "${target_dir}"
mkdir -p "$(dirname "${target_dir}")"
mv "${package_dir}" "${target_dir}"
ln -sfn "${target_dir}" "${INSTALL_ROOT}/current"

echo "Downloaded ${VERSION} to ${target_dir}"
echo "Current release link: ${INSTALL_ROOT}/current"

if [[ "${DOWNLOAD_ONLY}" == "1" || "${SKIP_SETUP}" == "1" ]]; then
  exit 0
fi

binary_path="${target_dir}/codex-remote"
if [[ ! -x "${binary_path}" ]]; then
  echo "Downloaded package did not contain an executable codex-remote binary." >&2
  exit 1
fi

exec "${binary_path}" install -binary "${binary_path}" -bootstrap-only -start-daemon ${install_args[@]+"${install_args[@]}"}

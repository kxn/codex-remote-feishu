#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
BUILD_OUTPUT="${BIN_DIR}/codex-remote"
GO_BIN="${GO_BIN:-go}"
BASE_DIR="${HOME}"
UPGRADE_SLOT=""
ALLOW_DIRTY=0

usage() {
  cat <<'EOF'
usage: ./upgrade-local.sh [--base-dir <dir>] [--slot <slot>] [--allow-dirty]

Pull the current branch to the latest upstream commit, rebuild ./bin/codex-remote,
stage it into the fixed local-upgrade artifact path, and trigger the built-in
local upgrade transaction against the installed daemon state.

options:
  --base-dir <dir>  base dir used by the local install state (default: $HOME)
  --slot <slot>     optional explicit upgrade slot label
  --allow-dirty     skip the clean-worktree guard before git pull
  -h, --help        show this help text
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-dir)
      [[ $# -ge 2 ]] || { echo "missing value for --base-dir" >&2; exit 1; }
      BASE_DIR="$2"
      shift 2
      ;;
    --slot)
      [[ $# -ge 2 ]] || { echo "missing value for --slot" >&2; exit 1; }
      UPGRADE_SLOT="$2"
      shift 2
      ;;
    --allow-dirty)
      ALLOW_DIRTY=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

cd "${ROOT_DIR}"

if [[ "${ALLOW_DIRTY}" != "1" ]]; then
  if ! git diff --quiet --ignore-submodules -- || ! git diff --cached --quiet --ignore-submodules --; then
    echo "working tree has uncommitted changes; commit/stash them first or rerun with --allow-dirty" >&2
    exit 1
  fi
fi

state_path="${BASE_DIR}/.local/share/codex-remote/install-state.json"
artifact_dir="${BASE_DIR}/.local/share/codex-remote/local-upgrade"
artifact_path="${artifact_dir}/codex-remote"

printf '[1/4] git pull --ff-only\n'
git pull --ff-only

printf '[2/4] build %s\n' "${BUILD_OUTPUT}"
mkdir -p "${BIN_DIR}"
bash "${ROOT_DIR}/scripts/externalaccess/prepare-cloudflared-embed.sh"
"${GO_BIN}" build -o "${BUILD_OUTPUT}" "${ROOT_DIR}/cmd/codex-remote"

if [[ ! -f "${state_path}" ]]; then
  echo "install state not found: ${state_path}" >&2
  echo "run ./setup.sh first or pass --base-dir for the installed environment" >&2
  exit 1
fi

printf '[3/4] stage local artifact %s\n' "${artifact_path}"
mkdir -p "${artifact_dir}"
cp "${BUILD_OUTPUT}" "${artifact_path}"
chmod +x "${artifact_path}"

printf '[4/4] request built-in local upgrade transaction\n'
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy

cmd=("${BUILD_OUTPUT}" local-upgrade "-base-dir" "${BASE_DIR}")
if [[ -n "${UPGRADE_SLOT}" ]]; then
  cmd+=("-slot" "${UPGRADE_SLOT}")
fi
"${cmd[@]}"

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
BUILD_OUTPUT="${BIN_DIR}/codex-remote"
GO_BIN="${GO_BIN:-go}"
BASE_DIR="${HOME}"
INSTANCE=""
UPGRADE_SLOT=""
ALLOW_DIRTY=0

usage() {
  cat <<'EOF'
usage: ./upgrade-local.sh [--instance <stable|debug>] [--base-dir <dir>] [--slot <slot>] [--allow-dirty]

Pull the current branch to the latest upstream commit, rebuild ./bin/codex-remote,
stage it into the fixed local-upgrade artifact path, and trigger the built-in
local upgrade transaction against the installed daemon state.

options:
  --instance <id>   install instance to upgrade (default: repo-bound instance or stable)
  --base-dir <dir>  base dir used by the local install state (default: $HOME)
  --slot <slot>     optional explicit upgrade slot label
  --allow-dirty     skip the clean-worktree guard before git pull
  -h, --help        show this help text
EOF
}

instance_namespace() {
  local instance="$1"
  if [[ -z "${instance}" || "${instance}" == "stable" ]]; then
    printf 'codex-remote'
    return
  fi
  printf 'codex-remote-%s' "${instance}"
}

instance_state_root() {
  local base_dir="$1"
  local instance="$2"
  local namespace
  namespace="$(instance_namespace "${instance}")"
  if [[ "${instance}" == "stable" ]]; then
    printf '%s/.local/share/%s' "${base_dir}" "${namespace}"
    return
  fi
  printf '%s/.local/share/%s/codex-remote' "${base_dir}" "${namespace}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance)
      [[ $# -ge 2 ]] || { echo "missing value for --instance" >&2; exit 1; }
      INSTANCE="$2"
      shift 2
      ;;
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

if [[ -z "${INSTANCE}" ]]; then
  repo_instance_file="${ROOT_DIR}/.codex-remote/install-instance"
  if [[ -f "${repo_instance_file}" ]]; then
    INSTANCE="$(tr -d '[:space:]' < "${repo_instance_file}")"
  fi
fi
INSTANCE="${INSTANCE:-stable}"

cd "${ROOT_DIR}"

if [[ "${ALLOW_DIRTY}" != "1" ]]; then
  if ! git diff --quiet --ignore-submodules -- || ! git diff --cached --quiet --ignore-submodules --; then
    echo "working tree has uncommitted changes; commit/stash them first or rerun with --allow-dirty" >&2
    exit 1
  fi
fi

state_root="$(instance_state_root "${BASE_DIR}" "${INSTANCE}")"
state_path="${state_root}/install-state.json"
artifact_dir="${state_root}/local-upgrade"
artifact_path="${artifact_dir}/codex-remote"

printf '[1/4] git pull --ff-only\n'
git pull --ff-only

printf '[2/4] build %s\n' "${BUILD_OUTPUT}"
mkdir -p "${BIN_DIR}"
CLOUDFLARED_EMBED_ALLOW_DOWNLOAD=0 \
  bash "${ROOT_DIR}/scripts/externalaccess/prepare-cloudflared-embed.sh"
"${GO_BIN}" build -o "${BUILD_OUTPUT}" "${ROOT_DIR}/cmd/codex-remote"

if [[ ! -f "${state_path}" ]]; then
  echo "install state not found: ${state_path}" >&2
  echo "build ./bin/codex-remote and run './bin/codex-remote install -bootstrap-only -start-daemon' first, or pass --base-dir for the installed environment" >&2
  exit 1
fi

printf '[3/4] stage local artifact %s\n' "${artifact_path}"
mkdir -p "${artifact_dir}"
cp "${BUILD_OUTPUT}" "${artifact_path}"
chmod +x "${artifact_path}"

printf '[4/4] request built-in local upgrade transaction\n'
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy

cmd=("${BUILD_OUTPUT}" local-upgrade "-instance" "${INSTANCE}" "-base-dir" "${BASE_DIR}")
if [[ -n "${UPGRADE_SLOT}" ]]; then
  cmd+=("-slot" "${UPGRADE_SLOT}")
fi
CODEX_REMOTE_REPO_ROOT="${ROOT_DIR}" "${cmd[@]}"

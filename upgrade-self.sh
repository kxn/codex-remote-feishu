#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
BIN_DIR="${ROOT_DIR}/bin"
BUILD_OUTPUT="${BIN_DIR}/codex-remote"
GO_BIN="${GO_BIN:-go}"
SELF_TARGET_SCRIPT="${ROOT_DIR}/scripts/install/self-install-target.sh"
PULL=0
ALLOW_DIRTY=0
NO_WAIT=0
WAIT_TIMEOUT_SEC=90
UPGRADE_SLOT=""
RECORD_DIR="${ROOT_DIR}/.codex-remote"
RECORD_PATH="${RECORD_DIR}/self-upgrade-last.env"

usage() {
  cat <<'EOF'
usage: ./upgrade-self.sh [--pull] [--allow-dirty] [--slot <slot>] [--timeout <seconds>] [--no-wait]

Build the current checkout, stage it into the current daemon self target's
fixed local-upgrade artifact path, and request the built-in local-upgrade
transaction against that same daemon instance.

This path is intended for recovery cases where the currently installed daemon
is too old or too broken to rely on its own `/upgrade dev` or `upgrade local`
entrypoints. The upgrade request is driven by the freshly built repo binary,
not by the currently installed binary.

options:
  --pull             run git pull --ff-only before build
  --allow-dirty      only relevant with --pull; allow git pull attempt on a dirty tree
  --slot <slot>      optional explicit upgrade slot label
  --timeout <sec>    wait timeout after requesting upgrade; default 90
  --no-wait          exit after local-upgrade is submitted
  -h, --help         show this help text
EOF
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
  printf '%s\n' "dev"
}

wait_for_admin_recovery() {
  local admin_base="$1"
  local timeout_sec="$2"
  local deadline=$(( $(date +%s) + timeout_sec ))
  local bootstrap=""

  while (( $(date +%s) < deadline )); do
    if curl --noproxy '*' -fsS "${admin_base%/}/healthz" >/dev/null 2>&1; then
      bootstrap="$(curl --noproxy '*' -fsS "${admin_base%/}/api/admin/bootstrap-state" 2>/dev/null || true)"
      if [[ "${bootstrap}" == *'"setupRequired":true'* ]]; then
        sleep 1
        continue
      fi
      if curl --noproxy '*' -fsS "${admin_base%/}/api/admin/runtime-status" >/dev/null 2>&1 && \
         curl --noproxy '*' -fsS "${admin_base%/}/v1/status" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pull)
      PULL=1
      shift
      ;;
    --allow-dirty)
      ALLOW_DIRTY=1
      shift
      ;;
    --slot)
      [[ $# -ge 2 ]] || { echo "missing value for --slot" >&2; exit 1; }
      UPGRADE_SLOT="$2"
      shift 2
      ;;
    --timeout)
      [[ $# -ge 2 ]] || { echo "missing value for --timeout" >&2; exit 1; }
      WAIT_TIMEOUT_SEC="$2"
      shift 2
      ;;
    --no-wait)
      NO_WAIT=1
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

if ! [[ "${WAIT_TIMEOUT_SEC}" =~ ^[0-9]+$ ]] || [[ "${WAIT_TIMEOUT_SEC}" -le 0 ]]; then
  echo "--timeout must be a positive integer" >&2
  exit 1
fi

cd "${ROOT_DIR}"

if [[ "${PULL}" == "1" && "${ALLOW_DIRTY}" != "1" ]]; then
  if ! git diff --quiet --ignore-submodules -- || ! git diff --cached --quiet --ignore-submodules --; then
    echo "working tree has uncommitted changes; commit/stash them first or rerun with --allow-dirty" >&2
    exit 1
  fi
fi

if [[ "${PULL}" == "1" ]]; then
  printf '[1/6] git pull --ff-only\n'
  git pull --ff-only
else
  printf '[1/6] skip git pull; build current checkout as-is\n'
fi

printf '[2/6] resolve current daemon self target\n'
eval "$("${SELF_TARGET_SCRIPT}" --format shell)"

printf 'target install instance: %s\n' "${CODEX_REMOTE_SELF_TARGET_INSTANCE_ID}"
printf 'target state: %s\n' "${CODEX_REMOTE_SELF_TARGET_STATE_PATH}"
printf 'target admin: %s\n' "${CODEX_REMOTE_SELF_TARGET_ADMIN_URL}"
printf 'target log: %s\n' "${CODEX_REMOTE_SELF_TARGET_LOG_PATH}"

if [[ ! -f "${CODEX_REMOTE_SELF_TARGET_STATE_PATH}" ]]; then
  echo "install state not found for current daemon self target: ${CODEX_REMOTE_SELF_TARGET_STATE_PATH}" >&2
  exit 1
fi

printf '[3/6] write controller record %s\n' "${RECORD_PATH}"
mkdir -p "${RECORD_DIR}"
cat > "${RECORD_PATH}" <<EOF
CODEX_REMOTE_SELF_TARGET_INSTANCE_ID='${CODEX_REMOTE_SELF_TARGET_INSTANCE_ID}'
CODEX_REMOTE_SELF_TARGET_STATE_PATH='${CODEX_REMOTE_SELF_TARGET_STATE_PATH}'
CODEX_REMOTE_SELF_TARGET_ADMIN_URL='${CODEX_REMOTE_SELF_TARGET_ADMIN_URL}'
CODEX_REMOTE_SELF_TARGET_LOG_PATH='${CODEX_REMOTE_SELF_TARGET_LOG_PATH}'
CODEX_REMOTE_SELF_TARGET_CURRENT_BINARY_PATH='${CODEX_REMOTE_SELF_TARGET_CURRENT_BINARY_PATH}'
CODEX_REMOTE_SELF_TARGET_LOCAL_UPGRADE_ARTIFACT_PATH='${CODEX_REMOTE_SELF_TARGET_LOCAL_UPGRADE_ARTIFACT_PATH}'
CODEX_REMOTE_SELF_UPGRADE_REQUESTED_AT_UTC='$(date -u +%Y-%m-%dT%H:%M:%SZ)'
EOF

printf '[4/6] build %s\n' "${BUILD_OUTPUT}"
mkdir -p "${BIN_DIR}"
BUILD_BRANCH="$(resolve_build_branch)"
CLOUDFLARED_EMBED_ALLOW_DOWNLOAD=0 \
  bash "${ROOT_DIR}/scripts/externalaccess/prepare-cloudflared-embed.sh"
bash "${ROOT_DIR}/scripts/shim/prepare-shim-embed.sh"
"${GO_BIN}" build -ldflags "-X main.branch=${BUILD_BRANCH}" -o "${BUILD_OUTPUT}" "${ROOT_DIR}/cmd/codex-remote"

printf '[5/6] stage local artifact %s\n' "${CODEX_REMOTE_SELF_TARGET_LOCAL_UPGRADE_ARTIFACT_PATH}"
mkdir -p "$(dirname "${CODEX_REMOTE_SELF_TARGET_LOCAL_UPGRADE_ARTIFACT_PATH}")"
cp "${BUILD_OUTPUT}" "${CODEX_REMOTE_SELF_TARGET_LOCAL_UPGRADE_ARTIFACT_PATH}"
chmod +x "${CODEX_REMOTE_SELF_TARGET_LOCAL_UPGRADE_ARTIFACT_PATH}"

printf '[6/6] request built-in local upgrade transaction via freshly built binary\n'
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
cmd=("${BUILD_OUTPUT}" local-upgrade "-state-path" "${CODEX_REMOTE_SELF_TARGET_STATE_PATH}")
if [[ -n "${UPGRADE_SLOT}" ]]; then
  cmd+=("-slot" "${UPGRADE_SLOT}")
fi
"${cmd[@]}"

if [[ "${NO_WAIT}" == "1" ]]; then
  printf 'local-upgrade submitted; not waiting for recovery\n'
  exit 0
fi

printf 'waiting up to %ss for current daemon self target to recover via %s\n' "${WAIT_TIMEOUT_SEC}" "${CODEX_REMOTE_SELF_TARGET_ADMIN_URL}"
if wait_for_admin_recovery "${CODEX_REMOTE_SELF_TARGET_ADMIN_URL}" "${WAIT_TIMEOUT_SEC}"; then
  printf 'self upgrade recovered successfully\n'
  exit 0
fi

echo "self upgrade did not recover within ${WAIT_TIMEOUT_SEC}s" >&2
echo "admin: ${CODEX_REMOTE_SELF_TARGET_ADMIN_URL}" >&2
echo "log: ${CODEX_REMOTE_SELF_TARGET_LOG_PATH}" >&2
echo "record: ${RECORD_PATH}" >&2
exit 1

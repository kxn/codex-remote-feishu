#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
TARGET_SCRIPT="${ROOT_DIR}/scripts/install/repo-install-target.sh"

usage() {
  cat <<'EOF'
usage: scripts/install/repo-target-request.sh <admin|tool> <path> [curl args...]

Resolve the current repository's bound install target, then issue a localhost
HTTP request to that bound instance instead of whichever daemon currently
happens to be serving this Codex conversation.

examples:
  scripts/install/repo-target-request.sh admin /v1/status
  scripts/install/repo-target-request.sh admin /api/admin/bootstrap-state
  scripts/install/repo-target-request.sh tool /healthz
  scripts/install/repo-target-request.sh admin /v1/status | jq .
EOF
}

if [[ $# -lt 2 ]]; then
  usage >&2
  exit 1
fi

target_kind="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
path="${2:-}"
shift 2

case "${target_kind}" in
  admin|tool) ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    echo "unsupported target kind: ${target_kind}" >&2
    usage >&2
    exit 1
    ;;
esac

if [[ -z "${path}" ]]; then
  echo "missing request path" >&2
  usage >&2
  exit 1
fi
if [[ "${path}" != /* ]]; then
  path="/${path}"
fi

eval "$("${TARGET_SCRIPT}" --format shell)"

case "${target_kind}" in
  admin)
    base_url="${CODEX_REMOTE_TARGET_ADMIN_URL:-}"
    ;;
  tool)
    base_url="${CODEX_REMOTE_TARGET_TOOL_URL:-}"
    ;;
esac

if [[ -z "${base_url}" ]]; then
  echo "bound repo target has no ${target_kind} URL; resolve with scripts/install/repo-install-target.sh first" >&2
  exit 1
fi

unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
exec curl --noproxy '*' -fsS "$@" "${base_url%/}${path}"

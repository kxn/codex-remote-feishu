#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
GO_BIN="${GO_BIN:-go}"

mkdir -p "${BIN_DIR}"

"${GO_BIN}" build -o "${BIN_DIR}/codex-remote" "${ROOT_DIR}/cmd/codex-remote"

args=("$@")
if [[ ${#args[@]} -eq 0 ]]; then
  args=(-bootstrap-only -start-daemon)
fi

exec "${BIN_DIR}/codex-remote" install "${args[@]}"

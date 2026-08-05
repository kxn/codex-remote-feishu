#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

# Pass directories instead of an expanded file list: gofmt recurses into
# directories, and expanding 1200+ file paths breaks on Windows (CreateProcess
# command-line length limit) under Git Bash.
output="$(gofmt -l cmd internal testkit)"
if [[ -z "${output}" ]]; then
  exit 0
fi

echo "${output}" >&2
echo "Run make fmt to format remaining Go files before continuing." >&2
exit 1

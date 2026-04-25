#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

files="$(find cmd internal testkit -name '*.go' | sort)"
if [[ -z "${files}" ]]; then
  exit 0
fi

output="$(gofmt -l ${files})"
if [[ -z "${output}" ]]; then
  exit 0
fi

echo "${output}" >&2
echo "Run make fmt to format remaining Go files before continuing." >&2
exit 1

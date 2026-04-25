#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

staged_go_files=()
if command -v rg >/dev/null 2>&1; then
  while IFS= read -r file; do
    [[ -n "${file}" ]] || continue
    staged_go_files+=("${file}")
  done < <(git diff --cached --name-only --diff-filter=ACMR | rg '^(cmd|internal|testkit)/.*\.go$' || true)
else
  while IFS= read -r file; do
    [[ -n "${file}" ]] || continue
    case "${file}" in
      cmd/*|internal/*|testkit/*)
        [[ "${file}" == *.go ]] || continue
        staged_go_files+=("${file}")
        ;;
    esac
  done < <(git diff --cached --name-only --diff-filter=ACMR || true)
fi

if [[ ${#staged_go_files[@]} -gt 0 ]]; then
  gofmt -w "${staged_go_files[@]}"
  git add -- "${staged_go_files[@]}"
fi

bash scripts/check/no-local-paths.sh
bash scripts/check/no-legacy-names.sh
bash scripts/check/feishu-call-broker.sh
bash scripts/check/eventcontract-legacy-guards.sh
bash scripts/check/go-file-length.sh
bash scripts/check/go-format.sh

bash scripts/check/release-track-version.sh

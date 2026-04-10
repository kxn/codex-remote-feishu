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

files="$(find cmd internal testkit -name '*.go' | sort)"
if [[ -n "${files}" ]]; then
  output="$(gofmt -l ${files})"
  if [[ -n "${output}" ]]; then
    echo "${output}" >&2
    echo "Run make fmt to format remaining Go files before committing." >&2
    exit 1
  fi
fi

bash scripts/check/release-track-version.sh

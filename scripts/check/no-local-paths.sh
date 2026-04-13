#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if ! command -v rg >/dev/null 2>&1; then
  echo "rg is required for no-local-paths.sh" >&2
  exit 1
fi

escape_regex() {
  printf '%s' "$1" | sed -e 's/[.[\*^$()+?{|\\]/\\&/g'
}

repo_root_pattern="$(escape_regex "${ROOT_DIR}")"
repo_parent="$(dirname "${ROOT_DIR}")"
repo_parent_pattern="$(escape_regex "${repo_parent}")"
home_pattern=""
if [[ -n "${HOME:-}" ]]; then
  home_pattern="$(escape_regex "${HOME}")"
fi

patterns=(
  "${repo_root_pattern}"
  "${repo_parent_pattern}/fschannel[[:alnum:]_-]*"
  '(^|[^[:alnum:]_])/(home|Users)/[^/"'"'"'[:space:]]+/'
  '(^|[^[:alnum:]_])/private/var/folders/[^/"'"'"'[:space:]]+/[^/"'"'"'[:space:]]+/'
  '(^|[^[:alnum:]_])[A-Za-z]:\\\\Users\\\\[^\\/"'"'"'[:space:]]+\\\\'
)
if [[ -n "${home_pattern}" ]]; then
  patterns+=("${home_pattern}")
fi

files=()
while IFS= read -r file; do
  case "${file}" in
    scripts/check/no-local-paths.sh|scripts/check/no-legacy-names.sh)
      continue
      ;;
  esac
  files+=("${file}")
done < <(
  git ls-files -- \
    '*.go' '*.md' '*.yml' '*.yaml' '*.json' '*.sh' '*.ps1' \
    '*.ts' '*.tsx' '*.js' '*.jsx' '*.css' '*.html' '*.txt'
)

if [[ ${#files[@]} -eq 0 ]]; then
  exit 0
fi

args=()
for pattern in "${patterns[@]}"; do
  args+=(-e "${pattern}")
done

if rg -n "${args[@]}" -- "${files[@]}"; then
  echo "Found machine-local absolute paths in tracked source or docs." >&2
  exit 1
fi

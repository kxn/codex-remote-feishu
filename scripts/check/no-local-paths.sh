#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

path_pattern='(^|[^[:alnum:]_])/(data|home|Users|private|var/folders)/'
search_cmd=()
if command -v rg >/dev/null 2>&1; then
  search_cmd=(rg -n "${path_pattern}" README.md QUICKSTART.md DEVELOPER.md install-release.sh docs deploy .github --glob '*.md' --glob '*.yml' --glob '*.json' --glob '*.sh')
else
  search_cmd=(grep -RInE "${path_pattern}" README.md QUICKSTART.md DEVELOPER.md install-release.sh docs deploy .github --include='*.md' --include='*.yml' --include='*.json' --include='*.sh')
fi

if "${search_cmd[@]}"; then
  echo "Found machine-local absolute paths in public docs or workflow files." >&2
  exit 1
fi

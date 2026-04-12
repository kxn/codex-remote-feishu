#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

patterns=(
  'module '"fschannel"
  '"fs'channel/
  'CODEX_'RELAY_
  'codex-'relay
  'go-'rewrite-architecture\.md
  '/data/dl/'fschannel
  '/home/dl/'fschannel
)
pattern="$(IFS='|'; printf '%s' "${patterns[*]}")"

search_cmd=()
if command -v rg >/dev/null 2>&1; then
  search_cmd=(rg -n "${pattern}" README.md QUICKSTART.md DEVELOPER.md .env.example Makefile install-release.sh setup.ps1 cmd internal docs deploy .github)
else
  search_cmd=(grep -RInE "${pattern}" README.md QUICKSTART.md DEVELOPER.md .env.example Makefile install-release.sh setup.ps1 cmd internal docs deploy .github)
fi

if "${search_cmd[@]}"; then
  echo "Found legacy project names or deprecated paths." >&2
  exit 1
fi

#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
  NODE_DIR="$(bash "$ROOT_DIR/scripts/web/ensure-node.sh")"
  export PATH="$NODE_DIR/bin:$PATH"
fi

cd "$ROOT_DIR/web"
if [[ -f package-lock.json ]]; then
  npm ci || npm install
else
  npm install
fi
npm run build

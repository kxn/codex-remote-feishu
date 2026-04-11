#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

platforms=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

for platform in "${platforms[@]}"; do
  read -r goos goarch <<<"${platform}"
  CLOUDFLARED_EMBED_ALLOW_DOWNLOAD=1 \
    bash "${ROOT_DIR}/scripts/externalaccess/prepare-cloudflared-embed.sh" "${goos}" "${goarch}"
done

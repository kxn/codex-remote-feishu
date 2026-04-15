#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'USAGE'
usage: scripts/release/build-dev-manifest.sh [dist_dir] [output_path]
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

dist_dir="${1:-dist}"
output_path="${2:-${dist_dir}/dev-latest.json}"

repository="${CODEX_REMOTE_DEV_REPOSITORY:-${GITHUB_REPOSITORY:-kxn/codex-remote-feishu}}"
release_tag="${CODEX_REMOTE_DEV_RELEASE_TAG:-dev-latest}"
commit="${CODEX_REMOTE_DEV_COMMIT:-$(git rev-parse HEAD)}"
short_commit="${CODEX_REMOTE_DEV_SHORT_COMMIT:-${commit:0:12}}"
version="${CODEX_REMOTE_DEV_VERSION:-dev-${short_commit}}"
built_at="${CODEX_REMOTE_DEV_BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
base_url="${CODEX_REMOTE_DEV_ASSET_BASE_URL:-https://github.com/${repository}/releases/download/${release_tag}}"

if [[ ! -f "${dist_dir}/checksums.txt" ]]; then
  echo "missing ${dist_dir}/checksums.txt" >&2
  exit 1
fi

mkdir -p "$(dirname "${output_path}")"

python3 - "${dist_dir}" "${output_path}" "${base_url}" "${version}" "${commit}" "${built_at}" <<'PY'
import json
import pathlib
import re
import sys

if len(sys.argv) != 7:
    raise SystemExit("expected dist_dir output_path base_url version commit built_at")

dist_dir = pathlib.Path(sys.argv[1])
output_path = pathlib.Path(sys.argv[2])
base_url = sys.argv[3].rstrip("/")
version = sys.argv[4]
commit = sys.argv[5]
built_at = sys.argv[6]
pattern = re.compile(r"^codex-remote-feishu_dev_(linux|darwin|windows)_(amd64|arm64)\.(tar\.gz|zip)$")

checksums = {}
for raw_line in (dist_dir / "checksums.txt").read_text().splitlines():
    line = raw_line.strip()
    if not line:
        continue
    parts = line.split()
    if len(parts) < 2:
        raise SystemExit(f"invalid checksum line: {raw_line!r}")
    checksums[parts[-1].lstrip("./")] = parts[0]

assets = []
for path in sorted(dist_dir.iterdir()):
    if not path.is_file():
        continue
    match = pattern.match(path.name)
    if not match:
        continue
    goos, goarch, _ = match.groups()
    checksum = checksums.get(path.name)
    if not checksum:
        raise SystemExit(f"missing checksum for {path.name}")
    assets.append(
        {
            "goos": goos,
            "goarch": goarch,
            "name": path.name,
            "url": f"{base_url}/{path.name}",
            "sha256": checksum,
            "size": path.stat().st_size,
        }
    )

if not assets:
    raise SystemExit(f"no dev artifacts found in {dist_dir}")

manifest = {
    "channel": "dev",
    "version": version,
    "commit": commit,
    "built_at": built_at,
    "assets": assets,
}
output_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
PY

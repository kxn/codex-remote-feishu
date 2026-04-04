#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

work_dir="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" 2>/dev/null || true
    wait "${server_pid}" 2>/dev/null || true
  fi
  rm -rf "${work_dir}"
}
trap cleanup EXIT

version="v0.0.0-smoke"
dist_dir="${work_dir}/dist"
install_root="${work_dir}/install-root"
port="${CODEX_REMOTE_SMOKE_PORT:-}"
if [[ -z "${port}" ]]; then
  port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
fi

bash scripts/release/build-artifacts.sh "${version}" "${dist_dir}"

python3 -m http.server "${port}" --bind 127.0.0.1 --directory "${dist_dir}" >/dev/null 2>&1 &
server_pid="$!"
for _ in $(seq 1 20); do
  if curl --noproxy '*' -fsS "http://127.0.0.1:${port}/" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

curl --noproxy '*' -fsS "http://127.0.0.1:${port}/" >/dev/null

CODEX_REMOTE_VERSION="${version}" \
CODEX_REMOTE_BASE_URL="http://127.0.0.1:${port}" \
CODEX_REMOTE_INSTALL_ROOT="${install_root}" \
CODEX_REMOTE_SKIP_SETUP=1 \
bash ./install-release.sh

expected_dir="${install_root}/${version}"
[[ -d "${expected_dir}" ]]
[[ -x "${expected_dir}/setup.sh" ]]
[[ -f "${expected_dir}/README.md" ]]
[[ -f "${expected_dir}/QUICKSTART.md" ]]
[[ -f "${expected_dir}/install-release.sh" ]]
[[ -L "${install_root}/current" ]]

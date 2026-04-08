#!/usr/bin/env bash
set -euo pipefail

unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

install_bin_dir() {
  local h="$1"
  case "$(uname -s)" in
    Darwin) printf '%s\n' "${h}/Library/Application Support/codex-remote/bin" ;;
    *)      printf '%s\n' "${h}/.local/bin" ;;
  esac
}

work_dir="$(mktemp -d)"
server_pid=""
daemon_pid=""
cleanup() {
  if [[ -z "${daemon_pid}" && -n "${home_dir:-}" ]]; then
    daemon_pid="$(ps -eo pid=,args= | awk -v target="$(install_bin_dir "${home_dir}")/codex-remote daemon" '$0 ~ target && !f {f=1; print $1}')"
  fi
  if [[ -n "${daemon_pid}" ]]; then
    kill "${daemon_pid}" 2>/dev/null || true
  fi
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" 2>/dev/null || true
    wait "${server_pid}" 2>/dev/null || true
  fi
  rm -rf "${work_dir}"
}
trap cleanup EXIT

version="v0.0.0"
beta_version="v0.1.0-beta.1"
dist_dir="${work_dir}/dist"
prod_dist_dir="${work_dir}/dist-production"
beta_dist_dir="${work_dir}/dist-beta"
install_root="${work_dir}/install-root"
track_install_root="${work_dir}/install-root-beta"
home_dir="${work_dir}/home"
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
relay_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
admin_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"

mkdir -p "${home_dir}/.config/codex-remote"
cat > "${home_dir}/.config/codex-remote/config.json" <<EOF
{
  "version": 1,
  "relay": {
    "listenHost": "127.0.0.1",
    "listenPort": ${relay_port},
    "serverURL": "ws://127.0.0.1:${relay_port}/ws/agent"
  },
  "admin": {
    "listenHost": "127.0.0.1",
    "listenPort": ${admin_port},
    "autoOpenBrowser": false
  },
  "wrapper": {
    "codexRealBinary": "codex",
    "nameMode": "workspace_basename",
    "integrationMode": "none"
  },
  "feishu": {
    "useSystemProxy": false,
    "apps": []
  },
  "debug": {},
  "storage": {
    "previewRootFolderName": "Codex Remote Previews"
  }
}
EOF

bash scripts/release/build-artifacts.sh "${version}" "${prod_dist_dir}"
bash scripts/release/build-artifacts.sh "${beta_version}" "${beta_dist_dir}"

mkdir -p "${dist_dir}"
cp "${prod_dist_dir}"/codex-remote-feishu_* "${dist_dir}/"
cp "${beta_dist_dir}"/codex-remote-feishu_* "${dist_dir}/"

cat > "${dist_dir}/releases.json" <<EOF
[
  {
    "url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/2",
    "assets_url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/2/assets",
    "html_url": "https://github.com/kxn/codex-remote-feishu/releases/tag/${beta_version}",
    "id": 2,
    "tag_name": "${beta_version}",
    "draft": false,
    "prerelease": true,
    "assets": []
  },
  {
    "url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/1",
    "assets_url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/1/assets",
    "html_url": "https://github.com/kxn/codex-remote-feishu/releases/tag/${version}",
    "id": 1,
    "tag_name": "${version}",
    "draft": false,
    "prerelease": false,
    "assets": []
  }
]
EOF

python3 -m http.server "${port}" --bind 127.0.0.1 --directory "${dist_dir}" >/dev/null 2>&1 &
server_pid="$!"
for _ in $(seq 1 20); do
  if curl --noproxy '*' -fsS "http://127.0.0.1:${port}/" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

curl --noproxy '*' -fsS "http://127.0.0.1:${port}/" >/dev/null

HOME="${home_dir}" \
CODEX_REMOTE_VERSION="${version}" \
CODEX_REMOTE_BASE_URL="http://127.0.0.1:${port}" \
CODEX_REMOTE_INSTALL_ROOT="${install_root}" \
bash ./install-release.sh

expected_dir="${install_root}/${version}"
[[ -d "${expected_dir}" ]]
[[ -x "${expected_dir}/codex-remote" ]]
[[ -f "${expected_dir}/README.md" ]]
[[ -f "${expected_dir}/QUICKSTART.md" ]]
[[ -d "${expected_dir}/deploy" ]]
[[ ! -e "${expected_dir}/setup.sh" ]]
[[ ! -e "${expected_dir}/setup.ps1" ]]
[[ ! -e "${expected_dir}/install.sh" ]]
[[ -L "${install_root}/current" ]]

installed_version="$("${expected_dir}/codex-remote" version)"
[[ "${installed_version}" == "${version}" ]]

HOME="${home_dir}" \
CODEX_REMOTE_BASE_URL="http://127.0.0.1:${port}" \
CODEX_REMOTE_RELEASES_API_URL="http://127.0.0.1:${port}/releases.json" \
CODEX_REMOTE_INSTALL_ROOT="${track_install_root}" \
bash ./install-release.sh --track beta --download-only

beta_expected_dir="${track_install_root}/${beta_version}"
[[ -d "${beta_expected_dir}" ]]
[[ -x "${beta_expected_dir}/codex-remote" ]]
[[ -L "${track_install_root}/current" ]]

beta_installed_version="$("${beta_expected_dir}/codex-remote" version)"
[[ "${beta_installed_version}" == "${beta_version}" ]]

python3 - "${home_dir}" <<'PY'
import json, sys
from pathlib import Path

home = sys.argv[1]
config_path = Path(home) / ".config" / "codex-remote" / "config.json"
state_path = Path(home) / ".local" / "share" / "codex-remote" / "install-state.json"
config_payload = json.loads(config_path.read_text())
state_payload = json.loads(state_path.read_text())

assert config_payload["wrapper"]["integrationMode"] == "none", config_payload
assert state_payload.get("integrations", []) == [], state_payload
assert state_payload["installedBinary"].endswith("/codex-remote"), state_payload
PY

for _ in $(seq 1 60); do
  if curl --noproxy '*' -fsS "http://127.0.0.1:${admin_port}/api/setup/bootstrap-state" > "${work_dir}/bootstrap-state.json" 2>/dev/null; then
    daemon_pid="$(ps -eo pid=,args= | awk -v target="$(install_bin_dir "${home_dir}")/codex-remote daemon" '$0 ~ target && !f {f=1; print $1}')"
    break
  fi
  sleep 0.2
done

[[ -n "${daemon_pid}" ]]
curl --noproxy '*' -fsS "http://127.0.0.1:${admin_port}/api/setup/bootstrap-state" > "${work_dir}/bootstrap-state.json"
curl --noproxy '*' -fsS "http://127.0.0.1:${admin_port}/setup" > "${work_dir}/setup.html"

python3 - "${work_dir}" "${admin_port}" "${relay_port}" <<'PY'
import json, sys
from pathlib import Path

work_dir, admin_port, relay_port = sys.argv[1], sys.argv[2], sys.argv[3]
payload = json.loads((Path(work_dir) / "bootstrap-state.json").read_text())
assert payload["setupRequired"] is True, payload
assert payload["phase"] == "uninitialized", payload
assert payload["admin"]["listenPort"] == admin_port, payload
assert payload["relay"]["listenPort"] == relay_port, payload
assert payload["session"]["trustedLoopback"] is True, payload

html = (Path(work_dir) / "setup.html").read_text()
assert "Codex Remote" in html, html[:200]
PY

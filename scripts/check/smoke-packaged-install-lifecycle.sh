#!/usr/bin/env bash
set -euo pipefail

unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

usage() {
  cat <<'EOF'
usage: scripts/check/smoke-packaged-install-lifecycle.sh [options]

options:
  --version <version>          first-install version fixture (default: v0.0.0)
  --upgrade-version <version>  upgrade version fixture (default: v0.1.0-beta.1)
  --upgrade-track <track>      upgrade fixture track (default: beta)
  --prod-dist-dir <dir>        production artifact directory
  --upgrade-dist-dir <dir>     upgrade artifact directory
  -h, --help                   show this help
EOF
}

fail() {
  echo "smoke-packaged-install-lifecycle: $*" >&2
  exit 1
}

detect_goos() {
  case "$(uname -s)" in
    Darwin) printf '%s\n' "darwin" ;;
    *) fail "packaged installer lifecycle smoke currently requires macOS; got $(uname -s)" ;;
  esac
}

detect_goarch() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s\n' "amd64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

asset_name() {
  local version="$1"
  printf 'codex-remote-feishu_%s_%s_%s.tar.gz\n' "${version#v}" "$(detect_goos)" "$(detect_goarch)"
}

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

extract_binary() {
  local dist_dir="$1"
  local version="$2"
  local output_dir="$3"
  local asset
  asset="$(asset_name "${version}")"
  [[ -f "${dist_dir}/${asset}" ]] || fail "expected artifact missing: ${dist_dir}/${asset}"
  mkdir -p "${output_dir}"
  tar -xzf "${dist_dir}/${asset}" -C "${output_dir}"
  local binary
  binary="$(find "${output_dir}" -type f -name codex-remote -perm -111 | head -n1 || true)"
  [[ -n "${binary}" ]] || fail "codex-remote binary not found after extracting ${asset}"
  printf '%s\n' "${binary}"
}

assert_json_field() {
  local path="$1"
  local field="$2"
  local expected="$3"
  python3 - "$path" "$field" "$expected" <<'PY'
import json
import sys
from pathlib import Path

path, field, expected = sys.argv[1], sys.argv[2], sys.argv[3]
payload = json.loads(Path(path).read_text())
actual = payload
for part in field.split("."):
    actual = actual[part]
if str(actual) != expected:
    raise SystemExit(f"{field}={actual!r}, want {expected!r}")
PY
}

version="v0.0.0"
upgrade_version="v0.1.0-beta.1"
upgrade_track="beta"
prod_dist_dir=""
upgrade_dist_dir=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --upgrade-version)
      upgrade_version="${2:-}"
      shift 2
      ;;
    --upgrade-track)
      upgrade_track="${2:-}"
      shift 2
      ;;
    --prod-dist-dir)
      prod_dist_dir="${2:-}"
      shift 2
      ;;
    --upgrade-dist-dir)
      upgrade_dist_dir="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unexpected argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

[[ -n "${prod_dist_dir}" ]] || fail "--prod-dist-dir is required"
[[ -n "${upgrade_dist_dir}" ]] || fail "--upgrade-dist-dir is required"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-packaged-lifecycle-XXXXXX")"
daemon_pid=""
cleanup() {
  local status=$?
  if [[ -z "${daemon_pid}" && -n "${install_bin_dir:-}" ]]; then
    daemon_pid="$(ps -eo pid=,args= | awk -v target="${install_bin_dir}/codex-remote daemon" '$0 ~ target && !f {f=1; print $1}')"
  fi
  if [[ -n "${daemon_pid}" ]]; then
    kill "${daemon_pid}" 2>/dev/null || true
  fi
  if [[ -n "${state_path:-}" && -x "${live_binary:-}" ]]; then
    HOME="${base_dir:-}" "${live_binary}" service uninstall-user -state-path "${state_path}" >/dev/null 2>&1 || true
  fi
  rm -rf "${work_dir}" 2>/dev/null || true
  return "${status}"
}
trap cleanup EXIT

prod_binary="$(extract_binary "${prod_dist_dir}" "${version}" "${work_dir}/prod")"
upgrade_binary="$(extract_binary "${upgrade_dist_dir}" "${upgrade_version}" "${work_dir}/upgrade")"
base_dir="${work_dir}/home"
install_bin_dir="${work_dir}/install-bin"
state_path="${base_dir}/.local/share/codex-remote/install-state.json"
config_dir="${base_dir}/.config/codex-remote"
plist_path="${base_dir}/Library/LaunchAgents/com.codex-remote.service.plist"
result_dir="${work_dir}/results"
mkdir -p "${config_dir}" "${result_dir}"

relay_port="$(free_port)"
admin_port="$(free_port)"
tool_port="$(free_port)"
external_port="$(free_port)"
cat > "${config_dir}/config.json" <<EOF
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
  "tool": {
    "listenHost": "127.0.0.1",
    "listenPort": ${tool_port}
  },
  "externalAccess": {
    "listenHost": "127.0.0.1",
    "listenPort": ${external_port}
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

HOME="${base_dir}" "${prod_binary}" packaged-install \
  -base-dir "${base_dir}" \
  -install-bin-dir "${install_bin_dir}" \
  -binary "${prod_binary}" \
  -install-source release \
  -current-version "${version}" \
  -current-track production \
  -format json \
  -result-file "${result_dir}/first-install.ini" > "${result_dir}/first-install.json"

live_binary="${install_bin_dir}/codex-remote"
[[ -x "${live_binary}" ]] || fail "live binary missing after first install: ${live_binary}"
[[ -f "${state_path}" ]] || fail "install state missing after first install: ${state_path}"
[[ -f "${plist_path}" ]] || fail "launchd plist missing after first install: ${plist_path}"
assert_json_field "${state_path}" "currentVersion" "${version}"
assert_json_field "${state_path}" "currentTrack" "production"
assert_json_field "${state_path}" "serviceManager" "launchd_user"
"${live_binary}" version | grep -Fx "${version}" >/dev/null

HOME="${base_dir}" "${live_binary}" packaged-install-probe \
  -base-dir "${base_dir}" \
  -current-version "${version}" \
  -format json > "${result_dir}/repair-probe.json"
assert_json_field "${result_dir}/repair-probe.json" "mode" "repair"
assert_json_field "${result_dir}/repair-probe.json" "sameVersion" "True"

HOME="${base_dir}" "${live_binary}" packaged-install \
  -state-path "${state_path}" \
  -binary "${prod_binary}" \
  -install-source release \
  -current-version "${version}" \
  -current-track production \
  -format json \
  -result-file "${result_dir}/repair.ini" > "${result_dir}/repair.json"
assert_json_field "${result_dir}/repair.json" "mode" "repair"
assert_json_field "${state_path}" "currentVersion" "${version}"

HOME="${base_dir}" "${live_binary}" packaged-install \
  -state-path "${state_path}" \
  -binary "${upgrade_binary}" \
  -install-source release \
  -current-version "${upgrade_version}" \
  -current-track "${upgrade_track}" \
  -format json \
  -result-file "${result_dir}/upgrade.ini" > "${result_dir}/upgrade.json"
assert_json_field "${result_dir}/upgrade.json" "mode" "repair"
assert_json_field "${state_path}" "currentVersion" "${upgrade_version}"
assert_json_field "${state_path}" "currentTrack" "${upgrade_track}"
"${live_binary}" version | grep -Fx "${upgrade_version}" >/dev/null

HOME="${base_dir}" "${live_binary}" service uninstall-user -state-path "${state_path}" > "${result_dir}/uninstall.txt"
assert_json_field "${state_path}" "serviceManager" "detached"
[[ ! -e "${plist_path}" ]] || fail "launchd plist still exists after uninstall: ${plist_path}"

echo "packaged installer lifecycle smoke passed"

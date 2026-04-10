#!/usr/bin/env bash
set -uo pipefail

usage() {
  cat <<'EOF'
usage: scripts/relay/collect-diagnostics.sh [--base-dir DIR] [--relay-port PORT] [--admin-port PORT] [--tail-lines N]

Collect relay-stack runtime evidence in a fixed order:
1. proxy-related environment
2. service status
3. process and port checks
4. bootstrap-state and /v1/status
5. recent relayd log tail
6. symptom shortcuts

Options:
  --base-dir DIR    Base directory used to derive local state/log paths. Default: $HOME
  --relay-port N    Relay websocket port. Default: 9500
  --admin-port N    Relay admin/status port. Default: 9501
  --tail-lines N    Log lines to tail. Default: 200
  --help            Show this help text
EOF
}

base_dir="${HOME}"
relay_port="9500"
admin_port="9501"
tail_lines="200"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-dir)
      base_dir="${2:-}"
      shift 2
      ;;
    --relay-port)
      relay_port="${2:-}"
      shift 2
      ;;
    --admin-port)
      admin_port="${2:-}"
      shift 2
      ;;
    --tail-lines)
      tail_lines="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "${base_dir}" || -z "${relay_port}" || -z "${admin_port}" || -z "${tail_lines}" ]]; then
  echo "all option values must be non-empty" >&2
  exit 1
fi

log_file="${base_dir}/.local/share/codex-remote/logs/codex-remote-relayd.log"
raw_log_file="${base_dir}/.local/share/codex-remote/logs/codex-remote-relayd-raw.ndjson"
service_unit="${base_dir}/.config/systemd/user/codex-remote.service"
work_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

print_section() {
  printf '\n== %s ==\n' "$1"
}

print_cmd() {
  printf '$ %s\n' "$*"
}

run_section() {
  local title="$1"
  shift
  print_section "${title}"
  print_cmd "$@"
  if "$@"; then
    return 0
  fi
  local status=$?
  printf '[exit %d]\n' "${status}"
  return 0
}

run_no_proxy_section() {
  local title="$1"
  shift
  print_section "${title}"
  print_cmd env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy "$@"
  if env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy "$@"; then
    return 0
  fi
  local status=$?
  printf '[exit %d]\n' "${status}"
  return 0
}

print_section "Relay Diagnostic Context"
printf 'base_dir: %s\n' "${base_dir}"
printf 'relay_port: %s\n' "${relay_port}"
printf 'admin_port: %s\n' "${admin_port}"
printf 'log_file: %s\n' "${log_file}"
printf 'raw_log_file: %s\n' "${raw_log_file}"
printf 'service_unit: %s\n' "${service_unit}"
printf 'generated_at: %s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')"

print_section "Proxy Environment"
for name in http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy NO_PROXY no_proxy; do
  printf '%s=%s\n' "${name}" "${!name-<unset>}"
done

if command -v codex-remote >/dev/null 2>&1; then
  run_no_proxy_section "codex-remote Service Status" codex-remote service status
else
  print_section "codex-remote Service Status"
  echo "codex-remote not found in PATH; falling back to systemctl checks."
fi

if command -v systemctl >/dev/null 2>&1; then
  run_no_proxy_section "systemd User Unit Status" systemctl --user status codex-remote.service --no-pager
else
  print_section "systemd User Unit Status"
  echo "systemctl not available on this host."
fi

if command -v rg >/dev/null 2>&1; then
  run_no_proxy_section "Process Check" bash -lc "ps -ef | rg 'codex-remote|relayd|relay-wrapper' | rg -v 'rg|collect-diagnostics'"
else
  run_no_proxy_section "Process Check" bash -lc "ps -ef | grep -E 'codex-remote|relayd|relay-wrapper' | grep -vE 'grep|collect-diagnostics'"
fi

if command -v ss >/dev/null 2>&1; then
  run_no_proxy_section "Port Check (ss)" bash -lc "ss -ltnp | grep -E '(:${relay_port}|:${admin_port})\\b' || true"
elif command -v lsof >/dev/null 2>&1; then
  run_no_proxy_section "Port Check (lsof)" bash -lc "lsof -nP -iTCP -sTCP:LISTEN | grep -E '(:${relay_port}|:${admin_port})\\b' || true"
else
  print_section "Port Check"
  echo "Neither ss nor lsof is available on this host."
fi

fetch_local_http() {
  local path="$1"
  if command -v curl >/dev/null 2>&1; then
    env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy \
      curl --noproxy '*' -fsS "http://127.0.0.1:${admin_port}${path}"
    return $?
  fi
  env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy \
    bash -lc "exec 3<>/dev/tcp/127.0.0.1/${admin_port} && printf 'GET ${path} HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n' >&3 && cat <&3"
}

format_jsonish() {
  if command -v jq >/dev/null 2>&1; then
    jq .
  else
    cat
  fi
}

print_status_summary() {
  local status_file="$1"
  if ! command -v jq >/dev/null 2>&1; then
    cat "${status_file}"
    return 0
  fi
  jq '{
    instanceCount: (.instances | length),
    onlineInstanceCount: ([.instances[] | select(.Online == true)] | length),
    surfaceCount: (.surfaces | length),
    pendingRemoteTurnCount: (.pendingRemoteTurns | length),
    activeRemoteTurnCount: (.activeRemoteTurns | length),
    instances: (.instances | map({
      instanceId: .InstanceID,
      source: .Source,
      online: .Online,
      workspaceKey: .WorkspaceKey,
      observedFocusedThreadId: .ObservedFocusedThreadID,
      activeThreadId: .ActiveThreadID,
      activeTurnId: .ActiveTurnID
    })),
    surfaces: (.surfaces | map({
      surfaceSessionId: .SurfaceSessionID,
      productMode: .ProductMode,
      attachedInstanceId: .AttachedInstanceID,
      selectedThreadId: .SelectedThreadID,
      routeMode: .RouteMode,
      dispatchMode: .DispatchMode,
      activeQueueItemId: .ActiveQueueItemID,
      queuedQueueItemCount: (.QueuedQueueItemIDs | length),
      pendingHeadless: (.PendingHeadless != null)
    })),
    pendingRemoteTurns: .pendingRemoteTurns,
    activeRemoteTurns: .activeRemoteTurns,
    gateways: .gateways
  }' "${status_file}"
}

print_section "Bootstrap State"
print_cmd "GET http://127.0.0.1:${admin_port}/api/admin/bootstrap-state"
if fetch_local_http "/api/admin/bootstrap-state" | format_jsonish; then
  :
else
  printf '[exit %d]\n' "$?"
fi

print_section "Relay Status"
print_cmd "GET http://127.0.0.1:${admin_port}/v1/status"
status_file="${work_dir}/status.json"
if fetch_local_http "/v1/status" > "${status_file}"; then
  print_status_summary "${status_file}"
  :
else
  printf '[exit %d]\n' "$?"
fi

if [[ -f "${log_file}" ]]; then
  run_no_proxy_section "Recent relayd Log" tail -n "${tail_lines}" "${log_file}"
else
  print_section "Recent relayd Log"
  echo "log file not found: ${log_file}"
fi

if [[ -f "${raw_log_file}" ]]; then
  run_no_proxy_section "Recent relayd Raw Log" tail -n "${tail_lines}" "${raw_log_file}"
else
  print_section "Recent relayd Raw Log"
  echo "raw log file not found: ${raw_log_file}"
fi

print_section "Symptom Shortcuts"
cat <<'EOF'
- VS Code 有结果但飞书没有：
  - 先看 /v1/status 里的 surface 数量、attached instance、selected thread、dispatch mode
  - 再看 relayd 日志里是否有 gateway apply failed
- /list 或 /attach 似乎成功，但后续文本没反应：
  - 确认菜单点击和文本消息是否命中了同一个 surfaceSessionID
  - 确认当前 chat 没被附着到其他 surface
- 重启后行为变化：
  - 不要相信“started”提示，回头看进程、端口、/v1/status 和日志
EOF

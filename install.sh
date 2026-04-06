#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
GO_BIN="${GO_BIN:-go}"

BASE_DIR="${BASE_DIR:-${HOME}}"
XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-${HOME}/.config}"
XDG_STATE_HOME="${XDG_STATE_HOME:-${HOME}/.local/state}"
XDG_DATA_HOME="${XDG_DATA_HOME:-${HOME}/.local/share}"

CONFIG_DIR="${XDG_CONFIG_HOME}/codex-remote"
STATE_DIR="${XDG_DATA_HOME}/codex-remote"
RUN_DIR="${XDG_STATE_HOME}/codex-remote"
LOG_DIR="${STATE_DIR}/logs"

CONFIG_FILE="${CONFIG_DIR}/config.json"
PID_FILE="${RUN_DIR}/codex-remote-relayd.pid"
RUNTIME_LOCK_FILE="${RUN_DIR}/relayd.lock"
LOG_FILE="${LOG_DIR}/codex-remote-relayd.log"

mkdir -p "${BIN_DIR}" "${CONFIG_DIR}" "${STATE_DIR}" "${RUN_DIR}" "${LOG_DIR}"

build_bins() {
  "${GO_BIN}" build -o "${BIN_DIR}/codex-remote" "${ROOT_DIR}/cmd/codex-remote"
}

canonicalize_path() {
  local path="${1:-}"
  [[ -n "${path}" ]] || return 0
  readlink -f "${path}" 2>/dev/null || printf '%s\n' "${path}"
}

append_unique() {
  local value="${1:-}"
  shift
  [[ -n "${value}" ]] || return 0
  local current
  for current in "$@"; do
    [[ "${current}" == "${value}" ]] && return 0
  done
  printf '%s\n' "${value}"
}

detect_vscode_bundle_codex() {
  list_vscode_bundle_codexes | head -n 1
}

detect_install_bin_codex_remote() {
  local install_bin_dir="${INSTALL_BIN_DIR:-${HOME}/.local/bin}"
  printf '%s\n' "${install_bin_dir}/codex-remote"
}

list_candidate_install_state_files() {
  local root_parent
  root_parent="$(dirname "${ROOT_DIR}")"

  {
    printf '%s\n' "${STATE_DIR}/install-state.json"
    printf '%s\n' "${HOME}/.local/share/codex-remote/install-state.json"
    printf '%s\n' "${BASE_DIR}/.local/share/codex-remote/install-state.json"
    printf '%s\n' "${root_parent}/.local/share/codex-remote/install-state.json"
  } | awk 'NF && !seen[$0]++'
}

list_install_state_field_values() {
  local field="${1:-}"
  [[ -n "${field}" ]] || return 0

  local file value
  while IFS= read -r file; do
    [[ -f "${file}" ]] || continue
    value="$(sed -nE "s/^[[:space:]]*\"${field}\":[[:space:]]*\"([^\"]*)\".*/\\1/p" "${file}" | head -n 1)"
    [[ -n "${value}" ]] || continue
    printf '%s\n' "${value}"
  done < <(list_candidate_install_state_files)
}

list_candidate_vscode_extension_dirs() {
  local root_parent
  root_parent="$(dirname "${ROOT_DIR}")"

  {
    printf '%s\n' "${VSCODE_SERVER_EXTENSIONS_DIR:-}"
    printf '%s\n' "${HOME}/.vscode-server/extensions"
    printf '%s\n' "${BASE_DIR}/.vscode-server/extensions"
    printf '%s\n' "${root_parent}/.vscode-server/extensions"
  } | awk 'NF && !seen[$0]++'
}

list_vscode_bundle_codexes() {
  local extensions_dir dir

  while IFS= read -r extensions_dir; do
    [[ -d "${extensions_dir}" ]] || continue
    while IFS= read -r dir; do
      [[ -n "${dir}" ]] || continue
      if [[ -f "${dir}/bin/linux-x86_64/codex" ]]; then
        printf '%s\n' "${dir}/bin/linux-x86_64/codex"
      fi
      if [[ -f "${dir}/bin/linux-arm64/codex" ]]; then
        printf '%s\n' "${dir}/bin/linux-arm64/codex"
      fi
    done < <(find "${extensions_dir}" -maxdepth 1 -mindepth 1 -type d -name 'openai.chatgpt-*' -printf '%T@ %p\n' | sort -nr | cut -d' ' -f2-)
  done < <(list_candidate_vscode_extension_dirs)
}

list_cleanup_target_paths() {
  local binary_path="${BINARY_PATH:-${BIN_DIR}/codex-remote}"

  {
    printf '%s\n' "$@"
    printf '%s\n' "${binary_path}"
    detect_install_bin_codex_remote
    list_install_state_field_values "installedBinary"
    list_install_state_field_values "installedWrapperBinary"
    list_install_state_field_values "installedRelaydBinary"
    list_install_state_field_values "bundleEntrypoint"
    list_vscode_bundle_codexes
  } | awk 'NF && !seen[$0]++'
}

list_candidate_runtime_lock_files() {
  local root_parent
  root_parent="$(dirname "${ROOT_DIR}")"

  {
    printf '%s\n' "${RUNTIME_LOCK_FILE}"
    printf '%s\n' "${HOME}/.local/state/codex-remote/relayd.lock"
    printf '%s\n' "${BASE_DIR}/.local/state/codex-remote/relayd.lock"
    printf '%s\n' "${root_parent}/.local/state/codex-remote/relayd.lock"
  } | awk 'NF && !seen[$0]++'
}

read_lock_pid() {
  local lock_file="${1:-}"
  [[ -f "${lock_file}" ]] || return 0
  sed -nE 's/.*"pid":[[:space:]]*([0-9]+).*/\1/p' "${lock_file}" | head -n 1
}

list_runtime_lock_pids() {
  local lock_file pid
  while IFS= read -r lock_file; do
    [[ -f "${lock_file}" ]] || continue
    pid="$(read_lock_pid "${lock_file}")"
    [[ -n "${pid}" ]] || continue
    if kill -0 "${pid}" 2>/dev/null; then
      printf '%s\n' "${pid}"
      continue
    fi
    rm -f "${lock_file}"
  done < <(list_candidate_runtime_lock_files)
}

list_integration_process_pids() {
  local targets=()
  local candidate resolved

  for candidate in "$@"; do
    [[ -n "${candidate}" ]] || continue
    resolved="$(canonicalize_path "${candidate}")"
    mapfile -t targets < <(
      {
        printf '%s\n' "${targets[@]:-}"
        append_unique "${candidate}" "${targets[@]:-}"
        append_unique "${resolved}" "${targets[@]:-}" "${candidate}"
      } | awk 'NF' | awk '!seen[$0]++'
    )
  done

  [[ ${#targets[@]} -gt 0 ]] || return 0

  local pid args target
  while read -r pid args; do
    [[ -n "${pid}" ]] || continue
    for target in "${targets[@]}"; do
      if [[ "${args}" == "${target} app-server"* || "${args}" == "${target} daemon"* ]]; then
        printf '%s\n' "${pid}"
        break
      fi
    done
  done < <(ps -eo pid=,args=)
}

terminate_pids() {
  local label="${1:-processes}"
  shift
  local pids=("$@")
  [[ ${#pids[@]} -gt 0 ]] || return 0

  echo "stopping ${label}: ${pids[*]}"
  kill "${pids[@]}" 2>/dev/null || true

  local pending=()
  local attempt pid
  for attempt in {1..20}; do
    pending=()
    for pid in "${pids[@]}"; do
      if kill -0 "${pid}" 2>/dev/null; then
        pending+=("${pid}")
      fi
    done
    [[ ${#pending[@]} -eq 0 ]] && return 0
    sleep 0.2
  done

  echo "forcing ${label} to exit: ${pending[*]}"
  kill -9 "${pending[@]}" 2>/dev/null || true
}

stop_integration_processes() {
  mapfile -t pids < <(list_integration_process_pids "$@" | awk '!seen[$0]++')
  terminate_pids "integration processes" "${pids[@]}"
}

bootstrap() {
  build_bins

  local bundle_codex="${VSCODE_BUNDLE_CODEX:-}"
  if [[ -z "${bundle_codex}" ]]; then
    bundle_codex="$(detect_vscode_bundle_codex || true)"
  fi

  local integration_mode="${INTEGRATION_MODE:-}"
  if [[ -z "${integration_mode}" ]]; then
    if [[ -n "${bundle_codex}" ]]; then
      integration_mode="both"
    else
      integration_mode="editor_settings"
    fi
  fi

  local relay_url="${RELAY_URL:-ws://127.0.0.1:9500/ws/agent}"
  local codex_binary="${CODEX_BINARY:-}"
  if [[ -z "${codex_binary}" ]]; then
    if [[ -n "${bundle_codex}" ]]; then
      codex_binary="$(dirname "${bundle_codex}")/codex.real"
    else
      codex_binary="codex"
    fi
  fi
  local binary_path="${BINARY_PATH:-${BIN_DIR}/codex-remote}"
  local install_bin_dir="${INSTALL_BIN_DIR:-${HOME}/.local/bin}"
  local installed_binary
  installed_binary="$(detect_install_bin_codex_remote)"
  local vscode_settings="${VSCODE_SETTINGS:-${HOME}/.config/Code/User/settings.json}"
  local feishu_app_id="${FEISHU_APP_ID:-}"
  local feishu_app_secret="${FEISHU_APP_SECRET:-}"
  local use_system_proxy="${FEISHU_USE_SYSTEM_PROXY:-false}"
  local local_binary="${BINARY_PATH:-${BIN_DIR}/codex-remote}"

  stop || true
  mapfile -t cleanup_targets < <(list_cleanup_target_paths "${bundle_codex}" "${installed_binary}" "${local_binary}")
  stop_integration_processes "${cleanup_targets[@]}"

  "${BIN_DIR}/codex-remote" install \
    -base-dir "${BASE_DIR}" \
    -install-bin-dir "${install_bin_dir}" \
    -binary "${binary_path}" \
    -relay-url "${relay_url}" \
    -codex-binary "${codex_binary}" \
    -integration "${integration_mode}" \
    -feishu-app-id "${feishu_app_id}" \
    -feishu-app-secret "${feishu_app_secret}" \
    -use-system-proxy="${use_system_proxy}" \
    -vscode-settings "${vscode_settings}" \
    -bundle-entrypoint "${bundle_codex}"

  cat <<EOF
bootstrap completed
config: ${CONFIG_FILE}
binary: ${binary_path}
install bin dir: ${install_bin_dir}
integration mode: ${integration_mode}
bundle entrypoint: ${bundle_codex}
codex binary: ${codex_binary}
EOF
}

start() {
  if [[ "${SKIP_BUILD:-false}" != "true" ]]; then
    build_bins
  fi
  if [[ -f "${PID_FILE}" ]] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
    echo "relayd already running with pid $(cat "${PID_FILE}")"
    return 0
  fi

  local runtime_pid=""
  runtime_pid="$(list_runtime_lock_pids | head -n 1 || true)"
  if [[ -n "${runtime_pid}" ]]; then
    echo "${runtime_pid}" > "${PID_FILE}"
    echo "relayd already running with pid ${runtime_pid}"
    return 0
  fi
  rm -f "${PID_FILE}"
  setsid env CODEX_REMOTE_CONFIG="${CONFIG_FILE}" "${BIN_DIR}/codex-remote" daemon </dev/null >>"${LOG_FILE}" 2>&1 &
  local pid=$!
  echo "${pid}" > "${PID_FILE}"
  sleep 1
  if kill -0 "${pid}" 2>/dev/null; then
    echo "relayd started with pid ${pid}"
    return 0
  fi
  echo "relayd failed to start; recent log:" >&2
  tail -n 20 "${LOG_FILE}" >&2 || true
  rm -f "${PID_FILE}"
  return 1
}

stop() {
  local local_binary="${BINARY_PATH:-${BIN_DIR}/codex-remote}"
  local installed_binary
  installed_binary="$(detect_install_bin_codex_remote)"
  local bundle_codex="${VSCODE_BUNDLE_CODEX:-}"
  if [[ -z "${bundle_codex}" ]]; then
    bundle_codex="$(detect_vscode_bundle_codex || true)"
  fi

  mapfile -t managed_pids < <(
    {
      if [[ -f "${PID_FILE}" ]]; then
        local pid
        pid="$(cat "${PID_FILE}")"
        if kill -0 "${pid}" 2>/dev/null; then
          printf '%s\n' "${pid}"
        fi
      fi
      list_runtime_lock_pids
    } | awk '!seen[$0]++'
  )
  mapfile -t cleanup_targets < <(list_cleanup_target_paths "${bundle_codex}" "${installed_binary}" "${local_binary}")
  mapfile -t integration_pids < <(list_integration_process_pids "${cleanup_targets[@]}" | awk '!seen[$0]++')
  mapfile -t pids < <(
    {
      printf '%s\n' "${managed_pids[@]:-}"
      printf '%s\n' "${integration_pids[@]:-}"
    } | awk 'NF && !seen[$0]++'
  )

  if [[ ${#pids[@]} -eq 0 ]]; then
    rm -f "${PID_FILE}"
    list_runtime_lock_pids >/dev/null || true
    echo "relayd is not running"
    return 0
  fi

  terminate_pids "relayd/integration processes" "${pids[@]}"
  rm -f "${PID_FILE}"
  list_runtime_lock_pids >/dev/null || true
  echo "relayd stopped"
}

restart() {
  stop
  local bundle_codex="${VSCODE_BUNDLE_CODEX:-}"
  if [[ -z "${bundle_codex}" ]]; then
    bundle_codex="$(detect_vscode_bundle_codex || true)"
  fi
  local local_binary="${BINARY_PATH:-${BIN_DIR}/codex-remote}"
  local installed_binary
  installed_binary="$(detect_install_bin_codex_remote)"
  mapfile -t cleanup_targets < <(list_cleanup_target_paths "${bundle_codex}" "${installed_binary}" "${local_binary}")
  stop_integration_processes "${cleanup_targets[@]}"
  start
}

refresh() {
  bootstrap
  SKIP_BUILD=true start
}

status() {
  local pid=""
  if [[ -f "${PID_FILE}" ]]; then
    pid="$(cat "${PID_FILE}")"
    if ! kill -0 "${pid}" 2>/dev/null; then
      rm -f "${PID_FILE}"
      pid=""
    fi
  fi
  if [[ -z "${pid}" ]]; then
    pid="$(list_runtime_lock_pids | head -n 1 || true)"
    if [[ -n "${pid}" ]]; then
      echo "${pid}" > "${PID_FILE}"
    fi
  fi
  if [[ -n "${pid}" ]]; then
    echo "relayd: running (pid ${pid})"
  else
    echo "relayd: stopped"
  fi
  echo "config: ${CONFIG_FILE}"
  echo "log file: ${LOG_FILE}"
}

logs() {
  touch "${LOG_FILE}"
  tail -n 200 -f "${LOG_FILE}"
}

usage() {
  cat <<EOF
usage: ./install.sh <bootstrap|start|stop|restart|refresh|status|logs|build>

environment overrides:
  GO_BIN
  BASE_DIR
  RELAY_URL
  CODEX_BINARY
  BINARY_PATH
  INSTALL_BIN_DIR
  INTEGRATION_MODE
  VSCODE_SETTINGS
  VSCODE_BUNDLE_CODEX
  VSCODE_SERVER_EXTENSIONS_DIR
  FEISHU_APP_ID
  FEISHU_APP_SECRET
  FEISHU_USE_SYSTEM_PROXY
EOF
}

case "${1:-}" in
  bootstrap)
    bootstrap
    ;;
  start)
    start
    ;;
  stop)
    stop
    ;;
  restart)
    restart
    ;;
  refresh)
    refresh
    ;;
  status)
    status
    ;;
  logs)
    logs
    ;;
  build)
    build_bins
    ;;
  *)
    usage
    exit 1
    ;;
esac

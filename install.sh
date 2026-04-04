#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
GO_BIN="${GO_BIN:-go}"

BASE_DIR="${BASE_DIR:-${HOME}}"
XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-${HOME}/.config}"
XDG_STATE_HOME="${XDG_STATE_HOME:-${HOME}/.local/state}"
XDG_DATA_HOME="${XDG_DATA_HOME:-${HOME}/.local/share}"

CONFIG_DIR="${XDG_CONFIG_HOME}/codex-relay"
STATE_DIR="${XDG_DATA_HOME}/codex-relay"
RUN_DIR="${XDG_STATE_HOME}/codex-relay"
LOG_DIR="${STATE_DIR}/logs"

WRAPPER_CONFIG="${CONFIG_DIR}/wrapper.env"
SERVICES_CONFIG="${CONFIG_DIR}/services.env"
PID_FILE="${RUN_DIR}/relayd.pid"
LOG_FILE="${LOG_DIR}/relayd.log"

mkdir -p "${BIN_DIR}" "${CONFIG_DIR}" "${STATE_DIR}" "${RUN_DIR}" "${LOG_DIR}"

build_bins() {
  "${GO_BIN}" build -o "${BIN_DIR}/relayd" "${ROOT_DIR}/cmd/relayd"
  "${GO_BIN}" build -o "${BIN_DIR}/relay-wrapper" "${ROOT_DIR}/cmd/relay-wrapper"
  "${GO_BIN}" build -o "${BIN_DIR}/relay-install" "${ROOT_DIR}/cmd/relay-install"
}

detect_vscode_bundle_codex() {
  local extensions_dir="${VSCODE_SERVER_EXTENSIONS_DIR:-${HOME}/.vscode-server/extensions}"
  [[ -d "${extensions_dir}" ]] || return 0

  while IFS= read -r dir; do
    [[ -n "${dir}" ]] || continue
    if [[ -f "${dir}/bin/linux-x86_64/codex" ]]; then
      printf '%s\n' "${dir}/bin/linux-x86_64/codex"
      return 0
    fi
    if [[ -f "${dir}/bin/linux-arm64/codex" ]]; then
      printf '%s\n' "${dir}/bin/linux-arm64/codex"
      return 0
    fi
  done < <(find "${extensions_dir}" -maxdepth 1 -mindepth 1 -type d -name 'openai.chatgpt-*' -printf '%T@ %p\n' | sort -nr | cut -d' ' -f2-)
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
      integration_mode="managed_shim"
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
  local wrapper_binary="${WRAPPER_BINARY:-${BIN_DIR}/relay-wrapper}"
  local vscode_settings="${VSCODE_SETTINGS:-${HOME}/.config/Code/User/settings.json}"
  local feishu_app_id="${FEISHU_APP_ID:-}"
  local feishu_app_secret="${FEISHU_APP_SECRET:-}"

  "${BIN_DIR}/relay-install" \
    -base-dir "${BASE_DIR}" \
    -wrapper-binary "${wrapper_binary}" \
    -relay-url "${relay_url}" \
    -codex-binary "${codex_binary}" \
    -integration "${integration_mode}" \
    -feishu-app-id "${feishu_app_id}" \
    -feishu-app-secret "${feishu_app_secret}" \
    -vscode-settings "${vscode_settings}" \
    -bundle-entrypoint "${bundle_codex}"

  cat <<EOF
bootstrap completed
wrapper config: ${WRAPPER_CONFIG}
services config: ${SERVICES_CONFIG}
wrapper binary: ${wrapper_binary}
integration mode: ${integration_mode}
bundle entrypoint: ${bundle_codex}
codex binary: ${codex_binary}
EOF
}

start() {
  build_bins
  if [[ -f "${PID_FILE}" ]] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
    echo "relayd already running with pid $(cat "${PID_FILE}")"
    return 0
  fi
  rm -f "${PID_FILE}"
  setsid env CODEX_RELAY_SERVICES_CONFIG="${SERVICES_CONFIG}" "${BIN_DIR}/relayd" </dev/null >>"${LOG_FILE}" 2>&1 &
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
  if [[ ! -f "${PID_FILE}" ]]; then
    echo "relayd is not running"
    return 0
  fi
  local pid
  pid="$(cat "${PID_FILE}")"
  if kill -0 "${pid}" 2>/dev/null; then
    kill "${pid}"
    wait "${pid}" 2>/dev/null || true
    echo "relayd stopped"
  else
    echo "stale pid file removed"
  fi
  rm -f "${PID_FILE}"
}

status() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid="$(cat "${PID_FILE}")"
    if kill -0 "${pid}" 2>/dev/null; then
      echo "relayd: running (pid ${pid})"
    else
      rm -f "${PID_FILE}"
      echo "relayd: stopped"
    fi
  else
    echo "relayd: stopped"
  fi
  echo "wrapper config: ${WRAPPER_CONFIG}"
  echo "services config: ${SERVICES_CONFIG}"
  echo "log file: ${LOG_FILE}"
}

logs() {
  touch "${LOG_FILE}"
  tail -n 200 -f "${LOG_FILE}"
}

usage() {
  cat <<EOF
usage: ./install.sh <bootstrap|start|stop|status|logs|build>

environment overrides:
  GO_BIN
  BASE_DIR
  RELAY_URL
  CODEX_BINARY
  WRAPPER_BINARY
  INTEGRATION_MODE
  VSCODE_SETTINGS
  VSCODE_BUNDLE_CODEX
  VSCODE_SERVER_EXTENSIONS_DIR
  FEISHU_APP_ID
  FEISHU_APP_SECRET
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

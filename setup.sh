#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────
# Codex Relay — 一键安装与启动脚本
# ─────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.pids"
ENV_FILE="$SCRIPT_DIR/.env"
ENV_EXAMPLE="$SCRIPT_DIR/.env.example"
WRAPPER_BIN="$SCRIPT_DIR/wrapper/target/release/codex-relay-wrapper"

# ── Colors ──────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

info()    { echo -e "${BLUE}ℹ${NC}  $*"; }
success() { echo -e "${GREEN}✔${NC}  $*"; }
warn()    { echo -e "${YELLOW}⚠${NC}  $*"; }
error()   { echo -e "${RED}✖${NC}  $*"; }
header()  { echo -e "\n${BOLD}${CYAN}═══ $* ═══${NC}\n"; }

# ── Prerequisites ───────────────────────────────────────

check_prerequisites() {
    header "检查前置依赖"
    local missing=0

    if command -v node &>/dev/null; then
        success "Node.js $(node --version)"
    else
        error "未找到 Node.js — 请安装 Node.js 18+"
        missing=1
    fi

    if command -v npm &>/dev/null; then
        success "npm $(npm --version)"
    else
        error "未找到 npm"
        missing=1
    fi

    if command -v cargo &>/dev/null; then
        success "Cargo $(cargo --version | awk '{print $2}')"
    else
        error "未找到 Cargo/Rust — 请安装 Rust 工具链 (https://rustup.rs)"
        missing=1
    fi

    if command -v rustc &>/dev/null; then
        success "rustc $(rustc --version | awk '{print $2}')"
    else
        error "未找到 rustc"
        missing=1
    fi

    if [[ $missing -ne 0 ]]; then
        error "缺少必要依赖，请先安装后重试"
        exit 1
    fi

    echo ""
    success "所有依赖检查通过"
}

# ── Install / Build ─────────────────────────────────────

do_install() {
    check_prerequisites

    header "安装 npm 依赖"
    cd "$SCRIPT_DIR"
    npm install
    success "npm 依赖安装完成"

    header "构建 shared 模块"
    npm -w shared run build
    success "shared 构建完成"

    header "构建 server"
    npm -w server run build
    success "server 构建完成"

    header "构建 bot"
    npm -w bot run build
    success "bot 构建完成"

    header "构建 wrapper (Rust release)"
    cd "$SCRIPT_DIR/wrapper"
    cargo build --release
    success "wrapper 构建完成: $WRAPPER_BIN"

    cd "$SCRIPT_DIR"
    echo ""
    success "全部组件构建完成！"
}

# ── Configure ───────────────────────────────────────────

do_configure() {
    header "交互式配置"

    # Start from example if no .env
    if [[ ! -f "$ENV_FILE" ]]; then
        if [[ -f "$ENV_EXAMPLE" ]]; then
            cp "$ENV_EXAMPLE" "$ENV_FILE"
            info "已从 .env.example 创建 .env"
        else
            error "未找到 .env.example 模板"
            exit 1
        fi
    fi

    # Read current values
    source_env_defaults

    # Prompt for Feishu config
    echo -e "${BOLD}飞书机器人配置${NC}"
    echo ""

    read -rp "$(echo -e "${CYAN}飞书 App ID${NC} [$FEISHU_APP_ID]: ")" input
    [[ -n "$input" ]] && FEISHU_APP_ID="$input"

    read -rp "$(echo -e "${CYAN}飞书 App Secret${NC} [$FEISHU_APP_SECRET]: ")" input
    [[ -n "$input" ]] && FEISHU_APP_SECRET="$input"

    echo ""
    echo -e "${BOLD}服务配置${NC}"
    echo ""

    read -rp "$(echo -e "${CYAN}Relay WebSocket 端口${NC} [$RELAY_PORT]: ")" input
    [[ -n "$input" ]] && RELAY_PORT="$input"

    read -rp "$(echo -e "${CYAN}Relay API 端口${NC} [$RELAY_API_PORT]: ")" input
    [[ -n "$input" ]] && RELAY_API_PORT="$input"

    read -rp "$(echo -e "${CYAN}Relay WebSocket URL${NC} [$RELAY_SERVER_URL]: ")" input
    [[ -n "$input" ]] && RELAY_SERVER_URL="$input"

    read -rp "$(echo -e "${CYAN}Codex 真实二进制路径${NC} [$CODEX_REAL_BINARY]: ")" input
    [[ -n "$input" ]] && CODEX_REAL_BINARY="$input"

    # Write .env
    cat > "$ENV_FILE" <<EOF
# Relay Server
RELAY_PORT=$RELAY_PORT
RELAY_API_PORT=$RELAY_API_PORT
SESSION_GRACE_PERIOD=${SESSION_GRACE_PERIOD:-300}
MESSAGE_BUFFER_SIZE=${MESSAGE_BUFFER_SIZE:-100}

# Feishu Bot
FEISHU_APP_ID=$FEISHU_APP_ID
FEISHU_APP_SECRET=$FEISHU_APP_SECRET
RELAY_API_URL=http://localhost:$RELAY_API_PORT

# Wrapper (also configurable via CLI args)
RELAY_SERVER_URL=$RELAY_SERVER_URL
CODEX_REAL_BINARY=$CODEX_REAL_BINARY
EOF

    success ".env 配置已保存"
    echo ""

    # Configure VS Code
    read -rp "$(echo -e "${CYAN}是否配置 VS Code? (Y/n):${NC} ")" yn
    yn="${yn:-Y}"
    if [[ "$yn" =~ ^[Yy] ]]; then
        do_vscode
    fi

    echo ""
    success "配置完成！"
}

source_env_defaults() {
    RELAY_PORT="${RELAY_PORT:-9500}"
    RELAY_API_PORT="${RELAY_API_PORT:-9501}"
    SESSION_GRACE_PERIOD="${SESSION_GRACE_PERIOD:-300}"
    MESSAGE_BUFFER_SIZE="${MESSAGE_BUFFER_SIZE:-100}"
    FEISHU_APP_ID="${FEISHU_APP_ID:-}"
    FEISHU_APP_SECRET="${FEISHU_APP_SECRET:-}"
    RELAY_SERVER_URL="${RELAY_SERVER_URL:-ws://localhost:9500}"
    CODEX_REAL_BINARY="${CODEX_REAL_BINARY:-codex}"

    if [[ -f "$ENV_FILE" ]]; then
        while IFS='=' read -r key value; do
            key="$(echo "$key" | xargs)"
            [[ -z "$key" || "$key" == \#* ]] && continue
            value="$(echo "$value" | xargs)"
            case "$key" in
                RELAY_PORT)          RELAY_PORT="$value" ;;
                RELAY_API_PORT)      RELAY_API_PORT="$value" ;;
                SESSION_GRACE_PERIOD) SESSION_GRACE_PERIOD="$value" ;;
                MESSAGE_BUFFER_SIZE) MESSAGE_BUFFER_SIZE="$value" ;;
                FEISHU_APP_ID)       FEISHU_APP_ID="$value" ;;
                FEISHU_APP_SECRET)   FEISHU_APP_SECRET="$value" ;;
                RELAY_SERVER_URL)    RELAY_SERVER_URL="$value" ;;
                CODEX_REAL_BINARY)   CODEX_REAL_BINARY="$value" ;;
                RELAY_API_URL)       ;;  # derived, skip
            esac
        done < "$ENV_FILE"
    fi
}

# ── VS Code Configuration ──────────────────────────────

find_vscode_settings() {
    local candidates=()

    if [[ "$(uname)" == "Darwin" ]]; then
        candidates=(
            "$HOME/Library/Application Support/Code/User/settings.json"
            "$HOME/Library/Application Support/Code - Insiders/User/settings.json"
            "$HOME/Library/Application Support/Cursor/User/settings.json"
        )
    else
        candidates=(
            "$HOME/.config/Code/User/settings.json"
            "$HOME/.config/Code - Insiders/User/settings.json"
            "$HOME/.config/Cursor/User/settings.json"
        )
    fi

    local found=()
    for path in "${candidates[@]}"; do
        if [[ -f "$path" ]]; then
            found+=("$path")
        fi
    done

    echo "${found[@]}"
}

set_vscode_setting() {
    local settings_file="$1"
    local key="chatgpt.cliExecutable"
    local value="$WRAPPER_BIN"

    if [[ ! -f "$settings_file" ]]; then
        warn "settings.json 不存在，将创建: $settings_file"
        mkdir -p "$(dirname "$settings_file")"
        echo '{}' > "$settings_file"
    fi

    # Try jq first
    if command -v jq &>/dev/null; then
        local tmp
        tmp="$(mktemp)"
        jq --arg v "$value" '.["'"$key"'"] = $v' "$settings_file" > "$tmp" && mv "$tmp" "$settings_file"
        success "已通过 jq 设置 $key"
        return 0
    fi

    # Fallback: python3
    if command -v python3 &>/dev/null; then
        python3 -c "
import json, sys
path = sys.argv[1]
with open(path, 'r') as f:
    data = json.load(f)
data['$key'] = sys.argv[2]
with open(path, 'w') as f:
    json.dump(data, f, indent=4, ensure_ascii=False)
    f.write('\n')
" "$settings_file" "$value"
        success "已通过 python3 设置 $key"
        return 0
    fi

    # Fallback: sed
    if grep -q "\"$key\"" "$settings_file"; then
        sed -i.bak "s|\"$key\"[[:space:]]*:.*|\"$key\": \"$value\",|" "$settings_file"
        rm -f "${settings_file}.bak"
        success "已通过 sed 更新 $key"
    else
        # Insert before last closing brace
        sed -i.bak "s|}|    \"$key\": \"$value\"\n}|" "$settings_file"
        rm -f "${settings_file}.bak"
        success "已通过 sed 添加 $key"
    fi
}

do_vscode() {
    header "配置 VS Code"

    if [[ ! -f "$WRAPPER_BIN" ]]; then
        warn "Wrapper 二进制尚未构建: $WRAPPER_BIN"
        warn "请先运行 ./setup.sh install"
        return 1
    fi

    local settings_paths
    settings_paths=$(find_vscode_settings)

    if [[ -z "$settings_paths" ]]; then
        warn "未找到 VS Code settings.json"
        info "手动添加以下配置到 VS Code settings.json:"
        echo ""
        echo -e "    ${BOLD}\"chatgpt.cliExecutable\": \"$WRAPPER_BIN\"${NC}"
        echo ""
        return 0
    fi

    for path in $settings_paths; do
        info "找到: $path"
        set_vscode_setting "$path"
    done

    success "VS Code 配置完成"
}

# ── Start ───────────────────────────────────────────────

do_start() {
    header "启动服务"

    # Check if already running
    if [[ -f "$PID_FILE" ]]; then
        local still_running=0
        while read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                still_running=1
            fi
        done < "$PID_FILE"
        if [[ $still_running -eq 1 ]]; then
            warn "服务已在运行中，请先执行 ./setup.sh stop"
            return 1
        fi
        rm -f "$PID_FILE"
    fi

    # Check .env
    if [[ ! -f "$ENV_FILE" ]]; then
        warn ".env 文件不存在，请先运行 ./setup.sh configure"
        return 1
    fi

    # Check builds
    if [[ ! -f "$SCRIPT_DIR/server/dist/index.js" ]]; then
        error "Server 尚未构建，请先运行 ./setup.sh install"
        return 1
    fi
    if [[ ! -f "$SCRIPT_DIR/bot/dist/index.js" ]]; then
        error "Bot 尚未构建，请先运行 ./setup.sh install"
        return 1
    fi

    # Create logs directory
    mkdir -p "$SCRIPT_DIR/logs"

    # Start server
    info "启动 Relay Server..."
    cd "$SCRIPT_DIR"
    NODE_ENV=production node server/dist/index.js \
        >> "$SCRIPT_DIR/logs/server.log" 2>&1 &
    local server_pid=$!
    echo "$server_pid" > "$PID_FILE"
    success "Relay Server 已启动 (PID: $server_pid)"

    # Brief pause to let server initialize
    sleep 1

    # Start bot
    info "启动 Feishu Bot..."
    NODE_ENV=production node bot/dist/index.js \
        >> "$SCRIPT_DIR/logs/bot.log" 2>&1 &
    local bot_pid=$!
    echo "$bot_pid" >> "$PID_FILE"
    success "Feishu Bot 已启动 (PID: $bot_pid)"

    echo ""
    success "所有服务已启动！"
    info "Server 日志: $SCRIPT_DIR/logs/server.log"
    info "Bot 日志:    $SCRIPT_DIR/logs/bot.log"
    info "PID 文件:    $PID_FILE"
    echo ""
    info "使用 ${BOLD}./setup.sh stop${NC} 停止服务"
    info "使用 ${BOLD}./setup.sh status${NC} 查看状态"
}

# ── Stop ────────────────────────────────────────────────

do_stop() {
    header "停止服务"

    if [[ ! -f "$PID_FILE" ]]; then
        info "没有发现运行中的服务"
        return 0
    fi

    while read -r pid; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            success "已停止进程 PID: $pid"
        else
            info "进程 PID: $pid 已不存在"
        fi
    done < "$PID_FILE"

    rm -f "$PID_FILE"
    echo ""
    success "所有服务已停止"
}

# ── Status ──────────────────────────────────────────────

do_status() {
    header "服务状态"

    if [[ ! -f "$PID_FILE" ]]; then
        info "没有发现运行中的服务"
        return 0
    fi

    local line=0
    local labels=("Relay Server" "Feishu Bot")
    while read -r pid; do
        local label="${labels[$line]:-Service $line}"
        if kill -0 "$pid" 2>/dev/null; then
            success "$label — ${GREEN}运行中${NC} (PID: $pid)"
        else
            error "$label — ${RED}已停止${NC} (PID: $pid)"
        fi
        line=$((line + 1))
    done < "$PID_FILE"

    echo ""

    # Show .env summary
    if [[ -f "$ENV_FILE" ]]; then
        source_env_defaults
        info "WebSocket 端口: $RELAY_PORT"
        info "API 端口:       $RELAY_API_PORT"
        info "Wrapper 二进制: $WRAPPER_BIN"
    fi
}

# ── Main ────────────────────────────────────────────────

usage() {
    echo -e "${BOLD}Codex Relay 安装与管理脚本${NC}"
    echo ""
    echo "用法: $0 [命令]"
    echo ""
    echo "命令:"
    echo "  install     构建全部组件 (npm install, tsc, cargo build)"
    echo "  configure   交互式配置 (.env, VS Code)"
    echo "  start       启动 server 和 bot 服务"
    echo "  stop        停止所有服务"
    echo "  vscode      配置 VS Code chatgpt.cliExecutable"
    echo "  status      查看服务运行状态"
    echo "  help        显示此帮助信息"
    echo ""
    echo "无参数运行时将执行: install → configure → start"
}

main() {
    local cmd="${1:-}"

    case "$cmd" in
        install)
            do_install
            ;;
        configure)
            do_configure
            ;;
        start)
            do_start
            ;;
        stop)
            do_stop
            ;;
        vscode)
            do_vscode
            ;;
        status)
            do_status
            ;;
        help|--help|-h)
            usage
            ;;
        "")
            # No args = one-click: install + configure + start
            header "Codex Relay 一键安装"
            do_install
            do_configure
            do_start
            ;;
        *)
            error "未知命令: $cmd"
            echo ""
            usage
            exit 1
            ;;
    esac
}

main "$@"

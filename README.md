# 🔗 Codex Relay

> 通过飞书机器人远程监控和控制 VS Code Codex 会话

[![Node.js](https://img.shields.io/badge/Node.js-18%2B-green)]()
[![Rust](https://img.shields.io/badge/Rust-stable-orange)]()
[![License](https://img.shields.io/badge/License-MIT-blue)]()

---

## 📖 项目介绍

Codex Relay 是一个中继系统，让你通过飞书机器人远程监控和控制运行在本地（或远程服务器）上的 VS Code Codex 会话。它在 Codex CLI 和飞书之间建立了一条实时通信链路，使你能够随时随地通过手机飞书查看 Codex 的工作状态、审批代码变更、发送新指令。

## 🎯 使用场景

1. 🏠 **在家启动任务** — 打开 VS Code，启动一个 Codex 编码任务
2. 🚶 **出门在外** — 离开电脑，该做的事情交给 Codex
3. 📱 **手机监控** — 打开飞书，实时查看 Codex 的输出和进度
4. ✅ **远程审批** — Codex 请求确认时，直接在飞书回复 `y` 或 `n`
5. 💬 **继续指挥** — 发送新的提示词，让 Codex 继续工作

## 🏗 系统架构

```
┌─────────────────┐         ┌──────────────────┐         ┌──────────────────┐
│   VS Code +     │  stdio  │     Wrapper      │   WS    │   Relay Server   │
│   Codex Ext.    │◄───────►│  (Rust Binary)   │◄───────►│   (Node.js)      │
│                 │         │                  │  :9500  │                  │
└─────────────────┘         └──────────────────┘         └────────┬─────────┘
                                                                  │ REST
                                                                  │ :9501
                                                         ┌────────▼─────────┐
                                                         │   Feishu Bot     │
                                                         │   (Node.js)      │
                                                         └────────┬─────────┘
                                                                  │ WebSocket
                                                                  │ (长连接)
                                                         ┌────────▼─────────┐
                                                         │   飞书云端        │
                                                         │   (Feishu Cloud) │
                                                         └──────────────────┘
```

**数据流:**

1. **Wrapper** 通过 stdio 接管 Codex CLI 的输入输出
2. **Wrapper** 通过 WebSocket 将消息转发到 **Relay Server**
3. **Relay Server** 暴露 REST API 供 **Feishu Bot** 调用
4. **Feishu Bot** 通过飞书 WebSocket 长连接与 **飞书云端** 通信
5. 用户在飞书发送指令 → 逆向传递到 Codex

## 📋 前置要求

| 依赖 | 版本要求 | 说明 |
|------|---------|------|
| **Node.js** | ≥ 18.0 | 运行 Server 和 Bot |
| **npm** | ≥ 9.0 | 包管理 (随 Node.js 安装) |
| **Rust toolchain** | stable | 编译 Wrapper (`rustup` 安装) |
| **VS Code** | 最新版 | 需安装 Codex 扩展 |
| **Codex CLI** | 最新版 | `npm install -g @anthropic-ai/codex` |
| **飞书企业应用** | — | 需要企业管理员权限创建 |

**可选依赖:**

| 工具 | 用途 |
|------|------|
| `jq` | 自动配置 VS Code settings.json (有 python3 也可) |

## 🚀 快速开始

最简单的方式 — 一条命令完成安装、配置、启动：

```bash
git clone <your-repo-url> codex-relay
cd codex-relay
chmod +x setup.sh
./setup.sh
```

脚本会依次执行:
1. ✅ 检查依赖 (Node.js, Rust)
2. 📦 安装 npm 依赖并构建 TypeScript
3. 🔨 编译 Rust wrapper (release 模式)
4. ⚙️ 交互式配置 (飞书凭据、端口、VS Code)
5. 🚀 启动 Server 和 Bot

## 🔧 手动安装

如果你更喜欢手动操作，可以按以下步骤进行：

### 1. 安装依赖并构建 TypeScript

```bash
# 安装所有 npm 依赖 (workspace 模式)
npm install

# 按顺序构建 TypeScript 模块
npm -w shared run build
npm -w server run build
npm -w bot run build
```

### 2. 编译 Rust Wrapper

```bash
cd wrapper
cargo build --release
cd ..
```

编译产物位于 `wrapper/target/release/codex-relay-wrapper`。

### 3. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env，填入你的飞书 App ID 和 App Secret
```

### 4. 启动服务

```bash
# 终端 1: 启动 Relay Server
node server/dist/index.js

# 终端 2: 启动 Feishu Bot
node bot/dist/index.js
```

### 5. 配置 VS Code

在 VS Code 的 `settings.json` 中添加:

```json
{
    "chatgpt.cliExecutable": "/你的路径/codex-relay/wrapper/target/release/codex-relay-wrapper"
}
```

## ⚙️ 配置说明

所有配置通过 `.env` 文件管理:

### Server 配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `RELAY_PORT` | `9500` | WebSocket 端口，Wrapper 连接此端口 |
| `RELAY_API_PORT` | `9501` | REST API 端口，Bot 调用此端口 |
| `SESSION_GRACE_PERIOD` | `300` | Wrapper 断连后保留会话的秒数 |
| `MESSAGE_BUFFER_SIZE` | `100` | 每个会话的消息历史缓冲区大小 |

### Bot 配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FEISHU_APP_ID` | — | 飞书应用的 App ID (必填) |
| `FEISHU_APP_SECRET` | — | 飞书应用的 App Secret (必填) |
| `RELAY_API_URL` | `http://localhost:9501` | Relay Server 的 API 地址 |

### Wrapper 配置

| 环境变量 | CLI 参数 | 默认值 | 说明 |
|---------|----------|--------|------|
| `RELAY_SERVER_URL` | `--relay-url` | `ws://localhost:9500` | Relay Server 的 WebSocket 地址 |
| `CODEX_REAL_BINARY` | `--codex-binary` | `codex` | 真实 Codex CLI 的路径 |
| — | `--name` | 当前目录名 | 会话显示名称 |

> 💡 Wrapper 的配置优先级: CLI 参数 > 环境变量 > 默认值

## 🤖 飞书机器人配置

### 第一步: 创建企业自建应用

1. 登录 [飞书开放平台](https://open.feishu.cn/app)
2. 点击 **创建企业自建应用**
3. 填写应用名称 (如 "Codex Relay") 和描述
4. 上传一个应用图标

### 第二步: 开启机器人能力

1. 在应用页面左侧菜单，找到 **添加应用能力**
2. 添加 **机器人** 能力

### 第三步: 权限配置

在 **权限管理** 页面，申请以下权限：

| 权限 | 权限名称 | 说明 |
|------|---------|------|
| `im:message` | 获取与发送单聊、群组消息 | 接收用户消息 |
| `im:message:send_as_bot` | 以应用的身份发送消息 | 发送回复 |

### 第四步: 事件订阅

1. 进入 **事件订阅** 页面
2. **接收方式** 选择 → **使用长连接接收事件（WebSocket）**
3. 添加以下事件:

| 事件 | 说明 |
|------|------|
| `im.message.receive_v1` | 接收用户消息 |
| `application.bot.menu_v6` | 机器人菜单点击事件 (可选) |

> ⚠️ **重要**: 必须选择 **WebSocket 长连接** 方式，而不是 HTTP 回调方式。Bot 使用飞书 SDK 的 WebSocket 模式连接飞书云端，无需公网 IP 或域名。

### 第五步: 获取凭据

在 **凭证与基础信息** 页面获取:
- **App ID** → 填入 `.env` 的 `FEISHU_APP_ID`
- **App Secret** → 填入 `.env` 的 `FEISHU_APP_SECRET`

### 第六步 (可选): 配置机器人菜单

在 **机器人** 页面可以配置快捷菜单:

| 菜单名称 | 指令 |
|---------|------|
| 查看会话列表 | `/list` |
| 查看当前状态 | `/status` |
| 查看历史消息 | `/history` |
| 断开连接 | `/detach` |
| 中断任务 | `/stop` |

### 第七步: 发布应用

1. 在 **版本管理与发布** 页面创建版本
2. 提交审核并等待企业管理员通过
3. 审核通过后，在飞书中搜索机器人名称即可开始使用

## 🖥 VS Code 配置

Codex VS Code 扩展通过 `chatgpt.cliExecutable` 设置指定 CLI 二进制。将其指向 Wrapper 即可接入中继系统。

### 自动配置

```bash
./setup.sh vscode
```

### 手动配置

1. 打开 VS Code，按 `Ctrl+Shift+P` (macOS: `Cmd+Shift+P`)
2. 输入 `Preferences: Open Settings (JSON)`
3. 添加:

```json
{
    "chatgpt.cliExecutable": "/absolute/path/to/wrapper/target/release/codex-relay-wrapper"
}
```

### settings.json 路径

| 操作系统 | 路径 |
|---------|------|
| Linux | `~/.config/Code/User/settings.json` |
| macOS | `~/Library/Application Support/Code/User/settings.json` |
| Insiders 版 | 将 `Code` 替换为 `Code - Insiders` |
| Cursor | 将 `Code` 替换为 `Cursor` |

## 📱 飞书命令参考

在飞书中与机器人对话时，可使用以下命令:

| 命令 | 说明 | 示例 |
|------|------|------|
| `/list` | 列出所有在线 Codex 会话 | `/list` |
| `/attach <session>` | 连接到指定会话 (支持模糊匹配) | `/attach my-project` |
| `/detach` | 断开当前会话 | `/detach` |
| `/status` | 查看当前会话详细状态 | `/status` |
| `/history [n]` | 查看最近 n 条消息 (默认 10) | `/history 20` |
| `/stop` | 中断正在执行的任务 | `/stop` |
| 普通文本 | 作为提示词发送给 Codex | `请帮我重构这个函数` |
| `y` | 同意 Codex 的审批请求 | `y` |
| `n` | 拒绝 Codex 的审批请求 | `n` |

### 典型使用流程

```
用户: /list
Bot:  📋 在线会话:
      1. my-project (空闲)
      2. api-server (执行中)

用户: /attach my-project
Bot:  ✅ 已连接到会话: my-project

用户: 帮我给 User 模型添加 email 验证
Bot:  🤖 Codex: 好的，我来为 User 模型添加 email 验证...
      [Codex 输出实时转发]

Bot:  ⚠️ Codex 请求确认: 修改 models/user.ts，添加 email 字段验证
用户: y
Bot:  ✅ 已确认

用户: /detach
Bot:  👋 已断开会话
```

## 🌐 部署方案

### 方案一: 本地开发 (推荐入门)

所有组件运行在同一台机器:

```
[你的电脑]
├── VS Code + Codex + Wrapper
├── Relay Server (:9500, :9501)
└── Feishu Bot
```

```bash
# .env 配置
RELAY_SERVER_URL=ws://localhost:9500
RELAY_API_URL=http://localhost:9501
```

### 方案二: 远程服务器

Server 和 Bot 部署在云服务器，Wrapper 在开发机:

```
[云服务器]                        [开发机]
├── Relay Server (:9500, :9501)   ├── VS Code + Codex
└── Feishu Bot                    └── Wrapper → ws://云服务器:9500
```

```bash
# 开发机 .env
RELAY_SERVER_URL=ws://your-server.com:9500

# 云服务器 .env
RELAY_PORT=9500
RELAY_API_PORT=9501
RELAY_API_URL=http://localhost:9501
FEISHU_APP_ID=your-app-id
FEISHU_APP_SECRET=your-app-secret
```

> 💡 确保云服务器的 9500 端口对开发机可达。API 端口 9501 只需 Bot 本地访问，无需公开。

### 方案三: Docker 部署

🚧 *规划中，尚未实现。* 将提供 `docker-compose.yml` 一键部署 Server + Bot。

## 🔒 安全注意事项

### WebSocket 连接

- 当前 Wrapper 与 Server 之间的 WebSocket 连接 **未加密** (ws://)
- 在公网部署时，强烈建议:
  - 使用 **反向代理** (如 Nginx) 加上 TLS (wss://)
  - 或通过 **SSH 隧道** 转发端口
  - 配合防火墙限制 9500 端口访问来源

### 端口暴露

| 端口 | 用途 | 公开建议 |
|------|------|---------|
| 9500 | WebSocket (Wrapper → Server) | 仅允许 Wrapper 所在 IP |
| 9501 | REST API (Bot → Server) | **不应公开**，仅本地访问 |

### API 密钥

- 飞书 App Secret 存储在 `.env` 文件中
- `.env` 已在 `.gitignore` 中，**不要提交到版本控制**
- 定期轮换飞书应用密钥

## 🔍 故障排查

### 常见问题

<details>
<summary><strong>Wrapper 连接不上 Relay Server</strong></summary>

1. 确认 Server 已启动: `./setup.sh status`
2. 检查 `RELAY_SERVER_URL` 是否正确
3. 检查端口是否被占用: `lsof -i :9500`
4. 检查防火墙设置
5. 查看 Server 日志: `cat logs/server.log`

</details>

<details>
<summary><strong>飞书机器人没有响应</strong></summary>

1. 确认 Bot 已启动: `./setup.sh status`
2. 检查 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET` 是否正确
3. 确认飞书应用已发布并通过审核
4. 确认事件订阅使用了 **WebSocket 长连接** 方式
5. 查看 Bot 日志: `cat logs/bot.log`

</details>

<details>
<summary><strong>VS Code 中 Codex 无法启动</strong></summary>

1. 确认 Wrapper 已编译: `ls wrapper/target/release/codex-relay-wrapper`
2. 确认 `chatgpt.cliExecutable` 路径正确 (使用绝对路径)
3. 确认 Wrapper 有执行权限: `chmod +x wrapper/target/release/codex-relay-wrapper`
4. 在终端手动测试: `./wrapper/target/release/codex-relay-wrapper --help`

</details>

<details>
<summary><strong>消息延迟或丢失</strong></summary>

1. 检查网络连接稳定性
2. 增大 `MESSAGE_BUFFER_SIZE` (默认 100)
3. 检查 `SESSION_GRACE_PERIOD` 设置 (默认 300 秒)
4. 查看 Server 日志中是否有 WebSocket 断连重连记录

</details>

<details>
<summary><strong>编译 Wrapper 失败</strong></summary>

1. 确认 Rust 工具链已安装: `rustup show`
2. 更新工具链: `rustup update stable`
3. 清理后重新构建: `cd wrapper && cargo clean && cargo build --release`
4. 检查是否安装了 OpenSSL 开发库 (native-tls 依赖):
   - Ubuntu/Debian: `sudo apt install libssl-dev pkg-config`
   - macOS: `brew install openssl`

</details>

## 📁 项目结构

```
codex-relay/
├── setup.sh                 # 一键安装与管理脚本
├── .env.example             # 环境变量模板
├── .env                     # 实际配置 (不提交到 git)
├── package.json             # npm workspace 根配置
├── eslint.config.js         # ESLint 配置
│
├── shared/                  # 共享 TypeScript 类型
│   ├── src/                 # 源码
│   ├── dist/                # 编译输出
│   ├── package.json
│   └── tsconfig.json
│
├── server/                  # Relay Server
│   ├── src/                 # 源码 (Express + WebSocket)
│   ├── dist/                # 编译输出
│   ├── package.json
│   └── tsconfig.json
│
├── bot/                     # 飞书机器人
│   ├── src/                 # 源码 (Feishu SDK)
│   ├── dist/                # 编译输出
│   ├── package.json
│   └── tsconfig.json
│
├── wrapper/                 # Codex CLI 包装器
│   ├── src/                 # Rust 源码
│   ├── target/              # 编译产物
│   │   └── release/
│   │       └── codex-relay-wrapper  # 最终二进制
│   └── Cargo.toml
│
└── logs/                    # 运行日志 (自动创建)
    ├── server.log
    └── bot.log
```

## 🛠 开发指南

### 运行测试

```bash
# 运行所有 TypeScript 测试
npm -w shared run test
npm -w server run test
npm -w bot run test

# 运行 Rust 测试
cd wrapper && cargo test
```

### 开发模式构建

```bash
# TypeScript 增量编译 (以 server 为例)
cd server && npx tsc --watch

# Rust debug 构建 (更快，但不优化)
cd wrapper && cargo build
```

### 代码检查

```bash
# ESLint
npx eslint .

# Rust 格式和 lint
cd wrapper && cargo fmt --check && cargo clippy
```

### 项目架构要点

- **shared/** — 定义所有 TypeScript 类型，被 server 和 bot 共同引用
- **server/** — Express HTTP + WebSocket 服务器，管理 Wrapper 会话
- **bot/** — 飞书 SDK 客户端，桥接飞书消息和 Relay API
- **wrapper/** — Rust 二进制，stdio 代理 + WebSocket 客户端

## 📌 快速参考卡片

```
┌─────────────────────────────────────────────────────┐
│                 Codex Relay 速查表                    │
├─────────────────────────────────────────────────────┤
│                                                     │
│  安装: ./setup.sh install                           │
│  配置: ./setup.sh configure                         │
│  启动: ./setup.sh start                             │
│  停止: ./setup.sh stop                              │
│  状态: ./setup.sh status                            │
│  VS Code: ./setup.sh vscode                         │
│  一键: ./setup.sh                                    │
│                                                     │
│  飞书命令:                                           │
│  /list            列出在线会话                        │
│  /attach <名称>    连接会话                           │
│  /detach          断开会话                            │
│  /status          查看状态                            │
│  /history [n]     历史消息                            │
│  /stop            中断任务                            │
│  y / n            审批操作                            │
│                                                     │
│  端口:                                               │
│  9500  WebSocket (Wrapper ↔ Server)                 │
│  9501  REST API  (Bot ↔ Server)                     │
│                                                     │
│  日志:                                               │
│  logs/server.log   Server 日志                       │
│  logs/bot.log      Bot 日志                          │
│                                                     │
└─────────────────────────────────────────────────────┘
```

## 📄 License

MIT

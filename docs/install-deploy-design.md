# 安装与部署设计

## 1. 范围

这份文档描述当前 Go 版本的安装、配置和部署模型，覆盖：

- 在线安装脚本
- 交互安装脚本
- `relay-install` 非交互安装
- VS Code / VS Code Remote 接管方式
- `relayd` 本机运行与 Docker 运行
- 飞书应用配置模板

## 2. 操作入口

### 2.1 `setup.sh` / `setup.ps1`

产品入口。

职责：

- 构建三个二进制
- 调起 `relay-install`
- 无参数时默认进入交互向导

默认行为：

- Linux: 默认 `editor_settings + managed_shim`
- macOS: 默认 `editor_settings`
- Windows: 默认 `editor_settings`

### 2.2 `install-release.sh`

在线安装入口，面向最终用户。

职责：

- 解析平台和架构
- 下载最新或指定 release 包
- 解压到本地 release cache
- 启动包内的 `setup.sh`

它需要兼容：

- `curl | bash`
- 指定版本安装
- CI 中通过本地 HTTP server 做 smoke test

默认缓存目录：

- Linux: `~/.local/share/codex-remote/releases`
- macOS: `~/Library/Application Support/codex-remote/releases`

### 2.3 `relay-install`

安装器核心逻辑。

职责：

- 写 `wrapper.env`
- 写 `services.env`
- 保留已有飞书凭证
- 安装或复制二进制到稳定目录
- patch `settings.json`
- 或 patch 扩展 bundle 入口
- 记录 `install-state.json`

它同时支持：

- 交互模式：`-interactive`
- 非交互模式：直接传 flags

### 2.4 `install.sh`

Linux 仓库内运维脚本，不是跨平台产品入口。

职责：

- `bootstrap`
- `start`
- `stop`
- `status`
- `logs`
- `build`

它主要方便开发和仓库内联调。

## 3. 默认布局

### 3.1 配置与状态

当前 runtime 仍使用统一布局：

```text
<baseDir>/.config/codex-remote/wrapper.env
<baseDir>/.config/codex-remote/services.env
<baseDir>/.local/share/codex-remote/install-state.json
<baseDir>/.local/share/codex-remote/logs/codex-remote-relayd.log
<baseDir>/.local/state/codex-remote/codex-remote-relayd.pid
```

默认 `baseDir` 是用户 home 目录。

### 3.2 已安装二进制目录

默认安装目录按平台区分：

- Linux: `~/.local/bin`
- macOS: `~/Library/Application Support/codex-remote/bin`
- Windows: `%LOCALAPPDATA%\codex-remote\bin`

这里是安装器放置稳定二进制的位置，和配置目录是两回事。

## 4. 集成模式

### 4.1 `editor_settings`

修改 VS Code `settings.json`：

- `chatgpt.cliExecutable`

适用：

- 本机桌面 VS Code
- 不方便直接改 bundle 时

### 4.2 `managed_shim`

直接接管扩展 bundle 里的 `codex` 入口：

1. 原始 `codex` 重命名为 `codex.real` 或 `codex.real.exe`
2. 把 `relay-wrapper` 二进制复制到原始 `codex` 路径
3. `wrapper.env` 的 `CODEX_REAL_BINARY` 自动指向保留的 `codex.real`

适用：

- VS Code Remote
- 希望不依赖 `settings.json` 的场景

### 4.3 默认选择策略

- Linux 默认两者都做
- macOS / Windows 默认只做 `editor_settings`
- 非交互安装如果显式传 `-integration`，以显式参数为准

## 5. 配置内容

### 5.1 `wrapper.env`

安装器写入：

- `RELAY_SERVER_URL`
- `CODEX_REAL_BINARY`
- `CODEX_REMOTE_WRAPPER_NAME_MODE`
- `CODEX_REMOTE_WRAPPER_INTEGRATION_MODE`

规则：

- 如果启用了 `managed_shim` 且未显式给 `CODEX_REAL_BINARY`
- 则自动使用 bundle 内保留下来的 `codex.real`

### 5.2 `services.env`

安装器写入：

- `RELAY_PORT`
- `RELAY_API_PORT`
- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- `FEISHU_USE_SYSTEM_PROXY`

规则：

- 如果这次安装没有显式传新的飞书凭证
- 会保留已有值，不做清空

### 5.3 `install-state.json`

当前记录：

- config 路径
- 安装状态路径
- 已安装 wrapper / relayd 二进制路径
- 实际启用的 integrations
- `settings.json` 路径
- bundle 入口路径

## 6. Docker 模型

Docker 只部署 `relayd`。

不放进容器的部分：

- `relay-wrapper`
- 真实 `codex`
- VS Code 扩展 bundle

原因：

- wrapper 必须和 VS Code / Codex 进程在同一侧
- `relayd` 作为常驻服务，容器化收益更高

当前资产：

- [deploy/docker/Dockerfile](../deploy/docker/Dockerfile)
- [deploy/docker/compose.yml](../deploy/docker/compose.yml)
- [deploy/docker/.env.example](../deploy/docker/.env.example)

默认映射：

- `127.0.0.1:9500 -> relay websocket`
- `127.0.0.1:9501 -> status api`

因此 host 上的 wrapper 默认仍可直接连：

```text
ws://127.0.0.1:9500/ws/agent
```

## 7. 飞书配置模板

当前仓库提供：

- [deploy/feishu/app-template.json](../deploy/feishu/app-template.json)
- [deploy/feishu/README.md](../deploy/feishu/README.md)

它们的作用是固定项目依赖的菜单、事件和权限，不是飞书官方导入格式。

至少需要配置：

- 文本消息接收
- 图片消息接收
- reaction 创建事件
- 机器人菜单点击事件
- 机器人发送文本 / 卡片 / reaction 的能力
- P2P 单聊消息权限

## 8. 非交互安装示例

在线安装：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

固定版本在线安装：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

Linux + VS Code Remote：

```bash
./setup.sh \
  -integration both \
  -bundle-entrypoint "$HOME/.vscode-server/extensions/openai.chatgpt-<version>/bin/linux-x86_64/codex" \
  -feishu-app-id cli_xxx \
  -feishu-app-secret secret_xxx \
  -relay-url ws://127.0.0.1:9500/ws/agent
```

Docker relayd + host wrapper：

```bash
cp deploy/docker/.env.example deploy/docker/.env
docker compose -f deploy/docker/compose.yml --env-file deploy/docker/.env up -d --build
./setup.sh -relay-url ws://127.0.0.1:9500/ws/agent
```

## 9. 已知边界

- 还没有 uninstall / rollback
- 飞书模板还不是官方一键导入格式
- runtime config lookup 仍沿用统一 `.config` / `.local` 布局
- `install.sh` 仍是仓库脚本，不是系统级服务管理器

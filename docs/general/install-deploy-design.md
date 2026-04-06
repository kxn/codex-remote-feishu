# 安装与部署设计

> Type: `general`
> Updated: `2026-04-06`
> Summary: 迁移到 `docs/general` 并统一文档元信息头，同步修正目录迁移后的相对链接。

## 1. 范围

这份文档描述当前 Go 版本的安装、配置和部署模型，覆盖：

- 在线安装脚本
- 交互安装脚本
- `codex-remote install` 非交互安装
- VS Code / VS Code Remote 接管方式
- `relayd` 本机运行与 Docker 运行
- 飞书应用配置模板

## 2. 操作入口

### 2.1 `setup.sh` / `setup.ps1`

产品入口。

职责：

- 构建统一二进制 `codex-remote`
- 调起 `codex-remote install`
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

### 2.3 `codex-remote install`

安装器核心逻辑。

职责：

- 写统一配置 `config.json`
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
<baseDir>/.config/codex-remote/config.json
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
2. 把统一二进制 `codex-remote` 复制到原始 `codex` 路径
3. `config.json` 的 `wrapper.codexRealBinary` 自动指向保留的 `codex.real`

适用：

- VS Code Remote
- 希望不依赖 `settings.json` 的场景

### 4.3 默认选择策略

- Linux 默认两者都做
- macOS / Windows 默认只做 `editor_settings`
- 非交互安装如果显式传 `-integration`，以显式参数为准

## 5. 配置内容

### 5.1 `config.json`

安装器统一写入：

- `relay.serverURL`
- `relay.listenHost`
- `relay.listenPort`
- `admin.listenHost`
- `admin.listenPort`
- `wrapper.codexRealBinary`
- `wrapper.nameMode`
- `wrapper.integrationMode`
- `feishu.useSystemProxy`
- `feishu.apps[0].appId`
- `feishu.apps[0].appSecret`

规则：

- wrapper role 和 daemon role 从同一个文件里各取所需
- 如果启用了 `managed_shim` 且未显式给 `wrapper.codexRealBinary`
- 则自动使用 bundle 内保留下来的 `codex.real`
- 如果这次安装没有显式传新的飞书凭证
- 会保留已有值，不做清空
- 如果当前机器还只有 legacy `config.env` / `wrapper.env` / `services.env`
- 启动时会自动迁移到 `config.json` 并备份旧文件

### 5.2 `install-state.json`

当前记录：

- config 路径
- 安装状态路径
- 已安装统一二进制路径
- 实际启用的 integrations
- `settings.json` 路径
- bundle 入口路径

## 6. Docker 模型

Docker 只部署 `relayd`。

不放进容器的部分：

- `codex-remote` 的 wrapper role
- 真实 `codex`
- VS Code 扩展 bundle

原因：

- wrapper 必须和 VS Code / Codex 进程在同一侧
- `relayd` 作为常驻服务，容器化收益更高

当前资产：

- [deploy/docker/Dockerfile](../../deploy/docker/Dockerfile)
- [deploy/docker/compose.yml](../../deploy/docker/compose.yml)
- [deploy/docker/.env.example](../../deploy/docker/.env.example)

默认映射：

- `127.0.0.1:9500 -> relay websocket`
- `127.0.0.1:9501 -> status api`

因此 host 上的 wrapper 默认仍可直接连：

```text
ws://127.0.0.1:9500/ws/agent
```

## 7. 飞书配置模板

当前仓库提供：

- [deploy/feishu/app-template.json](../../deploy/feishu/app-template.json)
- [deploy/feishu/README.md](../../deploy/feishu/README.md)

它们的作用是固定项目依赖的菜单、事件和权限，不是飞书官方导入格式。

至少需要配置：

- 文本消息接收
- 图片消息接收
- reaction 创建事件
- 机器人菜单点击事件
- 机器人发送文本 / 卡片 / reaction 的能力
- P2P 单聊消息权限

如果要启用 assistant 最终回复里的本地 `.md` 预览，推荐额外开通：

- `drive:drive`

原因是当前实现会在发送前自动完成：

- 创建应用云空间目录
- 上传 Markdown 文件
- 查询文件访问链接
- 给目录和文件增加协作者权限

如果缺少这部分权限，relay 主功能仍可使用，但 `.md` 链接会保留原样，不会被替换成飞书预览链接。

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

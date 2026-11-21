# Codex Remote Feishu

`codex-remote-feishu` 把运行在你机器上的 Agent 接入飞书。项目群由负责人协调任务、邀请合适的同事并汇报成果。私聊可以处理个人工作、查看已有工作，也可以进入你有权限的项目。

详细使用说明见 [用户使用说明书](./docs/general/user-guide.md)。

## 安装

当前支持 Linux、macOS（Intel / Apple Silicon）和 Windows（amd64）。安装方式任选一种。

### 方式一：原生安装器（推荐）

#### Windows

1. 从 [GitHub Releases](https://github.com/kxn/codex-remote-feishu/releases) 下载：

   ```text
   codex-remote-feishu_<version>_windows_amd64_installer.exe
   ```

2. 双击运行安装器。
3. 首次安装完成后，安装器结果页会出现 **Continue WebSetup**，点击后进入 WebSetup 完成飞书接入。
4. 已经安装过时再次运行安装器，会按 repair / 升级处理，不会覆盖你已有的配置。

#### macOS

1. 从 [GitHub Releases](https://github.com/kxn/codex-remote-feishu/releases) 下载：

   ```text
   codex-remote-feishu_<version>_darwin_universal_installer.dmg
   ```

2. 打开 DMG，运行其中的 **Install Codex Remote.app**。
3. 首次安装可以选择安装目录；完成后在结果页打开 WebSetup 完成飞书接入。
4. 已安装过时再次运行，会按 repair / 升级处理。

### 方式二：在线脚本安装

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1 | iex
```

安装最新 `beta` track：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Track beta
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Version v1.0.0
```

脚本会自动识别平台、下载对应 release 包、安装并启动本地 daemon，然后打开或打印 WebSetup 地址。

### 方式三：手动解压 release 包

从 GitHub Releases 下载对应平台的二进制归档，解压后运行：

macOS / Linux：

```bash
./codex-remote install -bootstrap-only -start-daemon
```

Windows PowerShell：

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

然后打开输出里的 `/setup` 地址完成初始化。

## 首次配置

安装完成后，WebSetup 会带你完成：

1. **运行环境检查**：确认本机 `codex` 可用、路径正确。
2. **连接飞书机器人**：扫码新建应用，或接入已有应用；WebSetup 会做只读配置检查，缺失权限会给出可复制的导入 JSON。
3. **飞书自动配置与菜单确认**：按页面提示处理。
4. **本机集成**：按需配置自动启动。
5. **完成**：进入本地管理页。

飞书应用不需要在安装前手动准备 `App ID` / `App Secret`；安装后通过 WebSetup 完成即可。

## 升级

在飞书私聊中发送以下命令，然后核对卡片上的目标版本并确认升级。

```text
/upgrade latest
```

查看或切换升级渠道：

```text
/upgrade track
```

daemon 不会在后台自动弹升级提示；升级只通过这条手动入口触发。

## 模型后端与配置

项目同时支持 **Codex** 和 **Claude Code** 两个后端：

- `/mode codex`（默认）：使用机器上已有的 Codex CLI、登录状态和配置
- `/mode claude`：使用机器上已有的 Claude Code（`claude` CLI）登录状态和配置

默认不需要你做任何额外模型配置，开箱就用机器上已经配好的环境。

如果你想在不影响现有配置的前提下，并行使用多套模型参数，也可以在管理页（Admin UI）中添加：

- **Codex API Profile**：独立设置 base URL、API key、模型、推理强度等
- **Claude Profile**：独立设置认证方式、base URL、模型、推理强度等

这些 profile 保存在 codex-remote 自己的 `config.json` 里，不会改写 `~/.codex` 或 `~/.claude` 的原有配置，可以随时切换、并行使用。

在飞书里切换：

```text
/codexprofile
/claudeprofile
```

## 开始使用

- 私聊发送 `/menu`，可以打开项目、新建工作、继续已有工作、选择模型和工作目录。
- 在项目群向负责人说明需求、期望的结果和已有资料。负责人协调其他 Agent，向你确认需要决定的事项。
- 在飞书中补充说明、审阅结果、暂停或继续工作。私聊支持的命令和操作见 [用户使用说明书](./docs/general/user-guide.md#7-常用命令)。

## 排障

先确认本地链路没有被代理污染：

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
```

然后按顺序检查：

1. `curl --noproxy '*' -sf http://127.0.0.1:9501/api/admin/bootstrap-state`
2. `curl --noproxy '*' -sf http://127.0.0.1:9501/v1/status`
3. `config.json` 中的 `relay.serverURL`、飞书凭证和监听地址
4. daemon 日志（默认 `~/.local/share/codex-remote/logs/codex-remote-relayd.log`）

## 相关文档

- [用户使用说明书](./docs/general/user-guide.md)
- [飞书应用配置说明](./deploy/feishu/README.md)
- [文档索引](./docs/README.md)
- [变更记录](./CHANGELOG.md)

面向开发者：

- [开发者指南](./DEVELOPER.md)
- [安装与部署设计](./docs/general/install-deploy-design.md)
- [架构说明](./docs/general/architecture.md)

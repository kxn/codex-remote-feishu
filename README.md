# Codex Remote Feishu

`codex-remote-feishu` 把一台机器上的 Codex 工作现场带到飞书：你可以远程接管工作区、继续已有会话、新建会话、发图、看进度、停止任务，也能在需要时接入 VS Code。

它适合这些场景：

- 出门在外，用手机继续刚才在电脑上做的工作
- 不方便开电脑时，快速查看 Codex 当前进展并补一句指令
- 在飞书里继续某个项目、发一张截图、让任务继续往下跑

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
4. **本机集成**：按需处理自动启动和 VS Code 集成；这两项都是可选的，确认后即可继续。
5. **完成**：进入本地管理页。

飞书应用不需要在安装前手动准备 `App ID` / `App Secret`；安装后通过 WebSetup 完成即可。

## 升级

日常升级直接在飞书会话里发送：

```text
/upgrade latest
```

查看或切换升级渠道：

```text
/upgrade track
```

daemon 不会在后台自动弹升级提示；升级只通过这条手动入口触发。

## 开始使用

最常用的入口：

- `/list`：选择或添加工作区
- `/use`：继续最近会话
- `/useall`：查看全部可见会话
- `/new`：在当前工作区新建会话
- `/history`：查看当前会话历史
- `/status`：查看当前状态、队列和模型配置
- `/stop`：中断当前任务并清空队列
- `/detach`：断开当前接管
- `/sendfile`：把当前工作区里的文件发回飞书
- `/cron`：配置当前 daemon 的定时任务
- `/help`：查看当前可用命令
- `/menu`：打开命令菜单首页

工作方式：

- 先发图片不会立刻触发请求，图片会暂存，等你下一条文字一起发给 Codex。
- 直接回复当前正在执行的那条消息，可以作为对当前任务的跟进；排队消息也可以点 `ThumbsUp` 升级成跟进。
- `/steerall` 可以把队列里可并入本轮执行的输入一次性并入。
- 最终回复会直接回在触发它的原始消息下面，群聊里更容易看懂上下文。

## VS Code（可选）

VS Code 接入是可选增强，不是开始使用的前提。如果你需要飞书跟随 VS Code 当前焦点：

1. 先退出 VS Code。
2. 在 WebSetup / Admin UI 中执行 VS Code 集成（当前统一使用 `managed_shim`）。
3. 完成后重新打开 VS Code。

旧版本的 `editor_settings` 接管方式已不再作为产品路径；检测到旧状态时会自动迁移到 `managed_shim`。

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

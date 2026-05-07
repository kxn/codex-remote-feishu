# Codex Remote Feishu

`codex-remote-feishu` 把本机或远端 Linux 上的 Codex 工作现场带到飞书，让你在飞书里接管工作区、切换 thread、继续对话、发图和中断当前 turn。

使用说明 https://my.feishu.cn/docx/PTncdNBf1oS9N5xBikBcGi2enzc

## 快速开始

**无需提前配置飞书应用。** 安装后通过内置 WebSetup 向导即可扫码完成飞书接入——飞书配置是启动后的步骤，不是安装前提。

1. **确认本机已安装 `codex`** — 如果还需要 VS Code 联动，也装上对应扩展
2. **一条命令安装** — 见下方「一条命令安装」，脚本会自动下载、解压、引导安装并启动 daemon
3. **打开 WebSetup** — 安装完成后终端会输出 `/setup` 链接，浏览器打开它
4. **扫码接入飞书** — 在 WebSetup 中按向导完成飞书自建应用的创建、权限配置和扫码绑定
5. **开始使用** — 在飞书机器人聊天中发送 `/menu` 或 `/list` 即可接管工作区、继续或新建对话

整个流程从安装到首次飞书交互通常只需几分钟。

## 功能

- 在飞书里列出可接管工作区，直接继续已有对话或新开会话
- 在统一目标选择卡里直接添加工作区：接入已有本地目录，或导入 Git 仓库
- 需要时切到 VS Code 跟随当前编辑器对话（可选增强，非默认前提）
- 回复当前正在执行的源消息可直接 steer 进本轮执行，也支持 `/steerall`
- 排队中的文字支持用点赞升级成对当前执行的跟进
- 支持暂存图片、`/sendfile` 挑文件发回聊天
- 支持 `/compact` 手动上下文整理
- 查看并临时覆盖当前模型和推理强度
- 最终答复卡显示 token usage、thread usage 和 context left 信息，方便判断上下文余量
- 最终卡正文过长时自动拆分，更长回复不丢内容
- `/history` 历史卡支持分页和单轮详情来回切换
- `/cron` 全套定时任务子命令：`status / list / edit / reload / repair`
- 支持多实例 `systemd --user`，一台机器跑多套环境互不干扰
- 区分系统提示、过程消息和最终回复；最终回复直接回在触发消息下方
- 共享过程卡展示计划更新、工具调用、Web 搜索、命令执行摘要
- 旧卡片、旧按钮和旧命令会明确提示已过期
- 最终回复中的本地 `.md` 和单文件 `.html` 链接可替换成飞书云空间预览

## 一条命令安装

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1 | iex
```

脚本会自动：识别平台 → 下载 GitHub release → 解压到本地缓存 → 安装稳定路径二进制 → 启动 daemon → 输出 WebSetup 链接。

安装指定 track 的最新版本：

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
```

```powershell
# Windows
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Track beta
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Version v1.0.0
```

支持的环境变量：`CODEX_REMOTE_VERSION`、`CODEX_REMOTE_REPO`、`CODEX_REMOTE_BASE_URL`、`CODEX_REMOTE_INSTALL_ROOT`、`CODEX_REMOTE_SKIP_SETUP=1`。

## WebSetup 与按需接入 VS Code

安装器只做 bootstrap：复制稳定二进制、写 `config.json`、启动 daemon 与嵌入式 Web UI。后续全部在 WebSetup 中完成：

- **飞书 App 创建与验证**（扫码接入，无需提前准备）
- **运行环境检查**
- **自动启动配置**
- 按需执行 VS Code 接入（`detect` / `editor_settings` / `managed_shim` / `reinstall-shim`）

建议顺序：先完成飞书接入 → 直接在飞书用 `normal` 模式工作 → 需要时再回页面接入 VS Code。

## 手动安装 release 包

从 [GitHub Releases](https://github.com/kxn/codex-remote-feishu/releases) 下载对应平台归档，运行：

```bash
./codex-remote install -bootstrap-only -start-daemon
```

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

启动后打开输出的 `/setup` 链接。

## Linux 常驻服务

```bash
codex-remote service install-user
codex-remote service enable
codex-remote service start
codex-remote service status
# 如需用户未登录时保持自启：
loginctl enable-linger "$USER"
```

日常升级直接在飞书发送 `/upgrade latest`，daemon 不会后台自动检查。

## 飞书端使用

**模式：** 默认 `normal` 模式以工作区为中心，`vscode` 模式跟随编辑器焦点。

**常用命令：**

| 命令 | 用途 |
|------|------|
| `/list` | 列出可接管工作区 |
| `/use` | 列出最近会话并继续 |
| `/new` | 新开会话 |
| `/history` | 查看当前 thread 历史（支持分页） |
| `/status` | 查看接管状态、队列和模型 |
| `/stop` | 中断当前 turn |
| `/steerall` | 合并排队输入到当前执行 |
| `/compact` | 手动上下文整理 |
| `/detach` | 断开实例接管 |
| `/sendfile` | 从工作区挑文件发回聊天 |
| `/model` | 查看/设置模型覆盖 |
| `/reasoning` | 查看/设置推理强度 |
| `/access` | 查看/设置执行权限（full/confirm/clear） |
| `/verbose` | 控制过程消息详细程度 |
| `/cron` | 定时任务全套操作（status/list/edit/reload/repair） |
| `/upgrade latest` | 升级到当前 track 最新版本 |

## 仓库内联调

```bash
go build -o ./bin/codex-remote ./cmd/codex-remote
./bin/codex-remote install -bootstrap-only -start-daemon
```

Windows 也提供 `./setup.ps1` 辅助脚本。

## 排障

先确认不是代理污染：

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
```

依次检查：

1. `curl --noproxy '*' -sf http://127.0.0.1:9501/api/admin/bootstrap-state | jq .`
2. `curl --noproxy '*' -sf http://127.0.0.1:9501/v1/status | jq .`
3. `config.json` 里的 `relay.serverURL`、`wrapper.codexRealBinary`、飞书凭证
4. `~/.local/share/codex-remote/logs/codex-remote-relayd.log`

## 文档

- [变更记录](./CHANGELOG.md)
- [用户使用说明书](./docs/general/user-guide.md)
- [文档索引](./docs/README.md)
- [架构说明](./docs/general/architecture.md)
- [安装与部署](./docs/general/install-deploy-design.md)
- [开发者说明](./DEVELOPER.md)

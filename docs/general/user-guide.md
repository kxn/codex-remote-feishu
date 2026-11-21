# 飞书使用说明书

> Type: `general`
> Updated: `2026-11-22`
> Summary: 说明项目群与私人工作的入口、工作目录和模型选择、会话控制以及旧入口的迁移边界。

## 1. 项目群和私人工作

项目群里由负责人与你确认要解决的问题、安排其他 Agent 并汇报进展与成果。私聊里可以处理个人工作，也可以在有权限的项目中查看进展并单独讨论。私人讨论仍留在私聊。

## 2. 安装前准备

在实际运行 Agent 的机器上安装并登录你要使用的后端。飞书应用需开通机器人能力、消息权限、事件订阅和卡片回调。权限和模板见 [飞书应用配置说明](../../deploy/feishu/README.md)。

## 3. 安装

### 3.1 安装位置

将服务安装在实际运行 Agent 的机器上。本机和远端主机都需要独立运行 Agent 时，可以分别安装。

### 3.2 原生安装器（Windows / macOS）

如果你更习惯桌面安装器，可以直接从 [GitHub Releases](https://github.com/kxn/codex-remote-feishu/releases) 下载对应平台的 native installer。

#### Windows

下载并运行：

```text
codex-remote-feishu_<version>_windows_amd64_installer.exe
```

首次安装完成后，安装器结果页会出现 **Continue WebSetup**，点击后进入 WebSetup 完成飞书接入。已经安装过时再次运行，会按 repair / 升级处理，不会覆盖已有配置。

#### macOS

下载并打开：

```text
codex-remote-feishu_<version>_darwin_universal_installer.dmg
```

运行 DMG 里的 **Install Codex Remote.app**。首次安装可以选择安装目录；完成后在结果页打开 WebSetup。已经安装过时再次运行，会按 repair / 升级处理。

### 3.3 一条命令安装

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1 | iex
```

安装器会自动：

1. 识别当前平台
2. 下载对应 release 包
3. 安装 `codex-remote`
4. 启动本地后台服务
5. 打开或打印 WebSetup 地址

如果你要装某个 prerelease track，例如 `beta`：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Track beta
```

如果你要固定某个版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Version <version>
```

### 3.4 手动安装 release 包

macOS / Linux：

```bash
./codex-remote install -bootstrap-only -start-daemon
```

Windows PowerShell：

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

安装完成后，打开输出里的 `/setup` 地址继续初始化。

## 4. 首次配置

首次配置主要都在 WebSetup 完成。

推荐顺序：

1. 连接飞书机器人（扫码新建或接入已有应用）
2. 运行环境检查
3. 飞书自动配置与菜单确认
4. 按需配置本机自动启动
5. 完成并进入本地管理页

### 4.1 配置飞书机器人

当前有两条入口：

1. 扫码新建飞书应用
2. 接入已有飞书应用

默认更推荐第一条，因为它会自动帮你处理掉绝大部分容易出错的东西。

扫码新建路径的优点是：

- 会自动创建应用，并把权限、事件、回调预填到扫码确认页
- 接入后 WebSetup 只做只读检查：列出缺失项和影响，并提供可直接粘贴到飞书后台“批量导入/导出权限”的导入 JSON

机器人菜单可按[飞书应用配置说明](../../deploy/feishu/README.md)添加工作入口。配置了 `menu` 后，可以从菜单直接打开私人工作导航。

如果你走的是“已有应用”路径，就按 WebSetup 页面的自动配置结果处理。页面只做只读检查并列出缺失项与影响；缺失权限会生成可直接粘贴到飞书后台的导入 JSON。需要你去飞书后台处理的事项，以页面给出的当前状态和链接为准。

### 4.2 运行环境检查

安装完成后，页面会先检查当前机器的基础运行条件。

这一步主要会告诉你：

- 当前 daemon 会用哪个 `codex-remote` 作为 headless 启动器
- wrapper 实际会调用哪个真实 `codex`
- 这些路径在当前环境里是否真的可用
- 有没有明显风险，比如只靠 `PATH` 找到、或者错误地回指到 `codex-remote` 自己

注意它检查的是“能不能启动起来”，不负责帮你验证 Codex 登录状态、账号配置或 provider 凭据。

### 4.3 自动启动

当前最成熟的是 Linux 下的 `systemd --user` 路径。

这一步不是必须的，但如果你希望机器重启后自己恢复服务，会比较有用。

## 5. 开始工作

### 私聊

发送 `/menu` 打开「和助手一起工作」。选择「新建工作」开始新的讨论，或打开「已有工作」查看和继续已有记录。打开「项目」可查看你有权限的项目。「模型和设置」与「工作目录」分别用于选择模型和授权使用的已有目录。

### 项目群

在要开展项目的群中明确 @ 一位负责人，说明期望的结果、已有资料和约束。负责人可以邀请其他 Agent、协调分工，并向你询问需要决定的事项。项目开始后可以直接在群里向负责人补充或调整要求。

### 工作目录与 Git

选择你允许使用的已有目录或仓库。需要并行 checkout 或 worktree 时，告诉 Agent 工作约束，由 Agent 使用 Git 创建、复用或清理相应目录。私聊的旧目录创建命令会把请求交给当前 Agent 处理，执行结果会在这项工作中回复。

## 6. 指导和查看进展

- 在对应工作中补充要求或回复明确归属的消息。用 `/status` 或项目入口查看当前状态。
- 用菜单中的「暂停」「继续」控制工作。暂停项目会保留尚未完成的项目工作。
- 需要审阅时，可在私聊发起 `/review`。原生工具的待确认事项通过飞书卡片处理。
- 使用 `/sendfile` 从当前工作允许使用的目录发送文件。需要项目成员持续访问的成果，可由团队发布到项目资料中。

## 7. 常用命令

| 目的 | 私聊入口 |
| --- | --- |
| 查看工作入口 | `/menu`、`/help` |
| 开始或选择工作 | `/new`、`/use`、`/useall`、`/history` |
| 设置工作目录及模型 | `/workspace`、`/model`、`/reasoning`、`/access`，可用项取决于后端 |
| 选择后端和配置 | `/mode`、`/codexprofile`、`/claudeprofile`、`/opencodeprofile` |
| 查看状态和控制当前工作 | `/status`、`/stop`、`/steerall` |
| 返回私人工作 | `/detach` |
| 审阅、整理上下文和发送文件 | `/review`、`/compact`、`/sendfile` |
| 设置原生会话目标 | `/goal`，适用于支持目标的 Codex 自动执行会话 |

在支持原生会话目标的当前工作中，用 `/goal` 查看现状，`/goal new --budget 1200 完成当前登录流程` 新建带预算的目标。也可以使用 `/goal edit`、`/goal pause`、`/goal resume` 或 `/goal clear`。会话目标用来推进当前原生会话中的工作。项目方向和验收要求仍由项目团队协商。

## 8. 从旧版本迁移

旧卡片提示入口停用或失效时，回到私聊发送 `/menu`，或在项目群向负责人说明你想完成的操作。旧群的 primary/coworker 配置、Cron 执行和 VS Code 人工接管入口已停用。旧执行记录会按迁移规则保留或隔离。

## 9. 相关文档

- [项目 README](../../README.md)
- [飞书应用配置说明](../../deploy/feishu/README.md)
- [文档索引](../README.md)

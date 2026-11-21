# 快速开始

## 方式一：一条命令安装最新正式版

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

```powershell
irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1 | iex
```

这条命令会自动：

1. 识别当前平台
2. 下载 GitHub 构建好的 release 包
3. 解压到本地 release 缓存目录
4. 安装稳定路径下的 `codex-remote`
5. 启动本地 daemon 并打印 WebSetup 地址

如果你想固定到某个版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Version <version>
```

如果你想安装某个 prerelease track 的最新版本，例如 `beta`：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Track beta
```

## 方式二：下载 release 压缩包

1. 从 GitHub Releases 下载适合你平台的压缩包
2. 解压
3. 执行：

macOS / Linux：

```bash
./codex-remote install -bootstrap-only -start-daemon
```

Windows PowerShell：

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

## 在 WebSetup 里完成首次配置

daemon 启动后，打开命令输出里的 `/setup` 地址。

推荐顺序：

1. 添加或验证飞书应用凭据
2. 看一下这台机器的运行环境检查结果
3. 如有需要，开启自动启动
4. 在飞书私聊发送 `/menu`，打开工作入口。

## Linux `systemd --user` 常驻服务

如果你希望 Linux 上的 daemon 由长期运行的用户服务托管，而不是依赖 detached 进程：

```bash
codex-remote service install-user
codex-remote service enable
codex-remote service start
codex-remote service status
```

如果你希望系统重启后在没有手工打开终端的情况下也能恢复，需要额外执行：

```bash
loginctl enable-linger "$USER"
```

## 升级到当前 track 的最新版本

对于已经接入完成的用户，当前推荐的升级入口统一是：

```text
/upgrade latest
```

在飞书里发送这条命令即可。它适合：

- 检查是否有可用新版本
- 开始升级到当前 track 的最新 release
- 继续上一次未完成的升级

## 开始在飞书里使用

1. 按 [飞书应用配置说明](./deploy/feishu/README.md) 检查消息、事件与卡片回调权限。
2. 私聊机器人，发送 `/menu`。从「模型和设置」确认后端及模型，从「工作目录」选择已有目录，然后新建工作或继续已有工作。
3. 在项目群明确 @ 一位负责人，说明你要解决的问题和期望交付的结果。已建立的项目可以直接在群中向负责人补充要求或查看进展。

更多操作见 [用户使用说明书](./docs/general/user-guide.md)。

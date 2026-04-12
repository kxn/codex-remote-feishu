# 快速开始

## 方式一：macOS / Linux 一条命令安装最新正式版

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

这条命令会自动：

1. 识别当前平台
2. 下载 GitHub 构建好的 release 包
3. 解压到本地 release 缓存目录
4. 安装稳定路径下的 `codex-remote`
5. 启动本地后台服务并打印 WebSetup 地址

如果你想固定到某个版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version <version>
```

如果你明确想提前体验预发布版本，也可以额外指定：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
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

后台服务启动后，打开命令输出里的 `/setup` 地址。

推荐顺序：

1. 添加或验证飞书应用凭据
2. 看一下这台机器的运行环境检查结果
3. 如有需要，开启自动启动
4. 直接开始使用默认 `normal` 模式
5. 只有在你明确需要“飞书跟着编辑器当前焦点走”时，再去处理 VS Code 接入

`v1.5.0` 的默认推荐路径是：先用 `normal` 模式，再按需启用 VS Code 跟随能力。

## Linux `systemd --user` 常驻服务

如果你希望 Linux 上的后台服务由长期运行的用户服务托管，而不是依赖普通后台进程：

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

## 升级到后续新版本

对于已经接入完成的用户，当前推荐的升级入口统一是：

```text
/upgrade latest
```

在飞书里发送这条命令即可。

如果你默认安装的是正式版，这会继续升级到最新正式版；如果你之前装的是 `beta` 或 `alpha`，它也会继续沿着同一条更新路线往前走。

它适合：

- 检查是否有可用新版本
- 开始升级到后续新版本
- 继续上一次未完成的升级

## 开始在飞书里使用

在测试前先确认：

- 飞书应用已经完成基础消息、事件、卡片回调和机器人菜单配置
- 如果你希望本地 `.md` 链接自动变成飞书预览链接，还需要 `drive:drive`

然后在飞书里：

- 默认先走 `normal` 模式：用 `/list` 选工作区，再 `/use` 继续已有会话，或者 `/new` 新开一个干净会话
- 如果只是想快速回到最近对话，直接发 `/use`
- 只有当你明确想让飞书跟着编辑器当前焦点变化时，才切到 `vscode` 模式
- 如果你一时记不住命令，先发 `/help` 或 `/menu`
- `/list` 在 `normal` 模式下显示工作区，在 `vscode` 模式下才显示在线 VS Code 实例
- `/use` 用来继续最近可见会话；`/threads` 仍可作为旧别名使用；`/useall` 会显示全部可见会话
- 优先使用卡片按钮；如果卡片提示已经过期，直接重发对应命令
- 最终回复会回在触发它的那条消息下面，群聊里更容易看懂上下文
- 如果一条文字还在排队，而当前已经有一条回复在执行，可以给这条排队文字点 `ThumbsUp`，把它升级成对当前执行回复的跟进
- `/detach` 会断开当前接管，并取消正在等待的后台恢复
- 如果你需要编辑器跟随行为，使用 `/mode vscode`，然后 `/list`，最后 `/follow`
- 默认执行权限是 `full`；如果你暂时想改成确认模式，可以发送 `/access confirm`

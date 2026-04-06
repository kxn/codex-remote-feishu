# 安装与部署设计

> Type: `general`
> Updated: `2026-04-06`
> Summary: 将 release / 安装 / 部署文档统一更新为 WebSetup-first 发布模型，并明确 GitHub 端构建发布是唯一正式 release 路径。

## 1. 范围

这份文档描述当前 Go 版本的安装、配置和部署模型，覆盖：

- GitHub Release 产物形态
- 在线安装脚本与手动解压安装
- `codex-remote install` 的 bootstrap 语义
- WebSetup / Admin UI 的职责边界
- 仓库 helper 与产品入口的区分
- Docker relayd 部署

## 2. 当前产品入口

### 2.1 `install-release.sh`

在线安装入口，面向最终用户。

职责：

- 解析平台和架构
- 下载 GitHub Releases 中最新或指定版本的平台包
- 解压到本地 release cache
- 执行：

```bash
codex-remote install -bootstrap-only -start-daemon
```

默认缓存目录：

- Linux: `~/.local/share/codex-remote/releases`
- macOS: `~/Library/Application Support/codex-remote/releases`

它必须兼容：

- `curl | bash`
- 指定版本安装
- CI 中通过本地 HTTP server 做 smoke test

### 2.2 手动解压 release 包

release 包解压后，最终用户直接运行统一二进制：

macOS / Linux:

```bash
./codex-remote install -bootstrap-only -start-daemon
```

Windows PowerShell:

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

这一步只负责：

- 安装稳定二进制
- 写入统一配置
- 启动 daemon 与嵌入式 Web UI

后续飞书与 VS Code 配置都在 `/setup` 和 `/` 管理页中完成。

### 2.3 WebSetup / Admin UI

产品配置入口已经收敛到 WebSetup：

- 飞书 App 凭证与多 App 管理
- VS Code detect / apply
- `managed_shim` reinstall
- bootstrap state / runtime state 展示

也就是说，release 安装器不再做这些事情：

- CLI 交互采集飞书 `App ID` / `App Secret`
- CLI 交互采集 VS Code settings/bundle 路径
- 直接在 release 包里分发 `setup.sh` / `setup.ps1` 作为产品入口

### 2.4 仓库 helper

仓库中仍保留两个辅助入口，但它们不再是 release 包产品路径：

- `setup.sh` / `setup.ps1`
  - 源码仓库 helper
  - 默认执行本地构建后再跑 `-bootstrap-only -start-daemon`
- `install.sh`
  - Linux 仓库内运维 / 联调 helper
  - 提供 `bootstrap/start/stop/restart/refresh/status/logs/build`

## 3. `codex-remote install` 的当前语义

### 3.1 默认非交互 bootstrap

当前 release / 在线安装路径使用：

- `-bootstrap-only`
- `-start-daemon`

意义是：

- 写 `config.json`
- 保留旧配置迁移与已有凭证
- 写 `install-state.json`
- 不直接改 VS Code
- 启动 daemon 并输出 WebSetup / Admin URL

### 3.2 仍保留的高级模式

`codex-remote install` 仍然保留旧的完整安装能力，方便仓库联调和特殊场景：

- `-interactive`
- `-integration editor_settings`
- `-integration managed_shim`
- `-integration both`

这些能力主要用于源码仓库或定向调试，不再作为发布包默认路径。

## 4. 默认布局

### 4.1 配置与状态

当前 runtime 仍使用统一布局：

```text
<baseDir>/.config/codex-remote/config.json
<baseDir>/.local/share/codex-remote/install-state.json
<baseDir>/.local/share/codex-remote/logs/codex-remote-relayd.log
<baseDir>/.local/state/codex-remote/codex-remote-relayd.pid
```

默认 `baseDir` 是用户 home 目录。

### 4.2 已安装二进制目录

默认稳定安装目录按平台区分：

- Linux: `~/.local/bin`
- macOS: `~/Library/Application Support/codex-remote/bin`
- Windows: `%LOCALAPPDATA%\\codex-remote\\bin`

release 包中的归档目录只是版本缓存位置，不是长期运行路径。

## 5. VS Code 接管模型

### 5.1 `editor_settings`

修改 VS Code `settings.json`：

- `chatgpt.cliExecutable`

适用：

- 本机桌面 VS Code
- 不方便直接改 bundle 的场景

### 5.2 `managed_shim`

直接接管扩展 bundle 里的 `codex` 入口：

1. 原始 `codex` 重命名为 `codex.real` 或 `codex.real.exe`
2. 把统一二进制 `codex-remote` 复制到原始 `codex` 路径
3. `config.json` 的 `wrapper.codexRealBinary` 自动指向保留的 `codex.real`

适用：

- VS Code Remote
- 希望不依赖 `settings.json` 的场景

### 5.3 当前产品约束

对 release 用户：

- 安装器 bootstrap 完成后，`wrapper.integrationMode` 默认会记录为 `none`
- 真正的 `editor_settings` / `managed_shim` apply 在 WebSetup / Admin UI 中进行

对仓库联调：

- `setup.sh`
- `install.sh bootstrap`
- `codex-remote install -interactive`

仍然可以直接在 CLI 里触发接管。

## 6. release 打包与发布

### 6.1 产物内容

当前 `scripts/release/build-artifacts.sh` 为每个平台构建：

- 一个带版本号的 `codex-remote`
- `README.md`
- `QUICKSTART.md`
- `deploy/`

另外单独生成：

- `codex-remote-feishu-install.sh`
- `checksums.txt`

release 包内不再附带：

- `setup.sh`
- `setup.ps1`
- `install.sh`

### 6.2 构建与发布位置

正式 release 只走 GitHub Actions：

- `Release` workflow 在 GitHub 端构建 admin UI 与多平台二进制
- GitHub 端生成 release notes 和 checksums
- GitHub 端创建并发布 GitHub Release

本地 `make release-artifacts VERSION=...` 仅用于打包预演，不是正式发布路径。

### 6.3 smoke test 要求

release smoke test 必须覆盖真实产品路径：

1. 构建 release 归档
2. 通过本地 HTTP server 模拟 release 下载
3. 执行 `install-release.sh`
4. 确认：
   - 归档内容正确
   - 二进制版本号正确
   - `config.json` / `install-state.json` 被写入
   - daemon 成功启动
   - `/api/setup/bootstrap-state` 可访问

## 7. Docker 模型

Docker 只部署 `codex-remote daemon`。

不放进容器的部分：

- `codex-remote` 的 wrapper role
- 真实 `codex`
- VS Code 扩展 bundle

原因：

- wrapper 必须和 VS Code / Codex 进程在同一侧
- daemon 作为常驻服务，容器化收益更高

当前资产：

- [deploy/docker/Dockerfile](../../deploy/docker/Dockerfile)
- [deploy/docker/compose.yml](../../deploy/docker/compose.yml)
- [deploy/docker/.env.example](../../deploy/docker/.env.example)

## 8. 飞书配置模板

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

## 9. 示例

在线安装：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

固定版本在线安装：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

手动解压后启动 WebSetup：

```bash
./codex-remote install -bootstrap-only -start-daemon
```

仓库联调：

```bash
./setup.sh
./install.sh bootstrap
./install.sh start
```

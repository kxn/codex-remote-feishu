# 安装与部署设计

> Type: `general`
> Updated: `2026-04-11`
> Summary: 增补 Linux systemd user service、本地 binary 升级事务入口、`codex.real` 子进程的定向 provider env 补齐规则，以及 release smoke/test 复用正式产物的当前实现。

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
- 默认下载 GitHub Releases 中最新 `production` 平台包
- 支持显式安装指定版本，或按 `--track production|beta|alpha` 解析该 track 的最新 release
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
- 指定 track 的最新 release 安装
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

仓库中保留的联调入口已经收敛到现有单 binary 路径，它们不再是 release 包产品路径：

- `setup.sh` / `setup.ps1`
  - 源码仓库 helper
  - 默认执行本地构建后再跑 `-bootstrap-only -start-daemon`
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - 已经构建过本地二进制时，可直接重复 bootstrap
- `./bin/codex-remote daemon`
  - 需要前台观察 daemon 启动或日志时使用

仓库中不再保留单独的 `install.sh` 生命周期脚本。

## 3. `codex-remote install` 的当前语义

### 3.1 默认非交互 bootstrap

当前 release / 在线安装路径使用：

- `-bootstrap-only`
- `-start-daemon`

意义是：

- 写 `config.json`
- 保留旧配置迁移与已有凭证
- 写 `install-state.json`
- 写当前安装来源、track、version、稳定入口路径和版本缓存根目录
- 不直接改 VS Code
- 启动 daemon 并输出 WebSetup / Admin URL

### 3.2 仍保留的高级模式

`codex-remote install` 仍然保留旧的完整安装能力，方便仓库联调和特殊场景：

- `-interactive`
- `-integration managed_shim`
- 兼容旧脚本时，`-integration editor_settings|both|all` 仍可被接受，但当前都会归一化到 `managed_shim`

这些能力主要用于源码仓库或定向调试，不再作为发布包默认路径。

## 4. 默认布局

### 4.1 配置与状态

当前 runtime 仍使用统一布局：

```text
<baseDir>/.config/codex-remote/config.json
<baseDir>/.local/share/codex-remote/install-state.json
<baseDir>/.local/share/codex-remote/releases/
<baseDir>/.local/share/codex-remote/logs/codex-remote-relayd.log
<baseDir>/.local/state/codex-remote/codex-remote-relayd.pid
```

默认 `baseDir` 是用户 home 目录。

### 4.3 Linux `systemd --user` 管理模式

Linux 当前已支持显式选择 `systemd_user` 作为 daemon lifecycle manager。

当前产品语义：

- `detached`
  - 仍保留为默认兼容模式
- `systemd_user`
  - 由 `codex-remote service install-user|enable|start|stop|restart|status` 管理
  - unit 安装到 `~/.config/systemd/user/codex-remote.service`
  - 运行身份仍保持为当前用户
  - 继续使用当前 XDG config/data/state 目录

如果希望机器重启后在没有手工打开终端的情况下也恢复 user service，需要额外启用：

```bash
loginctl enable-linger "$USER"
```

### 4.4 统一升级入口

当前产品已经把升级入口统一为 daemon 内置事务：

- release 升级
  - 用户发送 `/upgrade latest`
  - daemon 按当前 track 检查或继续升级到最新 release
- 本地编译产物升级
  - 用户先把新编译的 binary 放到固定 artifact 路径
  - 再发送 `/upgrade local`

本地 artifact 路径按当前 install-state 推导，默认位于：

```text
<stateDir>/local-upgrade/codex-remote
```

Windows 下文件名为 `codex-remote.exe`。

统一事务行为：

- 把目标 binary 准备到 `versionsRoot/<slot>/`
- 写入 `PendingUpgrade` 与 rollback candidate
- daemon 复制当前 live binary 作为 `upgrade-helper`
- 在 `systemd_user` 模式下，通过独立 transient unit 启动 helper，避免 stop 旧服务时把 helper 一并杀掉
- helper 负责 stop old service -> switch stable binary -> start new service -> observe health
- 新版本启动或健康检查失败时，自动回滚 binary 和 live config

### 4.2 已安装二进制目录

默认稳定安装目录按平台区分：

- Linux: `~/.local/bin`
- macOS: `~/Library/Application Support/codex-remote/bin`
- Windows: `%LOCALAPPDATA%\\codex-remote\\bin`

release 包中的归档目录只是版本缓存位置，不是长期运行路径。

当前 `install-state.json` 还会记录升级所需的最小基线：

- `installSource`
- `currentTrack`
- `currentVersion`
- `currentBinaryPath`
- `versionsRoot`
- `currentSlot`

其中：

- release 安装默认把 `installSource` 记为 `release`
- 源码仓库 / 本地构建路径默认把 `installSource` 记为 `repo`
- repo 来源默认按 `alpha` track 语义记录
- 运行期自动升级由隐藏 `upgrade-helper` 角色执行：
  - daemon 只负责检查、提示、落 journal 和启动 helper
  - helper 负责停当前 daemon、切换稳定入口、观察健康并在失败时自动回滚
  - daemon 在停机窗口里会先进入 shutdown gate，停止自动补拉 headless / wrapper
  - daemon 会向当前在线 wrapper 广播 `process.exit`，最多等待约 3 秒；仍未退出的实例按 PID 强制结束，避免升级切 stable entry 时命中 `ETXTBSY`

## 5. VS Code 接管模型

### 5.1 当前产品策略

当前产品已经收敛到 `managed_shim` 单一路径：

1. WebSetup / Admin UI / daemon 内部迁移卡片都只会执行 `managed_shim`。
2. 执行 `managed_shim` apply/reinstall 时，会同时清理旧的 `chatgpt.cliExecutable`，避免 host 侧 `settings.json` override 继续污染 Remote SSH。
3. 若检测到存量 `editor_settings` 状态，或扩展升级导致 managed shim 失效，系统会提示用户迁移/重新接入，而不是继续把 `editor_settings` 当成可选策略。

### 5.2 `managed_shim`

直接接管扩展 bundle 里的 `codex` 入口：

1. 原始 `codex` 重命名为 `codex.real` 或 `codex.real.exe`
2. 把统一二进制 `codex-remote` 复制到原始 `codex` 路径
3. `config.json` 的 `wrapper.codexRealBinary` 自动指向保留的 `codex.real`

适用：

- 当前机器本地 VS Code
- VS Code Remote
- 希望不依赖 `settings.json` 的场景

当前 wrapper / headless 在真正拉起 `codex.real` 前，还会补一条稳定规则：

- wrapper 自己仍按本地 relay 通信要求清理代理环境
- 启动 `codex.real` 时会恢复已捕获的 proxy env
- 若当前 active provider 的 `env_key` 不在父进程环境中，会按同一套 child-env 规则定向补齐这个 key，而不是整包导入 shell 环境

### 5.3 legacy `settings.json` 迁移

旧版本可能还会留下：

- VS Code `settings.json` 中的 `chatgpt.cliExecutable`
- `wrapper.integrationMode=editor_settings`

当前处理方式：

1. detect 仍会识别这些 legacy 状态。
2. 一旦用户执行迁移或重新接入，系统会：
   - patch 最新检测到的扩展入口
   - 更新 install-state / config
   - 清掉旧的 `chatgpt.cliExecutable`
3. 迁移完成后，不再保留 `editor_settings` 作为产品可选路径。

### 5.4 当前产品约束

对 release 用户：

- 安装器 bootstrap 完成后，`wrapper.integrationMode` 默认会记录为 `none`
- 真正的 VS Code 接入统一在 WebSetup / Admin UI / daemon 迁移卡片中执行 `managed_shim`

对仓库联调：

- `setup.sh`
- `./bin/codex-remote install -bootstrap-only -start-daemon`
- `codex-remote install -interactive`

仍然可以直接在 CLI 里触发接管，但 legacy `editor_settings` 形态只保留兼容解析，不再作为推荐输出。

## 6. release 打包与发布

### 6.1 产物内容

当前 `scripts/release/build-artifacts.sh` 为每个平台构建：

- 一个带版本号的 `codex-remote`
- `QUICKSTART.md`
- `CHANGELOG.md`

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
- workflow 显式区分 `production / beta / alpha` 三条 track
- `beta / alpha` 由 track 自动映射到 GitHub `prerelease=true`
- workflow 会先算出本次发布版本，再构建正式 release 产物
- GitHub 端生成 release notes 和 checksums
- release notes 优先引用 `CHANGELOG.md` 中当前版本的人类整理摘要，再附带按提交分组的明细
- GitHub 端创建并发布 GitHub Release

本地 `make release-artifacts VERSION=...` 仅用于打包预演，不是正式发布路径。

### 6.3 smoke test 要求

release smoke test 必须覆盖真实产品路径：

1. 复用当前 workflow 已构建好的正式 release 归档
2. 通过本地 HTTP server 模拟 release 下载
3. 执行 `install-release.sh`
4. 确认：
   - 归档内容正确
   - 二进制版本号正确
   - `config.json` / `install-state.json` 被写入
   - daemon 成功启动
   - `/api/setup/bootstrap-state` 可访问

当前 smoke 的额外约束：

- 正式 release 归档只构建一次，不在 smoke 里重复全量打包
- 若 smoke 还要验证 `--track beta|alpha` 的 release API 解析，只补一份“当前 runner 平台”的轻量 fixture，而不是再做一轮全平台构建

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

安装最新 beta track：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
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
./bin/codex-remote install -bootstrap-only -start-daemon
./bin/codex-remote daemon
```

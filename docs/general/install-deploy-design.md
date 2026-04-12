# 安装与部署设计

> Type: `general`
> Updated: `2026-04-13`
> Summary: 补充全局 stable/beta/master 实例与 workspace 绑定模型，并同步 build flavor（shipping/dev）能力边界、`/upgrade track` 入口语义、`managed_shim` tiny shim + sidecar 绑定模型与按当前平台筛选 VS Code 入口的规则。

## 1. 范围

这份文档描述当前 Go 版本的安装、配置和部署模型，覆盖：

- GitHub Release 产物形态
- 在线安装脚本与手动解压安装
- `codex-remote install` 的 bootstrap 语义
- WebSetup / Admin UI 的职责边界
- 仓库 helper 与产品入口的区分

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

- `setup.ps1`
  - Windows 上的源码仓库 helper
  - 默认执行本地构建后再跑 `-bootstrap-only -start-daemon`
- `go build -o ./bin/codex-remote ./cmd/codex-remote`
  - Linux / macOS 上先构建本地二进制
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - 已经构建过本地二进制时，可直接重复 bootstrap
- `./bin/codex-remote daemon`
  - 需要前台观察 daemon 启动或日志时使用

仓库中不再保留单独的 `install.sh` 生命周期脚本。

### 2.5 不再支持 Docker 部署

当前产品不再提供 Docker 部署模型。

原因不是二进制无法容器化，而是这类场景下对任意文件和目录访问的配置体验很差；与此同时，当前实现已经收敛为 Go 单二进制，直接本机安装和运行的复杂度已经足够低，继续维护 Docker 入口的收益不高。

## 3. `codex-remote install` 的当前语义

### 3.1 默认非交互 bootstrap

当前 release / 在线安装路径使用：

- `-bootstrap-only`
- `-start-daemon`

意义是：

- 写 `config.json`
- 保留已有 `config.json` 中的凭证与显式配置
- 不再自动读取或迁移 `config.env` / `wrapper.env` / `services.env`
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

### 3.3 build flavor 与能力边界

当前构建元数据额外引入了 `build flavor`，用于和 release track 解耦：

- `shipping`
  - release workflow 的默认构建 flavor
- `dev`
  - 源码仓库本地构建的默认 flavor

当前由统一策略决定“这个构建能暴露什么能力”，而不是把能力边界硬编码在 track 逻辑里。

当前策略基线：

- shipping
  - 允许切换的 release track 只有 `beta`、`production`
  - 新安装默认/回退 track 收敛到 `production`
  - 不暴露本地 binary 升级入口（`/upgrade local`）
  - `pprof` 保留但默认关闭
- dev
  - 允许 track：`alpha`、`beta`、`production`
  - 保留本地 binary 升级入口（`/upgrade local`）
  - `pprof` 默认开启

`/upgrade track`、帮助文案和卡片入口都会读取这套策略；旧 `/debug track` 仅作为兼容别名保留。

## 4. 默认布局

### 4.1 配置与状态

当前 runtime 采用“默认 stable + 命名实例 namespaced layout”：

```text
stable:
<baseDir>/.config/codex-remote/config.json
<baseDir>/.local/share/codex-remote/install-state.json
<baseDir>/.local/share/codex-remote/releases/
<baseDir>/.local/share/codex-remote/logs/codex-remote-relayd.log
<baseDir>/.local/state/codex-remote/codex-remote-relayd.pid

named instance <instanceId>:
<baseDir>/.config/codex-remote-<instanceId>/codex-remote/config.json
<baseDir>/.local/share/codex-remote-<instanceId>/codex-remote/install-state.json
<baseDir>/.local/share/codex-remote-<instanceId>/codex-remote/releases/
<baseDir>/.local/share/codex-remote-<instanceId>/codex-remote/logs/codex-remote-relayd.log
<baseDir>/.local/state/codex-remote-<instanceId>/codex-remote/codex-remote-relayd.pid
```

默认 `baseDir` 是用户 home 目录。

### 4.2 Workspace 绑定与全局实例

源码仓库联调不再默认在“stable 已存在”时自动派生 `repo-xxxx` 实例。

当前模型收敛为：

- 机器级长期实例由显式命名的全局实例承担，例如 `stable` / `beta` / `master`
- 每个 workspace 只记录“我绑定哪套全局实例”和对应 `baseDir`
- 仓库内的 `install` / `service` / `local-upgrade` / `upgrade-local.sh` 默认先读 workspace 绑定
- 若当前 workspace 没有绑定，则默认退回 `stable`；退回前会优先向上查找 repo 祖先目录里已经存在的 stable install/config，再回退到用户 home

当前 repo-local 绑定文件为：

```text
<repoRoot>/.codex-remote/install-target.json
```

当前会记录：

- `instanceId`
- `baseDir`
- `installBinDir`
- `configPath`
- `statePath`
- `logPath`
- `serviceName`
- `serviceUnitPath`

兼容旧路径时，仍会同步保留一份仅含实例 id 的：

```text
<repoRoot>/.codex-remote/install-instance
```

### 4.3 已安装二进制目录

默认稳定安装目录按平台区分：

- Linux: `~/.local/share/codex-remote/bin`
- macOS: `~/Library/Application Support/codex-remote/bin`
- Windows: `%LOCALAPPDATA%\\codex-remote\\bin`

命名实例默认安装到 namespaced data 目录：

- Linux: `<baseDir>/.local/share/codex-remote-<instanceId>/bin`
- macOS: `<baseDir>/Library/Application Support/codex-remote-<instanceId>/bin`
- Windows: `<baseDir>/AppData/Local/codex-remote-<instanceId>/bin`

如果目标 `install-state.json` 已经存在，则 `codex-remote install` 在未显式传 `-install-bin-dir` 时会优先复用现有 `installedBinary` 所在目录，而不是擅自迁移稳定入口。

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

### 4.4 Linux `systemd --user` 管理模式

Linux 当前已支持显式选择 `systemd_user` 作为 daemon lifecycle manager。

当前产品语义：

- `detached`
  - 仍保留为默认兼容模式
- `systemd_user`
  - 由 `codex-remote service install-user|enable|start|stop|restart|status` 管理
  - stable unit 为 `<serviceHome>/.config/systemd/user/codex-remote.service`
  - 命名实例 unit 为 `<serviceHome>/.config/systemd/user/codex-remote-<instanceId>.service`
  - 这里的 `serviceHome` 指真实 `systemd --user` home，通常仍是 `$HOME`；它不等于 install `baseDir`
  - 运行身份仍保持为当前用户
  - unit 里的 `WorkingDirectory` 与 `XDG_CONFIG_HOME` / `XDG_DATA_HOME` / `XDG_STATE_HOME` 仍指向目标实例自己的 `baseDir`

如果希望机器重启后在没有手工打开终端的情况下也恢复 user service，需要额外启用：

```bash
loginctl enable-linger "$USER"
```

### 4.5 统一升级入口

当前产品已经把升级入口统一为 daemon 内置事务：

- release 升级
  - 用户发送 `/upgrade latest`
  - daemon 按当前 track 检查或继续升级到最新 release
  - 用户发送 `/upgrade track` 或 `/upgrade track <track>` 可查看/切换升级渠道
  - track 可选范围由 build flavor 策略决定（旧 `/debug track` 仅保留兼容）
  - daemon 不再后台自动检查 GitHub release，也不再主动弹出升级提示卡
- 本地编译产物升级
  - 用户先把新编译的 binary 放到固定 artifact 路径
  - 再发送 `/upgrade local`（当前只在允许本地升级的 flavor 下开放）

本地 artifact 路径按当前 install-state 推导，默认位于：

```text
<stateDir>/local-upgrade/codex-remote
```

Windows 下文件名为 `codex-remote.exe`。

源码仓库 helper `./upgrade-local.sh` 现在也遵循同一套 workspace 绑定解析：

- 优先读取 `.codex-remote/install-target.json`
- 没有 binding 时，优先向上查找现有全局实例的 `install-state.json` / `config.json`
- 解析完成后，再把当前 repo 构建产物复制到对应实例的固定 local-upgrade artifact 路径

统一事务行为：

- 把目标 binary 准备到 `versionsRoot/<slot>/`
- 写入 `PendingUpgrade` 与 rollback candidate
- daemon 复制当前 live binary 作为 `upgrade-helper`
- 在 `systemd_user` 模式下，通过独立 transient unit 启动 helper，避免 stop 旧服务时把 helper 一并杀掉
- helper 负责 stop old service -> switch stable binary -> start new service -> observe health
- 新版本启动或健康检查失败时，自动回滚 binary 和 live config

本地自升级链路的完整时序、helper 来源、路径布局和回滚细节，单独见：

- [local-self-upgrade-flow.md](./local-self-upgrade-flow.md)

## 5. VS Code 接管模型

### 5.1 当前产品策略

当前产品已经收敛到 `managed_shim` 单一路径：

1. WebSetup / Admin UI / daemon 内部迁移卡片都只会执行 `managed_shim`。
2. 执行 `managed_shim` apply/reinstall 时，会同时清理旧的 `chatgpt.cliExecutable`，避免 host 侧 `settings.json` override 继续污染 Remote SSH。
3. 若检测到存量 `editor_settings` 状态，或扩展升级导致 managed shim 失效，系统会提示用户迁移/重新接入，而不是继续把 `editor_settings` 当成可选策略。

### 5.2 `managed_shim`

当前 `managed_shim` 已从“复制主 binary 到扩展入口”收敛为“tiny shim + sidecar 绑定”：

1. 原始 `codex` 重命名为 `codex.real` 或 `codex.real.exe`
2. 在原始入口路径写入独立 tiny shim（不再复制整份 `codex-remote`）
3. 在入口旁写入 sidecar 绑定配置，记录该入口对应的 install target / state/config 定位信息
4. 运行时由 tiny shim 读取 sidecar，再解析 install-state / config，定位该实例当前可用的 `codex-remote` 并 `exec`
5. 若 sidecar 或目标安装失效，shim 会回退执行同目录 `codex.real`，避免 VS Code 入口直接不可用

detect/apply/reinstall 的当前规则也同步收紧：

- 每个扩展版本只按当前主机平台选择唯一入口（例如 Windows 只看 `windows-*`，不会误 patch `linux-*`）
- 不再只处理“最新入口”，会一并处理当前实例已知的历史 repo-managed 入口
- 若 probe 显示 live daemon 与当前 shim 版本不兼容或 fingerprint 不匹配，runtime manager 会拒绝替换，避免 stale shim 误停健康实例

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
- 历史扩展目录中的 repo-managed copied shim

当前处理方式：

1. detect 仍会识别这些 legacy 状态。
2. 一旦用户执行迁移或重新接入，系统会：
   - 按当前平台 patch 目标入口，并把当前实例已知历史 repo-managed 入口统一迁到 tiny shim + sidecar 绑定模型
   - 更新 install-state / config / sidecar 绑定信息
   - 清掉旧的 `chatgpt.cliExecutable`
3. 迁移完成后，不再保留 `editor_settings` 作为产品可选路径。

### 5.4 当前产品约束

对 release 用户：

- 安装器 bootstrap 完成后，`wrapper.integrationMode` 默认会记录为 `none`
- 真正的 VS Code 接入统一在 WebSetup / Admin UI / daemon 迁移卡片中执行 `managed_shim`

对仓库联调：

- `go build -o ./bin/codex-remote ./cmd/codex-remote`
- `./bin/codex-remote install -bootstrap-only -start-daemon`
- `codex-remote install -interactive`

仍然可以直接在 CLI 里触发接管，但 legacy `editor_settings` 形态只保留兼容解析，不再作为推荐输出。

## 6. release 打包与发布

### 6.1 产物内容

当前 `scripts/release/build-artifacts.sh` 为每个平台构建：

- 一个带版本号的 `codex-remote`
- `README.md`
- `QUICKSTART.md`
- `CHANGELOG.md`
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
go build -o ./bin/codex-remote ./cmd/codex-remote
./bin/codex-remote install -bootstrap-only -start-daemon
./bin/codex-remote daemon
```

# 安装与部署设计

> Type: `general`
> Updated: `2026-05-13`
> Summary: 同步 macOS native packaged installer 的当前 contract、GUI shell / dmg 脚本入口，以及其与 shared packaged-install contract、release workflow 的现阶段边界。

## 1. 范围

这份文档描述当前 Go 版本的安装、配置和部署模型，覆盖：

- GitHub Release 产物形态
- 在线安装脚本与手动解压安装
- `codex-remote install` 的 bootstrap 语义
- `codex-remote packaged-install` 的 shared contract
- WebSetup / Admin UI 的职责边界
- 仓库 helper 与产品入口的区分

## 2. 当前产品入口

### 2.1 `install-release.sh` / `install-release.ps1`

在线安装入口，面向最终用户。

职责：

- 解析平台和架构
- 默认下载 GitHub Releases 中最新 `production` 平台包
- 支持显式安装指定版本，或按 `production|beta|alpha` track 解析该 track 的最新 release
- 解压到本地 release cache
- 执行统一的：

```bash
codex-remote install -bootstrap-only -start-daemon
```

默认缓存目录：

- Linux: `~/.local/share/codex-remote/releases`
- macOS: `~/Library/Application Support/codex-remote/releases`
- Windows: `%LOCALAPPDATA%\codex-remote\releases`

它必须兼容：

- `curl | bash`
- `irm | iex`
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

这些能力主要用于源码仓库或定向调试，不再作为发布包默认路径。

### 3.3 `codex-remote packaged-install` shared contract

当前仓库已经新增内部 shared contract：

```bash
codex-remote packaged-install [flags]
```

它不是当前对最终用户直接暴露的主入口，而是后续 macOS / Windows native packaged installer 统一要调用的 install 内核接口。

当前语义：

- first install
  - 先把 packaged payload 导入 `versionsRoot/currentSlot`
  - 再复用现有 bootstrap 主链
  - 启动 daemon
  - 返回 WebSetup / Admin URL 与日志定位信息
- existing install
  - 先把新 payload staging 到 `versionsRoot/currentSlot`
  - 再按现有 `install-state` 定位 live install
  - 停掉旧 daemon / service
  - 覆盖当前 live binary
  - 启动新 daemon / service
  - 返回明确成功/失败结果与日志定位信息

这个路径的产品定位是：

- 它是显式的“手动重装 / repair”路径
- 它不等同于后台事务升级
- 它不承诺复用 `upgrade-helper` 的事务回滚语义
- 当 `/upgrade` 一类后台升级失败时，用户下载最新 packaged installer 重装一次，应当成为最后一条可理解的兜底修复路径

因此后续 native packaged installer 的平台包装层应尽量保持很薄：

- 不自己猜 first install / existing install
- 不自己散写版本目录布局
- 不自己直接拼平台 stop/start/ready 逻辑
- 统一把这些语义收在 `internal/app/install/**`

当前已确认的首期平台形态：

- Windows
  - 首期要求交付 `NSIS installer`
  - wrapper 应携带 release payload `codex-remote.exe`，并调用 `codex-remote packaged-install`
  - wrapper 不应直接解析长 stdout；应通过 `-result-file` 一类 machine-readable 结果文件读取 `setupURL/adminURL/logPath/error`
- macOS
  - 首期形态已收口为 `dmg + Install Codex Remote.app`
  - installer app 自身是 universal，可同时在 Intel / Apple Silicon 上原生运行
  - app bundle 的 `Contents/Resources/payload/` 内同时携带：
    - `codex-remote-darwin-amd64`
    - `codex-remote-darwin-arm64`
  - app 运行时根据目标机架构自动选择 payload，并调用：
    - `codex-remote packaged-install-probe`
    - `codex-remote packaged-install`
  - GUI 不得自行猜测 first-install / repair / same-version repair，必须先使用 `packaged-install-probe` 拿到稳定状态
  - first-install 允许选择安装目录；repair / upgrade 必须锁定当前 live binary 目录
  - 与 Windows 一样，同版本重复运行 installer 也必须允许，语义视为 repair
  - GUI 当前最小流程固定为：
    1. Welcome / 环境探测
    2. Install Location
    3. Installing（展示 stdout 过程日志）
    4. Finished / Error
  - 成功页优先展示：
    - 已安装版本
    - 日志路径
    - `setupURL` / `adminURL`
  - 失败页优先展示：
    - 结果文件中的 `error`
    - 结果日志路径
    - 过程 stdout/stderr

当前 macOS 平台包装层的工程入口已经固定为：

- `deploy/macos/InstallerApp/`
  - plain Swift AppKit GUI shell
- `scripts/release/build-macos-packaged-installer.sh`
  - 面向 workflow / CI 的顶层入口
  - 统一调用 `.app` 组装与 `dmg` 生成，并在落到 dist 根目录时刷新 `checksums.txt`
- `scripts/release/build-macos-installer-app.sh`
  - 从 release tarball 中提取双架构 payload
  - 编译两份 GUI binary 并 `lipo` 成 universal app executable
  - 组装 `Install Codex Remote.app`
- `scripts/release/build-macos-dmg.sh`
  - 基于生成好的 `.app` 产出最终分发 `dmg`

现阶段边界：

- macOS installer 的 GUI shell 与本地打包脚本已经存在
- 这套产物 contract 还没有并入正式 GitHub Release workflow；后续由 `#652` 收口
- 当前非 macOS runner 只能做静态检查，不能在 Linux runner 上直接编译 AppKit GUI

### 3.4 build flavor 与能力边界

当前构建元数据额外引入了 `build flavor`，用于和 release track 解耦：

- `shipping`
  - beta / production release workflow 的构建 flavor
- `alpha`
  - alpha release workflow 的构建 flavor
- `dev`
  - 源码仓库本地构建与 `dev-latest` workflow 的默认 flavor

当前由统一策略决定“这个构建能暴露什么能力”，而不是把能力边界硬编码在 track 逻辑里。

当前策略基线：

- shipping
  - 允许切换的 release track 只有 `beta`、`production`
  - 新安装默认/回退 track 收敛到 `production`
  - 不暴露滚动开发构建升级入口（`/upgrade dev`）
  - 不暴露本地 binary 升级入口（`/upgrade local`）
  - `pprof` 保留但默认关闭
- alpha
  - 允许 track：`alpha`、`beta`、`production`
  - 暴露滚动开发构建升级入口（`/upgrade dev`）
  - 不暴露本地 binary 升级入口（`/upgrade local`）
  - `pprof` 保留但默认关闭
- dev
  - 允许 track：`alpha`、`beta`、`production`
  - 暴露滚动开发构建升级入口（`/upgrade dev`）
  - 保留本地 binary 升级入口（`/upgrade local`）
  - `pprof` 默认开启

额外约束：

- `production|beta|alpha` 仍然只表示 GitHub semver release track。
- `/upgrade dev` 是单独的“滚动开发构建源”命令，不属于 `track` 子空间。
- `dev-latest` 由固定 GitHub prerelease + `dev-latest.json` manifest 提供，客户端按 manifest 解析当前平台资产并做 checksum 校验。
- 当前 `dev-latest` workflow 产出的 binary 使用 `dev` flavor；它和源码仓库直接构建出的默认能力边界保持一致，但产物来源仍是受支持 push 分支成功构建后的公开测试构建。

`/upgrade track`、帮助文案和卡片入口都会读取这套策略。`/upgrade dev` 则始终是显式命令入口，不跟随 track 按钮一起大面积曝光。

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

### 4.2 Repo 绑定与全局实例

源码仓库联调不再默认在“stable 已存在”时自动派生 `repo-xxxx` 实例。

当前模型收敛为：

- 机器级长期实例由显式命名的全局实例承担，例如 `stable` / `beta` / `master`
- 每个 workspace 只记录“我绑定哪套全局实例”和对应 `baseDir`
- 仓库内的 `install` / `service` / `local-upgrade` / `upgrade-local.sh` 默认先读 repo binding
- 若当前 workspace 没有绑定，则默认退回 `stable`；退回前会优先向上查找 repo 祖先目录里已经存在的 stable install/config，再回退到用户 home

这里的 workspace / repo 绑定只定义 repo helper 的 install target。

它不应该被理解成：

- 当前 daemon 的 self 身份
- 通用 debug / log / bug 排查的默认目标

当前 daemon 的报 bug、查日志、看状态默认都应该查 self target；只有在用户显式点名 repo 绑定目标或某个实例时，才跨到对应 install target。

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
  - daemon 会向当前在线的 managed headless wrapper 广播 `process.exit`，最多等待约 3 秒；仍未退出的实例按 PID 强制结束，避免升级切 stable entry 时命中 `ETXTBSY`
  - `source=vscode` 且 `managed=false` 的 VS Code 侧连接不会在 daemon shutdown 阶段被主动 `process.exit`

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
  - track 可选范围由 build flavor 策略决定
  - daemon 不再后台自动检查 GitHub release，也不再主动弹出升级提示卡
- 滚动开发构建升级
  - 用户发送 `/upgrade dev`
  - daemon 读取固定 `dev-latest.json`
  - 按当前平台选择 `dev-latest` 的公开 release asset，并做 checksum 校验
  - `dev` 不会改写当前 `track`；它只是一次显式的升级源选择
- 本地编译产物升级
  - 用户先把新编译的 binary 放到固定 artifact 路径
  - 再发送 `/upgrade local`（当前只在允许本地升级的 flavor 下开放）

本地 artifact 路径按当前 install-state 推导，默认位于：

```text
<stateDir>/local-upgrade/codex-remote
```

Windows 下文件名为 `codex-remote.exe`。

源码仓库 helper `./upgrade-local.sh` 现在也遵循同一套 repo install target 解析：

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
3. 若检测到存量 `editor_settings` 状态且存在可接管入口，系统会默认静默自动迁到 `managed_shim`；迁移成功时不再额外弹显式迁移提示卡。
4. 若缺少可接管入口、自动迁移失败，或扩展升级导致 managed shim 失效，系统才保留必要的失败/修复反馈，而不是继续把 `editor_settings` 当成可选策略。

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
2. 若 detect 时已经存在可接管的扩展入口，daemon 默认会直接静默尝试迁到 `managed_shim`，不再先弹“确认迁移”主提示卡。
3. 自动迁移或用户显式重试时，系统会：
   - 按当前平台 patch 目标入口，并把当前实例已知历史 repo-managed 入口统一迁到 tiny shim + sidecar 绑定模型
   - 更新 install-state / config / sidecar 绑定信息
   - 清掉旧的 `chatgpt.cliExecutable`
4. 若缺少 target、自动迁移失败，或迁移后状态检查仍异常，才保留必要的失败/重试反馈。
5. 迁移完成后，不再保留 `editor_settings` 作为产品可选路径。

### 5.4 当前产品约束

对 release 用户：

- 安装器 bootstrap 完成后，`wrapper.integrationMode` 默认会记录为 `none`
- 真正的 VS Code 接入统一在 WebSetup / Admin UI / daemon 迁移卡片中执行 `managed_shim`

对仓库联调：

- `go build -o ./bin/codex-remote ./cmd/codex-remote`
- `./bin/codex-remote install -bootstrap-only -start-daemon`
- `codex-remote install -interactive`

仍然可以直接在 CLI 里触发接管，但当前只保留 `managed_shim` 这一条接入路径。

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
- `codex-remote-feishu-install.ps1`
- `codex-remote-feishu_<version>_windows_amd64_installer.exe`
- `codex-remote-feishu_<version>_darwin_universal_installer.dmg`
- `checksums.txt`

macOS packaged installer 由本地脚本 contract 统一构建：

- `scripts/release/build-macos-installer-app.sh`
- `scripts/release/build-macos-dmg.sh`
- `scripts/release/build-macos-packaged-installer.sh`

它们要求在 mac runner 上执行，并复用已经构建好的：

- `codex-remote-feishu_<version>_darwin_amd64.tar.gz`
- `codex-remote-feishu_<version>_darwin_arm64.tar.gz`

release 包内不再附带：

- `setup.sh`
- `setup.ps1`
- `install.sh`

### 6.2 构建与发布位置

正式 release 只走 GitHub Actions：

- `Release` workflow 在 GitHub 端构建 admin UI 与多平台二进制
- `Release` workflow 在现有 Windows zip 归档构建完成后，额外安装 `NSIS` 并生成 `codex-remote-feishu_<version>_windows_amd64_installer.exe`
- `Release` workflow 额外在 mac runner 上复用 `scripts/release/build-macos-packaged-installer.sh`，生成 `codex-remote-feishu_<version>_darwin_universal_installer.dmg`
- workflow 显式区分 `production / beta / alpha` 三条 track
- `beta / alpha` 由 track 自动映射到 GitHub `prerelease=true`
- workflow 会先算出本次发布版本，再构建正式 release 产物
- GitHub 端生成 release notes，并在追加 packaged installer 后刷新 `checksums.txt`
- release notes 优先引用 `CHANGELOG.md` 中当前版本的人类整理摘要，再附带按提交分组的明细
- GitHub 端创建并发布 GitHub Release

滚动开发构建另外走独立的 `Dev Release` workflow：

- 触发条件是 `master` 的 CI 全部 job 成功后直接进入发布步骤，或手动指定 ref
- 固定更新同一条 `dev-latest` prerelease
- 不重复跑正式 release 那套 smoke / 发布校验
- 只覆盖 asset / manifest，不再持续新增一串 `alpha.N`
- 公开契约固定为：
  - `dev-latest.json`
  - `checksums.txt`
  - `codex-remote-feishu_dev_<goos>_<goarch>.tar.gz|zip`
- 同时额外暴露供实验下载的 packaged installer 资产：
  - `codex-remote-feishu_dev_windows_amd64_installer.exe`
  - `codex-remote-feishu_dev_darwin_universal_installer.dmg`
- `dev-latest.json` 继续只描述 archive 资产，不把 packaged installer 拉进 `/upgrade dev` 的 manifest 选择合同

本地 `make release-artifacts VERSION=...` 仅用于打包预演，不是正式发布路径。

### 6.3 smoke test 要求

release / installer smoke test 必须覆盖真实产品路径：

1. 复用当前 workflow 已构建好的正式 release 归档
2. 通过本地 HTTP server 模拟 release 下载
3. 在对应平台执行在线安装脚本
4. 确认：
   - 归档内容正确
   - 二进制版本号正确
   - `config.json` / `install-state.json` 被写入
   - daemon 成功启动
   - `/api/setup/bootstrap-state` 可访问

当前 smoke 的额外约束：

- 正式 release 归档只构建一次，不在 smoke 里重复全量打包
- 若 smoke 还要验证 `--track beta|alpha` 的 release API 解析，只补一份“当前 runner 平台”的轻量 fixture，而不是再做一轮全平台构建

macOS packaged installer 的额外验证要求：

1. 在 mac runner 上先构建双架构 release tarball。
2. 优先通过 `scripts/release/build-macos-packaged-installer.sh` 统一产出最终 `dmg`。
3. 如需排查 bundle 组装细节，再单独调用：
   - `scripts/release/build-macos-installer-app.sh`
   - `scripts/release/build-macos-dmg.sh`
4. 至少验证三条用户路径：
   - first install
   - 已安装版本升级
   - 同版本重复运行触发 repair
5. 验证 GUI 行为与 shared contract 一致：
   - first-install 可选安装目录
   - repair / upgrade 不可改 live binary 目录
   - 失败页能看到 `error` 和日志路径
   - `setupRequired=true` 时会给出 WebSetup 打开入口

当前 `installer-smoke` workflow 已额外在 `macos-latest` runner 上做两件事：

1. 从生产 / beta fixture 真实构建 `codex-remote-feishu_*_darwin_universal_installer.dmg`
2. 校验生成的 `dmg` 已进入对应 `checksums.txt`

这条链的职责是验证“GitHub mac runner 能真实把包打出来”；它不替代后续的 GUI 运行态交互 smoke。

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

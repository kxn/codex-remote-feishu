# Codex Remote Feishu

`codex-remote-feishu` 把一台机器上的 Codex 工作现场带到飞书，让你可以在飞书里接管工作区、切换 thread、继续对话、发图和停止当前 turn。

使用说明 https://my.feishu.cn/docx/PTncdNBf1oS9N5xBikBcGi2enzc

核心目标场景是：

- 本机或远端 Linux 上运行 Codex
- 默认直接在飞书里按工作区和已有会话继续当前工作
- 只有在需要跟着编辑器当前焦点走时，才按需接入 VS Code
- 尽量保留原有对话的 thread、模型配置和工作目录语义

## 组件

当前 release 只发布一个统一二进制：

- `codex-remote`
  - `daemon` role
    - 常驻服务
    - 管理实例、thread、消息队列、Feishu 投影和状态接口
  - `app-server` / wrapper role
    - 包装真实 `codex`
    - 把原生 app-server 协议翻译成统一事件流
  - `install` role
    - 引导安装器
    - 负责安装稳定二进制、写统一配置并启动 WebSetup

当前官方发布模型是：

- GitHub Releases 的平台包内只放最终用户需要的运行资产
  - `codex-remote` / `codex-remote.exe`
  - `README.md`
  - `QUICKSTART.md`
  - `CHANGELOG.md`
  - `deploy/`
- 在线安装脚本 `install-release.sh` / `install-release.ps1` 单独作为 release 资产和仓库入口提供
- GitHub Releases 现在区分 `production / beta / alpha` 三条 track
  - 默认在线安装入口始终指向最新 `production`
  - 需要时可显式安装最新 `beta` / `alpha`
- 正式 release 构建与发布全部在 GitHub Actions 的 `Release` workflow 上完成

## 功能

- 在飞书里列出可接管工作区，并直接继续已有对话或新开会话
- 在统一目标选择卡里直接添加工作区：接入已有本地目录，或导入 Git 仓库
- 直接从最近或全部会话列表继续已有对话
- 需要时切到 VS Code 跟随当前编辑器对话
- 文本消息排队、typing reaction、stop 中断
- 回复当前正在执行的源消息可直接 steer 进本轮执行，也支持 `/steerall`
- 排队中的文字消息支持用点赞升级成对当前执行的跟进
- 支持暂存图片，并在下一条文本里一起发给 Codex
- 支持 `/sendfile`，从当前工作区挑一个文件直接发回飞书聊天
- 支持 `/compact`，对当前 thread 主动做一次手动上下文整理
- 查看当前生效的模型和推理强度，并做飞书侧临时覆盖
- 用 `/cron` 为当前 daemon 实例打开专属定时任务多维表格，并可调度本地工作区或 Git 仓库来源的任务
- 可在飞书里看到计划更新、工具调用、Web 搜索、命令执行等共享进度卡
- 区分系统提示、过程消息和最终回复
- 最终回复会直接回在触发它的那条消息下方
- 旧卡片、旧按钮和旧命令会明确提示已过期或已移除
- 最终回复中的本地 `.md` 和单文件 `.html` 链接可自动替换成飞书云空间预览链接

## 安装前准备

1. 确保真实 `codex` 在目标机器上可运行
2. 如果你需要和 VS Code 联动，再安装 VS Code 的 ChatGPT / Codex 扩展
3. 准备飞书自建应用
4. 只有在源码构建或仓库联调时才需要 Go 1.24+

飞书应用配置可参考：

- [deploy/feishu/app-template.json](./deploy/feishu/app-template.json)
- [deploy/feishu/README.md](./deploy/feishu/README.md)

至少要准备：

- `App ID`
- `App Secret`
- 基础交互所需的消息接收、reaction、机器人菜单相关权限和事件
- 如果要启用 `/cron` 定时任务配置，推荐额外开通 `bitable:app`
- 如果要启用本地文档预览，推荐额外开通 `drive:drive`

本地文档预览当前实现会在发送最终回复前自动创建目录、上传文件、查询访问链接，并给当前对话用户或群补协作者权限。不开这部分权限时，主对话功能仍可使用，但本地 `.md` 或单文件 `.html` 链接不会被替换成飞书预览链接。

## 一条命令安装

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1 | iex
```

这个脚本会自动：

- 识别平台
- 下载 GitHub 构建好的 release 包
- 解压到本地 release 缓存目录
- 安装稳定路径下的 `codex-remote`
- 启动本地 daemon
- 打开或打印 WebSetup 链接

如果要安装某个 prerelease track 的最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Track beta
```

如果要安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Version <version>
```

## 手动安装 release 包

从 GitHub Releases 下载对应平台归档后，直接运行归档内的统一二进制。

macOS / Linux:

```bash
./codex-remote install -bootstrap-only -start-daemon
```

Windows PowerShell:

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

启动后打开输出中的 `/setup` 链接，后续飞书配置、normal mode 使用准备、以及按需的 VS Code detect/apply 和 shim 重装都在 WebSetup / Admin UI 完成。

## 当前用户升级方式

如果你已经完成安装，后续要升级到当前 track 的最新版本，面向用户的推荐入口统一是：

```text
/upgrade latest
```

在已接入的飞书会话里发送这条命令即可。

这条入口适合日常升级检查、继续上一次未完成的升级，以及把当前安装更新到最新 release。用户文档里不再要求你手动准备本地升级产物或执行仓库内 helper。

daemon 不会再后台自动检查并主动弹出 release 升级提示；release 升级只会在你手动发送 `/upgrade latest` 之后检查或继续。

## WebSetup 与按需接入 VS Code

release 安装器现在只做 bootstrap：

- 复制或安装稳定二进制
- 写统一配置 `config.json`
- 保留已有 `config.json` 中的凭证与关键配置
- 启动 daemon 与嵌入式 Web UI

真正的产品配置入口已经收敛到 WebSetup / Admin UI：

- 飞书 App 新增、验证、启停
- 运行环境检查
- 自动启动
- 按需执行 VS Code `detect`
- 按需执行 `editor_settings` apply
- 按需执行 `managed_shim` apply
- 按需执行扩展升级后的 `reinstall-shim`

当前默认推荐顺序是：

1. 先完成飞书 App 接入和运行环境检查
2. 直接开始在飞书里用默认 `normal` 模式工作
3. 只有在你明确需要“飞书跟着 VS Code 当前焦点走”时，再回到页面接入 VS Code

换句话说，VS Code 接入现在是可选增强，不再是开始使用前的默认前提。

VS Code 两种接管方式的区别：

- `editor_settings`
  - 修改 `settings.json` 的 `chatgpt.cliExecutable`
  - 更适合本机桌面 VS Code
- `managed_shim`
  - 直接替换扩展 bundle 里的 `codex`
  - 原始入口会保留成 `codex.real`
  - 更适合 VS Code Remote

如果你是从源码仓库联调而不是从 release 包安装：

- `go build -o ./bin/codex-remote ./cmd/codex-remote`
  - 构建源码仓库本地二进制
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - 直接用本地构建的 binary 做 bootstrap 并拉起 daemon
- `./setup.ps1`
  - Windows 上的辅助脚本，会构建本地 binary 后执行相同 install 命令

这些都是源码仓库联调入口，不是 release 包产品入口。

## Linux 常驻服务与用户升级

如果你希望 Linux 上的正式常驻实例由 `systemd --user` 托管，而不是依赖 detached daemon：

```bash
codex-remote service install-user
codex-remote service enable
codex-remote service start
codex-remote service status
```

如果希望用户未登录时也保持 user service 可自启，还需要：

```bash
loginctl enable-linger "$USER"
```

这条路径保持运行身份为当前用户，并继续使用当前 XDG 配置/状态目录。

完成安装并接入飞书后，用户日常升级到当前 track 的最新版本，直接在飞书里发送：

```text
/upgrade latest
```

如果当前有新版本可用，系统会在后台完成升级；如果上一次升级中断，这条命令也会用于继续或检查当前升级状态。
daemon 不会在后台自动检查 release 并主动弹卡；升级只通过这条手动入口触发。

## 仓库内联调入口

源码仓库里不再保留单独的 `install.sh` 生命周期脚本。现在统一使用现有单 binary 入口：

- `./setup.ps1`
  - Windows 上的同等辅助脚本
- `go build -o ./bin/codex-remote ./cmd/codex-remote`
  - 先构建本地 binary
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - 已经构建过二进制时，可直接重新 bootstrap 并确保本地 daemon 就绪
- `./bin/codex-remote daemon`
  - 需要前台直接观察 daemon 启动过程和日志时使用

如果当前 workspace 下存在 `.codex-remote/install-target.json`，这些 repo-local 命令会优先跟随该 workspace 绑定的全局实例与 `baseDir`；没有 binding 时，默认退回 `stable`。

stable 默认会写入：

- `~/.config/codex-remote/config.json`
- `~/.local/share/codex-remote/install-state.json`
- `~/.local/share/codex-remote/logs/codex-remote-relayd.log`

命名实例则会写入 namespaced 路径，例如 `master`：

- `<baseDir>/.config/codex-remote-master/codex-remote/config.json`
- `<baseDir>/.local/share/codex-remote-master/codex-remote/install-state.json`
- `<baseDir>/.local/share/codex-remote-master/codex-remote/logs/codex-remote-relayd.log`

当前磁盘配置入口只保留 `config.json`；旧的 `config.env` / `wrapper.env` / `services.env` 不再自动读取或迁移。

## 不再支持 Docker 部署

当前项目不再提供 Docker 部署入口。

原因是 Docker 场景下对任意文件和目录访问的配置体验太差，而当前产品已经是 Go 单二进制，直接本机安装和运行的成本已经很低，继续维护 Docker 形态的实际收益不大。

## 飞书端使用

当前默认推荐先用 `normal` 模式；大多数情况下，不需要先处理 VS Code 接入也可以开始工作。

常见用法：

- 继续当前项目：`/list` 接管工作区，再用 `/use` 继续已有会话，或用 `/new` 开一个干净的新会话
- 添加新工作区：`/list` -> `添加工作区`，可以接入已有目录，也可以直接拉一个 Git 仓库
- 直接回到最近对话：`/use`
- 需要跟着编辑器当前焦点走：`/mode vscode` -> `/list` -> `/follow`
- 先发截图再补说明：先发图片，下一条文字会把图片一起带给 Codex
- 想把补充说明并进当前执行：直接回复当前 running 的那条用户消息，或发送 `/steerall`
- 需要手动整理当前 thread：`/compact`
- 想把当前工作区里的文件发回聊天：`/sendfile`

命令：

- `/help`：查看当前可用命令、示例和说明
- `/menu`：打开阶段感知的命令菜单首页；未接管时优先 `/list`、`/use`、`/status`，工作态优先 `/stop`、`/new` 和发送设置
- `/list`：列出当前可手工接管的目标；`normal` 模式下是工作区，也是默认推荐入口；`vscode` 模式下才是在线 VS Code 实例
- 选择方式：`/menu` 和参数卡现在是按钮优先的紧凑卡片，主操作尽量一行一个按钮；`/help` 仍保持文本帮助
- 如果看到旧卡片，请重新发送命令
- `/use`：列出最近可见会话；即使当前还没显式 attach，也可以直接从这里继续已有对话
- `/threads`：`/use` 的兼容别名
- `/useall`：列出全部可见会话
- 会话选择后：系统会切到目标会话；必要时会自动接管在线实例，或在后台恢复目标会话
- `/history`：查看当前输入目标 thread 的历史 turn 列表，并在卡片里翻页或看单轮详情
- `/status`：查看当前接管状态、队列和模型配置
- `/follow`：在 `vscode` 模式下切回跟随当前 VS Code thread
- `/stop`：中断当前 turn，并清空尚未发出的飞书队列
- `/compact`：对当前已绑定 thread 主动发起一次手动上下文整理；当前有其他任务时会直接拒绝
- `/steerall`：把当前队列里可并入本轮执行的输入一次性并入当前 running turn
- 排队中的文字消息如果还没发出，也可以给这条消息点 `ThumbsUp`，把它升级成对当前执行的跟进
- 如果你直接回复当前正在执行的那条用户消息，系统也会优先把这条 reply 当成对当前 turn 的 steer
- `/detach`：断开当前实例接管；如果当前正在后台恢复，也会一并取消
- `/sendfile`：打开文件选择卡，从当前工作区挑一个文件发送到当前聊天
- `/model`：查看或设置飞书侧模型覆盖；bare `/model` 会返回模型卡与 capture/apply fallback
- `/reasoning`：查看或设置飞书侧推理强度覆盖；bare `/reasoning` 会返回参数卡
- `/access`：查看或设置飞书侧执行权限覆盖，支持 `full`、`confirm`、`clear`
- `/approval`：`/access` 的别名
- `/verbose`：控制飞书前端显示过程消息的详细程度；`quiet` 更安静，`verbose` 会显示共享进行中卡
- `/cron`：打开当前实例专属的定时任务菜单；`/cron edit` 打开配置表，编辑后执行 `/cron reload` 生效
- `/repair`：断联后的一键修复；默认重连当前飞书 bot runtime，并在当前实例安全空闲时修复 provider child 或恢复离线 managed headless，不改全局 Codex 配置；需要重启托管 daemon 时发送 `/repair daemon`

另外：

- 最终回复现在会直接回在你触发它的原始消息下面，群聊里更容易看懂上下文
- 共享过程卡会尽量把计划更新、工具调用、Web 搜索和命令执行整理成更容易扫读的摘要，而不是一长串零散文本
- 旧卡片、旧按钮或旧菜单动作如果已经过期，会收到明确提示；直接重发对应命令即可

机器人菜单：

- `menu`
- `stop`
- `steerall`
- `new`
- `reasoning`
- `model`
- `access`

WebSetup 里的推荐菜单、飞书模板和 `/help` 当前都来自同一套命令定义；其中 `/help` 保持文本帮助，`/menu` / 参数卡走紧凑按钮卡片。
另外，菜单卡片里不再重复放“返回首页”按钮；如果要回首页，直接点飞书 bot 菜单里的 `menu` 更快。

关于本地文档预览：

- 当前只处理 assistant 最终回复里的 Markdown 链接，例如 `[README](docs/README.md)` 或 `[mock](docs/mock.html)`
- 不会改写普通纯文本路径、代码块里的路径或用户输入里的路径
- 预览文件默认上传到应用云空间目录 `Codex Remote Previews/`
- `.html` 当前按单文件预览处理，不会额外打包它引用的其它本地资源
- 单聊会给当前用户授权；群聊会同时给当前用户和当前群授权

## 排障

先确认不是代理污染本地链路：

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
```

然后按顺序检查：

1. `curl --noproxy '*' -sf http://127.0.0.1:9501/api/admin/bootstrap-state | jq .`
2. `curl --noproxy '*' -sf http://127.0.0.1:9501/v1/status | jq .`
3. `config.json` 里的 `relay.serverURL`、`wrapper.codexRealBinary`、飞书凭证和监听地址
4. VS Code 是否真的已经通过 wrapper 启动 Codex
5. `~/.local/share/codex-remote/logs/codex-remote-relayd.log`

## 文档

- [变更记录](./CHANGELOG.md)
- [用户使用说明书](./docs/general/user-guide.md)
- [文档索引](./docs/README.md)
- [架构说明](./docs/general/architecture.md)
- [协议说明](./docs/general/relay-protocol-spec.md)
- [飞书产品行为](./docs/general/feishu-product-design.md)
- [飞书 Markdown 预览设计](./docs/implemented/feishu-md-preview-design.md)
- [安装与部署](./docs/general/install-deploy-design.md)
- [测试策略](./docs/general/go-test-strategy.md)

## 发布

- Push / PR 会触发 GitHub Actions CI
- `Release` workflow 支持显式指定版本号
- admin UI、各平台二进制、checksums 和 GitHub Release 发布都在 GitHub 端完成
- 本地 `make release-artifacts VERSION=vX.Y.Z` 只建议作为打包预演，不是正式发布路径

## 开发

开发者说明见 [DEVELOPER.md](./DEVELOPER.md)。

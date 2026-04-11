# Codex Remote Feishu

`codex-remote-feishu` 把 VS Code 里的 Codex 会话桥接到飞书，让你可以在飞书里接管、切换 thread、继续对话、发图和停止当前 turn。

核心目标场景是：

- 本机或远端 Linux 上运行 Codex / VS Code
- 飞书里继续使用同一个 Codex instance
- 保留 VS Code 原有 thread、模型配置和工作目录语义

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
- 在线安装脚本 `install-release.sh` 单独作为 release 资产和仓库入口提供
- GitHub Releases 现在区分 `production / beta / alpha` 三条 track
  - 默认在线安装入口始终指向最新 `production`
  - 需要时可显式安装最新 `beta` / `alpha`
- 正式 release 构建与发布全部在 GitHub Actions 的 `Release` workflow 上完成

## 功能

- 在飞书里列出在线 VS Code 实例并显式接管
- 直接从最近或全部会话列表继续已有对话
- 文本消息排队、typing reaction、stop 中断
- 排队中的文字消息支持用点赞升级成对当前执行的跟进
- 支持暂存图片，并在下一条文本里一起发给 Codex
- 查看当前生效的模型和推理强度，并做飞书侧临时覆盖
- 区分系统提示、过程消息和最终回复
- 最终回复会直接回在触发它的那条消息下方
- 旧卡片、旧按钮和旧命令会明确提示已过期或已移除
- 最终回复中的本地 `.md` 和单文件 `.html` 链接可自动替换成飞书云空间预览链接

## 安装前准备

1. 确保真实 `codex` 在目标机器上可运行
2. 安装 VS Code 的 ChatGPT / Codex 扩展
3. 准备飞书自建应用
4. 只有在源码构建或仓库联调时才需要 Go 1.24+

飞书应用配置可参考：

- [deploy/feishu/app-template.json](./deploy/feishu/app-template.json)
- [deploy/feishu/README.md](./deploy/feishu/README.md)

至少要准备：

- `App ID`
- `App Secret`
- 基础交互所需的消息接收、reaction、机器人菜单相关权限和事件
- 如果要启用本地文档预览，推荐额外开通 `drive:drive`

本地文档预览当前实现会在发送最终回复前自动创建目录、上传文件、查询访问链接，并给当前对话用户或群补协作者权限。不开这部分权限时，主对话功能仍可使用，但本地 `.md` 或单文件 `.html` 链接不会被替换成飞书预览链接。

## 一条命令安装

macOS / Linux 可以直接执行：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
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

如果要安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
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

启动后打开输出中的 `/setup` 链接，后续飞书配置、VS Code detect/apply 和 shim 重装都在 WebSetup / Admin UI 完成。

## WebSetup 与 VS Code 接管

release 安装器现在只做 bootstrap：

- 复制或安装稳定二进制
- 写统一配置 `config.json`
- 保留旧版 `config.env` / `wrapper.env` / `services.env` 的迁移结果
- 启动 daemon 与嵌入式 Web UI

真正的产品配置入口已经收敛到 WebSetup / Admin UI：

- 飞书 App 新增、验证、启停
- VS Code `detect`
- `editor_settings` apply
- `managed_shim` apply
- 扩展升级后的 `reinstall-shim`

VS Code 两种接管方式的区别：

- `editor_settings`
  - 修改 `settings.json` 的 `chatgpt.cliExecutable`
  - 更适合本机桌面 VS Code
- `managed_shim`
  - 直接替换扩展 bundle 里的 `codex`
  - 原始入口会保留成 `codex.real`
  - 更适合 VS Code Remote

如果你是从源码仓库联调而不是从 release 包安装：

- `./setup.sh`
  - 构建本地二进制
  - 默认执行 `codex-remote install -bootstrap-only -start-daemon`
- `./setup.ps1`
  - Windows 上的同等辅助脚本

它们是仓库 helper，不是 release 包产品入口。

## Linux 常驻服务与内置升级

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

如果你已经在源码仓库里编译了一个新的本地 binary，产品入口仍然是把它放到固定 artifact 路径，再发送产品命令：

```bash
cp ./bin/codex-remote ~/.local/share/codex-remote/local-upgrade/codex-remote
```

然后在已接入的飞书会话里发送：

```text
/upgrade local
```

如果要检查或继续升级到当前 track 的最新 GitHub release，则发送：

```text
/upgrade latest
```

默认 Linux `systemd --user` 安装的本地 artifact 固定路径是：

```text
~/.local/share/codex-remote/local-upgrade/codex-remote
```

这两条入口都会复用同一套内置 upgrade transaction：

- 准备目标 slot 与 rollback candidate
- 复制当前 live binary 为 `upgrade-helper`
- 在 `systemd_user` 模式下通过独立 transient unit 执行切换
- 如果新版本启动或健康检查失败，自动回滚 binary 和 live config

源码仓库里如果只是想本地拉最新、重新构建并直接发起同一套内置事务，可以使用：

```bash
./upgrade-local.sh
```

## 仓库内联调入口

源码仓库里不再保留单独的 `install.sh` 生命周期脚本。现在统一使用现有单 binary 入口：

- `./setup.sh`
  - 构建本地 `./bin/codex-remote`
  - 默认执行 `codex-remote install -bootstrap-only -start-daemon`
- `./upgrade-local.sh`
  - `git pull --ff-only`
  - 构建本地 `./bin/codex-remote`
  - 复制到固定 local-upgrade artifact 路径
  - 调用 `./bin/codex-remote local-upgrade`
- `./bin/codex-remote local-upgrade`
  - 使用固定 local-upgrade artifact 路径触发同一套内置 local upgrade transaction
- `./setup.ps1`
  - Windows 上的同等辅助脚本
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - 已经构建过二进制时，可直接重新 bootstrap 并确保本地 daemon 就绪
- `./bin/codex-remote daemon`
  - 需要前台直接观察 daemon 启动过程和日志时使用

默认会写入：

- `~/.config/codex-remote/config.json`
- `~/.local/share/codex-remote/install-state.json`
- `~/.local/share/codex-remote/logs/codex-remote-relayd.log`

如果本机还只有旧的 `config.env` / `wrapper.env` / `services.env`，bootstrap 或启动时会自动迁移到 `config.json`，并把旧文件备份成 `*.migrated-<timestamp>.bak`。

## Docker 部署

如果你只想把 `relayd` 容器化，可以使用：

- [deploy/docker/Dockerfile](./deploy/docker/Dockerfile)
- [deploy/docker/compose.yml](./deploy/docker/compose.yml)
- [deploy/docker/.env.example](./deploy/docker/.env.example)

release 包内会附带：

- [QUICKSTART.md](./QUICKSTART.md)
- [CHANGELOG.md](./CHANGELOG.md)
- [deploy/docker/Dockerfile](./deploy/docker/Dockerfile)
- [deploy/docker/compose.yml](./deploy/docker/compose.yml)
- [deploy/feishu/app-template.json](./deploy/feishu/app-template.json)

用法：

```bash
cp deploy/docker/.env.example deploy/docker/.env
docker compose -f deploy/docker/compose.yml --env-file deploy/docker/.env up -d --build
```

注意：

- Docker 只部署 `codex-remote daemon`
- `codex-remote` 的 wrapper role 仍然运行在 VS Code 所在机器
- wrapper 连接容器时，默认仍使用 `ws://127.0.0.1:9500/ws/agent`

## 飞书端使用

命令：

- `/help`：查看当前可用命令、示例和说明
- `/menu`：打开阶段感知的命令菜单首页；未接管时优先 `/list`、`/use`、`/status`，工作态优先 `/stop`、`/new` 和发送设置
- `/list`：列出当前可手工接管的目标；`normal` 模式下是工作区，`vscode` 模式下是在线 VS Code 实例
- 选择方式：`/menu` 和参数卡现在是按钮优先的紧凑卡片，主操作尽量一行一个按钮；`/help` 仍保持文本帮助
- 如果看到旧卡片，请重新发送命令
- `/use`：列出最近可见会话；即使当前还没显式 attach，也可以直接从这里继续已有对话
- `/threads`：`/use` 的兼容别名
- `/useall`：列出全部可见会话
- 会话选择后：系统会切到目标会话；必要时会自动接管在线实例，或在后台恢复目标会话
- `/status`：查看当前接管状态、队列和模型配置
- `/follow`：切回跟随当前 VS Code thread
- `/stop`：中断当前 turn，并清空尚未发出的飞书队列
- 排队中的文字消息如果还没发出，也可以给这条消息点 `ThumbsUp`，把它升级成对当前执行的跟进
- `/detach`：断开当前实例接管；如果当前正在后台恢复，也会一并取消
- `/model`：查看或设置飞书侧模型覆盖；bare `/model` 会返回模型卡与 capture/apply fallback
- `/reasoning`：查看或设置飞书侧推理强度覆盖；bare `/reasoning` 会返回参数卡
- `/access`：查看或设置飞书侧执行权限覆盖，支持 `full`、`confirm`、`clear`
- `/approval`：`/access` 的别名

另外：

- 最终回复现在会直接回在你触发它的原始消息下面，群聊里更容易看懂上下文
- 旧卡片、旧按钮或旧菜单动作如果已经过期，会收到明确提示；直接重发对应命令即可

机器人菜单：

- `menu`
- `stop`
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

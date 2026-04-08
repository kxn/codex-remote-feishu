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
- 支持暂存图片，并在下一条文本里一起发给 Codex
- 查看当前生效的模型和推理强度，并做飞书侧临时覆盖
- 区分系统提示、过程消息和最终回复
- 最终回复中的本地 `.md` Markdown 链接可自动替换成飞书云空间预览链接

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
- 如果要启用 `.md` 预览，推荐额外开通 `drive:drive`

`.md` 预览当前实现会在发送最终回复前自动创建目录、上传文件、查询访问链接，并给当前对话用户或群补协作者权限。不开这部分权限时，主对话功能仍可使用，但 `.md` 链接不会被替换成飞书预览链接。

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

## 仓库内联调入口

源码仓库里不再保留单独的 `install.sh` 生命周期脚本。现在统一使用现有单 binary 入口：

- `./setup.sh`
  - 构建本地 `./bin/codex-remote`
  - 默认执行 `codex-remote install -bootstrap-only -start-daemon`
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

- `/list`：列出当前可手工接管的在线 VS Code 实例
- 选择方式：当前通过卡片里的按钮直接触发；如果看到旧卡片，请重新发送命令
- `/threads` 或 `/use`：列出最近可见会话；即使当前还没显式 attach，也可以直接从这里继续已有对话
- `/useall`：列出全部可见会话
- 会话选择后：系统会切到目标会话；必要时会自动接管在线实例，或复用/启动后台 headless 实例
- `/status`：查看当前接管状态、队列和模型配置
- `/follow`：切回跟随当前 VS Code thread
- `/stop`：中断当前 turn，并清空尚未发出的飞书队列
- `/detach`：断开当前实例接管
- `/model`：查看或设置飞书侧模型覆盖
- `/reasoning`：查看或设置飞书侧推理强度覆盖
- `/access`：查看或设置飞书侧执行权限覆盖，支持 `full`、`confirm`、`clear`
- `/approval`：`/access` 的别名

机器人菜单：

- `list`
- `status`
- `threads`
- `stop`
- `access_full`
- `access_confirm`

关于 `.md` 预览：

- 当前只处理 assistant 最终回复里的 Markdown 链接，例如 `[README](docs/README.md)`
- 不会改写普通纯文本路径、代码块里的路径或用户输入里的路径
- 预览文件默认上传到应用云空间目录 `Codex Remote Previews/`
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

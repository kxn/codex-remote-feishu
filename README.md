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
    - 安装器
    - 负责写配置并接管 VS Code / VS Code Remote

## 功能

- 在飞书里列出在线实例并 attach
- 列出 thread 并切换当前输入目标
- 文本消息排队、typing reaction、stop 中断
- 支持暂存图片，并在下一条文本里一起发给 Codex
- 查看当前生效的模型和推理强度，并做飞书侧临时覆盖
- 区分系统提示、过程消息和最终回复
- 最终回复中的本地 `.md` Markdown 链接可自动替换成飞书云空间预览链接

## 安装前准备

1. 安装 Go 1.24+
2. 确保真实 `codex` 在目标机器上可运行
3. 安装 VS Code 的 ChatGPT / Codex 扩展
4. 准备飞书自建应用

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
- 下载最新 release 包
- 解压到本地 release 缓存目录
- 启动包内的 `setup.sh` 进入交互安装

如果要安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

## 交互安装

macOS / Linux:

```bash
./setup.sh
```

Windows PowerShell:

```powershell
.\setup.ps1
```

安装向导会：

- 构建 `codex-remote`
- 询问飞书 `App ID` / `App Secret`
- 询问 relay 地址
- 让你选择 VS Code 集成方式
- 自动写入统一配置 `config.json` 和 `install-state.json`

默认集成方式：

- Linux: `editor_settings + managed_shim`
- macOS: `editor_settings`
- Windows: `editor_settings`

两种集成方式的区别：

- `editor_settings`
  - 修改 `settings.json` 的 `chatgpt.cliExecutable`
  - 更适合本机桌面 VS Code
- `managed_shim`
  - 直接替换扩展 bundle 里的 `codex`
  - 原始入口会保留成 `codex.real`
  - 更适合 VS Code Remote

如果你想做非交互安装，可以直接给 `setup.sh` / `setup.ps1` 透传 `codex-remote install` 的参数，例如：

```bash
./setup.sh \
  -integration both \
  -feishu-app-id cli_xxx \
  -feishu-app-secret secret_xxx \
  -relay-url ws://127.0.0.1:9500/ws/agent
```

## 运行 relayd

本机 Linux 运维脚本：

```bash
./install.sh start
./install.sh restart
./install.sh refresh
./install.sh status
./install.sh logs
./install.sh stop
```

建议区分两种场景：

- 只想重启当前 relay 服务链路，或回收“pid 文件丢了但 daemon 还活着”的残留状态：`./install.sh restart`
- 刚改过 Go 代码，且启用了 `managed_shim`，需要把 `~/.local/bin` 和 VS Code 扩展 bundle 一起刷新到新版本：`./install.sh refresh`

`restart` 和 `refresh` 都会尝试停止当前安装链路上的 wrapper/app-server/daemon 进程再拉起；如果 VS Code 里正开着 Codex，会话可能被中断，这是预期行为。区别是 `refresh` 还会把 `~/.local/bin` 和 managed shim bundle 入口重新刷新到最新构建。

默认会写入：

- `~/.config/codex-remote/config.json`
- `~/.local/share/codex-remote/install-state.json`
- `~/.local/share/codex-remote/logs/codex-remote-relayd.log`

如果本机还只有旧的 `config.env` / `wrapper.env` / `services.env`，启动时会自动迁移到 `config.json`，并把旧文件备份成 `*.migrated-<timestamp>.bak`。

## Docker 部署

如果你只想把 `relayd` 容器化，可以使用：

- [deploy/docker/Dockerfile](./deploy/docker/Dockerfile)
- [deploy/docker/compose.yml](./deploy/docker/compose.yml)
- [deploy/docker/.env.example](./deploy/docker/.env.example)

release 包内也会附带：

- [QUICKSTART.md](./QUICKSTART.md)
- [install-release.sh](./install-release.sh)
- `setup.sh` / `setup.ps1`

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

- `/list`：列出在线实例
- 回复序号：attach 到对应实例
- `/threads` 或 `/use`：列出当前实例可见 thread
- 回复序号：切换输入目标 thread
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

1. `./install.sh status`
2. `curl --noproxy '*' -sf http://127.0.0.1:9501/v1/status | jq .`
3. `config.json` 里的 `relay.serverURL`、`wrapper.codexRealBinary`、飞书凭证和监听地址
5. VS Code 是否真的已经通过 wrapper 启动 Codex
6. `codex-remote-relayd.log`

## 文档

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
- release 产物会包含各平台安装包、在线安装脚本和校验文件

## 开发

开发者说明见 [DEVELOPER.md](./DEVELOPER.md)。

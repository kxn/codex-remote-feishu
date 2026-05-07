# Developer Guide

## 项目定位

当前仓库维护的是公开可发布的 Go 版本：

```text
github.com/kxn/codex-remote-feishu
```

不要再把旧的 `fschannel` 命名、旧的 Node/Rust 结构、或本机绝对路径带回仓库。

## 二进制与职责

- `codex-remote`
  - `daemon`
    - relay websocket 服务端
    - orchestrator
    - Feishu gateway
    - 状态 API
  - `app-server` / wrapper role
    - 真实 `codex` 的包装器
    - native app-server -> canonical protocol 适配
  - `install`
    - bootstrap 安装器
    - 安装稳定二进制
    - 写统一配置
    - 启动 WebSetup / Admin UI

兼容期内仓库仍保留：

- `cmd/relayd`
- `cmd/relay-wrapper`
- `cmd/relay-install`

但它们只是薄 shim，统一转发到 `codex-remote` 的 launcher。

## 目录

```text
cmd/
  codex-remote/
  relayd/
  relay-wrapper/
  relay-install/

deploy/
  feishu/

docs/

internal/
  adapter/
  app/
  config/
  core/

testkit/
```

## 用户入口与开发入口

产品入口：

- `install-release.sh`
  - 面向最终用户的在线安装入口
  - 默认下载最新 `production` release 包
  - 支持按 `--track production|beta|alpha` 拉取对应 track 的最新 release
  - 执行 `codex-remote install -bootstrap-only -start-daemon`
- release archive 内的 `codex-remote`
  - 手动安装时执行 `./codex-remote install -bootstrap-only -start-daemon`

仓库 helper：

- `make start`
  - 一键构建二进制到 `./bin/codex-remote` 并启动 daemon（Linux / macOS 优先）
- `setup.ps1`
  - Windows 上的源码仓库辅助脚本
  - 默认构建本地 binary 后启动同一套 WebSetup 流程
  - 如显式传参，则直接透传给 `codex-remote install`
- `go build -o ./bin/codex-remote ./cmd/codex-remote`
  - 构建本地 binary 到 `./bin/` 目录
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - 已经构建过本地 binary 时，可直接重新 bootstrap 并确保本地 daemon 就绪
- `./bin/codex-remote daemon`
  - 前台运行 daemon，方便直接观察启动过程和日志

不要把源码仓库 helper 当成 release 包产品入口。

## 关键文档

- [文档索引](./docs/README.md)
- [架构说明](./docs/general/architecture.md)
- [协议说明](./docs/general/relay-protocol-spec.md)
- [飞书产品行为](./docs/general/feishu-product-design.md)
- [安装与部署](./docs/general/install-deploy-design.md)
- [测试策略](./docs/general/go-test-strategy.md)
- [单一二进制设计](./docs/inprogress/unified-binary-design.md)

如果改了这些内容，文档也要同步：

- wrapper 和 relayd 之间的 canonical protocol
- Feishu 输入输出逻辑
- 安装流程、默认路径、部署方式

## 架构概览

### 单一二进制与三种角色

项目编译为单个 Go 二进制文件 `codex-remote`，通过命令行参数选择三种角色（详见 [二进制与职责](#二进制与职责)）：

| 角色 | 启动方式 | 职责 |
|---|---|---|
| **Daemon** | `codex-remote daemon` 或无参数 | WebSocket 服务端（端口 9500）、Feishu gateway、orchestrator、Admin API（端口 9501） |
| **Wrapper** | `codex-remote wrapper app-server` / `codex-remote wrapper claude-app-server` | 位于父进程（VS Code 扩展、headless 启动器）与子 AI 进程之间的代理层；代理 stdin/stdout，同时通过 WebSocket 与 daemon 通信 |
| **Install** | `codex-remote install` | 处理 bootstrap、配置迁移、systemd 服务安装、WebSetup / Admin UI 启动 |

旧 shim（`cmd/relayd`, `cmd/relay-wrapper`, `cmd/relay-install`）在兼容期内保留，但仅作为转发到统一 launcher 的薄封装。

### 适配器模式（Adapter Pattern）

Wrapper 通过 `backendRuntime` 接口支持多种 AI 客户端后端：

```
backendRuntime interface {
    Backend() agentproto.Backend
    Capabilities() agentproto.Capabilities
    Launch(ctx, app, logger, onError) (*childSession, error)
    ObserveClient([]byte) (runtimeObserveResult, error)
    ObserveServer([]byte) (runtimeObserveResult, error)
    TranslateCommand(agentproto.Command) (runtimeCommandResult, error)
    PrepareChildRestart(string, agentproto.Target) error
    BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error)
    CancelChildRestartRestore(string)
}
```

每个后端在 `internal/adapter/<name>/` 下有独立适配器，负责将后端原生协议（Codex 的 JSON-RPC、Claude 的 stream-json 等）翻译为 canonical `agentproto.Event` / `agentproto.Command` 类型。

集成新后端的完整步骤指南请参考 [新增 AI 后端集成指南](./docs/general/adding-new-ai-backend.md)。

### I/O 管道

```
Parent (VS Code / headless) ──stdin──→ Wrapper ──stdin──→ Child (codex / claude)
                             ←stdout──         ←stdout──
                                            │
                                            └──WebSocket──→ Daemon (relayd)
                                                              │
                                                              └──Feishu Gateway──→ Feishu API
```

Wrapper 中四个并发 goroutine 处理 I/O：

- **stdinLoop** — 读取父进程 stdin，经 translator 提取事件后转发给子进程和 daemon
- **stdoutLoop** — 读取子进程 stdout，经 translator 提取事件后转发给 daemon 和父进程
- **writeLoop** — 从 daemon 接收命令，经 translator 转换为子进程原生指令
- **streamCopy** — 将子进程 stderr 拷贝到 wrapper 日志

### Canonical 协议

Wrapper 与 daemon 之间使用 canonical 协议（`agentproto`）通信，包括：

- **信封**（Envelope）：`hello`, `welcome`, `event_batch`, `command`, `command_ack`, `error`
- **事件**（Event）：thread 生命周期、turn 生命周期、request/response、steer、interrupt、session catalog
- **命令**（Command）：`promptSend`, `turnSteer`, `interrupt`, `requestRespond` 等

### 配置系统

配置按以下优先级（低 → 高）合并：

1. 内置默认值（`DefaultAppConfig()`）
2. `config.json`（XDG 路径，或 `$CODEX_REMOTE_CONFIG` 覆盖）
3. 环境变量（覆盖 config.json 值）

详见下方 [配置与环境变量参考](#配置与环境变量参考)。

## 配置与环境变量参考

### 配置文件结构

默认路径（按 XDG 规范）：

| 平台 | 路径 |
|---|---|
| Linux / macOS | `~/.config/codex-remote/config.json` |
| Windows | `%USERPROFILE%\.config\codex-remote\config.json` |
| 任意 | `$CODEX_REMOTE_CONFIG`（完全覆盖） |

`config.json` 主要字段结构：

```jsonc
{
  "version": 1,
  "relay": {
    "serverURL": "ws://127.0.0.1:9500/ws/agent",  // WebSocket relay 地址
    "listenHost": "127.0.0.1",
    "listenPort": 9500
  },
  "admin": {
    "listenHost": "127.0.0.1",
    "listenPort": 9501,          // Admin API / WebSetup 端口
    "autoOpenBrowser": true,
    "onboarding": { /* 启动向导状态机 */ }
  },
  "wrapper": {
    "codexRealBinary": "codex",  // 真实 codex 二进制路径
    "nameMode": "workspace_basename",
    "integrationMode": "managed_shim"
  },
  "codex": {
    "providers": [               // 自定义 Codex provider
      {
        "id": "default",
        "name": "My Provider",
        "baseURL": "https://api.example.com",
        "apiKey": "sk-...",
        "model": "gpt-4"
      }
    ]
  },
  "claude": {
    "profiles": [                // Claude 配置 profile
      {
        "id": "default",
        "name": "My Profile",
        "authMode": "auth_token",
        "baseURL": "https://api.anthropic.com",
        "authToken": "...",
        "model": "claude-sonnet-4-20250514"
      }
    ]
  },
  "feishu": {
    "useSystemProxy": false,
    "apps": [                    // 飞书应用配置
      {
        "id": "main",
        "appId": "cli_xxxx",
        "appSecret": "...",
        "enabled": true
      }
    ]
  },
  "externalAccess": {
    "listenPort": 9512,
    "defaultLinkTTLSeconds": 600,
    "provider": { "kind": "trycloudflare", "lazyStart": true }
  },
  "debug": {
    "relayFlow": false,          // relay 调试日志
    "relayRaw": false
  }
}
```

完整 schema 见 `internal/config/configfile.go`。

### 关键环境变量

#### 通用配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CODEX_REMOTE_CONFIG` | XDG 路径 | 覆盖 config.json 路径 |
| `RELAY_SERVER_URL` | `ws://127.0.0.1:9500/ws/agent` | WebSocket relay 地址 |
| `RELAY_HOST` | `127.0.0.1` | relay 监听 host |
| `RELAY_PORT` | `9500` | relay 监听端口 |
| `RELAY_API_HOST` | `127.0.0.1` | Admin API 监听 host |
| `RELAY_API_PORT` | `9501` | Admin API 监听端口 |
| `FEISHU_GATEWAY_ID` | 首个启用 app | 选择飞书应用入口 |
| `FEISHU_USE_SYSTEM_PROXY` | `false` | 飞书 API 是否使用系统代理 |

#### Wrapper / 运行时配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CODEX_REAL_BINARY` | `codex` | 真实 codex 二进制路径（VS Code 服务器） |
| `CODEX_REMOTE_INSTANCE_ID` | 自动生成 | 覆盖实例 ID |
| `CODEX_REMOTE_INSTANCE_DISPLAY_NAME` | workspace 名 | 覆盖实例显示名称 |
| `CODEX_REMOTE_INSTANCE_BACKEND` | (空 → 默认) | 后端选择（`codex` / `claude`） |
| `CODEX_REMOTE_RESUME_THREAD_ID` | (空) | 连接后自动恢复的 thread ID |

#### AI 后端配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CODEX_REMOTE_CODEX_PROVIDER_ID` | `default` | 选择 Codex provider |
| `CODEX_REMOTE_CLAUDE_PROFILE_ID` | `default` | 选择 Claude profile |
| `CLAUDE_BIN` | (空) | Claude Code 二进制路径 |
| `ANTHROPIC_BASE_URL` | 默认 | Anthropic API 地址覆盖 |
| `ANTHROPIC_AUTH_TOKEN` | (空) | Anthropic API 认证令牌 |
| `ANTHROPIC_MODEL` | (空) | 默认模型选择 |
| `CLAUDE_CODE_EFFORT_LEVEL` | (空) | 推理努力级别（`low` / `medium` / `high` / `max`） |

#### Debug / 诊断

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CODEX_REMOTE_DEBUG_RELAY_FLOW` | `false` | 启用 relay 流式调试日志 |
| `CODEX_REMOTE_DEBUG_RELAY_RAW` | `false` | 启用 relay 原始数据调试日志 |

### 实例配置路径

按实例 ID 区分配置和数据目录：

- **stable 实例：**
  - `<baseDir>/.config/codex-remote/config.json`
  - `<baseDir>/.local/share/codex-remote/logs/codex-remote-relayd.log`
- **命名实例 `<instanceId>`：**
  - `<baseDir>/.config/codex-remote-<instanceId>/codex-remote/config.json`
  - `<baseDir>/.local/share/codex-remote-<instanceId>/codex-remote/logs/codex-remote-relayd.log`

其中 `<baseDir>` 在 Linux/macOS 为 `~`，在 Windows 为 `%USERPROFILE%`。

### 代理环境

以下环境变量会被 wrapper 捕获并清理后传递给 daemon 子进程，再在启动 codex 子进程时恢复：

- `http_proxy`, `https_proxy`, `all_proxy`（小写）
- `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`（大写）

## 常用命令

格式化：

```bash
gofmt -w $(find cmd internal testkit -name '*.go' | sort)
```

测试：

```bash
go test ./...
```

构建（生成到 `./bin/codex-remote`）：

```bash
go build -o ./bin/codex-remote ./cmd/codex-remote
```

源码仓库一键构建并启动 daemon（推荐）：

```bash
make start
```

`make start` 等价于以下手动步骤：

```bash
go build -o ./bin/codex-remote ./cmd/codex-remote
./bin/codex-remote install -bootstrap-only -start-daemon
```

> 在 Windows 上如果 `make start` 不可用，可直接使用上面的手动步骤。
> 在 Linux / macOS 上 `make start` 是单命令构建 + 启动方式。

源码仓库直接跑单 binary（已构建过之后）：

```bash
./bin/codex-remote install -bootstrap-only -start-daemon
./bin/codex-remote daemon
```

release 安装器 smoke test：

```bash
bash scripts/check/smoke-install-release.sh
```

release track 版本计算校验：

```bash
bash scripts/check/release-track-version.sh
```

本地仅做 release 打包预演：

```bash
make release-artifacts VERSION=v0.1.0
```

这不是正式发布路径，正式 release 由 GitHub Actions 完成构建和发布。

## 安装实现要点

- release / online installer 默认只做 bootstrap，不再在 CLI 里采集飞书凭证或 VS Code 路径
- 默认在线安装入口保持 production-first，beta / alpha 必须显式通过 `--track` 选择
- 飞书配置、VS Code detect/apply、shim 重装统一在 WebSetup / Admin UI 中完成
- 仓库联调统一走本地构建 binary 的 `install -bootstrap-only -start-daemon`
- 多 workspace 联调当前走“workspace 绑定全局实例”模型：
  - repo-local 绑定文件为 `.codex-remote/install-target.json`
  - 兼容旧工具时仍会同步写 `.codex-remote/install-instance`
  - `install` / `service` / `local-upgrade` / `upgrade-local.sh` 默认先读这个 binding
  - 若无 binding，则默认退回 `stable`，并优先向上查找 repo 祖先目录里已存在的 stable install
- 仓库里不再保留单独的 `install.sh` 生命周期脚本
- `managed_shim` 会把扩展 bundle 中的 `codex` 重命名为 `codex.real`
- 然后把统一二进制 `codex-remote` 复制到原始 `codex` 路径
- `CODEX_REAL_BINARY` 会自动指向保留下来的 `codex.real`
- `install-release.sh` 必须兼容 `curl | bash`
- release 包必须能在没有 Go toolchain 和没有源码目录的情况下完成安装

当前配置路径按实例区分：

- stable:
  - `<baseDir>/.config/codex-remote/config.json`
  - `<baseDir>/.local/share/codex-remote/install-state.json`
  - `<baseDir>/.local/share/codex-remote/logs/codex-remote-relayd.log`
- named instance `<instanceId>`:
  - `<baseDir>/.config/codex-remote-<instanceId>/codex-remote/config.json`
  - `<baseDir>/.local/share/codex-remote-<instanceId>/codex-remote/install-state.json`
  - `<baseDir>/.local/share/codex-remote-<instanceId>/codex-remote/logs/codex-remote-relayd.log`

这是当前 runtime config lookup 的约束，不要随意只改安装器而不改运行时读取逻辑。

## 实链路调试

先清理代理环境，避免本地回环链路被污染：

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
```

再按顺序看：

1. 进程和端口

```bash
ps -ef | rg 'codex-remote|relayd|relay-wrapper' | rg -v rg
ss -ltnp | rg '9500|9501'
```

2. WebSetup / admin 状态接口

```bash
curl --noproxy '*' -sf http://127.0.0.1:9501/api/admin/bootstrap-state | jq .
curl --noproxy '*' -sf http://127.0.0.1:9501/v1/status | jq .
```

重点字段：

- `phase`
- `setupRequired`
- `gateways[*].state`
- `instances[*].Online`
- `surfaces[*].AttachedInstanceID`
- `surfaces[*].SelectedThreadID`

3. relayd 日志

重点前缀：

- `startup state:`
- `web setup:`
- `web admin:`
- `surface action:`
- `agent event:`
- `ui command:`
- `relay command ack:`
- `ui event:`
- `gateway apply failed:`

调状态机问题时，不要只看最终失败点，要把单条消息沿这些日志完整串起来。

## 代理环境规则

- 本地 wrapper <-> relayd 通信不应走代理
- 本地 `curl 127.0.0.1` / websocket 调试前应先 `unset` 代理
- `codex-remote` 的 wrapper role 拉起真实 `codex.real` 时，应恢复捕获到的代理环境
- `relayd` 是否使用系统代理，只由 `FEISHU_USE_SYSTEM_PROXY` 控制

## 开发约束

- 完整编码规范请参考 [AGENTS.md](./AGENTS.md)，包括工作区整洁性、触发器规则、GitHub Issue 流程、提交与推送策略等
- 协议或状态机问题先看全链路，再改局部
- 不要在 wrapper 层吞掉上游协议信息来“修 UI”
- 产品可见性、队列和 thread 选择应由 orchestrator 决策
- helper/internal traffic 只能靠明确协议标识关联，不能靠猜测
- mock 必须贴近真实协议，不能用静态脚本假装通过
- 公开文档、README、模板文件里不要泄露本机路径
- `Tick` / ticker / timeout loop 属于高频热路径，默认先假设新逻辑不应该放进去
- 只有这几类事情才适合进 `Tick`：
  - deadline / TTL / backoff 到期
  - 没有可靠事件回调的跨进程结果轮询
  - 已经有显式下一次扫描时间的低频维护
- 如果某段逻辑本来可以由用户动作、agent 事件、command ack、实例上下线等明确事件触发，就不要因为“省事”塞进 `Tick`
- 新增 `Tick` 逻辑时，必须同时回答：
  - 为什么事件路径不够
  - 为什么不会在空闲周期被无意义重复执行
  - 需要什么 gating / next-check / backoff 才能把频率压下来
- 对 `Tick` 里的文件系统、网络、进程管理、提示生成类逻辑尤其谨慎；没有明确限频就不要放进去

## Git Hooks

首次在本地启用仓库自带 hook：

```bash
make install-hooks
```

当前约定：

- `pre-commit` 只跑快速、低副作用检查；实际检查项以 `scripts/check/pre-commit.sh` 为准，不再在文档里逐条展开
- `./safe-push.sh` 会在推送前补跑仓库级 Go 格式检查、公开文件本机路径泄漏检查、旧项目名回流检查和 Feishu broker guardrail，然后负责 clean worktree、同步远端、必要时补跑 `go test ./...` 后再推送；它仍不替代 `pre-commit` 的其余本地提交检查
- 提交和推送阶段都不跑 Web 构建或 release smoke；发布前仍应执行下面的完整自检

## 发布前自检

```bash
gofmt -w $(find cmd internal testkit -name '*.go' | sort)
go test ./...
bash scripts/check/no-local-paths.sh
bash scripts/check/no-legacy-names.sh
bash scripts/check/smoke-install-release.sh
```

## GitHub Actions

- `CI`
  - 检查公开文档是否泄漏本机路径
  - 检查旧项目名是否回流
  - 检查 `gofmt`
  - 跑 WebSetup release 安装器 smoke test
  - 构建并运行 `go test ./...`
- `Release`
  - 支持 `production / beta / alpha` track
  - 支持显式指定版本或按 track 自动决定下一个语义化版本
  - 在 GitHub 上构建 admin UI 与多平台产物
  - 对 `beta / alpha` 自动标记 GitHub prerelease
  - 生成 release notes、checksums 并创建 GitHub Release

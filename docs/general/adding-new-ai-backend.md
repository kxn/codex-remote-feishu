# 新增 AI 后端集成指南

> Type: `general`
> Updated: `2026-05-06`
> Summary: 面向开发者的步骤式指南，说明如何为 codex-remote-feishu 集成一个新的本地 AI 客户端（如 droid、pi.dev、opencode 等）。覆盖全部 7 个集成步骤、I/O 管道、execlaunch 要求、Capabilities 与命令显示配置的区分、测试要求，并以 Claude 后端作为完整参考实例。

---

## 概述

codex-remote-feishu 通过一个**适配器模式**支持多种 AI 客户端后端。每个后端在 wrapper 层有独立的 `backendRuntime` 实现，负责：

- 将后端的原生协议（JSON-RPC、stream-json 等）翻译为 canonical `agentproto` 事件/命令
- 管理子进程生命周期（启动、重启、退出）
- 通过 Feishu 网关暴露后端的能力

本指南以 **Claude 后端** 作为已完成的工作实例，逐步说明如何集成一个新的后端。完整架构概览见 [DEVELOPER.md](../../DEVELOPER.md)。

---

## I/O 管道总览

集成新后端前，先理解数据在系统各层间的流动路径：

```
Parent (VS Code 扩展 / headless 启动器)
  │
  ├── stdin ──→ Wrapper
  │                │
  │                ├── stdin ──→ Child AI 进程 (codex / claude / droid …)
  │                │                │  ←── stdout ──
  │                │                │
  │                │                └── stderr (日志)
  │                │
  │                └── WebSocket ──→ Daemon (relayd)
  │                                    │
  │                                    └── Feishu Gateway ──→ Feishu API
  │
  ←── stdout ──
```

**管道中的四个并发 goroutine** (`internal/app/wrapper/app_io.go`)：

1. **stdinLoop** — 读取父进程 stdin，调用 `backendRuntime.ObserveClient()` 提取事件，转发至 daemon (WebSocket) 和子进程
2. **stdoutLoop** — 读取子进程 stdout，调用 `backendRuntime.ObserveServer()` 提取事件，转发至 daemon 和父进程
3. **writeLoop** — 从 daemon 接收命令，调用 `backendRuntime.TranslateCommand()` 转换为子进程原生指令
4. **streamCopy** — 将子进程 stderr 拷贝到 wrapper 日志

**协议翻译**发生在 `ObserveClient`、`ObserveServer` 和 `TranslateCommand` 三个方法中 — 这是适配器 translator 的核心职责。

---

## 集成步骤

### 步骤 1：添加 Backend 常量

**文件：** `internal/core/agentproto/backend.go`

1. 在 `Backend` 类型中新增常量：

```go
const (
    BackendCodex  Backend = "codex"
    BackendClaude Backend = "claude"
    BackendDroid  Backend = "droid"   // <-- 新增
)
```

2. 更新 `NormalizeBackend()`，为新的 backend 值添加 case。
3. 更新 `BackendDisplayName()`，返回用户可读的后端名称。
4. 更新 `DefaultCapabilitiesForBackend()`，为新后端定义默认能力。

**Claude 参考：**
- 常量定义：`BackendClaude` (`internal/core/agentproto/backend.go`)
- 规范化和显示名：`NormalizeBackend()`、`BackendDisplayName()` 中对应 case
- 默认能力：`DefaultCapabilitiesForBackend(agentproto.BackendClaude)` 返回包含 `ThreadsRefresh`、`RequestRespond`、`SessionCatalog`、`ResumeByThreadID`、`RequiresCWDForResume` 的 `Capabilities`

---

### 步骤 2：创建适配器 Translator

**新文件：** `internal/adapter/<name>/translator.go`

Translator 是适配器的核心，负责在**原生协议**和 **canonical agentproto 类型**之间双向翻译。

**主要职责：**

- `ObserveClient(line []byte)` — 解析父进程 → 子进程方向的帧，提取 `agentproto.Event`（如 `EventTurnStarted`、`EventTurnCompleted`、`EventRequestStarted` 等）
- `ObserveServer(line []byte)` — 解析子进程 → 父进程方向的帧，提取事件
- `TranslateCommand(command agentproto.Command)` — 将 canonical 命令（如 `promptSend`、`turnSteer`、`interrupt`）转换为子进程原生帧
- `BuildChildRestartRestoreFrame(commandID)` — 构建子进程重启后恢复会话所需的帧

**关键类型：** `Result` 结构体包含 `Events`、`OutboundToChild`、`OutboundToParent`、`Suppress` 四个字段，控制事件分发和原始帧透传。

**Claude 参考：**
- `internal/adapter/claude/translator.go` — Translator 结构体定义、`ObserveClient`/`ObserveServer`/`TranslateCommand` 实现
- `internal/adapter/claude/observe.go` — 帧解析逻辑
- `internal/adapter/claude/observe_task_lifecycle.go` — turn/thread 生命周期事件提取
- `internal/adapter/claude/commands.go` — 命令翻译
- `internal/adapter/claude/session_catalog.go` — 会话目录事件

---

### 步骤 3：实现 backendRuntime

**文件：** `internal/app/wrapper/backend_runtime.go`

`backendRuntime` 接口定义了 wrapper 与后端之间的契约：

```go
type backendRuntime interface {
    Backend() agentproto.Backend
    Capabilities() agentproto.Capabilities
    Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error)
    ObserveClient([]byte) (runtimeObserveResult, error)
    ObserveServer([]byte) (runtimeObserveResult, error)
    TranslateCommand(agentproto.Command) (runtimeCommandResult, error)
    PrepareChildRestart(string, agentproto.Target) error
    BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error)
    CancelChildRestartRestore(string)
}
```

**需要实现的内容：**

1. 创建一个新的 `*BackendRuntime` 结构体（如 `droidBackendRuntime`），内含 translator 实例和必要的启动状态
2. 实现所有接口方法
3. 在 `newBackendRuntime(cfg Config)` 函数中添加 case，将 `agentproto.BackendDroid` 映射到新 runtime

**Claude 参考：**
- `claudeBackendRuntime` 结构体 (`internal/app/wrapper/backend_runtime.go:168-208`)
- `newBackendRuntime()` 中的 `case agentproto.BackendClaude:` 分支 (`backend_runtime.go:42-52`)
- `Capabilities()` 返回 `agentproto.DefaultCapabilitiesForBackend(agentproto.BackendClaude)`
- `Launch()` 委托给 `app.launchClaudeChildSession()`

---

### 步骤 4：创建子进程启动逻辑

**新文件：** `internal/app/wrapper/app_child_session_<name>.go`

**核心要求：必须使用 `internal/execlaunch` 包而非裸 `os/exec`**。

```go
import "github.com/kxn/codex-remote-feishu/internal/execlaunch"

cmd := execlaunch.CommandContext(childCtx, binaryPath, args...)
cmd.Dir = workspaceRoot
cmd.Env = childEnv
```

`execlaunch` 包会应用仓库统一的子进程启动配置（如 Windows 上隐藏控制台窗口），所有生产代码的 `exec.Command` 或 `exec.CommandContext` 调用都应通过它。

**职责：**

- 解析后端二进制路径（支持环境变量覆盖）
- 构建命令行参数和子进程环境
- 启动子进程并建立 stdin/stdout 管道
- 执行引导握手（bootstrap），等待子进程就绪
- 返回 `*childSession`（含 cmd、stdin、stdout、stderr、cancel 等）

**Claude 参考：**
- `internal/app/wrapper/app_child_session_claude.go` — `launchClaudeChildSession()` 完整实现
- `internal/app/wrapper/app_child_settings_claude.go` — Claude 运行时设置覆盖（profile、reasoning effort 等）
- `internal/app/wrapper/app_process.go` — `startChild()` 通用子进程启动辅助函数

---

### 步骤 5：接入入口调度

**文件：**

- `internal/app/wrapper/entry.go` — wrapper 入口
- `internal/app/launcher/role.go` — 统一二进制角色调度
- `internal/app/launcher/launcher.go` — 使用说明文本

**需要添加的内容：**

1. 在 `entry.go` 的 `RunMain()` 中添加新的 entry 模式：

```go
case args[0] == "droid-app-server":
    backend = agentproto.BackendDroid
```

2. 在 `role.go` 的 `isWrapperMode()` 中添加新模式名称
3. 更新 `role.go` 的 dispatch 逻辑，将新模式路由到 `RoleWrapper`
4. 更新 `launcher.go` 的使用说明文本，添加 `codex-remote droid-app-server` 条目

**Claude 参考：**
- `internal/app/wrapper/entry.go:20-21` — `"claude-app-server"` 映射到 `agentproto.BackendClaude`
- `internal/app/launcher/role.go` — `isWrapperMode()` 中 `"claude-app-server"` case
- `internal/app/launcher/launcher.go` — 使用说明文本中的 `codex-remote claude-app-server`

---

### 步骤 6：定义命令显示配置 (FeishuCommandDisplayProfile)

**文件：** `internal/core/control/feishu_command_display_profiles.go`

`FeishuCommandDisplayProfile` 定义当 wrapper 关联到 Feishu 会话时，**哪些斜杠命令可见、其支持类型、以及在菜单的哪些阶段显示**。这是一个纯 Feishu 前端配置层，与控制后端实际能力的 `Capabilities` 是**两个独立系统**（详见下文「Capabilities 与 FeishuCommandDisplayProfile 的区分」）。

**要添加的内容：**

1. 在 `feishuCommandDisplayProfiles` map 中添加新条目（如 `"droid"`）
2. 定义哪些命令 `Visible`、其 `SupportKind`（`native` / `approximation` / `passthrough` / `reject`）以及拒绝时的提示文字
3. 如果新后端应该与其他后端共享已有的 profile key（如 `"codex"`），则不需要新建 profile

**Claude 参考：**
- `internal/core/control/feishu_command_display_profiles.go` — `"claude"` profile 定义（约 line 50-80），包含 `commandSupportVisible()`、`commandSupportHiddenReject()`、`commandSupportVisibleAs()` 等辅助函数
- 关键差异：Claude profile 的 `DefaultDispatchAllowed: false`（默认拒绝未显式列出的命令），而 Codex profile 是 `DefaultDispatchAllowed: true`

---

### 步骤 7：添加配置与环境变量支持

**文件：**

- `internal/config/<name>_profiles.go` — 后端配置结构体、profile 定义、环境变量常量
- `internal/config/configfile.go` — 在全局配置结构体中添加新的后端配置字段
- `.env.example` — 若后端有常用环境变量，在 wrapper.env 或 services.env 节中添加示例

**Claude 参考：**
- `internal/config/claude_profiles.go` — `ClaudeProfile`、`ClaudeProfileConfig` 结构体，`ClaudeBaseURLEnv`、`ClaudeAuthTokenEnv` 等环境变量常量，`ApplyClaudeProfileLaunchEnv()` 运行时环境设置
- `internal/config/claude_runtime_settings.go` — `ClaudeRuntimeSettings` 和运行时覆盖逻辑
- `internal/config/configfile.go` — `Claude ClaudeSettings` 字段（约 line 100）
- `internal/app/wrapper/app.go:148-151` — 从环境变量读取 `CODEX_REMOTE_INSTANCE_BACKEND`、`CODEX_REMOTE_CLAUDE_PROFILE_ID`、`CLAUDE_CODE_EFFORT_LEVEL` 等

---

## Capabilities 与 FeishuCommandDisplayProfile 的区分

这是集成新后端时最容易混淆的地方。它们是**两个独立系统**，服务于不同的目的：

### `agentproto.Capabilities`（状态机行为声明）

**位置：** `internal/core/agentproto/wire.go` 中的 `Capabilities` 结构体

**用途：** 告知 daemon 和 orchestrator 该后端的**运行时能力** — 影响 state machine 如何路由和处理事件。

| 字段 | 含义 |
|---|---|
| `ThreadsRefresh` | 支持刷新线程列表 |
| `TurnSteer` | 支持在已开始 turn 中插入新指令（Codex 原生支持，Claude 不支持） |
| `RequestRespond` | 支持向用户发出请求并等待回复 |
| `SessionCatalog` | 支持列出所有会话 |
| `ResumeByThreadID` | 支持通过 thread ID 恢复会话 |
| `RequiresCWDForResume` | 恢复时需要提供工作目录 |
| `VSCodeMode` | 支持 VS Code 编辑器模式（Codex 原生，Claude 无） |

**定义方式：** `DefaultCapabilitiesForBackend()` 为每个后端返回默认值。

### `FeishuCommandDisplayProfile`（Feishu 前端命令可见性配置）

**位置：** `internal/core/control/feishu_command_display_profiles.go`

**用途：** 控制在 Feishu 消息中**哪些命令可见**，以及**执行时的行为类型**。

| 概念 | 含义 |
|---|---|
| `Visible` | 该命令在帮助/菜单中是否可见 |
| `SupportKind` | `native`（原生支持）、`approximation`（近似实现）、`passthrough`（透传）、`reject`（拒绝） |
| `MenuStages` | 在菜单的哪些阶段出现（如正常对话、VS Code 工作阶段） |
| `DispatchAllowed` | 是否允许通过 Feishu 发送该命令到后端 |

**两者关系：** 一个后端可以能力很强但只暴露少量 Feishu 命令（如 Claude 隐藏了 auto-whip、auto-continue、cron），也可以能力有限但 Feishu 上显示完整菜单（后端在收到不支持的命令时返回错误）。

---

## 测试要求

集成新后端时，**至少**需要创建以下三类测试。更多测试策略细节请参考 [go-test-strategy.md](./go-test-strategy.md)。

### 1. 适配器 Translator 测试

**位置：** `internal/adapter/<name>/translator_test.go`

**覆盖目标：**

- 原生协议帧 → canonical 事件翻译
- canonical 命令 → 原生帧翻译
- 边界情况（空帧、格式错误、中断、恢复）

**Claude 参考：**
- `internal/adapter/claude/translator_test.go`
- `internal/adapter/claude/translator_steer_test.go`
- `internal/adapter/claude/translator_token_usage_test.go`

### 2. backendRuntime 测试

**位置：** `internal/app/wrapper/backend_runtime_test.go`（或独立的 `<name>_runtime_test.go`）

**覆盖目标：**

- `Backend()` / `Capabilities()` 返回正确的值
- `Launch()` 调用正确的子进程启动函数
- runtime 作为 `runtimeDebugLogger` 接口的实现是否正确
- 重启恢复逻辑

**Claude 参考：**
- `internal/app/wrapper/backend_runtime_test.go`

### 3. 命令显示配置测试

**位置：** `internal/core/control/feishu_command_display_profiles_test.go`（或 `internal/core/control/feishu_command_display_resolver_test.go`）

**覆盖目标：**

- 新 profile 的正确注册与解析
- 特定命令在 profile 中的可见性和支持类型
- 菜单阶段可见性边界情况

**Claude 参考：**
- `internal/core/control/feishu_command_display_resolver_test.go`

### 4. 配置加载测试（如适用）

**位置：** `internal/config/<name>_profiles_test.go`

**覆盖目标：**

- profile 序列化/反序列化
- 运行环境变量应用
- 默认值逻辑

**Claude 参考：**
- `internal/config/claude_profiles_test.go`
- `internal/config/claude_runtime_settings_test.go`

---

## 文件清单速查

| 步骤 | 文件路径 | 操作 |
|---|---|---|
| 1 | `internal/core/agentproto/backend.go` | 修改：添加 Backend 常量 |
| 2 | `internal/adapter/<name>/translator.go` | 新建：适配器 Translator |
| 2 | `internal/adapter/<name>/observe.go` | 新建（可选的拆分）：帧解析 |
| 2 | `internal/adapter/<name>/commands.go` | 新建（可选的拆分）：命令翻译 |
| 2 | `internal/adapter/<name>/session_catalog.go` | 新建（可选）：会话目录 |
| 3 | `internal/app/wrapper/backend_runtime.go` | 修改：添加 runtime 结构体和工厂函数 |
| 4 | `internal/app/wrapper/app_child_session_<name>.go` | 新建：子进程启动 |
| 4 | `internal/app/wrapper/app_child_settings_<name>.go` | 新建（可选）：运行时设置覆盖 |
| 5 | `internal/app/wrapper/entry.go` | 修改：添加 entry 模式 |
| 5 | `internal/app/launcher/role.go` | 修改：添加角色路由 |
| 5 | `internal/app/launcher/launcher.go` | 修改：更新使用说明 |
| 6 | `internal/core/control/feishu_command_display_profiles.go` | 修改：添加命令显示配置 |
| 7 | `internal/config/<name>_profiles.go` | 新建：配置结构与 env 常量 |
| 7 | `internal/config/configfile.go` | 修改：添加配置字段 |
| 7 | `.env.example` | 修改：添加环境变量示例 |

---

## 参考文档

- [DEVELOPER.md](../../DEVELOPER.md) — 架构概览、构建系统、配置系统
- [go-test-strategy.md](./go-test-strategy.md) — 测试策略与通过标准
- [architecture.md](./architecture.md) — 系统组件与交互
- [relay-protocol-spec.md](./relay-protocol-spec.md) — canonical 协议规范
- [backend.go](../../internal/core/agentproto/backend.go) — Backend 类型与默认能力定义
- [backend_runtime.go](../../internal/app/wrapper/backend_runtime.go) — backendRuntime 接口与实现
- [feishu_command_display_profiles.go](../../internal/core/control/feishu_command_display_profiles.go) — 命令显示配置
- [execlaunch](../../internal/execlaunch/command.go) — 子进程启动工具

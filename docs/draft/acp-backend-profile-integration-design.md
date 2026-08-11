# ACP 后端与实例 profile 接入设计调研

> Type: `draft`
> Updated: `2026-08-11`
> Summary: 同步当前 Codex Profile-only 基线：核心合同使用 `CodexProfileID`，不再以 `CodexProviderID` 描述当前状态。

## 1. 文档定位

这份文档回答四个问题：

1. 以 OpenCode 的 `opencode acp` 为第一候选，ACP 能否承接当前 Feishu/headless 后端能力。
2. 当前 Codex/Claude 封装里哪些抽象可以复用，哪些需要升级为通用 ACP 概念。
3. OpenCode/Kimi ACP 与我们需求的差异是什么，应该在 adapter、canonical 协议、orchestrator、Feishu 交互哪一层处理。
4. OpenCode/Kimi 是否满足实例级临时 profile、局部覆盖、继承现有配置这个非 ACP 硬门槛。

调研对象：

- Kimi Code 官方 `kimi acp` 文档：<https://www.kimi.com/code/docs/en/kimi-code-cli/reference/kimi-acp.html>
- Kimi Code GitHub 源码：<https://github.com/MoonshotAI/kimi-code>
- 本次源码快照：`MoonshotAI/kimi-code@01c74e9372fcbbbe99614e859b53b505ed1664a8`
- OpenCode GitHub 源码：<https://github.com/sst/opencode>
- 本次源码快照：`sst/opencode@fe82a1b6ca4f535beb973b0867017e3f639f85ed`
- ACP v1 官方文档：<https://agentclientprotocol.com/protocol/v1/overview>
- 既有仓库文档：`docs/draft/acp-claude-integration-design.md`、`docs/general/adding-new-ai-backend.md`

本文不是 PoC 计划。目标是先把抽象和风险想清楚，避免把任一 ACP agent 做成第三套一次性 backend。

## 2. 结论

ACP 后端接入值得做，但第一条真实 profile 不应只按 ACP 协议完整度选择，还必须满足“实例级临时 profile / 局部覆盖 / 继承现有配置”这个非 ACP 硬门槛。

按当前源码调研，OpenCode 比 Kimi Code 更适合作为第一条 ACP 接入样本：

- OpenCode 有 `OPENCODE_CONFIG_CONTENT`，会在全局配置、显式配置、项目配置、`.opencode` 目录之后最后 merge，天然支持“继承现有配置，只覆盖本实例少数字段”。
- OpenCode 有 `OPENCODE_AUTH_CONTENT`，可以为单实例注入 provider auth，避免改写全局 `auth.json`。
- OpenCode 的 ACP subprocess 测试已经用 `OPENCODE_CONFIG_CONTENT` 注入 verifier provider，说明这条路径覆盖真实 `opencode acp` 启动链路。
- Kimi Code 有 `KIMI_CODE_HOME` 和若干 env/CLI override，但 `KIMI_CODE_HOME` 会搬走整个数据根；如果不复制用户 config，就不能继承；如果复制，又会把凭据、sessions、logs、skills、MCP 等一起带入临时目录，所有权和刷新语义都很差。

推荐设计是：

- 新增一个通用 `acp` 后端族。
- OpenCode 是 `acp` 后端族下的第一候选 `ACPAgentProfile`，启动命令为 `opencode acp`。
- Kimi Code 保留为第二候选，除非后续确认它支持 config overlay 文件或 inline config override，否则不应作为默认落地目标。
- wrapper 内部新增 `ACPRuntimeAdapter`，负责 ACP JSON-RPC、session 生命周期、request id 关联、replay 分类、config option 映射。
- `relay.agent.v1` 主干不改，先通过扩展 `agentproto` 的事件/命令承接 ACP 特有能力。
- 第一阶段仍定位为 Feishu/headless 后端，不复用当前 VS Code Codex native shim。

Kimi ACP 对我们有利的地方：

- stdio JSON-RPC 入口清晰，`kimi acp` 启动后不打印 banner，日志走 stderr。
- 支持 `initialize`、`authenticate`、`session/new`、`session/load`、`session/resume`、`session/prompt`、`session/cancel`、`session/list`、`session/set_config_option`。
- 支持 `session/request_permission`，且 approval option 已是 ACP 动态选项，适合驱动 Feishu 审批卡片。
- `session/resume` 明确不 replay 历史，正好匹配我们恢复会话时不能刷屏的需求。
- 支持 `configOptions`，能把 model、thinking、mode 做成统一动态配置。

OpenCode ACP 对我们额外有利的地方：

- `opencode acp` 已实现 stdio JSON-RPC server，能力包含 `loadSession`、`sessionCapabilities.list/resume/close/fork`、MCP http/sse、prompt embeddedContext/image。
- `session/load` 同样会 replay messages，`session/resume` 存在，能复用同一套 hydration / resume 策略。
- `configOptions` 暴露 `model`、`effort`、`mode`，比 Kimi 的 `model`、`thinking`、`mode` 只是命名不同，适合验证 dynamic config 抽象。
- `available_commands_update` 会从 command list 和 skills 生成，作为 backend command catalog 的验证价值高于 Kimi 当前的空更新。

主要差异和坑：

- `session/load` 会 replay 历史。adapter 必须优先用 `session/resume`；fallback 到 `load` 时必须把 replay traffic 标为 hydration，orchestrator/renderer 默认不投影。
- Kimi 文档声明 MCP http/stdio/sse 转发，但源码注释指出 v2 engine 没有 caller `mcpServers` channel。这不能作为稳定产品承诺。
- Kimi 的 `available_commands_update` 当前可能是确定性的空更新，不能立刻替代我们的 Feishu 命令目录。
- Kimi 的 terminal reverse-RPC 未接入，shell 命令由 Kimi 本地执行。我们不能期待 ACP terminal widget 级体验。
- Kimi 的 `authenticate` 只验证磁盘 token，真实登录要客户端另起 terminal auth。Feishu/headless 初版应要求主机预先登录。
- ACP prompt content 比当前 `agentproto.Input` 更宽，有 `resource` / `resource_link` / audio。Kimi 会丢弃 audio、blob resource，我们 adapter 也应显式降级并记录 protocol notice。
- OpenCode 的 `authenticate` 当前主要是协议层确认动作，真实 provider auth 仍依赖 config/auth 层；初版仍应做启动前 profile health check。
- OpenCode 的项目配置默认参与 merge。我们要明确是否允许工作区 `.opencode` 影响 Feishu 实例；需要隔离时设置 `OPENCODE_DISABLE_PROJECT_CONFIG=1`。

## 3. 当前抽象评估

### 3.1 现有后端抽象

当前新增后端指南把集成拆成 7 步：加 `Backend` 常量、建 translator、实现 `backendRuntime`、写 child launch、接入口、配置 Feishu command display profile、加配置和环境变量。

这个模式适合 Claude 这种独立 native protocol，但如果照搬给 Kimi，会导致后续 OpenCode、Droid、Pi.dev 每个 ACP agent 都复制一套 runtime：

- `internal/core/agentproto/backend.go` 现在只有 `codex` / `claude`，未知值默认归 Codex。
- `internal/app/wrapper/backend_runtime.go` 把 native translator、child launch、restart/resume 逻辑绑在一个 backend-specific runtime 里。
- `internal/core/state/surface_backend.go` 的 surface contract 已有 `CodexProfileID`、`ClaudeProfileID` 和 `OpenCodeProfileID` 这些 profile 轨道。
- `internal/core/control/feishu_command_display_profiles.go` 是按 visible mode / backend profile 写死命令暴露规则。

这些不是错误，只是说明当前抽象层级是“产品 backend”，不是“协议 backend + agent profile”。

### 3.2 agentproto 的可复用部分

当前 `agentproto` 足够承接 ACP 基础主链：

| ACP | 当前 canonical | 结论 |
| --- | --- | --- |
| `session/list` | `threads.snapshot` | 可复用，需保留 cursor/unsupported metadata |
| `session/new` | `prompt.send` with start-new dispatch / `thread.discovered` | 可复用 |
| `session/resume` | `thread.focused` / resume restore | 可复用，Kimi 首选 |
| `session/load` | `thread.focused` + hydration replay | 需要新增 hydration 标记 |
| `session/prompt` | `prompt.send` -> `turn.started` / item stream / `turn.completed` | 可复用 |
| `session/cancel` | `turn.interrupt` | 可复用 |
| `session/request_permission` | `request.started` / `request.respond` | 可复用，但 option 要动态 |
| `agent_message_chunk` | `item.started` / `item.delta` / `item.completed` | 可复用 |
| `agent_thought_chunk` | reasoning item / hidden metadata | 需产品决策，默认不主动投影 |
| `tool_call*` | item/tool events | 可复用，展示需降级 |
| `plan` | `turn.plan.updated` | 可复用 |
| `usage_update` | `thread.token_usage.updated` | 部分可复用，cost 需 metadata |

### 3.3 agentproto 需要新增或泛化的部分

推荐新增：

1. `BackendACP`
   - 值为 `acp`。
   - 表示 child protocol family，不表示具体 vendor。
   - `BackendDisplayName` 对 `acp` 默认返回 profile display name，缺 profile 时为 `ACP`。

2. `ACPProfileID`
   - 加入 wrapper config、instance hello、surface backend contract、launch contract。
   - 第一版 profile 字段可包含：`id`、`name`、`command`、`args`、`env`、`workingDirectoryMode`、`authMode`、`implementation`。
   - OpenCode profile 默认 `command = opencode`、`args = ["acp"]`；Kimi profile 默认 `command = kimi`、`args = ["acp"]`。

3. `TrafficClassHydration`
   - 和现有 `TrafficClassInternalHelper` 同级。
   - 用于标记 `session/load` 历史 replay。
   - renderer / Feishu projector 默认不发 hydration item，但 state store 可以选择更新本地 transcript cache。

4. `config.options.updated`
   - 承接 ACP `config_option_update` 和 `session/new/load/resume` response 中的 `configOptions`。
   - 不能硬塞进当前 `config.observed(model/reasoning/access)`，因为 ACP config 是动态 schema。

5. `session.config.set`
   - wrapper 翻译为 `session/set_config_option`。
   - Feishu 菜单后续从动态 config options 派生，而不是写死 `/model`、`/reasoning`、`/access`。

6. `commands.available.updated`
   - 承接 ACP `available_commands_update`。
   - 第一版只入 state，不直接替代 FeishuCommandDisplayProfile。

7. 更泛化的 prompt input metadata
   - 当前 `Input` 只有 text/local_image/remote_image。
   - ACP `resource` / `resource_link` 初版可降级成 text block，但要保留 `metadata.acpDroppedContent` / `protocol.notice`。

## 4. 推荐架构

### 4.1 分层目标

保持现有稳定中枢：

```text
Feishu / admin / queue / state
        |
relayd + orchestrator
        |
relay.agent.v1
        |
wrapper host
        |
ACPRuntimeAdapter
        |
OpenCode: `opencode acp` / Kimi Code: `kimi acp`
```

`relay.agent.v1` 不替换成 ACP。ACP 只作为 wrapper 到 child agent 的 native protocol。

### 4.2 wrapper host 与 adapter 拆分

现有 `backendRuntime` 已经接近可插拔接口，但仍承载太多 backend-specific 细节。建议分成两层：

```go
type AgentRuntimeAdapter interface {
    Backend() agentproto.Backend
    DisplayName() string
    Capabilities() agentproto.Capabilities
    ObserveParentFrame([]byte) (runtimeObserveResult, error)
    ObserveChildFrame([]byte) (runtimeObserveResult, error)
    TranslateCommand(agentproto.Command) (runtimeCommandResult, error)
    PrepareChildRestart(string, agentproto.PromptDispatchPlan, *agentproto.CodexResumePolicy) error
    BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error)
    CancelChildRestartRestore(string)
}

type ChildLauncher interface {
    Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error)
}
```

现有 Codex/Claude 可以先通过 thin wrapper 适配这个接口，不必一次性大重构。ACP 新实现则直接使用 `ACPRuntimeAdapter + ACPChildLauncher`。

### 4.3 ACP adapter 内部状态

`ACPRuntimeAdapter` 至少需要维护：

- JSON-RPC request id 分配和 pending map。
- `initialize` 结果，包括 negotiated protocol version、agent capabilities、auth methods。
- ACP `sessionId` 到 canonical `threadId` 的映射。
- 当前 active prompt request id 到 canonical `turnId` 的映射。
- `session/load` hydration window。
- tool call id 到 canonical item id 的映射。
- permission request id 到 canonical request id 的映射。
- config options snapshot。
- available commands snapshot。

不要用“当前线程下一条消息”或时间窗口推断 replay/helper 流量。所有分类必须由 request id、session id、prompt id、load/resume 状态显式关联。

## 5. Kimi ACP 能力差异与处理结论

### 5.1 启动与初始化

Kimi 文档说明 `kimi acp` 通过 stdin/stdout JSON-RPC 通信，启动后不打印 banner，日志写 stderr 和本地日志目录。这与 wrapper 的 child stdio 模型匹配。

处理结论：

- 新增 `internal/app/wrapper/app_child_session_acp.go`，用 `execlaunch.CommandContext` 启动 profile command。
- launch 后由 ACP adapter 发送 `initialize`，不要复用 Codex synthetic initialize。
- 初始化失败时以 `system.error` 上报，并保留 stderr log path。
- `initialize.authMethods` 中如果只提供 terminal auth，Feishu 初版不尝试登录，只提示需要在主机执行 `kimi login`。

### 5.2 Session new / list / resume / load

Kimi 支持 `session/list`，但 `nextCursor` 当前恒为 `null`，即没有分页。`session/load` 会同步 replay 历史；`session/resume` 不 replay。

处理结论：

- `/list` 调 `session/list`，`cwd` 按当前 workspace 过滤；没有分页时一次性转 `threads.snapshot`。
- `/use` 和 headless restore 优先调 `session/resume`。
- 只有当 target agent 不支持 `sessionCapabilities.resume` 时才 fallback 到 `session/load`。
- fallback 到 `load` 必须开启 hydration window：从发出 `session/load` request 到收到 response 之间的 `session/update` 全部标 `TrafficClassHydration`。
- Feishu 不投影 hydration replay；最多发一条状态提示“已恢复历史会话”。

### 5.3 Prompt 与 turn 生命周期

ACP `session/prompt` 是 request/response 方法，流式内容通过 `session/update` 通知回来，最终 response 带 `stopReason`。Kimi 将 `completed` 映射为 `end_turn`，`cancelled` 映射为 `cancelled`，部分失败会降级成 `end_turn` 或 `refusal`。

处理结论：

- wrapper 发送 `session/prompt` 时立即生成 canonical `turn.started`，并把 JSON-RPC request id 绑定到 turn。
- `agent_message_chunk` 映射为 assistant item delta。
- `agent_thought_chunk` 默认作为 reasoning metadata 或隐藏 item，不直接发 Feishu 主消息。
- `session/prompt` response 到达时生成 `turn.completed`。
- `stopReason = cancelled` 映射为 interrupted/cancelled；`refusal` 映射为 completed with refusal metadata；`end_turn` 但期间有 error notice 时保留 protocol notice。

### 5.4 Cancel

ACP `session/cancel` 是 notification，规范上没有响应。Kimi 对未知 session 只 log warn。

处理结论：

- `turn.interrupt` 翻译为 `session/cancel` notification。
- relay 不能等待 command response gate；应该以本地 command ack 表示“已发出取消”。
- 最终状态仍以 `session/prompt` response 或后续事件为准。
- 如果超时未收到 turn completion，由 wrapper 生成 runtime reconciliation，避免 Feishu 卡在 running。

### 5.5 Approval 与用户输入

Kimi 将普通工具 approval 转为三类 option：approve once、approve for session、reject。plan review 会生成 plan-specific option，包括 revise / reject and exit。问题询问也复用 `session/request_permission`，并且 Kimi 源码里多问题、多选会降级。

处理结论：

- `RequestPrompt.Options` 必须支持动态 option，不能写死 accept/decline/cancel。
- ACP option id 应原样 round-trip，Feishu label 只负责展示。
- `kind = allow_once / allow_always / reject_once` 可映射 Feishu button style，但业务语义不能靠 label 推断。
- plan review 归入现有 `plan_confirmation` presentation，但 option 从 ACP prompt 带出。
- Kimi 的 question 复用 permission channel；我们 adapter 应把它映射成 `RequestTypeRequestUserInput` 或 `mcp_elicitation` 风格的动态表单，而不是工具审批。

### 5.6 Config options

Kimi 使用 `SessionConfigOption[]` 暴露 model、thinking、mode：

- `model`：category `model`
- `thinking`：category `thought_level`
- `mode`：category `mode`

处理结论：

- 不要把 Kimi model/thinking/mode 强行塞进 Codex provider 或 Claude profile。
- 新增 `config.options.updated` 后，Feishu 菜单可以逐步从动态配置生成。
- 第一版 `/model`、`/reasoning`、`/access` 可以只作为兼容入口，内部查当前 ACP config option 是否存在：
  - `/model` -> `configId = "model"`
  - `/reasoning` -> `configId = "thinking"`
  - `/access` 或 `/plan` -> `configId = "mode"`，但要按 Kimi mode 语义明确映射
- 如果 option 不存在，返回 backend-specific reject note，而不是静默忽略。

### 5.7 MCP forwarding

Kimi 文档声明 ACP request 中的 `mcpServers` 支持 http/stdio/sse，丢弃 acp transport。源码中 `server.ts` 注释指出 v2 engine 当前没有 caller `mcpServers` channel，如何让 ACP-supplied servers 进入 v2 engine 留给未来设计。

处理结论：

- 初版不要承诺“Feishu 动态注入 MCP 到 Kimi ACP session”。
- 可以把当前 workspace/配置文件已有 MCP 交给 Kimi 自己处理。
- 如果我们确实传 `mcpServers`，必须在 status/debug 中显示“best effort”，并记录被丢弃的 `acp` transport。
- MCP tool approval 仍走 `session/request_permission`，不要求我们直接理解 Kimi 内部 MCP 配置。

### 5.8 File system reverse-RPC

Kimi 支持 `fs/read_text_file` / `fs/write_text_file` reverse-RPC，前提是 client 在 initialize 中声明 fs capability。

处理结论：

- Feishu/headless wrapper 初版不应声明 ACP fs capability，除非我们准备把所有读写通过 relay/orchestrator 文件服务代理。
- 不声明 fs 时，Kimi 会使用本地文件系统执行工具，这更接近现有 headless 运行方式。
- 将来如果要声明 fs capability，必须先设计 path authorization、workspace root enforcement、large file limits 和 audit log。

### 5.9 Terminal reverse-RPC

Kimi 文档明确 terminal reverse-RPC 未连接，shell commands use local execution。

处理结论：

- 我们不实现 ACP terminal client。
- Feishu 只展示 tool_call/tool_call_update 文本摘要、命令名、状态和结果。
- 不把 terminal widget 当作 ACP 接入的验收项。

### 5.10 Available commands

Kimi 源码说明 adapter 当前没有接入 TUI slash-command registry，会发送确定性空的 `available_commands_update`，未来上层可填。

处理结论：

- 第一版不让 Kimi `available_commands_update` 驱动 Feishu 主命令菜单。
- 仍使用 `FeishuCommandDisplayProfile` 控制 `/stop`、`/list`、`/use`、`/mode`、`/status` 等产品命令。
- `available_commands_update` 入 state，后续可用于“backend slash command”二级入口，不影响现有 catalog。

## 6. OpenCode ACP 能力差异与处理结论

### 6.1 启动与初始化

OpenCode 的 `opencode acp` 会启动内部 HTTP server，再用 SDK 连接这个 server，把外部 stdio 转成 ACP JSON-RPC。初始化返回：

- `protocolVersion = 1`
- `agentCapabilities.loadSession = true`
- `sessionCapabilities.close/fork/list/resume`
- `mcpCapabilities.http/sse`
- `promptCapabilities.embeddedContext/image`
- `authMethods = opencode-login`

处理结论：

- OpenCode profile 启动命令为 `opencode acp --cwd <workspace>`。
- wrapper 不需要理解内部 HTTP server，只和 stdio ACP 连接交互。
- 初版可以声明不支持 ACP terminal-auth，由 wrapper 做启动前 health check；缺 provider auth 时返回可操作错误，提示本机运行 `opencode auth login` 或配置 OpenCode profile 的私有 auth launch material。

### 6.2 Config overlay 与 auth overlay

OpenCode 当前生产 loader 会在最后加载 `OPENCODE_CONFIG_CONTENT`，并且 `OPENCODE_AUTH_CONTENT` 会优先于磁盘 `auth.json`。

处理结论：

- `OpenCodeProfileCompiler` 可生成 `OPENCODE_CONFIG_CONTENT`。
- `OpenCodeProfileCompiler` 可按认证策略生成 secret env 或 `OPENCODE_AUTH_CONTENT`。
- wrapper 不修改用户 `opencode.json/jsonc`、`.opencode`、`auth.json`。
- `ProjectConfigMode = inherit` 时不设置 `OPENCODE_DISABLE_PROJECT_CONFIG`；`ignore` 时设置为 `1`。
- `DataIsolationMode` 默认不要改 `XDG_*`，以便继承用户 auth/session/log；只有用户明确要求隔离运行数据时才注入实例级 XDG roots。

### 6.3 Session new / list / resume / load / fork

OpenCode `session/list` 会从 server session list 和 ACP live session map 合并结果，按 `updatedAt` 倒序返回，并用时间戳 cursor 分页。`session/load` 会 replay 全量 messages；`session/resume` 只读最近 messages 恢复 model/variant/mode 状态，不 replay。

处理结论：

- `/list` 可以使用 cursor，不能假设所有 ACP agent 都无分页。
- `/use` 和 wrapper restore 同样优先 `session/resume`。
- `session/load` 和 `session/fork` 的 replay 都标为 `TrafficClassHydration`，默认不投影到 Feishu。
- `session/fork` 可以先进入 state，但 Feishu 初版不一定暴露命令。

### 6.4 Cancel 与 close

OpenCode `session/cancel` 会取当前 ACP session，再调用 backing session abort。`session/close` 会从 ACP live session map 移除，并 abort backing session。

处理结论：

- `/stop` 映射 `session/cancel`，仍按 ACP notification 语义处理：本地 ack 只表示取消已发出，最终状态看后续 turn completion/error。
- 如果将来暴露 close，需要和我们当前 detach/use 语义区分；close 是 backend session 生命周期操作，不是 Feishu surface detach。

### 6.5 Dynamic config options

OpenCode `configOptions` 包含：

- `model`：category `model`
- `effort`：category `thought_level`
- `mode`：category `mode`

`session/set_config_option` 只接受 string value，并显式校验 model、effort、mode；未知 config id 会返回 invalid config option。

处理结论：

- `/model` 直接映射 `configId = "model"`。
- `/reasoning` 映射 `configId = "effort"`，而不是 Kimi 的 `thinking`。
- `/plan` 或 `/access` 映射 `configId = "mode"` 前要使用 OpenCode agent/mode 的显示名和描述，不沿用 Codex sandbox 文案。
- 通用 adapter 不应写死 Kimi 的 `thinking` id；只能按 ACP option category 和当前 profile alias 做兼容入口。

### 6.6 Available commands 与 skills

OpenCode directory snapshot 会并行读取 providers、agents、commands、skills、default model，并把 commands 与 skills 合并成 ACP available commands。

处理结论：

- OpenCode 比 Kimi 更适合验证 `commands.available.updated`，因为它不是确定性空更新。
- 第一版仍不让 backend commands 替代 Feishu 产品命令，但可以做二级入口或 debug/status 展示。
- skills 可能来自用户配置、项目 `.opencode` 和 overlay；因此 command catalog 必须带来源和 profile id，避免跨实例污染。

### 6.7 MCP

OpenCode initialize 声明 MCP http/sse capability，new/load/resume/fork 都接受 `mcpServers` 并注册到 backing session。

处理结论：

- OpenCode 可以作为 ACP MCP injection 的第一验证目标。
- 初版仍应保守：只注册我们能审计来源和生命周期的 MCP server。
- remote MCP OAuth 仍是 OpenCode 自己的 auth 子系统；Feishu 初版只展示需要认证，不接管完整 OAuth。

## 7. Feishu 产品行为建议

新增一个 `acp` display profile，初始可见命令：

- `/stop`：native，映射 `session/cancel`。
- `/list`、`/use`：native，映射 `session/list`、`session/resume`。
- `/new`：native/approximation，映射 `session/new`。
- `/status`、`/help`、`/menu`、`/debug`：沿用产品层。
- `/model`：dynamic，只有当前 config option 有 `model` 时可用。
- `/reasoning`：dynamic，只有当前 config option 有 `thinking` / `thought_level` 时可用。
- `/plan`、`/access`：dynamic，映射 `mode` 时必须显示当前 ACP profile 的 mode 名称，不要沿用 Codex sandbox 文案。
- `/workspace*`：产品层可用，但 workspace 选择后只影响 ACP `cwd` / launch contract。
- `/review`、`/compact`、`/patch`、`/auto-continue`、`/auto-whip`、`/cron`：第一版隐藏并拒绝，等 ACP command bridge 或独立 contract 补齐。

审批卡片：

- 优先使用 ACP option 的 `name`。
- option id 原样回传。
- option `kind` 只影响样式，不影响业务判断。
- 如果 option 数量超过 Feishu 卡片按钮限制，降级为表单选择。

## 8. 非 ACP 硬门槛：实例级临时 profile / 配置继承

这个要求不属于 ACP 协议，但属于我们选择后端的硬门槛：

- 同一台机器上可以同时运行多个实例。
- 每个实例可以有不同 profile，例如模型、agent/mode、权限、MCP、身份、凭据来源。
- 只覆盖我们明确指定的字段。
- 没覆盖的字段继承用户当前系统/全局/项目配置。
- 启动、切换、销毁实例时不修改用户原有配置文件。

### 8.1 Kimi Code 结论：只能部分满足

Kimi Code 的配置入口主要是：

- `$KIMI_CODE_HOME/config.toml`：主配置文件。
- `$KIMI_CODE_HOME/tui.toml`：TUI/client 偏好。
- `$KIMI_CODE_HOME/mcp.json` 和项目 `.kimi-code/mcp.json`：MCP 配置。
- `$KIMI_CODE_HOME` 下的 sessions、logs、OAuth credentials、Kimi-specific `AGENTS.md`、skills、plugins。
- CLI/env 临时覆盖：`--model`、`--plan`、`--yolo`、`--skills-dir`、`KIMI_MODEL_*`、`KIMI_SECONDARY_MODEL`、`KIMI_SECONDARY_EFFORT`、若干 timeout/image/token-counting env。

Kimi 的问题是配置覆盖通道不完整：

- `KIMI_CODE_HOME` 是整根目录 relocation，不是 overlay。设置后会同时改变 config、sessions、logs、OAuth、skills、plugins 的归属。
- 如果给每个实例分配临时 `KIMI_CODE_HOME`，它不会继承用户 `~/.kimi-code/config.toml`；除非我们复制原配置。
- 如果复制原配置，就引入凭据复制、配置陈旧、写回语义、MCP/OAuth 文件同步这些副作用。
- 普通 provider credentials 不自动读 shell env。`KIMI_API_KEY` 这类名字必须写在 `config.toml` 的 `[providers.<name>.env]` 里，shell 环境不会自动生效。
- `KIMI_MODEL_*` 可以合成临时内存 provider/model，是很有用的特例，但它只覆盖模型/provider 这一条窄路径，不能表达完整 profile。

因此 Kimi 不能自然满足“继承用户配置 + 局部实例覆盖”。可选处理只有三种：

| 方案 | 做法 | 结论 |
| --- | --- | --- |
| 只用 CLI/env override | 用 `--model`、`--plan`、`--yolo`、`KIMI_MODEL_*` 注入少数字段 | 只适合非常窄的 profile，无法覆盖权限、MCP、agent identity 等完整需求 |
| 生成临时 `KIMI_CODE_HOME` 并复制配置 | 启动前复制 `~/.kimi-code` 的必要文件，再 patch 临时 `config.toml` | 技术可行但风险高，容易复制凭据和陈旧状态，不推荐作为默认架构 |
| 上游支持 overlay | 需要 Kimi 增加类似 `KIMI_CONFIG_CONTENT` / `--config-overlay` 的最后优先级配置层 | 这是 Kimi 成为默认候选的理想条件 |

Kimi 的设计结论：在没有上游 overlay 之前，它可以作为 ACP 协议兼容性研究对象，但不应作为第一条生产接入目标。

### 8.2 OpenCode 结论：更接近硬门槛

OpenCode 有两套相关 config loader：

- 当前 `packages/opencode/src/config/config.ts` 读取 global、`OPENCODE_CONFIG`、项目 `opencode.json/jsonc`、`.opencode` 目录、`OPENCODE_CONFIG_DIR`、`OPENCODE_CONFIG_CONTENT`。
- V2 core `packages/core/src/config.ts` 也按 global config directory、ancestor project files、`.opencode` directories 生成从低到高优先级的 config entries。

当前 `opencode acp` 使用的生产 loader 里，关键优先级是：

1. global config：`$XDG_CONFIG_HOME/opencode/config.json`、`opencode.json`、`opencode.jsonc`。
2. `OPENCODE_CONFIG` 指定的额外 config 文件。
3. workspace ancestor 里的 `opencode.json/jsonc`，除非设置 `OPENCODE_DISABLE_PROJECT_CONFIG=1`。
4. global/project `.opencode` 目录中的 `opencode.json/jsonc`、commands、agents、plugins。
5. `OPENCODE_CONFIG_DIR` 指定的配置目录。
6. `OPENCODE_CONFIG_CONTENT`，最后加载，source 标记为 local。
7. active account / managed config / MDM managed preferences 会在不同阶段参与 merge，其中 MDM managed preferences 仍有最终权威。

这基本满足我们的实例 profile 需求：

- 对单实例设置 `OPENCODE_CONFIG_CONTENT=<json>`，只写要覆盖的字段；它最后 merge，未覆盖字段继承用户现有配置。
- 如果需要实例级 auth，可设置 `OPENCODE_AUTH_CONTENT=<json>`，绕开并不修改 `Global.Path.data/auth.json`。
- 如果需要隔离运行数据，可设置 `XDG_DATA_HOME` / `XDG_STATE_HOME` / `XDG_CACHE_HOME` 或 `OPENCODE_TEST_HOME` 风格的启动 env；但这会改变 auth/session/log 等数据根，不能和“继承已有 auth/session”同时成立。
- 如果需要禁止仓库本地配置影响 Feishu 实例，设置 `OPENCODE_DISABLE_PROJECT_CONFIG=1`；否则保留默认以继承项目约定。

OpenCode 仍有几个边界：

- `OPENCODE_CONFIG_CONTENT` 是完整 JSON 字符串，适合由 wrapper 生成；不适合用户手写长配置。
- merge 是 deep merge，`instructions` 有去重 concat 特例；其他数组是否替换要按字段逐个确认，profile schema 不能依赖“所有数组都追加”。
- `OPENCODE_CONFIG_DIR` 不是 overlay 文件，而是额外 config directory；如果使用它，目录里可能还会触发插件依赖安装。
- `OPENCODE_AUTH_CONTENT` 优先级很高，但一旦运行中通过 HTTP control auth set/remove，代码会尝试写 `auth.json`。Feishu/headless wrapper 不应开放这类写 auth 的操作，除非明确选择持久化 profile。

### 8.3 现有覆盖需求逐项对照 OpenCode

当前 Codex/Claude profile 覆盖不是一个单字段模型选择，而是一组“连接合同 + 线程策略 + 工具/MCP 注入 + 凭据隔离”。OpenCode 大体能表达，但不是所有字段都有同名等价物。

| 我们现在的覆盖需求 | 当前 Codex/Claude 投影方式 | OpenCode 可表达方式 | 结论 |
| --- | --- | --- | --- |
| 主模型 | Codex `thread/start.model` / `thread/resume.model`；Claude `ANTHROPIC_MODEL` | `OPENCODE_CONFIG_CONTENT.model = "provider/model"`，或创建 profile agent 后用 `agent.<id>.model` | 可支持 |
| 自定义 provider base URL | Codex `model_providers.<id>.base_url`；Claude `ANTHROPIC_BASE_URL` | `provider.<id>.options.baseURL` | 可支持 |
| API key / auth token | Codex child env 专用 key；Claude `ANTHROPIC_AUTH_TOKEN` | `provider.<id>.options.apiKey`，或更推荐 `OPENCODE_AUTH_CONTENT` 注入 provider auth | 可支持，但要避免把 secret 放进普通 state/log |
| provider 注册字段 | Codex 覆盖 `model_provider`、`wire_api`、`env_key`、`requires_openai_auth`、`supports_websockets` | v1 config `provider.<id>.npm/api/id/env/options/models` | 可支持，但 schema 形状是 OpenCode v1 `provider`，不是 v2 `providers` |
| 模型 metadata / catalog | Codex 对 DeepSeek/MiMo 写 managed `model_catalog_json`，含 context/cost/reasoning/tool metadata | `provider.<id>.models.<model>` 可声明 `limit`、`cost`、`reasoning`、`tool_call`、`variants` 等 | 大体可支持；不需要 Codex 那套 managed catalog 文件，但要生成 OpenCode provider model entries |
| reasoning effort | Codex `model_reasoning_effort`；Claude `CLAUDE_CODE_EFFORT_LEVEL` | OpenCode 用 model `variants` 暴露 ACP `effort` option；运行时 `session/set_config_option(configId="effort")` 设置 variant | 可支持，但要把我们的 reasoning effort 编译成 OpenCode variant，而不是顶层字段 |
| review / small model | Codex `review_model`；Claude `ANTHROPIC_DEFAULT_HAIKU_MODEL` | OpenCode 顶层 `small_model`，以及 `agent.title/summary/compaction.model` 等 agent-specific model | 部分支持；如果我们只需要小模型/标题模型可映射，Codex `review_model` 的精确语义需要产品降级说明 |
| subagent model | Codex `agents.default_subagent_model`；Claude `CLAUDE_CODE_SUBAGENT_MODEL` | OpenCode 可配置 `agent.<name>.model` / `agent.<name>.mode = "subagent"`，也有 `subagent_depth` | 可支持，但需要定义“默认 subagent”映射策略，不能假设存在单个全局 default_subagent_model |
| profile instruction | Codex `developerInstructions`；Claude append system prompt env | OpenCode v1 `agent.<id>.prompt`，v2 草案为 `agent.<id>.system`；也可用 `instructions` 文件列表 | 可支持；当前应按 v1 `prompt` 生成 profile agent，避免用 v2 `system` |
| access / permission | Codex approvalPolicy+sandbox；Claude permission env/SDK 更新 | OpenCode v1 `permission` 或 `agent.<id>.permission`，支持 `ask/allow/deny` 和 tool-specific rules | 可支持；但 Codex sandbox 不是等价概念，需映射成 OpenCode tool permission 规则 |
| context window / auto compact | Codex `model_context_window`、`model_auto_compact_token_limit`；Claude `[1m]` model suffix | OpenCode model metadata `limit.context` 和 `compaction` 配置；ACP token usage 可回传 context 信息有限 | 部分支持；可表达偏好和模型上限，但无法承诺和 Codex `extended_1m` 完全等价 |
| Feishu tool MCP 注入 | Codex CLI `mcp_servers.<id>.url/bearer_token_env_var`；Claude 写临时 `--mcp-config` | 优先通过 ACP `session/new/load/resume/fork.mcpServers` 注册；也可用 `OPENCODE_CONFIG_CONTENT.mcp.<id>` | 可支持；推荐 ACP `mcpServers`，因为它是 session-scoped，避免污染 OpenCode config |
| MCP bearer secret | Codex bearer env var；Claude config 内 `${ENV}` 模板 | ACP `mcpServers.headers` 可直接传 header value；config overlay 用 `{env:NAME}` 语法，不是 `${NAME}` | 可支持；不能复用 Claude config 模板语法 |
| MCP OAuth | `/mcpoauth` 命令路由到 Codex MCP OAuth events | OpenCode remote MCP 有 OAuth 配置和 auth API，但 ACP `mcpServers` 注册不会自动把我们的 `/mcpoauth` 语义变成 OpenCode OAuth 流程 | 部分支持；初版只显示 needs-auth，不接管完整 OAuth |
| 项目配置继承 | Codex/Claude 默认继承 cwd 下原生项目配置 | OpenCode 默认继承 project `opencode.json/jsonc` 与 `.opencode`；可用 `OPENCODE_DISABLE_PROJECT_CONFIG=1` 关闭 | 可支持，而且开关清晰 |
| 实例运行数据隔离 | Codex/Claude 可按 profile/child env 控制认证或 settings，不必改用户配置 | OpenCode 可用 XDG roots 隔离 data/state/cache，但这会同时隔离 sessions/auth/logs | 可支持但要独立建模；不能把 data isolation 和 config overlay 混为一谈 |
| 任意上游配置覆盖 | Codex 只开放受控字段，不支持任意 native profile 字段 | OpenCode `OPENCODE_CONFIG_CONTENT` 理论可写任意 v1 config 字段 | 技术可支持，但产品上仍应 allowlist，避免 profile 变成任意 OpenCode config 编辑器 |

关键修正：

- OpenCode 当前启动合同应以 v1 config 为准：`provider`、`agent`、`mcp`、`permission`、`model`、`small_model`。v2 草案的 `providers`、`agents`、`mcp.servers` 只能作为后续迁移方向。
- Feishu MCP 注入不要默认塞进 `OPENCODE_CONFIG_CONTENT.mcp`，因为我们的工具服务是实例级、turn/session 相关，并且 URL 带 `codex_remote_instance_id`。OpenCode ACP 已经支持 `mcpServers` 参数，应优先走 ACP session 参数。
- 需要生成一个 OpenCode profile agent，而不是只设置顶层 `model`。这样 profile instruction、permission、subagent/primary mode、model/variant 才能聚合到一个可选择的 OpenCode agent/mode 上。
- review model、context window、sandbox 这三类是语义差异，不是字段缺失。它们需要在 profile compiler 里有明确降级或拒绝策略。

### 8.4 推荐的 OpenCode overlay 编译形状

对一个 API profile，wrapper 可生成类似如下 `OPENCODE_CONFIG_CONTENT`：

```json
{
  "model": "codex-remote-profile/main-model",
  "provider": {
    "codex-remote-profile": {
      "name": "Codex Remote Profile",
      "npm": "@ai-sdk/openai-compatible",
      "env": [],
      "models": {
        "main-model": {
          "id": "upstream-model-id",
          "name": "Main Model",
          "reasoning": true,
          "tool_call": true,
          "limit": { "context": 272000, "output": 10000 },
          "variants": {
            "low": {},
            "medium": {},
            "high": {}
          }
        }
      },
      "options": {
        "baseURL": "https://example.invalid/v1",
        "apiKey": "{env:CODEX_REMOTE_ACP_PROFILE_API_KEY}"
      }
    }
  },
  "agent": {
    "codex-remote-profile": {
      "mode": "primary",
      "model": "codex-remote-profile/main-model",
      "variant": "high",
      "prompt": "profile instruction",
      "permission": {
        "bash": "ask",
        "edit": "ask",
        "webfetch": "allow"
      }
    }
  },
  "default_agent": "codex-remote-profile",
  "compaction": {
    "auto": true,
    "reserved": 27200
  }
}
```

同时 child env 注入：

```text
OPENCODE_CONFIG_CONTENT=<上面的 JSON>
CODEX_REMOTE_ACP_PROFILE_API_KEY=<secret>
```

如果使用 `OPENCODE_AUTH_CONTENT`，则不要把 `apiKey` 同时放在 `provider.options`，避免两条认证来源互相覆盖。两种 secret 注入方式需要二选一：

- **env token in config**：适合 OpenAI-compatible 自定义 provider，profile compiler 控制 provider id 和 env key。
- **`OPENCODE_AUTH_CONTENT`**：适合复用 OpenCode 已有 provider auth 机制，尤其是内建 provider。

Feishu MCP 则随 ACP session request 注入：

```json
{
  "mcpServers": [
    {
      "name": "codex_remote_feishu",
      "type": "http",
      "url": "http://127.0.0.1:9702/mcp?codex_remote_instance_id=inst-1",
      "headers": [{ "name": "Authorization", "value": "Bearer <token>" }]
    }
  ]
}
```

### 8.5 Claude 经验对这层设计的约束

Claude 支持本身已经证明：profile / loader 不是一个能“按字段原样映射”的通用层。

Claude 当前做法也是后端专属近似：

- base URL、auth token、main model、small model、subagent model、reasoning effort 通过环境变量注入。
- profile instruction 不走 `--settings`，而是在 SDK 初始化请求里追加 `appendSystemPrompt`。
- MCP 注入不改用户 Claude 配置，而是写运行态临时 `--mcp-config` 文件。
- extended context 通过模型名 `[1m]` 后缀表达，且 built-in default 和 custom model 有不同处理。
- permission/access 不是通用 sandbox，而是 Claude native permission mode、plan permission updates、permission suggestions 的组合。
- prompt input、steer、部分 permission update 都存在明确 unsupported 分支。

也就是说，Claude 已经不是“完整覆盖所有现有功能”，而是把产品上重要的 profile 能力近似投影到 Claude 能接受的 native loader / SDK / env 组合里。OpenCode/Kimi 不应被要求高于这个标准。

因此，ACP 接入的抽象边界应调整为：

- ACP 协议段可以抽象：JSON-RPC、initialize、session lifecycle、prompt stream、cancel、permission request、config option、available commands、hydration。
- profile/loader 段不要抽象成统一 overlay：OpenCode、Kimi、Claude、Codex 各自单写 compiler。
- 产品层只定义“我们希望表达的 profile intent”和“哪些 intent 是 required / best-effort / unsupported”，不定义统一配置 schema。
- 每个后端 compiler 负责把 intent 编译成自己的 launch material，并显式产出 capability/diagnostic。

### 8.6 Loader 层的实际设计：放弃强抽象，只保留合同

不要新增通用的 `ConfigOverlay` / `AuthOverlay` 数据结构作为所有 ACP profile 的必经层。它会制造一种错误期待：好像不同 ACP agent 只要填同一份 overlay 就能运行。实际情况不是这样。

建议只保留一个很薄的后端私有 compiler 合同：

```go
type BackendProfileCompiler interface {
    Compile(ProfileIntent, RuntimeContext) (LaunchMaterial, ProfileCapabilityReport, error)
}
```

这里的 `ProfileIntent` 也不应该暴露为“任意 native config map”。它只是我们产品已经有的少数意图字段：

- connection：provider/baseURL/auth source
- model：main/small/subagent/review
- instruction
- reasoning / effort
- access / permission preference
- context preference
- Feishu MCP publication
- project config inherit mode

`LaunchMaterial` 是后端私有结果：

- Codex：CLI `-c`、secret child env、managed model catalog file。
- Claude：env、`--settings` 临时文件、`--mcp-config` 临时文件、SDK init fields。
- OpenCode：`OPENCODE_CONFIG_CONTENT`、secret env 或 `OPENCODE_AUTH_CONTENT`、ACP `mcpServers` session 参数、可选 XDG/data env。
- Kimi：CLI/env limited overrides、可选 `KIMI_CODE_HOME` 受限模式；超出表达范围时 hard fail 或 best-effort diagnostic。

`ProfileCapabilityReport` 是给 orchestrator/Feishu/status 的解释，不参与协议抽象：

```go
type ProfileCapabilityReport struct {
    RequiredUnsupported []ProfileFeatureDiagnostic
    BestEffortDropped   []ProfileFeatureDiagnostic
    ApproximateMappings []ProfileFeatureDiagnostic
}
```

这样做的好处是：

- 不把 ACP 协议和非 ACP loader 绑死。
- 新接 Kimi、Goose、OpenCode 时只写各自 compiler，不污染 `ACPRuntimeAdapter`。
- 可以接受“近似支持”：比如 OpenCode 的 review model / context window / sandbox 不完全等价，但能给出明确降级说明。
- 失败条件更清楚：只有 required intent 不能表达时才拒绝启动；best-effort intent 可以降级并在 status/debug 中说明。

### 8.7 OpenCode 的近似边界

OpenCode 作为第一候选，不代表它要 100% 复刻 Codex/Claude 每个 profile 字段。按 Claude 的标准，下面这些近似可以接受：

| 能力 | OpenCode 处理 | 产品结论 |
| --- | --- | --- |
| review model | 优先映射到 `small_model` 或 title/summary 类 agent model；没有 Claude/Codex review 完全同义项 | best-effort，不作为首版硬阻塞 |
| context window | 通过 model `limit.context` 和 compaction preference 表达偏好；不能承诺等价 Codex `model_context_window` runtime clamp | best-effort，首版 status 标明 approximate |
| sandbox/access | 映射到 OpenCode `permission` rules；没有 Codex sandbox 目录/网络隔离等价项 | required 的只应是 ask/allow/deny 审批体验，不是 sandbox 等价 |
| subagent default | 通过配置特定 subagent agents 或 agent model；不存在单个标准 `default_subagent_model` | best-effort，需要定义默认 subagent 映射 |
| MCP OAuth | OpenCode 有自己的 MCP OAuth；我们的 `/mcpoauth` 不应首版接管它 | unsupported in v1，显示 needs-auth/引导本机处理 |
| arbitrary native config | `OPENCODE_CONFIG_CONTENT` 能写，但产品不开放任意 passthrough | unsupported by policy，避免 profile 变成 native config editor |

### 8.8 选择结论

如果 profile 隔离是硬门槛，当前优先级应改为：

1. OpenCode：作为第一条 ACP 接入目标。它同时有 ACP server、dynamic config options、commands/skills catalog、inline config/auth overlay。
2. Kimi Code：作为协议差异参考或第二阶段候选。除非补齐上游 overlay，否则不要承诺完整 profile 能力。

这个结论不否定 Kimi ACP 的协议价值，但会改变落地顺序：我们应先用 OpenCode 把通用 `ACPRuntimeAdapter` 做扎实，同时把 OpenCode loader/profile compiler 作为后端私有实现。后续接 Kimi 时复用 ACP 协议 adapter，但重新写 Kimi compiler，并按 required / best-effort / unsupported 给出明确诊断。

## 9. 新 backend 接入地图与当前缺口

上面的章节已经覆盖了 ACP 协议段和 OpenCode/Kimi loader 的主要差异，但它还不是完整的“新增 backend 宏观地图”。对照当前 Codex/Claude 接入链路，真正要接入一条 OpenCode ACP backend，至少还要覆盖下面这些产品架构块。

### 9.1 Backend identity 与 profile catalog

当前 `agentproto.Backend` 只有 `codex` / `claude`，未知值会归一化成 Codex。新增 ACP 时不能继续依赖这个默认行为，否则旧字段缺失、状态恢复或配置解析失败都会静默变成 Codex。

缺口：

- 新增 `BackendACP`，并让 unknown backend 明确失败或保留为 unsupported diagnostic，而不是默认 Codex。
- 新增 `ACPProfileID` / `ACPProfileSummary`，用于表达 `opencode-default`、`opencode-team-proxy`、后续 `kimi-*` 等 profile。
- 保留 vendor display name：backend 是协议族 `acp`，用户看到的是 profile display name，例如 `OpenCode`。
- 现有 `CodexProfiles`、`ClaudeProfiles`、`OpenCodeProfiles`、workspace profile snapshot 都是专用轨道；通用 ACP profile catalog 如未来需要抽象，不能塞进 Codex Profile 或 Claude profile。

### 9.2 Surface / instance / launch contract

当前核心合同有三层：`SurfaceBackendContract`、`InstanceBackendContract`、`HeadlessLaunchContract`。它们已使用 `CodexProfileID` / `ClaudeProfileID` / `OpenCodeProfileID`，surface resume 会把持久化记录 materialize 回这些字段。

缺口：

- 三个 contract 都要增加 `ACPProfileID`，并调整 normalize/effective 函数。
- `SurfaceConsoleRecord`、`InstanceRecord`、`HeadlessLaunchRecord`、`DaemonCommand`、`InstanceHello` 都要携带 ACP profile。
- `BotCapabilitySettingsRecord` 也要携带 ACP profile，否则 bot 级默认能力设置无法选择 OpenCode。
- `WorkspaceDefaultsStorageKey` 这类 backend/profile 相关 key 要包含 ACP profile，避免不同 ACP profile 共享错误的 workspace default。
- 旧 resume store 需要兼容：没有 ACP 字段的旧记录按旧逻辑恢复；有 `backend=acp` 但 profile 缺失时必须进入 invalid/reselect，而不是回落 Codex。

这块不是 ACP adapter 能解决的，它是 orchestrator 的产品状态合同。

### 9.3 Headless launch planning 与兼容性判断

Codex/Claude 当前不只是“启动一个子进程”：它们会判断当前实例是否兼容目标合同，不兼容时走 workspace route restart、exact-thread restart、prompt-dispatch restart 或 fresh workspace launch。Claude 还会因为 reasoning effort override 触发 prompt 前重启。

缺口：

- 新增 ACP launch compatibility：`current instance ACPProfileID == desired ACPProfileID` 才能复用。
- 如果 prompt override 或 dynamic config 只能通过 launch material 表达，则要像 Claude reasoning 一样进入重启 preflight；如果能通过 `session/set_config_option` 表达，则不重启。
- OpenCode compiler 的 `ProfileCapabilityReport` 要进入 start failure/status/debug，不能只写 wrapper log。
- `headlessLaunchModeForBackend`、daemon env 注入、wrapper entry mode 都要支持 ACP/OpenCode。
- pending headless、room reservation、workspace claim、restart notice 的路径要覆盖 ACP，不然 profile switch 会只改 surface 字段但不重启实例。

结论：需要一个 backend-aware launch planner，而不是只在 wrapper 里新增 `opencode acp` 命令。

### 9.4 Wrapper runtime 与 ACP protocol adapter

这是最适合抽象的部分。`ACPRuntimeAdapter` 应该只拥有 ACP wire protocol，不拥有 OpenCode/Kimi 的配置编译。

必须覆盖：

- JSON-RPC line framing、request/response pending map、notification dispatch。
- initialize/authenticate gate。
- `session/list/new/resume/load/fork/close` 能力发现和降级。
- `session/prompt` 到 turn/item/request lifecycle。
- `session/cancel` notification 到本地 ack 和最终 reconciliation。
- `session/request_permission` 的动态 option round-trip。
- `configOptions`、`available_commands_update`、usage/update/plan/tool call 的 canonical projection。
- `TrafficClassHydration`，把 `load/fork` replay 与真实新输出区分开。

不能放进这里：

- `OPENCODE_CONFIG_CONTENT` / `OPENCODE_AUTH_CONTENT`。
- Kimi `KIMI_CODE_HOME` 或 CLI/env override。
- Feishu MCP 是通过 config 文件、CLI 参数还是 ACP `mcpServers` 注入。
- profile 字段是否 required/best-effort/unsupported。

这些都属于 backend-specific profile compiler。

### 9.5 Feishu command catalog 与动态 backend capability

当前 Feishu 菜单由 `FeishuCommandDisplayProfile` 控制，只有 `codex` / `claude` / `vscode` 三种 visible mode。ACP 的动态能力有两类，不能混在一起：

- 产品命令：`/stop`、`/list`、`/use`、`/workspace`、`/status`、`/menu` 等，仍由我们控制。
- backend commands：ACP `available_commands_update` 暴露的 agent slash commands / skills，属于 backend 内容。

缺口：

- 新增 `acp` command display profile，默认显式 allowlist。
- `/model`、`/reasoning`、`/plan` 需要从当前 ACP `configOptions` 和 profile alias 推导，不存在 option 时返回 backend-specific reject。
- `available_commands_update` 要进 state/status/debug，首版不要替代 Feishu 主菜单。
- `/mode opencode`、`/mode kimi` 可以是用户入口 alias，但持久化 contract 应是 `backend=acp + acpProfileID=<id>`。
- admin/web/API 的 profile 选择面也要展示 ACP profile，否则只能靠手写配置启动。

### 9.6 Request / permission bridge

ACP 的 `session/request_permission` 比当前 Codex/Claude 的审批更动态：option id、option kind、问题询问、plan review、MCP elicitation 都可能走同一条 channel。

当前 `RequestPrompt` 已经有 `Options`、`Questions`、`Permissions`、`MCPElicitation`，具备承接基础，但仍有缺口：

- ACP option id 必须原样 round-trip，不能用 label 或固定 accept/decline 语义推断。
- 多 option 要适配 Feishu 卡片按钮上限，必要时转 structured form。
- question/elicitation 与 tool approval 要按 `RequestType` 区分，不能全部显示成权限审批。
- request capture、owner routing、old-card replacement、resolved event 要覆盖 ACP request id。
- unsupported request kind 要明确 reject，并带 protocol notice，不能卡住 backend prompt。

### 9.7 MCP publication 与 MCP OAuth

现有 MCP 注入已经是后端专属：

- Codex：CLI `-c mcp_servers.*` 加 bearer env。
- Claude：写临时 `--mcp-config`，secret 用 env placeholder。
- OpenCode：优先用 ACP `mcpServers` session 参数，因为它天然是 session-scoped；必要时才写 `OPENCODE_CONFIG_CONTENT.mcp`。
- Kimi：文档声称支持，但源码路径不稳定，初版只能 best-effort。

缺口：

- 抽出一个“Feishu MCP publication intent”，由各 backend compiler 或 ACP session builder 消费。
- status/debug 要说明 MCP 是 injected / skipped / best-effort dropped。
- `/mcpoauth` 当前更偏 Codex MCP OAuth；OpenCode 自己的 MCP OAuth 不应首版接管，最多展示 needs-auth 和本机处理建议。
- MCP caller instance id 仍要保留，否则工具服务无法正确关联 turn/instance。

### 9.8 Session catalog、hydration 与 resume/fresh-start

`/list`、`/use`、surface resume、workspace resume 不是单纯调用 backend `session/list`。它们还涉及 workspace claim、route mode、selected thread、prepared cwd、missing target fallback、visible workspace compatibility。

缺口：

- ACP session list 要携带 backend/profile/cwd metadata；分页 cursor 不能丢。
- `session/resume` 优先用于恢复，`session/load` 只作为 fallback，并标 hydration。
- `session/fork` 如果接入，也必须按 hydration 处理。
- surface resume store 要能恢复 ACP profile，否则 daemon 重启后无法判断“当前可见 workspace 实例是否兼容”。
- Claude/Codex 现有的 “workspace mismatch -> fresh headless” 流程要扩展到 ACP。

### 9.9 Observability、diagnostics 与 admin surface

ACP 接入的很多失败不是协议错误，而是 profile compiler 或上游实现差异。没有观测面会很难定位。

缺口：

- `/status` 显示 backend、ACP profile、native implementation、session id、current config options、capability report。
- `/debug` 或 admin API 暴露 initialize capabilities、available commands、profile compiler diagnostics、hydration counters。
- start failure 要区分：binary missing、auth missing、profile required unsupported、ACP initialize failed、session capability missing、MCP injection dropped。
- capability state projection 目前偏 MCP/account login；需要能承接 ACP dynamic config / available commands 的只读状态。

### 9.10 测试地图

OpenCode 细节最终必须靠真实可执行黑盒测出来，但宏观接入前要先分层建测试：

- state contract tests：backend normalize、surface/instance/launch contract、resume store、bot capability settings。
- orchestrator tests：profile switch、workspace restart、fresh-start fallback、prompt restart preflight、command display support。
- wrapper adapter tests：mock ACP server 覆盖 JSON-RPC、resume/load hydration、cancel、request permission、config option。
- compiler tests：OpenCode overlay/auth/MCP/session material 生成，required/best-effort diagnostics。
- black-box executable tests：后续专门对 `opencode acp` 构造真实用例，验证源码阅读假设。

因此，旧的“新增后端 7 步指南”只覆盖 wrapper translator/launch/command display/config 的基础面；它缺少现在实际存在的 profile catalog、surface capability settings、resume store、headless compatibility/restart、dynamic config、MCP publication 和 observability 这些块。宏观设计如果不补这些，后续 OpenCode 详细设计会不断撞到 orchestrator 状态边界。

## 10. 实施拆分建议

### Stage 1：状态合同与产品地图

- 新增 `BackendACP`，禁止未知 backend 默认落到 Codex 的风险继续扩大。
- 新增 `ACPProfile` 配置、profile catalog、`ACPProfileID` contract 字段。
- 扩展 surface / instance / launch / daemon command / hello / resume store / bot capability settings。
- 新增后端私有 profile compiler 合同，只规定输入 intent、launch material、capability report，不规定统一 overlay schema。
- 新增 `TrafficClassHydration`。
- 新增 `config.options.updated` / `session.config.set` / `commands.available.updated` 的 canonical 类型。
- 添加 focused tests 覆盖 backend normalize、surface contract、capabilities、resume store、bot settings、command display profile。

### Stage 2：ACPRuntimeAdapter

- 实现 JSON-RPC line framing、request id pending map、initialize/auth gate。
- 实现 `session/list` -> `threads.snapshot`。
- 实现 `session/new/resume/load`，其中 load replay 标 hydration。
- 实现 `session/prompt` -> turn/item lifecycle。
- 实现 `session/cancel` notification。
- 实现 `session/request_permission` dynamic request prompt。
- 用 mock ACP server 写 adapter 单测，不依赖真实 Kimi。

### Stage 3：OpenCode profile compiler 与 launch

- 新增默认 OpenCode ACP profile：`opencode acp`。
- 实现 `OpenCodeProfileCompiler`，把产品 profile intent 编译为 `OPENCODE_CONFIG_CONTENT`、secret env 或 `OPENCODE_AUTH_CONTENT`、ACP `mcpServers`。
- 按 `ProjectConfigMode` 设置或清除 `OPENCODE_DISABLE_PROJECT_CONFIG`。
- 按 `DataIsolationMode` 决定是否注入 `XDG_DATA_HOME` / `XDG_STATE_HOME` / `XDG_CACHE_HOME`。
- 对 review model、context window、sandbox、MCP OAuth 等差异输出 capability report，不伪装为完全等价。
- 使用真实 `opencode acp` 做黑盒 smoke：initialize、session/list、session/new、prompt、cancel、resume、config option set。

### Stage 4：Orchestrator / Feishu 产品接入

- 新增 `acp` command display profile 和 `/mode opencode` alias。
- 接入 ACP profile switch、workspace route restart、exact-thread resume、fresh workspace fallback。
- `/model`、`/reasoning`、`/plan` 通过 ACP dynamic config option 派生。
- `/status`、`/debug` 展示 ACP profile、capabilities、config options、available commands、compiler diagnostics。
- Feishu request card 支持 ACP dynamic option 和 overflow 表单降级。

### Stage 5：Kimi profile compatibility check

- 新增默认 Kimi ACP profile：`kimi acp`。
- 加二进制解析、env overlay、预登录检查。
- 实现 `KimiProfileCompiler`，只允许模型/plan/yolo/skills-dir 等当前 Kimi 能无副作用表达的 intent。
- 对 required intent 无法表达返回 hard fail；对 best-effort intent 给出 dropped diagnostic，不静默降级到复制 `KIMI_CODE_HOME`。
- 不实现 terminal auth 自动登录，只给清晰错误和操作建议。
- 使用真实 `kimi acp` 做黑盒 smoke：initialize、session/list、session/new、prompt、cancel、resume。

### Stage 6：补高级能力

- backend slash command 二级入口。
- hydration transcript cache。
- ACP fs reverse-RPC 代理。
- MCP caller injection 的稳定性复核。
- Kimi 作为第二个 ACP profile 验证抽象是否真通用。

## 11. 测试重点

必须覆盖：

- `backend=acp` 不会在 normalize、resume store、bot settings 中回落 Codex。
- ACP profile switch 会触发正确的 workspace/exact-thread restart 或复用判断。
- OpenCode `OPENCODE_CONFIG_CONTENT` 只覆盖指定字段且继承 global/project config。
- OpenCode `OPENCODE_AUTH_CONTENT` 不读写用户 `auth.json`。
- OpenCode `OPENCODE_DISABLE_PROJECT_CONFIG` 打开时不继承 workspace config，关闭时继承。
- OpenCode compiler 对 review/context/sandbox 的 approximate mapping 有 capability report。
- Kimi 对 required intent 不可表达时 hard fail，不自动复制整个 `KIMI_CODE_HOME`。
- `session/load` replay 不投影到 Feishu。
- `session/resume` 不等待 replay 且能继续 prompt。
- `session/cancel` 是 notification，不能依赖 response gate。
- `request_permission` option id 原样 round-trip。
- unknown option id 拒绝，不默认 approve。
- config option set 失败不半应用。
- Kimi auth missing 时给可操作错误。
- MCP `acp` transport 被丢弃时有 protocol notice。
- `available_commands_update` 为空时不清空 Feishu 产品命令。
- wrapper 重启恢复后 session id / thread id 映射不漂移。

## 12. 仍需产品决策

1. 第一条 ACP profile 选 OpenCode 还是 Kimi：
   - 技术上建议 OpenCode 先做，因为它满足 profile overlay 硬门槛。
   - 如果产品更想优先 Kimi，需要接受“受限 profile”或推动上游提供 overlay。

2. 对外显示叫 `OpenCode` / `Kimi` 还是 `ACP / <profile>`：
   - 技术上建议 backend 是 `acp`，profile display name 是 `Kimi Code`。
   - 产品上 `/mode opencode`、`/mode kimi` 可以作为 alias，但持久化不应写成 vendor-only backend。

3. Feishu 是否允许触发 backend 登录：
   - 初版建议不允许，只提示主机执行 `opencode auth login` 或 `kimi login`。
   - 如果要支持，需单独设计 terminal auth 或 web auth。

4. `agent_thought_chunk` 是否展示：
   - 初版建议隐藏或只进 debug/status。
   - 如果展示，需要和现有 reasoning summary 卡片规则统一。

5. ACP fs reverse-RPC 是否声明：
   - 初版建议不声明。
   - 声明后必须由 relay/orchestrator 成为文件访问策略拥有者。

6. 是否把 `/access` 映射 ACP `mode`：
   - 可以做，但必须改文案，避免把 Kimi mode 误解释成 Codex sandbox。

## 13. 最终判断

ACP 与当前需求的距离不在“能不能聊天”，而在两组抽象缺口：

1. 协议缺口：当前 canonical config、request/menu、hydration/replay 还不够承接 ACP 的 dynamic config、dynamic option、available commands 和 `session/load` replay。
2. 产品状态缺口：当前 backend/profile 状态合同、bot capability settings、surface resume、headless launch/restart、MCP publication、status/debug 仍是 Codex/Claude 双轨，不足以完整接入第三条 backend。

因此，实现接入的正确目标不是最小 PoC，而是先让 OpenCode 作为第一条真实 ACP profile，把 wrapper runtime、session hydration、dynamic approval、dynamic config 这些协议坑踩完整；同时承认 profile loader 是 OpenCode 私有 compiler，不试图抽象成 ACP 通用 overlay。只要这个边界处理干净，后续接 Kimi/Goose 的工作量会主要落在各自 compiler 和能力差异表，而不是重写 ACP 协议 adapter。

# 新 Backend 接入评估 Playbook

> Type: `general`
> Updated: `2026-08-08`
> Summary: 新增面向任意 AI 客户端/backend 的接入评估流程、测试矩阵、能力分级和结论模板。

## 1. 文档定位

这份 playbook 用于评估一个新的 AI 客户端或 backend 是否适合接入 codex-remote-feishu，并指导后续设计文档和黑盒测试。

它不是实现步骤清单。旧的 [新增 AI 后端集成指南](./adding-new-ai-backend.md) 仍可作为 wrapper/translator/launcher 的代码参考，但它只覆盖基础实现面。真实接入还必须评估产品状态、profile、resume、MCP、Feishu、admin/config、恢复和诊断。

每次评估一个新 backend，至少产出一份候选报告：

- `docs/draft/<backend>-backend-integration-evaluation.md`
- 或者在已有专项设计文档中增加 “按 playbook 评估结论” 章节

候选报告必须引用本文，并使用本文的能力分级和结论模板。

## 2. 评估目标

评估前先明确目标类型：

| 目标类型 | 含义 | 结论标准 |
| --- | --- | --- |
| production candidate | 计划作为可用 backend 落地 | required 能力必须完整支持；best-effort 必须有明确降级 |
| constrained candidate | 可以落地，但能力明显受限 | required 能力可降级为更窄范围，但产品入口必须写清限制 |
| protocol reference | 主要用于验证某个协议族，例如 ACP | 允许 loader/profile 不完整，但协议行为必须可测 |
| experimental | 只做探索，不承诺产品入口 | 不进入正式 command/profile/admin surface |

如果目标是 production candidate，不能只证明“能聊天”。必须证明它能融入当前 headless + Feishu + profile + workspace + resume 的产品模型。

## 3. 能力分级

每个能力都必须归入一个等级：

| 等级 | 定义 | 产品处理 |
| --- | --- | --- |
| required | 不支持就不能作为当前目标落地 | 启动前 hard fail，或候选结论为不建议接入 |
| best_effort | 可近似或部分表达 | 允许启动，但 status/debug 必须显示 diagnostic |
| unsupported | 明确不支持 | 隐藏入口，或命令/action 返回 backend-specific reject |
| deferred | 技术可做，但不进当前阶段 | 文档记录后续阶段和依赖 |
| unknown | 仅靠文档/源码无法确认 | 必须补源码验证或黑盒测试，不能当作 supported |

评估报告中禁止使用“应该可以”作为最终结论。只能写 supported、best_effort、unsupported、deferred、unknown，并附证据。

## 4. 接入地图

### 4.1 Backend identity

要确认 backend 如何进入系统身份模型。

代码载体：

- `internal/core/agentproto/backend.go`
- `internal/core/agentproto/wire.go`
- `internal/core/state/surface_backend.go`
- `internal/app/wrapper/app.go`

检查项：

- backend 值是否需要新增 vendor backend，还是使用协议族 backend + profile。
- unknown backend 是否会错误回落到 Codex。
- `BackendDisplayName` 是否能表达用户可见名称。
- `InstanceHello` 是否携带足够身份字段。
- capabilities 默认值是否合理，是否需要 runtime hello 覆盖。

测试：

- backend normalize table test。
- hello round-trip test。
- unknown backend 不静默进入 Codex 路径。

### 4.2 Entrypoint、launcher 与运行模式

要确认用户和 daemon 如何启动这个 backend。wrapper 支持不等于 launcher 支持。

代码载体：

- `internal/app/wrapper/entry.go`
- `internal/app/launcher/role.go`
- `internal/app/launcher/launcher.go`
- `internal/app/wrapper/app.go`
- `internal/config/envfile.go`

检查项：

- 是否需要新增 wrapper mode，例如 `<backend>-app-server`。
- launcher role dispatch 是否识别该 mode。
- 使用说明、安装脚本、环境变量是否需要更新。
- headless、VS Code、probe、managed lifetime 是否都适用。
- `CODEX_REMOTE_INSTANCE_BACKEND` 和 backend 专属 env 是否足够表达实例身份。
- wrapper hello 是否带上 backend/profile/source/managed/pid。

测试：

- `wrapperBackendFromArgs` table test。
- launcher role dispatch test。
- wrapper hello backend/profile/capabilities。
- managed headless env 到 wrapper config 的 round-trip。

### 4.3 Profile catalog 与配置来源

要确认用户如何声明多个 profile，以及每个实例如何选择 profile。

代码载体：

- `internal/config/configfile.go`
- `internal/config/codex_profiles.go`
- `internal/config/claude_profiles.go`
- `internal/core/state/profile_catalog.go`
- `internal/core/state/bot_capability_settings.go`
- `internal/app/daemon/botcapabilitysettings/state.go`

检查项：

- 是否需要新增 backend 专属 profile catalog。
- profile 是否有稳定 ID、display name、revision/etag、status。
- secret 是否和 public summary 分离。
- profile name 是否防重、防控制字符、防过长。
- 配置迁移失败时是否阻止启动。
- bot capability settings 是否能选择该 backend/profile。

测试：

- profile create/update/normalize/validate。
- duplicate name、unsafe text、missing required field。
- secret 不出现在 public projection、status、debug。
- bot capability settings 持久化和 materialize。

### 4.4 Loader / profile compiler

这是 backend 专属层，不能假设协议相同就能复用。

代码载体：

- `internal/app/daemon/app_headless.go`
- `internal/app/daemon/app_headless_codex_provider.go`
- `internal/app/daemon/app_headless_claude_profile.go`
- `internal/app/wrapper/app_child_session.go`
- `internal/app/wrapper/app_child_session_claude.go`
- `internal/app/wrapper/app_child_settings_claude.go`
- `internal/app/wrapper/app_child_mcp.go`

检查项：

- 能否实例级覆盖配置，而不修改用户全局配置。
- 未覆盖字段是否继承系统/global/project 配置。
- auth/token 如何注入，是否污染全局文件。
- 工作目录、project config、data/state/cache 隔离是否独立可控。
- profile intent 中哪些字段 required、best_effort、unsupported。
- compiler 是否能产出 diagnostic，而不是只返回模糊 error。

测试：

- 临时 profile 不写用户配置文件。
- 只覆盖指定字段，其余继承现有配置。
- secret 只进 child env、临时 0600 文件或 backend 私有 auth overlay。
- required intent 不可表达时 hard fail。
- best_effort dropped/approximate mapping 进入 capability report。

### 4.5 Configuration mapping contract

配置对应关系要独立于协议对应关系评估。一个 backend 即使协议很像，也可能因为配置无法实例级覆盖、无法继承、无法观察回传而不适合接入。

核心代码载体：

- `internal/config/configfile.go`
- `internal/config/codex_profiles.go`
- `internal/config/claude_profiles.go`
- `internal/config/claude_runtime_settings.go`
- `internal/app/codexprofile/runtime_resolver.go`
- `internal/core/state/codex_profile_runtime.go`
- `internal/core/state/prompt_config.go`
- `internal/core/state/bot_capability_settings.go`
- `internal/core/state/workspace_defaults.go`
- `internal/app/daemon/app_headless.go`
- `internal/app/daemon/app_headless_codex_provider.go`
- `internal/app/daemon/app_headless_claude_profile.go`
- `internal/core/orchestrator/service_snapshot_prompt.go`
- `internal/core/orchestrator/service_config_prompt*.go`
- `internal/core/orchestrator/service_codex_provider.go`
- `internal/core/orchestrator/service_codex_resume_policy.go`
- `internal/core/control/feishu_command_config_summary.go`
- `internal/core/control/feishu_command_config_catalog.go`

必须逐项对应：

| 配置语义 | 产品 intent / state | Codex 当前做法 | Claude 当前做法 | 新 backend 必须确认 |
| --- | --- | --- | --- | --- |
| profile identity | stable id、name、revision/etag、status | `CodexAPIProfileRecord` + immutable revisions | `ClaudeProfileConfig` + normalized id | 是否需要 revision、etag、status、limit、name uniqueness |
| profile kind | native / OAuth / API / custom | native/OAuth/API 三类影响 resolver | built-in default vs custom profile | backend 是否有内建账号、自定义 provider、受限 profile |
| connection | base URL、provider id、endpoint id、capability set | `CodexConnectionContract` + CLI `model_providers.*` | `ANTHROPIC_BASE_URL` | native config 如何表达 provider/base URL；是否实例级 |
| credential | API key、OAuth、auth token、auth generation | secret child env、ephemeral credential store、OAuth probe state | `ANTHROPIC_AUTH_TOKEN` 或 inherit | secret 注入路径、是否污染全局 auth、generation 如何判断兼容 |
| main model | profile model、runtime override、observed model | thread policy + prompt override + Codex observed settings | env `ANTHROPIC_MODEL`，不支持 `/model` 临时切换 | launch-only 还是 runtime set；能否 observe effective model |
| review/small model | review model、small/title/summary model | `reviewModel`、managed catalog | `ANTHROPIC_DEFAULT_HAIKU_MODEL` | 是否有同义项；没有则 best_effort 或 unsupported |
| subagent model | default subagent / delegated task model | `agents.default_subagent_model` | `CLAUDE_CODE_SUBAGENT_MODEL` | 是否能表达 default subagent；是否影响 delegated task |
| instruction | profile role/system/developer instruction | `DeveloperInstruction` in thread policy / resume policy | `CODEX_REMOTE_CLAUDE_APPEND_SYSTEM_PROMPT` | 是 launch config、init request、prompt prefix，还是不支持 |
| reasoning | low/medium/high/max、thought level、variant | profile `ReasoningEffort` + prompt override + thread policy | `CLAUDE_CODE_EFFORT_LEVEL` + adaptive env | 值域如何映射；invalid value 是否 reject；是否需要 restart |
| access / permission | confirm/full/plan/mode/sandbox | approval/sandbox observed settings + prompt override | Claude permission mode / plan permission updates | 是否等价 sandbox；approval policy 和 tool permission 是否分开 |
| plan mode | plan on/off/auto/backend mode | `PlanMode` observed/prompt override | plan permission / ExitPlanMode flow | 是配置、tool flow、还是协议 request |
| context preference | default/272k/extended、auto compact limit | context preference revision -> context window/auto compact | extended context 用 model `[1m]` suffix | backend context window 如何表达；是否只是 metadata |
| model catalog metadata | context/cost/reasoning/tool/service tier | managed model catalog file | backend native or none | 是否需要生成 native model entries；是否能 observe list |
| MCP publication | Feishu tool service injection | CLI `mcp_servers.*` + bearer env | temp `--mcp-config` + env placeholder | config file、CLI、runtime session 参数，还是 unsupported |
| project config inheritance | workspace config 是否参与 | Codex native/project config + CLI overrides | Claude cwd config/inherit | 是否可 disable project config；默认是否继承 |
| data/auth/session isolation | config/data/state/cache roots | profile launch material 不整体搬 data root | env/settings，不搬用户目录 | 是否能只隔离 config 而继承 auth/session；不能混为一谈 |
| runtime dynamic config | `/model`、`/reasoning`、`/access`、`/plan` | command overrides / observed config / thread settings | 部分 launch-only，reasoning 可 prompt restart | 是否支持 runtime set；set 失败是否半应用 |
| observed effective config | backend 实际采用了什么 | `config.observed`、`thread.settings.updated`、`CodexEffectiveThread` | `config.observed` + profile snapshots | 必须能回传或由 launch contract 证明；不能只假设 |
| workspace defaults | workspace 下次默认模型/权限 | key 按 backend/profile 分区 | key 按 backend/profile 分区 | 新 backend profile 必须进 storage identity |
| bot capability settings | gateway/chat 默认 backend/profile/override | record 存 backend/provider/prompt override | record 存 backend/profile/prompt override | backend profile 和 overrides 必须可持久化/materialize |
| compatibility contract | 当前实例是否能复用 | connection contract + thread policy + admission ref | profile id + reasoning effort | 哪些配置变化要求 restart，哪些可 runtime update |
| diagnostics | required/best_effort/unsupported | runtime error codes + migration diagnostics | profile not found/settings prepare failed | compiler report 必须进 start failure/status/debug |

配置 mapping 的闭环：

1. `ProfileIntent` / config file / admin input。
2. normalized profile catalog + secret storage。
3. backend-specific compiler 生成 launch material。
4. daemon start command 携带 backend/profile/contract。
5. wrapper child env/args/temp files/session params 生效。
6. adapter 观察或证明 effective config。
7. orchestrator 更新 thread/surface/workspace defaults/status。
8. Feishu/admin config projection 显示 current/effective/override。

字段级断言要求：

- 每个 profile intent 字段都要有 target native config、launch material、observed config、product projection 四列结论。
- secret 字段必须单独标记 storage、transport、redaction、log exposure。
- launch-only 字段和 runtime-dynamic 字段必须分开；runtime 不支持时要定义 restart/fresh-start 策略。
- 继承和覆盖必须分别测试：继承全局/项目配置、只覆盖指定字段、禁止项目配置。
- context、sandbox、review model 这类非等价字段必须给 approximate mapping 和用户可见诊断。
- 不能把 backend native arbitrary config 当作产品 profile schema；产品 intent 必须 allowlist。

测试：

- compiler golden tests：`ProfileIntent -> LaunchMaterial + CapabilityReport`。
- config overlay black-box：真实 binary 证明覆盖优先级和继承行为。
- secret redaction tests：state/log/temp config/status 不泄漏。
- observed config tests：`config.observed` / `thread.settings.updated` / dynamic config response 能更新产品视图。
- restart compatibility tests：配置变化触发复用、runtime set 或 restart 的分支正确。
- Feishu/admin projection tests：current/effective/override 与 backend 真实能力一致。

### 4.6 Runtime protocol adapter

要确认 backend 的 wire protocol 如何转成 `relay.agent.v1`。

代码载体：

- `internal/app/wrapper/backend_runtime.go`
- `internal/app/wrapper/app_io.go`
- `internal/app/wrapper/command_phase_executor.go`
- `internal/app/wrapper/command_response_tracker.go`
- `internal/app/wrapper/runtime_turn_tracker.go`
- `internal/core/agentproto/types.go`

检查项：

- child framing 是 line JSON、stream-json、JSON-RPC、stdio RPC，还是 HTTP/WebSocket。
- prompt、turn、item、tool、plan、usage 是否能映射到 canonical events。
- request/response id 是否足够关联 command、turn、request。
- cancel 是 request 还是 notification；是否需要本地 ack。
- replay/hydration 是否能和真实新输出区分。
- helper/internal traffic 是否有明确标记。
- protocol error 是否进入 `protocol.notice` 或 `system.error`。

测试：

- mock backend server 覆盖正常 prompt stream。
- cancel race、late completion、duplicate event、unknown frame。
- command ack timeout 和 reject。
- hydration/replay 不投影到 Feishu 主消息。
- wrapper restart restore 不漂移 thread/turn/session id。

### 4.7 Canonical mapping contract

移植一个 backend 的主要工作不是抽象，而是逐项证明“backend 原始行为”能对应到我们现有 canonical contract 和产品行为。任何新 backend 都必须建立字段级 mapping 表，不能只证明 lifecycle 大致相似。

核心代码载体：

- `internal/core/agentproto/types.go`
- `internal/core/agentproto/plan.go`
- `internal/core/agentproto/thread_state.go`
- `internal/core/agentproto/thread_runtime.go`
- `internal/core/agentproto/token_usage.go`
- `internal/core/agentproto/model_catalog.go`
- `internal/core/agentproto/model_adjunct.go`
- `internal/core/agentproto/protocol_notice.go`
- `internal/core/orchestrator/service.go`
- `internal/app/wrapper/event_coalescer.go`
- `internal/app/wrapper/runtime_turn_tracker.go`
- `internal/adapter/codex/*`
- `internal/adapter/claude/*`

必须逐项对应：

| 语义面 | raw backend 必须确认什么 | canonical 目标 | 产品消费点 / 风险 |
| --- | --- | --- | --- |
| thread/session identity | session id、thread id、cwd、workspace、fork/source、archived/closed 状态是否稳定 | `thread.discovered`、`thread.focused`、`threads.snapshot`、`thread.lifecycle.updated` | `/list`、`/use`、resume、workspace claim；id 不稳会串会话 |
| turn identity | prompt request id、turn id、session id 如何关联，失败/cancel 是否仍有 completion | `turn.started`、`turn.completed`、`TurnCompletionOrigin` | queue 清理、active turn、auto-continue、final card |
| initiator / traffic class | remote/local/helper/hydration 是否可区分 | `Initiator`、`TrafficClass` | helper/hydration 不能刷 Feishu；remote turn 才驱动 queue |
| assistant text | 是 delta、snapshot、final full text，还是三者混合；是否会重复 | `item.started`、`item.delta`、`item.completed` with `ItemKind=agent_message`、`Delta`、`Metadata.text` | final output、delta coalescing、重复文本、空 final card |
| reasoning / thinking | 是否有 start/delta/stop；哪些片段可见；是否包含签名或隐藏块 | `ItemKind=reasoning_summary` 或 `reasoning_content`，必要时 hidden metadata / protocol notice | reasoning 展示、debug、避免泄漏不可展示内容 |
| plan snapshot | plan 是普通文本、tool result、独立 event，还是 request approval；step/status/explanation 字段是什么 | `turn.plan.updated` + `TurnPlanSnapshot`，以及 plan confirmation `request.started` | plan 卡片、完成后 plan proposal、Claude/Codex 差异最大 |
| tool lifecycle | tool call id、name、args、status、result、error、parent-child 关系 | `item.started/delta/completed` with stable `ItemID`、`Name`、`Status`、`Metadata` | exec progress、MCP progress、request resolution |
| command execution | shell command、cwd、stdout/stderr、exit code、terminal interaction 是否结构化 | `ItemKind=command_execution`、`command_execution_output`、`item.command_execution.terminal_interaction` | 命令进度卡、tail、失败总结 |
| delegated/subagent task | subtask start/output/complete 是否可识别，父 tool id 是否存在 | `ItemKind=delegated_task` + metadata | 子任务进度、隐藏/显示策略 |
| file change | edit/write/patch 的 path、kind、move path、diff 是否可得 | `ItemKind=file_change`、`item.file_change.patch_updated`、`FileChanges` | file summary、patch/review、final card |
| MCP tool call | MCP server/tool/status/progress/error 是否可识别 | `ItemKind=mcp_tool_call`、`MCPToolProgress`、capability state | MCP 进度卡、OAuth/reauth notice |
| request / permission | request id、tool call id、options、questions、approval kind、plan review、elicitation schema | `request.started` / `request.resolved` + `RequestPrompt` | Feishu 卡片、old-card replacement、不能卡 backend |
| config observed | model、reasoning、access、plan/mode、permission、scope 是否可观察 | `config.observed`、`thread.settings.updated`、dynamic config events if added | `/model`、`/reasoning`、`/access`、status |
| model catalog | list 模型、reasoning options、service tier、hidden/default、pagination、unsupported | `models.catalog.updated` | model picker、profile/status |
| token usage | input/cache/output/reasoning/total/context window 是 per-turn 还是 cumulative | `thread.token_usage.updated` + `ThreadTokenUsage` | usage display、context warning |
| model adjunct | reroute、verification、safety buffering、fallback model 是否有事件 | `turn.model_rerouted`、`turn.model_verification`、`turn.model_safety_buffering.updated` | 模型切换提示、安全缓冲 UI |
| runtime status | active/idle/system error、waiting approval/user input 是否可观察 | `thread.runtime_status.updated` + `ThreadRuntimeStatus` | target picker、resume eligibility、waiting state |
| thread goal/settings | goal budget/status、model/provider/sandbox/approval 更新是否可观察 | `thread.goal.updated`、`thread.settings.updated` | goal/status/debug、配置投影 |
| review lifecycle | enter/exit review、review thread/source、commit/uncommitted metadata | `thread.discovered` + review metadata，review lifecycle items | `/review`、review session tracking |
| compact lifecycle | compact command 是否有 turn lifecycle 或 error | `thread.compact.start` command -> turn/item/system events | `/compact` notice、compact problem handling |
| protocol degradation | raw 字段无法表达、被丢弃、重复、顺序异常 | `protocol.notice` with method/kind/severity/path | debug/status，不静默丢能力 |
| system error | backend fatal、command reject、auth missing、invalid request | `system.error` + `ErrorInfo` code/layer/stage | surface notice、start failure、queue cleanup |

字段级断言要求：

- 所有 raw fixture 都要保存原始 frame，不只保存转换后的 event。
- 每个 fixture 至少断言 `Kind`、`ThreadID`、`TurnID`、`ItemID`、`RequestID`、`Status`、`TrafficClass`、`Initiator` 中适用字段。
- delta 与 snapshot 混合时必须断言不会重复输出；final full text 只能进入 `Metadata.text` 或被去重规则消费。
- item id 必须稳定；没有 raw id 时，adapter 必须定义 deterministic synthetic id 规则。
- status 必须归一化；unknown status 要保留 raw metadata 或 protocol notice。
- metadata 可以保留 backend 私有字段，但产品依赖字段必须提升到 canonical typed field。
- raw 字段不可表达时，结论必须是 best_effort / unsupported / protocol.notice，不能静默丢弃。

测试分层：

- adapter golden tests：`raw frame(s) -> agentproto.Event(s)`，字段级断言。
- coalescer tests：相邻 delta 合并不跨 item/thread/turn/traffic class/metadata。
- orchestrator projection tests：canonical event 进入 `ApplyAgentEvent` 后产生正确 Feishu/status/state 结果。
- black-box capture tests：真实 binary raw frames 固化成 fixture，再喂 adapter golden tests。

候选报告的能力矩阵必须有一张 “canonical mapping coverage” 表。没有覆盖这张表时，不能给 production candidate verdict。

### 4.8 Headless launch 与实例生命周期

新增 backend 必须接入 daemon-managed headless，而不是只在 wrapper 能启动。

代码载体：

- `internal/app/daemon/app_headless.go`
- `internal/app/daemon/headlessruntime`
- `internal/core/state/surface_backend.go`
- `internal/core/orchestrator/service_surface_backend.go`
- `internal/core/orchestrator/service_headless_contract_switch.go`
- `internal/core/orchestrator/service_claude_headless_preflight.go`
- `internal/core/orchestrator/service_surface_daemon_command.go`

检查项：

- `SurfaceBackendContract`、`InstanceBackendContract`、`HeadlessLaunchContract` 是否携带该 backend/profile。
- `DaemonCommandStartHeadless` 是否携带 launch material 所需字段。
- 当前实例和目标合同如何判断 compatible。
- profile switch 时是复用、exact-thread restart、workspace route restart，还是 fresh workspace。
- prompt override 是否需要重启，或能 runtime set config。
- 启动失败是否释放 workspace claim、room reservation、pending queue。

测试：

- compatible instance reuse。
- profile mismatch restart。
- selected thread exact restart。
- workspace route restart。
- fresh workspace fallback。
- launch failure cleanup。
- prompt dispatch restart 后自动继续发送。

### 4.9 Surface resume 与恢复状态

daemon 重启后，surface 必须恢复到正确 backend/profile/workspace/thread。

代码载体：

- `internal/app/daemon/app_surface_resume_state.go`
- `internal/app/daemon/surfaceresume/state.go`
- `internal/core/orchestrator/service_surface_resume_test.go`
- `internal/core/orchestrator/service_surface_thread_selection.go`

检查项：

- resume state 是否持久化 backend/profile。
- 旧 state schema 是否兼容。
- backend/profile 缺失时是 invalid/reselect，还是 fallback。
- visible workspace 是否只复用 compatible backend/profile instance。
- missing target fallback 是否能启动 fresh headless。

测试：

- old entry load。
- new backend entry round-trip。
- backend/profile mismatch 不 attach。
- missing thread/workspace fallback。
- daemon restart 后 `/list`、`/use` 仍按 backend 分区。

### 4.10 Workspace、thread catalog 与 defaults

`/list`、`/use`、workspace default 不只是 backend `session/list`。

代码载体：

- `internal/core/orchestrator/service_thread_backend_partition.go`
- `internal/core/orchestrator/service_target_picker*.go`
- `internal/core/orchestrator/service_workspace*.go`
- `internal/core/state/workspace_defaults.go`
- `internal/core/agentproto/types.go`

检查项：

- session catalog 是否按 backend/profile/workspace 分区。
- persisted recent threads 是否按 backend 过滤。
- thread CWD/workspaceKey 是否可靠。
- workspace defaults 是否包含 backend/profile identity。
- backend 是否要求 cwd 才能 resume。
- thread history 是否支持该 backend。

测试：

- `/list` 不混入其他 backend/profile thread。
- `/use` workspace/thread 选择后 attach 或 restart 正确。
- workspace defaults 跨 profile 不串。
- recent persisted thread metadata merge。
- thread history read 支持或明确 reject。

### 4.11 Prompt dispatch、队列与输入载荷

Feishu 输入不是简单的一条 text prompt。接入前要确认 backend 能否承接队列、steer、图片、文件和自动触发任务。

代码载体：

- `internal/core/state/types.go`
- `internal/core/orchestrator/service_thread_selection.go`
- `internal/core/orchestrator/service_surface_thread_selection.go`
- `internal/core/orchestrator/service_plan_command.go`
- `internal/core/orchestrator/service_exec_command_progress*.go`
- `internal/core/control/feishu_commands_sendfile.go`
- `internal/app/wrapper/runtime_turn_tracker.go`

检查项：

- queued prompt、running prompt、steer prompt 是否都能映射。
- backend 是否支持 mid-turn steer；不支持时如何排队或 reject。
- staged images、staged files、send file 是否支持，或只支持文本。
- local image、remote image、document/resource 的降级策略。
- auto-continue、auto-whip 是否依赖 backend stop reason 或 runtime state。
- turn progress、exec progress、file change、reasoning summary 是否能投影。
- active queue item 在 restart/failure/cancel 后是否正确收敛。

测试：

- text prompt while idle。
- prompt while running -> queue 或 reject。
- steer all / turn steer。
- staged image/file dispatch。
- unsupported attachment reject。
- auto-continue/auto-whip unsupported backend 不触发。
- cancel/restart 后 active queue item 清理。

### 4.12 Feishu command、menu 与 config flow

命令可见性和 backend 能力是两套系统，必须分别评估。

代码载体：

- `internal/core/control/feishu_command_display_profiles.go`
- `internal/core/control/feishu_command_registry.go`
- `internal/core/control/feishu_config_flow.go`
- `internal/core/orchestrator/service_command_support.go`
- `internal/core/orchestrator/service_command_menu.go`
- `internal/core/orchestrator/service_config_prompt*.go`

检查项：

- 是否需要新增 command display profile。
- 每个命令是 native、approximation、passthrough、reject。
- `/model`、`/reasoning`、`/access`、`/plan` 是 launch-only 还是 runtime configurable。
- `/review`、`/patch`、`/cron`、`/auto-continue`、`/auto-whip`、`/primary`、`/coworkers`、`/sendfile` 是否 native、best_effort、deferred 或 unsupported。
- backend-specific reject 文案是否准确。
- 菜单阶段是否正确，例如 normal working、VS Code working、headless detached。
- config flow 是否能显示当前值、effective value、override value。

测试：

- command support table。
- menu visible families。
- unsupported command reject。
- config page current/effective/override projection。
- mode/profile switch action。
- 高级命令 hidden reject 不漏进默认 dispatch。

### 4.13 Request / permission / elicitation

工具审批、plan review、MCP elicitation、用户问题都要走统一 request 生命周期。

代码载体：

- `internal/core/agentproto/types.go`
- `internal/core/control/feishu_request_bridge.go`
- `internal/core/control/feishu_request_view.go`
- `internal/core/orchestrator/service_request*.go`
- `internal/core/frontstagecontract/request_control.go`

检查项：

- backend request option id 是否能原样 round-trip。
- option kind/label/style 是否只是展示，不承担业务推断。
- 多按钮是否超过 Feishu 限制，是否需要 structured form。
- request owner、reply anchor、old-card replace、capture gate 是否适用。
- unsupported request type 是否会 reject，不能让 backend 无限等待。

测试：

- approve/reject/dynamic option round-trip。
- stale card action reject。
- request resolved 后旧卡不可重复响应。
- structured form select/multi-select。
- unsupported request type safe reject。

### 4.14 MCP publication 与 MCP OAuth

Feishu 工具服务注入是产品能力，不是某个 backend 的自然能力。

代码载体：

- `internal/app/wrapper/app_child_mcp.go`
- `internal/core/toolservicecontract/toolservicecontract.go`
- `internal/app/daemon/app_mcp_oauth.go`
- `internal/core/orchestrator/service_mcp*.go`
- `internal/core/orchestrator/service_capability_state_projection.go`

检查项：

- backend 支持 stdio/http/sse/ACP 哪些 MCP transport。
- Feishu MCP 是 launch config、临时文件、runtime session 参数，还是不支持。
- bearer secret 是否不落盘，或只以 env placeholder 落盘。
- caller instance id 是否保留。
- MCP startup failure 是否能投影到 surface notice。
- `/mcpoauth` 是否适用该 backend；不适用时必须 reject 或引导本机处理。

测试：

- MCP 注入成功 path。
- missing tool service state。
- unsupported token type。
- bearer 不出现在临时 config 文件。
- MCP auth required notice。
- `/mcpoauth` unsupported backend reject。

### 4.15 Capability state、observability 与 diagnostics

如果无法解释为什么某个 backend 能力被降级，后续调试成本会非常高。

代码载体：

- `internal/core/agentproto/capability_state.go`
- `internal/core/orchestrator/service_capability_state.go`
- `internal/core/orchestrator/service_capability_state_projection.go`
- `internal/core/orchestrator/service_feishu_command_view.go`
- `internal/core/orchestrator/service_admin_page.go`
- `internal/app/wrapper/problem_reporter.go`

检查项：

- `/status` 是否显示 backend、profile、session/thread、capabilities。
- `/debug` 是否显示 native protocol capabilities、compiler diagnostics、dropped features。
- start failure 是否区分 binary missing、auth missing、profile invalid、protocol initialize failed。
- runtime capability update 是否可观察，例如 dynamic config、available commands、MCP status。
- error code 是否可用于测试断言，而不是只靠中文文案。

测试：

- status projection。
- debug/admin projection。
- start failure code。
- capability state update projection。
- problem reporter 不泄漏 secret。

### 4.16 Admin/config/API surface

能否接入生产，取决于用户能否配置、切换、恢复，而不是只能手写文件。

代码载体：

- `internal/core/control/feishu_admin_page_catalog.go`
- `internal/core/control/feishu_config_flow.go`
- `internal/core/orchestrator/service_admin_page.go`
- `internal/core/orchestrator/service_bot_capability_settings.go`
- `internal/config/configfile.go`

检查项：

- 是否需要 admin page 管理 profile。
- 是否需要 Feishu config flow 选择 profile。
- bot capability settings 是否有 read-only / gateway default 语义。
- config file migration 是否保留既有凭据。
- profile 删除/更新时现有 surface 如何处理。

测试：

- admin/config projection。
- config round-trip。
- profile update revision/etag。
- delete active profile 的处理。
- bot-level default override surface-local selection。

### 4.17 Security、isolation 与平台行为

backend 接入容易把 secret、代理、路径、权限做错。

代码载体：

- `internal/config/proxyenv.go`
- `internal/pathscope`
- `internal/execlaunch`
- `internal/app/wrapper/app_process.go`
- `internal/app/codexprofile/*probe*.go`

检查项：

- 生产 Go 代码必须通过 `internal/execlaunch` 启动子进程。
- localhost 调试和 child proxy env 是否遵守现有 proxy 规则。
- secret 不进入 logs、state、surface messages。
- 临时文件权限是否 0600。
- path/workspace root 是否有授权边界。
- Windows 行为是否需要额外验证。
- 上游 login/auth 命令是否会写全局状态。

测试：

- proxy capture/restore。
- secret redaction。
- temp file permissions。
- invalid workspace path。
- Windows wrapper mode smoke，如果该 backend 支持 Windows。

### 4.18 现有 backend 特化审计

评估新 backend 时，必须反向搜索现有 `Claude` / `Codex` 特化代码。凡是代码名、测试名、状态字段或错误码里出现 backend 名称，都要判断它代表的是产品语义、协议语义、配置语义，还是历史兼容语义。不能只看 adapter 和 loader。

建议搜索入口：

- `rg -n "Claude|claude" internal`
- `rg -n "Codex|codex" internal`
- `rg -n "func Test.*(Claude|Codex)|Claude|Codex" internal --glob '*test.go'`

必须归类的特化面：

| 特化面 | 当前代码信号 | 新 backend 要回答的问题 | 测试要求 |
| --- | --- | --- | --- |
| backend identity 默认值 | `agentproto/backend.go` 默认回 Codex | unknown backend 会不会误进 Codex 路径 | normalize/hello/capability table test |
| binary discovery / runtime requirements | `config/claude_binary.go`、`wrapper/codex_binary_resolver.go`、`admin_runtime_requirements*` | binary 如何发现、冻结、校验版本和 requirements | explicit env、PATH、missing、wrong binary、admin status |
| wrapper mode / bootstrap | `wrapper/app_headless*.go`、`wrapper/entry.go` | 是否需要专属 app-server mode、initialize frame、stdout bootstrap | bootstrap handshake、stderr pollution、wrong role |
| capability / auth probe | `app/codexprofile/*probe*.go`、`app_codex_*_probe*` | 是否需要启动前 probe 来证明协议/config/auth 可用 | probe timeout、contract mismatch、auth missing、diagnostic |
| profile compiler | `app_headless_codex_provider.go`、`app_headless_claude_profile.go`、`codexprofile/runtime_resolver.go` | profile intent 如何变成 launch material、contract、restart policy | compiler golden、black-box config overlay |
| local session store | `claudesessionstore/*`、`claudestate/session_catalog.go`、`codexstate/sqlite_threads.go` | backend thread catalog 是 native API、local DB、JSONL，还是不可读 | list/history/resume/workspace filter fixture |
| persisted thread metadata | `persisted_thread_catalog.go`、`codexstate/sqlite_threads.go` | recent workspaces、thread title、preview、model、reasoning 从哪里来 | catalog merge、probe thread filtering、cross-backend isolation |
| permission / access projection | `claudesessionstore/permission_mode.go`、`adapter/*/permission*`、`translator_overrides*` | native permission 与 access/plan/sandbox 是否等价 | observed permission、override、session grant、unsupported mode |
| plan semantics | `translator_plan*`、`service_plan_command.go`、`agentproto/plan.go` | plan 是配置、tool、request，还是普通文本 | plan snapshot、plan approval、decline/revise、final plan state |
| tool taxonomy | `claudeutil/ClaudeToolItemKind`、`adapter/codex/translator_requests*` | raw tool name/kind 如何分类到 command/file/MCP/delegated/dynamic | raw-to-canonical item kind matrix |
| assistant/reasoning text reconciliation | `translator_test.go`、`service_exec_command_progress*` | delta、final full text、thinking side channel 是否重复或泄漏 | delta/final de-dupe、hidden reasoning、coalescer |
| request bridge | `service_request_bridge*`、`translator_requests*`、`translator_tool_permissions*` | request id、option id、tool call id、plan review 如何 round-trip | approve/reject/form/stale/unsupported safe reject |
| runtime config observation | `service_config_prompt*`、`commands_permission_mode*`、`translator_resume_policy*` | effective model/reasoning/access/context 从 raw event 还是 launch contract 得出 | config.observed、thread.settings、restart compatibility |
| context preference | `profilecontextstate/*`、`profile_catalog_migration.go`、`codexprofile/runtime_resolver.go` | context window / compact limit / extended context 是否可表达 | preference revision、launch config、observed effective context |
| workspace profile snapshots | `claudeworkspaceprofile/*`、`service_claude_workspace_profile*` | workspace/profile/runtime override 是否要另存快照 | partition、restore、clear unsupported runtime override |
| surface resume compatibility | `surfaceresume/state.go`、`app_surface_resume_state.go`、`service_surface_contract_*` | old state、backend/profile mismatch、missing profile 怎么恢复 | old schema、conflict diagnostic、fresh fallback |
| headless preflight / restart | `service_claude_headless_preflight*`、`service_headless_contract_switch*` | profile/override 变化是复用、重启、exact resume 还是 fresh | reservation cleanup、auto-continue、restart restore |
| child restart restore | `adapter/codex/translator_restart_restore*`、`wrapper/backend_runtime*` | wrapper restart 后如何恢复 focused thread 和 pending command | restore suppress replay、failure event、pending drop |
| product commands | `translator_review*`、`translator_compact*`、`codexstate/turn_patch_*`、`service_review_session*` | `/review`、`/compact`、`/patch` 是否 native/unsupported/best_effort | command support、reject、lifecycle fixture |
| model catalog / adjunct | `translator_model_catalog*`、`translator_model_adjunct*`、`feishu_reasoning_options.go` | model list、reasoning options、reroute/verification/safety 是否存在 | catalog updated、unsupported catalog、adjunct events |
| token usage | `translator_token_usage*`、`agentproto/token_usage.go` | usage 是 per-turn、cumulative、cache 分项还是不可得 | accumulation、cache/reasoning fields、unknown handling |
| MCP publication / OAuth | `app_child_mcp.go`、`translator_mcp_oauth*`、`service_mcp*` | MCP config 注入和 OAuth 是否同协议可复用 | injection、bearer redaction、oauth URL/completion |
| Feishu command/menu branding | `feishu_command_*`、`service_feishu_command_view*`、`service_command_support*` | backend-specific command 是否隐藏、改文案、改 catalog backend | menu visibility、catalog backend、unsupported reject |
| admin/config APIs | `admin_codex_profiles*`、`admin_claude_profiles*`、`admin_codex_providers*` | profile CRUD、context preference、redaction、ETag 是否需要新 API | CRUD/redaction/ETag/migration |
| upgrade lifecycle | `app/codexupgrade/*`、`app_codex_upgrade*` | backend binary 是否纳入 self-upgrade 或明确不支持 | upgrade entry hidden/visible、unsupported status |
| media/final output | `service_image_output*`、`app_final_card*` | image/file/final block 依赖哪些 typed fields | image lifecycle、final card、file summary |

审计结论必须进入候选报告，不能只作为开发者脑内 checklist。每个特化面要给出 `supported`、`best_effort`、`unsupported`、`deferred` 或 `不适用`，并写明证据和测试位置。

## 5. 静态调研流程

静态调研阶段必须收集证据，不能只读 README。

1. 确认版本
   - 记录 upstream repo、commit、release、binary version。
   - 记录文档链接和源码路径。

2. 入口和协议
   - 找 CLI entry、stdio/http/ws server、启动参数、env。
   - 找协议 schema、examples、tests。

3. 配置加载顺序
   - global config、project config、env、CLI flag、inline config 的优先级。
   - 是否支持局部 overlay。
   - 是否会写回用户配置。

4. auth 和 secret
   - token 来源、login 流程、auth cache、可否 inline auth。
   - 是否支持实例级 auth material。

5. session 与 resume
   - new/list/load/resume/fork/close 是否存在。
   - load 是否 replay。
   - session id 是否稳定。
   - cwd/workspace metadata 是否可用。

6. tools/MCP/permissions
   - tool call event shape。
   - approval channel。
   - MCP transport 与 OAuth。
   - file/terminal reverse-RPC。

7. runtime config
   - model/reasoning/access/mode 是否 runtime set。
   - set 失败是否回滚。
   - options 是否 dynamic。

8. observability
   - stderr/log behavior。
   - error code。
   - debug endpoints。
   - tests 是否覆盖真实 entry。

9. product-specific commands
   - review/patch/cron/auto-continue/auto-whip 是否能支持。
   - 不支持时是否必须隐藏、reject 或降级。
   - 是否依赖 backend 专属 stop reason、diff、thread history、file change event。

10. 现有特化代码反查
   - 按 `Claude` / `Codex` 关键词列出命中的包和测试。
   - 对照 4.18 表逐项归类。
   - 候选报告必须说明每个特化面如何对应、弱化或不适用。

静态调研的输出是能力矩阵初稿，所有 unknown 必须进入黑盒测试计划。

## 6. 黑盒测试流程

真实可执行测试用于验证源码阅读假设。测试脚本应能重复运行，并记录 binary version、cwd、env、临时目录。

### 6.1 启动与握手

必须测：

- binary missing / wrong path。
- clean env 启动。
- 指定 cwd 启动。
- stderr 是否污染 stdout protocol。
- initialize/auth 或等价 handshake。
- 缺 auth 时的错误形态。

### 6.2 配置 overlay

必须测：

- global config 继承。
- project config 继承。
- 禁用 project config。
- inline/temporary override 优先级。
- 多实例不同 profile 并发启动。
- 退出后用户配置文件未变化。

### 6.3 Auth isolation

必须测：

- 复用系统 auth。
- 实例级 auth override。
- invalid token。
- auth refresh/login 是否写全局文件。
- secret 是否出现在日志、state、临时配置。

### 6.4 Configuration mapping fixture

每个 production candidate 必须建立 configuration mapping fixture。它和 raw protocol fixture 不同，重点是证明产品 profile intent 被正确编译、启动、生效、观察、投影。

必须包含：

- profile identity：id/name/revision/status。
- connection：provider/base URL/native endpoint。
- credential：secret storage、launch transport、redaction。
- main model、review/small model、subagent model。
- instruction/system/developer prompt。
- reasoning/thought level。
- access/permission/sandbox/mode。
- plan mode。
- context preference / context window / auto compact。
- model catalog metadata。
- MCP publication。
- project config inherit/disable。
- data/auth/session isolation。
- runtime dynamic config set。
- observed effective config。
- workspace defaults 和 bot capability settings。
- restart compatibility decision。

每个 fixture 的断言必须包含：

- 输入 profile intent。
- compiler 输出的 launch material 和 capability report。
- 真实 binary 启动时看到的 native config 行为。
- adapter/orchestrator 观察到的 effective config。
- Feishu/admin/status 投影的 current/effective/override。
- secret 是否不出现在 state/log/debug。
- required/best_effort/unsupported 结论是否和产品入口一致。

### 6.5 Session lifecycle

必须测：

- new session。
- list sessions。
- resume existing session。
- load existing session 是否 replay。
- fork/close 如果声称支持。
- 不同 cwd 的 session 是否可区分。

### 6.6 Prompt lifecycle

必须测：

- text prompt。
- image/file/resource 能力。
- long prompt。
- streaming delta。
- tool call。
- reasoning/plan。
- final stop reason。
- backend error。
- queued prompt。
- steer/mid-turn input。
- unsupported attachment。

### 6.7 Raw-to-canonical fixture corpus

每个 production candidate 必须建立 raw-to-canonical fixture corpus。黑盒测试时先捕获真实 backend 原始帧，再沉淀成 adapter golden tests。

必须包含：

- thread/session list、focus、resume/load/fork/close。
- turn start、message delta、message final snapshot、turn completion。
- reasoning/thinking start、delta、hidden block、completion。
- plan snapshot update、plan approval request、plan approval/decline resolution。
- tool call start、progress/delta、completion、failure。
- command execution stdout/stderr/exit。
- file edit/write/patch diff。
- MCP tool call progress、MCP auth required。
- request permission dynamic options、question/form/elicitation。
- config observed、dynamic config option update/set failure。
- token usage、model catalog、runtime status。
- protocol notice / unsupported field / malformed frame。

每个 fixture 的断言必须包含：

- 输入 raw frame 顺序。
- 输出 event 数量和顺序。
- event kind、thread id、turn id、item id、request id。
- status、traffic class、initiator。
- delta 和 final snapshot 的去重结果。
- typed field，例如 `PlanSnapshot`、`FileChanges`、`TokenUsage`、`RequestPrompt`。
- metadata 中保留的 backend 私有字段。
- 不可映射字段对应的 protocol notice。

### 6.8 Orchestrator projection fixture

adapter golden tests 只能证明字段转换正确，还必须证明产品层消费正确。对高风险 canonical event，要补 `ApplyAgentEvent` 或更上层 service tests。

必须覆盖：

- `turn.completed` 清 active turn、清 request、dispatch next。
- `item.completed(agent_message)` 形成 final output。
- `turn.plan.updated` 更新 plan card。
- `item.completed(file_change)` 进入 final file summary。
- request started/resolved 更新 Feishu 卡片状态。
- runtime status waiting approval/user input 影响 target picker/status。
- protocol notice 只进入 notice/debug，不污染主消息。
- hydration/internal helper traffic 不投影到 Feishu 主消息。

### 6.9 Cancel 和并发

必须测：

- prompt running 时 cancel。
- cancel 后 late event。
- cancel unknown session。
- second prompt while running。
- wrapper restart during running，如果目标要求支持。

### 6.10 Request / permission

必须测：

- allow once。
- allow for session / always。
- reject。
- dynamic option id。
- question/form elicitation。
- stale/invalid response。
- backend 等待超时。

### 6.11 MCP

必须测：

- 注入 Feishu MCP。
- tool service missing。
- bearer invalid。
- MCP startup failed。
- MCP auth required。
- MCP OAuth 命令是否适用。

### 6.12 Runtime config

必须测：

- list dynamic options。
- set model。
- set reasoning/thought level。
- set mode/access。
- set invalid option。
- set invalid value。
- set 失败后当前值不半应用。

### 6.13 Recovery

必须测：

- daemon restart 后 surface resume。
- wrapper child restart restore。
- backend process exit。
- workspace route restart。
- profile switch restart。
- missing workspace/thread fallback。

### 6.14 Product command compatibility

必须测：

- `/review` 是否支持，或明确 reject。
- `/patch` / turn patch rollback 是否支持，或明确 reject。
- `/cron` 是否支持后台触发。
- `/auto-continue` 是否支持自动续跑。
- `/auto-whip` 是否支持自动催办。
- `/primary` / `/coworkers` 是否仍是产品层命令，不被 backend 限制误伤。
- `/sendfile` 对该 backend 的附件策略。

### 6.15 Existing specialization regression

必须把 4.18 的特化面转成回归测试计划。最低要求：

- backend-specific test names 中出现的能力，要么复用现有测试改成 backend-parameterized，要么新增该 backend 的 supported/unsupported 测试。
- 当前只在 Claude 或 Codex 存在的能力，不能默认继承；必须有 explicit reject 或 hidden command 测试。
- session store、thread catalog、profile compiler、request bridge、plan、tool taxonomy、runtime config、restart restore、admin/config API 至少各有一个正向或负向测试。
- 新 backend 的 fixture corpus 必须标注覆盖了哪些现有特化面，没有覆盖的写入候选报告风险列表。

## 7. 测试分层与证据要求

候选报告里每个 supported 结论必须至少有一种证据：

| 测试层 | 证明什么 |
| --- | --- |
| source inspection | 上游代码确实有能力入口 |
| upstream test | 上游已有测试覆盖真实路径 |
| local black-box | 本机真实 binary 行为符合预期 |
| adapter unit test | 我们的 translator/adapter 映射正确 |
| compiler golden fixture | 产品 profile intent 到 launch material / capability report 的配置对应正确 |
| config black-box fixture | 真实 binary 的配置继承、覆盖、auth、observed config 行为符合预期 |
| adapter golden fixture | raw backend frame 到 canonical event 字段级对应正确 |
| orchestrator test | 产品状态、restart、resume、Feishu route 正确 |
| projection fixture | canonical event 到产品 state/Feishu/status 的消费正确 |
| negative test | 缺能力/错误输入不会卡死或误投影 |

production candidate 的 required 能力必须同时有 local black-box 或等价真实路径证据，以及我们侧 adapter/orchestrator 测试计划。

## 8. 候选报告模板

每个 backend 候选报告建议使用以下结构。

```md
# <Backend> 接入评估

> Type: `draft`
> Updated: `YYYY-MM-DD`
> Summary: 记录 <Backend> 按 backend integration playbook 的接入结论。

## 1. 结论

- Verdict: production candidate / constrained candidate / protocol reference / experimental / not recommended
- 推荐接入阶段：
- 最大风险：
- 必须上游补齐：

## 2. 调研对象

- Repo:
- Commit / version:
- Docs:
- Binary:
- 本地测试环境：

## 3. 能力矩阵

| 能力 | 等级 | 证据 | 处理结论 |
| --- | --- | --- | --- |
| profile overlay | required | source + black-box | supported |

## 4. 接入地图差异

按 playbook 第 4 章逐项说明。

## 5. Canonical mapping coverage

| raw 语义 | canonical 目标 | 证据 | 缺口/降级 |
| --- | --- | --- | --- |
| agent text delta | item.delta(agent_message) | fixture | supported |

## 6. Configuration mapping coverage

| 产品配置 intent | backend native 目标 | launch material | observed/product projection | 证据 | 缺口/降级 |
| --- | --- | --- | --- | --- | --- |
| main model | provider/model | env/config/session option | config.observed + status | compiler + black-box | supported |

## 7. 黑盒测试计划

列出每个 unknown / required / risky best_effort 的测试用例。

## 8. Existing specialization audit

| 现有特化面 | 当前 Claude/Codex 代码信号 | 新 backend 结论 | 证据 | 测试/降级 |
| --- | --- | --- | --- | --- |
| local session store | `claudesessionstore/*` | unsupported | source | `/list` hidden 或 native list 替代 |

## 9. unsupported / best_effort 决策

| 能力 | 原因 | 产品处理 |
| --- | --- | --- |

## 10. 实施建议

- adapter：
- compiler：
- orchestrator：
- Feishu：
- admin/config：
- launcher/entry：
- queue/attachments：
- fixture corpus：
- config fixture：
- tests：
```

## 9. Verdict 判定规则

### 9.1 production candidate

必须满足：

- 可实例级启动，不污染用户配置。
- launcher/daemon/wrapper 都能携带正确 backend/profile 身份。
- required profile intent 可表达。
- configuration mapping fixture 覆盖 required profile intent、secret、observed config、product projection。
- auth/secret 有可控注入路径。
- session new/list/resume 基本可用。
- prompt/cancel/request 可映射。
- raw-to-canonical fixture corpus 覆盖 required canonical mapping。
- prompt queue、attachments、unsupported command 都有明确产品处理。
- Feishu MCP 至少有明确 supported 或 unsupported 结论。
- surface resume、workspace restart、profile switch 有设计和测试计划。
- failure mode 可诊断。

### 9.2 constrained candidate

允许：

- 只支持部分 profile intent。
- 不支持部分 Feishu 命令。
- MCP 或 runtime config 是 best_effort。

但必须：

- 产品入口清楚显示限制。
- unsupported 命令不静默执行。
- 不复制或污染用户全局配置。
- 不把 unknown 当作 supported。

### 9.3 protocol reference

适用于 ACP 这类协议验证目标。允许 loader/profile 不完整，但必须把协议能力测清楚：

- handshake。
- session lifecycle。
- prompt stream。
- cancel。
- request permission。
- config options。
- available commands。
- hydration/replay。

### 9.4 not recommended

命中任一条件，应判为不建议接入：

- 不能实例级隔离或局部覆盖 required profile。
- 需要复制整套用户数据目录才能运行，且会带来凭据/状态同步风险。
- session/resume 无法满足产品最低要求。
- auth 必须交互式 terminal，且没有 headless fallback。
- protocol stdout/stderr 混乱，无法稳定 framing。
- required 能力只能靠脆弱时序或 UI 自动化实现。

## 10. Double-check 清单

写完候选报告后，必须反向对照当前代码载体检查一次：

- `agentproto/backend.go`：backend normalize/display/capabilities。
- `agentproto/wire.go`：hello/command/event 是否带足身份和能力字段。
- `wrapper/entry.go`、`launcher/role.go`、`launcher/launcher.go`：入口和角色调度。
- `state/surface_backend.go`：surface/instance/launch contract。
- `state/types.go`：root/surface/instance/pending headless/request state。
- `state/workspace_defaults.go`：workspace defaults 分区。
- `state/bot_capability_settings.go`：bot 默认能力设置。
- `daemon/surfaceresume/state.go`：resume state schema。
- `daemon/app_headless.go`：start env、launch args、failure cleanup。
- `wrapper/backend_runtime.go`：runtime interface、restart restore。
- `wrapper/app_child_mcp.go`：Feishu MCP publication。
- `control/feishu_command_display_profiles.go`：命令显隐与支持类型。
- `control/feishu_config_flow.go`：配置页和当前值投影。
- `orchestrator/service_headless_contract_switch.go`：workspace/profile restart。
- `orchestrator/service_claude_headless_preflight.go`、`service_codex_resume_policy.go`：backend 专属 preflight/resume policy 是否有等价处理。
- `orchestrator/service_claude_workspace_profile.go`、`daemon/claudeworkspaceprofile/state.go`：workspace profile snapshot 是否要新增或明确不适用。
- `orchestrator/service_surface_resume*.go`：resume/fresh-start。
- `orchestrator/service_thread_selection*.go`：prompt queue、steer、selected thread。
- `orchestrator/service_request*.go`：request lifecycle。
- `orchestrator/service_capability_state*.go`：capability/status projection。
- `orchestrator/service_workspace*.go` 和 `service_target_picker*.go`：workspace/list/use。
- `orchestrator/service_plan_command.go`、`service_exec_command_progress*.go`：plan、exec、reasoning、file change 投影。
- `config/configfile.go`：config schema、migration、secret handling。
- `config/codex_profiles.go`、`config/claude_profiles.go`、`config/claude_runtime_settings.go`：profile 字段、runtime settings、context preference 是否有新 backend 对应。
- `adapter/codex/*`、`adapter/claude/*`：raw-to-canonical fixture 是否覆盖所有 canonical event 和命令。
- `claudesessionstore/*`、`claudestate/*`、`codexstate/*`：local session/history/catalog/turn patch 是否需要新 backend 实现或显式 unsupported。
- `app/codexprofile/*probe*.go`、`app/codexupgrade/*`：probe、capability gate、upgrade lifecycle 是否适用。

如果报告里没有覆盖某个载体，必须说明“不适用”的理由。没有理由就是漏项。

# 飞书 Agent 团队：Codex／Claude 运行合同附录

> Type: `draft`
> Updated: `2026-11-21`
> Summary: 按现有代码与固定 Raft 快照拆解两后端的会话时序、上下文注入、回执和恢复缺口，定义实施接线与验证要求。

## 1. 范围与证据等级

本文是[技术主文档](./feishu-agent-team-technical-design.md)的专项附录，不另立产品架构。产品对象、项目约定、人的参与和交付边界以技术主文档及[产品主文档](./feishu-agent-team-product-design.md)为准；[执行设计](./feishu-agent-team-execution-design.md)保留推导依据。这里解释怎样把那些对象接入真实 runtime；不要求用户管理 thread、进程或 worktree。

- **现有事实**：本仓 `dbd006c5` 的源码及文末定向测试。测试证明翻译和协议模拟行为，不能替代安装版本的原生验收。
- **Raft 事实**：`botiverse/raft-source` 固定快照 `5214e34e726ed715f455e78c96fa521791ef2de3`，只用于对照，不表示本仓已经获得同样能力。
- **拟新增合同**：为完整团队运行目标需要实施的字段、状态和接线。
- **未证明**：需要真实 Claude Code／Codex 二进制与协议帧验证；不得用 API、其他产品或上游源码的能力直接代替。

## 2. 产品身份与原生身份

| 对象 | 应负责什么 | 不能等同于什么 |
| --- | --- | --- |
| WorkContext | role＋scope 下某条工作流的任务输入、检查点、资料／约定版本、恢复线索 | 飞书群、仓库路径或完整模型 transcript |
| WorkerSession | 一个 WorkContext 的后端绑定、控制权、原生会话代次、采用配置与执行位置 | 一个 Bot 的全局单例，或仍由 surface 掌握的当前 thread |
| ExecutionAttempt | 一次启动／执行尝试，冻结输入 ID、任务 revision、使用的配置／上下文版本及结束证据 | 项目最终完成；也不是所有后端都存在的原生 turn ID |
| Native binding | backend、host、wrapper instance、process generation、native session/thread、turn 及来源 | 用任一 ID 单独反推 role、项目或消息受众 |

**现有事实**：[Command](../../internal/core/agentproto/types.go:237) 有 command ID、Origin、Target、Prompt、Overrides 和 CodexResume；[PromptDispatchPlan](../../internal/core/agentproto/prompt_dispatch.go:20) 已区分 start new、resume existing、ephemeral fork，但包含 surface binding policy。当前[远程 turn binding](../../internal/core/orchestrator/service_dispatch_core.go:14)仍绑定 surface、queue item、reply message，且 pending／active ownership 按 instance 查询。

**拟新增合同**：执行 owner 从 surface 上移到 WorkerSession。一次 Attempt 保存确定的 `worker_session_id + session_generation + attempt_id`，每次投递另有 `delivery_id/command_id`；消息受众由 Conversation／Delivery 决定。Steer 是向已有 Attempt 添加输入回执，不因一个补充消息就重建任务；compact／恢复等维护操作独立记录，不能冒充业务执行成功。一次高层 Attempt 可包含启动 RPC、恢复 RPC、原生 turn；这些子操作必须保留关联，不要求二者强制一对一。

Claude 的 `relay-claude-turn-N` 是 [translator 本地生成](../../internal/adapter/claude/translator.go:240)的 turn ID；原生 session ID 来自 `system.init.session_id`。重启 wrapper 后计数可能重来，必须带 instance／generation 去重。Codex 的 turn ID 来自 app-server。两者都不可用“最新活动会话”替代精确绑定。

## 3. 首次启动与空闲续跑

### 3.1 Codex：进程初始化、thread、turn 三个阶段

**现有事实**：

1. [launchCodexChildSession](../../internal/app/wrapper/app_child_session.go:36) 以 wrapper 的 WorkspaceRoot 启动私有 stdio app-server；launch args/env 决定进程级 provider、MCP 等配置。
2. Headless [synthetic initialize](../../internal/app/wrapper/app_headless.go:30) 等待指定 request ID 的成功响应，再发 initialized；这是进程协议 ready，不是工作 thread 已恢复。VS Code source 不发送这套 synthetic 初始化，由父客户端握手。
3. `prompt.send/start_new` 经 [translatePromptSendThreadStart](../../internal/adapter/codex/translator_commands.go:293) 发 thread/start，保存 request ID → Command。
4. [thread/start 响应](../../internal/adapter/codex/translator_observe_server.go:153) 提取 thread ID，然后才构造 turn/start。恢复旧 thread 时先 thread/resume，成功后按原 Command 继续；若已是 currentThreadID，则[直接 turn/start](../../internal/adapter/codex/translator_commands.go:371)，不会再次 resume。
5. 原生 turn/item 通知经 [stdoutLoop](../../internal/app/wrapper/app_io.go:84) 归一化、回传 relay，并将 follow-up 帧送回 child。真正开始／结束以关联后的原生事件为准。

当前失败路径不全有完整关联：例如 thread/start 拒绝构造的 turn.completed 只有失败内容和 origin enum，没有 CommandID。团队模式必须补齐 command/session/attempt 关联，不能继续依赖“该 instance 只有一个待启动请求”来归属错误。

### 3.2 Claude：CLI 进程绑定一个当前原生 session

**现有事实**：

1. [buildClaudeChildLaunch](../../internal/app/wrapper/app_child_session_claude.go:71) 使用 `--print --input-format stream-json --output-format stream-json`、partial/replay flags，以及 stdio permission 控制。恢复通过 launch 参数 `--resume <session>`，临时审阅 fork 另加 `--fork-session` 和受限 reviewer 定义。
2. [bootstrapClaude](../../internal/app/wrapper/app_headless_claude.go:17) 发 control_request/initialize，等成功 response。profile instruction 从 wrapper 环境读取，以 `request.appendSystemPrompt` 附加。这里没有 Codex 的 thread/start RPC。
3. [translatePromptSend](../../internal/adapter/claude/commands.go:48) 先登记 pending turn；需要切权限模式时先发 set_permission_mode，并由 [phase gate](../../internal/app/wrapper/backend_runtime.go:534) 等成功后才发 user 消息。
4. [user 帧](../../internal/adapter/claude/commands.go:417) 仅包含 type/message，没有 command UUID 或目标 session。`system.init` 填入真实 session/model/cwd；assistant 流触发合成 turn.started，`result` 触发 [turn.completed](../../internal/adapter/claude/observe.go:890)。文本片段完成不等于整个 turn 结束。
5. 同一当前 session 的空闲续跑直接写下一条 user 帧。换 session 或明确新建时，由 [restartPlanForCommand](../../internal/app/wrapper/backend_runtime.go:753) 请求 child restart，重启完成再重新翻译同一 command；completedRestartCommandID 用于避免再次触发同一 restart。

**接口冲突，实施前必须处理**：

- 当前 [lookupStoredResumeTarget](../../internal/app/wrapper/backend_runtime.go:886) 强制目标 session 属于 wrapper 的 workspace；实际 `cmd.Dir` 始终取 a.config.WorkspaceRoot。把 Agent 新建工作目录登记到 Target.CWD，并不会让现存 Claude wrapper 自动换目录。新位置需要匹配 launch contract 的新 wrapper／明确实现的位置切换，不应删除此校验后假装已支持。
- 若请求另一个 session 而 catalog 未找到，restartPlanForCommand 返回无 restart；随后 translator 的 [canonicalThreadID](../../internal/adapter/claude/translator.go:335) 优先当前 session。团队入口必须将明确目标缺失判为 resume_target_missing，绝不能把输入发给当前其他工作。允许 fresh rebuild 时由恢复用例显式新建代次并记录原因。
- bootstrap 成功仅证明 control 协议 ready；`system.init` 给出 ID 不证明该新 session 已持久到可恢复。需要单独记录 observed session 与 durable resume checkpoint。

### 3.3 拟新增统一握手，保留后端差异

顺序为：持久化 Attempt 与输入范围 → claim WorkerSession／冻结 launch contract → 分配匹配宿主 → backend 初始化 → 验证 native binding → 发送输入 → 记录对应接收证据 → 观察运行／结束。无法提前获得 Claude 新 session ID 时，允许启动中的暂时空绑定，以 instance/generation/command 关联首次事件，再原子补齐；不能先编造 thread ID。

统一对外字段应是阶段与证据，不是虚构两后端都有同一个 RPC：process_ready、session_observed、input_queued、native_accepted（有原生证据时）、turn_started、turn_terminal、resume_checkpoint。部分阶段可以缺少原生证明，状态应明确 pending/unknown，而不是猜成功。

## 4. 忙时输入、停止与回执

| 能力 | Codex 现状 | Claude 现状 | 团队合同 |
| --- | --- | --- | --- |
| 忙时补充 | [turn/steer](../../internal/adapter/codex/translator_commands.go:84) 携带 expectedTurnId；wrapper 有 response gate | [translateTurnSteer](../../internal/adapter/claude/commands.go:80) 仅本地校验 active thread/turn，再写普通 user NDJSON | 两者保留不同 receipt strength；已写入不等于已消费 |
| 开始命令 ACK | 常规 start 先排入 writeCh，后续由 native 响应／事件判断 | 权限切换 gate 后把 user 帧排入 writeCh | 不用通用 ACK 单独关闭 inbox 输入 |
| Steer ACK | 原生 RPC 成功／拒绝可见 | 没有对应原生 request ID gate，user replay 目前也未作为输入回执归一化 | Claude 的不确定输入保留至后续工作确认／对账；不编造 consumed |
| 停止 | threadId＋turnId 的 turn/interrupt | control_request/interrupt 作用当前进程；本地状态标记 interrupt requested | 停止在 WorkerSession/generation 校验后直接到 runtime，实际结束与请求已发送分开 |

[executeCommandPhases](../../internal/app/wrapper/command_phase_executor.go:20) 只对指定 ResponseGate 等待；其余是 channel enqueue。[relay CommandAck](../../internal/adapter/relayws/client.go:367) 在 OnCommand 返回后发送 Accepted=true。因此“群消息成功 → ACK → 模型已接收 → 任务完成”不能合并成一个状态。

**拟新增**：每个输入持久化 delivery ID、内容 hash、来源 scope、目标 session generation、投递方式与阶段。消息先进入 inbox，按同一 WorkerSession 的顺序推进。重要修正可 steer；后端不支持或活动 turn 已变化时继续保存在 inbox，由下一安全边界消费。仅投递摘要／inbox 变化提示时，Agent 再通过 CLI 读取并确认具体输入 ID，形成业务层消费证据；此确认也不代表任务已完成。

不要把未知结果一律转成再次 turn/start。比如 turn/steer 已写入但断线，重发完整命令可能重复执行。恢复应检查原生状态、transcript／检查点和团队操作回执；仍无法确定则保持结果未知并发恢复输入，要求检查已发生效果，不能声称原生调用 exactly-once。

## 5. 上下文与配置何时生效

### 5.1 分三类，不能只追加一段提示词

| 配置类别 | 现有落点 | 拟新增接线 |
| --- | --- | --- |
| 进程级 | binary、provider/env、Claude settings、MCP、cwd | 有版本的 LaunchContract；匹配后复用，否则安全边界重建宿主 |
| 常驻说明 | Codex thread start/resume developerInstructions；Claude initialize appendSystemPrompt | 组合 profile＋角色＋平台核心合同；记录 instruction revision 与已应用阶段 |
| 每次工作输入 | Codex turn/start/steer input；Claude user message | WorkInputEnvelope：run_ref、输入 ID、任务/参与约定 revision、必要记忆和资料引用、变化摘要；详细方法按需 CLI 读取 |

**现有事实**：Codex [applyCodexResumePolicyToThreadParams](../../internal/adapter/codex/translator_config.go:79) 把 `DeveloperInstruction` 放到 developerInstructions；但[同 thread 的 turn policy](../../internal/adapter/codex/translator_config.go:185)只更新 model／reasoning 等，不重新应用该说明。单纯在下一 Command 改 DeveloperInstruction 不会更新 currentThread 的常驻角色规则。线程启动未给 typed policy 时还会复制 latestThreadStartParams；这可用于原交互继承，不能用于不同团队身份之间的隐式模板复用。

Claude profile [ApplyClaudeProfileLaunchEnv](../../internal/config/claude_profiles.go:186) 写 wrapper 环境，settings overlay 在 child launch 生成；initialize 再读 wrapper 的 `os.Getenv`。重启 child 会重发旧 wrapper 环境里的说明，**不等于**自动载入 daemon 最新 profile／角色版本。需要新的 launch snapshot 传入 wrapper，或在安全边界启动采用新 snapshot 的 wrapper。现有权限模式可单独 control 更新，不代表 model、provider、MCP、角色说明都可热改。

**拟新增**：构造一次可审计的 InstructionBundle，保留既有 profile instruction 和项目／仓库规则，不覆盖 native baseInstructions、不改写用户 CLAUDE.md／AGENTS.md。平台身份与工具边界、用户角色规范、项目规范和任务输入分别标来源；正文资料不能升级成高优先级指令。配置记录 desired revision 与 applied revision；只看到 native model 回报时，不能推断整个 bundle 已生效。

规则变更分两条路径：当前任务纠正以 steering／下一输入传递；常驻规则在后端已验证的 resume/relaunch 边界重新应用。Codex 需显式安排 resume／重新绑定，而不能被 currentThread fast path 跳过；Claude 需新的 launch/init snapshot。后端切换不能把 Claude session ID 交给 Codex 恢复，只能使用同一 WorkContext 检查点建立新的 WorkerSession 绑定。

### 5.2 工具身份与每轮 run_ref

现有 [app_child_mcp.go](../../internal/app/wrapper/app_child_mcp.go:20) 为 Codex 加 `-c mcp_servers...`，为 Claude 加 `--mcp-config`；调用身份以 caller instance 为线索，VS Code source 明确不自动发布。团队 CLI 的 process credential 应绑定 role/scope/session generation，输入提供本次 attempt/run_ref；服务端核对二者，不信任模型任意填写另一个项目 ID。

环境在进程创建后不会因为每轮输入而改变。同一 WorkerSession 跨 Attempt 时，不能只设置一次 RUN_ID 环境变量就认为后续工具自动归属正确。采用 session credential＋显式 run_ref，并由服务端校验其固定执行绑定；缺失 run_ref 时拒绝操作，不用当前最新 Attempt 补全；如果允许一个原生 session 并发多个 turn，就必须做到工具级精确关联，不能从“最近一个 Attempt”推断。此附录建议同一 WorkerSession 只保留一个主动执行 owner，并行工作分配不同 WorkerSession，项目并行能力不缩减。

CLI 的方法发现／读取可以共享业务层；不以 dynamicTools 存在就推断按需加载已实现。此处不改变主文档的薄 CLI 主入口和可选 MCP 方向。

## 6. 压缩、重建与崩溃恢复

### 6.1 压缩不能等同于重置工作

**Codex 现有**：[thread.compact.start](../../internal/adapter/codex/translator_commands.go:19) 会在非 current thread 时先 resume，再发 thread/compact/start。现有 [service_compact](../../internal/core/orchestrator/service_compact.go:13) 要求已选 surface thread、无 active turn／待派发／steer／队列，是交互流程约束，不是新团队调度必须清空所有 inbox 的理由。

**Claude 现有**：translator 命令入口没有 compact 分支；[system observer](../../internal/adapter/claude/observe.go:14)只处理 init 和权限状态，没有归一化 compact_boundary／compacting 状态。不能因为原生 Claude 可能自动压缩，就称本仓已具备维护命令和压缩完成合同。

**拟新增**：维护取得 session 级屏障，暂停向该 native session 投递新输入但保留 inbox；先保存任务输入边界、未完事项、资料与执行位置引用。Codex 使用原生 compact 并跟踪真实完成／失败；Claude 先补自动 compact 事件观测，显式 compact 入口须真实协议验证，不能直接把 `/compact` 普通文本当已支持 RPC。可用的替代是明确的 checkpoint → 新 session generation → 恢复输入，旧 transcript 与检查点引用保留，不能把它描述为保留了原生完整上下文。

压缩后重新验证角色／scope、工具身份和仍待处理输入；持久记忆、参与约定、资料与任务状态不依赖模型压缩摘要。原生自动记忆的隔离或关闭需要单独接线，不因增加平台 MemoryEntry 就自动解决。

### 6.2 子进程重启不等于持久恢复

现有 [restartChildSession](../../internal/app/wrapper/app_child_session.go:227) 在新 child 前停止旧 IO、关闭 pipes，再启动下一 generation；[stdoutLoop](../../internal/app/wrapper/app_io.go:114)过滤旧 generation。Codex 通过[restore frame](../../internal/adapter/codex/translator_restart_restore.go:10)恢复 current thread；Claude 用启动参数恢复，其 BuildChildRestartRestoreFrame 为空。不能把 Claude 的无 restore frame 成功事件理解为已经验证 native history 恢复成功。

意外 child exit 时，[App.Run](../../internal/app/wrapper/app.go:467)先做 exit reconciliation 再退出 wrapper，不在该函数里实现长期重启循环。relay events 使用[内存 outbox](../../internal/adapter/relayws/client.go:164)，ACK 跨 websocket 重连的保留机制不等于 wrapper 进程崩溃后的持久日志。

**拟新增恢复顺序**：

1. 工作库存输入意图和 command ID，再执行外部投递；落库与外部投递之间仍有未知窗口，状态必须表达。
2. 发现失联后 fence 原 session generation，确认旧进程／控制权，防止两个宿主共同消费。失联不立即证明工具进程已停。
3. 核查 native session、工作目录和已完成副作用。可恢复则采用原绑定与新 process generation；不可恢复则通过显式 recovery Attempt 建新 session generation。
4. 重新应用 launch／instruction snapshot，发送 checkpoint 和未确认输入 ID。团队 CLI 的有副作用操作以 operation ID 去重；原生 shell/Git 操作依据现场与回执核查，不盲重放。
5. turn terminal 只结束对应 NativeTurn；存在已授权原生 continuation 时，同一业务 Attempt/run_ref 持续到该 continuation 真正结束或暂停确认，不能未注入新 run_ref 就切换 Attempt。Task、成果交接、Delivery、issue 状态按各自合同确认，不继承“模型停止＝工作已完”。

## 7. Raft 固定快照提供了什么证据

| 已核实源码 | 事实与可借鉴边界 |
| --- | --- |
| [Codex buildCodexThreadRequest](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/codex.ts#L1191) | standingPrompt 通过 developerInstructions 在 start/resume 重发，保留原生 base；源码明确说 compaction/session persistence 仍需后续验证，不能引用它保证压缩后绝不丢规则 |
| [Codex encodeStdinMessage](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/codex.ts#L1706) | idle 使用 turn/start，busy 用 expectedTurnId 的 turn/steer；记录 pendingDeliveryRequests，并在 RPC 错误时发 delivery_error，不把 write 成功当最终交付 |
| [Claude launch args](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/claudeLaunch.ts#L118)与 [spawn](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/claude.ts#L121) | Raft 用 `--append-system-prompt-file`，需要时 `--resume`；cwd 取工作位置，首条 user 消息写 stdin。这与本仓 initialize.appendSystemPrompt 是不同注入路径，不能假设其版本兼容性完全相同 |
| [Claude driver capability](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/claude.ts#L60) | busy 直接走 native stream-json queued-command，fresh session 的 delivery-ready 在 turn_end，既有 resume session 在 launch；注释区分“看到 ID”与“已可恢复” |
| [Claude normalize compaction](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/claudeEventNormalizer.ts#L450) | 把 status=compacting、compact_boundary 转为压缩事件；这证明它有观测接线，不证明本仓已有手动 compact API |
| [RuntimeSession.send](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/runtimeSession.ts#L175) | 正在停止／关闭不再写；idle 开新 turn origin，busy 保留当前 origin；返回 acceptedAs 表示驱动发送阶段，不等于任务／文件已经交付 |
| [Claude memory](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/claude.ts#L153) | 关闭 Claude auto memory，避免与工作区 MEMORY.md 双份规则。我们的个人／项目 MemoryEntry 要明确实现隔离策略，不能照搬一个环境变量就称所有后端隔离完成 |

Raft 的[Codex 恢复降级](https://github.com/botiverse/raft-source/blob/5214e34e726ed715f455e78c96fa521791ef2de3/packages/daemon/src/drivers/codex.ts#L1555)对特定错误尝试 unarchive／fresh thread，并加入 recovery notice；这是其实现选择。我们不得在恢复失败时悄悄换成当前其他 session，也不能把任意 provider／权限错误都当作可新建重试。

## 8. 实施文件与新增契约

| 位置 | 保留／扩展的具体内容 |
| --- | --- |
| `internal/core/agentproto/{types,prompt_dispatch}.go` | 扩展执行绑定、generation、input delivery 与 runtime receipt；把 surface 选择从执行目标权威中分离，保留单聊显示映射 |
| `internal/adapter/codex/{translator_commands,translator_config,translator_observe_server,translator_restart_restore}.go` | start/resume/steer/compact 主链复用；补完整 command→thread→turn 关联与失败关联、instruction revision 应用边界，禁止跨角色复制未知模板 |
| `internal/adapter/claude/{commands,translator,observe,session_runtime_state}.go` | 缺失目标严格拒绝；保留合成 turn 证据来源；新增可验证的 input receipt／compaction 观测，不能把 Codex RPC 名直接移植 |
| `internal/app/wrapper/{backend_runtime,app_child_session,app_child_session_claude,app_headless_claude,app_child_settings_claude,app_child_mcp}.go` | 增加版本化 LaunchContract／InstructionBundle 和明确 ready 阶段，安全重启携带新 snapshot，session credential 与 run_ref 校验接线 |
| `internal/app/daemon/{app_headless,app_headless_pool,app_headless_claude_profile}.go` | 新宿主匹配按 role/scope/session generation／cwd／配置合同，空闲回收看 WorkerSession lease／inbox／活跃 Attempt；不能只看 surface attachment |
| `internal/core/orchestrator/service_dispatch_core.go`、`service_compact.go` 与运行事件入口 | 提取 WorkerSession ownership、持久投递／恢复／维护用例；原单聊命令调用同一用例，不能让新旧 owner 双写 |
| 新协作持久层与 CLI broker | 保存 Attempt、input receipt、checkpoint、操作去重、配置 desired/applied；消息与成果交付 outbox 独立于 native 最终文本 |

当前 headless pool [syncManagedHeadlessLocked](../../internal/app/daemon/app_headless_pool.go:14)以 surface attachment／ActiveTurn 判 busy。新系统没有 surface 挂靠但有角色身份、收件箱或恢复中的 WorkerSession 时，不能被当作无归属空闲实例。预热可复用未绑定的进程能力；一旦包含 scope 凭据、角色说明、自动记忆或 session 状态，就按明确隔离与归还合同复用。

VS Code attached 保留人控制原会话的价值，但不能假定有和 headless 一样的 launch/env 注入能力。现有 Feishu MCP 自动发布明确排除 VS Code；自动运行接管须验证 instruction/tool readiness 并取得唯一控制权。不满足时保持观察／人工控制，或把工作转给已完整配置的 WorkerSession，不冒充“接上 thread 就获得团队身份”。

## 9. 验证结果与实施验收矩阵

本次已运行以下定向测试，三包均通过：

```text
go test ./internal/adapter/codex ./internal/adapter/claude ./internal/app/wrapper -run 'TestTranslatePromptSendApplyTargetProfile|TestTranslateCompactAndChildRestartRestoreCarryCodexResumePolicy|TestClaudeTranslatorTurnSteer|TestClaudeBootstrapInitializeFrame|TestBuildClaudeChildLaunch' -count=1
```

它们覆盖 profile policy、compact/restart payload、Claude steer 输入、initialize instruction 与 review fork 参数。此次没有启动真实 provider，不宣称端到端可运行。实施验收必须补以下实际协议证据：

1. 两后端分别 cold start、同 session 空闲续跑、不同 session 恢复、target 缺失拒绝；不存在输入落入别的工作流。
2. 模型运行中 steer 与 turn 恰好结束的竞争、写后断线、ACK 丢失；记录原生接收强度，不重复执行已发生操作。
3. Codex 同 thread 修改角色规则必须越过正确应用边界；Claude profile/settings 改动采用新 launch snapshot；用户原 profile／仓库规则仍在。
4. 两角色同仓库不同目录、同角色两项目、个人与群同时运行；凭据、自动记忆、native state 和配置无串用。
5. Codex compact 与恢复后规则／工具可用；Claude 自动压缩事件可见，显式维护能力按安装版本验证；必要重建能继续未完成输入。
6. child exit、wrapper 崩溃、daemon 重启分别恢复；旧 generation 迟到事件不污染新 Attempt，未知副作用先对账。
7. Agent 自主管理 Git 后登记新 cwd：Claude 不继续使用旧 wrapper 目录；Codex 区分进程 cwd、thread cwd 和工具真实位置。平台只校验／记录执行绑定，不增加 worktree 创建／合并管理器。
8. 群与单聊的停止都绕过模型，旧 session 的迟到停止／批准不作用于新 generation；原生结束与任务／Delivery 完成分别验收。

关联：[技术主文档](./feishu-agent-team-technical-design.md)、[执行设计](./feishu-agent-team-execution-design.md)、[代码复用审计](./feishu-agent-codebase-reuse-assessment.md)。

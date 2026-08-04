# Feishu Group Context Multi-Bot Design

> Type: `draft`
> Updated: `2026-08-04`
> Summary: 记录飞书群聊多 context / 多机器人共享群 workspace 的产品结论、底层调研与当前落地状态。

## 1. 背景

当前主要使用模式是用户和单个飞书机器人私聊。一个机器人通常对应一份上下文和一个工作区接管状态；即使把机器人拉进群聊并在群里 @，多个群之间也容易落到共享上下文或共享工作区管理语义上。用户如果要并行处理多个项目或多个任务，往往需要创建多个机器人，并频繁切换 workspace。

目标方向是让一个飞书机器人可以在不同群里拥有不同 context。群 A @ 同一个机器人时看到的是群 A 的上下文，群 B @ 时看到的是群 B 的上下文；底层 Codex / Claude 会话和实例路由也应随群 context 隔离。

## 2. 已确认的产品约束

1. 私聊体验保持现状。
2. 群聊内只共享 workspace，不共享会话。
3. `mode`、Codex provider、Claude profile、模型、推理强度等决定机器人能力的设置属于机器人本身，只能在私聊中修改。
4. `AutoWhip`、`AutoContinue`、会话选择、暂存文件 / 图片、队列等辅助或运行态能力按群 context 隔离。
5. 只有已经通过 `/primary on` 成为 room primary 的 bot，可以在群里通过 @ 任意机器人切换群 workspace；没有 primary 时必须先对目标 bot 执行 `/primary on`。
6. 群 workspace 切换会重置该群内所有机器人针对这个群 workspace 的上下文。
7. 同一个 workspace 不能被多个私聊或多个群共享。
8. 唯一例外是同一个群内的多个机器人可以共享这个群绑定的 workspace。
9. 同一个群 workspace 的机器人并发默认上限为 1；由当前 primary bot 通过 `/coworkers N` 按群设置，`0` 表示不限制。上限只约束新的独立 agent dispatch，不中断已经运行的任务。

## 3. 推荐的 V1 产品形态

### 3.1 私聊

私聊继续作为机器人级设置入口。用户通过私聊设置 backend、provider / profile、模型、推理强度等能力默认值。私聊也继续保留当前单人工作流，不进入群 room 共享逻辑。

### 3.2 群聊首次使用

机器人首次在群里被 @ 时，如果该群尚未绑定 workspace，应引导设置群 workspace。群 workspace 一旦绑定，该群内后续 @ 任意机器人都使用同一个 workspace 绑定。

V1 建议默认不暴露轻量级随手切换入口。切换 workspace 是 room primary bot 才能执行的重置动作，并明确提示会重置该群内所有机器人在该群 workspace 下的上下文。

### 3.3 群聊执行

群里每个机器人拥有自己的会话上下文和实例 / thread 路由。多个机器人共享同一个 workspace 绑定，但不共享同一个 Codex / Claude 会话。

每个机器人 surface 继续保留自己的 queue。群级 room concurrency 只作为独立 agent dispatch 的 admission budget：新的普通消息、detour、review、AutoContinue、AutoWhip、Claude prompt restart 和群按需恢复，在创建 queue item 或进入对应 pending 生命周期前先取得 room reservation；超出当前上限的 sibling bot 直接拒绝，不创建 room 级队列。已经进入某个 bot 自己 queue 的任务不因后来降低上限而中断，`/compact` 继续走独立生命周期，不占本单 room slot。

### 3.4 群聊设置

群聊中如果用户尝试修改机器人级设置，应提示到私聊修改。群聊菜单可以展示当前继承的机器人能力配置，但不提供修改入口。

群聊可修改的设置应限制为群 context 级功能，例如 `AutoWhip`、`AutoContinue`、当前会话选择、当前 staged 输入等。

## 4. 当前代码基础

### 4.0 2026-07-22 本机群聊实测补充

本机新建群实测显示，当前实现已经天然具备“不同群不同 context”的一部分效果。原因不是已经存在 room 抽象，而是群聊 surface identity 已经包含 `gatewayID + chatID`。

本次实测群 `chat_id` 为 `oc_1eb491c89f520efd0255b3717ed9e3a0`。同一群内两个 gateway / bot 收到的 `chat_id` 相同：

1. `feishu:legacy-default:chat:oc_1eb491c89f520efd0255b3717ed9e3a0`
2. `feishu:Codex-5:chat:oc_1eb491c89f520efd0255b3717ed9e3a0`

这说明当前环境里 `chat_id` 在两个 app / gateway 下实测稳定，但这仍不是官方长期合同。V1 room identity 采用 `feishu:chat:<chatID>`，同时保留 gateway / surface evidence；如果后续发现租户或 app 维度下 `chat_id` 不稳定，再引入显式 linkage 或迁移策略。

当时实测也显示当前和目标仍有关键差距：

1. 同一群内两个 bot 仍是两个 surface，不是同一个 room owner。
2. 两个 surface 可以分别绑定不同 workspace。本次分别是 `/data/dl/oclaw` 与 `/data/dl/apitest`。
3. 两个 bot 可以同时启动各自的 managed headless instance / thread / turn，当时没有 room active lock。
4. 同一条 `@Codex Bleeding 你好` 群消息曾同时进入 `legacy-default` 与 `Codex-5` 两个 surface，说明当时入站入口缺少“群消息是否 @ 当前 bot”的 gate。

因此，大方案可以收缩：不需要重新发明“不同群 context 隔离”，而是在现有 surface 隔离上新增 room workspace binding、room concurrency reservation、primary bot 授权 gate，以及 bot 设置 / 群设置边界。

这些差距已经拆入 #728、#730、#732、#733、#734 分阶段收口：入站 @ 当前 bot gate 已由 #728 承接；room context、workspace claim owner、room workspace binding/reset 与 room concurrency reservation 分别由后续子单落地。本文保留本节作为历史实测依据，不再表示当前 live 仍缺这些能力。

### 4.1 Feishu surface 已经区分私聊和群聊

入站 surface identity 已经按 chat type 区分：

1. 私聊：`feishu:<gatewayID>:user:<actorID>`
2. 群聊：`feishu:<gatewayID>:chat:<chatID>`

因此“不同群形成不同 surface”已有基础，不需要从零识别群 context。

### 4.2 Surface 运行态已经承载大量 context 状态

`SurfaceConsoleRecord` 当前已经保存 workspace claim、attached instance、selected thread、route mode、pending headless、pending request、queue、staged file / image、`AutoWhip`、`AutoContinue` 等状态。这说明群 context 的执行隔离可以复用 surface 运行态。

### 4.3 当前 workspace claim 是全局独占

当前 headless workspace claim 以 workspace key 为索引，并指向单个 `SurfaceSessionID`。这会阻止另一个 surface 接管同一个 workspace。

新的产品约束不是取消独占，而是把 claim owner 从单个 surface 提升为：

1. 私聊 owner：单个私聊 surface。
2. 群 room owner：同一个 Feishu 群 room。

这样可以保持“workspace 不能被多个私聊 / 群共享”，同时允许同一群内多个机器人共享同一个 workspace。

## 5. 需要新增或调整的抽象

### 5.1 Feishu Room Context

需要新增群 room 级记录，用于保存：

1. `RoomContextID`
2. `ChatID`
3. 绑定的 `WorkspaceKey`
4. 绑定 / 切换操作者
5. 最近更新时间
6. 当前 active owner surface / bot
7. workspace reset generation

V1 支持同一个群里多个 Feishu app / bot 进入同一 room context，room key 使用当前实测稳定的 `chatID`。由于官方未明确承诺跨 app 稳定性，record 需要保留 gateway / surface evidence，后续若发现不稳定再补显式 linkage。

### 5.2 Workspace Claim Owner

当前 claim owner 是 `SurfaceSessionID`。建议改成结构化 owner：

1. `scope=surface`：私聊或非共享 surface。
2. `scope=room`：群 room。
3. `ownerID`：surface id 或 room id。

claim 判断规则：

1. 私聊接管 workspace 时，如果已有任何其他 owner，拒绝。
2. 群 room 接管 workspace 时，如果 owner 是同一 room，允许。
3. 群 room 接管 workspace 时，如果 owner 是其他 room 或私聊 surface，拒绝。
4. 同一 room 内不同机器人允许看到同一 workspace 已绑定，但不自动共享 thread / session。

### 5.3 Room Active Reservations

为了满足群级并发配置，需要在 room context 上维护 runtime active reservations；它不是一个只能容纳单个 bot 的 holder lock：

1. reservation id 与 surface id
2. instance / thread / turn / queue item evidence
3. reservation reason（普通 dispatch、review、headless replay 等）
4. limit 缺失时默认 1，显式 0 表示 unlimited
5. 失败、断线、超时、detach、reset 等终止路径的释放与保留策略

当前实现把 room limit 持久化在 `FeishuRoomStateRecord.ConcurrencyLimit`，把 `ActiveReservations` 留在 `FeishuRoomContextRecord` 运行时维护：

1. 独立 dispatch 在 queue item / staged input 绑定 / route mutation 前进行 room admission；成功后 reservation 随 queue item 或 pending lifecycle 刷新。
2. `turn.started` 后用真实 thread / turn evidence 刷新；同一 surface 的既有内部 queue 不被 room limit 错误阻断。
3. 普通 queue、AutoContinue、AutoWhip、review、Claude prompt restart 与群按需 replay 都使用 reservation；compact、refresh、model list 等 context-only/control action 不占 slot。
4. turn completed / failed、命令发送失败、headless 超时、transport degrade、detach、purge、workspace reset 等路径按 ownership 清理或保留 reservation。
5. 上限降低不 interrupt、detach 或清理已有任务；上限提高或改为 0 后，后续 admission 可立即使用空余预算。

该 reservation 仍由 orchestrator 的 turn started / completed、queue dispatch / finish、transport degrade / reconnect 路径维护。不能只靠用户输入时临时检查，否则断线、恢复、AutoContinue、AutoWhip 或 pending headless 可能绕过。

## 6. Codex / Claude 底层调研结论

### 6.1 Codex

当前 Codex translator 支持 `thread/start`、`thread/resume`、`turn/start` 等协议动作。远端 prompt 派发到非当前 thread 时，translator 会先发 `thread/resume`，再进入目标 thread 的 turn。

官方 Codex app-server 文档也显示 app-server 支持 `thread/start`、`thread/resume`、`thread/fork`、`thread/read`、`thread/loaded/list` 等 thread 级操作；`thread/read` / `thread/list` 返回的 thread 对象包含 runtime `status`，可为 `notLoaded`、`idle`、`systemError` 或带 `activeFlags` 的 `active`。这说明 Codex app-server 至少支持一个 server 进程内加载和管理多个 thread。

进一步查看上游 Codex Python SDK 后，结论需要修正：Codex 底层已经按 `turn_id` 做消息路由，并且 SDK API Reference 明确写到 `stream()` / `run()` 只消费自身 turn 的 notification，单个 `Codex` / `AsyncCodex` instance 可以同时 stream 多个 active turn。`openai_codex._message_router.MessageRouter` 也以 `_turn_notifications` / `_pending_turn_notifications` 为每个 active turn 维护独立队列，解决 app-server stdio 单有序流上的多 turn 分发问题。

因此底层能力判断是：Codex app-server / SDK 已经具备单实例多 active turn 的协议基础，不只是快速 `thread/resume` 后串行执行。

当前仓库实现和状态模型明确按“一个 instance 同一时间只有一个 active turn”设计：

1. `InstanceRecord` 只有一个 `ActiveThreadID` 和一个 `ActiveTurnID`。
2. dispatch 前会检查 `inst.ActiveTurnID != ""`，有活动 turn 就不继续派发。
3. `turn.started` 会覆盖 instance 的 active thread / turn。
4. `turn.completed` 会清理 active turn。

因此在当前实现里不能直接把同一个 Codex instance 当作并发多 session worker 使用。要承接上游能力，需要把 instance active turn 从单槽改为按 `turn_id` / `thread_id` / surface / queue item 绑定的多槽结构，并同步改造 queue binding、request ownership、final card ownership、stop / steer / approval 路由、reconnect recovery 和状态展示。

V1 仍建议按“每个活跃 context 使用独立 managed headless instance，或至少同一实例串行切换 thread”设计。原因不是 Codex 底层不支持，而是当前产品状态机和飞书输出承接还没有多 active turn 的所有权模型。

### 6.2 Claude

当前 Claude translator 明确只有一个 `activeTurn`，另有 `pendingTurns` 队列和 `completedTurn`。`PrepareForChildLaunch` 会用 `resumeThreadID` 初始化 session，并清空 active / pending 状态。

这说明 Claude 当前更适合“一进程一个当前 session / active turn”的模型。恢复旧 session 可通过启动时携带 resume thread id 实现，但不应假设单个 Claude child 可以并发跑多个 session。

本次参考 `claudecode.zip` 后，Claude Code 自身的 Remote Control 多 session 方案也支持这个判断。参考代码中 `bridgeMain.ts` 使用 `activeSessions = new Map<string, SessionHandle>()` 和 `maxSessions` 做容量管理；`sessionRunner.ts` 的 `createSessionSpawner` 每个 session 都通过 `spawn(...)` 启动一个子进程，并传入 `--print --sdk-url --session-id --input-format stream-json --output-format stream-json`。`types.ts` 把 spawn mode 明确定义为 `single-session`、`worktree`、`same-dir`，其中 `same-dir` 也特别标注多个 session 共享 cwd 可能互相踩踏。

也就是说，Claude Code 可以在一个 bridge 进程里管理多个 remote-control session，但 active 执行是通过多个 session handle / child process 承接，不是一个 Claude `--print` 子进程内部同时 multiplex 多个 session。`replBridge.ts` 还保留了 `maxSessions: 1`、`spawnMode: single-session` 的单 session REPL 路径；`replBridgeTransport.ts` 也说明多并发 session 需要 per-instance auth token source，不能靠进程级 env var。

对本仓库的含义是：现有 Claude 集成应继续按“一个 managed child 绑定一个当前 session；切 session 走 `--resume` 或独立进程”设计。如果未来要支持 Claude 多活跃 context，优先参考 Claude Code bridge 的多 child / capacity 模型，而不是试图在当前 translator 内做单进程多 session。

### 6.3 Headless runtime

managed headless 启动时会为每个 `InstanceID` 启动独立进程，并通过环境变量标识 backend、provider/profile、resume thread。Codex 与 Claude 分别使用不同 launch mode。

这更支持“按 context 拉起 / 恢复独立 managed headless”的实现路径，而不是在一个进程内 multiplex 多个活跃 session。

当前 wrapper restart 逻辑也支持这个判断。Codex child restart 会在新 child 拉起后发送 `thread/resume` 恢复当前 thread；Claude child restart 会根据目标 session 构造 `--resume <session_id>`，并且重启前会先停止旧 child IO，避免新旧 child 交叉写入。这说明现有快速切换更接近“串行恢复 / 重启恢复”，不是“同一 child 内多 session 并发”。

## 7. Feishu 群授权语义

### 7.1 Primary bot 是群级授权 SSOT

飞书事件不会把 bot 变成群管理员，机器人也不能依赖 Feishu 原生群管理员身份执行 workspace 切换。当前产品使用 room 的 `PrimaryGatewayID` 作为群级授权事实源：`/primary on` 先通过 `PrimaryBotPermissionChecker` 确认当前 bot 能接收群普通消息，再写入或替换该字段。

已有 room workspace 且目标 workspace 不同的时候，只有当前 surface 的 gateway 与 `PrimaryGatewayID` 精确匹配才允许切换，并继续经过 active / pending / review / running blocker 和 sibling reset。没有 primary、primary 状态无法证明或当前 bot 不是 primary 时，拒绝切换并提示先对目标 bot 执行 `/primary on`。

此 gate 不调用 `im.v1.chat.get`，也不要求 `im:chat:readonly`、`im:chat:read` 或 `im:chat`。`/primary off`、`status`、`refresh` 和 primary replacement 仍按现有 room primary 状态工作。

### 7.2 `OpenChatID` / `chat_id` 跨 app 稳定性

官方“群 ID 说明”只说明 `chat_id` 是群组唯一标识，可用于发送消息、拉群、获取群信息等群组 OpenAPI；没有明确承诺同一个群在不同 app / bot 下的 `chat_id` 一定相同。官方用户 ID 文档明确说明 `open_id` 是应用内身份，同一用户在不同应用中的 `open_id` 不同，并建议不要跨应用使用 `open_id`。

2026-07-22 本机实测中，同一飞书群在 `legacy-default` 与 `Codex-5` 两个 gateway 下的 `chat_id` 完全相同，均为 `oc_1eb491c89f520efd0255b3717ed9e3a0`。这是支持 V1 采用 `chat_id` 作为 room identity 强信号的实际证据。

结合当前证据，不能把跨 app 稳定的 `OpenChatID` 当作已证明前提；但 V1 已决定采用 `feishu:chat:<chatID>` 作为 room identity，让同 `chatID` 的不同 gateway / app surface 进入同一个 room context。

这个决定的边界是：

1. room context record 必须保留 gateway / surface evidence，不能只留下不可审计的裸 `chatID`。
2. workspace claim、reset、active reservation 等后续动作都应通过 room helper 获取同 room surfaces，避免各处重复拼接 identity。
3. 如果后续实测或官方文档证明某些跨 app 场景下 `chat_id` 不稳定，再额外设计显式 room linkage：由当前 primary bot 通过产品内授权流程完成绑定确认，系统记录 `tenant_key + app_id + chat_id` 到同一内部 `RoomContextID` 的映射，并在 workspace claim、重置广播上全部按内部 room id 操作。

## 8. 当前主要风险

1. 当前多 bot 群聊缺少“是否 @ 当前 bot”的入站过滤；同一条群消息可能被多个 bot/gateway 同时消费。此问题已拆到 #728，优先修复。
2. 群 room key V1 使用 `feishu:chat:<chatID>`，同 `chatID` 跨 gateway 进入同一 room；但 Feishu `OpenChatID` / `chat_id` 在多 app 场景下未找到官方稳定性承诺，因此必须保留 gateway / surface evidence 和未来显式 linkage 余地。
3. workspace claim 从 surface owner 改为 room owner 会影响 `/list`、workspace picker、target picker、surface resume、headless recovery、busy 文案。
4. room active reservation 已覆盖普通 queue、AutoWhip、AutoContinue、review、Claude prompt restart、群按需 headless replay 与 active turn 观测；后续新增独立 agent dispatch 入口仍必须先接入同一 reservation helper，避免绕过群级预算。
5. 群 workspace 切换会重置多 surface 状态，需要设计原子化 reset，不能只清当前触发切换的机器人 surface。
6. 私聊设置改成机器人级后，当前 surface 级 `ProductMode`、`Backend`、provider/profile、prompt override 的持久化语义需要重新梳理。
7. Codex 底层虽支持单实例多 active turn，但当前仓库单槽 active turn 模型会导致所有权、stop / steer、请求审批和飞书最终卡投递风险；不能半改。
## 9. 代码改造落点初稿

### 9.1 Claim 层

当前 `workspaceClaims`、`instanceClaims`、`threadClaims` 的直接访问已经被测试限制在 claim facade 内。这意味着 workspace claim owner 重构应优先落在 claim facade，而不是扩散到业务命令处理里。

建议步骤：

1. 新增 `workspaceClaimOwner`，包含 owner scope、owner id、display surface id。
2. 保留 `instanceClaims` 和 `threadClaims` 的 surface 独占语义。
3. 将 `workspaceBusyOwnerForSurface` 拆成“是否同 owner 允许共享”和“返回阻塞 owner 展示信息”两层。
4. 在 surface attach / thread attach / workspace picker 中统一调用 claim facade，不新增旁路判断。

### 9.2 Room 层

新增 room context storage 后，`ensureSurface` 或更靠近 Feishu inbound 的位置需要把 group surface 映射到 room context。私聊 surface 不进入 room 共享。

room context 至少需要支持：

1. 按 chat id 查找 room。
2. 保存 room workspace binding。
3. 查询同 room 下的 surfaces。
4. 对 room 下所有 surfaces 执行 reset。
5. 维护 room concurrency limit 与 runtime active reservations。
6. 保存 room identity proof：至少包含 `gatewayID` / `app_id`、`chat_id` 与 surface evidence；后续如发现 `chatID` 跨 app 不稳定，再补显式 linkage。

### 9.3 Command 层

群聊中的机器人级设置命令需要改为只读或拒绝修改。现有 `/mode`、`/codexprovider`、`/claudeprofile`、`/model`、`/reasoning` 都是 surface 级命令，需要改成：

1. 私聊：允许修改机器人级设置。
2. 群聊：展示当前值或提示私聊修改。
3. 群聊执行时：读取机器人级设置作为默认配置。

这部分不能只改菜单可见性，因为用户仍可直接发 slash command。

### 9.4 Feishu 群 primary 授权层

workspace switch 不新增 Feishu chat info client，也不查询原生群管理员。授权只读取 room `PrimaryGatewayID` 与当前 surface gateway 的匹配关系；`/primary on` 负责通过 `PrimaryBotPermissionChecker` 确认群普通消息能力并写入 primary。

### 9.5 当前落地状态

截至 2026-08-04，已完成 room concurrency 这一轮落地：

1. room identity/state：`state.Root.FeishuRoomContexts` 保存 `feishu:chat:<chatID>` room context；`ensureSurface` 在群聊 surface materialize/resume 时登记 gateway/surface evidence；私聊 surface 不进入 room context。该记录现在是 room workspace binding/reset 与 room concurrency limit 的 durable SSOT，runtime active reservations 只保留在内存。
2. Feishu 群 primary 授权：`/primary on` 通过 `PrimaryBotPermissionChecker` 强制刷新当前 bot 的群普通消息能力并写入 `PrimaryGatewayID`；workspace switch 只允许当前 primary bot，完全不依赖 Feishu 群管理员 API。
3. workspace claim owner：`workspaceClaims` 已从单 `SurfaceSessionID` 扩展为 `surface` / `room` 结构化 owner；同 room 群 surface 可以共享 workspace claim，不同 room / 私聊 surface 仍互斥；instance/thread claim 仍保持 surface 独占。
4. room workspace binding / switch / reset：`FeishuRoomContextRecord` 已保存 `WorkspaceKey`、绑定操作者、绑定更新时间与 reset generation。workspace attach、attach instance、跨 workspace thread attach、fresh workspace prepare 等真正改变 workspace claim 的入口统一经过 room binding helper；没有自身 workspace route 的 same-room surface 会把 room binding 作为当前 workspace 默认值，因此第二个 bot 首次打开 `/use` / target picker 时会默认看到群 workspace。room 已绑定且目标 workspace 不同时，先确认当前 surface 可以安全离开，再检查同 room 是否有 active/pending request/headless/review/running blocker，随后要求当前 surface gateway 与 `PrimaryGatewayID` 匹配。primary gate 失败时拒绝；primary bot 切换成功会 reset 同 room 其它 surface 的 attachment、thread selection、queue、staged input、pending request/capture、exec/reasoning progress、review、plan proposal 和 target picker runtime。最终 room binding 只在 route/attach 成功或 fresh workspace 连接完成后写入新 workspace。
5. room durable state：`FeishuRoomStateRecord` 通过原 `feishu-room-primary.json` 路径的 schema v2 持久化 workspace/update/reset、primary/update 与可选 `ConcurrencyLimit`；旧 v1 primary-only 文件原位迁移，缺失字段运行时默认 1。设置失败时恢复旧 runtime 值，不允许只在内存中成功。

6. room concurrency command：统一 command registry 提供 `/coworkers N` 与 `/coworkers status` 的 text/slash、menu、help/catalog projection；status 群内只读，设置要求当前 `/primary on` bot，单聊直接拒绝，0 表示 unlimited。超限新 dispatch 直接拒绝，不引入 room 级队列；每个 bot 的内部 queue 保持独立。

## 10. 已完成调研问题

1. Codex app-server 是否允许单实例多 active session：底层协议 / SDK 支持多 active turn，当前仓库状态机未承接，V1 不直接依赖。
2. Claude CLI / app-server 封装是否能单进程快速切换 session：Claude Code Remote Control 的多 session 是 bridge 多 child / 多 session handle 模型；当前仓库 Claude `--print --resume` 集成应按一 child 一 session / restart resume 处理。
3. Feishu 群授权事实源：bot 不能成为群管理员，使用 `/primary on` 写入的 `PrimaryGatewayID` 作为 room 级授权 SSOT；workspace switch 不调用群信息 API。
4. Feishu `OpenChatID` 是否跨不同 app / bot 稳定：未找到官方明确承诺；V1 仍采用本机实测可用的 `chatID` room key，并通过 evidence 字段保留未来迁移空间。

room concurrency 的产品决策已经收口：默认上限 1，可由当前 primary bot 通过 `/coworkers N` 按群设置；超限的新独立 dispatch 直接拒绝，不进入 room 级队列；已有 bot 自身 queue 继续保持原有语义。

调研依据：

1. Codex upstream `sdk/python/docs/api-reference.md`、`sdk/python/src/openai_codex/_message_router.py`、`sdk/python/src/openai_codex/api.py`。
2. Claude Code 参考代码 `src/bridge/bridgeMain.ts`、`src/bridge/sessionRunner.ts`、`src/bridge/replBridge.ts`、`src/bridge/replBridgeTransport.ts`、`src/bridge/types.ts`。
3. 飞书开放平台：接收消息事件、获取群信息、获取群成员列表、群 ID 说明、用户身份概述。

截至 2026-07-22，本地环境未发现可直接执行的 `codex` / `claude` 命令，因此尚未完成真实 runtime 并发探测。现阶段结论来自上游源码 / SDK 文档、Claude Code 参考代码、飞书官方文档，以及本仓库 wrapper / translator / orchestrator 代码。

## 11. 初步推荐

V1 不支持本仓库内的单实例并发多 session。推荐按以下顺序推进：

1. 先修 #728：群聊入站必须只处理 @ 当前 bot 的消息，避免当前多 bot 群聊里非目标机器人响应。
2. workspace claim 仍全局独占，但 owner 可以是群 room。
3. 同一群 room 内多个机器人共享 workspace claim。
4. 每个机器人 surface 保持独立 session / thread / instance route。
5. 同一 room workspace 的独立 agent dispatch 受 `ConcurrencyLimit` 约束，默认只允许一个 active reservation，显式 0 表示不限制；每个 surface 的内部 queue 仍独立。
6. 群 workspace 切换只允许当前 primary bot 执行，并重置整个 room 下的 surface context。
7. 跨不同 Feishu app / gateway 但 `chatID` 相同的“同一群”在 V1 进入同一 room context；如后续发现 `chatID` 不稳定，再引入显式管理员绑定或迁移策略。

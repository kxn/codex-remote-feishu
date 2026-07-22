# Feishu Group Context Multi-Bot Design

> Type: `draft`
> Updated: `2026-07-22`
> Summary: 记录飞书群聊多 context / 多机器人共享群 workspace 的早期产品结论与底层可行性调研入口。

## 1. 背景

当前主要使用模式是用户和单个飞书机器人私聊。一个机器人通常对应一份上下文和一个工作区接管状态；即使把机器人拉进群聊并在群里 @，多个群之间也容易落到共享上下文或共享工作区管理语义上。用户如果要并行处理多个项目或多个任务，往往需要创建多个机器人，并频繁切换 workspace。

目标方向是让一个飞书机器人可以在不同群里拥有不同 context。群 A @ 同一个机器人时看到的是群 A 的上下文，群 B @ 时看到的是群 B 的上下文；底层 Codex / Claude 会话和实例路由也应随群 context 隔离。

## 2. 已确认的产品约束

1. 私聊体验保持现状。
2. 群聊内只共享 workspace，不共享会话。
3. `mode`、Codex provider、Claude profile、模型、推理强度等决定机器人能力的设置属于机器人本身，只能在私聊中修改。
4. `AutoWhip`、`AutoContinue`、会话选择、暂存文件 / 图片、队列等辅助或运行态能力按群 context 隔离。
5. 只有群管理员可以在群里通过 @ 任意机器人切换群 workspace。
6. 群 workspace 切换会重置该群内所有机器人针对这个群 workspace 的上下文。
7. 同一个 workspace 不能被多个私聊或多个群共享。
8. 唯一例外是同一个群内的多个机器人可以共享这个群绑定的 workspace。
9. 同一个群 workspace 上同一时间只允许一个机器人活跃执行，避免多个机器人同时修改同一目录。

## 3. 推荐的 V1 产品形态

### 3.1 私聊

私聊继续作为机器人级设置入口。用户通过私聊设置 backend、provider / profile、模型、推理强度等能力默认值。私聊也继续保留当前单人工作流，不进入群 room 共享逻辑。

### 3.2 群聊首次使用

机器人首次在群里被 @ 时，如果该群尚未绑定 workspace，应引导设置群 workspace。群 workspace 一旦绑定，该群内后续 @ 任意机器人都使用同一个 workspace 绑定。

V1 建议默认不暴露轻量级随手切换入口。切换 workspace 应作为管理员级重置动作出现，并明确提示会重置该群内所有机器人在该群 workspace 下的上下文。

### 3.3 群聊执行

群里每个机器人拥有自己的会话上下文和实例 / thread 路由。多个机器人共享同一个 workspace 绑定，但不共享同一个 Codex / Claude 会话。

如果群 workspace 当前已有其他机器人 active running，新的输入应排队、拒绝或提示等待。V1 倾向先拒绝或提示等待，不做跨机器人共享队列，因为共享队列会引入“到底由哪个机器人继续执行”的产品歧义。

### 3.4 群聊设置

群聊中如果用户尝试修改机器人级设置，应提示到私聊修改。群聊菜单可以展示当前继承的机器人能力配置，但不提供修改入口。

群聊可修改的设置应限制为群 context 级功能，例如 `AutoWhip`、`AutoContinue`、当前会话选择、当前 staged 输入等。

## 4. 当前代码基础

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

如果未来要支持同一个群里多个 Feishu app / bot 共享 workspace，需要确认 Feishu `OpenChatID` 是否跨 app 稳定。如果不稳定，room key 不能只依赖当前 `chatID`。

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

### 5.3 Room Active Lock

为了满足“同一 workspace 上只允许一个机器人活跃”，需要在 room 或 workspace claim 上维护 active lock：

1. active surface id
2. active instance id
3. active thread id
4. active turn id / queue item id
5. lock reason
6. expires / recovery policy

该 lock 应由 orchestrator 的 turn started / completed、queue dispatch / finish、transport degrade / reconnect 路径维护。不能只靠用户输入时临时检查，否则断线、恢复、AutoContinue、AutoWhip 可能绕过。

## 6. Codex / Claude 底层初步判断

### 6.1 Codex

当前 Codex translator 支持 `thread/start`、`thread/resume`、`turn/start` 等协议动作。远端 prompt 派发到非当前 thread 时，translator 会先发 `thread/resume`，再进入目标 thread 的 turn。

官方 Codex app-server 文档也显示 app-server 支持 `thread/start`、`thread/resume`、`thread/fork`、`thread/read`、`thread/loaded/list` 等 thread 级操作；`thread/read` / `thread/list` 返回的 thread 对象包含 runtime `status`，可为 `notLoaded`、`idle`、`systemError` 或带 `activeFlags` 的 `active`。这说明 Codex app-server 至少支持一个 server 进程内加载和管理多个 thread。

但官方文档没有明确承诺“同一个 app-server 进程可以同时在多个 thread 上并发运行多个 active turn”。文档中 `turn/steer` 的语义是追加到某个 thread 当前 in-flight turn，`thread/shellCommand` 也描述为“如果该 thread 已有 active turn，则作为该 turn 的辅助动作”。这些语义是 thread 级的，但不能直接推导为跨 thread 并发 turn 是稳定产品合同。

当前仓库实现和状态模型明确按“一个 instance 同一时间只有一个 active turn”设计：

1. `InstanceRecord` 只有一个 `ActiveThreadID` 和一个 `ActiveTurnID`。
2. dispatch 前会检查 `inst.ActiveTurnID != ""`，有活动 turn 就不继续派发。
3. `turn.started` 会覆盖 instance 的 active thread / turn。
4. `turn.completed` 会清理 active turn。

因此在当前实现里不能把同一个 Codex instance 当作并发多 session worker 使用。即便底层 app-server 实际已经能跨 thread 并发，仓库也需要重做 active turn 状态、queue binding、request ownership、final card ownership、stop/steer 路由等多处逻辑，才能安全承接。

V1 应按“每个活跃 context 使用独立 managed headless instance，或至少同一实例串行切换 thread”设计，不应依赖单 Codex instance 并发运行多个 session。

### 6.2 Claude

当前 Claude translator 明确只有一个 `activeTurn`，另有 `pendingTurns` 队列和 `completedTurn`。`PrepareForChildLaunch` 会用 `resumeThreadID` 初始化 session，并清空 active / pending 状态。

这说明 Claude 当前更适合“一进程一个当前 session / active turn”的模型。恢复旧 session 可通过启动时携带 resume thread id 实现，但不应假设单个 Claude child 可以并发跑多个 session。

### 6.3 Headless runtime

managed headless 启动时会为每个 `InstanceID` 启动独立进程，并通过环境变量标识 backend、provider/profile、resume thread。Codex 与 Claude 分别使用不同 launch mode。

这更支持“按 context 拉起 / 恢复独立 managed headless”的实现路径，而不是在一个进程内 multiplex 多个活跃 session。

当前 wrapper restart 逻辑也支持这个判断。Codex child restart 会在新 child 拉起后发送 `thread/resume` 恢复当前 thread；Claude child restart 会根据目标 session 构造 `--resume <session_id>`，并且重启前会先停止旧 child IO，避免新旧 child 交叉写入。这说明现有快速切换更接近“串行恢复 / 重启恢复”，不是“同一 child 内多 session 并发”。

## 7. 当前主要风险

1. 群 room key 是否能跨多个机器人稳定识别，需要进一步验证 Feishu `OpenChatID` 在多 app 场景下的稳定性。
2. workspace claim 从 surface owner 改为 room owner 会影响 `/list`、workspace picker、target picker、surface resume、headless recovery、busy 文案。
3. room active lock 必须覆盖 AutoWhip / AutoContinue / review / request approval / pending headless / transport degrade，不然会出现同群多机器人并发写同一 workspace。
4. 群 workspace 切换会重置多 surface 状态，需要设计原子化 reset，不能只清当前触发切换的机器人 surface。
5. 私聊设置改成机器人级后，当前 surface 级 `ProductMode`、`Backend`、provider/profile、prompt override 的持久化语义需要重新梳理。

## 8. 代码改造落点初稿

### 8.1 Claim 层

当前 `workspaceClaims`、`instanceClaims`、`threadClaims` 的直接访问已经被测试限制在 claim facade 内。这意味着 workspace claim owner 重构应优先落在 claim facade，而不是扩散到业务命令处理里。

建议步骤：

1. 新增 `workspaceClaimOwner`，包含 owner scope、owner id、display surface id。
2. 保留 `instanceClaims` 和 `threadClaims` 的 surface 独占语义。
3. 将 `workspaceBusyOwnerForSurface` 拆成“是否同 owner 允许共享”和“返回阻塞 owner 展示信息”两层。
4. 在 surface attach / thread attach / workspace picker 中统一调用 claim facade，不新增旁路判断。

### 8.2 Room 层

新增 room context storage 后，`ensureSurface` 或更靠近 Feishu inbound 的位置需要把 group surface 映射到 room context。私聊 surface 不进入 room 共享。

room context 至少需要支持：

1. 按 chat id 查找 room。
2. 保存 room workspace binding。
3. 查询同 room 下的 surfaces。
4. 对 room 下所有 surfaces 执行 reset。
5. 维护 room active lock。

### 8.3 Command 层

群聊中的机器人级设置命令需要改为只读或拒绝修改。现有 `/mode`、`/codexprovider`、`/claudeprofile`、`/model`、`/reasoning` 都是 surface 级命令，需要改成：

1. 私聊：允许修改机器人级设置。
2. 群聊：展示当前值或提示私聊修改。
3. 群聊执行时：读取机器人级设置作为默认配置。

这部分不能只改菜单可见性，因为用户仍可直接发 slash command。

## 9. 待调研问题

1. Codex app-server 是否在官方语义上允许一个实例同时 active run 多个 session，还是只支持快速 `thread/resume` 后串行执行。
2. Claude CLI / app-server 封装是否能在一个进程内快速切换 session，还是必须通过 `--resume` / 子进程重启切 session。
3. Feishu 群管理员身份是否能在当前事件 payload 或补充 API 中可靠判断。
4. Feishu `OpenChatID` 是否跨不同 app / bot 稳定。
5. room active lock 应该是拒绝第二个机器人输入，还是进入 room 级队列。

截至 2026-07-22，本地环境未发现可直接执行的 `codex` / `claude` 命令，因此尚未完成真实 runtime 并发探测。现阶段结论来自官方 Codex app-server 文档、仓库 wrapper / translator / orchestrator 代码，以及现有测试覆盖。

## 10. 初步推荐

V1 不支持单实例并发多 session。推荐实现为：

1. workspace claim 仍全局独占，但 owner 可以是群 room。
2. 同一群 room 内多个机器人共享 workspace claim。
3. 每个机器人 surface 保持独立 session / thread / instance route。
4. 同一 room workspace 同时只允许一个 surface active running。
5. 群 workspace 切换只允许群管理员执行，并重置整个 room 下的 surface context。

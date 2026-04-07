# Remote Surface 核心状态机

> Type: `general`
> Updated: `2026-04-07`
> Summary: 记录当前已实现的 remote surface 状态机，包括多 app 全局仲裁、`/new` 的 `new_thread_ready`、空 thread 归属、thread 级未投递回放、watchdog 与死状态审计结论，作为提交前复审基线。

## 1. 文档定位

这份文档描述的是**当前代码已经实现**的 remote surface 状态机，不是历史问题列表，也不是未来方案草稿。

它承担两个职责：

1. 作为当前 remote surface 行为的长期 source of truth。
2. 作为后续状态机相关改动在提交前必须回看的 guardrail。

审计基线覆盖：

1. [internal/core/orchestrator/service.go](../../internal/core/orchestrator/service.go)
2. [internal/core/orchestrator/service_test.go](../../internal/core/orchestrator/service_test.go)
3. [internal/core/state/types.go](../../internal/core/state/types.go)
4. [internal/core/control/types.go](../../internal/core/control/types.go)
5. [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
6. [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
7. [internal/app/daemon/app_test.go](../../internal/app/daemon/app_test.go)

## 2. 审计前提

### 2.1 `threadID` 当前就是 relay 全局仲裁键

当前 thread claim 是 `map[string]*threadClaimRecord`，key 只有 `threadID`。

这依赖下面这个前提，而且现在就是产品前提：

1. 同一台机器上，`threadID` 在单个 `relayd` 仲裁域内全局唯一。
2. 同一台机器上只运行一个 `relayd`。

这个假设必须保留在文档里，避免以后误改成“按 instance 局部唯一”。

### 2.2 surface 按 gateway/chat 区分，但 claim 是 relay 全局的

surface 本身仍按 `gatewayID + chat/user` 区分，不同飞书 app 会形成不同 surface。

但 `instanceClaims` 和 `threadClaims` 都在同一个 orchestrator 里仲裁，所以：

1. 不同飞书 app 之间会竞争同一套 instance/thread 资源。
2. instance attach 互斥、thread attach 互斥都是**跨 app 的全局规则**。

## 3. 当前状态机的四层结构

surface 不是单一枚举，而是四层正交状态叠加。

### 3.1 路由主状态

| 代号 | 条件 | 用户语义 |
| --- | --- | --- |
| `R0 Detached` | `AttachedInstanceID == ""` | 当前没有接管任何实例 |
| `R1 AttachedUnbound` | `AttachedInstanceID != ""`，`RouteMode=unbound`，`SelectedThreadID == ""` | 已接管实例，但当前没有可发送 thread |
| `R2 AttachedPinned` | `AttachedInstanceID != ""`，`RouteMode=pinned`，`SelectedThreadID != ""`，且持有 thread claim | 当前输入固定发到该 thread |
| `R3 FollowWaiting` | `AttachedInstanceID != ""`，`RouteMode=follow_local`，`SelectedThreadID == ""` | 已进入 follow，但当前没有可接管 thread |
| `R4 FollowBound` | `AttachedInstanceID != ""`，`RouteMode=follow_local`，`SelectedThreadID != ""`，且持有 thread claim | 已跟随到一个 thread |
| `R5 NewThreadReady` | `AttachedInstanceID != ""`，`RouteMode=new_thread_ready`，`SelectedThreadID == ""`，`PreparedThreadCWD != ""` | 已释放旧 thread；下一条普通文本会创建新 thread |

### 3.2 执行状态

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `E0 Idle` | `DispatchMode=normal`，无 active，无 queued | 空闲 |
| `E1 Queued` | `QueuedQueueItemIDs` 非空，`ActiveQueueItemID == ""` | 有待派发远端输入 |
| `E2 Dispatching` | `ActiveQueueItemID` 指向 `dispatching` | prompt 已发给 wrapper，turn 尚未建立 |
| `E3 Running` | `ActiveQueueItemID` 指向 `running` | turn 已进入执行 |
| `E4 PausedForLocal` | `DispatchMode=paused_for_local` | 观察到本地 VS Code 活动，远端暂停出队 |
| `E5 HandoffWait` | `DispatchMode=handoff_wait` | 本地刚结束，等待短窗口后恢复远端队列 |
| `E6 Abandoning` | `Abandoning=true` | surface 已放弃接管，等待已有 turn 收尾后最终 detach |

### 3.3 输入门禁状态

| 代号 | 条件 | 作用 |
| --- | --- | --- |
| `G0 None` | 无附加门禁 | 普通输入按主路由走 |
| `G1 PendingHeadlessStarting` | `PendingHeadless.Status=starting` | headless 仍在启动 |
| `G2 PendingHeadlessSelecting` | `PendingHeadless.Status=selecting` | headless 已 attach，但等待用户选恢复 thread |
| `G3 PendingRequest` | `PendingRequests` 非空 | 普通文本/图片会被确认卡片门禁挡住 |
| `G4 RequestCapture` | `ActiveRequestCapture != nil` | 下一条普通文本会被当成拒绝反馈 |
| `G5 AbandoningGate` | `Abandoning=true` | 只有 `/status` 继续正常，其余动作被挡 |

### 3.4 草稿状态

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `D0 NoDraft` | 无 staged image，无 queued draft | 没有待绑定输入 |
| `D1 StagedImages` | `StagedImages` 中存在 `ImageStaged` | 图片已上传，但尚未冻结到 queue item |
| `D2 QueuedDrafts` | `QueuedQueueItemIDs` 非空 | 已冻结 route/cwd/override，等待派发 |
| `D3 NewThreadFirstInput` | `RouteMode=new_thread_ready` 且已存在 queued/dispatching/running 的首条消息 | 新 thread 尚未落地，但本轮创建已占用 |

关键区别：

1. `D2` 已冻结路由。
2. `D1` 还没有冻结路由，所以 route change 时必须显式处理。
3. `D3` 不是独立 route state，而是 `R5` 上的附加约束。

## 4. 当前已实现的不变量

### 4.1 instance attach 全局互斥

当前 `attachInstance()` 与 `attachHeadlessInstance()` 都走 `instanceClaims`。

结果：

1. 一个 instance 同时只能被一个飞书 surface attach。
2. 第二个 surface attach 同一 instance 会直接收到 `instance_busy`。
3. 不会进入“instance attach 成功但 thread attach 失败且用户不知道下一步”的半 attach 状态。

唯一保留的显式可恢复状态是 `R1 AttachedUnbound`：

1. attach instance 成功。
2. 默认 thread 当前拿不到 claim 或没有默认 thread。
3. surface 进入 `R1`。
4. 服务端会主动发 thread 选择卡片。

这不是死状态，因为用户仍然只有一条明确下一步：`/use` 或点 thread 卡片。

### 4.2 thread attach 全局互斥

当前 `threadClaims` 仍按 `threadID` 做全局仲裁。

结果：

1. 一个 thread 同时只能被一个飞书 surface 占有。
2. `/use` 命中已被他人占用的 thread 时：
   1. 如果目标 thread 在**当前 attached instance 内可见**，仍保留现有强踢逻辑：
      1. 对方 idle 才会弹强踢确认。
      2. 对方 queued/running 会直接拒绝。
   2. 如果目标 thread 走的是 global thread-first attach 路径，不提供强踢，只会在列表里显示 busy 并禁用。

### 4.3 `PendingHeadless` 仍是 dominant gate

只要 `PendingHeadless != nil`：

1. 允许：`/status`、`/killinstance`、`resume_headless_thread`、消息撤回、reaction。
2. 其余 surface action 全部在 `ApplySurfaceAction()` 顶层被拦截。

这意味着：

1. `starting` 时不能旁路 attach/use/follow/new。
2. `selecting` 时也不能通过 `/use`、`/follow`、`/new`、普通文本去改路由。
3. 对 thread-first `/use` 触发的 preselected headless，`starting` 结束后会直接落到目标 thread，不会进入 `selecting`。
4. 手工 `/newinstance` 的 headless 选择期，唯一正常逃生口仍是“恢复某个 thread”或“/killinstance”。

### 4.4 选择卡片不再是服务端持久 modal 状态

当前服务端已经不再保存 `SelectionPrompt` 状态，也不再把“纯数字文本”解释成选择。

当前行为：

1. attach/use/headless resume/kick confirm 都改成**直达动作**。
2. Feishu 卡片按钮直接携带：
   1. `attach_instance`
   2. `use_thread`
   3. `resume_headless_thread`
   4. `kick_thread_confirm`
   5. `kick_thread_cancel`
3. 旧 `prompt_select` 只保留兼容解析，服务端统一返回 `selection_expired`。
4. `"1"`、`"2"` 这类纯数字文本现在就是普通文本。

### 4.5 route change 与 `/new` 都会显式处理未发送草稿

当前有两类固定规则：

1. 普通 route change，例如 `/use`、`/follow`、follow 自动切换、claim 丢失回退：
   1. 只丢 staged image。
   2. 不会静默把未冻结图片串到新 thread。
2. clear 语义，例如 `/stop`、`/detach`、`/new`、`R5` 下的 `/use` `/follow`：
   1. staged image 和 queued draft 都会被显式丢弃。
   2. 会发 discard reaction / notice。

当前实现不允许未发送草稿在 route change 时 silently retarget。

### 4.6 `R5 NewThreadReady` 是稳定态，不是半成品

当前 `/new` 已实现为 clear-and-prepare：

1. 只在 surface 已 attach、已真实持有一个可见 thread、且该 thread `CWD` 非空时允许进入。
2. 不允许 fallback 到 `Instance.WorkspaceRoot` 或 home。
3. 进入时会释放旧 thread claim，但保留 instance attachment 与 `PromptOverride`。
4. `PreparedThreadCWD`、`PreparedFromThreadID`、`PreparedAt` 会显式保存。

这带来三个关键性质：

1. `R5` 没有“attach 成功但用户无路可走”的问题。
2. `R5` 下第一条普通文本合法，且会创建新 thread。
3. `R5` 下如果只有 staged/queued draft，用户仍然能 `/use`、`/follow`、`/detach`、`/stop` 或重复 `/new`。

### 4.7 空 thread turn 不再靠 `ActiveThreadID` 猜归属

当前 empty-thread 首条消息的 turn 归属已经改成显式相关性：

1. queue item 仍以 `FrozenThreadID == ""` 派发。
2. translator 在 `turn.started` 时提供 `InitiatorRemoteSurface + SurfaceSessionID`。
3. orchestrator 优先用 `Initiator.SurfaceSessionID` 命中 pending remote item。
4. 命中后回填真实 `threadID`，并把 surface 从 `R5` 切回 `R2 AttachedPinned`。

当前不再用“`FrozenThreadID == ""` 时退化匹配 `inst.ActiveThreadID`”来猜归属。

### 4.8 `PausedForLocal` 和 `Abandoning` 都有 watchdog

当前 `Tick()` 已经提供两类恢复：

1. `paused_for_local` 超时后：
   1. 自动回到 `normal`
   2. 发 `local_activity_watchdog_resumed`
   3. 继续 `dispatchNext`
2. `abandoning` 超时后：
   1. 强制 `finalizeDetachedSurface`
   2. 发 `detach_timeout_forced`

所以这两个状态不再依赖单一异步事件才能退出。

### 4.9 thread 级未投递回放是单槽、内存态、一次性

当前 `ThreadRecord` 增加了 `UndeliveredReplay`，但它不是完整历史，只是 thread 级的单槽候选。

当前规则：

1. 只记录两类内容：
   1. 没有任何 flybook surface 可投递时产生的 final assistant block。
   2. 没有任何目标 surface 时产生的 thread-scoped system/problem notice。
2. 一条新候选会覆盖旧候选，不保留 backlog。
3. 同一 thread 的内容一旦已经成功投递到当前 surface，就会清空旧 replay，避免后续重复补发。
4. 只有两条显式入口会尝试回放：
   1. `/attach` 成功后默认选中的 thread。
   2. `/use` 选中的 thread。
5. 回放前会检查该 thread 是否 idle：
   1. 若 `inst.ActiveTurnID != ""` 且 `inst.ActiveThreadID == threadID`，则本次不补发。
   2. 候选继续保留，等待后续 idle 的 `/attach` 或 `/use`。
6. 回放成功后立即清空，因此同一条内容只会补发一次。
7. 该状态仅保存在 relay 内存里；`relayd` 重启后丢失是当前已接受语义。

## 5. 主要状态迁移

### 5.1 attach / use / follow / new

```text
R0 Detached
  -- /attach(instance，可拿默认 thread) --> R2 AttachedPinned
  -- /attach(instance，默认 thread 不可拿或不存在) --> R1 AttachedUnbound
  -- /use(thread，可解析到当前可用实例) --> R2 AttachedPinned
  -- /use(thread，需要新 headless) --> R0 + G1 PendingHeadlessStarting
  -- /newinstance --> R0 + G1 PendingHeadlessStarting

R1 AttachedUnbound
  -- /use(thread，同 instance 可见) --> R2 AttachedPinned
  -- /use(thread，需要切换实例) --> detach 语义清理后 -> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- /follow --> R4 FollowBound 或 R3 FollowWaiting
  -- /detach --> R0 Detached
  -- instance offline --> R0 Detached

R2 AttachedPinned
  -- /use(other thread，同 instance 可见) --> R2 AttachedPinned
  -- /use(other thread，需要切换实例) --> detach 语义清理后 -> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- /follow --> R4 FollowBound 或 R3 FollowWaiting
  -- /new(无 live remote work，当前 thread 有 cwd) --> R5 NewThreadReady
  -- selected thread claim 丢失 --> R1 AttachedUnbound 或 R3 FollowWaiting
  -- /detach(no live work) --> R0 Detached
  -- /detach(live work) --> E6 Abandoning -> R0 Detached
  -- instance offline --> R0 Detached

R3 FollowWaiting
  -- VS Code focus 到可接管 thread --> R4 FollowBound
  -- /use(thread，同 instance 可见) --> R2 AttachedPinned
  -- /use(thread，需要切换实例) --> detach 语义清理后 -> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- /detach --> R0 Detached
  -- instance offline --> R0 Detached

R4 FollowBound
  -- VS Code focus 切到其他可接管 thread --> R4 FollowBound
  -- VS Code focus 消失或被别人占用 --> R3 FollowWaiting
  -- /use(thread，同 instance 可见) --> R2 AttachedPinned
  -- /use(thread，需要切换实例) --> detach 语义清理后 -> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- /new(无 live remote work，当前 thread 有 cwd) --> R5 NewThreadReady
  -- /detach(no live work) --> R0 Detached
  -- /detach(live work) --> E6 Abandoning -> R0 Detached
  -- instance offline --> R0 Detached

R5 NewThreadReady
  -- 第一条普通文本 --> R5 + E1/E2，等待新 thread 落地
  -- turn.started(remote_surface，新 thread) --> R2 AttachedPinned
  -- /use(thread) 且仅有 staged/queued draft --> discard drafts + R2 AttachedPinned
  -- /follow 且仅有 staged/queued draft --> discard drafts + R4 FollowBound 或 R3 FollowWaiting
  -- 重复 /new 且无 draft --> 保持 R5，仅回 already_new_thread_ready
  -- 重复 /new 且仅有 staged/queued draft --> discard drafts，保持 R5
  -- thread/start/dispatch 失败 --> 保持 R5
  -- /detach(no live work 或仅 unsent draft) --> R0 Detached
  -- /detach(dispatching/running 首条消息) --> E6 Abandoning -> R0 Detached
```

补充说明：

1. `R5` 下首条文本 queued 后，第二条文本与新图片都会被拒绝，直到该新 thread 真正落地。
2. `R5` 下 `/use`、`/follow` 只会在首条消息已 `dispatching/running` 时被拒绝；若只是 staged/queued draft，会先丢弃再切走。
3. `/attach` 或 `/use` 进入某个已选 thread 后，还会执行一次 thread replay 检查：
   1. 该 thread idle 且存在 `UndeliveredReplay` 时，会立刻补发并清空。
   2. 该 thread busy 时不会插入旧 final/旧 notice，候选保留到后续 idle 的 `/attach` 或 `/use`。
4. global `/use` 的 resolver 顺序当前是：
   1. 当前 attached instance 内可见 thread。
   2. free existing visible instance。
   3. reusable managed headless。
   4. create managed headless。
5. 当 `/use` 命中第 2/3/4 类 resolver 时，当前实现会先走 detach 语义清理：
   1. queued / staged draft 会被清掉。
   2. `PromptOverride`、pending request、request capture 会被清掉。
   3. 当前 instance claim 会先释放，再 attach 到新目标。

### 5.2 远端队列生命周期

```text
E0 Idle
  -- enqueue --> E1 Queued
  -- dispatchNext --> E2 Dispatching

E2 Dispatching
  -- turn.started(remote_surface) --> E3 Running
  -- command rejected / dispatch failure --> E0 Idle

E3 Running
  -- turn.completed(remote_surface) --> E0 Idle
```

补充说明：

1. `pendingRemote` 先按 instance 保留“哪个 queue item 正在等 turn”。
2. turn 建立后再提升到 `activeRemote`。
3. 对空 thread 首条消息，promote 会优先按 `Initiator.SurfaceSessionID` 命中。
4. 若 queue item 来自 `R5`，turn.started 后 surface 必须切回 `pinned`，不会继续停在 `new_thread_ready`。

### 5.3 本地 VS Code 仲裁

```text
E0/E1
  -- local.interaction.observed 或 local turn.started --> E4 PausedForLocal

E4 PausedForLocal
  -- local turn.completed 且 queue 空 --> E0 Idle
  -- local turn.completed 且 queue 非空 --> E5 HandoffWait
  -- Tick 超时 --> E0 Idle 并自动恢复 dispatch

E5 HandoffWait
  -- Tick 到期 --> E0 Idle 并继续 dispatchNext
```

补充说明：

1. `/new` 本身不会绕过 instance 级本地仲裁。
2. `R5` 下首条消息如果碰到本地活动，仍可能先在 `PausedForLocal/HandoffWait` 中排队。

### 5.4 headless 生命周期

```text
G0 None
  -- /newinstance --> G1 PendingHeadlessStarting
  -- /use(thread，需要 create headless) --> G1 PendingHeadlessStarting

G1 PendingHeadlessStarting
  -- instance connected 且 pending.ThreadID != "" --> R2 AttachedPinned + G0 None
  -- instance connected 且 pending.ThreadID == "" --> attach headless instance + G2 PendingHeadlessSelecting
  -- /killinstance --> G0 None
  -- Tick timeout --> kill headless + clear pending + detach if needed

G2 PendingHeadlessSelecting
  -- resume_headless_thread(thread) --> R2 AttachedPinned + G0 None
  -- /killinstance --> G0 None
  -- 无 recoverable threads --> kill headless + R0 Detached + G0 None
```

### 5.5 detach / abandoning 生命周期

```text
/detach
  -- 无 live work --> finalizeDetachedSurface --> R0 Detached
  -- 有 live work --> discard drafts + E6 Abandoning

E6 Abandoning
  -- 当前 turn 收尾 / disconnect / queue fail --> R0 Detached
  -- Tick 超时 --> force finalize --> R0 Detached
```

detach 时额外保证：

1. 未发送 queue item 会被丢弃。
2. staged image 会被丢弃。
3. request prompt / request capture 会被清空。

## 6. 命令矩阵

### 6.1 基础路由态

| 命令 | `R0 Detached` | `R1 AttachedUnbound` | `R2 AttachedPinned` | `R3 FollowWaiting` | `R4 FollowBound` | `R5 NewThreadReady` |
| --- | --- | --- | --- | --- | --- | --- |
| `/list` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/newinstance` | 允许 | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 拒绝 |
| `/new` | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 允许；若首条消息已 dispatching/running 则拒绝 |
| `/killinstance` | 仅 pending headless 时有效 | 仅 headless attach/launch 时有效 | 同左 | 同左 | 同左 | 同左 |
| `/use` `/useall` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许；若仅有 unsent draft 会先丢弃 |
| `/follow` | 拒绝 | 允许 | 允许 | 允许 | 允许 | 允许；若仅有 unsent draft 会先丢弃 |
| 文本 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 允许首条；首条 queued/dispatching/running 后拒绝第二条 |
| 图片 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 仅在首条文本尚未入队前允许 |
| 请求按钮 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 理论上通常不会出现；若出现仍按 attached surface 处理 |
| `/stop` | 通常无效果 | 通常无效果 | 允许 | 允许 | 允许 | 允许；可清掉 staged/queued draft |
| `/status` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/detach` | 允许但通常只提示已 detached | 允许 | 允许 | 允许 | 允许 | 允许；dispatching/running 时走 abandoning |
| `/model` `/reasoning` `/access` | 拒绝 | 允许 | 允许 | 允许 | 允许 | 允许 |

### 6.2 覆盖门禁

| 覆盖状态 | 当前行为 |
| --- | --- |
| `G1/G2 PendingHeadless` | 只允许 `/status`、`/killinstance`、`resume_headless_thread`、revoke/reaction；其余动作统一被 headless notice 挡住 |
| `G3 PendingRequest` | 普通文本、图片、`/new` 被挡；若 `/use` 需要切换到其他实例，也会先被挡住；用户必须先处理请求卡片 |
| `G4 RequestCapture` | 下一条文本优先被当成反馈；`/new` 和需要切换实例的 `/use` 都会被 request-capture gate 拒绝 |
| `E6 Abandoning` | 只允许 `/status`；再次 `/detach` 只回 `detach_pending`；其余动作统一拒绝 |

## 7. UI 动作协议

当前 Feishu 卡片动作与服务端 action 对应关系如下：

| 卡片动作 | 服务端 action | 说明 |
| --- | --- | --- |
| `attach_instance` | `ActionAttachInstance` | 直达 attach |
| `use_thread` | `ActionUseThread` | 直达 thread 切换 |
| `resume_headless_thread` | `ActionResumeHeadless` | 直达 headless 恢复 |
| `kick_thread_confirm` | `ActionConfirmKickThread` | 强踢前再次校验实时状态 |
| `kick_thread_cancel` | `ActionCancelKickThread` | 仅回 notice |
| `prompt_select` | `ActionSelectPrompt` | 旧兼容入口，统一回 `selection_expired` |

菜单与文本命令里新增：

1. `/new`
2. 菜单 `new`

二者都直接映射到 `ActionNewThread`。

## 8. 当前死状态审计结论

这轮按当前实现重新审计后，以下几类 bug-grade 半死状态已经收口：

1. **instance 半 attach**：已修复。第二个 surface attach 同一 instance 会直接失败。
2. **数字文本误切换 thread**：已修复。数字文本现在是普通消息。
3. **headless 选择期还能旁路 `/use` `/follow` `/new`**：已修复。`PendingHeadless` 仍是顶层 gate。
4. **staged image 跟着 route change 串 thread**：已修复。route change 或 clear 会显式丢图并告知用户。
5. **`PausedForLocal` 永久卡住**：已修复。现在有 watchdog。
6. **`Abandoning` 永久锁死**：已修复。现在有 watchdog。
7. **`/follow` 切模式但 thread 不变时 UI 不知道 route mode 已变**：已修复。现在会补发 route-mode selection 投影。
8. **`/new` 的空 thread 归属靠 `ActiveThreadID` 猜**：已修复。现在改成显式 `remote_surface + SurfaceSessionID` 相关性。
9. **`R5 NewThreadReady` 在 queued draft 时没有出口**：已修复。现在 `/use`、`/follow`、`/detach`、`/stop`、重复 `/new` 都有明确语义。
10. **detach 期间最后一条 final / thread notice 会被完全吞掉**：已修复。当前会保留单条 thread 级 replay，并在后续 idle 的 `/attach` 或 `/use` 时一次性补发。
11. **detached 状态下 `/use` 是死入口，只能先 attach instance**：已修复。现在 `/use` 会展示 global merged thread list，并按 resolver 自动 attach。
12. **cross-instance `/use` 会绕过 detach 语义，保留旧 request/capture/override**：已修复。现在会先走 detach 风格清理与门禁，再 attach 新 thread。
13. **thread-first create headless 仍然要先起空实例再选 thread**：已修复。global `/use` 触发的新 headless 会带 preselected thread 直接落到目标 thread。

当前审计范围内，未再发现“attach/use 成功后用户没有任何可恢复下一步”的 bug-grade 状态。

## 9. `/new` 相关补充文档

`/new` 已经是当前实现的一部分。

功能级实现说明见：

1. [new-thread-command-design.md](../implemented/new-thread-command-design.md)

## 10. 提交前复审基线

凡是修改以下任一行为，都应该在提交前回看本文并同步更新：

1. instance/thread attach/detach
2. `/use`、`/follow`、`/new`
3. `PendingHeadless`
4. queue/dispatch/turn ownership
5. staged image / draft 命运
6. request capture / request prompt
7. Feishu 卡片动作协议
8. watchdog 与恢复路径

最低复审问题：

1. 有没有新增“用户表面上看已 attach 或已选 thread，但文本/图片仍无路可走”的状态。
2. 有没有新增只靠异步事件才能退出、但没有 watchdog 或手动逃生口的 blocked state。
3. 有没有让未冻结草稿在 route change 时静默改投目标。
4. 有没有把 UI helper 状态重新变回服务端持久 modal state。
5. 有没有让 `R5 NewThreadReady` 在首条消息失败后落回无恢复路径的状态。

## 11. 待讨论取舍

- 是否继续保留 `/newinstance` 这条“先起实例，再选恢复 thread”的旧路径，还是在后续阶段与 global `/use` 的 thread-first 语义完全统一。
- 影响状态与迁移：`G1 PendingHeadlessStarting`、`G2 PendingHeadlessSelecting`、手工 headless 恢复卡片。
- 当前最安全默认值：保留 `/newinstance` 作为显式手工入口，把默认 thread-first 工作流放在 `/use`，避免两条路径在同一轮里同时重写。

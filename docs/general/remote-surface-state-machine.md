# Remote Surface 核心状态机

> Type: `general`
> Updated: `2026-04-10`
> Summary: 同步当前 workspace-aware normal mode 与 vscode mode，并补齐新的飞书命令面：canonical slash/menu key、阶段感知 `/menu` 首页，以及 bare `/mode` `/autocontinue` `/reasoning` `/access` `/model` `/debug` `/upgrade` 的统一参数卡表单；同时记录 `/use` / `/useall` 的 scoped/global 展示规则，以及 persisted sqlite recent-thread freshness 只补主交互会话并过滤内部 probe / agent-role 会话。

## 1. 文档定位

这份文档描述的是**当前代码已经实现**的 remote surface 状态机，不是历史问题列表，也不是未来方案草稿。

它承担两个职责：

1. 作为当前 remote surface 行为的长期 source of truth。
2. 作为后续状态机相关改动在提交前必须回看的 guardrail。

审计基线覆盖：

1. [internal/core/orchestrator/service.go](../../internal/core/orchestrator/service.go)
2. [internal/core/orchestrator/service_surface.go](../../internal/core/orchestrator/service_surface.go)
3. [internal/core/orchestrator/service_thread_global.go](../../internal/core/orchestrator/service_thread_global.go)
4. [internal/core/orchestrator/service_snapshot.go](../../internal/core/orchestrator/service_snapshot.go)
5. [internal/core/orchestrator/service_test.go](../../internal/core/orchestrator/service_test.go)
6. [internal/core/state/types.go](../../internal/core/state/types.go)
7. [internal/core/control/types.go](../../internal/core/control/types.go)
8. [internal/core/control/feishu_commands.go](../../internal/core/control/feishu_commands.go)
9. [internal/core/orchestrator/service_autocontinue.go](../../internal/core/orchestrator/service_autocontinue.go)
10. [internal/codexstate/sqlite_threads.go](../../internal/codexstate/sqlite_threads.go)
11. [internal/adapter/feishu/gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go)
12. [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
13. [internal/core/orchestrator/service_command_menu.go](../../internal/core/orchestrator/service_command_menu.go)
14. [internal/app/daemon/app_headless.go](../../internal/app/daemon/app_headless.go)
15. [internal/app/daemon/app_headless_restore_hints.go](../../internal/app/daemon/app_headless_restore_hints.go)
16. [internal/app/daemon/app_ingress.go](../../internal/app/daemon/app_ingress.go)
17. [internal/app/daemon/app_surface_resume_state.go](../../internal/app/daemon/app_surface_resume_state.go)
18. [internal/app/daemon/surface_resume_state.go](../../internal/app/daemon/surface_resume_state.go)
19. [internal/app/daemon/app_test.go](../../internal/app/daemon/app_test.go)
20. [internal/app/daemon/surface_resume_state_test.go](../../internal/app/daemon/surface_resume_state_test.go)
21. [internal/app/daemon/admin_vscode.go](../../internal/app/daemon/admin_vscode.go)
22. [internal/app/daemon/app_vscode_migration.go](../../internal/app/daemon/app_vscode_migration.go)
23. [internal/app/daemon/app_vscode_migration_test.go](../../internal/app/daemon/app_vscode_migration_test.go)

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

## 3. 当前状态机的五层结构与运行时 overlay

surface 不是单一枚举，而是五层正交状态叠加。

### 3.1 产品模式 overlay

| 代号 | 条件 | 用户语义 |
| --- | --- | --- |
| `M0 Normal` | `ProductMode=normal` | 新 surface 默认值；当前会开启 workspace claim 仲裁，并把已占用 workspace 投影到 `/status` |
| `M1 VSCode` | `ProductMode=vscode` | 只能显式 `/mode vscode` 进入；当前不参与 workspace claim，仍保留既有 instance/thread-first 路由语义 |

补充说明：

1. `ProductMode` 当前是 surface 级字段；`/detach` 不会清掉它。
2. `ProductMode` 当前已经进入 daemon 级 `surface resume state`：
   1. 进程内已有 surface 会保留它。
   2. daemon 重启后，startup 会先从 `surface resume state` materialize latent surface，并恢复之前的 `ProductMode`。
   3. `normal` mode surface 随后会按 persisted resume target 继续尝试恢复：
      1. 优先 exact visible thread 恢复。
      2. visible thread 当前不可见时，可降级回原 workspace 的 attached-unbound 语义。
      3. 若还带有 headless restore hint，则只有在 visible/workspace 路径没有恢复成功时，才继续交给 headless auto-restore。
      4. 若持久化目标里包含 `ResumeThreadID`，则在 daemon 启动后的首轮 `threads.refresh -> threads.snapshot` 完成前，会先保持 detached 并静默等待，避免过早降级或过早报失败。
   4. `vscode` mode surface 会按 persisted `ResumeInstanceID` 继续尝试恢复：
      1. 先做本机 VS Code 兼容性检查：
         1. 若检测到旧版 `settings.json` override，或当前 managed shim 已失效，则保持 detached，并发迁移/修复卡片。
         2. 若兼容性检查通过，才继续 exact-instance 恢复。
      2. 只允许恢复到 exact VS Code instance，不做 workspace fallback。
      3. 恢复成功后直接回到现有 vscode attach/follow-local 路径。
      4. 若当前还没有新的 VS Code 活动可继续 follow，会保留 follow waiting，并明确提示用户去 VS Code 再说一句话或手动 `/use`。
      5. stale headless restore hint 会被主动清理，恢复链路不会 attach/start headless。
3. `/mode` 当前只在没有 live remote work 的 surface 上执行切换：
   1. 会先走 detach-like 清理。
   2. 清掉 attachment / workspace claim / thread claim、`PromptOverride`、`PendingRequest`、`RequestCapture`、`PreparedThread*`、staged image 与 queued draft。
   3. 如果当时还带着 `PendingHeadless`，会先显式 kill 当前 headless 启动流程，并清掉 headless restore hint 与内存恢复状态。
4. 若当前仍有 live remote work，则 `/mode` 直接拒绝，并明确提示用户 `/stop` 或 `/detach`。
5. `Abandoning` 仍是更高优先级 gate；但 `PendingHeadless` 不再阻塞 `/mode`，用户可以直接切到 `vscode` 终止恢复流程。
6. 当前 `/list` 已按 mode 分流：
   1. normal mode 列 workspace，并走 workspace attach/switch。
   2. vscode mode 继续列在线 VS Code instance。
7. `normal mode` 当前已经完成这一轮产品收窄：
   1. detached `/use` 现在直接等同 detached `/useall`：都会展示 cross-workspace 的全部会话列表。
   2. attached `/use` 现在只展示当前 workspace 的最近 5 个会话；若超过 5 个，会在卡片底部追加一个 `show_scoped_threads` 按钮进入“当前工作区全部会话”。
   3. attached `/useall` 现在改成 cross-workspace 的全部会话列表；卡片里的 `use_thread` 按钮会显式携带 `allow_cross_workspace=true`，允许直接切到其他 workspace。
   4. `/new` 已变成 workspace-owned prepared state。
   5. `/follow` 在 normal mode 下只返回迁移提示，不再进入 follow route。
8. `vscode mode` 当前已经完成这一轮收窄：
   1. `/list` attach/switch instance 后默认进入 follow-first，而不是落回 pinned/unbound。
   2. 默认跟随目标只看 `ObservedFocusedThreadID`，不再回落 `ActiveThreadID`。
   3. detached `vscode /use` / `/useall` 会直接拒绝，并要求先 `/list`。
   4. attached `vscode /use` / `/useall` 只看当前 attached instance 的已知 thread 集合；其中 `/use` 也是最近 5 个 + `show_scoped_threads`，`/useall` 才是当前实例全部会话。
   5. `vscode /use` 的 one-shot force-pick 会保留 `RouteMode=follow_local`，后续 observed focus 仍可覆盖。

### 3.2 路由主状态

| 代号 | 条件 | 用户语义 |
| --- | --- | --- |
| `R0 Detached` | `AttachedInstanceID == ""` | 当前没有接管任何目标；normal mode 下表现为“未接管工作区”，vscode mode 下表现为“未接管实例” |
| `R1 AttachedUnbound` | `AttachedInstanceID != ""`，`RouteMode=unbound`，`SelectedThreadID == ""` | 已接管目标但当前没有可发送 thread；normal mode 下通常表示“已接管 workspace、未选 thread” |
| `R2 AttachedPinned` | `AttachedInstanceID != ""`，`RouteMode=pinned`，`SelectedThreadID != ""`，且持有 thread claim | 当前输入固定发到该 thread |
| `R3 FollowWaiting` | `AttachedInstanceID != ""`，`RouteMode=follow_local`，`SelectedThreadID == ""` | 仅 `vscode mode` 合法：已进入 follow，但当前没有可接管 thread |
| `R4 FollowBound` | `AttachedInstanceID != ""`，`RouteMode=follow_local`，`SelectedThreadID != ""`，且持有 thread claim | 仅 `vscode mode` 合法：已跟随到一个 thread |
| `R5 NewThreadReady` | `AttachedInstanceID != ""`，`RouteMode=new_thread_ready`，`SelectedThreadID == ""`，`PreparedThreadCWD != ""` | 仅 `normal mode` 合法：已准备一个待 materialize 的新 thread；下一条普通文本会创建新 thread |

补充说明：

1. `R0 Detached` 现在允许存在一种 daemon materialize 出来的 latent surface：
   1. surface 有 `gateway/chat/user` 路由信息。
   2. surface 的 `ProductMode` 已从持久化 `surface resume state` 恢复。
   3. surface 可能还带有持久化的 resume target 元数据（instance / thread / workspace / route 语义）；它们不会在 materialize 当下直接投影成 live attach，但 daemon 随后会异步评估恢复。
   4. 对 `normal` mode 来说，这个 latent detached 可能是短暂中间态：
      1. exact visible thread 恢复成功后会进入 `R2 AttachedPinned`。
      2. visible thread 不可见但 workspace 仍可接管时，会进入 `R1 AttachedUnbound`。
      3. 若还在等待 daemon 启动后的首轮 refresh，则会暂时保持 `R0 Detached` 并静默等待。
   5. 对 `vscode` mode 来说，这个 latent detached 也可能是短暂中间态：
      1. 若本机 VS Code 集成仍是旧版 `settings.json` override，或 managed shim 因扩展升级而失效，会保持 `R0 Detached` 并改发迁移/修复卡片。
      2. 兼容性检查通过后，exact instance 恢复成功会进入 `R3 FollowWaiting` 或 `R4 FollowBound`。
      3. 若目标 instance 还没重新连回，会保持 `R0 Detached` 并静默等待。
      4. 不做 workspace fallback，也不会进入 headless 恢复。
   6. 若该 surface 还带有持久化的 headless restore hint，daemon 只会在 `normal` mode 的 visible/workspace 恢复链路之后，再继续评估 normal-mode 的 headless auto-restore；`vscode` mode 会主动清理这类 stale hint。
2. 这种 latent surface 在 route 维度上仍然是 `R0 Detached`，不是新的 route state。
3. 当前 startup 阶段不会因为 resume target 元数据而在 materialize 当下直接进入 `R1~R5`；是否进入后台恢复、是否转入 `G1 PendingHeadlessStarting`，仍取决于 daemon 后续恢复调度，而不是 materialize 本身。

### 3.3 执行状态

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `E0 Idle` | `DispatchMode=normal`，无 active，无 queued | 空闲 |
| `E1 Queued` | `QueuedQueueItemIDs` 非空，`ActiveQueueItemID == ""` | 有待派发远端输入 |
| `E2 Dispatching` | `ActiveQueueItemID` 指向 `dispatching` | prompt 已发给 wrapper，turn 尚未建立 |
| `E3 Running` | `ActiveQueueItemID` 指向 `running` | turn 已进入执行 |
| `E4 PausedForLocal` | `DispatchMode=paused_for_local` | 观察到本地 VS Code 活动，远端暂停出队 |
| `E5 HandoffWait` | `DispatchMode=handoff_wait` | 本地刚结束，等待短窗口后恢复远端队列 |
| `E6 Abandoning` | `Abandoning=true` | surface 已放弃接管，等待已有 turn 收尾后最终 detach |

补充说明：

1. 当前还存在一个**可叠加**的 steering overlay：
   1. 某个 queued item 被点赞升级后，会离开 `QueuedQueueItemIDs`
   2. 该 item 进入 `QueueItemStatus=steering`
   3. 相关命令记录在 `pendingSteers`
2. 这个 overlay 不占用 `ActiveQueueItemID`，所以可以与 `E3 Running` 并存。
3. steering ack 成功后，item 进入 `steered`；失败时恢复回原 queue 位置。

### 3.4 输入门禁状态

| 代号 | 条件 | 作用 |
| --- | --- | --- |
| `G0 None` | 无附加门禁 | 普通输入按主路由走 |
| `G1 PendingHeadlessStarting` | `PendingHeadless.Status=starting` | headless 仍在启动 |
| `G2 PendingRequest` | `PendingRequests` 非空 | 普通文本/图片会被确认卡片门禁挡住 |
| `G3 RequestCapture` | `ActiveRequestCapture != nil` | 下一条普通文本会被当成拒绝反馈 |
| `G4 CommandCapture` | `ActiveCommandCapture != nil` | 仅保留旧 `/model` 历史兼容：当前 UI 不再创建新 capture；若 surface 上残留旧 capture，下一条普通文本会被直接转换成 `/model <输入>` |
| `G5 AbandoningGate` | `Abandoning=true` | 只有 `/status` 与 `/autocontinue` 继续正常，其余动作被挡 |
| `G6 VSCodeCompatibilityBlocked` | `ProductMode=vscode`，surface detached，且本机检测到 legacy `settings.json` override 或 stale managed shim | daemon 不再自动恢复 exact instance，也不再发普通“请先打开 VS Code”提示，而是改发迁移/修复卡片 |

### 3.5 草稿状态

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

### 3.6 auto-continue overlay

`AutoContinueRuntimeRecord` 当前不是新的 route state，而是 surface 上附加的一层运行时 overlay：

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `A0 Disabled` | `AutoContinue.Enabled=false` | 当前 surface 不做自动续跑 |
| `A1 EnabledIdle` | `AutoContinue.Enabled=true`，`PendingReason==""` | 已开启，但当前没有待触发的 auto-continue |
| `A2 Scheduled` | `AutoContinue.Enabled=true`，`PendingReason!= ""`，`PendingDueAt` 非空 | 已记录一次待触发 auto-continue，等待 backoff 到期并再次过门禁 |

补充说明：

1. `A2 Scheduled` 不会直接占用 `ActiveQueueItemID`。
2. 真正 enqueue auto-continue item 发生在 `Tick()`，而不是 `turn.completed` 同步路径里。
3. `A2 Scheduled` 只有在下列条件同时满足时才会真正发出：
   1. surface 仍 attached
   2. `DispatchMode=normal`
   3. 没有 `PendingHeadless` / `PendingRequest` / `RequestCapture` / `Abandoning`
   4. 当前没有 live remote work
4. auto-continue queue item 的 reply anchor 与 pending projection 当前已显式拆开：
   1. 最终回复仍挂回原用户消息
   2. queue / typing / reaction 不再回写到原用户消息

## 4. 当前已实现的不变量

### 4.1 normal mode `/list` 先选 workspace，再落到 `R1 AttachedUnbound`

当前 normal mode 的 `/list` 已经不再列 instance，而是列 workspace。

对应实现里：

1. `presentWorkspaceSelection()` 优先按所有在线 instance 的可见 thread `CWD` 归并 workspace。
   1. 只有当某个 instance 当前完全没有可见 thread 时，才回退到该 instance 的 `WorkspaceKey/WorkspaceRoot`。
   2. 这样 broad headless pool 不会再把多个真实 workspace 压扁成一个实例级根目录。
2. 卡片按钮走 `attach_workspace -> ActionAttachWorkspace`。
3. `attachWorkspace()` 在 normal mode 下先做 `workspaceClaims`，再按“当前 instance / free instance / 当前 workspace 可见 thread 数 / exact workspace match”选择一个可接管的 online instance 落到该 workspace。
4. attach / switch 成功后，统一进入 `R1 AttachedUnbound`，不再复用默认 thread 自动 pin。

同时，`attachInstance()`、`attachSurfaceToKnownThread()` 与 `startHeadlessForResolvedThread()` 在 normal mode 下仍然会先走 `workspaceClaims`，再进入现有 `instanceClaims` / `threadClaims`。

结果：

1. 同一个 workspace 当前最多只允许一个 normal-mode surface 占有。
2. 第二个 normal-mode surface 如果试图通过 `/list` attach/switch 到同 workspace，或 `/use` / headless 恢复到该 workspace，会直接收到 `workspace_busy`。
3. 同一个 instance 仍然只能被一个飞书 surface attach；也就是说 instance claim 还在，只是已经退回到 workspace claim 之后。
4. 不会进入“workspace 仲裁层已经冲突，但仍然 attach 成功”的半 attach 状态。
5. normal mode 的 `/list` attach/switch 不会自动抢默认 thread；用户会明确落到 `R1`，然后继续 `/use` 或点 thread 卡片。

### 4.2 thread claim 仍是全局的，但在 normal mode 下退回 workspace 内仲裁

当前 `threadClaims` 仍按 `threadID` 做全局仲裁。

结果：

1. 一个 thread 同时只能被一个飞书 surface 占有。
2. normal mode 下，如果目标 thread 所在 workspace 已被其他 normal-mode surface 占有，会先在 workspace 层被禁用，不再进入 thread kick 逻辑。
3. `/use` 命中已被他人占用的 thread 时：
   1. 如果目标 thread 在**当前 attached instance 内可见**，仍保留现有强踢逻辑：
      1. 对方 idle 才会弹强踢确认。
      2. 对方 queued/running 会直接拒绝。
   2. 如果目标 thread 走的是 global thread-first attach 路径，不提供强踢，只会在列表里显示 busy 并禁用。

### 4.3 `PendingHeadless` 仍是 dominant gate

只要 `PendingHeadless != nil`：

1. 允许：`/status`、`/autocontinue`、`/mode`、`/detach`、旧 `/newinstance` / 旧 `/killinstance` / 历史 `resume_headless_thread` 的兼容提示、消息撤回、reaction。
2. 其余 surface action 全部在 `ApplySurfaceAction()` 顶层被拦截。

这意味着：

1. `starting` 时不能旁路 attach/use/follow/new。
2. detached `/use` 触发的 preselected headless，在实例连上后会直接落到目标 thread，不会再进入手工 selecting。
3. `/mode vscode` 与 `/detach` 都会主动取消当前恢复流程，并回到 detached 态；此外还有启动超时 watchdog。
4. 旧 `/newinstance`、旧 `/killinstance` 与旧 `resume_headless_thread` 卡片即使仍被用户触发，也只会返回迁移提示，不会改动当前 pending headless。
5. 后台 auto-restore 触发的 pending headless 也复用同一个 `G1` gate：
   1. 启动阶段默认静默，不额外发 “headless_starting”。
   2. 成功后只发一条恢复成功 notice。
   3. 失败或超时后只发一条恢复失败 notice，并回到 `R0 Detached`。

### 4.4 选择卡片不再是服务端持久 modal 状态

当前服务端已经不再保存 `SelectionPrompt` 状态，也不再把“纯数字文本”解释成选择。

当前行为：

1. attach/use/kick confirm 都改成**直达动作**。
2. Feishu 卡片按钮直接携带：
   1. `attach_workspace`
   2. `attach_instance`
   3. `use_thread`
   4. `show_scoped_threads`
   5. `kick_thread_confirm`
   6. `kick_thread_cancel`
3. `use_thread` 会按卡片来源附带额外上下文：
   1. normal `/useall` 与 detached/global `/use` 会携带 `allow_cross_workspace=true`
   2. attached current-scope `/use` 不会带这个标记，因此仍只允许留在当前 workspace / 当前 instance 内
4. 旧 `resume_headless_thread` 只保留兼容解析，服务端统一返回迁移提示。
4. 旧 `prompt_select` 只保留兼容解析，服务端统一返回 `selection_expired`。
5. `"1"`、`"2"` 这类纯数字文本现在就是普通文本。

### 4.5 route change 与 `/new` 都会显式处理未发送草稿

当前有两类固定规则：

1. 普通 route change，例如 `/use`、`vscode mode /follow`、follow 自动切换、claim 丢失回退：
   1. 只丢 staged image。
   2. 不会静默把未冻结图片串到新 thread。
2. clear 语义，例如 `/stop`、`/detach`、`/mode`、`/new`、`R5` 下的 `/use` / `vscode mode /follow`：
   1. staged image 和 queued draft 都会被显式丢弃。
   2. 会发 discard reaction / notice。

当前实现不允许未发送草稿在 route change 时 silently retarget。

### 4.6 queued 点赞 steering 只升级当前 item，不做隐式重排

当前 `surface.message.reaction.created` 的产品语义已经固定：

1. 只有 `ThumbsUp` 才会触发。
2. 只有 queued item 的主文本 `SourceMessageID` 能触发。
3. 图片消息上的点赞不会单独触发任何状态迁移。
4. 目标 item 必须和当前 active running turn 属于同一 `FrozenThreadID`。
5. 命中后不会改写其他 queued item 的相对顺序，也不会跨 thread 偷偷 retarget。
6. steering 失败时，目标 item 必须恢复回原 queue 位置，不能 silently 消失。

### 4.7 `R5 NewThreadReady` 是稳定态，不是半成品

当前 `/new` 已实现为 clear-and-prepare：

1. normal mode 下，只要 surface 已 attach 且当前 workspace 已知，就允许进入。
2. vscode mode 下，`/new` 直接拒绝，并明确提示用户先 `/mode normal`，或继续 follow / `/use` 当前 VS Code 会话。
3. 不允许 fallback 到 home。
4. 进入时会释放旧 thread claim，但保留 instance attachment 与 `PromptOverride`。
5. `PreparedThreadCWD`、`PreparedFromThreadID`、`PreparedAt` 会显式保存。

这带来三个关键性质：

1. `R5` 没有“attach 成功但用户无路可走”的问题。
2. `R5` 下第一条普通文本合法，且会创建新 thread。
3. `R5` 下如果只有 staged/queued draft，用户仍然能 `/use`、`/detach`、`/stop` 或重复 `/new`。

### 4.8 空 thread turn 不再靠 `ActiveThreadID` 猜归属

当前 empty-thread 首条消息的 turn 归属已经改成显式相关性：

1. queue item 仍以 `FrozenThreadID == ""` 派发。
2. translator 在 `turn.started` 时提供 `InitiatorRemoteSurface + SurfaceSessionID`。
3. orchestrator 优先用 `Initiator.SurfaceSessionID` 命中 pending remote item。
4. 命中后回填真实 `threadID`，并把 surface 从 `R5` 切回 `R2 AttachedPinned`。

当前不再用“`FrozenThreadID == ""` 时退化匹配 `inst.ActiveThreadID`”来猜归属。

### 4.9 `PausedForLocal` 和 `Abandoning` 都有 watchdog

当前 `Tick()` 已经提供两类恢复：

1. `paused_for_local` 超时后：
   1. 自动回到 `normal`
   2. 发 `local_activity_watchdog_resumed`
   3. 继续 `dispatchNext`
2. `abandoning` 超时后：
   1. 强制 `finalizeDetachedSurface`
   2. 发 `detach_timeout_forced`

所以这两个状态不再依赖单一异步事件才能退出。

### 4.10 thread 级未投递回放是 thread-global 单槽、内存态、一次性

当前 `ThreadRecord` 增加了 `UndeliveredReplay`，但它不是完整历史，只是 thread 级的单槽候选。

当前规则：

1. 只记录两类内容：
   1. 没有任何飞书 surface 可投递时产生的 final assistant block。
   2. 没有任何目标 surface 时产生的 thread-scoped system/problem notice。
2. 同一 `threadID` 的 replay 当前按 relay 全局单槽处理：
   1. 一条新候选会覆盖旧候选，不保留 backlog。
   2. cross-instance attach / `/use` 时会先从其他 instance 迁移到当前目标 thread，再尝试补发。
3. 同一 thread 的内容一旦已经成功投递到当前 surface，就会清空所有已知 instance 上的旧 replay，避免后续重复补发。
4. 只有两条显式入口会尝试回放：
   1. `/attach` 成功后默认选中的 thread。
   2. `/use` 选中的 thread。
5. 回放前会检查该 thread 是否 idle：
   1. 若 `inst.ActiveTurnID != ""` 且 `inst.ActiveThreadID == threadID`，则本次不补发。
   2. 候选继续保留，等待后续 idle 的 `/attach` 或 `/use`。
6. 回放成功后立即清空，因此同一条内容只会补发一次。
7. 该状态仅保存在 relay 内存里；`relayd` 重启后丢失是当前已接受语义。
8. 后台 headless auto-restore attach 是明确例外：
   1. 不会补发旧 replay。
   2. 会直接清空该 thread 的旧 replay。
   3. 用户只会看到一条新的恢复成功提示。

### 4.11 `/status` 当前至少会显式投影 mode / attach object / gate / dispatch / retained-offline

当前 `Snapshot` 不再只展示 attachment 和 next prompt。

现在至少会额外投影七类“决定下一条输入会发生什么”的状态：

1. 当前 `ProductMode`
   1. `normal`
   2. `vscode`
2. 当前 attach 对象类型
   1. `工作区`
   2. `VS Code 实例`
   3. `headless 实例`
   4. `实例`
3. 当前已占用的 workspace（若有）
4. request gate：
   1. `PendingRequest`
   2. `RequestCapture`
5. dispatch / queue：
   1. `Dispatching`
   2. `Running`
   3. `PausedForLocal`
   4. `HandoffWait`
   5. queued count
6. auto-continue runtime：
   1. enabled / disabled
   2. pending reason
   3. pending due time
   4. consecutive count
7. transport degraded 后“attachment 仍保留但实例已离线”的 retained-offline 状态。

它仍然不是完整调试面板，但已经能回答最关键的问题：

1. 当前到底记住的是 `normal` 还是 `vscode`。
2. 当前接管的是工作区、VS Code 实例，还是 headless/其他实例。
3. 当前到底占着哪个 workspace。
4. 下一条文本是不是会先被 request gate 吃掉。
5. 下一条文本是不是会先被 legacy `/model` capture 兼容态吃掉。
6. 现在是执行中、排队中，还是被本地 VS Code 暂停。
7. auto-continue 当前是关闭、待触发，还是刚因 backoff 暂缓。
8. attachment 还在不在，以及当前是不是在等实例恢复。

### 4.12 `/mode` 是 surface 级 overlay，当前只负责记忆与清理切换

当前 `/mode` 的实现边界已经固定为：

1. bare `/mode` 当前不再直接回 `Snapshot`，而是返回当前模式 + normal/vscode 切换卡。
2. `/mode normal` / `/mode vscode` 允许在 detached、idle attached、或 `PendingHeadless` 尚未进入 live remote work 的 surface 上切换。
3. 切换时一定先做 detach-like 清理，再进入目标 mode 的 detached 态；workspace claim 也会一起释放。
4. 若切换前存在 `PendingHeadless` 或已持久化的 headless restore hint，会一并 kill / clear，避免 mode 切完以后又被后台恢复拉回 headless。
5. `vscode` surface 不参与 headless auto-restore；即使仍有残留 hint 或可复用 headless，后台恢复入口也会直接跳过。
6. 当前 mode 会跨 daemon 重启保留：
   1. startup 会先恢复 latent surface 与 `ProductMode`
   2. `normal` mode 会继续按 persisted target 尝试自动恢复：exact visible thread > workspace attach fallback > 与 headless restore 协同
   3. 若存在 `ResumeThreadID`，在首轮 `threads.refresh -> threads.snapshot` 完成前会先静默等待，不会过早降级或直接报失败
   4. `vscode` mode 会按 exact `ResumeInstanceID` 尝试恢复：恢复成功后回到 `follow_local`，若暂时缺少新的 VS Code 活动则明确提示用户去 VS Code 再说一句话或手动 `/use`
7. 切换当前已经会改变 `/list` 的主交互语义：
   1. `normal` 下 `/list` 是 workspace chooser。
   2. `vscode` 下 `/list` 是 instance chooser。
8. `normal mode` 下 `/follow` 已退出长期路径；`vscode mode` 当前则固定走 follow-first，并把 `/use` 收窄到当前 instance 内的一次性 force-pick。
9. 若当前仍有 running / dispatching / queued work，则 `/mode` 会直接拒绝，而不是进入半切换状态。

### 4.13 `/autocontinue` 是 surface 级、内存态、跨 route 可查询的 overlay 开关

当前 `/autocontinue` 不要求 surface 已 attach：

1. detached surface 也可以直接 bare `/autocontinue` 查询并打开 on/off 参数卡；带参数时可直接切换。
2. `PendingHeadless` 期间 `/autocontinue` 仍然允许，不会被顶层 gate 挡住。
3. `Abandoning` 期间 `/autocontinue` 也仍然允许，用户可以查看或关闭当前 surface 的 auto-continue。
4. daemon 重启后不恢复该开关；当前已接受这是内存态语义。

### 4.14 `/menu` 现在是阶段感知首页，不再是静态平铺目录

当前 `/menu`、静态 bot 菜单和 slash parser 已经统一到同一套 canonical command metadata。

当前行为：

1. `/menu` 首页按当前阶段重排，但二级分组保持稳定：
   1. `当前工作`
   2. `发送设置`
   3. `切换目标`
   4. `低频与维护`
2. detached 首页前排固定为：
   1. `/list`
   2. `/use`
   3. `/status`
3. `normal` working 首页前排固定为：
   1. `/stop`
   2. `/new`
   3. `/reasoning`
   4. `/model`
   5. `/access`
4. `vscode` working 首页前排固定为：
   1. `/stop`
   2. `/reasoning`
   3. `/model`
   4. `/access`
   5. `/follow`
5. `normal` working 首页与主路径里不再暴露 `/follow`。
6. bare 参数命令现在统一走“快捷按钮 + 单字段表单”：
   1. `send settings`：`/reasoning`、`/model`、`/access`
   2. `maintenance`：`/mode`、`/autocontinue`、`/debug`、`/upgrade`
   3. 表单提交通过 card callback `submit_command_form` 拼回 canonical slash text，再复用文本命令解析链路。
7. 旧 `/model start_command_capture` 卡片只保留历史兼容：
   1. 点击后不会再创建新的 `G4 CommandCapture`
   2. 服务端会直接重新打开新的 `/model` 表单卡
   3. 若 daemon 热更新前已经残留 `G4`，下一条文本会立即应用，不再要求再点一次 Apply
8. 二级分组当前通过卡片按钮 + breadcrumb 返回首页实现，不依赖飞书后台把整棵导航树都铺成静态菜单。

### 4.15 auto-continue 调度只允许走显式 reply-anchor，不再伪造用户消息 pending/typing

当前 auto-continue queue item 增加了显式来源类型：

1. `SourceKind=user`
2. `SourceKind=auto_continue`

当前行为已经固定为：

1. 普通用户输入 item：
   1. `SourceMessageID` / `SourceMessageIDs` 用于 pending、typing、revoke、reaction 投影。
   2. 最终回复默认 reply 到同一条原用户消息。
2. auto-continue item：
   1. `SourceMessageID` 为空，不再触发 pending / typing / thumbs projection。
   2. `ReplyToMessageID` 单独保留原用户消息锚点。
   3. 最终回复继续 reply 到原用户消息。

## 5. 主要状态迁移

### 5.1 attach / use / follow / new

```text
R0 Detached
  -- /list -> attach_workspace(normal mode，workspace 可接管) --> R1 AttachedUnbound
  -- /list -> attach_instance(vscode mode 且 observed focus 可接管) --> R4 FollowBound
  -- /list -> attach_instance(vscode mode 且尚无可接管 observed focus) --> R3 FollowWaiting
  -- /use(thread，normal mode 且可解析到当前可用实例) --> R2 AttachedPinned
  -- /use(thread，normal mode 且需要新 headless) --> R0 + G1 PendingHeadlessStarting
  -- /use(thread，vscode mode) --> 拒绝 + migration to /list
  -- daemon startup latent normal surface + exact visible thread restore --> R2 AttachedPinned
  -- daemon startup latent normal surface + workspace fallback --> R1 AttachedUnbound
  -- daemon startup latent normal surface + waiting first refresh --> 保持 R0 Detached
  -- daemon startup latent vscode surface + exact instance resume --> R3 FollowWaiting 或 R4 FollowBound

R1 AttachedUnbound
  -- /use(thread，同 instance 可见) --> R2 AttachedPinned
  -- /use(thread，normal mode 且目标在其他 workspace) --> 拒绝 + migration to /list
  -- /use(thread，normal mode 且需要切换实例但仍在当前 workspace) --> detach 语义清理后 -> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- /follow(vscode mode) --> R4 FollowBound 或 R3 FollowWaiting
  -- /follow(normal mode) --> 拒绝 + migration notice
  -- /new(normal mode，workspace 已知) --> R5 NewThreadReady
  -- /detach --> R0 Detached

R2 AttachedPinned
  -- /use(other thread，同 instance 可见) --> R2 AttachedPinned
  -- /use(other thread，normal mode 且目标在其他 workspace) --> 拒绝 + migration to /list
  -- /use(other thread，normal mode 且需要切换实例但仍在当前 workspace) --> detach 语义清理后 -> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- /follow(vscode mode) --> R4 FollowBound 或 R3 FollowWaiting
  -- /follow(normal mode) --> 拒绝 + migration notice
  -- /new(normal mode 且无 live remote work，workspace 已知) --> R5 NewThreadReady
  -- selected thread claim 丢失 --> R1 AttachedUnbound 或 R3 FollowWaiting(vscode mode)
  -- /detach(no live work) --> R0 Detached
  -- /detach(live work) --> E6 Abandoning -> R0 Detached

R3 FollowWaiting
  -- VS Code focus 到可接管 thread --> R4 FollowBound
  -- /use(thread，当前 attached instance 可见) --> R4 FollowBound
  -- /use(thread，其他 instance / persisted global thread) --> 拒绝 + migration to /list
  -- /detach --> R0 Detached

R4 FollowBound
  -- VS Code focus 切到其他可接管 thread --> R4 FollowBound
  -- VS Code focus 消失或被别人占用 --> R3 FollowWaiting
  -- /use(thread，当前 attached instance 可见) --> R4 FollowBound
  -- /use(thread，其他 instance / persisted global thread) --> 拒绝 + migration to /list
  -- /new --> 拒绝 + 提示先 `/mode normal`，或继续 follow / `/use`
  -- /detach(no live work) --> R0 Detached
  -- /detach(live work) --> E6 Abandoning -> R0 Detached

R5 NewThreadReady
  -- 第一条普通文本 --> R5 + E1/E2，等待新 thread 落地
  -- turn.started(remote_surface，新 thread) --> R2 AttachedPinned
  -- /use(thread，normal mode) 且仅有 staged/queued draft --> discard drafts + R2 AttachedPinned
  -- /follow(normal mode) --> 拒绝 + migration notice
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
4. normal mode detached/global `/use` 与 `/useall` 的 resolver 顺序当前是：
   1. 当前 attached instance 内可见 thread。
   2. free existing visible instance。
   3. reusable managed headless。
   4. create managed headless。
5. normal mode detached/global `/use` 与 `/useall` 的**候选 thread 列表**当前先 merge 两类来源：
   1. runtime/catalog 已可见 thread。
   2. Codex sqlite 中最近 persisted 的主交互 thread metadata：
      1. 仅 `cli` / `vscode` source。
      2. 排除 subagent role、`exec` / `mcp` 等后台线程。
      3. 排除内部 probe workspace（例如 `_tmp-codex-thread-latency-*`、`_tmp-codex-appserver-*`）。
6. sqlite 只负责补 freshness，不旁路 resolver：
   1. busy / claim / free-visible / reusable-headless / create-headless 仍只由现有 runtime resolver 决定。
   2. sqlite read 失败或 schema 不兼容时，会安全回退到 runtime/catalog-only 行为。
7. attached normal `/use` / `show_scoped_threads` 当前只看当前 claimed workspace：
   1. `/use` = 最近 5 个
   2. `show_scoped_threads` = 当前工作区全部会话
   3. 两者都不显示 workspace 行，只显示接管状态
8. attached `vscode /use` / `/useall` 当前有两条额外约束：
   1. 只展示当前 attached instance 的可见 thread，不再走 merged global thread view。
   2. force-pick 后会保留 `RouteMode=follow_local`，后续 observed focus 变化仍可覆盖。
9. attached normal `/useall` 当前会显示 cross-workspace 的全部会话，并允许直接点击切到其他 workspace。
10. 当 normal mode `/use` / `/useall` 命中第 2/3/4 类 resolver 时，当前实现会先走 detach 语义清理：
   1. queued / staged draft 会被清掉。
   2. `PromptOverride`、pending request、request capture 会被清掉。
   3. 当前 instance claim 会先释放，再 attach 到新目标。
9. 当 surface 处于 `PendingRequest` 或 `RequestCapture` 时：
   1. same-instance `/use`
   2. `/follow`
   3. follow-local 自动重绑定
   当前都会被冻结，避免 UI 宣布的新目标和下一条普通输入的实际落点不一致。
10. 旧 `/newinstance` 在所有 route state 下都只会回迁移提示，不会创建 headless，也不会改动当前 route。
11. daemon 侧后台 auto-restore 使用的是 headless-only resolver：
   1. 当前可见 thread 若只存在于 VS Code instance，不会被自动 attach 到 VS Code。
   2. 它仍可复用该 thread 的 metadata / cwd。
   3. 后续只允许落到 free visible headless、reusable managed headless，或 create managed headless。

### 5.2 远端队列生命周期

```text
E0 Idle
  -- enqueue --> E1 Queued
  -- dispatchNext --> E2 Dispatching

E1 Queued
  -- queued 主文本被 `ThumbsUp`，且当前有同 thread active turn --> `SteerPending` overlay

E2 Dispatching
  -- turn.started(remote_surface) --> E3 Running
  -- command rejected / dispatch failure --> E0 Idle

E3 Running
  -- turn.completed(remote_surface) --> E0 Idle

`SteerPending` overlay
  -- `turn.steer` command ack accepted --> item `steered`，移除 queue pending reaction，并给主文本 + 已绑定图片补 `ThumbsUp`
  -- `turn.steer` dispatch failure / command rejected --> 恢复到原 queue 位置
  -- transport degraded / disconnect / remove instance --> 恢复到原 queue 位置
```

补充说明：

1. `pendingRemote` 先按 instance 保留“哪个 queue item 正在等 turn”。
2. turn 建立后再提升到 `activeRemote`。
3. 对空 thread 首条消息，promote 会优先按 `Initiator.SurfaceSessionID` 命中。
4. 若 queue item 来自 `R5`，turn.started 后 surface 必须切回 `pinned`，不会继续停在 `new_thread_ready`。
5. `turn.steer` 不会占用 `ActiveQueueItemID`，它只复用当前已经存在的 active running turn。
6. remote turn 在 `turn.completed` 时，若当前 item 满足 auto-continue 触发条件：
   1. surface 不会立刻同步 enqueue 新 item
   2. 只会把 surface 置入 `A2 Scheduled`
   3. 后续等 `Tick()` 到期后再真正 enqueue
7. auto-continue 当前有两条独立触发通道：
   1. `problem.Retryable=true` 的 retryable failure
   2. final assistant 文本命中 incomplete-stop heuristics
8. `/stop` 命中 live remote work 时，会给当前 surface 打一次 `SuppressOnce`：
   1. 本轮 turn 收尾时不会触发 auto-continue
   2. suppress 只消费一次，之后 auto-continue 恢复正常评估
9. 当前 backoff 固定为：
   1. `incomplete_stop`: `3s -> 10s -> 30s`，最多 3 次
   2. `retryable_failure`: `10s -> 30s -> 90s -> 300s`，最多 4 次

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

### 5.4 daemon 重启恢复与 headless 生命周期

```text
G0 None
  -- daemon startup latent normal surface + persisted target --> 先走 normal visible/workspace 恢复判定
  -- /use(thread，需要 create headless) --> G1 PendingHeadlessStarting
  -- R0 Detached 且存在 restore hint --> 后台恢复判定

G1 PendingHeadlessStarting
  -- instance connected 且 pending.ThreadID != "" 且非 auto-restore --> R2 AttachedPinned + G0 None
  -- instance connected 且 pending.ThreadID != "" 且 auto-restore --> R2 AttachedPinned + G0 None + 单条恢复成功 notice
  -- instance connected 且 pending.ThreadID == ""（仅历史兼容兜底） --> kill headless + migration notice + G0 None
  -- /mode vscode --> kill headless + clear restore hint + G0 None + R0 Detached(M1 VSCode)
  -- /detach --> kill headless + G0 None + R0 Detached
  -- /newinstance / /killinstance / 历史 resume_headless_thread --> migration notice，状态不变
  -- Tick timeout --> kill headless + clear pending + detach if needed
```

daemon startup 的 normal resume 额外规则：

1. 触发点：
   1. daemon startup 后的 tick
   2. `hello`
   3. `threads.snapshot`
   4. `thread.discovered`
   5. `thread.focused`
   6. `disconnect`
2. 前置条件：
   1. surface 当前是 `normal` mode
   2. surface 当前没有显式 attach
   3. surface 当前没有 pending headless
   4. `surface resume state` 里仍有 `ResumeThreadID` 或 `ResumeWorkspaceKey`
3. 恢复优先级：
   1. exact visible thread 恢复
   2. workspace attach fallback
   3. 只有以上路径都没有恢复成功，才继续评估 headless restore hint
4. 若 daemon 启动后的首轮 `threads.refresh -> threads.snapshot` 还没走完，且 persisted target 里包含 `ResumeThreadID`：
   1. 保持 `R0 Detached`
   2. 静默等待
   3. 不降级到 workspace，也不给失败提示
5. exact visible thread 恢复成功时：
   1. 进入 `R2 AttachedPinned`
   2. 只发一条 “已恢复到之前会话” notice
   3. 清掉该 thread 的旧 replay，避免补历史噪音
   4. 若同时存在 headless restore hint，会一并清掉
6. visible thread 不可见但 workspace attach fallback 成功时：
   1. 进入 `R1 AttachedUnbound`
   2. 只发一条 “已先回到工作区” notice
   3. 明确提示后续 `/use` 或 `/new`
   4. 若同时存在 headless restore hint，会一并清掉
7. 若首轮 refresh 已完成，但 visible/workspace 路径仍无法恢复：
   1. 若不存在 headless restore hint：保持 `R0 Detached`，发一条恢复失败提示，并进入 daemon 内存态 backoff
   2. 若仍存在 headless restore hint：继续交给 headless auto-restore 链路；normal resume 自己不额外发失败提示

daemon startup 的 vscode resume 额外规则：

1. 触发点：
   1. daemon startup 后的 tick
   2. `hello`
   3. `threads.snapshot`
   4. `thread.discovered`
   5. `thread.focused`
   6. `disconnect`
2. 前置条件：
   1. surface 当前是 `vscode` mode
   2. surface 当前没有显式 attach
   3. surface 当前没有 pending headless
   4. `surface resume state` 里仍有 `ResumeInstanceID`
3. 恢复规则：
   1. 只认 exact `ResumeInstanceID`
   2. unrelated instance 的 `hello` 不会触发 attach
   3. 不做 workspace fallback，也不走 headless
   4. 若仍有 stale headless restore hint，会先主动清理
4. exact instance 当前在线且可接管时：
   1. 复用现有 vscode attach/follow-local 路径
   2. 若已有可跟随焦点，则进入 `R4 FollowBound`
   3. 若还没有新的 VS Code 活动，则进入 `R3 FollowWaiting`
   4. 只发一条“已恢复到 VS Code 实例”的 notice，并明确提示去 VS Code 再说一句话或手动 `/use`
5. 若 exact instance 还没连回：
   1. 保持 `R0 Detached`
   2. 静默等待
   3. 不给失败提示
6. 若 exact instance 当前已被其他飞书 surface 接管：
   1. 保持 `R0 Detached`
   2. 发一条恢复失败提示
   3. 进入 daemon 内存态 backoff

后台 auto-restore 额外规则：

1. 触发点：
   1. daemon startup 后的 tick
   2. `hello`
   3. `threads.snapshot`
   4. `thread.discovered`
   5. `thread.focused`
2. daemon startup 时会先根据 `surface resume state` materialize latent detached surface，并恢复 `ProductMode`；headless restore hint 只负责后续的 headless auto-restore 资格与目标。
3. 后台恢复前置条件：
   1. surface 当前是 `normal` mode
   2. surface 当前没有显式 attach
   3. surface 当前没有 pending headless
   4. restore hint 仍存在
4. 解析顺序：
   1. 先看当前 merged thread view
   2. 若 thread 不可见但 hint 仍有 `threadID + threadCWD`，允许构造 synthetic view
   3. 之后只允许落到 headless 目标，不会自动 attach 到 VS Code
5. 若 surface 当前是 `vscode` mode，后台恢复会直接跳过，不会 attach 现有 headless，也不会启动新的 headless。
6. 若 daemon 启动后的首轮 `threads.refresh -> threads.snapshot` 还没走完，且当前又无法从 visible/synthetic view 判定恢复目标：
   1. 保持 `R0 Detached`
   2. 静默等待
   3. 不给用户失败提示
7. 若首轮 refresh 已完成，目标 thread 仍不可判定：
   1. 保持 `R0 Detached`
   2. 发一条 “暂时无法找到之前会话” 的恢复失败提示
   3. 进入 daemon 内存态 backoff，避免重复重试噪音
8. 后台恢复成功 attach 时：
   1. 不补发 thread replay
   2. 不补 thread selection changed 卡片
   3. 只发一条恢复成功 notice
9. headless launch 失败或超时时：
   1. 清掉 pending
   2. 保持 `R0 Detached`
   3. 发恢复失败提示
   4. 进入 backoff

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

### 5.6 transport degraded / reconnect / hard disconnect

```text
R1/R2/R3/R4/R5 + inst.Online=true
  -- ApplyInstanceTransportDegraded --> 保持当前 route state + inst.Online=false

transport degraded retained attachment
  -- 当前 active item 若在 dispatching/running 且已有 remote binding --> 保留 active item 与 remote ownership
  -- 当前 active item 若尚未绑定可恢复 turn --> fail 当前 active item
  -- queued items --> 保留 queued
  -- /stop --> 仅提示实例离线，暂时无法发送 interrupt
  -- /detach --> 立即 finalize 到 R0 Detached
  -- reconnect(ApplyInstanceConnected) --> 继续当前 route state，但不会抢先 dispatch queued work
  -- preserved turn.completed --> 再继续 dispatchNext / reevaluateFollow
  -- hard disconnect(ApplyInstanceDisconnected / RemoveInstance) --> R0 Detached
```

补充说明：

1. `transport_degraded` 和真正离线不是同一路径。
2. degraded 会保留：
   1. `AttachedInstanceID`
   2. `SelectedThreadID`
   3. queued work
   4. 已进入 `dispatching/running` 且有真实 remote binding 的 active queue item
   5. 该 preserved turn 对应的 remote ownership 与 turn artifacts
3. degraded 会清掉：
   1. 当前 active turn 归属
   2. request prompt / request capture
   3. surface-level prompt override
4. degraded 不再把“链路过载/等待恢复”直接翻译成“当前执行已中断”。若 active turn 已经发出且可相关到 remote binding，当前 turn 仍可能继续执行，只是实时输出可能延迟或丢失。
5. 因为 attachment 仍在，所以 `/status` 必须明确显示“实例离线但接管关系保留”；同时 retained-offline 状态下必须保留显式逃生口：
   1. `/detach` 立即生效，不进入 `Abandoning`
   2. `/stop` 只返回 `stop_instance_offline` 提示，不伪造已发送 interrupt
6. reconnect 只恢复实例在线和 follow 评估，不会因为 queued item 还在就抢先重派；必须等 preserved turn 自己 `completed/failed` 后，后续 queued work 才会继续出队。
7. 如果该 surface 同时还保留 headless restore hint，hard disconnect 回到 `R0 Detached` 后会重新进入上面的后台 auto-restore 判定。
8. daemon graceful shutdown 也不是 `transport_degraded`。当前实现会在真正停掉 Feishu gateway 前，对内存里已知的 surface best-effort 广播单条 `daemon_shutting_down` notice；如果某个 surface 或 gateway 发送失败，只记录日志，不阻塞最终退出。

## 6. 命令矩阵

### 6.1 基础路由态

| 命令 | `R0 Detached` | `R1 AttachedUnbound` | `R2 AttachedPinned` | `R3 FollowWaiting` | `R4 FollowBound` | `R5 NewThreadReady` |
| --- | --- | --- | --- | --- | --- | --- |
| `/list` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/newinstance` | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 |
| `/new` | 拒绝 | `normal`: 允许；`vscode`: 拒绝 | `normal`: 允许；`vscode`: 拒绝 | 拒绝 | 拒绝 | 允许；若首条消息已 dispatching/running 则拒绝 |
| `/killinstance` | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 | 兼容提示，不改状态 |
| `/use` `/useall` | `normal`: `/use`=`/useall`，都会展示 global 全量会话；`vscode`: 拒绝并提示先 `/list` | `normal`: `/use`=当前 workspace 最近 5 个，`/useall`=global 全量，卡片 `show_scoped_threads`=当前 workspace 全量；`vscode`: `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | `normal`: `/use`=当前 workspace 最近 5 个，`/useall`=global 全量，卡片 `show_scoped_threads`=当前 workspace 全量；`vscode`: `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | 允许；若仅有 unsent draft 会先丢弃 |
| `/follow` | `normal`: 拒绝并提示迁移；`vscode`: 拒绝并提示先 `/list` | `normal`: 拒绝并提示迁移；`vscode`: 允许 | `normal`: 拒绝并提示迁移；`vscode`: 允许 | 允许 | 允许 | 拒绝并提示迁移 |
| `/mode` | 允许 | 允许；若有 queued/dispatching/running 则拒绝 | 允许；若有 queued/dispatching/running 则拒绝 | 允许；若有 queued/dispatching/running 则拒绝 | 允许；若有 queued/dispatching/running 则拒绝 | 允许；若有 queued/dispatching/running 则拒绝 |
| `/autocontinue` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/help` `/menu` `/debug` `/upgrade` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| 文本 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 允许首条；首条 queued/dispatching/running 后拒绝第二条 |
| 图片 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 仅在首条文本尚未入队前允许 |
| 请求按钮 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 理论上通常不会出现；若出现仍按 attached surface 处理 |
| `/stop` | 通常无效果 | 通常无效果 | 允许 | 允许 | 允许 | 允许；可清掉 staged/queued draft |
| `/status` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/detach` | 允许但通常只提示已 detached | 允许 | 允许 | 允许 | 允许 | 允许；dispatching/running 时走 abandoning |
| bare `/mode` / bare `/autocontinue` | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 |
| bare `/model` `/reasoning` `/access` | 允许，但 detached 时只回恢复/参数卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 |
| bare `/debug` `/upgrade` | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 |
| 带参数 `/model` `/reasoning` `/access` | 拒绝 | 允许 | 允许 | 允许 | 允许 | 允许 |

### 6.2 覆盖门禁

| 覆盖状态 | 当前行为 |
| --- | --- |
| `G1 PendingHeadlessStarting` | 只允许 `/status`、`/autocontinue`、`/mode`、`/detach`、旧 `/newinstance` / 旧 `/killinstance` / 历史 `resume_headless_thread` 兼容提示、revoke/reaction；其中 `/mode vscode` 会直接 kill 当前恢复流程并清空 headless restore 语义；reaction 即使放行到 action 层，也只会在满足 steering 条件时生效 |
| `G2 PendingRequest` | 普通文本、图片、`/new` 被挡；`/use`、`/follow`、follow 自动重绑定只要会改路由也都会被冻结；`/mode` 允许，并会把 request gate 一并清掉；用户也可以先处理请求卡片 |
| `G3 RequestCapture` | 下一条文本优先被当成反馈；图片、`/new`、`/use`、`/follow`、follow 自动重绑定只要会改路由也都会被 request-capture gate 冻住；`/mode` 允许，并会把 capture gate 一并清掉 |
| `G4 CommandCapture` | 当前只可能来自旧 runtime 残留兼容态；下一条普通文本会被直接转换成 `/model <输入>` 并立即应用；图片会被拒绝；新的 slash command 或卡片动作会直接清掉这次 capture；超时后会发 `command_capture_expired` 并提示重新打开 `/model` 卡片 |
| `E6 Abandoning` | 只允许 `/status`、`/autocontinue`；再次 `/detach` 只回 `detach_pending`；`/mode` 与其余动作统一拒绝 |
| `G6 VSCodeCompatibilityBlocked` | 只影响 daemon 的 detached-vscode 恢复路径：exact-instance auto-resume 与普通 open-vscode prompt 会被抑制，改发迁移/修复卡片；surface 侧 `/list`、`/mode`、`/status` 等动作仍按 route matrix 正常处理 |

retained-offline overlay 额外规则：

1. 条件：`Attachment.InstanceID != ""` 且 `Dispatch.InstanceOnline=false`。
2. 当前若保留了 active running/dispatching item，`/stop` 只返回恢复中提示，不会发送 interrupt。
3. `/detach` 直接 finalize，不进入 `E6 Abandoning`。
4. `/status` 必须把“attachment 仍保留”和“实例当前离线”同时投影出来。

## 7. UI 动作协议

当前 Feishu 卡片动作与服务端 action 对应关系如下：

| 卡片动作 | 服务端 action | 说明 |
| --- | --- | --- |
| `attach_workspace` | `ActionAttachWorkspace` | normal mode `/list` 的 workspace attach/switch 入口 |
| `attach_instance` | `ActionAttachInstance` | 直达 attach |
| `use_thread` | `ActionUseThread` | 直达 thread 切换 |
| `resume_headless_thread` | `ActionRemovedCommand` | 历史兼容入口，统一回迁移提示 |
| `kick_thread_confirm` | `ActionConfirmKickThread` | 强踢前再次校验实时状态 |
| `kick_thread_cancel` | `ActionCancelKickThread` | 仅回 notice |
| `prompt_select` | `ActionSelectPrompt` | 旧兼容入口，统一回 `selection_expired` |
| `run_command(/vscode-migrate)` | `ActionVSCodeMigrate` | 仅由 daemon 发出的 VS Code 迁移/修复卡片使用，点击后走本机 managed-shim 迁移链路 |

菜单与文本命令里新增：

1. `/new`
2. 菜单 `new`

二者都直接映射到 `ActionNewThread`。

同时，文本命令里新增：

1. `/mode`
2. `/mode normal`
3. `/mode vscode`

三者都映射到 `ActionModeCommand`，由服务端在当前 surface 上解释并决定是否执行切换。

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
12. **cross-instance `/use` 会绕过 detach 语义，保留旧 request/capture/override**：已修复。现在只有 normal-mode detached/global `/use` 还会做这类 resolver attach；切换前会先走 detach 风格清理与门禁。
13. **旧 `/newinstance` 手工 headless 选择分支仍能把用户带进过时状态**：已修复。当前只保留 thread-first `/use` 的 preselected headless；旧命令和旧 `resume_headless_thread` 卡片统一回迁移提示。
14. **same-instance `/use` / `/follow` / auto-follow 会在旧 request gate 还活着时静默改路由**：已修复。现在只要 request gate 仍在，所有会改路由的动作都会被冻结，包括 `follow_local` 下手动 force-pick 后再 `/follow` 的回切。
15. **attached vscode `/use` 会误走全局 merged thread view，甚至跨 instance retarget**：已修复。现在 detached vscode 必须先 `/list`，attached vscode 只允许当前 instance 已知 thread，且 one-shot force-pick 仍保持 `follow_local`。
16. **cross-instance attach 到复用/新建 headless 时会丢 thread replay**：已修复。当前 replay 会先按 `threadID` 全局迁移，再在目标 attach 上一次性补发。
17. **transport degraded 后既误报“已中断当前执行”，又缺少 retained-offline 逃生口**：已修复。当前会保留可相关的 in-flight turn，不再伪造“已中断”；同时 `/status`、`/stop`、`/detach` 都已显式区分 retained-offline 与真正 detach。
18. **queued 点赞升级成 steering 后 item 会脱离普通 queue，若 ack 失败则可能丢失**：已修复。当前已强制恢复原 queue 位置，并补失败 notice。
19. **headless 自动恢复在首轮 refresh 前过早报失败，或恢复成功后重放旧 replay / 额外补 attached 噪音**：已修复。当前会先静默等待首轮 refresh，恢复成功只发单条成功 notice，且会清空旧 replay 而不是补发。
20. **`PendingHeadless` 只能靠隐藏的 `/killinstance` 逃生**：已修复。当前 `/detach` 可以直接取消恢复流程并回到 `R0 Detached`；旧 `/killinstance` 只回迁移提示。
21. **显式切 mode 会保留旧 attachment / request gate / draft 残留，导致进入半切换状态**：已修复。当前 idle/detached 时 `/mode` 会先做 detach-like 清理；busy path 则明确拒绝并提示 `/stop` 或 `/detach`。
22. **normal mode 仍然只按 instance/thread 仲裁，导致同 workspace 多 surface 并存**：已修复。当前 normal mode 的 attach/use/headless 恢复都会先经过 workspace claim；只有显式切到 `vscode` mode 才绕过这层仲裁。
23. **normal mode 还能长期停留在 follow 路径，导致 workspace-first 叙事失真**：已修复。当前新 `/follow` 会直接回迁移提示；历史 normal follow route 也会在读取 surface 时落回 pinned/unbound。
24. **旧版本残留的 `vscode + new_thread_ready` 会在升级后继续活着，等价绕回被设计移除的 `/new` 路径**：已修复。现在 surface 读取时会自动归一化回 `follow_local`，并尽量复用当前 observed focus。
25. **normal `/list` 仍按 instance root 聚合，broad headless pool 会把多个 thread `cwd` workspace 压成一个选项**：已修复。当前 workspace 列表先看可见 thread `CWD`，只有无可见 thread 时才回退到实例级 workspace metadata。
26. **normal detached / attach / disconnect 等路径仍向用户暴露“实例”措辞，导致 workspace-first 叙事不一致**：已修复。当前 normal mode 的 detached、attach、offline、degraded、stop-offline 等提示都统一回到工作区语义。
27. **切到 `vscode` 后仍可能保留 headless restore 入口，最终进入“`vscode` surface 底层实际 attach/pending 的是 headless”半死状态**：已修复。当前 `/mode` 会清掉 pending headless 与 restore hint，且 `vscode` surface 会在 auto-restore 入口被硬拒绝。
28. **daemon 重启后 latent surface 会丢 mode，或根本无法在没有 headless hint 的情况下重新 materialize**：已修复。当前 startup 会先从 `surface resume state` 恢复 surface 路由与 `ProductMode`。
29. **normal mode daemon 重启后会静默掉回 detached、过早报失败，或与 headless restore 优先级互相打架**：已修复。当前恢复顺序已收敛为 exact visible thread > workspace fallback > headless restore；首轮 refresh 前会静默等待，成功后会清理 stale headless hint，失败路径也只保留单条 notice + backoff。
30. **vscode mode daemon 重启后只保留 mode、不恢复实例，或者恢复链路误走 headless**：已修复。当前会按 exact `ResumeInstanceID` 恢复到原 VS Code 实例，回到 follow-local 语义；若还没有新的 VS Code 活动，会明确提示去 VS Code 再说一句话或手动 `/use`，并且会主动清理 stale headless hint。
31. **vscode mode 进入或 daemon 重启恢复时，会在 legacy `settings.json` / stale managed shim 状态下继续尝试恢复，导致用户看起来进入了 vscode mode，但底层仍沿用旧接入方式或失效入口**：已修复。当前 detached-vscode 恢复会先做本机 VS Code 兼容性检查；命中旧 `settings.json` override 或 stale managed shim 时，会保持 detached，发迁移/修复卡片，并在点击后统一迁移到 managed shim，同时清掉旧 `chatgpt.cliExecutable`。

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

当前无。

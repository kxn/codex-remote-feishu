# Feishu Group On-Demand Resume Design

> Type: `draft`
> Updated: `2026-07-31`
> Summary: 记录 Feishu 群聊重启后不刷屏、但被 @ 时按群上下文恢复的产品边界；room workspace durable SSOT 已与单 surface 恢复目标拆开。

## 背景

当前 `surface resume state` 同时承担两类职责：

1. 持久化 surface 恢复合同：重启后记住私聊或群聊的 `ProductMode`、backend、thread、route 与该 surface 的恢复目标；群 workspace binding 由独立 room state 持久化。
2. daemon 后台自动恢复：重启后在没有用户交互时，主动尝试恢复这些 surface，并把恢复成功或失败通知发回对应飞书窗口。

这两个职责对私聊基本合理，但对群聊不合理。历史群 surface 数量可能很多，daemon 启动或升级重启后不应该把所有历史群都当成当前前台窗口去自动拉起 headless、自动恢复、自动失败通知。

## 产品目标

目标行为：

1. 私聊保持现状：daemon 重启后可以后台自动恢复，并可在私聊里提示恢复成功或失败。
2. 群聊不做无人触发的后台恢复：daemon 启动、升级重启、tick 不主动给历史群拉起实例，也不主动向群里发恢复成功或失败。
3. 群聊保留上下文：群 surface 的 resume entry 继续持久化单 surface 恢复目标，room state v2 独立持久化并优先 materialize 群 workspace binding。
4. 群聊被 @ 时按需恢复：用户在群里 @ 对应机器人并发送消息时，如果该群 surface 有可恢复的 workspace/thread，应在这次用户触发的入站动作里尝试恢复或启动。
5. 群聊主动恢复失败时只回复当前交互：失败提示可以发到群里，因为这是用户刚刚 @ 触发的结果，不是后台噪声。
6. 升级/重启生命周期通知只发私聊：群聊不接收“服务正在关闭/恢复”这类全局生命周期广播。

## 目标体验

daemon 重启后，历史群里不会出现恢复失败刷屏。

用户在群里 @ 机器人发送普通文本时：

1. 如果原 workspace 可恢复且没有被其他私聊或其他群占用，则启动或接管对应 backend，然后把这条消息排队发送。
2. 如果原 thread 可恢复，则优先恢复原 thread。
3. 如果原 thread 不可见但 workspace 可恢复，则回到该 workspace，并按当前 headless 语义新建或选择会话。
4. 如果 workspace 不存在、provider/profile 不可用、backend runtime 缺失，则对这次 @ 返回明确失败原因。
5. 如果同一个群内另一个机器人正在处理同 workspace，则沿用 room `ActiveLock`，提示等待，不并发启动两个实例。

用户没有 @ 时：

1. 不 materialize 新动作。
2. 不触发恢复。
3. 不发失败提示。

## 实现方向

### 0. 执行前基线与后续更新

以下 1-8 记录的是 `2026-07-26` 执行 A/B/C 前的 live code 基线，用于解释当时为何拆分，不代表当前实现仍缺这些能力：

1. `#729` 已完成 room context 主体：同群多 bot 共享 room workspace，不共享 thread / queue / staged input；workspace claim owner 已支持 `surface` / `room`；room active lock 已覆盖普通 dispatch、AutoWhip、AutoContinue。
2. `#728` 已完成 Feishu 群聊 @ 当前 bot gate：`internal/adapter/feishu/gateway/support.go:groupMessageMentionGateReason` 会在 gateway 入站层忽略未 @ 当前 bot 的群消息；daemon 不需要重新判断 mention。
3. `#738` 已完成后台恢复失败 notice 投递前 gate：`PendingHeadless.AutoRestore` timeout 与 connect/attach failure 不再绕过 recovery notice accounting。
4. `surface resume state` 仍同时承担“持久化上下文”和“后台恢复队列来源”：`configureSurfaceResumeStateLocked()` 会 materialize 所有 entry，`syncSurfaceResumeRecoveryStateLocked()` 会把所有 `surfaceResumeEntryNeedsRecovery(entry)` 的 entry 放入 `surfaceResumeRuntime.recovery`。
5. `runSurfaceRecoveryPipelineLocked()` 当前没有 surface 类型过滤；`onTick()`、`onDisconnect()` 和部分 mode 切换后恢复都会按 options 跑 `maybeRecoverHeadlessSurfacesLocked()` / `maybeRecoverVSCodeSurfacesLocked()`。
6. `beginShutdownNotices()` 当前遍历 `service.Surfaces()` 给所有 surface 发 shutdown notice；Feishu 群聊 surface 会被包含进去。
7. `TryAutoResumeHeadlessSurface()` 已经是可复用的恢复核心：它能按 persisted target attach visible instance、reuse/restart managed headless、fresh start headless 或返回 failed/waiting。
8. `applyIngressActionLocked()` 当前在 `ActionTextMessage` 进入 orchestrator 前会先 `prepareInboundTextFilesActionLocked()`；这个文件暂存路径依赖已 attached workspace。群聊 lazy recovery 的 V1 主链不要把“首条 @ 同时带文件并自动暂存”放进同一个阶段。
9. `#751` 已把 room durable state 从 primary-only 演进为 v2：原 `feishu-room-primary.json` 路径原位保存 workspace/reset/primary facts；旧 surface resume 只在候选一致时一次性补录缺失 room workspace，冲突时在统一 ingress 前 fail closed。

当前 live code 已有 `surfaceResumeEntryAllowsBackgroundRecovery`、`surfaceAllowsDaemonLifecycleNotice` 和 `maybeStartFeishuGroupOnDemandResumeLocked`，A/B/C 已完成；下文计划保留为实现与回归索引。执行前结论是：不重新实现 room context，也不新造恢复解析，改动集中在两个 SSOT：

1. `surfaceRecoveryPolicy`：同一个地方判断 surface resume entry 是否允许 background recovery、是否允许 on-demand recovery、是否允许 daemon lifecycle notice。
2. `onDemandHeadlessResume`：同一个地方把“群聊被 @ 的当前 action”转成恢复尝试和恢复后的 continuation。

### 1. 拆分 resume entry 的两个职责

保留 `surface resume state` 作为单 surface backend/thread 恢复合同的 SSOT，但不要直接把所有 entry 都放进同一个 background recovery queue。群 workspace binding 的 durable SSOT 是 room state，surface resume 不得覆盖已有 room workspace。

必须拆出三个判定：

1. `entryAllowsBackgroundRecovery(entry)`：只允许私聊和非 Feishu legacy surface 进入 daemon 启动/tick 后台恢复队列。
2. `entryAllowsOnDemandRecovery(entry, action)`：允许 Feishu 群聊在被当前 bot @ 的入站动作里恢复。
3. `surfaceAllowsDaemonLifecycleNotice(surface)`：只允许 Feishu 私聊和非 Feishu surface 收 shutdown / restored 这类 daemon lifecycle notice。

群聊 entry 不应被删除，也不应丢失 resume target。

建议实现位置：

1. 新增 `internal/app/daemon/app_surface_resume_policy.go`，集中放 surface/resume policy helper，避免在 tick、shutdown、on-demand 三处各自写 `strings.Contains(":chat:")` 之类判断。
2. policy 判定优先基于 Feishu surface id 的结构化解析能力；没有现成解析 helper 时，新增 daemon-local helper 并用测试锁定：
   - `feishu:<gatewayID>:chat:<chatID>` 是 Feishu 群聊 surface。
   - `feishu:<gatewayID>:user:<openID>` 是 Feishu 私聊 surface。
   - 非 Feishu 或解析失败 surface 保持当前 background 行为，避免误伤非 Feishu surface。
3. `syncSurfaceResumeRecoveryStateLocked()` 应只把 `entryAllowsBackgroundRecovery(entry)` 为真的 entry 放进 `surfaceResumeRuntime.recovery`。群聊 entry 继续 materialize 成 latent surface，但不进入 background recovery map。
4. `maybePromptDetachedVSCodeSurfacesLocked()` 也要使用同一 policy；Feishu 群聊不应在 daemon 启动后收到 VS Code open prompt。

### 2. 增加群聊 on-demand resume 入口

位置应在 daemon 入站动作进入 `service.ApplySurfaceAction` 前后都可以，但要满足两个条件：

1. 在普通 `handleText` 返回 `not_attached` 之前完成恢复意图处理。
2. 恢复过程必须保留当前用户消息，恢复成功后这条消息要继续进入 queue，而不是要求用户再发一遍。

推荐路径：

1. daemon 收到 Feishu 群聊 @ 文本后，确认 action 已通过现有 mention gate。
2. 如果 surface 当前 detached 且 persisted resume entry 有 workspace/thread target，则进入 on-demand recovery。
3. 复用现有 `TryAutoResumeHeadlessSurface` / workspace continuation 解析能力，但以“当前 action 触发”为上下文。
4. 若恢复需要启动 headless，则创建 `PendingHeadless`，把当前消息作为等待恢复后的 continuation queue item 或 pending input。
5. 若恢复能立即 attach visible/managed instance，则继续正常 `ApplySurfaceAction` 处理当前文本。

V1 推荐实现边界：

1. 只对 Feishu 群聊 `ActionTextMessage` 做 on-demand recovery，且要求 action 已通过 gateway mention gate。`ActionImageMessage` 可以作为第二阶段接入；`ActionFileMessage` 和带 `Files` 的文本不进入第一阶段。
2. 对不进入 V1 on-demand 的文件类动作，如果 surface detached 且有可恢复 entry，返回一次明确 notice：当前群上下文需要先恢复，请先发送一条普通文本 @ 机器人；不要静默丢文件。
3. on-demand recovery 只处理 headless backend。VS Code surface 的群聊 lazy recovery 第一版不做自动 attach；如果群 surface 是 VS Code mode 且 detached，回复让用户在群内发送 `/mode codex` 或私聊处理 VS Code 连接，避免在群里恢复本地桌面实例语义。
4. 当前 action 的 continuation 不应存在 Feishu gateway lane 里；应在 daemon/orchestrator surface runtime 中保存，因为恢复完成事件来自 relay hello / tick，不会回到 gateway lane。
5. Pending continuation 至少要保存：
   - `Action` 的 clone，包括 `GatewayID`、`SurfaceSessionID`、`ChatID`、`ActorUserID`、`MessageID`、`TargetMessageID`、`Text`、`Inputs`、`SteerInputs`、`Inbound`。
   - 触发时间和过期时间。建议过期时间与 `PendingHeadless.ExpiresAt` 对齐。
   - continuation kind，例如 `group_on_demand_text_dispatch`，避免和 `prompt_dispatch_restart` 混淆。
6. `PendingHeadless` 已成功 attach 后，由 `ApplyInstanceConnected()` 产生的 attach 成功事件或 daemon 后续 sync 点触发 replay：先清 continuation，再把 clone 的 action 重新走统一 locked ingress episode，重新经过 rejected/room/upgrade/turn-patch 等动态 gate；replay 入口必须跳过再次 on-demand recovery，避免循环。
7. 如果 `PendingHeadless` 超时、启动失败、connect attach failure，清掉 continuation，并只对当前消息所在群回复一次恢复失败 notice。这个 notice 属于用户触发反馈，不走 background lifecycle fanout。
8. 如果 on-demand 恢复发现 workspace/thread busy，复用现有 failure notice，但本次失败仍应调用 `recordSurfaceResumeFailureLocked()` 写 backoff；同一用户连续 @ 的间隔内可以直接返回“还在恢复/稍后重试”，不要每条消息都重新拉 headless。

### 3. 生命周期通知目标收口

`beginShutdownNotices()` 当前遍历 `service.Surfaces()`，这个集合包含持久化 materialized 的历史群 surface，不等价于“需要系统通知的窗口”。

应新增单一策略函数，例如 `daemonLifecycleNoticeAllowedForSurface(surface)`，规则：

1. Feishu 私聊允许。
2. Feishu 群聊不允许。
3. 非 Feishu surface 按现有行为保留，除非后续明确产品策略。

该策略只用于 daemon lifecycle notice，不用于用户主动命令结果。

### 4. 与 `#738` 的边界

`#738` 已经解决“后台恢复失败 notice 投递前统一 gate”。本设计不应该回滚或复制那套逻辑。

本设计继续要做的是：

1. 防止 Feishu 群聊进入 background recovery queue，从源头避免无人触发恢复。
2. 允许 Feishu 群聊在被 @ 时进入 on-demand recovery，并把用户当前消息接上 continuation。
3. 保留 `#738` gate 作为兜底：即使未来某条后台路径误产出 `headless_restore_*` notice，也不会在同一 episode 内重复刷。

不应该做的是：

1. 在 `handleUIEventsLocked()` 或 Feishu projector 层按群聊吞所有恢复失败 notice。这会误吞用户刚刚 @ 触发的恢复失败反馈。
2. 删除群聊 resume entry。那会破坏单 surface thread/lazy recovery 目标；room workspace binding 本身由 room state 独立持久化。
3. 把群聊 on-demand recovery 做成新的一套 workspace/thread 解析。应复用 `TryAutoResumeHeadlessSurface()` 和现有 workspace continuation。

## 当前事故分析

日志证据：

1. `2026-07-26 14:08:38` 出现 `headless kill requested: surface=feishu:Codex-5:chat:oc_1eb491c89f520efd0255b3717ed9e3a0 ...`。
2. 之后同一个群 surface 连续出现 `kind=notice`，间隔约 2-3 秒。
3. 这个 surface 是 Feishu 群聊 surface：`feishu:Codex-5:chat:oc_...`。

直接原因：

1. daemon 重启后从 `surface resume state` materialize 了历史群 surface。
2. 该群 surface 进入了后台恢复链路。
3. 后台恢复尝试拉起 headless 后，启动或恢复没有成功，进入 `PendingHeadless` 超时/失败路径。
4. `Tick()` 中的 `expirePendingHeadless` 发出 `headless_restore_start_timeout`，并触发 `DaemonCommandKillHeadless`。
5. 群聊被当作普通 surface 接收了这些恢复失败 notice，于是出现群里刷屏。

为什么之前“恢复失败原因和去重”没有挡住：

1. 之前的修正主要覆盖 `recordSurfaceResumeFailureLocked` 管理的 surface resume episode：即 `TryAutoResumeHeadlessSurface` 返回 `SurfaceResumeStatusFailed` 后，稳定失败根因、backoff、`LastNoticeCode` 这些状态不重复刷。
2. 这次日志里出现了 `headless kill requested`，对应的是已经进入 `PendingHeadless` 后的超时/kill 路径。
3. `expirePendingHeadless` 会直接生成 `headless_restore_start_timeout` notice；这不是单纯的 `NoticeForSurfaceResumeFailure` 出口。
4. 更关键的是，之前修正没有解决“群聊是否应该进入后台恢复队列”这个边界，所以即使某些失败能去重，群聊仍可能被 daemon 主动恢复并产生其它路径的系统通知。

结论：之前修正不是完全错误，但它解决的是“恢复 episode 内失败原因稳定和部分重复提示”，没有覆盖群聊产品边界，也没有把 `PendingHeadless` 超时 notice 统一纳入同一套后台恢复通知策略。因此这次行为说明修正范围不够深。

## 可开工拆分计划

本设计已达到执行闭包，建议拆成三个子 issue 顺序推进。不要单 issue 一次做完，因为三部分验收面不同：background policy、用户触发 continuation、生命周期通知。

### 子 issue A：收口 surface recovery policy SSOT

目标：群聊 resume entry 保留并 materialize，但不进入 background recovery / VS Code detached prompt。

涉及文件：

1. 新增 `internal/app/daemon/app_surface_resume_policy.go`。
2. 修改 `internal/app/daemon/app_surface_resume_state.go`：
   - `syncSurfaceResumeRecoveryStateLocked()`
   - `maybePromptDetachedVSCodeSurfacesLocked()`
3. 修改或新增测试：
   - `internal/app/daemon/surface_resume_state_test.go`
   - 可新增 `internal/app/daemon/surface_resume_policy_test.go`

TDD 步骤：

1. 先写失败测试：`TestFeishuGroupSurfaceResumeStateMaterializesButSkipsBackgroundRecovery`。
   - 构造 `surfaceresume.Entry{SurfaceSessionID:"feishu:app-1:chat:oc_room", ChatID:"oc_room", ProductMode:"normal", Backend:"codex", ResumeThreadID:"thread-1", ResumeWorkspaceKey:workspace, ResumeHeadless:true}`。
   - `newRestoreHintTestApp(stateDir)` 后断言 `SurfaceSnapshot(...) != nil`，但 `surfaceResumeRuntime.recovery` 不含该 surface。
   - 调用 `app.onTick(...)`，断言没有 gateway operation，也没有 `PendingHeadless`。
2. 再写私聊保持现状测试：`TestFeishuUserSurfaceResumeStateStillEntersBackgroundRecovery`。
   - 使用 `SurfaceSessionID:"feishu:app-1:user:ou_user"`。
   - 断言 recovery map 含该 surface；在 runtime 可用或 mock startHeadless 情况下仍能进入原恢复路径。
3. 再写 VS Code 群聊 prompt 测试：`TestFeishuGroupVSCodeResumeDoesNotEmitDetachedPromptOnStartup`。
   - 群 surface 为 `ProductMode:"vscode"` 且 `ResumeInstanceID` 非空。
   - 调用 `maybePromptDetachedVSCodeSurfacesLocked()`，期望没有 notice。
4. 实现 policy helper：
   - `surfaceResumeEntryAllowsBackgroundRecovery(entry surfaceresume.Entry) bool`
   - `surfaceAllowsDaemonLifecycleNotice(surface *state.SurfaceConsoleRecord) bool`
   - `surfaceResumeEntryIsFeishuGroup(entry surfaceresume.Entry) bool`
5. 把 `syncSurfaceResumeRecoveryStateLocked()` 的 `surfaceResumeEntryNeedsRecovery(entry)` 改成同时要求 `surfaceResumeEntryAllowsBackgroundRecovery(entry)`。
6. 把 `maybePromptDetachedVSCodeSurfacesLocked()` 对候选 entry 增加同一 policy gate。
7. 运行：
   - `go test ./internal/app/daemon -run 'TestFeishu(Group|User).*Resume|Test.*VSCode.*Prompt' -count=1`
   - `go test ./internal/app/daemon -count=1`

验收：

1. Feishu 群聊 entry 重启后仍 materialize。
2. Feishu 群聊 entry 不触发后台 headless start / VS Code prompt / 恢复失败 notice。
3. Feishu 私聊保持原 background recovery 行为。
4. 非 Feishu surface 不因 policy helper 误伤。

### 子 issue B：群聊 @ 文本 on-demand headless recovery + continuation

目标：群聊 detached 但有 persisted headless resume target 时，用户 @ bot 发普通文本，系统按需恢复并在恢复成功后继续处理这条消息。

涉及文件：

1. 新增 `internal/app/daemon/app_group_on_demand_resume.go`。
2. 修改 `internal/app/daemon/app_ingress.go`：
   - `handleAction()` 或 `applyIngressActionLocked()` 的入口顺序。
3. 修改 `internal/app/daemon/app_surface_resume_state.go`：
   - 复用 `recordSurfaceResumeFailureLocked()` / `gateUngatedManagedHeadlessResumeOutcomeEventsLocked()`。
4. 必要时修改 `internal/core/state/types.go`：
   - 如果 continuation 需要持久化到 orchestrator root，新增明确 runtime record。
   - 如果只存在 daemon 内存，可先放 `App.surfaceResumeRuntime`，但要测试 daemon 生命周期内完整闭环。
5. 修改或新增测试：
   - `internal/app/daemon/surface_resume_group_on_demand_test.go`
   - 必要时补 `internal/core/orchestrator/service_surface_thread_selection_test.go`

推荐实现形态：

1. `maybeStartFeishuGroupOnDemandResumeLocked(ctx, action) (events []eventcontract.Event, handled bool)`：
   - 只接受 `ActionTextMessage`。
   - surface 必须是 Feishu 群聊。
   - surface 当前无 attached instance、无 pending headless。
   - resume store 中对应 entry 必须存在，且 headless product mode、`ResumeHeadless=true` 或有 `ResumeWorkspaceKey`。
   - action 不能带 `Files`；带文件时返回 user-facing notice 并 `handled=true`。
2. 调用 `service.TryAutoResumeHeadlessSurface(surfaceID, attempt, allowMissingTargetFailure=true)`。
3. 结果处理：
   - `ThreadAttached` / `WorkspaceAttached`：直接继续执行当前 action。
   - `Starting`：保存 continuation，投递恢复启动相关事件，当前 action 暂不进入 queue。
   - `Failed`：调用 `recordSurfaceResumeFailureLocked()`，投递一次当前交互失败 notice。
   - `Waiting` / `Skipped`：返回一条当前交互 notice，说明正在准备恢复或无法恢复。
4. `replayPendingGroupOnDemandActionLocked(surfaceID)`：
   - 在 `onHello()` 的 `ApplyInstanceConnected()` 后、`onTick()` 处理 pending timeout 后检查。
   - 如果 surface 已 attached 且 continuation 未过期，清 continuation 后调用统一 locked ingress episode helper。
   - replay 必须重新经过当前动态 ingress gate，并且不能再次写入相同 continuation。
5. 超时/失败清理：
   - `gateUngatedManagedHeadlessResumeOutcomeEventsLocked()` 看到该 surface 的 `headless_restore_*` failure 时，清 continuation。
   - `PendingHeadless` 消费后如果未 attached，也清 continuation。

TDD 步骤：

1. 失败测试 `TestFeishuGroupOnDemandTextStartsHeadlessAndDefersMessage`：
   - 群 entry 已持久化但未进入 background recovery。
   - 用户发 `ActionTextMessage`。
   - 期望 startHeadless 被调用，surface 进入 `PendingHeadless.AutoRestore=true` 或明确 on-demand purpose，当前消息没有立即 dispatch。
2. 失败测试 `TestFeishuGroupOnDemandTextReplaysAfterHeadlessConnect`：
   - 延续上一个 pending continuation。
   - 模拟 headless `Hello` / `ApplyInstanceConnected()`。
   - 期望 continuation 被清掉，原 `MessageID` 对应消息进入 queue/dispatch，并产生 typing reaction 或 dispatch command。
3. 失败测试 `TestFeishuGroupOnDemandTextFailureRepliesOnceAndClearsContinuation`：
   - 构造 workspace missing / provider unavailable。
   - 期望只发一条恢复失败 notice，continuation 清理。
4. 失败测试 `TestFeishuGroupOnDemandFileMessageDoesNotStartRecoveryV1`：
   - detached 群 surface + `ActionTextMessage.Files` 或 `ActionFileMessage`。
   - 期望返回“先发送普通文本恢复”的 notice，不 startHeadless，不丢 pending file。
5. 实现最小 helper 和 runtime record。
6. 运行：
   - `go test ./internal/app/daemon -run TestFeishuGroupOnDemand -count=1`
   - `go test ./internal/app/daemon ./internal/core/orchestrator -count=1`

验收：

1. 群聊 @ 普通文本能触发 lazy recovery。
2. 恢复成功后原消息自动继续，不要求用户再发一遍。
3. 恢复失败只回复当前群当前交互一次。
4. 文件类首条消息不进入半恢复半暂存状态。
5. 私聊不经过 on-demand gate。

### 子 issue C：daemon lifecycle notice 只发私聊

目标：升级/重启 shutdown/restored 这类系统生命周期通知不再广播到 Feishu 群聊。

涉及文件：

1. 修改 `internal/app/daemon/app_shutdown.go`：
   - `beginShutdownNotices()` 使用 `surfaceAllowsDaemonLifecycleNotice(surface)`。
2. 搜索并修正其它 lifecycle fanout：
   - `rg -n "GlobalRuntimeShutdownNotice|GlobalRuntime|恢复后|服务正在|service.Surfaces\\(\\)" internal/app/daemon`
   - 重点检查 upgrade owner card、startup restored notice、transport degraded notice 是否属于用户当前交互或全局 fanout。
3. 修改测试：
   - `internal/app/daemon/app_test.go`
   - `internal/app/daemon/app_upgrade_test.go`

TDD 步骤：

1. 修改现有 shutdown 测试，新增私聊 + 群聊两个 surface：
   - 私聊 `feishu:app-1:user:ou_user` 应收到 shutdown notice。
   - 群聊 `feishu:app-1:chat:oc_group` 不应收到 shutdown notice。
2. 保留非 Feishu surface 测试，确认不被误伤。
3. 如果发现 upgrade restored fanout 也遍历所有 surface，新增对应测试并套同一 policy。
4. 运行：
   - `go test ./internal/app/daemon -run 'TestDaemonShutdown|TestUpgrade' -count=1`
   - `go test ./internal/app/daemon -count=1`

验收：

1. daemon shutdown notice 不发群聊。
2. daemon 恢复/升级生命周期 fanout 不发群聊。
3. 用户主动在群里触发的命令结果、恢复失败、workspace 切换反馈仍能发群聊。

## 实现顺序建议

1. 先做子 issue A。它是最小 root-cause：群聊不进入 background recovery，能立即消除无人触发恢复噪声。
2. 再做子 issue C。它独立、风险小，可以快速消除升级/重启生命周期广播噪声。
3. 最后做子 issue B。它涉及 continuation 和 replay，状态机风险最大，需要单独验收。

如果只允许先做一小步，优先做 A，不要先在投递层吞群聊 notice。

## 状态机与文档同步要求

实现任一子 issue 后必须同步：

1. `docs/general/remote-surface-state-machine.md`：
   - startup materialization 后群聊 latent surface 不进入 background recovery。
   - 群聊 @ 文本可进入 on-demand recovery pending state。
   - on-demand recovery 的 success/failure/timeout 转移。
2. 如果 on-demand continuation 产生或修改 Feishu 卡片 callback / inline replace 行为，再同步 `docs/general/feishu-card-ui-state-machine.md`。当前计划 B 只处理文本入站，不应触发卡片状态机变化。
3. 继续保留本设计为 `draft`，直到 A/B/C 全部完成并回卷后再移动到 `implemented`。

## 已拍板决议

1. Feishu 群聊不做无人触发 background recovery。
2. Feishu 群聊被 @ 时可以 lazy recovery。
3. lazy recovery 成功后继续处理本次普通文本消息。
4. lazy recovery 失败时只反馈本次交互，不做后台广播。
5. daemon lifecycle notice 只发 Feishu 私聊，不发群聊。
6. V1 不支持群聊首条 @ 带文件的自动 lazy recovery；文件场景用明确提示收口，避免文件暂存和 pending continuation 打架。
7. V1 不做 VS Code 群聊 lazy recovery；群聊 lazy recovery 先限定 headless backend。

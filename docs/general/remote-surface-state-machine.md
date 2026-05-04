# Remote Surface 核心状态机

> Type: `general`
> Updated: `2026-05-04`
> Summary: 当前实现同步了 workspace-aware headless 主链与 vscode 主链，并把当前 live 的 backend-aware 可见命令面收口到新的投影：`codex` 继续以 `workspace` 命令族作为主展示壳，`claude` 当前 live 实现把工作会话主链显式收口到 `/new`、`/list`、`/use`、`/detach`，把 `常用工具` 收口到 `/history`；`send_settings` 则改成 backend 互斥入口：`codex headless` 可见 `/codexprovider`，`claude headless` 可见 `/claudeprofile`，`vscode` 两者都隐藏，且手动输入错误 backend 的命令也会显式拒绝。`/list` `/use` / target picker / workspace recency 全部只按当前 backend 过滤，且不再因为 surface/instance `ClaudeProfileID` 不同而隐藏 Claude workspace/session 候选。2026-05-01 的新变化是：headless attach/reuse/restart/create/reject 已进一步收口成单一路径，visible 与 compatibility 继续拆层，但所有 consumer 现在都共享同一个 `desired surface contract vs observed instance contract` 解析核。结果是：
> 1. visible 但 contract mismatch 的 workspace/session 仍然可见，不会再被 `/list`、`/use`、workspace recency、target picker 直接吞掉；
> 2. 这些 mismatch 候选不会再假装“可直接接管”；
> 3. detached `/use`、headless exact-thread restore、workspace attach、startup resume、`/mode` backend switch、`/claudeprofile`、`/codexprovider` 现在都会统一先判定 `attach visible compatible / reuse managed compatible / restart managed incompatible / fresh-start matching headless / reject`，而不是各自维护平行 continuation；
> 4. headless restore 不再把 visible VS Code 或 visible external mismatch 误当成 exact-thread auto-restore 目标；手动 `attach workspace` 也不再 silent 接管 profile/provider mismatch 的实例。
> 5. Claude managed headless 的 exact-thread restore 现在额外要求“目标 session 的 cwd 仍属于该 instance 当前 workspace”才允许原地复用；跨 workspace 旧 session 会改走 restart/fresh-start，不再把当前 attached Claude instance 的 metadata 直接 silent retarget 到别的目录。
> `claude <-> codex` 的 headless backend 互切现在统一锚定当前工作区目录：只要切换前已有当前 workspace，surface 就会保留这份 workspace claim，并优先 attach 目标 backend 下同 workspace 的兼容在线实例；若只剩 incompatible managed headless，则会 restart 成匹配合同；若没有兼容实例，则会 fresh-start matching managed headless，并保留原来的 unbound / new-thread-ready / exact-thread continuation 意图。进入 Claude workspace 时，surface 只会按 `workspace+profile` 快照恢复飞书临时 `reasoning` override；`access / plan` 不再写入或恢复这套快照，surface resume 也不跨 daemon 恢复 `PlanMode`，而 `/status` 只读投影当前 profile。2026-05-02 的新变化是：Claude headless 的 `/reasoning` 也正式并入 headless launch contract，queue item / auto-continue / review apply 都会冻结各自目标 reasoning；真正 dispatch 前统一比较 `desired launch contract` 与 wrapper hello 上报的 observed runtime contract，若不一致则进入 `PendingHeadless(Purpose=prompt_dispatch_restart)`，由 daemon 显式 `kill + start headless`，实例重新 attach 后再自动继续原 dispatch。`/access` 与 `/plan` 仍保留动态 permission-mode 通道，不被并入这条 restart-only 合同。Codex provider 切换也已并入同一条 surface 级 headless 重启主链，切换时会沿用与 Claude profile 相同的 request-gate / busy-gate / current-workspace continuation 规则；idle detached 只改当前 surface 的 provider，workspace 内切换则会直接重启或 fresh-start 当前工作区。其余能力仍保持此前基线：headless 的被动恢复入口（attach unbound、`selected_thread_lost`、`thread_claim_lost`）统一回到“锁定当前工作区”的 target picker，不再回退旧 scoped selection prompt；VS Code `/list` / `/use` / `/useall` 继续走结构化实例/线程卡，其中线程选择统一成当前实例内的 dropdown，并隐藏不可切换会话、改用 plain-text 提示说明。surface-level backend seam 也已正式落成真实状态：headless 下区分 `codex` / `claude` 两个 backend，workspace defaults、surface resume 与 detached catalog context 都按 backend 分区，`/mode` 的底层语义收口成 `codex|claude|vscode`，其中 `normal` 仍作为 `codex` 的兼容 alias。另一个新变化是把上游 runtime 问题自动继续从 `autowhip` 中拆成独立 `autocontinue` overlay：它由 orchestrator 本地 codex/gateway error-family policy 驱动，拥有自己的 queue lane、reply anchor、tail-only 状态卡与 backoff，不再和“正常结束后继续催活”混用；request gate 现在还补上了 `item/tool/call` 的最小 fail-closed 分支：relay / Feishu / headless 会展示只读 `tool_callback` 提示，并立即自动回写 unsupported 结构化结果，避免 tool 在中途 silent hang；同时 detached-branch 产品入口已经正式接上：普通文本里的 `[什么？]` / `[耸肩摊手]` 会分别触发 `fork_ephemeral` / `start_ephemeral`，统一复用 `keep_surface_selection`，且不会再让 detour turn 污染当前 surface 默认 thread；review mode 的 detached review session 也已接入同一条远端状态机：review thread 会带显式 `source=review` / parent-thread 元数据，surface 会在不改绑当前选中 thread 的前提下记录 `ReviewSession` runtime，并把后续审阅文本继续路由到 review thread；与此同时，普通 attach/list/use 候选现在会显式排除 `source=review` 会话，不再把 detached review thread 混进 merged thread list、current-instance dropdown 或 workspace recency。本轮还把 `process.child.restart` 收口成“两段式 restart 合同”：`ack` 只代表新 child 已接管，thread restore 结果改由独立 outcome event 回传，daemon 会对 `/bendtomywill` 与 standalone Codex upgrade 统一等待最终 outcome，因此 late restore 不会再把 patch / upgrade 误判成已失败或已完成。同一轮里，remote-surface 的 execution lifecycle 也已明确拆成两个 sibling seam：dispatch core 统一 owner `DispatchMode / ActiveQueueItemID / QueuedQueueItemIDs / pendingRemote / activeRemote`，recovery core 统一 owner `PendingHeadless / headless attach-fail-expire / disconnect-degraded-timeout teardown`；`prompt_dispatch_restart` 只保留显式 handshake，不再让 queue/recovery 在业务路径里平行散写同一批 carrier。注意：2026-04-29 新拍板的下一轮 Claude MVP 产品边界不再由本文定义，而改以 [Claude Backend Integration Plan](../inprogress/claude-backend-integration-plan.md) 第 `7.6` / `12.1` 节为准；本文仍只记录当前 live 实现。

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
5. [internal/core/orchestrator/service_surface_backend.go](../../internal/core/orchestrator/service_surface_backend.go)
5. [internal/core/orchestrator/service_test.go](../../internal/core/orchestrator/service_test.go)
6. [internal/core/state/types.go](../../internal/core/state/types.go)
7. [internal/core/state/surface_backend.go](../../internal/core/state/surface_backend.go)
8. [internal/core/state/workspace_defaults.go](../../internal/core/state/workspace_defaults.go)
7. [internal/core/control/types.go](../../internal/core/control/types.go)
8. [internal/core/control/feishu_commands.go](../../internal/core/control/feishu_commands.go)
9. [internal/core/orchestrator/service_autocontinue.go](../../internal/core/orchestrator/service_autocontinue.go)
10. [internal/core/orchestrator/service_claude_headless_preflight.go](../../internal/core/orchestrator/service_claude_headless_preflight.go)
11. [internal/core/orchestrator/service_headless_contract_switch.go](../../internal/core/orchestrator/service_headless_contract_switch.go)
12. [internal/core/orchestrator/service_queue.go](../../internal/core/orchestrator/service_queue.go)
13. [internal/core/orchestrator/service_review_actions.go](../../internal/core/orchestrator/service_review_actions.go)
14. [internal/core/orchestrator/service_surface_attach.go](../../internal/core/orchestrator/service_surface_attach.go)
15. [internal/core/orchestrator/service_overlay_runtime.go](../../internal/core/orchestrator/service_overlay_runtime.go)
16. [internal/core/orchestrator/service_recovery.go](../../internal/core/orchestrator/service_recovery.go)
16. [internal/codexstate/sqlite_threads.go](../../internal/codexstate/sqlite_threads.go)
17. [internal/adapter/feishu/gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go)
18. [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
19. [internal/adapter/feishu/gateway_runtime.go](../../internal/adapter/feishu/gateway_runtime.go)
20. [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
21. [internal/core/orchestrator/service_command_menu.go](../../internal/core/orchestrator/service_command_menu.go)
22. [internal/app/daemon/app_headless.go](../../internal/app/daemon/app_headless.go)
23. [internal/app/daemon/app_ingress.go](../../internal/app/daemon/app_ingress.go)
24. [internal/app/daemon/app_surface_resume_state.go](../../internal/app/daemon/app_surface_resume_state.go)
25. [internal/app/daemon/surface_resume_state.go](../../internal/app/daemon/surface_resume_state.go)
26. [internal/app/daemon/app_test.go](../../internal/app/daemon/app_test.go)
27. [internal/app/daemon/surface_resume_state_test.go](../../internal/app/daemon/surface_resume_state_test.go)
28. [internal/app/daemon/admin_vscode.go](../../internal/app/daemon/admin_vscode.go)
29. [internal/app/daemon/app_vscode_migration.go](../../internal/app/daemon/app_vscode_migration.go)
30. [internal/app/daemon/app_vscode_migration_test.go](../../internal/app/daemon/app_vscode_migration_test.go)
31. [internal/app/wrapper/app.go](../../internal/app/wrapper/app.go)
32. [internal/core/orchestrator/service_path_picker.go](../../internal/core/orchestrator/service_path_picker.go)
33. [internal/core/orchestrator/service_target_picker.go](../../internal/core/orchestrator/service_target_picker.go)
34. [internal/core/orchestrator/service_ui_runtime.go](../../internal/core/orchestrator/service_ui_runtime.go)
35. [internal/core/control/feishu_target_picker.go](../../internal/core/control/feishu_target_picker.go)
36. [internal/core/orchestrator/service_target_picker_git_import.go](../../internal/core/orchestrator/service_target_picker_git_import.go)
37. [internal/app/daemon/app_git_workspace_import.go](../../internal/app/daemon/app_git_workspace_import.go)
38. [internal/app/gitworkspace/import.go](../../internal/app/gitworkspace/import.go)
39. [internal/app/daemon/app_turn_patch.go](../../internal/app/daemon/app_turn_patch.go)
40. [internal/app/daemon/app_turn_patch_tx.go](../../internal/app/daemon/app_turn_patch_tx.go)
41. [internal/app/daemon/app_turn_patch_view.go](../../internal/app/daemon/app_turn_patch_view.go)
36. [internal/app/daemon/turnpatchruntime/model.go](../../internal/app/daemon/turnpatchruntime/model.go)
37. [internal/codexstate/turn_patch_storage.go](../../internal/codexstate/turn_patch_storage.go)
38. [internal/codexstate/turn_patch_ledger.go](../../internal/codexstate/turn_patch_ledger.go)
39. [internal/app/daemon/app_codex_upgrade.go](../../internal/app/daemon/app_codex_upgrade.go)
40. [internal/app/daemon/app_child_restart_wait.go](../../internal/app/daemon/app_child_restart_wait.go)
41. [internal/app/wrapper/app_child_session.go](../../internal/app/wrapper/app_child_session.go)
42. [internal/app/wrapper/app_io.go](../../internal/app/wrapper/app_io.go)
43. [internal/adapter/codex/translator_observe_server.go](../../internal/adapter/codex/translator_observe_server.go)

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

### 2.3 飞书私聊 surface identity 当前依赖 preferred actor id

飞书 P2P surface 当前不是“任意 user id 字符串都可互换”，而是 gateway-aware 的：

1. surface id 形如 `feishu:<gatewayID>:user:<preferredActorId>`。
2. `preferredActorId` 当前优先级固定为：
   1. `open_id`
   2. `user_id`
   3. `union_id`
3. 文本消息、bot menu、reaction actor、卡片 callback operator 都必须遵守同一优先级。
4. 卡片 callback 还必须先尝试通过 `open_message_id -> 已记录的 surfaceSessionId` 回到原 surface；只有消息查不到时，才允许回退到 callback 自带 operator id 推导 surface。

这个规则是当前状态机正确性的前置条件之一。否则同一个飞书私聊用户可能被裂成两个 surface：

1. 一个 surface 已 attach workspace / thread。
2. 另一个 surface 仍是 detached。
3. 用户随后发送 `/detach`、`/use`、普通文本或继续点卡片时，会命中不同 surface，表现成“界面看起来已接管，但命令又说当前没有接管中的工作区”。

## 3. 当前状态机的五层结构与运行时 overlay

surface 不是单一枚举，而是五层正交状态叠加。

### 3.1 产品模式 / backend overlay

| 代号 | 条件 | 用户语义 |
| --- | --- | --- |
| `M0 HeadlessCodex` | `ProductMode=normal`，`Backend=codex` | headless 主链的 Codex 分支；也是新 surface 默认值。当前会开启 workspace claim 仲裁，并把已占用 workspace 投影到 `/status` |
| `M1 HeadlessClaude` | `ProductMode=normal`，`Backend=claude` | headless 主链的 Claude 分支。workspace defaults、surface resume 与 detached catalog context 都按 Claude backend 分区；surface 还会额外携带当前 `ClaudeProfileID`，并按 `workspace+profile` 只恢复飞书临时 `reasoning` override；`access / plan` 不再从这套快照恢复；不进入 VS Code 语义，但已经共享 headless exact-thread 恢复主链，并在需要时通过 Claude 原生 `--resume` 恢复旧 session |
| `M2 VSCode` | `ProductMode=vscode`，`Backend=codex` | VS Code 专属分支；只能显式 `/mode vscode` 进入。当前不参与 workspace claim，仍保留既有 instance/thread-first 路由语义 |

补充说明：

1. `ProductMode` 与 `Backend` 当前都是 surface 级字段；`/detach` 不会清掉它们。
2. `ProductMode` / `Backend` 当前已经进入 daemon 级 `surface resume state`：
   1. 进程内已有 surface 会保留它。
   2. daemon 重启后，startup 会先从 `surface resume state` materialize latent surface，并恢复之前的 `ProductMode` / `Backend`。
   3. `surface resume state` 当前不仅记录 `ProductMode` / `Backend` / `ClaudeProfileID` / `Verbosity` / instance / thread / workspace / route，还会记录 headless thread restore 所需的 thread title / thread cwd / `ResumeHeadless` 标记；它已经是唯一持久化恢复源，但不再持久化或恢复 `PlanMode`。这里的 `ResumeHeadless` 现在只代表“恢复一个 concrete headless thread”，不再复用来表示 `fresh workspace prepare`。旧 entry 缺失 `Backend` 时会 lazy 默认成 `codex`；若 backend 是 `claude` 且 entry 缺失 profile，则会 lazy 默认成内置 `default`；若旧 entry 带着非空 `ClaudeProfileID`，load/save canonicalization 会反向把 headless backend 纠正回 `claude`，避免把 Claude exact-thread 恢复目标误投到 Codex 路由；若旧 entry 误把 `pending fresh workspace` 写成 `ResumeHeadless=true + ResumeRouteMode=pinned + ResumeThreadID=\"\"`，load 时会自动迁回 workspace-owned `new_thread_ready` 语义。
   4. headless surface（当前 persisted token 仍是 `ProductMode=normal`）随后会按 persisted resume target 继续尝试恢复：
      1. 优先 exact visible thread 恢复。
      2. 只允许消费同 backend 的 visible instance / workspace；不会再把 `codex` 的 headless resume target 恢复到 `claude`，反之亦然。managed headless 的复用候选也会先按 backend 过滤，attach / resume 路径若发现 thread view 与目标实例 backend 不一致，会直接拒绝这次恢复绑定，而不是把错配状态继续写回 surface resume。
      3. visible 与 compatibility 当前明确拆层：visible mismatch 目标仍然保留在 `/list`、`/use`、workspace recency、target picker 的候选里，但 exact-thread restore、workspace resume、backend switch、profile/provider switch 不会再把它们当成“可直接接管”的目标。解析核会统一先判定 `attach compatible visible -> reuse compatible managed headless -> restart incompatible managed headless -> fresh-start matching headless -> reject`。其中 Claude managed headless 只有在目标 session `cwd` 仍属于该 instance 当前 workspace 时才允许走 `reuse managed`；若目标 session 已经跨到别的 workspace，则必须 restart/fresh-start，不能继续 silent retarget 当前 attached Claude instance。
      4. exact thread 当前不可见但仍存在同 backend 的 persisted thread/session metadata 时，headless resume、detached `/use` 与 managed headless exact-thread continuation 仍会继续消费这份 metadata，不再退回 Codex-only lookup。
      5. 若 persisted route 本身就是 workspace-owned 的 `ResumeRouteMode=unbound|new_thread_ready`，则会优先按同 backend workspace 继续恢复：已有同 workspace 兼容实例时直接回到 `R1 AttachedUnbound` 或 `R5 NewThreadReady`；若只剩 incompatible managed headless，则先 restart 成匹配合同；若还没有兼容实例，则会 fresh-start managed headless，并保留同一条 workspace route intent。
      6. 若 persisted target 是普通 pinned thread 且当前 visible thread 不可见，则允许降级回原 workspace 的 same-backend 续接语义：已有同 workspace 兼容实例时 attach 回 `R1 AttachedUnbound`；若只剩 incompatible managed headless，则 restart 成 matching headless；若还没有兼容实例，则会 fresh-start managed headless，并进入 `new_thread_ready`。
      7. 只有 `ResumeHeadless=true` 且 `ResumeThreadID != ""` 的 concrete managed-headless thread-restore 目标，visible exact-thread 路径没恢复成功时才会继续留在 exact-thread continuation，而不是降级进 workspace fallback。
      8. 这条 managed-headless exact-thread continuation 当前仍按 backend 生效：Codex 继续走 sqlite/persisted-thread + child-restore 语义；Claude 会把同 backend persisted session metadata 转成 launch-time `ResumeThreadID`，最终由 wrapper 用 `claude --resume <session_id>` 恢复旧 session。
      8. 若持久化目标里包含 `ResumeThreadID`，则在 daemon 启动后的首轮 `threads.refresh -> threads.snapshot` 完成前，会先保持 detached 并静默等待，避免过早降级或过早报失败。
      9. 若同时带着 `ResumeHeadless=true` 且 `ResumeInstanceID` 指向一个已连回的 visible instance，managed-headless exact-thread continuation 也会让出这一轮 startup refresh，先给 exact visible thread 恢复机会，避免刚收到 snapshot 前就抢先拉起新的 headless。
   5. `vscode` mode surface 会按 persisted `ResumeInstanceID` 继续尝试恢复：
      1. 先做本机 VS Code 兼容性检查：
         1. 若检测到旧版 `settings.json` override，或当前 managed shim 已失效，则保持 detached，并发迁移/修复卡片。
         2. 若兼容性检查通过，才继续 exact-instance 恢复。
      2. 只允许恢复到 exact VS Code instance，不做 workspace fallback。
      3. 恢复成功后直接回到现有 vscode attach/follow-local 路径。
      4. 若当前还没有新的 VS Code 活动可继续 follow，会保留 follow waiting，并明确提示用户去 VS Code 再说一句话或手动 `/use`。
      5. `vscode` 不会参与 managed-headless exact-thread continuation；`surface resume state` 在 load/write 时也会把所有非 headless entry（当前即 `ProductMode!=normal`）的 `ResumeHeadless` 强制归零，因此 `vscode` surface 不会保留可继续触发这条恢复分支的持久化目标。
3. `/mode` 当前只在没有 live remote work 的 surface 上执行切换：
   1. 接受 `normal|codex|claude|vscode` 四种字面值；其中 `normal` 是 `codex` 的兼容 alias。
   2. `codex = Backend=codex + ProductMode=normal`，`claude = Backend=claude + ProductMode=normal`，`vscode = Backend=codex + ProductMode=vscode`。
   1. 会先走 detach-like 清理。
   2. 清掉 attachment / workspace claim / thread claim、`PromptOverride`、`PendingRequest`、`RequestCapture`、`PreparedThread*`、staged image / staged file 与 queued draft。
   3. 如果切换前后都处于 headless 主链（当前 persisted token 仍是 `ProductMode=normal`），且 backend 发生了 `codex <-> claude` 变化，只要切换前已经存在当前 workspace，则 surface 会恢复这份 workspace claim，并立即优先 attach 目标 backend 下同 workspace 的在线 instance；若当前还没有可 attach 的目标 backend instance，则直接启动 fresh managed headless，并保留 `new_thread_ready` 意图，而不是停在 detached idle。Claude fresh headless 仍会显式走 `claude-app-server`，而不是复用 Codex `app-server` 入口再靠 env 猜 backend。
   4. 如果切换前后发生了 backend 或 `ProductMode` 变化，会清掉 `surface resume state` 里的旧 resume target，避免 `codex` / `claude` 之间串恢复。
   5. 如果当时还带着 `PendingHeadless`，会先显式 kill 当前 headless 启动流程，并清掉 `surface resume state` 里的 headless 恢复目标与内存恢复状态。
4. 若当前仍有 live remote work，则 `/mode` 直接拒绝，并明确提示用户 `/stop` 或 `/detach`。
5. `Abandoning` 仍是更高优先级 gate；但 `PendingHeadless` 不再阻塞 `/mode`，用户可以直接切到 `vscode` 终止恢复流程。
6. 当前工作会话命令已经按主运行面分流：
   1. `codex` 的主展示命令是 `workspace` 命令族：`/workspace` / `/workspace new` 负责父页导航，`/workspace list` / `/workspace new dir` / `/workspace new git` / `/workspace new worktree` 打开四张独立业务卡；旧 `/list` / `/use` / `/useall` 只是 alias。
   2. `claude` 的 visible MVP 继续复用同一套命令目录壳：工作会话主链显式开放 `/new`、`/list`、`/use`、`/detach`，`常用工具` 显示 `/history` 与 `/sendfile`；`/review` 与 `/bendtomywill` 当前已经退出 Claude 主展示面，回到 hidden + reject；`workspace*` 与 `/useall` 作为 hidden + allow 兼容入口继续复用同一工作区 / 会话壳。
   3. `vscode` 主链继续列在线 VS Code instance。
7. `Verbosity` 当前也是 surface 级偏好：
   1. `/verbose quiet|normal|verbose|chatty` 直接改当前 surface。
   2. `/detach` 不会清掉它。
   3. daemon 重启后，latent surface 会从 `surface resume state` 恢复之前的 `Verbosity`。
8. `PlanMode` 当前也是 surface 级偏好，但 headless 与 `vscode` 的下发语义不同：
   1. `/plan on|off` 直接改当前 surface，只影响后续新 turn；`/plan clear` 会清掉显式 plan 覆盖并把 surface 投影恢复成 `off`。
   2. 当前 running turn、已入队消息、当前 turn 的 `/steer` 与 reply auto-steer 都不受新设置追溯改写。
   3. headless 主链的 queue item 会在入队时冻结 `PlanMode`，dispatch `turn/start` 时再把它落到 `PromptOverrides.PlanMode -> collaborationMode.mode=plan/default`。
   4. `vscode` 主链属于 shared-authority：只有用户显式 `/plan on|off` 后才会冻结 `PlanMode`；若未设置或已 `/plan clear`，queue item 的 `FrozenPlanMode` 保持 empty，dispatch 时不下发 plan override，让 VS Code/backend 保持当前状态。
   5. `/detach`、`/new`、`/use`、`/mode normal|codex|claude|vscode` 不会顺手清掉当前内存里的 `PlanMode`；daemon 重启后，latent surface 不再从 `surface resume state` 恢复 `PlanMode`，旧持久化 entry 里的 `planMode` 会被忽略并在下一次保存时清理。
   6. 在 `claude` 模式下，`PlanMode` 也不进入 `workspace+profile` 快照：
      1. 进入某个 Claude workspace 时，只会按 `workspace + ClaudeProfileID` 恢复最近一次飞书临时 `ReasoningEffort` 覆盖。
      2. 离开该 workspace 或切走该 profile 前，只把当前临时 `ReasoningEffort` 覆盖写回独立的 `workspace+profile` 持久化 store。
      3. 若目标 `workspace+profile` 没有快照，则会恢复成空 override + `PlanMode=off`，不会沿用别的 workspace/profile 残留值。
      4. `Model`、`AccessMode` 与 `PlanMode` 明确不在这套快照里；Claude workspace/profile 恢复时会主动清掉这些临时运行态，不把它们当作可持久化热改能力。
   7. Claude `ExitPlanMode` 被批准后，本地 surface 不会在“用户点了批准”时立刻清掉 `PlanMode`；只有等到对应 `request.resolved(plan_confirmation + accept)` 真正到达，才会同步清理显式 plan override。`decline` / cancel / 过期都不会误清。
   8. 若某轮 turn 结束时缓存了 `item/plan/delta` 最终正文，surface 会在 final 落完后追加一张“提案计划”手动 handoff 卡；这张卡不是 request gate，不阻塞后续输入，但命中新的输入、route 变化、turn 变化或用户显式点击动作后都会 seal。
      对 `keep_surface_selection` 的 detached-branch turn，这张卡仍回原 surface，并按 source/main thread 判断是否 suppress，不会因为 execution thread 不同而被误吞。
   9. 点击提案计划卡的 `直接执行` / `清空上下文并执行`，会先把当前 surface 的 `PlanMode` 切回显式 `off`，再继续派发 follow-up turn；`取消` 只 seal 卡片，不改 route。
9. `PromptOverride` 当前承载飞书侧显式 model / reasoning / access requested override：
   1. headless 主链为了保持现有执行合同，queue item 仍会冻结最终 effective model / reasoning / access。
   2. Claude headless 的 runtime `permissionMode` 现在会通过标准 `config.observed(thread)` 回填 thread observed access/plan；`/status`、`/access`、`/plan` 和 headless prompt freeze 都读这条 observed state，而不是把它误持久成 workspace default。
   3. Claude headless 在没有飞书显式 `/access` override 时，下一条 prompt 的 base access 会优先跟随当前 thread observed access；旧的 Claude workspace default access 不再参与这条解析。
   4. `vscode` 主链只冻结飞书显式 requested override；observed cwd/thread config 仍可用于 `/status` / 参数卡展示，但不会在没有本地显式覆盖时被重新下发给 backend。
   5. Codex translator 收到 empty access override 时不会改写 `approvalPolicy` / `sandboxPolicy`；只有显式 `full` / `confirm` 才会下发对应权限策略。
10. headless workspace-first 主链当前已经完成这一轮产品收窄：
   1. bare `/workspace` 是工作会话父页，固定展示 `切换`、`从目录新建`、`从 GIT URL 新建`、`解除接管` 四个入口；bare `/workspace new` 是只含三条新建路径的子页。
   2. `/workspace list` 与 alias `/list` / `/use` / `/useall` / `show_workspace_threads` 都收敛到同一张 `切换工作会话` 卡。
   3. 这张切换卡直接落在“工作区 + 会话”同页：
      1. 工作区候选只出现真实 workspace，不再混入动作型来源项。
      2. 工作区 label 足够时只显示 label；只有 basename 冲突时，才额外补路径 meta 做消歧。
      3. 会话候选始终基于当前选中的 workspace 重新生成，且只包含既有会话，不再追加 `新建会话`。
      4. session 只会在 surface 已经绑定到同一 thread 时保守预填；detached / unbound 即使只剩一个候选也不会自动代填。
      5. confirm 既有会话时，会复用现有 `/use` / cross-workspace attach 语义；必要时会先统一经过 `resolveWorkspaceContract(...)` 与对应的 workspace continuation owner，再落到 attach / restart-managed / fresh-start 的单一路径。
   4. `/workspace new dir`、`/workspace new git` 与 `/workspace new worktree` 是三张独立业务卡：
      1. `从目录新建` 主卡会显示路径字段、`选择目录` 按钮与 `接入并继续` 主按钮；`target_picker_open_path_picker` 会把主卡 inline replace 成目录模式 path picker，confirm/cancel 后再返回主卡。
      2. `从目录新建` 在主卡上回填出有效目录后即可继续；若命中已知 workspace，会直接复用该工作区，并进入新会话待命，而不是把用户打回“切换”路径。
      3. `从 GIT URL 新建` 主卡会内联收集 `repo_url`、可选 `directory_name` 与父目录；父目录会在表单里和右侧的 `选择目录` 按钮同行显示，底部动作区则统一收口成带分隔线的横排按钮。这些草稿跟随同一个 active target picker runtime 保存，不会进入 `PendingRequest`。
      4. `从 GIT URL 新建` 的主卡 confirm 会直接下发 daemon-side `workspace.git_import` 命令；真正的 `git clone` 在 daemon 持锁外执行，不阻塞主锁。
      5. confirm 后 owner card 立即进入 processing：先显示“正在导入 Git 工作区”，clone 成功后若 flow 仍有效，则继续 patch 成“正在接入工作区”；success / failure / cancel 都封回同卡 terminal。
      6. clone / prepare 期间，surface 会进入 coarse-grained `target_picker` gate：普通输入与 competing route mutation 被拒绝，只保留 `/status`、reaction/recall 与同卡 `取消导入`。
      7. `取消导入` 会优先停止业务流，并对 clone / fresh-workspace prepare 做 best-effort 取消；若本地已留下目录残留，不自动清理，只在 terminal card 提醒用户按需手动处理。
      8. clone 成功但 flow stale 时，只保留本地目录并回 stale notice；后续接入失败时，则同卡显示失败 terminal，并明确目录已保留。
      9. 当本机缺少 `git` 时，`从 GIT URL 新建` 仍可直接打开，但 `克隆并继续` 会禁用，并附带不可用说明，不会进入死流程。
      10. `从 Worktree 新建` 主卡会显示基准工作区 dropdown、新分支名 input、可选目录名 input 与只读目标路径预览；底部动作为 `取消 / 返回上一层 / 创建并进入`。
      11. 这张卡的工作区候选会先按现有 workspace attach/recoverable 规则收口，再额外过滤为可识别的 Git workspace；如果当前 attached workspace 不是 Git 目录，不会继续把它伪装成默认项，而是回退到第一个可用 Git workspace。
      12. `从 Worktree 新建` 不打开 path picker；branch / directory 草稿始终保存在 active target picker runtime 里，并跟随同卡 workspace dropdown 刷新保留。
      13. `从 Worktree 新建` 的主卡 confirm 会直接下发 daemon-side `workspace.git_worktree.create` 命令；真正的 `git worktree add` 在 daemon 持锁外执行，不阻塞主锁。
      14. confirm 后 owner card 立即进入 processing：先显示“正在创建 Worktree 工作区”，创建成功且 flow 仍有效时继续 patch 成“正在接入工作区”；success / failure / cancel 都封回同卡 terminal。
      15. `取消创建` 会优先停止业务流，并对 `git worktree add` / fresh-workspace prepare 做 best-effort 取消；若本地目录已经留下，不自动清理，只在 terminal card 提醒用户按需手动处理。
   5. `target_picker_select_workspace` / `target_picker_select_session` / `target_picker_open_path_picker` 都只刷新同一张卡或其子步骤，不会立即 attach 或 switch；旧 `target_picker_select_mode` / `target_picker_select_source` 回调与 mode/source 中间页已删除，不再是 transport contract。
   6. `target_picker_cancel` 是当前四张工作会话业务卡的显式退出路径：编辑态会把当前卡封成 `已取消` 终态；若 Git / Worktree 长链路正处于 processing，则会封成 `已取消导入` / `已取消创建` 并执行 best-effort cancel。
   7. `show_threads` / `show_all_threads` / `show_scoped_threads` / `show_workspace_threads` / `show_all_workspaces` / `show_recent_workspaces` / `show_all_thread_workspaces` / `show_recent_thread_workspaces` 在 headless 主链下当前都只负责“重新打开或刷新 `/workspace list` 切换卡”，不再维持旧的分页 selection-card 主路径。
   8. 被动恢复入口也统一复用 target picker，而不是旧的 scoped selection prompt：
      1. `attach unbound`、`selected_thread_lost`、`thread_claim_lost` 当前都会打开“锁定当前工作区”的 target picker。
      2. 这类卡片会隐藏工作区下拉，只保留当前工作区的会话候选；若当前工作区只剩 `new_thread` 可走，会自动预选这一个候选。
      3. 用户若尝试从旧卡切到别的工作区，或确认一个已经不属于当前工作区的旧候选，服务端不会 cross-workspace fallback，而是刷新同一张锁定卡，并明确提示“当前工作区已锁定”。
   9. `/new` 已变成 workspace-owned prepared state。
   10. `/follow` 在 headless 主链下只返回迁移提示，不再进入 follow route。
11. `vscode` 主链当前已经完成这一轮收窄：
   1. `/list` attach/switch instance 后默认进入 follow-first，而不是落回 pinned/unbound。
   2. 默认跟随目标只看 `ObservedFocusedThreadID`，不再回落 `ActiveThreadID`。
   3. detached `vscode /use` / `/useall` 会直接拒绝，并要求先 `/list`。
   4. attached `vscode /use` / `/useall` 只看当前 attached instance 的已知 thread 集合；`/use` 显示最近 5 个，`/useall` 显示当前实例全部会话，两者都统一成当前实例内的 dropdown 选择。
   5. dropdown 当前不会再把不可切换 thread 作为 disabled 选项渲染在卡面里，而是直接隐藏，并在卡片正文追加 plain-text 提示说明“已省略当前不可切换的会话”。
   6. `vscode /use` 的 one-shot force-pick 会保留 `RouteMode=follow_local`，后续 observed focus 仍可覆盖。
   7. 若 `/list`、`/use`、`/useall` 来自带 `daemon_lifecycle_id` 的当前菜单卡 callback，实例列表 / 线程列表 / attach 结果 / use 结果会继续沿当前菜单卡时间线收口，不再退回 submission-anchor 或额外 detached notice。
   8. 若 stamped `/mode vscode` 在切换后立刻命中 legacy `editor_settings` 且存在可接管入口，daemon 会先静默自动迁到 `managed_shim`，成功后直接继续 open prompt / resume failure 等后续链路；只有缺 target、自动迁移失败、或需要修复的 managed shim，才会把首张可投影提示卡优先替换当前卡。纯文本 `/mode vscode` 仍保留原来的异步提示语义。

### 3.2 路由主状态

| 代号 | 条件 | 用户语义 |
| --- | --- | --- |
| `R0 Detached` | `AttachedInstanceID == ""` | 当前没有接管任何目标；headless 主链下表现为“未接管工作区”，`vscode` 下表现为“未接管实例” |
| `R1 AttachedUnbound` | `AttachedInstanceID != ""`，`RouteMode=unbound`，`SelectedThreadID == ""` | 已接管目标但当前没有可发送 thread；headless 主链下通常表示“已接管 workspace、未选 thread” |
| `R2 AttachedPinned` | `AttachedInstanceID != ""`，`RouteMode=pinned`，`SelectedThreadID != ""`，且持有 thread claim | 当前输入固定发到该 thread |
| `R3 FollowWaiting` | `AttachedInstanceID != ""`，`RouteMode=follow_local`，`SelectedThreadID == ""` | 仅 `vscode` 合法：已进入 follow，但当前没有可接管 thread |
| `R4 FollowBound` | `AttachedInstanceID != ""`，`RouteMode=follow_local`，`SelectedThreadID != ""`，且持有 thread claim | 仅 `vscode` 合法：已跟随到一个 thread |
| `R5 NewThreadReady` | `AttachedInstanceID != ""`，`RouteMode=new_thread_ready`，`SelectedThreadID == ""`，`PreparedThreadCWD != ""` | 仅 headless 主链合法：已准备一个待 materialize 的新 thread；下一条普通文本会创建新 thread |

补充说明：

1. 从 2026-05-03 起，`R1~R5` 的 live mutation 已收口到同一个 route-core transition seam：
   1. `AttachedInstanceID`、`SelectedThreadID`、`RouteMode`、`PreparedThread*` 不再允许由 attach/use/follow/new/detach/kick/thread-lost 各自平行直写。
   2. 真正发生 attachment 变更时，会在同一处重做 workspace / instance / thread claim 对齐。
   3. 仅在同一 attachment 内切换 route 时，不再重复改写 instance claim；这类 transition 只重排 thread claim 与 route 主字段，避免把历史兼容态或 kick-thread 迁移路径卡死在“instance claim 必须先转移”的半状态。
2. detached 态当前仍允许保留一个“记住当前 workspace”的弱 carrier：`AttachedInstanceID == ""` 时 `ClaimedWorkspaceKey` 可以仅作为 continuation intent 存在，但它不等价于 active workspace claim。
3. `R0 Detached` 现在允许存在一种 daemon materialize 出来的 latent surface：
   1. surface 有 `gateway/chat/user` 路由信息。
   2. surface 的 `ProductMode`、`Backend` 与 `Verbosity` 已从持久化 `surface resume state` 恢复；`PlanMode` 不再跨 daemon 恢复。
   3. surface 可能还带有持久化的 resume target 元数据（instance / thread / workspace / route 语义）；它们不会在 materialize 当下直接投影成 live attach，但 daemon 随后会异步评估恢复。
   4. 对 headless 主链来说，这个 latent detached 可能是短暂中间态：
      1. exact visible thread 恢复成功后会进入 `R2 AttachedPinned`。
      2. visible thread 不可见但同 backend workspace 仍可接管时，会进入 `R1 AttachedUnbound`。
      3. 若 persisted route 本身就是 workspace-owned `new_thread_ready`，且同 backend 当前已有对应 workspace 实例，则会直接进入 `R5 NewThreadReady`。
      4. visible/workspace 路径需要 fresh workspace prepare 时，会先进入 `G1 PendingHeadlessStarting`；fresh headless 完成后再回到 `R1 AttachedUnbound` 或带 `PreparedThreadCWD` 的 `R5 NewThreadReady`。
      5. 若还在等待 daemon 启动后的首轮 refresh，则会暂时保持 `R0 Detached` 并静默等待；若 persisted target 还带着 `ResumeHeadless=true` + 已连回的 visible `ResumeInstanceID`，managed-headless continuation 也会一起等待这轮 refresh。
   5. 对 `vscode` 来说，这个 latent detached 也可能是短暂中间态：
      1. 若本机 VS Code 集成仍是旧版 `settings.json` override，或 managed shim 因扩展升级而失效，会保持 `R0 Detached` 并改发迁移/修复卡片。
      2. 兼容性检查通过后，exact instance 恢复成功会进入 `R3 FollowWaiting` 或 `R4 FollowBound`。
      3. 若目标 instance 还没重新连回，会保持 `R0 Detached` 并静默等待。
      4. 不做 workspace fallback，也不会进入 headless 恢复。
   6. 若该 surface 的 `surface resume state` 里仍带有 `ResumeHeadless=true` 的 concrete thread-restore 目标，daemon 会在同一条 headless recovery 主链里先尝试 exact visible attach；只有 visible 路径没恢复成功时，才继续进入 managed-headless exact-thread continuation；`vscode` 不会进入这条分支。这里的 continuation 已按 backend 生效，不再只限 Codex。
2. 这种 latent surface 在 route 维度上仍然是 `R0 Detached`，不是新的 route state。
3. 当前 startup 阶段不会因为 resume target 元数据而在 materialize 当下直接进入 `R1~R5`；是否进入后台恢复、是否转入 `G1 PendingHeadlessStarting`，仍取决于 daemon 后续恢复调度，而不是 materialize 本身。

### 3.2.1 thread 运行时状态 overlay

thread 自身现在还有一层**authoritative runtime status overlay**，来源只认 upstream `thread.status`：

| 代号 | 来源 | 当前实现语义 |
| --- | --- | --- |
| `T0 notLoaded` | `thread/list` / `thread/read` / `thread/started.thread.status` / `thread/status/changed` | thread 当前未 loaded 在某个实例里；会同步成 `ThreadRecord.Loaded=false`，但不会因此把 thread 从可见列表里删掉 |
| `T1 idle` | 同上 | thread 当前 loaded 且空闲 |
| `T2 systemError` | 同上 | thread 当前 loaded，但处于上游 system error 语义 |
| `T3 active` | 同上 | thread 当前 loaded 且 active；额外 activeFlags 当前包括 `waitingOnApproval`、`waitingOnUserInput` |

补充说明：

1. 这层 overlay 当前承载在 `ThreadRecord.RuntimeStatus`，并投影到 `control.ThreadSummary.RuntimeStatus`、`WaitingOnApproval`、`WaitingOnUserInput`。
2. 兼容旧展示链路时，thread summary 的 `State` 仍保留 legacy 字面值：
   1. `active -> running`
   2. `notLoaded -> not_loaded`
   3. `systemError -> system_error`
3. `notLoaded` 的当前产品语义是“thread 目前没 loaded 在实例里”，不是“thread 不可恢复”：
   1. `threadVisible(...)` 仍只看 `Archived` 与 `TrafficClass`。
   2. 只要 thread 仍然可见且保留 `CWD/workspace` 恢复锚点，headless `/use` 仍会走现有 resolver：当前可见 thread、复用 headless、或创建 headless。
   3. detached `/use` 命中这类 thread 时，允许直接进入 preselected headless 恢复。
4. `active(waitingOnApproval|waitingOnUserInput)` 当前只影响 thread 运行态投影与 claimed-thread busy 文案细化：
   1. 若目标 thread 已被别的 surface claim，且 authoritative runtime status 仍是 `active`，kick 判定会落到 `thread_busy_running`。
   2. 这不会单独新增 workspace/thread claim 冲突；没有 claim 的 thread 不会因为 `waitingOnApproval` 就额外变成跨 surface blocker。
5. surface 交互 gate 仍由本地 queue/request/path-picker/capture 事实决定：
   1. `PendingRequest` / `RequestCapture` 继续冻结 route mutation。
   2. thread runtime status 不直接替代 `DispatchMode`、`ActiveQueueItemID`、`PendingRequests`。
   3. 因此允许出现“thread authoritative status 已 idle，但当前 surface 仍有 queued/running queue item”的短暂并存态；两者分别回答不同问题。

### 3.3 执行状态

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `E0 Idle` | `DispatchMode=normal`，无 active，无 queued | 空闲 |
| `E1 Queued` | `QueuedQueueItemIDs` 非空，`ActiveQueueItemID == ""` | 有待派发远端输入 |
| `E2 Dispatching` | `ActiveQueueItemID` 指向 `dispatching` | prompt 已发给 wrapper，turn 尚未建立 |
| `E3 Running` | `ActiveQueueItemID` 指向 `running` | turn 已进入执行 |
| `E4 PausedForLocal` | `DispatchMode=paused_for_local` | 当前 surface 的远端派发被暂停；现有来源包括本地 VS Code 活动，以及 daemon 显式发起的 standalone Codex 升级或 current-thread patch 事务暂停 |
| `E5 HandoffWait` | `DispatchMode=handoff_wait` | 本地刚结束，等待短窗口后恢复远端队列 |
| `E6 Abandoning` | `Abandoning=true` | surface 已放弃接管，等待已有 turn 收尾后最终 detach |

补充说明：

1. 从 `2026-05-03` 起，`执行态 carrier` 与 `恢复态 carrier` 的 live mutation 已显式分到两个 sibling seam：
   1. dispatch core 统一 owner `DispatchMode / ActiveQueueItemID / QueuedQueueItemIDs / pendingRemote / activeRemote / queue item fail-complete-promote`。
   2. recovery core 统一 owner `PendingHeadless / attach-fail-expire / prompt-dispatch-restart reattach / disconnect-degraded-timeout teardown`。
   3. 两者交界只保留显式 handshake：dispatch 在真正 `prompt.send` 前可以请求 recovery 先做 `prompt_dispatch_restart`，但 queue item 与 remote binding 的最终归属仍回到 dispatch core 收口。
2. `E2 Dispatching` 当前只表示“本地 active queue item 已派发，真实 remote turn 还没完成建联”；它并不自动等价于“已有 live turn”。
3. 对 Claude backend，pre-start remote turn 的 stage-0 关联键当前先用 dispatch `CommandID`，再回退 `Initiator.SurfaceSessionID` 与 thread 信息；Claude translator 也会把 remote-surface initiator 显式带进 turn lifecycle。即使某些早期事件仍带 blank initiator，daemon 也会先把它视为 unknown，再通过 `CommandID` 命中 pending turn 并提升成真实 turn lifecycle；因此 backend/runtime 的早失败与 `start_new` 首条消息都不会再把 surface 永久卡在 `dispatching`。
3.1. Feishu MCP 发送类工具和 Drive comments 工具当前也消费这套 remote turn 绑定：wrapper 发布 MCP URL 时只附带 caller instance id，daemon 在 tool call 时先按该 instance 查询 `activeRemote`，再查询 `pendingRemote`，并把产物或评论读取上下文绑定到命中的 `SurfaceSessionID`。工具参数里的 legacy `surface_session_id` 不参与路由；如果 caller instance 当前没有 active/pending remote turn，工具会 fail closed，不回退到 workspace surface context。
4. `/detach` 在 `E2 Dispatching` 下当前分两类处理：
   1. 若仍是 pre-start dispatch（`pendingRemote` 还没有 `TurnID`、没有 output、active item 仍是 `dispatching`），会立即把 active item 标成 failed、清掉 pending remote ownership，并直接 detach。
   2. 只有已经存在真实 started remote turn、或 compact/steer 等仍需等待的 live work 时，才会进入 `E6 Abandoning`。
5. `E6 Abandoning` 现在只覆盖“确实还有 live work 在收尾”的场景，不再把 pre-start dispatching 残留也一并塞进 watchdog-only 等待路径。

### 3.4 审阅态 overlay

review mode 第一版当前不是新的 route state，而是挂在 surface 上的一层 detached review session overlay。

| 代号 | 条件 | 当前实现语义 |
| --- | --- | --- |
| `V0 None` | `ReviewSession == nil`、`Phase != active`，或 `ParentThreadID` / `ReviewThreadID` / `AttachedInstanceID` 任一为空 | 当前 surface 不在 review session 里 |
| `V1 Active` | `ReviewSession.Phase=active`，且 `ParentThreadID`、`ReviewThreadID`、`AttachedInstanceID` 都非空；正常不变量是 `SelectedThreadID == ParentThreadID`，兼容恢复路径允许识别 `SelectedThreadID == ReviewThreadID` | 当前 surface 正处于 detached review session；普通文本默认续发到 review thread，但 surface 主选择必须保持或恢复到 parent thread |

补充说明：

1. `ReviewSession` 当前挂在 `SurfaceConsoleRecord`，字段包括：
   1. `ParentThreadID`
   2. `ReviewThreadID`
   3. `ActiveTurnID`
   4. `ThreadCWD`
   5. `SourceMessageID`
   6. `TargetLabel`
   7. `LastReviewText`
2. review thread 当前必须有显式 `ThreadRecord.Source.Kind=review`；parent thread 关系优先来自 `ForkedFromID`，其次来自 `ThreadSourceRecord.ParentThreadID`。
3. 当前激活条件不是“点了某个前台按钮”，而是更底层的 runtime 事实：
   1. 同一 attached instance 上已知某个 review thread
   2. 该 review thread 命中了当前 surface 的 review runtime 事件：优先是 `turn.started(remote_surface)`；若上游这轮 `turn.started` 还没带回 surface 归属，则 `entered_review_mode` / `exited_review_mode` 生命周期 item 也会把 pending session 提升成 active
4. `SelectedThreadID == ParentThreadID` 是 detached review session 的选择不变量，不再是判断 `ReviewSession` 是否存在的唯一依据。若旧版本或异常投影曾把 `SelectedThreadID` 污染成 `ReviewThreadID`，后续审阅文本、`放弃审阅`、`按审阅意见继续修改` 会先尝试恢复 parent selection，再继续处理。
5. 新发起 review 时，若当前候选线程本身是 `source=review`，服务端必须先回溯到它的 parent thread，再把 `review.start` 发给 parent；review thread 不能作为新的 review 启动目标。
6. `V1 Active` 不会把 surface route 从 `pinned/follow` 改成新的 route 值；review session 只是额外改写“普通文本该发到哪个 execution thread”。若用户进入 `/new` 的 `new_thread_ready`，空闲 review session 会先被清掉，避免首条新会话文本继续落到 review thread。
7. 当前 review session 没有独立的 attach/list 暴露语义，也不会自动把 review thread 变成 surface 默认选中 thread；普通 attach/list/use 候选现在会显式过滤 `source=review` 的 detached review thread。
8. 当 `ReviewSession.ActiveTurnID` 非空时，这层 overlay 还会进入统一 route-mutation blocker seam：`/use`、`/follow`、`/new`、`/claudeprofile`、`/codexprovider`、`/compact` 等会改工作目标的动作都会直接拒绝，并返回 `review_running`；只有 idle review session 会在 detach-like cleanup 或 route change 时被自动清掉。

补充说明：

1. 当前还存在一个**可叠加**的 steering overlay：
   1. 某个 queued item 被点赞升级后，会离开 `QueuedQueueItemIDs`
   2. 或者用户 reply 当前 processing 的 source message，且 reply 内容属于当前 v1 支持的文本 / 本地图片输入时，会创建一个临时 steering item；独立文件 reply 当前不会走 steering，而是保留为 staged file
   3. 该 item 进入 `QueueItemStatus=steering`
   4. 相关命令记录在 `pendingSteers`
2. 这个 overlay 不占用 `ActiveQueueItemID`，所以可以与 `E3 Running` 并存。
3. steering ack 成功后，item 进入 `steered`；失败时恢复回普通语义：
   1. 文本 / 图文 reply 恢复为普通 queued item
   2. 独立图片 reply 恢复为 `ImageStaged`
4. `E4 PausedForLocal` 当前有三条来源分支：
   1. local-activity 分支由 `pauseForLocal(...)` 写入 `pausedUntil`，因此仍有 watchdog，并且后续可能进入 `E5 HandoffWait`。
   2. standalone Codex 升级分支由 daemon 通过 `PauseSurfaceDispatch(...)` 显式写入；这条路径会主动清掉 `pausedUntil/handoffUntil`，因此不会被 `Tick()` watchdog 自动恢复，只会在升级事务显式 `ResumeSurfaceDispatch(...)` 时继续派发。
   3. current-thread patch 事务同样由 daemon 通过 `PauseSurfaceDispatch(...)` 显式写入；它会暂停同一 instance 上所有 attached surface 的 dispatch，直到 patch apply / rollback 成功或失败收口后再显式 `ResumeSurfaceDispatch(...)`。

### 3.4 输入门禁状态

| 代号 | 条件 | 作用 |
| --- | --- | --- |
| `G0 None` | 无附加门禁 | 普通输入按主路由走 |
| `G1 PendingHeadlessStarting` | `PendingHeadless.Status=starting` | headless 仍在启动 |
| `G2 PendingRequest` | `PendingRequests` 非空 | 普通文本/图片/文件会被待处理请求门禁挡住；当前仍只有一套 pending request substrate，但卡面语义会按 `SemanticKind` 区分为 approval、`approval_command`、`approval_file_change`、`approval_network`、`request_user_input`、`permissions_request_approval`、`mcp_server_elicitation_form`、`mcp_server_elicitation_url`、`tool_callback` 等变体。顶层 `tool/requestUserInput` 与 `item` 形式共用同一 `request_user_input` gate；`tool_callback` 当前不会等待用户作答，而是以只读提示 + 自动 unsupported 回写的 fail-closed 方式短暂占用 gate，直到上游 `request.resolved` 清理 |
| `G3 RequestCapture` | `ActiveRequestCapture != nil` | 下一条普通文本会被当成拒绝反馈 |
| `G4 PathPicker` | 当前 surface 的 active path picker runtime 非空 | 当前存在一个仍有效的飞书路径选择器；core 只关心“gate 是否存在、是否阻断 competing UI / route mutation、confirm/cancel 后如何交给 consumer”，不关心目录浏览细节 |
| `G5 TargetPickerProcessing` | 当前 surface 的 active target picker 处于 Git import 或 Worktree create processing | 当前存在一个仍有效的 Git/Worktree owner-card 业务流；普通文本/图片/文件、`/list`、`/use`、`/useall`、`/new`、`/follow`、`/detach`、bare config 与其它 competing card flow 都会被挡住并提示等待完成、取消，或使用 `/status`；只保留 `/status`、reaction/recall 与 `target_picker_cancel` |
| `G6 AbandoningGate` | `Abandoning=true` | 只有 `/status`、`/autowhip` 与 `/autocontinue` 继续正常，其余动作被挡 |
| `G7 VSCodeCompatibilityBlocked` | `ProductMode=vscode`，surface detached，且本机检测到“不能安全自动收口”的 VS Code 兼容性问题 | daemon 不再自动恢复 exact instance，也不再发普通“请先打开 VS Code”提示，而是改发必要的修复/失败反馈；legacy `editor_settings` 若已存在可接管入口，会先静默自动迁到 `managed_shim`，只有缺 target、自动迁移失败或 stale managed shim 时才真正停在这个 gate。若这张提示由 stamped `/mode vscode` 当前卡同步触发，则优先承接到当前卡，否则保持独立 runtime 提示 |
| `G8 TurnPatchEditing` | daemon 侧存在当前 surface 的 active turn-patch flow，且 `stage=editing` | 当前 frontstage 被 patch 卡占用；只有同一张 patch 卡的 `request_respond` / `request_control` 与 reaction/recall 可以继续，其它动作会被挡住并提示先提交或取消 |
| `G9 UpgradeOwnerFlowRunning` | daemon 侧 active upgrade owner-flow 处于 `running` / `cancelling` / `restarting` | 这是 daemon 顶层的独立升级 gate；只允许 `/status`、`/upgrade`、`/debug`、reaction/recall 与同一张升级卡自身动作继续，其它 competing 输入会被拒绝 |
| `G10 StandaloneCodexUpgradeRunning` | daemon 侧 active standalone Codex upgrade transaction 非空 | 这是 daemon 顶层的独立 upgrade gate，不复用现有 `codex-remote` owner-flow。发起 surface 的普通输入会被直接挡住；其它真正依赖 standalone Codex 的 attached surface 会继续走“写入队列 + notice + `paused_for_local`”语义；VS Code surface / instance 当前完全排除在这条 gate 之外；非 queueable 命令/卡片动作当前仍直接拒绝。事务只有在 install 完成、child restart `ack` 已确认、且匹配的 restore outcome 成功/失败/超时收口后才会退出，不会在 bare restart ack 后提前解 gate |
| `G11 TurnPatchTransactionRunning` | daemon 侧存在当前 instance 的 active turn-patch transaction | 发起 surface 与同 instance 上其它 attached surface 的状态改写类输入都会被挡住；当前只保留 `/status`、`/list`、`/help`、`/menu`、`/history`、`/debug`、reaction/recall 等查看类动作，直到 patch apply / rollback 收口。事务只会在 rollout 写盘后对应的 child restart 最终 outcome 成功，或在失败/超时后自动回滚并完成恢复 restart 收口后退出 |

补充说明：

1. `ActivePathPicker` 当前是一个 coarse-grained modal overlay：
   1. root / current / selected path、owner、expiresAt、consumer 元数据当前由 orchestrator service 持有的 per-surface runtime 记录承载，不再直接挂在 `core/state.SurfaceConsoleRecord` 上。
   2. core 不引入新的 route mode，也不追踪“当前浏览到了第几层目录”这类 UI 细节。
   3. core 只在两类地方感知它：
      1. route-mutation / competing Feishu card flow gate
      2. confirm / cancel 的 gate 清理与 consumer handoff
      3. unauthorized 只回拒绝 notice，不清当前 gate
   4. `ApplySurfaceAction()` 入口当前会先做一次 expired picker 清理：
      1. 若 active path picker runtime 的 `ExpiresAt <= now`，先清 gate，再继续处理当前 action。
      2. 这样即使用户不再点击旧 picker 卡片，只要发任意新动作，也不会卡在长期 `path_picker_active`。
2. route-mutation blocker 当前不再由 `/use`、`/follow`、`/new`、`/compact`、`/claudeprofile`、`/codexprovider` 等各自横向拼装，而是统一走一条查询 seam：
   1. 当前 blocker 只会回答 `target_picker`、`path_picker`、`request_capture`、`pending_request`、`review_running` 这五类原因。
   2. `review_running` 只在 `ReviewSession.ActiveTurnID` 非空时成立；idle review session 不再长期占用 gate，而是交给 route-change / detach-like cleanup 清掉。

### 3.4.1 context-bound overlay cleanup seam

除了真正阻断路由的 gate 之外，surface 当前还有一组“随当前 attach / route 上下文生效”的 overlay runtime：target picker、path picker、thread history、review commit picker、workspace page，以及 idle review session。

当前规则：

1. 这些 overlay 的清理不再散落在 detach / `/use` / `/follow` / `/new` 各处，而是统一经由 cleanup seam 处理。
2. 会触发这条 seam 的路径包括：
   1. `finalizeDetachedSurface(...)`
   2. `prepareSurfaceForExecutionReattach(...)`
   3. `/use`、`/follow`、`/new`
   4. `selected_thread_lost`、`thread_claim_lost`、follow retarget、victim release 这类 route retarget
3. cleanup seam 同时负责两件事：
   1. 清掉 runtime carrier。
   2. 若旧 overlay 仍持有稳定 `message_id` / owner anchor，则主动把旧卡 patch 成 sealed `已失效` / 只读失败态，而不是等用户再点一次旧卡才发现失效。
4. 当前会主动 seal 的 overlay 有：
   1. workspace page
   2. target picker
   3. path picker
   4. thread history
   5. review commit picker
5. 若旧 overlay 已失去稳定 anchor，则允许退化为“只清 runtime + 后续 callback fail-closed”；不会伪造补封。
6. target-picker owner-subpage 里的 path picker 额外有一条优先级规则：
   1. 若当前可见的是 path picker 子步骤，而 target picker 父卡其实隐藏在同一张 owner message 后面，则 cleanup 只 patch 可见子步骤。
   2. 隐藏的 target picker runtime 只静默清掉，避免对同一张消息做重复或相互覆盖的失效 patch。
7. 少数明确仍要让 target picker 持续接管当前卡的路径，会显式声明 `PreserveTargetPicker`：
   1. target picker confirm 直接进入 `/use` / `/new`
   2. fresh workspace headless prepare
   3. pending headless reconnect 重新接回同一张 target picker owner card
8. detach-like cleanup 还会统一清掉 idle review session；只有 running review turn 会保留 review runtime 并改走前面的 `review_running` blocker。

### 3.5 草稿状态

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `D0 NoDraft` | 无 staged image / staged file，无 queued draft | 没有待绑定输入 |
| `D1 StagedAttachments` | `StagedImages` 中存在 `ImageStaged`，或 `StagedFiles` 中存在 `FileStaged` | 附件已到达，但尚未冻结到 queue item；图片直接作为 image input 带入，文件则会在真正 dispatch 时生成本地路径引用块 |
| `D2 QueuedDrafts` | `QueuedQueueItemIDs` 非空 | 已冻结 route/cwd/override，等待派发 |
| `D3 NewThreadFirstInput` | `RouteMode=new_thread_ready` 且已存在 queued/dispatching/running 的首条消息 | 新 thread 尚未落地，但本轮创建已占用 |

关键区别：

1. `D2` 已冻结路由。
2. `D1` 还没有冻结路由，所以 route change 时必须显式处理。
3. `D3` 不是独立 route state，而是 `R5` 上的附加约束。

### 3.6 autowhip overlay

`AutoWhipRuntimeRecord` 当前不是新的 route state，而是 surface 上附加的一层运行时 overlay；用户可见命令面当前统一叫 `autowhip`：

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `A0 Disabled` | `AutoWhip.Enabled=false` | 当前 surface 不做 autowhip |
| `A1 EnabledIdle` | `AutoWhip.Enabled=true`，`PendingReason==""` | 已开启，但当前没有待触发的 autowhip |
| `A2 Scheduled` | `AutoWhip.Enabled=true`，`PendingReason!= ""`，`PendingDueAt` 非空 | 已记录一次待触发 autowhip，等待 backoff 到期并再次过门禁 |

补充说明：

1. `A2 Scheduled` 不会直接占用 `ActiveQueueItemID`。
2. 真正 enqueue autowhip item 发生在 `Tick()`，而不是 `turn.completed` 同步路径里。
3. `A2 Scheduled` 只有在下列条件同时满足时才会真正发出：
   1. surface 仍 attached
   2. `DispatchMode=normal`
   3. 没有 `PendingHeadless` / `PendingRequest` / `RequestCapture` / `Abandoning`
   4. 当前没有 live remote work
4. autowhip queue item 的 reply anchor 与 pending projection 当前已显式拆开：
   1. 最终回复仍挂回原用户消息
   2. queue / typing / reaction 不再回写到原用户消息
5. autowhip 的系统提示当前分三类：
   1. `incomplete_stop` 不会在 schedule 瞬间发 notice，而是在真正从 `A2 Scheduled` 转成实际补打时发一条 `AutoWhip` notice：`Codex疑似偷懒,已抽打 N次`
   2. 若 final assistant 文本命中收工口令，则不会继续 schedule / dispatch，而是立刻发一条 `AutoWhip` notice：`Codex 已经把活干完了，老板放过他吧`
   3. 若 `incomplete_stop` 已达到连续补打上限，则会回一条停止 notice，并清空当前 autowhip runtime

### 3.7 autoContinue overlay

`AutoContinueRuntimeRecord` 当前也是 surface 上附加的一层运行时 overlay；用户可见命令面当前统一叫 `autocontinue`：

| 代号 | 条件 | 含义 |
| --- | --- | --- |
| `R0 Disabled` | `AutoContinue.Enabled=false` | 当前 surface 不做上游失败自动继续 |
| `R1 EnabledIdle` | `AutoContinue.Enabled=true`，`Episode==nil` | 已开启，但当前没有待自动继续的 episode |
| `R2 Scheduled` | `AutoContinue.Enabled=true`，`Episode.State=scheduled` | 已记录一次待自动继续 episode，等待可派发或 backoff 到期 |
| `R3 Running` | `AutoContinue.Enabled=true`，`Episode.State=running` | 当前正在执行一次自动继续尝试 |
| `R4 TerminalRetained` | `AutoContinue.Enabled=true`，`Episode.State=failed/cancelled` | 当前 episode 已停止；保留最后一次状态，等待用户切换目标、关闭 `/autocontinue`，或新的 episode 覆盖 |

补充说明：

1. autoContinue 只承接 `terminalCause=autocontinue_eligible_failure`：
   1. `completed`、`user_interrupted`、`startup_failed`、`nonretryable_failure`、`transport_lost` 都不会进入这条 overlay。
   2. 这层资格由 orchestrator 本地 `problem -> terminalCause` 分类统一拥有；当前只接受 layer 属于 `""/codex/gateway`，且 code 命中 `responseStreamDisconnected`、`responseTooManyFailedAttempts`、`serverOverloaded`、`other` 的问题，不再依赖 upstream `willRetry/problem.Retryable`。
   3. translator 已把 `turn.start/thread.resume` 前置拒绝与真正 runtime failure 分开，因此 autoContinue 不会误接管“turn 还没真正开始”的失败。
2. autoContinue queue item 是独立来源：
   1. `SourceKind=auto_continue`
   2. 真正 dispatch 前不会伪造用户 pending / typing / reaction
   3. 恢复 prompt 固定为系统生成的“请从中断处继续”
3. autoContinue 的 dry-failure backoff 当前固定为：
   1. 第 1、2 次连续空失败：立即重试
   2. 第 3 次：`2s`
   3. 第 4 次：`5s`
   4. 第 5 次：`10s`
   5. 第 6 次连续空失败：直接进入 `failed`
4. 一旦某次恢复尝试出现任何输出，下一次再失败时会把 dry-failure 计数重置回“第一次立即重试”。
5. autoContinue 调度优先级高于普通 queued item：
   1. `dispatchNext(...)` 会先检查 pending autoContinue，再考虑普通 queue
   2. 用户新消息与已排队消息会保留在原队列里，不会被丢弃
6. autoContinue 状态卡与真正业务输出当前显式拆开：
   1. 状态卡 reply 到原始用户消息
   2. 后续自动继续链路里的 final / request / plan / image / progress 继续沿用原始 reply anchor，不会改挂到状态卡下面
   3. 状态卡只允许在自己仍是当前 surface 尾消息时 patch；一旦后面出现更新消息，就冻结旧卡，后续状态改为 append 新卡
7. autoContinue episode 当前不会跨目标长期悬挂：
   1. `/detach`
   2. `/new`
   3. `/use` / `/follow` 等显式 route mutation
   4. 目标 thread 丢失或被强踢
   以上路径都会清掉当前 episode，只保留 `/autocontinue` 的 enable 开关
8. 若某个 episode 来自 `keep_surface_selection` detour：
   1. surface 当前选中的 thread 仍以 source/main thread 为准
   2. 但真正的 retry 目标仍会继续打到 execution thread
   3. 因此 detour 的 autoContinue 不会因为 `SelectedThreadID != executionThreadID` 被误清理

## 4. 当前已实现的不变量

### 4.1 `codex` 的 `workspace` 命令族先打开工作会话页面或业务卡，confirm 后再改 route

当前 `codex` 的工作会话主展示已经切到 `workspace` 命令族：bare `/workspace` / `/workspace new` 负责父页导航，`/workspace list` / `/workspace new dir` / `/workspace new git` / `/workspace new worktree` 负责四张独立业务卡；`/list` / `/use` / `/useall` 只保留 alias，并在 `codex` 下汇合到 `/workspace list`。

对应实现里：

1. `targetPickerWorkspaceEntries()` 仍以当前可操作 workspace 为候选源。
   1. 在线实例先按可见 thread 的 `CWD` 归并 workspace；只有当某个 instance 当前完全没有可见 thread 时，才回退到该 instance 的 `WorkspaceKey/WorkspaceRoot`。
   2. merged thread views / persisted recent threads 仍会把 recoverable-only workspace 补进候选。
   3. 仍会过滤 busy workspace，以及既不能 attach 也没有 recoverable thread 支撑的 workspace。
2. bare `/workspace` / `/workspace new` 的直接动作，以及 `codex headless` 下从菜单首页点 `工作会话`，当前都会先产出 `UIEventFeishuPageView`。
   1. bare `/workspace` 固定打开工作会话父页，展示 `切换`、`从目录新建`、`从 GIT URL 新建`、`解除接管` 四个入口。
   2. bare `/workspace new` 固定打开新建方式子页，展示 `从目录新建`、`从 GIT URL 新建` 与 `从 Worktree 新建` 三个入口。
3. `/workspace list` 的直接动作，以及 headless 主链下的 `show_*` 同上下文导航，现在都会产出 `UIEventFeishuTargetPicker`。
   1. `/workspace list` 与 alias `/list` / `/use` / `/useall` / `show_workspace_threads` 都直接打开 `Page=target`。
   2. attached `/use` 会预填当前 workspace；`/useall` 与 workspace-scoped 入口仍允许跨 workspace，但不会锁死选择。
   3. 当前没有已有 workspace 时，不会再退回旧的 `模式` / `来源` 页；切换卡会直接落在 `目标` 页，并通过阻塞消息告诉用户先走新建路径。
   4. `attach unbound`、`selected_thread_lost`、`thread_claim_lost` 这类被动恢复入口当前也走同一套 `UIEventFeishuTargetPicker`，但会打开锁定当前工作区的变体：隐藏工作区下拉，只允许在当前工作区内重新确认会话或继续 `new_thread`。
3. `目标` 页下，会话下拉始终基于当前选中的 workspace 动态重建。
   1. 先列该 workspace 下当前可接管或可恢复的 thread。
   2. picker 首次打开时，只有 surface 当前已经绑定到该 workspace 的某个 `SelectedThreadID`，且该 thread 仍在候选里，才默认选中该 thread。
   3. 如果当前 workspace 虽然已选中，但 surface 处于 unbound / detached，或者当前路由并不属于这个 workspace，则会话下拉保持空值，不再回退到“第一个可恢复 thread”。
   4. 只要用户随后切换了工作区，当前会话选择就会被显式清空，卡片回到“未选会话”占位态；必须重新选择后才能 confirm，不再 silent fallback 到新的默认会话。
   5. 工作区下拉只显示当前可操作的真实 workspace；busy / 不可接管 workspace 不再单独列在主路径里。
   6. workspace label 足够时只显示 label；只有 basename 冲突时，才会额外补路径 meta 做消歧。
4. `/workspace new dir`、`/workspace new git` 与 `/workspace new worktree` 的主卡会直接打开各自业务页，不再先经过共同的模式 / 来源向导。
   1. `从目录新建` 主卡会显示路径字段、`选择目录` 按钮与 `接入并继续` 主按钮。
   2. `从 GIT URL 新建` 主卡会内联保存 `repo_url` / `directory_name` 草稿，并通过 `target_picker_open_path_picker` 选择父目录；底部动作为 `取消 / 上一步 / 克隆并继续`。
   3. `从 Worktree 新建` 主卡会显示基准工作区 dropdown、新分支名 input、可选目录名 input 与只读目标路径预览；底部动作为 `取消 / 返回上一层 / 创建并进入`。
   4. Git / Worktree 两条路径都把草稿保存在 active target picker runtime 里，不进入 `PendingRequest`。
5. 选择工作区、选择会话，或从主卡打开 path picker 子步骤时，只会刷新 target picker 本身或其子步骤，不会立即 attach / switch。
   1. `/workspace list` 主路径用的是 `target_picker_select_workspace` / `target_picker_select_session`。
   2. `/workspace new dir` / `/workspace new git` 主路径用的是 `target_picker_open_path_picker`；`/workspace new worktree` 主路径则复用 `target_picker_select_workspace` / `target_picker_page` 刷新基准工作区 dropdown。
   3. 这些回调属于 same-context pure navigation，满足 daemon freshness 时会 inline replace 当前卡；`target_picker_cancel` 也会 inline replace，但它的效果是把当前 owner card 收束成 sealed terminal，并清掉 active picker / owner-card flow。
6. 真正的产品状态变化只发生在 `target_picker_confirm`。
   1. `/workspace list` 选既有会话时，复用现有 `/use` / `use_thread` / cross-workspace attach 语义；必要时会先统一经过 `resolveWorkspaceContract(...)` 与对应的 workspace continuation owner，再落到 attach / restart-managed / fresh-start 的单一路径。
   2. `/workspace new dir` 下，`target_picker_open_path_picker` 会先打开目录 path picker；confirm/cancel 回调会先异步 ack，再把最新主卡 patch 回同一张 owner card。主卡只要已经回填出有效目录，`target_picker_confirm` 就会继续：若命中已知 workspace，则直接复用该工作区并进入新会话待命；若不是已知 workspace，则把该目录解析成 workspace，并按 `PrepareNewThread=true` 的语义进入 `R5` / fresh headless `R5` 路径。
   3. `/workspace new git` 下，主卡会内联保存 `repo_url` / `directory_name` 草稿，并通过 `target_picker_open_path_picker` 选择父目录；`target_picker_confirm` 随后直接下发 daemon-side `workspace.git_import` 命令。
   4. `/workspace new worktree` 下，主卡会内联保存 `target_picker_worktree_branch_name` / `target_picker_worktree_directory_name` 草稿，并允许同卡切换基准工作区；`target_picker_confirm` 随后直接下发 daemon-side `workspace.git_worktree.create` 命令。
   5. Git import 的 path picker cancel 不会改 route，只会回到 target picker 主卡；clone 成功但 flow stale 时会回 stale notice 并保留本地目录；若后续 attach / prepare 失败，则会把同一张 owner card 封成 failed terminal，并明确目录已保留。
7. `target_picker_confirm` 当前还有一条显式防呆。
   1. 若用户按下确认时，工作区或会话候选已经变化到不再包含原选择，服务端不会再 silent fallback 到别的默认候选。
   2. 当前行为是追加一张最新 target picker + `target_picker_selection_changed` notice，要求用户在最新卡片上重新确认。
8. 旧的 headless grouped workspace/thread selection cards 不再是主路径。
   1. 当前不再保留旧 `create_workspace` transport 兼容入口；headless 主链的新工作区路径统一走 `/workspace new dir` / `/workspace new git` / `/workspace new worktree` 这三张业务卡。
   2. `show_all_workspaces` / `show_recent_workspaces` / `show_workspace_threads` / `show_all_thread_workspaces` / `show_recent_thread_workspaces` 在 headless 主链下当前都退化成“用指定 source / workspace 重新打开 target picker”的兼容导航入口。
   3. 被动恢复路径也不再回到旧 scoped selection prompt；一律刷新锁定当前工作区的 target picker，并拒绝 silent fallback。
9. confirm 后真正 attach / switch 时，`attachWorkspace()`、`attachSurfaceToKnownThread()` 与 `startHeadlessForResolvedThread()` 在 headless 主链下仍然会先走 `workspaceClaims`，再进入现有 `instanceClaims` / `threadClaims`。

结果：

1. 同一个 workspace 当前最多只允许一个 headless surface 占有。
2. 第二个 headless surface 如果试图通过 target picker attach/switch 到同 workspace，或通过 `/use` / headless 恢复到该 workspace，会直接收到 `workspace_busy`。
3. 同一个 instance 仍然只能被一个飞书 surface attach；也就是说 instance claim 还在，只是已经退回到 workspace claim 之后。
4. 不会进入“workspace 仲裁层已经冲突，但仍然 attach 成功”的半 attach 状态。
5. 通过旧 `attach_workspace` 兼容入口时，成功后仍会落到 `R1 AttachedUnbound`；而 `/workspace list` confirm 既有会话时会直接落到 `R2`，`/workspace new dir` / `git` / `worktree` confirm 成功时则会进入 `R5`。
6. managed headless instance 一旦已经被 retarget 到某个精确 workspace，后续 `thread.focused` / `threads.snapshot` 里的更宽父目录 `cwd` 当前不会再把它的 `WorkspaceRoot` 回退成父目录，避免 `/status` 与 `/use` 再次出现“实例显示是 A，实际 thread 在 B”的分裂态。

### 4.1.1 当前 live 的 `claude` 命令面继续复用同一工作会话壳，但已回退 review / patch 主展示入口

当前 `claude` 不再沿用 `codex` 的 `workspace` 主入口投影，但会继续复用同一套 workspace/session route contract 与 target picker 壳。

注意：这段描述的是 **当前 live code** 的一轮 dev-only Claude 暴露面，不是 2026-04-29 已批准的下一轮 MVP 产品边界；后者改以 [Claude Backend Integration Plan](../inprogress/claude-backend-integration-plan.md) 第 `7.6` 节为准。

对应实现里：

   1. `ResolveFeishuCommandSupport()`、`ResolveFeishuCommandDisplayProfileForContext()` 与 display-group projection 当前共用同一 command support profile，把 Claude visible MVP 与静态 dispatch 支持固定为：
   1. `current_work` 只保留 `/stop`、`/new`、`/status`、`/detach`。
   2. `switch_target` 只保留 `/list`、`/use`。
   3. `send_settings` 当前显式开放 `/reasoning`、`/access`、`/plan`、`/verbose` 与 backend 互斥的 provider/profile 入口：
      - `codex headless` 可见 `/codexprovider`
      - `claude headless` 可见 `/claudeprofile`
      - `vscode` 不显示这两条入口
   4. `common_tools` 当前显式开放 `/history` 与 `/sendfile`。
   5. `/review`、`/bendtomywill` 与 `/autocontinue` 继续 hidden + reject，不会在 Claude mode 下伪装成可用入口。
   6. `workspace*` 与 `/useall` 当前是 hidden + allow 兼容入口：不出现在 Claude 主展示菜单里，但仍由同一 support profile 显式允许，供 target picker / 旧 slash / 子页回退继续复用同一工作区与会话壳。
2. `/list` 与 `/use` 当前继续复用 headless workspace-session target picker 壳，不另造一套 Claude 专用卡。
3. 但 target picker 的 workspace/session 候选已经只按当前 backend 过滤：
   1. `mergedThreadViews()` 只合并当前 headless backend 的在线实例。
   2. `normalModeListWorkspaceSetWithViews()`、`targetPickerWorkspaceEntries()` 与 `buildWorkspaceSelectionModel()` 只保留当前 backend 的在线 workspace。
   3. workspace/session 候选现在统一依赖 backend-scoped online/recoverable workspace 集合与 `resolveWorkspaceContract(...)` 结果；不再保留按 `ClaudeProfileID` 缩窄同 backend 候选的旧 helper 路径。
   4. Claude headless 不再把 persisted Codex recent threads/workspaces 混进 `/list` / `/use` 候选。
   5. detached `/use`、headless startup resume 与 concrete headless restore 现在也会按当前 backend 读取 persisted exact-thread metadata；Claude 可以消费 persisted Claude session，但不会误吃 Codex sqlite thread。
4. `/mode claude` 若切换前已有当前 workspace，不会只留下 detached surface：
   1. 优先 attach 同 workspace 的在线 Claude instance。
   2. 若当前没有可 attach 的 Claude instance，则直接起 fresh managed headless，并把该 workspace 记成 `PendingHeadless.ThreadCWD`。
5. Claude runtime 只有在目标 thread 能解析成“当前 workspace 下真实可恢复的 Claude session”时，才会为了 `prompt.send` 触发内部 child restart；普通新会话的目标 thread 壳值不会误判成 session switch。
6. `/claudeprofile` 当前是 Claude headless 专属的 surface 级切换入口：
   1. bare `/claudeprofile` 会打开 dropdown 参数卡，候选固定为内置 `default` 加当前持久化 profile 列表。
   2. 只有 `ProductMode=normal && Backend=claude` 时允许切换；其他 mode/backend 会直接拒绝并要求先 `/mode claude`。
   3. request gate、`PendingHeadless`、live remote work 与 delayed-detach 当前都会阻断 profile 切换，不会让 surface 进入半重启状态。
   4. detached idle surface 切 profile 时，只会替换 `ClaudeProfileID`，并清空当前 surface 的临时 `PlanMode / PromptOverride`，不会偷偷沿用旧 profile 的残留值。
   5. 当前 workspace 已占用时，切 profile 会先 detach-like 清理旧 runtime，再按目标 `workspace+profile` 快照只恢复飞书临时 `ReasoningEffort` override，并把 `PlanMode / AccessMode` 归零；随后按切换前的 continuation intent 直接 fresh-restart 到新 profile：原来只是 workspace-owned `unbound/new_thread_ready` 时继续走 fresh workspace prepare；原来已经 pinned 到某个 Claude thread 时，会保留该 exact-thread 恢复目标并直接拉起新的 thread-restore headless，而不会要求用户重新 `/use`。
   6. 若切换前当前 surface 接着的正是一个 Claude managed headless，切 profile 时还会先显式 kill 旧 child，再启动新 profile 对应的 child，避免 surface 被错误地重新 attach 回“旧 profile 但同 workspace”的在线实例。
7. `ClaudeProfileID` 当前也是 surface runtime carrier：
   1. 若 surface 当前已持有某个 profile，`/mode claude`、workspace attach、visible resume 与 fresh/preselected headless launch 都会继续沿用它。
   2. 若 surface 还没有显式 profile，而 backend 已切到 `claude`，则会 lazy 默认成内置 `default`。
   3. daemon 重启后，latent surface 会从 `surface resume state` 恢复之前的 `ClaudeProfileID`，因此后续 Claude workspace 恢复不会退回“只记 backend、不记 profile”。
8. daemon 在 fresh/preselected headless launch 时，会只消费 orchestrator 已冻结下来的 headless launch contract：
   1. `PendingHeadless` 与 `DaemonCommandStartHeadless` 当前都会显式携带 `Backend / CodexProviderID / ClaudeProfileID`；若 frozen backend 是 `claude`，还会额外带 `ClaudeReasoningEffort`。
   2. daemon 不再在真正启动时回读 live surface backend；若 `headless.start` 命令本身缺少 frozen backend contract，会直接按启动失败收口，而不是再隐式借值。
   3. 实际注入环境时仍会落 `CODEX_REMOTE_INSTANCE_BACKEND=<frozen backend>`；若当前 frozen backend 是 `claude`，会把当前 `ClaudeProfileID` 下发到 headless command，并且只在 frozen reasoning 非空时下发 `CLAUDE_CODE_EFFORT_LEVEL`，因此 Claude headless 会按正确 profile 与显式推理强度启动，而不是默回 Codex、默回别的 Claude profile，或继续沿用旧 reasoning。
9. Claude profile 只改变启动时注入给 wrapper/Claude child 的 endpoint、auth token 与 model env；它不会改写或创建 profile 专属 `CLAUDE_CONFIG_DIR`。因此 exact-thread 恢复、session catalog 与 history 继续共享同一个 Claude 会话目录视图，不能因为切换 `ClaudeProfileID` 把同一组 session 分裂成多套。
10. Claude `/reasoning` 当前也已并入统一的 dispatch 前 preflight，而不再只是 surface 上的 UI 值：
   1. 当前 turn 与已冻结 queue item 不会被 `/reasoning` 回改。
   2. Claude 可选档位是 `low / medium / high / max / clear`；`xhigh` 只属于 Codex/VS Code 投影，Claude 下会被拒绝。
   3. `workspace+ClaudeProfileID` 快照会保存 Claude reasoning override，`/reasoning clear` 会同步删除空快照；fresh workspace 与 concrete thread restore 都在生成 `PendingHeadless` 和 daemon start command 前先恢复快照。
   4. 普通 queue dispatch、auto-continue 与 review apply 都会在真正 `prompt.send` 前，用各自 frozen override 生成 `desired headless launch contract`，再与 wrapper hello 上报的 observed runtime contract 比较。
   5. 合同一致时直接发送；不一致时会进入 `PendingHeadless(Purpose=prompt_dispatch_restart)`，先显式 kill 当前 managed headless，再 fresh-start 匹配 reasoning 的新实例。
   6. 新实例连回后只做最小 reattach，不会重置 queue、review session 或 auto-continue runtime；统一 dispatch owner 会继续把原始 prompt 发出去。
   7. `/access` 与 `/plan` 仍保留动态 permission-mode 通道，不并入这条 restart-only 合同。
   8. `/model` 在 Claude 模式下 hidden + reject；Claude 模型只来自 profile 注入，Codex 默认模型与模型覆盖不会投影成 Claude prompt 配置。
11. `/codexprovider` 当前是 Codex headless 专属的 surface 级切换入口：
   1. bare `/codexprovider` 会打开 dropdown 参数卡，候选固定为内置 `default` 加当前持久化 provider 列表。
   2. 只有 `ProductMode=normal && Backend=codex` 时允许切换；其他 mode/backend 会直接拒绝并要求先 `/mode codex`。
   3. request gate、`PendingHeadless`、live remote work 与 delayed-detach 当前都会阻断 provider 切换，不会让 surface 进入半重启状态。
   4. detached idle surface 切 provider 时，只会替换 `CodexProviderID`，不会偷偷改写其他 surface 参数。
   5. 当前 workspace 已占用时，切 provider 会先 detach-like 清理旧 runtime，再按切换前的 continuation intent 直接 fresh-restart 到新 provider：原来只是 workspace-owned `unbound/new_thread_ready` 时继续走 fresh workspace prepare；原来已经 pinned 到某个 Codex thread 时，会保留该 exact-thread 恢复目标并直接拉起新的 thread-restore headless，而不会要求用户重新 `/use`。
   6. 若切换前当前 surface 接着的正是一个 Codex managed headless，切 provider 时还会先显式 kill 旧 child，再启动新 provider 对应的 child，避免 surface 被错误地重新 attach 回“旧 provider 但同 workspace”的在线实例。

### 4.1.2 `vscode` `/list` 先选 instance，并显式投影“当前实例”

当前 `vscode` 的 `/list` 仍然只列在线 VS Code instance，但卡片展示已经切到 instance-aware 的专用布局。

对应实现里：

1. `presentInstanceSelection()` 只保留在线且 `source=vscode` 的实例，不再夹带 headless。
2. Feishu 卡片当前走专用 `grouped_attach_instance` 布局，不再复用旧的通用 selection 模板。
   1. 若 surface 当前已 attach instance，会先在顶部投影“当前实例”摘要，格式为 `实例标签 + 当前跟随状态`，并附带“换实例才用 /list”的短提示。
   2. 当前实例不会再混进下面的可点击列表。
   3. 其他实例按“可接管 / 其他状态”分组，按钮使用全宽动作前缀文案，例如 `接管 · web`、`切换 · admin`、`不可接管 · ops`。
   4. 每个实例的第二行状态压缩为短元信息，例如 `2分前 · 当前焦点可跟随`、`1小时前 · 等待 VS Code 焦点`、`30分前 · 当前被其他飞书会话接管`。
   5. 组内排序优先 `ObservedFocusedThreadID` 非空的实例，再按该实例可见 thread 的最近活跃时间倒序；无时间时再回退到 `InstanceID`。
3. 卡片按钮仍走 `attach_instance -> ActionAttachInstance`。
4. attach / switch 成功后，surface 仍会进入既有的 follow-local 语义：有 observed focus 时进入 `R4 FollowBound`，否则进入 `R3 FollowWaiting`。
5. 若 `attach_instance` 来自 stamped 菜单卡 callback，attach 成功 / 失败结果会直接替换当前实例选择卡；若同一动作后面还带 thread-selection follow-up，daemon 会抑制这张重复 append，避免菜单卡已经收口后又补第二张卡。

### 4.1.3 vscode `/use` / `/useall` 仍是 instance-scoped thread 选择，但菜单路径会把结果留在原卡

当前 `vscode` 的 `/use` / `/useall` 产品语义没有放宽，仍然只围绕当前 attached VS Code instance 的 thread 集合展开。

对应实现里：

1. detached `/use` / `/useall` 仍直接拒绝，并提示先 `/list` 选择一个 VS Code 实例；若入口来自 stamped 菜单卡，这张提示卡会直接替换当前菜单卡，不再外跳提交态锚点。
2. attached `/use` 当前显示当前实例最近 5 个 thread 的 dropdown；`/useall` 显示当前实例全部 thread 的 dropdown。
3. dropdown 当前会直接过滤掉不可切换 thread，不再把它们作为 disabled 选项留在卡面里；若发生过滤，卡片正文会追加 plain-text 提示。
4. thread 选择仍走 `use_thread -> ActionUseThread`；只是 Feishu 投影从旧按钮/分页 prompt 收敛成当前实例内的结构化 dropdown。
5. 选择 thread 后，same-thread / busy / attach-known-thread / visible-thread 切换等既有产品语义保持不变；但若入口来自 stamped 菜单卡，首张可投影结果卡会继续替回当前菜单卡，不再额外 append 一张 detached notice 或“命令已提交”锚点卡。

### 4.1.4 stamped `/mode vscode` 与 `/vscode-migrate` 的 owner-card 收口边界

这轮实现没有改 `vscode` 兼容性检查本身的产品语义，只改了它在 card callback 场景下的承接 carrier。

对应实现里：

1. 若 `/mode vscode` 来自带 `daemon_lifecycle_id` 的当前参数卡 / 菜单卡 callback，daemon 会在切换成功后立即失效旧缓存，并对 VS Code 兼容性做一次同步判定。
2. 若这次同步判定发现 legacy `editor_settings` 且已存在可接管入口，daemon 会先同步静默自动迁到 `managed_shim`，不再先弹“确认迁移”主提示卡。
3. 若自动迁移成功且不再残留兼容性问题，后续继续走原有 `open VS Code` / recover 流；若缺 target、自动迁移失败、迁移后状态仍异常，或本来就是 stale managed shim 修复，daemon 才会把首张可投影提示卡直接替换当前卡，而不是再走独立 runtime notice / catalog。
4. 若 `/mode vscode` 是纯文本 slash 入口，仍保持原来的异步检测与提示语义，不把普通文本入口升级成 current-card replace。
5. `/vscode-migrate` 当前会先进入 `ActionVSCodeMigrateCommand -> DaemonCommandVSCodeMigrateCommand`，打开同一套 VS Code 迁移 page root；若入口来自 stamped current-card callback，root page / 校验失败页会直接同位替回当前卡。
6. 真正执行迁移的按钮当前不再发旧的文本重解析回调，而是显式发 `vscode_migrate_owner_flow -> ActionVSCodeMigrate`；迁移结果与后续 guidance 会继续 patch 在同一张 guidance card 上。

### 4.2 thread claim 仍是全局的，但在 headless 主链下退回 workspace 内仲裁

当前 `threadClaims` 仍按 `threadID` 做全局仲裁。

结果：

1. 一个 thread 同时只能被一个飞书 surface 占有。
2. headless 主链下，如果目标 thread 所在 workspace 已被其他 headless surface 占有，会先在 workspace 层被禁用，不再进入 thread kick 逻辑。
3. `/use` 命中已被他人占用的 thread 时：
   1. 如果目标 thread 在**当前 attached instance 内可见，且仍属于该 instance 当前 workspace**，仍保留现有强踢逻辑：
      1. 对方 idle 才会弹强踢确认。
      2. 对方 queued/running 会直接拒绝。
   2. 如果目标 thread 走的是 global thread-first attach 路径，不提供强踢，只会在列表里显示 busy 并禁用。

### 4.3 `PendingHeadless` 仍是 dominant gate

只要 `PendingHeadless != nil`：

1. 允许：`/status`、`/autowhip`、`/autocontinue`、`/debug`、`/upgrade`、`/mode`、`/detach`、消息撤回、reaction。
2. 其余 surface action 全部在 `ApplySurfaceAction()` 顶层被拦截。

这意味着：

1. `starting` 时不能旁路 attach/use/follow/new。
2. detached `/use` 触发的 preselected headless，在实例连上后会直接落到目标 thread，不会再进入手工 selecting。
3. `/mode vscode` 与 `/detach` 都会主动取消当前恢复流程，并回到 detached 态；此外还有启动超时 watchdog。
4. `PendingHeadless` 当前有三类产品语义：
   1. `Purpose=thread_restore`：显式 `/use` 一个需要后台恢复的 thread，或 auto-restore。
   2. `Purpose=fresh_workspace`：`/workspace new dir` 流程选了一个当前没有可复用实例的目录。
   3. `Purpose=prompt_dispatch_restart`：Claude queue / auto-continue / review apply 在 dispatch 前发现 frozen reasoning 与当前 runtime contract 不一致，需要先 restart 成匹配实例。
5. 旧 `/newinstance`、旧 `/killinstance` 当前都不再进入 parser；若实例连上时读到历史兼容残留的 pending headless，只会自动结束并提示改用 `/use` / `/useall`。
6. 后台 auto-restore 触发的 pending headless 也复用同一个 `G1` gate：
   1. 启动阶段默认静默，不额外发 “headless_starting”。
   2. 成功后只发一条恢复成功 notice。
   3. 失败或超时后只发一条恢复失败 notice，并回到 `R0 Detached`。
7. `PendingHeadless.AutoRestore=true` 时，手动 `/upgrade latest` 与允许 dev feed 的 flavor（源码 `dev` 与 release `alpha`）下的 `/upgrade dev` 检查结果 prompt 不再因为这条后台恢复占位被判成“当前窗口不空闲”；自动升级提示仍保持保守，不会优先挑这种 surface 弹卡。
8. `Purpose=prompt_dispatch_restart` 的 attach 完成后不会重走 fresh workspace / exact-thread restore 的大路径；surface 只做最小 reattach，然后由统一 dispatch owner 继续原本那条 queued 或 auto-continue 发送，避免在“切推理强度”时把 queue/runtime 状态清空。

### 4.4 选择卡片不再是服务端持久 modal 状态

当前服务端已经不再保存 `FeishuDirectSelectionPrompt` 状态，也不再把“纯数字文本”解释成选择。

当前行为：

1. attach/use/kick confirm 都改成**直达动作**。
2. Feishu 卡片按钮直接携带：
   1. `attach_workspace`
   2. `attach_instance`
   3. `use_thread`
   4. `show_scoped_threads`
   5. `show_workspace_threads`
   6. `show_all_threads`
   7. `show_all_thread_workspaces`
   8. `show_recent_thread_workspaces`
   9. `kick_thread_confirm`
   10. `kick_thread_cancel`
3. `use_thread` 会按卡片来源附带额外上下文：
   1. `codex headless` `/useall` 与 detached/global `/use` 会携带 `allow_cross_workspace=true`
   2. attached current-scope `/use` 不会带这个标记，因此仍只允许留在当前 workspace / 当前 instance 内
5. `"1"`、`"2"` 这类纯数字文本现在就是普通文本。

### 4.5 route change 与 `/new` 都会显式处理未发送草稿

当前有两类固定规则：

1. 普通 route change，例如 `/use`、`vscode /follow`、follow 自动切换、claim 丢失回退：
   1. 丢 staged image 与 staged file。
   2. 不会静默把未冻结附件串到新 thread。
2. clear 语义，例如 `/stop`、`/detach`、`/mode`、`/new`、`R5` 下的 `/use` / `vscode /follow`：
   1. staged image / staged file 和 queued draft 都会被显式丢弃。
   2. 会发 discard reaction / notice。

当前实现不允许未发送草稿在 route change 时 silently retarget。

### 4.6 queued 点赞 / reply 命中 processing source 的 steering 只升级当前输入，不做隐式重排

当前 steering 入口的产品语义已经固定：

1. queued 点赞入口：
   1. 只有 `ThumbsUp` 才会触发。
   2. 只有 queued item 的主文本 `SourceMessageID` 能触发。
   3. 图片消息上的点赞不会单独触发任何状态迁移。
2. reply 自动 steering 入口：
   1. 只有 reply 目标命中**当前 surface 正在 processing 的 source message**时才会触发。
   2. 必须命中当前 surface 自己的 active running turn；仅 instance 有 active turn 但 surface 不拥有该 running item 时不会触发。
   3. 当前只支持文本 / 本地图片内容；被 reply 的原消息不会再作为 quoted input 重新 steer 进去。
3. 无论哪种入口：
   1. 目标 item / reply fallback item 都必须和当前 active running turn 属于同一 `FrozenThreadID`。
   2. 命中后不会改写其他 queued item 的相对顺序，也不会跨 thread 偷偷 retarget。
   3. steering 失败时，目标输入必须恢复回普通语义，不能 silently 消失。
   4. 图片 reply 回退成 staged image 时，必须保留原始发送者归属；后续仍只允许同一个 actor 的下一条文本消费它，不能被别的 actor 抢绑。

### 4.7 `R5 NewThreadReady` 是稳定态，不是半成品

当前 `/new` 已实现为 clear-and-prepare：

1. headless 主链下，只要 surface 已 attach 且当前 workspace 已知，就允许进入。
2. `vscode` 下，`/new` 直接拒绝，并明确提示用户先 `/mode codex` 或 `/mode claude`，或继续 follow / `/use` 当前 VS Code 会话。
3. 不允许 fallback 到 home。
4. 进入时会释放旧 thread claim，但保留 instance attachment 与 `PromptOverride`。
5. `PreparedThreadCWD`、`PreparedFromThreadID`、`PreparedAt` 会显式保存。
6. 若 surface 处于空闲 detached review session，进入 `/new` 会同时清掉 `ReviewSession`；若 review turn 仍在运行，则 `/new` 会拒绝并提示等待或 `/stop`，避免新会话首条文本被旧 review overlay 截获。

这带来三个关键性质：

1. `R5` 没有“attach 成功但用户无路可走”的问题。
2. `R5` 下第一条普通文本合法，且会创建新 thread。
3. `R5` 下如果只有 staged/queued draft，用户仍然能 `/use`、`/detach`、`/stop` 或重复 `/new`。

### 4.8 空 thread turn 不再靠 `ActiveThreadID` 猜归属

当前 empty-thread 首条消息的 turn 归属已经改成显式相关性：

1. queue item 仍以 `FrozenThreadID == ""` 派发。
2. translator 在 `turn.started` 时提供 `InitiatorRemoteSurface + SurfaceSessionID`。
3. orchestrator 优先用 `Initiator.SurfaceSessionID` 命中 pending remote item。
4. 命中后会先 materialize 这个新 thread 的最小运行时元数据：至少把真实 `threadID`、继承的 `cwd` 与 primary traffic class 写进 state，然后再把 surface 从 `R5` 切回 `R2 AttachedPinned`。

当前不再用“`FrozenThreadID == ""` 时退化匹配 `inst.ActiveThreadID`”来猜归属。

### 4.9 local-activity `PausedForLocal` 和 `Abandoning` 都有 watchdog

当前 `Tick()` 已经提供两类 watchdog 恢复：

1. local-activity 来源的 `paused_for_local` 超时后：
   1. 自动回到 `normal`
   2. 发 `local_activity_watchdog_resumed`
   3. 继续 `dispatchNext`
2. `abandoning` 超时后：
   1. 强制 `finalizeDetachedSurface`
   2. 发 `detach_timeout_forced`

补充说明：

1. 这条 watchdog 只覆盖 `pauseForLocal(...)` 写入的 local-activity 分支。
2. standalone Codex 升级事务复用 `DispatchMode=paused_for_local` 时，不会写 `pausedUntil`，因此不会被 `Tick()` 自动恢复；这条路径必须等待 daemon 显式 `ResumeSurfaceDispatch(...)`。
3. `Abandoning` 仍保持原来的 watchdog 语义。

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
8. 后台 managed-headless exact-thread resume attach 是明确例外：
   1. 不会补发旧 replay。
   2. 会直接清空该 thread 的旧 replay。
   3. 用户只会看到一条新的恢复成功提示。

### 4.11 `/status` 当前至少会显式投影 mode / profile / attach object / gate / dispatch / retained-offline

当前 `Snapshot` 不再只展示 attachment 和 next prompt。

现在至少会额外投影九类“决定下一条输入会发生什么”的状态：

1. 当前 `ProductMode`
   1. `normal`
   2. `vscode`
2. 当前 Claude profile（仅 `Backend=claude` 时）
   1. 只读展示当前 `ClaudeProfileName / ClaudeProfileID`
   2. 不承担切换或管理入口
3. 当前 attach 对象类型
   1. `工作区`
   2. `VS Code 实例`
   3. `headless 实例`
   4. `实例`
4. 当前已占用的 workspace（若有）
5. request gate：
   1. `PendingRequest`
   2. `RequestCapture`
   3. active path picker runtime
6. dispatch / queue：
   1. `Dispatching`
   2. `Running`
   3. `PausedForLocal`
   4. `HandoffWait`
   5. queued count
7. autowhip runtime：
   1. enabled / disabled
   2. pending reason
   3. pending due time
   4. consecutive count
8. autoContinue runtime：
   1. enabled / disabled
   2. current episode state
   3. pending due time
   4. attempt count / consecutive dry failure count
9. transport degraded 后“attachment 仍保留但实例已离线”的 retained-offline 状态。

它仍然不是完整调试面板，但已经能回答最关键的问题：

1. 当前到底记住的是 `normal` 还是 `vscode`。
2. Claude 当前到底是哪个 profile。
3. 当前接管的是工作区、VS Code 实例，还是 headless/其他实例。
4. 当前到底占着哪个 workspace。
5. 下一条文本是不是会先被 request gate 吃掉。
6. 下一条文本是不是会先被 legacy `/model` capture 兼容态吃掉。
7. 现在是执行中、排队中，还是被本地 VS Code 暂停。
8. autowhip 当前是关闭、待触发，还是刚因 backoff 暂缓。
9. autoContinue 当前是 idle、等待自动继续，还是刚刚失败/取消。
10. attachment 还在不在，以及当前是不是在等实例恢复。

### 4.12 `/mode` 是 surface 级 overlay，当前只负责记忆与清理切换

当前 `/mode` 的实现边界已经固定为：

1. bare `/mode` 当前不再直接回 `Snapshot`，而是返回当前模式 + `codex` / `claude` / `vscode` 切换卡；其中 `normal` 仍只作为 `codex` 的兼容 slash alias。
2. `/mode normal|codex|claude|vscode` 允许在 detached、idle attached、或 `PendingHeadless` 尚未进入 live remote work 的 surface 上切换。
3. 切换时一定先做 detach-like 清理；大多数目标会进入 detached 态，但若切到 `claude` 且切换前已有当前 workspace，则会保留该 workspace claim，并立即进入“attach 已在线 Claude instance”或“启动 fresh Claude headless”的后续链路，而不是停在 detached idle。
4. 若切换前存在 `PendingHeadless` 或 `surface resume state` 里仍带着 headless 恢复目标，会一并 kill / clear，避免 mode 切完以后又被后台恢复拉回 headless。
5. `vscode` surface 不参与 managed-headless exact-thread continuation；而且 `surface resume state` 会把非 headless entry（当前即 `ProductMode!=normal`）的 `ResumeHeadless` 硬归零，避免 daemon 重启后从持久化状态重新长出这条恢复入口。
6. 当前 mode 会跨 daemon 重启保留：
   1. startup 会先恢复 latent surface、`ProductMode` 与 `Verbosity`
   2. headless 主链会继续按 persisted target 尝试自动恢复：workspace-owned route 直恢复 workspace intent；thread target 先 exact visible attach；`ResumeHeadless=true` 时再继续 exact-thread continuation
   3. 若存在 `ResumeThreadID`，在首轮 `threads.refresh -> threads.snapshot` 完成前会先静默等待，不会过早降级或直接报失败
   4. `vscode` 会按 exact `ResumeInstanceID` 尝试恢复：恢复成功后回到 `follow_local`，若暂时缺少新的 VS Code 活动则明确提示用户去 VS Code 再说一句话或手动 `/use`
7. 切换当前已经会改变 `/list` 的主交互语义：
   1. `codex` 与 `claude` 下 `/list` 都是 workspace chooser。
   2. `vscode` 下 `/list` 是 instance chooser。
8. headless 主链下 `/follow` 已退出长期路径；`vscode` 当前则固定走 follow-first，并把 `/use` 收窄到当前 instance 内的一次性 force-pick。
9. 若当前仍有 running / dispatching / queued work，则 `/mode` 会直接拒绝，而不是进入半切换状态。

### 4.13 `/autowhip` 是 surface 级、内存态、跨 route 可查询的 overlay 开关

当前 `/autowhip` 不要求 surface 已 attach：

1. detached surface 也可以直接 bare `/autowhip` 查询并打开 on/off 参数卡；带参数时可直接切换。
2. `PendingHeadless` 期间 `/autowhip` 仍然允许，不会被顶层 gate 挡住。
3. `Abandoning` 期间 `/autowhip` 也仍然允许，用户可以查看或关闭当前 surface 的 autowhip。
4. daemon 重启后不恢复该开关；当前已接受这是内存态语义。
5. 旧命令 alias 已移除；主展示与实际命令统一只保留 `/autowhip`。

### 4.14 `/autocontinue` 是 surface 级、内存态、跨 route 可查询的自动继续开关

当前 `/autocontinue` 不要求 surface 已 attach：

1. detached surface 也可以直接 bare `/autocontinue` 查询并打开 on/off 参数卡；带参数时可直接切换。
2. `PendingHeadless` 期间 `/autocontinue` 仍然允许，不会被顶层 gate 挡住。
3. `Abandoning` 期间 `/autocontinue` 也仍然允许，用户可以查看或关闭当前 surface 的 autoContinue。
4. daemon 重启后不恢复该开关；当前已接受这是内存态语义。
5. 旧 `recovery` / `autorecovery` alias 已移除，UI、文档与解析器统一只保留 `/autocontinue`。
6. `/autocontinue` 只影响上游可重试失败自动继续，不影响 `autowhip` 的 `incomplete_stop` 语义。

### 4.15 `/menu` 现在是阶段感知首页，不再是静态平铺目录

当前 `/menu`、静态 bot 菜单和 slash parser 已经统一到同一套 canonical command metadata。

当前行为：

1. `/menu` 首页当前只保留分组导航，不再在首页额外平铺“常用操作”或“前排固定命令”：
   1. `基本命令`
   2. `参数设置`
   3. `工作会话`
   4. `常用工具`
   5. `系统管理`
2. 二级分组顺序稳定，但组内可见命令会按当前 `product mode + menu stage` 做 display projection：
   1. `codex` 的菜单首页点击 `工作会话` 时，不再进入旧的命令分组页，而是直接打开 bare `/workspace` 父页。
   2. bare `/workspace` 当前固定展示四个并列入口：`切换`、`从目录新建`、`从 GIT URL 新建`、`解除接管`。
   3. bare `/workspace new` 当前是单独的新建方式页，展示 `从目录新建`、`从 GIT URL 新建` 与 `从 Worktree 新建`。
   4. `codex` 下 `/list`、`/use`、`/useall`、`/detach` 不再作为主展示菜单项，但 alias / parser 兼容仍保留，并分别汇合到 `/workspace list` 与 `/workspace detach`。
   5. `claude` 下 `工作会话` 分组当前直接显示 `/new`、`/status`、`/detach`，`switch_target` 分组直接显示 `/list`、`/use`；`workspace*`、`/useall`、`/review` 与 `/bendtomywill` 不再出现在主展示菜单里。
   6. `vscode` 的 `工作会话` 仍分别显示 `/list`、`/use`、`/useall`。
   7. headless 主链不展示 `/follow`；`vscode` 才展示 `/follow`。
   8. `/new` 只在 headless working 可见；`/status` 当前在 `基本命令`，`/history` 在 `常用工具`，其中 `/status` 与 `/history` 在 headless / vscode 都可见；对 `claude`，`/detach` 当前也进入 `工作会话` 分组，而 `/review` / `/bendtomywill` 已退出主展示面。
3. `/help` 当前也复用同一套 display projection：
   1. `codex` 下帮助文本里的主展示入口已经切到 `workspace` 命令族：`/workspace`、`/workspace list`、`/workspace new`、`/workspace new dir`、`/workspace new git`、`/workspace new worktree`、`/workspace detach`。
   2. `claude` 下帮助文本的主展示入口当前会保留 `/new`、`/list`、`/use`、`/detach` 这组 visible MVP 会话主链，并在 `常用工具` 展示 `/history` 与 `/sendfile`；`/review`、`/bendtomywill` 仍只存在于 parser / reject 兼容层，不作为主展示帮助入口；`workspace*` 与 `/useall` 是 hidden + allow 兼容入口，也不作为主展示帮助入口。
   3. `vscode` 下帮助文本仍保留 `/list`、`/use`、`/useall` 三个独立入口。
4. bare 参数命令现在统一走“快捷按钮 + 单字段表单”：
  1. `参数设置`：`/reasoning`、`/model`、`/access`、`/plan`、`/verbose`、`/autocontinue`
  2. `常用工具 / 系统管理`：`/autowhip`、`/mode`
   3. 表单提交通过 card callback `page_submit` 直接回填结构化 `action_kind/field_name/action_arg_prefix`，再生成 canonical `Action.Text`。
5. `常用工具` 分组里的 `/cron`、`/bendtomywill`，以及 `系统管理` 分组里的 `/debug`、`/upgrade` 当前仍然是直接触发 daemon 动作的命令入口，不属于参数卡表单。
7. 二级分组当前通过卡片按钮 + breadcrumb 返回首页实现，不依赖飞书后台把整棵导航树都铺成静态菜单。
8. 同上下文菜单导航当前已经支持“替换当前卡片”而不是追加新卡，但只限窄范围：
   1. `/menu` 首页 <-> 二级分组页
  2. 从 `/menu` 分组页打开 bare `/mode`、`/autowhip`、`/autocontinue`、`/reasoning`、`/access`、`/plan`、`/model`、`/verbose`
   3. bare 参数卡里的“返回上一层”
9. 这条原地替换链路当前只在动作来自带 `CardDaemonLifecycleID` 的飞书卡片时启用：
   1. 网关通过 card callback 同步回包返回替换后的整张卡
   2. 同样的命令如果由 slash 文本或飞书后台 bot 菜单触发，仍按普通 append-only UIEvent 新发卡片
   3. `/help`、result/notice 类卡片不参与这条导航替换语义

### 4.16 autowhip 调度只允许走显式 reply-anchor，不再伪造用户消息 pending/typing

当前 autowhip queue item 仍沿用显式来源类型：

1. `SourceKind=user`
2. `SourceKind=auto_whip`

当前行为已经固定为：

1. 普通用户输入 item：
   1. `SourceMessageID` / `SourceMessageIDs` 用于 pending、typing、revoke、reaction 投影。
   2. 最终回复默认 reply 到同一条原用户消息。
2. autowhip item：
   1. `SourceMessageID` 为空，不再触发 pending / typing / thumbs projection。
   2. `ReplyToMessageID` 单独保留原用户消息锚点。
   3. 最终回复继续 reply 到原用户消息。

### 4.17 autoContinue 调度走独立 queue lane，不借用状态卡 message id

当前 autoContinue queue item 也沿用显式来源类型：

1. `SourceKind=auto_continue`
2. `ReplyToMessageID` 固定保留原始用户消息锚点
3. `AutoContinueEpisodeID` 独立标识当前 autoContinue episode

当前行为已经固定为：

1. autoContinue 状态卡：
   1. 首次发送显式走 reply-thread lane
   2. 当前只在自己仍是尾消息时允许 patch
   3. 一旦尾部已被后续消息占用，旧卡冻结；后续状态改为 append 新卡
2. autoContinue item：
   1. 不会把 autoContinue 状态卡自身的 `message_id` 反写成后续业务输出的 reply anchor
   2. 真正的 final / request / plan / image / progress 输出仍 reply 到原用户消息
3. dispatch 优先级高于普通 queued user item，但不会清空原队列

### 4.18 `/bendtomywill` 是 headless 当前 thread 的前台事务卡，不回改已展示旧消息

当前 `/bendtomywill` 的实现边界已经固定为：

1. 入口与适用面：
   1. bare `/bendtomywill` 与菜单里的 `修补当前会话` 是同一条 daemon-side 流程。
   2. 只允许在 `headless + attached instance + selected thread` 下打开。
   3. VS Code surface、未 attached surface、未选 thread、实例离线，或未启用 patch storage 时都会直接拒绝。
2. 打开 patch 卡前的预检：
   1. 当前 instance 必须空闲。
   2. 除了自身正在编辑的 patch 卡外，不能存在 active turn-patch tx、upgrade tx、upgrade owner-flow、active remote、pending remote、compact、steer、pending request、request capture、queued item、非 `normal` dispatch mode、`PendingHeadless` 或 `Abandoning`。
   3. 这使 `/bendtomywill` 成为强互斥的高风险事务入口，不走排队。
3. 编辑阶段：
   1. daemon 会从 rollout truth 读取当前 thread 的最新 completed assistant turn 预览，而不是改已展示消息。
   2. 当前只对显式 refusal / placeholder 模式做候选点检测；若没有命中，会直接返回 notice，不打开卡。
   3. 命中后会打开一张复用 `request_user_input` 载体的多题 patch 卡：每题只展示命中片段摘录与预填模板，不展示全文。
   4. 同一张卡会按 `request_revision` 逐题 inline 刷新；只有发起者本人可以继续回答或取消。
   5. 编辑期间当前 surface 进入 `G8 TurnPatchEditing`：除同一张卡的 `request_respond` / `request_control` 与 reaction/recall 外，其它动作都会被挡住。
4. apply 事务：
   1. 全部题目确认后，daemon 会再次确认当前 attached instance / thread 没有漂移。
   2. 事务开始时会对同一 instance 上全部 attached surface 调 `PauseSurfaceDispatch(...)`，因此这些 surface 都会表现成 `E4 PausedForLocal`；但中间 child stop/start 噪音不会对上层额外翻译成 offline/online 提示。
   3. 存储层只写 rollout JSONL：会先做 digest/turn 校验、写备份、记录 latest-only rollback ledger，再替换 latest assistant turn 命中的 message，并同步清掉该 turn 的 reasoning line。
   4. 写盘完成后 daemon 会发送 `process.child.restart`，并通过 shared restart waiter 继续等待两段式结果：
      1. `ack=true` 只代表新 child 已成功拉起并接管实例。
      2. 只有后续收到匹配的 `process.child.restart.updated(status=succeeded)`，这次 apply 才算真正成功，前台才会切到 patch 成功页。
      3. wrapper 会对 child stdout 做 generation fence，因此旧代 child 的晚到 restore/输出不会再回头污染当前 patch 事务。
   5. 若 launch ack 被拒绝、restore outcome 明确失败，或 daemon 侧等待 outcome 超时，daemon 会先自动把 rollout 回滚到备份，再发第二次 child restart 尝试恢复运行态；失败页会明确区分“修补未生效，磁盘已恢复”与“运行态恢复也失败”。
5. rollback 事务：
   1. rollback 入口支持 `/bendtomywill rollback [patch_id]`，以及 patch 成功页上的回滚按钮。
   2. 只允许回滚同一 thread 最近一次 patch；若 latest pointer 已变化，或 patch 后 rollout digest 已漂移，就会拒绝回滚。
   3. rollback 同样要求原发起者、当前 attached thread 与 instance 仍然匹配，并复用与 apply 同级别的 dispatch freeze + child restart。
6. 用户可见语义：
   1. patch 与 rollback 成功后，后续输入会继续落在同一 thread。
   2. 已经发出去的旧消息不会被回改；patch 只影响后续上下文。
   3. 成功页会保留最近一次 rollback 按钮；失败页不会留下半活跃事务。

## 5. 主要状态迁移

### 5.1 attach / use / follow / new

```text
R0 Detached
  -- /list(headless) --> 保持 R0 Detached，打开 target picker
  -- /use(headless) --> 保持 R0 Detached，打开 target picker
  -- /useall(headless substrate) --> 保持 R0 Detached，打开 target picker
  -- target picker confirm(thread，headless 且可解析到当前可用实例) --> R2 AttachedPinned
  -- target picker confirm(thread，headless 且需要新 headless) --> R0 + G1 PendingHeadlessStarting
  -- target picker confirm(new_thread，headless 且 workspace 可直接 attach) --> R5 NewThreadReady
  -- target picker confirm(new_thread，headless 且 workspace 仅 recoverable-only) --> R0 + G1 PendingHeadlessStarting
  -- /list -> attach_instance(vscode 且 observed focus 可接管) --> R4 FollowBound
  -- /list -> attach_instance(vscode 且尚无可接管 observed focus) --> R3 FollowWaiting
  -- /use(thread，vscode) --> 拒绝 + migration to /list
  -- daemon startup latent headless surface + exact visible thread restore --> R2 AttachedPinned
  -- daemon startup latent headless surface + workspace fallback --> R1 AttachedUnbound
  -- daemon startup latent headless surface + waiting first refresh --> 保持 R0 Detached
  -- daemon startup latent vscode surface + exact instance resume --> R3 FollowWaiting 或 R4 FollowBound

R1 AttachedUnbound
  -- 普通文本(headless，workspace 已知) --> 隐式进入 R5 并立刻消费首条文本（R5 + E1/E2）
  -- 图片消息(headless，workspace 已知) --> 隐式进入 R5，并先停留在 D1 StagedImages（不会仅凭图片创建新 thread）
  -- 文件消息(headless，workspace 已知) --> 隐式进入 R5，并先停留在 D1 StagedAttachments（不会仅凭文件创建新 thread）
  -- /list(headless) --> 保持 R1 AttachedUnbound，打开 target picker
  -- /use(headless) --> 保持 R1 AttachedUnbound，打开 target picker（默认当前 workspace）
  -- /useall(headless substrate) --> 保持 R1 AttachedUnbound，打开 target picker（允许跨 workspace）
  -- target picker confirm(thread，同/跨 workspace) --> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- target picker confirm(new_thread，当前/其它可接管 workspace) --> R5 NewThreadReady
  -- /follow(vscode) --> R4 FollowBound 或 R3 FollowWaiting
  -- /follow(headless) --> 拒绝 + migration notice
  -- /new(headless，workspace 已知) --> R5 NewThreadReady
  -- /detach --> R0 Detached

R2 AttachedPinned
  -- /list(headless) --> 保持 R2 AttachedPinned，打开 target picker
  -- /use(headless) --> 保持 R2 AttachedPinned，打开 target picker（默认当前 workspace）
  -- /useall(headless substrate) --> 保持 R2 AttachedPinned，打开 target picker（允许跨 workspace）
  -- target picker confirm(other thread，同/跨 workspace) --> R2 AttachedPinned 或 G1 PendingHeadlessStarting
  -- target picker confirm(new_thread，同/跨 workspace) --> R5 NewThreadReady 或 G1 PendingHeadlessStarting
  -- /follow(vscode) --> R4 FollowBound 或 R3 FollowWaiting
  -- /follow(headless) --> 拒绝 + migration notice
  -- /new(headless 且无 live remote work，workspace 已知) --> R5 NewThreadReady
  -- selected thread claim 丢失 --> R1 AttachedUnbound 或 R3 FollowWaiting(vscode)
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
  -- /new --> 拒绝 + 提示先 `/mode codex` 或 `/mode claude`，或继续 follow / `/use`
  -- /detach(no live work) --> R0 Detached
  -- /detach(live work) --> E6 Abandoning -> R0 Detached

R5 NewThreadReady
  -- 第一条普通文本 --> R5 + E1/E2，等待新 thread 落地
  -- turn.started(remote_surface，新 thread) --> 保持 R5，turn 进入 running，但 surface 仍停留在 workspace-owned prepared state
  -- 首轮 turn.completed(success，新 thread 已权威建立) --> R2 AttachedPinned
  -- 首轮 turn.completed(任意终态，且新 thread 已权威建立) --> R2 AttachedPinned；若失败则同时展示对应 failure notice
  -- /list(headless) --> 保持 R5 NewThreadReady，打开 target picker
  -- /use / /useall(headless substrate) 且仅有 staged/queued draft --> 打开 target picker；confirm 后 discard drafts + 切换或重新准备
  -- /use / /useall(headless substrate) 且首条消息已 dispatching/running --> 打开 target picker；confirm 时拒绝 route exit
  -- /follow(headless) --> 拒绝 + migration notice
  -- 重复 /new 且无 draft --> 保持 R5，仅回 already_new_thread_ready
  -- 重复 /new 且仅有 staged/queued draft --> discard drafts，保持 R5
  -- dispatch / command reject / thread-start reject / runtime fail(新 thread 尚未权威建立) --> 保持 R5
  -- /detach(no live work 或仅 unsent draft) --> R0 Detached
  -- /detach(dispatching/running 首条消息) --> E6 Abandoning -> R0 Detached
```

补充说明：

1. `R5` 下首条文本 queued 后，第二条文本、新图片与新文件都会被拒绝，直到该新 thread 真正落地。
2. `R5` 的 surface 提交点不再是 `turn.started`，而是“新 thread 的权威身份已经建立并且本轮 bootstrap 已经完成提交”。
   这里的“已建立”当前不仅指拿到 `threadID`，还要求 daemon 侧已经把后续继续发送所需的最小 thread 元数据（尤其是继承 `cwd`）一起落进 state；否则 surface 不会停在一个“看起来 pinned 但下一条文本仍因为缺 cwd 被拒绝”的半绑定状态。
3. 这意味着 `turn.started` 期间 surface 仍可能显示为 `new_thread_ready`；这是当前实现刻意保留的 bootstrap overlay，而不是卡死状态。
4. 若首轮失败时 durable thread 尚未建立，surface 会自动回到可重试的 `R5`；下一条文本会再次尝试 create-thread。
5. 若 durable thread 已建立但本轮仍失败，例如 `thread/start` 成功后 `turn/start` 被拒绝，则 surface 会提交到新 thread，并在该 thread 上展示失败。
6. 若是在 `R1 AttachedUnbound` 下先发图片或文件，当前实现会先隐式进入 `R5` 并把附件保留为 staged；随后第一条文本会按“新 thread 首条输入”把 staged image / staged file + 文本一起发送。
7. `R5` 下 `/use`、`/follow` 只会在首条消息已 `dispatching/running` 时被拒绝；若只是 staged/queued draft，会先丢弃再切走。
8. `/attach` 或 `/use` 进入某个已选 thread 后，还会执行一次 thread replay 检查：
   1. 该 thread idle 且存在 `UndeliveredReplay` 时，会立刻补发并清空。
   2. 该 thread busy 时不会插入旧 final/旧 notice，候选保留到后续 idle 的 `/attach` 或 `/use`。
5. headless 主链 `/list` / `/use` / `/useall` 当前共享同一套 workspace candidate / resolver 基础：
   1. workspace 候选来自 runtime 可见 workspace 与 merged recent thread / persisted recent thread 导出的 recoverable workspace。
   2. persisted sqlite 只负责补 freshness，不旁路 resolver；busy / claim / free-visible / reusable-headless / create-headless 仍只由现有 runtime resolver 决定。
   3. sqlite read 失败或 schema 不兼容时，会安全回退到 runtime/catalog-only 行为。
   4. 最终仍会过滤 busy workspace，以及没有任何 merged thread / online instance 支撑的历史脏 workspace key。
6. target picker 当前承担的是 headless 主链下四张独立工作会话业务卡，而不是旧的 unified 大卡：
   1. bare `/workspace` 是父页；bare `/workspace new` 是新建方式子页；它们都走 `FeishuPageView`，不直接承接目标选择。
   2. `/workspace list` 与 alias `/list`、`/use`、`/useall` 当前共用同一张“切换工作会话”卡；attached `/use` 会默认当前 workspace，attached `/useall` 仍允许跨 workspace。
   3. 这张切换卡现在只保留“工作区 + 会话”两个下拉，不再出现模式切换、来源切换，也不再提供 `新建会话`。
   4. `/workspace new dir`、`/workspace new git` 与 `/workspace new worktree` 是三张独立业务卡：前者直接做目录接入，后两者分别做 Git URL 导入与 Worktree 派生；`/workspace new` 只负责把这三条路径并列展示出来。
   5. headless 主链下的 `show_threads` / `show_all_threads` / `show_scoped_threads` / `show_workspace_threads` / `show_all_workspaces` / `show_recent_workspaces` / `show_all_thread_workspaces` / `show_recent_thread_workspaces` 当前都只负责在 same-context 中重新打开或刷新 `/workspace list` 这张切换卡。
   6. `attach unbound`、`selected_thread_lost`、`thread_claim_lost` 当前也会复用这张切换卡，但会锁定在当前 workspace：工作区下拉隐藏、旧跨 workspace 选择会被驳回并刷新提示。
   7. `/workspace list` 当前主路径只会发出 `target_picker_select_workspace` / `target_picker_select_session`。`/workspace new dir` / `git` 会继续使用 `target_picker_open_path_picker` 打开目录子步骤；`/workspace new worktree` 则继续使用 `target_picker_select_workspace` / `target_picker_page` 切换与翻页基准工作区，并在同一张 owner card 内 inline replace 往返。
   8. `target_picker_confirm` 虽然仍是异步产品动作，但四条业务卡都会把 processing / terminal 结果收回同一张 owner card，而不再额外 append 主结果卡。
   9. 若 confirm 时原选择已经失效，当前会刷新一张最新 picker 并返回 `target_picker_selection_changed`，不会 silent fallback 到别的 thread / workspace；锁定当前工作区的恢复卡也遵守同一条规则。
7. target picker confirm 的产品落点当前分三类：
   1. `/workspace list` 既有会话：复用现有 resolver 顺序 `当前 attached instance 内可见 thread -> free existing visible instance -> reusable managed headless -> create managed headless`。
   2. `/workspace list` 既有会话但需要跨 workspace / 跨实例：仍会先走 detach-like 清理，丢弃 staged/queued draft、清 request / capture / prompt override，再 attach 到新目标。
   3. `/workspace new dir`：不会立即改 route，而是先打开目录 path picker；confirm/cancel 回调会先异步 ack，再把最新主卡 patch 回同一张 owner card。只有主卡确认时才真正进入 `R5` / fresh managed headless `R5`，cancel 则保持当前 route 不变。
   4. `/workspace new git`：不会立即改 route，而是在同一张主卡上填写仓库地址/目录名、选择父目录，并由 daemon-side `workspace.git_import` 在持锁外执行 `git clone`；confirm 后 surface 进入 `G5 TargetPickerProcessing`，success / failure / cancel 都封回同卡 terminal，其中 success 最终进入 `R5`。
   5. `/workspace new worktree`：不会立即改 route，而是在同一张主卡上填写基准工作区、新分支名与可选目录名，并由 daemon-side `workspace.git_worktree.create` 在持锁外执行 `git worktree add`；confirm 后 surface 同样进入 `G5 TargetPickerProcessing`，success / failure / cancel 都封回同卡 terminal，其中 success 最终进入 `R5`。
8. attached `vscode /use` / `/useall` 当前有两条额外约束：
   1. 只展示当前 attached instance 的可见 thread，不再走 merged global thread view。
   2. force-pick 后会保留 `RouteMode=follow_local`，后续 observed focus 变化仍可覆盖。
   3. attached `vscode /use` / `/useall` 当前都会在顶部插入一个“当前实例”摘要，格式为 `实例标签 + 当前跟随状态`。
   4. attached `vscode /list` 当前不再走旧 attach-instance prompt，而是直接渲染结构化 instance card：保留按钮式操作，但底层已经不再依赖 `FeishuDirectSelectionPrompt`。
   5. attached `vscode /use` 当前会直接渲染结构化 thread dropdown，只保留最近 5 个可切换会话；选项文案改成 `workspace basename · 首条用户消息摘要`，不再保留旧分页 / “更多”按钮。
   6. attached `vscode /useall` 当前会直接渲染结构化 thread dropdown，并列出当前实例全部可切换会话；不可切换项不再逐条展示，只在卡片下方补一条“已省略当前不可切换的会话”提示。
   7. VS Code thread dropdown 当前直接把 `use_thread(field_name=selection_thread)` callback 发回 gateway；选择动作不再先回投旧 selection prompt 再取按钮 payload。
9. target picker confirm 进入跨 workspace / cross-instance target 时，当前实现仍会先走 detach 语义清理：
   1. queued / staged draft 会被清掉。
   2. `PromptOverride`、pending request、request capture 会被清掉。
   3. 当前 instance claim 会先释放，再 attach 到新目标。
10. 当 surface 处于 `PendingRequest`、`RequestCapture` 或 active path picker runtime 存在时：
   1. same-instance `/use`
   2. `/follow`
   3. follow-local 自动重绑定
   当前都会被冻结，避免 UI 宣布的新目标和下一条普通输入的实际落点不一致。
   4. 若是 active path picker runtime，当前还会额外把 `/list`、`/menu`、bare config cards、`/detach` 等 competing Feishu card flow 一并挡住，只保留 picker 自身回调与 `/status`。
11. 若实例连上时发现历史兼容残留的 pending headless 且没有 preselected thread，只会自动 kill 该 headless、清 gate，并提示改用 `/use` / `/useall`。
12. daemon 侧后台 auto-restore 使用的是 headless-only resolver：
   1. 当前可见 thread 若只存在于 VS Code instance，不会被自动 attach 到 VS Code。
   2. 它仍可复用该 thread 的 metadata / cwd。
   3. 后续只允许落到 free visible headless、reusable managed headless，或 create managed headless。

### 5.2 远端队列与 compact 生命周期

```text
E0 Idle
  -- enqueue --> E1 Queued
  -- dispatchNext --> E2 Dispatching
  -- /compact(当前已绑定 thread，且无 queued/dispatching/running/steering/其他 compact) --> `CompactPending` overlay

E1 Queued
  -- queued 主文本被 `ThumbsUp`，且当前有同 thread active turn --> `SteerPending` overlay
  -- `/steerall` 命中且存在同 thread queued 项 --> `SteerPending` overlay

E2 Dispatching
  -- turn.started(remote_surface) --> E3 Running
  -- command rejected / dispatch failure --> E0 Idle

E3 Running
  -- turn.completed(remote_surface) --> E0 Idle
  -- reply 当前 processing source message（文本 / 本地图片，且命中当前 surface active running item） --> `SteerPending` overlay

`CompactPending` overlay
  -- 显式 `/compact` 已提交 --> 同时创建前台 compact owner-card，首卡阶段为 `dispatching`
  -- command dispatch accepted，等待 compact 对应 `turn.started` --> 保持 `CompactPending`
  -- turn.started(remote_surface，命中当前 compact 请求) --> `CompactRunning` overlay，同时把 compact owner-card patch 到 `running`
  -- command rejected / dispatch failure / `system.error(operation=thread.compact.start)` --> 清 compact overlay，把 compact owner-card patch 到 `failed`，并恢复后续 queue 出队
  -- transport degraded / disconnect --> 清 compact overlay；若 surface 仍在且当前 flow 仍是 compact owner-card，则 best-effort patch 到 `failed`；disconnect 继续走 detach，degraded 保留 route 但不再视为 compact 进行中
  -- remove instance --> 清 compact overlay；后续实例级移除语义继续按 detach / cleanup 处理，不再视为 compact 进行中

`CompactRunning` overlay
  -- `item.completed(context_compaction)` --> 若这是显式 `/compact` 的当前 turn，则直接把 compact owner-card patch 到 `completed`；若是被动 compact，则 quiet 保持静默，normal / verbose 继续并入共享过程卡
  -- turn.completed(remote_surface) --> 清 compact overlay；若前面没收到 compact item，则按 `turn.completed` 的最终状态 fallback 把 compact owner-card patch 到 `completed` 或 `failed`；随后继续 dispatchNext / finishSurfaceAfterWork
  -- compact 期间新文本 --> 先按普通 queued follow-up 入队，不立即派发
  -- compact 期间 reply auto-steer / `/steerall` --> 不命中 compact turn
  -- transport degraded / disconnect --> 清 compact overlay，并 best-effort 封 compact owner-card 为 `failed`；后续 reconnect 后可重新 `/compact`
  -- remove instance --> 清 compact overlay；compact owner-card 不再继续 patch，surface 继续进入实例移除后的常规 cleanup

`SteerPending` overlay
  -- `turn.steer` command ack accepted --> 被并入的 item 逐条转 `steered`，并给对应主文本 + 已绑定图片补 `ThumbsUp`
  -- `turn.steer` dispatch failure / command rejected --> 被并入的输入按普通语义恢复（queued item 按原顺序恢复；独立图片 reply 恢复为 staged image）
  -- transport degraded / disconnect / remove instance --> 被并入的输入按普通语义恢复
```

补充说明：

1. `pendingRemote` 先按 instance 保留“哪个 queue item 正在等 turn”，并同时保留 stage-0 dispatch `CommandID`。
2. turn 建立后再提升到 `activeRemote`。
3. 对空 thread 首条消息，promote 当前按 `CommandID -> Initiator.SurfaceSessionID -> thread 信息` 这个顺序命中；blank initiator 会先被视为 unknown，不会作为“可信非 remote initiator”直接绕过归并。
4. 若 queue item 来自 `R5`，`turn.started` 只负责把 pending remote 提升到 running；surface 只有在 durable thread 已建立、且这个 thread 已被补齐最小可继续发送元数据后，才会在 bootstrap 提交时切回 `pinned`。
5. instance 级 `ActiveTurnID/ActiveThreadID` 当前只跟踪“当前主交互面真正可中断的 turn”：
   1. local UI turn 会更新它
   2. 命中当前 `pendingRemote/activeRemote` 绑定、且 surface 策略是 `follow_execution_thread` 的 remote turn 也会更新它
   3. 未绑定的 unknown/helper side-turn 不会再覆盖或清空它
   4. `keep_surface_selection` 的 detached-branch turn 不会把 execution thread 写进 `ActiveThreadID`，因此不会污染后续 attach/resume 默认目标
6. `/stop` 当前会优先看当前 surface 的 `activeRemote` 绑定：
   1. 即使 instance 级 `ActiveTurnID` 暂时缺失，只要当前 surface 仍保留 active running remote binding，仍会对该主 turn 发 `turn.interrupt`
   2. 若已进入 retained-offline / transport degraded，则仍以 offline notice 为准，不会因为 retained binding 存在而伪造 interrupt
7. `pendingRemote/activeRemote` 当前显式保留四类 runtime 事实：
   1. dispatch command identity
   2. execution thread
   3. source/main thread
   4. surface binding policy
   这使 detour turn 可以在临时 thread 上跑完整轮 turn，同时 request / progress / image / final / interrupt 仍回原 surface，但不强制 surface 改绑到 execution thread
8. `turn.steer` 不会占用 `ActiveQueueItemID`，它只复用当前已经存在的 active running turn。
9. compact 当前不是普通 queue item，也不会占用 `ActiveQueueItemID`；它按 instance 级 `compactTurns` 单独跟踪 pending/running 状态。
10. 显式 `/compact` 还会在当前 surface 建立一条 compact owner-card flow：
   1. 首卡由 orchestrator 直接 append 一张 patchable `FeishuPageView`
   2. 首次发送时靠 `TrackingKey` 回写 `message_id`
   3. 后续 running / terminal 都继续 patch 同一张卡
   4. 这条显式 owner-card 不受 verbosity 影响
11. 只要 compact 仍在 pending/running，`dispatchNext` 就不会再把后续 queued 输入发给同一实例。
12. `/steerall` 当前会把同一 active thread 下所有 queued 项聚合为一次 `turn.steer`；若没有可并入项，只返回 noop 提示，不改队列状态；compact turn 本身不会成为 steer 目标。
13. compact pending/running 也属于 `surfaceHasLiveRemoteWork`：
   1. `/mode` 会直接拒绝
   2. `/detach` 会进入 delayed detach / abandoning
   3. `/use`、`/follow`、`/new` 这类 route mutation 会被挡住，不会在 compact 期间偷偷切走当前 thread
14. remote turn 在 `turn.completed` 时，若当前 item 满足 autowhip 触发条件：
   1. surface 不会立刻同步 enqueue 新 item
   2. 只会把 surface 置入 `A2 Scheduled`
   3. 后续等 `Tick()` 到期后再真正 enqueue
15. autowhip 当前只有一条触发通道：
   1. final assistant 文本**不包含**收工口令 `老板不要再打我了，真的没有事情干了`
16. 若 final assistant 文本命中收工口令：
   1. 当前 surface 会回到 `A1 EnabledIdle`
   2. 不会继续 schedule / dispatch autowhip
   3. 会补一条 `AutoWhip` notice：`Codex 已经把活干完了，老板放过他吧`
17. `/stop` 命中 live remote work 时，会给当前 surface 打一次 `SuppressOnce`：
   1. 本轮 turn 收尾时不会触发 autowhip
   2. suppress 只消费一次，之后 autowhip 恢复正常评估
18. 当前 backoff 固定为：
   1. `incomplete_stop`（文本未出现收工口令）: `3s -> 10s -> 30s`，最多 3 次
19. autowhip 当前不会伪造用户消息回显，也不会补 `THINKING` / `ThumbsUp` / `ThumbsDown` reaction；额外可见性只来自上面的 `AutoWhip` notice。
19. remote turn 在 `turn.completed` 时，若当前 item 命中 `terminalCause=autocontinue_eligible_failure`，则 autoContinue 会接管收口：
   1. direct `turn_failed` notice 被抑制
   2. surface 进入 autoContinue overlay，而不是 autowhip overlay
   3. 若当前没有 gate/backoff 阻挡，会立刻开始第 1 次自动继续
20. `/stop` 对 autoContinue 有两条收口：
   1. 若 autoContinue 还在 `scheduled`，直接取消等待中的 episode
   2. 若 autoContinue attempt 已经 running，则 turn 收尾时按 `user_interrupted` 归因，不会继续 schedule 下一轮 autoContinue
21. autoContinue 当前不会跨目标长期悬挂：
   1. `/detach`
   2. `/new`
   3. `/use` / `/follow`
   4. thread 丢失 / 被强踢
   都会清掉当前 episode，只保留 enable 开关
22. detached-branch 文本入口当前已经直接挂在普通 text ingress：
   1. 文本里出现 `[什么？]` 时，当前会走 `fork_ephemeral + keep_surface_selection`；它要求 surface 当前已经选中一个可见 thread，服务端会把这个 thread 作为 `source/main thread`
   2. 文本里出现 `[耸肩摊手]` 时，当前会走 `start_ephemeral + keep_surface_selection`；它不要求 surface 已有选中 thread
   3. detour 触发文本只作为入口信号：服务端会先把它从实际 prompt 文本里剥掉，再把消息派发给上游
   4. detour 文本当前会显式绕过 reply auto-steer、implicit `/new` 准备态推进，以及 normal/vscode 的 unbound 输入门禁；运行时只把“当前选中 thread 的 cwd / prepared cwd / workspace root”当作临时会话的 base cwd，不会因此改写 surface 自己的 `SelectedThreadID` 或 `RouteMode`
   5. detour turn 收尾后，surface 仍保持原先的 route / selected thread；当前只会在同一 reply lane（或原本的顶层 append lane）追加一条 `detour_returned` notice，提示“临时会话已结束，已切回原会话。”
23. detached review session 当前复用同一套“execution thread 与 surface selected thread 分离”的承接方式，但语义比 detour 更强：
   1. review thread 必须先带 `source=review`；surface 会优先在 `turn.started(remote_surface)` 时把它识别成 review session，若这轮 `turn.started` 还没拿到 remote-surface 归属，则会退回由 `entered_review_mode` / `exited_review_mode` 生命周期 item 激活同一个 pending session
   2. 进入 review session 后，instance 级 `ActiveTurnID/ActiveThreadID` 会跟随 review thread，这保证 `/stop`、running 判定和 request gate 仍能命中真正的 review turn
   3. surface 自己的 `SelectedThreadID` 仍保持 parent thread；因此 review turn 不会污染后续普通 attach/resume 默认目标。`ReviewSession` 的合法性不再依赖 `SelectedThreadID == ParentThreadID`，但这是正常不变量；若旧污染状态里 selected thread 已经变成 review thread，继续审阅文本、放弃审阅、按审阅意见继续修改都会先恢复到 parent selection。若此时用户重新发起 `/review uncommitted` 或 `/review commit <sha>`，启动目标也会先从 review thread 回溯到 parent thread。`/new` 进入 `new_thread_ready` 会清掉空闲 review session
   4. 当 review session 处于 `ActiveTurnID != ""` 时，它也属于 `surfaceHasLiveRemoteWork`；普通文本会先排队，直到当前 review turn 收尾
   5. 当前 review session 后续普通文本会冻结成：
      1. `PromptExecutionMode=resume_existing`
      2. `Target.ThreadID=ReviewThreadID`
      3. `Target.SourceThreadID=ParentThreadID`
      4. `Target.SurfaceBindingPolicy=keep_surface_selection`
   6. 若某个 request / item / final turn 输出来自 review thread，但当前没有普通 `pendingRemote/activeRemote` 绑定，surface 归属与 reply anchor 会回退到 `ReviewSession` runtime，而不是丢失到 thread claim 猜测；同一路径也会让 review thread 输出等价于 `keep_surface_selection`，不会触发默认 `follow_execution_thread` 改绑
   7. `entered_review_mode` / `exited_review_mode` 生命周期 item 当前不会直接投影成前台卡片；它们会把 `TargetLabel` / `LastReviewText` 写回 `ReviewSession` runtime，并在需要时把 pending / partial review session 提升成 active、补齐 `ReviewThreadID` / `ParentThreadID` / `ActiveTurnID`，供后续前台子单消费

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

### 5.3.1 standalone Codex 升级事务对 dispatch 的影响

```text
G0 None
  -- daemon startStandaloneCodexUpgrade(initiator surface) --> 发起 surface 进入 G10 StandaloneCodexUpgradeRunning

E0/E1(other standalone-codex-backed surface)
  -- daemon startStandaloneCodexUpgrade --> E4 PausedForLocal
  -- 用户发送文本/图片/文件 --> 保持 E4；输入先按原 route 冻结进 queue，但不 dispatch
  -- daemon finishStandaloneCodexUpgrade(success/failure) --> E0 Idle 或 E1 Queued，并继续 dispatchNext
```

补充说明：

1. 这是 daemon 级全局事务，不要求其它 surface 先收到主动广播。
2. 这里只覆盖真正依赖 standalone Codex 的 surface；attached VS Code surface、以及 detached 的 `vscode` mode surface 当前不会因为这条事务被 pause / busy / restart。
3. 非发起 surface 当前只有在用户真的尝试输入时，才会看到“当前正在升级 Codex，这条输入会在升级完成后执行”的 notice。
4. 发起 surface 不会走“排队后恢复”语义；它的普通输入会被直接挡住，避免把 owner-flow 和普通 turn 混在一起。
5. 这条路径当前只把 `ActionTextMessage`、`ActionImageMessage`、`ActionFileMessage` 当作 queueable input；其它 slash/menu/card 动作仍直接拒绝。
6. install 完成后的 child restart 当前同样走 shared restart waiter：
   1. `process.child.restart` 的 bare `ack` 只代表新 child 已接管。
   2. 只有匹配的 restore outcome `process.child.restart.updated(status=succeeded)` 到达后，upgrade transaction 才会真正结束并恢复 `paused_for_local` surface。
   3. 若 restore outcome 失败或 daemon 等待超时，upgrade 也会以失败事务收口，不会再出现“child 其实还在恢复，但 surface 先被误判成升级失败/成功”的半死状态。

### 5.4 daemon 重启恢复与 headless 生命周期

```text
G0 None
  -- daemon startup latent normal surface + persisted target --> 先走 normal visible/workspace 恢复判定
  -- /use(thread，需要 create headless) --> G1 PendingHeadlessStarting
  -- R0 Detached 且 `surface resume state` 里仍有 `ResumeHeadless=true` 的 concrete thread-restore 目标 --> 后台 exact-thread continuation 判定

G1 PendingHeadlessStarting
  -- instance connected 且 pending.Purpose=fresh_workspace 且 pending.PrepareNewThread=false --> R1 AttachedUnbound + G0 None
  -- instance connected 且 pending.Purpose=fresh_workspace 且 pending.PrepareNewThread=true --> R5 NewThreadReady + G0 None
  -- instance connected 且 pending.ThreadID != "" 且非 auto-restore --> R2 AttachedPinned + G0 None
  -- instance connected 且 pending.ThreadID != "" 且 auto-restore --> R2 AttachedPinned + G0 None + 单条恢复成功 notice
  -- instance connected 且 pending.ThreadID == "" 且也不是 fresh_workspace（仅历史兼容兜底） --> kill headless + generic notice + G0 None
  -- /mode codex|claude|vscode（目标 backend 或 ProductMode 发生变化） --> kill headless + clear persisted resume target + G0 None + R0 Detached(目标 mode/backend)
  -- /detach --> kill headless + G0 None + R0 Detached
  -- Tick timeout --> kill headless + clear pending + detach if needed
```

daemon startup 的 headless resume 额外规则：

1. 触发点：
   1. daemon startup 后的 tick
   2. `hello`
   3. `threads.snapshot`
   4. `thread.discovered`
   5. `thread.focused`
   6. `disconnect`
2. 前置条件：
   1. surface 当前处于 headless 主链（当前持久化 token 仍是 `ProductMode=normal`）
   2. surface 当前没有显式 attach
   3. surface 当前没有 pending headless
   4. `surface resume state` 里仍有 `ResumeThreadID` 或 `ResumeWorkspaceKey`
3. 恢复优先级：
   1. exact visible thread 恢复
   2. workspace-owned route 恢复（`ResumeRouteMode=unbound|new_thread_ready`）
   3. 非 headless pinned thread 的 workspace fallback
   4. concrete headless thread restore 的 auto-restore
4. 若 daemon 启动后的首轮 `threads.refresh -> threads.snapshot` 还没走完，且 persisted target 里包含 `ResumeThreadID`：
   1. 保持 `R0 Detached`
   2. 静默等待
   3. 不降级到 workspace，也不给失败提示
5. exact visible thread 恢复成功时：
   1. 进入 `R2 AttachedPinned`
   2. 只发一条 “已恢复到之前会话” notice
   3. 清掉该 thread 的旧 replay，避免补历史噪音
6. workspace attach / workspace prepare fallback 成功时：
   1. 若目标 route 是普通 workspace attach，则进入 `R1 AttachedUnbound` 并发一条 “已先回到工作区” notice
   2. 若目标 route 是 `new_thread_ready`，则直接进入 `R5 NewThreadReady`
   3. 若需要先 fresh-start workspace，则会先发 `workspace_create_starting`；成功后再按上面两种 route 落位
7. 若首轮 refresh 已完成，但 visible/workspace 路径仍无法恢复：
   1. 若目标属于普通 non-headless resume 且连 workspace route 也不存在：保持 `R0 Detached`，发一条恢复失败提示，并进入 daemon 内存态 backoff
   2. 若目标已经进入 fresh workspace prepare，但 launcher 失败或超时：保持 `R0 Detached`，发 `workspace_create_start_failed` / `workspace_create_start_timeout`
   3. 若目标是 `ResumeHeadless=true` 的 concrete managed-headless thread restore：headless resume 会在同一条主链里继续 exact-thread continuation，并投影 headless-specific notice；不会再把这件事交给独立的第二 recovery 通道

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
   4. 若 surface resume 里还残留 headless 恢复目标，mode 切换与后续 state sync 会把它清掉；旧 hint 文件不参与运行时恢复
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
2. daemon startup 时会先根据 `surface resume state` materialize latent detached surface，并恢复 `ProductMode`、`Backend`、`ClaudeProfileID` 与 `Verbosity`；`PlanMode` 不再跨 daemon 恢复；`surface resume state` 当前也携带 headless 恢复所需的 thread 元数据，而且它已经是唯一持久化恢复源。startup 不再导入独立的 `headless-restore-hints.json`。
3. 后台恢复前置条件：
   1. surface 当前处于 headless 主链（当前持久化 token 仍是 `ProductMode=normal`）
   2. surface 当前没有显式 attach
   3. surface 当前没有 pending headless
   4. `surface resume state` 里仍存在 `ResumeHeadless=true` 且 `ResumeThreadID` 非空的恢复目标
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
2. staged image / staged file 会被丢弃。
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
7. 如果该 surface 的 `surface resume state` 仍保留 `ResumeHeadless=true` 的 concrete thread-restore 目标，hard disconnect 回到 `R0 Detached` 后会重新进入同一条 headless recovery 主链里的 exact-thread continuation 判定。
8. daemon graceful shutdown 也不是 `transport_degraded`。当前实现会在真正停掉 Feishu gateway 前，对内存里已知的 surface best-effort 广播单条 `daemon_shutting_down` notice；如果某个 surface 或 gateway 发送失败，只记录日志，不阻塞最终退出。
9. 这几类提示当前统一归类为 `global runtime` 独立车道，而不是 `owner-flow` 或 `turn-owned`：
   1. 真正脱离当前 owner-card 上下文的 surface resume / VS Code resume failure
   2. 真正脱离当前 owner-card 上下文的 `open VS Code` prompt
   3. `attached_instance_transport_degraded`
   4. `daemon_shutting_down`
   5. `gateway_apply_failed`
10. `global runtime` 提示当前统一保持顶层 append-only：
   1. 不 reply 到 turn 源消息
   2. 不 patch 当前 owner-card / target picker / request prompt；当前唯一例外是 stamped `/mode vscode` 会在 fallback 到这条车道前，先尝试把首张可投影兼容提示卡承接到当前卡
   3. 不借用 turn-owned reply-chain 或 final-card anchor
11. 当前的重复触发策略已经按 family 收口到同一 helper，而不再散在各入口各写一份：
   1. resume failure / VS Code open prompt 仍以 source 侧恢复 backoff 为主，helper 只额外做短窗去重，避免同批次重复弹出
   2. `attached_instance_transport_degraded` 当前按 `surface + family + code` 做短窗节流，避免离线抖动时连续刷同一张系统提示
   3. `daemon_shutting_down` 当前仍是一轮 shutdown 里每 surface 最多一条，并附带较长 dedupe 窗口
   4. `gateway_apply_failed` 当前会先排入 daemon 的 pending runtime notice 队列，待下一次正常 gateway apply 成功前置冲刷；同 surface 同 family 的重复错误会按 dedupe key 收敛
12. 因此，后续若新增真正属于 runtime 层的提示，默认应进入这条独立车道，而不是继续直接手写普通 `UIEventNotice`。

## 6. 命令矩阵

### 6.1 基础路由态

| 命令 | `R0 Detached` | `R1 AttachedUnbound` | `R2 AttachedPinned` | `R3 FollowWaiting` | `R4 FollowBound` | `R5 NewThreadReady` |
| --- | --- | --- | --- | --- | --- | --- |
| `/list` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/workspace` `/workspace new` | `codex headless`: 允许，分别打开工作会话父页 / 新建方式页；`claude headless`: hidden + allow，继续复用同一工作区与会话壳；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开工作会话父页 / 新建方式页；`claude headless`: hidden + allow，继续复用同一工作区与会话壳；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开工作会话父页 / 新建方式页；`claude headless`: hidden + allow，继续复用同一工作区与会话壳；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开工作会话父页 / 新建方式页；`claude headless`: hidden + allow，继续复用同一工作区与会话壳；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开工作会话父页 / 新建方式页；`claude headless`: hidden + allow，继续复用同一工作区与会话壳；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开工作会话父页 / 新建方式页；`claude headless`: hidden + allow，继续复用同一工作区与会话壳；`vscode`: 拒绝并提示先切到 headless |
| `/workspace list` `/workspace new dir` `/workspace new git` `/workspace new worktree` | `codex headless`: 允许，分别打开切换卡 / 目录新建卡 / Git 新建卡 / Worktree 新建卡；`claude headless`: hidden + allow，继续复用同一 target picker / 新建工作区卡；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开切换卡 / 目录新建卡 / Git 新建卡 / Worktree 新建卡；`claude headless`: hidden + allow，继续复用同一 target picker / 新建工作区卡；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开切换卡 / 目录新建卡 / Git 新建卡 / Worktree 新建卡；`claude headless`: hidden + allow，继续复用同一 target picker / 新建工作区卡；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开切换卡 / 目录新建卡 / Git 新建卡 / Worktree 新建卡；`claude headless`: hidden + allow，继续复用同一 target picker / 新建工作区卡；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开切换卡 / 目录新建卡 / Git 新建卡 / Worktree 新建卡；`claude headless`: hidden + allow，继续复用同一 target picker / 新建工作区卡；`vscode`: 拒绝并提示先切到 headless | `codex headless`: 允许，分别打开切换卡 / 目录新建卡 / Git 新建卡 / Worktree 新建卡；`claude headless`: hidden + allow，继续复用同一 target picker / 新建工作区卡；`vscode`: 拒绝并提示先切到 headless |
| `/new` | 拒绝 | `headless`: 允许；`vscode`: 拒绝 | `headless`: 允许；若存在 compact/steer/queued/dispatching/running 或运行中的 review turn 则拒绝；空闲 review session 会被清掉；`vscode`: 拒绝 | 拒绝 | 拒绝 | 允许；若首条消息已 dispatching/running 则拒绝；空闲 review session 会被清掉 |
| `/compact` | 提示先 `/list` / `/use` 接管并绑定会话 | 提示先 `/use`，或直接发文本开启新会话 | 允许；仅对当前已绑定 thread 生效，若已有 compact/steer/queued/dispatching/running 则拒绝 | 提示先在 VS Code 里进入会话，或手动 `/use` | 允许；仅对当前跟随到的 thread 生效，若已有 compact/steer/queued/dispatching/running 则拒绝 | 提示先发送首条文本真正创建会话 |
| `/history` | 提示先 `/list` 接管在线实例 | 提示先 `/use`，或直接发文本开启新会话 | 允许；读取当前选中 thread 的历史 | 提示先在 VS Code 里进入会话，或手动 `/use` | 允许；读取当前跟随 thread 的历史 | 提示先发送首条文本真正创建会话 |
| `/use` `/useall` | `codex headless`: 二者都只是 `/workspace list` 的 alias；`/use` 偏向当前 workspace，`/useall` 允许跨 workspace；`claude headless`: `/use` 可见、`/useall` hidden + allow，二者复用 workspace-session 切换卡；`vscode`: 拒绝并提示先 `/list` | `codex headless`: 二者都打开 `/workspace list` 切换卡；`/use` 默认当前 workspace，`/useall` 允许跨 workspace；`claude headless`: `/use` 可见、`/useall` hidden + allow，二者复用同一底层切换卡；`vscode`: `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | `codex headless`: 二者都打开 `/workspace list` 切换卡，允许在 confirm 时切到其他 workspace 的已有会话；但若存在 compact/steer/queued/dispatching/running，confirm 时拒绝 route exit；`claude headless`: `/use` 可见、`/useall` hidden + allow，二者复用同一切换卡；`vscode`: `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量 | `/use`=当前 instance 最近 5 个，`/useall`=当前 instance 全量；若存在 compact/steer/queued/dispatching/running，则拒绝切走当前 thread | `codex headless`: 允许打开 `/workspace list` 切换卡；若仅有 unsent draft，confirm 前会先丢弃；若首条已 dispatching/running，则 confirm 时拒绝 route exit。`claude headless`: `/use` 可见、`/useall` hidden + allow，二者复用同一路径 |
| `/follow` | `headless`: 拒绝并提示迁移；`vscode`: 拒绝并提示先 `/list` | `headless`: 拒绝并提示迁移；`vscode`: 允许 | `headless`: 拒绝并提示迁移；`vscode`: 允许；若存在 compact/steer/queued/dispatching/running 则拒绝 route change | 允许 | 允许；若存在 compact/steer/queued/dispatching/running，则保持当前 thread 不切换 | 拒绝并提示迁移 |
| `/mode` | 允许 | 允许；若有 compact/steer/queued/dispatching/running 则拒绝 | 允许；若有 compact/steer/queued/dispatching/running 则拒绝 | 允许；若有 compact/steer/queued/dispatching/running 则拒绝 | 允许；若有 compact/steer/queued/dispatching/running 则拒绝 | 允许；若有 compact/steer/queued/dispatching/running 则拒绝 |
| `/autowhip` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/autocontinue` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/help` `/menu` `/debug` `/upgrade` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/steerall` | 允许；通常返回 noop 提示 | 允许；通常返回 noop 提示 | 允许；仅在存在同 thread queued + running turn 时并入，否则 noop | 允许；通常返回 noop 提示 | 允许；仅在存在同 thread queued + running turn 时并入，否则 noop | 允许；通常返回 noop 提示 |
| 文本 | 拒绝 | `headless`: 允许并隐式进入新会话首条输入；`vscode`: 拒绝 | 允许 | 拒绝 | 允许 | 允许首条；首条 queued/dispatching/running 后拒绝第二条 |
| 图片 | 拒绝 | `headless`: 允许并隐式进入 `R5` 后暂存；`vscode`: 拒绝 | 允许 | 拒绝 | 允许 | 仅在首条文本尚未入队前允许 |
| 文件 | 拒绝 | `headless`: 允许并隐式进入 `R5` 后暂存；`vscode`: 拒绝 | 允许 | 拒绝 | 允许 | 仅在首条文本尚未入队前允许 |
| 请求按钮 | 拒绝 | 拒绝 | 允许 | 拒绝 | 允许 | 理论上通常不会出现；若出现仍按 attached surface 处理 |
| `/stop` | 通常无效果 | 通常无效果 | 允许 | 允许 | 允许 | 允许；可清掉 staged/queued draft |
| `/status` | 允许 | 允许 | 允许 | 允许 | 允许 | 允许 |
| `/detach` | 允许但通常只提示已 detached；`codex` 主展示命令仍是 `/workspace detach`，`claude` 主展示命令已切到 `/detach` | 允许；`codex` 主展示命令仍是 `/workspace detach`，`claude` 主展示命令已切到 `/detach` | 允许；`codex` 主展示命令仍是 `/workspace detach`，`claude` 主展示命令已切到 `/detach` | 允许；`codex` 主展示命令仍是 `/workspace detach`，`claude` 主展示命令已切到 `/detach` | 允许；`codex` 主展示命令仍是 `/workspace detach`，`claude` 主展示命令已切到 `/detach` | 允许；dispatching/running 时走 abandoning；`codex` 主展示命令仍是 `/workspace detach`，`claude` 主展示命令已切到 `/detach` |
| bare `/mode` / bare `/autowhip` / bare `/autocontinue` | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 |
| bare `/model` `/reasoning` `/access` | 允许，但 detached 时只回恢复/参数卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 | 允许，返回快捷按钮 + 表单卡 |
| bare `/debug` `/upgrade` | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 | 允许，返回状态 + 快捷按钮 + 表单卡 |
| 带参数 `/model` `/reasoning` `/access` | 拒绝 | 允许 | 允许 | 允许 | 允许 | 允许 |

### 6.2 覆盖门禁

| 覆盖状态 | 当前行为 |
| --- | --- |
| `G1 PendingHeadlessStarting` | 只允许 `/status`、`/autowhip`、`/autocontinue`、`/debug`、`/upgrade`、`/mode`、`/detach`、revoke/reaction；其中 `/mode` 若实际切到了新的 backend 或 `ProductMode`，会直接 kill 当前恢复流程并清空持久化 headless / resume target；reaction 即使放行到 action 层，也只会在满足 steering 条件时生效。若当前 pending 只是后台 auto-restore 占位，手动 `/upgrade latest` 与允许 dev feed 的 flavor（源码 `dev` 与 release `alpha`）下的 `/upgrade dev` 允许继续弹候选升级卡 |
| `G2 PendingRequest` | 普通文本、图片、文件、`/new`、`/compact` 被挡；`/use`、`/follow`、follow 自动重绑定只要会改路由也都会被冻结；`/mode` 允许，并会把 request gate 一并清掉；用户也可以先处理请求卡片。request family 当前仍共用同一 gate / revision / waiting-dispatch substrate，但卡面语义已由 orchestrator 单点归一化成 `SemanticKind`：approval family 会区分 `approval_command`、`approval_file_change`、`approval_network`，并继续按 `availableDecisions` 生成 `accept`、`acceptForSession`、`decline`、`cancel` 等决策；`request_user_input` 继续支持“单题自动推进”；`permissions_request_approval` 会投影成权限授予卡，支持“允许本次 / 本会话允许 / 拒绝”；`mcp_server_elicitation` 会分成 url / form 两种语义，其中 form 型同样是单题自动推进，optional 字段需要显式 `skip_optional`，底部 `cancel_request` 只取消当前 request、不打断 turn；这些卡都会在 `request_revision` 上做 same-daemon freshness 校验。`tool_callback` 当前则进入只读 fail-closed 分支：orchestrator 会先 append 一张 sealed `tool_callback` 提示卡，再立即自动派发结构化 unsupported 结果，不等待任何用户输入；在上游 `request.resolved` 之前，这个 pending request 仍会保持 gate，避免 route/输入穿透。若这条自动回写被本地 Codex 拒绝，卡片不会错误退回可编辑态，而是继续保持 sealed，并明确提示用户可用 `/stop` 结束当前 turn |
| `G3 RequestCapture` | 下一条文本优先被当成反馈；图片、文件、`/new`、`/compact`、`/use`、`/follow`、follow 自动重绑定只要会改路由也都会被 request-capture gate 冻住；`/mode` 允许，并会把 capture gate 一并清掉 |
| `G4 PathPicker` | 只允许当前 active picker 自己的 enter/up/select/confirm/cancel callback、`/status`、普通文本/图片/文件、revoke/reaction；`/workspace` 命令族、`/list`、`/use`、`/useall`、`/follow`、`/new`、`/detach`，以及 `/menu` / bare config / 其它 competing Feishu card flow 当前都会被挡住并提示先确认或取消 picker。confirm / cancel 会先清 gate，再把结果交给 consumer 或默认 notice；unauthorized 只回拒绝 notice，不清当前 gate；若 picker 已过期，则会在下一次 action 入口自动清 gate |
| `G5 TargetPickerProcessing` | 只允许当前 Git/Worktree owner-card 自己的 `target_picker_cancel`、`/status`、reaction/recall；普通文本/图片/文件、`/workspace` 命令族、`/list`、`/use`、`/useall`、`/new`、`/follow`、`/detach`、bare config 与其它 competing Feishu card flow 当前都会被挡住，并按当前 pending kind 提示“正在导入 Git 工作区”或“正在创建 Worktree 工作区”；unauthorized 只回拒绝 notice，不清当前 gate；Git clone / worktree create / prepare 完成、失败、取消或 flow 失效后会清 gate |
| `G9 UpgradeOwnerFlowRunning` | daemon 侧 active upgrade owner-flow 处于 `running` / `cancelling` / `restarting` 时，只允许 `/status`、`/upgrade`、`/debug`、reaction/recall 与同一张升级卡的 `upgrade_owner_flow(confirm/cancel)`；普通文本/图片/文件、`/list`、`/use`、`/useall`、`/new`、`/follow`、`/detach`、bare config 与其它 competing card flow 当前都会在 `handleAction(...)` 顶层被挡住，并提示“当前正在准备升级”；helper 启动前若用户取消，会先切到 cancelling，再封成 terminal `升级已取消`；helper 即将切换前会把 owner card 封成 `正在重启`，随后等待 daemon 生命周期切换自然结束 |
| `G10 StandaloneCodexUpgradeRunning` | daemon 侧 active standalone Codex upgrade transaction 运行中时，会先于现有 self-upgrade owner-flow gate 处理输入。发起 surface 只允许 `/status`、`/debug`、`/upgrade`、reaction/recall 与后续 standalone-codex-upgrade owner-flow 动作；其它普通输入直接返回 `codex_upgrade_running`。其它真正依赖 standalone Codex 的 attached surface 的文本/图片/文件会先通过既有 ingress 路径入队，但 dispatch 维持暂停，并追加“升级完成后执行”的同 code notice；VS Code surface / instance 当前完全跳过这条 gate，不进入 pause / queue-after-upgrade 语义；非 queueable 命令/卡片动作当前直接拒绝，不进入缓存 |
| `G11 TurnPatchTransactionRunning` | daemon 侧 active turn-patch transaction 运行中时，会先于普通 reducer 处理输入。当前 instance 的 attached surface 会统一被 `PauseSurfaceDispatch(...)`；发起 surface 与同 instance 其它 surface 的普通文本/图片/文件、`/bendtomywill`、`/bendtomywill rollback`、`/new`、`/compact`、`/detach`、bare config 与其它 competing card flow 都会被拒绝，并提示“当前正在修补/回滚当前会话”；只保留 `/status`、`/list`、`/help`、`/menu`、`/history`、`/debug`、reaction/recall 这类查看动作，直到 transaction 成功或失败收口 |
| `G6 AbandoningGate / E6 Abandoning` | `Abandoning` 已在执行 overlay 中持有真实状态；对外门禁与 `G6` 一致：只允许 `/status`、`/autowhip`、`/autocontinue`；再次 `/detach` 只回 `detach_pending`；`/mode` 与其余动作统一拒绝。该 gate 当前只会在“已有真实 started turn 或其它 live work 仍需收尾”时进入；若只是 pre-start dispatching 残留，则 `/detach` 会直接失败 active item 并完成 detach，不再经过 `G6` |
| `G7 VSCodeCompatibilityBlocked` | 只影响 daemon 的 detached-vscode 恢复路径：exact-instance auto-resume 与普通 open-vscode prompt 会被抑制，改发必要的修复/失败反馈；legacy `editor_settings` 若能安全静默迁到 `managed_shim`，则不会长时间停在这条 gate。surface 侧 `/list`、`/mode`、`/status` 等动作仍按 route matrix 正常处理。若提示由 stamped `/mode vscode` 当前卡同步触发，则优先承接到当前卡；后台恢复路径仍走独立 runtime 提示 |

retained-offline overlay 额外规则：

1. 条件：`Attachment.InstanceID != ""` 且 `Dispatch.InstanceOnline=false`。
2. 当前若保留了 active running/dispatching item，`/stop` 只返回恢复中提示，不会发送 interrupt；即使 retained `activeRemote` binding 仍在，也以 offline notice 为准。
3. `/detach` 直接 finalize，不进入 `E6 Abandoning`。
4. `/status` 必须把“attachment 仍保留”和“实例当前离线”同时投影出来。

## 7. UI 动作协议

当前 Feishu 卡片动作与服务端 action 对应关系如下：

补充说明：

1. 这张表描述的是 gateway / parser 边界上的 transport action 映射，不等于最终 owner。
2. `show_*` 与 bare config `Action*Command` 当前在 live path 中会先被归并成 `FeishuUIIntent`，再进入 Feishu UI controller；它们保留对应 `ActionKind` 主要是为了统一文本命令、菜单和卡片 callback 的 transport 兼容面。

| 卡片动作 | 服务端 action | 说明 |
| --- | --- | --- |
| `attach_workspace` | `ActionAttachWorkspace` | headless 主链下 `/list` 的 workspace attach/switch 入口 |
| `show_all_workspaces` | `ActionShowAllWorkspaces` | headless 主链下重新打开 `/workspace list` 切换卡（兼容旧分页导航动作） |
| `show_recent_workspaces` | `ActionShowRecentWorkspaces` | headless 主链下重新打开 `/workspace list` 切换卡（兼容旧分页返回动作） |
| `show_workspace_threads` | `ActionShowWorkspaceThreads` | headless 主链下以指定 workspace 为默认项重新打开 `/workspace list` 切换卡（兼容旧 recoverable-workspace 入口） |
| `attach_instance` | `ActionAttachInstance` | 直达 attach |
| `use_thread` | `ActionUseThread` | 直达 thread 切换 |
| `show_threads` | `ActionShowThreads` | headless 主链下重新打开 `/workspace list` 切换卡；`vscode` 下仍是当前实例最近会话视图；两条路径当前都会默认排除 `source=review` 的 detached review thread |
| `show_all_threads` | `ActionShowAllThreads` | headless 主链下重新打开 `/workspace list` 切换卡；`vscode` 下仍是当前实例全部会话视图；两条路径当前都会默认排除 `source=review` 的 detached review thread |
| `show_all_thread_workspaces` | `ActionShowAllThreadWorkspaces` | headless 主链下重新打开 `/workspace list` 切换卡（兼容旧 grouped 总览展开动作） |
| `show_recent_thread_workspaces` | `ActionShowRecentThreadWorkspaces` | headless 主链下重新打开 `/workspace list` 切换卡（兼容旧 grouped 总览返回动作） |
| `history_page` | `ActionHistoryPage` | `/history` 列表页翻页；会先同步把当前卡切到 loading，再异步重查当前 thread history |
| `history_detail` | `ActionHistoryDetail` | `/history` 进入某一轮详情，或在详情页前后切换；同样会先同步 loading，再异步回填结果 |
| `target_picker_select_workspace` | `ActionTargetPickerSelectWorkspace` | `/workspace list` 切换卡与 `/workspace new worktree` 基准工作区下拉回调；只刷新当前卡，不直接改 route |
| `target_picker_select_session` | `ActionTargetPickerSelectSession` | `/workspace list` 切换卡的会话下拉回调；只刷新当前卡，不直接改 route |
| `target_picker_open_path_picker` | `ActionTargetPickerOpenPathPicker` | `/workspace new dir` / `/workspace new git` 的子步骤导航回调；会打开目录 path picker，并把 Git 主卡草稿一起保存在 active target picker runtime 里；path picker confirm/cancel 回调会先异步 ack，再把最新主卡 patch 回原 target picker owner card |
| `target_picker_cancel` | `ActionTargetPickerCancel` | 四张工作会话业务卡共用的退出按钮；编辑态会把当前卡 inline replace 成 `已取消`，Git processing 态会 replace 成 `已取消导入`，Worktree processing 态会 replace 成 `已取消创建`，并 best-effort 停止 clone / `git worktree add` / prepare；surface route 只保留取消后的安全状态 |
| `target_picker_confirm` | `ActionTargetPickerConfirm` | 四张工作会话业务卡共用的确认按钮；`/workspace list` 真正执行 attach / switch，`/workspace new dir` / `git` / `worktree` 则消费主卡里已保存的目录、Git 或 Worktree 草稿来执行接入、导入或创建 |
| `request_respond` | `ActionRespondRequest` | 承载 approval、`approval_command`、`approval_file_change`、`approval_network`、`request_user_input`、`permissions_request_approval`、`mcp_server_elicitation` 的按钮回传。approval family 的按钮集合仍沿用归一化后的 `availableDecisions`，包括 `cancel`；但 title/body/hint 与 MCP form/url、permissions grant 等卡面语义，当前都由 orchestrator 先归一化成 `SemanticKind` 并写入 pending request/view。`request_user_input` 的纵向 direct-response 按钮会把当前题答案写入 pending request 草稿，并在未完成时刷新到下一题；`permissions_request_approval` 会按按钮回写 `{permissions, scope}`；`mcp_server_elicitation` 会按按钮回写 `{action, content, _meta}`，其中 form 模式的 direct-response 按钮同样先写入局部草稿，只有最后一题答完后才会真正 accept。`tool_callback` 当前不产生任何用户可点击的 `request_respond` 动作；收到 request 后服务端会直接自动派发一条结构化 unsupported 响应 |
| `request_control` | `ActionControlRequest` | 承载 request 的非回答型动作。当前 live 路径使用 `skip_optional`、`cancel_turn`、`cancel_request`：`skip_optional` 会标记当前 optional 题已跳过，并在必要时直接触发最终 dispatch；`cancel_turn` 会把 `request_user_input` 当前卡 seal 后发送 `turn.interrupt`；`cancel_request` 只用于 form 模式 `mcp_server_elicitation` 的请求级取消 |
| `submit_request_form` | `ActionRespondRequest` | 顶层/`item` 两种 `request_user_input` 与 form 模式 `mcp_server_elicitation` 的表单提交入口；按 `question.id -> answers[]` 回传，不再额外携带 live `request_option_id`。orchestrator 会根据当前是否还有未完成题，决定是“保存草稿并自动跳到下一题”还是“seal 当前卡并最终提交” |
| `kick_thread_confirm` | `ActionConfirmKickThread` | 强踢前再次校验实时状态 |
| `kick_thread_cancel` | `ActionCancelKickThread` | 仅回 notice |
| `/vscode-migrate` | `ActionVSCodeMigrateCommand` | 打开 VS Code 迁移 page root；仅在 `vscode` 的 slash help 中展示，headless 主链下不进入 help/menu，直接输入则返回“仅 VS Code 模式可用”页 |
| `vscode_migrate_owner_flow` | `ActionVSCodeMigrate` | VS Code 迁移 page 的 owner-flow callback；点击后走本机 managed-shim 迁移链路，并把结果继续收口在同一张 guidance card 上 |

菜单与文本命令里新增：

1. `/new`
2. 菜单 `new`
3. `/history`
4. 菜单 `history`

其中 `/new` / 菜单 `new` 都直接映射到 `ActionNewThread`；`/history` / 菜单 `history` 都映射到 `ActionShowHistory`。

同时，文本命令里新增：

1. `/mode`
2. `/mode normal`
3. `/mode codex`
4. `/mode claude`
5. `/mode vscode`

这些字面值都映射到 `ActionModeCommand`，由服务端在当前 surface 上解释并决定是否执行切换。

同时，文本命令里新增：

1. `/plan`
2. `/plan on`
3. `/plan off`
4. `/plan clear`

四者都映射到 `ActionPlanCommand`，由服务端在当前 surface 上解释并决定新的 surface-level `PlanMode` / 显式 plan override；真正的提案 handoff 卡按钮则单独走 `ActionPlanProposalDecision`。

同时，文本命令里新增：

1. `/bendtomywill`
2. `/bendtomywill rollback`
3. `/bendtomywill rollback <patch_id>`

三者当前都会先映射到 `ActionTurnPatchCommand`，再由 daemon 按参数二次解析成“打开 patch 卡”或“回滚最近一次修补”；成功页上的回滚按钮则单独走 `ActionTurnPatchRollback`。

补充说明：

1. 当前 Feishu gateway 只为一小组 pure-navigation action 开放同步 `replace_current_card` 回包：
   1. `ActionShowCommandMenu`
   2. `ActionShowAllWorkspaces`
   3. `ActionShowRecentWorkspaces`
   4. `ActionShowThreads`
   5. `ActionShowAllThreads`
   6. `ActionShowScopedThreads`
   7. `ActionShowWorkspaceThreads`
   8. `ActionTargetPickerSelectWorkspace`
   9. `ActionTargetPickerSelectSession`
   10. `ActionShowHistory`
   11. `ActionHistoryPage`
   12. `ActionHistoryDetail`
   13. bare `ActionModeCommand` / `ActionAutoWhipCommand` / `ActionReasoningCommand` / `ActionAccessCommand` / `ActionModelCommand`
2. 这些动作只要命中 `ResolveFeishuFrontstageActionContract(action).CurrentCardMode=inline_view`、来源卡片带有当前 daemon 的 lifecycle 标识、且首个 `UIEvent` 显式标记 `InlineReplaceCurrentCard`，就会先走原地替换；若同一动作后面还带异步命令（当前就是 `/history` 的 `thread.history.read`），daemon 仍会继续执行后续事件，不会因为同步 replace 而提前终止。
3. `/help` 这类静态目录卡、apply 终态、request prompt 终态，以及 bare `/upgrade` / `/debug` 的状态卡与 upgrade 重启后结果 notice 等仍然沿用 append-only 消息语义，不在这轮同步回包范围内；当前例外是 `/upgrade latest` 一旦进入 daemon owner-card 流，会在同一张升级卡上继续 patch 到 confirm/running/restarting。

## 8. 当前死状态审计结论

这轮按当前实现重新审计后，以下几类 bug-grade 半死状态已经收口：

1. **instance 半 attach**：已修复。第二个 surface attach 同一 instance 会直接失败。
2. **数字文本误切换 thread**：已修复。数字文本现在是普通消息。
3. **headless 选择期还能旁路 `/use` `/follow` `/new`**：已修复。`PendingHeadless` 仍是顶层 gate。
4. **staged attachment 跟着 route change 串 thread**：已修复。route change 或 clear 会显式丢弃 staged image / staged file 并告知用户。
5. **`PausedForLocal` 永久卡住**：已修复。现在有 watchdog。
6. **`Abandoning` 永久锁死**：已修复。现在有 watchdog。
7. **`/follow` 切模式但 thread 不变时 UI 不知道 route mode 已变**：已修复。现在会补发 route-mode selection 投影。
8. **`/new` 的空 thread 归属靠 `ActiveThreadID` 猜**：已修复。现在改成显式 `remote_surface + SurfaceSessionID` 相关性。
9. **`R5 NewThreadReady` 在 queued draft 时没有出口**：已修复。现在 `/use`、`/follow`、`/detach`、`/stop`、重复 `/new` 都有明确语义。
10. **detach 期间最后一条 final / thread notice 会被完全吞掉**：已修复。当前会保留单条 thread 级 replay，并在后续 idle 的 `/attach` 或 `/use` 时一次性补发。
11. **detached 状态下 `/use` 是死入口，只能先 attach instance**：已修复。现在 `/use` 会展示 global merged thread list，并按 resolver 自动 attach。
12. **cross-instance `/use` 会绕过 detach 语义，保留旧 request/capture/override**：已修复。现在只有 headless detached/global `/use` 还会做这类 resolver attach；切换前会先走 detach 风格清理与门禁。
13. **旧 `/newinstance` 手工 headless 选择分支仍能把用户带进过时状态**：已修复。当前 parser 已不再接收旧命令，只保留 thread-first `/use` 的 preselected headless；历史兼容残留只会在启动时被自动清掉。
14. **same-instance `/use` / `/follow` / auto-follow 会在旧 request gate 还活着时静默改路由**：已修复。现在只要 request gate 仍在，所有会改路由的动作都会被冻结，包括 `follow_local` 下手动 force-pick 后再 `/follow` 的回切。
15. **attached vscode `/use` 会误走全局 merged thread view，甚至跨 instance retarget**：已修复。现在 detached vscode 必须先 `/list`，attached vscode 只允许当前 instance 已知 thread，且 one-shot force-pick 仍保持 `follow_local`。
16. **cross-instance attach 到复用/新建 headless 时会丢 thread replay**：已修复。当前 replay 会先按 `threadID` 全局迁移，再在目标 attach 上一次性补发。
17. **transport degraded 后既误报“已中断当前执行”，又缺少 retained-offline 逃生口**：已修复。当前会保留可相关的 in-flight turn，不再伪造“已中断”；同时 `/status`、`/stop`、`/detach` 都已显式区分 retained-offline 与真正 detach。
18. **queued 点赞升级成 steering 后 item 会脱离普通 queue，若 ack 失败则可能丢失**：已修复。当前已强制恢复原 queue 位置，并补失败 notice。
19. **headless 自动恢复在首轮 refresh 前过早报失败，或恢复成功后重放旧 replay / 额外补 attached 噪音**：已修复。当前会先静默等待首轮 refresh，恢复成功只发单条成功 notice，且会清空旧 replay 而不是补发。
20. **`PendingHeadless` 只能靠隐藏的 `/killinstance` 逃生**：已修复。当前 `/detach` 可以直接取消恢复流程并回到 `R0 Detached`；旧 `/killinstance` 也已不再解析。
21. **显式切 mode 会保留旧 attachment / request gate / draft 残留，导致进入半切换状态**：已修复。当前 idle/detached 时 `/mode` 会先做 detach-like 清理；busy path 则明确拒绝并提示 `/stop` 或 `/detach`。
22. **headless 主链仍然只按 instance/thread 仲裁，导致同 workspace 多 surface 并存**：已修复。当前 headless 主链的 attach/use/headless 恢复都会先经过 workspace claim；只有显式切到 `vscode` 才绕过这层仲裁。
23. **headless 主链还能长期停留在 follow 路径，导致 workspace-first 叙事失真**：已修复。当前新 `/follow` 会直接回迁移提示；历史 headless follow route 也会在读取 surface 时落回 pinned/unbound。
24. **旧版本残留的 `vscode + new_thread_ready` 会在升级后继续活着，等价绕回被设计移除的 `/new` 路径**：已修复。现在 surface 读取时会自动归一化回 `follow_local`，并尽量复用当前 observed focus。
25. **headless `/list` 仍按 instance root 聚合，broad headless pool 会把多个 thread `cwd` workspace 压成一个选项**：已修复。当前 workspace 列表先看可见 thread `CWD`，只有无可见 thread 时才回退到实例级 workspace metadata。
26. **headless detached / attach / disconnect 等路径仍向用户暴露“实例”措辞，导致 workspace-first 叙事不一致**：已修复。当前 headless 主链的 detached、attach、offline、degraded、stop-offline 等提示都统一回到工作区语义。
27. **切到 `vscode` 后仍可能保留 headless restore 入口，最终进入“`vscode` surface 底层实际 attach/pending 的是 headless”半死状态**：已修复。当前 `/mode` 会清掉 pending headless 与 `surface resume state` 里的 headless 恢复目标，且 `vscode` surface 会在 auto-restore 入口被硬拒绝。
28. **daemon 重启后 latent surface 会丢 mode / verbosity，或根本无法在没有 headless hint 的情况下重新 materialize**：已修复。当前 startup 会先从 `surface resume state` 恢复 surface 路由、`ProductMode` 与 `Verbosity`。
29. **headless 主链 daemon 重启后会静默掉回 detached、过早报失败，或把 `fresh workspace prepare` 和 headless thread restore 混成一条恢复语义**：已修复。当前恢复已经收口到单一 headless 主链：workspace-owned route 直接恢复 workspace intent；带 `ResumeThreadID` 的 target 先尝试 exact visible attach；若同时带 `ResumeHeadless=true`，visible miss 后继续同 backend 的 managed-headless exact-thread continuation；普通 pinned-thread 才会再按 workspace fallback / `new_thread_ready` 收口。`ResumeHeadless` 只再代表 managed headless thread-restore strategy，`fresh workspace prepare` 会持久化成 workspace+route 语义，首轮 refresh 前仍静默等待，失败路径也按 workspace prepare / generic resume / headless-specific notice 分开收口。
30. **`vscode` daemon 重启后只保留 mode、不恢复实例，或者恢复链路误走 headless**：已修复。当前会按 exact `ResumeInstanceID` 恢复到原 VS Code 实例，回到 follow-local 语义；若还没有新的 VS Code 活动，会明确提示去 VS Code 再说一句话或手动 `/use`，而且运行时不再读取独立 headless hint 文件。
31. **`vscode` 进入或 daemon 重启恢复时，会在 legacy `settings.json` / stale managed shim 状态下继续尝试恢复，导致用户看起来进入了 `vscode`，但底层仍沿用旧接入方式或失效入口**：已修复。当前 detached-vscode 恢复会先做本机 VS Code 兼容性检查；命中旧 `settings.json` override 且存在可接管入口时，会先静默自动迁到 `managed_shim` 并清掉旧 `chatgpt.cliExecutable`，成功后再继续后续恢复/open-prompt 链路；只有缺 target、自动迁移失败，或 stale managed shim 需要修复时，才保持 detached 并发可见反馈卡。
32. **同一张 `/menu` 或 `/use` 导航卡每点一步就继续在消息流里堆新卡，导致用户停留在同一选择上下文却要反复找最新卡**：已修复。当前限定范围内的 same-context 导航已经改成 card callback 同步替换当前卡，不再制造额外历史噪音。
33. **headless `/list` 只能展示仍有 online instance 的 workspace，导致仅能从 persisted/offline thread 恢复的 workspace（例如 `picdetect`）完全不可见**：已修复。当前 headless `/list` 会把 recoverable-only workspace 也列出来，但不会伪装成 attach；按钮会直接进入该 workspace 的会话列表，再复用现有 `/use` 恢复链路。
34. **transport degraded / hard disconnect / remove instance 后 compact overlay 可能残留，导致后续 `/compact` 永久 busy**：已修复。当前这三条路径都会清掉 `compactTurns`，不会再把实例卡在伪 `compact_in_progress`；若仍保留当前 surface + compact owner-card，上层还会 best-effort 把显式 `/compact` 卡封成失败态。
35. **hard disconnect 时 pending steer 没恢复，steering 中的 queued 输入会脱离普通队列后直接消失**：已修复。当前 disconnect 也会按原顺序恢复 `pendingSteers`，再继续 offline/detach 语义。
36. **VS Code 菜单进入的 `/list`、`/use`、`/useall` 与迁移/恢复提示会在菜单首跳后逃逸成 submission-anchor / notice / runtime prompt**：已修复。当前 stamped 菜单卡会把实例 / 线程结果、attach / use 终态，以及 stamped `/mode vscode` 与 `/vscode-migrate` 命中的兼容提示 / 迁移结果优先收口到原卡；只有真正脱离当前 card 上下文的后台 runtime 提示才继续走独立 `global runtime` 车道。
37. **headless 主链的 detached backend 仍隐式回退 `codex`，导致 `codex` / `claude` 共用 workspace defaults、surface resume target 或恢复到错误 backend**：已修复。当前 surface 会单独持久化 `Backend`；`WorkspaceDefaults`、surface resume 与 detached catalog context 都按 backend 分区，旧数据缺失 backend 时 lazy 默认 `codex`，而 `codex <-> claude` 切换会显式清掉旧恢复目标。
38. **Claude 早失败会把 surface 永久卡在 `dispatching`，且 `/detach` 还会被 pre-MVP gate 拒绝或只能进入无意义的 `abandoning`**：已修复。当前 Claude translator 会在首个 `assistant` / `control_request` / `result` 事件上提升 pending turn 并收口终态；若 surface 仍停在没有 `TurnID`、没有 output 的 pre-start dispatching，`/detach` 会直接失败 active item、清掉 pending remote ownership 并完成 detach；只有真实 started turn / compact / steer 才会进入 `E6 Abandoning`。
39. **route / attach 上下文已经变化，但旧 workspace page / target picker / path picker / history / review picker 还要等“再点一次旧卡”才暴露失效，甚至出现第一次返回无效的假活状态**：已修复。当前 detach-like / route-change cleanup 会统一清掉 context-bound overlay runtime；只要仍有稳定 owner message，就会主动把旧卡封成失效态。若当前可见的是 target-picker-owned path picker 子步骤，则只 patch 这张可见子卡，隐藏父卡 runtime 静默清理；没有 anchor 的旧卡则继续按 callback fail-closed。

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
5. staged image / staged file / draft 命运
6. request capture / request prompt
7. Feishu 卡片动作协议
8. watchdog 与恢复路径

最低复审问题：

1. 有没有新增“用户表面上看已 attach 或已选 thread，但文本/图片仍无路可走”的状态。
2. 有没有新增只靠异步事件才能退出、但没有 watchdog 或手动逃生口的 blocked state。
3. 有没有让未冻结草稿在 route change 时静默改投目标。
4. 有没有把 UI helper 状态重新变回服务端持久 modal state。
5. 有没有让 `R5 NewThreadReady` 在首条消息失败后落回无恢复路径的状态。
6. `request_user_input` 与 form 模式 `mcp_server_elicitation` 的按钮/表单提交后，是否符合“单题自动推进、optional 只能显式跳过、最后一题才真正 resolve request、旧 revision 不能继续改写当前草稿”的现状语义，并确保 turn 完成、切线程、重连时不会残留旧问题卡。
7. route / attach 上下文变化后，旧 target picker / path picker / history / review picker / workspace page 是否仍可能留下一张“看起来还能操作、但其实只会第一次点击才报过期”的假活卡。

## 11. 待讨论取舍

1. 当前无未决产品取舍。

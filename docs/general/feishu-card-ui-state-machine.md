# Feishu 卡片 UI 状态机

> Type: `general`
> Updated: `2026-04-28`
> Summary: 当前实现把 normal mode 的工作会话收敛到 `workspace` 命令族与四张独立 target-picker 卡：bare `/workspace` 与 `/workspace new` 是 page-owner 父页，`/workspace list`、`/workspace new dir`、`/workspace new git`、`/workspace new worktree` 分别承接切换/目录/Git/Worktree 四条业务路径，`/list` `/use` `/useall` 只保留 alias；其中 `/workspace` 根页与 `/workspace new` 子页现在都会直接暴露 `从 Worktree 新建`，菜单 handoff 进入 workspace page 后也会把当前 `message_id + from_menu` 记入 page-owner runtime，后续目录/Git/worktree 子卡的 `返回上一层` 会稳定回到对应父页，不再退化成 target-picker 内部 no-op 返回。`/workspace list` 的 target page 与 `/workspace new worktree` 的基准工作区 dropdown 现已接入 byte-budget dropdown pagination：workspace / session 双下拉与单 workspace 下拉统一通过 `target_picker_page(picker_id + field_name + cursor)` 走同卡翻页，target page 的 workspace 预算目标 `1/3`、session `2/3`，并支持空余预算回借；workspace 翻页会重算 session 候选，session 翻页则保留 workspace 状态并清空不可见会话选择。复用 path picker 现在也接入同类 byte-budget dropdown pagination：目录模式单下拉、文件模式目录/文件双下拉、target-picker owner-subpage 的 compact 目录下拉都改走 `path_picker_page(picker_id + field_name + cursor)`；目录 lane 固定保留 `.` / `..`，文件 lane 翻页会清空不可见文件选择并禁用 confirm。selection 卡片这轮进一步收口到 `FeishuSelectionView + FeishuSelectionSemantics`：VS Code `/list` 继续按钮式实例卡，VS Code `/use` / `/useall` 统一成当前实例内的 dropdown，大集合候选会按 transport byte budget 动态分页，并通过 `thread_selection_page(view_mode + cursor)` 原地翻页；若当前 thread 不在可见页，不再伪造 `initial_option`。kick-thread confirm 也走同一 selection substrate；adapter live 路径不再回退 `FeishuDirectSelectionPrompt`。workspace/session family 这轮还把 provenance roundtrip 补到了 selection / target picker callback：page、selection、target-picker 现在都会在 payload 里继续携带 `catalog_family_id` / `catalog_variant_id` / `catalog_backend`，后续分页、刷新、确认与返回不再主要依赖裸 `ActionKind` 反推 family。path / target / thread 这三类 dropdown lane 与 callback 取值规则当前进一步收口到 `internal/adapter/feishu/selectflow` 的 `PaginatedSelectFlowDefinition`：projector 只提供 lane read model 与页面上下文，gateway 统一按 `field_name + payload value + option/form_value` contract 恢复选中值，不再分别维护 path/target/thread 三套 recover 规则。其余 target picker / path picker / history 继续共用 owner-card runtime，并统一承载 `body / notice / sealed` contract；launcher-backed bare config cards 这轮还补上了 sealed terminal 页保留显式 footer 的约束，因此菜单进入 `/mode` `/autowhip` `/autocontinue` `/reasoning` `/access` `/plan` `/model` `/verbose` 后，即使 apply 成功把参数卡封成终态，底部 `返回菜单/返回上一层` 也不会再被 page normalize 错误清掉。request family 新增 `tool_callback` 只读 fail-closed 分支：卡片会直接以 sealed `waiting_dispatch` 态落地，无按钮、无表单，并由服务端自动回写 unsupported 结构化结果；本轮还新增了 `/autocontinue` 参数卡与 reply-thread、tail-only patch 的自动继续状态卡、normal / verbose 下对用户新消息立即回一张 reply-thread `已接收` 状态卡，以及 detour 临时会话的 header subtitle 标识 / 原锚点回切提示；review mode 这轮扩到 commit review：bare `/review` / `/review commit` 会打开最近 10 条 commit 的 picker page，picker submit 通过 `page_submit(surface.command.review + action_arg_prefix=commit)` 继续走 detached review owner；普通 final card 除了按当前 thread cwd 对应 Git worktree 的 dirty 状态追加 `Review 待提交内容`，还会对正文里命中的“最近 commit”追加 `评审 <short-sha>` footer 按钮；这些 final-card review affordance 仍保持 append-only，不 patch 掉源 final card。review session 内我们自己发出的卡片标题统一加 `审阅中 ·` 前缀；若当前 target 是 commit，会进一步在标题中带上 `commit <short-sha>`。review final card 只保留 `放弃审阅` / `按审阅意见继续修改` 两个显式出口；review family 卡片按钮继续走 `page_action + daemon_lifecycle_id` 的既有 freshness/old-card reject 链路。`/upgrade` 根页现在会按 standalone Codex 安装状态决定是否暴露 `Codex 升级`，`/upgrade latest` 与 `/upgrade codex` 共用 `upgrade_owner_flow` callback family，但分别收口到 release / Codex 两条 owner-card flow；同时还新增 `/bendtomywill` latest-turn 修补流：它先打开多题 `request_user_input` 风格卡，最终收口到 patchable progress/success/failure 页面，并通过 `page_action(ActionTurnPatchRollback)` 承接最近一次回滚；菜单、帮助、page-result replacement、request cards、plan proposal、VS Code guidance、共享过程卡、原锚点 attention annotation 与 turn reply 语义见正文。

## 1. 文档定位

这份文档描述的是 **当前代码已经实现** 的 Feishu 卡片 UI / callback 层行为。

它关注的是：

- 飞书卡片导航、展开、返回、表单提交的 callback 协议面
- 哪些动作属于同上下文 UI 导航，哪些动作会真正进入产品状态机
- `daemon_lifecycle_id`、old card reject、callback 同步 replace 的现状边界
- gateway / projector / daemon / orchestrator 四层之间的当前 owner 划分

它**不替代** [remote-surface-state-machine.md](./remote-surface-state-machine.md)。

- 那份文档描述的是 core route / attach / follow / queue / request gate 状态。
- 本文描述的是 Feishu 卡片 UI session、payload、replace-vs-append、freshness 边界。

两者合起来，才是这条交互链路当前的双 guardrail。

菜单卡的工程使用规约另见：

- [feishu-menu-card-usage-guidelines.md](./feishu-menu-card-usage-guidelines.md)

## 2. 双 Guardrail 规则

### 2.1 什么时候只回看本文

下面这些改动，即使不改 core 路由，也必须更新本文并跑 Feishu UI guardrail：

- Feishu 卡片按钮或表单的 `kind` / payload 字段变化
- `projector` 卡片结构、按钮 value、表单字段名变化
- `gateway` 对 callback payload 的解析、回调同步等待策略变化
- inline replace 与 append-only 的边界变化
- `daemon_lifecycle_id` stamping / 校验 / old-card reject 行为变化

### 2.2 什么时候还要同时回看 core 状态机

如果改动同时影响下面任一项，除了本文，还必须同步回看 [remote-surface-state-machine.md](./remote-surface-state-machine.md)：

- `attach_*`、`use_thread`、`/follow`、`/new` 的 route 语义
- request gate / request capture 对 route mutation 的冻结
- 哪些命令在某个 surface state 下可见、可点、可执行
- 任何会改变“用户接下来能做什么”的产品状态迁移

### 2.3 owner 边界的总原则

- `gateway`
  - 负责把 Feishu callback 解析成 `control.Action`
  - 负责决定 callback 是同步等待 replace，还是立即 ack 后异步处理
  - 对无 callback 的共享更新卡，负责执行首次发送与后续 `message.patch`
  - path / target / thread dropdown 的选中值恢复当前统一经 `internal/adapter/feishu/selectflow` 收口：同一条 helper 负责 `payload value -> option/options -> form_value[field_name]` 恢复顺序，gateway 不再按 picker 类型分别维护 `selected-entry` / `form-value` fallback
- `daemon`
  - 负责 old-card / old-message 生命周期判定
  - 负责在 ingress 层统一把动作交给主 `ApplySurfaceAction()` 入口；`FeishuUIIntent` 分流发生在 service 内，避免绕开 request/path-picker 等产品门禁
  - 负责只在安全条件下把同上下文导航转成 `ReplaceCurrentCard`
  - 当前统一使用 `FeishuFrontstageActionContract` 判定 replace：
    - `inline_view`：命中 `SupportsFeishuSynchronousCurrentCardReplacement(action)` 且首个事件显式 `InlineReplaceCurrentCard`
    - `first_result_card`：命中同一同步条件后，从事件流里挑首张可投影卡直接作为 `ReplaceCurrentCard`；`inline_view` 严格命中失败时也允许回退到这条首结果替换
    - active picker 阻断保护：若事件流是 `path_picker_active` / `target_picker_processing` 这类阻断 notice，daemon 会保持当前卡不替换，避免把活跃 owner 子步骤误封成终态
  - 命令菜单 launcher 的 handoff 当前也统一读取 `ResolveFeishuFrontstageActionContract(action).LauncherDisposition`：`keep` 保留菜单导航/配置页，`enter_terminal` 当前用于 stamped `/help`、`/status`，其余 stamped launcher 动作默认 `enter_owner`
  - `ResolveFeishuFrontstageActionContract(action)` 里的 `ContinuationDaemonCommand` 与 `FollowupPolicy` 现在也优先从 `FeishuCommandBinding` 读取：`/debug`、`/cron`、`/upgrade`、`/vscode-migrate` 的首结果卡续接 daemon command，以及 `/help`、`/status`、`/stop`、`/new`、`/follow`、`/detach` 这组命令的 followup drop 策略，不再分别散落在 lifecycle switch 里
  - 旧 bare continuation / command submission anchor 当前已退出 live 路径，不再承接 stamped current-card 回调
- `orchestrator / Feishu UI controller`
  - 负责 `show_*`、`/menu`、bare config-card 这类 pure navigation 的 controller 分流与事件构建
  - 负责通过阶段 1 暴露的 `Feishu*Context` query/policy 边界生成 UI-owned read model 与 request 事件
  - 对 normal mode `/list` / `/use` / `/useall`，以及 attach-unbound / `selected_thread_lost` / `thread_claim_lost` 这类被动恢复入口，当前先产出 `FeishuTargetPickerView` read model，再连同 `FeishuTargetPickerContext` 穿过 `UIEvent` 边界
  - 对 VS Code instance/thread selection、kick-thread confirm，以及仍保留 selection 卡形态的少量兼容路径，当前统一先产出 `FeishuSelectionView` read model，再连同 `FeishuSelectionContext` 穿过 `UIEvent` 边界
  - 对 `/menu` 与 bare `/mode` `/autowhip` `/autocontinue` `/reasoning` `/access` `/plan` `/model` `/verbose`，当前统一产出 `FeishuPageView` read model，并连同 `FeishuPageContext` 走 `UIEventFeishuPageView` 边界（配置页内部仍复用 catalog-to-page builder 生成 page 内容）
  - 这组 bare config-card 的 open intent、launcher keep contract、controller 分发与 config page builder 当前已通过 `FeishuConfigFlowDefinition` registry 收口，不再分别在 intent / lifecycle / controller / config catalog 多层平行枚举
  - 命令入口类型当前还额外通过 `FeishuCommandBinding` 统一建模为 `config_flow / workspace_session / inline_page / terminal_page / daemon_command / owner_entry` 六类；`FeishuUIIntentFromAction(...)`、launcher handoff 与 direct daemon dispatch 都优先读取这份 binding，而不再各自维护平行的 command/action 分类
  - 对 approval / `request_user_input` / `tool_callback` / MCP request cards，当前先产出 `FeishuRequestView`，再连同 `FeishuRequestContext` 穿过 `UIEvent` 边界
  - 对飞书文件/目录选择器，当前先产出 `FeishuPathPickerView` read model，再连同 `FeishuPathPickerContext` 穿过 `UIEvent` 边界；进入目录、返回上一级、文件选择属于 controller 内 pure navigation，confirm/cancel 则转到 picker consumer handoff
- `projector`
  - 负责把 `control.UIEvent` 渲染成 Feishu 卡片
  - 负责把当前需要的 callback payload 字段写进卡片按钮/表单/下拉
  - 纯 projector builder / payload projection 现在已下沉到邻接子包 `internal/adapter/feishu/projector`；`internal/adapter/feishu/projector.go` 继续保留 `Projector` 入口、`Operation` 组装与少量兼容 wrapper
  - 对共享过程卡（当前承载 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `file_change` / 被动 `context_compaction`），负责在首次发送时打开 `config.update_multi=true`，让后续同一窗口卡可被 `message.patch` 更新；首卡当前固定顶层 append，不继承 turn reply anchor。若当前窗口卡在 projector 层按“每行一个 element”的粒度已放不下，projector 会继续 patch 同一张卡：丢弃最旧可见行、把 `card_start_seq` 前移到当前窗口首行，并在顶部补一行“较早过程已省略，仅保留最近进度。”
  - 对 turn timeline 文本（非 final assistant 文本、`用户补充` 这类轻量 text event），负责根据 `ReplyToMessageID` 选择 reply 发送；reply 失败时 gateway 会回退到普通 text/card create。若某条 `OperationSendText` 携带显式 attention annotation，gateway 会改走结构化 `post` 文本，把单目标 `at` 与 attention 正文合并投递；当前正式启用的 attention 锚点都落在原卡片/主消息本身，不再派生独立 `attention_ping` timeline text
  - 对显式 `/compact` 这种 direct-command owner card，当前通过 patchable `FeishuPageView` 发送首卡，并依赖 `TrackingKey -> message_id` 回写把 running / terminal 状态继续 patch 回同一张卡
  - 当前是 selection / target-picker / page cards 最终 projection 的 owner：
    [internal/adapter/feishu/projector/target_picker.go](../../internal/adapter/feishu/projector/target_picker.go)
    负责把 `FeishuTargetPickerView` 投影成当前 target picker 卡片
    [internal/adapter/feishu/projector/selection_view.go](../../internal/adapter/feishu/projector/selection_view.go)
    负责把 `FeishuSelectionView + FeishuSelectionSemantics` 归一整理成本地 render model，供 button/dropdown 两类投影复用
    [internal/adapter/feishu/projector/selection_structured.go](../../internal/adapter/feishu/projector/selection_structured.go)
    负责 selection view 的统一结构化投影入口：VS Code `/list` 按钮式 instance view、VS Code `/use` / `/useall` dropdown、normal button-based selection 与 kick-thread confirm 当前都直接消费 `FeishuSelectionView`；`projector.go` 不再保留单独的 selection fallback 分支
    [internal/adapter/feishu/projector/page_catalog.go](../../internal/adapter/feishu/projector/page_catalog.go)
    负责把 `FeishuPageView` 投影成页面卡，并为按钮/表单写入 `page_action` / `page_submit` payload；当前 page callback 还会补写 `catalog_family_id` / `catalog_variant_id` / `catalog_backend` provenance，减少后续 variant page 对裸 `ActionKind` 的反推依赖
    [internal/adapter/feishu/projector/path_picker.go](../../internal/adapter/feishu/projector/path_picker.go)
    负责把 `FeishuPathPickerView` 投影成当前复用路径选择器卡片
- `orchestrator`
  - 负责 attach / use / follow / request gate / capture / new-thread 等产品状态
  - 负责 mixed/product-owned 动作仍然进入主 reducer 的那部分产品语义
  - 当前还会在 `UIEvent` 上额外挂出显式 `Feishu*Context`，作为 Feishu UI controller 的稳定 query/policy 输入

## 3. 当前 owner 分类表

| 交互面 | 当前 owner | 当前边界 |
| --- | --- | --- |
| `/menu` 首页 / 分组 / 返回 | `feishu-ui-owned (launcher)` | 当前由 Feishu UI controller 处理同一张命令菜单内的层级切换；首页仅保留分组导航入口，不再额外渲染“常用操作”区块；首页 breadcrumb 固定停在 `菜单首页`，不会再从 `/menu` 命令定义继承 `系统管理` 分组 breadcrumb 或回退按钮；首页分组按钮直接显示分组名，分组页才显示显式 `返回上一层`；normal mode 下像 `工作会话` 这种“分组根入口直接 handoff 到业务页”的情况，当前也通过 group-root command 元数据统一治理，不再靠 group id 特判；菜单导航状态当前通过 owner-card runtime 的 `command_menu + role=launcher` 记录，不再依赖旧 `Surface.MenuFlow` |
| `show_all_workspaces` / `show_recent_workspaces` | `feishu-ui-owned` | normal mode 下当前只负责重新打开 `/workspace list` 切换卡；`/list` 这类旧入口只是 alias，不再决定新的主展示结构 |
| `show_threads` / `show_all_threads` / `show_scoped_threads` | `feishu-ui-owned` | normal mode 下当前也只负责重新打开 `/workspace list` 切换卡；vscode mode 下会刷新当前实例的结构化 thread dropdown，不再维持旧分页 prompt；两条路径当前都会默认排除 `source=review` 的 detached review thread，不把 review session 当普通候选展示 |
| `thread_selection_page` | `feishu-ui-owned` | VS Code 结构化 thread dropdown 的 byte-budget 翻页动作；payload 携带 `view_mode + cursor(start-index)`。命中当前 surface 时 controller 会按当前 surface 状态重建 `FeishuThreadSelectionView`，projector 再按 transport budget 切出新页并 inline replace 当前卡，不引入 owner runtime |
| `show_workspace_threads` / `show_all_thread_workspaces` / `show_recent_thread_workspaces` | `feishu-ui-owned` | normal mode 下当前只负责用指定 workspace 重新打开 `/workspace list` 切换卡；legacy selection path 下才继续承担旧分页导航 |
| `target_picker_select_mode` / `target_picker_select_source` / `target_picker_select_workspace` / `target_picker_select_session` | `feishu-ui-owned` | 当前 normal mode 主路径实际使用的是四张独立工作会话卡：`/workspace list` 直接落 `target` 页，`/workspace new dir` / `git` / `worktree` 直接落各自业务页，因此 `target_picker_select_workspace` 是 `/workspace list` 与 `/workspace new worktree` 都会使用的主路径回调，`target_picker_select_session` 则只服务 `/workspace list`；`target_picker_select_mode` / `target_picker_select_source` 只保留 transport/runtime 兼容。命中当前 active picker 时都只原地替换当前卡，不直接改 route；切真实 workspace、显式改选 session，或在 worktree 卡上切换基准工作区时，都会按当前卡状态重建 picker read model |
| `target_picker_page` | `feishu-ui-owned` | `/workspace list` target page 与 `/workspace new worktree` 基准工作区 dropdown 的翻页动作；payload 携带 `picker_id + field_name + cursor(start-index)`。命中当前 active picker 时继续 inline replace 当前卡，不直接改 route。target page 的 workspace lane 翻页会把 cursor 指向的新 workspace 设为当前工作区并重算 session 候选；session lane 翻页会保留 workspace cursor / 选中工作区，但若原 session 掉出可见页则清空选中并禁用 confirm。worktree 页的 workspace lane 翻页则只更新基准工作区选择，并保留同卡 branch / directory 草稿 |
| `target_picker_open_path_picker` | `feishu-ui-owned` | 当前用于从 `/workspace new dir` / `/workspace new git` 主卡打开目录 path picker，并在打开前保留主卡草稿；命中当前 active picker 时直接原地替换当前卡 |
| `target_picker_cancel` | `feishu-ui-owned` | target picker 的显式退出动作；命中当前 active picker owner flow 时，会把当前卡同步 replace 成 sealed terminal card；普通编辑态是 `已取消`，Git import processing 态是 `已取消导入`，worktree processing 态是 `已取消创建`，并会分别 best-effort 停掉 clone / prepare 或 `git worktree add`；随后清掉 active target picker / owner-card flow |
| `target_picker_confirm` | `mixed` | callback 协议、picker ownership 与 freshness 校验仍属 Feishu UI；真正 attach / switch、按已选目录执行接入、按主卡 Git 表单 + 已选父目录执行导入，或按 worktree 主卡里的基准工作区 + 分支名 + 可选目录名执行创建，产品语义仍由 orchestrator 决定。当前 `/workspace list` 只做“已有工作区 + 已有会话”切换，不再暴露 `新建会话`；`/workspace new dir`、`/workspace new git` 与 `/workspace new worktree` 都会把同一张 owner card 推进到 processing / succeeded / failed，并在同卡 notice 区收口状态反馈；其中 Git/worktree 这两条依赖 Feishu 文本输入的 inline form 不再靠禁用按钮做前置校验，而是允许提交后由服务端原卡回写阻塞原因。Git import / worktree 长链路 processing 期间仍显式阻断普通输入，只保留 `/status` 与同卡取消 |
| `path_picker_enter` / `path_picker_up` / `path_picker_select` / `path_picker_page` | `feishu-ui-owned` | 当前由 Feishu UI controller 处理同一张路径选择器卡片内的浏览、返回、文件选择与下拉翻页；命中当前 active picker 时直接原地替换当前卡。复用路径选择器 projector 当前统一渲染成紧凑 `select_static`：目录模式提供“进入目录”下拉，文件模式提供“进入目录 + 选择文件”双下拉，target-picker owner-subpage 也复用同一目录 lane；当下拉候选过长时，projector 会按 Feishu transport byte budget 动态分页，并保证底部 footer 仍可见。目录 lane 的 `.` / `..` 属于固定项，不消耗 `cursor`；真实目录项里普通目录排在前，`.` 开头目录排在后。目录翻页保留当前目录；文件翻页会清空不可见文件选择并禁用 confirm，避免 invisible confirm |
| `path_picker_confirm` / `path_picker_cancel` | `mixed` | callback 协议与 owner/freshness 校验仍属 Feishu UI；这两类动作当前不在 inline-replace allow-list，回调会立即 ack 并异步处理；当前默认不再把“确认/取消成功”外发成新的主结果卡，而是优先在当前 picker 卡内 sealed 收口。若 consumer 返回新的可投影主卡，则交由 follow-up event 承接；target picker owner-flow 子步骤会把当前 path picker 卡换回主 owner card，独立 `/sendfile` picker 则会把 cancel、启动前失败与启动成功终态继续 patch 在当前 picker 卡上。只有旧卡 / 过期 / 非本人点击这类 freshness/ownership 拒绝仍保留为显式独立提示，不直接改写当前活跃 picker 卡 |
| bare `/history` / `history_page` / `history_detail` | `mixed` | 当前由 Feishu UI controller 先把 owner-card runtime v1 中的当前 history flow 同步切到 loading，再异步发起 `thread.history.read`；列表/详情结果与失败态默认继续 patch 回同一张 history owner card，loading/error 不再整块覆盖主区，而是保留摘要/业务区并把反馈放进 notice 区 |
| bare `/compact` | `mixed` | 文本入口当前会先由 orchestrator 建立 compact owner-card flow，并 append 一张 patchable direct-command card；若入口来自 stamped `/menu current_work` 卡，则当前菜单卡会直接被绑定成 compact owner card。dispatching / running / completed / failed 都继续 patch 同一张卡。被动 compact completion 不复用这条前台 owner card；quiet 静默，normal/verbose 则继续并入共享过程卡 |
| bare `/bendtomywill` / stamped `/menu maintenance -> /bendtomywill` | `mixed` | 文本或菜单入口当前都先走 daemon-side patch flow runtime：打开时会读取当前 attached thread 的 latest completed assistant turn 预览，并命中 refusal / placeholder 候选后生成一张 `request_user_input` 风格的多题 patch 卡。逐题回答时同一张卡会按 `request_revision` inline replace；全部题目完成后当前卡会切到 patchable progress page，随后 success / failure / rollback 结果继续 patch 同一张卡。若首卡还没有 `message_id`，runtime 会先用 `TrackingKey=flow_id` append，再在 gateway 分配 `message_id` 后回写；最近一次回滚按钮则通过 `page_action(ActionTurnPatchRollback, patch_id)` 继续收口到同一张卡。旧卡、他人点击、busy / VS Code / detached 拒绝，以及候选点不存在等路径，若入口来自 stamped 当前卡，会优先走 page-result replacement 收口，否则继续 append-only notice |
| stamped `/menu maintenance -> /help` / `/status` | `launcher -> terminal` | 点击后 daemon 会把 handler 的首个结果卡（帮助目录或 snapshot 状态卡）直接 `ReplaceCurrentCard`，同时把 `command_menu` launcher flow 标记为 terminal/已退出。纯文本 `/help` / `/status` 仍保持 append-only |
| stamped `/menu 首页 -> 工作会话` 与后续 `/workspace` / `/workspace new` / `/workspace list` / `/workspace new dir` / `/workspace new git` / `/workspace new worktree` | `launcher -> owner` | normal mode 下从菜单首页点 `工作会话` 会先把原菜单卡替成 bare `/workspace` 父页；该父页当前直接显示 `切换 / 从目录新建 / 从 GIT URL 新建 / 从 Worktree 新建 / 解除接管`，`/workspace new` 子页也继续保留三条新建入口。workspace page owner runtime 现在会显式记录 handoff 来源卡的 `message_id` 与 `from_menu`，因此从这些父页继续进入目录/Git/worktree 业务卡时，子卡 footer 的 `返回上一层` 会稳定回到父页或菜单，而不是退化到 target-picker 内部回退。该路径当前通过显式 `launcher -> owner` handoff 进入业务卡，不再依赖旧 `MenuFlow` 长驻语义。旧 alias `/list` / `/use` / `/useall` 直接输入时也会落到相同业务卡，但不再作为菜单主展示 |
| stamped `/menu current_work|switch_target -> /stop` / `/new` / `/follow` / `/detach` | `launcher -> terminal` | 点击后 daemon 会把 handler 返回的首个 notice / thread-selection 结果卡直接作为 `ReplaceCurrentCard`，并抑制重复终态 append。菜单 launcher flow 在 handoff 后立即退出，不再保留“半菜单半业务”活跃态 |
| stamped `/menu current_work -> /steerall` | `launcher -> owner(steerall)` | 点击后会把当前菜单卡直接交给 steer-all owner flow。requested 进度态继续 patch 同一张卡；completed、no-op、failed / disconnect restore 才会把这张卡封成 sealed terminal，不再留下可重复点击的旧菜单 |
| bare `/mode` / `/autowhip` / `/autocontinue` / `/reasoning` / `/access` / `/plan` / `/model` / `/verbose` | `mixed` | bare open-card 当前由 Feishu UI controller 处理；其中 `/mode` `/autowhip` `/autocontinue` `/reasoning` `/access` `/plan` `/verbose` 只保留固定选项，`/model` 额外保留一张手动输入表单。参数卡当前也统一承接 `body / notice / sealed` contract：业务区保留当前值/覆盖值/模型等上下文，notice 区承接成功、错误和 reopen 提示；若 apply 来自带 `daemon_lifecycle_id` 的当前参数卡 callback，则同一张参数卡会继续被 patch 成同卡反馈/终态；当这张参数卡本身是从菜单 launcher handoff 进来的 stamped 配置页时，即使终态 sealed，显式 footer 的 `返回菜单/返回上一层` 也会继续保留；page normalize 只会关闭主交互，不会再额外清掉这类显式 related buttons。其余 direct-open 路径若没有显式 footer，sealed 后也不会自动推导默认 back。`/plan` 的 apply 只改当前 surface 的后续 `PlanMode`，不会追溯改写当前 running turn；如果某轮 turn 结束时存在最终 `item/plan/delta`，后续 append 的“提案计划”卡则单独归到 `plan_proposal` owner family，不复用 bare `/plan` 参数卡本身；其中 stamped `/mode vscode` 若切换后立刻命中 legacy `editor_settings` 且存在可接管入口，daemon 会先同步静默自动迁到 `managed_shim`，成功后不再额外弹迁移提示卡；若仍需要可见下一步（缺 target、自动迁移失败、managed shim 修复、open prompt 或恢复提示），daemon 才会把首张可投影提示卡同位替回当前卡，并把该 surface 记录成可继续 patch 的 VS Code guidance card；后续异步 runtime 提示只要仍命中这块 card，就继续回写同一张卡；若是纯文本 slash 或其他非 card-owned 入口，则仍保持 append-only |
| `plan_proposal` | `mixed` | turn 完成后若本轮缓存了最终 `item/plan/delta`，orchestrator 会 append 一张 patchable `FeishuPageView` 提案计划卡；这张 page 当前显式设置 `SuppressDefaultRelatedButtons=true`，不会自动补默认 related back button。点击 `直接执行` / `清空上下文并执行` / `取消` 时，gateway 解析 `picker_id + option_id` 回到 `ActionPlanProposalDecision`，并继续在同一张卡上 seal 收口。该卡不是 request gate，不阻塞后续输入；一旦用户继续输入、切线程/切 route、开始新 turn 或卡片过期，也会被服务端 seal 成失效态 |
| `autocontinue_status` | `mixed` | 上游可重试失败进入 autoContinue overlay 时，orchestrator 会 append 一张 patchable `FeishuPageView` 状态卡。该卡显式 reply 到原始用户消息，并通过 `TrackingKey=AutoContinueEpisodeID` 回写 `message_id`；只要它仍是当前 surface 尾卡，scheduled / running / failed / cancelled 会继续 patch 回同一张卡。一旦后面出现新的消息，这张卡就冻结；后续同 episode 状态改为 append 新卡。该卡只承载自动继续状态，不接管后续业务输出的 reply anchor |
| bare `/review` / `/review commit` / `/review uncommitted` / 普通 final card review footer / review final card `放弃审阅` / `按审阅意见继续修改` | `mixed` | bare `/review` 与 `/review commit` 当前会先进入同一张 page-owner commit picker：卡片记录当前 `instance_id + parent_thread_id + thread_cwd + recent_commits`，并要求后续 submit/cancel 命中同一 `message_id` 才继续；若中途切换到其他实例，picker submit 会直接拒绝并要求重新发送 `/review commit`。picker submit 通过 `page_submit(surface.command.review + action_arg_prefix=commit)` 进入 detached review owner；bare `/review uncommitted`、常用工具菜单入口与普通 final card 上的 `Review 待提交内容` 则继续直接汇合到同一 detached review owner。普通 final card 的 commit review footer 仍保持 append-only：projector 只会在原 final chunk 底部追加 `评审 <short-sha>` 按钮，不 patch 掉源 final card；这些按钮只对正文里命中的“当前仓库最近 commit”生效，并继续复用 `page_action + daemon_lifecycle_id` 的 freshness / old-card reject。review session final card 上的 `放弃审阅` / `按审阅意见继续修改` 仍复用 stamped first-result replacement，在当前审阅结果卡上同位收口。真正的 detached review session 启动、review runtime 清理，以及把审阅结果带回 parent thread 继续修改，仍由 orchestrator 承接。普通 final card 只有在对应 thread cwd 落在 Git repo/worktree 内且存在未提交内容时才会追加 `Review 待提交内容`；commit footer 则只依赖最近 commit 命中，不依赖 dirty 状态；review session 内的 final card 只保留 `放弃审阅` / `按审阅意见继续修改` 两个显式出口 |
| bare `/cron` / `/upgrade` / `/restart` / `/debug` | `mixed` | 参数不足时当前统一打开 `FeishuPageView` 根页，不再顺手展示独立状态卡；根页现在只保留实际菜单入口，不再混入“快捷操作 / 手动输入 / 说明文案”，其中 `/debug` 根页当前仅保留 `管理页外链`，`/upgrade track` 子页当前仅保留 track 切换按钮，`/restart` 根页当前仅保留一个 `重启 child` 按钮，并明确说明它只重启当前 attached instance 的 provider child、不重启 daemon。若来自带 `daemon_lifecycle_id` 的当前 page callback，且动作属于“不立即执行”的根页 / 子页 / 非法参数回显路径，daemon 会走 page result replacement，把下一张 page 继续同位替回当前卡；真正立即执行的动作（如 `/cron reload`、`/cron repair`、`/cron run <id>`、`/upgrade latest`、`/upgrade codex`、允许 dev feed 的 flavor 下的 `/upgrade dev`、源码 `dev` flavor 下的 `/upgrade local`、`/restart child`、`/debug admin`）仍进入各自原有执行流。文本或表单输入的非法参数当前不会外跳 notice，而是继续留在同一张 page 上显示错误并保留表单默认值；`/restart` 根页若检测到 detached、实例离线、surface busy、abandoning，或已有 child restart 在飞，会直接把按钮禁用并把原因留在当前页上 |
| stamped `/vscode-migrate` / `vscode_migrate_owner_flow` | `mixed` | `/vscode-migrate` 当前先打开 `FeishuPageView` root page；若入口来自带 `daemon_lifecycle_id` 的当前卡 callback，daemon 会走 page-result replacement，把 root page / 校验失败页 / `仅 VS Code 模式可用` 页同位替回当前卡。真正执行迁移的按钮当前发 `vscode_migrate_owner_flow` callback，迁移结果与后续 `/list` / open VS Code / 恢复提示都会继续 patch 在同一张 guidance card 上，不再经由旧文本重解析回调或 bare continuation |
| `request approve` / `approval_command` / `approval_file_change` / `approval_network` / `request_user_input` / `tool_callback` / `permissions_request_approval` / `mcp_server_elicitation` / `captureFeedback` | `mixed` | 卡片按钮、表单字段、`request_control` payload、lifecycle stamp 属于 Feishu UI；request gate、反馈 capture、request family 统一的 `editing -> waiting_dispatch -> resolved/restore` 生命周期，以及由 orchestrator 单点 request presentation owner 基于 `requestType/rawType/metadata` 归一化出的 `SemanticKind + Title/Sections/Options/Questions/HintText` contract，属于产品状态机。`tool_callback` 当前也走同一 owner，但落成只读 fail-closed auto-dispatch；projector 当前只消费 `FeishuRequestView`，不再自己回猜 approval / permissions / MCP subtype |
| `attach_instance` / `attach_workspace` / `use_thread` | `product-owned` | 卡片只负责把选择结果送入产品层；是否允许接管、是否跨 workspace、接管后进入什么 route 都由 orchestrator 决定 |
| `/follow` | `product-owned` | 是否可用、是否被冻结、跟随到哪个 thread、normal/vscode mode 差异都属于 core 状态机 |
| `/new` | `product-owned` | 是否进入 `new_thread_ready`、何时消耗第一条消息、request gate 是否阻断都属于 core 状态机 |

注：

- 上表中的 `mixed` 只描述当前实现里 ownership / callback / reducer 的分层现状，不代表产品要求长期保留“半菜单半业务”形态。
- 当前产品复核结论是：现有已确认需求中，没有任何路径强制要求菜单卡与业务 owner 长期共存；后续如继续收口，应优先改成显式 `launcher -> owner/terminal` handoff，再删除对应 mixed 链路。
- 这条收口工作的后续执行跟踪见 GitHub issue `#359`。

补充规则：

- request cards 现在跨 `UIEvent` 边界携带的是 `control.FeishuRequestView`
  - `FeishuRequestView` 当前已经是独立的 UI-owned request view，不再借用 retained direct request DTO alias 过边界
  - orchestrator 会一次性写入 `SemanticKind`、`HintText`、`Sections`、`Options`、`Questions`
  - projector 直接把它当作 request-card owner payload 渲染，不再依赖 `FeishuDirectRequestPrompt` 这类过渡形状，也不再额外读取 `requestKind` 回猜 subtype
- command/config cards 当前已分为两条 read-model 边界：
  - `/menu`、bare config cards、bare `/cron` `/upgrade` `/restart` `/debug` 根页当前跨边界统一携带 `control.FeishuPageView`（`UIEventFeishuPageView`）
  - compact / steerall / sendfile terminal、plan proposal、upgrade owner-flow、vscode guidance 等活跃 owner-card 路径当前也统一携带 `control.FeishuPageView`
  - `control.FeishuCatalogView` 当前只保留 orchestrator 内部的 menu/config/page 语义 read model；controller/daemon 在发卡前会先统一收敛成 `FeishuPageView`，adapter 只消费 `UIEventFeishuPageView`；最终卡片都遵循 `业务区 -> notice 区 -> footer`，notice 区仅在存在内容时出现
  - compact owner card 当前已完全迁到 page contract：running 态保留“当前会话”业务区，终态把结果提示放在 notice 区并标记 sealed
- `control.FeishuTargetPickerView` 当前已经是 normal mode `/workspace list` / `/workspace new dir` / `/workspace new git` / `/workspace new worktree` 跨 `UIEvent` 边界的主载体：
  - 旧的 normal-mode unified target picker 产品形态已经下线；现在跨边界携带的仍是 `control.FeishuTargetPickerView`，但它分别承接四张独立业务卡
  - projector 直接以它为 owner 生成 `target_picker_*` callback payload
  - dropdown 刷新与 confirm 已不再经由 `FeishuDirectSelectionPrompt` 兜底
  - `Page` / `StageLabel` / `Question` 当前仍是 editing / processing / terminal 的稳定页头合同，但 normal mode 主路径已经改成：`/workspace list` 直接落 `Page=target`，`/workspace new dir` 直接落 `Page=local_directory`，`/workspace new git` 直接落 `Page=git`，`/workspace new worktree` 直接落 `Page=worktree`
  - `FeishuTargetPickerPageMode` / `FeishuTargetPickerPageSource` 仍保留在 DTO / callback 协议里做兼容，但当前主路径默认不会把它们投影给 normal-mode 用户
- `control.FeishuSelectionView` 当前已经是 live selection UI 的主载体：
  - VS Code `/list` 的 instance selection、VS Code `/use` / `/useall` 的 thread selection，以及 kick-thread confirm，当前都直接跨 `UIEvent` 边界携带 `FeishuSelectionView`
  - title / layout / context / hidden-entry hint 当前统一由 `control.DeriveFeishuSelectionSemantics(...)` 派生，orchestrator 的 `FeishuSelectionContext` 与 adapter projector 共用这一份语义 owner，不再各自重复推导
  - VS Code thread dropdown 当前会直接隐藏 disabled thread，并用 plain-text hint 提示“已省略当前不可切换的会话”
  - VS Code `/use` / `/useall` 的大候选集不再整包塞进单个 `select_static`；projector 会按 Feishu transport byte budget 做动态分页，并通过 `thread_selection_page(view_mode + cursor)` 同卡翻页
  - 若当前 thread 不在当前 dropdown 可见页，projector 不会再给 `select_static` 写 `initial_option`
- `control.FeishuDirectSelectionPrompt` 仍然保留，但当前只剩测试 / mockfeishu 兼容用途，不再是 live adapter 输入：
  - workspace/thread selection live 路径已经不再从 `FeishuSelectionView` 回退投影成 `FeishuDirectSelectionPrompt`
  - kick-thread confirm 等 live selection 也直接消费 `FeishuSelectionView`
- `control.FeishuPathPickerView` 当前已经是路径选择器跨 `UIEvent` 边界的主载体：
  - projector 直接以它为 owner 生成 `path_picker_*` callback payload
  - 当前不会再把目录浏览过程编码回 `FeishuDirectSelectionPrompt`
  - 当 path picker 作为 target picker owner-flow 子步骤打开时，`StageLabel` / `Question` 会把卡片切到 compact owner-subpage 布局：页头、允许范围、当前位置、目录下拉，以及 `返回` / `使用这个目录` 双按钮
- 这些 DTO 当前都已经显式标注 owner，并与 query/policy context 分离：
  - DTO 形状暂未全部迁出
  - `UIEvent` 已经携带独立的 `FeishuSelectionContext` / `FeishuPageContext` / `FeishuRequestContext`（legacy command 路径仍保留 `FeishuCommandContext`）
  - Feishu UI controller 已通过这层 boundary 分流 pure navigation；后续继续扩 controller 时，默认仍应优先依赖这些 query/context 元数据，而不是继续直接读 orchestrator 内部字段
  - target picker cards 现在是 “read model -> `FeishuTargetPickerView` -> adapter projection -> Feishu V2 卡片” 三段；后续修改 normal `/list` / `/use` / `/useall` 的默认选择、页头单题文案、前置阻塞校验、confirm 按钮或 stale-selection 行为时，默认应落在 target picker read model / projection 层，而不是回到旧 selection query 函数里继续混改
  - selection cards 现在主要服务于 VS Code instance/thread selection 与 kick-thread confirm；它们已经统一成 “read model -> `FeishuSelectionView` / `FeishuSelectionSemantics` -> adapter structured projection -> Feishu V2 卡片” 三段。后续修改这些路径的交互与文案时，默认应落在 selection semantics / structured projection / selection view 结构层，而不是重新引入 prompt compat 载体
  - menu/config/root pages 现在统一是 “`FeishuCatalogView` -> `FeishuPageView` -> page projector -> Feishu V2 卡片” 三段；直接 owner-flow 页面则是 “`FeishuPageView` -> page projector -> Feishu V2 卡片”；后续修改 `/menu` 或 bare config cards 的 breadcrumbs、按钮布局、回退按钮、摘要文案时，默认应落在 catalog/page read model 或 page projector 层
- `ActionShow*` 与 bare config `Action*Command` 当前若仍存在，属于 gateway / parser 的 transport compatibility 层；live path 会先归并到 `FeishuUIIntent`，不再代表主产品 reducer owner。
- 如果只是换卡片样式、按钮 payload、inline replace 策略，优先更新本文。
- 如果改了 DTO 里的可选项语义、route 约束或 request gate 行为，必须同时更新 core 状态机文档。

## 4. Callback Payload 协议面

### 4.1 当前统一字段

所有需要回到 daemon 的 Feishu 卡片 callback，当前至少依赖这些字段：

| 字段 | 来源 | 作用 |
| --- | --- | --- |
| `kind` | button/form `value.kind` | 决定 gateway 解析成哪种 `control.Action` |
| `daemon_lifecycle_id` | projector stamp 到按钮/form | 允许 daemon 判定“这张卡是否来自当前 daemon 生命周期” |

当前 owner：

- callback payload schema 已收束到 [internal/core/frontstagecontract/callback_payload.go](../../internal/core/frontstagecontract/callback_payload.go)
- projector、gateway、daemon owner-card producer 与 orchestrator owner-card producer 现在共用这份 schema 常量/构造 helper，不再继续各自扩一份裸字符串约定
- `page_action` / `page_submit` 当前除 `action_kind` 与参数字段外，还会在有来源上下文时附带 `catalog_family_id` / `catalog_variant_id` / `catalog_backend`；gateway 解析后会直接回填 `Action.Catalog*`，旧 payload 仍保持兼容
- `request_respond` / `submit_request_form` 与 `upgrade_owner_flow` / `vscode_migrate_owner_flow` 当前在 gateway 解析后只写入 `Action.Request` / `Action.OwnerFlow` family；这些回调不再依赖 root `Action.Request*` 或 root `PickerID/OptionID` 兼容字段作为 live 路径输入
- 分页导航与 target/path picker / selection dropdown 当前也复用这套 schema：projector 负责写入 `page` / `view_mode` / `return_page` / `picker_id` / `field_name` / `cursor`，gateway 负责解析回 `control.Action`，Feishu UI controller 再用这些字段重建当前页 view、thread selection view 或当前 picker 并 inline replace 原卡。其中 thread/history 等固定分页仍用 `page`，target/path picker / VS Code thread dropdown 的动态 byte-budget 分页改用 `cursor(start-index)`。
- workspace/session family 的 selection / target picker callback 当前也会在有来源上下文时继续补写 `catalog_family_id` / `catalog_variant_id` / `catalog_backend`。这意味着 bare `/list` `/use` `/useall`、菜单按钮、selection refresh、target-picker 翻页/确认这些后续动作，都能继续把 family provenance 带回 gateway / orchestrator，而不是只剩 `ActionKind` / `field_name`。
- adapter 内部这组 lane/read-model contract 现统一由 `internal/adapter/feishu/selectflow` 承接：`PaginatedSelectFlowDefinition` 拥有默认 `field_name`、payload value key 与共享 pagination hint；projector 与 gateway 通过同一份定义对齐 lane 字段、hint 和 callback recover 规则。

### 4.2 当前常见 payload 字段

| `kind` | 关键字段 | 当前含义 |
| --- | --- | --- |
| `attach_instance` | `instance_id` | 接管指定实例 |
| `attach_workspace` | `workspace_key` | 接管指定工作区 |
| `use_thread` | `thread_id`、`field_name`、`allow_cross_workspace` | 选择 thread；VS Code 结构化 thread dropdown 走 `field_name` + `form_value/option` 取值，其余按钮路径仍可直接带 `thread_id` |
| `thread_selection_page` | `view_mode`、`cursor` | VS Code 结构化 thread dropdown 的 byte-budget 翻页回调；`view_mode` 指明当前是 recent / all / scoped_all 哪条 direct selection flow，`cursor` 是候选项 start-index。服务端只重建当前 selection view，不持久化 owner runtime；projector 若发现当前 thread 不在新页，会清空 `initial_option` |
| `show_threads` / `show_all_threads` / `show_scoped_threads` | `view_mode`、`page` | normal mode 下重新打开 `/workspace list` 切换卡；vscode / legacy selection path 下仍用于在当前 same-context thread 列表里切页。当前普通线程候选会显式过滤 `source=review` 的 detached review thread |
| `show_all_workspaces` / `show_recent_workspaces` | `page` | normal mode 下重新打开 `/workspace list` 切换卡；旧分页字段继续保留 transport 兼容 |
| `show_all_thread_workspaces` / `show_recent_thread_workspaces` | `page` | normal mode 下重新打开 `/workspace list` 切换卡；旧分页字段继续保留 transport 兼容 |
| `show_workspace_threads` | `workspace_key`、`page`、`return_page` | normal mode 下以指定 workspace 重新打开 `/workspace list` 切换卡，并预填当前 workspace；legacy selection path 下仍可表示进入某个 workspace 的会话详情 |
| `target_picker_select_mode` | `picker_id`、`target_value` | 兼容保留的模式页回调；gateway 直接从 payload 里的 `target_value` 取当前模式，但当前 normal-mode 主路径默认不会发出这类回调 |
| `target_picker_select_source` | `picker_id`、`target_value` | 兼容保留的来源页回调；当前 projector 仍支持把 `local_directory` / `git_url` 写入 payload，但 normal-mode 主路径默认不会发出这类回调 |
| `target_picker_select_workspace` | `picker_id`、`field_name` | `/workspace list` 切换卡与 `/workspace new worktree` 基准工作区下拉的回调；gateway 从 `form_value[field_name]` / `option` / `options` 中提取工作区键 |
| `target_picker_select_session` | `picker_id`、`field_name` | `/workspace list` 切换卡的会话下拉回调；gateway 从 `form_value[field_name]` / `option` / `options` 中提取 thread；当前 normal-mode 卡不再发出 `new_thread` |
| `target_picker_page` | `picker_id`、`field_name`、`cursor` | `/workspace list` target page 与 `/workspace new worktree` 基准工作区 dropdown 的翻页回调；`field_name` 区分 workspace / session lane，`cursor` 是动态 byte-budget 分页使用的 start-index。target page 的 workspace 翻页会把对应 cursor 处的 workspace 设为当前选择并重算 session 列表；session 翻页会保留 workspace 状态，但显式清空当前 session 选择，避免 invisible confirm。worktree workspace 翻页会保留 branch / directory 草稿，只重算基准工作区与目标路径预览 |
| `target_picker_open_path_picker` | `picker_id`、`target_value`、`request_answers` | `/workspace new dir` / `/workspace new git` 主卡的子步骤导航；当前 `target_value` 表示 `local_directory` 或 `git_parent_dir`，`request_answers` 用来把 Git 主卡里的 `repo_url` / `directory_name` 草稿一起带回服务端 |
| `target_picker_cancel` | `picker_id`、`request_answers` | 四张独立工作会话卡共用的退出按钮；gateway 只需命中当前 active picker，并把必要草稿带回；服务端随后会把当前卡封成对应 terminal 态：普通编辑态为 `已取消`，Git import processing 态为 `已取消导入`，worktree processing 态为 `已取消创建`，随后清掉 active picker / owner-card flow |
| `target_picker_confirm` | `picker_id`、`target_picker_workspace`、`target_picker_session`、`request_answers` | 四张独立工作会话卡共用的确认按钮：`/workspace list` 把当前表单值送到产品层执行 attach / switch；`/workspace new dir`、`/workspace new git` 与 `/workspace new worktree` 都会在同一张 owner card 上进入 processing / terminal，其中前者要求目录已通过前置校验且不能命中已知 workspace，Git 路径要求 repo / 落地父目录预览有效，worktree 路径则从 `request_answers` 里恢复 `target_picker_worktree_branch_name` / `target_picker_worktree_directory_name` 草稿，并要求基准工作区、分支名和目标路径预览有效；Git 长链路会先 patch 成 `正在导入 Git 工作区`，随后在 clone 成功后继续 patch 到“接入工作区 / 准备会话”，并允许同卡 `取消导入`；worktree 长链路会先 patch 成 `正在创建 Worktree 工作区`，随后在创建成功后继续 patch 到“正在接入工作区”，并允许同卡 `取消创建` |
| `history_page` | `picker_id`、`page` | `/history` 列表页翻页；命中当前 history owner-card flow 时同步替换当前卡为 loading，然后异步重查当前 thread history |
| `history_detail` | `picker_id`、`turn_id` 或 `field_name + selected option` | `/history` 进入某一轮详情，或在详情页前后切换；命中当前 history owner-card flow 时同样先切 loading；gateway 继续兼容 `form_value[field_name]` / `option` / `options` 取值 |
| `upgrade_owner_flow` | `picker_id`、`option_id` | `/upgrade latest` 与 `/upgrade codex` 共用的 daemon owner-card 显式动作；gateway 只解析 `picker_id` / `option_id`，daemon 再按 flow id 前缀路由到 release 或 Codex flow。release flow 当前使用 `check` / `confirm` / `cancel`，Codex flow 当前使用 `check` / `confirm`；两者都要求命中当前 active flow id，旧卡或他人卡片不会继续改写升级状态。首卡若没有现成 `message_id`，会先以 page `TrackingKey` append，待 gateway 分配 `message_id` 后再回写到对应 owner flow，后续 checking / confirm-ready / running / terminal 一律 patch 同一张卡 |
| `plan_proposal` | `picker_id`、`option_id` | 提案计划卡的 owner-flow callback；`option_id` 当前只用 `execute` / `execute_new` / `cancel`。gateway 只负责按当前 active proposal id 解析回 `ActionPlanProposalDecision`；真正的 `PlanMode=off`、继续派发 follow-up turn，以及 seal 当前卡，仍由 orchestrator 决定 |
| `page_action` | `action_kind`、`action_arg(可选)`、`catalog_family_id(可选)`、`catalog_variant_id(可选)`、`catalog_backend(可选)` | page 卡按钮的结构化动作；gateway 解析后直接写回 `Action.Kind`，并用 `BuildFeishuActionText` 生成 canonical `Action.Text`；若 payload 携带 catalog provenance，也会同步写回 `Action.Catalog*`。当前 `/bendtomywill` success card 的“回滚最近一次修补”会用这条 payload 携带 `action_kind=surface.command.turn_patch.rollback` 与 `action_arg=patch_id`；`/cron list` 的“立即触发”按钮现在也显式走这条 payload，使用 `action_kind=surface.command.cron` 与 `action_arg=run <record-id>`，daemon 只在 card-action 路径兼容这类内部 run 动作，直接文本输入 `/cron run <record-id>` 已不再视为用户命令；review family 的 final-card footer 也走这条 payload：`surface.button.review_start` 只出现在普通 final card 的 uncommitted 入口，`surface.command.review + action_arg=commit <full-sha>` 用于普通 final card 的 commit review footer，`surface.button.review_discard` / `surface.button.review_apply` 只出现在 review session final card |
| `page_submit` | `action_kind`、`field_name`、`action_arg_prefix(可选)`、`catalog_family_id(可选)`、`catalog_variant_id(可选)`、`catalog_backend(可选)` | page 卡表单提交；gateway 从 `form_value[field_name]`/`option` 取参数，按 `action_arg_prefix + 参数` 组装后写回 `Action.Text`；若 payload 携带 catalog provenance，也会同步写回 `Action.Catalog*`。当前 `/review commit` picker 就是这条路径：`action_kind=surface.command.review`、`field_name=commit_sha`、`action_arg_prefix=commit`，最终会拼成 canonical `/review commit <full-sha>` |
| `path_picker_enter` | `picker_id`、`entry_name` 或 `field_name + selected option` | 进入当前 active picker 里的一个子目录；`/sendfile` 文件模式下通常来自目录下拉 |
| `path_picker_up` | `picker_id` | 回到当前 active picker 的上一级目录 |
| `path_picker_select` | `picker_id`、`entry_name` 或 `field_name + selected option` | 在当前 active picker 里选择一个文件或目录；`/sendfile` 文件模式下通常来自文件下拉，当前只更新待发送文件，不直接触发发送 |
| `path_picker_page` | `picker_id`、`field_name`、`cursor` | path picker dropdown 的 byte-budget 翻页回调；`field_name` 区分目录 / 文件 lane，`cursor` 是候选项 start-index，不包含固定 `.` / `..`。目录翻页只更新可见候选页；文件翻页会保留当前目录，但显式清空文件选择并禁用 confirm，避免 invisible confirm |
| `path_picker_confirm` | `picker_id` | 用当前 active picker 的已校验结果触发 consumer handoff；若 picker 带有 `owner_flow_id` 且命中 target picker owner card，consumer 可直接回填并 patch 原 owner card；独立 `/sendfile` picker 则会在 confirm 后保留自身 lifecycle，启动前失败继续 patch 当前卡，启动成功把当前卡封成 terminal |
| `path_picker_cancel` | `picker_id` | 结束当前 active picker，并把取消结果交给 consumer 或默认 notice；target picker 子步骤当前会直接恢复原 owner card，而不是额外发一张取消卡 |
| `request_respond` | `request_id`、`request_type`、`request_option_id`、`request_answers`、`request_revision` | 响应 approval、`approval_command`、`approval_file_change`、`approval_network`、`request_user_input`、`permissions_request_approval`、`mcp_server_elicitation`。approval family 的按钮集合仍跟随上游 `availableDecisions` 归一化结果，包括 `cancel`；但卡面标题、正文、hint 与 MCP form/url、permissions grant 等 subtype 语义，当前都由 orchestrator 先写入 `FeishuRequestView.SemanticKind/HintText` 后再渲染。顶层 `tool/requestUserInput` 与 `item/tool/requestUserInput` 继续共用 `request_user_input` 提交流程；`permissions_request_approval` 通过按钮直接携带 scope 语义；`request_user_input` 与 form 模式 `mcp_server_elicitation` 会用这条 payload 承载纵向 direct-response 按钮与恢复态 `重新提交`；url 模式 `mcp_server_elicitation` 仍直接承载 continue/decline/cancel。对于真正的 pending request，服务端当前都会先把当前卡 inline replace 成 sealed `waiting_dispatch` 只读态，再下发真正的 request response 命令；当前 `/bendtomywill` 编辑卡也复用这条 payload family，但它不是 core `PendingRequest`：服务端会先按 `request_revision` 保存 patch 草稿、必要时 inline 刷到下一题；全部题目完成后不会下发 request response 命令，而是切到 daemon-side patch transaction progress page。`tool_callback` 当前不通过任何用户可点击的 `request_respond` 回调；卡片落地后会由服务端自动派发结构化 unsupported 响应 |
| `request_control` | `request_id`、`request_type`、`request_control`、`question_id(可选)`、`request_revision` | 承载 request 的非回答型动作。当前 live 路径主要使用 `skip_optional`、`cancel_turn`、`cancel_request`：`skip_optional` 会把当前 optional 题标记为已跳过，并在必要时直接触发最终 dispatch；真实 pending request 上的 `cancel_turn` 会把 `request_user_input` 当前卡 seal 为终态后发 `turn.interrupt`；`cancel_request` 则用于 form 模式 `mcp_server_elicitation` 的请求级取消。当前 `/bendtomywill` 编辑卡同样复用这条 payload，但其 `cancel_turn/cancel_request` 只会把 patch 卡封成 `已取消`，不会向当前 thread 发送 interrupt |
| `submit_request_form` | `request_id`、`request_type`、`request_revision`、`field_name(可选)` | 从表单里提取 `request_answers` 后回到 request 响应路径；当前用于顶层/`item` 两种 `request_user_input` 以及 form 模式 `mcp_server_elicitation`。表单只负责提交当前题，服务端会决定是“保存后自动跳到下一题”还是“当前题答完后直接 seal + dispatch” |

### 4.3 当前表单提交规则

`gateway_routing.go` 当前约定：

- `page_submit`
  - 必须携带 `value.action_kind`，并按它直接回填 `Action.Kind`
  - 若 payload 同时带 `catalog_family_id` / `catalog_variant_id` / `catalog_backend`，gateway 会直接把它们回填到 `Action.Catalog*`；缺失时继续兼容旧 payload，不阻断当前卡片
  - `field_name` 为空时默认读取 `command_args`
  - 命令表单当前同时兼容普通 `input` 与 `select_static`
  - 对带文本输入的 page form，提交按钮当前默认保持可点；参数格式、环境前置条件或业务校验失败统一在提交后由服务端回写到当前卡片，而不是要求前端先禁用按钮
  - `select_static` 命令字段当前只投影 `placeholder/options/initial_option`；组件级 `label` 不会下发，因为飞书会把它判成非法字段
  - 参数读取顺序为：`form_value[field_name] -> action.option -> action.options[0] -> input_value`
  - 若 payload 带 `action_arg_prefix`，gateway 会先拼前缀再拼表单值，最后用 `BuildFeishuActionText` 组装 canonical `Action.Text`
  - 该路径不再依赖 `command_text`，避免 page 卡回调退化成裸文本重解析
- `page_action`
  - 必须携带 `value.action_kind`，并按它直接回填 `Action.Kind`
  - 若 payload 同时带 `catalog_family_id` / `catalog_variant_id` / `catalog_backend`，gateway 会直接把它们回填到 `Action.Catalog*`
  - 若 payload 不带 catalog provenance，服务端仍会在进入产品状态机前按当前 surface context 做 best-effort provenance 补齐，保证旧卡兼容
- `submit_request_form`
  - 优先把 `form_value` 整体转成 `request_answers`
  - `request_user_input` 与 form 模式 `mcp_server_elicitation` 当前都只会为“需要手填”的字段渲染 form input（纯选项题不再渲染自由输入框）
  - 当前不再额外携带 `request_option_id`
  - 对需要手填的 request / MCP 表单，`提交` 按钮当前也默认保持可点；缺字段、格式错误或 dispatch 前校验失败时，服务端会刷新同卡状态，而不是要求前端做实时禁用
  - 表单按钮文案当前统一为 `提交`
  - 这一步只提交当前题：
    - 若仍有未完成题，orchestrator 会保存草稿、递增 `request_revision`、把当前卡 inline replace 到下一题
    - 若当前题提交后已经凑齐完整答案，orchestrator 会先把当前卡 seal 成等待态，再派发最终 request response
  - `request_user_input` 与 form 模式 `mcp_server_elicitation` 的 optional 字段当前都必须显式回答或显式 `skip_optional`；仅仅留空不会被视为已完成
  - 旧的“显式最终提交 / 留空确认提交 / 取消确认提交”按钮流当前已不再是 live UI 合同；服务端只保留旧 `request_respond(step_*)` 的兼容处理，不再由 projector 生成这些按钮
  - request 卡的按钮与表单提交都会携带 `request_revision`
    - 只要当前 request 因为“当前题已保存”“跳过 optional”“进入 sealed waiting”“提交失败恢复”而刷新，revision 就会递增
    - 同 daemon 生命周期里的旧 request 卡，如果 revision 落后于当前 pending request，会收到 `request_card_expired`，不会再改写当前草稿状态
  - 非回答型动作当前统一改走 `request_control`
    - `skip_optional` 需要额外带 `question_id`
    - `cancel_turn` / `cancel_request` 不再复用 `request_respond`
  - 若表单没有字段值，再回退 `input_value`
  - approval 旧写法 `approved=true/false` fallback 已下线；approval 决策必须走 `request_option_id`
- `path_picker_enter` / `path_picker_select`
  - 旧按钮路径继续直接读取 `entry_name`
  - `select_static` 路径允许 payload 只带 `field_name`
  - gateway 当前按 `action.option -> action.options[0] -> form_value[field_name]` 的顺序提取被选中的目录/文件条目
  - 这样 projector 可以把复用 path picker 统一收敛成紧凑下拉，而不必继续为每个条目单独渲染按钮；当前目录模式使用单目录下拉，文件模式使用目录/文件双下拉
  - 目录下拉当前会在 `CanGoUp=true` 时额外插入一个值为 `..` 的首项；这条值仍然走 `path_picker_enter`，最终由 orchestrator 复用现有 root-boundary 校验解析到父目录
  - 当前路径选择器卡片已不再额外渲染 `path_picker_up` 按钮；目录下拉里的 `..` 是默认“返回上一级”入口，因此卡面统一保持“目录浏览走目录下拉、文件选择走文件下拉（若有）、确认/取消走底部按钮”的结构
- `path_picker_page`
  - projector 当前把 path picker 目录/文件下拉的超长候选改成 byte-budget 分页，并用 `path_picker_page(picker_id + field_name + cursor)` 触发同卡翻页
  - `cursor` 表示候选项 start-index；目录 lane 固定项 `.` / `..` 会始终保留在当前页里，不参与 `cursor` 计算
  - 目录翻页只更新当前可见目录切片；文件翻页会主动清空已选文件，避免用户在看不到旧选择的情况下继续 confirm
- `target_picker_open_path_picker` / `target_picker_confirm`
  - `Git URL` 分支当前会把 `form_value` 里的 `target_picker_git_repo_url` 与 `target_picker_git_directory_name` 解析成 `request_answers`
  - `open_path_picker` 与 `confirm` 都会携带这份草稿，服务端据此回填 active target picker record
  - `Git URL` 与 `Worktree` 这两类 inline form 当前都按“submit-time validation”处理：`克隆并继续` / `创建并进入` 保持可点，缺少 repo、branch、基准工作区，或预览/环境校验失败时，服务端会把具体阻塞原因 patch 回同一张 owner card，而不是要求前端先把按钮禁掉
  - 这样即使用户在 Git 主卡和 path picker 子步骤之间来回切换，仓库地址与目录名草稿也不会丢失，而且不会进入 `PendingRequest`

`request_user_input` 卡片当前额外的可视语义：

- 卡片顶部会展示 `回答进度 x/y · 当前第 N 题`
- answering 态当前只渲染**当前题**，不再把所有题一口气铺在同一张卡里
- 当前题会先渲染固定题号标题 `问题 N/M`，再把题目标题/说明/选项/答案聚合到同一个 `plain_text` 正文块，避免动态文本重新回流到 markdown
- 当前题会展示 `状态：已回答/已跳过/待回答`
- 对于非私密题，已暂存答案会显示为 `当前答案：...`
- 对 direct-options 题，若已有已答值，已选项保持 `primary`，其他选项降为 `default`，用于降低误触成本
- 对 direct-options 题，当前改成**纵向按钮**；点击选项会直接把当前题答案写回草稿并自动推进到下一题
- 对需要手填的题，表单区当前只渲染当前题，并收敛成“左侧输入、右侧 `提交` 按钮”的单行紧凑表单；提交后同样自动推进
- optional 题若当前未回答，会在正文下方额外显示 `跳过` 按钮
- 底部当前只保留 `取消` footer，不再渲染 `上一题 / 下一题 / 保存本题 / 提交答案`
- 当前题答完后若已经凑齐整套答案，卡片会先 inline replace 成 sealed waiting 态，再下发真正的 request response
- 若 dispatch 失败或 wrapper 显式 reject，会清掉 pending-dispatch 标记、递增 `request_revision`、刷新一张新的可编辑 request 卡并附带失败 notice；当所有题其实都已完成但 request 又重新进入交互态时，卡片会额外显示一个恢复用 `重新提交` 按钮
- 顶层 `tool/requestUserInput` 与 `item/tool/requestUserInput` 当前都复用这一套卡片、草稿暂存与自动推进状态机
- 真正发起 request 提交后，pending request 不会立刻从 orchestrator 状态里删除；会先记录 `PendingDispatchCommandID`
  - 成功路径仍由上游 `request_resolved` 事件最终清掉 pending request
  - 若 daemon dispatch 失败或 wrapper 显式 reject，会清掉 pending-dispatch 标记、递增 `request_revision`、刷新一张新 request 卡并附带失败 notice
  - 在 pending-dispatch 期间，同一 request 的重复点击会收到“已提交，等待处理”提示，不会重复下发命令
  - `取消` 当前会先把卡片 seal 成“已放弃答题，并向当前 turn 发送停止请求”，再派发 `turn.interrupt`

通用 approval request 卡片当前新增的可视语义：

- request family 当前仍共用一套 pending request substrate，但卡面语义已收口到 orchestrator 的单点 presentation owner
  - `FeishuRequestView` 会显式携带 `SemanticKind`
  - 当前 live approval subtype 至少包括 `approval_command`、`approval_file_change`、`approval_network`
- `approval_command` / `approval_file_change` / `approval_network`
  - 不再只是“同一张 approval 卡换几段文案”；orchestrator 会先按 subtype 生成标题、正文 sections、按钮集合和 hint
  - 选项直接跟随上游 `availableDecisions` 归一化结果，当前至少覆盖 `accept`、`acceptForSession`、`decline`、`cancel`
  - `approval_command` 会额外展示 `cwd`、附加权限，并优先给出“告诉 Codex 怎么改”的命令向 hint
  - `approval_file_change` 会额外展示 `grantRoot`，并给出写入范围导向的 hint
  - `approval_network` 会把 `networkApprovalContext` 投影成主机/协议/端口等“网络目标”正文，并给出联网导向的 hint
  - 最终点击任一决策后，当前卡会先切到 sealed waiting 态，不再保留“看起来还能继续点”的旧按钮

MCP request 卡片当前新增的可视语义：

- `permissions_request_approval`
  - 渲染为单张 request 卡
  - 默认按钮是“允许本次 / 本会话允许 / 拒绝”
  - view 当前会带专用 hint：强调“仅本次”与“本会话允许”的 scope 差异
  - 按钮点击直接走 `request_respond`
  - 最终决策提交后会先切到 sealed waiting 态，再派发真正的 permission response
- `mcp_server_elicitation`
  - `mode=url`
    - 当前先归一化成 `mcp_server_elicitation_url` 语义，再投影成 continue/decline/cancel 卡
    - 渲染为 continue/decline/cancel 按钮卡
    - “继续”前允许先去外部页面完成授权或确认
    - continue/decline/cancel 任一决策提交后，当前卡会先切到 sealed waiting 态，再派发真正的 elicitation response
  - `mode=form`
    - 当前先归一化成 `mcp_server_elicitation_form` 语义，再进入单题表单状态机
    - 与 `request_user_input` 一样，当前只显示一题，并使用“固定题号标题 + plain_text 正文”显示字段说明与动态内容
    - 会优先把 top-level flat object schema 投影成字段列表
    - 简单枚举字段会直接渲染成纵向按钮，点击后回填局部草稿并自动推进
    - 需要手填的字段走单行 form submit，按钮文案统一为 `提交`
    - optional 字段可通过 `request_control(skip_optional)` 显式跳过
    - 底部只保留 `取消`，不再渲染 `上一题 / 下一题 / 提交并继续`
    - 当前题提交后若已凑齐完整内容，会先把当前卡 seal 成等待态，再真正把结果回写给 MCP server
    - 若 schema 超过当前平铺能力，会回退成单字段 JSON 输入，而不是直接 unsupported
- 这两类 MCP request 与 `request_user_input` 一样，都会继续携带 `request_revision`，用于阻止同 daemon 生命周期里的旧卡继续改写当前 request 草稿
- `tool_callback`
  - 当前渲染为只读 request 卡，不出现按钮、表单或 footer cancel
  - 卡片从第一次投影开始就处于 sealed `waiting_dispatch` 态，正文固定说明“当前客户端不支持执行该 callback，已自动上报 unsupported 结果”
  - projector 只渲染 plain_text 信息区和末尾状态 markdown，不会生成任何 `request_respond` / `request_control` / `submit_request_form` payload
  - orchestrator 在投影后会立即自动派发一条结构化 unsupported response；在上游 `request.resolved` 到来之前，这张卡仍作为 pending request 保持 gate
  - 若这条自动派发被本地 Codex 拒绝，当前卡会继续保持 sealed，只把状态文案改成“自动回写失败，可使用 `/stop` 结束本轮”，不会错误退回交互态

### 4.4 当前 surface 解析规则

卡片 callback 回到哪个 surface，当前按下面顺序解析：

1. 优先用 `open_message_id -> 已记录的 surfaceSessionID`
2. 如果消息映射找不到，再回退到 callback operator 的 preferred actor id
3. 最后才退到 `open_chat_id`

这个顺序是当前 P2P surface 不被拆裂的前提之一。

## 5. 当前同步 Replace 与 Append 边界

### 5.1 同步 replace 的必要条件

当前 `gateway` 会在命中以下任一路径时，同步等待 handler 结果并返回 callback replace：

1. frontstage contract 同步门槛
  - callback payload 带有非空 `daemon_lifecycle_id`
  - action 命中 `control.SupportsFeishuSynchronousCurrentCardReplacement(action)`
2. `inline_view` 路径
  - action contract 为 `CurrentCardMode=inline_view`
  - daemon 侧首个事件显式 `InlineReplaceCurrentCard == true`
3. `first_result_card` 路径
  - action contract 为 `CurrentCardMode=first_result_card`，或 `inline_view` 严格命中失败后回退
  - 当前事件流里至少存在一张可直接投影成卡片的结果事件；daemon 取第一张作为 `ReplaceCurrentCard`
  - review session final card 的 `放弃审阅` / `按审阅意见继续修改` 当前落在这条路径上：按钮本身继续走 `page_action + daemon_lifecycle_id`，同步 replace 复用既有 first-result 规则，不额外引入 review 专用 callback 通道
  - 普通 final card 上的 `Review 待提交内容` 当前不再命中 first-result replacement，而是保持 append-only：daemon 会保留源 final card，并额外发送一张“正在进入审阅”提示卡
  - bare `/review uncommitted` 与常用工具菜单入口虽然也会启动同一 detached review owner，但它们不复用当前卡同步 replace；命中后只 append 新的进入审阅提示卡
  - 首结果替换后的 followup 抑制当前已从旧的 notice/thread-selection 布尔位迁到显式 `FollowupPolicy`：
    - action contract 提供 `DropClasses / KeepClasses`
    - daemon 统一按 `eventcontract` handoff class 过滤 followup，而不是再按 payload/kind 做散落判断
    - `control` action contract 与 `eventcontract` 当前共享同一套 handoff taxonomy，不再各自维护一套 notice/thread-selection 枚举真相
  - `/stop`、`/new`、`/follow`、`/detach` 落地后会抑制重复终态 notice append
  - `attach_instance` 若后续带 thread-selection announce，daemon 也会抑制这类重复 append，避免菜单卡收口后又补一张同义结果卡
4. active picker 阻断例外
  - 当事件流只有 `path_picker_active` / `target_picker_processing` 阻断 notice 时，daemon 不做首结果替换，保持当前卡可继续在原 owner 子步骤里完成

少任一条，都不会同步等待 replace。

### 5.2 当前被视为 pure navigation 的动作

当前命中 `ResolveFeishuFrontstageActionContract(...).CurrentCardMode=inline_view` 的 pure navigation 动作是：

- `ActionShowCommandMenu`
- bare `/mode`
- bare `/autowhip`
- bare `/autocontinue`
- bare `/reasoning`
- bare `/access`
- bare `/plan`
- bare `/model`
- bare `/verbose`
- `ActionListInstances`
- `ActionSendFile`
- `ActionShowAllWorkspaces`
- `ActionShowRecentWorkspaces`
- `ActionShowAllThreadWorkspaces`
- `ActionShowRecentThreadWorkspaces`
- `ActionShowThreads`
- `ActionShowAllThreads`
- `ActionShowScopedThreads`
- `ActionShowWorkspaceThreads`
- `ActionShowHistory`
- `ActionHistoryPage`
- `ActionHistoryDetail`
- `ActionPathPickerEnter`
- `ActionPathPickerUp`
- `ActionPathPickerSelect`
- `ActionTargetPickerSelectMode`
- `ActionTargetPickerSelectSource`
- `ActionTargetPickerSelectWorkspace`
- `ActionTargetPickerSelectSession`
- `ActionTargetPickerPage`
- `ActionTargetPickerOpenPathPicker`
- `ActionTargetPickerCancel`

当前语义补充：

- 这批动作的 owner 已经从“daemon/gateway 里的散落动作白名单”收束成“`FeishuUIIntent` -> lifecycle policy -> controller replaceable event”三段。
- `/menu` 分组内命令的阶段可见性当前已收束到统一策略：
  - `/follow` 仅在 `vscode_working` 可见
  - `/new` 仅在 `normal_working` 可见
  - `/history` 当前不额外分阶段，normal / vscode 里都默认可见；真正能否拿到历史由当前 route 是否能解析出 thread 决定
  - 其余命令默认可见
  - `current_work` 分组当前的 canonical menu-visible 命令为 `/stop`、`/compact`、`/steerall`、`/status`，以及仅 `normal_working` 可见的 `/new`
  - `send_settings` 分组当前的 canonical menu-visible 命令为 `/reasoning`、`/model`、`/access`、`/plan`、`/verbose`、`/autocontinue`
  - `common_tools` 分组当前的 canonical menu-visible 命令为 `/autowhip`、`/history`、`/cron`、`/sendfile`
  - `maintenance` 分组当前的 canonical menu-visible 命令为 `/mode`、`/upgrade`、`/restart`、`/debug`、`/help`
  - `switch_target` 分组当前还带一层 mode-aware display projection：
    - `normal mode` 只显示一个入口，标题为 `工作会话`，实际命令仍是 canonical `/list`
    - `vscode mode` 继续分别显示 `/list`、`/use`、`/useall`
  - orchestrator 与 projector 共同复用该策略函数，避免两侧分叉
- 当前所有可 replace 的 Feishu UI 导航，都采用同一套 lifecycle 策略：
  - daemon freshness：`daemon_lifecycle`
  - view/session 策略：`surface_state_rederived`
  - 不要求额外 view token
- `ActionRespondRequest` 与 `ActionControlRequest` 当前也纳入这条 transport 级 inline-replace allow-list，但只有 request handler 显式返回 `InlineReplaceCurrentCard=true` 时才会真的 replace：
  - `request_user_input` / form 模式 `mcp_server_elicitation` 的局部保存后自动跳到下一题
  - optional 题的 `skip_optional`
  - 所有 request family 的最终提交/最终决策后切到 sealed waiting 态
  - `request_user_input` 的 `cancel_turn` 与 form 模式 `mcp_server_elicitation` 的 `cancel_request`
  - 纯 notice 的无效/过期点击不会 replace 当前卡
- 这意味着同 daemon 生命周期里的旧卡/并发点击，如果仍属于 pure navigation，不会因为“旧 view”被拒绝；它们会基于**当前** surface state 重新生成卡片。
- 本次新增的 target picker 下拉刷新也沿用这条 replace 边界：
  - normal `/workspace list` 的工作区切换
  - normal `/workspace list` 的会话切换
  - normal `/workspace new dir` / `/workspace new git` 的 `target_picker_open_path_picker` 子步骤切换
  - normal `/workspace new worktree` 的基准工作区切换与 dropdown 翻页
  - alias `/list` / `/use` / `/useall` 命中同一张 `切换工作会话` 卡时的刷新
  - VS Code / legacy selection path 里的上一页 / 下一页 / 返回分组
  都属于 pure navigation，继续原地替换当前卡，而不是 append 新卡。
- workspace target picker 当前额外有一条明确的 UI 语义：
  - `/workspace list` 与 alias `/list` / `/use` / `/useall` / `show_workspace_threads` 首次打开都直接落在 `目标` 页；其中 attached `/use` 会预填当前 workspace，但 detached / unbound 不会替用户猜一个会话
  - `/workspace new dir`、`/workspace new git` 与 `/workspace new worktree` 首次打开则分别直接落在 `目录` / `Git` / `Worktree` 页；bare `/workspace` 与 `/workspace new` 自身只做 page-owner 父页导航
  - editing / processing / terminal 页面当前统一先显示 step tag，再显示单一主问题；不再把旧的“当前工作区 / 当前会话 / 路径摘要”作为编辑页首屏
  - `目标` 页继续把 `工作区 + 会话` 保留在同一页；工作区 label 足够时只显示 label，只有 basename 冲突时才额外补路径 meta 做消歧
  - `目标` 页当前不再把全部 `workspace/session` 选项直接灌进两个 `select_static`；projector 会按 Feishu transport byte budget 做动态分页，并确保底部 footer 仍可见
  - 双下拉预算目标当前固定为 `workspace 1/3 : session 2/3`，并支持空余预算回借；工作区锁定场景则只分页 session lane
  - dropdown 翻页当前统一走 `target_picker_page(picker_id + field_name + cursor)`；`cursor` 是 start-index，不是固定页码
  - workspace lane 翻页属于主上下文切换：服务端会把 cursor 处 workspace 设为当前选择、重算 session options，并把旧 session 选择清空到不可确认态
  - session lane 翻页属于同 workspace 内浏览更多：服务端会保留 workspace cursor / 选中 workspace，但若原 session 不在新可见页内，会主动清空选中并禁用 confirm，避免 invisible confirm
  - `已有工作区` 路径下，不再为了“帮用户猜一个候选”去回退到其他 recoverable thread
  - 或者 surface 当前已经绑定到同 workspace 的某个 thread，且该 thread 仍在候选里，才默认选中该 thread
  - 如果只是当前 workspace 已选中，但 surface 处于 detached / unbound，session 会保持空值，等待用户显式选择
  - 但工作区一旦变化，session 下拉不会再 silently fallback 到新的真实 workspace 默认会话
  - 若切到真实 workspace，session 会被主动清空，confirm 按钮随之禁用，直到用户重新选定会话
  - `从目录新建` 主卡展示目录字段 + `选择目录` 按钮 + `接入并继续` 主按钮；`从 GIT URL 新建` 主卡展示落地父目录字段、仓库地址/目录名表单；其中父目录行右侧内嵌 `选择目录`，底部动作区是带分隔线的横排 `取消 / 上一步 / 克隆并继续`
  - `从目录新建` 命中已知 workspace 时，当前页会直接显示阻塞消息并禁用 `接入并继续`
  - `target_picker_open_path_picker` 当前会把主卡 inline replace 成 path picker 子步骤；子步骤复用 owner-card 标题，并展示 step tag、单题问题、允许范围与当前位置；path picker confirm/cancel 后不会再走同步 inline restore，而是异步 ack 后把最新 target picker 主卡 patch 回同一张 owner card
  - path picker 当前也不再把全部目录/文件候选直接灌进 `select_static`；目录模式单下拉、文件模式目录/文件双下拉，以及 target-picker owner-subpage 的 compact 目录下拉，都会按 Feishu transport byte budget 动态分页，并确保 footer 仍可见
  - path picker dropdown 翻页统一走 `path_picker_page(picker_id + field_name + cursor)`；`cursor` 是 start-index，不是固定页码，目录 lane 固定项 `.` / `..` 不参与分页计数
  - 目录页翻页只更新当前可见目录候选，并保留当前目录；文件页翻页会主动清空文件选择并禁用 confirm，避免 invisible confirm
  - 独立 path picker 卡现在也会在首次发送后把自身 `message_id` 记回 runtime；因此后续非 inline 的异步结果不再只能回退成外发 notice，而是可以显式 patch 当前这张 picker 卡
  - path picker 当前不再把确认成功、取消成功，或大部分前台校验失败直接外发成独立结果卡；默认行为是保留 `允许范围 / 当前目录 / 当前选择` 业务区，并把“当前只可选择目录 / 文件”“条目不可用”“已确认路径”“已取消路径选择”等反馈放进 notice 区；sealed 后移除交互。`path_picker_confirm` 的“当前还不能确认”等前台校验失败也统一走异步 patch 回原卡，不再尝试同步 inline replace
  - `/sendfile` 文件模式 picker 当前基于这条规则形成“菜单卡/独立 picker -> 前台启动 -> 后台发送”的单卡 handoff：打开 picker 时若来自 stamped 菜单卡，会先把当前菜单卡直接替换成文件选择器；若前置条件不满足（例如 VS Code 模式或尚未接管工作区），则当前菜单卡会直接封成不可交互的错误终态，而不是回退成额外 notice
  - `/sendfile` confirm 后会先做启动前校验，失败则把错误提示继续留在当前 picker 卡；启动成功则把当前卡封成 `已开始发送，可继续其他操作`，展示文件名/大小，超 `100 MB` 时再追加 `文件较大，请耐心等待`
  - `/sendfile` cancel 当前也会把当前 picker 卡封成 `已取消发送文件` 终态，而不是把旧卡留在原地再额外 append 一条取消 notice
  - `/sendfile` 启动成功后，真实文件消息会直接出现在聊天流里作为成功结果；不再额外补一张成功确认卡。只有后台异步失败才补轻量 notice
  - `target_picker_cancel` 当前会直接把这张 owner card 封成 terminal 状态；普通编辑态为 `已取消`，Git import processing 态为 `已取消导入`，Worktree processing 态为 `已取消创建`，并会分别 best-effort 停止 clone / prepare 或 `git worktree add`
  - target picker 的 processing / terminal 阶段当前也不再把整张卡覆写成纯状态块；`模式 / 来源 / 工作区 / 会话 / 目录 / 仓库 / 落地目录 / 目标路径` 等业务上下文会继续保留在业务区，状态推进与终态结果统一进入 notice 区
  - `从目录新建` 的主按钮当前仍会前置阻塞已知必败条件；只有目录可接入时，`接入并继续` 才会启用
  - `从 GIT URL 新建` 与 `从 Worktree 新建` 因依赖 Feishu 文本输入，当前不再把 `克隆并继续` / `创建并进入` 的可点击性绑定到 live preview；按钮保持可点，提交后再由服务端做 repo / branch / 目录名 / 基准工作区 / 最终路径 / 环境检查，并把阻塞原因保留在同卡提示区
  - 若当前机器缺少 `git`，`从 GIT URL 新建` 与 `从 Worktree 新建` 仍可直接打开；相关不可用说明会先显示在卡面上，用户点击确认后若仍不可执行，服务端会继续把错误留在同一张卡片里
  - `/workspace list`、`/workspace new dir`、`/workspace new git`、`/workspace new worktree` confirm 成功时，不再 append 一张新的主结果卡；当前 owner card 会直接进入 processing，并在后续 headless / daemon 结果到达时继续 `message.patch` 到同卡终态
  - Git import / worktree processing 期间，卡内只保留结构化阶段、最近状态摘要与 `取消导入` / `取消创建`；阶段块标题当前固定为 `当前阶段`，并用 `✅ / 🔄 / ⚪` 这类 emoji 标记当前推进位置；普通输入会被显式拒绝，提示用户等待完成、取消，或使用 `/status`
  - 若这些路径在准备阶段失败，失败态也会封回同一张 owner card，而不是再额外发一张 notice 卡作为主承载
  - `/history` loading / error 当前也改成同一套前台卡 contract：摘要和当前列表/详情上下文会保留在业务区，读取中与错误信息进入 notice 区，而不是把主区提前 return 掉

### 5.3 当前明确保持 append-only 的动作

下面这些动作即使来自卡片，也不会同步 replace 当前卡：

- `path_picker_confirm` / `path_picker_cancel`；它们虽然也先走 `FeishuUIIntent`，但不命中 `CurrentCardMode=inline_view` 的动作集合，gateway 会立即 ack 并异步处理；当前默认终态会 sealed 回当前 picker 卡，target picker owner-flow 子步骤会 patch 回原 owner card，独立 `/sendfile` picker 的 cancel / 启动前失败 / 启动成功终态也会 patch 回当前 picker 卡。真正仍保持独立 append-only 的只剩 freshness/ownership 拒绝，或 consumer 主动返回新的 follow-up 可见项
- attach 这类真正改变产品状态且不属于当前菜单原卡规则的动作
- 纯文本 slash 的 `/help`、`/status`、`/stop`、`/new`、`/follow`、`/detach`；它们不会把普通文本入口升级成 replace
- request 的最终 dispatch 结果，以及 notice-only 的 request invalid / request expired 处理结果
- 各类 notice、final reply、补充预览、状态类卡片

当前新增补充：

- 参数卡 apply 当前是分流语义：
  - 若动作来自当前参数卡的 stamped callback（也就是 callback payload 带有效 `daemon_lifecycle_id`，且命中当前参数页 owner），`/mode` `/autowhip` `/autocontinue` `/verbose` `/model` `/reasoning` `/access` 的 apply 会走同卡 patch
  - 成功与 no-op 会把原卡封成 sealed terminal card，并附带“如需再次调整，请重新发送对应命令”的 reopen 提示
  - 校验失败、参数格式错误、或仍未接管目标等前置条件失败，会继续留在同一张参数卡上，保留可重试表单；必要时把刚才输入的参数回填到默认值
  - 若动作不是从当前参数卡 callback 进入，例如用户直接发送 `/mode vscode`、`/autowhip on`、`/autocontinue on`，则仍保持 append-only，不会把普通文本 slash 升级成 inline replace
- `/restart` 当前也明确分成 page 与 execute 两类：
  - bare `/restart` 若来自 stamped 当前卡，会走 `first_result_card` replace，把重启根页直接替回当前卡
  - `/restart child` 即使来自 stamped 当前卡也不走同步 replace；它只 append 一条 `正在重启` notice，后续成功/失败结果再继续 append-only
- stamped 菜单命令里的非 inline 命令当前分成几类：
  - `/help`、`/status` 会直接把首个结果卡替成当前菜单卡；不再 append 一张脱离原卡的帮助卡/状态卡
  - `/list`、`/use`、`/useall` 会直接把首个实例列表 / 线程列表 / 提示 / 结果卡替成当前菜单卡；不再回退到 submission anchor。`/list` attach 成功后若同一事件流里还带 thread-selection follow-up，daemon 也会抑制这张重复卡
  - `/stop`、`/new`、`/follow`、`/detach` 会把首个 notice / thread-selection 结果卡直接作为当前菜单卡；不再走 submission anchor，也不再 recall
  - `/compact`、`/steerall`、`/sendfile` 的 `current_work` 菜单入口不再复用锚点路径，而是直接把原菜单卡交给 owner/terminal card 流继续收口
- stamped 参数卡与迁移卡的非 inline 命令当前额外分两类：
  - stamped `/mode vscode` 若切换后立刻命中 legacy `editor_settings` 且存在可接管入口，daemon 会先同步静默自动迁到 `managed_shim`；只有缺 target、自动迁移失败、状态检查仍异常，或后续进入 open prompt / 恢复提示时，才会把首张可投影提示卡替回当前参数卡。之后这张卡会被登记为当前 surface 的 `vscode guidance card`，后续异步命中的兼容修复、open prompt、恢复成功/失败、`not_attached_vscode` `/list` 提示，都会继续以 `message.patch` 回到同一张卡；这条强制同步兼容性检测只用于 stamped callback，纯文本 `/mode vscode` 仍保持旧的异步提示语义
- `/vscode-migrate` 当前也已并入同一套 page contract 根页 / 校验页模型；stamped current-card callback 会先把 root page 或错误页同位替回当前卡，真正执行迁移则改走 `vscode_migrate_owner_flow` callback。迁移结果与后续异步 guidance 继续 patch 在同一张 guidance card 上，不再经由旧文本重解析回调或 bare continuation
- bare `/upgrade`、bare `/restart`、bare `/debug`、bare `/cron`、bare `/vscode-migrate` 当前已经退出 bare continuation 与提交态锚点；它们统一改成 page contract 根页/子页模型，stamped current-card callback 会直接把下一张 page 同位替回当前卡，非法参数也继续留在当前页内报错。
- `/upgrade` 命令族当前还明确收成“page vs execute”两类共享分类：`/upgrade` 与 `/upgrade track` 属于 page subpage，会继续命中 stamped current-card replace；`/upgrade latest`、`/upgrade dev`、`/upgrade local`、`/upgrade codex` 与 `/upgrade track <track>` 都属于立即执行分支，不再依赖零散 token heuristics 区分。
- `/upgrade latest` 当前不走 callback 同步 replace；但只要进入 daemon owner-card 流，同一张升级卡会继续通过 `message.patch` 在 `checking -> confirm -> running/cancelling -> restarting(sealed)` 之间推进，不再依赖“再次发送 `/upgrade latest`”。
- `/upgrade codex` 当前也属于立即执行命令，不走 callback 同步 replace；若入口来自 stamped `/upgrade` 根页当前卡，daemon 会直接把这张根页卡交给 Codex upgrade owner-card flow。Codex 卡片当前先即时打开，不自动跑最新版本查询；只有点击 `检查更新` 才会异步 lookup latest。检查结果无论是“已是最新”“发现新版本但暂时不能升级”“发现新版本且可升级”，都会回到同一张可再次检查的 owner card；真正进入升级后只在 initiator surface 的同卡上继续 patch `running -> success/failed`，其它 surface 默认保持静默，只有用户主动输入时才收到缓存提示。
- turn-owned 的投递策略当前已经改成“高价值文本 reply、过程卡仍顶层 append”：
  - `当前计划`、request prompt、共享过程卡、图片输出、preview supplement 以及大多数 turn-owned notice 当前都固定顶层 append，不再继承 `SourceMessageID`
  - 新的 `remote_turn_accepted` notice 是刻意保留的轻量例外：用户从飞书发起新消息后，normal / verbose 会立即 reply 到原消息给出一张 `已接收` 状态卡；quiet 继续静默
  - 文本触发 `/history` 的首张 patchable history card 仍保持顶层 append；它属于 owner-card / history 混合路径，也不跟随 turn reply anchor
  - final reply（含 overflow continuation）继续 reply 到 turn anchor；later replay 若命中已记录的 reply anchor，也会优先回到原回复链
  - 非 final 的 assistant 普通文本（当前只限 `render.BlockAssistantMarkdown` / `render.BlockAssistantCode`）现在也会沿用当前 turn reply anchor；是否真正可见仍由 surface verbosity 过滤决定，quiet 下不会因为 reply thread 改动而强行变可见
  - detour 临时会话当前不再额外派生“进入临时会话”确认卡；而是直接把临时语义挂回原卡/原消息：
    - request prompt、提案计划卡、`turn_failed` notice、plan update、共享 progress card 与主 final reply card，会把 `临时会话 · 分支` / `临时会话 · 空白` 提升为卡片 header subtitle，并以 `lark_md` 加粗显示
    - 非 final assistant 普通文本不会为了 detour 再硬插前缀，继续保持原正文
    - detour turn 完成、失败或用户中断后，orchestrator 会再补一条 `detour_returned` notice；这条提示跟随原 turn 的 reply/top-level lane，正文固定是“临时会话已结束，已切回原会话。”
  - steer accept 成功后，orchestrator 现在会额外发一条 `UIEventTimelineText(type=steer_user_supplement)`；这条文本 reply 到当前 turn anchor，内容只镜像本次真正并入 turn 的用户补充，不复用 assistant block / notice 语义，也不重发图片或文件实体
  - `用户补充` 的图片计数当前只来自 steer 输入里的 `InputLocalImage` / `InputRemoteImage`；文件计数只来自结构化转发/引用文本中显式编码的 `file` 节点
  - daemon 当前会先在 `[]UIEvent` 批处理入口为原锚点事件打 attention annotation，不再追加独立 `UIEventTimelineText(type=attention_ping)`：
    - 这条提醒不是新的 owner-card / request-card substrate，而是原事件自身的 delivery annotation；若原事件未送达，或 `global runtime` notice 被节流 / suppress，则不会额外补发第二条 `@` 消息
    - request prompt started 命中 `approval` / `request_user_input` / `permissions_request_approval` / `mcp_server_elicitation` 时，当前按 `surface + request_id + revision` 只标注一次；inline rerender 不会重复标注；request dedupe 只在带 attention 的原 request 卡真正送达后记账，因此原 request 卡投递失败后的同 revision 重试仍可补发 attention。`tool_callback` 当前不进入 attention policy，因为它不等待用户处理
    - turn 结束批次里，`turn_failed` 优先于 final；若同批既有 final 又有 `提案计划` 卡，则只把“本轮已结束且有提案待确认”的 attention 挂到 `提案计划` 卡上
    - `attached_instance_transport_degraded`、`gateway_apply_failure`、`daemon_shutting_down` 这三类 `global runtime` notice 当前也会把 attention 直接挂到原 notice card，并继续复用同一套 family + dedupe key + throttle window
    - 若原事件是 reply-chain（例如 final reply），attention 也跟随这张原消息 reply 到同一 anchor；若原事件本来是顶层 append（例如 request prompt、plan proposal、global runtime notice），attention 也保持顶层 append
    - mention 目标固定取当前 surface 的 `ActorUserID`；若当前 surface 没有可用 actor identity，则直接跳过 attention annotation，原事件照常投递
  - 这两类新增 reply-thread 文本当前都属于 live delivery；`ThreadReplayRecord` 仍只负责 final reply / notice replay，中途脱离 surface 时不承诺补发完整 reply-thread 文本轨迹
- `/history` 当前是单独的混合路径：
  - bare `/history`、`history_page`、`history_detail` 都在 inline-replace allow-list 里
  - `openThreadHistory(...)` 现在会先建立 owner-card runtime v1 flow，再建立 history 专用业务态；flow 持有 `flow id / owner / message id / revision / phase / created / expires`
  - `activeThreadHistoryRecord` 现在只保留 `thread / view mode / page / turn` 这些 history 业务字段，不再和 owner lifecycle 形成双真相源
  - card callback 命中时，daemon 会先同步 replace 当前卡为 loading history card
  - 同一动作返回的 `thread.history.read` daemon command 仍会继续异步执行，不会因为同步 replace 而被吞掉
  - 成功/失败结果会优先 patch 回同一张 history owner card；文本触发 `/history` 时会先直接 append 一张 patchable history card，再在结果回来后 `message.patch`
  - inline `/history` loading replace 仍然通过清空 loading view 的 `MessageID` 来强制走 `ReplaceCurrentCard`，同时把来源消息 id 记回 owner-card flow，供异步结果继续 patch 同一张卡
- final reply 当前继续保持 append-only，不会去 replace 现有卡；但一旦 final reply card 发送成功，daemon 会把这张卡的 `message_id` 连同 `instance/thread/turn/item` 与 `daemon_lifecycle_id` 一起记录成 recent final-card anchor：
  - projector 当前会先尝试把完整 final body 投影成单张主卡；若单张卡超限，则会在应用层按正文结构拆成“主 final card + overflow reply cards”，避免把超限处理继续主要交给 gateway `trimCardPayloadToFit(...)`
  - 主 final card 继续沿用原标题（如 `✅ 最后答复` 或带源消息预览的标题），并保留文件摘要 / turn footer / recent final-card anchor
  - overflow cards 当前统一标题为 `✅ 最后答复（续）`，只承载正文 continuation，不再追加文件摘要或 footer
  - split 后的各张卡当前都会继续 reply 到同一个源消息；这条路径仍属于 append-only final delivery，不进入 inline replace
  - 这份 anchor 只用于同 daemon 生命周期内的后续同卡补强路径，不暴露给 callback
  - lookup 当前要求 `surface + instance + thread + turn` 命中；若同时提供 `item` / `daemon_lifecycle_id`，也会继续做精确匹配
  - 同一 turn 再次记录会覆盖旧 anchor；不同 turn 会按最近窗口保留少量 recent anchors
  - surface detach 会清空这些 recent anchors；daemon 重启后也不会恢复旧 anchor
  - 若同步 `RewriteFinalBlock` 因超时或失败而退回原始正文 / fallback 预览，daemon 当前只会在这类失败路径下触发一次后台 second-chance preview：
    - 后台 preview timeout 会放宽到同步时限的两倍，并设有最小值
    - 只有后台结果相对首发 final block 真正产生改进时，才会继续发 `message.patch`
    - 若 final reply 走了 split，后台 second-chance 会继续重跑主卡对应的原始正文片段；patch 目标固定是主 final card，即使改写后的全文重新投影后仍会 split，也只会抽取新的主卡内容回补这张主卡
    - 这意味着 overflow cards 继续保持 append-only，不会被后台 preview patch 追补，也不会在 patch 时重发
    - patch 目标固定是这张 final reply 自己，不会追加第二张 final card，也不会回填 preview supplement
    - 若 anchor 已因 detach、daemon lifecycle 变化或 turn identity 不匹配而失效，则静默放弃，不再尝试补丁
  - 若 final 发生在当时无可投递 surface 的时刻，replay state 现在也会一并保存原始 `SourceMessageID` / 预览；later replay 命中这份 anchor 时，会继续回到原回复链，而不是降级成顶层新卡
- `global runtime` 提示当前也明确保持 append-only，但它和普通 turn-owned notice 的差异不再是 reply anchor，而是独立 delivery lane：
  - 它们通过 `control.Notice.DeliveryClass=global_runtime` 明确标记，不再只靠“刚好没传 `SourceMessageID`”这种隐式约定
  - projector 当前对所有 notice 都不会 reply 到任何 turn 源消息；`global runtime` 的特殊点只剩 dedupe / family 与独立系统车道
  - 当前这条车道覆盖真正脱离当前 owner-card / guidance-card 上下文的：surface resume failure、无 active guidance card 可复用的 VS Code resume failure / `open VS Code` prompt、`attached_instance_transport_degraded`、`daemon_shutting_down`、`gateway_apply_failed`
  - 若 `VS Code` 兼容修复、`open VS Code` prompt、恢复成功/失败或 `not_attached_vscode` guidance 已经拥有当前 surface 的 active `vscode guidance card`，daemon 会先把 notice 改写成 patchable direct-command card 并回写同一张 guidance card；只有没有可复用 guidance card 的后台 runtime 路径才会落到这条独立车道
- 这些真正的 global runtime 系统提示不会借用 final-card anchor 或 turn reply-chain；它们仍作为独立系统提示出现在主时间线
- `/cron`、`/upgrade`、`/restart`、`/debug` 的 stamped 菜单入口与 stamped page callback，当前属于根页 / 子页 / 非法参数回显的路径都会直接把下一张 page 或结果卡同位替回当前卡，不再先外跳 append 一张独立状态/输入卡；其中 `/restart child` 仍然保持立即执行 + append-only。
- “命令已提交”锚点卡当前只剩少量命令继续使用（主要是 `/use`、`/useall`）；这批锚点卡会在短延时后尝试 best-effort 自动撤回，撤回失败时仅静默降级，不影响主流程。
- 这条路径不会改变产品动作 owner；参数卡 apply、VS Code 菜单 handoff 与迁移卡收口都不复用“命令已提交”锚点，而是由产品 handler 直接返回可 replace / patch 的结果卡。
- 共享过程卡（当前承载 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `file_change` / `context_compaction`，并可在底部临时附着 reasoning 状态）不走 callback replace，也不属于旧卡 freshness 判定面：
  - 第一次当前固定顶层 append，不继承当前 turn 的 `SourceMessageID`
  - 若同一 turn 内继续收到新的可见过程项，则优先对当前 active progress card 做 `message.patch`；当前会把 `exec_command`、`web_search`、`mcp_tool_call`、`dynamic_tool_call`、`file_change` 与 `context_compaction` 累积到同一条共享“工作中”时间线里
  - 若 projector 发现当前 active progress card 在 Feishu payload 限制内已无法继续容纳新增的可见行，则不会再依赖 gateway 的尾部截断来“省略后文”；当前实现会：
    - 保持同一张共享过程卡作为 patch 目标，不再追加 continuation card
    - 从窗口头部按“可见进度行”为单位丢弃最旧历史，直到当前 payload 重新落回预算内
    - 只要确实丢弃过历史，就在顶部补一行“较早过程已省略，仅保留最近进度。”
    - 并把 daemon 侧记录的 active progress `card_start_seq` 前移到当前仍可见窗口的首条持久行，供下一次 patch 继续复用
  - 这条滚动窗口当前仍不做行内硬截断；若只有底部 transient reasoning 状态会导致超长，则当前优先丢弃这条 transient，而不是因此挤掉更多持久历史
  - gateway 层的 oversized card trim 仍保留为最后一道兜底，但共享过程卡当前不应以它作为主路径；只有单条可见行本身就无法放入单卡时，才可能触发这层退化
- `reasoning_summary` 当前不会进入普通 timeline；verbose 下若能解析到稳定阶段标题，会只在卡片最底部临时显示一条本地化状态（例如 `思考中`、`规划中`）
- 共享过程卡的 projector 不再把整段 timeline 压成单个 markdown body；当前改成“每个可见行一个 markdown element”，避免单行语法异常把后续行一起污染
  - 这条 reasoning 状态不是历史记录；一旦普通进度继续追加、assistant 正文开始输出、turn 完成/失败/中断，orchestrator 会先把它从旧卡清掉，再决定是否继续 patch 或终结这张卡
  - `web_search` 会按动作类型显示行级摘要（例如“搜索 / 打开网页 / 页内查找”），其中 begin 阶段先用“正在搜索网络”占位，end 阶段再把对应行改写成具体摘要
  - `mcp_tool_call` 会以 `MCP：server.tool` 的行级摘要进入同一张卡；完成态会补耗时，失败态会内联失败原因
  - `dynamic_tool_call` 会按 `tool + 参数` 的形式进入同一张卡；若同一 turn 内连续出现同名 tool，则会复用同一行并按首次出现顺序持续追加参数（例如 `Read：a.cpp` -> `Read：a.cpp b.cpp`）；失败态会在该行内补 `（失败）`
  - `file_change` 现在会以“修改 + 文件路径 + 绿色/红色 `+/-` 行数统计”的形式进入同一张卡；quiet 保持静默，normal 就会显示这一层文件行，verbose 则会在该文件行下面继续内联一个 diff fenced code block。这里仍是过程观察，不承担 final summary / authoritative diff 的最终审阅语义
  - `context_compaction` 不再单独 append 一张 notice 卡；attached surface 命中 normal / verbose 时，会以 `整理：上下文已整理。` 单行并入共享过程卡
  - 对没有用户可展示文本或图片结果的 `dynamic_tool_call`，当前实现保持静默，不再额外发“空结果”notice
  - 可见性当前分两层：`file_change` / `mcp_tool_call` / `context_compaction` 在 normal / verbose 可见，quiet 静默；`exec_command` / `web_search` / `dynamic_tool_call` 以及 exploration / reasoning 仍只在 verbose 可见。normal 继续保留 plan、final reply，以及会影响当前状态的共享过程项；若某轮 turn 还没有进入共享过程卡可见项，normal / verbose 仍会先收到一张 reply-thread `已接收` 状态卡，避免飞书侧完全静默；若 compact 完成发生在无 attached surface 时，replay 到 normal / verbose surface 会继续显示，quiet 仍保持静默
  - 一旦 assistant 正文开始输出，orchestrator 会终结这张进度卡的生命周期，后续不再继续 patch，避免“正文已出现但进度卡还在跳”的并发偏移
- 若清掉底部 reasoning 状态后整张卡已无任何可见行，projector 不会再补 `message.delete` 撤回旧卡；这次清理只终止活跃 progress state，已经发出的旧卡保留为历史

### 5.4 当前保留的独立例外

当前仍有几类语义明确、但不应强行并回普通前台卡/notice 主路径的保留例外：

1. 全局运行时 notice
  - `surface_resume_*`
  - `vscode_open_required`
  - `attached_instance_transport_degraded`
  - `daemon_shutting_down`
  - `gateway_apply_failure`
  - 这些提示继续走独立 runtime notice 车道，不伪装成某张前台业务卡的 notice 区
2. freshness / ownership 拒绝
  - `old_card`
  - `owner_card_expired`
  - `owner_card_unauthorized`
  - `path_picker_expired`
  - `path_picker_unauthorized`
  - `history_expired`
  - 这些提示的目的就是阻止旧卡或非 owner 点击继续改写当前前台状态，因此当前继续保留为显式独立拒绝提示
3. legacy `FeishuSelectionView`
  - normal mode 主 `/workspace list`，以及 alias `/list` / `/use` / `/useall`，已迁到 target picker
  - VS Code instance/thread selection 与 kick-thread confirm 当前仍走 `FeishuSelectionView`，但 adapter live 路径已经直接消费 `FeishuSelectionView + FeishuSelectionSemantics`，不再回退 `FeishuDirectSelectionPrompt`
  - 因此这条 selection substrate 现在是明确保留的 live path，而不是旧 compat prompt 的残留漏网路径
  - 若整轮没有正文，turn 完成时当前实现会直接停止更新并清理内存态，不再额外补一张最终过程卡

## 6. 当前 freshness / old-card 语义

### 6.1 daemon 侧判定

`daemon` 当前对入站动作分三种生命周期判定：

| verdict | 触发条件 | 当前结果 |
| --- | --- | --- |
| `current` | 未命中旧消息窗口，且 `daemon_lifecycle_id` 为空或匹配 | 正常继续处理 |
| `old` | `message_create_time` 或 `menu_click_time` 落在旧窗口外 | 发“旧动作已忽略” notice，不进入产品处理 |
| `old_card` | callback 带 `daemon_lifecycle_id` 且与当前 daemon 不匹配 | 发“旧卡片已过期” notice，不进入产品处理，也不会 replace 当前卡；review final card 的 `Review 待提交内容` / `放弃审阅` / `按审阅意见继续修改` 也完全复用这条拒绝路径 |

### 6.2 当前一个重要边界

**没有 `daemon_lifecycle_id` 的卡片 callback，不会被判成 old card，也不会进入同步 inline replace。**

当前行为是：

- gateway 立即 ack，异步处理
- daemon 不会做 old-card 生命周期拒绝
- 这保证了旧卡/未打标卡仍能兼容旧路径，但也意味着 freshness 证明不足

这是当前实现的兼容性边界，不是未来一定要保留的产品结论。

### 6.3 daemon freshness 与 view/session freshness 的当前边界

当前实现已经显式区分两层概念：

- daemon freshness
  - 通过 `daemon_lifecycle_id` 判定
  - 负责拒绝“来自旧 daemon 生命周期”的旧卡
- view/session freshness
  - workspace/thread selection 与 `/menu` / bare config cards 当前**没有**单独的 per-card view token
  - 这些 replaceable pure navigation 统一采用 `surface_state_rederived` 策略
  - 即：只要 callback 仍在当前 daemon 生命周期内，就直接用**当前** surface state 重建卡片，而不是尝试恢复点击时那一版旧 view
  - request prompt 当前是这个规则的例外
    - request 卡不是 replaceable pure navigation
    - `request_user_input` 与 form 模式 `mcp_server_elicitation` 的草稿、跳过态、sealed waiting 态都属于可变产品状态，所以同 daemon 生命周期内额外要求 `request_revision` 匹配
    - 这条规则只用于防止旧 request 卡继续改写当前 request 草稿，不扩展到 `/menu` / selection / path picker / target picker
  - `/bendtomywill` 编辑卡当前也复用这条例外
    - 它沿用 `request_id + request_revision` 的 freshness 合同，但不接入 core `PendingRequest`
    - 旧 patch 卡、旧 revision，或非 owner 用户的点击都会直接收到失效/无权限提示，不会继续改写当前 patch 草稿
  - path picker 当前在这条规则上额外有一个 coarse-grained `picker_id`
    - 它不是每一步导航都变化的 per-view token
    - 但它要求 callback 必须命中当前 surface 上仍然 active 的 picker 生命周期
    - 同 daemon 生命周期里的旧 picker 卡片如果 `picker_id` 不匹配，会直接收到 `path_picker_expired`，不会继续替换当前 active picker；这类 freshness/ownership 拒绝当前仍保留为显式独立提示，避免旧卡或非本人点击去改写当前前台 picker 卡
  - target picker 当前也有一个 coarse-grained `picker_id`，并且在短路径上再额外绑定 owner-card flow
    - `target_picker_select_*` / `target_picker_open_path_picker` / `target_picker_cancel` 必须命中当前 active picker 与当前 active target-picker owner flow，才会继续 inline replace
    - `target_picker_confirm` 还会额外校验当前工作区 / 会话候选是否仍包含用户刚刚提交的组合
    - 同 daemon 生命周期里的旧 target picker 如果 `picker_id` 不匹配、owner flow 已结束，或候选已变化，会返回 `target_picker_expired` / 无权限提示，或刷新出最新 picker
    - 当前即使只是“原会话已不再有效”，刷新后的最新 picker 也会把 session 重新置空，而不是 silent fallback 到别的默认候选
  - `/history` 当前也有一个 coarse-grained `picker_id`
    - 它现在对应的是 owner-card runtime v1 的 `flow id`
    - `history_page` / `history_detail` 必须命中当前 surface 上仍然 active 的 history owner-card flow
    - 同 daemon 生命周期里的旧 history 卡如果 `picker_id` 不匹配、flow 已过期，或点击者不是当前 flow owner，会收到失效/无权限提示，而不会继续改写当前卡

因此当前的 same-daemon 并发点击 / 旧 view 点击策略是：

- pure navigation：允许，按当前 surface state 重建
- 产品动作：不走 inline replace，仍按 append-only 产品语义处理
- old daemon card：直接拒绝并提示重开卡片

## 7. 当前回归基线

### 7.1 当前关键实现文件

- [internal/core/control/feishu_ui_intent.go](../../internal/core/control/feishu_ui_intent.go)
- [internal/core/control/feishu_ui_lifecycle.go](../../internal/core/control/feishu_ui_lifecycle.go)
- [internal/core/control/feishu_ui_boundary.go](../../internal/core/control/feishu_ui_boundary.go)
- [internal/core/control/feishu_target_picker.go](../../internal/core/control/feishu_target_picker.go)
- [internal/core/control/feishu_selection_view.go](../../internal/core/control/feishu_selection_view.go)
- [internal/core/control/feishu_selection_semantics.go](../../internal/core/control/feishu_selection_semantics.go)
- [internal/core/control/feishu_command_view.go](../../internal/core/control/feishu_command_view.go)
- [internal/core/control/feishu_request_view.go](../../internal/core/control/feishu_request_view.go)
- [internal/core/control/feishu_command_page_catalog.go](../../internal/core/control/feishu_command_page_catalog.go)
- [internal/core/control/feishu_path_picker.go](../../internal/core/control/feishu_path_picker.go)
- [internal/adapter/feishu/gateway_runtime.go](../../internal/adapter/feishu/gateway_runtime.go)
- [internal/core/frontstagecontract/callback_payload.go](../../internal/core/frontstagecontract/callback_payload.go)
- [internal/adapter/feishu/gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [internal/adapter/feishu/projector/request.go](../../internal/adapter/feishu/projector/request.go)
- [internal/core/orchestrator/service_ui_runtime.go](../../internal/core/orchestrator/service_ui_runtime.go)
- [internal/core/orchestrator/service_target_picker_owner_card.go](../../internal/core/orchestrator/service_target_picker_owner_card.go)
- [internal/core/orchestrator/service_feishu_ui_context.go](../../internal/core/orchestrator/service_feishu_ui_context.go)
- [internal/adapter/feishu/projector_exec_command_progress.go](../../internal/adapter/feishu/projector_exec_command_progress.go)
- [internal/adapter/codex/translator_helpers.go](../../internal/adapter/codex/translator_helpers.go)
- [internal/core/orchestrator/service_compact_notice.go](../../internal/core/orchestrator/service_compact_notice.go)
- [internal/core/orchestrator/service_exec_command_progress.go](../../internal/core/orchestrator/service_exec_command_progress.go)
- [internal/core/orchestrator/service_mcp_tool_call_progress.go](../../internal/core/orchestrator/service_mcp_tool_call_progress.go)
- [internal/core/orchestrator/service_replay.go](../../internal/core/orchestrator/service_replay.go)
- [internal/core/orchestrator/service_final_card.go](../../internal/core/orchestrator/service_final_card.go)
- [internal/adapter/feishu/projector/target_picker.go](../../internal/adapter/feishu/projector/target_picker.go)
- [internal/adapter/feishu/projector/selection_view.go](../../internal/adapter/feishu/projector/selection_view.go)
- [internal/adapter/feishu/projector/path_picker.go](../../internal/adapter/feishu/projector/path_picker.go)
- [internal/core/orchestrator/service_feishu_ui_controller.go](../../internal/core/orchestrator/service_feishu_ui_controller.go)
- [internal/core/orchestrator/service_thread_history_view.go](../../internal/core/orchestrator/service_thread_history_view.go)
- [internal/core/orchestrator/service_target_picker.go](../../internal/core/orchestrator/service_target_picker.go)
- [internal/core/orchestrator/service_path_picker.go](../../internal/core/orchestrator/service_path_picker.go)
- [internal/core/orchestrator/service_feishu_command_view.go](../../internal/core/orchestrator/service_feishu_command_view.go)
- [internal/core/orchestrator/service_surface_selection.go](../../internal/core/orchestrator/service_surface_selection.go)
- [internal/core/orchestrator/service_surface_thread_selection.go](../../internal/core/orchestrator/service_surface_thread_selection.go)
- [internal/app/daemon/app_ingress.go](../../internal/app/daemon/app_ingress.go)
- [internal/app/daemon/app_ui.go](../../internal/app/daemon/app_ui.go)
- [internal/app/daemon/app_upgrade.go](../../internal/app/daemon/app_upgrade.go)
- [internal/app/daemon/app_upgrade_owner_card.go](../../internal/app/daemon/app_upgrade_owner_card.go)
- [internal/app/daemon/app_turn_patch.go](../../internal/app/daemon/app_turn_patch.go)
- [internal/app/daemon/app_turn_patch_tx.go](../../internal/app/daemon/app_turn_patch_tx.go)
- [internal/app/daemon/app_turn_patch_view.go](../../internal/app/daemon/app_turn_patch_view.go)
- [internal/app/daemon/app_thread_history.go](../../internal/app/daemon/app_thread_history.go)
- [internal/app/daemon/app_inbound_lifecycle.go](../../internal/app/daemon/app_inbound_lifecycle.go)
- [internal/adapter/feishu/projector_thread_history.go](../../internal/adapter/feishu/projector_thread_history.go)
- [internal/adapter/feishu/projector/thread_history.go](../../internal/adapter/feishu/projector/thread_history.go)

### 7.2 当前关键测试基线

- [internal/core/control/inline_replacement_test.go](../../internal/core/control/inline_replacement_test.go)
  - 锁定 pure navigation 的 lifecycle policy、daemon freshness 与 append-only 的动作集合
- [internal/core/control/feishu_ui_intent_test.go](../../internal/core/control/feishu_ui_intent_test.go)
  - 锁定哪些动作会被分流到 Feishu UI controller，哪些 mixed/product-owned 动作仍留在主 reducer
- [internal/adapter/feishu/projector_test.go](../../internal/adapter/feishu/projector_test.go)
  - 锁定 `FeishuDirectSelectionPrompt` / `FeishuSelectionView` / `FeishuCatalogView -> FeishuPageView` / `FeishuRequestView` 的 lifecycle stamp、projection 结果、request prompt 顶层投递语义与 callback payload 结构
- [internal/adapter/feishu/projector_selection_structured_test.go](../../internal/adapter/feishu/projector_selection_structured_test.go)
  - 锁定 VS Code 结构化 thread dropdown 的 `thread_selection_page` 分页投影、prev/next payload、以及当前 thread 掉出可见页时不再写 `initial_option`
- [internal/adapter/feishu/projector_notice_test.go](../../internal/adapter/feishu/projector_notice_test.go)
  - 锁定结构化 notice 继续走纯文本 section 渲染，以及 `global runtime` notice 的独立 append-only delivery lane
- [internal/adapter/feishu/projector_plan_update_test.go](../../internal/adapter/feishu/projector_plan_update_test.go)
  - 锁定 `当前计划` 卡保持顶层 append-only，不继承 turn reply anchor
- [internal/adapter/feishu/projector_image_output_test.go](../../internal/adapter/feishu/projector_image_output_test.go)
  - 锁定图片输出保持顶层发送，不 reply 到 turn 源消息
- [internal/adapter/feishu/projector_preview_supplement_test.go](../../internal/adapter/feishu/projector_preview_supplement_test.go)
  - 锁定 final preview supplement 保持顶层 append-only，不借用 final reply 的 reply anchor
- [internal/adapter/feishu/projector_target_picker_test.go](../../internal/adapter/feishu/projector_target_picker_test.go)
  - 锁定 `FeishuTargetPickerView` 的页头 `StageLabel` / `Question`、target page 双下拉的 byte-budget pagination / footer 保留、Git 表单 payload、`daemon_lifecycle_id` stamp、confirm 按钮结构，以及带 `MessageID` 时改走 `OperationUpdateCard`、terminal stage 移除交互控件
- [internal/adapter/feishu/projector_path_picker_test.go](../../internal/adapter/feishu/projector_path_picker_test.go)
  - 锁定 `FeishuPathPickerView` 的按钮 payload、`path_picker_page` callback、目录/文件 lane 的 byte-budget pagination / footer 保留、`daemon_lifecycle_id` stamp、enter/select 区分、带 `MessageID` 时改走 `OperationUpdateCard`，以及 target-picker-owned 子步骤会切到 compact owner-subpage 布局，terminal path picker 会移除选择控件并只保留状态摘要
- [internal/core/orchestrator/service_final_card_test.go](../../internal/core/orchestrator/service_final_card_test.go)
  - 锁定 final reply recent anchor 的 turn-scope 回查、同 turn 覆盖、lifecycle 匹配与 detach 清理
- [internal/adapter/feishu/projector_snapshot_final_test.go](../../internal/adapter/feishu/projector_snapshot_final_test.go)
  - 锁定 final reply 在普通场景仍保持单主卡；超长 Markdown / code final 会在 projector 层 split 成主卡 + `✅ 最后答复（续）`，且每张卡单独都能落在 Feishu payload 限制内；同时锁定非 final assistant 文本与 `timeline text` 会正确挂 reply anchor，而挂在 final reply 上的 attention annotation 只会落在主卡，不会在 overflow cards 重复 `@`
- [internal/app/daemon/app_final_card_test.go](../../internal/app/daemon/app_final_card_test.go)
  - 锁定同步 preview 超时后的 second-chance final patch：同卡 `message.patch`、无改进静默跳过、detach 后 anchor 失效即放弃，以及 split final reply 只回补主卡、不重发 overflow cards
- [internal/adapter/feishu/gateway_target_picker_test.go](../../internal/adapter/feishu/gateway_target_picker_test.go)
  - 锁定 `target_picker_*` 与 `target_picker_page` callback payload 能正确回到 `control.Action`
- [internal/adapter/feishu/gateway_test.go](../../internal/adapter/feishu/gateway_test.go)
  - 锁定 callback payload 解析、同步等待 replace 的触发条件（inline navigation + stamped command result replacement + dormant command submission anchor compatibility branch）、无 lifecycle 导航仍异步 ack、card/text attention annotation 的 reply/fallback 出站路径，以及共享更新卡的 `message.patch` 出站路径
- [internal/app/daemon/app_review_mode_test.go](../../internal/app/daemon/app_review_mode_test.go)
  - 锁定普通 final card 上的 `Review 待提交内容` 与 `评审 <short-sha>` footer 保持 append-only、不覆盖源 final card，并分别按 Git dirty 状态 / 最近 commit 命中决定显隐；同时锁定 `/review uncommitted`、final-card commit review 与 review session final card 的 `放弃审阅` / `按审阅意见继续修改` 路径
- [internal/app/daemon/app_turn_patch_test.go](../../internal/app/daemon/app_turn_patch_test.go)
  - 锁定 `/bendtomywill` 打开多题 request 卡、owner/revision 校验、busy/VS Code 拒绝、apply 成功后的 rollout 改写与 reasoning 清理，以及 child restart 失败后的自动回滚收口
- [internal/app/daemon/app_upgrade_owner_card_test.go](../../internal/app/daemon/app_upgrade_owner_card_test.go)
  - 锁定 `/upgrade latest` checking / confirm / cancel 的 owner-card flow 会通过 page `TrackingKey -> message_id` 回写继续 patch 同一张升级卡
- [internal/app/daemon/app_codex_upgrade_owner_card_test.go](../../internal/app/daemon/app_codex_upgrade_owner_card_test.go)
  - 锁定 `/upgrade codex` owner-card 的即时打开、重复检查、confirm-time 重校验回退、旧卡失效，以及 running / terminal 只留在 initiator surface 的语义
- [internal/adapter/feishu/projector_exec_command_progress_test.go](../../internal/adapter/feishu/projector_exec_command_progress_test.go)
  - 锁定共享过程卡对 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `file_change` / `context_compaction` 行级摘要的投影边界、首卡顶层 append / 后续 patch 语义、`file_change` 在 normal/verbose 下的分层投影、超长时单卡滚动窗口与顶部省略提示、底部瞬时 reasoning 状态的渲染位置、逐行 markdown element 投影，以及空 transient 清理不会撤回旧卡的语义
- [internal/adapter/codex/translator_requests_test.go](../../internal/adapter/codex/translator_requests_test.go)
  - 锁定 `web_search` item started/completed 的 kind 归一化与 `query` / `actionType` / `queries` / `url` / `pattern` 提取，以及 `dynamic_tool_call` 的 `tool` / `arguments` / 结构化摘要提取
- [internal/adapter/feishu/gateway_delete_message_test.go](../../internal/adapter/feishu/gateway_delete_message_test.go)
  - 锁定 message.delete 出站能力与“消息已不存在”类错误的静默降级
- [internal/adapter/feishu/gateway_path_picker_test.go](../../internal/adapter/feishu/gateway_path_picker_test.go)
  - 锁定 `path_picker_*` 与 `path_picker_page` callback payload 能正确回到 `control.Action`
- [internal/core/orchestrator/service_test.go](../../internal/core/orchestrator/service_test.go)
  - 锁定 `UIEventFeishuTargetPicker` 会携带显式 `FeishuTargetPickerContext`，以及 normal `/list` 的基础 target picker 语义
- [internal/core/orchestrator/service_thread_selection_test.go](../../internal/core/orchestrator/service_thread_selection_test.go)
  - 锁定 VS Code direct selection 会用 `thread_selection_page` 按当前 surface 状态重建 `FeishuThreadSelectionView`，而不是引入新的 owner runtime
- [internal/core/orchestrator/service_exec_command_progress_test.go](../../internal/core/orchestrator/service_exec_command_progress_test.go)
  - 锁定共享过程卡对 `exec_command` / `web_search` / `dynamic_tool_call` / `mcp_tool_call` / `file_change` / `context_compaction` 的可见性分档、首卡顶层 append、同卡复用、`file_change` / `mcp_tool_call` / `context_compaction` 在 normal 下也会进入共享过程卡、滚动窗口时 `card_start_seq` 前移、正文出现后终止、同类 tool 行级聚合、失败态行内标记，以及底部瞬时 reasoning 状态的本地化/清理时机
- [internal/app/daemon/app_ui_progress_test.go](../../internal/app/daemon/app_ui_progress_test.go)
  - 锁定共享过程卡在 `message.patch` 回来时也会把 active progress 的 `card_start_seq` 回写到当前同卡窗口，而不是只在首卡发送时记录
- [internal/core/orchestrator/service_mcp_tool_call_progress_test.go](../../internal/core/orchestrator/service_mcp_tool_call_progress_test.go)
  - 锁定 `mcp_tool_call` 已并入共享过程卡：started/failed 的同卡复用、去重与行级摘要更新语义
- [internal/core/orchestrator/service_compact_notice_test.go](../../internal/core/orchestrator/service_compact_notice_test.go)
  - 锁定 `context_compaction` 已并入共享过程卡：attached normal / verbose 都会进入 `整理` 行，quiet 保持静默；无 surface 时的 replay 也只在 normal / verbose attach 下可见，并继续保持顶层 append-only
- [internal/core/orchestrator/service_image_output_test.go](../../internal/core/orchestrator/service_image_output_test.go)
  - 锁定 `dynamic_tool_call` 只产出文字摘要 / 图片链接摘要，不再因图片 rich result 自动生成 `UIEventImageOutput`；空输出场景保持静默、不再补缺省 notice
- [internal/core/orchestrator/service_target_picker_test.go](../../internal/core/orchestrator/service_target_picker_test.go)
  - 锁定 target picker 的 inline refresh、页头单题文案、owner-subpage path picker 回流、`/workspace list` 切换、`/workspace new dir` / `git` / `worktree` 路径的前置阻塞校验、worktree 只列 Git workspace、recoverable-only workspace headless 路径、Git import / worktree 长链路的 processing / cancel / blocked-input / terminal 收口，以及 stale selection 不会 silent fallback
- [internal/core/orchestrator/service_path_picker_test.go](../../internal/core/orchestrator/service_path_picker_test.go)
  - 锁定路径规范化、root 边界、symlink escape、owner / expire / active picker gate、consumer handoff，以及目录/文件分页 cursor 的刷新、清空与重置语义
- [internal/app/daemon/app_target_picker_cancel_test.go](../../internal/app/daemon/app_target_picker_cancel_test.go)
  - 锁定 `target_picker_cancel` 在 callback replace 路径上会把当前卡封成 terminal `已取消`，而不是额外 append 一张 notice 卡
- [internal/app/daemon/app_send_file_test.go](../../internal/app/daemon/app_send_file_test.go)
  - 锁定 `/sendfile` 当前会在独立 file picker 卡上完成启动前校验与 terminal handoff：cancel、启动前失败继续 patch 当前卡、启动成功封成 `已开始发送，可继续其他操作`、后台成功不额外发成功卡、后台失败只补轻量 notice；menu handoff 路径也会复用同一张 picker/message id
- [internal/core/orchestrator/service_thread_history_view_test.go](../../internal/core/orchestrator/service_thread_history_view_test.go)
  - 锁定 `/history` 已迁到 owner-card runtime v1：flow 建立、loading/resolved phase 推进、列表/详情回填与 message patch 目标不漂移
- [internal/app/daemon/app_thread_history_test.go](../../internal/app/daemon/app_thread_history_test.go)
  - 锁定 history daemon command 的分发、pending 跟踪、reject/loaded/failure 的收口行为
- [internal/app/daemon/app_history_card_test.go](../../internal/app/daemon/app_history_card_test.go)
  - 锁定 inline `/history` 会先 replace 当前卡为 loading，同时继续异步派发查询，不把后续 result patch 链路挤坏
- [internal/core/orchestrator/service_local_request_test.go](../../internal/core/orchestrator/service_local_request_test.go)
  - 锁定 `UIEvent` 现在会携带显式 `Feishu*Context` query/policy 元数据；selection/command view 的 UI owner 已切到 read model，但用户可见行为保持不变
- [internal/core/orchestrator/service_local_request_menu_test.go](../../internal/core/orchestrator/service_local_request_menu_test.go)
  - 锁定 `/help` 与 `/menu` 当前共用 display projection：normal mode 会把 `/list` / `/use` / `/useall` 收口成 `工作会话`，vscode mode 继续保留三者分开展示
- [internal/core/control/feishu_command_page_catalog_test.go](../../internal/core/control/feishu_command_page_catalog_test.go)
  - 锁定 `/menu` 首页不会再从 `/menu` 命令定义隐式继承 maintenance breadcrumb / back button，首页分组按钮文案直接复用分组标题
- [internal/adapter/feishu/projector_command_catalog_test.go](../../internal/adapter/feishu/projector_command_catalog_test.go)
  - 锁定 `/menu` 首页投影结果只显示根 breadcrumb `菜单首页`，并把每个分组渲染成同名按钮
- [internal/core/orchestrator/service_command_card_test.go](../../internal/core/orchestrator/service_command_card_test.go)
  - 锁定参数卡 apply 的同卡收口边界：成功 / no-op 封成 sealed terminal card、格式错误保留同卡重试、未接管目标时回到同卡恢复态
- [internal/app/daemon/app_test.go](../../internal/app/daemon/app_test.go)
  - 锁定 daemon ingress 统一入口下的 inline replace 结果、纯文本 `/help` 继续 append-only、参数卡 callback apply 走同卡 replace 而纯文本参数 apply 继续 append-only、active path picker 会阻断 competing `/menu`、same-daemon pure navigation 采用 current-surface rerender，以及 old-card 导航/命令被拒绝而不是继续 replace
- [internal/app/daemon/app_global_runtime_notice_test.go](../../internal/app/daemon/app_global_runtime_notice_test.go)
  - 锁定 `global runtime` 提示维持独立 delivery lane，并按 family + dedupe key 做短窗节流 / pending queue 去重
- [internal/app/daemon/app_attention_ping_test.go](../../internal/app/daemon/app_attention_ping_test.go)
  - 锁定 request prompt / final reply / `turn_failed` / `提案计划` / targeted `global runtime` notice 的 attention annotation 归属规则、reply/append 跟随原事件位置的语义、request anchor 失败后不会错误消耗 dedupe 且重试仍可补发，以及 same-batch suppressed runtime notice 不会额外泄漏第二条消息
- [internal/app/daemon/app_menu_handoff_test.go](../../internal/app/daemon/app_menu_handoff_test.go)
  - 锁定 `/list` 在 normal / vscode 两种模式下都改走菜单同卡 handoff，vscode `/list` / `/use` / `/useall` 的空态、attach 结果与 `use_thread` 结果都会继续收口在原菜单卡；同时 `/help`、`/steerall`、`/compact`、`/sendfile` 会直接把菜单卡交给后续结果/owner/picker 卡继续收口，`/stop`、`/new`、`/follow`、`/detach` 也会直接 seal 当前菜单卡
- [internal/app/daemon/app_submission_anchor_test.go](../../internal/app/daemon/app_submission_anchor_test.go)
  - 锁定 `/status` 已退出菜单提交态锚点并直接改成同卡状态结果，同时纯文本 `/status` 继续 append-only；`/cron` / `/upgrade` 的 stamped current-card 路径当前已改成 page-result replacement，不再命中 bare continuation 或提交态锚点
- [internal/app/daemon/app_vscode_migration_test.go](../../internal/app/daemon/app_vscode_migration_test.go)
  - 锁定 stamped `/mode vscode` 命中的 legacy `editor_settings` 会默认静默自动迁到 `managed_shim`，成功时不再显式展示迁移提示卡；只有自动迁移失败 / 缺 target / 需要修复时才回落可见 guidance。stamped `/vscode-migrate` root page 仍会继续沿 page-result replacement 打开当前卡；真正的 `vscode_migrate_owner_flow` callback 会把迁移结果同位收口到当前迁移卡，后续命中的 `not_attached_vscode` `/list` guidance 也会继续 patch 回原卡
- [internal/app/daemon/app_vscode_migration_async_test.go](../../internal/app/daemon/app_vscode_migration_async_test.go)
  - 锁定后台异步 detect 触发的 VS Code 失败/修复 guidance card，后续 `open VS Code` guidance 会复用同一张 tracked guidance card，而不是额外 append 第二张卡
- [internal/app/daemon/surface_resume_state_test.go](../../internal/app/daemon/surface_resume_state_test.go)
  - 锁定 detached vscode surface 的 open prompt 在 exact reconnect 后，会继续 patch 回原 guidance card，而不是追加独立“恢复成功”卡
- [internal/core/control/inline_replacement_test.go](../../internal/core/control/inline_replacement_test.go)
  - 锁定 `ResolveFeishuFrontstageActionContract(...)` / `SupportsFeishuSynchronousCurrentCardReplacement(...)` 的当前 frontstage contract：`inline_view`、`first_result_card`、`LauncherDisposition` 分类、lifecycle freshness、以及 legacy bare continuation / submission anchor 已退出 live 路径
- [internal/app/daemon/app_inbound_lifecycle_test.go](../../internal/app/daemon/app_inbound_lifecycle_test.go)
  - 锁定 old / old-card 生命周期分类，以及 reject detail 已按当前 UI intent / command 语义收束
- [internal/core/orchestrator/service_config_prompt_test.go](../../internal/core/orchestrator/service_config_prompt_test.go)
- [internal/core/orchestrator/service_reply_auto_steer_test.go](../../internal/core/orchestrator/service_reply_auto_steer_test.go)
- [internal/core/orchestrator/service_steer_all_test.go](../../internal/core/orchestrator/service_steer_all_test.go)
  - 锁定 steer accepted 后的 `用户补充` timeline text：reply 到 turn anchor、只镜像当前补充本体、不泄漏引用/structured bundle tag、图片/文件只计数不重发实体
- [internal/core/orchestrator/service_thread_selection_test.go](../../internal/core/orchestrator/service_thread_selection_test.go)
  - 锁定 request gate 对 `/follow`、`/use`、selection rebind 的冻结
- [internal/core/orchestrator/service_headless_thread_test.go](../../internal/core/orchestrator/service_headless_thread_test.go)
  - 锁定 normal mode target picker 的 workspace 过滤、recoverable-only workspace 暴露与 VS Code path 的隔离

## 8. 审计清单

每次改 Feishu 卡片 UI 相关行为，提交前至少检查：

1. projector 发出的 `kind` / 额外字段，gateway 是否还能完整解析
2. 某个同上下文导航动作是否意外从 replace 退回 append，或反之
3. path picker 的 `picker_id` / `entry_name` 是否与 gateway 解析和 active picker freshness 仍然一致
4. old card 是否还能继续命中产品状态变更
5. active picker confirm / cancel 是否意外变成 replace，或在 owner-card 子步骤里错误地退回 append 外卡，掩盖了真正的 consumer 结果
6. 没有 `daemon_lifecycle_id` 的 callback 是否被错误地当成可同步 replace
7. target picker confirm 是否会对 stale 选择 silent fallback 到别的默认候选
8. request prompt / selection prompt / path picker / target picker 是否把产品状态机职责偷渡进 Feishu UI 层
9. `/history` 的 owner-card runtime 与 history 业务态是否仍保持单一真相源，而不是重新长回两套 owner lifecycle

## 待讨论取舍

- 是否要把“缺少 `daemon_lifecycle_id` 的纯导航 callback”从当前的兼容异步路径，收紧成显式 reject 或显式降级提示；这会影响旧卡兼容性与 freshness 保证之间的取舍。
- final reply split 当前采用“主卡保留原标题与 footer，overflow 统一标题为 `✅ 最后答复（续）`”的最小语义；是否要进一步升级成显式 `1/N` 编号、或把 footer 改挂到最后一张 continuation card，仍是产品取舍。

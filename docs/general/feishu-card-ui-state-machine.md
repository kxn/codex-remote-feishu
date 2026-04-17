# Feishu 卡片 UI 状态机

> Type: `general`
> Updated: `2026-04-17`
> Summary: 在阶段 1 的显式 Feishu UI query/context 边界和阶段 2 的 Feishu UI controller 分流之上，阶段 3 把 selection cards 拆成 view + adapter projection，阶段 4 又把 `/menu` 与 bare config cards 的最终投影 owner 下沉到 Feishu adapter；当前又补上了可复用 `FeishuPathPickerView`、normal `/list` / `/use` / `/useall` 共享的 `FeishuTargetPickerView`（顶部 `已有工作区` / `添加工作区` 模式按钮；已有工作区路径显示工作区下拉 + 会话下拉，并额外提供 `target_picker_cancel` 退出当前 modal；添加工作区路径显示来源按钮，本地目录会 inline replace 到 path picker 子步骤，Git URL 则在主卡内渲染仓库地址/目录名表单并通过 `target_picker_open_path_picker` 选择落地父目录；取消会清掉 active picker，并把当前卡 inline replace 成一张“已取消选择工作区/会话”的系统提示，不改 route）、`/history` 的 `FeishuThreadHistoryView`（同卡 loading -> async query -> `message.patch` 回填列表/详情；文本触发首卡现为直接 append 到会话，不再 reply）、`path_picker_*` / `target_picker_*` / `history_*` callback 协议、active picker / active history 的 same-daemon freshness 边界、gateway 对 `select_static` 取值的 `option` / `options` / `form_value[field_name]` 兼容解析，以及 target picker Git 内联表单草稿在 mode/source/path 子步骤之间的保留语义、多题 `request_user_input` 的分题暂存与“仅为需要手填的问题渲染表单输入”的卡片语义、题级回答进度与已答/待答状态展示、“未答题先进入确认态，再显式确认留空提交”的 request 交互路径、`permissions_request_approval` / `approval_command` / `approval_file_change` / `approval_network`、顶层 `tool/requestUserInput` 与 `mcp_server_elicitation` 已一起进入 request 卡体系（按钮/表单、`availableDecisions` 归一化、权限按钮、url continue 卡、schema 派生表单、same-daemon `request_revision` freshness、`cancel` 决策回写）、“菜单命令提交态锚点卡”路径（同步 replace 提交态 + 结果继续 append，并支持 best-effort 自动撤回，当前包含 `/steerall`）、`/menu` 首页只保留分组导航（不再额外渲染“常用操作”区块）以及 `current_work` / `switch_target` 的阶段可见性矩阵（`/new` 仅 normal，`/follow` 仅 vscode，`/history` 默认双模式可见），以及无回调的共享过程卡（当前承载 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `context_compaction`，首次直接 append 到会话，后续 `message.patch` 同卡更新，正文出现后终结；共享过程卡当前统一只在 verbose 可见；同类 `dynamic_tool_call` 会按 tool 名单行聚合并持续追加参数；compact 完成态也改为 `整理：上下文已整理。` 单行并入同卡）；final reply 当前已从“单卡 + 网关超限截断”升级成 projector 层的“主 final card + overflow reply cards”交付，主卡保留 recent final-card anchor、footer 与 second-chance patch，overflow cards 继续 append-only；`/sendfile` 文件模式路径选择器的目录下拉当前会在可返回时把 `..` 固定置顶，并把 `.` 开头目录排在普通目录之后。

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
- `daemon`
  - 负责 old-card / old-message 生命周期判定
  - 负责在 ingress 层统一把动作交给主 `ApplySurfaceAction()` 入口；`FeishuUIIntent` 分流发生在 service 内，避免绕开 request/path-picker 等产品门禁
  - 负责只在安全条件下把同上下文导航转成 `ReplaceCurrentCard`
  - 当前有两条 replace 路径：
    - inline navigation：当 action 命中 **inline-replace allow-list**（并非所有 `FeishuUIIntent`）、且 controller 产出的 `UIEvent` 显式标记 `InlineReplaceCurrentCard`
    - command submission anchor：当 action 命中提交态锚点 allow-list（如 `/status`、`/list`、`/steerall`）时，daemon 回一张轻量“命令已提交”替换卡，同时保留原结果 append
- `orchestrator / Feishu UI controller`
  - 负责 `show_*`、`/menu`、bare config-card 这类 pure navigation 的 controller 分流与事件构建
  - 负责通过阶段 1 暴露的 `Feishu*Context` query/policy 边界生成 UI-owned read model 与 request 事件
  - 对 normal mode `/list` / `/use` / `/useall`，当前先产出 `FeishuTargetPickerView` read model，再连同 `FeishuTargetPickerContext` 穿过 `UIEvent` 边界
  - 对 VS Code instance/thread selection 与其余 legacy selection path，当前仍先产出 `FeishuSelectionView` read model，再连同 `FeishuSelectionContext` 穿过 `UIEvent` 边界
  - 对 `/menu` 与 bare `/mode` `/autowhip` `/reasoning` `/access` `/model`，当前先产出 `FeishuCommandView` read model，再连同 `FeishuCommandContext` 穿过 `UIEvent` 边界
  - 对飞书文件/目录选择器，当前先产出 `FeishuPathPickerView` read model，再连同 `FeishuPathPickerContext` 穿过 `UIEvent` 边界；进入目录、返回上一级、文件选择属于 controller 内 pure navigation，confirm/cancel 则转到 picker consumer handoff
- `projector`
  - 负责把 `control.UIEvent` 渲染成 Feishu 卡片
  - 负责把当前需要的 callback payload 字段写进卡片按钮/表单/下拉
  - 对共享过程卡（当前承载 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `context_compaction`），负责在首次发送时打开 `config.update_multi=true`，让后续同一张卡可被 `message.patch` 更新
  - 当前是 selection / target-picker / command/config cards 最终 projection 的 owner：
    [internal/adapter/feishu/projector_target_picker.go](../../internal/adapter/feishu/projector_target_picker.go)
    负责把 `FeishuTargetPickerView` 投影成 unified target picker 卡片
    [internal/adapter/feishu/projector_selection_view.go](../../internal/adapter/feishu/projector_selection_view.go)
    负责把 `FeishuSelectionView` 投影成当前仍被卡片 renderer 消费的 `FeishuDirectSelectionPrompt`
    [internal/adapter/feishu/projector_command_view.go](../../internal/adapter/feishu/projector_command_view.go)
    负责把 `FeishuCommandView` 投影成当前仍被卡片 renderer 消费的 `FeishuDirectCommandCatalog`
    [internal/adapter/feishu/projector_path_picker.go](../../internal/adapter/feishu/projector_path_picker.go)
    负责把 `FeishuPathPickerView` 投影成当前复用路径选择器卡片
- `orchestrator`
  - 负责 attach / use / follow / request gate / capture / new-thread 等产品状态
  - 负责 mixed/product-owned 动作仍然进入主 reducer 的那部分产品语义
  - 当前还会在 `UIEvent` 上额外挂出显式 `Feishu*Context`，作为 Feishu UI controller 的稳定 query/policy 输入

## 3. 当前 owner 分类表

| 交互面 | 当前 owner | 当前边界 |
| --- | --- | --- |
| `/menu` 首页 / 分组 / 返回 | `feishu-ui-owned` | 当前由 Feishu UI controller 处理同一张命令菜单内的层级切换；首页仅保留分组导航入口，不再额外渲染“常用操作”区块；不再直接进入主 reducer，也不改 core route |
| `show_all_workspaces` / `show_recent_workspaces` | `feishu-ui-owned` | normal mode 下当前只负责重新打开 `/list` target picker；不直接改变 attach 状态 |
| `create_workspace` | `mixed` | 旧 normal `/list` 卡片残留的 transport 兼容入口；点击后仍会直接打开本地目录 path picker，但当前 unified target picker 主路径已经改成模式切换 + 来源选择 |
| `show_threads` / `show_all_threads` / `show_scoped_threads` | `feishu-ui-owned` | normal mode 下当前只负责重新打开 `/use` / `/useall` target picker；vscode mode 下仍沿用 thread selection / scoped-all 导航 |
| `show_workspace_threads` / `show_all_thread_workspaces` / `show_recent_thread_workspaces` | `feishu-ui-owned` | normal mode 下当前只负责用指定 workspace/source 重新打开 target picker；vscode / legacy selection path 下才继续承担旧分页导航 |
| `target_picker_select_mode` / `target_picker_select_source` / `target_picker_select_workspace` / `target_picker_select_session` | `feishu-ui-owned` | unified target picker 的模式按钮、来源按钮与工作区/会话下拉回调；命中当前 active picker 时直接原地替换当前卡，不直接改 route；切模式时卡片会在“已有工作区”和“添加工作区”两套布局间切换；切真实 workspace 时会显式清空当前会话选择 |
| `target_picker_open_path_picker` | `feishu-ui-owned` | unified target picker 的子步骤导航回调；当前用于从主卡打开“本地目录”或“Git 落地父目录”的 path picker，并在打开前保留 Git 内联表单草稿；命中当前 active picker 时直接原地替换当前卡 |
| `target_picker_cancel` | `feishu-ui-owned` | unified target picker 的显式退出动作；命中当前 active picker 时会清掉 active target picker，并把当前卡 inline replace 成一张“已取消选择工作区/会话”的 notice；不改 attach/use/new-thread route |
| `target_picker_confirm` | `mixed` | callback 协议、picker ownership 与 freshness 校验仍属 Feishu UI；真正 attach / switch / `新建会话`、按已选本地目录执行接入、或按主卡内联 Git 表单 + 已选父目录执行导入的产品语义仍由 orchestrator 决定，并保持 append-only |
| `path_picker_enter` / `path_picker_up` / `path_picker_select` | `feishu-ui-owned` | 当前由 Feishu UI controller 处理同一张路径选择器卡片内的浏览、返回与文件选择；命中当前 active picker 时直接原地替换当前卡。复用路径选择器 projector 当前统一渲染成紧凑 `select_static`：目录模式提供“进入目录”下拉，文件模式提供“进入目录 + 选择文件”双下拉；若当前不在根目录，目录下拉会把 `..` 固定放在第一项作为返回上一级入口，并承担原先单独“上一级”按钮的职责；真实目录项里普通目录排在前，`.` 开头目录排在后 |
| `path_picker_confirm` / `path_picker_cancel` | `mixed` | callback 协议与 owner/freshness 校验仍属 Feishu UI；这两类动作当前不在 inline-replace allow-list，回调会立即 ack 并异步处理；真正确认后做什么、取消后回什么卡由 picker consumer 决定 |
| bare `/history` / `history_page` / `history_detail` | `mixed` | 当前由 Feishu UI controller 先把同一张卡同步切到 loading，再异步发起 `thread.history.read`；列表/详情结果与失败态默认继续 patch 回这张 history 卡 |
| bare `/mode` / `/autowhip` / `/reasoning` / `/access` / `/model` | `mixed` | bare open-card 当前由 Feishu UI controller 处理；真正应用参数后仍进入产品状态变更，因此 apply 继续保持 append-only |
| `request approve` / `approval_command` / `approval_file_change` / `approval_network` / `request_user_input` / `permissions_request_approval` / `mcp_server_elicitation` / `captureFeedback` | `mixed` | 卡片按钮、表单字段、lifecycle stamp 属于 Feishu UI；request gate、反馈 capture、通用 approval 的 `requestKind`/`availableDecisions` 归一化、`request_user_input` 的分题暂存、`mcp_server_elicitation` form 的局部草稿、“提交答案/提交并继续”触发的最终校验，以及 permissions / elicitation 的结构化回写属于产品状态机 |
| `attach_instance` / `attach_workspace` / `use_thread` | `product-owned` | 卡片只负责把选择结果送入产品层；是否允许接管、是否跨 workspace、接管后进入什么 route 都由 orchestrator 决定 |
| `/follow` | `product-owned` | 是否可用、是否被冻结、跟随到哪个 thread、normal/vscode mode 差异都属于 core 状态机 |
| `/new` | `product-owned` | 是否进入 `new_thread_ready`、何时消耗第一条消息、request gate 是否阻断都属于 core 状态机 |

补充规则：

- `control.FeishuDirectRequestPrompt` 当前仍是**产品层拥有语义、Feishu 层拥有序列化**的 shared DTO。
- `control.FeishuDirectCommandCatalog` 仍保留为当前 card renderer 的过渡 DTO，但已经不再是 `/menu` 与 bare config cards 跨 `UIEvent` 边界的主载体：
  - `/help`、静态帮助目录、daemon upgrade / vscode migration cards、legacy 测试样例仍可直接使用 `FeishuDirectCommandCatalog`
  - `/menu` 与 bare config cards 现在跨边界携带的是 `control.FeishuCommandView`
  - projector 在 adapter 层把它投影成当前卡片 renderer 仍可消费的 `FeishuDirectCommandCatalog`
- `control.FeishuTargetPickerView` 当前已经是 normal mode `/list` / `/use` / `/useall` 跨 `UIEvent` 边界的主载体：
  - unified target picker 现在跨边界携带的是 `control.FeishuTargetPickerView`
  - projector 直接以它为 owner 生成 `target_picker_*` callback payload
  - dropdown 刷新与 confirm 已不再经由 `FeishuDirectSelectionPrompt` 兜底
- `control.FeishuDirectSelectionPrompt` 仍然存在，但已经不再是 workspace/thread selection 的唯一主载体：
  - vscode instance/thread selection 与其余 legacy selection path 现在跨边界携带的是 `control.FeishuSelectionView`
  - projector 在 adapter 层把它投影成当前卡片 renderer 仍可消费的 `FeishuDirectSelectionPrompt`
  - 其他 selection 场景，例如 instance selection、kick-thread confirm，仍可直接使用 `FeishuDirectSelectionPrompt`
- `control.FeishuPathPickerView` 当前已经是路径选择器跨 `UIEvent` 边界的主载体：
  - projector 直接以它为 owner 生成 `path_picker_*` callback payload
  - 当前不会再把目录浏览过程编码回 `FeishuDirectSelectionPrompt`
- 这些 DTO 当前都已经显式标注 owner，并与 query/policy context 分离：
  - DTO 形状暂未全部迁出
  - `UIEvent` 已经携带独立的 `FeishuSelectionContext` / `FeishuCommandContext` / `FeishuRequestContext`
  - Feishu UI controller 已通过这层 boundary 分流 pure navigation；后续继续扩 controller 时，默认仍应优先依赖这些 query/context 元数据，而不是继续直接读 orchestrator 内部字段
  - target picker cards 现在是 “read model -> `FeishuTargetPickerView` -> adapter projection -> Feishu V2 双下拉卡片” 三段；后续修改 normal `/list` / `/use` / `/useall` 的默认选择、摘要文案、confirm 按钮或 stale-selection 行为时，默认应落在 target picker read model / projection 层，而不是回到旧 selection query 函数里继续混改
  - selection cards 现在主要服务于 VS Code / legacy selection path，是 “read model -> `FeishuSelectionView` -> adapter projection -> `FeishuDirectSelectionPrompt`” 四段；后续修改这些旧路径的分页、分组、文案时，默认仍应落在 adapter projection 或 selection view 结构层
  - command/config cards 现在是 “read model -> `FeishuCommandView` -> adapter projection -> `FeishuDirectCommandCatalog`” 四段；后续修改 `/menu` 或 bare config cards 的 breadcrumbs、按钮布局、回退按钮、摘要文案时，默认也应落在 adapter projection 或 command view 结构层，而不是回到 orchestrator query 函数里继续混改
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

- callback payload schema 已收束到 [internal/adapter/feishu/card_action_payload.go](../../internal/adapter/feishu/card_action_payload.go)
- projector 与 gateway 现在共用这份 schema 常量/构造 helper，不再继续各自扩一份裸字符串约定
- 分页导航与 target picker 下拉当前也复用这套 schema：projector 负责写入 `page` / `view_mode` / `return_page` / `picker_id` / `field_name`，gateway 负责解析回 `control.Action`，Feishu UI controller 再用这些字段重建当前页 view 或当前 picker 并 inline replace 原卡。

### 4.2 当前常见 payload 字段

| `kind` | 关键字段 | 当前含义 |
| --- | --- | --- |
| `attach_instance` | `instance_id` | 接管指定实例 |
| `attach_workspace` | `workspace_key` | 接管指定工作区 |
| `create_workspace` | 无额外字段 | 直接打开本地目录 path picker；当前只作为 legacy transport 兼容入口存在 |
| `use_thread` | `thread_id`、`allow_cross_workspace` | 选择 thread，必要时允许跨 workspace |
| `show_threads` / `show_all_threads` / `show_scoped_threads` | `view_mode`、`page` | normal mode 下重新打开 target picker；vscode / legacy selection path 下仍用于在当前 same-context thread 列表里切页 |
| `show_all_workspaces` / `show_recent_workspaces` | `page` | normal mode 下重新打开 `/list` target picker；旧分页字段继续保留 transport 兼容 |
| `show_all_thread_workspaces` / `show_recent_thread_workspaces` | `page` | normal mode 下重新打开 `/useall` target picker；旧分页字段继续保留 transport 兼容 |
| `show_workspace_threads` | `workspace_key`、`page`、`return_page` | normal mode 下以指定 workspace 重新打开 target picker；legacy selection path 下仍可表示进入某个 workspace 的会话详情 |
| `target_picker_select_mode` | `picker_id`、`target_value` | unified target picker 的模式按钮回调；gateway 直接从 payload 里的 `target_value` 取当前模式 |
| `target_picker_select_source` | `picker_id`、`target_value` | unified target picker 的来源按钮回调；当前 projector 直接在 payload 里写 `local_directory` / `git_url`，gateway 仍兼容从 `form_value[field_name]` / `option` / `options` 回退取值 |
| `target_picker_select_workspace` | `picker_id`、`field_name` | unified target picker 的工作区下拉回调；gateway 从 `form_value[field_name]` / `option` / `options` 中提取工作区键 |
| `target_picker_select_session` | `picker_id`、`field_name` | unified target picker 的会话下拉回调；gateway 从 `form_value[field_name]` / `option` / `options` 中提取 thread 或 `new_thread` |
| `target_picker_open_path_picker` | `picker_id`、`target_value`、`request_answers` | unified target picker 的子步骤导航；当前 `target_value` 表示 `local_directory` 或 `git_parent_dir`，`request_answers` 用来把 Git 主卡里的 `repo_url` / `directory_name` 草稿一起带回服务端 |
| `target_picker_cancel` | `picker_id`、`request_answers` | unified target picker 的退出按钮；gateway 只需命中当前 active picker，并把 Git 内联表单草稿按同样方式带回，服务端随后清掉 active picker 并原地替换成 notice |
| `target_picker_confirm` | `picker_id`、`target_picker_workspace`、`target_picker_session`、`request_answers` | unified target picker 的确认按钮；`已有工作区` 模式下把当前表单值送到产品层执行 attach / switch / `新建会话`；`添加工作区 / 本地目录` 下按已回填到主卡的目录执行接入；`添加工作区 / Git URL` 下按主卡 Git 表单 + 已选父目录执行导入 |
| `history_page` | `picker_id`、`page` | `/history` 列表页翻页；命中当前 active history 时同步替换当前卡为 loading，然后异步重查当前 thread history |
| `history_detail` | `picker_id`、`turn_id` 或 `field_name + selected option` | `/history` 进入某一轮详情，或在详情页前后切换；gateway 同样兼容 `form_value[field_name]` / `option` / `options` 取值 |
| `run_command` | `command_text` 或 `command` | 把卡片按钮退化成文本命令解析 |
| `path_picker_enter` | `picker_id`、`entry_name` 或 `field_name + selected option` | 进入当前 active picker 里的一个子目录；`/sendfile` 文件模式下通常来自目录下拉 |
| `path_picker_up` | `picker_id` | 回到当前 active picker 的上一级目录 |
| `path_picker_select` | `picker_id`、`entry_name` 或 `field_name + selected option` | 在当前 active picker 里选择一个文件或目录；`/sendfile` 文件模式下通常来自文件下拉，当前只更新待发送文件，不直接触发发送 |
| `path_picker_confirm` | `picker_id` | 用当前 active picker 的已校验结果触发 consumer handoff |
| `path_picker_cancel` | `picker_id` | 结束当前 active picker，并把取消结果交给 consumer 或默认 notice |
| `request_respond` | `request_id`、`request_type`、`request_option_id`、`request_answers`、`request_revision` | 响应 approval、`approval_command`、`approval_file_change`、`approval_network`、`request_user_input`、`permissions_request_approval`、`mcp_server_elicitation`。通用 approval 现在会保留归一化后的 `requestKind` 与 `availableDecisions`，包括 `cancel`；顶层 `tool/requestUserInput` 与 `item/tool/requestUserInput` 继续共用 `request_user_input` 提交流程；`permissions_request_approval` 通过按钮直接携带 scope 语义；`mcp_server_elicitation` 在 url 模式下直接承载 continue/decline/cancel，在 form 模式下也可用局部 `request_answers` 暂存字段后再显式提交 |
| `submit_command_form` | `command_text` 或 `command`、`field_name` | 从表单里取参数后重新走文本命令解析 |
| `submit_request_form` | `request_id`、`request_type`、`request_revision`、`field_name` | 从表单里提取 `request_answers` 后回到 request 响应路径；当前用于顶层/`item` 两种 `request_user_input` 以及 form 模式 `mcp_server_elicitation` |

### 4.3 当前表单提交规则

`gateway_routing.go` 当前约定：

- `submit_command_form`
  - 先读 `value.command_text`，没有则读 `value.command`
  - 参数默认从 `form_value["command_args"]` 读取
  - 若 `value.field_name` 存在，则改为读取该字段
  - 若 `form_value` 为空，则回退 `input_value`
- `submit_request_form`
  - 优先把 `form_value` 整体转成 `request_answers`
  - `request_user_input` 与 form 模式 `mcp_server_elicitation` 当前都只会为“需要手填”的字段渲染 form input（纯选项题不再渲染自由输入框）
  - `request_user_input` 里的 optional 字段当前不会阻止最终提交
  - 表单提交按钮统一带 `request_option_id=submit`
    - `request_user_input` 的文案是“提交答案”
    - `mcp_server_elicitation` 的文案是“提交并继续”
  - `request_user_input` 若本次提交后仍有未答题，orchestrator 会把 request 切到“确认留空提交”状态并刷新 request 卡片
  - request 卡的按钮与表单提交都会携带 `request_revision`
    - 只要当前 request 因为“局部答案已保存”“进入确认态”“取消确认态”“提交失败恢复”而刷新，revision 就会递增
    - 同 daemon 生命周期里的旧 request 卡，如果 revision 落后于当前 pending request，会收到 `request_card_expired`，不会再改写当前草稿状态
  - 确认态按钮用 `request_respond` 回传：
    - `request_option_id=confirm_submit_with_unanswered`：确认留空提交
    - `request_option_id=cancel_submit_with_unanswered`：返回继续补答
  - 若表单没有字段值，再回退 `input_value`
- `path_picker_enter` / `path_picker_select`
  - 旧按钮路径继续直接读取 `entry_name`
  - `select_static` 路径允许 payload 只带 `field_name`
  - gateway 当前按 `action.option -> action.options[0] -> form_value[field_name]` 的顺序提取被选中的目录/文件条目
  - 这样 projector 可以把复用 path picker 统一收敛成紧凑下拉，而不必继续为每个条目单独渲染按钮；当前目录模式使用单目录下拉，文件模式使用目录/文件双下拉
  - 目录下拉当前会在 `CanGoUp=true` 时额外插入一个值为 `..` 的首项；这条值仍然走 `path_picker_enter`，最终由 orchestrator 复用现有 root-boundary 校验解析到父目录
  - 当前路径选择器卡片已不再额外渲染 `path_picker_up` 按钮；目录下拉里的 `..` 是默认“返回上一级”入口，因此卡面统一保持“目录浏览走目录下拉、文件选择走文件下拉（若有）、确认/取消走底部按钮”的结构
- `target_picker_open_path_picker` / `target_picker_confirm`
  - `Git URL` 分支当前会把 `form_value` 里的 `target_picker_git_repo_url` 与 `target_picker_git_directory_name` 解析成 `request_answers`
  - `open_path_picker` 与 `confirm` 都会携带这份草稿，服务端据此回填 active target picker record
  - 这样即使用户在 Git 主卡和 path picker 子步骤之间来回切换，仓库地址与目录名草稿也不会丢失，而且不会进入 `PendingRequest`

`request_user_input` 卡片当前额外的可视语义：

- 卡片顶部会展示 `回答进度 x/y`
- 若存在未答题且用户触发提交，会进入“确认留空提交”提示块（含“继续补答”/“确认提交已有答案”）
- 每道题都会展示 `状态：已回答/待回答`
- 对于非私密题，已暂存答案会显示为 `当前答案：...`
- 对 direct-options 题，若已有已答值，已选项保持 `primary`，其他选项降为 `default`，用于降低误触成本
- 顶层 `tool/requestUserInput` 与 `item/tool/requestUserInput` 当前都复用这一套卡片、草稿暂存与提交/确认状态机
- 真正发起 request 提交后，pending request 不会立刻从 orchestrator 状态里删除；会先记录 `PendingDispatchCommandID`
  - 成功路径仍由上游 `request_resolved` 事件最终清掉 pending request
  - 若 daemon dispatch 失败或 wrapper 显式 reject，会清掉 pending-dispatch 标记、递增 `request_revision`、刷新一张新 request 卡并附带失败 notice
  - 在 pending-dispatch 期间，同一 request 的重复点击会收到“已提交，等待处理”提示，不会重复下发命令

通用 approval request 卡片当前新增的可视语义：

- `approval_command` / `approval_file_change` / `approval_network`
  - 统一复用 approval 卡投影，不再因为 request method 来源不同而静默丢失
  - 选项直接跟随上游 `availableDecisions` 归一化结果，当前至少覆盖 `accept`、`acceptForSession`、`decline`、`cancel`
  - command/file approval 的补充上下文（如 `cwd`、`grantRoot`、`networkApprovalContext`、`additionalPermissions`）会继续保留在 request metadata，供卡片与后续交互使用

MCP request 卡片当前新增的可视语义：

- `permissions_request_approval`
  - 渲染为单张 request 卡
  - 默认按钮是“允许本次 / 本会话允许 / 拒绝”
  - 按钮点击直接走 `request_respond`
- `mcp_server_elicitation`
  - `mode=url`
    - 渲染为 continue/decline/cancel 按钮卡
    - “继续”前允许先去外部页面完成授权或确认
  - `mode=form`
    - 会优先把 top-level flat object schema 投影成字段列表
    - 简单枚举字段可直接用按钮回填局部草稿
    - 需要手填的字段走 form submit，并使用“提交并继续”按钮
    - 若 schema 超过当前平铺能力，会回退成单字段 JSON 输入，而不是直接 unsupported
- 这两类 MCP request 与 `request_user_input` 一样，都会继续携带 `request_revision`，用于阻止同 daemon 生命周期里的旧卡继续改写当前 request 草稿

### 4.4 当前 surface 解析规则

卡片 callback 回到哪个 surface，当前按下面顺序解析：

1. 优先用 `open_message_id -> 已记录的 surfaceSessionID`
2. 如果消息映射找不到，再回退到 callback operator 的 preferred actor id
3. 最后才退到 `open_chat_id`

这个顺序是当前 P2P surface 不被拆裂的前提之一。

## 5. 当前同步 Replace 与 Append 边界

### 5.1 同步 replace 的必要条件

当前 `gateway` 会在命中以下任一路径时，同步等待 handler 结果并返回 callback replace：

1. inline navigation 路径
  - callback payload 带有非空 `daemon_lifecycle_id`
  - action 命中 `control.InlineCardReplacementPolicy(action)`
  - daemon 侧产出的单个 `UIEvent` 显式标记 `InlineReplaceCurrentCard == true`
2. command submission anchor 路径
  - callback payload 带有非空 `daemon_lifecycle_id`
  - action 命中 `control.AllowsCommandSubmissionAnchorReplacement(action)`
  - daemon 返回轻量“命令已提交”替换卡（并且不会吞掉原事件 append）

少任一条，都不会同步等待 replace。

### 5.2 当前被视为 pure navigation 的动作

`control.InlineCardReplacementPolicy(...)` 当前等价覆盖的 pure navigation 动作是：

- `ActionShowCommandMenu`
- bare `/mode`
- bare `/autowhip`
- bare `/reasoning`
- bare `/access`
- bare `/model`
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

当前语义补充：

- 这批动作的 owner 已经从“daemon/gateway 里的散落动作白名单”收束成“`FeishuUIIntent` -> lifecycle policy -> controller replaceable event”三段。
- `/menu` 分组内命令的阶段可见性当前已收束到统一策略：
  - `/follow` 仅在 `vscode_working` 可见
  - `/new` 仅在 `normal_working` 可见
  - `/history` 当前不额外分阶段，normal / vscode 里都默认可见；真正能否拿到历史由当前 route 是否能解析出 thread 决定
  - 其余命令默认可见
  - `switch_target` 分组当前还带一层 mode-aware display projection：
    - `normal mode` 只显示一个入口，标题为 `选择工作区/会话`，实际命令仍是 canonical `/list`
    - `vscode mode` 继续分别显示 `/list`、`/use`、`/useall`
  - orchestrator 与 projector 共同复用该策略函数，避免两侧分叉
- 当前所有可 replace 的 Feishu UI 导航，都采用同一套 lifecycle 策略：
  - daemon freshness：`daemon_lifecycle`
  - view/session 策略：`surface_state_rederived`
  - 不要求额外 view token
- 这意味着同 daemon 生命周期里的旧卡/并发点击，如果仍属于 pure navigation，不会因为“旧 view”被拒绝；它们会基于**当前** surface state 重新生成卡片。
- 本次新增的 target picker 下拉刷新也沿用这条 replace 边界：
  - normal `/list` / `/use` / `/useall` 的模式切换
  - normal `/list` / `/use` / `/useall` 的来源切换
  - normal `/list` 的工作区切换
  - normal `/use` / `/useall` 的工作区切换
  - normal `/list` / `/use` / `/useall` 的会话切换
  - normal `/list` / `/use` / `/useall` 的 `target_picker_open_path_picker` 子步骤切换
  - VS Code / legacy selection path 里的上一页 / 下一页 / 返回分组
  都属于 pure navigation，继续原地替换当前卡，而不是 append 新卡。
- unified target picker 当前额外有一条明确的 UI 语义：
  - picker 首次打开时默认进入 `已有工作区` 模式；如果当前没有任何真实 workspace 候选，才会自动切到 `添加工作区`
  - `已有工作区` 模式下，不再为了“帮用户猜一个候选”去回退到其他 recoverable thread
  - 只有当前 surface 已经处在同 workspace 的 `R5 NewThreadReady` 时，才默认选中 `新建会话`
  - 或者 surface 当前已经绑定到同 workspace 的某个 thread，且该 thread 仍在候选里，才默认选中该 thread
  - 如果只是当前 workspace 已选中，但 surface 处于 detached / unbound，session 会保持空值，等待用户显式选择
  - 但工作区一旦变化，session 下拉不会再 silently fallback 到新的真实 workspace 默认会话
  - 若切到真实 workspace，session 会被主动清空，confirm 按钮随之禁用，直到用户重新选定会话
  - 若切到 `添加工作区`，卡片会改成来源按钮布局：`本地目录` 主卡展示路径字段 + `选择目录` 按钮 + `接入并继续` 主按钮；`Git URL` 主卡展示父目录字段、仓库地址/目录名表单、`选择目录` 按钮与 `克隆并继续` 主按钮
  - `target_picker_open_path_picker` 当前会把主卡 inline replace 成 path picker；path picker confirm/cancel 后，会再 inline restore 回原 target picker 主卡
  - `target_picker_cancel` 当前会直接关闭这张 modal：active picker 被清掉，当前卡 inline replace 成一张无后续动作的 notice；surface 保持原来的 attach/use/new-thread 状态
  - 若当前机器缺少 `git`，`Git URL` 仍显示在来源按钮里，但 `克隆并继续` 会禁用，并额外显示不可用说明

### 5.3 当前明确保持 append-only 的动作

下面这些动作即使来自卡片，也不会同步 replace 当前卡：

- 参数应用，例如 `/mode vscode`、`/autowhip on`
- `path_picker_confirm` / `path_picker_cancel`；它们虽然也先走 `FeishuUIIntent`，但不进入 `InlineCardReplacementPolicy` allow-list，gateway 会立即 ack 并异步处理，最终结果交给 picker consumer，保持 append-only
- `target_picker_confirm`；它会真正执行 attach / switch / `新建会话`，并且 stale selection 时会 append 最新 picker + notice，而不是同步 replace 当前卡
- attach / use / follow / `/new` 这类真正改变产品状态的动作
- `/help` 这类静态帮助/目录卡，即使底层仍是 `FeishuDirectCommandCatalog`，当前也不属于 replaceable UI navigation
- request approve / request submit 的处理结果
- 各类 notice、final reply、补充预览、状态类卡片

当前新增补充：

- stamped 菜单命令里的非 inline 命令（例如 `/status`、`/list`、`/stop`、`/steerall`、`/new`、`/follow`、`/detach`）会先同步 replace 为“命令已提交”锚点卡，再继续 append 原命令结果卡。
- 中间结果卡当前统一偏向直接 append 到会话，而不再 reply 到触发消息；当前命中的路径包括 `当前计划`、共享过程卡，以及文本触发 `/history` 的首张 patchable history card。保留 reply 语义的主路径只剩 final reply（以及图片输出这类非卡片结果）。
- `/history` 当前是单独的混合路径：
  - bare `/history`、`history_page`、`history_detail` 都在 inline-replace allow-list 里
  - card callback 命中时，daemon 会先同步 replace 当前卡为 loading history card
  - 同一动作返回的 `thread.history.read` daemon command 仍会继续异步执行，不会因为同步 replace 而被吞掉
  - 成功/失败结果会优先 patch 回同一张 history 卡；文本触发 `/history` 时会先直接 append 一张 patchable history card，再在结果回来后 `message.patch`
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
    - 若 final reply 走了 split，后台 second-chance 只会重跑主卡对应的原始正文片段；patch 目标固定是主 final card，自身若会再次 split 则静默放弃
    - 这意味着 overflow cards 继续保持 append-only，不会被后台 preview patch 追补，也不会在 patch 时重发
    - patch 目标固定是这张 final reply 自己，不会追加第二张 final card，也不会回填 preview supplement
    - 若 anchor 已因 detach、daemon lifecycle 变化或 turn identity 不匹配而失效，则静默放弃，不再尝试补丁
- bare `/upgrade`、bare `/debug` 在 stamped 菜单卡里会直接同位承接为对应状态/输入卡（replace 当前菜单卡），不再先外跳 append 一张新卡。
- “命令已提交”锚点卡当前会在短延时后尝试 best-effort 自动撤回；撤回失败时仅静默降级，不影响主流程。
- 这条路径不会改变产品动作 owner，也不会把参数应用动作改成 inline replace。
- 共享过程卡（当前承载 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `context_compaction`）不走 callback replace，也不属于旧卡 freshness 判定面：
  - 第一次直接 append 到当前会话，不再 reply 到触发这轮 turn 的源消息
  - 若同一 turn 内继续收到新的可见过程项，则改用 `message.patch` 更新同一消息；当前会把 `exec_command`、`web_search`、`mcp_tool_call`、`dynamic_tool_call` 与 `context_compaction` 累积到同一张“处理中”卡
  - `web_search` 会按动作类型显示行级摘要（例如“搜索 / 打开网页 / 页内查找”），其中 begin 阶段先用“正在搜索网络”占位，end 阶段再把对应行改写成具体摘要
  - `mcp_tool_call` 会以 `MCP：server.tool` 的行级摘要进入同一张卡；完成态会补耗时，失败态会内联失败原因
  - `dynamic_tool_call` 会按 `tool + 参数` 的形式进入同一张卡；若同一 turn 内连续出现同名 tool，则会复用同一行并按首次出现顺序持续追加参数（例如 `Read：a.cpp` -> `Read：a.cpp b.cpp`）；失败态会在该行内补 `（失败）`
  - `context_compaction` 不再单独 append 一张 notice 卡；attached surface 命中 verbose 时，会以 `整理：上下文已整理。` 单行并入共享过程卡
  - 对没有用户可展示文本或图片结果的 `dynamic_tool_call`，当前实现保持静默，不再额外发“空结果”notice
  - 可见性当前统一收敛到 verbose：`exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `context_compaction` 都只在 verbose 可见；normal 继续保留 plan 与 final reply，但不再显示这类共享进行中卡；若 compact 完成发生在无 attached surface 时，replay 到 non-verbose surface 也会保持静默
  - 一旦 assistant 正文开始输出，orchestrator 会终结这张进度卡的生命周期，后续不再继续 patch，避免“正文已出现但进度卡还在跳”的并发偏移
  - 若整轮没有正文，turn 完成时当前实现会直接停止更新并清理内存态，不再额外补一张最终过程卡

## 6. 当前 freshness / old-card 语义

### 6.1 daemon 侧判定

`daemon` 当前对入站动作分三种生命周期判定：

| verdict | 触发条件 | 当前结果 |
| --- | --- | --- |
| `current` | 未命中旧消息窗口，且 `daemon_lifecycle_id` 为空或匹配 | 正常继续处理 |
| `old` | `message_create_time` 或 `menu_click_time` 落在旧窗口外 | 发“旧动作已忽略” notice，不进入产品处理 |
| `old_card` | callback 带 `daemon_lifecycle_id` 且与当前 daemon 不匹配 | 发“旧卡片已过期” notice，不进入产品处理，也不会 replace 当前卡 |

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
    - `request_user_input` 与 form 模式 `mcp_server_elicitation` 的草稿/确认态都属于可变产品状态，所以同 daemon 生命周期内额外要求 `request_revision` 匹配
    - 这条规则只用于防止旧 request 卡继续改写当前 request 草稿，不扩展到 `/menu` / selection / path picker / target picker
  - path picker 当前在这条规则上额外有一个 coarse-grained `picker_id`
    - 它不是每一步导航都变化的 per-view token
    - 但它要求 callback 必须命中当前 surface 上仍然 active 的 picker 生命周期
    - 同 daemon 生命周期里的旧 picker 卡片如果 `picker_id` 不匹配，会直接收到 `path_picker_expired`，不会继续替换当前 active picker
  - target picker 当前也有一个 coarse-grained `picker_id`
    - `target_picker_select_workspace` / `target_picker_select_session` 必须命中当前 active picker，才会继续 inline replace
    - `target_picker_confirm` 还会额外校验当前工作区 / 会话候选是否仍包含用户刚刚提交的组合
    - 同 daemon 生命周期里的旧 target picker 如果 `picker_id` 不匹配或候选已变化，会返回 `target_picker_expired` 或 `target_picker_selection_changed`
    - 当前即使只是“原会话已不再有效”，刷新后的最新 picker 也会把 session 重新置空，而不是 silent fallback 到别的默认候选

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
- [internal/core/control/feishu_command_view.go](../../internal/core/control/feishu_command_view.go)
- [internal/core/control/feishu_path_picker.go](../../internal/core/control/feishu_path_picker.go)
- [internal/adapter/feishu/gateway_runtime.go](../../internal/adapter/feishu/gateway_runtime.go)
- [internal/adapter/feishu/card_action_payload.go](../../internal/adapter/feishu/card_action_payload.go)
- [internal/adapter/feishu/gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [internal/adapter/feishu/projector_exec_command_progress.go](../../internal/adapter/feishu/projector_exec_command_progress.go)
- [internal/adapter/codex/translator_helpers.go](../../internal/adapter/codex/translator_helpers.go)
- [internal/core/orchestrator/service_compact_notice.go](../../internal/core/orchestrator/service_compact_notice.go)
- [internal/core/orchestrator/service_exec_command_progress.go](../../internal/core/orchestrator/service_exec_command_progress.go)
- [internal/core/orchestrator/service_mcp_tool_call_progress.go](../../internal/core/orchestrator/service_mcp_tool_call_progress.go)
- [internal/core/orchestrator/service_replay.go](../../internal/core/orchestrator/service_replay.go)
- [internal/core/orchestrator/service_final_card.go](../../internal/core/orchestrator/service_final_card.go)
- [internal/adapter/feishu/projector_target_picker.go](../../internal/adapter/feishu/projector_target_picker.go)
- [internal/adapter/feishu/projector_selection_view.go](../../internal/adapter/feishu/projector_selection_view.go)
- [internal/adapter/feishu/projector_command_view.go](../../internal/adapter/feishu/projector_command_view.go)
- [internal/adapter/feishu/projector_path_picker.go](../../internal/adapter/feishu/projector_path_picker.go)
- [internal/core/orchestrator/service_feishu_ui_context.go](../../internal/core/orchestrator/service_feishu_ui_context.go)
- [internal/core/orchestrator/service_feishu_ui_controller.go](../../internal/core/orchestrator/service_feishu_ui_controller.go)
- [internal/core/orchestrator/service_target_picker.go](../../internal/core/orchestrator/service_target_picker.go)
- [internal/core/orchestrator/service_path_picker.go](../../internal/core/orchestrator/service_path_picker.go)
- [internal/core/orchestrator/service_feishu_command_view.go](../../internal/core/orchestrator/service_feishu_command_view.go)
- [internal/core/orchestrator/service_surface_selection.go](../../internal/core/orchestrator/service_surface_selection.go)
- [internal/core/orchestrator/service_surface_thread_selection.go](../../internal/core/orchestrator/service_surface_thread_selection.go)
- [internal/app/daemon/app_ingress.go](../../internal/app/daemon/app_ingress.go)
- [internal/app/daemon/app_inbound_lifecycle.go](../../internal/app/daemon/app_inbound_lifecycle.go)

### 7.2 当前关键测试基线

- [internal/core/control/inline_replacement_test.go](../../internal/core/control/inline_replacement_test.go)
  - 锁定 pure navigation 的 lifecycle policy、daemon freshness 与 append-only 的动作集合
- [internal/core/control/feishu_ui_intent_test.go](../../internal/core/control/feishu_ui_intent_test.go)
  - 锁定哪些动作会被分流到 Feishu UI controller，哪些 mixed/product-owned 动作仍留在主 reducer
- [internal/adapter/feishu/projector_test.go](../../internal/adapter/feishu/projector_test.go)
  - 锁定 `FeishuDirectSelectionPrompt` / `FeishuSelectionView` / `FeishuCommandView` / `FeishuDirectCommandCatalog` / `FeishuDirectRequestPrompt` 的 lifecycle stamp、projection 结果与 callback payload 结构
- [internal/adapter/feishu/projector_target_picker_test.go](../../internal/adapter/feishu/projector_target_picker_test.go)
  - 锁定 `FeishuTargetPickerView` 的双下拉 payload、`daemon_lifecycle_id` stamp 与 confirm 按钮结构
- [internal/adapter/feishu/projector_path_picker_test.go](../../internal/adapter/feishu/projector_path_picker_test.go)
  - 锁定 `FeishuPathPickerView` 的按钮 payload、`daemon_lifecycle_id` stamp 与 enter/select 按钮区分
- [internal/core/orchestrator/service_final_card_test.go](../../internal/core/orchestrator/service_final_card_test.go)
  - 锁定 final reply recent anchor 的 turn-scope 回查、同 turn 覆盖、lifecycle 匹配与 detach 清理
- [internal/adapter/feishu/projector_snapshot_final_test.go](../../internal/adapter/feishu/projector_snapshot_final_test.go)
  - 锁定 final reply 在普通场景仍保持单主卡；超长 Markdown / code final 会在 projector 层 split 成主卡 + `✅ 最后答复（续）`，且每张卡单独都能落在 Feishu payload 限制内
- [internal/app/daemon/app_final_card_test.go](../../internal/app/daemon/app_final_card_test.go)
  - 锁定同步 preview 超时后的 second-chance final patch：同卡 `message.patch`、无改进静默跳过、detach 后 anchor 失效即放弃，以及 split final reply 只回补主卡、不重发 overflow cards
- [internal/adapter/feishu/gateway_target_picker_test.go](../../internal/adapter/feishu/gateway_target_picker_test.go)
  - 锁定 `target_picker_*` callback payload 能正确回到 `control.Action`
- [internal/adapter/feishu/gateway_test.go](../../internal/adapter/feishu/gateway_test.go)
  - 锁定 callback payload 解析、同步等待 replace 的触发条件（inline navigation + command submission anchor）、无 lifecycle 导航仍异步 ack，以及共享更新卡的 `message.patch` 出站路径
- [internal/adapter/feishu/projector_exec_command_progress_test.go](../../internal/adapter/feishu/projector_exec_command_progress_test.go)
  - 锁定共享过程卡对 `exec_command` / `web_search` / `mcp_tool_call` / `dynamic_tool_call` / `context_compaction` 行级摘要的投影边界，以及首次直接 append 与后续 update 的同卡更新语义
- [internal/adapter/codex/translator_requests_test.go](../../internal/adapter/codex/translator_requests_test.go)
  - 锁定 `web_search` item started/completed 的 kind 归一化与 `query` / `actionType` / `queries` / `url` / `pattern` 提取，以及 `dynamic_tool_call` 的 `tool` / `arguments` / 结构化摘要提取
- [internal/adapter/feishu/gateway_delete_message_test.go](../../internal/adapter/feishu/gateway_delete_message_test.go)
  - 锁定 message.delete 出站能力与“消息已不存在”类错误的静默降级
- [internal/adapter/feishu/gateway_path_picker_test.go](../../internal/adapter/feishu/gateway_path_picker_test.go)
  - 锁定 `path_picker_*` callback payload 能正确回到 `control.Action`
- [internal/core/orchestrator/service_test.go](../../internal/core/orchestrator/service_test.go)
  - 锁定 `UIEventFeishuTargetPicker` 会携带显式 `FeishuTargetPickerContext`，以及 normal `/list` 的基础 target picker 语义
- [internal/core/orchestrator/service_exec_command_progress_test.go](../../internal/core/orchestrator/service_exec_command_progress_test.go)
  - 锁定共享过程卡对 `exec_command` / `web_search` / `dynamic_tool_call` 的可见性分档、同卡复用、正文出现后终止、同类 tool 行级聚合、失败态行内标记，以及 turn 完成清理语义
- [internal/core/orchestrator/service_mcp_tool_call_progress_test.go](../../internal/core/orchestrator/service_mcp_tool_call_progress_test.go)
  - 锁定 `mcp_tool_call` 已并入共享过程卡：started/failed 的同卡复用、去重与行级摘要更新语义
- [internal/core/orchestrator/service_compact_notice_test.go](../../internal/core/orchestrator/service_compact_notice_test.go)
  - 锁定 `context_compaction` 已并入共享过程卡：attached verbose 进入 `整理` 行、normal/quiet 保持静默，以及无 surface 时的 replay 只在 verbose attach 下可见
- [internal/core/orchestrator/service_image_output_test.go](../../internal/core/orchestrator/service_image_output_test.go)
  - 锁定 `dynamic_tool_call` 在有文本/图片输出时仍走原结果渲染路径，而空输出场景保持静默、不再补缺省 notice
- [internal/core/orchestrator/service_target_picker_test.go](../../internal/core/orchestrator/service_target_picker_test.go)
  - 锁定 target picker 的 inline refresh、confirm attach / `新建会话`、recoverable-only workspace headless 路径，以及 stale selection 不会 silent fallback
- [internal/core/orchestrator/service_path_picker_test.go](../../internal/core/orchestrator/service_path_picker_test.go)
  - 锁定路径规范化、root 边界、symlink escape、owner / expire / active picker gate、consumer handoff
- [internal/core/orchestrator/service_local_request_test.go](../../internal/core/orchestrator/service_local_request_test.go)
  - 锁定 `UIEvent` 现在会携带显式 `Feishu*Context` query/policy 元数据；selection/command view 的 UI owner 已切到 read model，但用户可见行为保持不变
- [internal/core/orchestrator/service_local_request_menu_test.go](../../internal/core/orchestrator/service_local_request_menu_test.go)
  - 锁定 `/help` 与 `/menu` 当前共用 display projection：normal mode 会把 `/list` / `/use` / `/useall` 收口成 `选择工作区/会话`，vscode mode 继续保留三者分开展示
- [internal/app/daemon/app_test.go](../../internal/app/daemon/app_test.go)
  - 锁定 daemon ingress 统一入口下的 inline replace 结果、菜单命令提交态锚点（replace 提交态 + append 结果）、`/help` 保持 append-only、active path picker 会阻断 competing `/menu`、same-daemon pure navigation 采用 current-surface rerender，以及 old-card 导航/命令被拒绝而不是继续 replace
- [internal/app/daemon/app_submission_anchor_test.go](../../internal/app/daemon/app_submission_anchor_test.go)
  - 锁定阶段 A/B/C 菜单提交承接行为：普通命令“提交态锚点 + append 结果 + 延时自动撤回”、bare `/upgrade` 同位承接 replace
- [internal/app/daemon/app_inbound_lifecycle_test.go](../../internal/app/daemon/app_inbound_lifecycle_test.go)
  - 锁定 old / old-card 生命周期分类，以及 reject detail 已按当前 UI intent / command 语义收束
- [internal/core/orchestrator/service_config_prompt_test.go](../../internal/core/orchestrator/service_config_prompt_test.go)
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
5. active picker confirm / cancel 是否意外变成 replace，掩盖了真正的 consumer 结果
6. 没有 `daemon_lifecycle_id` 的 callback 是否被错误地当成可同步 replace
7. target picker confirm 是否会对 stale 选择 silent fallback 到别的默认候选
8. request prompt / selection prompt / path picker / target picker 是否把产品状态机职责偷渡进 Feishu UI 层

## 待讨论取舍

- 是否要把“缺少 `daemon_lifecycle_id` 的纯导航 callback”从当前的兼容异步路径，收紧成显式 reject 或显式降级提示；这会影响旧卡兼容性与 freshness 保证之间的取舍。
- final reply split 当前采用“主卡保留原标题与 footer，overflow 统一标题为 `✅ 最后答复（续）`”的最小语义；是否要进一步升级成显式 `1/N` 编号、或把 footer 改挂到最后一张 continuation card，仍是产品取舍。

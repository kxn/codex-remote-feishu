# Feishu 卡片 UI 状态机

> Type: `general`
> Updated: `2026-04-13`
> Summary: 在阶段 1 的显式 Feishu UI query/context 边界和阶段 2 的 Feishu UI controller 分流之上，阶段 3 把 selection cards 拆成 view + adapter projection，阶段 4 又把 `/menu` 与 bare config cards 的最终投影 owner 下沉到 Feishu adapter；当前又补上了可复用 `FeishuPathPickerView`、`path_picker_*` callback 协议、active picker 的 same-daemon freshness / append-only confirm-cancel 边界，以及 `request_user_input` 多题分题暂存与“仅为需要手填的问题渲染表单输入”的卡片语义。

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
- `daemon`
  - 负责 old-card / old-message 生命周期判定
  - 负责在 ingress 层统一把动作交给主 `ApplySurfaceAction()` 入口；`FeishuUIIntent` 分流发生在 service 内，避免绕开 request/path-picker 等产品门禁
  - 负责只在安全条件下把同上下文导航转成 `ReplaceCurrentCard`
  - 当前只有当 action 命中 **inline-replace allow-list**（并非所有 `FeishuUIIntent`）、且 controller 产出的 `UIEvent` 显式标记 `InlineReplaceCurrentCard` 时，才会真正发 inline replace
- `orchestrator / Feishu UI controller`
  - 负责 `show_*`、`/menu`、bare config-card 这类 pure navigation 的 controller 分流与事件构建
  - 负责通过阶段 1 暴露的 `Feishu*Context` query/policy 边界生成 UI-owned read model 与 request 事件
  - 对 workspace/thread selection，当前先产出 `FeishuSelectionView` read model，再连同 `FeishuSelectionContext` 穿过 `UIEvent` 边界
  - 对 `/menu` 与 bare `/mode` `/autowhip` `/reasoning` `/access` `/model`，当前先产出 `FeishuCommandView` read model，再连同 `FeishuCommandContext` 穿过 `UIEvent` 边界
  - 对飞书文件/目录选择器，当前先产出 `FeishuPathPickerView` read model，再连同 `FeishuPathPickerContext` 穿过 `UIEvent` 边界；进入目录、返回上一级、文件选择属于 controller 内 pure navigation，confirm/cancel 则转到 picker consumer handoff
- `projector`
  - 负责把 `control.UIEvent` 渲染成 Feishu 卡片
  - 负责把当前需要的 callback payload 字段写进卡片按钮/表单
  - 当前是 selection 与 command/config cards 最终 projection 的 owner：
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
| `/menu` 首页 / 分组 / 返回 | `feishu-ui-owned` | 当前由 Feishu UI controller 处理同一张命令菜单内的层级切换；不再直接进入主 reducer，也不改 core route |
| `show_all_workspaces` / `show_recent_workspaces` | `feishu-ui-owned` | 当前由 Feishu UI controller 处理工作区列表展开/收起；不改变 attach 状态 |
| `show_threads` / `show_all_threads` / `show_scoped_threads` | `feishu-ui-owned` | 当前由 Feishu UI controller 处理最近会话与“当前工作区全部会话”的视图切换；真正接管 thread 不在这里发生 |
| `show_workspace_threads` / `show_all_thread_workspaces` / `show_recent_thread_workspaces` | `feishu-ui-owned` | 当前由 Feishu UI controller 处理 `/useall` 里的 workspace-group 展开/返回；不直接改变 selected thread |
| `path_picker_enter` / `path_picker_up` / `path_picker_select` | `feishu-ui-owned` | 当前由 Feishu UI controller 处理同一张路径选择器卡片内的浏览、返回与文件选择；命中当前 active picker 时直接原地替换当前卡 |
| `path_picker_confirm` / `path_picker_cancel` | `mixed` | callback 协议与 owner/freshness 校验仍属 Feishu UI；这两类动作当前不在 inline-replace allow-list，回调会立即 ack 并异步处理；真正确认后做什么、取消后回什么卡由 picker consumer 决定 |
| bare `/mode` / `/autowhip` / `/reasoning` / `/access` / `/model` | `mixed` | bare open-card 当前由 Feishu UI controller 处理；真正应用参数后仍进入产品状态变更，因此 apply 继续保持 append-only |
| `request approve` / `request_user_input` / `captureFeedback` | `mixed` | 卡片按钮、表单字段、lifecycle stamp 属于 Feishu UI；request gate、反馈 capture、`request_user_input` 的分题暂存与最终提交校验属于产品状态机 |
| `attach_instance` / `attach_workspace` / `use_thread` | `product-owned` | 卡片只负责把选择结果送入产品层；是否允许接管、是否跨 workspace、接管后进入什么 route 都由 orchestrator 决定 |
| `/follow` | `product-owned` | 是否可用、是否被冻结、跟随到哪个 thread、normal/vscode mode 差异都属于 core 状态机 |
| `/new` | `product-owned` | 是否进入 `new_thread_ready`、何时消耗第一条消息、request gate 是否阻断都属于 core 状态机 |

补充规则：

- `control.FeishuDirectRequestPrompt` 当前仍是**产品层拥有语义、Feishu 层拥有序列化**的 shared DTO。
- `control.FeishuDirectCommandCatalog` 仍保留为当前 card renderer 的过渡 DTO，但已经不再是 `/menu` 与 bare config cards 跨 `UIEvent` 边界的主载体：
  - `/help`、静态帮助目录、daemon upgrade / vscode migration cards、legacy 测试样例仍可直接使用 `FeishuDirectCommandCatalog`
  - `/menu` 与 bare config cards 现在跨边界携带的是 `control.FeishuCommandView`
  - projector 在 adapter 层把它投影成当前卡片 renderer 仍可消费的 `FeishuDirectCommandCatalog`
- `control.FeishuDirectSelectionPrompt` 仍然存在，但已经不再是 workspace/thread selection 跨 `UIEvent` 边界的主载体：
  - workspace/thread selection 现在跨边界携带的是 `control.FeishuSelectionView`
  - projector 在 adapter 层把它投影成当前卡片 renderer 仍可消费的 `FeishuDirectSelectionPrompt`
  - 其他 selection 场景，例如 instance selection、kick-thread confirm，仍可直接使用 `FeishuDirectSelectionPrompt`
- `control.FeishuPathPickerView` 当前已经是路径选择器跨 `UIEvent` 边界的主载体：
  - projector 直接以它为 owner 生成 `path_picker_*` callback payload
  - 当前不会再把目录浏览过程编码回 `FeishuDirectSelectionPrompt`
- 这些 DTO 当前都已经显式标注 owner，并与 query/policy context 分离：
  - DTO 形状暂未全部迁出
  - `UIEvent` 已经携带独立的 `FeishuSelectionContext` / `FeishuCommandContext` / `FeishuRequestContext`
  - Feishu UI controller 已通过这层 boundary 分流 pure navigation；后续继续扩 controller 时，默认仍应优先依赖这些 query/context 元数据，而不是继续直接读 orchestrator 内部字段
  - selection cards 现在是 “read model -> `FeishuSelectionView` -> adapter projection -> `FeishuDirectSelectionPrompt`” 四段；后续修改 `/list` / `/use` / `/useall` 的分组、文案、recent/all 视图时，默认应落在 adapter projection 或 selection view 结构层，而不是回到 selection query 函数里继续混改
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

### 4.2 当前常见 payload 字段

| `kind` | 关键字段 | 当前含义 |
| --- | --- | --- |
| `attach_instance` | `instance_id` | 接管指定实例 |
| `attach_workspace` | `workspace_key` | 接管指定工作区 |
| `use_thread` | `thread_id`、`allow_cross_workspace` | 选择 thread，必要时允许跨 workspace |
| `show_workspace_threads` | `workspace_key` | 展开某个 workspace 下的全部会话 |
| `run_command` | `command_text` 或 `command` | 把卡片按钮退化成文本命令解析 |
| `path_picker_enter` | `picker_id`、`entry_name` | 进入当前 active picker 里的一个子目录 |
| `path_picker_up` | `picker_id` | 回到当前 active picker 的上一级目录 |
| `path_picker_select` | `picker_id`、`entry_name` | 在当前 active picker 里选择一个文件或目录 |
| `path_picker_confirm` | `picker_id` | 用当前 active picker 的已校验结果触发 consumer handoff |
| `path_picker_cancel` | `picker_id` | 结束当前 active picker，并把取消结果交给 consumer 或默认 notice |
| `request_respond` | `request_id`、`request_type`、`request_option_id`、`request_answers` | 响应 approval 或 `request_user_input`；`request_user_input` 可携带局部 `request_answers` 进入分题暂存 |
| `submit_command_form` | `command_text` 或 `command`、`field_name` | 从表单里取参数后重新走文本命令解析 |
| `submit_request_form` | `request_id`、`request_type`、`field_name` | 从表单里提取 `request_answers` 后回到 request 响应路径 |

### 4.3 当前表单提交规则

`gateway_routing.go` 当前约定：

- `submit_command_form`
  - 先读 `value.command_text`，没有则读 `value.command`
  - 参数默认从 `form_value["command_args"]` 读取
  - 若 `value.field_name` 存在，则改为读取该字段
  - 若 `form_value` 为空，则回退 `input_value`
- `submit_request_form`
  - 优先把 `form_value` 整体转成 `request_answers`
  - `request_user_input` 当前只会为“需要手填”的问题渲染 form input（纯选项题不再渲染自由输入框）
  - 若表单没有字段值，再回退 `input_value`

### 4.4 当前 surface 解析规则

卡片 callback 回到哪个 surface，当前按下面顺序解析：

1. 优先用 `open_message_id -> 已记录的 surfaceSessionID`
2. 如果消息映射找不到，再回退到 callback operator 的 preferred actor id
3. 最后才退到 `open_chat_id`

这个顺序是当前 P2P surface 不被拆裂的前提之一。

## 5. 当前同步 Replace 与 Append 边界

### 5.1 同步 replace 的必要条件

当前 `gateway` 只会在同时满足下面三条时，同步等待 handler 结果并返回 callback replace：

1. callback payload 带有非空 `daemon_lifecycle_id`
2. action 命中 `control.InlineCardReplacementPolicy(action)`
3. daemon 侧产出的单个 `UIEvent` 显式标记 `InlineReplaceCurrentCard == true`

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
- `ActionPathPickerEnter`
- `ActionPathPickerUp`
- `ActionPathPickerSelect`

当前语义补充：

- 这批动作的 owner 已经从“daemon/gateway 里的散落动作白名单”收束成“`FeishuUIIntent` -> lifecycle policy -> controller replaceable event”三段。
- 当前所有可 replace 的 Feishu UI 导航，都采用同一套 lifecycle 策略：
  - daemon freshness：`daemon_lifecycle`
  - view/session 策略：`surface_state_rederived`
  - 不要求额外 view token
- 这意味着同 daemon 生命周期里的旧卡/并发点击，如果仍属于 pure navigation，不会因为“旧 view”被拒绝；它们会基于**当前** surface state 重新生成卡片。

### 5.3 当前明确保持 append-only 的动作

下面这些动作即使来自卡片，也不会同步 replace 当前卡：

- 参数应用，例如 `/mode vscode`、`/autowhip on`
- `path_picker_confirm` / `path_picker_cancel`；它们虽然也先走 `FeishuUIIntent`，但不进入 `InlineCardReplacementPolicy` allow-list，gateway 会立即 ack 并异步处理，最终结果交给 picker consumer，保持 append-only
- attach / use / follow / `/new` 这类真正改变产品状态的动作
- `/help` 这类静态帮助/目录卡，即使底层仍是 `FeishuDirectCommandCatalog`，当前也不属于 replaceable UI navigation
- request approve / request submit 的处理结果
- 各类 notice、final reply、补充预览、状态类卡片

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
  - path picker 当前在这条规则上额外有一个 coarse-grained `picker_id`
    - 它不是每一步导航都变化的 per-view token
    - 但它要求 callback 必须命中当前 surface 上仍然 active 的 picker 生命周期
    - 同 daemon 生命周期里的旧 picker 卡片如果 `picker_id` 不匹配，会直接收到 `path_picker_expired`，不会继续替换当前 active picker

因此当前的 same-daemon 并发点击 / 旧 view 点击策略是：

- pure navigation：允许，按当前 surface state 重建
- 产品动作：不走 inline replace，仍按 append-only 产品语义处理
- old daemon card：直接拒绝并提示重开卡片

## 7. 当前回归基线

### 7.1 当前关键实现文件

- [internal/core/control/feishu_ui_intent.go](../../internal/core/control/feishu_ui_intent.go)
- [internal/core/control/feishu_ui_lifecycle.go](../../internal/core/control/feishu_ui_lifecycle.go)
- [internal/core/control/feishu_ui_boundary.go](../../internal/core/control/feishu_ui_boundary.go)
- [internal/core/control/feishu_selection_view.go](../../internal/core/control/feishu_selection_view.go)
- [internal/core/control/feishu_command_view.go](../../internal/core/control/feishu_command_view.go)
- [internal/core/control/feishu_path_picker.go](../../internal/core/control/feishu_path_picker.go)
- [internal/adapter/feishu/gateway_runtime.go](../../internal/adapter/feishu/gateway_runtime.go)
- [internal/adapter/feishu/card_action_payload.go](../../internal/adapter/feishu/card_action_payload.go)
- [internal/adapter/feishu/gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [internal/adapter/feishu/projector_selection_view.go](../../internal/adapter/feishu/projector_selection_view.go)
- [internal/adapter/feishu/projector_command_view.go](../../internal/adapter/feishu/projector_command_view.go)
- [internal/adapter/feishu/projector_path_picker.go](../../internal/adapter/feishu/projector_path_picker.go)
- [internal/core/orchestrator/service_feishu_ui_context.go](../../internal/core/orchestrator/service_feishu_ui_context.go)
- [internal/core/orchestrator/service_feishu_ui_controller.go](../../internal/core/orchestrator/service_feishu_ui_controller.go)
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
- [internal/adapter/feishu/projector_path_picker_test.go](../../internal/adapter/feishu/projector_path_picker_test.go)
  - 锁定 `FeishuPathPickerView` 的按钮 payload、`daemon_lifecycle_id` stamp 与 enter/select 按钮区分
- [internal/adapter/feishu/gateway_test.go](../../internal/adapter/feishu/gateway_test.go)
  - 锁定 callback payload 解析、同步等待 replace 的触发条件、无 lifecycle 导航仍异步 ack
- [internal/adapter/feishu/gateway_path_picker_test.go](../../internal/adapter/feishu/gateway_path_picker_test.go)
  - 锁定 `path_picker_*` callback payload 能正确回到 `control.Action`
- [internal/core/orchestrator/service_test.go](../../internal/core/orchestrator/service_test.go)
  - 锁定 workspace selection read model 保留全量语义条目，再由 projection 决定 recent/all 可见范围
- [internal/core/orchestrator/service_path_picker_test.go](../../internal/core/orchestrator/service_path_picker_test.go)
  - 锁定路径规范化、root 边界、symlink escape、owner / expire / active picker gate、consumer handoff
- [internal/core/orchestrator/service_local_request_test.go](../../internal/core/orchestrator/service_local_request_test.go)
  - 锁定 `UIEvent` 现在会携带显式 `Feishu*Context` query/policy 元数据；selection/command view 的 UI owner 已切到 read model，但用户可见行为保持不变
- [internal/app/daemon/app_test.go](../../internal/app/daemon/app_test.go)
  - 锁定 daemon ingress 统一入口下的 inline replace 结果、`/help` 保持 append-only、active path picker 会阻断 competing `/menu`、same-daemon pure navigation 采用 current-surface rerender，以及 old-card 导航/命令被拒绝而不是继续 replace
- [internal/app/daemon/app_inbound_lifecycle_test.go](../../internal/app/daemon/app_inbound_lifecycle_test.go)
  - 锁定 old / old-card 生命周期分类，以及 reject detail 已按当前 UI intent / command 语义收束
- [internal/core/orchestrator/service_config_prompt_test.go](../../internal/core/orchestrator/service_config_prompt_test.go)
- [internal/core/orchestrator/service_headless_thread_test.go](../../internal/core/orchestrator/service_headless_thread_test.go)
- [internal/core/orchestrator/service_thread_selection_test.go](../../internal/core/orchestrator/service_thread_selection_test.go)
  - 锁定 request gate 对 `/follow`、`/use`、selection rebind 的冻结
  - 锁定 thread selection read model 保留全量 workspace groups，而 recent/all 裁剪与展开动作由 projection 决定

## 8. 审计清单

每次改 Feishu 卡片 UI 相关行为，提交前至少检查：

1. projector 发出的 `kind` / 额外字段，gateway 是否还能完整解析
2. 某个同上下文导航动作是否意外从 replace 退回 append，或反之
3. path picker 的 `picker_id` / `entry_name` 是否与 gateway 解析和 active picker freshness 仍然一致
4. old card 是否还能继续命中产品状态变更
5. active picker confirm / cancel 是否意外变成 replace，掩盖了真正的 consumer 结果
6. 没有 `daemon_lifecycle_id` 的 callback 是否被错误地当成可同步 replace
7. request prompt / selection prompt / path picker 是否把产品状态机职责偷渡进 Feishu UI 层

## 待讨论取舍

- 是否要把“缺少 `daemon_lifecycle_id` 的纯导航 callback”从当前的兼容异步路径，收紧成显式 reject 或显式降级提示；这会影响旧卡兼容性与 freshness 保证之间的取舍。

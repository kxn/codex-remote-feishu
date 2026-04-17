# Feishu 产品设计

> Type: `general`
> Updated: `2026-04-17`
> Summary: 描述当前 Go 版本的 Feishu surface 行为，并同步 canonical 命令清单、reply auto-steer、manual `/compact`、`/cron` 与共享过程卡的产品语义。

## 1. 文档定位

这份文档描述的是**当前 Go 版本实现中的 Feishu 产品层行为**。

当前链路是：

`Feishu Gateway -> control.Action -> orchestrator -> control.UIEvent -> Feishu Projector`

它不再是独立 bot 进程，也不再依赖公开的 `relay.render.v1` 拉流接口。

## 2. Surface 模型

产品状态按 `surfaceSessionId` 建模，而不是按裸 `userId` 建模。

当前实现：

- P2P 会话：`feishu:<gatewayId>:user:<preferredActorId>`
- 群聊/其他 chat：`feishu:<gatewayId>:chat:<chatId>`

其中 `preferredActorId` 的优先级当前是：

- `open_id`
- `user_id`
- `union_id`

卡片回调还有一个额外规则：

- 若 callback 带着 `open_message_id`，会先回查该消息已记录的 `surfaceSessionId`
- 只有当消息没有命中已记录 surface 时，才回退到 callback 自带的 operator id 重新推导 surface

这样可以避免同一个私聊用户在“文本消息 / 菜单 / 卡片按钮”之间因为拿到的飞书 id 类型不同而裂成两个 surface。

这个规则定义在 [gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go) 的 `surfaceIDForInbound()`、`surfaceForCardAction()` 与相关 user-id 解析函数中。

## 3. 当前支持的飞书入口

### 3.1 文本消息

普通文本进入：

- `surface.message.text`

当前 ACK 语义已经拆成两层：

- slash command 文本
  - 仍走轻量同步解析
  - 识别出 command 后立即 ACK，再异步进入后续 handler
- 普通文本 / reply 文本
  - 不再要求在飞书 callback 里同步跑完 quoted-input 补查、daemon/orchestrator 和 UI 投影
  - 当前会先完成轻量归类，然后进入 gateway-local 的 per-surface FIFO lane
  - 成功入 lane 后立即 ACK，lane 内再继续做引用消息补查和后续业务处理

当前主展示的 canonical 文本命令：

- `/help`
- `/menu`
- `/list`
- `/status`
- `/use`
- `/useall`
- `/new`
- `/history`
- `/follow`
- `/detach`
- `/stop`
- `/compact`
- `/steerall`
- `/sendfile`
- `/mode`
- `/autowhip`
- `/model`
- `/reasoning`
- `/access`
- `/verbose`
- `/cron`
- `/debug`
- `/upgrade`

alias 仍继续兼容，但不再作为主展示入口：

- `/threads`、`/sessions` -> `/use`
- `/approval` -> `/access`
- `/effort` -> `/reasoning`
- 旧 `/autocontinue` -> `/autowhip`
- `menu` -> `/menu`
- 旧 `/newinstance`、`/killinstance` -> 显式迁移提示

其中：

- `/menu` 当前会打开阶段感知的命令首页，而不是静态平铺目录
- `/menu` 和参数卡当前采用紧凑按钮优先布局，尽量让主操作一屏可见；`/help` 保持文本帮助取向
- bare `/reasoning`、`/access`、`/mode`、`/autowhip` 会返回当前状态 + 快捷按钮 + 单字段表单
- bare `/model` 会返回当前状态 + 常见示例 + 手动输入表单
- bare `/debug`、`/upgrade` 会返回当前状态卡；卡内既有快捷按钮，也有手动输入表单
- bare `/cron` 会返回当前实例专属的 Cron 菜单卡，卡内提供 `status / list / edit / reload / repair` 快捷入口

除了纯文本外，当前还支持两类更完整的入站整理：

- `post`
  - 单条图文混合消息会把文本和图片一起整理进同一次 prompt
  - 当前会先轻量校验正文结构，再进入与普通文本共用的 per-surface FIFO lane
  - 正文里的图片下载已经移到 ACK 之后
- reply / quote
  - 会补查被引用消息
  - 引用文本会作为额外提示文本带入
  - 引用图文混合消息时，会把其中的文本和图片一起带入
  - 若 reply 目标命中当前 surface 正在 processing 的 source message，且 reply 当前消息属于文本 / 图片输入，则不会把被引用原消息再次重发，而是把“当前 reply 自身内容”直接 steer 进当前 running turn
- `merge_forward`
  - 正文转发聊天记录不会再拍平成普通 prose 摘要
  - 当前会先尽早 ACK，再在 per-surface FIFO lane 里展开整棵转发树
  - 当前会先构造成结构化树，再以首个文本 input 发送：
    - `<forwarded_chat_bundle_v1>{...json...}</forwarded_chat_bundle_v1>`
  - reply / quote 一个转发聊天记录时，复用同一结构，但 wrapper 会变成：
    - `<quoted_forwarded_chat_bundle_v1>{...json...}</quoted_forwarded_chat_bundle_v1>`
  - tree 中保留 bundle 层级、消息顺序、sender、message type 与 `image_refs`
  - 真实图片会按稳定遍历顺序作为后续 `local_image` / `remote_image` inputs 追加，JSON 里只保留引用关系
  - `file` / unknown / unavailable child 不会 silently 丢失，而是保留占位节点

### 3.2 菜单事件

当前静态推荐机器人菜单 key：

- `menu`
- `stop`
- `new`
- `reasoning`
- `model`
- `access`

canonical menu key 语法当前固定为：

- 去掉前导 `/`
- 参数使用 `_` 连接
- 完整 slash command 与完整 menu key 一一对应

例子：

- `/list` <-> `list`
- `/use` <-> `use`
- `/reasoning high` <-> `reasoning_high`
- `/access confirm` <-> `access_confirm`
- `/mode vscode` <-> `mode_vscode`
- `/autowhip on` <-> `autowhip_on`
- `/model gpt-5.4` <-> `model_gpt-5.4`

旧 menu key alias 仍兼容：

- `threads` / `sessions` -> `/use`
- `approval_confirm` -> `/access confirm`
- `reason_high` -> `/reasoning high`
- `autocontinue` / `autocontinue_on` -> `/autowhip`

### 3.3 图片消息

图片进入：

- `surface.message.image`

图片当前会先完成轻量 envelope 解析并进入 per-surface FIFO lane。

真实图片下载已经移到 ACK 之后；下载成功后再进入 staged image 队列。

### 3.4 Reaction 创建

当前只消费：

- `im.message.reaction.created_v1`

它会被翻译成：

- `surface.message.reaction.created`

当前**不处理** reaction deleted 事件。

当前实现只把下列 reaction 当作产品动作：

- 用户对 **queued 主文本消息** 加 `ThumbsUp`

触发条件：

- 目标消息必须命中当前 surface 某个 `queued` queue item 的 `SourceMessageID`
- 当前 attached instance 必须已有 `ActiveTurnID`
- queue item 的 `FrozenThreadID` 必须和当前 active turn thread 一致

当前不会触发 steering 的情况：

- 给图片消息点赞
- 给已 dispatching/running/completed 的消息点赞
- 当前没有 active running turn
- queued item 属于别的 frozen thread
- bot 自己补上的 reaction 回流事件

ACK 语义上，reaction created 现在也会进入和普通文本共用的 per-surface FIFO lane，而不是继续单独同步直达 handler。

这样可以避免“原文本还在后台排队处理中，但 reaction 已经先进入 orchestrator”这类同 surface 乱序。

### 3.5 消息撤回

当前也消费：

- `im.message.recalled_v1`

它会被翻译成：

- `surface.message.recalled`

当前用途：

- 撤回尚未派发的排队输入
- 取消尚未绑定的 staged image

如果飞书侧没有订阅这个事件：

- 基础收发仍能工作
- 但“撤回消息即取消输入”这条体验不会生效

ACK 语义上，message recalled 现在同样会进入和普通文本共用的 per-surface FIFO lane，保证撤回和原文本之间仍按同 surface 顺序处理。

### 3.6 卡片回调

当前支持以下几类卡片回调：

- command menu 首页 / 面包屑 / submenu 导航
- 参数卡快捷按钮
- 参数卡表单提交按钮
- selection prompt 选择
- approval request 确认

其中参数表单当前统一走 `submit_command_form` 回调，把卡片内输入的参数尾巴拼回 canonical slash command，再复用文本命令解析链路。

旧的 `start_command_capture` / `cancel_command_capture` 只保留 `/model` 历史卡片兼容职责：

- 旧“开始输入模型名”卡片点下去时，不再创建新的 capture 状态
- 服务端会直接重新打开新的 `/model` 表单卡
- 若 daemon 热更新前已经进入旧 capture 等待态，下一条文本会被直接转换成 `/model <输入>` 立即应用，避免用户卡死

approval request 卡片当前按动态 option 渲染，常见选项包括：

- `accept`
- `acceptForSession`
- `decline`
- `captureFeedback`

不再支持靠文本回复 `yes/no` 处理确认。

### 3.7 旧生命周期动作判定

每条飞书入站 action 在进入业务处理前，都会先统一产出 lifecycle verdict：

- `current`
- `old`
- `old_card`

当前判定规则：

- 文本消息 / 文本命令：
  - 以消息创建时间对比当前 daemon 生命周期
- 菜单事件：
  - 以菜单点击时间对比当前 daemon 生命周期
- 卡片回调：
  - 依赖发卡时写入按钮 value 的 `daemon_lifecycle_id`
  - 若回调带回的 lifecycle id 与当前 daemon 不一致，则判为 `old_card`

当前产品策略：

- `old`
  - 不执行原动作
  - 给用户返回“旧动作已忽略，请重发或重新点击”
- `old_card`
  - 不执行按钮逻辑
  - 给用户返回“旧卡片已过期，请重新触发对应命令”

同一 daemon 生命周期内的精确 `event_id` 去重当前不是这条产品规则的一部分。

但在 ordinary inbound 的 gateway-local FIFO lane 内，当前已经补了一层 daemon 生命周期内的短窗 `event_id/request_id` 去重，用来压住飞书普通消息、reaction 和 recalled 的重复投递执行风险。

## 4. Attachment 与 thread 路由

### 4.1 产品模式与 `/mode`

新 surface 默认进入 `normal mode`。

当前两种模式分工已经固定：

1. `normal mode`
   - `/list` 列**工作区**
   - detached `/use` / `/useall` 仍可全局继续已有会话
   - 已 attach 后 `/use` / `/useall` 只看当前 workspace
   - `/new` 只要求当前 workspace 已知
   - `/follow` 只返回迁移提示
2. `vscode mode`
   - 需要显式 `/mode vscode`
   - `/list` 改为列**在线 VS Code 实例**
   - attach 后默认走 follow-first
   - detached `/use` / `/useall` 不再提供 global shortcut

`/mode normal` / `/mode vscode` 当前只允许在 detached 或 idle attached surface 上切换。

切换时会先做 detach-like 清理：

- 释放 attachment / workspace claim / thread claim
- 清掉 `PromptOverride`
- 清掉 request gate / prepared new thread / staged image / queued draft

若当前仍有 live remote work，或 surface 正处于 `Abandoning`，`/mode` 会直接拒绝。

`PendingHeadless` 现在不再单独阻塞 `/mode`：

- 用户可以在恢复尚未真正跑起来时直接切到 `vscode`
- daemon 会先 kill 当前 headless 恢复流程，再完成 mode 切换

### 4.2 `attach(workspace|instance)`

`/list` 当前已经按 `ProductMode` 分流：

1. normal mode
   - 列出**可用工作区**
   - 选择卡按钮走 `attach_workspace -> ActionAttachWorkspace`
   - detached 与 attached 都可以通过 `/list` attach / switch workspace
   - attach / switch 成功后统一进入 `attached_unbound`
   - 不再静默自动 pin 默认 thread
   - 若当前实例仍有可见会话，会主动补一张 `/use` 选择卡
2. vscode mode
   - 继续只列出**在线 VS Code 实例**
   - 按钮走 `attach_instance -> ActionAttachInstance`
   - attach 成功后：
     1. 若 `ObservedFocusedThreadID` 当前可接管
        - 立即进入 `follow_local`
        - 并绑定到该 thread
     2. 否则
        - 进入 follow waiting
        - 明确提示用户先在 VS Code 里实际操作一次会话
        - 若当前实例仍有可见会话，会主动补一张**只包含当前 instance** 的 `/use` 选择卡

无论哪种 mode：

- 旧 `prompt_select` 兼容动作统一回 `selection_expired`
- 普通数字文本不会再被解释成实例或工作区选择

### 4.3 `use-thread(thread)`

`/threads`、`/use`、`/sessions` 当前都走同一条主入口：展示**最近会话**。

这里的会话列表当前按 `ProductMode + attach 状态` 分层：

- normal mode detached 时：
  - 保留 global merged thread shortcut
  - 也会 merge Codex sqlite 中最近 persisted 的主交互 thread metadata，降低对 `threads.refresh -> thread/list` 时机的依赖
  - sqlite 侧会过滤 subagent role、`exec` / `mcp` 等后台线程，以及内部 probe workspace
- normal mode 已 attach workspace 时：
  - `/use` / `/useall` 只展示当前 workspace 内会话
  - 不再通过 `/use` 静默跳到其他 workspace
- vscode mode detached 时：
  - `/use` / `/useall` 不再走 global merged thread shortcut
  - 必须先 `/list` 选择一个 VS Code instance
- vscode mode 已 attach instance 时：
  - `/use` / `/useall` 只展示当前 attached instance 的已知会话
  - 手动选择只做 one-shot force-pick
  - `SelectedThreadID` 会切到目标 thread
  - `RouteMode` 保持 `follow_local`，后续 observed focus 仍可覆盖
- `/useall` / `/sessionsall` 仍走同一套入口，但展示范围由当前 `ProductMode + attach` 状态决定
- sqlite 只负责补 freshness；最终 attach/reuse/create/busy 判定仍走现有 runtime resolver
- sqlite read 失败或 schema 不兼容时，会安全回退到当前 runtime/catalog-only 行为

当前会话选择同样走按钮回调：

- `use_thread` 直达 `ActionUseThread`
- 普通数字文本不会再被解释成会话选择

切换后：

- normal mode 或 detached/global `/use` 进入目标 thread 时：
  - `SelectedThreadID` 更新
  - `RouteMode = pinned`
- attached vscode force-pick 时：
  - `SelectedThreadID` 更新
  - `RouteMode` 继续保持 `follow_local`

选择目标会话时，当前实现会按 resolver 自动决定后续动作：

- 当前实例可见：直接切到目标 thread
- normal mode detached 且目标会话在其他在线实例上可见：自动接管所属 workspace，再落到目标 thread
- normal mode 已 attach workspace 时：只允许留在当前 workspace 内部解析
- vscode mode detached 时：直接拒绝，并提示用户先 `/list`
- vscode mode 已 attach instance 时：只允许当前 instance 已知 thread；跨 instance / persisted global thread 会直接拒绝，并提示先 `/list` 切实例
- 当前没有合适在线实例但会话带有可恢复 `cwd`：只有 normal mode detached/global `/use` 仍会自动复用现有恢复链路，或在后台准备恢复

如果用户点到旧卡片上的 legacy `prompt_select`，会统一收到 `selection_expired` 提示，要求重新发送 `/list`、`/use` 或 `/useall`。

### 4.4 `follow`

`/follow` 当前只在 `vscode mode` 下保留：

- 会清空显式 thread 绑定，并进入 `RouteMode = follow_local`
- 后续 prompt 跟随 instance 当前观测到的 focused thread

`normal mode` 下 `/follow` 已废弃：

- 不再进入 follow 路径
- 会返回迁移提示，要求用户改走当前 workspace 下的 `/use`、`/new`
- 如果确实需要 VS Code follow 语义，用户需要先显式 `/mode vscode`

### 4.5 无可用 thread 的等待态

当 surface 已接管实例，但当前没有拿到可发送的 thread 时：

- normal mode 会进入 `attached_unbound`
  - 系统会明确提示下一步应该走 `/use`、`/useall`、`/new` 或 `/list`
- vscode mode 会进入 follow waiting
  - 系统会明确提示下一步应该走“先在 VS Code 里实际操作一次会话”或 `/use`
  - 若当前实例仍有可见会话，会主动补发一张**当前 instance 范围**的 `/use` 选择卡
- 普通文本不会再被当成“隐式创建 thread”来直接发出

### 4.6 `/new`

`/new` 当前已经是稳定的 prepared state，而不是临时兼容入口。

规则：

- normal mode 下，只要当前已 attach 且 workspace 已知，就允许进入 `new_thread_ready`
- vscode mode 下，`/new` 直接拒绝，并提示先 `/mode normal`，或继续 follow / `/use` 当前 VS Code 会话
- 进入后会保留 instance attachment，但释放旧 thread claim
- 下一条普通文本会成为新 thread 的首条输入
- 若首条消息已进入 queued/dispatching/running，第二条文本与新图片会被挡住，直到新 thread 落地
- 若此时改走 `/use`、重复 `/new`、`/detach` 或 `/stop`，会先按当前规则处理未发送 draft

## 5. Queue、Typing 与本地优先

### 5.1 文本 queue

每个 surface 有一条独立 queue：

- `queued`
- `steering`
- `steered`
- `dispatching`
- `running`
- `completed`
- `failed`
- `discarded`

已入队项会冻结：

- `threadId`
- `cwd`
- `model/reasoning/access override`
- `routeModeAtEnqueue`

所以 thread 切换只影响**后续**消息，不会改写已入队项。

另外有一个专门的 steering 升级路径：

- queued 文本被点赞后，目标 item 会先离开普通 queue，进入 `steering`
- wrapper 对 `turn.steer` 返回 `accepted=true` 后，该 item 记为 `steered`
- 若 dispatch 失败或 wrapper reject，则恢复到原 queue 位置
- 若用户 reply 当前 processing 的 source message，且 reply 内容是当前 v1 支持的文本 / 图片，则会直接创建一个临时 steering item：
  - 立即给这条 reply 自己加 `OneSecond`
  - 发送 `turn.steer`
  - steering 成功后给这条 reply 自己补 `ThumbsUp`
  - steering 失败时恢复回普通语义：文本 / 图文 reply 回到 queue，独立图片 reply 回到 staged image

另外，autowhip 实际补发时也复用同一条 queue，但它和普通用户输入不是同一种来源：

- 普通用户输入 queue item 记录 `SourceKind=user`
- autowhip queue item 记录 `SourceKind=auto_continue`
- autowhip item 仍会保留“最终回复挂回哪条原用户消息”的 reply anchor
- 但不会把 queue / typing / reaction 再投影回原用户消息，避免把系统自动续推伪装成新的用户输入状态

### 5.1.1 manual `/compact`

`/compact` 当前是一个真正的 manual compact 入口，不会把字符串 `/compact` 当普通文本发给 Codex。

当前产品语义：

- 只对当前已绑定 thread 生效
- 若当前没有可用 thread：
  - 明确提示先 `/use` 选择会话
- 若当前已有 regular turn、排队消息、正在派发中的请求、正在进行 steer，或当前实例已有 compact：
  - 直接拒绝
  - 不排队 compact
  - 也不替换当前 turn
- compact 请求会走上游 `thread/compact/start`
- compact turn 自身不会接收 reply auto-steer 或 `/steerall`
- compact pending/running 期间，后续普通文本会先进入当前 surface queue
- 等 compact 对应的 `turn.completed` 到来后，后续排队消息才会继续自动派发
- 若 compact 在 turn 建立前就被上游拒绝或派发失败：
  - 会给出失败提示
  - 并恢复后续排队消息的正常出队

### 5.2 Typing reaction

当前规则：

- queue item 进入 `dispatching` 时，给原始用户消息加 `THINKING`
- 远端 turn 完成时，移除 `THINKING`
- 只有当前活动 queue item 有 Typing
- steering 成功后，会移除 `OneSecond`，并给该 item 的主文本和已绑定图片统一补 `ThumbsUp`
- reply 当前 processing 源消息触发 auto-steer 时，也会先给这条 reply 自己加 `OneSecond`；accepted 后移除并补 `ThumbsUp`
- 被显式丢弃的 queued/staged 输入仍补 `ThumbsDown`

例外：

- autowhip queue item 不会给原用户消息加/减 `THINKING`
- autowhip queue item 失败或完成时，也不会额外给原用户消息补 `ThumbsUp` / `ThumbsDown`
- 但 autowhip 产出的最终回复卡片，仍会 reply 到最初那条用户消息下面

### 5.3 本地优先

若本地 VS Code 先发起交互：

- `local.interaction.observed` 会把 surface 切到 `paused_for_local`
- 后续飞书文本继续入队，但不会自动发送

当本地 turn 完成后：

- 进入 `handoff_wait`
- 超时后若 queue 非空，则恢复远端发送

### 5.4 `stop`

`/stop` 会：

1. 若当前有 active turn，发送 `turn.interrupt`
2. 丢弃飞书侧尚未发送的 queue item
3. 丢弃未绑定到文本的 staged image
4. 对被丢弃项加 `THUMBSDOWN`
5. 若当前 surface 已开启 autowhip，且 `/stop` 命中了 live remote work，则本轮 turn 收尾时会 suppress 一次 autowhip，避免“用户刚停下，系统又自己续跑”

### 5.5 `autowhip`

`/autowhip` 当前是 surface 维度、daemon 内存态的开关：

- `/autowhip`：查看当前状态
- `/autowhip on`：开启
- `/autowhip off`：关闭
- 不持久化；daemon 重启后不会恢复之前的 autowhip 状态
- 旧 `/autocontinue` 与 `autocontinue_*` 仅作为兼容 alias 保留，不再是主展示入口

当前固定补发文案：

- `你看还有没有别的任务需要完成，有就继续做，没有就说"老板不要再打我了，真的没有事情干了"`

当前收工口令：

- `老板不要再打我了，真的没有事情干了`

当前有两条触发通道：

1. `turn.completed` 后，当前 surface queue 已空，且 final assistant 文本**不包含**收工口令。
2. `turn.completed` 携带 `problem.Retryable=true`，认为是 retryable upstream / API failure。

当前 backoff：

- `incomplete_stop`: `3s -> 10s -> 30s`，最多 3 次
- `retryable_failure`: `10s -> 30s -> 90s -> 300s`，最多 4 次

当前调度方式：

- `turn.completed` 只负责在 surface 上记录 pending autowhip runtime
- 真正 enqueue 发生在后续 `Tick()`
- enqueue 前会再次检查 surface 是否仍可发送：attached、非 abandoning、无 request gate、`DispatchMode=normal`、无 live remote work
- `incomplete_stop` 不会在 schedule 瞬间回显；真正开始补打时，会发一条短 `AutoWhip` 系统提示：`Codex疑似偷懒,已抽打 N次`
- 若 final assistant 文本命中收工口令，不会继续 schedule / dispatch，而是立刻发一条短 `AutoWhip` 系统提示：`Codex 已经把活干完了，老板放过他吧`
- `retryable_failure` 会在记录 backoff 时立刻发一条短 `AutoWhip` 系统提示：`上游不稳定，第 N/M 次，Xs后重试`

### 5.6 待确认请求优先级

当前 surface 上只要存在 pending approval request：

- 普通文本不会进入 queue，而是返回 notice，要求先处理卡片
- 图片也不会进入 staged 队列，避免形成“当前 turn 等确认，后续消息又排在它后面”的死锁感
- `/use`、`/useall`、`/follow`、`/new` 这类会改路由的动作也会被冻结
- 用户需要先处理 request card；request response 按钮仍可用

### 5.7 `captureFeedback`

当用户在 approval card 上点击“告诉 Codex 怎么改”后：

- surface 进入一次性反馈捕获模式，默认有效期 10 分钟
- 下一条普通文本不会按普通消息入队
- 系统会先对当前 request 发送 `decline`
- 再把这条文本作为 follow-up queue item 插入队列头部

如果在这个模式下发送图片：

- 返回提示，要求先发文字或重新处理卡片

### 5.8 飞书侧发送前覆盖

飞书侧 prompt override 当前包含：

- `model`
- `reasoningEffort`
- `accessMode`

当前规则：

- `/model`、`/reasoning`、`/access` 更新的是 surface 级 override
- 这些覆盖只影响**之后从飞书发出的消息**
- 已经入队的请求会继续使用它入队时冻结下来的配置
- 覆盖不会同步回 VS Code
- 覆盖会一直保留到：
  - 你显式 `clear`
  - `/detach`
  - `/mode` 切换
  - 系统因跨工作区切换或恢复链路而执行 detach-like 清理
- 默认执行权限仍是 `full access`
- `/access confirm` 或菜单 `access_confirm` 会把之后飞书发出的消息切到确认模式
- `/access full` 或菜单 `access_full` 会恢复为全放行
- `/access clear` 会清除 surface override，并回到默认的 `full access`

## 6. 图片语义

图片在当前实现中是**暂存**语义，不会单独触发 turn。

规则：

- 图片先进入 `staged`
- 下一条文本入队时，按接收顺序一起绑定到该 queue item
- 图片单独点赞没有产品语义，不会触发 steering 或取消
- 若图片消息被撤回，则未绑定图片标记为 `cancelled`
- 若被 `stop` 或 `detach` 丢弃，则标记为 `discarded`
- 若所属 queue item 被 queued 文本点赞升级为 steering，绑定图片会跟着主文本一起 steer，并在成功后收到 bot `ThumbsUp`

## 7. 飞书输出投影

当前投影由 [projector.go](../../internal/adapter/feishu/projector.go) 完成。

### 7.1 系统卡片

下面这些 `UIEvent` 会被投影成“系统提示”或状态卡片：

- `snapshot.updated`
- `notice`
- `selection.prompt`
- `request.prompt`
- `thread.selection.changed`

### 7.1.0 选择卡片

当前选择卡片是按钮直达流，而不是“回复数字”交互：

- `attach_workspace`
- `attach_instance`
- `use_thread`
- `kick_thread_confirm`
- `kick_thread_cancel`

旧 `resume_headless_thread` 只保留为历史兼容入口，统一回迁移提示。
旧 `/killinstance` 只保留为历史兼容入口，统一提示改用 `/detach`。
旧 `prompt_select` 只保留为兼容入口，统一回 `selection_expired`。

### 7.1.1 Approval Request 卡片

当本地 Codex 发出 approval request 时：

- bot 会发送一张单独的确认卡片
- 卡片包含当前会话标题和请求正文
- 若 native payload 带有命令文本，正文会以 fenced code block 展示
- 卡片按钮来自 `request.prompt.options`

常见按钮组合：

- `允许一次`
- `本会话允许`
- `拒绝`
- `告诉 Codex 怎么改`

点击按钮后：

- 直接通过卡片回调下发 `request.respond`
- 不依赖文本 `yes/no`
- `告诉 Codex 怎么改` 不会直接发送 native decision，而是让 surface 进入一次性反馈捕获模式
- 若请求已经在其他端处理完成，再次点击会提示已过期

### 7.1.2 Turn 失败卡片

当远端 turn 以非用户主动停止的已知错误结束时：

- 会投影一张红色错误卡片，而不是静默停住
- 当前错误卡片使用 `turn_failed` notice，对应飞书 error theme
- 卡片正文会尽量保留可调试的技术信息，而不是只给一个模糊的“系统错误”
- 若同一次 turn 的错误已经在完成路径里被折叠成统一失败提示，链路会避免再额外补一张重复告警
- 这条能力当前只覆盖“明确观测到的失败提示”，不等价于自动重启或自动 resume

### 7.2 过程消息

非 final 的 `block.committed`：

- 直接发纯文本
- 对用户在飞书发起的正常 remote turn，若某段 `agent_message` 已收到 `item.completed`，会尽早作为过程文本投影，不必强等到 `turn.completed`
- 这条“提前投影已完成 assistant item”的策略当前不扩展到 local UI turn，也不扩展到 autowhip turn
- 当前仍受 surface verbosity 约束：`/verbose quiet` 会抑制这类进行中过程文本；`normal` 与 `verbose` 都会显示它们；final reply 不受这条限制

### 7.2.1 图片与 dynamic tool 结果

当前会单独回显两类富结果，而不是只依赖 final text：

- `image_generation`
  - item 完成后会直接向当前飞书会话发送图片消息
  - 优先上传本地 `saved_path`，拿不到时回退到 base64 / data-url
  - 同一 turn 内若产生多张图，会逐张发送
- `dynamic_tool_call`
  - 会保留 tool 名称、成功状态与可读文本摘要
  - 若结果里带有可直接上传的图片内容，同样会发送飞书图片消息
  - 若结果只有远程图片 URL，当前不会额外抓取转存，而是降级成可读链接或摘要
  - 若结果主要是结构化内容，当前先给用户可读摘要，而不是暴露整份原始 payload

这些富结果和普通文本可以在同一 turn 内并存：

- 图片不会吞掉 final text
- final text 也不会把已经单独发出的图片重新包进 markdown
- 若图片上传或构造 payload 失败，链路会给出可见提示，而不是静默丢失

### 7.2.2 结构化计划更新卡

当前 `turn/plan/updated` 会投影为 append-only 的“计划更新”卡片：

- 若上游携带 `explanation`，卡片会展示说明文本
- step 列表会带状态：
  - `completed`
  - `in_progress`
  - `pending`
- 同一个 live turn 内，若新快照与上一份完全一致，不会重复发卡
- 只有 step 内容或状态发生变化时，才会追加发送新的计划更新卡

当前边界：

- 这条卡片只消费 `turn/plan/updated` 结构化快照
- `item/plan/delta` 仍按普通文本 item 链路处理，不会混作 checklist 卡
- 计划更新卡首版保持 append-only，不做 inline replace

### 7.3 最终回复

final `block.committed`：

- 发卡片
- 标题固定前缀为 `最后答复`
- 若能拿到 reply anchor 对应的原用户消息预览，则标题会变成 `最后答复：<原消息预览>`
- 若当前 turn 带有可用的飞书源消息 `SourceMessageID`，会优先 reply 到触发消息
- 若 reply 失败、目标消息已不存在或不可回复，则回退到独立发卡
- assistant 正文在 turn 完成前保持缓冲；最终回复会在完成时统一投影成 final card
- 若单张 final card 会超出 Feishu payload 限制，当前会在 projector 层先拆成“主 final card + 若干 continuation cards”，而不是继续主要依赖网关截断
- 主 final card 保留原标题、文件摘要与 turn summary footer；后续 continuation cards 只承载正文 continuation，并继续 reply 到同一个源消息
- 若该 turn 带有文件修改 summary，会把摘要直接追加在 final assistant card 底部，而不是额外再发一张独立卡片
- 文件摘要会展示本轮修改文件数、总 `+/-` 行数，以及逐文件的 `+/-` 统计
- 文件展示名优先使用“最短唯一后缀”，避免直接铺完整长路径；重命名会显示 `旧路径 → 新路径`
- 当前最多展开前 6 个文件；超出的部分只提示“另有 N 个文件未展开”
- 若本轮没有可展示的最终正文，但存在文件修改 summary，仍会补一张合成 final card，正文为 `已完成文件修改。`
- final card 底部当前会追加一条 turn summary footer，显示本轮用时；若 token usage 可用，则会显示本轮累计（优先 `turn end total - turn start total`，缺失时回退到 `last usage`）和线程累计（`thread total`），并附带 `input`、`cached input`、`cache ratio`、`output`、`reasoning output`
- 若当前 turn 同时带有可用 `last.inputTokens` 与 `modelContextWindow`，footer 还会按近似公式 `1 - input / window` 追加 `context left`（优先使用线程最新一次 usage 的 `last.inputTokens`，缺失时才回退到本轮累计 input）
- 这条 `context left` 当前是估算值，不等价于协议中的精确上下文剩余值
- 若正文里存在可识别的本地 `.md` Markdown 链接，发送前会先尝试重写成飞书云空间预览链接
- Markdown 预览重写与最终 reply/create message 发送使用独立 timeout 预算
- 预览物化失败时不会阻塞主回复；显式远端 Markdown 链接保持可点击，本地 Markdown 链接会降级成稳定文本形态（例如 `说明文档 (`./docs/guide.md`)`），避免把整段 final card 的 Markdown 解析搞坏
- 若 final reply 已经 split，后台 second-chance preview patch 当前只作用于主 final card；continuation cards 继续保持 append-only，不会被回头重发

### 7.4 代码块

若 block 是代码类：

- 卡片正文使用 fenced code block

## 8. 当前已实现但值得注意的限制

- attach/use 当前已经收敛到按钮直达交互；普通数字文本会按普通消息处理
- reaction deleted 事件未接入
- Feishu 输出不是流式更新卡片，而是 append-only 文本/卡片
- queued 点赞 steering 当前只认 `ThumbsUp`，也只认主文本消息，不支持其他 emoji 和图片独立 steering
- 当前主要按 P2P 场景测试，group chat 虽有 surface id 规则，但不是主要联调路径

## 9. 与旧设计文档的关系

如果你看到旧文档里还在讨论：

- 独立 bot 进程
- `relay.render.v1` 外部事件流
- `/v1/users/:userId/...` 控制接口

那属于旧阶段设计，不代表当前仓库的实际实现。

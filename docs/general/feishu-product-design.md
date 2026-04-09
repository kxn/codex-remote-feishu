# Feishu 产品设计

> Type: `general`
> Updated: `2026-04-09`
> Summary: 描述当前 Go 版本的 Feishu surface 行为，包括文本命令、auto-continue、图文/引用入站、旧生命周期动作判定、卡片交互、queued 点赞 steering、最终回复 reply 与状态提示。

## 1. 文档定位

这份文档描述的是**当前 Go 版本实现中的 Feishu 产品层行为**。

当前链路是：

`Feishu Gateway -> control.Action -> orchestrator -> control.UIEvent -> Feishu Projector`

它不再是独立 bot 进程，也不再依赖公开的 `relay.render.v1` 拉流接口。

## 2. Surface 模型

产品状态按 `surfaceSessionId` 建模，而不是按裸 `userId` 建模。

当前实现：

- P2P 会话：`feishu:user:<userId>`
- 群聊/其他 chat：`feishu:chat:<chatId>`

这个规则定义在 [gateway.go](../../internal/adapter/feishu/gateway.go) 的 `surfaceIDForInbound()` 中。

## 3. 当前支持的飞书入口

### 3.1 文本消息

普通文本进入：

- `surface.message.text`

文本命令当前支持：

- `/list`
- `/status`
- `/autocontinue`
- `/new`
- `/stop`
- `/threads` / `/use` / `/sessions`
- `/useall` / `/sessionsall` / `/sessions/all`
- `/follow`
- `/detach`
- `/model`
- `/reasoning` / `/effort`
- `/access` / `/approval`
- `/help`
- `menu` / `/menu`

除了纯文本外，当前还支持两类更完整的入站整理：

- `post`
  - 单条图文混合消息会把文本和图片一起整理进同一次 prompt
- reply / quote
  - 会补查被引用消息
  - 引用文本会作为额外提示文本带入
  - 引用图文混合消息时，会把其中的文本和图片一起带入

### 3.2 菜单事件

当前默认模板使用的机器人菜单 key：

- `list`
- `status`
- `stop`
- `threads` / `use` / `sessions` / `show_threads` / `show_sessions`
- `threads_all` / `useall` / `sessions_all` / `show_all_threads` / `show_all_sessions`
- `reason_low` / `reason_medium` / `reason_high` / `reason_xhigh`
- `access_full` / `approval_full`
- `access_confirm` / `approval_confirm`

推理强度菜单仍兼容历史 alias：

- `reasonlow` / `reasonmedium` / `reasonhigh` / `reasonxhigh`

默认模板当前只预置其中常用的一组菜单项；其余 alias 属于兼容输入。

### 3.3 图片消息

图片进入：

- `surface.message.image`

图片会先下载到本地临时文件，再进入 staged image 队列。

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

### 3.6 卡片回调

当前支持两类卡片按钮：

- command menu 快捷动作
- selection prompt 选择
- approval request 确认

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

## 4. Attachment 与 thread 路由

### 4.1 `attach(workspace|instance)`

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
     1. 若 instance 有 `ObservedFocusedThreadID`
        - 立即 pin 到该 thread
     2. 否则若 instance 有 `ActiveThreadID`
        - pin 到该 thread
     3. 否则
        - 进入 `attached_unbound`
        - 若当前仍有可见会话，会主动补一张 `/use` 选择卡

无论哪种 mode：

- 旧 `prompt_select` 兼容动作统一回 `selection_expired`
- 普通数字文本不会再被解释成实例或工作区选择

### 4.2 `use-thread(thread)`

`/threads`、`/use`、`/sessions` 当前都走同一条主入口：展示**最近会话**。

这里的会话列表不是“当前 instance 内 thread 列表”这么窄，而是 merged thread view：

- 已 attach 时，会包含当前实例的可见会话
- detached 时，也会 merge Codex sqlite 中最近 persisted 的非 archived thread metadata，降低对 `threads.refresh -> thread/list` 时机的依赖
- `/useall` / `/sessionsall` 仍走同一套 merged source，只是展示范围更大
- sqlite 只负责补 freshness；最终 attach/reuse/create/busy 判定仍走现有 runtime resolver
- sqlite read 失败或 schema 不兼容时，会安全回退到当前 runtime/catalog-only 行为

当前会话选择同样走按钮回调：

- `use_thread` 直达 `ActionUseThread`
- 普通数字文本不会再被解释成会话选择

切换后：

- `SelectedThreadID` 更新
- `RouteMode = pinned`

选择目标会话时，当前实现会按 resolver 自动决定后续动作：

- 当前实例可见：直接切到目标 thread
- 目标会话在其他在线实例上可见：自动接管目标实例
- 当前没有合适在线实例但会话带有可恢复 `cwd`：自动复用现有恢复链路，或在后台准备恢复

如果用户点到旧卡片上的 legacy `prompt_select`，会统一收到 `selection_expired` 提示，要求重新发送 `/list`、`/use` 或 `/useall`。

### 4.3 `follow`

`/follow` 会清空显式 thread 绑定，并进入：

- `RouteMode = follow_local`

后续 prompt 会跟随 instance 当前观测到的 focused thread。

### 4.4 `attached_unbound`

当 surface 已接管实例，但当前没有拿到可发送的 thread 时，会进入 `attached_unbound`：

- 系统会明确提示下一步应该走 `/use`、`/useall`、`/follow` 或 `/detach`
- 若当前实例仍有可见会话，会主动补发 `/use` 选择卡
- 普通文本不会再被当成“隐式创建 thread”来直接发出

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
- `model/reasoning override`
- `routeModeAtEnqueue`

所以 thread 切换只影响**后续**消息，不会改写已入队项。

另外有一个专门的 steering 升级路径：

- queued 文本被点赞后，目标 item 会先离开普通 queue，进入 `steering`
- wrapper 对 `turn.steer` 返回 `accepted=true` 后，该 item 记为 `steered`
- 若 dispatch 失败或 wrapper reject，则恢复到原 queue 位置

另外，auto-continue 实际补发时也复用同一条 queue，但它和普通用户输入不是同一种来源：

- 普通用户输入 queue item 记录 `SourceKind=user`
- auto-continue queue item 记录 `SourceKind=auto_continue`
- auto-continue item 仍会保留“最终回复挂回哪条原用户消息”的 reply anchor
- 但不会把 queue / typing / reaction 再投影回原用户消息，避免把系统自动续推伪装成新的用户输入状态

### 5.2 Typing reaction

当前规则：

- queue item 进入 `dispatching` 时，给原始用户消息加 `THINKING`
- 远端 turn 完成时，移除 `THINKING`
- 只有当前活动 queue item 有 Typing
- steering 成功后，会移除 `OneSecond`，并给该 item 的主文本和已绑定图片统一补 `ThumbsUp`
- 被显式丢弃的 queued/staged 输入仍补 `ThumbsDown`

例外：

- auto-continue queue item 不会给原用户消息加/减 `THINKING`
- auto-continue queue item 失败或完成时，也不会额外给原用户消息补 `ThumbsUp` / `ThumbsDown`
- 但 auto-continue 产出的最终回复卡片，仍会 reply 到最初那条用户消息下面

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
5. 若当前 surface 已开启 auto-continue，且 `/stop` 命中了 live remote work，则本轮 turn 收尾时会 suppress 一次 auto-continue，避免“用户刚停下，系统又自己续跑”

### 5.5 `autocontinue`

`/autocontinue` 当前是 surface 维度、daemon 内存态的开关：

- `/autocontinue`：查看当前状态
- `/autocontinue on`：开启
- `/autocontinue off`：关闭
- 不持久化；daemon 重启后恢复默认关闭

当前固定补发文案：

- `任务都完成了吗？如果没有就继续干，都完成了就可以停下来`

当前有两条触发通道：

1. `turn.completed` 后，当前 surface queue 已空，且 final assistant 文本命中“疑似未完成就停下”的 deterministic heuristics。
2. `turn.completed` 携带 `problem.Retryable=true`，认为是 retryable upstream / API failure。

当前 backoff：

- `incomplete_stop`: `3s -> 10s -> 30s`，最多 3 次
- `retryable_failure`: `10s -> 30s -> 90s -> 300s`，最多 4 次

当前调度方式：

- `turn.completed` 只负责在 surface 上记录 pending auto-continue runtime
- 真正 enqueue 发生在后续 `Tick()`
- enqueue 前会再次检查 surface 是否仍可发送：attached、非 abandoning、无 request gate、`DispatchMode=normal`、无 live remote work

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

### 5.8 飞书侧执行权限覆盖

飞书侧 prompt override 包含 `accessMode`。

当前规则：

- 默认有效执行权限是 `full access`
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

### 7.2 过程消息

非 final 的 `block.committed`：

- 直接发纯文本

### 7.3 最终回复

final `block.committed`：

- 发卡片
- 标题固定前缀为 `最后答复`
- 若能拿到 reply anchor 对应的原用户消息预览，则标题会变成 `最后答复：<原消息预览>`
- 若当前 turn 带有可用的飞书源消息 `SourceMessageID`，会优先 reply 到触发消息
- 若 reply 失败、目标消息已不存在或不可回复，则回退到独立发卡
- 若正文里存在可识别的本地 `.md` Markdown 链接，发送前会先尝试重写成飞书云空间预览链接
- Markdown 预览重写与最终 reply/create message 发送使用独立 timeout 预算
- 预览物化失败时不会阻塞主回复，正文保持原样

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

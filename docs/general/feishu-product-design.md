# Feishu 产品设计

> Type: `general`
> Updated: `2026-04-06`
> Summary: 补充消息撤回事件与当前飞书产品行为，保持安装清单与代码语义一致。

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
- `/stop`
- `/threads` / `/use` / `/sessions`
- `/useall` / `/sessionsall`
- `/follow`
- `/detach`
- `/model`
- `/reasoning` / `/effort`
- `/access` / `/approval`

### 3.2 菜单事件

当前代码支持的机器人菜单 key：

- `list`
- `status`
- `stop`
- `threads` / `use` / `sessions` / `show_threads` / `show_sessions`
- `threads_all` / `useall` / `sessions_all` / `show_all_threads` / `show_all_sessions`
- `reasonlow` / `reasonmedium` / `reasonhigh` / `reasonxhigh`
- `access_full` / `approval_full`
- `access_confirm` / `approval_confirm`

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

- selection prompt 选择
- approval request 确认

approval request 卡片当前按动态 option 渲染，常见选项包括：

- `accept`
- `acceptForSession`
- `decline`
- `captureFeedback`

不再支持靠文本回复 `yes/no` 处理确认。

## 4. Attachment 与 thread 路由

### 4.1 `attach(instance)`

`/list` 或菜单列出在线实例后，用户通过数字回复 attach。

attach 成功后：

1. 若 instance 有 `ObservedFocusedThreadID`
   - 立即 pin 到该 thread
2. 否则若 instance 有 `ActiveThreadID`
   - pin 到该 thread
3. 否则
   - 进入 `unbound`

### 4.2 `use-thread(thread)`

`/threads` 或 `/use` 列出当前 instance 的可见 thread，用户通过数字回复切换。

切换后：

- `SelectedThreadID` 更新
- `RouteMode = pinned`

实例选择和 thread 选择当前都会生成一个数字选择 prompt。

- 默认有效期 10 分钟
- 过期后继续回复数字，会提示重新发送 `/list` 或 `/use`

### 4.3 `follow`

`/follow` 会清空显式 thread 绑定，并进入：

- `RouteMode = follow_local`

后续 prompt 会跟随 instance 当前观测到的 focused thread。

### 4.4 `unbound`

当没有可用 thread 且未显式绑定时：

- 下一条飞书文本会以 `createThreadIfMissing=true` 形式发出
- 由 wrapper 决定是否先做 `thread/start`

## 5. Queue、Typing 与本地优先

### 5.1 文本 queue

每个 surface 有一条独立 queue：

- `queued`
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

### 5.2 Typing reaction

当前规则：

- queue item 进入 `dispatching` 时，给原始用户消息加 `THINKING`
- 远端 turn 完成时，移除 `THINKING`
- 只有当前活动 queue item 有 Typing

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

### 5.5 待确认请求优先级

当前 surface 上只要存在 pending approval request：

- 普通文本不会进入 queue，而是返回 notice，要求先处理卡片
- 图片也不会进入 staged 队列，避免形成“当前 turn 等确认，后续消息又排在它后面”的死锁感
- slash command 和 selection prompt 数字回复仍然照常处理

### 5.6 `captureFeedback`

当用户在 approval card 上点击“告诉 Codex 怎么改”后：

- surface 进入一次性反馈捕获模式，默认有效期 10 分钟
- 下一条普通文本不会按普通消息入队
- 系统会先对当前 request 发送 `decline`
- 再把这条文本作为 follow-up queue item 插入队列头部

如果在这个模式下发送图片：

- 返回提示，要求先发文字或重新处理卡片

### 5.7 飞书侧执行权限覆盖

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
- 若图片在绑定前被 reaction 取消，则标记为 `cancelled`
- 若被 `stop` 或 `detach` 丢弃，则标记为 `discarded`

## 7. 飞书输出投影

当前投影由 [projector.go](../../internal/adapter/feishu/projector.go) 完成。

### 7.1 系统卡片

下面这些 `UIEvent` 会被投影成“系统提示”或状态卡片：

- `snapshot.updated`
- `notice`
- `selection.prompt`
- `request.prompt`
- `thread.selection.changed`

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
- 标题为 `最终回复 · <thread title>`
- 若正文里存在可识别的本地 `.md` Markdown 链接，发送前会先尝试重写成飞书云空间预览链接
- 预览物化失败时不会阻塞主回复，正文保持原样

### 7.4 代码块

若 block 是代码类：

- 卡片正文使用 fenced code block

## 8. 当前已实现但值得注意的限制

- attach/use 目前仍采用“列出选项 + 数字回复”的交互，不是按钮回调流
- reaction deleted 事件未接入
- Feishu 输出不是流式更新卡片，而是 append-only 文本/卡片
- 当前主要按 P2P 场景测试，group chat 虽有 surface id 规则，但不是主要联调路径

## 9. 与旧设计文档的关系

如果你看到旧文档里还在讨论：

- 独立 bot 进程
- `relay.render.v1` 外部事件流
- `/v1/users/:userId/...` 控制接口

那属于旧阶段设计，不代表当前仓库的实际实现。

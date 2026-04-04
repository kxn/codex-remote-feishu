# Feishu 产品层设计

## 1. 文档定位

这份文档定义的是 `server <-> bot` 这一层的**产品语义**，目标是把下面这些问题一次性定死：

- 飞书里的一个“会话面板”到底怎么建模
- `list / attach / use-thread / stop` 的实际交互怎么走
- 文本、图片、排队、取消、reaction 的状态机怎么走
- 哪些状态归 server，哪些动作归 bot，哪些细节仍归 wrapper

这份文档和另外两份文档的关系如下：

- [relay-protocol-spec.md](./relay-protocol-spec.md)
  - 负责 `wrapper <-> server` 的 canonical protocol
- [app-server-redesign.md](./app-server-redesign.md)
  - 负责整体分层和重构方向
- 本文
  - 负责 Feishu 侧产品行为和 server 的产品状态机

## 2. 先解决的产品问题

这轮需求本质上要解决六个老问题：

1. 之前 attachment 是“按 userId 记一个当前 session”，这会导致多聊天窗口串状态。
2. 之前 queue 在 wrapper 里，只能排纯文本，无法表达图片、取消和 stop discard。
3. 之前 attach 以后到底跟哪个 thread 说话不稳定，会受本地 UI 变化影响。
4. 之前数字回复没有上下文，`1` 到底是 attach、切 thread 还是普通 prompt 不明确。
5. 之前图片没有正式的产品层语义，只能靠未来实现时再猜。
6. 之前 bot 只能根据输出流猜什么时候给 Typing、什么时候结束，无法精确对应到用户发出的那条消息。

## 3. 冻结决策

### 3.1 用户层主键改为 `surfaceSessionId`

产品状态不再按裸 `userId` 建模，而是按一个具体的飞书会话面板建模。

第一版定义：

```ts
type SurfaceSessionId = `feishu:chat:${string}`;
```

也就是：

- 同一个用户在不同 chat 里，状态彼此隔离
- attachment、queue、图片暂存、selection prompt 都挂在 surface 上
- `userId` 只用于鉴权和“谁触发了动作”，不再是产品状态主键

这一步是必须的，否则会出现：

- 一个聊天窗口里 `/list`，另一个聊天窗口里回复 `1`，结果 attach 错实例
- 图片在 A chat 暂存，结果在 B chat 的下一条文字里被带出去

### 3.2 `attach(instance)` 和 `use-thread(thread)` 仍然分离

- `attach(instance)` = 接管某个 Codex wrapper instance
- `use-thread(thread)` = 给当前 surface 选定输入目标 thread

attach 的对象始终是 instance，不是 thread。

### 3.3 attach 成功时，默认 pin 到“attach 当下的 thread”

这是对旧草案的一个明确修正。

attach 成功后，server 立即按下面规则初始化当前 surface 的路由状态：

1. 如果 instance 当前有 `observedFocusedThreadId`
   - `selectedThreadId = observedFocusedThreadId`
   - `routeMode = "pinned"`
2. 否则
   - `selectedThreadId = null`
   - `routeMode = "unbound"`

直接结论：

- 飞书端默认用的是“attach 当下”的 thread
- 之后本地 VS Code 再切 thread，不会把飞书端输入目标偷偷改掉
- 如果用户希望重新跟随本地 UI，需要显式执行“跟随当前 VS Code”

这样做是为了消除“我 attach 完还没说话，本地 UI 又切了一下，结果飞书 prompt 跑到别的 thread”这种不稳定行为。

### 3.4 输出转发按 instance，输入路由按 thread

attach 以后：

- **输出面**：当前 attached instance 的可见文本输出都会转发到飞书
- **输入面**：飞书发出的 prompt 只发往当前 surface 选中的 thread

也就是说：

- attach 决定“看哪个 instance”
- use-thread 决定“往哪个 thread 发”

这是故意的，因为用户已经明确希望 attach 的概念是“接管整个 instance”，而不是只接一个 thread。

### 3.5 queue 归 server，且队列项在入队时冻结目标 thread

文本消息一旦入队，就立即冻结它的目标路由：

```ts
type FrozenRoute = {
  routeModeAtEnqueue: "pinned" | "follow_local" | "new_thread";
  threadId: string | null;
  cwd: string | null;
};
```

规则：

- 已入队文本，不受后续 `use-thread` 影响
- 已入队文本，不受本地 VS Code 再次切 thread 影响
- 这保证“用户发消息那一刻看到的路由语义”不会被之后的 UI 变化改写

### 3.6 图片先暂存，不直接形成 turn

飞书里的图片消息只进入 surface draft，不直接提交给 Codex。

第一版冻结为：

- 图片可连续发送多张
- 图片会在“后续第一条文本消息入队时”一起被打包进去
- 仅图片、没有后续文本时，不自动提交

这和 Codex 原生能力并不冲突。Codex 本身支持 image-only submit，但产品上先不开放自动发送，避免用户只发了一张图就立刻起一个 turn。

### 3.7 图片在“绑定到文本之前”不绑定 thread

图片暂存是 surface draft 级状态，不是 thread 级状态。

这意味着：

- 用户先发图片，再切 thread，再发文字
- 这些图片会跟随后续那条文字，一起发到切换后的 thread

这个语义是刻意选择的。原因是：

- 飞书里图片和文字是分开发送的
- 在没有后续文字前，图片还不构成一个完整 user turn

一旦图片被某条文字“吃进去”，它们就不再是 staged image，而是该队列项的一部分，后续随该队列项一起运行、取消或丢弃。

### 3.8 stop 会中断当前 turn，并清空“尚未送到 Codex 的所有内容”

stop 的产品语义冻结为：

1. 若当前 surface attached instance 有 active turn
   - 发 `turn.interrupt`
2. 清空当前 surface 的：
   - queued text prompt
   - staged images
   - 过期但尚未处理的 selection prompt
3. 对所有因此被丢弃的用户消息，打 bot 侧 `THUMBSDOWN` reaction

注意：

- 正在运行的那条文本不会被标记 `THUMBSDOWN`
- 它只是被中断，最终会进入 `interrupted`

### 3.9 request 响应走卡片，不和普通文本复用

为避免“现在这条文本到底是回答 approval，还是普通 prompt”这种歧义：

- approval / request-user-input 统一走卡片按钮或表单
- 普通文本仍视为新的 prompt，按 queue 规则排队

这样可以把 request 响应和普通 prompt 明确分流。

### 3.10 检测到本地 VS Code 交互时，不 auto detach，而是进入 local-priority

这里必须明确承认：attach 以后，instance 仍然可能同时被本地 VS Code 使用。

第一版冻结行为：

- 不 auto detach
- 但一旦检测到本地交互请求
  - 包括 `turn/start`
  - 也包括 `turn/steer`
  - 当前 surface 进入 `local_priority`
  - 所有后续飞书文本只入队，不自动发送
  - bot 发 notice，提示“检测到本地 VS Code 正在使用，飞书消息将继续排队”

恢复规则：

- 只有当 instance 空闲，且一段时间内没有再出现新的本地交互，才自动恢复远端 autosend
- 同时允许后续增加显式“恢复远端队列”按钮

补充约束：

- 只有 `local.interaction.observed(interactionClass=local_ui)` 才会触发 local-priority
- 对 `interactionClass=internal_helper` 的本地 helper/internal 流量：
  - server 可以记录
  - 但不得暂停飞书队列
  - 也不得向用户发送“检测到本地 VS Code 正在使用”的提示

这样做的原因是：

- wrapper 看得到本地 `turn/start` / `turn/steer`
- 但看不到本地 draft / composer / queued draft 的完整状态
- 所以远端不能假设“当前空闲就一定轮到飞书”

### 3.11 本地客户端自己的 queue 视为 opaque，不尝试清理

这是一个关键约束。

server 能管理的只有：

- 飞书侧 staged image
- 飞书侧 queued prompt

server 不能管理的包括：

- VS Code / Codex 客户端本地 draft
- VS Code / Codex 客户端本地 queued message
- 本地中断后客户端如何恢复 composer

因此：

- stop 只会清飞书侧未送出的内容
- 不承诺清掉本地客户端自己的 queue
- 不承诺本地客户端中断后不会恢复自己的 draft

## 4. 需要补上的 use case 和对应处理

下面这些是需求里没完全写死，但实现时必须补上的边界。

### 4.1 数字回复必须带上下文

`1` 不能全局解释。

server 必须维护当前 surface 的 `SelectionPromptRecord`：

```ts
type SelectionPromptRecord = {
  promptId: string;
  kind: "attach_instance" | "use_thread";
  createdAt: string;
  expiresAt: string;
  options: Array<{
    index: number;
    optionId: string;
    label: string;
    subtitle: string | null;
    isCurrent: boolean;
    disabled: boolean;
  }>;
};
```

规则：

- 只有在 active selection prompt 存在时，纯数字文本才解释为“选项”
- `attach_instance` 和 `use_thread` 不能共享同一个 prompt
- prompt 默认 10 分钟过期
- 新的 list / thread-list 会覆盖旧 prompt

### 4.2 attach 切换实例时必须清草稿

如果当前 surface 已 attach 到 A，又切到 B：

- staged images 必须丢弃
- queued text 必须丢弃
- 对被丢弃的用户消息打 `THUMBSDOWN`

否则会出现：

- A 的图带到 B
- A 的排队 prompt 跑到 B

### 4.3 detach 也要清草稿，但不自动 stop instance

detach 的产品语义：

- 只解除当前 surface 与 instance 的绑定
- 不会自动 stop 远端正在跑的 turn
- 但会清当前 surface 尚未送出的 queue / staged image / selection prompt

### 4.4 本地 thread 切换只影响两件事

本地 VS Code 切 thread 时，server 只更新：

- `instance.observedFocusedThreadId`
- thread 列表中的 `isObservedFocused`

它**不会**：

- 改掉已 pin 的 `selectedThreadId`
- 改掉已入队文本的 frozen route

### 4.5 切 thread 本质上也切 cwd

对 Codex 来说，thread 隐含了它自己的工作目录上下文。

所以：

- `use-thread(thread-X)` 不只是“切对话”
- 也意味着后续 prompt 会继承该 thread 的 `cwd`

这正是之前 `/data/dl` 和 `/data/dl/droid` 混淆问题的根源之一。

### 4.6 本地 VS Code 和飞书同时说话时，远端不能抢下一轮

这是不做 auto detach 以后最重要的边界。

如果当前 turn 结束，飞书侧 queue 非空，同时本地客户端也已经排了下一条消息，那么 server 若立即 autosend 飞书队列，就很容易和本地客户端抢 turn。

### 处理

- turn 完成后，远端 autosend 必须先进入短暂 `handoff_wait`
- 若在这个窗口内观察到本地交互
  - 包括新的 `turn/start`
  - 也包括对当前 turn 的 `turn/steer`
  - 本地优先
  - 飞书继续排队
- 若窗口结束仍没有新的本地交互
  - 才允许发送飞书队头

### 4.7 stop 无法清本地客户端自己的 queued draft

这不是实现缺口，而是协议边界。

### 处理

- 文档中明确 stop 只清飞书侧 queue
- 本地客户端若自行恢复 draft / queued message，视为本地行为
- 远端只需要避免在此期间继续抢 turn

## 5. 状态模型

## 5.1 InstanceRecord

在产品层，instance 必须额外保留稳定的工作目录身份：

```ts
type InstanceRecord = {
  instanceId: string;
  workspaceRoot: string | null;
  workspaceKey: string;
  displayName: string;
  shortName: string;
  online: boolean;
  observedFocusedThreadId: string | null;
  activeThreadId: string | null;
  activeTurnId: string | null;
};
```

约束：

- `workspaceKey` = 规范化后的工作目录路径
- 列表展示必须至少包含 `workspaceKey`
- `shortName` 可取 basename，但它不是唯一键

如果极少数情况下 `workspaceKey` 仍冲突：

- UI 上附加 `instanceId` 短后缀
- 但协议层仍以 `instanceId` 为真正标识

## 5.2 SurfaceConsoleRecord

```ts
type SurfaceConsoleRecord = {
  surfaceSessionId: string;
  platform: "feishu";
  chatId: string;
  actorUserId: string;
  attachedInstanceId: string | null;
  selectedThreadId: string | null;
  routeMode: "pinned" | "follow_local" | "unbound";
  dispatchMode: "normal" | "handoff_wait" | "paused_for_local";
  activeTurnOrigin: "local_ui" | "remote_surface" | "unknown" | null;
  localPriorityUntil: string | null;
  activeQueueItemId: string | null;
  queuedQueueItemIds: string[];
  stagedImageIds: string[];
  selectionPrompt: SelectionPromptRecord | null;
  openRequestId: string | number | null;
};
```

说明：

- 一个 surface 只 attach 一个 instance
- 一个 surface 同时只 dispatch 一个 queue item
- 但可以保留多个 queued item 和 staged image
- `dispatchMode = paused_for_local` 时，飞书侧只排队不发送
- `paused_for_local` 的进入条件不是只看 `turn.started`
  - 只要观测到本地 `turn/start` 或 `turn/steer`，就应先暂停远端 autosend

## 5.3 StagedImageRecord

```ts
type StagedImageRecord = {
  imageId: string;
  surfaceSessionId: string;
  sourceMessageId: string;
  localPath: string;
  mimeType: string | null;
  sizeBytes: number | null;
  state: "staged" | "cancelled" | "bound" | "discarded";
  createdAt: string;
};
```

## 5.4 PendingQueueItemRecord

```ts
type PendingQueueItemRecord = {
  queueItemId: string;
  surfaceSessionId: string;
  sourceMessageId: string;
  text: string;
  imageSourceMessageIds: string[];
  localImagePaths: string[];
  frozenRoute: {
    routeModeAtEnqueue: "pinned" | "follow_local" | "new_thread";
    threadId: string | null;
    cwd: string | null;
  };
  state:
    | "queued"
    | "dispatching"
    | "running"
    | "completed"
    | "failed"
    | "cancelled"
    | "discarded";
  commandId: string | null;
  turnId: string | null;
  errorMessage: string | null;
};
```

## 5.5 MessageBindingIndex

为了支持 reaction 取消和 Typing / `THUMBSDOWN` 回写，server 必须维护：

```ts
type MessageBindingIndex = {
  sourceMessageId: string;
  surfaceSessionId: string;
  kind: "staged_image" | "queue_item";
  entityId: string;
  cancelable: boolean;
};
```

## 6. bot -> server 动作模型

bot 不再自己维护 attachment / queue 语义，只负责把飞书事件翻成 surface action。

```ts
type SurfaceAction =
  | {
      kind: "surface.menu.list_instances";
      surfaceSessionId: string;
      actorUserId: string;
    }
  | {
      kind: "surface.menu.stop";
      surfaceSessionId: string;
      actorUserId: string;
    }
  | {
      kind: "surface.button.show_threads";
      surfaceSessionId: string;
      actorUserId: string;
    }
  | {
      kind: "surface.button.attach_instance";
      surfaceSessionId: string;
      actorUserId: string;
      instanceId: string;
    }
  | {
      kind: "surface.button.follow_local";
      surfaceSessionId: string;
      actorUserId: string;
    }
  | {
      kind: "surface.message.text";
      surfaceSessionId: string;
      actorUserId: string;
      sourceMessageId: string;
      text: string;
    }
  | {
      kind: "surface.message.image";
      surfaceSessionId: string;
      actorUserId: string;
      sourceMessageId: string;
      localPath: string;
      mimeType: string | null;
      sizeBytes: number | null;
    }
  | {
      kind: "surface.message.reaction.created";
      surfaceSessionId: string;
      actorUserId: string;
      sourceMessageId: string;
      emojiType: string;
    }
  | {
      kind: "surface.request.respond";
      surfaceSessionId: string;
      actorUserId: string;
      requestId: string | number;
      response: Record<string, unknown>;
    };
```

## 6.1 文本消息的解释顺序

server 收到 `surface.message.text` 时，必须按下面顺序解释：

1. 若 text 是 slash command
   - 走控制命令
2. 否则若存在 active `selectionPrompt` 且 text 是有效数字
   - 作为 selection reply
3. 否则若当前无 attachment
   - 返回“请先 list / attach”
4. 否则
   - 视为普通 prompt

这个顺序必须固定，否则数字回复和普通 prompt 会互相抢。

## 6.2 图片消息的处理

bot 收到飞书图片消息时：

1. 使用飞书 `message resource` API 下载到本地临时文件
2. 发送 `surface.message.image`
3. server 建立 `StagedImageRecord`
4. bot 不立刻给 Typing

图片本身不形成 queue item。

## 6.3 reaction.created 的处理

只有下面两类消息支持“用户 reaction = 取消”：

- `staged_image.state = staged`
- `queue_item.state = queued`

其他状态一律不取消：

- `dispatching`
- `running`
- `completed`
- `failed`
- `discarded`

也就是说，reaction 只能取消“还没真正送给 Codex”的内容。

## 7. server -> bot 异步事件模型

bot 持续消费 `relay.render.v1` 的 surface 事件流。对本轮需求，至少需要下面这些事件。

```ts
type SurfaceRenderEvent =
  | PendingInputStateEvent
  | TurnStateEvent
  | BlockCommittedEvent
  | RequestOpenedEvent
  | RequestClosedEvent
  | NoticeEvent
  | ErrorEvent
  | ThreadSelectionChangedEvent;
```

## 7.1 `pending.input.state`

这是 Typing、取消和 discard 的核心事件。

```ts
type PendingInputStateEvent = {
  protocol: "relay.render.v1";
  kind: "pending.input.state";
  eventId: number;
  surfaceSessionId: string;
  instanceId: string | null;
  threadId: string | null;
  turnId: string | null;
  input: {
    sourceMessageId: string;
    inputKind: "text" | "image";
    state:
      | "staged"
      | "queued"
      | "dispatching"
      | "running"
      | "completed"
      | "failed"
      | "cancelled"
      | "discarded";
    queuePosition: number | null;
    reactionHint: "typing_on" | "typing_off" | "thumbsdown" | "none";
    message: string | null;
  };
};
```

bot 的处理规则：

- `typing_on` -> 给对应文本消息加 `Typing`
- `typing_off` -> 删除该 `Typing`
- `thumbsdown` -> 给对应消息加 `THUMBSDOWN`

## 7.2 `thread.selection.changed`

```ts
type ThreadSelectionChangedEvent = {
  protocol: "relay.render.v1";
  kind: "thread.selection.changed";
  eventId: number;
  surfaceSessionId: string;
  instanceId: string | null;
  selectedThreadId: string | null;
  observedFocusedThreadId: string | null;
  routeMode: "pinned" | "follow_local" | "unbound";
  message: string | null;
};
```

用途：

- 切 thread 成功提示
- 跟随当前 VS Code 成功提示
- 本地 observed focus 更新提示

## 7.3 `notice`

这里要额外约定两类 notice，后续 bot 需要稳定处理：

- `local_activity_detected`
  - 检测到本地 VS Code turn，远端进入排队模式
- `remote_queue_resumed`
  - 本地优先窗口结束，远端恢复 autosend

## 7.3 `block.committed`

沿用 [relay-protocol-spec.md](./relay-protocol-spec.md) 中已经冻结的 append-only block 模型，但在 bot 展示层增加两条规则：

1. 每个 block 必须带 instance 颜色键
2. 每个 block 必须可拿到 thread 标题文本

推荐标题格式：

- 默认优先使用 `实例短名 · thread 标题`
- 示例：`droid · 修复登录流程`

标题生成规则冻结为：

1. 若 thread 有稳定 `name`
   - `title = "{instance.shortName} · {thread.name}"`
2. 否则若 thread 有可读 `preview`
   - 取 preview 首段截断后作为右半部分
3. 否则
   - `title = "{instance.shortName} · thread-{threadId短后缀}"`

这样即便同一个 instance 有多个 thread 输出，也不至于完全混淆。

## 8. 文本展示规则

用户已经明确要求：

- 飞书端只展示文本型信息
- 当前基于 item 边界和结构型切分的效果基本可接受

因此第一版展示策略冻结为：

### 8.1 可见内容

- `assistant_markdown`
- `assistant_code`
- `status`
- `error`
- `approval` / `request-user-input` 卡片

### 8.2 默认隐藏内容

- 原始 tool call JSON
- reasoning 明细
- 原始 command output 流
- 原始 file change 流

如果确实需要展示工具阶段，只允许通过 server renderer 折叠成简短 `status` 文本，而不是把原生输出直接扔给 bot。

### 8.3 分块原则

- 一个 committed render block = 一条飞书消息
- 不把整个 turn 合成一条
- 不跨 item 合并
- 不发空白消息
- 若单个 block 超出飞书长度限制，只在 block 内做 continuation split

## 9. 关键流程

## 9.1 list -> attach

```text
用户点击机器人菜单 list
bot    -> server : surface.menu.list_instances
server -> bot    : SelectionPrompt(kind=attach_instance, options=online instances)
bot    -> 飞书   : 列表卡片，显示序号 + basename + 完整路径
用户    -> bot    : 回复 "2" 或点击“接管”
bot    -> server : surface.message.text("2") 或 surface.button.attach_instance
server -> state  : 解析 selection prompt，attach 到目标 instance
server -> state  : 清理旧 surface 的 staged/queued 内容
server -> bot    : thread.selection.changed / notice
bot    -> 飞书   : 显示“已接管 xxx”
```

## 9.2 attach 后切 thread

```text
用户点击“切换线程”
bot    -> server : surface.button.show_threads
server -> bot    : SelectionPrompt(kind=use_thread, options=known threads)
用户    -> bot    : 回复 "3"
bot    -> server : surface.message.text("3")
server -> state  : selectedThreadId = thread-3, routeMode = pinned
server -> bot    : thread.selection.changed
bot    -> 飞书   : 提示“后续消息将发送到 thread-3”
```

补充：

- 线程列表默认只展示当前 attached instance 的非 archived thread
- 排序按 `updatedAt desc`
- 卡片里还应提供一个“跟随当前 VS Code”按钮，效果等同 `selectedThreadId = null, routeMode = follow_local`

## 9.3 文本 prompt，当前空闲

```text
用户    -> bot    : 发送文本
bot    -> server : surface.message.text
server -> state  : 消费 staged image，生成 queue item
server -> state  : 当前 idle -> 立即 dispatch
server -> wrapper: agent.command(prompt.send inputs=[images..., text])
wrapper -> server: agent.command.ack(accepted)
server -> bot    : pending.input.state(dispatching, typing_on)
wrapper -> server: turn.started
server -> bot    : pending.input.state(running)
... assistant block.committed ...
wrapper -> server: turn.completed
server -> bot    : pending.input.state(completed, typing_off)
server -> bot    : turn.state(completed)
```

## 9.4 文本 prompt，当前正在执行

```text
用户    -> bot    : 发送文本 A
bot    -> server : surface.message.text
server -> state  : queue item A -> queued
server -> bot    : pending.input.state(queued, queuePosition=1)
...
当前 turn 结束
server -> state  : 取队头 A，进入 dispatching
server -> bot    : pending.input.state(dispatching, typing_on)
```

注意：

- A 在 `queued` 阶段没有 Typing
- 只有真正开始 dispatch 才会有 Typing

## 9.5 本地 VS Code 开始使用当前 attached instance

```text
本地 VS Code -> wrapper : turn/start 或 turn/steer
wrapper       -> server  : local.interaction.observed(action=...)
server        -> state   : dispatchMode = paused_for_local
server        -> bot     : notice(local_activity_detected)
飞书用户        -> bot     : 再发文本
bot           -> server  : surface.message.text
server        -> state   : 只入队，不发送
```

补充：

- 本地 turn 的输出仍然正常转发到飞书
- 但飞书队列不会插队发到 agent
- 如果这次本地交互实际触发了新 turn，后续仍会看到 `turn.started(initiator=local_ui)`
- 如果本地使用的是 `turn/steer`，则可能不会出现新的 `turn.started`

## 9.6 本地 turn 完成后的恢复

```text
wrapper -> server : turn.completed(initiator=local_ui)
server  -> state  : 进入 handoff_wait / local priority lease
如果窗口内又看到本地 local.interaction.observed
  -> 继续 paused_for_local
如果窗口结束仍未看到新的本地交互
  -> dispatchMode = normal
  -> 发送飞书队头
  -> bot 收到 notice(remote_queue_resumed)
```

建议默认值：

- `TURN_HANDOFF_WAIT_MS = 800`
- `LOCAL_PRIORITY_HOLD_MS = 10000`

这两个值后续可以做配置，但第一版逻辑必须先按这个思路实现。

## 9.7 图片 -> 文字

```text
用户    -> bot    : 发图片 1
bot    -> 飞书API : 下载图片到本地临时路径
bot    -> server : surface.message.image(localPath=...)
server -> state  : stagedImage #1

用户    -> bot    : 发图片 2
... 同上 ...

用户    -> bot    : 发文本“帮我分析这两张图”
bot    -> server : surface.message.text
server -> state  : stagedImage #1/#2 绑定到这条 queue item
server -> wrapper: prompt.send inputs=[local_image, local_image, text]
```

## 9.8 reaction 取消

```text
用户    -> bot    : 对尚未发送的图片消息加任意 reaction
bot    -> server : surface.message.reaction.created
server -> state  : staged image -> cancelled
server -> bot    : pending.input.state(cancelled, thumbsdown)
```

文本 queued 的取消同理：

- `queued -> cancelled`
- 已经 `dispatching/running` 的文本 reaction 无效

## 9.9 stop

```text
用户点击 stop
bot    -> server : surface.menu.stop
server -> wrapper: turn.interrupt (if active)
server -> state  : queued text -> discarded
server -> state  : staged images -> discarded
server -> bot    : 对被丢弃的消息发 pending.input.state(discarded, thumbsdown)
... active turn eventually interrupted ...
server -> bot    : 当前 running 文本 typing_off
server -> bot    : turn.state(interrupted)
```

补充：

- stop 会清飞书侧 queued text / staged image
- stop 不承诺清本地客户端自己的 queued draft
- 若当前 active turn 是本地发起，仍然允许 interrupt，但后续是否恢复本地 draft 取决于本地客户端实现

## 9.10 上游失败 / wrapper 拒绝

如果出现：

- relay 不可达
- wrapper `command.ack = rejected`
- native upstream 503

规则：

1. 当前 queue item -> `failed`
2. 删除 Typing
3. 发一条明确错误提示
4. 不自动重试

这一步必须保留，不再“静默卡住”。

## 10. 底层实现落点

## 10.1 wrapper

wrapper 只负责：

- app-server 原生协议翻译
- thread / turn / item / request 硬边界上报
- `prompt.send(inputs[])` -> Codex `turn/start.input[]`
- `turn.interrupt`
- `request.respond`

wrapper 不负责：

- 用户 queue
- 图片暂存
- reaction 取消
- attach / selection prompt

## 10.2 server

server 是真正的产品编排层，负责：

- surface console 状态
- attachment
- 线程选择
- queue
- staged image
- selection prompt
- request lifecycle
- render planner
- render event feed

## 10.3 bot

bot 只负责：

- 接飞书事件
- 下载飞书图片资源到本地临时目录
- 把飞书事件翻成 `SurfaceAction`
- 渲染 server 返回的卡片、文本和 reaction

bot 不再自己推断：

- 当前应该 attach 哪个 instance
- 当前哪条消息应该打 Typing
- 哪条 queued 文本该什么时候发出去

## 10.4 `prompt.send` 到 Codex 的最终映射

server 发给 wrapper：

```json
{
  "kind": "prompt.send",
  "origin": {
    "surface": "feishu",
    "userId": "ou_xxx",
    "chatId": "oc_xxx",
    "messageId": "om_text_1001"
  },
  "target": {
    "threadId": "thread-3",
    "createThreadIfMissing": false,
    "cwd": null
  },
  "prompt": {
    "inputs": [
      {
        "type": "local_image",
        "path": "/tmp/fschannel/feishu/oc_xxx/om_img_1.png",
        "mimeType": "image/png",
        "source": "feishu_upload"
      },
      {
        "type": "text",
        "text": "帮我分析这两张图",
        "textElements": []
      }
    ]
  }
}
```

wrapper 映射为 Codex `turn/start.params.input`：

```json
[
  { "type": "localImage", "path": "/tmp/fschannel/feishu/oc_xxx/om_img_1.png" },
  { "type": "text", "text": "帮我分析这两张图", "text_elements": [] }
]
```

## 11. 飞书权限需求

为了满足这轮产品需求，飞书应用至少需要：

- `im:message`
- `im:message.p2p_msg:readonly`
- `im:message:send_as_bot`
- `im:message.reactions:read`
- `im:message.reactions:write_only`
- `im:resource`

订阅事件至少需要：

- `im.message.receive_v1`
- `application.bot.menu_v6`
- `card.action.trigger`
- `im.message.reaction.created_v1`

`im.message.reaction.deleted_v1` 第一版可以先订阅但不使用。

## 12. 业务漏洞与边界条件

## 12.1 旧 list 卡片上的数字回复

必须依赖 `SelectionPromptRecord.expiresAt`。

否则用户翻到半小时前的 list 卡片再回一个 `2`，很容易 attach 错。

### 处理

- prompt 过期后，数字不再解释为选择
- 若过期后用户发纯数字，按普通文本处理

## 12.2 queued 文本和后续切 thread 的竞态

如果不冻结 route，用户会认为“明明发消息时选的是 A，怎么结果跑到 B”。

### 处理

- 文本在 enqueue 时冻结 route
- thread 切换只影响后续新入队文本

## 12.3 staged image 和 thread 切换的竞态

如果 image 在未成 turn 前就绑定 thread，会让用户很难理解。

### 处理

- staged image 不绑定 thread
- 直到下一条文本出现才绑定

## 12.4 同一个 instance 出现多 thread 输出

即使理论上大多数时候只有一个前台 turn，也不能假设永远不会出现别的 thread 输出。

### 处理

- 输出仍按 instance 全量可见文本转发
- 每个 block 都必须带 thread 标题
- 颜色仍按 instance 固定，不按 thread 变

## 12.5 attach 时没有 focused thread

这是合法状态，例如：

- VS Code 刚打开
- 还没选中会话
- wrapper 初始同步还没完成

### 处理

- `routeMode = unbound`
- 第一条文本 dispatch 时再按：
  1. `selectedThreadId`
  2. `observedFocusedThreadId`
  3. 新建 thread

## 12.6 只发图片，不发文字

这是第一版明确留空的 use case。

### 当前处理

- 图片一直 staged
- detach / attach 切换 / stop 时会被清理

### 后续可扩展

- 增加“发送暂存图片”按钮
- 或允许 image-only submit

## 13. 本轮可直接照着实现的最小闭环

如果后面开始编码，建议按下面顺序实施：

1. 先把 server 的 `surfaceSessionId` 状态和 queue / staged image / selection prompt 模型搭起来。
2. 再把 bot 改成 `SurfaceAction` 适配层，补 reaction.created 和 image 下载。
3. 再把 wrapper 的 `prompt.send` 改成结构化 `inputs[]`，优先走 Codex `UserInput.localImage`。
4. 最后接上 render feed 的 `pending.input.state`，把 Typing / `THUMBSDOWN` 反应做完整。

在这个闭环成立前，不要再往 wrapper 里加产品策略。

# Relay Protocol Spec

> Type: `general`
> Updated: `2026-04-16`
> Summary: 继续作为当前 canonical 协议文档，并同步 `turn.steer`、Feishu reaction steering、daemon 驱动的 wrapper 退出命令、`thread/tokenUsage/updated` usage 事件、`turn/plan/updated` 的结构化计划快照事件、`thread.history.read` 定向历史查询 command/event、`thread/status/changed` 到 `thread.runtime_status.updated` 的 authoritative thread runtime status 链路、`turn/diff/updated` 到 `turn.diff.updated` 的 authoritative turn-level aggregated diff 链路、`model/rerouted` 到 `turn.model_rerouted` 的 turn 级模型改路由语义、`threads.snapshot` / `thread.discovered` 上新增的结构化 `runtimeStatus` 投影、`contextCompaction` 到 compact notice 的标准化语义，以及新的 `thread.compact.start` 手动上下文整理 command。

## 1. 文档定位

这份文档描述的是**当前仓库已经实现的协议和内部模型**。

当前真实存在的三层边界是：

1. `Codex app-server native protocol`
   - VS Code / Codex 扩展和 `relay-wrapper` 之间
   - JSONL / JSON-RPC request-response + notification
2. `relay.agent.v1`
   - `relay-wrapper` 和 `relayd` 之间
   - WebSocket + JSON envelope
3. `control.Action` / `control.UIEvent`
   - `relayd` 进程内部的控制与渲染模型
   - 当前**不是**公开网络协议

需要特别说明：

- 当前仓库没有实现公开的 `relay.control.v1` HTTP API
- 当前仓库也没有实现公开的 `relay.render.v1` 拉流 API
- 飞书控制和投影是在 `relayd` 进程内完成的

## 2. Native 协议边界

`relay-wrapper` 针对 Codex app-server 观测并翻译下面这些原生信号：

- `thread/start`
- `thread/resume`
- `thread/name/set`
- `thread/list`
- `thread/read`
- `thread/compact/start`
- `thread/started`
- `thread/status/changed`
- `thread/name/updated`
- `thread/tokenUsage/updated`
- `turn/start`
- `turn/steer`
- `turn/interrupt`
- `turn/started`
- `turn/diff/updated`
- `turn/plan/updated`
- `model/rerouted`
- `turn/completed`
- `item/started`
- `item/completed`
- `item/*/delta`

当前实现中的关键规则：

- wrapper 只负责协议翻译和显式标注
- wrapper 可以 suppress 自己主动注入的原生命令响应
  - 例如远端 `prompt.send` 触发的内部 `thread/start`、`thread/resume`、`turn/start`
- wrapper 不允许因为 helper/internal traffic 而吞掉真实 runtime lifecycle event
  - helper 生命周期必须继续翻译成 canonical event，并显式带上 `trafficClass`

## 3. `relay.agent.v1`

### 3.1 协议名

当前固定为：

```json
{
  "protocol": "relay.agent.v1"
}
```

### 3.2 Envelope 类型

当前实现的 envelope 类型定义在 [wire.go](../../internal/core/agentproto/wire.go)：

- `hello`
- `welcome`
- `event_batch`
- `command`
- `command_ack`
- `error`
- `ping`
- `pong`

当前实际主链路使用的是：

- `hello`
- `welcome`
- `event_batch`
- `command`
- `command_ack`
- `error`

### 3.3 `hello`

wrapper 建立连接后首先发送：

```json
{
  "type": "hello",
  "hello": {
    "protocol": "relay.agent.v1",
    "probe": false,
    "instance": {
      "instanceId": "inst-67f7045577c78c7a",
      "displayName": "dl",
      "workspaceRoot": "/workspace/demo",
      "workspaceKey": "/workspace/demo",
      "shortName": "demo",
      "version": "1.0.0",
      "buildFingerprint": "sha256:abcd...",
      "binaryPath": "<wrapper-binary-path>",
      "pid": 12345
    },
    "capabilities": {
      "threadsRefresh": true
    }
  }
}
```

说明：

- 普通 wrapper 连接使用默认 `probe=false`
- runtime manager 的兼容性探测连接使用 `probe=true`
- `probe=true` 的连接只用于版本/可达性检查，`relayd` 不得把它注册成可见实例，也不得触发普通实例初始化动作

### 3.4 `welcome`

`relayd` 接收 `hello` 后首先返回：

```json
{
  "type": "welcome",
  "welcome": {
    "protocol": "relay.agent.v1",
    "server": {
      "product": "codex-remote",
      "version": "1.0.0",
      "buildFingerprint": "sha256:abcd...",
      "binaryPath": "<daemon-binary-path>",
      "pid": 23456,
      "startedAt": "2026-04-05T10:00:00Z",
      "configPath": "<config-env-path>"
    }
  }
}
```

握手顺序规则：

- 对普通实例连接，首个业务 envelope 必须是 `welcome`
- `welcome` 之后，`relayd` 才可以发送 `command`
- 对 `probe=true` 的连接，只返回 `welcome`，不注册实例，不触发 `threads.refresh`

### 3.5 `event_batch`

wrapper 向 `relayd` 上送 canonical event 时使用：

```json
{
  "type": "event_batch",
  "eventBatch": {
    "instanceId": "inst-67f7045577c78c7a",
    "events": [
      {
        "kind": "turn.started",
        "threadId": "thread-1",
        "turnId": "turn-1",
        "status": "running"
      }
    ]
  }
}
```

### 3.6 `command`

`relayd` 向 wrapper 下发 canonical command 时使用：

```json
{
  "type": "command",
  "command": {
    "commandId": "cmd-1",
    "kind": "threads.refresh"
  }
}
```

### 3.7 `command_ack`

wrapper 收到 `command` 后总是回传 accept/reject：

```json
{
  "type": "command_ack",
  "commandAck": {
    "instanceId": "inst-67f7045577c78c7a",
    "commandId": "cmd-1",
    "accepted": true
  }
}
```

## 4. Canonical Command

当前只实现八个公共 command：

- `prompt.send`
- `thread.compact.start`
- `turn.steer`
- `turn.interrupt`
- `request.respond`
- `threads.refresh`
- `thread.history.read`
- `process.exit`

这些 command 的真实定义在 [types.go](../../internal/core/agentproto/types.go)。

### 4.1 `prompt.send`

关键字段：

- `origin`
  - `surface`
  - `userId`
  - `chatId`
  - `messageId`
- `target`
  - `threadId`
  - `cwd`
  - `createThreadIfMissing`
- `prompt.inputs[]`
  - `text`
  - `local_image`
  - `remote_image`
- `overrides`
  - `model`
  - `reasoningEffort`
  - `accessMode`

### 4.2 `turn.steer`

用于把一个已经在 surface queue 中冻结好的 `queue item` 升级成对当前 running turn 的 steering。

关键字段：

- `target.threadId`
- `target.turnId`
- `prompt.inputs[]`
  - `text`
  - `local_image`
  - `remote_image`

当前 server 侧语义：

- 只有 Feishu `queued` 文本上的点赞会触发；
- steering 的单位是整个 `queue item`，不是裸文本；
- 若该 item 已绑定图片，则 `inputs[]` 会把主文本和同 item 图片一起带上；
- 成功信号当前定义为 wrapper 返回 `command_ack.accepted=true`。

### 4.3 `thread.compact.start`

用于对当前已绑定 thread 发起一次 manual compact。

关键字段：

- `target.threadId`

当前 wrapper / translator 侧语义：

- 若目标 thread 不是 wrapper 当前 native thread，translator 会先发一次 native `thread/resume`
- `thread/resume` 成功后，再继续发 native `thread/compact/start`
- translator 把它翻译成 native `thread/compact/start`
- 该请求的同步成功响应会被 suppress，不额外回传独立 result event
- 后续实际生命周期仍沿用标准 `turn.started` / `item.*` / `turn.completed`
- 若 native 同步返回错误，translator 会上送 `system.error`
  - `operation=thread.compact.start`
  - 以及对应 `surfaceSessionID` / `threadId`

### 4.4 `turn.interrupt`

关键字段：

- `target.threadId`
- `target.turnId`

### 4.5 `request.respond`

用于将 approval / structured response 回写给 native request id。

对于当前已经打通的 approval request，canonical `response` 形态是：

```json
{
  "type": "approval",
  "decision": "accept"
}
```

当前已实现的 `decision`：

- `accept`
- `acceptForSession`
- `decline`

兼容规则：

- Feishu gateway 仍接受旧卡片里的 `approved: true/false`
- orchestrator 会把旧布尔值映射成 `accept` / `decline`
- 发往 wrapper 的 canonical command 优先使用 `decision`，不再只依赖布尔字段

需要注意：

- `captureFeedback` 是 Feishu 产品层 option，不是 native approval decision
- 它会在 server 层翻译成：
  - 对当前 request 发送 `decision=decline`
  - 再把用户下一条文字作为 follow-up prompt 入队

### 4.6 `threads.refresh`

触发 wrapper 走 `thread/list + thread/read`，再返回标准化的 `threads.snapshot`。

当前还会把 native `thread.status` 统一折叠到 canonical `ThreadSnapshotRecord.runtimeStatus`：

- `thread/list` / `thread/read` 给出的 `status` 现在会直接进入 `runtimeStatus`
- 兼容旧投影时，`state` 仍保留 legacy 字面值：
  - `active -> running`
  - `notLoaded -> not_loaded`
  - `systemError -> system_error`
- `loaded` 当前优先按 authoritative `runtimeStatus` 推导，而不是继续单独猜测

### 4.7 `thread.history.read`

用于让 daemon 按需向某个 wrapper 查询指定 thread 的完整历史。

关键字段：

- `commandId`
- `target.threadId`

当前 wrapper 侧语义：

- translator 把它翻译成 native `thread/read`
- 固定带 `includeTurns=true`
- 成功后不复用 `threads.snapshot`，而是回传单独的 `thread.history.read` event

### 4.8 `process.exit`

由 daemon 在 shutdown / upgrade 窗口下发，要求 wrapper 自行退出并结束其 child `codex app-server`。

约束：

- wrapper 收到后仍返回正常 `command_ack.accepted=true`
- wrapper 不应等待新的 surface / queue 调度
- wrapper 应在断开 relay 前完成 child 收敛，避免 daemon 误以为 stable entry 已释放

## 5. Canonical Event

当前事件类型：

- `threads.snapshot`
- `thread.history.read`
- `thread.discovered`
- `thread.focused`
- `thread.runtime_status.updated`
- `thread.token_usage.updated`
- `turn.diff.updated`
- `turn.model_rerouted`
- `config.observed`
- `local.interaction.observed`
- `turn.started`
- `turn.plan.updated`
- `turn.completed`
- `item.started`
- `item.delta`
- `item.completed`
- `request.started`
- `request.resolved`

### 5.1 `thread.runtime_status.updated`

这是 wrapper 对 native `thread/status/changed` 的标准化事件；另外 `thread/started` 与 `threads.snapshot` 现在也会携带相同结构的 `runtimeStatus`。

关键字段：

- `threadId`
- `runtimeStatus`
  - `type`
    - `notLoaded`
    - `idle`
    - `systemError`
    - `active`
  - `activeFlags`
    - `waitingOnApproval`
    - `waitingOnUserInput`
- `status`
  - 当前仍保留 legacy 展示值：
    - `running`
    - `idle`
    - `system_error`
    - `not_loaded`
- `loaded`
  - 当前由 `runtimeStatus` 同步推导

当前语义：

- wrapper 现在不会再吞掉 native `thread/status/changed`
- `thread/started.thread.status` 与 `thread/list` / `thread/read.thread.status` 也统一走同一套解析
- orchestrator 把它当作 thread 级 authoritative runtime source；surface queue/request gate 仍由本地调度状态决定

### 5.2 `thread.history.read`

这是一个 command-correlated result event，用于把 `thread/read(includeTurns=true)` 的结构化结果从 wrapper 定向送回 daemon。

关键字段：

- `commandId`
- `threadId`
- `threadHistory`
  - `thread`
  - `turns[]`
    - `turnId`
    - `status`
    - `startedAt`
    - `completedAt`
    - `errorMessage`
    - `items[]`

### 5.3 `turn.diff.updated`

这是 wrapper 对 native `turn/diff/updated` 的标准化 turn 级派生事件。

关键字段：

- `threadId`
- `turnId`
- `turnDiff`

当前语义：

- `turnDiff` 直接对应 app-server 给出的 latest aggregated unified diff snapshot
- 对同一 `(threadId, turnId)` 的多次更新，orchestrator 采用 overwrite / latest-wins 语义，而不是 append 拼接
- 这条链路与 `item.fileChange` 并存，但语义不同：
  - `item.fileChange` 仍代表过程中的局部 file-change item
  - `turn.diff.updated` 代表整轮 authoritative 聚合 diff 快照
- 当前实现会在 orchestrator 内按 turn 暂存 latest snapshot，并在最终 block 事件上挂出 `TurnDiffSnapshot`
- 当前尚未新增 Feishu 独立 diff UI；因此这次协议承接以语义保真和上层可消费为主，不改变现有 file-change 渲染路径

### 5.4 `turn.model_rerouted`

这是 wrapper 对 native `model/rerouted` 的标准化 turn 级派生事件。

关键字段：

- `threadId`
- `turnId`
- `modelReroute`
  - `threadId`
  - `turnId`
  - `fromModel`
  - `toModel`
  - `reason`

当前语义：

- wrapper 会保留 `fromModel` / `toModel` / `reason`，不再把 turn 级模型改路由静默吞掉
- orchestrator 对同一 `(threadId, turnId)` 采用 latest-wins 语义保存最近一次 reroute
- thread 级当前有效模型会同步切到 `toModel`，这样现有 snapshot / prompt / status 展示不会继续误报 reroute 前模型
- 当前仍不额外生成 Feishu 强提示；用户可见层先保持安静，后续是否展示、展示到哪里再单独讨论

### 5.5 关键字段

#### `initiator`

当前使用：

- `remote_surface`
- `local_ui`
- `internal_helper`
- `unknown`

#### `trafficClass`

当前使用：

- `primary`
- `internal_helper`

#### `tokenUsage`

当前仅在 `thread.token_usage.updated` 上使用。

字段形状对齐 app-server `thread/tokenUsage/updated`：

- `total`
  - `totalTokens`
  - `inputTokens`
  - `cachedInputTokens`
  - `outputTokens`
  - `reasoningOutputTokens`
- `last`
  - `totalTokens`
  - `inputTokens`
  - `cachedInputTokens`
  - `outputTokens`
  - `reasoningOutputTokens`
- `modelContextWindow`

当前语义：

- wrapper 只做字段标准化，不在这层计算展示文案
- orchestrator 将 thread 级快照持久在内存 `ThreadRecord`
- remote turn 绑定会额外记录 `last`，供 final summary 精确消费

这两个字段共同决定：

- turn 是否应进入远端 queue 状态机
- item 是否应进入 Feishu 主渲染面
- 本地交互是否应触发 `paused_for_local`

#### `planSnapshot`

当前仅在 `turn.plan.updated` 上使用。

字段形状：

- `explanation`
- `steps[]`
  - `step`
  - `status`
    - `pending`
    - `in_progress`
    - `completed`

当前语义：

- wrapper 会把 native `turn/plan/updated` 标准化成 `turn.plan.updated + planSnapshot`
- `item/plan/delta` 仍属于 `item.delta` 文本流，不会被折叠成 `planSnapshot`
- orchestrator 在产品层按 live turn + surface 去重同内容快照，避免重复投影相同计划更新

#### `item.completed.itemKind=context_compaction`

当前语义：

- wrapper 会把 native `item.type=contextCompaction` 标准化成 `item.completed + itemKind=context_compaction`
- 这个 item 不走 assistant 正文渲染，也不参与普通文本缓冲
- orchestrator 会把它投影成一条单独的 compact 成功提示
- 若 compact 发生时没有 live surface，server 会把该提示存成 thread 级一次性 replay，等用户重新接入该 thread 时只补投一次

### 5.6 Request 元数据

`request.started` 当前至少会携带：

- `requestId`
- `threadId`
- `turnId`
- `metadata.requestType`
- `metadata.requestKind`
- `metadata.title`
- `metadata.body`

若 native payload 显式暴露 request options，translator 还会透传：

- `metadata.options[]`
  - `optionId`
  - `label`
  - `style`

当前 server 只在产品层对 approval request 做额外补全：

- 若 upstream 未显式给出 option，但请求种类可确认支持 session 级放行，则补出 `acceptForSession`
- `captureFeedback` 只存在于 Feishu `request.prompt` 渲染层，不回写到 canonical event

### 5.7 Helper/Internal traffic 规则

当前冻结规则：

- `ephemeral`
- `persistExtendedHistory`
- `outputSchema`

只能影响：

- wrapper 的模板复用
- canonical event 上的 `trafficClass=internal_helper`
- canonical event 上的 `initiator=internal_helper`

不能直接导致 wrapper 吞掉下面这些生命周期事件：

- `thread.discovered`
- `turn.started`
- `item.*`
- `turn.completed`

## 6. 当前实现中的内部控制与渲染模型

虽然当前没有公开的 `relay.control.v1` / `relay.render.v1` 网络协议，但这两层语义已经稳定存在于进程内模型中。

### 6.1 Inbound control

飞书入口最终被归一到 [control.Action](../../internal/core/control/types.go)：

- `surface.menu.list_instances`
- `surface.menu.status`
- `surface.menu.stop`
- `surface.menu.compact`
- `surface.command.model`
- `surface.command.reasoning`
- `surface.command.access`
- `surface.request.respond`
- `surface.message.text`
- `surface.message.image`
- `surface.message.reaction.created`
- `surface.button.attach_instance`
- `surface.button.show_threads`
- `surface.button.use_thread`
- `surface.button.follow_local`
- `surface.button.detach`

其中与 approval 相关的关键字段是：

- `surface.request.respond`
  - `requestId`
  - `requestType`
  - `requestOptionId`
  - `approved`

当前含义：

- `requestOptionId` 是主路径，来自飞书卡片按钮值
- `approved` 只是旧卡片兼容字段
- `reactionType + targetMessageId` 当前用于 queued 文本点赞 steering

### 6.2 Outbound UI events

`orchestrator` 输出 [control.UIEvent](../../internal/core/control/types.go)，再由 Feishu projector 映射成文本、卡片和 reaction：

- `snapshot.updated`
- `selection.prompt`
- `request.prompt`
- `pending.input.state`
- `notice`
- `thread.selection.changed`
- `block.committed`
- `agent.command`

其中与 approval 相关的关键字段是：

- `request.prompt`
  - `requestId`
  - `requestType`
  - `title`
  - `body`
  - `threadTitle`
  - `options[]`

`options[]` 是 Feishu 可直接渲染的产品层动作集合，可能包含：

- upstream 原生透传的 approval option
- server 合成的 `acceptForSession`
- Feishu 专用的 `captureFeedback`

`pending.input.state` 当前除 queue/typing/discard 外，还会投影：

- steering 成功后的 `QueueOff + ThumbsUp`
- 对同一 queue item 主文本和已绑定图片的统一反馈

## 7. 当前不暴露的能力

下面这些在当前仓库里**没有作为公共协议暴露**：

- 公开的 attach/detach/use-thread HTTP API
- 公开的 render event 拉流 API
- block update / replace
- native frame debug/replay export

如果后续真的需要对外开放控制面，应该在现有 `control.Action` / `control.UIEvent` 基础上重新设计，而不是继续使用旧文档里的 `/v1/users/:userId/...` 形式。

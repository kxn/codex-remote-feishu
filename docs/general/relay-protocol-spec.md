# Relay Protocol Spec

> Type: `general`
> Updated: `2026-04-06`
> Summary: 迁移到 `docs/general` 并统一文档元信息头，继续作为当前 canonical 协议文档。

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
- `thread/started`
- `thread/name/updated`
- `turn/start`
- `turn/steer`
- `turn/interrupt`
- `turn/started`
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

当前只实现四个公共 command：

- `prompt.send`
- `turn.interrupt`
- `request.respond`
- `threads.refresh`

这四个 command 的真实定义在 [types.go](../../internal/core/agentproto/types.go)。

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

### 4.2 `turn.interrupt`

关键字段：

- `target.threadId`
- `target.turnId`

### 4.3 `request.respond`

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

### 4.4 `threads.refresh`

触发 wrapper 走 `thread/list + thread/read`，再返回标准化的 `threads.snapshot`。

## 5. Canonical Event

当前事件类型：

- `threads.snapshot`
- `thread.discovered`
- `thread.focused`
- `config.observed`
- `local.interaction.observed`
- `turn.started`
- `turn.completed`
- `item.started`
- `item.delta`
- `item.completed`
- `request.started`
- `request.resolved`

### 5.1 关键字段

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

这两个字段共同决定：

- turn 是否应进入远端 queue 状态机
- item 是否应进入 Feishu 主渲染面
- 本地交互是否应触发 `paused_for_local`

### 5.2 Request 元数据

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

### 5.3 Helper/Internal traffic 规则

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

## 7. 当前不暴露的能力

下面这些在当前仓库里**没有作为公共协议暴露**：

- 公开的 attach/detach/use-thread HTTP API
- 公开的 render event 拉流 API
- 远端 `turn.steer`
- block update / replace
- native frame debug/replay export

如果后续真的需要对外开放控制面，应该在现有 `control.Action` / `control.UIEvent` 基础上重新设计，而不是继续使用旧文档里的 `/v1/users/:userId/...` 形式。

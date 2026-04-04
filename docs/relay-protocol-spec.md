# Relay Protocol Spec

## 1. 状态

这是一份实现前置的协议规格，目标是先冻结下面几件事：

- wrapper 和 server 之间到底传什么
- server 和 bot 之间到底暴露什么
- 哪些边界属于协议硬语义
- 哪些边界属于 server 渲染层
- attach / use-thread / prompt / interrupt / request-response 的控制流
- 用什么信息支撑 Feishu 侧正确切分消息

飞书侧更细的产品交互和状态机，见：

- [feishu-product-design.md](./feishu-product-design.md)

这份文档是语言无关的。后续可以继续沿用当前的多进程实现，也可以按这份接口改成单语言重写。

## 2. 设计冻结项

下面这些点在后续实现里视为冻结，不再反复摇摆。

### 2.1 三层职责

- `wrapper` 是 agent adapter，不是产品调度器
- `server` 是唯一的产品状态中心和渲染规划中心
- `bot` 只消费用户层事件，不直接解析 agent 原生协议
- 用户层状态按 `surfaceSessionId` 建模，不再按裸 `userId` 建模

### 2.2 两类边界

- 硬边界
  - `thread`
  - `turn`
  - `item`
  - `request`
- 软边界
  - 一个 `assistant_message` item 内部的展示切分

### 2.3 软边界归属

软边界必须由 `server renderer planner` 处理，不允许再放在 bot 里靠 message classification 猜。

### 2.4 Bot 主链路

bot 主链路只接收“已经可以发给用户”的用户层事件，不再接收：

- 原生 JSON-RPC request/response
- 原生 `item/.../delta`
- 原生 `thread/...`
- 原生 `turn/...`

### 2.5 Debug / Replay

原生协议不能丢，但只进入 debug/replay 通道，不进入 bot 主链路。

### 2.6 信息保留原则

canonical model 不能再把重要语义压扁成“纯文本流”。

至少必须保留：

- `threadId`
- `turnId`
- `itemId`
- `requestId`
- item 原始种类
- turn / request 最终状态
- 与执行相关的结构化元数据
  - 例如 `command`、`cwd`、`tool name`、`change status`

无法统一抽象的字段必须进入：

- `metadata`
- 或 `agentSpecific`

不能直接丢弃。

### 2.7 V1 已冻结决策

- `relay.render.v1` 只发不可变的 `block.committed`
- v1 不要求 bot 处理 block update / replace
- `reasoning` 默认只进 debug，不进普通 bot feed
- `plan` 默认不以原样正文发给 bot；必要时只能降级为 `status` 类提示
- 用户层排队、图片暂存、数字选择、reaction 取消都归 `server`，不归 wrapper
- `prompt.send` 承载结构化 `inputs[]`，而不是只有纯文本 `prompt.text`
- server 到 wrapper 的公共命令面只保留：
  - `prompt.send`
  - `turn.interrupt`
  - `request.respond`
  - `threads.refresh`
- helper/internal traffic 不能再靠 wrapper 直接吞掉 runtime lifecycle event
  - `ephemeral`
  - `persistExtendedHistory`
  - `outputSchema`
  这些字段只允许影响：
  - adapter 的模板复用
  - canonical event 上的 `trafficClass`
- 产品层是否“跟随本地 focused thread”必须由显式 `routeMode` 表达，不能再只靠 `selectedThreadId = null` 猜
- reconnect 只保证“同一个 wrapper 进程复用同一个 `instanceId`”时可恢复
- wrapper 进程重启视为新 instance，不做隐式逻辑合并

## 3. 术语

### 3.1 Native Frame

agent 原生协议中的一帧消息。

对 Codex 来说，就是一行 JSONL 的 request / response / notification。

### 3.2 Canonical Event

wrapper 翻译后发给 server 的统一事件。

### 3.3 Canonical Command

server 发给 wrapper 的统一命令。

### 3.4 Render Event

server 发给 bot 的用户层事件。

### 3.5 Render Block

最终要发送到飞书的一块稳定内容。

### 3.6 Instance

一个 wrapper 连接实例。

### 3.7 Connection

一次具体的 WS 连接。一个 instance 在断线重连后会出现多个 connection，但 `instanceId` 不变。

### 3.8 Attachment

某个用户当前接管了哪个 instance。

### 3.9 Observed Focused Thread

从本地 VS Code / agent UI 行为中观察到的当前 thread。

### 3.10 Remote Selected Thread

飞书端显式选中的当前远端输入目标 thread。

### 3.11 Surface Session

一个具体的 bot 会话面板。

对飞书来说，至少应包含：

- `chatId`
- 平台类型

必要时还可附带：

- `actorUserId`
- 群线程信息

### 3.12 Staged Asset

用户已经发给 bot，但还没有真正提交给 agent 的附件资源。

当前第一版主要是：

- 图片

## 4. 协议总览

系统存在三条协议面：

### 4.1 Native Protocol

`agent <-> wrapper`

这是 agent 自己的原生协议。

当前针对 Codex：

- JSON-RPC request/response
- notification
- JSONL framing

### 4.2 relay.agent.v1

`wrapper <-> server`

这是 agent adapter 协议。

职责：

- 上报 canonical event
- 接收 canonical command
- 传输 debug native frame

### 4.3 relay.control.v1 / relay.render.v1

`server <-> bot`

职责：

- bot 发控制动作
- bot 拉取 render event
- bot 获取控制面快照

说明：

- `wrapper <-> server` 的 agent 适配协议以本文为准
- `server <-> bot` 的产品层动作、surface 状态、队列和图片语义，以
  [feishu-product-design.md](./feishu-product-design.md) 为准

## 5. Native Protocol 约束

这一层只做两件事：

- wrapper 读取原生消息
- wrapper 把原生消息翻译成 canonical

不能让原生协议继续泄露到 bot 主链路。

### 5.1 当前确认的 Codex 硬边界

已经确认 Codex 能稳定提供：

- `thread/started`
- `thread/name/updated`
- `thread/tokenUsage/updated`
- `turn/started`
- `turn/completed`
- `item/started`
- `item/completed`
- `item/agentMessage/delta`
- `item/plan/delta`
- `item/reasoning/*`
- `item/commandExecution/outputDelta`
- `item/fileChange/outputDelta`
- `item/mcpToolCall/progress`
- server request methods

另外，Codex 客户端到 agent 的请求流中，还能观测到：

- `thread/start`
- `thread/resume`
- `turn/start`
- `turn/steer`
- `turn/interrupt`
- `thread/loaded/list`
- `thread/read`

其中前四类对“当前 focused thread”和“当前 cwd”的判断很关键。

### 5.2 当前确认的限制

Codex 的 `agentMessage` 只保证：

- 属于哪个 `itemId`
- 文本增量内容是什么
- item 何时结束

它不保证：

- 文本内部的引导语边界
- 文件列表边界
- 尾注边界
- 哪一句应该单独发成一个 bot 消息

所以 canonical model 必须保留 item 级硬边界，render planner 再做文本级细分。

### 5.3 对切分需求的直接结论

bot 需要的几个关键展示需求，都可以由“硬边界 + server 侧结构化切分”满足：

- tool block 和 assistant 正文分开：靠 `itemKind`
- final message 不和前面的工具输出粘连：靠 item 生命周期
- 文件列表整块展示：靠 assistant item 完整文本 + render planner
- 长 turn 不要等整轮结束：靠 turn/item 流 + committed block 逐块提交

唯独下面这种更细的切分不属于原生协议语义：

- 一个 `agentMessage` 里前导短句是否要单独成块
- fenced code block 前后的说明文字是否要拆开
- 尾注是否独立成条

这部分必须由 server renderer 负责，不能要求 wrapper 或 bot 猜。

## 6. relay.agent.v1

## 6.1 传输

- 传输层：WebSocket
- 编码：UTF-8 JSON
- 每个 WS message 是一条完整 envelope
- 推荐支持 batch，以降低高频 delta 开销

## 6.2 Envelope

所有消息共享以下公共字段：

```ts
type AgentEnvelopeBase = {
  protocol: "relay.agent.v1";
  type: string;
  messageId: string;
  sentAt: string; // ISO-8601
  instanceId?: string;
};
```

约束：

- `messageId` 在发送方本地唯一
- `instanceId` 从 `agent.hello` 开始固定
- `sentAt` 用于调试与重放，不参与业务排序

真正排序以事件里的 `seq` 为准。

## 6.3 握手

### 6.3.1 `agent.hello`

wrapper 首条消息。

`instanceId` 规则冻结为：

- 由 wrapper 进程在启动时生成一次
- 在同一个 wrapper 进程内，所有重连都复用同一个 `instanceId`
- wrapper 进程重启后，必须生成新的 `instanceId`

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.hello",
  "messageId": "hello-1",
  "sentAt": "2026-04-03T12:18:33.000Z",
  "instanceId": "inst_codex_01",
  "adapter": {
    "family": "codex",
    "name": "codex-app-server",
    "version": "0.1.0"
  },
  "instance": {
    "displayName": "droid",
    "workspaceRoot": "/data/dl/droid",
    "pid": 1347581
  },
  "capabilities": {
    "supportsPromptSend": true,
    "supportsInterrupt": true,
    "supportsStructuredRequestResponse": true,
    "supportsObservedThreadFocus": true,
    "supportsThreadsRefresh": true,
    "supportsLoadedThreadSnapshot": true,
    "supportsDebugNativeFrames": true,
    "inputKinds": [
      "text",
      "local_image",
      "remote_image"
    ],
    "itemKinds": [
      "assistant_message",
      "plan",
      "reasoning",
      "command",
      "file_change",
      "tool_call",
      "collab_agent_call",
      "web_search",
      "image",
      "review_state",
      "context_compaction"
    ]
  }
}
```

### 6.3.2 `agent.welcome`

server 对 hello 的确认。

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.welcome",
  "messageId": "welcome-1",
  "sentAt": "2026-04-03T12:18:33.010Z",
  "instanceId": "inst_codex_01",
  "connectionId": "conn_7f4d",
  "debug": {
    "collectNativeFrames": true
  }
}
```

## 6.4 Canonical Event

## 6.4.1 事件基类

```ts
type CanonicalEventBase = {
  seq: number; // 每个 instance 内严格递增
  occurredAt: string;
  kind: string;
  agentFamily: "codex" | string;
  trafficClass?: "primary" | "internal_helper";
  agentSpecific?: Record<string, unknown>;
};
```

`trafficClass` 语义冻结为：

- `primary`
  - 默认值
  - 表示该事件属于正常对话主链路
- `internal_helper`
  - 表示该事件来自 native client 自己发起的 helper/internal 流量
  - server 必须接收，但是否进入产品主链路由 server 决定

额外约束：

- `trafficClass` 不能由 bot 决定
- 不能因为 `trafficClass = internal_helper`，就在 wrapper 里直接吞掉 runtime lifecycle event
- wrapper 唯一允许 suppress 的，仍然只有“adapter 自己主动注入的原生命令响应”

排序和恢复规则：

- server 必须按 `seq` 处理同一 instance 的事件
- `seq <= lastSeq` 的事件视为重复，必须丢弃
- 若出现 `seq > lastSeq + 1` 的缺口，server 必须：
  - 记录 gap
  - 清空仍在 streaming 的临时 render buffer
  - 触发一次 `threads.refresh`

## 6.4.2 `agent.event.batch`

wrapper 推荐批量发送 canonical event：

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.event.batch",
  "messageId": "evt-1042",
  "sentAt": "2026-04-03T12:18:33.200Z",
  "instanceId": "inst_codex_01",
  "firstSeq": 1042,
  "events": [
    {
      "seq": 1042,
      "occurredAt": "2026-04-03T12:18:33.180Z",
      "kind": "thread.focused",
      "agentFamily": "codex",
      "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
      "cwd": "/data/dl/droid",
      "focusSource": "local_ui"
    }
  ]
}
```

## 6.5 Canonical Event 类型

以下是第一版必须支持的事件。

### 6.5.1 Thread 描述

```ts
type ThreadDescriptor = {
  threadId: string;
  name: string | null;
  preview: string | null;
  cwd: string | null;
  createdAt: number | null;
  updatedAt: number | null;
  archived: boolean | null;
  loaded: boolean | null;
  path: string | null;
  source: string | null;
  modelProvider: string | null;
  metadata: Record<string, unknown>;
};
```

### 6.5.2 Thread 事件

```ts
type ThreadsSnapshotEvent = CanonicalEventBase & {
  kind: "threads.snapshot";
  scope: "loaded";
  replace: true;
  threads: Array<ThreadDescriptor>;
};

type ThreadDiscoveredEvent = CanonicalEventBase & {
  kind: "thread.discovered";
  thread: ThreadDescriptor;
};

type ThreadUpdatedEvent = CanonicalEventBase & {
  kind: "thread.updated";
  threadId: string;
  patch: Record<string, unknown>;
};

type ThreadFocusedEvent = CanonicalEventBase & {
  kind: "thread.focused";
  threadId: string;
  cwd: string | null;
  focusSource: "local_ui" | "remote_created_thread" | "recovery";
};
```

`thread.focused` 语义冻结为：

- 只能由明确的本地 UI 请求或远端新建 thread 完成后触发
- 不能被后台线程的输出事件隐式改写
- 不能被 `turn/completed` 之类的结果事件改写

### 6.5.3 本地交互观测事件

```ts
type LocalInteractionObservedEvent = CanonicalEventBase & {
  kind: "local.interaction.observed";
  threadId: string | null;
  turnId: string | null;
  cwd: string | null;
  action: "turn_start" | "turn_steer";
  interactionClass: "local_ui" | "internal_helper";
};
```

`local.interaction.observed` 语义冻结为：

- 由 wrapper 在观察到本地 UI 直接发出的交互请求时立刻产生
- 当前第一版至少覆盖：
  - 原生 `turn/start` -> `action = turn_start`
  - 原生 `turn/steer` -> `action = turn_steer`
- 只能来自本地客户端直连流量，不能由 wrapper 自己替 server 发出的内部命令伪造
- 它的用途是给 server 一个“本地正在抢占交互权”的早期信号
- 它不等价于“新 turn 已经开始”
  - 尤其 `turn/steer` 可能不会对应新的 `turn.started`
- `interactionClass = internal_helper` 时
  - 该事件只用于 server 记录或 debug
  - 不得触发本地优先仲裁

### 6.5.4 Turn 事件

```ts
type TurnStartedEvent = CanonicalEventBase & {
  kind: "turn.started";
  threadId: string;
  turnId: string;
  status: "running";
  initiator:
    | { kind: "local_ui" }
    | { kind: "internal_helper" }
    | { kind: "remote_surface"; surfaceSessionId: string | null }
    | { kind: "unknown" };
};

type TurnCompletedEvent = CanonicalEventBase & {
  kind: "turn.completed";
  threadId: string;
  turnId: string;
  status: "completed" | "failed" | "interrupted" | "cancelled";
  errorMessage: string | null;
  errorDetails: string | null;
  initiator:
    | { kind: "local_ui" }
    | { kind: "internal_helper" }
    | { kind: "remote_surface"; surfaceSessionId: string | null }
    | { kind: "unknown" };
};
```

Codex 的原生 `inProgress` 统一映射为 canonical `running`。

`initiator` 的判定规则冻结为：

- wrapper 自己根据 canonical `prompt.send` 触发的 turn，标记为 `remote_surface`
- 从本地 VS Code / Cursor 客户端直接观察到的 `turn/start`，标记为 `local_ui`
- 从 native client 自己发起、但已识别为 helper/internal 的 turn，标记为 `internal_helper`
- 无法可靠判定时，标记为 `unknown`

补充约束：

- `turn.started.initiator` 用于确认“已经开始的这轮 turn 属于谁”
- 但 server 做本地优先仲裁时，不能只依赖 `turn.started`
- 因为本地 `turn/steer` 可能不会产生新的 `turn.started`
- 因此“尽早暂停远端 autosend”应以前面的 `local.interaction.observed` 为准

### 6.5.5 Item 事件

```ts
type CanonicalItemKind =
  | "assistant_message"
  | "plan"
  | "reasoning"
  | "command"
  | "file_change"
  | "tool_call"
  | "collab_agent_call"
  | "web_search"
  | "image"
  | "review_state"
  | "context_compaction"
  | "unknown";

type RenderHints = {
  preferredBlockKind:
    | "assistant_markdown"
    | "assistant_code"
    | "tool_summary"
    | "tool_output"
    | "plan"
    | "reasoning_summary"
    | "approval"
    | "status"
    | "error";
  preserveWhitespace: boolean;
  markdown: boolean;
  language: string | null;
  flushPolicy:
    | "progressive_paragraph"
    | "line_buffered"
    | "on_item_complete"
    | "manual";
  exposure: "visible" | "status_fallback" | "debug_only";
};

type ItemStartedEvent = CanonicalEventBase & {
  kind: "item.started";
  threadId: string;
  turnId: string;
  initiator?:
    | { kind: "local_ui" }
    | { kind: "internal_helper" }
    | { kind: "remote_surface"; surfaceSessionId: string | null }
    | { kind: "unknown" };
  item: {
    itemId: string;
    itemKind: CanonicalItemKind;
    title: string | null;
    summary: string | null;
    renderHints: RenderHints;
    metadata: Record<string, unknown>;
  };
};

type ItemDeltaEvent = CanonicalEventBase & {
  kind: "item.delta";
  threadId: string;
  turnId: string;
  itemId: string;
  itemKind: CanonicalItemKind;
  initiator?:
    | { kind: "local_ui" }
    | { kind: "internal_helper" }
    | { kind: "remote_surface"; surfaceSessionId: string | null }
    | { kind: "unknown" };
  delta:
    | { type: "text"; text: string }
    | { type: "command_output"; stream: "stdout" | "stderr" | "combined"; text: string }
    | { type: "diff_text"; text: string }
    | { type: "plan_text"; text: string }
    | { type: "reasoning_summary_text"; text: string }
    | { type: "reasoning_text"; text: string }
    | { type: "tool_progress"; text: string }
    | { type: "other"; payload: Record<string, unknown> };
};

type ItemCompletedEvent = CanonicalEventBase & {
  kind: "item.completed";
  threadId: string;
  turnId: string;
  initiator?:
    | { kind: "local_ui" }
    | { kind: "internal_helper" }
    | { kind: "remote_surface"; surfaceSessionId: string | null }
    | { kind: "unknown" };
  item: {
    itemId: string;
    itemKind: CanonicalItemKind;
    finalText: string | null;
    metadata: Record<string, unknown>;
  };
};
```

metadata 最低要求：

| itemKind | metadata 最低要求 |
| --- | --- |
| `command` | `command`, `cwd`, `processId`, `status`, `exitCode`, `durationMs` |
| `file_change` | `status`, `changeCount` |
| `tool_call` | `server`, `tool`, `status`, `durationMs` |
| `collab_agent_call` | `tool`, `senderThreadId`, `receiverThreadIds`, `status` |
| `web_search` | `query` |
| `review_state` | `transition`, `review` |

helper/internal 补充约束：

- 如果 item 所属 turn 已被 adapter 标记为 `internal_helper`
  - item lifecycle 仍然要进入 canonical event 流
  - 但必须带上：
    - `trafficClass = internal_helper`
    - `initiator.kind = internal_helper`
- server 可以决定这些 item 不进入普通 render feed
- 但 wrapper 不得直接吞掉这些 item event

### 6.5.5 Request 事件

```ts
type RequestStartedEvent = CanonicalEventBase & {
  kind: "request.started";
  request: {
    requestId: string | number;
    requestKind: "approval_command" | "approval_file_change" | "user_input" | "tool_call" | "other";
    threadId: string | null;
    turnId: string | null;
    itemId: string | null;
    title: string;
    body: string;
    structured: Record<string, unknown>;
  };
};

type RequestResolvedEvent = CanonicalEventBase & {
  kind: "request.resolved";
  requestId: string | number;
  resolution: "approved" | "denied" | "submitted" | "cancelled" | "unknown";
};
```

### 6.5.6 其他事件

```ts
type WarningEvent = CanonicalEventBase & {
  kind: "warning";
  code: string;
  message: string;
  details?: Record<string, unknown>;
};

type ErrorEvent = CanonicalEventBase & {
  kind: "error";
  code: string;
  message: string;
  details?: Record<string, unknown>;
};
```

## 6.6 Codex 到 Canonical 的 item 映射

| Codex item type | Canonical itemKind |
| --- | --- |
| `agentMessage` | `assistant_message` |
| `plan` | `plan` |
| `reasoning` | `reasoning` |
| `commandExecution` | `command` |
| `fileChange` | `file_change` |
| `mcpToolCall` | `tool_call` |
| `collabAgentToolCall` | `collab_agent_call` |
| `webSearch` | `web_search` |
| `imageView` | `image` |
| `enteredReviewMode` / `exitedReviewMode` | `review_state` |
| `contextCompaction` | `context_compaction` |

## 6.7 Canonical Command

## 6.7.1 命令基类

```ts
type CanonicalCommandBase = {
  commandId: string;
  issuedAt: string;
  kind: string;
};
```

### 6.7.2 必须支持的命令

```ts
type CanonicalUserInput =
  | {
      type: "text";
      text: string;
      textElements: Array<Record<string, unknown>>;
    }
  | {
      type: "local_image";
      path: string;
      mimeType: string | null;
      source: "feishu_upload" | "local_fs" | "other";
    }
  | {
      type: "remote_image";
      url: string;
    };

type PromptSendCommand = CanonicalCommandBase & {
  kind: "prompt.send";
  origin: {
    surface: "feishu";
    userId: string;
    chatId: string | null;
    messageId: string | null;
  };
  target: {
    threadId: string | null;
    createThreadIfMissing: boolean;
    cwd: string | null;
  };
  prompt: {
    inputs: Array<CanonicalUserInput>;
  };
  overrides: {
    approvalPolicy: string | null;
    sandboxPolicy: Record<string, unknown> | null;
    model: string | null;
    effort: string | null;
    collaborationMode: Record<string, unknown> | null;
  };
};

type TurnInterruptCommand = CanonicalCommandBase & {
  kind: "turn.interrupt";
  target: {
    threadId: string | null;
    turnId: string | null;
    useActiveTurnIfOmitted: boolean;
  };
};

type RequestRespondCommand = CanonicalCommandBase & {
  kind: "request.respond";
  requestId: string | number;
  response:
    | { type: "approval"; approved: boolean }
    | { type: "structured"; result: Record<string, unknown> };
};

type ThreadsRefreshCommand = CanonicalCommandBase & {
  kind: "threads.refresh";
  scope: "loaded";
};
```

`prompt.send` 的语义冻结为：

- server 不直接下发原生 `thread/start` / `thread/resume` / `turn/start`
- adapter 根据 target 自己决定需要：
  - 直接 `turn/start`
  - 先 `thread/resume` 再 `turn/start`
  - 先 `thread/start` 再 `turn/start`

额外冻结：

- v1 canonical command 面不直接暴露 `turn.steer`
- 若后续产品要支持“远端把新输入追加到当前进行中的 turn”，应单独增加 `turn.steer` 命令
- 不能把这种 mid-turn append 语义偷偷塞回 `prompt.send`

`target.cwd` 规则冻结为：

- `threadId != null` 且 `cwd == null`
  - 继承目标 thread 的 `cwd`
- `threadId == null` 且 `cwd == null`
  - 使用 instance `defaultCwd`
  - 若无 `defaultCwd`，再回退到 `workspaceRoot`
- `cwd != null`
  - 视为显式 override

`prompt.inputs` 对 Codex adapter 的映射冻结为：

- `text` -> `turn/start.params.input[]` 中的 `UserInput.text`
- `local_image` -> `turn/start.params.input[]` 中的 `UserInput.localImage`
- `remote_image` -> `turn/start.params.input[]` 中的 `UserInput.image`

补充约束：

- adapter 应优先依赖原生 `input[]` 能力承载图片
- 原生协议里若仍存在兼容性 `attachments` 字段，它也只是 adapter 私有细节
- `attachments` 不能进入 canonical contract

### 6.7.3 `agent.command`

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.command",
  "messageId": "cmd-msg-71",
  "sentAt": "2026-04-03T12:18:34.000Z",
  "instanceId": "inst_codex_01",
  "command": {
    "commandId": "cmd-71",
    "issuedAt": "2026-04-03T12:18:34.000Z",
    "kind": "prompt.send",
    "origin": {
      "surface": "feishu",
      "userId": "ou_xxx",
      "chatId": "oc_xxx",
      "messageId": "om_xxx"
    },
    "target": {
      "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
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
          "text": "请结合图片内容，列一下当前目录下的文件。",
          "textElements": []
        }
      ]
    },
    "overrides": {
      "approvalPolicy": null,
      "sandboxPolicy": null,
      "model": null,
      "effort": null,
      "collaborationMode": null
    }
  }
}
```

### 6.7.4 `agent.command.ack`

wrapper 对命令翻译接收的确认。

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.command.ack",
  "messageId": "ack-71",
  "sentAt": "2026-04-03T12:18:34.005Z",
  "instanceId": "inst_codex_01",
  "commandId": "cmd-71",
  "status": "accepted"
}
```

拒绝例子：

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.command.ack",
  "messageId": "ack-72",
  "sentAt": "2026-04-03T12:18:34.010Z",
  "instanceId": "inst_codex_01",
  "commandId": "cmd-72",
  "status": "rejected",
  "reason": {
    "code": "thread_not_known",
    "message": "target thread is not known to the adapter",
    "retryable": false
  }
}
```

`status = rejected` 的错误码冻结为：

| code | 语义 |
| --- | --- |
| `unsupported_command` | adapter 不支持该 canonical command |
| `invalid_payload` | 字段缺失或字段组合非法 |
| `unsupported_input_kind` | adapter 不支持某种结构化输入 |
| `thread_not_known` | 目标 thread 不存在于 adapter 已知范围 |
| `thread_not_loaded` | adapter 不支持对未加载 thread 自动 resume |
| `turn_not_known` | 目标 turn 不存在 |
| `no_active_turn` | 当前没有可中断 turn |
| `request_not_known` | requestId 不存在 |
| `request_already_resolved` | request 已关闭 |
| `adapter_not_ready` | adapter 尚未完成初始化或同步 |
| `native_protocol_error` | adapter 无法构造合法原生命令 |
| `internal_error` | adapter 内部异常 |

说明：

- 长操作的完成不通过 `command.result` 报告
- 真正的执行结果统一通过 canonical event 返回
- `turn.started` / `item.*` / `turn.completed` 才是命令成效的权威来源

## 6.8 Debug Native Frame

这是 debug/replay 通道，不进入 bot 主链路。

```json
{
  "protocol": "relay.agent.v1",
  "type": "agent.debug.native_frame",
  "messageId": "native-2017",
  "sentAt": "2026-04-03T12:18:33.181Z",
  "instanceId": "inst_codex_01",
  "frame": {
    "seq": 2017,
    "direction": "client_to_agent",
    "raw": "{\"method\":\"thread/resume\",\"params\":{\"threadId\":\"019d5102-0cce-75d1-ad30-e4350b353cd3\",\"cwd\":\"/data/dl/droid\"}}\\n",
    "parsed": {
      "method": "thread/resume",
      "params": {
        "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
        "cwd": "/data/dl/droid"
      }
    }
  }
}
```

server 必须分别保留：

- native frame log
- canonical event log
- render event log

## 7. relay.control.v1

这是 bot 使用的控制 API。

它是用户中心的，而不是协议中心的。

2026-04-03 补充：

- 这里原来的“按 `userId` 直接建模”的草案，已经不足以承载飞书端的
  attach / use-thread / 队列 / 图片 / reaction 取消语义
- 对新的 bot 产品层，请以
  [feishu-product-design.md](./feishu-product-design.md) 里的
  `surfaceSessionId` 模型为准
- 本节仍然保留对错误体、thread 路由、append-only render 的通用约束

## 7.1 通用错误体

所有 `4xx/5xx` 响应使用：

```json
{
  "error": {
    "code": "thread_not_found",
    "message": "target thread does not belong to the current attachment"
  }
}
```

错误码冻结为：

| code | HTTP | 语义 |
| --- | --- | --- |
| `invalid_body` | 400 | 请求体非法 |
| `invalid_query` | 400 | 查询参数非法 |
| `not_attached` | 409 | 用户当前未 attach 到任何 instance |
| `instance_not_found` | 404 | instance 不存在 |
| `instance_offline` | 409 | instance 已离线 |
| `instance_attached_by_other_user` | 409 | instance 已被其他用户接管 |
| `thread_not_found` | 404 | thread 不属于当前 attachment 或根本不存在 |
| `thread_not_selectable` | 409 | thread 当前不可用 |
| `request_not_found` | 404 | requestId 不存在 |
| `request_closed` | 409 | request 已被解决 |
| `no_active_turn` | 409 | 当前没有可中断 turn |
| `command_rejected` | 409 | wrapper 拒绝了 canonical command |
| `relay_unavailable` | 503 | server 无法联系目标 wrapper |

## 7.2 快照接口

### 7.2.1 `GET /v1/users/:userId/snapshot`

返回 bot 所需控制面快照。

```ts
type AttachmentSummary = {
  instanceId: string;
  displayName: string;
  selectedThreadId: string | null;
  routeMode: "pinned" | "follow_local" | "unbound";
};

type InstanceSummary = {
  instanceId: string;
  displayName: string;
  workspaceRoot: string | null;
  online: boolean;
  state: "idle" | "running" | "waiting_request" | "offline";
  attachedByUserId: string | null;
  observedFocusedThreadId: string | null;
};

type ThreadSummary = {
  threadId: string;
  name: string | null;
  preview: string | null;
  cwd: string | null;
  state: "idle" | "running" | "waiting_request";
  loaded: boolean | null;
  isObservedFocused: boolean;
  isSelected: boolean;
};

type SnapshotResponse = {
  surfaceSessionId: string;
  actorUserId: string;
  attachment: AttachmentSummary | null;
  instances: Array<InstanceSummary>;
  threads: Array<ThreadSummary>;
};
```

约束：

- `instances` 默认只返回在线 instance
- `threads` 只返回当前 attachment 对应 instance 的 thread 列表
- `threads` 默认按 `updatedAt desc` 排序
- `threads` 默认只返回 `archived != true`

```json
{
  "surfaceSessionId": "feishu:chat:oc_xxx",
  "actorUserId": "ou_xxx",
  "attachment": {
    "instanceId": "inst_codex_01",
    "displayName": "droid",
    "selectedThreadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
    "routeMode": "pinned"
  },
  "instances": [
    {
      "instanceId": "inst_codex_01",
      "displayName": "droid",
      "workspaceRoot": "/data/dl/droid",
      "online": true,
      "state": "running",
      "attachedByUserId": "ou_xxx",
      "observedFocusedThreadId": "019d5102-0cce-75d1-ad30-e4350b353cd3"
    }
  ],
  "threads": [
    {
      "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
      "name": "列一下当前目录文件",
      "preview": "请列一下当前目录下的文件",
      "cwd": "/data/dl/droid",
      "state": "idle",
      "loaded": true,
      "isObservedFocused": true,
      "isSelected": true
    }
  ]
}
```

## 7.3 控制动作接口

### 7.3.1 `POST /v1/users/:userId/attach`

请求：

```json
{
  "instanceId": "inst_codex_01",
  "chatId": "oc_xxx"
}
```

语义：

- 如果该用户已 attach 到别的 instance，原 attachment 原子地 detach
- 如果目标 instance 已被其他用户 attach，返回 `409 instance_attached_by_other_user`
- 新的产品层语义里，attach 时若当前已有 `observedFocusedThreadId`，应立即 pin 为默认输入目标
- 若 attach 时没有 observed focus，则进入 `unbound`
- 成功后返回新的 snapshot

### 7.3.2 `POST /v1/users/:userId/detach`

请求体可为空。

语义：

- detach 当前 attachment
- 如果本来没 attach，返回幂等成功
- 成功后返回新的 snapshot

### 7.3.3 `POST /v1/users/:userId/use-thread`

请求：

```json
{
  "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3"
}
```

仅清空显式 pin 时：

```json
{
  "threadId": null
}
```

语义：

- 仅修改 `selectedThreadId`
- 不要求本地 VS Code UI 跟着切换
- `threadId = null` 只表示清空显式 pin
- 清空后到底是 `follow_local` 还是 `unbound`，应由产品层显式 `routeMode` 决定
- 成功后返回新的 snapshot

### 7.3.4 `POST /v1/users/:userId/prompt`

请求：

```json
{
  "text": "请列一下当前目录下的文件",
  "chatId": "oc_xxx",
  "messageId": "om_xxx"
}
```

成功响应：

```json
{
  "accepted": true,
  "commandId": "cmd-71",
  "route": {
    "mode": "selected_thread",
    "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3"
  }
}
```

```ts
type PromptAcceptedResponse = {
  accepted: true;
  commandId: string;
  route: {
    mode: "selected_thread" | "observed_focus" | "new_thread";
    threadId: string | null;
  };
};
```

server 路由规则冻结为：

1. `selectedThreadId`
2. 仅当 `routeMode = follow_local` 时才看 `observedFocusedThreadId`
3. 否则创建新 thread

额外规则：

- 命中 `selectedThreadId` 时，后续继续使用该 thread
- 命中 `observedFocusedThreadId` 时，只有显式 follow-local 模式才合法
- 走“创建新 thread”分支时，成功创建后必须把新的 thread 自动写回 `selectedThreadId`

### 7.3.5 `POST /v1/users/:userId/interrupt`

请求体可为空。

语义：

- 若 attachment 对应 instance 存在 active turn，则中断该 turn
- 若没有 active turn，返回 `409 no_active_turn`

```ts
type InterruptAcceptedResponse = {
  accepted: true;
  commandId: string;
  threadId: string | null;
  turnId: string | null;
};
```

### 7.3.6 `POST /v1/users/:userId/requests/:requestId/respond`

approval 示例：

```json
{
  "response": {
    "type": "approval",
    "approved": true
  }
}
```

user-input 示例：

```json
{
  "response": {
    "type": "structured",
    "result": {
      "answers": {
        "environment": {
          "answers": [
            "Remote"
          ]
        }
      }
    }
  }
}
```

```ts
type RequestRespondAcceptedResponse = {
  accepted: true;
  commandId: string;
  requestId: string | number;
};
```

## 8. relay.render.v1

## 8.1 Render Event 基类

```ts
type RenderEventBase = {
  protocol: "relay.render.v1";
  eventId: number;
  occurredAt: string;
  kind: string;
  surfaceSessionId: string;
  actorUserId: string;
  instanceId: string | null;
  threadId: string | null;
  turnId: string | null;
};
```

## 8.2 Render Event Feed

### 8.2.1 `GET /v1/users/:userId/render-events?after=<cursor>`

返回用户层事件。

```json
{
  "latestEventId": 412,
  "events": [
    {
      "protocol": "relay.render.v1",
      "eventId": 410,
      "occurredAt": "2026-04-03T12:18:35.000Z",
      "kind": "turn.state",
      "surfaceSessionId": "feishu:chat:oc_xxx",
      "actorUserId": "ou_xxx",
      "instanceId": "inst_codex_01",
      "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
      "turnId": "turn-2",
      "state": "running",
      "message": null
    },
    {
      "protocol": "relay.render.v1",
      "eventId": 411,
      "occurredAt": "2026-04-03T12:18:36.000Z",
      "kind": "block.committed",
      "surfaceSessionId": "feishu:chat:oc_xxx",
      "actorUserId": "ou_xxx",
      "instanceId": "inst_codex_01",
      "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
      "turnId": "turn-2",
      "block": {
        "blockId": "blk_411",
        "groupId": "grp_turn-2_item-1",
        "sourceItemId": "item-1",
        "sequence": 1,
        "kind": "assistant_markdown",
        "title": null,
        "body": "我先列一下当前目录文件。",
        "language": null,
        "final": true,
        "sessionColorKey": "inst_codex_01"
      }
    },
    {
      "protocol": "relay.render.v1",
      "eventId": 412,
      "occurredAt": "2026-04-03T12:18:40.000Z",
      "kind": "turn.state",
      "surfaceSessionId": "feishu:chat:oc_xxx",
      "actorUserId": "ou_xxx",
      "instanceId": "inst_codex_01",
      "threadId": "019d5102-0cce-75d1-ad30-e4350b353cd3",
      "turnId": "turn-2",
      "state": "completed",
      "message": null
    }
  ]
}
```

## 8.3 Render Event 类型

第一版 bot 只需要下面这些：

- `snapshot.changed`
- `thread.selection.changed`
- `turn.state`
- `request.opened`
- `request.closed`
- `block.committed`
- `notice`
- `error`

```ts
type SnapshotChangedEvent = RenderEventBase & {
  kind: "snapshot.changed";
  snapshot: SnapshotResponse;
};

type ThreadSelectionChangedEvent = RenderEventBase & {
  kind: "thread.selection.changed";
  selectedThreadId: string | null;
  observedFocusedThreadId: string | null;
  routeMode: "pinned" | "follow_local" | "unbound";
};

type TurnStateEvent = RenderEventBase & {
  kind: "turn.state";
  state: "running" | "waiting_request" | "completed" | "failed" | "interrupted";
  message: string | null;
};

type RequestOpenedEvent = RenderEventBase & {
  kind: "request.opened";
  request: {
    requestId: string | number;
    requestKind: "approval_command" | "approval_file_change" | "user_input" | "tool_call" | "other";
    title: string;
    body: string;
    structured: Record<string, unknown>;
  };
};

type RequestClosedEvent = RenderEventBase & {
  kind: "request.closed";
  requestId: string | number;
  resolution: "approved" | "denied" | "submitted" | "cancelled" | "unknown";
};

type BlockCommittedEvent = RenderEventBase & {
  kind: "block.committed";
  block: RenderBlock;
};

type NoticeEvent = RenderEventBase & {
  kind: "notice";
  code: string;
  message: string;
};

type RenderErrorEvent = RenderEventBase & {
  kind: "error";
  code: string;
  message: string;
};
```

## 8.4 Render Block

```ts
type RenderBlockKind =
  | "assistant_markdown"
  | "assistant_code"
  | "tool_summary"
  | "tool_output"
  | "plan"
  | "reasoning_summary"
  | "approval"
  | "status"
  | "error";

type RenderBlock = {
  blockId: string;
  groupId: string;
  sourceItemId: string | null;
  sequence: number;
  kind: RenderBlockKind;
  title: string | null;
  body: string;
  language: string | null;
  final: boolean;
  sessionColorKey: string;
};
```

规则：

- `groupId` 代表同一个 source item 或同一个 request 产生的一组 block
- 同一 `groupId` 内 `sequence` 严格递增
- 不允许跨 item 合并 `groupId`
- 不允许跨 turn 合并 `groupId`

## 8.5 Render Block 冻结规则

第一版冻结如下：

- bot 只接收不可变的 committed block
- 不要求 bot 处理 block update / replace
- server 只有在 block 足够稳定时才发 `block.committed`
- 不允许把整个 turn 的所有文本攒成一条再发

这意味着：

- Feishu 侧可以保持实现简单
- 不依赖卡片更新能力
- 不会再次把流式切分复杂度压回 bot
- 长 turn 仍然可以分多块渐进上屏

## 8.6 默认可见性矩阵

| itemKind | exposure | bot 默认行为 |
| --- | --- | --- |
| `assistant_message` | `visible` | 进入 render planner，输出 1..N 个 block |
| `command` | `visible` | 独立 tool block，不与 assistant 混合 |
| `file_change` | `visible` | 独立 block，通常 `assistant_code/diff` |
| `tool_call` | `visible` | 独立 block 或 summary |
| `web_search` | `visible` | 独立 summary block |
| `plan` | `status_fallback` | 默认不直接显示原文，仅必要时降级成状态提示 |
| `reasoning` | `debug_only` | 不进入普通 bot feed |
| `collab_agent_call` | `status_fallback` | 可降级为状态提示 |
| `review_state` | `status_fallback` | 可降级为状态提示 |
| `context_compaction` | `debug_only` | 只进 debug |

## 8.7 Render Planner 规则

这部分是这次设计最关键的部分。

### 8.7.1 强规则

- 一个 `command` item 不得和 `assistant_message` item 混成一个 block
- 一个 `approval` request 不得和普通正文混成一个 block
- 一个 `item` 的 block 不得跨到下一个 `item`
- `turn.failed` 必须发明确的 `error` 或 `turn.state=failed`
- 空白内容不得单独产出 block

### 8.7.2 不依赖“语义句法分类”

planner 不应依赖“这句话像不像进度句”这类纯语义分类器。

v1 的切分原则是：

- 先用原生协议给出的 item 边界做强切分
- 只在 `assistant_message` item 内做结构型切分
- 结构型切分优先看：
  - fenced code block
  - blank line
  - dense line group
  - 段落边界
  - item completed

也就是说，v1 的主要依据是“文本结构”，不是“句子语气”。

### 8.7.3 Assistant Message 软切分规则

一个 `assistant_message` item 可以被切成多个 block，但只能在 server 里做。

冻结规则：

1. 先按 item 做强切分
2. 只对 `assistant_message` item 做文本软切分
3. fenced code block 必须完整保留
4. dense 文件列表可转为 `assistant_code`
5. 前导短段落只有在后面跟着明显的结构块时，才允许单独提交
6. 尾段若非空，必须保留，不能被丢弃
7. 纯空白段落不得生成空消息
8. newline-only delta 要保留在 item buffer 中，但不能直接变成独立 block

### 8.7.4 渐进提交条件

server 对 `assistant_message` item 的提交单位不是“整 turn”，而是“稳定的子结构”：

- 一个短前导段落在后面出现 fenced block / dense list 前，可以先提交
- fenced block 必须等闭合后才能提交
- dense list 必须等列表结束或 item 完成后才能提交
- 若整个 item 没有明显结构块，则在 item 完成时整体提交

### 8.7.5 典型结果

下面这种情况应切成多个 committed block：

- 前导一句说明
- 文件列表代码块
- 收尾建议

下面这种情况不应硬拆：

- 连续两三段普通解释文字，中间没有明确结构边界

## 9. 状态模型

## 9.1 InstanceRecord

```ts
type InstanceRecord = {
  instanceId: string;
  adapterFamily: string;
  displayName: string;
  workspaceRoot: string | null;
  defaultCwd: string | null;
  online: boolean;
  state: "idle" | "running" | "waiting_request" | "offline";
  attachedByUserId: string | null;
  observedFocusedThreadId: string | null;
  activeTurnId: string | null;
  activeThreadId: string | null;
  connectionId: string | null;
  lastSeq: number;
};
```

## 9.2 ThreadRecord

```ts
type ThreadRecord = {
  threadId: string;
  instanceId: string;
  name: string | null;
  preview: string | null;
  cwd: string | null;
  createdAt: number | null;
  updatedAt: number | null;
  archived: boolean | null;
  loaded: boolean | null;
  state: "idle" | "running" | "waiting_request";
  lastTurnId: string | null;
};
```

## 9.3 AttachmentRecord

```ts
type AttachmentRecord = {
  surfaceSessionId: string;
  actorUserId: string;
  instanceId: string;
  selectedThreadId: string | null;
  routeMode: "pinned" | "follow_local" | "unbound";
  chatId: string | null;
  attachedAt: string;
};
```

## 9.4 PendingRequestRecord

```ts
type PendingRequestRecord = {
  requestId: string | number;
  instanceId: string;
  threadId: string | null;
  turnId: string | null;
  itemId: string | null;
  requestKind: "approval_command" | "approval_file_change" | "user_input" | "tool_call" | "other";
  structured: Record<string, unknown>;
};
```

## 9.5 Prompt 路由与线程选择规则

server 收到飞书 prompt 时，目标 thread 的解析规则冻结为：

1. 若 `attachment.selectedThreadId != null`
   - 直接使用该 thread
2. 否则若 `attachment.routeMode = follow_local` 且 `instance.observedFocusedThreadId != null`
   - 跟随该 thread
3. 否则
   - 新建 thread

补充规则：

- 后台 thread 的输出不允许改变 `observedFocusedThreadId`
- `turn.completed` 不允许改变 `observedFocusedThreadId`
- attach 当下若已有 observed focus，产品层应先把它 pin 到 `selectedThreadId`
- 远端新建 thread 成功后，server 必须自动把该 thread 写入 `selectedThreadId`
- 只有显式 `follow_local` 模式下，才允许借用 `observedFocusedThreadId` 发 prompt

## 10. 控制流

## 10.1 注册与初始同步

```text
wrapper -> server : agent.hello
server  -> wrapper: agent.welcome
server  -> wrapper: agent.command(threads.refresh scope=loaded)
wrapper -> native : thread/loaded/list
wrapper -> native : thread/read (for each loaded thread)
wrapper -> server : agent.event.batch(threads.snapshot / thread.focused ...)
server  -> state  : 更新 instance / thread snapshot
```

说明：

- `threads.snapshot` 是 loaded thread 的全量替换快照
- 这一步是为了避免“server 根本不知道当前有哪些 thread”
- 若 adapter 不支持初始主动枚举，必须在 `capabilities` 中显式声明

## 10.2 attach

```text
bot    -> server : POST /v1/users/:userId/attach { instanceId }
server -> state  : 若用户已有 attachment，先原子 detach 旧 attachment
server -> state  : 若目标 instance 被他人占用，返回 409
server -> state  : 若当前已有 observedFocusedThreadId，则 selectedThreadId = observedFocusedThreadId, routeMode = pinned
server -> state  : 否则 routeMode = unbound
server -> bot    : 返回 snapshot
```

## 10.3 use-thread

```text
bot    -> server : POST /v1/users/:userId/use-thread { threadId | null }
server -> state  : 更新 selectedThreadId
server -> bot    : 返回 snapshot
```

`use-thread` 只改 server 侧远端路由状态，不强制切换本地 VS Code UI。

产品层若需要重新进入“跟随本地 VS Code”模式，应显式设置 `routeMode = follow_local`。

## 10.4 prompt 发送到已有 thread

```text
bot     -> server  : POST /v1/users/:userId/prompt
server  -> state   : target = selectedThreadId || (routeMode == follow_local ? observedFocusedThreadId : null)
server  -> wrapper : agent.command(prompt.send)
wrapper -> server  : agent.command.ack(accepted)
wrapper -> native  : turn/start
native  -> wrapper : turn/started
wrapper -> server  : turn.started
native  -> wrapper : item/started(agentMessage)
wrapper -> server  : item.started(assistant_message)
native  -> wrapper : item/agentMessage/delta ...
wrapper -> server  : item.delta ...
server  -> planner : 累积并切分 assistant item
server  -> bot     : block.committed(...)
native  -> wrapper : item/completed
wrapper -> server  : item.completed
native  -> wrapper : turn/completed
wrapper -> server  : turn.completed
server  -> bot     : turn.state(completed)
```

## 10.5 prompt 发送到已知但未加载的 thread

```text
bot     -> server  : POST /v1/users/:userId/prompt
server  -> wrapper : agent.command(prompt.send target.threadId=X)
wrapper -> server  : agent.command.ack(accepted)
wrapper -> native  : thread/resume(threadId=X)
native  -> wrapper : thread/started or thread/resume response
wrapper -> server  : thread.discovered / thread.focused
wrapper -> native  : turn/start(threadId=X)
... 后续同普通 prompt
```

adapter 若不支持这个分支，必须在 `agent.command.ack` 中返回 `thread_not_loaded` 或 `thread_not_known`。

## 10.6 prompt 需要新 thread

```text
bot     -> server  : POST /v1/users/:userId/prompt
server  -> state   : 无 selectedThreadId，且当前不在可用的 follow_local 路由上
server  -> wrapper : agent.command(prompt.send target.threadId=null createThreadIfMissing=true)
wrapper -> server  : agent.command.ack(accepted)
wrapper -> native  : thread/start
native  -> wrapper : thread/started
wrapper -> server  : thread.discovered
wrapper -> server  : thread.focused(threadId=new, focusSource=remote_created_thread)
server  -> state   : selectedThreadId = new threadId, routeMode = pinned
wrapper -> native  : turn/start
... 后续同普通 prompt
```

## 10.7 本地 thread 切换

```text
native/local-ui -> wrapper : thread/resume
wrapper         -> server  : thread.focused(threadId=X, focusSource=local_ui)
server          -> state   : observedFocusedThreadId = X
server          -> bot     : thread.selection.changed(observedFocused=X)
```

规则冻结：

- 若 attachment 处于 `follow_local`，后续 prompt 跟随 `observedFocusedThreadId`
- 若 attachment 已显式 pin 某个 `selectedThreadId`，不覆盖该选择
- 后台 turn 的完成、失败、输出，不能把 prompt 路由重新指到别的 thread

## 10.7.1 本地交互与远端 queue 的仲裁

这是“不 auto detach，但又不允许本地和飞书互相抢 turn”的关键规则。

```text
native/local-ui -> wrapper : turn/start 或 turn/steer
wrapper         -> server  : local.interaction.observed(action=...)
server          -> state   : 对当前 attachment/surface 进入 paused_for_local，并刷新 local priority lease
server          -> bot     : notice(code=local_activity_detected)
bot/feishu      -> server  : 后续 prompt 继续入队，但不 autosend
```

若后续本地请求实际起了新 turn：

```text
native/local-ui -> wrapper : turn/start
native          -> wrapper : turn/started
wrapper         -> server  : turn.started(initiator=local_ui)
server          -> state   : activeTurnOrigin = local_ui
```

恢复规则：

```text
wrapper -> server : turn.completed(initiator=local_ui)
server  -> state  : 进入短暂 handoff_wait
若窗口内又观察到 local.interaction.observed
  -> 继续 paused_for_local
若窗口结束仍未观察到新的本地交互
  -> 恢复远端 autosend
  -> notice(code=remote_queue_resumed)
```

补充约束：

- server 只能管理飞书侧 queue，不能管理本地客户端自己的 draft / queue
- 因此 turn 完成后，server 不能立即假设“下一轮一定属于飞书”
- `local.interaction.observed` 是“暂停远端 autosend”的最早权威信号
- `turn.started.initiator` 是“确认这轮 turn 归属”的权威信号
- 若当前活动 turn 是远端发起，但本地观察到了 `turn/steer`
  - server 仍应继续暂停远端后续队列
  - 但不能把已经在运行的这轮 turn 重新改写为 `local_ui`

## 10.8 approval 请求

```text
native  -> wrapper : item/commandExecution/requestApproval
wrapper -> server  : request.started(kind=approval_command)
server  -> bot     : request.opened
bot     -> server  : POST /v1/users/:userId/requests/:requestId/respond
server  -> wrapper : agent.command(request.respond)
wrapper -> native  : JSON-RPC response
wrapper -> server  : request.resolved
server  -> bot     : request.closed
```

## 10.9 request-user-input

流程同 approval，但 request kind 为 `user_input`，响应内容是 structured result。

## 10.10 interrupt

```text
bot     -> server  : POST /v1/users/:userId/interrupt
server  -> wrapper : agent.command(turn.interrupt)
wrapper -> server  : agent.command.ack(accepted)
wrapper -> native  : turn/interrupt
native  -> wrapper : turn/completed(status=interrupted)
wrapper -> server  : turn.completed(status=interrupted)
server  -> bot     : turn.state(interrupted)
```

## 10.11 turn 失败

```text
native  -> wrapper : turn/completed(status=failed,error.message=...)
wrapper -> server  : turn.completed(status=failed,errorMessage=...)
server  -> bot     : error 或 turn.state(failed)
```

## 10.12 断线重连

```text
wrapper disconnects
server  : instance.online = false
server  : 保留 snapshot / attachment / debug logs 到 grace period 结束
same wrapper process reconnects with same instanceId
server  : 复用旧 instance record
server  : 接收更大的 seq
server  : 必要时触发 threads.refresh
```

规则冻结：

- 只有“同一个 wrapper 进程 + 同一个 `instanceId`”才视为恢复
- wrapper 进程重启后出现新的 `instanceId`，server 必须视为新 instance

## 11. 观测到的 Codex 原生样例

下面这些样例来自仓库现有测试中保留的真实或准真实负载，用来约束 adapter 设计。

## 11.1 本地 `thread/resume`

```json
{
  "method": "thread/resume",
  "params": {
    "threadId": "thread-selected",
    "cwd": "/tmp/selected"
  }
}
```

这个样例已经存在于：

- [ws_client.rs](/data/dl/fschannel/wrapper/src/ws_client.rs#L2049)

## 11.2 本地 `thread/start`

```json
{
  "id": "local-thread-start",
  "method": "thread/start",
  "params": {
    "cwd": "/tmp/project",
    "model": "gpt-5.2-codex",
    "modelProvider": "codex_vscode_copilot",
    "serviceTier": "flex",
    "approvalsReviewer": "guardian_subagent",
    "config": {
      "model_provider": "codex_vscode_copilot"
    },
    "serviceName": "codex_vscode",
    "approvalPolicy": "on-request",
    "baseInstructions": null,
    "developerInstructions": null,
    "sandbox": "workspace-write",
    "personality": "pragmatic",
    "ephemeral": null,
    "experimentalRawEvents": false,
    "dynamicTools": null
  }
}
```

来源：

- [ws_client.rs](/data/dl/fschannel/wrapper/src/ws_client.rs#L1606)

## 11.3 本地 `turn/start`

```json
{
  "method": "turn/start",
  "params": {
    "threadId": "thread-local",
    "input": [
      {
        "type": "text",
        "text": "hello",
        "text_elements": []
      }
    ],
    "cwd": "/tmp/project",
    "approvalPolicy": "on-request",
    "sandboxPolicy": {
      "type": "workspaceWrite",
      "writableRoots": [
        "/tmp/project"
      ],
      "excludeSlashTmp": false,
      "excludeTmpdirEnvVar": false,
      "networkAccess": false
    },
    "model": null,
    "effort": null,
    "summary": "auto",
    "personality": "pragmatic",
    "outputSchema": null,
    "collaborationMode": {
      "mode": "custom",
      "settings": {
        "model": "gpt-5.2-codex",
        "reasoning_effort": "medium",
        "developer_instructions": null
      }
    },
    "attachments": []
  }
}
```

来源：

- [ws_client.rs](/data/dl/fschannel/wrapper/src/ws_client.rs#L1610)

补充说明：

- 官方 app-server 还存在 `turn/steer`
- 它表示“向当前正在执行的 turn 追加新的用户输入”
- `turn/steer` 不应被当成新 turn 边界
- adapter 必须保留这类本地交互信号，不能因为当前 v1 还不下发远端 steer 就直接忽略

## 11.4 assistant item 边界

先有第一段 delta：

```json
{
  "method": "item/agentMessage/delta",
  "params": {
    "delta": "先确认这个工程对应的项目根目录。"
  }
}
```

随后出现一个明确的 item 完成边界：

```json
{
  "method": "item/completed",
  "params": {
    "threadId": "thread-1",
    "turnId": "turn-2",
    "item": {
      "id": "item-1",
      "type": "agentMessage"
    }
  }
}
```

然后才开始下一段 assistant 文本：

```json
{
  "method": "item/agentMessage/delta",
  "params": {
    "delta": "我先按 fschannel 这个工程统计了。"
  }
}
```

来源：

- [bot-service.test.ts](/data/dl/fschannel/bot/src/bot-service.test.ts#L1095)

这个例子证明：

- 不同 assistant block 的硬边界可以来自 item 生命周期
- 不必把所有 assistant delta 盲目拼成一个长字符串

## 11.5 request-user-input

```json
{
  "id": "req-input-1",
  "method": "item/tool/requestUserInput",
  "params": {
    "threadId": "thread-1",
    "turnId": "turn-1",
    "itemId": "item-1",
    "questions": [
      {
        "id": "environment",
        "header": "Environment",
        "question": "Where should this run?",
        "isOther": true,
        "isSecret": false,
        "options": [
          {
            "label": "Local",
            "description": "Use the local machine"
          },
          {
            "label": "Remote",
            "description": "Use the remote host"
          }
        ]
      }
    ]
  }
}
```

来源：

- [bot-service.test.ts](/data/dl/fschannel/bot/src/bot-service.test.ts#L1883)

## 11.6 turn failed

```json
{
  "method": "turn/completed",
  "params": {
    "threadId": "thread-1",
    "turn": {
      "id": "turn-2",
      "status": "failed",
      "error": {
        "message": "unexpected status 503 Service Unavailable: Service temporarily unavailable"
      }
    }
  }
}
```

来源：

- [bot-service.test.ts](/data/dl/fschannel/bot/src/bot-service.test.ts#L1388)

## 12. 文件列表示例

这是当前最关心的展示案例。

原始 assistant delta 流可能类似：

```json
[
  { "delta": "我先列一下当前目录文件。" },
  { "delta": "/data/dl 顶层内容如下（包含隐藏文件和目录，不含 . 和 ..）：" },
  { "delta": "\n\n" },
  { "delta": "```text" },
  { "delta": "\n" },
  { "delta": "README.md" },
  { "delta": "\n" },
  { "delta": "package.json" },
  { "delta": "\n" },
  { "delta": "src" },
  { "delta": "\n" },
  { "delta": "tsconfig.json" },
  { "delta": "\n" },
  { "delta": "```" },
  { "delta": "\n\n" },
  { "delta": "如果你要，我可以继续按“仅普通文件”“仅目录”或“包含隐藏文件”再列一版。" }
]
```

server renderer 期望产出的 render block：

```json
[
  {
    "kind": "assistant_markdown",
    "body": "我先列一下当前目录文件。"
  },
  {
    "kind": "assistant_code",
    "title": "/data/dl 顶层内容如下（包含隐藏文件和目录，不含 . 和 ..）：",
    "body": "README.md\npackage.json\nsrc\ntsconfig.json",
    "language": "text"
  },
  {
    "kind": "assistant_markdown",
    "body": "如果你要，我可以继续按“仅普通文件”“仅目录”或“包含隐藏文件”再列一版。"
  }
]
```

这个例子说明：

- canonical model 只需要保证 item 级硬边界和文本完整性
- 更细的块拆分由 server renderer 完成
- bot 不需要再理解原始 delta
- 空白换行必须保留在 buffer 里，但不能变成空消息
- 代码块后的尾注必须保留，不能在切分时丢失

## 13. 已定稿结论

实现前需要的关键协议决策，这一版已经定稿为：

1. bot 主链路只接 `relay.render.v1`
2. wrapper 对 server 只暴露统一 canonical event / command
3. `thread / turn / item / request` 是硬边界
4. `assistant_message` 内部切分是 server renderer 的职责
5. render block 在 v1 是 append-only committed 模型
6. `reasoning` 默认只进 debug
7. `plan` 默认只做状态降级，不直接当正文发送
8. `attach(instance)` 和 `use-thread(thread)` 是两个独立动作
9. 产品层是否跟随本地 focused thread，必须由显式 `routeMode` 表达
10. reconnect 只支持同一 wrapper 进程复用 `instanceId`

在这些决策不再变化的前提下，后续可以再开始编码。

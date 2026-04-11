# Codex MCP App-Server 协议基线

> Type: `general`
> Updated: `2026-04-11`
> Summary: 基于 upstream `openai/codex` HEAD `7999b0f60f048ca8398ec988f3aabd09d278e058` 梳理 MCP tool call、授权确认与 elicitation 的 app-server 时序，并对照当前仓库偏差。

## 1. 文档定位

这份文档记录的是截至 `2026-04-11` 已确认的 **Codex app-server 中与 MCP call 相关的协议基线**，用于指导本仓库后续的 translator、orchestrator、Feishu remote surface 设计。

本次核对的 upstream 基线为：

- 仓库：`openai/codex`
- HEAD：`7999b0f60f048ca8398ec988f3aabd09d278e058`
- 时间：`2026-04-10T16:05:21-07:00`
- subject：`Support clear SessionStart source (#17073)`

本文关注的是：

- app-server client 到 Codex CLI 的正确交互面与时序
- MCP 相关授权确认流程到底拆成了哪些线
- 本仓库当前处理逻辑和 upstream 基线的偏差点

本文不直接规定飞书端最终 UI，但它是后续实现拆分的协议依据。

## 2. 上游协议面总览

截至当前 upstream，和 MCP call 直接相关的 app-server surface 不是单一路径，而是至少包含下面六类：

| 类别 | upstream surface | 作用 | client 是否需要回写 |
| --- | --- | --- | --- |
| MCP item 生命周期 | `item/started` + `item/completed` 携带 `ThreadItem::McpToolCall` | 表示 MCP tool call 开始、完成、失败，以及最终结果/错误 | 否 |
| MCP 过程进度 | `item/mcpToolCall/progress` | 在 tool call 运行中追加轻量进度消息 | 否 |
| MCP elicitation | `mcpServer/elicitation/request` | 处理 MCP server 主动发起的 form/url elicitation | 是 |
| permissions approval | `item/permissions/requestApproval` | 请求附加权限授权 | 是 |
| guardian review 可见性 | `item/autoApprovalReview/started` / `item/autoApprovalReview/completed` | 暴露 guardian 对高风险动作的审查生命周期，`action.type` 可为 `mcpToolCall` | 否 |
| MCP tool 执行审批 fallback | `item/tool/requestUserInput` | 在某些模式下，MCP tool 执行审批并不走 `mcpServer/elicitation/request`，而是退回为 `request_user_input` | 是 |

这意味着“把 MCP 授权确认做好”至少要同时覆盖三条确认线：

- MCP tool 执行审批
- permissions approval
- MCP elicitation

并且还要补一条可见性线：

- guardian 对 MCP tool call 的审查通知

## 3. 核心类型与通知

### 3.1 `ThreadItem::McpToolCall`

upstream 在 `codex-rs/app-server-protocol/src/protocol/v2.rs` 中把 MCP tool call 定义为一等 item：

- `type = "mcpToolCall"`
- `id`
- `server`
- `tool`
- `status`
- `arguments`
- `result`
- `error`
- `durationMs`

其中：

- `status` 只有 `inProgress` / `completed` / `failed`
- `result` 包含：
  - `content`
  - `structuredContent`
  - `_meta`
- `error` 当前只有 `message`

这条 item 生命周期由 core 的 `McpToolCallBegin` / `McpToolCallEnd` 事件驱动，再由 app-server 映射成 `item/started` / `item/completed`。

### 3.2 `item/mcpToolCall/progress`

当前 upstream 还定义了独立通知：

- `method = "item/mcpToolCall/progress"`
- 字段：
  - `threadId`
  - `turnId`
  - `itemId`
  - `message`

这说明 MCP 过程状态不只靠 begin/end 两段，也可以在运行中补充细粒度进度信息。

### 3.3 `mcpServer/elicitation/request`

当前 app-server 的 MCP elicitation 是独立 server request，不是旧的泛化 approval。

请求参数包含：

- `threadId`
- `turnId: Option<String>`
- `serverName`
- `request`

其中 `request` 分两种：

- `mode = "form"`
  - `_meta`
  - `message`
  - `requestedSchema`
- `mode = "url"`
  - `_meta`
  - `message`
  - `url`
  - `elicitationId`

响应固定是：

- `action = accept | decline | cancel`
- `content`
- `_meta`

当前 upstream 还有一个重要限制：

- `McpServerElicitationRequestParams` 里还没有 `itemId`
- upstream 源码明确写了 TODO：当前 core 还不能把 elicitation 精确关联到某个 `McpToolCall` item id

因此，生产逻辑不能假设“所有 elicitation 都能精确绑定到某个 MCP item”。

### 3.4 `item/permissions/requestApproval`

permissions approval 也是独立 request 类型，不应再被压平成泛化 approval。

请求参数包含：

- `threadId`
- `turnId`
- `itemId`
- `reason`
- `permissions`

响应包含：

- `permissions`
- `scope = turn | session`

这里的关键点是：

- 这是结构化权限授权，不是简单布尔确认
- client 需要回写“授予了哪些权限”和“授权作用域”

### 3.5 `item/autoApprovalReview/*`

guardian 可见性当前通过两条单独通知暴露：

- `item/autoApprovalReview/started`
- `item/autoApprovalReview/completed`

它们携带：

- `threadId`
- `turnId`
- `targetItemId`
- `review`
- `action`

其中 `action.type` 可以是：

- `command`
- `execve`
- `applyPatch`
- `networkAccess`
- `mcpToolCall`

这条线当前是 **审查可见性**，不是 client 主动回写的 request surface。

## 4. Canonical 时序

### 4.1 MCP tool call 正常执行

正常成功/失败路径应按这个顺序理解：

1. turn 已经开始运行。
2. core 触发 `McpToolCallBegin`。
3. app-server 发出 `item/started`，`item.type = "mcpToolCall"`，`status = "inProgress"`。
4. tool call 执行过程中，可能额外发出 `item/mcpToolCall/progress`。
5. core 触发 `McpToolCallEnd`。
6. app-server 发出 `item/completed`，同一个 `item.id`，状态变为 `completed` 或 `failed`，并带上 `result` 或 `error`。
7. 整个 turn 后续才会进入 `turn/completed`。

结论：

- `item/completed` 才是 MCP tool call 的 authoritative 最终状态
- `turn/completed` 是更外层的 turn 终态，不能替代 item 终态

### 4.2 permissions approval

upstream 测试已经明确了顺序：

1. client 发送 `turn/start`
2. app-server 返回 turn start response
3. app-server 发出 `item/permissions/requestApproval`
4. client 回写 `PermissionsRequestApprovalResponse`
5. app-server 发出 `serverRequest/resolved`
6. turn 继续推进，最后才 `turn/completed`

这里最重要的协议事实是：

- `serverRequest/resolved` 先于 `turn/completed`
- 如果 client 不回写，turn 会停在中间

### 4.3 MCP elicitation

form/url 两种 elicitation 的时序与 permissions approval 同一类：

1. client 发送 `turn/start`
2. app-server 返回 turn start response
3. app-server 发出 `mcpServer/elicitation/request`
4. client 回写 `{action, content, _meta}`
5. app-server 发出 `serverRequest/resolved`
6. turn 继续推进，最后才 `turn/completed`

当前额外要注意两点：

- `turnId` 可能为空，不能把它当成恒定字段
- 当前 upstream 还没有把 elicitation 和某个 `mcpToolCall.itemId` 做强绑定

### 4.4 MCP tool 执行审批

这条线最容易被误解。当前 upstream 中，MCP tool 执行审批并不总是走同一条 app-server request。

#### 路径 A：无需审批或被策略自动放行

- core 直接继续执行
- client 只会看到 MCP item 生命周期，不会看到额外审批 request

#### 路径 B：route 到 guardian

- core 进入 guardian review
- app-server 发 `item/autoApprovalReview/started`
- review 完成后发 `item/autoApprovalReview/completed`
- client 看到的是审查生命周期，不是一个自己要回写的 request

#### 路径 C：使用 MCP elicitation 作为审批承载

当 `ToolCallMcpElicitation` 特性开启时：

- core 会把 MCP tool approval 包装成 `mcpServer/elicitation/request`
- client 回写的线仍然是 `{action, content, _meta}`
- 但 core 会进一步把 `content` / `_meta` 解析成：
  - `accept`
  - `accept for session`
  - `accept and remember`
  - `decline`
  - `cancel`

也就是说：

- wire response 仍是 MCP elicitation response
- 业务语义却是 MCP tool approval

#### 路径 D：fallback 到 `item/tool/requestUserInput`

当上面的 MCP elicitation feature 没开时：

- core 会把 MCP tool approval 降级成 `request_user_input`
- client 回写的是 `item/tool/requestUserInput` 的 answers
- core 再把 answers 解析成 MCP tool approval decision

结论：

- “MCP tool 执行审批”是业务语义
- 它在 app-server surface 上至少可能表现为三种形态：
  - guardian review notification
  - `mcpServer/elicitation/request`
  - `item/tool/requestUserInput`

所以本仓库不能再用“只要是 MCP 确认就一定是某一种 request”这种假设。

## 5. Surface 行为差异

### 5.1 TUI

当前 upstream TUI 明确单独处理：

- `McpServerElicitationRequest`
- `PermissionsRequestApproval`
- `ToolRequestUserInput`
- guardian review notifications

TUI 路线说明：

- upstream 认为这些 request 类型是有区分价值的
- client 侧应该保留而不是主动压平

### 5.2 exec

当前 upstream exec 的策略更保守：

- 自动 `cancel` MCP elicitation
- 拒绝 permissions approval
- 拒绝 `request_user_input`
- 拒绝其他 interactive approval path

这说明：

- exec 的行为只是某一种受限 surface 的 fallback
- 远端/飞书端不应默认拿 exec 的“拒绝/取消”策略当产品目标

## 6. 当前仓库偏差

### 6.1 translator 层

当前 `internal/adapter/codex/translator_observe_server.go` / `translator_helpers.go` 存在这些偏差：

- 只把 `serverRequest/started` / `request/started` 统一抽成泛化 request
- `extractRequestType()` 仍会把多种 request 压到 `approval`
- 没有为：
  - `mcpServer/elicitation/request`
  - `item/permissions/requestApproval`
  - guardian review notification
  - `item/mcpToolCall/progress`
  建立专门解析逻辑
- 虽然识别了 `mcp_tool_call` item kind，但 `extractItemMetadata()` 没有专门抽取：
  - `server`
  - `tool`
  - `arguments`
  - `result`
  - `error`
  - `durationMs`

### 6.2 agentproto / control / state 模型层

当前本仓库内部模型还不足以完整表达 upstream MCP surface：

- `internal/core/agentproto/types.go` 里的 request response 仍是 `map[string]any`
- `internal/core/control/types.go` 的 `FeishuDirectRequestPrompt` 只支持：
  - `options`
  - `questions`
- 它没有直接表达：
  - MCP elicitation 的 url 模式
  - elicitation `_meta`
  - permissions approval 的结构化 permission grant
  - guardian review 可见性
- `internal/core/state/types.go` 的 `RequestPromptRecord` 也没有保存上述专门字段

这意味着如果要正确支持 upstream 协议，当前结构本身就需要扩展，不能再强行塞回旧的 approval/request_user_input 二分法。

### 6.3 orchestrator 层

当前 `internal/core/orchestrator/service_request.go` 只产品化了：

- `approval`
- `request_user_input`

其他 request 类型当前都会落到 unsupported。

此外：

- `service_queue.go` / `service_helpers.go` 只对 `dynamic_tool_call` 和 `image_generation` 有专门结果路径
- `mcp_tool_call` 当前既没有过程状态路径，也没有富结果路径，也没有 progress 路径
- guardian review 当前在 orchestrator 侧完全没有消费路径

### 6.4 Feishu / remote surface 层

以当前产品模型，飞书端还缺少：

- MCP elicitation form/url 的原生承载
- permissions approval 的授权范围与权限子集回写
- guardian review 的轻量可见性
- MCP tool call 运行中 progress 展示

因此，如果不先做协议分层，后续任何“支持 MCP request”都只会继续停留在旧的近似兼容。

## 7. 建议的后续拆分

### 7.1 Stage 1：协议 metadata 与 request typing

先补基础设施，不先定最终 UI：

- 为 `mcpServer/elicitation/request` 建独立 request type
- 为 `item/permissions/requestApproval` 建独立 request type
- 为 guardian review 建独立 notification/event type
- 为 `item/mcpToolCall/progress` 建独立 event type
- 为 `mcp_tool_call` item 补完整 metadata/result 提取

### 7.2 Stage 2：可响应链路

- translator command 侧补齐：
  - MCP elicitation `{action, content, _meta}`
  - permissions approval `{permissions, scope}`
- orchestrator / surface 层定义哪些 request 可以直接在飞书端完成
- 明确 guardian review 在飞书端是只展示，还是未来允许人工干预

### 7.3 Stage 3：展示与产品化

- MCP tool call 过程状态
- MCP tool call progress
- MCP tool call 结果回显
- form/url elicitation 的飞书侧 UX

## 8. 对本仓库的直接结论

截至当前代码，可以直接确认：

1. 本仓库现在还没有遵守 upstream 最新 MCP app-server 协议分层。
2. 当前“泛化 approval/request_user_input”模型不够表达：
   - permissions approval
   - MCP elicitation
   - guardian review
   - MCP progress
3. 如果后续继续做 MCP 相关功能，应优先做结构扩展，而不是继续在旧结构里打补丁。
4. 对远端/飞书端来说，更合适的参考面是 upstream TUI，而不是 exec 的自动取消/拒绝策略。

## 9. 参考

- upstream：
  - `codex-rs/app-server-protocol/src/protocol/common.rs`
  - `codex-rs/app-server-protocol/src/protocol/v2.rs`
  - `codex-rs/app-server-protocol/src/protocol/thread_history.rs`
  - `codex-rs/app-server/src/bespoke_event_handling.rs`
  - `codex-rs/app-server/tests/suite/v2/mcp_server_elicitation.rs`
  - `codex-rs/app-server/tests/suite/v2/request_permissions.rs`
  - `codex-rs/protocol/src/approvals.rs`
  - `codex-rs/core/src/mcp_tool_call.rs`
  - `codex-rs/codex-mcp/src/mcp_connection_manager.rs`
  - `codex-rs/tui/src/chatwidget.rs`
  - `codex-rs/exec/src/lib.rs`
- 本仓库：
  - `internal/adapter/codex/translator_helpers.go`
  - `internal/adapter/codex/translator_observe_server.go`
  - `internal/adapter/codex/translator_commands.go`
  - `internal/core/agentproto/types.go`
  - `internal/core/control/types.go`
  - `internal/core/state/types.go`
  - `internal/core/orchestrator/service_request.go`
  - `internal/core/orchestrator/service_queue.go`

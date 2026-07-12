# Codex MCP App-Server 协议基线

> Type: `general`
> Updated: `2026-07-12`
> Summary: 基于 upstream `openai/codex` HEAD `9e552e9d15ba52bed7077d5357f3e18e330f8f38` 梳理 MCP tool call、授权确认、elicitation 与 OAuth login 的 app-server 时序，并同步当前仓库已落地的 MCP request typing、Feishu 可响应链路、approval-carrying elicitation 的 `_meta.persist` contract、OAuth 最小发起/收敛链路与剩余偏差。

## 1. 文档定位

这份文档记录的是截至 `2026-07-12` 已确认的 **Codex app-server 中与 MCP call 相关的协议基线**，用于指导本仓库后续的 translator、orchestrator、Feishu remote surface 设计。

本次核对的 upstream 基线为：

- 仓库：`openai/codex`
- HEAD：`9e552e9d15ba52bed7077d5357f3e18e330f8f38`
- 时间：`2026-07-12` 复核
- subject：当前 `main`

本文关注的是：

- app-server client 到 Codex CLI 的正确交互面与时序
- MCP 相关授权确认流程到底拆成了哪些线
- 本仓库当前处理逻辑和 upstream 基线的偏差点

本文不直接规定飞书端最终 UI，但它是后续实现拆分的协议依据。

## 2. 上游协议面总览

截至当前 upstream，和 MCP call / MCP server continuation 直接相关的 app-server surface 不是单一路径，而是至少包含下面七类：

| 类别 | upstream surface | 作用 | client 是否需要回写 |
| --- | --- | --- | --- |
| MCP item 生命周期 | `item/started` + `item/completed` 携带 `ThreadItem::McpToolCall` | 表示 MCP tool call 开始、完成、失败，以及最终结果/错误 | 否 |
| MCP 过程进度 | `item/mcpToolCall/progress` | 在 tool call 运行中追加轻量进度消息 | 否 |
| MCP elicitation | `mcpServer/elicitation/request` | 处理 MCP server 主动发起的 form/url elicitation | 是 |
| permissions approval | `item/permissions/requestApproval` | 请求附加权限授权 | 是 |
| guardian review 可见性 | `item/autoApprovalReview/started` / `item/autoApprovalReview/completed` | 暴露 guardian 对高风险动作的审查生命周期，`action.type` 可为 `mcpToolCall` | 否 |
| MCP tool 执行审批 fallback | `item/tool/requestUserInput` | 在某些模式下，MCP tool 执行审批并不走 `mcpServer/elicitation/request`，而是退回为 `request_user_input` | 是 |
| MCP OAuth login lifecycle | `mcpServer/oauth/login` + `mcpServer/oauthLogin/completed` | client 主动请求某个 MCP server 的 OAuth 授权链接，并等待 async completion notification | 主动发起 request；completion 不回写 |

这意味着“把 MCP 授权确认做好”至少要同时覆盖三条确认线：

- MCP tool 执行审批
- permissions approval
- MCP elicitation

并且还要补一条可见性线：

- guardian 对 MCP tool call 的审查通知

OAuth login 则是相邻但独立的 client-initiated RPC lifecycle，不属于 `mcpServer/elicitation/request` pending request substrate。

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

当 form elicitation 承载 **MCP tool approval** 时，当前 upstream 会在 request `_meta` 中携带额外语义：

- `codex_approval_kind = "mcp_tool_call"`
- `persist = "session" | "always" | ["session", "always"]`

这条链路的关键点是：

- wire family 仍然是 `mcpServer/elicitation/request`，不是 `item/permissions/requestApproval`，也不是旧 approval family。
- response 仍然是 top-level `{action, content, _meta}`；选择“本次允许”时不能把 request `_meta.persist` 广告原样带回，选择“本会话允许”时才应回写 `action="accept"`，并在 response `_meta.persist` 中写入 `session`。
- `persist=always` 是 upstream 广告的持久授权能力；本仓库第一阶段只识别并提示“飞书端暂不支持跨会话持久允许”，不发出 `_meta.persist=always`。

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

### 3.6 `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`

当前 upstream 把 MCP server OAuth 登录建模为主动 RPC + 异步通知：

- request method：`mcpServer/oauth/login`
- request params：
  - `name`
  - optional `threadId`
  - optional `scopes`
  - optional `timeoutSecs`
- response：
  - `authorization_url`
- completion notification：`mcpServer/oauthLogin/completed`
- completion params：
  - `name`
  - `threadId`
  - `success`
  - optional `error`

这条 lifecycle 的关键点是：

- 它不是 server request，不会经过 `serverRequest/started` / `serverRequest/resolved`。
- completion notification 没有 request id，只能用 pending command state 与协议字段 `name + threadId` 做相关。
- 如果 Codex 拒绝发起 request，错误会出现在 JSON-RPC response error 上；常见错误包括非 streamable HTTP MCP server 不支持 OAuth login。
- Feishu/headless 侧不应该把 authorization URL 做流式/打字机展示；只需要展示完整 URL 与最终完成/失败结果。

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
- approval-carrying request 通过 `_meta.codex_approval_kind="mcp_tool_call"` 与普通 form/url elicitation 区分
- 但 core 会进一步把 `content` / `_meta` 解析成：
  - `accept`
  - `accept for session`
  - `accept and remember`
  - `decline`
  - `cancel`

也就是说：

- wire response 仍是 MCP elicitation response
- 业务语义却是 MCP tool approval
- 本仓库当前只把 `persist=session` 产品化成本会话允许；`persist=always` 只做不支持提示，不伪造跨会话持久授权

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

### 4.5 MCP OAuth login

OAuth login 的 canonical 时序和 request family 不同：

1. client 主动发送 `mcpServer/oauth/login`。
2. app-server 同步返回 `{ authorization_url }`，或返回 JSON-RPC error。
3. 用户在外部浏览器完成授权。
4. app-server 发出 `mcpServer/oauthLogin/completed` notification。
5. client 只消费 completion，不需要再回写 response。

当前本仓库的本地 contract：

- `agentproto.CommandMCPOAuthLogin` 发起登录。
- translator 将其映射到 upstream `mcpServer/oauth/login`，字段使用 upstream casing：`name`、`threadId`、`scopes`、`timeoutSecs`。
- response 成功转成本地 `EventMCPOAuthLoginURLReady`；response error 转成本地 `system.error` 并清理 pending flow。
- completion notification 转成本地 `EventMCPOAuthLoginCompleted`。
- daemon 通过 `/mcpoauth <server>` 发起；有 selected thread 时传 `threadId`，否则省略 `threadId` 做 app-scoped login。
- Feishu 侧只发送一次授权链接 notice 和一次完成/失败 notice，不进入 request card，不做流式 patch。

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

当前 translator 已经显式承接：

- `mcpServer/elicitation/request`
- `item/permissions/requestApproval`
- `item/tool/requestUserInput`
- `item/mcpToolCall/progress`
- `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`

当前仍存在这些偏差：

- guardian review notification 仍未产品化。
- MCP tool call 最终 result / structuredContent 仍未做富展示，只保留当前过程与摘要能力。
- OAuth completion notification 只能按 `name + threadId` 相关 pending flow；如果没有本地 pending flow，daemon 会保守忽略，不广播。

### 6.2 agentproto / control / state 模型层

当前本仓库内部模型已经把 MCP request family 与 OAuth RPC lifecycle 拆开：

- request family 继续通过 `RequestPrompt` / `FeishuRequestView` 表达，并由 orchestrator 归一化具体语义。
- OAuth login 使用 `MCPCommand.OAuthLogin` 与 `MCPOAuthLoginEvent`，不复用 request response DTO。

当前仍不足的是：

- guardian review 可见性还没有独立 agentproto event。
- MCP tool result 的结构化富展示还没有稳定 read model。
- OAuth 第一阶段只支持显式服务名发起，不提供 MCP server 列表、账号管理 UI 或持久 OAuth 状态页。

### 6.3 orchestrator 层

当前 orchestrator / daemon 已产品化：

- command/file/network approval
- `request_user_input`
- `permissions_request_approval`
- `mcp_server_elicitation` form/url/approval
- OAuth login 的 `/mcpoauth <server>` 最小入口、授权 URL notice 与完成/失败 notice

当前仍未产品化：

- guardian review 可见性
- MCP tool call 最终结果富展示
- MCP OAuth server 列表、账号状态与配置管理

### 6.4 Feishu / remote surface 层

以当前产品模型，飞书端剩余缺口主要是：

- guardian review 的轻量可见性
- MCP tool call 最终结果的更完整产品化回显
- MCP OAuth 的 server 选择器、账号状态页与错误恢复引导

OAuth 当前只落最小可用链路：显式 slash 发起、一次 URL notice、一次完成/失败 notice。它不进入 request card，也不使用频繁 patch。

## 7. 建议的后续拆分

### 7.1 Stage 1：协议 metadata 与 request typing

先补基础设施，不先定最终 UI：

- 为 `mcpServer/elicitation/request` 建独立 request type
- 为 `item/permissions/requestApproval` 建独立 request type
- 为 guardian review 建独立 notification/event type
- 为 `item/mcpToolCall/progress` 建独立 event type
- 为 `mcp_tool_call` item 补完整 metadata/result 提取

### 7.2 Stage 2：可响应链路

已完成：

- translator command 侧补齐：
  - MCP elicitation `{action, content, _meta}`
  - permissions approval `{permissions, scope}`
  - MCP OAuth login `mcpServer/oauth/login`
- orchestrator / surface 层已把这些 request 接入飞书端直答：
  - `permissions_request_approval`
  - `mcp_server_elicitation`
  - 其中 form 模式 elicitation 会先做 schema 派生字段或 JSON fallback，再走显式提交
- daemon / Feishu 已为 OAuth 提供显式 `/mcpoauth <server>`，并把 `authorization_url` 与 `oauthLogin/completed` 收敛为 append-only notice
- 明确 guardian review 在飞书端是只展示，还是未来允许人工干预

### 7.3 Stage 3：展示与产品化

- MCP tool call 过程状态
- MCP tool call progress
- MCP tool call 结果回显
- form/url elicitation 的飞书侧 UX
- MCP OAuth server 选择、账号状态和配置管理 UI

## 8. 对本仓库的直接结论

截至当前代码，可以直接确认：

1. 本仓库已经遵守 upstream 最新 MCP request typing：
   - `permissions_request_approval`
   - `mcp_server_elicitation`
   - `item/tool/requestUserInput` fallback
2. 当前飞书端已经具备这两条结构化回写能力：
   - permissions approval `{permissions, scope}`
   - MCP elicitation `{action, content, _meta}`
3. 当前已经具备 OAuth login 的最小可用链路：
   - `/mcpoauth <server>`
   - `authorization_url` 展示
   - `oauthLogin/completed` 成功/失败收敛
4. 当前剩余主要偏差集中在“过程可见性、结果展示与管理 UI”，而不是 request typing 本身：
   - guardian review notification 仍未产品化
   - `item/mcpToolCall/progress` 仍未做完整 UI 表达
   - MCP tool call 最终结果仍缺更富的产品回显
   - MCP OAuth 没有 server picker / account page，只提供显式 slash 最小入口
5. 对远端/飞书端来说，更合适的参考面仍然是 upstream TUI，而不是 exec 的自动取消/拒绝策略。

## 9. 参考

- upstream：
  - `codex-rs/app-server-protocol/src/protocol/common.rs`
  - `codex-rs/app-server-protocol/src/protocol/v2.rs`
  - `codex-rs/app-server-protocol/src/protocol/thread_history.rs`
  - `codex-rs/app-server/src/bespoke_event_handling.rs`
  - `codex-rs/app-server/tests/suite/v2/mcp_server_elicitation.rs`
  - `codex-rs/app-server/tests/suite/v2/request_permissions.rs`
  - `codex-rs/app-server/src/request_processors/mcp_processor.rs`
  - `codex-rs/protocol/src/approvals.rs`
  - `codex-rs/core/src/mcp_tool_call.rs`
  - `codex-rs/codex-mcp/src/mcp_connection_manager.rs`
  - `codex-rs/tui/src/chatwidget.rs`
  - `codex-rs/exec/src/lib.rs`
- 本仓库：
  - `internal/adapter/codex/translator_helpers.go`
  - `internal/adapter/codex/translator_observe_server.go`
  - `internal/adapter/codex/translator_commands.go`
  - `internal/adapter/codex/translator_mcp_oauth.go`
  - `internal/app/daemon/app_mcp_oauth.go`
  - `internal/core/agentproto/types.go`
  - `internal/core/control/types.go`
  - `internal/core/state/types.go`
  - `internal/core/orchestrator/service_request.go`
  - `internal/core/orchestrator/service_queue.go`

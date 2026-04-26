# Codex App Server 状态机遵循度审计

> Type: `inprogress`
> Updated: `2026-04-16`
> Summary: 对照 OpenAI 官方 Codex App Server 页面与 `openai/codex` 最新源码/schema（本轮复核到 HEAD `18d61f6923896aa273ae9a369d423ac75dd8963a`），按 VS Code 透传、relay/Feishu 归一化、headless 主动驱动三层审计当前仓库对各类状态机的遵循程度；本轮二次回写补上目标遵循策略、官方文档与 canonical schema 的漂移、遗漏的 realtime / MCP 启动 / skills 变化等状态机族群，并同步记录 `#228` 已补齐 headless `initialize -> initialized` 严格握手。

## 1. 审计范围与判定口径

官方基线这次分两层取：

- 官方页面：<https://developers.openai.com/codex/app-server>
- 上游 canonical source：`openai/codex` HEAD `18d61f6923896aa273ae9a369d423ac75dd8963a`（2026-04-15），重点看：
  - `codex-rs/app-server/README.md`
  - `codex-rs/app-server-protocol/src/protocol/common.rs`
  - `codex-rs/app-server-protocol/src/protocol/v2.rs`
  - `codex-rs/app-server-protocol/schema/json/ServerRequest.json`
  - `codex-rs/app-server-protocol/schema/json/ServerNotification.json`

如果官方页面、README 和 schema/source 之间有冲突，**以 schema 和源码为准**。

这次不只看“某个 JSON-RPC 方法有没有出现”，而是看**状态机有没有被我们正确承接**。本文优先关注：

- 多步 request/notification 流程；
- 会改变 loaded/unloaded、running/idle、waitingOnApproval、review mode 等运行时语义的 surface；
- 会要求 relay/headless 主动驱动的协议链路。

纯粹的一次性 request/response API 不作为本文主优先级，但如果它们会影响“审计是否完整”的判断，本轮也会在文中补记，避免把“没写到”误解成“上游没有”。

为了避免把“纯透传”误判成“已实现”，本文把仓库分成三层：

1. `VS Code/native app-server client 透传层`
   - 入口/出口：`internal/app/wrapper/app_io.go`
   - 这层的特点是：未知方法和未知通知通常会被 wrapper 原样转发给本地 client，不会主动产品化。
2. `relay/Feishu 归一化层`
   - 入口/出口：`internal/adapter/codex/translator_*.go`、`internal/core/agentproto/types.go`
   - 只有 translator 明确建模的方法/通知，才能变成 daemon/Feishu 可消费的标准事件。
3. `headless 主动驱动层`
   - 入口：`internal/app/wrapper/app_headless.go` + daemon 发出的 `agentproto.Command`
   - 这层决定“没有 VS Code 时，我们自己能不能主动跑完整条状态机”。

本文的状态结论使用四档：

- `严格遵循`：关键顺序、关键字段、终态语义都已按官方文档承接。
- `遵循但有适配压缩`：核心时序没错，但被 relay 层压平成自定义命令/事件，或只覆盖了官方语义的主路径。
- `部分遵循`：只覆盖了其中一段，或者 VS Code 透传可用，但 relay/headless 侧没有完整承接。
- `未遵循/未实现`：当前 relay/headless 无法正确承接这条状态机；若本地 client 不直接消费，就等于没有。

## 2. 总体结论

### 2.1 当前可以说“基本跟住了”的核心状态机

这些是目前最接近官方基线的部分：

- `thread/start | thread/resume -> turn/start -> turn/started -> item/* -> turn/completed`
  - remote prompt 路径已经按这个顺序展开，只是 relay 层把中间的 request/response 做了压缩处理。
  - 证据：`internal/adapter/codex/translator_commands.go`、`internal/adapter/codex/translator_observe_server.go`
- `turn/steer`
  - 已要求 `expectedTurnId`，也保留了“不再发新的 turn/started”这一官方语义。
  - 证据：`internal/adapter/codex/translator_commands.go`、`internal/adapter/codex/translator_test.go`
- `turn/interrupt -> turn/completed(status=interrupted)`
  - 已按官方语义处理。
  - 证据：`internal/adapter/codex/translator_commands.go`、`internal/adapter/codex/translator_observe_server.go`
- 通用 item 生命周期与主要 delta 流
  - 已承接 `item/started`、`item/completed`、`item/agentMessage/delta`、`item/plan/delta`、`item/reasoning/textDelta`、`item/reasoning/summaryTextDelta`、`item/commandExecution/outputDelta`、`item/fileChange/outputDelta`。
  - 证据：`internal/adapter/codex/translator_observe_server.go`
- `turn/plan/updated`、`thread/tokenUsage/updated`
  - 已有结构化解析，并进入 relay 标准事件。
  - 证据：`internal/adapter/codex/translator_observe_server.go`
- `thread/compact/start -> contextCompaction item lifecycle`
  - 已有主动命令与终态处理，方向与官方一致。
  - 证据：`internal/adapter/codex/translator_commands.go`、`internal/adapter/codex/translator_compact_test.go`

### 2.2 当前最大的偏差，不是“完全没接 app-server”，而是“只接了自己用到的那一截”

当前实现的真实情况是：

- **VS Code 模式**：很多官方方法其实不会坏，因为 wrapper 对未知帧大多是原样透传。
- **relay/Feishu/headless 模式**：只有 translator/agentproto 明确建模过的状态机才成立。
- 所以真正的风险点是：
  - 我们以为“wrapper 没拦，应该就是支持了”；
  - 但一到 Feishu/headless，这条状态机就直接断了。

这也是为什么这次必须区分“透传可用”和“我们自己真的实现了”。

## 3. 分项审计

### 3.1 连接初始化：`initialize -> initialized`

官方基线：

- 连接建立后先发 `initialize`
- 然后客户端还要发 `initialized`
- 在这之前 server 可以拒绝其他请求

当前实现结论：`遵循`

- `VS Code 透传层`：
  - **大体可用**。wrapper 会把来自上游 client 的未知方法原样转发给 Codex，也会把未知响应/通知原样回传。
  - 所以如果本地 client 自己完整执行 `initialize -> initialized`，wrapper 一般不会破坏它。
  - 证据：`internal/app/wrapper/app_io.go`
- `headless 主动驱动层`：
  - **已严格遵循**。wrapper 现在会在建立 relay client 之前先同步发送 `initialize`、等待对应 response、再发送 `initialized`，只有握手完成后才进入正常 stdout/relay 流程。
  - 同步 bootstrap 期间如果提前读到其他 stdout 帧，会缓冲并回放给后续 `stdoutLoop`，避免吞掉非握手数据。
  - 证据：`internal/app/wrapper/app_headless.go`、`internal/app/wrapper/app_headless_test.go`、`internal/app/wrapper/app_test.go`
- `relay/Feishu 归一化层`：
  - **仍未产品化连接级初始化状态**，也没有把 `optOutNotificationMethods` 等连接级能力显式建模到 headless bootstrap。
  - 但 headless 已不再依赖“未初始化也能继续发业务请求”的侥幸行为。

结论：

- 这条在 VS Code 透传与 headless 主动驱动两条主路径上都已经满足 canonical 握手顺序。
- 当前剩余缺口主要是“连接级 capabilities / 初始化状态没有产品化暴露”，而不是握手 correctness 本身。

### 3.2 线程启动/恢复主路径：`thread/start | thread/resume | thread/fork`

官方基线：

- `thread/start`：新建 thread，发 `thread/started`
- `thread/resume`：恢复 thread
- `thread/fork`：分叉历史到新 thread，并发 `thread/started`

当前实现结论：

- `thread/start`：`遵循但有适配压缩`
- `thread/resume`：`遵循但有适配压缩`
- `thread/fork`：`遵循但有适配压缩`

现状：

- relay 的 `prompt.send` 在缺 thread 时会先发 native `thread/start`，收到结果后再自动补 `turn/start`。
- 已有 thread 但未聚焦时，会先发 `thread/resume`，再自动补 `turn/start`。
- `fork_ephemeral` 执行目标会先发 native `thread/fork`，收到结果后再自动补 `turn/start`。
- 这条“先 thread，再 turn”的官方顺序是对的。
- 但我们在 relay 层把中间 JSON-RPC response 压平了，不把 `thread/start` / `thread/resume` response 暴露成上层状态机的一部分。
- `thread/fork` 也沿用同一套 response 压缩路径，不单独暴露中间 response。

证据：

- `internal/adapter/codex/translator_commands.go`
- `internal/adapter/codex/translator_observe_server.go`
- `internal/core/agentproto/types.go`

### 3.3 线程只读/列举/分页/状态类 API

包含：

- `thread/read`
- `thread/list`
- `thread/loaded/list`
- `thread/status/changed`
- `thread/name/set -> thread/name/updated`

当前实现结论：`部分遵循`

具体判断：

- `thread/name/set -> thread/name/updated`
  - 本地 client 路径已透传，translator 也能观察并生成 thread metadata 事件。
  - 结论：`遵循但有适配压缩`
- `thread/read(includeTurns)`
  - 我们把它压成了 relay 的 `thread.history.read`，并能回填 turn/item 历史。
  - 结论：`遵循但有适配压缩`
- `thread/list`
  - 当前 relay 只支持“刷新快照”，由 `thread/list + N 次 thread/read` 拼装成 snapshot。
  - 但官方的 cursor、filters（`cwd`、`modelProviders`、`sourceKinds`、`archived`）没有被 relay command 面完整表达。
  - 代码里还把 `limit` 固定成了 `50`。
  - 结论：`部分遵循`
- `thread/loaded/list`
  - mock 有，真实 translator 没有 command 建模。
  - 结论：`未遵循/未实现`
- `thread/status/changed`
  - 官方把它当成 loaded thread 的关键运行时状态通知，尤其用于 `waitingOnApproval` 之类状态。
  - 当前 translator 完全没有处理这个通知。
  - 结论：`未遵循/未实现`

证据：

- `internal/adapter/codex/translator_commands.go`
- `internal/adapter/codex/translator_observe_server.go`
- `testkit/mockcodex/mockcodex.go`

这条非常重要：

- 我们现在很多“线程是否处于等待审批/运行中/未加载”的产品判断，**并不是严格跟着官方 `thread/status/changed` 走的**。
- 这会影响后续如果要做更复杂的审批、review、review mode 或 loaded-thread 生命周期展示。

### 3.4 线程运维类状态机

包含：

- `thread/unsubscribe -> thread/status/changed -> thread/closed`
- `thread/archive -> thread/archived`
- `thread/unarchive -> thread/unarchived`
- `thread/rollback`
- `thread/shellCommand`
- `thread/backgroundTerminals/clean`

当前实现结论：

- `thread/compact/start`：`遵循但有适配压缩`
- 其余：`未遵循/未实现`

现状：

- 只有 `thread/compact/start` 被做成了 relay command。
- 其它线程运维类状态机在 relay/headless 侧都没有 command 建模，也没有通知消费。
- 这些能力在 VS Code 透传模式下理论上仍可由本地 client 直接使用，但 **Feishu/headless 当前等于没有**。

### 3.5 Turn 主状态机：`turn/start -> turn/started -> item/* -> turn/completed`

当前实现结论：`遵循但有适配压缩`

现状：

- remote prompt 发起时，我们会把 turn 的开始、完成、错误、traffic class、initiator 这些信息重新标准化进 `agentproto.Event`。
- turn 终态的 `completed / interrupted / failed` 也保留了。
- 如果中途有 `error` 事件，当前实现会把 turn-bound runtime error 先缓存，再挂到最终 `turn.completed` 上，以避免 Feishu 重复告警。

证据：

- `internal/adapter/codex/translator_observe_server.go`

这里的结论是：

- **核心 turn 生命周期我们是跟住了的**。
- 但 relay 侧对 JSON-RPC response 做了 suppress/compress，所以它不是“逐帧复刻”，而是“语义对齐后的适配实现”。

### 3.6 `turn/steer`

当前实现结论：`严格遵循`

原因：

- translator 发送 `expectedTurnId`
- 如果 server 返回“没有 active turn”或 turn id 不匹配，错误会被正确回传
- 也遵守了官方“不再发新的 `turn/started`”的语义
- 相关测试已经覆盖

证据：

- `internal/adapter/codex/translator_commands.go`
- `internal/adapter/codex/translator_test.go`
- `internal/app/wrapper/app_test.go`

### 3.7 `turn/interrupt`

当前实现结论：`严格遵循`

原因：

- 当前命令层直接发 `turn/interrupt`
- 终态依赖官方 `turn/completed(status=interrupted)` 收敛

证据：

- `internal/adapter/codex/translator_commands.go`
- `internal/adapter/codex/translator_observe_server.go`

### 3.8 Turn 衍生通知：`turn/plan/updated`、`turn/diff/updated`、`model/rerouted`

当前实现结论：

- `turn/plan/updated`：`严格遵循`
- `turn/diff/updated`：`严格遵循`
- `model/rerouted`：`遵循但有适配压缩`

现状：

- 计划更新已经有结构化快照抽取。
- `turn/diff/updated` 已经被接成 canonical `turn.diff.updated`，orchestrator 会按 `(threadId, turnId)` 覆盖保存 authoritative aggregated diff snapshot，并在 turn 结束时挂到最终 block summary 上。
- `model/rerouted` 现在也已经被接成 canonical `turn.model_rerouted`，会保留 `fromModel` / `toModel` / `reason`，并把 thread 当前有效模型同步到 `toModel`，避免后续产品面继续把 requested model 当成 actual model。
- 这条 reroute 链路当前仍属于“协议保真优先”的接法：daemon/state/snapshot 已保留 latest state，但还没有单独做强提示或历史链路 UI。

证据：

- `internal/adapter/codex/translator_observe_server.go`
- `internal/core/agentproto/types.go`
- `internal/core/orchestrator/service_model_reroute.go`

### 3.9 通用 item 生命周期与 item delta

当前实现结论：`部分遵循`

已跟住的部分：

- `item/started`
- `item/completed`
- `item/agentMessage/delta`
- `item/plan/delta`
- `item/reasoning/textDelta`
- `item/reasoning/summaryTextDelta`
- `item/commandExecution/outputDelta`
- `item/fileChange/outputDelta`
- `item/mcpToolCall/progress`

部分缺口：

- `item/reasoning/summaryPartAdded`
  - 官方文档有，当前没有处理。
- `enteredReviewMode` / `exitedReviewMode`
  - 虽然 generic `item/started/completed` 能把它们作为原始 item type 吞进来，但没有专门语义，也没有产品化路径。
- `collabToolCall`
  - 官方文档里的名字是 `collabToolCall`；当前 normalizer 识别的是 `collabAgentToolCall` / `collab_agent_tool_call`，未见对官方命名的显式兼容。
- `imageView`
  - 官方文档列出了 `imageView` item，当前没有专门 normalizer/metadata 抽取。
- `rawResponseItem/completed`
  - 最新 schema 已列出，但当前 translator 没有消费；这更接近原始 Responses surface / debug surface，而不是当前 chat 主路径 UI。

证据：

- `internal/adapter/codex/translator_observe_server.go`
- `internal/adapter/codex/translator_helpers.go`
- `internal/adapter/codex/translator_requests_test.go`

### 3.10 审批 / server request 状态机

官方页面当前明确写了三条：

1. `item/commandExecution/requestApproval`
2. `item/fileChange/requestApproval`
3. `tool/requestUserInput`

但这次对照最新 upstream README + schema 后，需要修正两点：

1. canonical schema / `common.rs` 当前真正定义的是 `item/tool/requestUserInput`，不是顶层 `tool/requestUserInput`；
2. 最新 source 还明确包含：
   - `item/permissions/requestApproval`
   - `mcpServer/elicitation/request`

当前实现结论：`部分遵循；核心交互请求面已补齐`

现状：

- 已有：
  - 泛化 `serverRequest/started` / `serverRequest/resolved`
  - `item/commandExecution/requestApproval`
  - `item/fileChange/requestApproval`
  - `item/tool/requestUserInput`
  - 以及为兼容官方页面/README 措辞而保留的顶层 `tool/requestUserInput` alias
  - 以及仓库中后来补的 `item/permissions/requestApproval`、`mcpServer/elicitation/request`（这两条超出本页主线，但属于我们额外追上 upstream 的部分）
- 当前剩余边界：
  - command/file approval 当前会先归一化成 `approval_command`、`approval_file_change`、`approval_network`，并复用通用 request 卡，而不是 1:1 复制 upstream 的更细专用 UI
  - `availableDecisions` 已会继续透传到 request options，包含 `cancel`；但像 `acceptWithExecpolicyAmendment` 这类更细决策还没有专门交互面
  - `item/permissions/requestApproval` 与 `mcpServer/elicitation/request` 虽然已接到 relay/Feishu/headless，但目前仍走通用 request 投影，而不是 source-native 的专门 UI surface

这意味着：

- relay/Feishu/headless 已经不会再把这几类真实 request 静默吞掉；
- 但**还不是严格按 canonical source 的每一条专用 request surface 完整复制**；
- 当前更准确的表述是：主链路已补齐，剩余差异集中在更细粒度 approval / elicitation / permissions UI，而不是“请求根本没有承接”。

证据：

- `internal/adapter/codex/translator_observe_server.go`
- `internal/adapter/codex/translator_helpers_request.go`
- `internal/core/orchestrator/service_helpers_request.go`
- `internal/adapter/codex/translator_requests_test.go`
- `internal/core/orchestrator/service_request_approval_test.go`

### 3.11 Dynamic tool call

官方基线：

1. `item/started(item.type=dynamicToolCall)`
2. `item/tool/call` server request
3. client response
4. `item/completed`

当前实现结论：`部分遵循`

现状：

- 我们已经能解析 `dynamicToolCall` 的 item started/completed 和 metadata。
- 但 `item/tool/call` 这条真正需要 client 回写的 request 状态机没有实现。
- 所以这条现在只是**看得见部分 item 生命周期**，但不能说完整支持了官方 dynamic tool call state machine。

证据：

- `internal/adapter/codex/translator_observe_server.go`
- `internal/adapter/codex/translator_helpers.go`
- 仓库内无 `item/tool/call` 命中

### 3.12 Review 状态机：`review/start -> turn/started -> enteredReviewMode -> exitedReviewMode`

当前实现结论：`未遵循/未实现`

现状：

- 没有 `review/start` command 建模。
- 没有 detached review thread 的承接逻辑。
- 没有对 `enteredReviewMode` / `exitedReviewMode` 做独立状态处理和产品化。
- 虽然本地 VS Code client 经 wrapper 透传理论上可能自己处理，但 relay/Feishu/headless 侧当前没有这条能力。

证据：

- 仓库内无 `review/start` 命中
- `internal/adapter/codex/translator_helpers.go` 未对 review item type 做专门映射

### 3.13 `command/exec` 家族

包含：

- `command/exec`
- `command/exec/write`
- `command/exec/resize`
- `command/exec/terminate`
- 可选 `command/exec/outputDelta`

当前实现结论：

- `outputDelta`：`部分遵循`
- 其余：`未遵循/未实现`

现状：

- 我们能解析 turn 内部 `commandExecution` item 的输出 delta。
- 但官方这页的 `command/exec` 是**独立于 thread/turn 的 server API**，当前没有 command 面，也没有 session/processId 跟踪。

### 3.14 认证与账号状态机

包含：

- `account/read`
- `account/login/start`
- `account/login/cancel`
- `account/login/completed`
- `account/logout`
- `account/updated`
- `account/chatgptAuthTokens/refresh`
- `account/rateLimits/read`
- `account/rateLimits/updated`

当前实现结论：`未遵循/未实现`

现状：

- relay/headless 完全没有这组状态机的 command / event 建模。
- 因此如果未来想让 Feishu/headless 参与 ChatGPT 登录、外部 token 刷新、rate limit 展示，这一层基本要从头设计。

### 3.15 Apps / MCP / Skills / 其他连接器状态机

包含：

- `app/list -> app/list/updated`
- `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`
- `mcpServer/startupStatus/updated`
- `mcpServerStatus/list`
- `mcpServer/resource/read`
- `config/mcpServer/reload`
- `skills/changed`

当前实现结论：`未遵循/未实现`

其中最需要注意的是几条真正带状态推进或 invalidation 语义的：

- `app/list -> app/list/updated`
- `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`
- `mcpServer/startupStatus/updated`
- `skills/changed`

这些在当前 relay/headless 侧都没有承接。

### 3.16 Realtime / watch / Windows / fuzzy search 状态机

包含：

- `thread/realtime/start -> thread/realtime/* -> thread/realtime/closed`
- `fs/watch -> fs/changed -> fs/unwatch`
- `windowsSandbox/setupStart -> windowsSandbox/setupCompleted`
- `fuzzyFileSearch/sessionUpdated -> fuzzyFileSearch/sessionCompleted`
- `externalAgentConfig/detect`
- `externalAgentConfig/import`

当前实现结论：

- `thread/realtime/*`、`fs/watch -> fs/changed -> fs/unwatch`、`windowsSandbox/setup*`、`fuzzyFileSearch/*`：`未遵循/未实现`
- `externalAgentConfig/*`：`未实现，但属于简单 request/response，不是本文的主风险`

现状：

- 当前本仓库对 realtime 没有任何 command / event 面建模。
- 而且 latest upstream 已经把旧的 `thread/realtime/transcriptUpdated` 收敛成两条通知：
  - `thread/realtime/transcript/delta`
  - `thread/realtime/transcript/done`
- 这说明如果未来要做 browser / voice / websocket client 产品化，不能再按旧名字推断协议。

### 3.17 本轮额外补记的 simple RPC 扩面

这次对照 `openai/codex` 最新 HEAD，相对我们之前引用的 `7999b0f...` 基线，还看到几组**不是主状态机、但会影响“协议面是否完整”判断**的 simple RPC：

- `thread/memoryMode/set`
- `memory/reset`
- `thread/inject_items`
- `marketplace/add`
- `mcpServer/tool/call`
- `configRequirements/read`
- `externalAgentConfig/detect`
- `externalAgentConfig/import`

当前实现结论：

- 这些能力当前大多也没有 relay/headless command 面；
- 但它们**不应该跟多步状态机 backlog 混在一起排优先级**，否则容易把“simple RPC 还没暴露”误判成“主运行时 correctness 还不成立”。

## 4. 一张总表

| 官方状态机 | 当前结论 | 备注 |
| --- | --- | --- |
| `initialize -> initialized` | 遵循 | VS Code 透传不乱序；headless 已在 relay 连接前同步补齐 `initialize -> initialized` 严格握手 |
| `thread/start -> thread/started` | 遵循但有适配压缩 | relay remote prompt 已按这个顺序驱动 |
| `thread/resume` | 遵循但有适配压缩 | remote prompt 恢复 thread 后再补 `turn/start` |
| `thread/fork` | 遵循但有适配压缩 | remote prompt 的 `fork_ephemeral` 已接通 `thread/fork -> turn/start` |
| `thread/list + cursor/filter` | 部分遵循 | 只实现“刷新快照”，固定 `limit=50`，没有 cursor/filter 面 |
| `thread/read(includeTurns)` | 遵循但有适配压缩 | 被压成 `thread.history.read` |
| `thread/loaded/list` | 未遵循/未实现 | 无真实 command 建模 |
| `thread/status/changed` | 未遵循/未实现 | 当前完全没消费 |
| `thread/unsubscribe -> thread/closed` | 未遵循/未实现 | 当前无 command / event 建模；且上游现在是“无 subscriber 且无 activity 30 分钟后才 unload”，不是立即 `closed` |
| `thread/archive/unarchive` | 未遵循/未实现 | 无 command / event 建模 |
| `thread/compact/start` | 遵循但有适配压缩 | 已接通，能看到 `contextCompaction` |
| `thread/rollback` | 未遵循/未实现 | 无 command 建模 |
| `thread/shellCommand` | 未遵循/未实现 | 无 command 建模 |
| `turn/start -> turn/started -> item/* -> turn/completed` | 遵循但有适配压缩 | 核心 turn 生命周期已接住 |
| `turn/steer` | 严格遵循 | `expectedTurnId`、无新 `turn/started` 都已保持 |
| `turn/interrupt` | 严格遵循 | 终态 `interrupted` 已承接 |
| `turn/plan/updated` | 严格遵循 | 有结构化 plan snapshot |
| `turn/diff/updated` | 严格遵循 | authoritative turn 聚合 diff 已进入 canonical event 并可进入 final summary |
| `model/rerouted` | 遵循但有适配压缩 | 已保留 `fromModel` / `toModel` / `reason` 并更新 thread 当前有效模型，但尚未单独做用户提示 |
| 通用 `item/started` / `item/completed` | 部分遵循 | 主流 item 已接；review/imageView/collab 命名仍有缺口 |
| `item/mcpToolCall/progress` | 遵循但有适配压缩 | translator 已标准化 typed progress event，但产品 UI 仍未深度表达 |
| `item/reasoning/summaryPartAdded` | 未遵循/未实现 | 缺失 |
| command/file approval 多步状态机 | 部分遵循 | relay/Feishu/headless 已承接 `item/commandExecution/requestApproval` 与 `item/fileChange/requestApproval`，并把 command/file/network approval 归一化成可渲染 request；更细的专用决策 UI 仍未补齐 |
| `item/permissions/requestApproval` | 部分遵循 | 请求面已补齐，权限子集与 scope 可回写；当前仍走通用 request 卡 |
| `mcpServer/elicitation/request` | 部分遵循 | form/url request 已接入，但仍是产品适配 UI，不是 source-native surface 逐帧复刻 |
| `item/tool/requestUserInput` | 严格遵循 | relay/Feishu/headless 已接；同时兼容官方页面/README 仍写的顶层 `tool/requestUserInput` alias |
| dynamic tool call (`item/tool/call`) | 部分遵循 | relay / Feishu / headless 已接 `request.started -> 自动 unsupported 回写 -> request.resolved` 的最小 fail-closed 链路，但仍未实现真正的 client-side callback executor |
| `review/start -> enteredReviewMode -> exitedReviewMode` | 未遵循/未实现 | relay/headless 无此能力 |
| `command/exec*` | 未遵循/未实现 | 仅解析 turn 内 command item 的 output delta |
| `account/*` auth state machine | 未遵循/未实现 | 完全未建模 |
| `app/list -> app/list/updated` | 未遵循/未实现 | 完全未建模 |
| `mcpServer/oauth/login -> ...completed` | 未遵循/未实现 | 完全未建模 |
| `mcpServer/startupStatus/updated` | 未遵循/未实现 | 当前无 startup status 事件建模 |
| `skills/changed` | 未遵循/未实现 | 当前无 skills invalidation 事件建模 |
| `thread/realtime/*` | 未遵循/未实现 | 当前无 realtime command/event 面；而且最新 upstream 已改成 transcript `delta/done` 两段通知 |
| `fs/watch -> fs/changed -> fs/unwatch` | 未遵循/未实现 | 当前无 watch/session 建模 |
| `windowsSandbox/setup*` | 未遵循/未实现 | 完全未建模 |
| `fuzzyFileSearch/*` | 未遵循/未实现 | 完全未建模 |

## 5. 这次审计最重要的三个结论

### 5.1 真正“严格跟住官方”的，主要还是 turn 主链和常用 item 主链

这部分我们可以有把握地说已经成体系了：

- remote prompt 走 `thread/start|resume -> turn/start -> turn/started -> item/* -> turn/completed`
- `turn/steer`
- `turn/interrupt`
- 常见文本 / 计划 / 推理 / 命令输出 / 文件改动输出事件
- `turn/plan/updated`
- `thread/tokenUsage/updated`

### 5.2 最大结构性缺口在“官方还有很多状态机，我们当前只让 VS Code 透传，不让 relay/headless 承接”

也就是说：

- 它们不一定会把 VS Code 客户端搞坏；
- 但我们自己的 Feishu / headless / relay 侧，其实还没有真正支持。

最典型的就是：

- `review/start`
- command/file approvals
- `thread/status/changed`
- `account/*`
- `app/list/updated`
- `mcpServer/oauth/login`

### 5.3 现在最容易误判的点是“透传不等于实现”

`internal/app/wrapper/app_io.go` 的策略让很多官方方法在本地 client 模式下仍能工作，但这不意味着：

- daemon 知道这条状态机发生了什么
- Feishu 能展示
- headless 能主动发起
- relay 状态机能正确收敛

后续讨论改法时，必须明确每一条需求到底是想要：

1. **只要不破坏 VS Code/native client 即可**；还是
2. **要把它纳入 relay/Feishu/headless 的正式状态机**。

## 6. 二次比对后的修正点

### 6.1 官方页面 / README / canonical schema 目前并不完全一致

- 官方页面与 README 目前都还写 `tool/requestUserInput`，但 `openai/codex` 当前 canonical schema / `common.rs` / `ServerRequest.json` 真正定义的是 `item/tool/requestUserInput`。
- 官方页面当前没有把 `item/permissions/requestApproval` 与 `mcpServer/elicitation/request` 放进 approvals 主叙事里，但最新 source / schema 已经明确存在这两条 request surface。
- 这意味着：**只看网页会低估 request surface 的真实范围；只看 README 的文字示例又会误把顶层 `tool/requestUserInput` 当成 canonical wire name。**

### 6.2 相对我们此前引用的 upstream `7999b0f...`，当前 `18d61f6...` 已确认的协议变化

- `thread/realtime` 的 transcript 通知已经从旧的 `thread/realtime/transcriptUpdated` 收敛成：
  - `thread/realtime/transcript/delta`
  - `thread/realtime/transcript/done`
- `thread/realtime/start` 现在显式带 `outputModality`。
- `thread/unsubscribe` 不再等价于“最后一个 subscriber 走后立刻 unload”。当前上游语义是：最后一个 subscriber 离开后，thread 要在“无 subscriber 且无 thread activity 持续 30 分钟”后才 unload，并发 `thread/closed` / `thread/status/changed(notLoaded)`。
- 上游最近还新增或正式化了几组 simple RPC：`thread/memoryMode/set`、`memory/reset`、`thread/inject_items`、`marketplace/add`、`mcpServer/tool/call`。它们不一定构成高优先级状态机，但需要在后续 protocol coverage 盘点里单列，不要被误以为“旧文档已穷尽全部 surface”。

### 6.3 这份审计此前漏记的族群

- 旧版审计没有显式列出 `thread/realtime/*`、`mcpServer/startupStatus/updated`、`skills/changed`、`fs/watch -> fs/changed -> fs/unwatch`。
- 旧版 item 段落也漏记了当前本仓库已经承接的 `item/mcpToolCall/progress`。
- 因此，后续如果有人拿旧版审计判断“哪些还没接”，应以本文这次二次回写后的结论为准。

## 7. 目标遵循策略判定原则

### 7.1 先区分四种“目标”，再判断要不要做产品化

每一类协议面，至少要分清四层目标：

1. `wrapper correctness target`
   - 只问：会不会破坏 VS Code / native client 的原生协议路径。
2. `relay canonicalization target`
   - 只问：translator 有没有把它变成 daemon/remote surface 可消费的稳定语义。
3. `product surface target`
   - 只问：Feishu / relay UI 要不要把它做成正式产品交互。
4. `headless autonomy target`
   - 只问：没有 VS Code 时，我们要不要自己主动驱动这条状态机。

**透传不等于实现。**
很多 surface 只满足第 1 层，不满足第 2~4 层。

### 7.2 什么时候必须严格遵循协议

下面这类属于 connection / ownership / terminal correctness，不能靠产品层猜：

- `initialize -> initialized`
- `thread/start | thread/resume | thread/fork`
- `turn/start | turn/steer | turn/interrupt`
- request / response 关联与 `serverRequest/resolved`
- thread/turn/item 的归属关系与 terminal status

这类即使 UI 要压缩，也只能**压显示**，不能改协议不变量。

### 7.3 什么时候允许做产品语义适配

下面这类可以做“语义压缩 / UI 重投影”，但必须守住协议不变量：

- `thread/read` / `thread/list` 的 snapshot 化展示
- `turn/plan/updated`、`turn/diff/updated`、`model/rerouted`
- item lifecycle 与主流 delta
- command/file approval、permissions request、MCP elicitation、request_user_input

允许适配不代表允许：

- 改变时序；
- 丢掉 requestId / threadId / turnId / itemId；
- 虚构终态；
- 把 `pending` / `resolved` / `notLoaded` / `waitingOnApproval` / `review mode` 之类运行时语义推断错。

### 7.4 什么时候只要求 wrapper / native path 不破坏

如果某条 surface：

- 明显是 native / browser / IDE 优先；
- 当前产品没有要把它放进 Feishu / headless；
- 实现成本高、收益低；

那么短期目标就可以只是：

- wrapper 不破坏透传；
- relay/headless 不 claim 支持；
- 不把它混入当前产品状态机。

### 7.5 什么时候当前无需纳入产品面

像下面这些，如果没有明确产品需求，当前更适合留在“后续按需决定”：

- `review/start`
- `thread/realtime/*`
- `fs/watch -> fs/changed -> fs/unwatch`
- `windowsSandbox/*`
- `fuzzyFileSearch/*`
- `account/*`
- `app/list` / `mcpServer/oauth/login`

这类不是“永远不做”，而是**不应该先和主聊天 turn 正确性绑在一起做**。

## 8. 分组决策矩阵

| 分组 | 目标遵循策略 | wrapper 目标 | relay/translator 目标 | orchestrator / Feishu 目标 | headless 目标 | 必守不变量 |
| --- | --- | --- | --- | --- | --- | --- |
| 连接初始化 `initialize -> initialized` | `必须严格遵循协议` | 透传且不乱序 | 不必产品化，但不能假设未初始化也可继续 | 无需独立 UI | 主动补齐 `initialized` | 握手顺序、capabilities、生效前拒绝其他方法 |
| thread/turn 主链：`thread/start/resume/fork`、`turn/start/steer/interrupt` | `必须严格遵循协议` | 不吞 response / notification | 可以压缩 response，但必须保持“先 thread、再 turn”、`expectedTurnId`、终态语义 | UI 可合并显示 | 无 VS Code 时也要主动按协议驱动 | 顺序、thread/turn 归属、单活 turn、terminal status |
| 线程运行时 / turn 派生通知：`thread/status/changed`、`turn/diff/updated`、`turn/plan/updated`、`model/rerouted`、`thread/tokenUsage/updated` | `允许做产品语义适配，但要守住协议不变量` | 透传 | 归一化成 canonical event | 用更自然 UI 展示 | 至少消费会影响路由/门禁的状态 | 最新 state snapshot、threadId/turnId 关联、`notLoaded` / `waitingOnApproval` / reroute 语义 |
| item 生命周期 / 主流 delta：`item/*`、`item/mcpToolCall/progress` | `允许做产品语义适配，但要守住协议不变量` | 透传 | 标准化 `started/delta/completed` | 按 item kind 投影 UI | 无需主动发起，但要能理解 | `itemId` 连续性、`started -> delta* -> completed`、最终 `item/completed` 权威 |
| request surfaces：command/file approval、`item/permissions/requestApproval`、`mcpServer/elicitation/request`、`item/tool/requestUserInput` | `允许做产品语义适配，但要守住协议不变量` | 透传真实 `requestId` / params | 统一 request abstraction 可以，但要保留 method / requestType / availableDecisions / scope / nullable `turnId` | 卡片可产品化，但 resolve 前后 gate 必须准确 | 必须能回写响应 | `requestId` 关联、pending/resolved、granted subset / action / unanswered 语义 |
| dynamic tool call：`dynamicToolCall -> item/tool/call` | `允许做产品语义适配，但仅在决定支持时` | 透传 | 当前已建模 request / resolve，并在 relay 路径自动回写 unsupported；若 claim full support，仍必须补齐真正 callback executor | 当前 UI 只做只读 fail-closed 提示，不暴露交互表单 | headless 当前同样自动回写 unsupported | `callId`、item lifecycle、success/result 不能丢 |
| review / realtime / fs watch / windows / fuzzy search | `当前无需纳入产品面，后续按需求再决定` | 只要不破坏 native path | 不 claim 支持时可不建模 | 默认不产品化 | 暂不主动驱动 | 一旦 claim 支持，就要遵守 detached vs inline、`sessionId` / `watchId`、close/completed 终态 |
| account / app / MCP OAuth / skills / plugin/marketplace 邻接面 | `只要求 wrapper / VS Code 透传不破坏` | 透传 | 可先不建模 | 默认不放进 chat 主交互 | 暂不主动驱动 | login completed、OAuth completed、list invalidation 等通知不能被误改语义 |
| simple RPC：`thread/memoryMode/set`、`memory/reset`、`thread/inject_items`、`marketplace/add`、`mcpServer/tool/call` | `不属于本轮主状态机优先级` | 透传 | 后续如实现，单独设计 command 语义 | 不要硬塞进现有 turn/approval UI | 暂不支持 | 不和状态机 backlog 混淆 |

## 9. 推荐后续顺序

如果后面要继续改，我建议按这个顺序讨论，而不是一次把所有官方 surface 都铺平：

1. 先修“明确不严格遵循且已经影响我们产品判断 / correctness”的：
   - `initialize -> initialized` 的 headless 缺口（已由 `#228` 修复，后续不再作为待补 correctness 缺口）
   - `thread/status/changed`
2. 再补“官方多步状态机、且我们很可能迟早要产品化”的：
   - 更细粒度 approval / permissions / elicitation 决策 UI
   - dynamic tool `item/tool/call` 的真正 callback executor
   - `review/start`
3. 然后再看“明显偏 native/browser 客户端，但后续也许值得做”的：
   - `mcpServer/startupStatus/updated`
   - `app/list/updated`
   - `mcpServer/oauth/login -> ...completed`
   - `skills/changed`
4. 明确放到后面、不要和 turn 主链 correctness 混做的：
   - `thread/realtime/*`
   - `fs/watch -> fs/changed -> fs/unwatch`
   - `windowsSandbox/*`
   - `fuzzyFileSearch/*`
   - `account/*`
   - 各类 simple RPC（`memory/reset`、`thread/inject_items`、`marketplace/add`、`mcpServer/tool/call` 等）

## 10. 证据索引

官方页面：

- <https://developers.openai.com/codex/app-server>

上游 canonical source（本轮复核到 `openai/codex` HEAD `18d61f6923896aa273ae9a369d423ac75dd8963a`）：

- `codex-rs/app-server/README.md`
- `codex-rs/app-server-protocol/src/protocol/common.rs`
- `codex-rs/app-server-protocol/src/protocol/v2.rs`
- `codex-rs/app-server-protocol/schema/json/ServerRequest.json`
- `codex-rs/app-server-protocol/schema/json/ServerNotification.json`

本仓库关键证据：

- `internal/app/wrapper/app_io.go`
- `internal/app/wrapper/app_headless.go`
- `internal/adapter/codex/translator_commands.go`
- `internal/adapter/codex/translator_observe_client.go`
- `internal/adapter/codex/translator_observe_server.go`
- `internal/adapter/codex/translator_helpers.go`
- `internal/adapter/codex/translator_helpers_request.go`
- `internal/core/agentproto/types.go`
- `internal/app/wrapper/app_test.go`
- `internal/adapter/codex/translator_test.go`
- `internal/adapter/codex/translator_requests_test.go`
- `internal/adapter/codex/translator_compact_test.go`

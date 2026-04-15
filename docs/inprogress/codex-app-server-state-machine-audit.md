# Codex App Server 状态机遵循度审计

> Type: `inprogress`
> Updated: `2026-04-16`
> Summary: 对照 OpenAI 官方 Codex App Server 文档，按 VS Code 透传、relay/Feishu 归一化、headless 主动驱动三层审计当前仓库对各类状态机的遵循程度；本轮回写补记 `#222` 已补齐 command/file approval 与顶层 `tool/requestUserInput` 的 relay/Feishu/headless 承接，剩余 gap 主要转向更细粒度 request UI 与其他未建模状态机。

## 1. 审计范围与判定口径

官方基线：<https://developers.openai.com/codex/app-server>

这次不只看“某个 JSON-RPC 方法有没有出现”，而是看**状态机有没有被我们正确承接**。为了避免把“纯透传”误判成“已实现”，本文把仓库分成三层：

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

当前实现结论：`部分遵循`

- `VS Code 透传层`：
  - **大体可用**。wrapper 会把来自上游 client 的未知方法原样转发给 Codex，也会把未知响应/通知原样回传。
  - 所以如果本地 client 自己完整执行 `initialize -> initialized`，wrapper 一般不会破坏它。
  - 证据：`internal/app/wrapper/app_io.go`
- `headless 主动驱动层`：
  - **没有严格遵循**。目前只合成了 `initialize`，没有继续发 `initialized`。
  - 证据：`internal/app/wrapper/app_headless.go`
- `relay/Feishu 归一化层`：
  - **没有建模**初始化状态，也没有把 `optOutNotificationMethods` 等连接级能力显式建模到 headless bootstrap。

结论：

- 这条在“本地 client 自己管”的 VS Code 模式下问题不大。
- 但在 **headless 模式下不是严格遵循官方握手**，这是一个明确缺口。

### 3.2 线程启动/恢复主路径：`thread/start | thread/resume | thread/fork`

官方基线：

- `thread/start`：新建 thread，发 `thread/started`
- `thread/resume`：恢复 thread
- `thread/fork`：分叉历史到新 thread，并发 `thread/started`

当前实现结论：

- `thread/start`：`遵循但有适配压缩`
- `thread/resume`：`遵循但有适配压缩`
- `thread/fork`：`未遵循/未实现`

现状：

- relay 的 `prompt.send` 在缺 thread 时会先发 native `thread/start`，收到结果后再自动补 `turn/start`。
- 已有 thread 但未聚焦时，会先发 `thread/resume`，再自动补 `turn/start`。
- 这条“先 thread，再 turn”的官方顺序是对的。
- 但我们在 relay 层把中间 JSON-RPC response 压平了，不把 `thread/start` / `thread/resume` response 暴露成上层状态机的一部分。
- `thread/fork` 完全没有对应 command/event 建模。

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

### 3.8 Turn 衍生通知：`turn/plan/updated`、`turn/diff/updated`

当前实现结论：

- `turn/plan/updated`：`严格遵循`
- `turn/diff/updated`：`未遵循/未实现`

现状：

- 计划更新已经有结构化快照抽取。
- 但官方文档里明确存在的 `turn/diff/updated` 当前完全没有处理。
- 这意味着我们虽然能从 `fileChange` item 里拿到单项 diff，但**没有官方聚合 diff 的 turn 级状态机**。

证据：

- `internal/adapter/codex/translator_observe_server.go`
- 仓库内无 `turn/diff/updated` 命中

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

部分缺口：

- `item/reasoning/summaryPartAdded`
  - 官方文档有，当前没有处理。
- `enteredReviewMode` / `exitedReviewMode`
  - 虽然 generic `item/started/completed` 能把它们作为原始 item type 吞进来，但没有专门语义，也没有产品化路径。
- `collabToolCall`
  - 官方文档里的名字是 `collabToolCall`；当前 normalizer 识别的是 `collabAgentToolCall` / `collab_agent_tool_call`，未见对官方命名的显式兼容。
- `imageView`
  - 官方文档列出了 `imageView` item，当前没有专门 normalizer/metadata 抽取。

证据：

- `internal/adapter/codex/translator_observe_server.go`
- `internal/adapter/codex/translator_helpers.go`

### 3.10 审批状态机

官方文档在这个页面里明确写了三条：

1. `item/commandExecution/requestApproval`
2. `item/fileChange/requestApproval`
3. `tool/requestUserInput` / 文内又写成 `item/tool/requestUserInput`

当前实现结论：`部分遵循；核心交互请求面已补齐`

现状：

- 已有：
  - 泛化 `serverRequest/started` / `serverRequest/resolved`
  - `item/commandExecution/requestApproval`
  - `item/fileChange/requestApproval`
  - `tool/requestUserInput`
  - `item/tool/requestUserInput`
  - 以及仓库中后来补的 `item/permissions/requestApproval`、`mcpServer/elicitation/request`（这两条超出本页主线，但属于我们额外追上 upstream 的部分）
- 当前剩余边界：
  - command/file approval 当前会先归一化成 `approval_command`、`approval_file_change`、`approval_network`，并复用通用 request 卡，而不是 1:1 复制 upstream 的更细专用 UI
  - `availableDecisions` 已会继续透传到 request options，包含 `cancel`；但像 `acceptWithExecpolicyAmendment` 这类更细决策还没有专门交互面

这意味着：

- relay/Feishu/headless 已经不会再把这几类真实 request 静默吞掉；
- 但**还不是严格按这页官方每一条专用 UI surface 完整复制**；
- 当前更准确的表述是：主链路已补齐，剩余差异集中在更细粒度 approval 决策 UI，而不是“请求根本没有承接”。

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

### 3.15 Apps / MCP OAuth / 其他连接器状态机

包含：

- `app/list -> app/list/updated`
- `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`
- `mcpServerStatus/list`
- `mcpServer/resource/read`
- `config/mcpServer/reload`

当前实现结论：`未遵循/未实现`

其中最需要注意的是两条真正带状态推进的：

- `app/list -> app/list/updated`
- `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`

这两条在当前 relay/headless 侧都没有承接。

### 3.16 Windows / fuzzy search / 外部 agent 配置迁移

包含：

- `windowsSandbox/setupStart -> windowsSandbox/setupCompleted`
- `fuzzyFileSearch/sessionUpdated -> fuzzyFileSearch/sessionCompleted`
- `externalAgentConfig/detect`
- `externalAgentConfig/import`

当前实现结论：

- `windowsSandbox/setup*`、`fuzzyFileSearch/*`：`未遵循/未实现`
- `externalAgentConfig/*`：`未实现，但属于简单 request/response，不是本文的主风险`

## 4. 一张总表

| 官方状态机 | 当前结论 | 备注 |
| --- | --- | --- |
| `initialize -> initialized` | 部分遵循 | VS Code 透传可用；headless 只发了 `initialize`，没发 `initialized` |
| `thread/start -> thread/started` | 遵循但有适配压缩 | relay remote prompt 已按这个顺序驱动 |
| `thread/resume` | 遵循但有适配压缩 | remote prompt 恢复 thread 后再补 `turn/start` |
| `thread/fork` | 未遵循/未实现 | 无 relay/headless command 建模 |
| `thread/list + cursor/filter` | 部分遵循 | 只实现“刷新快照”，固定 `limit=50`，没有 cursor/filter 面 |
| `thread/read(includeTurns)` | 遵循但有适配压缩 | 被压成 `thread.history.read` |
| `thread/loaded/list` | 未遵循/未实现 | 无真实 command 建模 |
| `thread/status/changed` | 未遵循/未实现 | 当前完全没消费 |
| `thread/unsubscribe -> thread/closed` | 未遵循/未实现 | 无 command / event 建模 |
| `thread/archive/unarchive` | 未遵循/未实现 | 无 command / event 建模 |
| `thread/compact/start` | 遵循但有适配压缩 | 已接通，能看到 `contextCompaction` |
| `thread/rollback` | 未遵循/未实现 | 无 command 建模 |
| `thread/shellCommand` | 未遵循/未实现 | 无 command 建模 |
| `turn/start -> turn/started -> item/* -> turn/completed` | 遵循但有适配压缩 | 核心 turn 生命周期已接住 |
| `turn/steer` | 严格遵循 | `expectedTurnId`、无新 `turn/started` 都已保持 |
| `turn/interrupt` | 严格遵循 | 终态 `interrupted` 已承接 |
| `turn/plan/updated` | 严格遵循 | 有结构化 plan snapshot |
| `turn/diff/updated` | 未遵循/未实现 | 当前完全没处理 |
| 通用 `item/started` / `item/completed` | 部分遵循 | 主流 item 已接；review/imageView/collab 命名仍有缺口 |
| `item/reasoning/summaryPartAdded` | 未遵循/未实现 | 缺失 |
| command/file approval 多步状态机 | 部分遵循 | relay/Feishu/headless 已承接 `item/commandExecution/requestApproval` 与 `item/fileChange/requestApproval`，并把 command/file/network approval 归一化成可渲染 request；更细的专用决策 UI 仍未补齐 |
| `tool/requestUserInput` / `item/tool/requestUserInput` | 严格遵循 | relay/Feishu/headless 现在同时识别顶层与 `item` 形式，并继续复用现有 `request_user_input` 草稿/提交状态机 |
| dynamic tool call (`item/tool/call`) | 部分遵循 | 只接 item 生命周期，没接 client 回写 request |
| `review/start -> enteredReviewMode -> exitedReviewMode` | 未遵循/未实现 | relay/headless 无此能力 |
| `command/exec*` | 未遵循/未实现 | 仅解析 turn 内 command item 的 output delta |
| `account/*` auth state machine | 未遵循/未实现 | 完全未建模 |
| `app/list -> app/list/updated` | 未遵循/未实现 | 完全未建模 |
| `mcpServer/oauth/login -> ...completed` | 未遵循/未实现 | 完全未建模 |
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
- `dynamicToolCall -> item/tool/call`
- `thread/status/changed`
- `turn/diff/updated`
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

## 6. 建议的后续讨论顺序

如果后面要继续改，我建议按这个顺序讨论，而不是一次把所有官方 surface 都铺平：

1. 先修“明确不严格遵循且已经会影响我们产品判断”的：
   - `initialize -> initialized` 的 headless 缺口
   - `thread/status/changed`
   - `turn/diff/updated`
2. 再补“官方多步状态机、且我们很可能迟早要产品化”的：
   - `review/start`
   - dynamic tool `item/tool/call`
3. 最后再看“更多是能力缺失，不是当前主路径 correctness 问题”的：
   - `account/*`
   - `app/list/updated`
   - `mcpServer/oauth/login`
   - `windowsSandbox/*`
   - `fuzzyFileSearch/*`

## 7. 证据索引

官方文档：

- <https://developers.openai.com/codex/app-server>

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

# Feishu Request Delivery Reliability Design

> Type: `draft`
> Updated: `2026-05-08`
> Summary: 设计一套 request owner 冻结、可见性交付状态与重投恢复主链，解决 Claude 特别是 Task/subagent 场景下“待授权请求未成功出现在前台却已卡住 turn”的问题。
>
> Related Issue: `#598`

## 1. 背景

当前远端 request（approval、`request_user_input`、`permissions_request_approval`、`mcp_server_elicitation`）在产品语义上已经属于会阻塞当前 turn 的一等状态：

- `request.started` 到达后，surface 会进入 `PendingRequest` gate
- 普通文本、图片、文件、部分路由切换会被冻结
- turn 要等待用户处理 request 才能继续

但它在投递语义上仍接近一次普通 UI event：

- request card 通过普通 `UIEvent -> gateway.Apply(...)` 发送到飞书
- 若发送失败，不会回滚 `PendingRequest`
- 若发送失败，也没有 request 自身的独立重投主链
- surface 侧只知道“当前有 pending request”，不知道“当前 request 是否真正成功出现在前台”

这在 Claude backend，尤其是 `Task` / delegated task 场景下更容易暴露：

- `Task` 当前不是 internal helper traffic，而是挂在同一个外层 turn 下的 `delegated_task`
- 子任务里的工具调用仍会复用外层 `activeTurn.ThreadID / TurnID`
- 子任务更容易产生多次工具授权或补充输入 request
- 一旦 request card 投递失败、落错 surface、或因队列串行没有成功进入当前可见前台，用户体感会变成“Task 卡住了，但前台没有弹授权卡”

这不是一个文案或 notice 缺失问题，而是 request 作为阻塞性业务状态，却没有配套的“可见性交付合同”。

## 2. 问题定义

当前实现存在三类结构性缺口：

### 2.1 request owner 在运行时靠 turn 反查

`presentRequestPrompt(...)` 会通过 `turnSurface(instanceID, threadID, turnID)` 当场决定 request 属于哪个 surface。

这会导致：

- 如果 remote turn binding 在恢复、续跑、切线程、Task 嵌套时不稳，request 可能落错 surface
- 如果 binding 已丢，只能退回 `threadClaimSurface(threadID)`，可能投到非当前用户正在看的前台

### 2.2 request 进入 gate 与 request 成功可见没有形成合同

当前只要 request record 被加入 `PendingRequests`，surface gate 就已经成立。

但这时 request card 可能：

- 尚未发出
- 发出失败
- 发到错误前台
- 发出后用户当前并不可见

即业务状态已经阻塞，但前台状态未被确认。

### 2.3 request 没有自己的恢复状态机

当前飞书投递失败后，系统只会：

- 记录一条 `gateway_apply_failed` 的 global runtime notice

但不会：

- 标记“哪一条 request 当前不可见”
- 对当前 active request 建立专门 redelivery 队列
- 在后续输入、状态刷新、定时心跳中优先重投 active request
- 给 blocker 明确区分“request 正常待处理”与“request 未成功送达”

## 3. 目标

本设计希望达成：

1. request 一旦进入阻塞态，必须有明确 owner，不再靠后续 event 临时猜归属。
2. request 的“进入 gate”与“成功可见”建立显式状态机。
3. request 投递失败后，不再只依赖泛化 notice，而是由 request 自身主导恢复。
4. Claude `Task` / subagent 场景下，子任务 request 仍复用统一 substrate，但用户能明确知道当前卡住的是哪类 request、来自哪段 delegated task 上下文。
5. 该方案要成为 request family 的统一基线，而不是只修 Claude 或只修某个 gateway 失败分支。

## 4. 非目标

本设计不直接处理：

- 重新设计 request 的视觉样式
- 放宽 request gate 的产品策略
- 改写 Claude translator 对 `can_use_tool` / `AskUserQuestion` / `ExitPlanMode` 的分类语义
- 为 delegated task 单独建立第二套 request substrate

## 5. 方案总览

核心思想：

**把 request 从“普通 UI event 投影物”升级成“带 owner 与 visibility lifecycle 的前台业务实体”。**

统一新增一条 `Request Delivery Contract`：

1. request owner 冻结
2. request visibility state 显式建模
3. request redelivery 成为统一恢复主链
4. request gate / notice / status / slash 行为都读取同一 visibility state

## 6. 数据模型调整

建议在 `state.RequestPromptRecord` 上增加以下 carrier：

- `OwnerSurfaceSessionID`
- `OwnerGatewayID`
- `OwnerChatID`
- `VisibilityState`
- `VisibleMessageID`
- `VisibleAt`
- `LastDeliveryAttemptAt`
- `LastDeliveryError`
- `NeedsRedelivery`
- `DeliveryAttemptCount`
- `SourceContextLabel`

其中：

- `VisibilityState`
  - `pending_visibility`
  - `visible`
  - `delivery_degraded`
  - `resolved`
- `VisibleMessageID`
  - 当前 active request card 真正送达后的 message id
- `SourceContextLabel`
  - 用于给 Claude Task / delegated task 场景做前台来源说明，例如 `Task (Explore)`

## 7. 状态机

### 7.1 request lifecycle

建议的 request lifecycle：

1. `editing/pending_visibility`
2. `editing/visible`
3. `editing/delivery_degraded`
4. `waiting_dispatch/visible`
5. `waiting_dispatch/delivery_degraded`
6. `resolved`

关键约束：

- `PendingRequest` gate 可以在 `pending_visibility` 之后立即成立
- 但 blocker 和 status 必须知道当前 request 不是“正常可见待处理”，而是“待显示/显示失败”
- `delivery_degraded` 不允许 silently 退回普通 editing 语义

### 7.2 delivery sub-state

#### `pending_visibility`

表示：

- request 已进入业务 gate
- owner 已冻结
- 尚未确认卡片成功送达

#### `visible`

表示：

- 当前 request card 已成功送到 owner surface
- 用户可从当前 frontstage 处理

#### `delivery_degraded`

表示：

- request 仍阻塞当前 turn
- 但最近一次投递失败或当前可见锚点失效
- 系统应优先尝试 redelivery

## 8. Owner 冻结

### 8.1 原则

request owner 应在 request record 创建时冻结，不在后续 refresh / resolve / retry 时重新通过 `turnSurface(...)` 反查。

建议来源优先级：

1. active remote turn binding 的 `SurfaceSessionID`
2. 当前 review/detached owner surface
3. 明确的 initiator surface

只有创建 request 时可以用现有 lookup 逻辑推一次 owner；一旦 record 落地，后续都只认 record 自身 owner。

### 8.2 效果

这样可以避免：

- request 因 active binding 漂移而发到别的 surface
- request resolve / refresh 时跨 surface 清理不一致
- Task 内部 request 跟着 thread claim 漂走

## 9. 投递与回填

### 9.1 统一激活入口

建议把现有：

- `activatePendingRequest(...)`
- `autoDispatchUnsupportedToolCallback(...)`
- request refresh

统一收口到：

- `ensurePendingRequestVisible(...)`

职责：

1. 检查 request owner
2. 检查 current visibility state
3. 选择 append / update / replace 的具体投递动作
4. 设置 `LastDeliveryAttemptAt`
5. 发送投递
6. 成功后回填 `VisibleMessageID/VisibleAt/VisibilityState=visible`
7. 失败后写入 `LastDeliveryError/NeedsRedelivery/VisibilityState=delivery_degraded`

### 9.2 gateway success 回填

当前 `gateway.Apply(...)` 已能得到发送/回复后的 `message_id`。

建议为 request event 增加一层 delivery callback / apply result 回写，使 daemon 能把“这次 request card 成功送达后的 message_id”同步回 orchestrator runtime。

没有这一步，就无法把 request 真正变成有锚点的 frontstage owner。

### 9.3 具体落点

当前可复用的实际代码缝有三处：

1. `internal/app/daemon/app_ui.go`
   - `deliverUIEventWithContextMode(...)` 当前会在本地构造 `operations := projector.ProjectEvent(...)`
   - `gateway.Apply(...)` 成功后，`operations[i]` 已会被 gateway runtime 原地回填 `MessageID`
   - 但 app 当前没有把这份 apply 结果再反馈给 orchestrator
2. `internal/adapter/feishu/controller_gateway.go`
   - `MultiGatewayController.Apply(...)` 已把 worker 内 mutate 过的 `group[i]` 回写到原 `operations[index]`
   - 说明 app 层其实已经拿得到“最终送达后的 operation”
3. `internal/adapter/feishu/gateway_runtime.go`
   - `OperationSendCard` / `OperationSendText` 在 create/reply 成功后会写回 `operation.MessageID`

因此这里不建议再额外发一条“request 已送达”的虚拟 event；更干净的做法是：

- 在 app 层新增统一的 `UI delivery report` 回填
- orchestrator 对 request family 消费该 report，并把 request visibility / visible message anchor 写回 runtime

这样 request 运行时拿到的是“真实 gateway 送达结果”，而不是二手推测。

## 10. 恢复主链

### 10.1 redelivery 队列

建议在 surface runtime 中新增 active request redelivery lane：

- 同一 surface 同时只 redeliver active request
- 队头 request 未恢复前，不推进后续 queued request 的可见化

### 10.2 触发时机

以下任一时机都应优先调用 `ensurePendingRequestVisible(...)`：

- request 刚创建
- request 成为 pending 队头
- 当前 surface 收到任意新 inbound action
- `/status`
- dispatch blocker notice 生成前
- daemon tick / 轻量定时器
- global runtime notice flush 前

### 10.3 降级策略

如果连续多次 redelivery 失败：

- request 继续保留 gate
- 明确进入 `delivery_degraded`
- blocker 文案升级为“当前 turn 正在等待授权，但请求卡尚未成功送达”
- 指向可恢复动作：等待自动重试、重新触发 `/status`、或 `/stop`

## 11. 前台投影调整

### 11.1 request blocker 文案

当前 `request_pending` 应拆成至少两类：

- 正常可见 request pending
- request delivery degraded

后者要明确告诉用户：

- 当前不是“你没点卡”
- 而是“系统还没把这张卡成功送到前台”

### 11.2 request card 标题/副标题

Claude delegated task 场景下，如能解析到 `SourceContextLabel`，建议在 request view 中追加来源说明：

- `来自 Task (Explore)`
- `来自 delegated task`

这能减少用户把“Task 卡住”误解成模型死掉。

### 11.3 不再把 request 当成纯 append-only 临时卡

当前 request projector 路径是：

- `requestPromptEvent(...)` 只发新的 request card
- request 本身没有 `MessageID`
- 后续等待 dispatch / cancel / resolve 主要依赖 inline callback replace 或直接清 runtime

这意味着 request 在“首次送达成功”之后，并没有一个被 runtime 明确认领的 owner card。

建议改成：

1. 首次 visible 前：仍允许 append/reply 送出新卡
2. 首次 visible 后：`VisibleMessageID` 成为 request owner card 锚点
3. 后续 waiting_dispatch / cancelled / degraded refresh 优先 update 同一张卡
4. 只有在 message 锚点失效时才退回 redelivery create/reply

这样可以直接复用现有 target picker / path picker 的“有锚点 owner card”语义，而不是再造一套 request 专属 patch 协议。

## 12. 与 Claude Task / subagent 的关系

本设计不单独为 Task 造第二套 request substrate。

原因：

- 当前实现里，Task 只是同一外层 turn 中的 `delegated_task`
- 子任务内工具授权仍然属于外层 turn 的业务阻塞状态
- 若为 Task 单独拆 request substrate，会引入并行 gate / 双重 owner / 双重恢复链，复杂度更高

正确做法是：

- 继续复用统一 request substrate
- 只在 request metadata / 投影层增强来源上下文
- 让 request owner / visibility / redelivery 统一处理所有 backend、所有 request family

## 13. 对现有逻辑的替换原则

以下做法应避免继续长期共存：

1. “request 已 pending，但只顺手发一张普通 event 卡片”
2. “投递失败后只丢一条 global runtime notice”
3. “request refresh / resolve 重新用 `turnSurface(...)` 猜 owner”

建议：

- 保留旧 helper 仅作为过渡适配层
- 最终由 request runtime 单点持有 owner + visibility + redelivery 合同

## 14. 测试建议

至少补以下测试面：

1. request 创建后首次投递成功
2. request 创建后首次投递失败，进入 `delivery_degraded`
3. surface 后续任意 action 触发 request redelivery
4. active request 未 visible 时，blocker 文案进入 degraded 分支
5. queued request 只有在前一条 resolved 后才 redeliver 成为队头
6. Claude Task 场景下，delegated task 内部 request 仍归属外层 surface
7. owner surface 漂移后，request refresh 仍落回冻结 owner
8. resolve 时按 request record owner 清理，而不是重新反查 surface

## 15. 风险与取舍

- 该设计会让 request runtime 与 gateway apply 结果耦合更深，但这是必须代价，因为 request 已经是阻塞型业务状态
- 实现后，request 相关状态机会比现在更显式，也意味着 `remote-surface-state-machine.md` 与 `feishu-card-ui-state-machine.md` 都需要同步
- 如果后续要支持别的 frontstage，不应把这套逻辑写死在 Feishu adapter，而应保持在 orchestrator / request runtime 层

## 16. 建议执行顺序

1. 冻结 request owner
2. 增加 visibility state carrier
3. 增加 request delivery success/failure 回填
4. 建立 active request redelivery 主链
5. 改 blocker / status / `/status` 投影
6. 增补 Claude Task 来源上下文
7. 同步状态机文档与回归测试

## 17. 实现切缝建议

建议把实现拆成 3 条顺序明确的 worker 单元：

### 17.1 子单 A：request owner + visibility carrier

目标：

- 在 request record 创建时冻结 owner
- 引入 `VisibilityState` / `VisibleMessageID` / `NeedsRedelivery`
- 让 resolve / refresh / clear 都只认 request record 自身 owner

主改动面：

- `internal/core/state/types.go`
- `internal/core/orchestrator/service_request.go`
- `internal/core/orchestrator/service_routing_request_state.go`
- `internal/core/orchestrator/service_helpers_request.go`

完成后验收：

- owner 不再依赖后续 `turnSurface(...)` 反查漂移
- request queue 仍保持串行
- 旧测试对 reply anchor / detached branch / queue promotion 保持通过

### 17.2 子单 B：UI delivery feedback + request redelivery runtime

目标：

- 把 gateway apply 成功/失败结果稳定回填给 orchestrator
- 建立 `ensurePendingRequestVisible(...)` 与 active request redelivery lane

主改动面：

- `internal/app/daemon/app_ui.go`
- `internal/adapter/feishu/controller_gateway.go`
- `internal/adapter/feishu/gateway_runtime.go`
- `internal/core/orchestrator/service_request*.go`

完成后验收：

- 首次 request 送达成功后记录真实 `VisibleMessageID`
- 首次投递失败后进入 `delivery_degraded`
- 后续 inbound action 或 `/status` 可触发 redelivery recovery

### 17.3 子单 C：frontstage degraded 投影 + Claude Task 来源上下文

目标：

- blocker / notice / `/status` 区分 visible 与 degraded
- request card 展示 delegated task 来源上下文
- 同步状态机文档与回归测试

主改动面：

- `internal/core/orchestrator/service_overlay_runtime.go`
- `internal/core/orchestrator/service_snapshot.go`
- `internal/app/daemon/app_attention_ping.go`
- `internal/adapter/claude/observe*.go`
- `docs/general/remote-surface-state-machine.md`
- `docs/general/feishu-card-ui-state-machine.md`

完成后验收：

- 用户能区分“等待你点卡”与“卡片尚未成功送达”
- Claude Task / delegated task 场景会显示来源上下文
- 文档与实现行为同步

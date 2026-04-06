# Feishu 确认请求增强设计

> Type: `implemented`
> Updated: `2026-04-06`
> Summary: 标记为已实现文档并迁移到 `docs/implemented`，同步修正对 canonical 文档的引用。

## 1. 文档定位

这份文档描述的是 **Feishu 侧确认请求增强方案**，记录这次 approval request 改造的设计依据、交互取舍和最终落地形态。

当前仓库已经按本文方案完成实现。实际当前行为说明仍以：

- [feishu-product-design.md](../general/feishu-product-design.md)
- [relay-protocol-spec.md](../general/relay-protocol-spec.md)

为准。

## 2. 背景

改造前，Feishu 端的 approval request 只有两种决策：

- `accept`
- `decline`

这和本地 VS Code / Codex 的真实交互不一致。

根据已确认的 VS Code 前端实现，用户在本地看到的并不只是一个布尔确认框，而是一组更丰富的 request 交互：

- `approval`
- `userInput`
- `implementPlan`
- `mcpServerElicitation`

其中常见 approval 场景至少包含以下体验：

- `Allow`
- `Allow this session`
- `Decline`
- `No, and tell Codex what to do differently`

这里需要特别区分两件事：

1. `Allow this session` 是一个真实的 approval decision，至少在本地 `exec` / `patch` approval 中是这样。
2. `Tell Codex what to do differently` 不是第三种 approval decision，更接近：
   - 先拒绝当前 request
   - 再补一条 follow-up，告诉 Codex 如何调整

## 3. 问题定义

改造前，Feishu 端存在三个产品问题。

### 3.1 语义过窄

改造前，内部模型把 request response 压缩成了 `approved bool`，丢失了：

- `acceptForSession`
- 更细粒度的 approval option
- 与拒绝同时附带反馈的能力

### 3.2 普通发消息与 request 回复语义冲突

当前 Feishu 普通文本会直接进入 queue，走普通 `prompt.send` 路径。

这意味着如果用户在 request pending 期间直接发一条“你改成 xxx”，系统目前会把它当成新 prompt 入队，而不是把它解释成对当前 request 的处理意见。

### 3.3 与 queue 状态机有潜在死锁/歧义

approval request 通常挂在当前 turn 上。

如果当前 turn 正在等待确认，而用户发的新文本又被当成普通 queue item：

- 它不会立刻发送
- 它会排队等当前 turn 结束
- 但当前 turn 又在等 request 被处理

这样用户会感觉“消息发出去了，但系统没动静”。

## 4. 设计目标

### 4.1 目标

- 在 Feishu 端支持比 `accept/decline` 更丰富的 approval 交互。
- 支持“告诉 Codex 怎么改”的远端体验。
- 避免普通消息和 request reply 混淆。
- 不破坏现有 queue / paused_for_local / handoff 语义。
- 允许后续扩展到 `userInput` / `implementPlan` / `mcpServerElicitation`。

### 4.2 非目标

- 本阶段不追求完全复制 VS Code 的所有 request UI 细节。
- 本阶段不优先支持 MCP 特有的 persist 选择器。
- 本阶段不默认支持在卡片内直接填写多行文本。
- 本阶段不把任意普通文本自动解释成 `No`。

## 5. 关键产品决策

### 5.1 保留显式 `拒绝`

`拒绝` 不能被完全删除。

原因：

- 它是最快、最确定的操作。
- 用户有时只想拒绝，不想再补充说明。
- 如果完全依赖后续文本来表达拒绝，会和普通发消息冲突。

因此应保留显式 `拒绝`，但它可以是次级按钮，不必做成最醒目的主按钮。

### 5.2 不把“直接发消息”默认等价为 `No`

不建议采用“只要当前有 pending request，用户下一条文本就自动等价于拒绝并给反馈”的方案。

原因：

- 语义过于隐式，容易误伤正常消息。
- 会和现有 queue 机制冲突。
- 用户可能只是想发 `/status`、切线程、或者发新的普通问题。

因此需要一个显式的“进入反馈模式”动作。

### 5.3 引入“反馈当前确认请求”模式

推荐交互：

1. 用户在卡片上点击 `告诉 Codex 怎么改`
2. Surface 进入一个短暂的“反馈当前确认请求”模式
3. 用户下一条普通文本被解释为：
   - 先 `decline` 当前 request
   - 再把该文本作为 follow-up prompt 发给 Codex

这个模式是：

- 显式进入
- 一次性消费
- 只绑定一个 request
- 不长期驻留

## 6. 建议交互

### 6.1 Approval 卡片动作

对于 Feishu 侧常规 approval request，建议卡片动作为：

- `允许一次`
- `本会话允许`
- `拒绝`
- `告诉 Codex 怎么改`

其中：

- `允许一次` -> `accept`
- `本会话允许` -> `acceptForSession`
- `拒绝` -> `decline`
- `告诉 Codex 怎么改` -> 不直接回写 native decision，而是进入“反馈当前确认请求”模式

### 6.2 “本会话允许”的展示规则

`本会话允许` 不应盲目硬编码为所有 approval 都有。

原因是目前 wrapper 已确认能观测到的 request started 原始 payload，至少在现有测试覆盖里，还只有：

- `acceptLabel`
- `declineLabel`
- `title`
- `message`
- `command`

尚未确认 native `serverRequest/started` 是否一定会把本地支持的全部 approval options 原样暴露出来。

因此产品设计应允许三种情况：

1. 上游明确给出 `acceptForSession`
   - Feishu 展示 `本会话允许`
2. 上游没有显式给出，但我们已经基于真实样本确认该 request kind 恒支持 `acceptForSession`
   - Feishu 可合成展示
3. 上游未知
   - Feishu 隐藏该按钮，退化为：
     - `允许一次`
     - `拒绝`
     - `告诉 Codex 怎么改`

这属于“能力可见性跟随真实 upstream 能力”的策略，而不是用产品假设替代协议事实。

### 6.3 “告诉 Codex 怎么改”的用户体验

点击该按钮后，机器人立即回复一个轻量提示：

`接下来一条普通文本将作为对当前确认请求的处理意见，它不会进入普通消息队列。`

随后：

- 用户下一条普通文本被消费
- 当前 request 被 `decline`
- 文本作为 follow-up prompt 插入 queue

如果用户在这个模式下发的是命令，例如 `/status`：

- 命令照常执行
- 不消耗反馈模式

如果用户在这个模式下发图片：

- 返回 notice，提示“当前正在等待文本反馈，请先发送文字或取消”
- 不消耗反馈模式

## 7. 与 queue 的关系

这是本设计的核心。

### 7.1 当前问题

当前普通文本统一进入 queue。

而 pending request 绑定在当前 turn 上，当前 turn 未被处理前不会结束。

如果把“补充说明”也当普通文本入队，就会形成：

- request 等待用户处理
- 用户反馈进入 queue
- queue 又等 request 所在 turn 结束

从用户角度看就是“点了/发了，但没反应”。

### 7.2 设计原则

- request reply 优先级高于普通文本 queue
- 进入反馈模式后的下一条文本，不走普通 `handleText -> queue` 语义
- 普通文本在 pending request 存在时，不应静默入队

### 7.3 文本路由优先级

建议把 Feishu 文本入口优先级改为：

1. slash command
2. selection prompt 数字回复
3. request feedback capture
4. 若当前存在 pending request，但未进入 feedback capture
   - 阻止普通文本入队
   - 返回 notice，提示先点卡片按钮
5. 普通文本入队

这样可以避免以下误行为：

- 用户以为自己在“告诉 Codex 怎么改”，实际上只是排了一个永远不会立刻执行的新消息

### 7.4 Feedback follow-up 的入队策略

当用户发送“反馈文本”时，系统应做两件事：

1. 对 request 发出 `decline`
2. 生成一个新的 follow-up queue item

这个 follow-up queue item 应该：

- 绑定同一个 surface
- 尽量沿用当前 request 的 thread 上下文
- 插入到 queue 头部，而不是尾部

原因：

- 它语义上是当前被拒绝 turn 的直接后续
- 优先级应高于用户之前排队但还没处理的其他普通消息

### 7.5 为什么不是“立即直发”

不建议在收到反馈文本后绕开 queue 直接发新的 `prompt.send`。

原因：

- 当前 turn 很可能尚未完全 resolved
- queue 机制已经负责处理 turn 串行化
- 直接发送会增加与 active turn / pendingRemote / ActiveTurnID 的竞争条件

因此更稳妥的做法是：

- 立即发 `request.respond(decline)`
- 立即创建一个高优先级 queued item
- 等当前 turn 自然完成后，由现有 `dispatchNext()` 发送

## 8. 产品状态机

### 8.1 新增 surface 级状态

建议在 `SurfaceConsoleRecord` 上新增：

- `ActiveRequestCapture *RequestCaptureRecord`

建议结构：

```go
type RequestCaptureRecord struct {
    RequestID   string
    RequestType string
    InstanceID  string
    ThreadID    string
    TurnID      string
    Mode        string // phase 1: "decline_with_feedback"
    CreatedAt   time.Time
    ExpiresAt   time.Time
}
```

约束：

- 每个 surface 同时最多一个 `ActiveRequestCapture`
- 它只在显式点击 `告诉 Codex 怎么改` 后产生
- 一次性消费，消费后立即清空

### 8.2 状态转换

#### 进入 capture

- 输入：点击 `告诉 Codex 怎么改`
- 条件：request 仍然 pending
- 结果：
  - 写入 `ActiveRequestCapture`
  - 发送 notice

#### 消费 capture

- 输入：下一条普通文本
- 条件：`ActiveRequestCapture` 存在且未过期
- 结果：
  - 生成 `request.respond(decline)`
  - 创建 follow-up queue item
  - 清空 `ActiveRequestCapture`

#### capture 失效

触发条件：

- request 已在其他端 resolved
- turn 已完成
- `/stop`
- `detach`
- attached instance offline
- capture 超时

结果：

- 清空 `ActiveRequestCapture`
- 可选发送 notice

## 9. 协议与内部模型设计

### 9.1 `control.Action` 不再只用 `Approved bool`

当前 `ActionRespondRequest` 只有：

- `RequestID`
- `RequestType`
- `Approved bool`

这不足以表达：

- `acceptForSession`
- 其他未来 decision

建议改为：

```go
type Action struct {
    ...
    RequestID       string
    RequestType     string
    RequestOptionID string
    ...
}
```

说明：

- `RequestOptionID` 表达用户点击的具体 option
- `Approved bool` 可以保留一段兼容期，但新代码不应继续以它为主

推荐 option id：

- `accept`
- `acceptForSession`
- `decline`
- `captureFeedback`

其中：

- `captureFeedback` 是 Feishu 产品层 option，不是 native decision

### 9.2 `control.RequestPrompt` 需要 option 列表

当前 request prompt 只有：

- `AcceptLabel`
- `DeclineLabel`

建议升级为：

```go
type RequestPromptOption struct {
    OptionID string
    Label    string
    Style    string // primary / default / danger / secondary
}

type RequestPrompt struct {
    RequestID    string
    RequestType  string
    Title        string
    Body         string
    ThreadID     string
    ThreadTitle  string
    Options      []RequestPromptOption
}
```

这样 projector 可以不再假设“所有 request 都只有两个按钮”。

### 9.3 `agentproto.CommandRequestRespond` 的 canonical 语义

建议把 approval reply 统一表达为：

```json
{
  "type": "approval",
  "decision": "accept"
}
```

而不是：

```json
{
  "type": "approval",
  "approved": true
}
```

即：

- 新 canonical 语义使用 `decision`
- `approved bool` 只做兼容 fallback

建议 phase 1 支持：

- `accept`
- `acceptForSession`
- `decline`

后续可扩展：

- `acceptWithExecpolicyAmendment`
- `applyNetworkPolicyAmendment`

### 9.4 Wrapper / translator 设计

### 输入侧：native -> canonical event

当前 translator 对 request started 的提取信息过少。

设计目标：

- 尽量从 native request payload 中提取显式 option / decision 能力
- 若无法提取，则保留原始基础字段并允许上层合成默认选项

建议 metadata 字段扩展为可选：

- `requestType`
- `title`
- `body`
- `options`
- `rawDecisionHints`

其中 `options` 形态可为：

```json
[
  {"id":"accept","label":"Allow"},
  {"id":"acceptForSession","label":"Allow this session"},
  {"id":"decline","label":"Decline"}
]
```

若 native 未提供，则 `options` 留空。

### 输出侧：canonical command -> native request response

translator 在处理 `request.respond` 时：

1. 如果 `Response["decision"]` 是字符串
   - 原样回写到 native `result.decision`
2. 否则如果只有 `approved bool`
   - 兼容映射到 `accept / decline`

这样可以最小化对旧路径的破坏。

## 10. Feishu 卡片设计

### 10.1 Approval 卡片

建议结构：

- 标题：当前 request 标题
- 正文：
  - request message
  - 相关命令或补丁摘要
  - thread 标题
- 按钮区：
  - `允许一次`
  - `本会话允许`（若可用）
  - `拒绝`
  - `告诉 Codex 怎么改`

按钮样式建议：

- `允许一次`：primary
- `本会话允许`：default
- `拒绝`：danger 或 default
- `告诉 Codex 怎么改`：secondary

### 10.2 点击 `告诉 Codex 怎么改` 后的机器人反馈

建议不是原地编辑原卡片，而是发送一条轻量提示：

`已进入反馈模式。接下来一条普通文本会作为对当前确认请求的处理意见，不会进入普通消息队列。`

理由：

- 当前 Feishu 投影是 append-only，更适合补一条 notice
- 避免为卡片原地更新引入额外复杂度

## 11. 行为细则

### 11.1 有 pending request 但未进入 capture 时

用户发送普通文本：

- 不入队
- 返回 notice

建议文案：

`当前有一个待确认请求。请先点击卡片上的“允许一次”、“本会话允许”、“拒绝”或“告诉 Codex 怎么改”。`

### 11.2 已进入 capture 时

用户发送普通文本：

- 文本被消费，不走普通 queue 文本入口
- request 先被拒绝
- 文本被转成 follow-up queue item

### 11.3 已进入 capture 时发送命令

- 命令正常执行
- capture 保持不变

### 11.4 已进入 capture 时发送图片

- 返回 notice
- capture 保持不变

### 11.5 其他端已处理 request

如果 request 已被 VS Code 或其他 surface 处理：

- 清空 capture
- 再次点击卡片或发送反馈时返回 `request_expired`

### 11.6 `/stop`

`/stop` 的行为扩展为：

- 继续中断 active turn
- 继续丢弃 queued item / staged image
- 清空当前 surface 的 `PendingRequests`
- 清空当前 surface 的 `ActiveRequestCapture`

## 12. 实施分期

### Phase 1

范围：

- 只做 approval request
- 支持：
  - `accept`
  - `acceptForSession`
  - `decline`
  - `captureFeedback`
- 引入 capture mode
- 普通文本在 pending request 期间改为阻止入队

### Phase 2

范围：

- 支持 `userInput`
- 支持 `implementPlan`
- 统一 request prompt renderer

### Phase 3

范围：

- 支持 `mcpServerElicitation`
- 支持 persist 选择器
- 视实际协议能力决定是否支持更复杂 decision

## 13. 测试设计

### 13.1 Gateway 测试

覆盖：

- approval 卡片四种 action 的回调解析
- `captureFeedback` action 解析
- 旧 `approved bool` 兼容路径

### 13.2 Projector 测试

覆盖：

- 多按钮 approval 卡片渲染
- `本会话允许` 可见/不可见
- feedback mode notice 投影

### 13.3 Orchestrator 测试

覆盖：

- 有 pending request 时普通文本被阻止，不入队
- 点击 `captureFeedback` 后下一条文本被消费
- feedback 文本触发：
  - `request.respond(decline)`
  - follow-up queue item 创建
- feedback follow-up 插入队头
- request 已过期时 capture 无效
- `/stop` 清空 capture
- `detach` 清空 capture
- instance offline 清空 capture
- 多 surface 互不影响

### 13.4 Translator 测试

覆盖：

- `decision` 优先透传
- `approved bool` 兼容映射
- request started metadata 中 option 提取
- option 不可提取时的保守退化

### 13.5 真实链路验证

在真实链路联调时，至少抓三类原始 request started 样本：

- command approval
- patch approval
- network approval

目标是确认：

- native `serverRequest/started` 是否提供显式 option 列表
- `acceptForSession` 是否能稳定从 native payload 观测到
- 是否存在 Feishu 端需要隐藏的更复杂 decision

在拿到真实样本前，不应仅凭 VS Code 前端代码假设所有 remote request 都具备同等可见能力。

## 14. 风险与取舍

#### 14.1 风险：upstream option 能力不完整

如果 native request payload 只给出 `acceptLabel/declineLabel`，而不给 option id：

- Feishu 端无法 100% 准确还原本地 option 集

应对方式：

- canonical 模型支持 option 列表，但允许为空
- 产品层对未知能力保守降级
- 用真实日志补齐样本后再逐步放开

#### 14.2 风险：反馈模式带来新的临时状态

新增 capture mode 会增加 surface 级状态复杂度。

应对方式：

- 限制为“一次性、单 request、单 surface”
- 明确定义清理时机
- 为所有清理场景写单元测试

#### 14.3 取舍：不做“任何文本都自动等于 No”

这是主动放弃的“更省按钮”方案。

原因不是实现不了，而是它会让系统在用户看来过于隐式，且和 queue 语义冲突最大。

## 15. 最终建议

推荐采用下面的最小可用产品方案：

- 保留显式 `拒绝`
- 不把普通文本默认等价为 `No`
- 新增 `告诉 Codex 怎么改`
- 引入一次性 feedback capture mode
- 在 pending request 存在时阻止普通文本静默入队
- approval response 从 `approved bool` 升级为 `decision string`

这条路线的优点是：

- 语义清楚
- 和现有 queue 状态机兼容
- 能自然扩展到更多 request 类型
- 不需要在第一阶段就复制 VS Code 全部复杂 UI

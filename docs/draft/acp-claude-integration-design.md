# ACP / Claude 接入设计调研

> Type: `draft`
> Updated: `2026-04-06`
> Summary: 初版调研归档到 `docs/draft`，等待后续讨论 ACP 适配抽象和 Feishu 交互细节。

## 1. 文档定位

这份文档回答的问题是：

1. 现在这套 `relayd + wrapper + Feishu` 架构，能不能通过 ACP 接入 Claude Code 一类 agent。
2. 如果能，现有协议抽象需要怎样调整。
3. 在 Feishu 侧最合理的交互是什么。
4. 哪些能力能原样保留，哪些会降级。

这里的“Claude Code”具体参考的是：

- 官方 ACP 生态
- `claude-agent-acp`
- `codex-acp`

调研日期为 **2026-04-06**。

## 2. 先给结论

### 2.1 可以接，但不是“零改造”

结论是：

- **当前 daemon / orchestrator 层已经足够接近通用 canonical protocol**
- **真正强耦合的是 wrapper 层**

所以：

- 这套框架**可以**通过新增 ACP adapter 支持 Claude
- 但前提是把当前 wrapper 从“写死 Codex translator”重构成“可插拔 agent adapter”

### 2.2 Feishu-only / headless 路径最容易先落地

如果目标是：

- 在飞书里用 Claude
- 支持 headless 实例
- 支持 `/list`、`/use`、发消息、停下、审批

那是可行的，而且实现路径比较清晰。

如果目标是：

- 保持现在“VS Code Codex 扩展 + relay-wrapper shim”这套本地工作流
- 但把底层 agent 换成 Claude / ACP

那就**不能直接复用现有 VS Code 集成**，因为现有本地前端说的是 Codex native app-server 协议，不是 ACP。

### 2.3 当前 `agentproto` 能承接基础能力，但承接不了“最佳 ACP 体验”

当前 `agentproto` 足以承接这些基础动作：

- 线程/会话发现
- 发送 prompt
- 中断 turn
- 处理 approval request
- 刷新 thread 列表

但如果要把 ACP 的这些能力做好：

- `configOptions`
- `available_commands_update`
- `session/list` 分页和能力发现
- `session/load` 的 replay / hydrate 区分
- 终端 / diff 更丰富的内容展示

那就需要对 canonical 层做适度扩展。

## 3. ACP 对这个项目意味着什么

ACP 的主流程和当前仓库关心的能力高度相关：

1. `initialize`
2. `session/new` / `session/load`
3. `session/prompt`
4. `session/update`
5. `session/cancel`
6. `session/request_permission`
7. 可选 `session/list`
8. 可选 `session/set_config_option`
9. 可选 terminals / fs / slash commands / modes

对这个仓库来说，ACP 的价值不是“替代 relay.agent.v1”，而是：

- 给 `relay.agent.v1` 背后新增一个**新的 native agent backend**

也就是：

- `relayd <-> wrapper` 仍然保留你自己的 canonical 协议
- `wrapper <-> child agent` 可以不再只支持 Codex native，也支持 ACP

## 4. 当前代码的真实耦合点

当前架构里，真正把系统锁死在 Codex 上的，不是 daemon，而是 wrapper。

关键事实：

- `internal/app/wrapper/app.go` 里 `App` 直接持有 `*codex.Translator`
- `stdinLoop` / `stdoutLoop` 直接调用：
  - `translator.ObserveClient`
  - `translator.ObserveServer`
  - `translator.TranslateCommand`
- `internal/adapter/codex/translator.go` 同时承担了三种职责：
  - 观察上游本地客户端帧
  - 观察下游 Codex child 帧
  - 把 canonical command 转成 Codex native 请求

所以当前并不存在一个真正的“agent adapter interface”。

这也是为什么现在虽然 `agentproto` 已经挺通用了，但系统整体仍然是 Codex-specific。

## 5. ACP 参考实现调研结果

## 5.1 官方 ACP 协议

官方 ACP 文档确认了这些关键点：

- 基础方法：
  - `initialize`
  - `session/new`
  - `session/prompt`
  - `session/cancel`
  - `session/update`
  - `session/request_permission`
- 可选能力：
  - `loadSession`
  - `session/list`
  - `session/set_config_option`
  - terminals
  - files
  - slash commands

这和你当前 Feishu 侧已经做过的很多产品能力天然对齐：

- thread list
- prompt send
- stop
- approval card
- model / reasoning / mode 切换

## 5.2 `claude-agent-acp`

我拉了 `claude-agent-acp` 的代码看，结论很明确：

- 它不是一个“只会聊天”的薄封装
- 它已经把 Claude Agent SDK 的大量能力映射进 ACP

当前公开能力包括：

- `loadSession`
- `session/list`
- `sessionCapabilities.fork`
- `sessionCapabilities.resume`
- `sessionCapabilities.close`
- `configOptions`
- `available_commands_update`
- tool calls with permission requests
- diff content
- terminal output

它还带两个非常重要的信号：

1. 会话配置不是硬编码字段，而是通过 `configOptions` 暴露 `mode`、`model`。
2. 它支持 `_meta.claudeCode.promptQueueing`，说明后端本身就有 prompt queue 概念。

## 5.3 `codex-acp`

`codex-acp` 是一个很有价值的对照组，因为它告诉我们：

- ACP 不只是给 Claude 用
- Codex 这种复杂 agent 也可以暴露成 ACP

更关键的是，它把你之前观察到的那些 approval 选项都标准化成了 ACP permission options，例如：

- `Yes, proceed`
- `Yes, and don't ask again for this command in this session`
- `No, continue without running it`
- `No, and tell Codex what to do differently`

这说明一个重要设计结论：

- Feishu 侧以后不应该再把审批 UI 设计成写死的 `yes/no`
- 而应该根据 backend 给出的 ACP permission options 动态渲染按钮

## 6. 最大的协议风险：`session/load` 会 replay 历史

这点非常重要。

ACP 官方规范要求：

- `session/load` 恢复会话时，agent 会通过 `session/update` 把历史对话 replay 给 client

这对普通 IDE 是合理的，但对当前 Feishu relay 会造成一个产品问题：

- 用户在飞书里 `/use` 一个历史 thread
- 如果 adapter 直接把 replay 翻译成普通消息
- 飞书群里会被历史内容重新刷一遍

所以 ACP 接入不能只做“协议映射”，还必须做**hydration / replay 分类**。

推荐规则：

1. 如果 agent 支持更轻量的 `resume` 且不会 replay，优先用 `resume`。
2. 如果只能 `load`，adapter 在 `session/load` 期间必须把 replay traffic 标成 hydration。
3. daemon / projector 必须默认不把 hydration replay 投影到 Feishu 聊天。

这个问题和你现在已经处理过的 helper/internal traffic 是同一类问题：

- 都需要协议级显式标注
- 不能靠“同线程下一条消息”之类的启发式猜

## 7. 推荐抽象重构

## 7.1 不要继续扩展 `codex.Translator`

推荐方向不是：

- 给 `codex.Translator` 里继续塞 `if protocol == acp`

而是明确抽出一层：

- `AgentAdapter`

## 7.2 推荐接口

建议把 wrapper 的 child-agent 侧抽象成：

```go
type AgentAdapter interface {
    Initialize(ctx context.Context) error
    HandleRelayCommand(ctx context.Context, cmd agentproto.Command) error
    HandleParentFrame(ctx context.Context, raw []byte) (AdapterResult, error)
    HandleChildFrame(ctx context.Context, raw []byte) (AdapterResult, error)
    Close(ctx context.Context) error
}
```

但更进一步，我建议把“本地上游客户端桥接”和“child agent 驱动”分成两个角色：

1. `LocalClientBridge`
2. `AgentRuntimeAdapter`

原因是：

- Codex 现在的 wrapper 是一个 shim，既夹在本地客户端和 child 之间，也对 relay 发 canonical event。
- ACP headless 场景下，往往根本没有本地上游客户端，wrapper 自己就是 ACP client。

所以真正稳定的抽象应该是：

- `LocalClientBridge`
  - 可选
  - 负责上游 IDE / 前端协议
- `AgentRuntimeAdapter`
  - 必选
  - 负责 child agent 协议

## 7.3 推荐的具体拆法

建议拆成三层：

### A. `relay.agent.v1` 保持不动

先不要动 daemon 到 wrapper 的 websocket 协议主干。

理由：

- 现在这层已经很稳定
- Feishu、queue、attach、status、headless 都围绕它工作

### B. wrapper 变成协议无关的 host

让 `internal/app/wrapper` 不再知道“Codex 是什么”，只负责：

- 启 child
- 连 relayd
- 收发 relay command / event
- 把 native protocol 细节委托给 adapter

### C. 新增两种 adapter

1. `CodexNativeAdapter`
   - 现有 `codex.Translator` 的重构版
   - 保留当前 VS Code + Codex path

2. `ACPAdapter`
   - 新增
   - 负责和 `claude-agent-acp` / `codex-acp` / 其他 ACP agent 通信

## 8. ACP 到当前 canonical 的映射建议

下面是建议映射。

| ACP | 当前 canonical | 说明 |
| --- | --- | --- |
| `session/list` | `threads.snapshot` | 一页或多页聚合后再发 snapshot |
| `session/new` result | `thread.discovered` | 建立新 thread/session |
| `session/load` / `resume` | `thread.focused` + hydration state | 恢复会话但不要重放到 Feishu |
| `session/prompt` | `prompt.send` 对应的执行结果 | 作为一个 remote turn |
| `session/update.agent_message_chunk` | `item.started/delta/completed` | 合并成 assistant markdown block |
| `session/update.agent_thought_chunk` | `item.*` with metadata or hidden | 默认不投影到 Feishu |
| `session/update.tool_call` | `item.started` or tool metadata event | Feishu 显示为工具执行状态 |
| `session/update.tool_call_update` | `item.delta/completed` | 终端、diff、文本都从这里出来 |
| `session/request_permission` | `request.started` | 渲染审批卡片 |
| permission response | `request.respond` | 选中的 optionId 回写 |
| `session/cancel` | `turn.interrupt` | 停止当前 turn |
| `config_option_update` | 新增 canonical config-options event | 当前 `config.observed` 不够用 |
| `available_commands_update` | 新增 canonical commands event | 当前没有等价事件 |

## 9. 当前 `agentproto` 哪些地方够用，哪些不够

## 9.1 足够的部分

这些已经够用：

- `threads.snapshot`
- `thread.discovered`
- `thread.focused`
- `turn.started`
- `turn.completed`
- `item.started`
- `item.delta`
- `item.completed`
- `request.started`
- `request.resolved`
- `prompt.send`
- `turn.interrupt`
- `request.respond`
- `threads.refresh`

也就是说，做一个**基础版 ACP adapter** 没问题。

## 9.2 不够的部分

如果想把 ACP 体验做完整，建议新增 canonical 概念：

1. `config.options.updated`
   - 承接 ACP `config_option_update`
   - 比当前 `config.observed(model/reasoning)` 更通用

2. `commands.available.updated`
   - 承接 ACP `available_commands_update`

3. `trafficClass = hydration` 或等价 metadata
   - 区分 `session/load` replay

4. 更泛化的 prompt input
   - 当前 `agentproto.Input` 只有 text/local_image/remote_image
   - 如果以后要完整吃 ACP 的 resource/audio，得扩展

5. 可选 `session.config.set` command
   - 让 Feishu 菜单能真正调用 ACP `session/set_config_option`
   - 而不是只把 model/reasoning/access 当作下次 prompt 的 surface override

## 10. Feishu 最佳交互建议

## 10.1 Session / Thread

对 Feishu 来说，最佳交互仍然应该保留你现在熟悉的 thread 概念：

- `/list` -> `session/list`
- `/use` -> `session/load` 或 `resume`
- `new instance` -> 选择 agent 类型后再列最近 sessions

也就是说，产品表面继续叫 thread 没问题，底层映射到 ACP session 即可。

## 10.2 配置菜单

如果 backend 是 ACP，菜单不应再写死成：

- `model_*`
- `reason_*`
- `access_*`

更好的做法是：

1. 优先读取 `configOptions`
2. 根据 `category` 做 Feishu 呈现

建议映射：

- `category = model`
  - 放到模型菜单
- `category = thought_level`
  - 放到推理强度菜单
- `category = mode`
  - 放到模式/权限菜单

这样：

- Claude 可以暴露 `Mode`
- Codex ACP 可以暴露 `mode/model/reasoning_effort`
- 未来别的 ACP agent 也能进来

## 10.3 审批卡片

ACP 场景下，审批卡片必须动态渲染。

也就是：

- backend 给几个 option，就渲染几个按钮
- 不要再假设只有 `yes/no`

这样才能同时覆盖：

- Claude 的 `Allow / Always Allow / Reject`
- Codex 的 `Allow once / Allow for session / Reject / Tell Codex what to do differently`
- 未来其他 agent 的更细粒度权限项

## 10.4 工具执行展示

Feishu 最合理的降级展示建议是：

- tool call 创建时：发状态块或卡片，显示 title + kind
- 进行中：追加文本进度
- diff：先做摘要或 plain text，后续再接你想要的 HTML preview handler
- terminal：先渲染成折叠文本或日志摘要，不追求 IDE 那种实时终端 widget

## 10.5 Queue 所有权

Claude ACP 自己带 prompt queueing 能力，但我不建议把队列主控权下放给 backend。

推荐规则：

- **relay 仍然是唯一队列拥有者**
- backend queue 只当作兜底，不当产品语义

原因：

- 你现在已经在 Feishu 侧做了撤回出队、reaction 状态、停下、路由控制
- 如果再把一部分排队语义交给 backend，排障会更痛苦

所以建议：

- relay 确认当前 session 空闲后才下发下一条 prompt
- 不依赖 Claude 的内部 prompt queue 作为正常主路径

## 11. 哪些能力会降级

## 11.1 无法直接复用当前 VS Code Codex 集成

这是最大的非兼容点。

原因不是 Claude 不行，而是：

- 当前本地前端讲的是 Codex native app-server 协议
- ACP 是另一套协议

所以除非：

- 另做一个 ACP-capable 本地 client bridge

否则：

- Claude / ACP 路径应优先定位成 Feishu + headless + 未来 ACP-capable editor

## 11.2 `session/load` 的历史回放不能原样显示

这不是不能做，而是不能按普通消息处理。

必须做 hydration 降噪。

## 11.3 config 菜单需要重做成动态

当前 model / reasoning / access 是写死产品逻辑。

ACP 世界里更合理的是：

- dynamic config options

如果不改，Claude 只能吃一个被强行映射的阉割版 UX。

## 11.4 terminal rich UI 会降级成文本

Feishu 不适合承载 ACP terminal widget。

合理预期是：

- 摘要
- 文本日志
- 卡片状态

而不是 IDE 级终端面板。

## 11.5 认证流程有现实门槛

`claude-agent-acp` 暴露了 terminal auth / gateway auth。

但 Feishu 不能直接完成“开一个本地交互终端让用户登录”这种动作。

所以初版要么：

- 依赖服务器上已完成 Claude 认证

要么：

- 另做独立 auth 流程

这不是协议问题，是产品接入问题。

## 12. 推荐实施策略

我建议的落地顺序是：

### 第一步：只做 headless ACP path

目标：

- Feishu 可以创建 / 连接 ACP agent 实例
- 支持 `session/list` / `use`
- 支持 prompt / stop / approval

先不碰本地 VS Code shim。

这是最小可用、风险最低的一步。

### 第二步：扩 canonical event/command

补齐：

- config options
- available commands
- hydration 标记

这样 Feishu 菜单和状态展示才能真正吃到 ACP 的优势。

### 第三步：按需再考虑本地 ACP client bridge

只有当你明确需要：

- 本地 IDE 也和 Claude / ACP 做双端协作

再考虑让 wrapper 同时支持“上游本地 ACP client”。

否则没必要一开始就把问题做太大。

## 13. 测试重点

如果后面立项实现，测试必须围绕下面几类风险写：

1. `session/list`
   - 多页聚合
   - cwd 过滤

2. `session/load`
   - replay 期间不向 Feishu 重放历史消息
   - load 完成后正常继续新 prompt

3. `session/prompt`
   - assistant chunk 正常流式显示
   - stop 正常结束

4. permission
   - 动态 option 渲染
   - selected / cancelled 都能回写

5. config options
   - backend 更新后 Feishu 状态刷新
   - 菜单修改能正确调用 `session/set_config_option`

6. queue
   - relay 队列与 backend busy 状态一致
   - 撤回出队不被 backend 内部 queue 吞掉

7. headless lifecycle
   - spawn / reconnect / attach / detach / kill

## 14. 总结

最终判断是：

- **要支持 Claude / ACP，这套系统不需要推翻重来**
- **但必须把 wrapper 从 Codex 专用桥接器提升成可插拔 agent host**

更具体地说：

1. daemon / orchestrator 层已经足够通用，可以继续作为中枢。
2. wrapper 层必须抽出通用 `AgentAdapter`。
3. Feishu-only / headless ACP 路径很值得先做，而且产品闭环清晰。
4. 如果以后要让本地 IDE 也深度接 Claude，才需要补本地 ACP client bridge。

所以这件事的工程结论不是“现在的抽象不行”，而是：

- **当前 canonical 层方向是对的**
- **但 native adapter 层还没抽象出来**

## 15. 参考资料

官方协议：

- ACP Overview: https://agentclientprotocol.com/protocol/overview
- ACP Initialization: https://agentclientprotocol.com/protocol/initialization
- ACP Session Setup: https://agentclientprotocol.com/protocol/session-setup
- ACP Prompt Turn: https://agentclientprotocol.com/protocol/prompt-turn
- ACP Tool Calls: https://agentclientprotocol.com/protocol/tool-calls
- ACP Session List: https://agentclientprotocol.com/protocol/session-list
- ACP Session Config Options: https://agentclientprotocol.com/protocol/session-config-options

参考实现：

- ACP 官方仓库（调研时本地检出 `8c8dbe7`）：https://github.com/agentclientprotocol/agent-client-protocol
- `claude-agent-acp`（调研时本地检出 `f422c7c`）：https://github.com/agentclientprotocol/claude-agent-acp
- `codex-acp`（调研时本地检出 `c3e95ca`）：https://github.com/zed-industries/codex-acp

本地实现参考：

- 当前架构：`docs/general/architecture.md`
- 当前 canonical 协议：`docs/general/relay-protocol-spec.md`
- 当前 canonical 类型：`internal/core/agentproto/types.go`
- 当前 wrapper：`internal/app/wrapper/app.go`
- 当前 Codex translator：`internal/adapter/codex/translator.go`

# App-Server Relay 重构设计

详细协议规格见：

- [relay-protocol-spec.md](./relay-protocol-spec.md)
- [feishu-product-design.md](./feishu-product-design.md)

## 1. 目标

这次重构要解决的不是“再补几个修修补补的 if”，而是把边界重新划清，避免后面继续出现：

- wrapper 自己夹带产品策略
- bot 被迫理解 Codex 原生协议
- server 只做转发，真正的状态和切分逻辑散落在各层
- 一改展示逻辑就影响协议翻译，一改协议翻译又把 bot 搞坏

新的设计目标有两条：

1. 足够通用，未来可以接别的 code agent
2. 足够细，能支撑当前 Feishu 侧对切分、卡片、状态反馈的需求

## 2. 调研结论

这次先对 Codex app-server 的事件面做了重新核对，重点看“bot 需要的边界到底能不能从协议里拿到”。

### 2.1 协议里确实存在的稳定边界

Codex 原生协议里，下面这些边界是稳定的：

- `thread`
  - `thread/started`
  - `thread/name/updated`
  - `thread/tokenUsage/updated`
  - 以及客户端侧的 `thread/start` / `thread/resume`
- `turn`
  - `turn/started`
  - `turn/completed`
  - 以及客户端侧的 `turn/start` / `turn/steer`
- `item`
  - `item/started`
  - `item/completed`
  - 以及带 `itemId` 的各类 delta
- `request`
  - approval
  - request user input
  - tool call

也就是说，协议天然能告诉我们：

- 当前是哪个 thread / turn
- 现在正在流哪一个 item
- 这个 item 是文本、plan、reasoning、command、file change 还是 tool
- 这个 item 什么时候结束
- 本地 UI 是否已经发起了新的交互，哪怕它只是对当前 turn 的 `steer`

另外，这次需要把一件事明确写死：

- `ephemeral`
- `persistExtendedHistory`
- `outputSchema`

这些字段最多只能作为“本地 helper/internal traffic 的识别线索”或“模板复用时的排除条件”。

它们**不能**直接变成：

- wrapper 里是否吞掉 `turn.started`
- 是否吞掉 `item.completed`
- 是否吞掉 `turn.completed`

换句话说，helper/internal traffic 的问题，不能再通过“在 adapter 里把生命周期事件吃掉”来解决。

### 2.2 协议里没有的边界

Codex 的 `agentMessage` 最终只是一条 `text` 字符串。

协议不会告诉我们：

- 哪一段是“引导语”
- 哪一段是“文件列表”
- 哪一段是“如果你要我可以继续...”
- 哪些段落应该拆成两条飞书消息

也就是说，更细的展示拆分不属于原生协议语义，而属于展示层语义。

### 2.3 对架构的直接影响

这说明必须明确区分两层边界：

- 硬边界
  - 由原生协议直接提供
  - 例如 `thread / turn / item / request`
- 软边界
  - 由展示层根据文本结构再细分
  - 例如一个 `agentMessage` item 里面的引导语、代码块、尾注

所以新的设计不能让 bot 去看原生协议细节，但也不能假设“统一抽象以后就完全不需要文本渲染逻辑”。

正确做法是：

- wrapper 负责把原生协议翻译成统一 `AgentEvent`
- server 负责把 `AgentEvent` 进一步整理成 bot 能消费的 `RenderEvent`
- bot 不看原生协议，也不猜 item/turn 边界
- server 内部保留一个 `renderer planner`，专门处理 `assistant_message` item 内部的软边界

## 3. 新的总体架构

目标链路调整为：

`Native Agent Protocol -> Wrapper Adapter -> Canonical Agent Model -> Server Orchestrator -> Render Planner -> Bot View Model -> Feishu`

对应职责如下。

### 3.1 Wrapper = Agent Adapter

wrapper 是“特定 agent 协议适配层”。

它负责：

- 连接原生 agent 进程
- 解析该 agent 的原生协议
- 把原生事件翻译成统一 `AgentEvent`
- 接收统一 `AgentCommand`
- 再翻译回该 agent 的原生请求
- 维护翻译所必需的最小协议状态
  - request/response 关联
  - item streaming 关联
  - 观察到的本地 focused thread
  - 原生命令模板
  - local-ui / internal-helper 的流量标注

它不负责：

- attach 哪个用户
- 飞书当前该向哪个 thread 发消息
- 文本最终如何切成卡片
- bot 展示策略
- 决定 native lifecycle event 是否对产品层“可见”

这里再单独冻结一条：

- wrapper 可以 suppress 自己为了执行 canonical command 而主动注入的原生命令响应
  - 例如 wrapper 自己发出的 `thread/start`
  - `thread/read`
  - `turn/start`
- wrapper 不可以因为它“看起来像 helper/internal traffic”，就 suppress 掉真实 runtime lifecycle event
  - 例如 `thread/started`
  - `turn/started`
  - `item/completed`
  - `turn/completed`

helper/internal traffic 如果需要区别对待，必须变成 canonical event 上的显式标注，交给 server 决定如何使用。

### 3.2 Server = Orchestrator + Renderer Planner

server 是唯一的产品语义中心。

它负责：

- 管理 instance / attachment / selected thread
- 管理 surface console，而不是只管理“某个 userId 当前挂在哪个 session 上”
- 保存统一状态快照
- 决定远端 prompt 路由到哪个 thread
- 管理用户层队列、图片暂存、数字选择 prompt 和取消语义
- 把 bot 动作翻成统一 `AgentCommand`
- 消费 wrapper 发来的 `AgentEvent`
- 维护 thread / turn / request / item 状态
- 把 canonical event 整理成 bot 需要的 `RenderEvent`
- 保存 debug / replay 数据

server 内部必须再分两层：

- `orchestrator`
  - attachment
  - surface session
  - thread selection
  - active turn
  - local-vs-remote arbitration
  - queued outbound input
  - staged assets
  - request lifecycle
  - prompt routing
- `renderer planner`
  - item 边界到 render group 的映射
  - `assistant_message` 内部的结构化切分
  - tool / plan / approval 的可见性策略

### 3.3 Bot = Renderer / Interaction Shell

bot 只做：

- 用户输入
- 菜单操作
- 卡片点击
- 把 `RenderEvent` 映射成 Feishu 文本、卡片和 reaction

bot 不应该再知道：

- `item/agentMessage/delta`
- `item/completed`
- `thread/started`
- JSON-RPC request/response
- Codex 特有字段结构

## 4. 统一抽象为什么足够

这次最大的顾虑其实不是“能不能抽象”，而是“抽象以后是不是又不够用，最后还是得回头猜”。

结论是：这次定义的 canonical model 足够支撑当前 bot 的核心需求。

### 4.1 能被硬边界直接覆盖的需求

下面这些需求不需要再靠文本猜：

- tool 输出和 assistant 正文分开
- final message 不和前面的 command output 粘连
- approval request 单独做卡片
- turn 失败明确报错
- thread 切换不被后台输出错误重定向

这些都可以直接由 `thread / turn / item / request` 边界提供。

### 4.2 仍然要靠 renderer planner 的需求

下面这些需求不属于原生协议语义，但可以稳定地在 server 里处理：

- 文件列表转代码块
- fenced code block 前后文字拆开
- 前导一句说明是否单独成块
- 尾注是否单独发一条

这里的关键不是“做一个进度句分类器”，而是：

- 先按 item 做强切分
- 再按文本结构做软切分
  - fenced code block
  - blank line
  - dense line group
  - 段落边界

也就是说，bot 需要的切分不依赖 wrapper 去猜，更不依赖 bot 去猜。

## 5. 对控制面的直接结论

### 5.1 `attach(instance)` 和 `use-thread(thread)` 必须分离

产品语义里，这两个动作不是一回事：

- `attach(instance)` 是接管某个 wrapper instance
- `use-thread(thread)` 是给当前 attachment 选定远端输入目标

结合飞书产品层需求，attach 成功后应优先：

- 若当前存在 `observedFocusedThreadId`
  - 立即 pin 成当前远端输入目标
- 若当前没有 observed focus
  - 进入 `unbound`，等后续 prompt 再决定是借用 focus 还是新建 thread

只有显式执行“跟随当前 VS Code”动作后，后续 prompt 才重新跟随本地 focused thread。

### 5.2 远端 prompt 的目标选择必须由 server 冻结

server 收到飞书 prompt 后，目标 thread 的选择顺序必须固定为：

1. `selectedThreadId`
2. 若 `routeMode = follow_local`，则使用 `observedFocusedThreadId`
3. 创建新 thread

而且：

- attach 当下已有 observed focus 时，应先 pin，而不是默认保持 follow-local
- 远端新建 thread 成功后，要自动 pin 到新的 `selectedThreadId`
- 只有显式 follow-local 模式下，才允许借用本地 focused thread 发 prompt
- 后台输出不能把路由指向改掉

### 5.3 公共命令面必须保持最小

server 不应该对 wrapper 暴露“原生命令级”的公共控制面。

公共命令面只保留：

- `prompt.send`
- `turn.interrupt`
- `request.respond`
- `threads.refresh`

至于 native `thread/start` / `thread/resume` / `turn/start`，由 adapter 内部决定什么时候发。

这件事很关键，因为它直接避免了之前“以为应该 startThread，结果方向反了”的问题。

### 5.4 helper/internal 的正式处理方式

这一轮新增一个明确约束：

- native client 自己发起的 helper/internal traffic，不再靠 wrapper 内部的吞消息逻辑处理
- adapter 只做两件事
  - 识别“它是不是 internal helper”
  - 在 canonical event 上打标

server 再根据这个标记决定：

- 是否进入 surface 主状态机
- 是否参与 attach/use-thread 可见 thread 列表
- 是否参与 local-priority 仲裁
- 是否进入普通 render feed
- 是否只留在 debug/replay

最小落地原则：

- `outputSchema != null` 的本地 `turn/start`
  - 只影响 turn template 复用
  - 同时标记后续 turn/item lifecycle 为 `internal_helper`
- `ephemeral = true` 或 `persistExtendedHistory = false` 的本地 `thread/start`
  - 只影响 thread template 复用
  - 同时标记对应 thread lifecycle 为 `internal_helper`

## 6. 逻辑边界不等于部署边界

这次文档里说的三层，是**逻辑边界**，不是强制要求保留当前的：

- Rust wrapper
- Node server
- Node bot

也就是说，后续实现上有三种选择：

### 6.1 保持当前多进程结构

优点：

- 改动面最小
- 可以渐进迁移

缺点：

- 协议层和语言边界都在
- 调试复杂

### 6.2 保持 bot 独立，合并 wrapper + server

优点：

- app-server 协议翻译和产品调度放到同一运行时
- 状态同步最简单

缺点：

- 仍保留 bot 单独部署

### 6.3 彻底单语言重写

优点：

- 所有内部边界都可以变成模块接口而不是进程协议
- 日志、状态机、renderer 更容易统一

缺点：

- 初始改动大

这三种实现方式都可以沿用同一套协议/状态设计。也就是说，**这份设计文档先冻结的是逻辑接口，不是代码组织形式**。

## 7. 对未来多 agent 支持的意义

如果后面要接别的 code agent，这套结构的扩展点也已经比较明确：

- wrapper 负责把特定 agent 协议翻译成统一 `AgentEvent`
- server 只认 canonical model，不认某个 agent 的私有字段
- bot 只认 `RenderEvent`

未来 agent 之间真正会变化的，主要是：

- native protocol
- item 类型覆盖范围
- request 类型
- render hint 细节

但 thread / turn / item / request 这组抽象，对绝大多数 code agent 都是成立的。

## 8. 当前建议

在开始编码前，应继续以 [relay-protocol-spec.md](./relay-protocol-spec.md) 为主，把下面几个点继续守住：

- 先按 canonical model 实现，不要再把 bot 绑回原生协议
- 先让 server 成为唯一的状态中心
- 先把 surface 级别的 attachment / queue / staged image 状态建好
- 先保证 item 边界和 thread 选择语义正确
- 再做 renderer planner
- 最后才是 Feishu 卡片和交互细节

如果后面决定重写，也应该是“按这份协议重写”，而不是再回到“先写代码，边测边猜协议”的方式。

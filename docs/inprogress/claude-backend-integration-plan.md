# Claude Backend Integration Plan

> Type: `inprogress`
> Updated: `2026-04-23`
> Summary: 合并此前三份 Claude 设计文档，收敛成单一实施方案，明确基座改造、Claude 接入路径与 Codex 零回归约束。

## 1. 文档定位

这份文档是当前仓库 **Claude 接入工作的唯一实施基线**。

它统一替代并吸收以下三份历史文档中的有效结论：

- `docs/obsoleted/claude-normal-mode-poc-design.md`
- `docs/obsoleted/claude-provider-protocol-mapping.md`
- `docs/obsoleted/claude-feidex-reassessment.md`

本文回答四个问题：

1. 为什么现在的架构还不能低成本接入 Claude。
2. 要低成本接入 Claude，必须先补哪些基座能力。
3. 这些基座能力应该如何设计，才能让 Codex 现有行为保持一致。
4. 在这些基座补齐后，Claude MVP 应该如何接入。

## 2. 目标与成功标准

### 2.1 总目标

我们希望得到的不是“一次性塞进 Claude 的特殊分支”，而是一套能够长期承载多 backend 的运行时基座。

最终目标：

1. 现有 Codex 路径在 Claude 真正启用前后都保持行为一致。
2. Claude 以较小增量接入现有产品命令和 Feishu 工作流。
3. 当前 canonical `turn / item / request` 语义继续作为上层稳定骨架。
4. 不支持的 Claude 能力显式拒绝，不出现 silent failure。

### 2.2 成功标准

满足下面四点时，可以认为这份方案成立：

1. Codex-only 部署不需要知道 Claude 的存在，运行行为与当前 master 一致。
2. Claude 接入后，不需要重写 daemon / orchestrator / Feishu 投影主链路。
3. `prompt.send`、`turn.interrupt`、`request.respond`、`threads.refresh`、`thread.history.read` 这些 canonical 面，在 Codex/Claude 下都能按能力矩阵稳定工作或显式降级。
4. 后续再接第三种 backend 时，不需要再次把系统 seam 从 Codex 形状里拔出来。

## 3. 设计原则

### 3.1 零回归优先

Claude 接入前后的第一条约束不是“先把 Claude 跑起来”，而是：

- 现有 Codex 路径不能被破坏

这意味着：

1. Codex 现有 wrapper child 启动路径、translator 语义、threads refresh、thread history、turn steer，在未切换到新 backend 前必须保持现状。
2. 任何新的 backend-aware 逻辑都必须先以 Codex 作为默认兼容值落地。
3. 任何不影响现有行为的抽象提取，优先于引入新的跨 backend 产品行为。

### 3.2 上层保持 canonical，中层做 backend-aware，底层允许原生差异

系统应该分三层看：

1. `daemon / orchestrator / control / Feishu`
   - 继续处理 canonical 产品语义
2. `wrapper / runtime host`
   - 负责 backend 选择、能力上报、命令转发、事件归一化
3. `provider implementation`
   - 各自保留原生协议和原生运行方式

这意味着：

1. 不应该让 Claude 假装成 Codex app-server。
2. 不应该为了抽象统一，把 Codex 和 Claude 都压缩成能力交集。
3. 统一的是产品骨架，不是底层 native protocol。

### 3.3 workspace 共享，会话隔离

需要固定两条边界：

1. `workspace` 是文件系统作用域，可以跨 backend 共享。
2. `thread/session` 是 backend 作用域，必须按 backend 隔离。

因此：

1. `/list` 只能展示当前 backend 的会话视图。
2. `/use` 只能消费当前 backend 的会话 id。
3. 持久状态不能只靠 `threadId` 作为全局唯一键。

### 3.4 明确拒绝，不做假兼容

对于 Claude 暂不支持或暂不等价的能力：

1. 显式 `command_rejected/problem + notice`
2. 不 silent failure
3. 不伪装成“差不多可用”

`turn.steer` 是最典型的例子。

### 3.5 能力声明与产品策略分离

这次基于 `feidex` 的重新复核，需要补一条更细的规则：

- backend capability declaration
- product command strategy

这两层不能再混在一起。

`feidex` 实际已经证明，Claude 相关产品行为不只两档：

1. native support
   - backend 原生支持 canonical 能力
2. business approximation
   - 产品层做近似实现，但语义与 canonical 能力不同
3. passthrough
   - 产品层把原始命令或原始提示透传给 backend
4. explicit reject
   - 明确提示当前 backend 不支持

`feidex` 里最典型的例子：

1. reply continuation / append
   - 对 Claude 是“同 session 下追加一个后续 turn”，不是 same-turn `turn.steer`
2. `/compact`
   - 对 Claude 是把字面量 `/compact` 作为 prompt 透传，不是本地原生 compact 能力
3. `/session fork`
   - 允许先进入 pending fork 产品状态，真实 session id 在下一条消息后才物化
4. `AskUserQuestion` / `ExitPlanMode`
   - Claude 原生交互工具被映射成现有 Feishu request/card 流程

这对我们的约束是：

1. capability matrix 只表达 canonical/native support，不把 approximation 或 passthrough 记成 support
2. command catalog 需要单独声明某个产品命令在某 backend 下走哪种策略
3. approximation 必须显式记录语义变化，不能伪装成 canonical 等价
4. 默认策略仍然是 reject，只有被明确批准的命令才允许 approximation 或 passthrough

## 4. 当前事实

### 4.1 当前仓库的优势

本仓库已经有一套适合复用的上层骨架：

1. canonical command/event 模型已经较稳定。
2. `turn/item/request` 的产品语义已经在 daemon/orchestrator 成型。
3. Feishu surface、queue、resume、headless 编排已经存在。
4. 现有 `request.started` 已能承载 approval / user input / permission / elicitation 这类交互骨架。

所以问题不在“产品层完全不适合 Claude”，而在“运行时 seam 仍然是 Codex-shaped”。

### 4.2 当前仓库的结构卡点

目前最明显的结构卡点如下：

1. wrapper 入口仍是 Codex-only。
   - `internal/app/wrapper/entry.go`
2. wrapper runtime 直接持有 `*codex.Translator`。
   - `internal/app/wrapper/app.go`
3. translator 硬编码 Codex JSON-RPC 方法。
   - `internal/adapter/codex/translator_commands.go`
4. `hello.capabilities` 现在只有 `ThreadsRefresh`。
   - `internal/core/agentproto/wire.go`
5. daemon `onHello()` 仍然无条件发送 `threads.refresh`。
   - `internal/app/daemon/app_ingress.go`
6. 状态层还没有 backend/provider 维度。
   - `internal/core/state/types.go`
7. `/mode` 仍只有 `normal | vscode`。
   - `internal/core/orchestrator/service_surface_command_settings.go`

### 4.3 `feidex` 给出的现实证据

`feidex` 已经证明了下面几件事：

1. Claude 可以做长会话 runtime，不只是单轮 prompt。
2. Claude 可以做 session list / history。
3. Claude 的 request reply 应该按 backend-specific 语义回写。
4. Claude 的 live transport 和 session catalog/history 应该是两条平面。
5. backend-aware help/menu/filter 是可落地的。

更重要的是，`feidex` 证明了正确 seam 不是“让 Claude 假装成 Codex”，而是：

- app/product 层保持 backend-neutral
- provider 层各自保留原生 runtime

## 5. 统一目标架构

```text
Feishu / control / orchestrator / daemon
                |
                | canonical command / canonical event
                v
         backend runtime host
                |
                +-- codex runtime
                |      |
                |      +-- codex translator
                |      +-- codex child process
                |
                +-- claude runtime
                       |
                       +-- live transport adapter
                       +-- session catalog/history adapter
                       +-- request/approval adapter
```

### 5.1 分层职责

上层：

- 继续只理解 canonical `prompt.send`、`turn.interrupt`、`request.respond`、`threads.refresh`、`thread.history.read`
- 继续只理解 canonical `turn/item/request` 生命周期

中层 runtime host：

- 选择当前实例 backend
- 上报 backend 和 capabilities
- 把 canonical command 分发给对应 provider runtime
- 把 provider native event 归一化成 canonical event

底层 provider runtime：

- Codex 保持现有 JSON-RPC 路径
- Claude 使用自己的 native runtime

## 6. 必须先补的基座能力

这一节是整份方案最重要的部分。

如果这些基座不先补齐，Claude 接入成本不会小，而且会明显污染 Codex 当前路径。

### 6.1 基座一：provider identity 与 capability model

#### 要解决的问题

当前系统还没有正式表达：

1. 这个实例到底是 `codex` 还是 `claude`
2. 这个实例到底支持哪些 canonical 能力

#### 设计要求

`hello` 需要增加：

1. `backend/provider`
2. 更完整的 capability 集

建议能力集合：

- `prompt_send`
- `turn_interrupt`
- `turn_steer`
- `request_respond`
- `threads_refresh`
- `thread_history_read`
- `session_catalog`
- `resume_by_thread_id`
- `requires_cwd_for_resume`
- `supports_vscode_mode`

#### 非回归要求

1. 旧 wrapper 未上报 backend 时，默认按 `codex` 解释。
2. 旧 wrapper 未上报新 capability 字段时，按当前 Codex 行为填默认值。
3. 对现有 Codex-only 部署，这一层只能增加元信息，不能改现有命令路径。

### 6.2 基座二：wrapper 从 Codex wrapper 升级为 backend runtime host

#### 要解决的问题

当前 wrapper 不是“多 backend runtime host”，而是“Codex app-server wrapper”。

#### 设计要求

需要把 wrapper seam 改成：

1. wrapper 主进程负责 relay 连接与 canonical 协议
2. provider runtime 负责 native child / native transport
3. `codex.Translator` 下沉成 Codex provider 的实现细节

建议接口形状：

```go
type ProviderRuntime interface {
    Kind() string
    Capabilities() agentproto.Capabilities
    Start(context.Context) error
    Close() error
    HandleCommand(context.Context, agentproto.Command) error
    Events() <-chan []agentproto.Event
}
```

Codex runtime：

- 复用现有 translator + child process

Claude runtime：

- 新实现，不复用 Codex translator

#### 非回归要求

1. 第一阶段只做 seam 提取，不改 Codex 语义。
2. Codex runtime 的输出事件、ack 行为、helper/internal annotation 必须保持一致。

### 6.3 基座三：把 session catalog/history 从 live transport 分离

#### 要解决的问题

当前系统默认把：

- `threads.refresh`
- `thread.history.read`

都理解成某种 live command。

这对 Codex 成立，对 Claude 不成立。

#### 设计要求

provider runtime 要明确分两条平面：

1. `live turn transport`
   - `prompt.send`
   - `turn.interrupt`
   - `request.respond`
2. `session catalog / history plane`
   - `threads.refresh`
   - `thread.history.read`

对 Claude：

1. live turn transport 使用 `claude --print --input-format stream-json --output-format stream-json`
2. session catalog/history 基于本地 transcript / metadata

对 Codex：

1. 两条能力都仍可通过现有原生协议提供

#### 非回归要求

1. 现有 Codex `threads.refresh` / `thread.history.read` 行为保持不变。
2. daemon `onHello()` 必须 capability-aware，不再对所有实例无条件发 `threads.refresh`。

### 6.4 基座四：backend-aware request bridge

#### 要解决的问题

上层 `request.respond` 已经相对中立，但底层如何回写 request 仍然 provider-specific。

#### 设计要求

要明确拆两层：

1. product-level canonical request
2. provider-specific request reply adapter

Codex：

- 继续走 native reply/result 语义

Claude：

- `can_use_tool`
- `elicitation`
- 后续可扩的 plan-mode confirm

都在 Claude adapter 内部分开处理

#### 非回归要求

1. 现有 approval / request_user_input / permission approval 行为在 Codex 下不变。
2. 不允许为了兼容 Claude，去修改现有 canonical request 外形使 Codex 路径回归。

### 6.5 基座五：backend-aware state partition

#### 要解决的问题

当前状态层还默认：

- instance 没有 backend
- workspace defaults 没有 backend namespace
- surface resume 没有 backend 维度

这会直接导致 Claude/Codex 会话串扰。

#### 设计要求

需要最小扩展：

1. `InstanceRecord.Backend`
2. `SurfaceResumeEntry.ResumeBackend`
3. `WorkspaceDefaults[workspace][backend]`
4. thread/session 任何持久引用都要带 backend 维度

推荐主键：

- `backend + instance_id + thread_or_session_id`

#### 非回归要求

1. 旧数据缺失 backend 时，默认按 `codex` 解释。
2. 不做破坏性迁移。
3. 能 lazy 兼容的地方优先 lazy 兼容。

### 6.6 基座六：backend-aware command catalog、mode 与 routing

#### 要解决的问题

即使底层 runtime 能工作，如果上层菜单、命令 parser、mode、route 仍只有 Codex 假设，Claude 进入后依然会出现“可见但不可用”的问题。

#### 设计要求

需要正式把下面几个维度接到同一套 dispatch 决策中：

1. 当前 surface mode
2. 当前 attached instance backend
3. 当前 attached instance capabilities
4. 当前命令在该 backend 下的策略

固定决策顺序：

1. 先确定目标实例
2. 再看实例 backend
3. 再看当前 mode
4. 再看 capability 是否允许
5. 再看命令策略是 native / approximation / passthrough / reject
6. 不允许则 reject/problem + notice

命令 catalog 至少要能表达：

1. 这个命令依赖哪个 canonical 能力
2. 当前 backend 下是 native support 还是 approximation
3. 如果是 approximation，语义差异是什么
4. 如果是 passthrough，是否允许从菜单入口触发
5. 如果是 reject，用户看到什么提示

`/mode` 目标语义：

1. `/mode codex`
2. `/mode claude`
3. `/mode vscode`
4. `/mode normal` 兼容为 `/mode codex`

#### 非回归要求

1. 在 Claude 真正启用前，现有 `normal` 语义仍表现为 Codex normal。
2. 未 attach Claude 实例时，不主动暴露 Claude 命令与 Claude 菜单。

### 6.7 基座七：workspace config 按 backend 分区

#### 要解决的问题

当前 `model / reasoning_effort / access_mode` 默认配置仍然是单命名空间。

这会导致：

1. Codex 配置泄漏到 Claude
2. Claude 配置覆盖 Codex

#### 设计要求

工作区默认配置要升级为：

- `workspace -> backend -> config`

Codex 与 Claude 至少分别维护：

1. model
2. reasoning effort
3. access / permission mode

#### 非回归要求

1. 旧配置默认挂到 `codex` 名下。
2. 现有 Codex `/model` `/reasoning` `/access` 行为不变。

### 6.8 基座八：dark launch 与兼容开关

#### 要解决的问题

如果在基座没完全稳定前就直接让 Claude 暴露到正常产品入口，风险会回流到现有 Codex 行为。

#### 设计要求

需要保留明确的启用边界：

1. Claude runtime 可以先存在，但不默认出现在用户可见路径
2. backend-aware 基座先按 Codex-only 方式运行
3. Claude 命令、Claude mode、Claude instance 选择可以按 feature flag 或实例存在条件显式开启

#### 非回归要求

1. 未开启 Claude 时，当前行为与 master 保持一致。
2. Claude 代码存在不代表 Claude 产品入口默认可见。

### 6.9 基座九：UI/交互语义归一化与 projector 边界固化

#### 要解决的问题

Claude 接入的风险点不在 Feishu projector 本身，而在 provider 上游事件进入 canonical 语义层之前。

如果直接沿用“上游拼 markdown、下游直接透传”的方式，会重新引入过去的混乱：

1. backend 语义混入卡片渲染层
2. 动态文本与 markdown 合成边界失真
3. 不同 backend 的中间过程无法在同一 UI 语义下对齐

`feidex` 的实现证明 Claude 可以跑通完整产品链路，但它在业务层 markdown 预组装较重；本仓库应保留当前“语义先行、projector 末端渲染”的边界，不回退到上游拼装 markdown 的路径。

#### 设计要求

1. 新增 Claude provider semantic mapper
   - 把 Claude native item/tool/process event 先归一化为 canonical `eventcontract` payload，再进入 orchestrator/projector
2. 复用现有 UI 语义载体，不新增 backend 专属卡片骨架
   - `RequestPayload`
   - `PagePayload`
   - `SelectionPayload`
   - `TimelineTextPayload`
   - `ImageOutputPayload`
   - `ExecCommandProgressPayload`
3. 补齐中间过程语义模型
   - 现有 `render.Block` 仅适合基础文本输出，不应承担 Claude 过程语义主承载
   - 优先扩展 canonical 过程载体（含 `ExecCommandProgress` / timeline kind），不要把丰富过程信息塞回 markdown 文本
4. 固化渲染职责
   - 上游不得预拼 Feishu markdown 片段
   - Feishu adapter/projector 继续负责 markdown/plain_text/card 的最终转换
5. 补一层 backend-aware 可见性策略
   - 明确 Claude 过程语义在 `quiet / normal / verbose` 下的展示策略
   - 默认不破坏 Codex 当前可见性行为

#### 非回归要求

1. Codex 现有 structured text -> projector 渲染边界保持不变。
2. 任何 Claude 接入改造都不能把动态外部文本回流到上游 markdown 拼接路径。
3. 现有卡片 UI 状态机（replace/append、request 流程、final 流程）不因 provider 切换而改变语义。
4. 需要新增契约测试覆盖：
   - Claude 语义映射后仍满足 structured/plain_text 边界
   - 同一 canonical payload 在 Codex/Claude 下投影行为一致（除明确声明的能力差异）
   - normal 模式下不出现未经策略批准的过程噪音外溢

## 7. Claude 接入方案

在第 6 节的基座完成后，Claude 接入应该被收敛成一个相对独立的 provider implementation，而不是全仓改造。

### 7.1 接入面

Claude MVP 首选接入面固定为：

- `claude --print`
- `--input-format stream-json`
- `--output-format stream-json`
- 长生命周期 child
- stdin/stdout NDJSON

不用：

- ACP
- remote-control
- 伪 Codex RPC

### 7.2 Claude provider 的能力矩阵

MVP 目标值：

| 能力 | Claude MVP |
| --- | --- |
| `prompt.send` | support |
| `turn.interrupt` | support |
| `request.respond` | support |
| `threads.refresh` | support，通过 session catalog plane |
| `thread.history.read` | support，通过 transcript plane |
| `turn.steer` | unsupported |
| `supports_vscode_mode` | false |
| `resume_by_thread_id` | true |
| `requires_cwd_for_resume` | true |

注意：

1. 这个矩阵只声明 canonical/native capability
2. 不包含业务层 approximation
3. 不包含 raw passthrough

### 7.3 Claude runtime 内部设计

Claude runtime 分三块：

1. live transport
   - 会话启动
   - turn 开始
   - stream event 解析
   - interrupt
   - request roundtrip
2. session catalog/history
   - 列表
   - 历史
   - metadata
3. request bridge
   - permission decision
   - elicitation reply
   - plan confirmation 的后续扩展位

### 7.4 Canonical 映射规则

需要固定的映射包括：

1. `threadId` 在 canonical 层继续承载 Claude `session_id`
2. `turn.started` 来自 Claude 进入 running 边界
3. `turn.completed` 由 Claude `result` 给出终态，再由 idle/running 边界完成收口
4. `threads.refresh` / `thread.history.read` 不从 live stream 提供
5. `turn.steer` 显式 rejected

### 7.5 明确不做的假兼容

第一版明确不做：

1. 把 `turn.steer` 伪装成 `interrupt + prompt.send`
2. 把 session catalog/history 伪装成 live RPC
3. 把 Claude session 强行投影成 Codex thread lifecycle 的一比一 native 等价物

但这不等于“所有 Claude 非原生命令都只能 reject”。

需要允许一层显式、受控的产品策略：

1. native support
   - 直接走 canonical capability
2. business approximation
   - 只用于少数明确批准的命令，并记录语义变化
3. passthrough
   - 只用于明确接受 backend 自主解释的命令
4. reject
   - 默认值

第一版建议：

1. `turn.steer`
   - 继续 reject，不做近似
2. `/compact`
   - 如果后续验证 Claude CLI 侧命令稳定，可作为 passthrough 候选；但不计入 capability support
3. 某些 session 管理入口
   - 可以允许 pending-state approximation，但只能在 command catalog 中显式声明
4. plan/question 这类 Claude 原生交互工具
   - 走 request bridge 适配，不算 approximation，也不暴露成新的上层 canonical 能力

## 8. 分阶段实施

### 阶段 A：基座接线，但不引入 Claude 可见行为

目标：

1. hello/provider/capabilities 进入状态与派发决策
2. `onHello()` capability-aware
3. state 和 workspace defaults 引入 backend 维度

本阶段的用户侧预期：

- 现有 Codex 行为不变

### 阶段 B：把 Codex 提取成 provider runtime，但保持行为完全一致

目标：

1. wrapper 改成 backend runtime host
2. 现有 `codex.Translator` 下沉到 Codex runtime
3. 事件、ack、错误、helper/internal annotation 与当前保持一致

本阶段的用户侧预期：

- 仍然只有 Codex 行为
- 没有新增 Claude 产品入口

### 阶段 C：实现 Claude runtime skeleton，先 dark launch

目标：

1. Claude child 启动与关闭
2. live transport 打通
3. session catalog/history 读取打通
4. request bridge 打通

本阶段的用户侧预期：

- Claude 仍可隐藏
- Codex 不回归

### 阶段 D：开启 Claude normal-mode MVP

目标：

1. `/mode claude`
2. Claude `prompt.send`
3. Claude `turn.interrupt`
4. Claude `request.respond`
5. Claude `threads.refresh`
6. Claude `thread.history.read`
7. 明确 `turn.steer` rejected

本阶段的用户侧预期：

- Claude 是可见 backend
- 不支持能力有明确提示

### 阶段 E：补齐 command catalog / help / menu / workspace config

目标：

1. backend-aware help/menu/filter
2. Claude-specific model / permission config 入口
3. backend-aware `/list` `/use` `/new`

本阶段的用户侧预期：

- Claude 产品闭环更完整
- Codex 路径仍无回归

## 9. 验收标准

### 9.1 Codex 零回归

必须验证：

1. `go test ./...` 全量通过
2. Codex-only 部署下，行为与当前 master 一致
3. wrapper 仍能正确翻译 helper/internal traffic
4. `threads.refresh`、`thread.history.read`、`turn.steer` 在 Codex 下不回归

### 9.2 Claude 最小闭环

必须验证：

1. 新会话发送 prompt 成功
2. 恢复既有 session 成功
3. interrupt 成功
4. approval / user input / elicitation 回写成功
5. `threads.refresh` 与 `thread.history.read` 成功
6. `turn.steer` 稳定 rejected，且不污染 queue / request state

### 9.3 混合场景安全

必须验证：

1. Codex / Claude 同机并存不串扰
2. `/list` `/use` 不跨 backend 误用 id
3. surface resume 不跨 backend 恢复错误目标
4. backend 切换时不会继承旧 backend 的 selected thread / pending request / queue

## 10. 这份方案为什么能让 Claude 以后低成本接进来

因为这份方案把高成本变化前移成了“系统 seam 重切”，而不是“Claude 特殊分支堆满全仓”。

一旦第 6 节的九项基座补齐：

1. 上层 canonical 产品骨架已经不需要再为 Claude 改形
2. Codex 路径已经从“默认系统形状”降级为“一个 provider implementation”
3. Claude 的主要工作会收敛到独立 runtime 模块

也就是说，后续真正实现 Claude 的成本会主要落在：

1. Claude runtime
2. Claude session catalog/history
3. Claude request bridge
4. 少量 backend-aware 命令与菜单投影

而不是重写：

1. daemon
2. orchestrator
3. Feishu projector
4. relay 协议主骨架

## 11. 非目标与后续预留

第一阶段明确不做：

1. Claude 的 vscode mode
2. Claude 等价 `turn.steer`
3. Claude review/compact/skills 的强对齐
4. Claude 多模态图片输入的完整承诺
5. 全仓 `thread -> session` 命名迁移

后续预留位：

1. Claude fork/session branching
2. Claude plan-mode 更完整产品化
3. 更细的 backend-aware command catalog
4. 第三种 backend 的未来接入

## 12. 实施约束

从现在开始，Claude 相关实现默认遵循这份文档。

如果后续实现发现本文与真实代码或真实 Claude 行为冲突，处理顺序必须是：

1. 先修正文档
2. 再改实现

不要回到多份并行设计文档同时生效的状态。

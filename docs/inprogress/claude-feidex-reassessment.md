# Claude Feidex Reassessment

> Type: `inprogress`
> Updated: `2026-04-22`
> Summary: 基于 `feidex` 的真实 Claude 实现重新评估本仓库的接入抽象、能力边界与后续设计方向。

## 1. 文档定位

这份文档回答的是三个实现判断：

1. `feidex` 现在到底是如何把 Claude 做进飞书工作流里的。
2. 这些能力里哪些已经是“真实落地能力”，哪些仍然只是设计预留。
3. 对本仓库来说，当前 `wrapper + translator + canonical relay protocol` 架构是否能较好包进 Claude；如果能，正确的 seam 应该放在哪里。

这份文档不是要替代：

- [claude-normal-mode-poc-design.md](./claude-normal-mode-poc-design.md)
- [claude-provider-protocol-mapping.md](./claude-provider-protocol-mapping.md)

而是作为它们的补充证据层，专门回答“`#185` 之后，基于 `feidex` 真实实现重新看，哪些判断需要修正”。

## 2. 结论先行

### 2.1 总结判断

结论很明确：

1. `feidex` 已经证明 Claude 可以比较深地接进飞书工作流，不只是“单轮 prompt 转发”。
2. `feidex` 采用的不是“让 Claude 假装成 Codex app-server”，而是“产品层后端中立 + 后端实现层各自原生”。
3. 本仓库现有产品层和 canonical `turn/item/request` 事件层，整体上是有机会容纳 Claude 的。
4. 但本仓库当前的 `wrapper -> codex.Translator -> Codex JSON-RPC` 这一条运行时 seam 仍然明显偏 Codex，不能直接拿来装 Claude。
5. 如果要让 Claude 比较自然地进入当前架构，应该把“translator”下沉为某个 provider 的实现细节，在其上面新增一层 backend/provider runtime seam；对 Claude 更合适的词是 `adapter` 或 `runtime`，不是继续把它塞进现有 `codex translator` 抽象。

### 2.2 对 `#185` 的重新判断

`#185` 现在只能作为历史背景，不应再单独作为 Claude 设计依据。

仍然成立的部分：

- 需要 backend-aware capability gate。
- `turn.steer` 不能假装和 Codex 完全同语义。
- Claude 与 Codex 的会话数据需要按 backend 隔离。
- vscode mode 不应在第一阶段硬兼容 Claude。

已经过时或不完整的部分：

- “Claude 缺少本地 session list / history / model / effort / permission 入口”这个判断已经被 `feidex` 推翻。
- “只要把 Claude 映射进 Codex 风格方法名，就能自然接入”这个方向不成立。
- 早期仅基于公开文档推断的接入结论，已经不如 `feidex` 这种真实产品实现可靠。

## 3. 调研基线

本次重新评估主要基于两类代码事实。

### 3.1 `feidex` 侧

仓库：

- <https://github.com/yuhuan417/feidex>

关键文档：

- <https://github.com/yuhuan417/feidex/blob/main/docs/backend-layering.md>
- <https://github.com/yuhuan417/feidex/blob/main/docs/capabilities.md>
- <https://github.com/yuhuan417/feidex/blob/main/docs/claude-cli-backend-design.md>

关键实现：

- `internal/app/backend_capability_facade.go`
- `internal/app/backend_runtime_facade.go`
- `internal/app/conversation_backend_facade.go`
- `internal/app/backend_server_request_adapter.go`
- `internal/app/claude_runtime.go`
- `internal/app/claude_session_catalog.go`
- `internal/app/claude_history.go`
- `internal/app/claude_permission_config.go`
- `internal/app/claude_model_config.go`
- `internal/claudecli/session.go`
- `internal/claudecli/protocol.go`
- `internal/state/store.go`

### 3.2 本仓库侧

关键文档：

- [claude-normal-mode-poc-design.md](./claude-normal-mode-poc-design.md)
- [claude-provider-protocol-mapping.md](./claude-provider-protocol-mapping.md)

关键实现：

- [internal/app/wrapper/entry.go](/data/dl/fschannel5/internal/app/wrapper/entry.go)
- [internal/app/wrapper/app.go](/data/dl/fschannel5/internal/app/wrapper/app.go)
- [internal/adapter/codex/translator_commands.go](/data/dl/fschannel5/internal/adapter/codex/translator_commands.go)
- [internal/core/agentproto/wire.go](/data/dl/fschannel5/internal/core/agentproto/wire.go)
- [internal/core/agentproto/types.go](/data/dl/fschannel5/internal/core/agentproto/types.go)
- [internal/core/state/types.go](/data/dl/fschannel5/internal/core/state/types.go)
- [internal/app/daemon/app_ingress.go](/data/dl/fschannel5/internal/app/daemon/app_ingress.go)
- [internal/core/orchestrator/service_surface_command_settings.go](/data/dl/fschannel5/internal/core/orchestrator/service_surface_command_settings.go)

## 4. `feidex` 的真实 Claude 抽象

### 4.1 分层方式不是“Codex 协议兼容层”

`feidex` 的关键设计不是 translator，而是 backend-aware app-layer facades。

它把逻辑拆成三层：

1. Feishu frontend / orchestration layer
2. backend capability layer
3. backend implementation layer

这一点在 `docs/backend-layering.md` 里写得很直接：产品行为层不直接消费具体 backend 的 CLI/RPC 细节，backend 差异先在 capability/runtime facade 和 implementation layer 收口。

对 Claude 来说，这个选择非常关键，因为它避免了两个坏方向：

1. 不是“只保留 Codex/Claude 的能力交集”。
2. 不是“让 Claude 模拟成 Codex RPC server”。

### 4.2 Claude runtime 是原生 runtime，不是假 Codex RPC

`feidex` 的 Claude 路径不是把 `stream-json` 伪装成 `thread/start`、`turn/start` 那种 Codex JSON-RPC 服务器。

它单独实现了：

- `internal/claudecli/session.go`
- `internal/claudecli/protocol.go`
- `internal/claudecli/types.go`

这套实现直接启动 `claude` CLI，通过 stdio NDJSON 驱动会话。

它有自己的：

- `SessionConfig`
- `ReadyEvent`
- `TextEvent`
- `ThinkingEvent`
- `ToolCompleteEvent`
- `TurnCompleteEvent`
- permission handler
- interactive tool handler

也就是说，`feidex` 对 Claude 的抽象是“独立 runtime + 事件归一化”，不是“给现有 Codex translator 多加几个 if”。

### 4.3 会话目录和历史不是从 live wire 硬抠出来的

`feidex` 已经把一个很重要的事实落到了产品里：

- Claude 的 live turn transport
- Claude 的 session catalog / history

是两条不同的数据面。

具体实现：

- `internal/app/claude_session_catalog.go`
- `internal/app/claude_history.go`

做法是直接读取 Claude 本地 transcript / session 文件，而不是假设 live stream 天然支持 `thread/list` / `thread/read`。

这点和我们当前 [claude-provider-protocol-mapping.md](./claude-provider-protocol-mapping.md) 的判断是一致的，而且 `feidex` 证明这条路不只是“理论可行”，而是已经被产品化使用。

### 4.4 request / approval / user input 是 backend-specific 回写，不是统一拍平

`feidex` 在 `internal/app/backend_server_request_adapter.go` 里把 Codex 和 Claude 的 request reply 分开处理。

Codex 路径：

- 直接回 `Reply(...)` / `ReplyError(...)`

Claude 路径：

- `ResolveApproval(...)`
- `ResolveUserInput(...)`
- `CancelPending(...)`

这意味着它没有强迫所有 backend 共享同一份 request wire shape，而是保留了：

- 产品层统一的“这里需要用户处理一个 request”
- backend-specific 的“这个 request 应该如何回写”

对我们当前架构来说，这一点非常有价值，因为我们的 canonical `request.started` / `request.respond` 已经比较中立，但底层实现现在还没把 provider-specific request semantics 收到正确层级。

### 4.5 backend-specific 配置和展示差异是第一等能力

`feidex` 没有因为双后端而把产品功能压平为交集。

它已经把下面这些差异做成了正式产品能力：

- `/thread ...` 对 Codex 生效
- `/session ...` 对 Claude 生效
- Claude 的 `/history` 是本地 transcript 读取
- Claude 的 `/model` / `/effort` 是本地入口
- Claude 的 permission mode 有独立配置入口
- `/help` 和菜单会按 backend 过滤

这说明一个重要产品结论：

- “backend-neutral 产品骨架”不等于“功能上只能做交集”

这和 `docs/claude-cli-backend-design.md` 的设计护栏完全一致。

## 5. `feidex` 证明 Claude 现在真实可用到什么程度

### 5.1 已被真实落地的核心能力

按 `feidex` 当前实现，Claude 已经不再是“只能简单发一句 prompt”的状态。

已经真实落地的能力包括：

| 能力 | `feidex` 状态 | 主要证据 |
| --- | --- | --- |
| 长会话 runtime | 已落地 | `internal/app/claude_runtime.go`, `internal/claudecli/session.go` |
| 会话恢复 | 已落地 | `EnsureSession(...)`, `--resume`, `claude_thread_binding.go` |
| 会话 fork | 已落地 | `conversation_backend_facade.go`, `claude_runtime.go` |
| 会话列表 | 已落地 | `claude_session_catalog.go` |
| 历史记录 | 已落地 | `claude_history.go` |
| interrupt | 已落地 | `claudecli.Session.Interrupt`, `interruptClaudeActiveTurn(...)` |
| approval 回写 | 已落地 | `backend_server_request_adapter.go` |
| 用户输入回写 | 已落地 | `backend_server_request_adapter.go` |
| plan mode 退出/确认 | 已落地 | `claude_runtime.go`, `claude_support.go` |
| model / effort 配置 | 已落地 | `claude_model_config.go` |
| permission mode 配置 | 已落地 | `claude_permission_config.go` |
| backend-aware 菜单/帮助过滤 | 已落地 | `backend_capability_facade.go`, `docs/capabilities.md` |

### 5.2 这会推翻哪些旧认知

`feidex` 当前实现至少推翻了下面这些旧结论：

1. Claude 没法做本地 session list。
2. Claude 没法做本地 history。
3. Claude 侧不能比较自然地做用户补充输入。
4. Claude 侧不能做会话级权限模式。
5. 双后端产品最终只能退化到“共同交集 + 一堆禁用按钮”。

更准确的结论应该是：

- Claude 可以做较完整的飞书产品闭环。
- 但前提是产品架构承认“Claude 是另一套原生 runtime”，而不是 Codex 协议的一个方言。

## 6. 和我们当前架构的差异

### 6.1 我们的强项

本仓库并不是“整体不适合接 Claude”。

相反，我们已经有几块非常适合复用的基础：

1. canonical `command / event / request` 模型已经比较清楚。
2. `turn/item/request` 产品语义已经在 daemon/orchestrator 侧成型。
3. `request.started` 已经覆盖 approval / `request_user_input` / `permissions_request_approval` / `mcp_server_elicitation` 这类交互骨架。
4. Feishu surface、queue、resume、headless 这些高层编排逻辑已经存在。

也就是说，我们真正缺的不是“产品层完全不懂 Claude”，而是“运行时 provider seam 还没长出来”。

### 6.2 我们现在最不适合直接承接 Claude 的部分

当前最明显的结构问题有五个。

#### 1. wrapper 入口还是 Codex-only

[internal/app/wrapper/entry.go](/data/dl/fschannel5/internal/app/wrapper/entry.go) 现在仍然只接受 `app-server`，并直接返回：

- `wrapper role only supports codex app-server mode`

这意味着当前 wrapper 的主身份不是“provider runtime host”，而是“Codex app-server wrapper”。

#### 2. wrapper runtime 绑死在 `*codex.Translator`

[internal/app/wrapper/app.go](/data/dl/fschannel5/internal/app/wrapper/app.go) 的 `App` 当前直接持有：

- `translator *codex.Translator`

并在 `New(...)` 时直接 `codex.NewTranslator(...)`。

这不是一个 provider-agnostic seam，而是一个已经用具体 provider 名字写死的运行时核心。

#### 3. translator 本身硬编码了 Codex JSON-RPC 方法

[internal/adapter/codex/translator_commands.go](/data/dl/fschannel5/internal/adapter/codex/translator_commands.go) 里直接硬编码：

- `thread/start`
- `thread/resume`
- `turn/start`
- `turn/steer`
- `turn/interrupt`
- `thread/list`
- `thread/read`

这对 Codex 完全合理，但对 Claude 不成立。

Claude 更接近：

- 长生命周期会话
- stdin/stdout NDJSON
- live transport + 本地 catalog/history 双平面

因此如果继续把“translator”定义成“canonical command -> 单条 native method 调用”的对象，Claude 会被迫扭曲。

#### 4. hello capability 模型仍然过薄

[internal/core/agentproto/wire.go](/data/dl/fschannel5/internal/core/agentproto/wire.go) 里的 `Capabilities` 现在只有：

- `ThreadsRefresh`

这还不够表达下面这些关键差异：

- 是否支持 session catalog
- 是否支持 history read
- 是否支持 turn steer
- 是否要求 resume 时提供 cwd
- 是否支持 vscode mode

#### 5. 产品状态里还没有 backend/provider 维度

[internal/core/state/types.go](/data/dl/fschannel5/internal/core/state/types.go) 里的 `ProductMode` 仍然只有：

- `normal`
- `vscode`

同时 surface / instance / workspace 默认配置里，也还没有成型的 backend-aware partition。

这会直接影响：

- session/thread lineage 隔离
- `/mode codex|claude|vscode` 这类入口
- workspace 级 model/access/reasoning 默认值分 backend 挂载

### 6.3 我们还有一个更隐蔽的问题：daemon 默认假设所有实例都支持 `threads.refresh`

[internal/app/daemon/app_ingress.go](/data/dl/fschannel5/internal/app/daemon/app_ingress.go) 现有 hello 路径仍然会把启动刷新绑在 `threads.refresh` 上。

这在 Codex-only 世界里没问题，但一旦引入 Claude，就会把“session catalog 是另一条平面”这个事实冲掉。

这也是为什么我们现有 [claude-normal-mode-poc-design.md](./claude-normal-mode-poc-design.md) 才会专门强调：

- `onHello` 不能再无条件发 `threads.refresh`

### 6.4 一张表看清 `feidex` 和我们的结构差异

| 维度 | `feidex` 当前做法 | 我们当前做法 | 对 Claude 的影响 |
| --- | --- | --- | --- |
| 产品分层 seam | app-layer backend facades | relay canonical protocol 在上，但 wrapper seam 仍偏 Codex | 我们高层可复用，但底层 runtime seam 要重切 |
| Codex 抽象位置 | Codex 是 backend implementation 之一 | Codex translator 接近 wrapper 核心 | 现在的 translator 太像“系统默认 backend” |
| Claude runtime | 独立 Claude runtime + `claudecli` | 尚不存在 | 需要新增真正的 Claude runtime，而不是给 Codex translator 加分支 |
| session catalog / history | 本地 transcript/catalog 单独实现 | 默认假设 `threads.refresh` / `thread.history.read` 走 live command | 必须拆双平面 |
| request reply | backend-specific adapter | canonical `request.respond` 已有，但底层回写尚未 provider 化 | 我们产品层占优，runtime 层要补 provider-specific reply |
| 菜单/帮助差异 | backend-aware filter 已落地 | 还未形成完整 backend-aware 命令可见性 | Claude 进入后若不先加 gate，会出现“可见但不可用” |
| 会话 lineage 持久化 | `BackendThreads` 显式分 backend 存 | 状态层仍缺 backend-aware 会话分区 | Claude / Codex 会话容易串扰 |
| backend 选择 | app 层显式 backend 选择 | 目前更接近 mode/instance 语义 | 不能照搬交互，但必须补 backend 维度 |

## 7. 回到你的问题：现在的 wrapper 和 translator 能不能包住 Claude

### 7.1 短答案

能包，但不是按现在的形状直接包。

### 7.2 更准确的判断

现在的架构里，应该分开看两层：

1. 产品层 / canonical 层
2. wrapper / translator 运行时层

对第一层的判断：

- 可以比较自然地容纳 Claude。

对第二层的判断：

- 现在还不行。

原因不是“Claude 和我们产品不匹配”，而是：

- 现有 translator seam 太接近 `Codex JSON-RPC method bridge`

它适合做：

- `canonical command <-> Codex app-server`

不适合直接做：

- `canonical command <-> Claude runtime + local transcript/catalog`

### 7.3 如果继续坚持当前 translator 形状，会遇到什么问题

如果强行把 Claude 继续塞进当前 translator 机制，通常会走到以下坏结果之一：

1. 在 `codex.Translator` 里堆 provider 分支，最后这个类型既不是 translator，也不是 runtime host。
2. 强行把 Claude 的 catalog/history 伪造成 live RPC method，导致状态和错误语义越来越假。
3. 为了兼容 `turn/steer` / `thread/list` / `thread/read` 等 Codex 专用方法，开始发明一堆伪 native method。
4. 最终上层以为自己拿到的是“通用协议”，实际上收到的是被 Codex 语义拉歪的 Claude 仿真层。

这条路会产生结构债，而且会让后续接入第三个 backend 时更难扩展。

## 8. 推荐的抽象调整

### 8.1 “translator”应该下沉为 provider 实现细节

推荐把运行时 seam 改成：

```text
daemon / orchestrator / control
          |
          | canonical command / canonical event
          v
    provider runtime host
          |
          +-- codex runtime
          |      |
          |      +-- codex translator (JSON-RPC translation)
          |
          +-- claude runtime
                 |
                 +-- live transport adapter
                 +-- session catalog/history adapter
```

在这个结构下：

- `translator` 仍然可以存在，但它只属于 Codex runtime。
- Claude 更适合叫 `runtime` / `adapter`，因为它不止做协议翻译，还要管本地 session catalog、history、resume 约束、permission/user input 回写。

### 8.2 对我们来说，最值得借 `feidex` 的不是代码细节，而是 seam 位置

最该借的点：

1. backend-aware capability facade
2. backend-aware runtime facade
3. conversation backend facade
4. provider-specific request reply adapter
5. session lineage 按 backend 分区存储
6. Claude 的 live transport 和 catalog/history 分面

最不该直接照搬的点：

1. `feidex` 的 backend 选择是 app-layer 本地选择，而我们还有 relay/wrapper/instance 这一层分离。
2. `feidex` 没有我们当前这种 wrapper canonical protocol 中心地位，所以它不需要解决 hello/capability/instance state 那一层协议演进。
3. 我们现有 relay 协议已经比 `feidex` 的 app internals 更中立，因此没必要回退到“所有差异都在 app layer if/else 里收口”的方式。

## 9. 在我们当前架构下，Claude 可以怎么包进来

### 9.1 推荐方向

推荐继续保留我们现有的高层骨架：

- relay protocol
- daemon
- orchestrator
- control
- Feishu projector / gateway

但把 wrapper 下面的 provider seam 改成真正的 backend-aware runtime。

最小需要新增的结构能力：

1. instance hello 必须显式带 backend/provider 和更完整的 capabilities。
2. wrapper runtime 必须能够选择 `codex` 或 `claude` provider。
3. provider 必须能声明：
   - live turn transport 能力
   - request respond 能力
   - session catalog 能力
   - history read 能力
   - resume 约束
   - vscode mode 支持情况
4. state 必须按 backend 维度隔离 session/thread lineage。
5. `/mode`、`/list`、`/use`、resume、workspace defaults 必须进入 backend-aware 语义。

### 9.2 我们现有两份 Claude 文档里，哪些判断已经被 `feidex` 实现印证

[claude-provider-protocol-mapping.md](./claude-provider-protocol-mapping.md) 里这些判断，现在都被 `feidex` 的真实实现印证了：

1. Claude live turn transport 应独立于 session catalog/history plane。
2. `threadId` 在产品层可以先继续承载 Claude `session_id`。
3. `turn.steer` 不应伪装成完全等价能力。
4. Claude 需要自己的 adapter/runtime，而不是往 Codex translator 里塞 provider 分支。

[claude-normal-mode-poc-design.md](./claude-normal-mode-poc-design.md) 里这些判断也仍然成立：

1. hello capability gate 必须先落地。
2. `onHello` 不能无条件发送 `threads.refresh`。
3. backend-aware mode / state partition 是必须项。

### 9.3 唯一需要明显修正的地方

真正需要修正的不是总体方向，而是对 Claude 能力完整度的预期。

现在更准确的预期应该是：

- Claude 不是“只有主链路，目录/历史/配置都要后补”的弱 backend。
- 只要 seam 放对，它已经能成为一个有自己 session 管理、history、permission mode、model/effort 入口的完整 backend。

## 10. 建议的后续设计动作

### 10.1 文档层

后续设计应以三份文档组合为基线：

1. [claude-normal-mode-poc-design.md](./claude-normal-mode-poc-design.md)
2. [claude-provider-protocol-mapping.md](./claude-provider-protocol-mapping.md)
3. 当前这份 `claude-feidex-reassessment.md`

各自职责：

- `normal-mode-poc-design`
  - 产品入口、状态机、阶段范围
- `provider-protocol-mapping`
  - canonical command/event 与 Claude 行为映射
- `feidex-reassessment`
  - 真实实现参考、结构差异、架构迁移判断

### 10.2 实现层

如果开始真正实现，推荐按下面顺序推进：

1. 扩展 `agentproto.Hello`
   - 增加 backend/provider
   - 增加完整 capability 集
2. 在 daemon/orchestrator 落 backend-aware command gate
3. 在 state 中引入 backend-aware lineage / workspace defaults
4. 重构 wrapper seam
   - `codex.Translator` 下沉为 `codex runtime` 内部细节
   - 新增 `claude runtime`
5. 把 Claude 拆成两平面
   - live transport
   - session catalog/history

### 10.3 命名建议

针对你问的“translator 还是 adaptor”：

推荐是：

- 上层统一叫 `provider runtime` 或 `backend runtime`
- provider 内部再有：
  - Codex `translator`
  - Claude `adapter`

原因很简单：

- Codex 这边确实更像协议翻译器。
- Claude 这边已经不仅仅是翻译，而是“runtime + catalog/history + request bridge”。

如果继续在顶层都叫 translator，抽象会被 Codex 语义牵着走。

## 11. 最终结论

基于 `feidex` 当前真实实现，结论可以收敛成一句话：

- 我们的高层架构可以较好容纳 Claude，但前提是把现有 `wrapper/codex.Translator` 从“系统总 seam”降级为“Codex provider 的实现细节”，在其上新增真正 backend-aware 的 runtime seam。

换句话说：

- Claude 可以 fit 进现在的产品命令和交互骨架。
- Claude 不能自然 fit 进现在这版 Codex-shaped translator。

真正该延续的是：

- canonical product semantics

真正该重切的是：

- provider runtime seam

# Claude Backend Integration Plan

> Type: `inprogress`
> Updated: `2026-05-04`
> Summary: 同步 Claude profile、session 平面与运行时 MCP 注入基线：profile 覆盖端点、认证、模型与默认 reasoning 环境，不拥有独立 `CLAUDE_CONFIG_DIR`，不同 profile 共享同一 Claude session/history/catalog；Claude child launch 追加运行时 MCP 时必须保留用户既有 MCP。当前实现还已把 Claude headless `/reasoning` 接进 dispatch 前 runtime preflight：新 turn 会冻结各自 reasoning，必要时在发送前自动 restart 到匹配实例；Claude 模型只来自 profile，不再开放飞书侧 `/model` 热改。最新基线还补上了 Claude `prompt.send` 与 `turn.steer` approximation 的本地图片输入支持，文件继续保持 `@path` 文本桥接；`turn.steer` 仍不宣称 native capability，但 reply auto steer 与 `/steerall` 已可把文本与本地图片补充并入当前 active turn。

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
- product command support profile

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
2. command catalog 需要通过 command support profile 单独声明某个产品命令在某 backend 下走哪种策略
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
4. hello 已能显式携带 backend identity 与扩展 capability matrix，但当前真正被 end-to-end 消费的仍主要是 `threads.refresh` gate。
   - `internal/core/agentproto/wire.go`
   - `internal/app/wrapper/app.go`
5. daemon `onHello()` 已改为按实例 capability gate 决定是否发送初始化 `threads.refresh`，但更广义的 capability-aware dispatch 仍未全面铺开。
   - `internal/app/daemon/app_ingress.go`
   - `internal/core/orchestrator/service_helpers_threads.go`
6. `#492` 已把 surface-level backend carrier、workspace defaults、surface resume 与 `/mode codex|claude|vscode` 的底层状态语义落成真实代码，但 thread/session 持久平面仍没有完全 backend-neutral。
   - `internal/core/state/types.go`
   - `internal/core/state/workspace_defaults.go`
   - `internal/app/daemon/app_surface_resume_state.go`
   - `internal/app/daemon/surfaceresume/state.go`
7. daemon 启动时挂接的持久 thread catalog 仍然是 Codex-specific。
   - `internal/app/daemon/entry.go`
8. command support profile（visible / dispatch allow / native / approximation / passthrough / reject）已经开始作为 durable contract 承载命令可见与静态可执行性。
   - `internal/core/control/feishu_command_display_profiles.go`
   - `docs/inprogress/claude-backend-integration-plan.md`
9. display / binding / registry / parser 仍有 `ProductMode` / `FamilyID` / `family.default` 固化点。
   - `internal/core/control/feishu_command_binding.go`
   - `internal/core/control/feishu_command_registry.go`
   - `internal/core/control/feishu_commands_parse.go`

### 4.2.1 2026-04-27 已落地部分（#463）

`#463` 已把第一段 backend/provider seam 落成真实代码：

1. wrapper hello 现在显式上报 `backend + capabilities`，Codex 默认值不再退化成“只有 `ThreadsRefresh` 一位”。
2. daemon `onHello()` 现在会消费 `threadsRefresh` gate；不支持该能力的实例不会再被错误下发初始化 refresh。
3. startup refresh pending 现在只对真实派发了 refresh 的实例记账，不支持 refresh 的实例会直接把首轮门控判定为 settled。
4. instance runtime 已持有最小 backend/capability snapshot，thread metadata refresh 也开始消费这份 gate。

当时尚未完成、且后续仍需继续推进的部分主要是：

1. wrapper 还不是 backend-aware runtime host。
2. command support profile 已收口当前 live 的 visible / dispatch allow / native / approximation / passthrough / reject；后续新增 backend 或能力时仍需继续扩展 profile。
3. display / binding / registry / parser 还没有真正以 variant/backend 作为主驱动。

### 4.2.2 2026-04-28 已落地部分（#492）

`#492` 已把最危险的一段 backend 串态 seam 收口到真实代码：

1. surface 现在有独立 `Backend` carrier，新 surface 默认 `codex`，detached catalog context 也会保留当前 backend。
2. `WorkspaceDefaults` 已改为按 `workspace + backend` 分区；旧数据缺失 backend 时 lazy 兼容为 `codex`。
3. `surface resume state` 已新增 `Backend`，materialize / persist / recovery / auto-restore 都要求同 backend 才继续；旧 entry 同样 lazy 默认 `codex`。
4. `/mode` 的底层语义已固定为：
   - `codex = Backend=codex + ProductMode=normal`
   - `claude = Backend=claude + ProductMode=normal`
   - `vscode = Backend=codex + ProductMode=vscode`
   - `normal` 仍作为 `/mode codex` 的兼容 alias
5. `codex <-> claude` 切换时会走 detach-like 清理，并显式清掉旧 backend 的 resume target；但 headless 下的 managed exact-thread continuation 已不再是 Codex-only，Claude 也走同一条恢复主链，只把 backend 差异留在 runtime-native resume mechanics。
6. headless attach / auto-resume / visible-instance resolver 已开始按 backend 隔离，避免把 `codex` 的 thread/workspace 恢复到 `claude`，反之亦然。

### 4.3 2026-04-26 来自最新 `feidex` 的新增实现约束

基于 `yuhuan417/feidex` 当前 `main` 及其最近几次 Claude 修补，新增需要吸收的护栏如下：

1. 不能假设 Claude 一定会发出单一、显式的 completed 终态事件。
   - `abddb95` 已证明：Claude CLI 可能在最终输出已经齐备后退出，但没有再发 canonical completed。
   - 本仓库后续必须补 runtime-exit reconciliation，并在会话退出时清理 stale active-op。
2. Feishu 卡片上对 Claude plan approve/reject 这类即时操作，优先走 callback response inline replace。
   - `6d05ba7` 与 `b454943` 已证明：如果只靠后续异步 patch，用户会看到卡片状态短暂不一致，甚至不更新。
3. quiet/progress 视图中的文件聚合 identity 必须使用稳定全路径。
   - `8c895e9` 已证明：只按显示文件名去重会错误折叠不同目录下的同名文件。
4. “Claude 还缺很多本地产品能力，所以先按大面积 reject 设计”这个旧前提已经过时。
   - `ff0c522` 与 `docs/capabilities.md` 已证明：backend-aware help/menu filter、`/history`、`/model`、`/effort`、`/session fork`、`/session permissions` 以及 `/compact` passthrough 都是现实能力，不是理论占位。
5. Claude 已经流出部分文本，但 turn 最终结果为 error 时，仍然必须 materialize 最终答复，而不是只把 turn 标成 failed。
   - `94ae729` 已证明：final output pipeline 不能把“是否发送 final message”只绑在 `success=true` 上；error result 也可能需要对用户已见文本做最终收口。
6. plan-mode reject 不只是“拒绝这次 request”，还必须显式触发 stop / interrupt。
   - `ff57016` 已证明：如果只发送 deny control response 而不停止 Claude CLI，Claude 会继续执行。
7. 如果后续要给 Claude 做 steer approximation，输入必须并入当前 active turn，不能起一个独立假 turn。
   - `2f25033` 已证明：“steer 另起一轮”会把 submission 卡在 queuing；interrupt 后还必须同步清理 active operation bookkeeping。
8. backend-aware workspace/product 命令不能只做 display filter；parse、help registry、local-command matcher 也要同步 backend-specific。
   - `8503be2`、`668eaa8`、`8c06d4e` 已证明：`/workspace choose`、`/workspace permissions` 这类命令只做菜单过滤不够，help copy、matcher、local registry 一旦漏一处，就会直接退化成 passthrough 或错误命令。

这些新增约束不会改变“先补基座再接 Claude”的主结论，但会直接影响 completed 收口、卡片交互、过程可见性，以及命令策略矩阵的收口方式。

### 4.4 `feidex` 给出的现实证据

`feidex` 已经证明了下面几件事：

1. Claude 可以做长会话 runtime，不只是单轮 prompt。
2. Claude 可以做 session list / history。
3. Claude 的 request reply 应该按 backend-specific 语义回写。
4. Claude 的 live transport 和 session catalog/history 应该是两条平面。
5. backend-aware help/menu/filter 是可落地的。

更重要的是，`feidex` 证明了正确 seam 不是“让 Claude 假装成 Codex”，而是：

- app/product 层保持 backend-neutral
- provider 层各自保留原生 runtime

### 4.5 2026-04-28 本机黑盒已确认行为

这一轮不再只靠 `feidex` 推断；本机真实 Claude CLI 黑盒已经把几条最关键的语义固定下来。

基线证据：

- 方案文档：`docs/draft/claude-cli-blackbox-test-plan.md`
- 结果文档：`docs/draft/claude-cli-blackbox-findings-2026-04-28.md`
- 关键样本：
  - `BB-08`：`AskUserQuestion`
  - `BB-09 deny/allow`：`ExitPlanMode`
  - `BB-04`：same-cwd / cross-cwd resume

已确认事实：

1. `claude` 不能假设存在于非交互 shell 的 `PATH`；运行时必须显式带 `CLAUDE_BIN`。
2. `stream-json` 模式下必须先发 `initialize`，否则采样会偏离真实协议路径。
3. 任何要桥接审批/交互工具的场景，都必须加 `--permission-prompt-tool stdio`。
4. `AskUserQuestion` 与 `ExitPlanMode` 都不是“独立来的 control_request 原语”，真实顺序是：
   - assistant 先发 `tool_use`
   - CLI 再发 `control_request(can_use_tool)`
5. `ExitPlanMode` 的 `control_request.request.input` 在真实样本里可以是空对象 `{}`；不能把 plan 正文的读取建立在这里。
6. `ExitPlanMode` 的 plan 正文主来源首先是 assistant 已输出文本；allow 后的 `user.tool_result.tool_use_result.filePath` 还会给出 Claude 本地 plan 文件线索，可作为 fallback materialization 来源。
7. Claude session/transcript 强绑定 cwd/workspace：same-cwd resume 可用，cross-cwd resume 会失败。

对本文的直接约束：

1. plan/question 不能只按 request bridge 设计，必须按多 carrier 设计。
2. `control_request` 只负责审批，不是 plan 正文的唯一来源。
3. `thread.history.read` 与 session 恢复都必须显式保留 workspace/cwd 维度。

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

#### 6.2.1 `#501` Claude profile schema + launch contract（2026-04-29）

`#501` 已把 Claude profile 的第一层基座 contract 落成真实代码，当前基线如下：

1. 主配置 schema
   - persisted Claude profiles 进入主 `config.json` 的 `claude.profiles`
   - built-in `default` profile 是 synthetic / immutable，不直接落盘
   - 当前只固定两种认证模式：
     - `inherit`
     - `auth_token`
   - model overrides 当前固定两项：
     - `model -> ANTHROPIC_MODEL`
     - `smallModel -> ANTHROPIC_DEFAULT_HAIKU_MODEL`
   - profile 可选默认 reasoning：
     - 空值表示不设置，继续使用更高优先级覆盖或系统默认
     - `low / medium / high / max -> CLAUDE_CODE_EFFORT_LEVEL`
     - `high / max` 额外强制 `CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING=1`
     - profile/default/runtime reasoning 生效时，会清掉 `CLAUDE_CODE_DISABLE_THINKING`
2. admin backend contract
   - `GET /api/admin/claude/profiles`
   - `POST /api/admin/claude/profiles`
   - `PUT /api/admin/claude/profiles/{id}`
   - `DELETE /api/admin/claude/profiles/{id}`
   - response 只回显 redacted summary，不回显旧 token；token 只暴露 `hasAuthToken`
   - built-in `default` profile 在 list 中可见，但只读、不可编辑、不可删除
3. product contract: profile is not a session boundary
   - Claude profile 是“调用配置”：端点、认证、模型、默认 reasoning。
   - Claude profile 不是“会话空间”：同一个用户在不同 profile 下看到同一组 Claude session/history/catalog。
   - 用户切换 profile 后，后续新启动或恢复的 Claude 子进程使用新 profile 的调用配置；已有 session id、workspace session catalog 与恢复逻辑不因为 profile 改变而分叉。
   - `/list`、`/use`、自动恢复、workspace recency 与 session history 只能按 backend/workspace/session 语义过滤，不允许重新引入按 profile 隔离 session 的路径。
4. launch-time profile injection
   - profile 注入发生在 daemon 启动 wrapper 时，而不是 Claude child-only flags
   - 当 backend=`claude` 且选择 custom profile 时：
     - 保留继承环境中的 `CLAUDE_CONFIG_DIR`
     - 清掉继承环境中的 `ANTHROPIC_*` profile 覆盖项
     - 再按 profile 注入 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_MODEL`、`ANTHROPIC_DEFAULT_HAIKU_MODEL`
     - profile 配置了默认 reasoning 时，会通过统一 helper 注入或覆盖 Claude reasoning env：始终设置 `CLAUDE_CODE_EFFORT_LEVEL`，`high / max` 额外禁用 adaptive thinking，并清掉 `CLAUDE_CODE_DISABLE_THINKING`；profile 未配置 reasoning 时不主动修改底层默认值
   - 如果 daemon start command 携带显式 `ClaudeReasoningEffort`，它必须在 profile env 注入之后覆盖 profile 默认值。
   - profile 只是 endpoint/key/model/reasoning 配置，不是 session namespace
   - wrapper 本地 session catalog/history plane 与 Claude child 必须继续共享同一个 `CLAUDE_CONFIG_DIR` 视图
   - 不允许为 custom profile 创建 `<stateDir>/claude/profiles/<profileID>` 这类 profile-scoped runtime config dir
5. built-in `default` profile 语义
   - 不主动覆盖当前进程已有的 Claude 环境
   - 不创建 profile-scoped runtime config dir
   - 语义保持为“尽量沿用当前本机 Claude 默认配置”

6. 黑盒验证补充（2026-04-30）
   - 本机真实 Claude CLI 2.1.37 已验证：同一 `CLAUDE_CONFIG_DIR` 下，只切换 `ANTHROPIC_*` endpoint/key/model env，可以正常 `--resume` 同一个 session。
   - 同一个 session 一旦切到另一个 `CLAUDE_CONFIG_DIR`，会稳定返回 `No conversation found with session ID`。
   - 因此 profile-scoped `CLAUDE_CONFIG_DIR` 不是 Claude 技术限制，而是错误地把认证配置隔离和 session 存储隔离绑在一起。
   - 私有实测产物位置：`/data/dl/.codex-shared/kxn-codex-remote-feishu/private/claude-blackbox-runs/2026-04-30/profile-env-shared-session`。

7. launch-time MCP injection（2026-05-01）
   - Claude 运行时 MCP 注入发生在 wrapper 启动 Claude child 时，不发生在 daemon headless wrapper 参数层。
   - wrapper 可以给 Claude child 追加 `--mcp-config <runtime-config-file>`，但不允许追加 `--strict-mcp-config`，否则会覆盖用户已有 Claude MCP 配置。
   - runtime config 只承载 `codex_remote_feishu` HTTP MCP server，token 通过 `CODEX_REMOTE_FEISHU_MCP_BEARER` 环境变量传给 Claude child。
   - runtime config 文件只能包含 `Authorization: Bearer ${CODEX_REMOTE_FEISHU_MCP_BEARER}` 占位符，不写入 bearer token 明文。
   - 该注入不修改用户 `CLAUDE_CONFIG_DIR/.claude.json`，也不改变 profile/session 共享同一个 `CLAUDE_CONFIG_DIR` 的基线。
   - 本机真实 Claude CLI 2.1.37 已验证：`--mcp-config` 会追加动态 MCP，同时保留 `CLAUDE_CONFIG_DIR/.claude.json` 中已有 MCP；真实 tool service 通过该方式连接后，`system:init.mcp_servers` 中 `codex_remote_feishu` 为 `connected`。

这条 contract 故意停在 launch seam，不在 `#501` 吸收 surface 持有 `profileID`、busy/idle 切换或 workspace+profile snapshot；这些仍属于 `#502`。

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
3. resume handle 还必须绑定 Claude 的 workspace/cwd 作用域，而不是只保存 `session_id`

对 Codex：

1. 两条能力都仍可通过现有原生协议提供

#### 6.3.1 `#498` session catalog / history / resume contract matrix（2026-04-28）

基于本机黑盒 `BB-04` 与最新 `feidex`（`89421a55b1bf8a07c710f26afc040aa4c3bc39fd`）当前 `main`，`#498` 需要固定下面这组 contract：

1. session catalog 的真实数据源
   - Claude 会话目录来自 `$CLAUDE_CONFIG_DIR/projects`；若未设置，则回退 `~/.claude/projects`
   - 每个会话 transcript 是 `<session_id>.jsonl`
   - 当前 workspace 如果能命中自己的 project dir，则默认只扫该目录
   - 如果当前 workspace 的 project dir 不存在，则退化成“扫全部目录 + 严格按 entry 内 `cwd` 过滤”
2. session list 的 canonical 输出
   - Claude catalog plane 的主键仍是 `session_id`
   - canonical `threadId` 继续承载 Claude `session_id`
   - `threads.refresh` 真正落地后，输出仍应是 canonical `EventThreadsSnapshot{Threads []ThreadSnapshotRecord}`
   - 每条 snapshot 至少要带：
     - `threadId=session_id`
     - `name`
     - `preview`
     - `cwd`
     - `loaded/archived/state`
3. session list 的过滤与去重语义
   - 默认只展示当前 workspace/cwd 的 Claude session
   - `includeAll` 才允许跨 workspace 展示
   - 同一 `session_id` 需要去重，不能因 project dir 扫描重复出现
   - 排序主依据应是 transcript 文件更新时间，而不是 runtime 最近活跃事件
4. `thread.history.read` 的真实数据源
   - Claude history 不能从 live stream 反推，必须直接读本地 transcript JSONL
   - canonical 输出仍是 `EventThreadHistoryRead{ThreadHistory *ThreadHistoryRecord}`
   - turn grouping 需要按 transcript 内的用户 prompt 边界来聚合，而不是把每条 JSONL entry 当成一轮
   - sidechain-only 记录不能直接进主 history 视图
5. `thread.history.read` 的运行态补丁
   - transcript 最后一轮即使尚未看到明确终态，也可能是“当前运行中的 turn”
   - 是否把最新 turn 标成 `running`，要结合本地 runtime/session 是否仍有 active submission，而不是只看 transcript 最后一条
6. resume 的真实语义
   - `resume_by_thread_id=true`
   - `requires_cwd_for_resume=true`
   - 允许 same-cwd / same-workspace resume
   - 对已知 `cwd` 不匹配的 session，必须在 provider 启动前显式 reject，而不是盲目尝试 resume
   - submission 启动时若旧 `session_id` 失效，可以退回 fresh session；这属于 runtime 行为，不属于 catalog 成功信号
7. capability 语义必须拆开
   - `SessionCatalog=true` 表示 backend 拥有 provider-local catalog/history plane
   - `ThreadsRefresh=true` 表示 runtime host 已经能把 canonical `threads.refresh` 正确桥接到这条 plane，并返回 `threads.snapshot`
   - 因此在 `#498` host bridge 落地后，Claude 应真实声明 `SessionCatalog=true` 与 `ThreadsRefresh=true`；bridge 落地前则只能先宣称 `SessionCatalog=true`
8. 本单第一版的 ownership 边界
   - pending fork materialization（fork 后直到第一条消息前 `session_id` 为空）是真实现象，但不进入 `#498` 第一版实现闭包
   - startup auto-recovery / hidden resume 也不要求在 `#498` 第一版落地
   - `#498` 第一版只拥有：
     - catalog/history data source
     - canonical output contract
     - `session_id + cwd/workspace` resume 语义
     - 与 `#492` backend partition 的持久层对齐

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
- `assistant tool_use` / `user.tool_result`
- 后续可扩的 plan-mode confirm

都在 Claude adapter 内部分开处理

#### `#494` 当前收口合同（2026-04-28）

这轮先把 provider-specific reply contract 固定到 canonical request carrier 上，而不改 Codex 既有 payload 外形：

1. canonical request 额外携带：
   - `bridgeKind`
   - `semanticKind`
   - `interruptOnDecline`
2. Claude `can_use_tool`
   - 归类为 `approval_can_use_tool`
   - `bridgeKind=can_use_tool`
3. MCP elicitation
   - 继续沿用当前 `action/content/_meta` canonical payload
   - 但显式标成 `bridgeKind=elicitation`
4. Claude plan confirmation
   - 归类为 `plan_confirmation`
   - `bridgeKind=plan_confirmation`
   - reject/decline 必须同时带 `interruptOnDecline=true`
   - 但 plan 正文不能依赖 `control_request.request.input`
   - runtime 需要单独保留 `assistant` 已输出 plan 文本与 `tool_use_result.filePath` fallback
5. Codex translator 继续只消费既有 approval / user-input / elicitation result shape，不因 Claude contract 字段而改现有 reply 语义。

#### 非回归要求

1. 现有 approval / request_user_input / permission approval 行为在 Codex 下不变。
2. 不允许为了兼容 Claude，去修改现有 canonical request 外形使 Codex 路径回归。

#### 6.4.1 `#494` 需要固定的多-carrier 规则

黑盒已经证明，Claude 交互工具桥接最少要同时消费下面四类 carrier：

1. `assistant tool_use`
   - 负责暴露工具名、tool-use id、问题列表或进入 plan 模式的事实
2. `control_request(can_use_tool)`
   - 负责 allow/deny/feedback 的交互回写
3. `user.tool_result`
   - 负责交互结束后的本地补充信息，例如 `tool_use_result.filePath`
4. `result`
   - 负责 turn 终态，但不能单独代表交互工具的完整结果

因此 `#494` 的 request bridge 不能再按“收到一个 request card，回答它，就算完成”来定义；它必须明确：

1. 事件如何关联同一条 Claude interaction
2. plan/question 内容从哪一层拿
3. decline 是否需要 interrupt
4. allow 后是否要读取 plan file fallback

### 6.5 基座五：backend-aware state partition

#### 当前状态（2026-04-28）

`#492` 已经落地了这组最小 state partition：

1. surface 持久层已有独立 `Backend` carrier。
2. workspace defaults 已按 `workspace + backend` 分区。
3. surface resume 已有 backend 维度，恢复只消费同 backend 目标。
4. `/mode codex|claude|vscode` 已与这套 state seam 对齐。

仍未完成的部分：

1. 更广义的 thread/session 持久 plane 还没有完全 backend-neutral。
2. daemon startup 挂接的持久 thread catalog 仍是 Codex-shaped。

#### 设计要求

后续 remaining work 仍应保持下面这些约束：

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

#### 当前状态（2026-04-28）

`#492` 已经收口了 `/mode` 的底层状态语义与 headless backend 隔离：

1. `/mode codex`
2. `/mode claude`
3. `/mode vscode`
4. `/mode normal` 兼容为 `/mode codex`

但这还不是 command catalog 层面的完工。当前真正剩下的问题是：

1. display / help / matcher / local registry 仍未完全 backend-aware。
2. binding / registry / parser 仍可能把 variant/backend 决策吞回 `FamilyID` 或 `family.default`。
3. mode 已有三态，但可见命令集合和策略矩阵还没有完全跟上。

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

#### 非回归要求

1. 在用户未显式切到 Claude 前，当前 headless 默认仍表现为 `codex` mode。
2. 只有显式 `/mode claude`，或 surface 恢复回原来的 `claude` headless mode 时，才进入 Claude 命令集与 Claude 菜单投影。

### 6.7 基座七：workspace config 按 backend 分区

#### 当前状态（2026-04-28）

`#492` 已把 workspace default config 升级成 backend-aware 存储：

1. `workspace -> backend -> config`
2. `model`
3. `reasoning effort`
4. `access / permission mode`

当前剩余工作不在存储层，而在可见命令层：

1. 不同 backend 下的 help / menu / matcher / local registry 还要继续收口。
2. Claude 真正可见后，config 入口需要继续跟 command support profile 保持一致，避免 visible、hidden/reject 与实际 dispatch 漂移。

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
6. Claude 计划确认这类即时交互优先走同步 callback replace
   - 不要把“按钮已点下”到“卡片状态已切换”的关键路径依赖在后续异步 patch 上
7. 工具文件汇总的内部 identity 使用稳定全路径
   - 展示层可以只显示 basename
   - 但去重、聚合、回放不能只按文件名判断

#### 非回归要求

1. Codex 现有 structured text -> projector 渲染边界保持不变。
2. 任何 Claude 接入改造都不能把动态外部文本回流到上游 markdown 拼接路径。
3. 现有卡片 UI 状态机（replace/append、request 流程、final 流程）不因 provider 切换而改变语义。
4. 需要新增契约测试覆盖：
   - Claude 语义映射后仍满足 structured/plain_text 边界
   - 同一 canonical payload 在 Codex/Claude 下投影行为一致（除明确声明的能力差异）
   - headless 下不出现未经策略批准的过程噪音外溢

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
4. `#498` 落地后的当前代码基线里，Claude 默认 capability 应真实宣称 `sessionCatalog=true` 与 `threadsRefresh=true`；这两个 bit 仍然不能在未接 host bridge 时被提前混为一谈
5. `turn.steer` 仍然不应出现在 capability declaration 里；当前文本与本地图片 steer 只是 command-profile 层的 approximation，不是 native capability

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
   - `assistant tool_use <-> control_request <-> user.tool_result` 关联
4. plan materialization helper
   - 先消费 assistant 已输出 plan 文本
   - 再消费 `tool_use_result.filePath`
   - 最后才回退 Claude 本地 `.claude/plans/*.md` 文件

### 7.4 Canonical 映射规则

需要固定的映射包括：

1. `threadId` 在 canonical 层继续承载 Claude `session_id`
2. `turn.started` 来自 Claude 进入 running 边界
3. `turn.completed` 由 Claude 最终结果、runtime 退出时的 completed reconciliation，以及 idle/running 边界共同收口
4. `threads.refresh` / `thread.history.read` 不从 live stream 提供
5. `turn.steer`
   - native capability 继续视为 unsupported
   - 但 command profile 可为明确批准的文本补充入口开放 approximation：向当前 active turn 追加一条 Claude `user` frame
   - accepted 语义只表示“本地 runtime 已完成 dispatch”，不表示 Claude 提供了独立 steer ack
6. `AskUserQuestion` / `ExitPlanMode`
   - 先观察 assistant `tool_use`
   - 再处理 `control_request(can_use_tool)`
   - 最后消费 `user.tool_result`
7. plan confirmation
   - 计划正文优先来自 assistant 文本
   - `control_request.request.input` 只当审批 carrier，不当 plan body carrier
   - allow 后的 `tool_use_result.filePath` 作为 plan file fallback 线索
8. `result.subtype` 与 `is_error`
   - 不能只靠 `is_error` 一位判断 turn 最终状态

#### 7.4.1 `#497` native -> canonical mapping matrix

| Claude native carrier | 本地语义 | canonical 承接位 | 当前结论 |
| --- | --- | --- | --- |
| `system:init` / `system:status` | 会话 ready、`session_id`、模型、cwd、permission mode | runtime host 内部 session bind + `config.observed(thread)` | 当前通过标准 `EventConfigObserved` 回填 Claude thread observed access/plan，不再只停留在 wrapper-local `permissionMode` 字段 |
| `stream_event.message_start` | 新一轮 assistant turn 真正进入 running | `turn.started` | 作为 Claude live turn 的主起点 |
| `assistant.text` | assistant 正文输出 | `item.started / item.delta / item.completed` with `itemKind=agent_message` | 现有文本 item 语义可直接复用 |
| `assistant.thinking` | provider-native reasoning / hidden chain-of-thought side channel | `item.delta` with `itemKind=reasoning_summary`；前台保留过滤后的 raw thinking，adapter 仅窄清洗已知系统 info block | pre-MVP 不需要新增公开 reasoning carrier，但需要流式 side-channel 可见边界抑制 |
| `assistant.tool_use`（外部工具） | 工具调用开始，已拿到稳定 `tool_use_id + name + input` | `item.started` with typed `itemKind` selected by tool family (`command_execution` / `web_search` / `file_change` / `dynamic_tool_call`) | 当前已知强语义工具直接进 typed owner，其余仍允许回落 `dynamic_tool_call` |
| `user.tool_result`（外部工具） | 工具执行完成，携带 stdout/stderr/error/image/interrupt 等真实结果 | `item.completed` with the same typed `itemKind` + structured metadata | 这是工具完成的主 carrier，不能再把 assistant `tool_use` 误当 completed |
| `control_request(can_use_tool)`（外部工具） | 外部审批点 | `request.started` with `type=approval` + `semanticKind=approval_can_use_tool` | `#494` 已有 canonical request contract，可直接复用 |
| `assistant.tool_use`（`AskUserQuestion` / `ExitPlanMode` / `EnterPlanMode`） | 内部交互工具种子，提供 `tool_use_id` 与补充上下文 | adapter-local correlation state | 这类 internal tool 不应直接投影成通用 `dynamic_tool_call` 过程噪音 |
| `control_request(can_use_tool + AskUserQuestion)` | 真正可外部响应的提问 request | `request.started` with `type=request_user_input` + `semanticKind=request_user_input` | 问题主来源可直接用 `control_request.input.questions` |
| `user.tool_result`（`AskUserQuestion`） | 用户答案回流，带 questions/answers 回显 | `request.resolved`；answers 进 request metadata/history sidecar | request 关闭应绑定匹配的 `tool_use_id` / request correlation，不应只靠本地发送成功 |
| assistant 先输出的计划正文 | `ExitPlanMode` 前的真实 plan body 主来源 | `item.delta / item.completed` with `itemKind=plan`，并作为 `plan_confirmation` body source | 现有 `plan` item 可承接自由文本计划，不要求先变成结构化 steps |
| `control_request(can_use_tool + ExitPlanMode)` | 真正可外部响应的计划确认 request | `request.started` with `type=approval` + `semanticKind=plan_confirmation` + `interruptOnDecline=true` | `control_request.input` 只当审批载体，不当计划正文 |
| `user.tool_result`（`ExitPlanMode`） | allow/deny 结果与 `tool_use_result.filePath` sidecar | `request.resolved` + plan file fallback sidecar | `filePath` 是高精度 fallback，优先级高于“去猜最新 plan 文件”；当 `decision=accept` 时，本地 surface 还应在这条 resolved 闭环上同步清掉旧 `PlanMode` override |
| `user.tool_result`（`EnterPlanMode`） | mode 进入确认与提示文本 | adapter-local mode transition / history sidecar | 当前无需新增上层 request 或 item |
| `result` | native turn 终态、usage、最终 result text | `turn.completed` + `thread.token_usage.updated` | `status` 需要综合 `subtype`、`is_error`、interrupt state 与 final text materialization |
| `stdout EOF / child exit without result` | native turn 缺失终态 | synthetic `turn.completed` | 这是 `#495` runtime-exit reconciliation 的职责，不是 Claude native 自带 carrier |

#### 7.4.2 `#497` 当前闭包判断

基于上面的矩阵，当前结论固定为：

1. pre-MVP live transport **不需要新增 public canonical carrier**。
   - 现有 `turn.started / turn.completed / item.* / request.* / thread.token_usage.updated / plan item` 已能承接 Claude live transport 的第一版语义。
2. 真正缺的是 provider-local correlation state。
   - 最少需要按 `tool_use_id` 关联 `assistant.tool_use -> control_request -> user.tool_result -> result`。
   - 这是一层 `internal/adapter/claude/**` 实现问题，不是上层协议字段缺失。
3. `#497` 的第一版闭包已经从“全量 `dynamic_tool_call`”推进到“typed owner + narrow fallback”。
   - 已明确语义的工具优先直接进入现有 canonical owner，例如 `Bash -> command_execution`、`Web* -> web_search`、`Edit/Write/NotebookEdit -> file_change`。
   - 只有尚未形成稳定产品语义的工具才继续保留 `dynamic_tool_call` fallback。
4. 不再额外拆“交互工具桥接/plan materialization”子单。
   - `#494` 已拥有 canonical request/reply contract。
   - 剩余工作是 Claude native 多 carrier 的相关性与 materialization 规则，属于 `#497` 的 live transport mapper closure。
   - 再拆一张单会把同一条 `tool_use_id` 相关状态拆散到多个 worker，验证面反而更差。

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
   - 不做 fake native support
   - 但可为 reply auto steer 与 `/steerall` 这类已批准入口开放文本与本地图片 approximation
   - 输入必须并入当前 active turn，不能退化成 `interrupt + prompt.send`
2. `/compact`
   - 如果后续验证 Claude CLI 侧命令稳定，可作为 passthrough 候选；但不计入 capability support
3. 某些 session 管理入口
   - 可以允许 pending-state approximation，但只能在 command catalog 中显式声明
4. plan/question 这类 Claude 原生交互工具
   - 走 request bridge 适配，不算 approximation，也不暴露成新的上层 canonical 能力

### 7.6 `#496` Claude MVP 产品决议（2026-04-29 refresh）

这份文档现在把 `#496` 的 Claude MVP 产品边界固定为下面这组规则；后续实现、帮助/菜单、target picker、恢复状态机与坏态逃生语义都以这份决议为准，而不是继续沿用此前那轮 dev-visible Claude 暴露面。

#### 7.6.1 命令面

| family / 入口 | Claude 策略 | help/menu 可见性 | 当前应否允许直接派发 | 说明 |
| --- | --- | --- | --- | --- |
| `/stop` `/status` `/history` `/reasoning` `/access` `/claudeprofile` `/verbose` `/mode` `/help` `/menu` `/debug` `/upgrade` | native | visible | allow | 仍属于 Claude MVP 已批准的 native 或纯本地产品入口。 |
| `/model` | reject | hidden | reject | Claude 模型只在 Claude profile 内配置，飞书会话不开放临时模型覆盖。 |
| `/sendfile` | native | visible | allow | 飞书/本地侧文件投递能力，不依赖 Claude runtime/backend；可按既有 sendfile picker 与后台发送流程开放。 |
| `/detach` | native | visible | allow | Claude MVP 正式开放；它不再只是隐藏逃生口，而是统一的脱困 / 解除接管入口。 |
| `/workspace detach` | native | hidden | allow | 仅保留兼容 alias；Claude 的主展示入口是 `/detach`。 |
| `/compact` | passthrough | hidden | reject for now | 仍只保留成后续 runtime host 的 passthrough 候选。 |
| `/new` `/list` `/use` | approximation | visible | allow | Claude MVP 的工作会话主链；继续复用现有产品壳，但底层改走 backend-aware session catalog / route contract。 |
| `/review` `/patch` | approximation | hidden | reject | 当前不纳入 Claude MVP；在 detached review / turn patch 的 runtime contract 补齐前，不对用户暴露。 |
| `/workspace*` `/useall` | approximation | hidden | reject | Claude MVP 不开放工作区父页或跨工作区总览。 |
| `/steerall` | approximation | visible | allow | 文本与本地图片补充可并入当前 active turn；远程图片与 document 输入仍显式拒绝，并恢复原 queue/staged 状态。 |
| `/plan` | native | visible | allow | 当前通过 surface-level `PlanMode` 与 Claude `set_permission_mode(plan/default)` 动态权限通道生效，可用于调试与后续 turn 的 plan mode 切换。 |
| `/follow` `/cron` `/vscode migrate` `/autowhip` `/autocontinue` | reject | hidden | reject | 不在当前 Claude MVP 范围内。 |

对 help/menu 的显式投影也一并固定为：

1. `current_work` 只保留 `/stop`、`/steerall`、`/new`、`/status`、`/detach`
2. `switch_target` 只保留 `/list`、`/use`
3. `send_settings` 继续保留 `/reasoning`、`/access`、`/plan`、`/verbose`、`/claudeprofile`
4. `common_tools` 当前保留 `/history`、`/sendfile`

#### 7.6.2 `/detach` 的产品语义

Claude MVP 下，`/detach` 是状态机的统一逃生原语，而不是“调试时才知道的隐藏命令”。

固定语义：

1. 能立即 detach 的状态，直接 detach
2. 需要 interrupt / cancel / abandon 才能退出来的状态，不直接拒绝，而是进入明确的 cancelling / abandoning / detaching
3. 最终一定回到可恢复的 detached 状态
4. 不允许留下“不能 detach、不能切 mode、也不能继续输入”的夹层坏态

#### 7.6.3 `/list` `/use` / target picker 过滤

`claude` headless mode 的 workspace / session 候选当前只按 backend 过滤：

1. 只区分 `codex` 与 `claude`
2. 不按 `ClaudeProfileID` 过滤 workspace / session 候选
3. `ClaudeProfileID` 继续只是 launch / snapshot / surface runtime 参数，不再作为可见性过滤条件

#### 7.6.4 重连 / 恢复时的 mode 语义

除 `vscode` 这类特殊 mode 外，surface 重连后默认应回到断开前的 mode：

1. 原来是 headless 下的 `codex` mode，就回 headless 下的 `codex` mode
2. 原来是 headless 下的 `claude` mode，就回 headless 下的 `claude` mode
3. `claude` 恢复链不得再偷偷掉回 headless 下的 `codex` 旧语义

对于 Claude，surface 重连后的恢复语义现在与 Codex 对齐：workspace-owned route 继续理解为 **workspace 级 prepare / reattach / fresh-start intent**，而 `ResumeHeadless=true` 的 exact-thread target 则继续理解为 **managed headless exact-thread continuation**。两边 remaining 差异只在 runtime-native resume mechanics（Claude 最终落到 `--resume <session_id>`），而不再是 daemon/orchestrator 的状态机结构不同。

#### 7.6.5 headless 下的 `Claude <-> Codex` 互切

headless 下 `Claude` 和 `Codex` 的互切，产品语义固定为“同一工作区切 backend”，而不是“切走工作区”。

固定规则：

1. 切换锚点是当前 **工作区目录**
2. 不使用当前 thread cwd 作为切换真值；若 cwd 与 workspace 不一致，以 workspace 目录为准
3. 切到目标 backend 后，先用该工作区目录查找目标 backend 是否已有对应 workspace
4. 若已有对应 workspace，则直接接入
5. 若没有，则以该工作区目录创建一个新的目标 backend workspace
6. `Claude` 与 `Codex` 的 session / thread 不跨 backend 继承
7. 切过去后允许出现“已有工作区，但还没有会话”的状态
8. 默认后续语义应偏向 **新会话待命**：
   - 用户可以 `/use` 选已有会话
   - 也可以直接发送文本启动新会话
   - 默认 route 应按 target backend 的 `new_thread_ready` 语义收口
9. 这套规则只适用于 headless 下的 `Claude <-> Codex`；`vscode` 仍是例外

#### 7.6.6 参数与 profile

backend 互切时，`reasoning / access / plan / profile` 不要求强保留 live 值。

优先级固定为：

1. 工作区连续性优先
2. backend 切换优先
3. 会话不继承
4. live 参数可按各 backend 自己的快照语义重新恢复

当前已落地的实现补充：

1. Claude `/reasoning` 继续保留 Codex 风格的 next-turn 语义：当前 turn 与已冻结 queue/autocontinue/review apply 不回改，新入队 turn 按当时 surface override 冻结 reasoning；Claude 可选档位固定为 `low / medium / high / max / clear`，不使用 Codex 的 `xhigh`。
2. 有效 Claude reasoning 的运行时优先级固定为：飞书 surface override / frozen override > Claude Profile 默认 reasoning > 系统默认。`/reasoning clear` 只清掉飞书临时覆盖；如果当前 profile 配了默认 reasoning，后续启动、恢复和 dispatch restart 会回落到 profile 默认值。
3. Claude headless 真正 dispatch 前，会用该 frozen/effective reasoning 扩展出 `HeadlessLaunchContract{Backend, ClaudeProfileID, ClaudeReasoningEffort}`，并与 wrapper hello 上报的 observed runtime contract 比较。
4. 若合同不一致，orchestrator 会统一走 `prompt_dispatch_restart`：写入 `PendingHeadless`、daemon `kill + start headless`、实例重新 attach 后自动继续原 dispatch。
5. `workspace+profile` 快照只保存飞书临时 `reasoning` 覆盖，不保存 profile 默认值，也不保存 `access / plan`；surface resume 也不跨 daemon 恢复 Claude plan；`/reasoning clear` 会同步删除空快照；fresh workspace 与 concrete thread restore 都必须先恢复临时 reasoning 覆盖，再用 profile fallback 生成 `PendingHeadless` 与 daemon start command。
6. `/access` 与 `/plan` 仍只走动态 `set_permission_mode` 通道，不被并入这条 restart 合同。
7. `/model` 不在 Claude 飞书命令面里：Claude 模型只从 Claude profile 注入，飞书侧不支持临时模型覆盖，也不把 Codex 默认模型投影成 Claude 当前模型。

### 7.7 `#494` final-output 终态合同（2026-04-28）

为避免 Claude runtime 在 error / completion 边界上再次留下歧义，这轮先固定 turn final-output materialization 规则：

1. `completed` 且无 error/problem：
   - 正常 materialize final output。
2. `completed` 但已存在完整 assistant text，且随后附带 error/problem：
   - 仍要 materialize final output。
3. `failed` 且已存在完整 assistant text：
   - 仍要 materialize final output，同时保留失败 notice / queue failed 语义。
4. `interrupted`
   - 不得把缓冲文本标成 final output；只能按 non-final flush。

## 8. 分阶段实施

### 阶段 A：基座接线，但不引入 Claude 可见行为（已完成）

目标：

1. hello/provider/capabilities 进入状态与派发决策
2. `onHello()` capability-aware
3. state 和 workspace defaults 引入 backend 维度

本阶段的用户侧预期：

- 现有 Codex 行为不变

当前落地情况：

1. `#463` 已完成 hello/provider/capabilities 与 `onHello()` gate。
2. `#492` 已完成 state partition、surface resume backend 与 `/mode codex|claude|vscode` 底层语义。

### 阶段 B：把 Codex 提取成 provider runtime，但保持行为完全一致

目标：

1. wrapper 改成 backend runtime host
2. 现有 `codex.Translator` 下沉到 Codex runtime
3. 事件、ack、错误、helper/internal annotation 与当前保持一致

本阶段的用户侧预期：

- 仍然只有 Codex 行为
- 没有新增 Claude 产品入口

当前落地情况（2026-04-28）：

1. wrapper 已完成 `backendRuntime` seam，并把 Codex translator 下沉到 `codexBackendRuntime`。
2. `claude-app-server` 入口与 `claudeSkeletonRuntime` 已进入仓库，保持 dark-launch / dev-only，不接受实际 Claude 命令执行。
3. runtime-exit reconciliation 已闭合：
   - child 在 final output 后退出会补发 synthetic `turn.completed`
   - child 在 interrupt ack 后退出会补发 `interrupted`
   - relay outbox 会在 wrapper close 前显式排空，避免 synthetic 终态或 shutdown ack 因过早关闭而丢失
4. capability truthfulness 已闭合：
   - wrapper hello 现在显式上报 `capabilitiesDeclared=true`
   - Claude skeleton 的零 capability 不再被 daemon/catalog context 按 backend 默认值补齐
5. 当前验证面已绿：
   - `go test ./internal/adapter/relayws ./internal/adapter/codex ./internal/app/wrapper ./internal/app/daemon ./internal/core/control ./internal/core/orchestrator`

阶段 B 结论：

1. `#495` 已完成其 seam-only closure。
2. 下一 ready worker 切换到 `#497`。

### 阶段 C：实现 Claude runtime skeleton，先 dark launch

目标：

1. Claude child 启动与关闭
2. live transport 打通
3. session catalog/history 读取打通
4. request bridge 打通

本阶段的用户侧预期：

- Claude 仍可隐藏
- Codex 不回归

当前判断：

1. 从研究 closure 看，阶段 C 已经没有协议方向上的大不确定性。
2. 但开始真正做阶段 C 之前，仍应先把阶段 B 相关 contract 和 `#494` 的 request/final-output contract 固定好。

### 阶段 D：开启 Claude headless-mode MVP（已完成）

目标：

1. `/mode claude`
2. Claude `prompt.send`
3. Claude `turn.interrupt`
4. Claude `request.respond`
5. Claude `threads.refresh`
6. Claude `thread.history.read`
7. 明确 `turn.steer` native capability unsupported，并只为批准入口保留后续 approximation 空间

本阶段的用户侧预期：

- Claude 是可见 backend
- 不支持能力有明确提示

当前结果：

1. `/mode claude` 已作为 dev 环境可见入口落地。
2. 普通文本、`/stop`、request/respond 主链、`/new`、`/list`、`/use` 已进入 Claude visible MVP。
3. `turn.steer` 继续不宣称 native capability；但 reply auto steer 与 `/steerall` 现已走 approximation，可把文本与本地图片补充并入当前 active turn。
4. 上述结果只证明 dev-visible Claude 主链已经可接通；不等于 2026-04-29 重新拍板后的最终 MVP 暴露面。按最新产品决议，`/detach` 应升级为 visible + allow，而 `/review` / `/bendtomywill` 应回退为 hidden + reject，直到 runtime contract 补齐。

### 阶段 E：补齐 command catalog / help / menu / workspace config（已完成）

目标：

1. backend-aware help/menu/filter
2. Claude-specific model / permission config 入口
3. backend-aware `/list` `/use` `/new`

本阶段的用户侧预期：

- Claude 产品闭环更完整
- Codex 路径仍无回归

当前结果：

1. backend-aware help/menu/filter 已落地。
2. Claude `/list` `/use` `/new` 的可见性、派发策略与 backend-aware target picker 已落地。
3. `workspace*` 与 `/useall` 继续 hidden + reject，维持当前 MVP 边界。
4. 下一轮实现还需要按 2026-04-29 的产品决议进一步收口：
   - `/detach` 进入 Claude visible MVP
   - `/review` / `/bendtomywill` 退出 Claude visible MVP
   - `target picker` 与 `/list` `/use` 不再按 profile 过滤
   - headless 下的 `Claude <-> Codex` 切换/恢复按工作区目录保连续性

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
6. 文本与本地图片 steer approximation 成功并入当前 active turn；远程图片与 document steer 仍显式 reject，且不污染 queue / request state

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
4. Claude 多模态图片输入的完整承诺（当前已接通 `prompt.send` 与 `turn.steer` approximation 的本地图片输入；remote image 与更完整的 document/image 产品承诺仍属后续）
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

### 12.1 2026-04-29 当前执行快照

当前阶段：

- Claude pre-MVP 的技术基座实现已推进到 `#498` 收尾阶段
- `#495`、`#497`、`#498` 均已完成本地实现并通过验证
- `#496` 的 Claude MVP 产品边界已在 2026-04-29 刷新并拍板，下一步不再继续抽象讨论，而是按这版边界收口实现

已完成：

1. 本机真实 Claude CLI / relay env 已验证可用。
2. `AskUserQuestion` / `ExitPlanMode` / same-cwd resume / interrupt / final-output 关键样本已采到。
3. `#493` 已完成并关闭：`#369` 前置 catalog seam repair 已真实落地，不再是当前 blocker。
4. `#494` 已完成并关闭：命令策略矩阵、request bridge canonical contract 与 final-output contract 已落地到当前 `master`。
5. 本文已吸收“plan 不能只从 control bridge 拿”的修正，并补齐 `#497` native -> canonical mapping matrix。
6. 本文已补齐 `#498` session catalog / history / resume contract matrix，并明确：
   - Claude catalog/history 是 provider-local transcript/meta plane
   - `SessionCatalog` 与 `ThreadsRefresh` 不是同一个 capability
   - pending fork materialization / startup auto-recovery 不进入 `#498` 第一版实现闭包
7. 当前工作区已有局部 `#495` prework：
   - backend runtime seam
   - claude skeleton entry
   - runtime-exit reconciliation tests
8. `#495` 已完成并验证：
   - runtime host seam / Codex runtime 下沉已收口
   - Claude skeleton shutdown ack 已收口
   - runtime-exit reconciliation 已稳定送达 relay
   - capability truthfulness 已通过 `capabilitiesDeclared` 闭合
9. `#497` 已完成并验证：
   - Claude live transport semantic mapper 已把 `stream-json` 主链路归一化到 canonical `turn/item/request/final-output`
   - request / final-output / runtime-exit reconciliation 的主链已闭合
10. `#498` 已完成并验证：
   - Claude provider-local session catalog / history / resume plane 已落地
   - `threads.refresh` / `thread.history.read` 已桥接到本地 transcript/meta scan
   - Claude 默认 capability 已真实提升到 `SessionCatalog=true` + `ThreadsRefresh=true`
11. `#496` 的 MVP 产品边界已完成这一轮拍板，并固定为：
   - `/detach` 进入 Claude visible MVP
   - `/review` / `/bendtomywill` 暂不纳入
   - `/list` `/use` / target picker 只按 backend 过滤
   - headless 下的 `Claude <-> Codex` 按工作区目录保连续性切 backend

还差的关键收口项：

1. `#498`
   - 独立 verifier
   - parent roll-up
   - commit / push / finish / close
2. `#496`
   - 按最新产品决议更新实现
   - 对齐 help/menu、target picker、resume/detach、Claude<->Codex workspace-preserving 切换语义

下一步：

1. 完成 `#498` 的 verifier、parent roll-up 与关单收尾
2. 以本文第 7.6 节为基线，先同步产品文档，再进入 `#496` 的实现收口
3. Claude MVP 的后续实现以 `/detach`、backend-only target filtering、workspace-preserving backend switch/resume 为主线推进

恢复步骤：

1. 先读本文第 4.5、6.4.1、7.4、12.1 节
2. 再读 `docs/draft/claude-cli-blackbox-findings-2026-04-28.md`
3. 若继续 `#498` close-out，优先检查 verifier 结果、父单 `#185` 回卷状态与 publish 状态
4. 若继续 `#496`，直接以本文第 7.6 节的产品决议为准，不再重复讨论同一轮 MVP 边界

### 12.2 2026-04-28 回卷审计结论

本节用于回答一个单独问题：在 2026-04-28 补完黑盒研究之后，前面已经写出来的代码里，有哪些需要高层回滚，哪些不用。

结论先行：

1. 当前 `master` 上已经合入的 `#492/#493/#494`，没有发现需要高层回滚的部分。
2. 当前工作区里的 `#495` 只是“方向可保留、实现不可直接继续合并”的 prework，需要按最新研究结论重收一次。

证据与判断：

1. `#495` 本地 prework 还不是 merge-ready。
   - `go test ./internal/app/wrapper` 当前直接失败，红在 `TestWrapperReconcilesCompletedTurnWhenChildExitsAfterFinalOutput`、`TestWrapperReconcilesFailedTurnWhenChildExitsAfterFinalOutputError`、`TestWrapperReconcilesInterruptedTurnWhenChildExitsAfterInterruptAck`
   - 这三条测试正对应最新研究里最关键的 seam：provider 在 final output 或 interrupt ack 之后退出，但没有原生 `turn.completed` carrier 时，wrapper 必须补发稳定终态
   - 当前失败不是“状态判错一点点”，而是 relay 根本没有收到预期的 synthetic `turn.completed`
2. 即使补上发送问题，现有 reconciliation heuristic 也偏粗。
   - 当前 `runtime_turn_tracker.go` 以 `AnyOutputObserved` 作为 `completed` 判定基础
   - 它把 `item.started`、`item.delta`、`item.completed`、`request.started`、`turn.plan.updated` 都算作“出现过输出”
   - 这弱于最新研究结论，因为 Claude 后续实现需要区分 request / partial / plan carrier 和真正可物化的 final turn materialization
3. capability seam 已经被最新研究推翻了“默认补齐即可”的假设。
   - 当前 `internal/app/wrapper/backend_runtime.go` 里的 `claudeSkeletonRuntime` 返回零 capability
   - 但 `internal/core/agentproto/backend.go` 与 `internal/app/daemon/app_ingress.go` 仍会按 Claude backend 默认值补齐 `RequestRespond`、`SessionCatalog`、`ResumeByThreadID`、`RequiresCWDForResume`
   - 最新研究已经明确：`SessionCatalog` 不是 `ThreadsRefresh` 的同义词，而且 capability 必须真实反映 host bridge 是否已落地，所以这一段不能原样带进后续实现
4. `#494` 当前合同仍然成立。
   - request bridge 对 `plan_confirmation` 的 `decline -> interrupt` 合同仍与黑盒结论一致
   - `#553` 之后，Claude command support profile 已把 `/new`、`/list`、`/use` 升成 visible + allow approximation；`/sendfile` 因为属于飞书/本地侧文件投递，不依赖 Claude backend，已作为 visible + allow native 入口开放；`/plan` 现在也作为 visible + allow native 入口开放，沿用 surface-level `PlanMode` 与 Claude `set_permission_mode` 的动态权限通道；`workspace*`、`/useall` 则作为 hidden + allow 兼容入口继续复用同一工作区 / 会话壳，避免 target picker 子流程依赖隐式 UI intent 绕过
5. `#492/#493` 的主体方向也仍成立。
   - backend-aware state partition、surface resume carrier、catalog/contextual command seam 目前没有发现被后续 Claude 研究推翻
   - 相关测试面当前仍是绿的：`go test ./internal/core/control ./internal/core/orchestrator ./internal/app/daemon`
6. 上述回卷红点现已在 `#495` 内真实收口。
   - synthetic `turn.completed` 现在会在 wrapper close 前等待 relay outbox 排空
   - `claudeSkeletonRuntime` 的 `/process exit` ack 现在不会因过早 `Close()` 丢失
   - capability truthfulness 通过 `capabilitiesDeclared` 进入 relay hello / daemon / catalog context 主链

因此当前恢复契约应改成：

1. `#495` 的 seam-only 实现已经可以保留并继续作为基线：
   - backend runtime interface
   - mockcodex exit fixtures
   - runtime-exit coverage surface
2. `#492/#493/#494/#495` 继续作为稳定基线，不做高层回滚；后续只在 `#497/#498` 上向前修正

## 13. 2026-04-28 下一步两种推进方案

基于当前 `master`、`#369` 的最新审计，以及最新 `feidex` 复核，下一步实际上有两种可执行路线。

### 13.1 方案 A：先补一小包基座，再开可见 Claude

适用前提：

1. 当前目标仍然是“尽量不改现有产品形态”。
2. 可以接受先做一轮对用户不太显性的基础工程。

这条路不建议继续无限扩基座；应把预备范围严格限制在下面 5 项：

1. provider runtime seam
2. backend-aware state partition
3. command catalog / binding / route resolver 从 `ProductMode + FamilyID` 升到 `CatalogContext + VariantID`
4. Claude request bridge 语义
   - `can_use_tool`
   - `elicitation`
   - plan reject = deny + interrupt
5. completed reconciliation / final output semantics
   - partial text + error
   - interrupt 后 active-op 清理

建议选项：

- `A1 保守版`
  - 完成上述 5 项后仍保持 Claude dark launch
  - 不暴露 `/mode claude`
  - 适合优先把 Codex 路径和基座边界完全稳住
- `A2 平衡版`
  - 完成上述 5 项后，立即开放 dev-only `/mode claude`
  - 只接 `prompt.send`、`turn.interrupt`、`request.respond`、`threads.refresh`、`thread.history.read`、`/model`、`/effort`、`/workspace permissions`
  - 其余能力继续 hidden / reject

优点：

1. 最能避免最新 `feidex` 暴露出来的 final output、plan reject、steer/interrupt、workspace matcher 类问题在本仓库重复出现。
2. 不需要在用户可见入口上频繁回滚命令语义。
3. `#185` 与 `#369` 的 seam 可以一次收口，而不是接入后再二次回收。

代价：

1. 第一阶段用户看不到太多新入口。
2. 需要先动状态、catalog、request、runtime 边界。

### 13.2 方案 B：先接一个最小可见 Claude MVP，再边做边补

适用前提：

1. 团队更需要尽快拿到真实 Claude 产品反馈。
2. 能接受第一版明确带着较多限制和短期技术债。

这条路不是“零基座”路径。最低前置仍要先补：

1. provider identity / capabilities
2. 最小 backend state partition
3. 最小 command gate
   - 至少保证 `/mode claude` 不会漏出错误命令
4. request respond / interrupt bridge

建议选项：

- `B1 内测 MVP`
  - 不开放 `/mode claude`
  - 通过显式绑定 Claude 实例或隐藏入口使用
  - 先覆盖纯 prompt/send、interrupt、approval、history
  - 适合工程内测，不适合普通用户
- `B2 用户可见 MVP`
  - 开放 `/mode claude`
  - 只承诺：
    - 新会话 / 恢复会话
    - 文本发送
    - interrupt
    - approval / user input / plan confirm
    - session list / history
    - `/model`、`/effort`、`/workspace permissions`
  - 明确不承诺：
    - steer approximation
    - vscode mode
    - review / skills
    - workspace sandbox / policy
    - compact parity

优点：

1. 更快拿到真实 Claude 交互数据。
2. 能更早验证 `/mode claude`、会话恢复、request bridge 的产品手感。

代价：

1. 最近 `feidex` 的 fix 已经说明，这条路很容易把“临时近似”写进主链。
2. 后面大概率还要回头拆 catalog / binding / state seam。
3. 如果范围控制不严，很容易一边接可见入口，一边反复修 current-card、request、final-output 边界。

### 13.3 推荐结论

推荐 `A2`，不是纯 `A1`，也不是直接 `B2`。

原因：

1. 当前 `master` 还存在 `#369` 审计揭示的 family-only / productMode-only 固化点，直接上 visible Claude 会把这些点变成真实回归面。
2. 最新 `feidex` 修掉的不是边角 bug，而是 final output、plan reject、steer/interrupt、workspace backend matcher 这类主链问题。
3. 但如果长期只补基座、不尽快接入可见 Claude，基座工作又会重新膨胀成无边界重构。

推荐顺序：

1. `#492` 已完成，state partition 不再是待办。
2. `#493/#494` 已完成，不再作为后续执行顺序里的待办。
3. `#495 -> #497 -> #498` 这条 pre-MVP 技术基座顺序现已全部落地完成。
4. `#496` 这一轮 MVP 决策门已在 2026-04-29 收口：`/detach` 进入 visible MVP，`/review` / `/bendtomywill` 退出，`/list` `/use` / target picker 只按 backend 过滤，headless 下的 `Claude <-> Codex` 按工作区目录保连续性切换。
5. 下一步不再继续拍板同一轮范围，而是按这版边界推进实现与验证。

补充解释：

1. 当前工作区里最关键的 pre-MVP 技术不确定性已经不再停留在 `#495/#497/#498`。
2. `#498` 的收口额外证明了一点：`SessionCatalog` 不是 `ThreadsRefresh` 的同义词，但在 host bridge 落地后这两个 capability 都应真实声明。
3. 因此当前真正下一步不再是“继续做调研”或“继续补技术基座”，而是按 `#496` 已拍板的边界进入实现收口。

如果一定要优先拿可见 MVP，最低也应按 `B1 -> B2` 走，不建议从当前 `master` 直接跳到 `B2`。

# Claude Normal Mode PoC 方案设计

> Type: `inprogress`
> Updated: `2026-04-13`
> Summary: 增加 `/mode codex|claude|vscode` 产品入口与 backend-aware 配置规则，明确切换时状态清理边界。

## 1. 背景

`#185` 的目标不是做一条与 Codex 完全隔离的新产品线，而是在同一套 relay/orchestrator 结构下支持多 backend 并存：

- 与 Codex 实例交互时，不触发 Claude 分支，行为保持当前语义。
- 与 Claude 实例交互时，未支持能力允许显式报错，但不能 silent failure。

当前仓库已经具备较强的 canonical `turn/item/request` 承载能力，但运行时调度仍存在 Codex 假设，需要先补一层 provider/capability 路由基座。

## 2. 设计目标

1. 支持 Codex/Claude 实例同机并存，按实例能力派发命令。
2. Codex 路径保持兼容，不因 Claude PoC 引入行为回归。
3. Claude 仅覆盖 normal mode；vscode mode 显式不支持。
4. 不支持能力返回明确 `command_rejected/problem + notice`。
5. workspace 可跨 backend 共用，但会话数据必须按 backend 隔离。

## 3. 非目标

- 第一阶段不实现 Claude 的 vscode follow/focus 语义。
- 第一阶段不追求 `turn.steer` 与 Codex 精确同语义。
- 第一阶段不做全仓 `thread -> session` 字段改名。

## 4. 现状差距

1. `daemon onHello` 目前无条件发送 `threads.refresh`。
2. `startup threads refresh` 使用全局 pending 门控，可能被不支持该能力的实例拖住恢复流程。
3. `Hello.Capabilities` 当前未进入实际派发决策。
4. wrapper 入口仍是 codex app-server 单路径。

## 5. 目标架构

## 5.1 实例级 provider 与能力模型

- 每个 instance 在 hello 阶段上报 provider 与 capabilities。
- daemon 持久化 instance capabilities。
- orchestrator/daemon 在发命令前必须进行 capability 检查。

建议能力集合：

- `threads_refresh`
- `turn_steer`
- `request_respond`
- `session_catalog`
- `resume_by_thread_id`
- `requires_cwd_for_resume`
- `supports_vscode_mode`

## 5.2 provider-aware 命令派发

- 命令派发路径保持统一，但在 dispatch 前根据目标实例能力决定：
  - 支持: 正常下发。
  - 不支持: 返回 `command_rejected/problem`，并投影用户可见 notice。

## 5.3 adapter 分层

- 保持 `codex` adapter 现有语义。
- 新增 `claude` adapter（normal mode）。
- 禁止在 codex translator 内堆 provider 分支。

## 5.4 数据分区模型（新增）

### 分区原则

1. `workspace` 是文件系统作用域，允许 Codex/Claude 共享同一目录。
2. `thread/session` 是 backend 作用域，必须隔离存储与展示。
3. 任何“会话选择/恢复”都必须带 backend 维度，禁止只靠 thread id 判定。

### 推荐键模型

- 会话唯一键：`backend + instance_id + thread_id(session_id)`
- workspace 键：保持现有 `workspace_key`
- surface 恢复目标：至少携带 `resume_backend + resume_instance_id + resume_thread_id`

### 存储与缓存规则

1. instance 内的 `Threads` 只存本 backend 会话，不做跨 backend merge。
2. `/list` 默认展示当前 attached instance 的会话视图。
3. 若后续提供“同 workspace 汇总视图”，必须按 backend 分组展示，不得混排成单一列表。
4. `/use <id>` 只在当前 backend 视图下解析；跨 backend 不自动跳转。

### 状态机约束

1. surface 当前 attached instance 决定本轮命令 backend。
2. 切换到不同 backend 实例时，不继承旧 backend 的 selected thread。
3. 失败/降级提示必须指明“当前 backend 不支持”，避免误导为全局异常。

## 5.5 backend-aware 配置模型（新增）

### 配置原则

1. workspace 级参数继续存在，但按 backend 命名空间隔离。
2. `model`、`reasoning_effort`、`access_mode` 这类参数不能跨 backend 复用。
3. surface 切换 backend 时，读取目标 backend 对应的 workspace 参数快照。

### 推荐结构

- 现有概念：`WorkspaceDefaults[workspace_key] -> ModelConfigRecord`
- 目标概念：`WorkspaceDefaults[workspace_key][backend] -> BackendConfigRecord`

其中 `BackendConfigRecord` 可先保持与当前 `ModelConfigRecord` 等价字段，再按 backend 逐步扩展。

## 5.6 旧数据升级与兼容（新增）

### 兼容原则

1. 旧版本没有 backend 维度的数据，默认按 `codex` 解释。
2. 升级后不得因为缺少 backend 字段导致：
   - surface 无法恢复
   - workspace 默认配置丢失
   - `/mode normal` 旧用户进入异常状态
3. 能 lazy 兼容的地方优先 lazy 兼容；只有在读写模型明显冲突时才做显式迁移。

### 需要重点审视的数据

1. surface resume state
2. workspace defaults / prompt override
3. instance snapshot / thread selection 相关持久状态
4. 任何把 thread id 当作全局唯一键缓存的状态

### 建议策略

1. 读取旧数据时：
   - 缺失 backend 字段 -> 视为 `codex`
   - 缺失 backend-aware workspace config -> 读旧配置并挂到 `codex` 名下
2. 写回时：
   - 按新结构持久化
   - 尽量做到一次读、按需升级、原地收敛
3. 若某状态无法安全自动迁移：
   - 明确丢弃该状态
   - 给用户可恢复的默认行为
   - 在文档和 notice 中说明

## 5.7 建议状态结构（深化）

本阶段不要求一次性重命名所有类型，但建议把 backend 维度明确塞进现有状态结构，而不是靠 `Instance.Source` 事后推断。

建议最小扩展：

- `state.InstanceRecord`
  - 新增 `Backend string`
  - `Source` 继续保留，表达运行来源，例如 `vscode/headless/claude`
- `state.ThreadRecord`
  - 不必单独存 backend，默认继承所属 instance backend
- `SurfaceResumeEntry`
  - 新增 `ResumeBackend`
- `WorkspaceDefaults`
  - 从单层配置升级为 `workspace -> backend -> config`

推荐解释：

- `Backend` 表达“这是谁的会话协议面”，候选值：
  - `codex`
  - `claude`
- `Source` 表达“这个实例从哪里来”，候选值继续包括：
  - `vscode`
  - `headless`
  - `claude`

这样可以避免把“来源”和“后端”混成一个字段。

## 6. 命令矩阵（PoC 版）

| Command | Codex | Claude PoC | 处理策略 |
| --- | --- | --- | --- |
| `prompt.send` | support | support | 正常派发 |
| `turn.interrupt` | support | support | 正常派发 |
| `request.respond` | support | support | 正常派发 |
| `threads.refresh` | support | optional | 按 capability 检查，不支持则拒绝 |
| `turn.steer` | support | unsupported(v1) | 显式拒绝 + notice |
| `vscode-mode path` | support | unsupported | 显式拒绝 + notice |

## 6.0 dispatch 决策顺序（深化）

任一发往 agent 的命令，在 daemon/orchestrator 侧都按以下顺序决策：

1. 确定目标 surface 当前 attached instance。
2. 读取 instance backend 与 capabilities。
3. 检查该命令是否被当前 mode 允许。
4. 检查该命令是否被当前 backend capability 允许。
5. 若允许，正常派发。
6. 若不允许，走统一 reject/problem + notice，不尝试跨 backend fallback。

这个顺序的目的是防止：

- mode 说可以，但 backend 不支持
- backend 支持，但当前 mode 不该开放
- 当前实例不支持，系统却偷偷落到别的实例

## 6.1 并行产品语义（新增）

1. `/list`：看“当前实例（当前 backend）”会话，不跨 backend 混看。
2. `/use`：只消费当前 backend 会话 id；如果用户给了另一个 backend 的 id，返回明确提示。
3. `/new`：在当前 backend 新建会话，workspace 沿用当前 workspace。
4. `/status`：建议在 attachment/instance 摘要中显示 backend 标识，避免误判上下文来源。

## 6.2 `/mode` 入口语义（新增）

### 用户可见模式

1. `/mode codex`：进入 Codex normal 语义。
2. `/mode claude`：进入 Claude normal 语义。
3. `/mode vscode`：保持现有 vscode 模式语义。

兼容规则：

- `/mode normal` 视为 `/mode codex`（兼容旧命令，不再新增第四种语义）。

### 切换行为

当 surface 在 `codex <-> claude` 间切换时：

1. 保留：`workspace` 相关信息（workspace key、workspace 级 backend 配置）。
2. 清除：当前会话态（attached instance、selected thread、pending request、queue、active turn、staged inputs/images、resume target 中的 thread/session 部分）。
3. 重建：目标 backend 的默认能力视图与命令可用性。

进一步细化：

- 若当前 surface 仍有 active turn / pending request / dispatching queue：
  - 默认拒绝切换
  - 提示用户先等待完成、`/stop` 或 `/detach`
- 切换成功后：
  - `SelectedThreadID = ""`
  - `AttachedInstanceID = ""`
  - `PendingRequests = nil`
  - `ActiveRequestCapture/ActiveCommandCapture = nil`
  - `QueueItems` 清空
  - `RouteMode` 回到“仅保留 workspace、未选会话”的初始 normal 态
- 若切到 `claude`：
  - 命令可见性立即切成 Claude 能力矩阵
- 若切回 `codex`：
  - 恢复 Codex normal 命令矩阵与 workspace 下的 Codex 默认配置

### 设计原因

- 用户需要主动感知“当前在用哪个 backend”。
- backend 会话数据天然不兼容，切换时保留会话态会制造隐性串扰与误恢复。
- workspace 是文件系统作用域，可共享，不应被切换动作清空。

## 7. 分阶段实施

## 阶段 A：兼容性护栏

1. 把 hello capabilities 接入 instance runtime state。
2. `onHello` 改为 capability-aware 初始化，不再无条件 `threads.refresh`。
3. 修正 startup refresh pending 统计，只追踪已派发 refresh 的实例。
4. 补充 backend 维度到 surface resume / instance snapshot 关键状态（至少写入并保留，不先改 UI）。
5. `/mode` 切换实现为 provider-aware（`normal -> codex` 别名），并补齐切换时状态清理。
6. 盘点并实现旧版本状态的读取兼容或迁移规则。
7. 明确 `Backend` 与 `Source` 的状态职责，避免后续再混字段语义。

交付物：

- capability 持久化与查询接口
- capability-aware `onHello` 分支
- backend-aware 基础状态结构草图落地
- 回归测试（Codex-only 不回归）

## 阶段 B：Claude bridge 契约（PoC）

1. 固化 wrapper <-> Claude bridge 最小 NDJSON 契约。
2. 打通 `prompt.send` / `interrupt` / `request.respond` / `session.list`。
3. 错误统一为 machine-readable `code/message/retryable`。

交付物：

- bridge 协议文档
- adapter 雏形与单测

## 阶段 C：normal mode 主链路闭环

1. `/new`、`/use`、`/list` 的 Claude 实例路径闭环。
2. `resume(session_id + cwd)` 成功路径与失败提示。
3. `turn.steer` 显式降级。
4. 后端并行时 `/list`/`/use` 不串 backend，会话选择与恢复可预期。
5. `/mode codex|claude` 在产品侧可感知，且切换行为符合“保留 workspace、清空会话态”。
6. workspace 参数读写切到 backend-aware 结构，至少兼容 `codex` 与 `claude` 两套默认值。

交付物：

- normal mode e2e 测试样例
- Feishu 可见提示文案
- `/mode` 切换状态清理测试

## 阶段 D：稳定化与文档收口

1. 混合实例稳定性回归（Codex + Claude）。
2. 更新状态机与用户文档。
3. 明确下一阶段（是否扩展到更深能力）。

## 8. 验收标准

1. Codex-only 场景行为与当前版本一致（关键命令与恢复路径不回归）。
2. Codex+Claude 混合场景下，实例间命令不会串扰。
3. Claude 不支持命令均为显式可见失败，不出现 silent failure。
4. `request` 与 `queue` 状态机在 reject 后可继续使用，不进入死状态。
5. 关闭 Claude 实例后，系统仍可按现有方式服务 Codex。
6. 同 workspace 并存 Codex+Claude 时，`/list` 与 `/use` 不混 backend 数据。
7. surface resume 不会把一个 backend 的会话误恢复到另一个 backend 实例。
8. `/mode normal` 与 `/mode codex` 语义一致；`/mode claude` 切换后不会残留旧 backend 会话态。
9. 旧版本持久化数据升级后仍可正常进入 Codex 路径，且不会因缺失 backend 字段崩坏。
10. `Backend` 与 `Source` 的职责在代码与产品文案中不混淆。

## 9. 风险与回退

- 风险：能力门控遗漏导致错误下发。
- 风险：startup recovery 被错误 pending 条件阻塞。
- 风险：拒绝路径只返回 ack 未投影 notice，造成“像卡死”。
- 风险：旧版本持久化状态没有 backend 维度，升级后被错误解释或覆盖。

回退策略：

- 所有 provider-aware 分支保持“Codex 默认路径优先”。
- 若 Claude 路径出现异常，可通过禁用 Claude 实例回退，不影响 Codex 运行。

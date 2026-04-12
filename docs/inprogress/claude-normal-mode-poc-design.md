# Claude Normal Mode PoC 方案设计

> Type: `inprogress`
> Updated: `2026-04-13`
> Summary: 补充 backend 并行场景下的数据分区与产品语义，明确 workspace 共享与 thread/session 隔离规则。

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

## 6. 命令矩阵（PoC 版）

| Command | Codex | Claude PoC | 处理策略 |
| --- | --- | --- | --- |
| `prompt.send` | support | support | 正常派发 |
| `turn.interrupt` | support | support | 正常派发 |
| `request.respond` | support | support | 正常派发 |
| `threads.refresh` | support | optional | 按 capability 检查，不支持则拒绝 |
| `turn.steer` | support | unsupported(v1) | 显式拒绝 + notice |
| `vscode-mode path` | support | unsupported | 显式拒绝 + notice |

## 6.1 并行产品语义（新增）

1. `/list`：看“当前实例（当前 backend）”会话，不跨 backend 混看。
2. `/use`：只消费当前 backend 会话 id；如果用户给了另一个 backend 的 id，返回明确提示。
3. `/new`：在当前 backend 新建会话，workspace 沿用当前 workspace。
4. `/status`：建议在 attachment/instance 摘要中显示 backend 标识，避免误判上下文来源。

## 7. 分阶段实施

## 阶段 A：兼容性护栏

1. 把 hello capabilities 接入 instance runtime state。
2. `onHello` 改为 capability-aware 初始化，不再无条件 `threads.refresh`。
3. 修正 startup refresh pending 统计，只追踪已派发 refresh 的实例。
4. 补充 backend 维度到 surface resume / instance snapshot 关键状态（至少写入并保留，不先改 UI）。

交付物：

- capability 持久化与查询接口
- capability-aware `onHello` 分支
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

交付物：

- normal mode e2e 测试样例
- Feishu 可见提示文案

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

## 9. 风险与回退

- 风险：能力门控遗漏导致错误下发。
- 风险：startup recovery 被错误 pending 条件阻塞。
- 风险：拒绝路径只返回 ack 未投影 notice，造成“像卡死”。

回退策略：

- 所有 provider-aware 分支保持“Codex 默认路径优先”。
- 若 Claude 路径出现异常，可通过禁用 Claude 实例回退，不影响 Codex 运行。

# Claude Normal Mode PoC 方案设计

> Type: `inprogress`
> Updated: `2026-04-13`
> Summary: 将 Claude backend PoC 收敛为实例级兼容方案，明确 capability 路由、命令降级策略与分阶段实施计划。

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

## 6. 命令矩阵（PoC 版）

| Command | Codex | Claude PoC | 处理策略 |
| --- | --- | --- | --- |
| `prompt.send` | support | support | 正常派发 |
| `turn.interrupt` | support | support | 正常派发 |
| `request.respond` | support | support | 正常派发 |
| `threads.refresh` | support | optional | 按 capability 检查，不支持则拒绝 |
| `turn.steer` | support | unsupported(v1) | 显式拒绝 + notice |
| `vscode-mode path` | support | unsupported | 显式拒绝 + notice |

## 7. 分阶段实施

## 阶段 A：兼容性护栏

1. 把 hello capabilities 接入 instance runtime state。
2. `onHello` 改为 capability-aware 初始化，不再无条件 `threads.refresh`。
3. 修正 startup refresh pending 统计，只追踪已派发 refresh 的实例。

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

## 9. 风险与回退

- 风险：能力门控遗漏导致错误下发。
- 风险：startup recovery 被错误 pending 条件阻塞。
- 风险：拒绝路径只返回 ack 未投影 notice，造成“像卡死”。

回退策略：

- 所有 provider-aware 分支保持“Codex 默认路径优先”。
- 若 Claude 路径出现异常，可通过禁用 Claude 实例回退，不影响 Codex 运行。


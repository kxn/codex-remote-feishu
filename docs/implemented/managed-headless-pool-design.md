# Managed Headless Pool 设计

> Type: `implemented`
> Updated: `2026-04-08`
> Summary: 把 `#22` 的最终实现结论沉淀为当前 source of truth，记录 managed headless pool 的状态、预热、后台 refresh 和复用边界。

## 1. 文档定位

这份文档描述的是**当前已经落地**的 managed headless pool 行为。

它承接的是 `#22` 最终实现结果，而不是继续保留 issue 作为唯一设计面。当前 source of truth 以本文和以下文档为准：

1. [remote-surface-state-machine.md](../general/remote-surface-state-machine.md)
2. [feishu-product-design.md](../general/feishu-product-design.md)
3. [user-guide.md](../general/user-guide.md)

历史上那条 `/newinstance` 手工 headless 恢复链已经废弃，相关背景保留在：

1. [feishu-headless-instance-design.md](../obsoleted/feishu-headless-instance-design.md)

## 2. 当前实现概览

当前代码里的 managed headless 不再只是“临时拉起后偶尔复用的 headless 实例”，而是 daemon 会持续维护的一组后台资源：

1. detached `/use` 命中离线 thread 时，resolver 会优先尝试复用可用 managed headless；没有合适实例时再启动 preselected headless。
2. daemon 会维护默认 `min-idle = 1` 的最小预热池，确保至少有一个 warm member 可以被后续 thread-first 路径复用。
3. idle managed headless 会按 bounded 策略后台发送 `threads.refresh`，持续维护 detached/global thread catalog 的 freshness。
4. admin 实例摘要会展示当前 pool member 的状态、最近 refresh 时间和真实 workspace metadata，而不是继续显示复用前的旧 root。

这里保留一条实现前提：

1. 仍然是一 remote surface 独占一 instance。
2. managed headless 是后台可复用资源，不是重新暴露给用户手工 `/attach` 的前台入口。

## 3. Pool Member 生命周期

### 3.1 状态机

当前 daemon 显式维护四种 pool member 状态：

1. `starting`
   - headless 进程刚被拉起，还在等待 hello。
   - 在 `StartTTL` 窗口内会计入 `min-idle`，避免重复预热。
2. `busy`
   - 当前实例已被 surface attach，或存在 active turn。
   - `busy` 不会被当作 idle pool member，也不会进入后台 idle refresh。
3. `idle`
   - 实例在线、未被 surface 占用、且没有 active turn。
   - daemon 会记录 `IdleSince`，并把它视为可复用 pool member。
4. `offline`
   - 实例断连，或 `starting` 超过 `StartTTL` 仍未连回 relay。
   - offline member 继续保留给 admin 可观测，但不再计入 `min-idle`。

## 3.2 后台 refresh 与最小预热

当前实现把“保持至少一个 warm member”和“让 idle member 持续刷新 thread catalog”分成两条守护逻辑：

1. 初次 hello 后，会先发一次 `threads.refresh`。
2. 进入 `idle` 的 managed headless，会按 `IdleRefreshInterval` 触发后台 `threads.refresh`。
3. 如果 refresh in-flight 超过 `IdleRefreshTimeout`，daemon 会清掉该轮 in-flight 标记并等待下一轮重试。
4. `ensureMinIdleManagedHeadlessLocked()` 会在 tick 中统计 warm member；只有 `idle` 与启动窗口内的 `starting` 计入池容量。
5. 当 pool 低于 `min-idle = 1` 时，daemon 会补起 replacement prewarm。
6. prewarm 默认使用 daemon state dir 作为 workdir，display name 默认为 `headless`。

这里的实现目标是“让 thread-first 主链稳定复用后台资源”，而不是提前做更激进的池化策略。

## 3.3 复用与 workspace retarget

当前 thread-first `/use` 和 managed headless pool 的衔接方式是：

1. 若目标 thread 只剩离线快照，但存在 idle managed headless，可直接复用该 idle member。
2. 复用或 preselected create 成功后，实例会自动 attach 到目标 thread。
3. managed headless 的 `WorkspaceRoot / DisplayName` 会收敛到当前真实目标 thread 的 `cwd`，避免 admin 面继续显示复用前的旧 root。
4. source instance 上暂存的 notice / final replay 会在 headless 接手时一并回放。

这意味着当前实现已经不再把 managed headless 当成“和 VS Code 实例并列的手工恢复入口”，而是当成 daemon 背后的 thread resolver 资源。

## 4. 当前边界

以下内容本轮**没有**一起改：

1. 不支持多个 remote surface 共享同一个 instance。
2. 不做 workspace-aware pool policy。
3. 不做更高默认 idle 数量或更激进的 replacement 策略。
4. 不额外引入比 `threads.refresh` 更强的 detached catalog freshness source。

如果后续要继续增强这些能力，应该另开 follow-up issue，而不是把它们默认为当前实现的一部分。

## 5. 验证与相关实现

本轮实现和文档回写依赖的关键路径包括：

1. daemon pool lifecycle:
   - [app_headless_pool.go](../../internal/app/daemon/app_headless_pool.go)
   - [admin_instances.go](../../internal/app/daemon/admin_instances.go)
2. thread-first reuse / preselected headless:
   - [service_test.go](../../internal/core/orchestrator/service_test.go)
3. daemon tests:
   - [app_test.go](../../internal/app/daemon/app_test.go)
   - [admin_instances_test.go](../../internal/app/daemon/admin_instances_test.go)

相关 issue：

1. GitHub issue: `#22`

本轮实现验收里明确覆盖过：

1. idle managed headless 的后台 refresh 调度
2. `min-idle = 1` 预热与 replacement
3. detached `/use` 对 idle managed headless 的复用
4. reused headless 的 workspace retarget 与 admin 可观测面一致性

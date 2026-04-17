# Daemon Package Refactor Plan

> Type: `draft`
> Updated: `2026-04-17`
> Summary: 为 `internal/app/daemon` 输出拆分方案，明确职责簇、共享状态压力、建议的分阶段抽取顺序，并同步当前已落地的 phase A / B runtime 边界。

## 1. 目标

这份文档只解决一个问题：

- 如何在不破坏当前 daemon 运行边界的前提下，逐步降低 `internal/app/daemon` 对单个 `*App` 容器的依赖。

目标不是马上把所有文件搬到新目录，而是先得到一份足够具体、能指导后续实现 issue 的拆分计划。

## 2. 当前现状

截至 `2026-04-11`，`internal/app/daemon` 是仓库里最大的生产包之一：

- 非测试代码约 `11,329` 行
- 生产文件 `38` 个
- `*App` 方法约 `254` 个

最大的几个生产文件分别是：

1. [app_upgrade.go](../../internal/app/daemon/app_upgrade.go)
2. [admin_feishu_onboarding.go](../../internal/app/daemon/admin_feishu_onboarding.go)
3. [admin.go](../../internal/app/daemon/admin.go)
4. [admin_feishu_handlers.go](../../internal/app/daemon/admin_feishu_handlers.go)
5. [app_ingress.go](../../internal/app/daemon/app_ingress.go)
6. [app_surface_resume_state.go](../../internal/app/daemon/app_surface_resume_state.go)
7. [admin_vscode.go](../../internal/app/daemon/admin_vscode.go)
8. [tool_service.go](../../internal/app/daemon/tool_service.go)

但真正的问题不是“某一个文件太长”，而是大量职责持续向 `*App` 聚合。

## 3. 当前职责簇

按代码现状，`internal/app/daemon` 已经自然长出至少六个职责簇。

### 3.1 进程与装配外壳

代表文件：

- [entry.go](../../internal/app/daemon/entry.go)
- [app.go](../../internal/app/daemon/app.go)
- [startup.go](../../internal/app/daemon/startup.go)
- [app_shutdown.go](../../internal/app/daemon/app_shutdown.go)
- [pprof.go](../../internal/app/daemon/pprof.go)

这部分负责：

- 加载配置与运行时路径
- 组装 Feishu gateway / relay / external access / tool runtime
- 建立 HTTP listener 和生命周期钩子
- 驱动 `Run / Bind / Shutdown`

这层天然应该保留在 `daemon` 根包，作为最终 composition root。

### 3.2 Relay ingress 与 UI 出口

代表文件：

- [app_ingress.go](../../internal/app/daemon/app_ingress.go)
- [ingress.go](../../internal/app/daemon/ingress.go)
- [relay_connections.go](../../internal/app/daemon/relay_connections.go)
- [app_ui.go](../../internal/app/daemon/app_ui.go)
- [app_inbound_lifecycle.go](../../internal/app/daemon/app_inbound_lifecycle.go)

这部分负责：

- relayws 入站连接排队
- hello / event / ack / disconnect 转换
- 对 orchestrator 调用和 UIEvent 下发
- 传输降级与 stale item 清理

这是 daemon 的核心执行环之一，和 `service`, `gateway`, `relay`, `mu` 高度耦合。

### 3.3 Admin / setup / onboarding

代表文件：

- [admin.go](../../internal/app/daemon/admin.go)
- [admin_feishu_handlers.go](../../internal/app/daemon/admin_feishu_handlers.go)
- [admin_feishu_onboarding.go](../../internal/app/daemon/admin_feishu_onboarding.go)
- [admin_feishu_runtime.go](../../internal/app/daemon/admin_feishu_runtime.go)
- [admin_vscode.go](../../internal/app/daemon/admin_vscode.go)
- [admin_instances.go](../../internal/app/daemon/admin_instances.go)
- [admin_storage_images.go](../../internal/app/daemon/admin_storage_images.go)
- [admin_storage_preview.go](../../internal/app/daemon/admin_storage_preview.go)

这部分负责：

- admin/setup HTTP route 注册
- 会话鉴权与 bootstrap state
- Feishu setup/admin 生命周期
- VS Code setup/admin 接口
- 实例管理、存储管理和运行时检测

这是最明显的“产品后端”职责簇，也是后续最值得形成独立 runtime 模块的一块。

### 3.4 Headless lifecycle 与恢复

代表文件：

- [app_headless.go](../../internal/app/daemon/app_headless.go)
- [app_headless_pool.go](../../internal/app/daemon/app_headless_pool.go)
- [app_headless_restore_hints.go](../../internal/app/daemon/app_headless_restore_hints.go)
- [app_headless_restore_recovery.go](../../internal/app/daemon/app_headless_restore_recovery.go)
- [app_surface_resume_state.go](../../internal/app/daemon/app_surface_resume_state.go)
- [surface_resume_state.go](../../internal/app/daemon/surface_resume_state.go)
- [headless_restore_hints.go](../../internal/app/daemon/headless_restore_hints.go)
- [workspace_surface_context.go](../../internal/app/daemon/workspace_surface_context.go)

这部分负责：

- managed headless 拉起、回收、预热和 refresh
- surface resume store 的持久化
- detached / vscode 恢复回退
- workspace 上下文文件同步

这块已经不是简单 helper，而是一个完整的恢复子系统。

### 3.5 Upgrade / release / external access

代表文件：

- [app_upgrade.go](../../internal/app/daemon/app_upgrade.go)
- [app_upgrade_execute.go](../../internal/app/daemon/app_upgrade_execute.go)
- [external_access.go](../../internal/app/daemon/external_access.go)
- [app_feishu_attention.go](../../internal/app/daemon/app_feishu_attention.go)

这部分负责：

- 版本检查和升级事务状态
- `/debug` / `/upgrade` 命令落地
- external access listener 与临时外链
- Feishu time sensitive 同步

这里的状态调度也相对独立，适合后续抽为单独 runtime manager。

### 3.6 Local tool service

代表文件：

- [tool_service.go](../../internal/app/daemon/tool_service.go)

这部分负责：

- loopback local tool HTTP server
- bearer token 管理
- tool manifest / call 路由
- surface-aware tool context 解析

这是当前最窄、最适合先抽离出 `App` 方法面的子系统之一。

## 4. 真正的耦合点

`daemon` 难拆，不是因为“文件之间 import 太多”，而是因为多个职责同时依赖同一批核心资源：

1. `a.mu`
   - 当前大多数 runtime 行为都直接持有 `App` 锁并读写内部状态。
2. `a.service`
   - orchestrator 读写几乎是所有子系统的汇合点。
3. `a.handleUIEvents(...)`
   - 很多子系统的最终副作用都要回落到统一 UI 出口。
4. `a.gateway` / `a.relay`
   - Feishu 和 relay transport 不是边缘依赖，而是主执行面。
5. App struct 自身字段过多
   - 进程 listener、headless pool、resume recovery、admin auth、external access、upgrade scheduler、tool runtime 都堆在同一个 struct 中。

这意味着：

1. 直接把文件挪成多个 package，短期只会把私有耦合改成导出耦合。
2. 第一阶段更合理的动作是“抽出 runtime 子对象并定义少量显式 API”，而不是立刻重画 package graph。

## 5. 建议的目标结构

### 5.1 第一原则

先做“同包内拆对象”，再决定是否拆子包。

原因：

1. 现在大量能力还依赖 `App` 私有字段和锁。
2. 先把边界从“任意方法都能碰全部字段”收束成“子系统对象只拿自己需要的依赖”，收益最高。
3. 只有当 API 稳定后，拆到子包才不会制造一层新的循环依赖和导出污染。

### 5.2 目标角色

建议把 `App` 逐步收束成一个薄的运行时外壳，只保留：

- composition root
- 顶层 lifecycle
- 顶层锁与少量共享依赖的托管
- 跨子系统的事件编排

然后把内部能力收束成几类对象：

1. `adminRuntime`
   - 管理 admin/setup HTTP 接口、bootstrap state、setup token、Feishu/VS Code setup 子流。
2. `surfaceResumeRuntime`
   - 管理 surface resume store、resume recovery、VS Code resume notice / migration prompt、workspace context 同步。
3. `headlessRuntime`
   - 管理 headless lifecycle、prewarm、restore hints、headless restore 重试与副作用调度。
4. `upgradeRuntime`
   - 管理版本检查、升级事务状态、升级结果回写和 `/upgrade` 触发。
5. `toolRuntime`
   - 管理 local tool service listener、manifest、鉴权与 tool 调用。
6. `ingressRuntime`
   - 管理 relay ingress pump、队列、transport degrade 和连接状态。

这里不要求它们第一天就变成独立 package，但要求开始拥有清晰的依赖构造方式。

## 6. 建议的分阶段顺序

### 6.1 Phase 1: 先抽 `toolRuntime`

切口：

- [tool_service.go](../../internal/app/daemon/tool_service.go)

原因：

1. HTTP 面清晰。
2. 需要的依赖少。
3. 生命周期边界清楚。
4. 已有测试相对集中。

目标：

- `App` 不再直接持有 tool listener/token 细节。
- `App` 只负责把 surface resolver / send-file 等 domain 回调注入 `toolRuntime`。

### 6.2 Phase 2: 再抽 `surface resume` 相关 runtime

切口：

- [app_surface_resume_state.go](../../internal/app/daemon/app_surface_resume_state.go)
- [surface_resume_state.go](../../internal/app/daemon/surface_resume_state.go)
- [workspace_surface_context.go](../../internal/app/daemon/workspace_surface_context.go)

原因：

1. 当前功能边界已经很强。
2. 主要围绕持久化、恢复与 context 同步。
3. 这是削减 `App` 状态字段最有效的一刀之一。

目标：

- 把 store / recovery / notice / context file 同步集中到统一对象。
- `App` 只在 action、tick、shutdown 等时机调用少量显式入口。

### 6.3 Phase 3: 收束 `headlessRuntime`

切口：

- [app_headless.go](../../internal/app/daemon/app_headless.go)
- [app_headless_pool.go](../../internal/app/daemon/app_headless_pool.go)
- [app_headless_restore_hints.go](../../internal/app/daemon/app_headless_restore_hints.go)
- [app_headless_restore_recovery.go](../../internal/app/daemon/app_headless_restore_recovery.go)

原因：

1. 当前逻辑已经具备明显的 manager 形态。
2. 与 `surface resume` 有关系，但不应该继续共享一个无边界的 `App` 状态区。

目标：

- 明确 headless runtime 的状态、时钟和 side effect API。
- 让 `App` 只保留 orchestration 级调用。

### 6.4 Phase 4: 收束 `upgradeRuntime`

切口：

- [app_upgrade.go](../../internal/app/daemon/app_upgrade.go)
- [app_upgrade_execute.go](../../internal/app/daemon/app_upgrade_execute.go)

原因：

1. 当前升级状态机已经自成体系。
2. 它和普通 ingress/admin 生命周期耦合较低。

目标：

- 把 upgrade check/start/result state 从 `App` 字段区独立出来。
- 明确 upgrade UI notice 与 command 触发边界。

### 6.5 Phase 5: 最后处理 `adminRuntime`

切口：

- [admin.go](../../internal/app/daemon/admin.go)
- [admin_feishu_handlers.go](../../internal/app/daemon/admin_feishu_handlers.go)
- [admin_feishu_onboarding.go](../../internal/app/daemon/admin_feishu_onboarding.go)
- [admin_feishu_runtime.go](../../internal/app/daemon/admin_feishu_runtime.go)
- [admin_vscode.go](../../internal/app/daemon/admin_vscode.go)

原因：

1. 这是当前最大的一团产品后端逻辑。
2. 也是最容易因为“先拆目录、后补边界”而把问题复制到新 package 的区域。

目标：

- 先把 route registration、handler、runtime orchestration、config I/O 明确分层。
- 稳定后再考虑是否落到 `internal/app/daemon/adminruntime` 一类子包。

## 7. 暂时不建议先动的部分

以下部分不适合作为第一刀：

1. [entry.go](../../internal/app/daemon/entry.go)
   - 它是组装层，不是主要复杂度来源。
2. [app.go](../../internal/app/daemon/app.go)
   - 它的问题是字段过多，不是应该先整体拆散。
3. `ingress + UI + orchestrator` 主环
   - 这是主执行路径，先动风险高，且需要在前面子系统边界更明确后再收束。

## 8. 建议的第一批落地切口

如果只做第一轮真实代码重构，建议顺序是：

1. `toolRuntime`
2. `surfaceResumeRuntime`

理由：

1. 这两块的边界最清楚。
2. 它们能明显减少 `App` 直接持有的状态和方法。
3. 对主事件环的侵入比 headless / admin / ingress 小得多。

当前进展：

- `#248` phase A 已先把 `toolRuntime` 和 `surfaceResumeRuntime` 的状态归属从 `App` 根字段收口到显式 runtime state；后续仍需要继续把更多 receiver / side-effect 边界从 `App` 根上剥离。
- `#248` phase B 已继续把 `upgradeRuntime` 的检查 / 调度 / 结果刷新状态迁移到显式 runtime state，并清掉 phase A 遗留在 `App` 根 struct 上的旧字段残留。

## 9. 验证要求

后续每一阶段的拆分都不应该只看编译通过，至少要保留这些验证：

1. `go test ./internal/app/daemon ./internal/core/orchestrator ./internal/adapter/feishu`
2. 对涉及的恢复、升级、tool service、admin handler 路径补针对性测试
3. 如果某一阶段动到 lifecycle 或 shutdown，必须再跑 `go test ./...`

## 10. 当前结论

当前 `internal/app/daemon` 的问题，本质上是“单容器聚合过多 runtime 职责”，不是“缺少子目录”。

因此最合理的路线是：

1. 先把 `App` 降为薄外壳。
2. 先做同包内 runtime 对象抽取。
3. 等 API 稳定后，再决定哪些区域值得物理拆成子包。

首轮建议从 `toolRuntime`、`surfaceResumeRuntime` 开始，并尽快补上 `upgradeRuntime` 这类边界清晰的状态收口。

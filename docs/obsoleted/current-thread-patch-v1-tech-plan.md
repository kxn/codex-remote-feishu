# Current Thread Patch V1 Tech Plan

> Type: `obsoleted`
> Updated: `2026-11-21`
> Summary: 用户明确要求完全删除直接改写 Codex 会话历史及回滚功能；本文仅保留历史设计，不再实施。
> Superseded By: `docs/draft/feishu-agent-team-product-design.md`

2026-11-21：命令、菜单、回调、会话文件改写、事务状态与回滚实现均已删除，不保留兼容入口。

## 1. 文档定位

本文是 `#464` 的技术计划稿。

它解决的不是“要不要做”，而是：

1. 现在 upstream 的 thread 正文到底落在 JSONL 还是 SQLite。
2. 在这个前提下，V1 应该把 patch 写到哪里。
3. V1 需要复用本仓库哪些运行时底座。
4. 实现顺序应该如何切分，才能先做出可验证的最小闭环。

相关文档：

- [current-thread-patch-v1-prd.md](./current-thread-patch-v1-prd.md)
- [codex-session-patcher-research-2026-04.md](./codex-session-patcher-research-2026-04.md)
- Tracking issue: `#464`

## 2. 当前已确认的产品边界

本计划默认以下边界已经拍板：

- 一次事务只处理当前 attached thread 的最新一个已完成 assistant turn
- 同一 turn 内允许存在多个 patch 候选点
- slash 命令和菜单项只是两个入口，进入同一条 patch 流程
- replacement text 通过前台 owner-card 风格的 patch 卡输入，不走 slash 参数
- patch 卡只展示命中片段和前后少量上下文，不展示整段全文
- 多个候选点在同一张卡里统一确认、统一提交，不支持部分跳过
- patch 不回改已经发出的旧消息，只影响后续上下文
- patch 前自动备份，失败自动回滚
- rollback 只支持最近一次 patch，且 patch 后如果已经产生新 turn，则拒绝 rollback
- busy instance 直接拒绝，不允许排队
- 只有高权限用户可以发起，且只有发起者本人可以继续确认/回滚
- VS Code 入口和 VS Code surface 在 V1 明确不支持

## 3. 当前 upstream 存储现实

这部分是 `#464` 最重要的技术前提。

### 3.1 本机安装实例

当前机器安装的 `@openai/codex` 版本是 `0.125.0`。

本机 `~/.codex/state_5.sqlite` 当前可见表包括：

- `threads`
- `thread_dynamic_tools`
- `stage1_outputs`
- `jobs`
- `backfill_state`
- `agent_jobs`
- `agent_job_items`
- `thread_spawn_edges`
- `remote_control_enrollments`
- `device_key_bindings`

其中：

- `threads` 表只保存 thread metadata，例如 `rollout_path`、`cwd`、`title`、`first_user_message`、模型信息、时间戳等
- `stage1_outputs` 保存 `raw_memory` / `rollout_summary` 这类派生数据，不是完整 transcript
- 当前本机 SQLite 中没有单独的 turn / transcript 正文表

### 3.2 upstream 官方仓库

本次核对的 upstream 仓库 HEAD 为 `5591912f0bf176257f71b3efbd37ee4479dfdfaf`。

关键事实：

- `codex-rs/state/src/lib.rs` 直接写明：state crate 的职责是“从 JSONL rollout 提取 metadata，并镜像到本地 SQLite”
- `codex-rs/state/migrations/0001_threads.sql` 以及后续 migration，没有出现 transcript / turn body 持久化表
- `codex-rs/state/src/extract.rs` 里 `ResponseItem` 当前不参与 thread metadata 提取
- `codex-rs/thread-store/src/local/read_thread.rs` 在读取 metadata 时可以优先走 SQLite，但 `include_history` 仍然回到 rollout 文件路径去加载历史
- `codex-rs/app-server/src/codex_message_processor.rs` 在 `includeTurns` 和 `thread/turns/list` 路径上，仍然调用 `read_rollout_items_from_rollout(...)`
- 同一文件里还有明确注释：当前每次 `thread/turns/list` 都会重放整个 rollout，直到未来 turn metadata 被单独索引
- `codex-rs/tui/src/lib.rs` 和 `codex-rs/core/tests/suite/rollout_list_find.rs` 的测试表明：部分 metadata 读取和 `thread_id -> rollout_path` 解析已经会优先信任 SQLite

### 3.3 对 `#464` 的直接结论

当前正确结论是：

- thread / turn 正文的权威来源仍然是 rollout JSONL
- SQLite 当前不是 transcript 的第二份真副本
- 但 SQLite 已经不是可完全忽略的旁路缓存，因为它已经参与了路径解析和部分 metadata 读取

所以 `#464` 不能走两个极端：

- 不能假设“只改 SQLite 就够了”
- 也不能把代码写死成“未来永远只有 JSONL”

V1 正确策略应该是：

- 以 rollout JSONL 作为正文 patch 的唯一权威写点
- 给 SQLite 留一个 metadata reconcile hook
- 用存储抽象把“当前 JSONL-only content write”与“未来可能出现的 secondary persistence”隔开

## 4. V1 技术原则

## 4.1 事务优先于热改

V1 不是做本地文件编辑器，而是做一段受控事务：

1. 校验当前实例空闲
2. 冻结 surface dispatch
3. 备份当前持久化状态
4. 执行 patch
5. 恢复 child / thread 上下文
6. 释放冻结并给出单条提示

## 4.2 存储优先于 UI

patch 卡只负责收集 replacement text，不负责决定底层怎么写。

真正的事务边界应该落在一个独立的 patch coordinator 里，而不是散落在 Feishu 卡片回调里。

## 4.3 运行态刷新采用唯一方案

V1 不保留热刷新分支。

唯一接受的运行态方案是：

- `pause surface dispatch`
- `stop child`
- `rewrite rollout`
- `restart child`
- `thread/resume`
- `resume surface dispatch`

原因很直接：

- 当前 child 可能已经把旧 turn 装进内存
- 仅改文件不保证后续上下文一定切到新内容
- 现有仓库已经具备 child restart + `thread/resume` 的恢复链路

这个决定的理由很直接：

- 当前 child 可能已经把旧 turn 装进内存
- 没有可靠证据证明 Codex 进程会从盘上重新加载这段已变更内容
- 仅改文件不保证后续上下文一定切到新内容
- 现有仓库已经具备 child restart + `thread/resume` 的恢复链路

因此 V1 不再把“无重启热刷新”当成待讨论分支。

## 4.4 生命周期可见性策略

虽然运行态必须重启，但这个过程默认应对上层静默。

V1 的可见性策略应固定为：

- patch 事务期间允许内部进入维护窗口
- 对 daemon / wrapper / child 的 stop / start / resume，不默认翻译成用户可见的 offline / online 提示
- 正常路径下，上层只看到一次 patch 成功结果
- 只有 patch、回滚或恢复链路失败时，才显式暴露异常

换句话说，重启是事实，但“让上层感觉实例掉线又上线一遍”不是 V1 允许的产品行为。

## 4.5 V1 明确排除 VS Code

VS Code surface 的 thread 语义、本地缓存和用户可见行为目前未完成调研。

因此 V1 必须在入口和事务前双重拒绝：

- 当前入口来自 VS Code
- 当前 surface 处于 VS Code 模式

## 5. 推荐架构切面

## 5.1 Patch Flow Runtime

新增一套和 upgrade owner-card 类似的 patch flow runtime，负责：

- 记录 flow ID、owner user、surface、thread、turn
- 保存候选点摘要和默认模板
- 记录卡片 message ID / tracking key
- 控制前台卡的过期、封口和所有权

这层只处理交互状态，不处理底层 patch。

## 5.2 Patch Coordinator

新增一层 daemon 侧 patch coordinator，负责：

- 统一 preflight
- surface dispatch freeze / resume
- backup / apply / rollback
- child restart + `thread/resume`
- 成功或失败提示

建议它成为 V1 的唯一事务入口。

## 5.3 Storage Adapter

新增一个独立的 storage adapter，例如：

```go
type ThreadPatchStorage interface {
    ResolveThreadTarget(threadID string) (PatchThreadTarget, error)
    PreviewLatestAssistantTurn(target PatchThreadTarget) (PatchPreview, error)
    ApplyLatestTurnPatch(req ApplyLatestTurnPatchRequest) (PatchApplyResult, error)
    RollbackLatestTurnPatch(req RollbackLatestTurnPatchRequest) (PatchRollbackResult, error)
}
```

当前实现只需要一个 `codexRolloutPatchStorage`，但接口不要写成只会改 JSONL。

## 5.4 Rollback Ledger

为“只允许最近一次 rollback”单独保存一份事务账本，建议记录：

- `patch_id`
- `thread_id`
- `turn_id`
- `rollout_path`
- `backup_path`
- `actor_user_id`
- `surface_session_id`
- `created_at`
- `completed_at`
- `rollout_before_digest`
- `rollout_after_digest`
- `latest_turn_after_patch`
- `status`（prepared / applied / rolled_back / failed）

V1 不需要做备份浏览器，但必须能精确判断“是否还是最近一次 patch、之后是否已经产生新 turn”。

## 6. 事务时序

## 6.1 打开 patch 卡

建议流程：

1. 校验 surface 已 attached，且当前存在 selected thread
2. 拒绝 VS Code surface
3. 读取当前 thread 的最新 assistant turn 预览
4. 运行候选点检测
5. 如果未命中候选点，直接返回明确提示
6. 命中后打开前台 patch 卡，逐项展示摘要和默认模板

这里还不进入实例级事务冻结。

## 6.2 确认提交

用户在卡上点击最终确认后，进入真正事务：

1. 再次校验 owner、surface、thread、turn 都未漂移
2. 再次校验 busy gate，确认实例空闲
3. `PauseSurfaceDispatch(surfaceID)`
4. 创建 rollout 备份和 rollback ledger
5. 执行最新 turn patch
6. 如有需要，执行 SQLite metadata reconcile hook
7. 触发 child restart
8. 复用现有 `thread/resume` 自动恢复
9. 成功后 `ResumeSurfaceDispatch(surfaceID, successNotice)`
10. 失败则回滚并恢复可继续使用状态

补充要求：

- coordinator 负责压住内部 restart 生命周期噪音
- 成功路径只发 patch 成功提示，不额外发 child offline / online 类 notice
- 失败路径才允许提升为可见异常

## 6.3 rollback

rollback 也是同级高风险事务：

1. 只允许 patch 发起者本人触发
2. 只允许命中该 thread 最近一次已完成 patch
3. 如果 patch 后已经产生新 turn，直接拒绝
4. 进入与 apply 相同级别的 busy gate 和 dispatch freeze
5. 恢复 rollout 备份
6. 执行 child restart + `thread/resume`
7. 标记 ledger 为 `rolled_back`

## 7. 代码切入点

基于当前代码结构，V1 建议从这些位置切：

### 7.1 control / frontstage

- `internal/core/control/types.go`
  - 新增 patch 相关 `ActionKind` / `DaemonCommandKind`
- `internal/core/frontstagecontract/callback_payload.go`
  - 新增 patch owner-flow payload
- `internal/core/control/feishu_commands.go`
  - 增加 slash / menu 入口定义

### 7.2 orchestrator

- `internal/core/orchestrator/service_dispatch_control.go`
  - 复用 `PauseSurfaceDispatch` / `ResumeSurfaceDispatch`
- `internal/core/orchestrator/service_ui_runtime.go`
  - 增加 active patch flow runtime
- 新增 patch owner-card 视图与回调处理
  - 参考现有 plan proposal / thread history / target picker 交互模式

### 7.3 daemon

- `internal/app/daemon/*`
  - 新增 patch command ingress
- 新增 bendtomywill apply / rollback handler
  - 串起 preflight、storage adapter、restart、notice

### 7.4 wrapper / codex translator

- `internal/app/wrapper/app_child_session.go`
- `internal/adapter/codex/translator_restart_restore.go`

V1 建议复用现有 child restart 和 `thread/resume`，不新增原生协议改造。

### 7.5 codexstate

- `internal/codexstate/*`
  - 新增 canonical rollout path 解析
  - 新增 rollout backup / rewrite / rollback
  - 新增 rollback ledger 持久化
  - 新增 metadata reconcile hook

## 8. 存储实现建议

## 8.1 先做 canonical rollout resolver

第一步不要急着写 patch，而是先收敛“当前 thread 对应哪个 rollout 文件”。

推荐策略：

1. 优先用 SQLite 中 thread metadata 提供的 `rollout_path`
2. 路径不存在或校验失败时，再回退文件系统查找
3. 在真正写入前，再次验证该 rollout 仍然对应当前 thread

这与 upstream 当前“metadata 可 SQLite-first、history 仍回 rollout”的行为一致。

## 8.2 patch 只改权威正文

V1 只应该改会影响后续 thread 上下文恢复的正文存储。

当前推荐：

- 改 rollout JSONL 中属于最新 assistant turn 的正文项
- 如该 turn 存在明确冗余镜像项，也在同一事务里同步
- 对当前并不存在正文语义的 SQLite 表，不做伪双写

## 8.3 reconcile hook 先做成 no-op 也可以

因为当前 latest assistant turn patch 不会改 `first_user_message`、`title` 这类 metadata，所以 SQLite reconcile 很可能在 V1 初版里是 no-op。

但 hook 仍然值得保留，因为：

- upstream 已经在一些路径上 SQLite-first
- 未来如果增加 turn metadata index，这个点可以扩成真实同步

## 9. 候选点检测建议

V1 不建议做泛化“任何看起来不满意的文本都能 patch”。

建议首版只覆盖显式模式：

- refusal / policy block 类固定文案
- 明显占位、空壳或“无法继续”类固定文案
- 仓库内明确列出的默认模板映射

如果最新 assistant turn 没有命中这些模式，V1 直接返回“不支持当前内容类型”，而不是退化成通用全文编辑器。

## 10. 阶段计划

## 10.1 Phase 1: Storage Foundation

先完成不涉及 Feishu 交互的底座：

- canonical rollout resolver
- latest turn preview reader
- backup / rollback ledger
- rollout patch / restore 单测
- SQLite compatibility no-op hook

完成标志：

- 能在本地对一个指定 thread 的最新 assistant turn 做离线 apply / rollback
- 单测覆盖“路径漂移、turn 漂移、无候选点、digest 不匹配”

## 10.2 Phase 2: Owner-Card Flow

再补交互层：

- slash / menu 同源入口
- patch flow runtime
- 前台 patch 卡渲染
- owner / expiry / seal 约束
- VS Code 入口拒绝

完成标志：

- 能在 Feishu 侧稳定打开同一张 patch 卡
- 多候选点输入、确认、取消、过期路径都可验证

## 10.3 Phase 3: Runtime Transaction

最后接事务编排：

- busy gate
- dispatch freeze / resume
- daemon patch coordinator
- child restart + `thread/resume`
- apply / rollback 成功失败路径

完成标志：

- patch 后下一次输入继续落在同一 thread
- 失败自动回滚且 surface 不残留半死状态

## 11. 当前状态

当前计划层已经没有剩余的运行态路线分歧。

现阶段可以直接按下列顺序进入实现：

1. storage foundation
2. owner-card flow
3. runtime transaction

如果后续真要研究热刷新，那只能作为 V2 之后的替代实验，不属于本单的计划前提。

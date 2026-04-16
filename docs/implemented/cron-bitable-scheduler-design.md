# Cron 多维表格与 owner-bound 调度设计

> Type: `implemented`
> Updated: `2026-04-16`
> Summary: 把 `#23`、`#224` 与 `#239` 的最终实现结论收敛为当前 `/cron` source of truth，记录 owner-bound 配置表、工作区/ Git 双来源任务、scheduler 的 hidden run 物化策略，以及冻结写回与内部运行目录隐藏语义。

## 1. 文档定位

这份文档描述的是仓库里 **已经落地** 的 Cron 能力，而不是 issue 讨论过程的摘录。

它回答五件事：

1. 当前 daemon 如何把 Cron 配置表绑定为实例级、owner-bound 的飞书资源。
2. `/cron`、`/cron repair`、`/cron reload`、`/cron migrate-owner` 现在各自负责什么。
3. 多 bot / 多 surface 下，后台读写为什么始终走 resolved owner bot，而不是当前发命令的 surface bot。
4. scheduler 与 hidden run 为什么要在启动时冻结写回目标。
5. 当前实现明确不做哪些事情。

相关 source of truth：

- [README.md](../../README.md)
- [QUICKSTART.md](../../QUICKSTART.md)
- [remote-surface-state-machine.md](../general/remote-surface-state-machine.md)

## 2. 当前实现概览

当前 `/cron` 已经收敛成“**实例级 Cron 多维表格 + owner-bound 后台管理 + daemon 本地 scheduler + hidden run 结果回写**”这条产品路径：

1. 每个 daemon 安装实例只绑定一份专属 Cron 多维表格，不与其他实例共用。
2. 这份多维表格有明确的 owner bot；后台 repair / reload / scheduler / writeback 都按 resolved owner bot 访问它。
3. 普通 `/cron` 现在是 **只读状态查看**，不再顺手 repair、sync workspace 或补权限。
4. `/cron repair` 才是显式修表入口：负责初始化或修复表结构、同步工作区清单，并 best-effort 给当前 surface 用户补编辑权限。
5. `/cron reload` 负责按 owner bot 重新加载任务配置，更新本地 job state。
6. `/cron migrate-owner` 负责显式把 owner 切换到当前 surface 对应 bot，并复制工作区、任务与运行记录。
7. `任务配置` 现在支持两类来源：`工作区` 与 `Git 仓库`；Git 来源只要求一列 `Git 仓库引用`，reload 时再解析成规范化 `repoURL + ref`。
8. scheduler 常驻扫描已加载任务，只支持 `每天定时` 与 `间隔运行`；每次触发都会启动一个 fresh hidden run。
9. Git 来源的 hidden run 采用 `mirror cache + detached worktree`：缓存长期保留在 `StateDir/cron-repos/cache/...`，每次运行目录落在 `StateDir/cron-repos/runs/<run-id>/worktree`，结束后清理 worktree。
10. hidden run 在 launch 时就冻结 writeback target，因此后续 surface 操作或 owner 迁移不会改道已经启动的运行结果写回。
11. Cron Git run 的内部目录不会进入普通 workspace / recent thread 体系；运行时和 SQLite 补全链路都会显式隐藏 `cron-repos/runs/` 前缀。

这意味着当前实现已经不是“将来可能做的后台任务设想”，而是仓库里的正式 daemon 能力。

## 3. 配置表绑定与 owner 模型

### 3.1 本地状态

当前 daemon 会在自己的 `StateDir` 下维护 `cron-state.json`，至少持久化以下信息：

1. `instance_scope_key` 与 `instance_label`
2. `ownerGatewayID`、`ownerAppID`、`ownerBoundAt`
3. legacy `gateway_id`（仅保留给历史状态迁移，不再作为正常路径的 owner 写入源）
4. 多维表格 app token / URL / 各表 table id / meta record id
5. 已加载任务列表、最近一次 reload 时间与摘要
6. 最近一次工作区同步时间

实例作用域主键优先复用安装态 `InstanceID`；如果缺失，才退化为 `config/state path` 的稳定 hash。这样 stable / beta / master 之类并行安装不会串到同一张表。

### 3.2 远端表结构

当前远端多维表格固定维护 4 张表：

1. `任务配置`
2. `运行记录`
3. `工作区清单`
4. `元信息`

其中 `元信息` 当前会镜像：

- `instance_scope_key`
- `instance_label`
- `created_at`
- `owner_gateway_id`
- `owner_app_id`
- `owner_bound_at`

这份镜像用于校验与修复，不承担“owner 丢失后的跨 app 自动发现”。真正的恢复入口仍然是本地状态。

### 3.3 任务来源模型

`任务配置` 表当前已经是双来源模型：

1. `来源类型=工作区`
   - 使用 `工作区` 关联字段
   - 行为与旧版兼容
2. `来源类型=Git 仓库`
   - 使用 `Git 仓库引用`
   - 支持 clone URL、GitHub/GitLab tree URL，以及 `#ref=<name>` 附加语法
   - reload 时解析并规范化为 `GitRepoURL + GitRef`

这意味着 Cron 不再要求“必须先手工准备一个用户工作区”才能调度仓库任务。

### 3.4 当前 owner 解析语义

当前实现把 Cron 配置表视为 **单实例、单 owner、可显式迁移** 的资源，owner 解析分成几类稳态：

1. `healthy`
   - 已有正式 `ownerGatewayID + ownerAppID`
   - 运行时仍能找到这个 gateway
   - 当前 runtime 配置里的 AppID 与持久化 ownerAppID 一致
2. `bootstrap`
   - 当前实例还没初始化 Cron 表
   - `/cron repair` 可用当前 surface 对应 bot 作为 bootstrap owner 创建新表
3. `legacy`
   - 历史状态里只有旧 `gateway_id`
   - 当前仍能确认该 gateway 存在，因此 repair / reload 成功后会回填正式 owner 字段
4. `unavailable`
   - 已绑定 owner bot 当前不在运行时配置中，或 secret 不可用
5. `mismatch`
   - gateway 还在，但运行时 AppID 与持久化 ownerAppID 不一致
6. `unresolved`
   - 只有 legacy 线索，但当前无法安全确认它是否还是正确 owner

`/cron` 会把这些状态展示成可读摘要与下一步建议；repair / reload / migrate-owner 不会对 `unavailable` / `mismatch` / `unresolved` 做 silent fallback。

## 4. 命令语义

### 4.1 `/cron`

`/cron` 现在是 **纯查看入口**，只负责返回当前实例的状态卡：

1. 当前绑定实例与配置表链接
2. 最近一次工作区同步时间
3. 最近一次 reload 时间与摘要
4. 当前 owner 健康状态、细节与下一步建议
5. 快捷按钮：`/cron repair`、`/cron reload`、`/cron migrate-owner`

这条路径当前不会：

- 创建或修复配置表
- 同步工作区清单
- 给当前用户补权限
- 占用 `cronSyncInFlight` mutating gate
- 重写 owner 状态

也就是说，用户只是“看一下当前 Cron 状态”时，不会再顺手触发重型修表路径，更不会污染 owner。

### 4.2 `/cron repair`

`/cron repair` 是显式重操作入口：

1. 若实例尚未绑定 Cron 表，则用当前 surface 对应 bot bootstrap 新表并绑定 owner。
2. 若已有 healthy owner，则始终按 owner bot 执行修表。
3. 若只有 healthy legacy `gateway_id`，repair 成功后会回填正式 owner 字段。
4. repair 过程中会确保 app / tables / schema / meta 记录齐全。
5. repair 完成后会同步 `工作区清单` 表。
6. 当前 surface 用户的编辑权限补齐是 best-effort；失败会以 warning 形式附在结果里，而不是把 repair 整体打成失败。

repair 的关键约束是：**普通访问不会覆写 owner**。只有 bootstrap 或历史 legacy 回填，才会写入正式 owner 绑定。

### 4.3 `/cron reload`

`/cron reload` 负责按 owner bot 重新读取 `任务配置` 表并刷新本地 job state：

1. 读取 `工作区清单` 索引与 `任务配置` 记录。
2. 按 `来源类型` 解析任务：
   - `工作区` 来源要求且只允许选择一个工作区
   - `Git 仓库` 来源要求提供合法 `Git 仓库引用`
3. 兼容当前 schema 与保留的 legacy 字段形态。
4. 非法行会被汇总到 reload 摘要里，但不会拖垮其他合法任务。
5. reload 成功后覆盖本地 `Jobs`、刷新 `LastReloadAt` / `LastReloadSummary`。
6. 若当前还是可确认的 legacy owner，reload 成功后同样会回填正式 owner 字段。

reload 不依赖“当前发命令的人”补权限成功；它关心的是 owner bot 能否读到正确的 Cron 表。

### 4.4 `/cron migrate-owner`

`/cron migrate-owner` 是显式 owner cutover 入口，只能从目标 bot 对应 surface 发起：

1. 先解析当前 owner，并确认目标 surface 对应的新 owner bot。
2. 若当前已经是同一个 owner，则直接返回无需迁移。
3. 若仍有运行中的 Cron 任务，则拒绝迁移，避免 in-flight run 与迁移过程互相打架。
4. 由 old owner 读取旧表中的工作区、任务和运行记录。
5. 由 new owner 创建并修复一份新表，然后复制工作区、任务与运行记录。
6. 重新按新表执行一次 reload，把新 binding、owner 与 jobs 一次性写回本地状态。
7. 当前 surface 用户的编辑权限补齐仍然是 best-effort warning。

当前实现是“显式复制并 cutover 到新表”，而不是在原表上偷偷改一个 owner 字段了事。

## 5. 调度、hidden run 与冻结写回

### 5.1 调度模型

daemon 当前按固定 tick 扫描任务，并把触发模型收敛为：

1. `每天定时`
2. `间隔运行`

同一任务若上一轮仍在运行，本轮会直接写一条 `skipped` 结果，并沿用 V1 固定规则跳过，不会排队，也不会并发再起第二个实例。

### 5.2 hidden run 执行链

每次 Cron 触发都会启动一个 daemon-owned headless 实例，并带 `inst-cron-` 前缀的实例 id：

1. hidden 实例连回后，daemon 立即发送 `prompt.send`
2. 目标 command 固定带 `CreateThreadIfMissing=true` 与 `InternalHelper=true`
3. `工作区` 来源直接复用目标工作区；`Git 仓库` 来源则先在 `cron-repos/cache/` 更新 mirror cache，再物化出本次运行的 detached worktree
4. hidden 实例环境显式带 `CODEX_REMOTE_INSTANCE_SOURCE=cron`
5. 这类实例不进入普通 `/list` / `/attach` 用户流，也不复用用户前台正在操作的可见会话

### 5.3 内部运行目录隐藏

Git Cron run 的真实工作目录属于 daemon 内部状态，而不是用户工作区对象，因此当前实现额外做了两层隐藏：

1. runtime 侧把这类实例标记为 `source=cron`，避免进入普通 workspace identity / attach 语义
2. 持久化补全侧在 Codex SQLite 查询里统一排除 `.../cron-repos/runs/...` 前缀

因此这类 run 即使在 Codex 本地状态里留下 thread 记录，也不会重新漏回 `/list`、recent workspace 或 thread 补全链路。

### 5.4 冻结写回目标

当前实现最关键的稳定性约束之一，是 **run 启动时就冻结 writeback target**：

1. scheduler 启动 hidden run 前，会从当前 resolved owner state 截取一次 `cronWritebackTarget`
2. run 完成时优先使用这份 frozen target 回写 `运行记录` 与 `任务配置`
3. 只有 frozen target 缺失时，才会退回当下全局状态的 snapshot
4. `recordCronImmediateResultLocked(...)` 这类立即失败 / 跳过写回，也统一按当前 resolved owner snapshot 执行

因此即使：

- B surface 在 A owner 的任务运行中执行 `/cron`
- 用户稍后又执行 `/cron repair` 或 `/cron migrate-owner`

已经启动的那条 run 仍会按启动时冻结的 owner target 完成写回，不会被后来的 surface 操作改道。

## 6. 当前边界

以下能力当前 **没有** 一起做：

1. 不支持原始 cron 表达式、webhook 触发或文件变化触发。
2. 不支持长期复用同一 hidden thread；每轮都是 fresh hidden run。
3. 不支持用户可配置的并发策略；当前固定为“运行中则跳过”。
4. 不在 `/cron` / `/cron repair` / `/cron reload` 中隐式执行 owner migration。
5. 不因为 owner 缺失就 silent fallback 到当前 surface bot。
6. 不把远端 meta 当成 owner 丢失后的跨 app 自动发现机制。
7. 不把结果额外投递到飞书私聊、邮箱或其他收件箱；正式结果面仍是同一份多维表格。

如果后续要扩到更多 trigger 类型、失败重试、额外投递面或更复杂的 owner 管理，应作为 follow-up 能力单独演进，而不是误判成当前已实现范围。

## 7. 验证与相关实现

相关 issue：

- GitHub issue: `#23`
- GitHub issue: `#224`
- GitHub issue: `#239`

关键实现：

- owner 绑定、legacy 迁移与本地状态：
  - [app_cron_state.go](../../internal/app/daemon/app_cron_state.go)
  - [app_cron_owner.go](../../internal/app/daemon/app_cron_owner.go)
- 任务来源模型、Git source 解析与运行物化：
  - [app_cron_source.go](../../internal/app/daemon/app_cron_source.go)
  - [app_cron_reload_result.go](../../internal/app/daemon/app_cron_reload_result.go)
  - [app_cron_launch.go](../../internal/app/daemon/app_cron_launch.go)
  - [source.go](../../internal/app/cronrepo/source.go)
  - [manager.go](../../internal/app/cronrepo/manager.go)
- repair / reload / migrate-owner 与远端表修复：
  - [app_cron_bitable.go](../../internal/app/daemon/app_cron_bitable.go)
  - [app_cron_migration.go](../../internal/app/daemon/app_cron_migration.go)
  - [app_cron_commands.go](../../internal/app/daemon/app_cron_commands.go)
  - [app_cron_ui.go](../../internal/app/daemon/app_cron_ui.go)
- scheduler、hidden run 生命周期与冻结写回：
  - [app_cron_scheduler.go](../../internal/app/daemon/app_cron_scheduler.go)
  - [app_cron_runtime.go](../../internal/app/daemon/app_cron_runtime.go)
  - [sqlite_threads.go](../../internal/codexstate/sqlite_threads.go)

本轮关闭 issue 时明确覆盖过的验证包括：

- `bash scripts/check/go-file-length.sh`
- `go test ./internal/app/cronrepo ./internal/codexstate ./internal/app/daemon -count=1`
- `go test ./internal/app/daemon -count=1`
- `go test ./...`
- daemon / cronrepo / codexstate 级测试覆盖 Git source 解析、mirror+worktree 物化、Cron hidden run 的 `source=cron` 标记、运行记录来源摘要写回、以及 SQLite recent thread/workspace 对 `cron-repos/runs/` 的过滤

# Cron 多维表格与 owner-bound 调度设计

> Type: `implemented`
> Updated: `2026-04-17`
> Summary: 把 `#23`、`#224`、`#239`、`#243` 与 `#252` 的最终实现结论收敛为当前 `/cron` source of truth，补齐 owner-bound 配置表、工作区/ Git 双来源任务、Cron 命令入口拆分、repair 接管语义，以及 scheduler 的 hidden run 物化策略。

## 1. 文档定位

这份文档描述的是仓库里 **已经落地** 的 Cron 能力，而不是 issue 讨论过程的摘录。

它回答五件事：

1. 当前 daemon 如何把 Cron 配置表绑定为实例级、owner-bound 的飞书资源。
2. `/cron`、`/cron status`、`/cron list`、`/cron edit`、`/cron repair`、`/cron reload` 现在各自负责什么。
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
3. 普通 `/cron` 现在是 **纯导航入口**，只负责列出下一级命令，而不是直接承载完整状态页。
4. `/cron status` 显示实例级 Cron 状态与统计；`/cron list` 显示当前已加载的有效任务；`/cron edit` 给出表格入口。
5. `/cron repair` 是统一维护入口：负责初始化、修 schema、同步工作区，并在绑定失效时自动由当前 bot 接管 Cron 配置。
6. `/cron reload` 负责按 owner bot 重新加载任务配置，更新本地 job state。
7. `任务配置` 现在支持两类来源：`工作区` 与 `Git 仓库`；Git 来源只要求一列 `Git 仓库引用`，reload 时再解析成规范化 `repoURL + ref`。
8. scheduler 常驻扫描已加载任务，只支持 `每天定时` 与 `间隔运行`；每次触发都会启动一个 fresh hidden run。
9. Git 来源的 hidden run 采用 `mirror cache + detached worktree`：缓存长期保留在 `StateDir/cron-repos/cache/...`，每次运行目录落在 `StateDir/cron-repos/runs/<run-id>/worktree`，结束后清理 worktree。
10. hidden run 在 launch 时就冻结 writeback target，因此后续 surface 操作或 repair 接管不会改道已经启动的运行结果写回。
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

当前实现把 Cron 配置表视为 **单实例、单 owner、可在 repair 中被显式接管** 的资源，owner 解析分成几类稳态：

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

当前用户面不会直接高频暴露这些底层 owner 名词；它们主要用于决定 `/cron repair` 是否进入“接管 Cron 配置”分支。

## 4. 命令语义

### 4.1 `/cron`

`/cron` 现在是 **纯导航入口**，负责列出下一级命令：

1. `/cron status`
2. `/cron list`
3. `/cron edit`
4. `/cron reload`
5. `/cron repair`

steady state 下，菜单默认高亮不再是 `repair`；只有在待初始化、待修复或绑定失效时，`repair` 才会升为主要入口。

### 4.2 `/cron status`

`/cron status` 显示实例级 Cron 状态与摘要，重点包括：

1. 当前实例与配置表链接
2. 当前已加载任务数量
3. 最近一次工作区同步时间
4. 最近一次 reload 时间与短摘要
5. 当前整体状态与下一步建议

这里展示的是产品层状态，例如：

- `正常`
- `待初始化`
- `待修复`
- `需要修复`

而不是要求普通用户理解 owner/migrate 等底层概念。

### 4.3 `/cron list`

`/cron list` 显示当前已加载且仍被视为有效的任务，重点信息包括：

1. 任务名
2. 调度方式
3. 大致下次执行时间
4. 来源摘要（工作区 / Git 仓库）

若当前 Cron 绑定已经失效，则不会把旧绑定下的本地缓存任务伪装成“当前有效任务”，而是先要求执行 `/cron repair`。

### 4.4 `/cron edit`

`/cron edit` 给出正式的 Cron 表格编辑入口，并提示：

1. 直接打开 `任务配置` / `工作区清单`
2. 编辑完成后执行 `/cron reload` 生效
3. 若当前绑定异常，则先执行 `/cron repair`

### 4.5 `/cron repair`

`/cron repair` 是统一维护入口：

1. 若实例尚未绑定 Cron 表，则用当前 surface 对应 bot bootstrap 新表并绑定 owner。
2. 若已有 healthy owner，则始终按 owner bot 执行修表。
3. 若只有 healthy legacy `gateway_id`，repair 成功后会回填正式 owner 字段。
4. 若当前绑定处于 `unavailable` / `mismatch` / `unresolved`，repair 会直接进入“接管 Cron 配置”分支，由当前 bot 新建并接管一份新的 Cron 表。
5. repair 过程中会确保 app / tables / schema / meta 记录齐全。
6. repair 完成后会同步 `工作区清单` 表。
7. 当前 surface 用户的编辑权限补齐是 best-effort；失败会以 warning 形式附在结果里，而不是把 repair 整体打成失败。
8. 接管分支不会保留旧绑定下的本地已加载任务；旧表数据也不会自动迁移。若需要保留旧配置或历史，需先恢复原 owner 环境。

repair 的关键约束是：普通查看不会悄悄改写 owner；只有用户显式执行 `/cron repair` 时，才会发生初始化、回填或接管。

### 4.6 `/cron reload`

`/cron reload` 负责按 owner bot 重新读取 `任务配置` 表并刷新本地 job state：

1. 读取 `工作区清单` 索引与 `任务配置` 记录。
2. 按 `来源类型` 解析任务：
   - `工作区` 来源要求且只允许选择一个工作区
   - `Git 仓库` 来源要求提供合法 `Git 仓库引用`
3. 兼容当前 schema 与保留的 legacy 字段形态。
4. reload 内部结果会按 `loaded`、`disabled`、`stopped`、`errors` 四类结构化归档，而不是只拼一段摘要字符串：
   - `loaded` 表示这次继续生效或新加载的任务，并附任务名、调度方式与大致触发时机
   - `disabled` 表示表里显式停用的任务
   - `stopped` 表示这次 reload 后不再继续生效的旧任务，并保留停用、删除或配置失效等原因
   - `errors` 表示读取或解析失败的记录，不会拖垮其他合法任务
5. 错误定位会尽量同时保留表名、读取顺序行号、字段名与 `record_id`；其中“第 N 行”只是本次读取顺序上的近似定位，不伪装成 bitable 原生行号。
6. reload 完成反馈会按上述分组生成可核对的结果卡；但持久化到本地状态的 `LastReloadSummary` 仍只保留短摘要，避免 `/cron` 状态页退化成长日志。
7. reload 成功后覆盖本地 `Jobs`、刷新 `LastReloadAt` / `LastReloadSummary`。
8. 若当前还是可确认的 legacy owner，reload 成功后同样会回填正式 owner 字段。

reload 不依赖“当前发命令的人”补权限成功；它关心的是当前有效 owner bot 能否读到正确的 Cron 表。

### 4.7 内部迁移 helper

仓库内部仍保留一条 owner 复制/迁移 helper，用于在 old owner 仍可访问时把旧表数据完整复制到新 owner 下。

但它已经不再作为普通用户可见 Cron 命令暴露，也不再是推荐的日常产品工作流。普通用户面对 owner 绑定异常时，应直接使用 `/cron repair`。

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
- 用户稍后又执行 `/cron repair`

已经启动的那条 run 仍会按启动时冻结的 owner target 完成写回，不会被后来的 surface 操作改道。

## 6. 当前边界

以下能力当前 **没有** 一起做：

1. 不支持原始 cron 表达式、webhook 触发或文件变化触发。
2. 不支持长期复用同一 hidden thread；每轮都是 fresh hidden run。
3. 不支持用户可配置的并发策略；当前固定为“运行中则跳过”。
4. 不在普通查看路径里隐式执行 owner 迁移或 owner 接管；接管只发生在用户显式执行 `/cron repair` 时。
5. 不因为 owner 缺失就 silent fallback 到当前 surface bot。
6. 不把远端 meta 当成 owner 丢失后的跨 app 自动发现机制。
7. 不把结果额外投递到飞书私聊、邮箱或其他收件箱；正式结果面仍是同一份多维表格。

如果后续要扩到更多 trigger 类型、失败重试、额外投递面或更复杂的 owner 管理，应作为 follow-up 能力单独演进，而不是误判成当前已实现范围。

## 7. 验证与相关实现

相关 issue：

- GitHub issue: `#23`
- GitHub issue: `#224`
- GitHub issue: `#239`
- GitHub issue: `#243`
- GitHub issue: `#252`

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
- repair / reload / 接管与远端表修复：
  - [app_cron_bitable.go](../../internal/app/daemon/app_cron_bitable.go)
  - [app_cron_repair.go](../../internal/app/daemon/app_cron_repair.go)
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

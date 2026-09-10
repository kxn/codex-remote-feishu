# Headless Pool 工作区会话归属判定与重命名识别修复

> Type: `implemented`
> Updated: `2026-09-10`
> Summary: 记录 managed headless pool 预热实例对会话 WorkspaceKey 污染的根因分析与修复方案，明确会话列表归属判定优先信赖真实物理 CWD，并补齐 Codex SQLite threads.name 自定义重命名读取能力。

## 1. 背景与问题现象

在多工作区使用飞书管理远程 Codex 会话时，出现以下现象：
1. **工作区内新会话在列表中不显示**：用户在特定物理工作区（如 `/home/qagent/program/svmpy`）中新开或重命名的会话，在飞书端发送 `/use` 或 `/list` 选择该工作区时，会话候选列表完全不包含该会话，甚至显示为无可用会话。
2. **会话名称未识别自定义重命名**：会话在终端 CLI 或 VS Code 中使用 `/rename` 重命名为自定义名称（如 `svmnew0910`）后，飞书端卡片顶部的标题与候选条目依然展示为创建会话时的首条输入长摘要或历史旧名称（如 `svmpy · svmpy_design_20260909`），自定义重命名完全失效。
3. **工作区路径展示偏离**：飞书表面展示的会话工作区路径偶发变为系统内部状态目录 `/home/qagent/.local/state/codex-remote`。

---

## 2. 根因分析（架构演进脱节）

该问题由两个独立的演进脱节问题交织造成：

### 2.1 Headless Pool 预热机制与会话工作区标记污染

- **旧架构（按需随起）**：
  历史版本中，headless 实例在用户发起请求时按需直接在目标物理目录（如 `/home/qagent/program/svmpy`）下冷启动。启动时实例的 `inst.WorkspaceRoot` 与该目录天然一致，内部会话继承该实例路径也不会发生错位。
- **新架构（Managed Headless Pool 预热）**：
  最新版本引入预热池以加速响应。预热实例为了提前通用待命，启动时的默认工作目录为状态目录 `~/.local/state/codex-remote`。
- **污染链路**：
  1. 预热实例启动后，立即向 Codex app-server 请求全局 `thread/list` 快照（包含各项目下的 50 个全局会话）。
  2. Codex app-server 返回的 `ThreadSnapshotRecord` 本身不携带 `WorkspaceKey`，仅携带 `CWD`。
  3. `orchestrator/service.go` 在合并 `EventThreadsSnapshot` 时，原逻辑为：
     ```go
     current.WorkspaceKey = state.ResolveWorkspaceKey(thread.WorkspaceKey, current.WorkspaceKey, inst.WorkspaceKey, inst.WorkspaceRoot)
     ```
     由于 `thread.WorkspaceKey` 为空，所有会话（包括 `svmpy` 会话）全部被无差别盖戳赋予了预热实例的当前目录：`~/.local/state/codex-remote`。
  4. 当该预热实例后续被飞书表面借用绑定到目标工作区（如 `/home/qagent/program/svmpy`）时，实例本身属性已切换，但内部缓存的 50 个会话身上的 `WorkspaceKey` 依然残留为 `~/.local/state/codex-remote`。
  5. 飞书在构建 `/use` 与 `/list` 候选会话列表时，执行 `threadBelongsToInstanceWorkspace`：
     ```go
     // 原实现：
     func threadBelongsToInstanceWorkspace(inst *state.InstanceRecord, thread *state.ThreadRecord) bool {
         return cwdBelongsToInstanceWorkspace(inst, xutil.FirstNonEmpty(threadWorkspaceKeyFromRecord(thread), thread.CWD))
     }
     ```
     它优先相信了已经被污染的 `threadWorkspaceKeyFromRecord`（即 state 目录），导致当实例工作区为 `svmpy` 时，这 50 个物理路径真实位于 `svmpy` 的会话因路径与 `svmpy` 不匹配而全部被判定为 `false`，候选列表匹配率直接降为 0/50，被全部过滤剔除。

### 2.2 Codex SQLite 缺少 `name` 自定义重命名列查询

- 早期的 Codex 版本在 SQLite `threads` 表中只存储 `title`（存放首轮对话自动生成的长文本摘要）。`codex-remote` 在 2026-04 引入 SQLite 持久化元数据合并时（`f6b84e0e`），SQL 语句固定为：
  ```sql
  SELECT id, title, cwd, updated_at, archived, model, reasoning_effort, first_user_message FROM threads
  ```
- 随着 Codex CLI 演进支持了 `/rename` 功能，自定义名称被写入了新增加的 `threads.name` 列。
- 由于 `codex-remote` 从未更新该查询，代码始终只取 `title` 列，导致自定义名称被无视，始终回退显示长摘要或旧名称。

---

## 3. 修复方案

针对上述根因，实施了三重深度对齐修复：

### 3.1 动态探测并优先读取 `threads.name` 自定义重命名

在 `internal/codexstate/sqlite_threads.go` 中：
1. 增加对 `threads` 表结构的动态探测（`PRAGMA table_info(threads)`），通过 `sync.Once` 安全缓存探测结果。
2. 若存在 `name` 列，SQL 查询自动调整为：
   ```sql
   COALESCE(NULLIF(name, ''), title) AS title
   ```
   优先提取用户自定义名称，当为空或旧数据库无 `name` 列时平滑兼容回退至 `title`。

### 3.2 阻断预热实例对异构工作区会话的 `WorkspaceKey` 污染

在 `internal/core/orchestrator/service.go` 合并 `EventThreadsSnapshot` 时：
- 当会话自身未显式指定 `WorkspaceKey` 时，检查会话真实 `thread.CWD` 是否属于当前实例根目录。
- 仅当 `cwdBelongsToInstanceWorkspace(inst, thread.CWD)` 成立时，才继承当前实例的 `inst.WorkspaceKey`；
- 若会话 `CWD` 与当前实例目录不相关（如预热池实例接收到其他项目的会话），直接以 `thread.CWD` 解析工作区键，防止通用预热实例的 state 目录强行覆盖异构会话的工作区标签。

### 3.3 列表归属与工作区解析优先信赖物理 `thread.CWD`

1. 在 `internal/core/orchestrator/service_surface_selection.go` 的 `threadBelongsToInstanceWorkspace` 中：
   ```go
   func threadBelongsToInstanceWorkspace(inst *state.InstanceRecord, thread *state.ThreadRecord) bool {
       if inst == nil || thread == nil {
           return false
       }
       return cwdBelongsToInstanceWorkspace(inst, xutil.FirstNonEmpty(thread.CWD, threadWorkspaceKeyFromRecord(thread)))
   }
   ```
   与同文件下的 `threadBelongsToInstanceWorkspaceForTarget` 保持一致，统一优先采用物理 `thread.CWD` 判定工作区归属。
2. 在 `internal/core/orchestrator/service_thread_global.go` 的 `threadWorkspaceKeyFromRecord` 中：
   - 校验已有的 `WorkspaceKey` 是否在路径层级上合法包含 `CWD`；
   - 若 `WorkspaceKey` 与 `CWD` 互相脱节（被跨实例改写），自动回退收敛至 `thread.CWD`，具备运行时自愈能力。

---

## 4. 验证与测试

1. **Codex SQLite 重命名验证**：
   - 新增 `TestSQLiteThreadCatalogPrefersRenamedNameColumn`：模拟 Codex 52+ 版本表结构，确认带有 `name` 列的重命名会话在 `RecentThreads` 和 `ThreadByID` 中均能正确返回新名称。
2. **目标选择器跨实例污染隔离验证**：
   - 新增 `TestTargetPickerSessionOptionsIncludesThreadsWithCrossInstancePollutedWorkspaceKey`：模拟预热池实例在 state 目录持有残留标记的会话快照，验证当飞书接管对应物理工作区后，目标选择器仍能正确展示并选择该物理工作区下的会话。
3. **快照工作区解析防污染验证**：
   - 新增 `TestEventThreadsSnapshotDoesNotPolluteThreadWorkspaceKeyFromUnrelatedInstance`：验证向预热实例推送异构项目的全局快照时，会话的 `WorkspaceKey` 保持物理真实路径，不再被预热池根目录污染。
4. **全量用例回归**：
   - `internal/codexstate` 与 `internal/core/orchestrator` 全部测试通过。

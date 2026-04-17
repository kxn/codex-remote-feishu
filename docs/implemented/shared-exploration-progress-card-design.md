# 共享探索过程卡设计

> Type: `implemented`
> Updated: `2026-04-17`
> Summary: 记录共用探索过程卡的已落地边界：后端输出结构化 exploration block，并支持飞书“工作中”卡底部瞬时 reasoning 状态；当前用户可见展示面为 Feishu 侧共用状态卡，Web admin 与 admin runtime API 不再承担这类运行中过程展示。

## 背景

上游 Codex CLI 在执行读文件、列目录、搜索代码这类“探索型”动作时，会把过程折叠成一组很细致的状态展示，例如：

- `Exploring` / `Explored`
- `Read foo.rs, bar.rs`
- `List src/`
- `Search compact in codex-rs/`

这套呈现有两个重要特点：

1. 它看起来像 tool 提示，但本质上是 TUI 对执行过程的二次语义化渲染，不是一个单独的 tool。
2. 它不是简单打印原始 shell 命令，而是先把命令归一成 `Read / List / Search / Unknown` 这几种语义，再决定是否进入“探索态”展示。

当前本仓库已经有共用过程状态卡链路：

- `internal/core/orchestrator/service_exec_command_progress.go`
- `internal/core/control/types.go`
- `internal/adapter/feishu/projector_exec_command_progress.go`

但现状仍以 `执行：<command>`、`工具：<summary>` 这类通用文本为主，尚未把“探索型过程”抽象成前端可稳定复用的语义块，因此：

- 用户在飞书 / Web 侧看不到接近上游的探索过程体验。
- 前端若想模仿上游，只能靠字符串猜测，稳定性很差。
- `dynamic_tool_call` 和 `command_execution` 两条链路目前也没有统一成同一套探索表达。

本文原本用于拆清上游逻辑和落地路径；当前代码已经按本文主线完成第一轮实现，因此迁移到 `docs/implemented/`，用于记录当前边界与后续扩展方向。

## 已实现状态

当前仓库已经落地这些行为：

1. `ExecCommandProgress` 增加了结构化 `Blocks`，其中 exploration block 可表达 `running / completed / failed` 与多条 `read / list / search` 行。
2. `command_execution` 会把最稳定的一批命令归一进 exploration block，包括 `cat / bat / head / tail / sed / ls / rg / grep`。
3. `dynamic_tool_call.read` 已接入同一套 exploration block，并继续沿用后端语义化而非前端猜字符串。
4. Feishu 过程卡优先渲染 exploration block，并可在卡片底部临时附着“思考中 / 规划中”这类瞬时 reasoning 状态；这类状态不会沉淀进历史进度，清理后也不会反向撤回已经发出的旧卡。
5. Feishu projector 当前按“每个可见行一个 markdown element”出站共享过程卡，而不是把整段 timeline 压成单个 markdown body，避免某一行的 inline 语法把后续行一起污染。
6. Web admin 已停止展示共享探索过程，admin runtime API 也不再额外暴露这类运行中进度，避免继续把管理页视为目标展示面。
7. 原有 `Entries` 仍保留为兼容回退，因此未进入 exploration block 的普通过程不会丢失。

当前仍然刻意保留的边界：

1. 管理页与 admin runtime API 都不再承担共享探索过程的正式展示职责；当前用户可见展示面只保留 Feishu 侧共用状态卡。
2. `dynamic_tool_call` 只有语义最明确的 `read` 进入 exploration block，其他 tool 继续走 generic progress。
3. 更广覆盖率的 shell parsing 与最近一次 exploration 摘要复用，留待后续迭代。

## 目标

1. 在共用过程状态卡里支持“探索态”分组，而不是继续只显示原始命令。
2. 让 `command_execution` 与 `dynamic_tool_call` 都可以喂给同一套探索态展示。
3. 前端拿到的是结构化语义，而不是自行猜命令字符串。
4. 在产品效果上尽量接近上游，但适配本仓库“共用状态卡”而不是直接照搬 TUI cell。

## 非目标

1. 不要求前端一比一复刻上游 TUI 的历史 cell 机制。
2. 不要求第一阶段就完整复刻上游全部 shell 解析能力。
3. 不把 `Explored / Read / List / Search` 直接当成新的 tool 类型暴露到协议表面。
4. 不在第一阶段重写 `/status` snapshot 卡的主体结构。

## 上游 Codex 行为拆解

### 1. 这不是一个独立 tool

上游用户看到的 `Explored`、`Read`、`List`、`Search`，不是 tool 调用提示名，而是执行单元（exec cell）的渲染结果。

关键位置：

- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/model.rs`
- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/render.rs`

也就是说，前端是否能出现这套体验，取决于“过程语义有没有被整理出来”，而不取决于是否开启了某个特定 tool。

### 2. 进入“探索态”的条件

上游会把一次执行视为 exploring call，当且仅当：

1. 不是 `ExecCommandSource::UserShell`
2. `parsed` 非空
3. `parsed` 里的每一项都属于：
   - `ParsedCommand::Read`
   - `ParsedCommand::ListFiles`
   - `ParsedCommand::Search`

一旦其中混入 `Unknown`，就不会按探索态展示，而是退回普通执行渲染。

对应代码：

- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/model.rs`
- `/tmp/openai-codex-src/codex-rs/protocol/src/parse_command.rs`

### 3. 探索态头部

探索态有两种头部状态：

- 仍在进行：`Exploring`
- 已完成：`Explored`

对应代码：

- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/render.rs`

注意：上游这里的“完成”不等于“整轮对话已经结束”，而是“这一组探索动作已经完成”。所以它可能会以已完成状态继续停留一段时间。

### 4. 探索态的行合并规则

上游不是逐条原样显示，而是做了专门的合并和格式化：

1. 连续纯 `Read` 会合并成一行
   - 例如：`Read foo.rs, bar.rs, baz.rs`
2. 混合类型时按语义逐行显示
   - `Read <file>`
   - `List <path-or-command>`
   - `Search <query [in path]>`
3. `Search` 优先显示 query + path；拿不到再退回原始 cmd

对应代码：

- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/render.rs`

### 5. 生命周期和“孤儿完成事件”

上游还有一个很关键、但容易被漏掉的逻辑：

1. 一个正在进行的 exploring group 可以继续吸纳后续兼容的探索调用。
2. 如果这时来了一个不属于当前 exploring group 的 exec end，上游不会拿它去覆盖当前 exploring group。
3. 这种“找不到归属但当前又有活动 exploring group 的完成事件”，会被单独插成一条历史项，避免把不同语义的过程错并到一起。

对应代码：

- `/tmp/openai-codex-src/codex-rs/tui/src/chatwidget.rs`
- `/tmp/openai-codex-src/codex-rs/tui/src/chatwidget/tests/exec_flow.rs`

这个点说明：探索态不只是“把若干文本换个显示”，还带着明显的分组生命周期语义。

### 6. 这和 sandbox 不是一回事

上游是否出现这类展示，不直接由 sandbox 决定。

- sandbox / tool config 会影响可用执行路径
- 但 `Explored / Read / List / Search` 的可见效果，核心仍然来自 exec 语义解析和 TUI 渲染

也就是说，我们当前环境里看不到它，主要不是因为关闭了 sandbox，而是因为本仓库还没有实现对应的过程语义展示层。

## 当前仓库现状

### 1. 已有共用过程卡链路

当前仓库已经有一条“共用过程卡”链路：

- translator 产出 `agentproto.Event`
- orchestrator 把过程累积成 `control.ExecCommandProgress`
- projector 渲染为用户可见的“处理中”卡片

关键代码：

- `internal/adapter/codex/translator_helpers.go`
- `internal/core/orchestrator/service_exec_command_progress.go`
- `internal/core/control/types.go`
- `internal/adapter/feishu/projector_exec_command_progress.go`

### 2. 已经具备一部分“探索”基础

当前并不是完全从零开始，至少已有这些基础：

1. `dynamic_tool_call` 已支持按 tool 合并同类项。
2. 同名动态工具可复用同一行，例如测试中 `Read a.cpp` 与 `Read b.cpp` 会合并成一行。
3. `dynamic_tool_call` 参数抽取已经会优先保留 `path / query / pattern / glob / url / name` 等对用户有意义的字段。

对应代码：

- `internal/core/orchestrator/service_exec_command_progress.go`
- `internal/core/orchestrator/service_exec_command_progress_test.go`

### 3. 当前主要缺口

真正的缺口主要有三个：

1. `command_execution` 只有原始 `command / cwd / exitCode`
   - 还没有等价于上游 `ParsedCommand` 的结构化语义
2. 当前 `ExecCommandProgress` 只有扁平 `Entries`
   - 不足以表达“一个探索块 + 块内多行 + 活动/完成状态”
3. 前端只能拿到字符串摘要
   - 很难稳定渲染出上游那种效果

对应代码：

- `internal/core/agentproto/types.go`
- `internal/adapter/codex/translator_helpers.go`
- `internal/core/control/types.go`

## 推荐产品边界

### 1. 仍然只保留一套“共用过程状态卡”

不建议为了这个需求再额外做一套新卡。

更合理的方向是：

- 继续沿用当前 `ExecCommandProgress` / “处理中”这条共用通道
- 但把它从“扁平文本列表”升级成“可表达探索块的结构化过程卡”

这样飞书、Web、后续其他前端都能共享同一套语义，而不是各自拼字符串。

### 2. 用户可见效果以“相似语义”为主，而不是逐字符复刻

建议最终对用户暴露的，是语义等价的效果，而不是硬性要求英文文案完全一致。

例如，共用状态卡里可以呈现为：

```text
处理中
探索中
读取 docs/README.md、internal/core/control/types.go
列目录 internal/core/orchestrator
搜索 compact in internal/
```

完成后更新为：

```text
处理中
已探索
读取 docs/README.md、internal/core/control/types.go
列目录 internal/core/orchestrator
搜索 compact in internal/
```

这里的关键不是中文还是英文，而是：

1. 有明确的探索块头部
2. `Read / List / Search` 是结构化行，不是原始 shell 文本
3. 连续读取会合并
4. 不相关的过程不要误并进同一个探索块

### 3. 对共享状态卡的适配方式

因为我们的前端容器是“共用状态卡”，不是上游 TUI 的 history cell，所以推荐做如下适配：

1. 卡片仍是同一张共用卡
2. 卡内支持多个 block
3. 其中一种 block 类型是 exploration block
4. exploration block 具备：
   - `active / completed / failed` 状态
   - 多条 `read / list / search` 行
   - 同类合并规则

这样即使同一时间还有其他普通过程，也不会逼着我们用“一个扁平 entry 列表”去硬凑复杂生命周期。

## 推荐语义模型

### 1. 不要让前端猜字符串

推荐在 orchestrator / control 层引入结构化 block，而不是让 projector 去从 `Summary` 文本里猜 `Read / List / Search`。

推荐方向：

- `ExecCommandProgress` 保留现有通道
- 新增可选的结构化 `Blocks`
- 第一类 block 即 `exploration`

block 至少应表达：

1. block 类型
2. block 状态
3. block 是否仍可吸收后续事件
4. block 内行列表

行至少应表达：

1. 行语义：`read / list / search / generic`
2. 主文本
3. 可选附加文本（例如 search 的 path）
4. 原始来源信息（供调试或回退）

### 2. 探索块的数据来源

推荐统一两条输入源：

#### A. `command_execution`

这是和上游最接近的一条链路，但缺少 parsed semantics。

建议：

1. 在 translator 或更靠近 orchestrator 的位置，补出“可投影的探索语义”
2. 至少先支持：
   - `read`
   - `list`
   - `search`
   - `unknown`
3. 只有全是 `read / list / search` 的一组，才进入 exploration block
4. 一旦混入 `unknown`，就退回现有 generic process block

#### B. `dynamic_tool_call`

这一条当前已经有 `Read` 合并基础，可以直接并入同一套探索模型。

建议：

1. tool 语义明确时，直接映射成探索行
   - 例如 `read` -> `read`
2. tool 语义不明确时，继续走 generic tool block
3. 第一阶段不要为了追求覆盖率而把模糊 tool 硬映射成探索语义

### 3. 兼容当前扁平 entry

因为现有 projector 还依赖 `Entries`，建议分阶段兼容：

1. 新 block 先加到 control/state
2. projector 优先渲染 block
3. 没有 block 时继续回退到原有 `Entries`

这样可以避免一次性改穿所有前端。

## 关键行为建议

### 1. 探索块的进入条件

只有在以下条件都满足时，才创建 exploration block：

1. 当前事件可被归一为 `read / list / search`
2. 同一 block 内不存在 `unknown`
3. 来源和 turn 归属一致

### 2. 连续读取合并

连续纯 `read` 行应合并为一行，尽量贴近上游：

- `Read a, b, c`
- 或中文本地化后的 `读取 a、b、c`

### 3. 混合探索行为逐行显示

如果同一探索块内既有 `read`，也有 `list / search`，则按语义分行，不要强合并成一串文本。

### 4. 不兼容事件不要覆盖当前探索块

如果当前卡里已经有 active exploration block，这时来了一个不兼容的普通过程：

1. 不能直接把当前探索块改写掉
2. 应该新增独立 block，或者进入 generic block
3. 但探索块本身仍保持自己的状态和内容

这相当于把上游“orphan history while active exec”的核心保护语义，翻译成“共享状态卡内的多 block 共存”。

### 5. 完成态可短暂停留

探索块完成后，不要立刻清空。

建议：

1. 探索块先更新为 `completed`
2. 直到后续答案稳定落地或过程卡自然结束，再整体退场

否则用户会只看到瞬时闪过的过程，体验会比上游差很多。

## 建议的分阶段实施

### 阶段 1：补语义模型

1. 为 `ExecCommandProgress` 增加 block 表达能力
2. 定义 exploration block 与 row 的最小结构
3. 保留旧 `Entries` 作为回退

### 阶段 2：先接通最确定的来源

1. `dynamic_tool_call` 先接 `read`
2. `command_execution` 先接能稳定识别的 `read / list / search`
3. ambiguous / unknown 全部继续走 generic

### 已完成阶段

1. 结构化 block 模型已落到 `control/state/orchestrator`
2. Feishu 已接上 exploration block；Web admin 的同构展示已下线
3. active / completed / failed 生命周期与兼容回退测试已补齐

### 后续可继续扩展

1. 补更多 dynamic tool 语义映射
2. 视需要把最近一次 exploration block 摘要复用到 snapshot / status 视图

## 验证参考

### 回归点

1. 连续多个 `Read` 会被合并
2. `Read + List + Search` 可在同一探索块逐行显示
3. 混入 `Unknown` 时退回 generic
4. active exploration block 不会被无关过程覆盖
5. completed exploration block 在最终答案出来前不会立刻丢失
6. Feishu 侧展示保持可用；Web admin 不再承担探索过程展示职责

### 建议测试面

- `internal/core/orchestrator/service_exec_command_progress_test.go`
- `internal/adapter/feishu/projector_exec_command_progress_test.go`
- Web 共用状态卡对应的前端测试

## 实现参考

### 本仓库关键文件

- `internal/core/orchestrator/service_exec_command_progress.go`
- `internal/core/control/types.go`
- `internal/core/state/types.go`
- `internal/adapter/codex/translator_helpers.go`
- `internal/adapter/feishu/projector_exec_command_progress.go`

### 上游参考文件

- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/model.rs`
- `/tmp/openai-codex-src/codex-rs/tui/src/exec_cell/render.rs`
- `/tmp/openai-codex-src/codex-rs/tui/src/chatwidget.rs`
- `/tmp/openai-codex-src/codex-rs/tui/src/chatwidget/tests/exec_flow.rs`
- `/tmp/openai-codex-src/codex-rs/protocol/src/parse_command.rs`

## 待讨论取舍

1. `dynamic_tool_call` 除 `read` 外，哪些 tool 能被稳定映射为 `list / search`。
2. `/status` snapshot 是否要额外展示“最近一次探索摘要”，还是只在过程卡里可见。

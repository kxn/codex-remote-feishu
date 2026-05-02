# Claude Live Projection Semantics Design

> Type: `inprogress`
> Updated: `2026-05-01`
> Summary: 固定 Claude/Cloud live projection 的产品语义基线，明确过程卡/交互卡/final message 的分工，拆分三类 Plan，并定义 Task 家族的可见性边界。

## 1. 文档定位

这份文档只回答一个问题：

- Claude/Cloud native runtime 事件，**从产品语义上**应该往 Codex 现有什么东西上面映射

它不是实现说明，也不是协议字段手册。

它的职责是先固定：

1. 用户应该看到哪些语义
2. 哪些内容只允许留在内部 metadata
3. 哪些同名对象其实是不同产品对象
4. 哪些 carrier 允许共用，哪些严格禁止混用

实现层必须服从这里的产品对象定义，而不是反过来用当前代码的偶然现状倒推产品语义。

## 2. 总体原则

### 2.1 三个用户可见面

Claude/Cloud 路径的用户可见输出，只允许落到三种面：

1. 过程卡
2. 交互卡
3. final message

除此之外，不再新增第四种“半文本半工具”的可见杂项。

### 2.2 原始结果默认不直出

以下内容默认都不直接展示给用户：

- 原始 tool result
- stdout / stderr
- 大块 JSON
- 文件正文
- 网页正文
- 子代理详细输出
- provider-native thinking 原文

这些内容可以保留在内部 metadata、history sidecar 或恢复链路里，但前端默认不渲染。

### 2.3 typed 语义成立后不得再退化成普通文本

如果某个 carrier 已经被判定为稳定产品语义，例如：

- `command_execution`
- `web_search`
- `delegated_task`

那么 turn 完成时不允许再把它回落成“工具 X 返回了 ...”这种普通文本说明。

### 2.4 request 语义与过程语义分离

以下东西不是过程项，而是交互项：

- `AskUserQuestion`
- `ExitPlanMode`
- 其他需要用户批准/回答/选择的 carrier

它们必须走 request contract，不和过程卡混排。

## 3. 可见层模型

### 3.1 过程卡

过程卡承载：

- 运行中的过程进展
- 工具调用的抽象语义
- 计划进行中的快照
- 子代理任务状态
- reasoning 的摘要态

过程卡按“每个稳定语义一行”组织。

如果一个事件只是某条已存在行的内部生命周期推进，而没有新增独立用户价值，就**不得新增新行**。

### 3.2 交互卡

交互卡承载：

- approval
- request_user_input
- 其他需要用户显式回应的交互

交互卡的正文可以引用某个过程语义对象，例如审批绑定的计划正文，但该正文的 owner 仍然属于交互流。

### 3.3 final message

final message 只承载 assistant 的最终回答正文。

它不是：

- 过程日志
- 原始工具结果
- 子代理输出镜像
- request 载体

## 4. Plan 家族严格拆分

产品上必须把名字都叫 “Plan” 的东西拆成三个独立对象。

### 4.1 `current_plan_snapshot`

来源：

- `TodoWrite`

语义：

- 当前 turn 运行中的待办/步骤快照

展示位置：

- 独立 `Plan` 卡

生命周期：

- 同一 turn 内按 latest snapshot 覆盖更新
- 只属于运行中的当前 turn

严格限制：

- 不得升级成 turn 结束后的提案计划卡
- 不得带继续执行/新上下文执行之类 turn-end 动作
- 复用现有 `turn.plan.updated` / `PlanUpdate` 展示 owner，但语义仍然不是 turn-end proposal

### 4.2 `approval_plan`

来源：

- `ExitPlanMode` 之前那段计划正文

语义：

- 一份准备执行、等待批准的计划正文

展示位置：

- 属于 approval flow

当前未定项：

- 它最终是独立显示
- 还是作为 approval card 的正文
- 还是“计划正文 + approval card”双投影

在没有看到更多真实样本前，**只固定语义归属，不固定最终排版位置**。

严格限制：

- 不得等同于 `current_plan_snapshot`
- 不得等同于 `turn_end_plan_proposal`
- 不得因为原始数据看起来像 assistant text，就自动当 final message

### 4.3 `turn_end_plan_proposal`

来源：

- 现有 Codex turn 结束后的 proposal 机制

语义：

- 一轮完成后给用户的后续执行提案

展示位置：

- 独立 turn-end plan 卡

生命周期：

- 只在 turn 完成后出现

特征：

- 可携带“继续执行 / 新上下文执行”类动作

### 4.4 三类 Plan 的禁止互转

以下互转一律禁止：

- `TodoWrite -> turn_end_plan_proposal`
- `TodoWrite -> approval_plan`
- `approval_plan -> current_plan_snapshot`
- `approval_plan -> turn_end_plan_proposal`
- `turn_end_plan_proposal -> current_plan_snapshot`

它们属于同一命名家族，但不是同一个产品对象。

## 5. Task 家族模型

### 5.1 单行聚合规则

`Task` 家族不能以三种可见事件存在，而应该是：

- 一条可见任务行
- 若干隐藏生命周期事件

建议产品对象名：

- `delegated_task`

### 5.2 可见性

用户可见：

- `Task`

用户不可见：

- `TaskOutput`
- `TaskStop`

### 5.3 生命周期

建议语义：

- `Task -> delegated_task.started`
- `TaskOutput -> delegated_task.internal_update`
- `TaskStop -> delegated_task.completed|failed`

但用户只看同一条任务行，不看内部事件本体。

### 5.4 显示内容

推荐显示：

- `Task: <description>`
- 或 `Task (<subagent_type>): <description>`

允许状态：

- running
- completed
- failed

默认不显示：

- 子代理详细输出
- 中间日志
- stop 细节
- 大段返回内容

### 5.5 当前实现状态

截至 `2026-05-01`，当前实现已经落地：

- `Task -> delegated_task`
- `TodoWrite -> current_plan_snapshot`
- `Bash -> command_execution`
- `WebSearch/WebFetch/ToolSearch -> web_search`
- `Edit/Write/NotebookEdit -> file_change`
- `Read/Glob/Grep -> dynamic_tool_call(exploration owner)`

同时已经明确：

- `TaskOutput` / `TaskStop` 继续隐藏，不单独可见
- `TaskStop` 已通过父 `Task` 的隐藏 lifecycle 驱动 `delegated_task.completed|failed`
- `TaskOutput` 已通过父 `Task` 的隐藏 delta 输入驱动同一条 `delegated_task`，但不新增可见行
- `TodoWrite` 直接复用独立 `turn.plan.updated` / `PlanUpdate` owner
- `delegated_task` 走单行过程项 owner
- `command_execution` / `web_search` / `delegated_task` / `file_change` 完成后不再默认回落成普通文本

## 6. Tool 家族的产品映射

### 6.1 正常正文

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `assistant.text` / `text_delta` | `agent_message` | final message |

### 6.2 reasoning

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `assistant.thinking` / `thinking_delta` | `reasoning_summary` | 过程卡一行 |

规则：

- `Codex` 保留现有 reasoning summary 语义：
  - 同一 `summaryIndex` 更新同一条
  - `summaryIndex` 递增时保留历史条目
  - 若上游带 markdown `**...**` 摘要，可继续按该摘要态承接
- `Claude` 不再做本地句子摘要化 / 粗体化 / 伪阶段改写
- `Claude` 默认保留过滤后的 raw thinking：
  - 仅窄清洗已知系统 side-channel info block
  - 当前名单仅限 `<claude_background_info>`、`<fast_mode_info>`
  - 清洗必须按流式 delta 可见边界进行，避免未闭合 tag 片段先露到前台
- 不做“所有尖括号标签统一剥离”的宽规则
- 这是过程状态，不是正文消息

### 6.3 命令执行

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `Bash` | `command_execution` | 过程卡一行 |

规则：

- 原始命令可以作为摘要显示
- stdout/stderr 默认不直出

### 6.4 探索型工具

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `Read` | exploration semantic | 过程卡一行 |
| `Glob` | exploration semantic | 过程卡一行 |
| `Grep` | exploration semantic | 过程卡一行 |

规则：

- 用户看到的是“读/列/搜”这类探索过程
- 不是原始 tool result
- 现阶段名字没有统一翻译时，可以先直接保留原名

### 6.5 联网工具

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `WebSearch` | `web_search` | 过程卡一行 |
| `WebFetch` | `web_search` 家族动作 | 过程卡一行 |
| `ToolSearch` | 可见过程项 | 过程卡一行 |
| `Skill` | 可见过程项 | 过程卡一行 |

规则：

- `WebSearch`、`Skill` 等都显示
- 没有稳定翻译时直接用原名

### 6.6 文件修改

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `Edit` / `Write` / `NotebookEdit` | `file_change` | 过程卡一行 |

规则：

- 三类文件修改工具统一进入现有 `file_change` owner，并贡献 turn-end file summary / history `[修改] ...`
- `Write` 的整文件 create/update payload、`NotebookEdit` 的 notebook cell 级 payload，都由同一份 file-change contract 归一化
- 不展示原始大块参数

### 6.7 计划进行中

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `TodoWrite` | `current_plan_snapshot` | 独立 `Plan` 卡 |

规则：

- 直接复用现有 Codex `turn.plan.updated` / `PlanUpdate` 展示 owner
- latest snapshot 覆盖
- 与 turn-end proposal plan 没关系

### 6.8 交互项

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| `AskUserQuestion` | `request_user_input` | 交互卡 |
| `ExitPlanMode` | `approval` + `approval_plan` | 交互卡 |

### 6.9 未知工具

| Native carrier | 产品对象 | 用户可见面 |
| --- | --- | --- |
| 其他未分类工具 | generic process item | 过程卡一行 |

规则：

- 未知工具也默认显示在过程卡中
- 但仍然不直出原始结果 payload

## 7. 去重与归并规则

### 7.1 `Task` 家族

- `TaskOutput`
- `TaskStop`

只能更新同一条 `Task` 行，不能新增新行。

### 7.2 `current_plan_snapshot`

同一 turn 内多次 `TodoWrite`：

- 只保留最新完整快照

### 7.3 `approval_plan`

若原始数据里同时出现：

- 一段 assistant 计划正文
- 一次 `ExitPlanMode` approval

则默认按同一语义对象处理，不允许再额外生成一条重复 final message。

### 7.4 typed item completed path

如果某语义已经在过程卡里稳定呈现：

- turn 完成时不得再补一条“工具 X 返回了 ...”文本来重复它

## 8. 当前未决但已收口的边界

以下问题仍允许在实现期继续确认，但产品归属已经固定：

1. `approval_plan` 最终排版位置
2. `reasoning_summary` 的 provider-aware owner cut 细节
3. `ToolSearch` 是否未来要升级成更专门语义

这些都不影响当前文档的核心结论：

- 它们是什么产品对象
- 属于哪个可见面
- 和哪些对象严禁混用

## 9. 与 `#524` 的关系

`#524` 的技术实现必须以本文为产品语义基线。

尤其是：

1. 三类 Plan 严格分开
2. `TaskOutput/TaskStop` 不可见
3. tool result 默认内部保留，不直接展示
4. typed item 不得在完成态降回普通文本

后续技术调研、计划和实现，如果发现当前代码结构不支持这套语义，应修改结构，而不是回退产品定义。

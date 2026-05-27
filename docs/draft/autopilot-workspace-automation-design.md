# Workspace Autopilot Design

> Type: `draft`
> Updated: `2026-05-27`
> Summary: 将工作区级 `Autopilot` 重定义为“由自然语言配置的运行时判断层”，统一承接系统主动抛出的结构化问题、turn 中的质量守门、turn 结束后的后续判断，以及定时触发。

## 0. 文档定位

本文替换此前把 `Autopilot` 定义为“工作区事件自动化 runtime”的主叙事。

旧定义过于围绕：

- `Autopilot.md -> load -> compiled JSON -> event-driven runtime`
- `/cron`、`autocontinue`、`autowhip` 的统一自动化框架

这条方向并非完全错误，但它没有抓住现在更有产品价值的问题：系统在运行中遇到判断点时，应如何按用户偏好自动决策、打断、续发或定时发起动作。

因此，本文把 `Autopilot` V1 收敛为：

`Autopilot = 工作区级、由自然语言配置的运行时判断层`

它的职责不是替用户完成他刚主动发起的选择，而是承接系统自己运行过程中出现的判断点。

## 1. 重新定义后的核心问题

当前系统里真正需要统一的，不是“再做一套新的规则自动化”，而是下面四类 runtime judgment：

1. 系统主动停下来问用户的问题
2. 模型在 turn 执行过程中暴露出的危险信号
3. turn 结束后系统该怎么处理
4. 到达定时触发点后系统该做什么

这些判断点的共同特征是：

1. 它们不是用户刚刚主动进入某个 picker 或配置页后产生的显式选择
2. 它们都可以被描述为“在什么上下文下，系统应该怎样判断”
3. 它们都适合由自然语言策略来描述，而不是硬编码 if/else

## 2. 产品定义

`Autopilot` 不是“自动替用户乱点 UI”。

`Autopilot` 也不是“任意事件触发任意 action 的开放式脚本系统”。

`Autopilot` 是：

1. 以工作区为边界的策略层
2. 以 `Autopilot.md` 为自然语言 source of truth
3. 通过显式 `/autopilot load` 编译成系统可执行的结构化 policy
4. 在少量明确的 runtime trigger family 上运行
5. 只允许输出少量明确的 response lane

## 3. 设计原则

### 3.1 只处理系统拥有的判断点

V1 的第一原则：

`凡是用户主动发起而导致的选择，不属于 Autopilot。`

这意味着以下场景默认不纳入：

- workspace / session / thread / commit picker
- path / directory / file picker
- `/mode` `/access` `/model` 这类 page-local 配置页
- back / root / menu 这类本地导航

原因很简单：

1. 这些动作本来就是用户自己主动进入的流程
2. 这时用户通常已经知道自己想做什么
3. 系统不应擅自猜测并替他做选择

### 3.2 V1 必须收敛在有限 trigger family

`Autopilot` 不应面对一个无限开放的“任何 UI 事件”平面。

V1 只建议先开放四类 trigger：

1. `system_question`
2. `turn_quality_guardrail`
3. `turn_terminal`
4. `schedule_tick`

### 3.3 V1 必须收敛在有限 response lane

`Autopilot` 的输出也必须是受限的。

V1 建议只开放这些 response lane：

1. `respond_request`
   - 回答一个当前 pending request / approval / elicitation
2. `interrupt_and_reframe`
   - 打断当前 turn，并补发一个要求重新分析或换策略的 follow-up
3. `same_thread_prompt`
   - 在当前 thread 补发一条 prompt
4. `new_thread_prompt`
   - 发起一个新 thread
5. `feishu_message`
   - 往绑定的 Feishu 聊天发文本或简单卡片
6. `launch_turn`
   - 定时触发时启动一轮 headless turn
7. `skip`
   - 明确不动作

### 3.4 对 mid-turn guardrail 采用“两段式判断”

V1 不应直接把若干危险词当作硬规则。

更稳的做法是：

1. 先用便宜信号发现可疑情况
2. 再触发一轮受限的额外推理，判断这次是否真的该打断
3. 确认后才执行 `interrupt_and_reframe`

这能避免把“合理的小修”与“明显在乱打补丁”混为一谈。

## 4. V1 的四类 trigger

### 4.1 `system_question`

这类 trigger 对应的是：系统主动停下来问用户“现在怎么办”。

典型来源：

- `plan_confirmation`
- tool / command / file / network approval
- permission / access approval
- MCP approval elicitation
- `request_user_input`
- MCP form / url continue

这类场景很适合 `Autopilot`，因为它天然已经具备：

1. 一个明确的问题
2. 一组候选回答或结构化 response contract
3. 一个系统已经暂停、正在等待回答的时机

V1 里，`Autopilot` 不是要开放式代写任何自由文本，而是先聚焦在：

- allow / deny / cancel
- once / session 这类授权范围
- continue / decline
- 对 request 的结构化回答

### 4.2 `turn_quality_guardrail`

这是本轮重定义里新增的核心能力。

它处理的不是“turn 结束后怎么办”，而是“turn 还在跑时，模型是否已经表现出明显糟糕的执行趋势”。

V1 建议优先支持三类高价值信号：

1. `strategy_thrash`
   - 模型遇到与预期不一致的结果后，开始盲目换方法尝试
   - 它没有先重新分析问题，而是快速改走另一条路继续试错
2. `patch_smell`
   - 模型在输出里明确出现补丁化话术，例如：
   - “简单修改一下”
   - “避免影响面过大”
   - “先保留兼容”
   - “加个兜底”
   - “这里先特殊处理”
   - 这些词本身不是硬错误，但常常意味着它正在绕开根因
3. `repeated_failure_without_reframing`
   - 连续失败、连续碰壁、连续退化，但没有形成新的问题判断
   - 它只是继续试，而不是先停下来重建理解

这类 guardrail 的推荐动作不是直接替模型设计方案，而是：

1. interrupt 当前 turn
2. 向当前 thread 补发一条“先停下重新分析”的 prompt
3. 要求模型先回答：
   - 根因是什么
   - 当前方案是不是补丁
   - 是否存在更结构性的修改点
   - 哪些已有改动应撤回、哪些应保留

### 4.3 `turn_terminal`

这类 trigger 处理的是：一轮 turn 结束后，系统该如何判断后续动作。

典型判断包括：

1. 已完成时直接结束，还是再补一轮
2. 失败时重试、换策略重试，还是停下汇报
3. 看起来结束了，但其实没做完时是否继续
4. 完成后是否发摘要、发通知、same-thread follow-up、new-thread follow-up

这部分会覆盖原来 `autocontinue` / `autowhip` 的主要产品语义，但不要求 1:1 复刻它们的专用状态机。

### 4.4 `schedule_tick`

`schedule_tick` 就是把 `/cron` 这条线纳入 `Autopilot`。

它虽然不属于“回答问题”，但属于“自然语言配置的运行时判断/执行策略”，因此应作为独立 trigger family 收口进来。

典型动作包括：

1. 到点发起一个 prompt
2. 到点发一条 Feishu 消息
3. 到点检查某个固定状态后决定是否 follow-up
4. 到点启动 same-thread / new-thread / headless turn

V1 建议保持此前已经收敛过的边界：

- `schedule_tick` 到点后走 direct-dispatch / launch path
- 不再临时跑一轮复杂的开放式 runtime 推理

## 5. 使用心智模型

用户最终只需要理解这条链路：

`Autopilot.md -> /autopilot load -> compiled policy registry -> runtime triggers`

其中：

1. `Autopilot.md`
   - 用户用自然语言描述自己的偏好
   - 例如：
   - 什么样的问题可以自动回答
   - 什么样的执行信号应该打断模型
   - turn 结束后怎样处理
   - 每天/每周何时定时发起动作
2. `/autopilot load`
   - 是唯一显式装载动作
   - 负责把自然语言编译成受支持的结构化 policy
3. compiled policy registry
   - 是 daemon 真正执行的版本
   - 它不再以“任何事件都可出 action”为目标，而是以少量 trigger family 的决策 contract 为目标
4. runtime trigger
   - 只在受支持的 trigger family 上触发
   - 不会重新解释整个开放文本

## 6. 粗粒度目标

V1 的粗粒度目标建议收敛为：

1. 用户仍然只写自然语言，不直接写 YAML/JSON 规则
2. `Autopilot` 有明确边界：只管系统拥有的判断点
3. mid-turn quality guardrail 成为一等能力，而不是附带技巧
4. `turn_terminal` 与 `schedule_tick` 被纳入同一 Autopilot 心智模型
5. 旧的 `/cron`、`autocontinue`、`autowhip` 能按“高层产品语义”迁入，而不是按旧状态机字段迁移
6. V1 的 trigger family、sources、response lane 都必须有限且可审计

## 7. 代表性 use cases

### 7.1 `system_question`

1. `plan_confirmation`
   - 当计划只是例行的小改动且权限请求符合工作区默认偏好时，自动允许一次并继续
2. MCP tool approval
   - 对低风险、已知 server、会话级授权允许自动回答
3. `request_user_input`
   - 对某些固定格式的系统提问，用预定义策略回复，而不是总停下来等用户

### 7.2 `turn_quality_guardrail`

1. 模型连续两次尝试都因为环境与预期不一致而失败
   - `Autopilot` 判断它进入 `strategy_thrash`
   - 自动 interrupt
   - 要求它先总结根因、再给出新的计划
2. 模型在实现中主动说出：
   - “这里我先简单修改”
   - “为了避免影响面过大，先保留兼容”
   - 若上下文显示这不是用户要求的 hotfix，而是一次应做正确收口的修改
   - `Autopilot` 触发额外推理
   - 判断确实有 `patch_smell` 后打断并要求根本性重构方案
3. 模型多次失败但只是换一种写法继续试
   - `Autopilot` 要求先做 root-cause analysis，而不是继续硬试

### 7.3 `turn_terminal`

1. 一轮 turn 表面结束，但 final message 明显留有“还需要继续”的尾巴
   - 自动 same-thread follow-up
2. 一轮 turn 因可重试失败停下
   - 先补一条“先分析失败根因，再决定是否重试”的 prompt
3. 一轮 turn 已经完成
   - 不继续改动
   - 仅发一条完成摘要到 Feishu

### 7.4 `schedule_tick`

1. 每天早上 9 点启动一轮 headless turn，输出工作区摘要
2. 每个工作日下午检查当前分支/当前 thread，若存在未收口工作则提醒
3. 每周固定时间起一个 review / triage 任务

## 8. V1 明确排除

以下内容默认不进 V1：

1. workspace / session / thread / commit picker 的自动代选
2. path / directory / file picker 的自动代选
3. page-local 配置页、菜单导航、本地 back/root/menu
4. 让 `Autopilot` 解释任意用户普通消息
5. 一个无限开放的 action 系统
6. 运行时自由调用任意 MCP / 内部工具
7. 自动 watch / 自动 reload `Autopilot.md`
8. 为了兼容旧设计而强行保留“任何 event 都能编译成 action”的开放式结构

## 9. 粗粒度技术形态

V1 仍建议保留这几层：

1. Source layer
   - 工作区根目录 `Autopilot.md`
2. Compile layer
   - 模型把自然语言编译成结构化 policy
3. Validation layer
   - 后端校验 trigger family、可用 source、response lane、授权边界
4. Registry layer
   - daemon 保存工作区级 policy registry、开关与元数据
5. Runtime trigger adapters
   - `system_question`
   - `turn_quality_guardrail`
   - `turn_terminal`
   - `schedule_tick`
6. Runtime responders
   - 只执行受支持的 response lane

但与旧设计不同的是：

1. 编译产物的中心不再是“任意 event -> action”
2. 编译产物的中心是“哪类 runtime judgment 应触发什么判断策略与 response lane”

## 10. 对旧能力的关系

### 10.1 `/cron`

`/cron` 应被视为 `schedule_tick` trigger family 的旧产品面。

应迁移的，是：

- 自然语言配置定时策略
- 到点发消息 / 到点起 turn 这类语义

不必 1:1 迁移的，是：

- Bitable 作为 source of truth
- owner-repair / takeover / writeback UI 的旧整套债务

### 10.2 `autocontinue`

`autocontinue` 应被视为 `turn_terminal` 的一个特例：

- 关键语义是“turn 停下后，是否继续”
- 不需要继续保留 `PendingAutoContinueEpisode` 这类专用状态机

### 10.3 `autowhip`

`autowhip` 也应被视为 `turn_terminal` 的一个特例：

- 关键语义是“completed 但像没做完时，是否继续补打一轮”
- 不需要继续保留固定 stop phrase、suppress-once、incomplete-stop backoff 这些实现细节

## 11. 建议的 issue 拆分

基于这次重定义，建议母单与子单至少拆成：

1. `#638`
   - 重定义 `Autopilot` 的产品边界
   - 冻结 trigger family、非目标、代表性 use cases
2. `#639`
   - 建立 `Autopilot` control plane 与 workspace policy registry
3. `#640`
   - 收敛 trigger family 的执行边界，并设计 `/cron`、`autocontinue`、`autowhip` 的迁移与切流

若后续继续拆 execution issue，建议优先顺序：

1. `system_question`
2. `turn_quality_guardrail`
3. `turn_terminal`
4. `schedule_tick`

## 12. 当前结论

这一轮最重要的变化不是“把旧自动化框架做得更完整”，而是承认旧定义抓错了产品中心。

`Autopilot` 真正应该承接的，是系统自己的运行时判断点：

- 系统提问
- 执行质量守门
- turn 结束判断
- 定时触发

只要这条定义成立，`/cron`、`autocontinue`、`autowhip` 才会自然变成它的特例，而不是反过来让 `Autopilot` 变成它们的技术拼盘。

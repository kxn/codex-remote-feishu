# Plan Mode 飞书支持设计草案

> Type: `draft`
> Updated: `2026-04-22`
> Summary: 为 `#214` 收敛最终实现口径：飞书侧以 `Plan mode on/off` 暴露真实 upstream Plan mode，补齐状态、队列冻结、协议落点、提案计划卡与执行 handoff。

## 1. 文档目标

这份文档服务于 `#214` 的直接实现，不再保留 `v1 / v2` 分版语义。

它只回答两件事：

1. 飞书用户最终会看到什么，以及 `Plan mode` 什么时候生效、什么时候不生效。
2. 这条能力在代码里应该挂在哪些真实状态和协议点上，避免继续依赖“本地线程模板刚好带过来”的偶然行为。

这里不再把产品暴露成泛化的 `collaboration mode` 平台。对飞书用户来说，只有一个明确能力：

- `Plan mode on`
- `Plan mode off`

upstream 协议字段仍然叫 `collaborationMode`，但这是协议细节，不是飞书产品主概念。

## 2. 定稿结论

1. `Plan mode` 是 surface 级的一等状态，不是 thread 属性，也不是实例属性。
2. 飞书侧命令面只保留 `Plan mode on/off`，canonical command 为：
   - `/plan`
   - `/plan on`
   - `/plan off`
3. 不保留 `/collab`，也不保留额外 alias；`/plan` 本身就是唯一主入口。
4. `Plan mode` 只影响后续新 turn，不反向改写：
   - 当前 running turn
   - 已入队消息
   - 当前 turn 的 `/steer`
   - reply processing 触发的 auto-steer
5. `request_user_input` 和 `mcp_server_elicitation` 继续复用现有同卡分步交互，不再造 Plan 专用问答卡。
6. `turn/plan/updated` 继续表示过程型 checklist 更新；`item/plan/delta` 明确表示最终提案计划正文，两者不混用。
7. `item/plan/delta` 不在 turn 中途单独落卡；一轮里只缓存最后一版，等 final message 落完且 turn 真正结束后，再追加提案计划卡。
8. 提案计划卡固定三个动作：
   - `直接执行`
   - `清空上下文并执行`
   - `取消`
9. 提案计划卡不进入 request gate，不阻塞继续输入；但在后续交互或上下文切换后必须及时 seal。

## 3. 产品模型

飞书侧需要把这三件事分开：

1. `ProductMode`
   - `normal`
   - `vscode`
   - 它决定“当前接管目标和路由方式”
2. `PlanMode`
   - `off`
   - `on`
   - 它决定“后续新 turn 是否按 Plan mode 启动”
3. `ObservedThreadPlanMode`
   - 只读提示
   - 表示当前 thread 最近一次在本地 UI / VS Code 里已知使用的 mode

其中：

- 真正能控制飞书后续行为的，只有 surface 自己的 `PlanMode`
- observed thread mode 只用于状态提示和调试说明，不反向覆盖飞书 surface 设置

## 4. 用户可见行为

### 4.1 命令面

新增独立命令：

- `/plan`
  - 打开 `Plan mode` 配置卡
- `/plan on`
  - 把当前 surface 的后续新 turn 默认改为 Plan mode
- `/plan off`
  - 把当前 surface 的后续新 turn 默认改为默认执行

菜单位置放在 `发送设置`，与下面几项并列：

- `/reasoning`
- `/model`
- `/access`
- `/verbose`
- `/plan`

### 4.2 配置卡

沿用现有 command config card 体系，不新造卡型。

卡片建议分三段：

1. 当前状态
   - `Plan mode：开启` 或 `Plan mode：关闭`
   - `作用范围：只影响后续新 turn`
2. 立即切换
   - `开启`
   - `关闭`
3. notice / sealed 区
   - 成功、错误、忙碌态和 sealed 后下一步提示

### 4.3 `/status` 与 snapshot

`/status` 和 snapshot 需要新增一条清晰状态：

- `Plan mode：开启`
- 或 `Plan mode：关闭`

当 surface 正处于 running / queued 状态，用户切换 `/plan` 后，要明确提示：

- `Plan mode：开启（当前正在执行的这一轮不受影响）`

如果 attached thread 最近一次本地 observed mode 和当前 surface 设置不同，可以追加只读提示：

- `提示：当前会话最近一次本地运行使用的是 Plan mode；飞书后续新 turn 仍按当前设置启动。`

### 4.4 Plan mode 下的正常交互

如果当前 `Plan mode=on`，用户发送新消息后：

1. 新 turn 显式按 Plan mode 启动
2. 普通过程文本继续按现有 verbosity 规则显示
3. `turn/plan/updated` 继续显示现有 append-only 计划更新卡
4. 若上游需要补充信息，继续复用现有 request card
5. final answer 继续走现有 final card

也就是说，Plan mode 主要改变的是 turn 的启动语义，而不是另起一套终端 UI。

## 5. 提案计划卡

### 5.1 触发时机

当一轮 turn 在过程中产生了 `item/plan/delta`：

1. turn 运行中只缓存“本轮最后一版计划正文”
2. 不在中途落卡
3. 等 final message 已落完且 turn `completed`
4. 若这轮确实存在计划正文，再 append 一张独立提案计划卡

这样可以避免：

- 中途落卡被 final message 顶掉
- 同一轮多版计划同时暴露
- 旧计划在后续 steer 后仍可误点

### 5.2 与 `turn/plan/updated` 的分工

1. `turn/plan/updated`
   - 过程型 checklist 快照
2. `item/plan/delta` 对应的提案计划卡
   - 最终提案正文
   - 带本地 handoff CTA

两者必须保持分工，不用 `turn/plan/updated` 去猜最终提案计划。

### 5.3 卡片动作

提案计划卡固定三个动作：

1. `直接执行`
   - 当前 surface 的 `PlanMode` 切回 `off`
   - 发送 follow-up：`Implement the plan.`
   - 当前卡片 seal 为结果态
2. `清空上下文并执行`
   - 当前 surface 的 `PlanMode` 切回 `off`
   - 开 fresh context
   - 发送 handoff prompt：
     - 前缀说明上一轮已经给出计划，需要在新上下文重新读文件并按计划开工
     - 后面拼接完整计划正文
   - 当前卡片 seal 为结果态
3. `取消`
   - 不回 upstream request
   - 不切线程
   - 不改 `PlanMode`
   - 只 seal 这张执行提示卡

### 5.4 不抓前台输入

这张卡不是 upstream request，不进入 request gate。

因此：

1. 用户看到它后，仍然可以继续直接输入新消息
2. 不需要先点 `取消` 才能继续交互
3. `/new`、`/use`、普通文本输入或后续 steer 都不应被它阻塞

### 5.5 seal / 失效规则

任一命中时都要 seal：

1. 用户点击 `直接执行`
2. 用户点击 `清空上下文并执行`
3. 用户点击 `取消`
4. 用户在同一 thread 上继续发了新的输入
5. 同一 thread 上又出现了新的 `item/plan/delta`
6. 用户切线程、`/new`、`/use`、`/detach` 或发生等价上下文切换
7. daemon lifecycle 已变化，旧卡不再安全可写

seal 后保留正文，移除按钮，并说明失效原因。

## 6. 技术设计

### 6.1 状态模型

新增显式状态，不复用泛化 `collaboration mode` 产品概念。

推荐新增：

- `state.PlanModeSettingOff`
- `state.PlanModeSettingOn`

并提供 normalize / display helper。

挂载位置：

1. `SurfaceConsoleRecord.PlanMode`
   - 当前 surface 的真实设置
2. `QueueItemRecord.FrozenPlanMode`
   - 入队时冻结，防止之后切 `/plan` 追溯改写已排队消息
3. `ThreadRecord.ObservedPlanMode`
   - 只读提示，表达 thread 最近一次本地已知 mode

### 6.2 agentproto

为避免继续偷塞到别的字段，建议显式补齐：

1. `agentproto.PromptOverrides.PlanMode`
2. `agentproto.Event.PlanMode`
3. `agentproto.ThreadSnapshotRecord.PlanMode`

这些字段对飞书产品显示为 `on/off`；translator 再映射到 upstream 协议的 `collaborationMode.mode=plan/default`。

### 6.3 queue dispatch

`dispatchNext(...)` 和 reply auto-steer 之外的新 turn 路径，需要把冻结后的 `PlanMode` 带进 `PromptOverrides`。

注意：

- `turn/start` 带 `PlanMode`
- `turn/steer` 不带 `PlanMode`

### 6.4 translator

translator 规则固定为：

1. outbound `turn/start`
   - `PlanMode=on` -> `collaborationMode.mode=plan`
   - `PlanMode=off` -> `collaborationMode.mode=default`
2. outbound `turn/steer`
   - 不带 `collaborationMode`
3. thread / turn observe
   - 从本地 `turn/start`、thread snapshot、thread read 中读取 `collaborationMode.mode`
   - 回填到 `Event.PlanMode` / `ThreadSnapshotRecord.PlanMode`

如果上游对 `mode=default` 有兼容性问题，fallback 可以改成“显式清空旧模板里的 `collaborationMode`”，但行为目标不变：

- 飞书 surface 的 `PlanMode=off` 必须稳定抵消旧 thread template 里残留的 `plan`

### 6.5 snapshot 与命令视图

`PromptRouteSummary` 需要新增至少：

- `EffectivePlanMode`
- `ObservedThreadPlanMode`

`/plan` config card 继续走现有：

1. `FeishuCommandDefinition`
2. `FeishuCatalogConfigView`
3. `BuildFeishuCommandConfigPageView(...)`
4. `handlePlanCommand(...)`

### 6.6 surface resume

surface resume 状态新增 `PlanMode`，与现有：

- `ProductMode`
- `Verbosity`

同级恢复。

这保证 daemon 重启后，surface 仍知道后续新 turn 是否应该按 Plan mode 启动。

## 7. 不该做的事

本方案明确不做：

1. 不再对飞书用户暴露泛化 `collaboration mode` 平台
2. 不保留 `/collab` 或 `/plan` 的双命令并存
3. 不让 observed thread mode 直接覆盖 surface 设置
4. 不让 `/plan` 影响当前 running turn
5. 不用 `turn/plan/updated` 去猜测“最终提案计划”
6. 不把计划更新、提案计划、request、final reply 强行揉进一张持续膨胀的巨卡

## 8. 建议执行顺序

虽然不再分产品版本，但实现顺序仍建议按下面推进：

1. 命令面、状态、snapshot、surface resume
2. queue freeze 与 outbound `turn/start` 落参
3. observed plan mode 回填与状态提示
4. `item/plan/delta` 的提案计划卡和三按钮动作
5. 边界测试、状态机文档同步与 close-out

## 9. 验证建议

至少覆盖这些场景：

1. `/plan on` 后发送新消息，出站 `turn/start` 明确带 `collaborationMode.mode=plan`
2. `/plan off` 后发送新消息，不会继续继承旧模板里的 `plan`
3. running turn 期间切 `/plan`，当前 turn 不变
4. 消息已入队后切 `/plan`，已入队项不变，后续项生效
5. reply processing 触发 auto-steer 时，不受 `/plan` 改写
6. `/new`、`/use`、`/detach`、`/mode normal|vscode` 后，`PlanMode` 保持
7. daemon 重启后恢复 `PlanMode`
8. `request_user_input` / `mcp_server_elicitation` 在 `Plan mode=on` 时继续按现有分步卡工作
9. `turn/plan/updated` 的现有 checklist 卡不回归
10. `item/plan/delta` 在一轮中多次更新时，只在 turn 完成后落最后一版提案计划卡
11. 提案计划卡的三按钮动作、seal 规则和后续继续输入行为符合设计

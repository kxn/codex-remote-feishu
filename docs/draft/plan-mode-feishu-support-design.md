# Plan Mode 飞书支持设计草案

> Type: `draft`
> Updated: `2026-04-21`
> Summary: 基于最新基座重写 Plan mode 支持草案，明确 Feishu 侧产品表现、状态承载和 staged rollout。

## 1. 文档目标

这份文档服务于 `#214`，讨论的是**下一阶段要落地的 Plan mode 支持方案**，不是当前代码已实现行为。

它要先回答两件事：

1. 从产品侧看，飞书用户以后会看到什么、怎么切、什么时候生效、什么时候不会生效。
2. 从技术侧看，这件事应该挂在哪些真实状态和协议点上，避免又做成“看起来像支持，其实只是碰巧跟上游模板串上了”。

这份文档默认只讨论 `default` 和 `plan` 两种 collaboration mode，不讨论任意自定义 preset 平台。

## 2. 设计结论

本轮草案先收敛为 7 条结论：

1. `Plan mode` 在我们产品里应被建成 **surface 级的一等协作状态**，而不是继续依赖“本地线程模板恰好带过来”。
2. 这个状态和现有 `/mode normal|vscode` 正交。
   - `/mode` 决定“接管哪个目标、按哪种路由模型工作”
   - `/collab` 决定“后续新 turn 按哪种协作方式启动”
3. v1 只暴露一个新命令面：`/collab`。
   - `/collab`
   - `/collab default`
   - `/collab plan`
4. `collaboration mode` 只影响**后续新 turn**，不反向改写：
   - 当前 running turn
   - 已入队的消息
   - 当前 turn 的 steer / reply auto-steer
5. 现有 `request_user_input` / `mcp_server_elicitation` 的“同卡分步答题”能力可以直接复用，不需要另做 Plan-mode 专用问答卡。
6. `turn/plan/updated` 在 v1 继续保持 append-only 计划更新卡；`item/plan/delta` 在 v1 继续当普通文本，不急着硬做 CTA。
7. “按这个计划开工”的一键 CTA 放到 v2 以后，前提是我们已经能稳定识别 proposed plan，而不是拿 `turn/plan/updated` 误判。

一句话压缩：

- v1 先把 **Plan mode 变成可选、可见、可恢复、可冻结的 surface 默认协作方式**
- v2 再补 **plan proposal 专属 CTA 和更完整的 Plan-mode 产品闭环**

## 3. 当前基座与约束

这次方案建立在下面这些已经确认的现状上：

### 3.1 已有能力

1. translator 已经会缓存最近的 `turn/start` 模板，并保留 `collaborationMode`。
2. `turn/plan/updated` 已有结构化事件和飞书计划更新卡。
3. `request_user_input` 和 form 模式 `mcp_server_elicitation` 已经是同卡分步交互。
4. 当前命令菜单、参数卡、状态卡都已经收敛到一套比较稳定的 owner-card / config-card 体系。
5. daemon 已经会持久化并恢复 surface 级的 `ProductMode` 和 `Verbosity`。

### 3.2 明确缺口

1. surface state 里还没有 collaboration mode。
2. queue freeze 只冻结 `model / reasoning / access`，没有冻结 collaboration mode。
3. `PromptRouteSummary` / snapshot / `/status` 还看不到 collaboration mode。
4. 当前 thread parser 只读 `latestCollaborationMode.settings.*`，还不读 `latestCollaborationMode.mode`。
5. `item/plan/delta` 仍只是普通文本链路，没有“计划草案 / 是否按此执行”的产品语义。

### 3.3 平台约束

Feishu 卡片侧继续遵守现有基线：

1. 不把整轮 transcript、plan、request、结果 CTA 都揉进一张持续膨胀的大卡。
2. 继续优先复用当前“append-only 计划更新卡 + 同卡 request 卡 + final reply 卡”的分工。
3. 需要交互时尽量走已有 config / request / page owner-card 结构，不额外发散出一套 Plan-mode 专用卡片协议。

这也是为什么本方案不推荐一上来就做“把计划、追问、最终确认、执行按钮全塞进一张 live card”。

## 4. 产品模型

### 4.1 三个概念必须分开

#### A. `ProductMode`

- `normal`
- `vscode`

它回答的是：飞书现在按什么目标模型工作。

#### B. `CollaborationMode`

- `default`
- `plan`

它回答的是：飞书**后续新 turn**要按什么协作方式启动。

#### C. `Observed Thread Collaboration Mode`

这是从本地 Codex UI / VS Code / thread snapshot 里观察到的“这个 thread 最近一次本地使用的 mode”。

它是**只读提示**，不是 v1 的产品真源。

推荐规则：

1. v1 的真实控制面只认 surface 自己的 `CollaborationMode`。
2. thread observed mode 只用于：
   - 状态提示
   - 调试说明
   - 未来可能的自动建议
3. 不让本地线程的 mode 变化悄悄反向覆盖飞书 surface 偏好。

### 4.2 用户看到的产品状态

以后一个飞书 surface 的核心状态应至少可被用户读成下面这句：

- `当前工作模式：normal`
- `后续协作方式：plan`
- `当前接管目标：工作区 / 会话 / VS Code 实例`

也就是说，用户在飞书侧应能区分：

1. 我现在接管的是谁
2. 我下一条消息会发到哪里
3. 我下一条消息会按 default 还是 plan 启动

这三件事不能再只靠用户猜。

## 5. 产品表现

## 5.1 新命令面：`/collab`

推荐新增一个独立命令，而不是复用现有 `/mode`。

### 5.1.1 命令语义

- `/collab`
  - 打开“协作方式”参数卡
- `/collab default`
  - 把当前 surface 的后续新 turn 默认协作方式设为 `default`
- `/collab plan`
  - 把当前 surface 的后续新 turn 默认协作方式设为 `plan`

### 5.1.2 菜单位置

推荐把“协作方式”放进 `发送设置` 分组，和下面几项并列：

- `/reasoning`
- `/model`
- `/access`
- `/verbose`
- `/collab`

原因很简单：

1. 它影响的是“后续怎么发新 turn”
2. 它和模型、推理、权限一样，属于发送前默认值
3. 它不属于 `/mode normal|vscode` 那种路由层概念

### 5.1.3 参数卡长相

推荐直接沿用现有 config-card 结构，不新造卡型。

卡片建议分三段：

1. 业务区
   - 当前：`默认执行` / `规划模式`
   - 作用范围：`只影响后续新 turn`
2. 按钮区
   - `默认执行`
   - `规划模式`
3. notice 区
   - 成功、错误、sealed 后的下一步提示

推荐文案：

- `默认执行`
  - 直接按普通模式开启新一轮
- `规划模式`
  - 新一轮优先进入 Plan mode，可能先产出计划或追问补充信息

### 5.1.4 v1 不做 `/plan` alias

推荐 v1 先不加 `/plan` alias。

原因：

1. `plan` 是 mode 值，不是新命令族。
2. 如果只做 `/plan` 而没有 `/default` 对称入口，命令面会开始歪。
3. 我们后续很可能还会有“查看当前协作方式”的需求，`/collab` 更稳定。

如果后面产品上强烈希望贴近 upstream，再补 `/plan -> /collab plan` alias 也不迟。

## 5.2 `/status` 与快照表现

### 5.2.1 快照里新增一条“后续协作”

当前 `/status` 和 snapshot 里已有：

- 模式
- 接管目标
- 下一条消息的模型 / 推理 / 权限

推荐新增：

- `后续协作：默认执行`
- 或 `后续协作：规划模式`

不要把它埋进长句里只当附属字段。

### 5.2.2 提示当前 running turn 不受影响

当 surface 处于 running / queued 状态时，用户切 `/collab` 后，`/status` 建议明确写成：

- `后续协作：规划模式（当前正在执行的这一轮不受影响）`

这样可以直接消除“我刚点了 plan，为什么眼前这轮没有变”的误解。

### 5.2.3 可选的 thread hint

当 attached thread 最近一次本地 observed mode 和当前 surface default 不一致时，`/status` 可追加一条只读提示：

- `提示：当前会话最近一次本地运行使用的是规划模式；飞书后续新 turn 仍将按“默认执行”启动。`

这条提示不是 blocker，只是解释状态差异。

## 5.3 用户在 Plan mode 下实际会看到什么

### 5.3.1 发出一条新消息

如果当前 `CollaborationMode=plan`，用户发出新的文本/图片/文件组合后：

1. 新 turn 按 Plan mode 启动
2. 这条 turn 的后续表现沿用当前已有 UI：
   - 过程文本继续按现有 verbosity 规则显示
   - `turn/plan/updated` 继续发计划更新卡
   - 若上游请求补充信息，则直接复用现有分步 request 卡
   - final answer 继续走现有 final card

也就是说，v1 不额外造“Plan 专用终端”。

### 5.3.2 如果上游要求补充信息

产品侧不需要再定义新卡。

直接复用现有 request 体系：

1. 只显示当前题
2. `上一题 / 下一题`
3. `保存本题`
4. `提交答案 / 提交并继续`

这和当前 `request_user_input` / `mcp_server_elicitation` 已经是一致的。

### 5.3.3 如果上游持续更新计划

继续沿用现有 append-only 计划更新卡：

1. 说明文本
2. checklist 风格步骤状态
3. 快照去重

v1 不改成单卡 live plan board。

原因：

1. 现在这条链路已经稳定
2. 它和 request 卡、final card 是不同节奏的 UI
3. 强行改成单卡混合态，反而会把 Feishu 交互、卡片体积和更新频率一起绑死

### 5.3.4 如果 turn 结束时只是产出一份计划正文

v1 继续按普通 final reply 处理。

当前不额外追加：

- `是否按此计划开工`
- `执行这个计划`
- `继续规划`

这不是产品缺失，而是故意后置。

原因是 v1 还没有稳定识别 proposed plan item，不应该拿 `turn/plan/updated` 或普通文本启发式误判。

## 5.4 关键交互边界

### 5.4.1 当前 running turn

如果当前已有 running turn，用户切 `/collab plan`：

1. 当前 running turn 不变
2. 当前 turn 的 steer 不变
3. 之后新的 turn 才生效

### 5.4.2 已排队消息

如果消息已经入队，再切 `/collab`：

1. 已排队项按入队时冻结的 collaboration mode 发出
2. 新设置只影响后面再来的消息

### 5.4.3 reply 当前 processing 消息

reply processing 触发的是当前 turn 的 auto-steer。

因此它不应受新的 `/collab` 改写。

### 5.4.4 `/new`、`/use`、`/detach`、`/mode`

推荐全部保留当前 `CollaborationMode`：

1. `/new`
   - 只改 route，不改 collaboration mode
2. `/use`
   - 只改接管目标，不改 collaboration mode
3. `/detach`
   - 只断接管，不改 collaboration mode
4. `/mode normal|vscode`
   - 只切产品路由模式，不改 collaboration mode

这保证了它真的是一个“surface 后续协作偏好”，而不是 thread 临时属性。

### 5.4.5 daemon 重启恢复

推荐和 `ProductMode`、`Verbosity` 一样恢复。

也就是：

- surface 重启前是 `plan`
- 重启后 materialize 回来仍然是 `plan`

## 6. v2 以后的产品增强

### 6.1 `Implement this plan?` CTA

这部分建议明确放到 v2 以后。

推荐交互形态：

1. 当前 plan turn 结束
2. 若识别到稳定的 proposed plan item
3. 在 final plan 答复之后追加一张**独立的小型 CTA 卡**
4. 按钮：
   - `按此计划开工`
   - `继续规划`

为什么推荐做成独立 CTA 卡，而不是把按钮塞进 final answer card：

1. final answer 适合保持可回看的静态结果
2. CTA 卡需要 sealed / patch / old-card freshness 语义
3. 分开以后，执行路径和结果路径更清楚

### 6.2 CTA 的推荐行为

如果用户点 `按此计划开工`：

1. 先把当前 surface `CollaborationMode` 切回 `default`
2. 再发送固定 follow-up：`Implement the plan.`
3. CTA 卡 sealed 成结果态
4. `/status` 里的“后续协作”同步显示为 `默认执行`

如果用户点 `继续规划`：

1. 不改 mode
2. CTA 卡 sealed 成说明态
3. 继续保持 `plan`

这个行为是**显式切模态**，不是自动切换。

## 7. 技术设计

## 7.1 状态模型

推荐新增一个独立枚举，而不是把 collaboration mode 塞回 `ModelConfigRecord`。

### 7.1.1 新枚举

新增：

- `state.CollaborationModeDefault`
- `state.CollaborationModePlan`

以及统一 normalize / display helper。

### 7.1.2 Surface 级状态

在 `SurfaceConsoleRecord` 上新增：

- `CollaborationMode state.CollaborationMode`

默认值：

- surface 首次 materialize 时默认 `default`

### 7.1.3 Queue freeze

在 `QueueItemRecord` 上新增：

- `FrozenCollaborationMode state.CollaborationMode`

理由：

1. 这条消息入队时就应该冻结“它要按什么协作方式发”
2. 否则 `/collab` 会追溯改写已经排队的消息

### 7.1.4 Thread 观测值

在 `ThreadRecord` 上新增只读字段：

- `ObservedCollaborationMode state.CollaborationMode`

它只表达：

- 这个 thread 最近一次已知的本地 collaboration mode

不直接驱动飞书 surface 的默认值。

## 7.2 协议落点

### 7.2.1 `agentproto`

在 `agentproto.PromptOverrides` 上新增：

- `CollaborationMode string`

这样 collaboration mode 可以和当前的 model / reasoning / access 一样，跟着 prompt 命令显式下发，但它不再借 `developerInstructions` 旁路表达。

### 7.2.2 queue dispatch

`dispatchNext(...)` 派发 `CommandPromptSend` 时，把 `FrozenCollaborationMode` 带进 `PromptOverrides`。

### 7.2.3 translator

translator 侧规则建议写死为：

1. `turn/start`
   - 当 override 是 `plan` 时，显式写 `collaborationMode.mode=plan`
   - 当 override 是 `default` 时，也显式写 `collaborationMode.mode=default`
2. `turn/steer`
   - 不带 collaboration mode
3. `thread/start`
   - 不需要靠它表达 Plan mode；真正的协作 mode 继续落在 `turn/start`

实现备注：

如果上游实测发现 `mode=default` 兼容性不足，则 fallback 可以改成：

1. 在 `default` 情况下显式清空模板里的旧 `collaborationMode`
2. 同时保证不会再继承 thread template 里的 `plan`

但设计目标不变：

- 飞书 surface 的 `CollaborationMode` 必须能稳定决定后续新 turn 的真实 mode

## 7.3 观测与回填

### 7.3.1 thread snapshot 解析

在 thread list / read 解析时，补读取：

- `latestCollaborationMode.mode`
- `collaborationMode.mode`

并回填到 `ThreadRecord.ObservedCollaborationMode`。

### 7.3.2 local client observe

在本地 `turn/start` observe 时，若带 `collaborationMode.mode`，也应同步更新当前 thread 的 observed mode。

原因：

1. 这样本地 UI 切 mode 后，不必等下一轮 threads refresh 才能在飞书 `/status` 里看到提示
2. 这条数据只是只读 hint，不会反向改 surface default，所以不会引入状态源竞争

## 7.4 Snapshot / Command View / Feishu Projection

### 7.4.1 snapshot

`PromptRouteSummary` 建议新增至少两个字段：

- `EffectiveCollaborationMode`
- `ObservedThreadCollaborationMode`

其中：

1. `EffectiveCollaborationMode`
   - 表示飞书这边下一条新 turn 会按什么发
2. `ObservedThreadCollaborationMode`
   - 表示当前 attached thread 最近本地 observed 到的 mode

### 7.4.2 `/status`

projector 侧在“下一条消息”或 prompt summary 区补一条：

- `协作 默认执行`
- 或 `协作 规划模式`

若存在 observed-thread hint，则作为单独说明 section，不混进主配置行。

### 7.4.3 `/collab` config card

沿用现有 command-config 体系：

1. `FeishuCommandDefinition`
2. `FeishuCommandConfigView`
3. `BuildFeishuCommandConfigCatalog(...)`
4. `handleCollabCommand(...)`

这样它天然就继承：

- bare `/collab`
- 菜单打开
- 同卡 apply
- sealed 结果态
- back buttons

不需要另开一套 Feishu card family。

## 7.5 Surface resume

`SurfaceResumeEntry` 新增：

- `CollaborationMode string`

materialize surface resume 时一并恢复。

这条字段和现有：

- `ProductMode`
- `Verbosity`

同级，都是 surface 级偏好。

## 7.6 不应该做的事

本方案明确不建议：

1. 把 collaboration mode 塞进 `WorkspaceDefaults`
2. 让本地 thread observed mode 直接覆盖 surface default
3. 让 `/collab` 直接影响当前 running turn
4. 用 `turn/plan/updated` 去猜测“这就是最终提案计划”
5. 在 v1 就把 plan update、plan proposal、request、最终 CTA 强行合成一张超级卡

## 8. 分阶段落地

### 阶段 1：控制面与状态可见性

1. 新增 `CollaborationMode` surface 状态
2. 新增 `/collab` 命令、菜单入口、config card
3. `/status` / snapshot 展示后续协作方式
4. surface resume 持久化和恢复

这阶段完成后，用户已经能：

1. 显式切 `default|plan`
2. 在状态卡里看到当前设置
3. 重启后保留设置

### 阶段 2：queue freeze 与协议落参

1. queue item 冻结 collaboration mode
2. dispatch 时带入 `PromptOverrides`
3. translator 在 `turn/start` 明确落 `collaborationMode.mode`
4. 验证 running / queued / new turn 边界

这阶段完成后，才算真正“支持 Plan mode 发新 turn”。

### 阶段 3：observed mode 提示

1. thread snapshot 解析 `latestCollaborationMode.mode`
2. local observe 回填当前 thread observed mode
3. `/status` 增加“本地最近 mode”提示

这阶段主要是解释性增强，不阻塞阶段 1~2。

### 阶段 4：plan proposal CTA

1. 识别 proposed plan item
2. 设计独立 CTA 卡
3. `按此计划开工 = 切回 default + 发送固定 follow-up`
4. 完整打通 `Implement this plan?` 闭环

## 9. 验证建议

至少覆盖下面这些场景：

1. `/collab plan` 后发送新消息，出站 `turn/start` 明确带 `collaborationMode.mode=plan`
2. `/collab default` 后发送新消息，不会再继承旧 thread template 里的 `plan`
3. running turn 期间切 `/collab`，当前 turn 不变
4. 消息已入队后切 `/collab`，已入队项不变，后续项生效
5. reply processing 触发 auto-steer 时，不受 `/collab` 改写
6. `/new`、`/use`、`/detach`、`/mode normal|vscode` 后，collaboration mode 保持
7. daemon 重启后恢复 collaboration mode
8. `request_user_input` / `mcp_server_elicitation` 在 `plan` 下继续按现有同卡分步交互工作
9. `turn/plan/updated` 在 `quiet / normal / verbose` 下继续遵守现有可见性规则

## 10. 推荐拍板项

这份草案里需要产品拍板的内容，建议先按下面 3 项确认：

1. 命令面是否采用 `/collab`，并在 v1 不做 `/plan` alias。
   - 推荐：是
2. v1 是否只做“可选、可见、可恢复的 Plan mode”，先不做“按此计划开工”按钮。
   - 推荐：是
3. thread observed mode 是否只作为 `/status` 提示，不自动改写 surface 默认值。
   - 推荐：是

如果这 3 项都认可，后面的实现基本就可以按阶段直接开工，不需要再重做产品骨架。

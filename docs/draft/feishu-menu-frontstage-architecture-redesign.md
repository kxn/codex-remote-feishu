# 飞书菜单 / 业务卡前台流架构重构方案

> Type: `draft`
> Updated: `2026-04-22`
> Summary: 基于既有卡片交互规约重新审计菜单与业务卡的脆弱点，并提出统一 frontstage flow 目标架构，消除菜单扩展与业务卡接入时的多处并行改动。

## 1. 背景

这次要解决的不是某一个菜单 bug，而是更底层的问题：

1. 为什么菜单、picker、业务卡这套东西一改就容易牵出很多别的地方。
2. 为什么“只是加一个入口”或“只是改一个返回规则”会经常把别的卡也带坏。
3. 怎样把它收敛成一套稳定框架，使后续新增菜单项和新增业务卡都变成低风险接入。

本方案以以下文档为产品基线：

- `docs/general/feishu-business-card-interaction-principles.md`
- `docs/general/feishu-card-interaction-model.md`
- `docs/general/feishu-card-ui-state-machine.md`
- `docs/general/feishu-card-api-constraints.md`

## 2. 目标

目标只有两个：

1. **加菜单项时，接入点单一、改动局部、不会漏改生命周期分支。**
2. **加业务卡时，默认继承同一套 owner-card / 返回 / 封卡 / 结果投递规则，不需要每张卡重复发明自己的行为。**

对应的产品约束也很明确：

- 菜单卡只负责分流，不负责长期拥有业务执行。
- 一条前台业务默认由一张 owner card 承接到底。
- 已经进入业务执行后，不能再语义含混地“退回菜单内部”。
- `help`、`status` 这类即时完成型结果不应伪装成还能继续返回的 preview。
- 后台例外必须显式声明，不能靠实现层碰巧 append 出来。

## 3. 现有规约其实已经很清楚

现有产品规约并不模糊，真正模糊的是实现。

按规约，飞书侧其实已经有相对稳定的角色划分：

### 3.1 Launcher Card

菜单首页、菜单分组页、配置入口页都属于 launcher。

特点：

- 同卡导航
- 可以返回菜单内部
- 不拥有业务结果

### 3.2 Owner Card

只要进入某条业务流，并且用户还在等待结果、还可能取消、还需要继续交互，这张卡就应该成为 owner。

特点：

- 同一业务默认一张卡走到底
- 中间的步骤切换、局部表单、返回上一步都在 owner 范围内解决
- 最终要明确收口，不留“像是还活着”的半死卡

### 3.3 Terminal Card

`help`、`status` 这类点完就完成的内容，本质上是 terminal。

特点：

- 发出结果就结束
- 不再作为 preview 允许回到菜单
- 不应该再参与“菜单内部返回链”

### 3.4 Background / Timeline Output

后台 notice、共享过程卡、checkpoint card、最终 reply 都属于时间线投递，不属于当前前台 owner 的菜单导航系统。

## 4. 现在为什么这么脆弱

问题不是一个点，而是同一个产品语义被拆散到多套机制里分别判断。

### 4.1 “当前该 patch 哪张卡”不是显式声明，而是多处推断

当前一个动作点下去以后，究竟是：

- inline replace 当前卡
- 替换成命令结果卡
- append 到下方时间线
- 先给一张“已提交”占位卡

并不是由某个统一的 flow contract 决定，而是分散在这些位置：

- `internal/core/control/feishu_ui_intent.go`
- `internal/core/control/feishu_ui_lifecycle.go`
- `internal/app/daemon/app_ingress.go`
- `internal/core/orchestrator/service_page_view.go`
- `internal/core/orchestrator/service_feishu_ui_context.go`

这意味着一个产品动作的语义，需要同时满足：

- action 能被识别成某类 Feishu UI intent
- lifecycle allow-list 允许它 inline replace
- event 第一条刚好可投影成 replacement card
- runtime 里正好还留着正确的 owner / menu / picker message id

任何一层漏掉，用户看到的行为就会变掉。

### 4.2 前台卡并没有统一 runtime，而是拆成很多套局部记录

现在表面上都叫“菜单 / picker / page / history / workspace”，但运行时并不是一个统一前台流，而是至少拆成：

- `MenuFlowRuntimeRecord`
- `activeOwnerCardFlowRecord`
- `activeWorkspacePageRecord`
- `activeTargetPickerRecord`
- `activePathPickerRecord`
- `activeThreadHistoryRecord`
- `activePlanProposalRecord`

这些记录各自保存一部分“当前是谁、能不能返回、该 patch 哪张卡、当前在哪一步”的信息。

结果是：

- 菜单有菜单自己的 phase
- owner card 有 owner 自己的 phase
- picker 又有 picker 自己的 stage
- path picker 再嵌一层自己的 consumer meta

产品上是“一张前台卡怎么流转”，工程上却是多套半重叠状态机并行。

### 4.3 菜单进入业务，不是第一类转移，而是散落 flag

从菜单进入业务，本来应该是非常明确的一次语义切换：

- launcher 结束
- owner 开始接管
- 原菜单卡封口

但当前实现不是围绕这个转移建模的，而是依赖一些零散信号组合起来判断，比如：

- `EnteredBusiness`
- `InlineReplaceCurrentCard`
- 特定 action kind 是否允许 replacement
- 各种 active record 是否存在

所以一旦某个业务想“从菜单进入，但内部还能返回上一步”，就很容易把“菜单内返回”和“业务内返回”混在一起。

### 4.4 加一个菜单项不是局部接入，而是多点联动

当前新增一个菜单项，往往要同时考虑：

- 命令定义 / canonical slash
- menu builder 里怎么显示
- action kind / intent 怎么识别
- lifecycle allow-list 要不要放行
- orchestrator 发什么 view
- runtime 要不要单独记 message id / picker id / flow id
- projector 最后能不能 inline replace

这不是“声明一个新入口”，而是在很多层拼装出一个新入口。

只要忘一处，就会出现：

- 入口能点，但不在原卡刷新
- 能刷新，但返回错层
- 主业务能跑，但旧卡没封住
- 本应 terminal 的结果被当成 preview

### 4.5 transport 决策和产品语义纠缠在一起

`app_ingress.go` 当前同时承担两类事情：

1. 网关/投递层要不要把当前 callback 同步返回成 replace
2. 产品上这一步到底算 launcher 导航、owner 更新，还是 timeline 独立结果

这两类判断本应分层：

- transport 关心的是“能不能回调内 replace、消息新鲜度够不够、旧卡能不能拒绝”
- 产品层关心的是“这一步该由谁拥有、是否继续在同一张卡、是否封口”

现在混在一起后，就会出现看似是产品 bug，实则是投递分支漏了；或者看似是 callback 技术分支，实则改坏了业务语义。

### 4.6 “特殊情况”越来越多，是因为缺少统一角色模型

现在最容易出问题的往往不是主流程本身，而是这些边缘情况：

- `help` / `status` 这类即时完成卡
- 菜单进入 workspace 再进入独立 picker
- owner card 内嵌 path picker 子步骤
- terminal 结果出现后还能不能返回
- 旧卡还亮着按钮，但其实主业务已经换 owner 了

这些都说明系统没有先把“角色”建清楚，导致每个功能都在自己解释什么叫返回、什么叫结束、什么叫当前 owner。

## 5. 根因归纳

一句话总结：

**当前系统把“前台卡交互语义”拆成了 action 识别、lifecycle allow-list、局部 runtime record、投递分支四套并行机制；它们共同近似出产品行为，但没有一个地方是真正的一等事实来源。**

这就是为什么它会经常表现为：

- 改动很小
- 编译没错
- 单点看也像合理
- 但联动起来后就开始乱

## 6. 目标态：统一 Frontstage Flow 框架

要根治这类问题，需要把“前台抓用户注意力的一张卡如何拥有一条流程”变成第一类模型。

### 6.1 核心抽象

引入统一的 `FrontstageFlow`：

- 一个 surface 在任意时刻最多只有一个活跃 frontstage flow
- 一个 flow 在任意时刻最多只有一张活跃 owner card
- launcher、owner、terminal 都只是 flow 内不同 step role

建议最小模型如下：

- `flow_id`
- `flow_kind`
- `entry_command`
- `role`
  - `launcher`
  - `owner`
  - `terminal`
- `phase`
  - `editing`
  - `running`
  - `completed`
  - `cancelled`
- `owner_message_id`
- `active_step_id`
- `parent_flow_id`
- `return_scope`
  - `within_launcher`
  - `within_owner`
  - `none`
- `delivery_mode`
  - `replace_current`
  - `patch_owner`
  - `append_timeline`

这套模型要成为唯一的一等事实来源。

### 6.2 用 step contract 代替 allow-list

每个前台步骤不再靠 action kind 被动推断行为，而是主动声明：

- 这是 launcher step、owner step 还是 terminal step
- 点击后是继续替换当前卡，还是交给 owner 卡 patch，还是只往时间线 append
- 当前 step 能否返回
- 返回的范围是在 launcher 内，还是 owner 内，还是根本不可返回
- 这一步结束时是否封口

也就是说，“怎么投递”不再由 `AllowsInlineCardReplacement(...)` 这类横向 allow-list 决定，而由 step 自己的 contract 决定。

transport 层只负责：

- callback freshness
- old-card reject
- 同步 replace 能不能做成
- 失败时怎么降级为 append

它不再自己猜产品语义。

### 6.3 用统一 child-step 模型承接 picker / path picker / history

`target picker`、`path picker`、`history`、未来类似多步业务卡，本质上都应是同一套机制的不同 step。

它们不该再各自维护一套半独立 runtime。

目标态里：

- path picker 是 owner flow 的 child step
- history 是 owner flow 的 child step
- workspace page 是 launcher flow 下的 owner entry step 或 owner flow 自身
- terminal 页面也是 frontstage flow 的 terminal step

这样“返回上一步”会天然回到当前 flow 的上一个 step，而不是回到某个历史遗留 message id。

### 6.4 菜单进入业务要做成显式 handoff

从菜单进入业务时，必须发生一次显式 handoff：

1. launcher step 结束
2. 生成 owner step
3. 原 launcher 卡封口或终结
4. 后续所有交互都由 owner flow 负责

这样以后就不会再出现：

- 面包屑还像在菜单深层
- 但实际上业务已经切走了
- 返回按钮却还在按菜单语义回跳

进入业务以后，只允许：

- 在 owner 内部返回上一步
- 或结束 / 取消 / 完成

不允许再混回 launcher 的树。

### 6.5 terminal 类型必须单独建模

`help`、`status` 不能再作为“还能返回的预览态”处理。

它们应该显式声明为 terminal step：

- 点击后产生结果
- 结果卡 sealed
- 不保留 launcher 内返回链

这会让系统少掉一大类“业务其实已经完成，但 UI 还假装没完成”的脆弱状态。

## 7. 菜单系统应该怎么接这个框架

菜单不该再是“命令定义 + page builder + intent + allow-list + runtime patch”拼出来的东西，而应是声明式目录。

建议把菜单项接入收敛成三类：

### 7.1 Group Entry

只负责打开下一层 launcher step。

配置项：

- `id`
- `parent_group`
- `label`
- `step_type = launcher_group`

### 7.2 Owner Entry

直接进入某条 owner flow。

配置项：

- `id`
- `parent_group`
- `label`
- `target_flow_kind`
- `initial_step`

例如 workspace 下面的三张独立卡，都应该是这类 entry。

### 7.3 Terminal Entry

点下去直接产出 terminal 结果，不进入可返回 preview。

配置项：

- `id`
- `parent_group`
- `label`
- `terminal_handler`

例如 `help`、`status` 这类就属于这里。

这样新增菜单项时，接入方只需要声明：

- 它属于哪一组
- 进入后是哪种 step / flow
- 它的标题和入口文案是什么

而不是再自己决定 message replacement 路径。

## 8. 业务卡应该怎么接这个框架

每一种多步业务卡，都应该实现统一的 `FlowSpec`，而不是各自带一套散的 DTO 和 runtime 特例。

每个 `FlowSpec` 至少声明：

- `flow_kind`
- `entry_points`
- `initial_step`
- `step graph`
- `render(step_state) -> view`
- `transition(action) -> next_step / effect`
- `completion_policy`
- `cancel_policy`
- `timeline_policy`

其中最关键的是：

### 8.1 返回语义属于 flow，不属于单个按钮临时发挥

以后“返回”必须是框架概念，不是页面上随手塞一个 `/menu xxx` 或其他命令按钮。

也就是说：

- launcher 内返回，由 launcher step graph 决定
- owner 内返回，由 owner step graph 决定
- terminal 没有返回

按钮只是触发 `back` 动作，不再自己编码返回目标。

### 8.2 同卡推进属于默认能力，不是业务自己拼

多步业务如果仍属于同一 owner flow，则默认：

- patch 同一张 owner card
- 更新 step state
- 保留当前 flow id / owner message id

业务代码只关心“下一步是什么”，不用再关心“这一步到底该 replace 还是 append”。

### 8.3 timeline 结果必须显式声明

一个业务如果要把某些内容发到时间线，而不是继续 patch owner card，必须显式声明：

- 这是 notice
- 这是 checkpoint
- 这是 final
- 这是后台输出

这样就不会再出现“实现上刚好 append 了，所以产品上就变成后台 notice”这种倒置关系。

## 9. 这套方案对现有需求的直接好处

### 9.1 加菜单项更容易

因为菜单项只声明“去哪里”，不再声明“怎么投递”。

### 9.2 加业务卡更稳

因为业务卡默认继承 owner-card contract，不再各自手搓返回、封卡和 message tracking。

### 9.3 特殊渲染可以保留，但不再改变语义

比如用户之前要求：

- VS Code mode list 仍保留按钮式菜单
- 其他选项列表改成下拉

这类差异应该只体现在 renderer 层，而不是把业务语义也分叉成两套。

换句话说：

- “按钮渲染”
- “下拉渲染”

只是 step view 的不同模板，不应再决定这一步是不是另一种生命周期。

### 9.4 菜单和业务边界会更清楚

用户体验上最重要的变化不是“更花哨”，而是：

- 进入菜单时很清楚自己还在菜单里
- 进入业务时很清楚 owner 已经换手
- 完成态很清楚已经结束
- 不再看到还能点但其实无意义的旧卡

## 10. 对当前产品要求的复核

现有大方向基本合理，但有两条需要明确坚持，不然技术上还会反复脆弱。

### 10.1 不要允许“半菜单半业务”的混合态长期存在

只要一个页面既想保留菜单返回树，又想承担业务 owner，就会重新走回今天的脆弱状态。

因此后续必须坚持：

- launcher 只做分流
- owner 只做业务
- handoff 明确发生

### 10.2 不要让返回目标由文案或命令文本编码

当前很多脆弱点，本质上是把“返回到哪里”编码进按钮命令文本。

这会导致：

- 文案改了，行为可能跟着变
- 菜单树调整后，旧按钮语义失效
- 同一个“返回”动作在不同地方各自实现

返回必须变成框架里的结构化转移。

## 11. 目标态下建议清理掉的旧机制

如果最终采用这套方案，下面这些旧机制应作为清理对象，而不是继续长期并存：

- 基于 action kind 的 inline replace allow-list
- `command result replacement` 这类用 action 侧推断的替换分支
- 把返回目标写成命令文本的页面级回跳
- `MenuFlowRuntimeRecord` 与各类 active picker/page record 的长期并存
- 由 page builder、projector、ingress 各自猜测 owner 的模式

最终应只保留：

- 统一 frontstage flow runtime
- 统一 step contract
- 统一 delivery contract
- transport 层的 freshness / callback 能力判断

## 12. 实现拆分建议

虽然目标态应该一步到位，不保留旧基座，但工程上仍建议按下面顺序切：

1. 先定义统一 `FrontstageFlow` / `StepContract` / `DeliveryContract` 数据模型。
2. 先把菜单系统改成只发结构化 `step action`，不再靠 slash 文本回跳。
3. 把 workspace / target picker / path picker / history 收进统一 flow runtime。
4. 把 `help` / `status` 这类 terminal step 独立建模。
5. 删掉旧 allow-list 和旧 runtime record。

这里的重点不是“做过渡兼容”，而是保证最终收口时：

- 旧基座彻底删掉
- 新增功能只能走新 flow framework
- 后来的人没有机会继续复用旧链路

## 13. 结论

当前菜单系统脆弱，不是因为某个实现者粗心，也不是因为某个 picker 特别复杂，而是因为：

**产品上本应是一套“前台 owner-card 流程框架”的东西，工程上却被拆成了多套局部状态和多条投递分支。**

要从根上解决，不能再继续补 `if` 和 allow-list，而要把下面三件事变成一等模型：

1. **谁是当前 frontstage flow**
2. **当前 step 的角色是什么**
3. **这一步的投递 contract 是什么**

只要这三件事统一了：

- 菜单新增入口会变容易
- 业务卡接入会自动继承稳定语义
- “改一处乱很多处”的概率才会真正降下来

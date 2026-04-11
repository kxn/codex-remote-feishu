# Feishu 卡片原地更新设计

> Type: `draft`
> Updated: `2026-04-11`
> Summary: 盘点当前 Feishu 卡片交互，明确哪些适合改成点击后原地更新，并给出推荐链路与分阶段落地方案。

## 1. 文档定位

这份文档讨论的是 **Feishu 卡片点击后的原地更新能力** 在当前仓库里的适用范围和落地方案。

目标不是把所有卡片都改成“会动的单页应用”，而是解决当前最明显的交互噪音：

- 同一个决策上下文里，点一下按钮就额外多发一张卡
- 菜单和会话选择卡在同一轮操作里不断堆叠
- 用户只是想“展开更多”或“切一层菜单”，却被迫在消息流里找最新那张卡

这份文档只约束 **卡片式菜单 / 选择卡 / 请求确认卡** 的未来改造方向。

明确不在本文范围内的内容：

- `/help` 的文本帮助呈现
- final reply / 补充预览 / 普通 notice 这类结果型消息的文案改写
- 非 Feishu surface 的 UI 形态

## 1.1 当前实现状态

截至 `2026-04-10`，第一阶段已经落了一条**窄同步回包**链路，但范围刻意收窄，没有把所有卡片都改成动态更新：

- 已实现：
  - `/menu` 首页 <-> 二级分组页原地替换
  - 从 `/menu` 打开的 bare `/mode`、`/autowhip`、`/reasoning`、`/access`、`/model` 参数卡原地替换
  - 参数卡里的“返回上一层”原地替换
  - `/use` -> `show_scoped_threads`
  - `/useall` -> `show_workspace_threads`
  - 上述展开视图中的返回动作：`show_threads`、`show_all_threads`
- 明确还没做：
  - apply 终态卡刷新
  - request prompt / kick confirm 终态卡刷新
  - `/list` / `/use` / `/useall` 接管成功后的终态卡刷新
  - `/upgrade` / 迁移修复这类异步动作的延时更新
  - 共享卡片、`update_multi=true`、历史消息 PATCH

所以当前实现不是“飞书卡片全面可回写”，而是只先收敛**同上下文导航噪音**。

## 2. 背景结论

### 2.1 飞书能力结论

飞书本身支持卡片点击后原地更新，当前至少有三条可用链路：

1. **卡片交互回调同步回包**
   - 点击卡片按钮后，服务端直接在回调响应里返回新的卡片内容
   - 最适合菜单导航、展开更多、表单切换、一步内可完成的即时交互
2. **基于卡片交互 token 的延时更新**
   - 适合按钮点击后还要等待一个短暂后台动作，再把当前卡片改成处理中 / 已完成
3. **PATCH 已发送消息**
   - 适合确实需要在回调时机之外更新历史卡片的场景
   - 但它依赖共享卡片、`update_multi=true` 等额外约束，不适合先拿来做菜单体系

### 2.2 当前仓库为什么还没有

当前实现还停留在“卡片交互只负责把动作送入 orchestrator，后续统一异步发新卡”的模型：

- [internal/adapter/feishu/gateway_runtime.go](../../internal/adapter/feishu/gateway_runtime.go)
  - `OnP2CardActionTrigger(...)` 现在固定返回空的 `CardActionTriggerResponse`
- [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
  - `ActionHandler` 只有异步 action 输入，没有同步回传卡片更新结果的接口
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
  - 当前只有 `send_card`，没有 `update_card`
- [internal/adapter/feishu/gateway_runtime.go](../../internal/adapter/feishu/gateway_runtime.go)
  - 当前构造卡片时没有设置 `update_multi`

所以现在按钮点下去后，即使业务上只是“展开同一张菜单”，表现上也往往会变成“又新发一张卡”。

## 3. 盘点原则

不是所有卡片都值得改成原地更新。

本文采用下面这条判断规则：

### 3.1 适合原地更新

满足下面任一条件，就优先考虑原地更新：

- 用户仍在同一个决策上下文里
- 用户只是切层级、切筛选、展开更多、填写参数
- 原卡片在动作完成后已经失去价值，只会变成噪音

### 3.2 可以原地更新，但不是第一优先级

满足下面任一条件，可以后续再做：

- 是一次性确认动作，点完后更适合把原卡片改成“已处理”
- 操作本身很短，但结果不一定需要新发一张卡
- 现状虽然不够优雅，但还没有菜单导航类那么影响频繁

### 3.3 不建议原地更新

满足下面任一条件，继续保留“追加消息”更合理：

- 这是结果消息、运行记录或系统通知
- 用户后续需要把它当作一个静态记录回看
- 这个动作本身跨越较长时间，原地改写反而会让消息历史失真

## 4. 当前交互盘点

### 4.1 命令菜单类卡片

当前入口主要来自：

- [internal/core/orchestrator/service_command_menu.go](../../internal/core/orchestrator/service_command_menu.go)
- [internal/core/control/feishu_commands.go](../../internal/core/control/feishu_commands.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)

当前属于这一类的卡片包括：

- `/menu` 首页
- `/menu current_work`
- `/menu send_settings`
- `/menu switch_target`
- `/menu maintenance`
- bare `/mode`
- bare `/autowhip`
- bare `/reasoning`
- bare `/access`
- bare `/model`
- `/debug`
- `/upgrade`
- VS Code 迁移 / 修复提示卡

这一类卡片的共同特点是：

- 本质都是“在同一个命令空间里继续钻取或回退”
- 很多按钮只是为了打开下一层卡片，不是为了产生一个独立历史记录
- 如果继续沿用“新发卡片”，消息流会堆很多已经失效的中间菜单

结论：

- **这是最适合第一批改成原地更新的交互面**

### 4.2 会话与目标选择类卡片

当前入口主要来自：

- [internal/core/orchestrator/service_surface.go](../../internal/core/orchestrator/service_surface.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [internal/adapter/feishu/gateway_routing.go](../../internal/adapter/feishu/gateway_routing.go)

当前属于这一类的卡片包括：

- normal mode `/list` 工作区列表
- vscode mode `/list` 在线实例列表
- `/use` 最近会话
- `/use` 里的 `show_scoped_threads`
- `/useall` cross-workspace 会话列表
- `/useall` 里的 `show_workspace_threads`
- 强踢会话确认卡

这类卡片内部还要再拆成两种：

#### 4.2.1 同上下文展开卡

典型动作：

- `/use` 末尾点“当前工作区全部会话”
- `/useall` 里点某个 workspace 的“查看全部”

这类动作点完以后，用户仍然停留在“选一个 thread”这个同一任务里。

结论：

- **强烈建议原地更新**
- 理想表现应是“当前卡片替换成下一层视图”，而不是新发一张卡

#### 4.2.2 一次性接管动作

典型动作：

- 点击 workspace 的“接管”
- 点击 instance 的“接管”
- 点击 thread 的“接管”

这类动作和“展开更多”不一样。用户点完以后，通常已经离开当前选择上下文，转入新的工作态。

结论：

- 可以做原地更新，但不应该把它做成复杂导航
- 更合理的方式是把原卡片改成一个简短终态，例如：
  - `已接管工作区 xxx`
  - `已接管会话 xxx`
  - `已切换到实例 xxx`
- 如果后续还需要明确下一步指引，可以再补发一张新 notice，但不要继续保留一张可点的旧选择卡

### 4.3 请求确认卡

当前入口主要来自：

- [internal/core/orchestrator/service_request.go](../../internal/core/orchestrator/service_request.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [docs/implemented/feishu-request-approval-design.md](../implemented/feishu-request-approval-design.md)

当前按钮包括：

- `允许一次`
- `本会话允许`
- `拒绝`
- `告诉 Codex 怎么改`

这类卡片的现状问题是：

- 点完以后，原卡片仍停留在消息流里，看起来像“还能继续点”
- `告诉 Codex 怎么改` 进入 capture mode 之后，卡片没有同步切到“等待你发送反馈”的状态

结论：

- **适合第二批改成原地更新**
- 建议行为：
  - `允许一次` / `本会话允许` / `拒绝`：原卡片改成已处理终态，并禁用按钮
  - `告诉 Codex 怎么改`：原卡片改成“已进入反馈模式，等待下一条文本”

### 4.4 升级 / 迁移 / 维护动作卡

当前入口主要来自：

- [internal/app/daemon/app_upgrade.go](../../internal/app/daemon/app_upgrade.go)
- [internal/app/daemon/app_vscode_migration.go](../../internal/app/daemon/app_vscode_migration.go)

典型卡片包括：

- `/debug` 状态卡
- `/upgrade` 状态卡
- “发现可升级版本”
- “VS Code 接入需要迁移”
- “VS Code 接入需要修复”

这类卡片的特点是：

- 操作往往不是纯前端导航，而是会触发真实后台动作
- 有些动作可能不是瞬间完成

结论：

- 可以利用原地更新，但优先级低于菜单和 thread 选择
- 更适合放到“第二阶段或第三阶段”处理

### 4.5 不建议改成原地更新的卡片

当前仍应继续保留追加消息语义的卡片包括：

- `/help`
- `/status`
- final reply
- preview supplement
- 普通系统 notice
- headless 恢复提示
- 过期卡片提示
- 任何本质上是在记录一次结果，而不是承载一个正在进行中的菜单上下文的卡片

原因很简单：

- 这些卡片更接近“日志”或“结果”
- 历史消息的稳定性，比把它们改成动态卡片更重要

## 5. 推荐改造清单

### 5.1 第一优先级：同上下文导航卡

建议优先改造下面这些交互：

| 交互 | 当前问题 | 推荐改法 | 推荐链路 |
| --- | --- | --- | --- |
| `/menu` 首页 <-> 分组页 | 每切一层都新发卡片 | 直接替换当前菜单卡 | 回调同步回包 |
| `/menu` -> `/mode` `/reasoning` `/access` `/model` | 从菜单进入参数卡时消息流堆叠 | 用当前卡片替换成参数卡 | 回调同步回包 |
| 参数卡里的返回上一级 | 返回操作又发新卡 | 直接替换为上一级菜单 | 回调同步回包 |
| `/use` -> `show_scoped_threads` | 只是展开更多却新发卡 | 当前卡片替换成“当前工作区全部会话” | 回调同步回包 |
| `/useall` -> `show_workspace_threads` | 只是看某个 workspace 全部会话却新发卡 | 当前卡片替换成该 workspace 详情 | 回调同步回包 |

这一阶段的目标不是“动作成功后自动刷新所有状态”，而是先把 **菜单导航噪音** 去掉。

### 5.2 第二优先级：一次性决策卡终态化

建议第二批改造下面这些交互：

| 交互 | 当前问题 | 推荐改法 | 推荐链路 |
| --- | --- | --- | --- |
| `/reasoning` `/access` `/mode` `/autowhip` 的应用按钮 | 点完后卡片和当前状态可能脱节 | 原卡片直接刷新为最新状态，并高亮当前值 | 回调同步回包 |
| `/model` 表单提交 | 提交后常要再看一张新状态卡 | 原卡片刷新为当前模型配置 | 回调同步回包 |
| request prompt | 按钮点完后旧卡仍像可操作 | 改成“已处理”或“等待反馈”终态 | 回调同步回包 |
| kick confirm | 确认后旧卡仍在 | 改成“已强踢”或“已取消” | 回调同步回包 |
| `/list` / `/use` 的接管按钮 | 操作完成后旧选择卡仍滞留 | 原卡片缩成简短终态 | 回调同步回包 |

这一阶段的重点是：

- 让按钮卡片点完后不再“悬空”
- 不再让用户看到一堆已经失效、但视觉上还像可点的旧卡

### 5.3 第三优先级：短时异步动作

建议最后再处理下面这些场景：

| 交互 | 特点 | 推荐改法 | 推荐链路 |
| --- | --- | --- | --- |
| `/upgrade latest` | 可能跨越短暂后台流程 | 先切“处理中”，再切“已完成 / 失败” | token 延时更新 或 PATCH |
| `/upgrade local` | 同上 | 同上 | token 延时更新 或 PATCH |
| VS Code 迁移 / 修复 | 可能需要后台检查或写入 | 先切“处理中”，后切结果 | token 延时更新 或 PATCH |

这一阶段才需要认真引入延时更新或消息 PATCH。

原因是：

- 这类动作不一定能在卡片回调的同步窗口里完成
- 直接上 PATCH 会把第一阶段该做的事情复杂化

## 6. 推荐实现策略

### 6.1 默认策略：先做“回调同步回包更新”

对于菜单和 thread 选择类交互，推荐默认走：

1. Feishu 收到卡片点击
2. 网关把 action 送进服务层
3. 服务层直接返回“替换当前卡片”的结果
4. 网关把这个结果编码进 `CardActionTriggerResponse.Card`

这样做的好处：

- 最贴近“点一下就展开 / 切层级”的产品语义
- 不依赖共享卡片配置
- 不需要引入 `update_multi=true`
- 不需要按消息 ID 找回并 PATCH 历史卡片

### 6.2 第二策略：为卡片点击补一条“延时更新”能力

当某个动作点下去后不能立刻拿到最终结果，但仍希望原地反映状态时，再补：

- 基于卡片回调 token 的延时更新

适用场景：

- upgrade
- migrate / repair
- 其他耗时几秒但仍希望原卡片不要悬空的动作

### 6.3 最后策略：谨慎引入消息 PATCH

只有在下面情况同时满足时，才考虑 `PATCH /messages/:message_id`：

- 这个更新不是一次即时点击回调里的结果
- 确实要改写已发送历史卡片
- 接受共享卡片与 `update_multi=true` 的约束

菜单体系不建议把 PATCH 作为第一阶段前提。

## 7. 对当前架构的建议改动

### 7.1 新增“卡片交互同步结果”通路

当前最关键的结构缺口，不在业务逻辑，而在网关接口。

建议目标形态是：

- 普通文本 / 菜单点击 / 其他事件，继续走当前异步 action 流
- **卡片点击额外允许返回一个同步 card update 结果**

这个结果至少需要表达：

- 是否替换当前卡片
- 新卡片标题 / body / elements
- 可选 toast

### 7.2 保留现有 `UIEvent -> send_card` 体系

不建议把所有 UIEvent 都改造成同步回包模型。

更合理的分工是：

- **append-only** 的结果消息，继续走现有 `UIEvent -> OperationSendCard`
- **same-context** 的卡片导航，走新的同步 update 通路

这样不会把当前消息投影体系整体推翻。

### 7.3 让原地更新成为显式产品决策，而不是隐式副作用

建议在服务层明确区分两种结果：

- `send_new_card`
- `replace_current_card`

不要靠“某些 action 恰好没发新卡，所以看起来像更新了”这种隐式行为拼出来。

## 8. 建议的分阶段实施顺序

### 阶段 1

先做最确定、收益最大的同上下文导航：

- `/menu` 首页与分组导航
- `/menu` -> 参数卡切换
- 参数卡返回上一级
- `/use` 的 `show_scoped_threads`
- `/useall` 的 `show_workspace_threads`

验收标准：

- 上述动作全部不再新发卡片
- 同一轮菜单导航始终只操作一张卡

当前状态：

- 已实现，但范围只覆盖上述导航与返回动作
- 技术策略是 card callback 同步回包替换整张卡，不引入共享卡片或 PATCH

### 阶段 2

再做一次性决策卡终态化：

- bare `/mode`
- bare `/autowhip`
- bare `/reasoning`
- bare `/access`
- bare `/model`
- request prompt
- kick confirm
- `/list` / `/use` / `/useall` 的接管终态

验收标准：

- 这些按钮点完后，旧卡片不再看起来“还可继续点击”

### 阶段 3

最后再扩展到短时异步动作：

- `/upgrade`
- `/debug track`
- VS Code migrate / repair

验收标准：

- 按钮点击后，原卡片能反映“处理中 / 完成 / 失败”
- 不需要再依赖额外 notice 才知道动作是否生效

## 9. 本轮明确不做的事情

- 不把 `/help` 改成动态卡片导航
- 不把 final reply 改成原地可编辑消息
- 不追求所有 notice 都能回写成一张动态卡
- 不在第一阶段引入共享卡片与 `update_multi=true` 作为前置条件

## 10. 官方能力参考

- 飞书卡片交互回调: https://open.feishu.cn/document/feishu-cards/card-callback-communication
- 飞书卡片交互配置: https://open.feishu.cn/document/feishu-cards/configuring-card-interactions
- 更新应用发送的消息: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch

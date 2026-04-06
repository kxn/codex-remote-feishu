# Feishu Headless 实例设计

> Type: `inprogress`
> Updated: `2026-04-06`
> Summary: 标记为进行中文档并迁移到 `docs/inprogress`，保留当前 headless 实例方案与未收尾部分。

## 1. 文档定位

这份文档描述的是 **Feishu 侧创建和管理 headless Codex app-server 实例** 的产品设计与可行性方案。

文档目标是解决这样一个真实使用场景：

- 用户不在电脑前
- VS Code 没开，无法依赖本地窗口拉起 wrapper / app-server
- 但仍希望在飞书里临时创建一个可用实例，继续对已有 thread 发消息

本文只讨论 **V1 推荐方案**，重点覆盖：

- 菜单与 slash command
- 交互流程
- 状态机
- 实例生命周期与回收
- 与当前 relay 架构的衔接方式

截至 **2026-04-05**，本文描述的是本次准备实施的目标版本；文中只保留当前确定要做的一体化方案，不再按多阶段拆分。

## 2. 背景与问题

当前系统中的在线实例，主要由 VS Code 侧拉起：

- 用户在工作目录打开 Codex / VS Code
- 本地 shim / wrapper 连到 `relayd`
- Feishu 通过 `/list`、`/attach`、`/use` 等操作接管该实例

这个模式的问题是：

- 如果电脑没开、VS Code 没开、或者当前人不在机器前
- Feishu 端就没有可接管实例
- 用户只能等待本地环境恢复

用户提出的新需求是：

- 允许在飞书中直接创建一个 **headless 实例**
- 这个实例运行在服务器上，不依赖 VS Code UI
- 为了降低复杂度，V1 不做“飞书里选目录新建 thread”
- V1 只做“恢复已有 thread”

## 3. 设计目标

### 3.1 目标

- 在 Feishu 端创建一个 detached 的 headless `codex app-server` 实例。
- 让该实例像普通在线实例一样进入当前 relay 体系。
- 先从 daemon 已知状态里选择一个可恢复 thread，再按该 thread 的 `cwd` 拉起实例。
- 恢复后，后续消息可以继续发给这个 thread。
- 给出明确的 kill / detach / stop 语义，避免混淆。
- 避免因为误操作或遗留实例导致服务器上堆积大量孤儿进程。

### 3.2 非目标

V1 不做：

- 在飞书中选择任意工作目录
- 新建全新 thread
- 在创建前列出“全局 thread 历史”
- 自动替换当前已 attach 的实例
- 全局实例管理后台
- 完整的 headless 实例池化或多实例编排

## 4. 核心结论

### 4.1 推荐命令

V1 推荐新增两个生命周期命令：

- 菜单事件：`new_instance`
- 菜单事件：`kill_instance`
- slash command：`/newinstance`
- slash command：`/killinstance`

不建议使用过于宽泛的 `new`：

- 语义不清楚
- 后续如果增加其他“新建”能力会冲突
- 不利于排查日志与菜单配置

### 4.2 推荐交互策略

**V1 要求当前 Feishu 会话必须先 `detach`，才能执行 `new_instance`。**

不推荐 V1 直接做“自动替换当前 attach 实例”，原因是：

- 当前 surface 可能有 queue item、active turn、pending request
- 自动切换实例会让状态机变复杂
- 如果新实例启动失败，回滚体验也很差

因此推荐保守策略：

1. 当前 surface 已 attach 时，`new_instance` 直接返回提示
2. 用户先 `/detach`
3. 再 `/newinstance`

### 4.3 推荐恢复策略

V1 不要求在“创建时立即 native `thread/resume`”。

推荐行为是：

1. 用户先从 daemon 当前已知的可恢复 thread 列表里选一个 thread
2. daemon 用该 thread 的 `cwd` 拉起 headless wrapper/app-server
3. 实例连上 relay 后，surface 自动 attach 到这个新实例，并把该 thread 设为当前目标
4. 用户后续第一条正常消息发出时，沿用当前现有 remote prompt 路径，自动执行 `thread/resume`

这样做的原因是：

- 当前远端协议里还没有单独的 `thread.resume` 命令种类
- 现有 remote prompt 发送链路已经稳定支持：
  - 先 `thread/resume`
  - 再 `turn/start`
- 可以最大化复用已有逻辑，避免引入新的协议面

这意味着：

- 从产品语义上看，用户“选择了恢复目标”
- 从底层协议上看，真正的 native `thread/resume` 会发生在第一条消息发送时

这在 V1 是可以接受的。

## 5. 推荐产品形态

### 5.1 用户主流程

主流程如下：

1. 用户当前未 attach 任何实例
2. 点击菜单 `new_instance` 或发送 `/newinstance`
3. 机器人立即返回一张选择卡片：`选择要恢复的会话`
4. 这张卡片只展示 daemon 当前已经知道、且带有 `cwd` 的最近 5 个 thread
5. 用户点选一个 thread
6. daemon 以该 thread 的 `cwd` 作为启动目录，在后台拉起 detached 的 headless wrapper/app-server
7. surface 进入 `headless_launching`
8. 实例连上 relay 后，系统自动 attach 到这个新实例，并把该 thread 设为当前目标
9. 后续第一条消息会沿现有 remote prompt 路径自动 `thread/resume`

### 5.2 选择卡片

V1 推荐复用当前 `/use` 的选择卡片样式，不另做新视觉组件。

区别仅在于：

- 标题从 `最近会话` 改为 `选择要恢复的会话`
- 提示文案改成与 headless 创建语境一致
- prompt kind 单独区分，不和普通 `/use` 复用同一个内部语义

推荐标题：

- `选择要恢复的会话`

推荐 hint：

- **只显示最近 5 个**
- **不提供 `newinstanceall`**
- **只展示当前 daemon 已知、且带 `cwd` 的可恢复会话**

理由：

- 需求本身是“在外面临时用”
- 最近 5 个已经足够覆盖主路径
- 可以避免再新增一组几乎重复的命令

## 6. 为什么现在改成“先选 thread 再启动”

当前产品约束已经明确：

- headless 的目的就是 **恢复已有 thread**
- thread 自己已经带有工作目录信息
- 我们反而是为了避免回退到 `$HOME`，才要求一定要先选 thread 再启动

因此现在的推荐顺序是：

1. 先使用 daemon 当前已知状态中的可恢复 thread 列表
2. 只有 thread 带有 `cwd`，才允许进入可选列表
3. 用户选定后，daemon 直接以这个 `cwd` 启动 headless 实例

这条路径的直接好处是：

- 不需要再假设默认目录
- 不需要在飞书里做目录选择器
- 启动后的实例天然落在正确项目目录

它的前提也很明确：

- 如果 daemon 当前还没有任何可恢复 thread，就不能创建 headless
- 这时要直接返回 notice，而不是偷偷回退到 `$HOME`

## 7. 命令与语义

### 7.1 `/newinstance`

语义：

- 创建一个新的 headless 实例，并进入“选择恢复 thread”流程

前置条件：

- 当前 surface 未 attach 实例
- 当前 surface 没有 headless 创建中的 pending 状态

失败提示建议：

- 已 attach 时：`当前会话已接管实例，请先 /detach 再创建 headless 实例。`
- 正在创建时：`当前会话已有 headless 实例创建中，请等待完成或执行 /killinstance 取消。`

### 7.2 `/killinstance`

语义：

- 杀掉当前 surface 正在使用的 headless 实例
- 如果当前存在 headless 创建中的 pending 实例，也允许用它取消创建

限制：

- 只允许 kill **headless 实例**
- 不允许 kill 普通 VS Code 实例

失败提示建议：

- 未 attach：`当前没有可结束的 headless 实例。`
- attach 的是 VS Code 实例：`当前接管的是 VS Code 实例，不能使用 /killinstance。`

### 7.3 `/detach`

语义保持不变：

- 只解除当前 surface 和实例的绑定
- 不杀实例

### 7.4 `/stop`

语义保持不变：

- 只中断当前 turn
- 不 detach
- 不 kill 实例

## 8. 状态机设计

建议在 surface 维度引入一个独立的 headless 启动状态，而不是把它混进现有 attach 状态里猜。

### 8.1 Surface 状态

V1 推荐引入以下概念状态：

- `idle_detached`
  - 未 attach
  - 无 headless 创建中任务
- `headless_launching`
  - 已提交创建请求
  - 进程尚未连上 relay
- `headless_selecting_thread`
  - 新实例已上线
  - 正在等待用户从最近 thread 中选择恢复目标
- `attached_headless`
  - 已 attach 到 headless 实例
- `attached_normal`
  - 已 attach 到普通 VS Code 实例

### 8.2 关键转移

- `idle_detached` -> `/newinstance` -> `headless_selecting_thread`
- `headless_selecting_thread` -> 用户选 thread -> `headless_launching`
- `headless_launching` -> 实例上线 -> `attached_headless`
- `attached_headless` -> `/detach` -> `idle_detached`
- `headless_launching` / `headless_selecting_thread` / `attached_headless` -> `/killinstance` -> `idle_detached`

## 9. 实例模型设计

### 9.1 设计原则

headless 实例应该被视为“实例来源的一种”，而不是另一套平行产品。

也就是说：

- 它仍然是一个正常的在线 instance
- 仍然进入 `/list`
- 仍然可以 `/attach`
- 仍然可以 `/status`
- 区别只是多一个来源标记：`headless`

### 9.2 建议实例元数据

建议在实例记录中增加如下字段：

- `Source`: `vscode` / `headless`
- `Managed`: `true/false`
- `PID`
- `CreatedAt`
- `OwnerSurfaceSessionID`
- `OwnerUserID`

其中：

- `Source=headless` 用于产品展示与 kill 权限判断
- `Managed=true` 表示这个进程由 daemon 主动拉起，可被 daemon 回收

### 9.3 `/list` 展示

推荐把 headless 实例在列表中明确标记。

例如：

- `[Headless] dl`
- `[VS Code] simplefq`

或者更自然一点：

- `dl (Headless)`
- `simplefq`

V1 推荐简单方案：

- 只给 headless 打标签
- 普通 VS Code 实例维持现状

## 10. 启动与工作目录

### 10.1 V1 工作目录策略

V1 不在飞书里让用户选目录，也不允许回退到 `$HOME`。

推荐策略：

- 只有带 `cwd` 的可恢复 thread 才能出现在选择卡片里
- 用户选中后，daemon 直接用该 thread 的 `cwd` 启动 headless 实例
- 如果 thread 没有 `cwd`，就视为不可恢复，不进入候选列表

### 10.2 为什么这个策略更合适

因为这个需求本来就是为了：

- 避免没有本地 VS Code 时落到错误目录
- 在外部场景里尽量复用已有 thread 的上下文

所以这里不应该引入“默认 home 目录”这种兜底行为。

## 11. 进程生命周期与回收

### 11.1 创建

daemon 负责拉起 detached 进程，推荐复用现有 detached 启动范式。

建议实际启动目标仍然是：

- `codex-remote app-server ...`

而不是直接绕过 wrapper 起裸 `codex.real`。

原因：

- 这样实例仍然走现有 wrapper -> relay 协议翻译链路
- 可以继续复用统一日志、原始帧日志、错误上报和后续产品能力

### 11.2 kill

`/killinstance` 的语义是：

1. 如果当前是创建中实例
   - 取消该创建流程
   - 杀掉对应进程
   - 清理 pending 状态
2. 如果当前 attach 的是 headless 实例
   - 向进程发送优雅退出
   - 超时后强制 kill
   - surface 自动 detach
   - 清空与该实例相关的 headless 临时状态

### 11.3 自动回收

V1 必须有垃圾回收，否则 headless 很容易堆积。

推荐两级回收：

- 选择提示超时
  - `/newinstance` 发出的选择卡片 10 分钟有效
  - 过期后不启动任何进程
- 启动超时回收
  - 用户选中 thread 后，如果实例在启动窗口内没有连上 relay
  - 自动 kill 已拉起进程并清理 pending 状态
- 空闲回收
  - headless 实例无 active turn，且没有任何 surface attach 持续 2 小时
  - 自动 kill 该实例

可配置项建议：

- `headless_prompt_timeout`
- `headless_launch_timeout`
- `headless_idle_ttl`
- `headless_kill_grace_period`

## 12. 异常与边界处理

### 12.1 启动失败

直接通过现有 structured error reporting 链路回飞书卡片：

- layer: `daemon`
- stage: `headless_start`
- operation: `new_instance`

### 12.2 daemon 当前没有可恢复 thread

处理建议：

- 直接返回 notice：`当前没有可恢复会话。请先让任一实例上报过 thread 列表后再创建 headless 实例。`
- 不启动任何进程

### 12.3 用户在创建中又发普通消息

建议不要偷偷入队。

推荐返回 notice：

- `headless 实例仍在创建中，请等待完成。`

### 12.4 用户在 selecting_thread 阶段发普通消息

推荐返回 notice：

- `请先选择一个要恢复的会话。`

不建议在这个阶段把消息默默入队，因为：

- 当前还没有明确的 thread 目标

### 12.5 误杀普通实例

必须防止。

`/killinstance` 必须检查：

- 当前实例 `Source == headless`

否则只返回 notice，不执行 kill。

## 13. 与现有 `/use` 的关系

V1 不建议把 `new_instance` 直接复用成普通 `/use`。

但建议最大化复用：

- 卡片布局
- 选项展示
- 点击/数字选择机制

不复用的部分是：

- prompt kind
- 选择后的业务语义

普通 `/use`：

- 切换当前 attach 实例的目标 thread

headless 创建后的选择：

- 决定一个尚未 attach 的新实例应该恢复哪个 thread

这是两个不同的状态机节点，内部语义应分开。

## 14. 实施范围

本次直接做一个合并后的完整版本，不再分三阶段。

实施内容：

- 新增 `new_instance` / `kill_instance` action 与 slash command
- surface 级 recoverable-thread 选择提示
- 选择后按 thread `cwd` 启动 detached headless wrapper/app-server
- surface 级 pending launch 状态
- headless 实例来源标记、PID 与 managed 信息
- headless 实例的 attach / kill / launch-timeout / idle-ttl
- `/status` 与实例列表中的 headless 标签
- headless 启动链路上的结构化错误回传
- 自动化测试覆盖 orchestrator / daemon / gateway / projector

## 15. 最终推荐决策

V1 推荐采用下面这组明确结论：

- 命令名使用 `/newinstance` 与 `/killinstance`
- 菜单事件使用 `new_instance` 与 `kill_instance`
- `new_instance` 仅允许在当前 surface 已 `detach` 时执行
- 先使用 daemon 当前已知的 recent threads 让用户选一个可恢复 thread
- 只显示最近 5 个、且带 `cwd` 的可恢复 thread
- 用户选中后，再以该 thread 的 `cwd` 启动 headless 实例
- 实例连上 relay 后：
  - 自动 attach 新实例
  - 自动绑定所选 thread 为当前目标
  - 真正的 native `thread/resume` 在用户下一条消息发送时发生
- `kill_instance` 只允许杀当前 headless 实例，不允许杀 VS Code 实例
- 必须带自动回收机制

这是当前架构下最符合实际约束的版本：不依赖默认 home，不引入目录选择器，同时能保证 headless 实例从一开始就在正确的项目目录里恢复已有 thread。

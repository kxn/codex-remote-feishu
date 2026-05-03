# Configuration State Storage Guidelines

> Type: `general`
> Updated: `2026-05-03`
> Summary: 新增配置项存储决策规则，区分显示配置、本地副作用配置、启动合同、backend 行为默认值和 backend 可变状态。

## 1. 适用范围

这份规范适用于所有以“参数、开关、档位、profile、provider、mode、override”形式影响 Codex Remote Feishu 行为的配置项。

典型入口包括：

- 飞书命令和菜单项，例如 `/model`、`/reasoning`、`/access`、`/plan`、`/verbose`
- 本地自动化开关，例如 AutoContinue、AutoWhip
- backend / mode / provider / profile 切换
- Web admin / Web setup 中新增的默认值、profile 字段或运行时偏好
- wrapper / relay / daemon 从 backend 观测并回传的配置状态

新增配置项前，必须先按本文完成存储分类，而不是先找一个现有 state 字段临时复用。

## 2. 基本原则

### 2.1 先判定影响层，再决定存储

配置项必须先归类到以下影响层之一：

1. 只影响展示
2. 只影响本地自动化，但有执行副作用
3. 影响本地路由或 backend 启动合同
4. 影响 backend 行为，且 Codex Remote Feishu 是唯一修改入口
5. 影响 backend 行为，但 backend 或其他客户端也可能修改

不能只根据“用户希望下次还在不在”来决定是否持久化。持久化本质上是在声明 source of truth，必须先确认这个配置是否真的应由本系统持有真相。

### 2.2 Desired state 和 observed state 分开

本地用户设置得到的是 desired state。backend 上报得到的是 observed state。

- desired state 表示 Codex Remote Feishu 希望后续行为是什么
- observed state 表示 backend 当前真实状态或历史记录是什么
- observed state 不能无条件覆盖 desired state
- desired state 也不能无条件压回 backend，尤其当 backend 本身允许用户或 LLM 主动改变状态时

如果一个配置同时存在 desired 和 observed 两面，数据结构、字段名和展示文案必须能明确区分这两层。

### 2.3 Prompt 入队时必须冻结执行参数

对下一条 prompt 生效的配置，在 queue item 创建时必须冻结。

冻结后：

- 已入队消息不受后续配置修改影响
- 正在运行的 turn 不受后续配置修改影响
- AutoContinue / AutoWhip 等自动续发逻辑应复用原 turn 的冻结参数，除非产品明确要求重新读取最新设置

这可以避免一个 surface 上后发的配置修改意外污染已经排队的用户输入。

### 2.4 持久化 key 必须包含隔离维度

持久化 key 必须覆盖实际会影响语义的隔离维度。

常见维度包括：

- `surfaceSessionID`
- backend: `codex` / `claude`
- product mode: headless / VS Code
- `workspaceKey`
- Codex `providerID`
- Claude `profileID`

如果某个配置值可能只对某个 provider 或 profile 有效，key 里必须包含 provider/profile 维度。不要只用 `backend + workspaceKey` 存模型名或 reasoning 档位，否则切换 provider/profile 后可能复用不兼容的默认值。

当前 `Root.WorkspaceDefaults` 的持久化 key 必须按 backend 身份隔离：

- Codex: `codex + codexProviderID + workspaceKey`
- Claude: `claude + claudeProfileID + workspaceKey`

旧版 `backend + workspaceKey` 只能作为兼容读取或迁移来源，不应作为新写入目标。

## 3. 配置分类规则

### 3.1 Display Only

定义：只影响飞书或 Web UI 如何展示，不改变 backend 执行行为，也不会自动发起动作。

建议存储：

- 可以持久化
- 默认 key 为 `surfaceSessionID`
- backend 不需要上报
- backend 上报不应影响该配置

当前例子：

- `/verbose`

设计要求：

- 这类配置可以在 daemon 重启后恢复
- 文案应明确它只影响前端展示
- 不应放入 prompt override 或 backend launch contract

### 3.2 Local Only With Side Effects

定义：配置只在本地 orchestrator 生效，但会自动发起用户可见动作或改变执行流。

建议存储：

- 默认不持久化
- key 只在内存 runtime record 内存在
- daemon 重启后丢失
- backend 不需要上报

当前例子：

- AutoContinue
- AutoWhip

设计要求：

- 这类开关开启后可能自动发送 prompt、自动重试或自动追加任务
- 服务重启后不应静默恢复，避免用户以为已经停止但系统继续自动行动
- UI 文案必须说明重启不会恢复

### 3.3 Local Routing / Launch Contract

定义：决定当前 surface 接下来连接哪类 runtime，或用哪个 provider/profile 启动 backend。

建议存储：

- 持久化 desired state
- 默认 key 为 `surfaceSessionID`
- provider/profile 配置本体按 `providerID` 或 `profileID` 保存
- backend hello 上报只作为 actual state，用于确认、显示和排障，不应反向覆盖 desired state

当前例子：

- `/mode`
- `/codexprovider`
- `/claudeprofile`

设计要求：

- 这类配置是 Codex Remote Feishu 的启动合同，系统应作为 SSOT
- 如果 desired state 与 backend actual state 不一致，应进入重启、重连、重新准备或错误提示流程
- 不应把 provider/profile 切换混入 prompt override 语义

### 3.4 Backend Behavior With Local SSOT

定义：配置会改变 backend 行为，并且确认只有 Codex Remote Feishu 一个入口会修改它；修改后 backend 会按该值持续执行。

建议存储：

- 持久化 desired state
- key 必须包含 backend、workspace 和 backend 配置维度
- 推荐 key 结构：
  - Codex: `codex + codexProviderID + workspaceKey`
  - Claude: `claude + claudeProfileID + workspaceKey`
- backend 上报用于校验 actual state，不应直接覆盖 desired state

候选例子：

- Codex headless 默认 model
- Codex headless 默认 reasoning
- Codex headless 默认 access
- Claude headless profile 默认 reasoning，前提是该默认值作为 profile 或 profile/workspace 默认值实现

设计要求：

- 只有确认 backend 不会由其他入口修改时，才能归入这一类
- 如果值只适用于某个 provider/profile，必须把 provider/profile 放进 key
- 如果配置会影响新启动的 backend，必须明确是“即时生效”还是“下次启动/重启生效”
- 如果需要重启 backend 才能生效，UI 必须反馈该事实

### 3.5 Backend Behavior With Shared Authority

定义：配置会改变 backend 行为，但 backend、LLM、VS Code 或其他本地客户端也可能改变它。

建议存储：

- 不把本地 desired state 当唯一真相
- 必须同时维护 observed state
- 本地设置默认只作为一次请求或下一条 prompt 的冻结 override
- 是否持久化 desired state 必须单独设计，并明确不会和 backend 主动变更打架

当前或疑似例子：

- VS Code 模式下的 model / reasoning / access / plan
- Claude plan mode
- Codex plan mode，仍需确认 Codex backend 是否可能主动退出或改变

设计要求：

- backend 上报或 session catalog 观测到的状态应能更新 observed state
- 如果 backend 主动退出某状态，例如 Claude `ExitPlanMode`，本地不应因为旧的持久 desired state 又强行切回去
- 飞书端 UI 应展示“当前 backend 状态”和“下一条飞书消息覆盖”之间的区别
- 没有完成底层调研前，不应把这类配置做成永久默认值

## 4. 当前能力的建议归类

| 配置 | 建议分类 | 建议持久化 | 建议 key | 备注 |
| --- | --- | --- | --- | --- |
| `/verbose` | Display Only | 是 | `surfaceSessionID` | 当前行为基本合理。 |
| AutoContinue | Local Only With Side Effects | 否 | runtime memory only | 当前不持久化是合理行为。 |
| AutoWhip | Local Only With Side Effects | 否 | runtime memory only | 当前不持久化是合理行为。 |
| `/mode` | Local Routing / Launch Contract | 是 | `surfaceSessionID` | desired state 应由本系统持有。 |
| `/codexprovider` | Local Routing / Launch Contract | 是 | surface: `surfaceSessionID`; config: `providerID` | provider 定义和 surface 选择应分开。 |
| `/claudeprofile` | Local Routing / Launch Contract | 是 | surface: `surfaceSessionID`; config: `profileID` | profile 定义和 surface 选择应分开。 |
| Codex headless model | Backend Behavior With Shared Authority | 本地不从 observed config 持久化 workspace default | Codex thread metadata + prompt frozen override | `model` 可由 Codex thread metadata 维持；本系统只记录 thread observed state 和入队冻结值。 |
| Codex headless reasoning | Backend Behavior With Shared Authority | 本地不从 observed config 持久化 workspace default | Codex thread metadata + prompt frozen override | `reasoning` 可由 Codex thread metadata 维持；本系统只记录 thread observed state 和入队冻结值。 |
| Codex headless access | Backend Behavior With Shared Authority | 否 | surface override + prompt frozen override | 不按 model/reasoning 同级别推断为 thread persisted default，不写 `Root.WorkspaceDefaults`。 |
| Codex headless plan mode | Backend Behavior With Shared Authority | 不跨 daemon resume 持久化 | live surface runtime + prompt frozen override | live session 内 sticky；不是 thread persisted default，surface resume 不恢复 Codex plan。 |
| Claude profile model | Local Routing / Launch Contract / profile config | 是 | `profileID` | 不应开放飞书 `/model` 临时改 Claude model。 |
| Claude profile reasoning 默认值 | Backend Behavior With Local SSOT | 是 | `profileID` 或 `claude + claudeProfileID + workspaceKey` | 若作为全局 profile 默认，用 `profileID`；若允许工作区覆盖，再加 `workspaceKey`。 |
| Claude access | Backend Behavior With Shared Authority，待确认 | 待定 | 待定 | 需要确认 Claude permission mode 是进程级、session 级还是持久状态。 |
| Claude plan mode | Backend Behavior With Shared Authority | 默认不做强持久 | observed state / per-turn override | Claude 可通过 `ExitPlanMode` 主动退出，本地不能强行恢复旧 plan。 |
| VS Code 下 model / reasoning / access / plan | Backend Behavior With Shared Authority | 默认不作为本地 SSOT | observed state + per-turn override | VS Code 端也可能修改，飞书不应覆盖 VS Code 最新状态。 |

## 5. 新增配置项决策流程

新增配置项时按以下顺序判断：

1. 它是否只影响显示？
   - 是：归入 Display Only，可按 `surfaceSessionID` 持久化。
2. 它是否会自动发起动作、自动发送 prompt 或自动改变执行流？
   - 是：归入 Local Only With Side Effects，默认不持久化。
3. 它是否决定 backend 类型、provider、profile 或启动参数？
   - 是：归入 Local Routing / Launch Contract，持久化 desired state，并用 backend hello 校验 actual state。
4. 它是否改变 backend 行为？
   - 否：不要放进 prompt/backend 配置路径。
5. backend 或其他客户端是否也可能改它？
   - 是：归入 Backend Behavior With Shared Authority，先设计 observed state，再讨论 desired state 是否需要持久化。
   - 否：归入 Backend Behavior With Local SSOT，持久化 desired state。
6. 它是否和 provider/profile/workspace 强相关？
   - 是：key 必须包含对应隔离维度。
7. 它是否需要重启 backend 才能生效？
   - 是：命令和 UI 必须明确反馈“正在重新准备”或“下次启动生效”。

## 6. 需要补调研的底层行为

以下行为不能靠产品假设决定，必须通过代码或黑盒测试确认：

- VS Code 端修改 model / reasoning / access / plan 时，wrapper 是否能完整观测并上报
- Claude permission mode 的真实作用域：进程级、session 级、thread 级还是临时状态
- Claude reasoning 运行中能否无损切换；若不能，重启 headless 是否是唯一正确方式

在这些调研完成前，对应配置不得直接做成强持久 SSOT。

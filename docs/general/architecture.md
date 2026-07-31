# 架构

> Type: `general`
> Updated: `2026-07-31`
> Summary: 对齐当前统一二进制入口、兼容 launcher 与实际目录结构，并补充 daemon 作为组合根的 runtime owner 收口、durable JSON store 的统一 fail-closed load policy、Feishu room primary 的 daemon snapshot 热路径、Feishu gateway 级 bot capability 单写源与 surface 执行投影、Feishu adapter 的 controller/gateway/projector/preview 边界、Feishu surface identity 与 ordinary inbound planner 的单一实现、gateway-local FIFO lane、orchestrator service-owned UI/runtime cluster、`control.Action` 的 request / owner-flow family 收口、`UIEvent` 的 view-kind 命名、daemon 本地 Feishu tool listener 的 MCP-native streamable HTTP 协议面、current-surface 显式图片/文件投递 contract，以及 editor 侧共享 VS Code bundle entrypoint 探测边界。

## 1. 当前状态

当前仓库只维护 Go 版本实现。

旧的 Node.js / Rust 版本已经不再随仓库发布，也不是当前文档和测试的讨论对象。

## 2. 运行角色与入口

当前产品入口已经收敛到统一二进制：

1. `codex-remote`
   - 无参数时默认进入 `daemon` role
   - `install` role 负责 bootstrap、写配置和启动 WebSetup
   - `app-server` / `wrapper` role 负责包装真实 `codex`

仓库里仍保留三个兼容 launcher：

1. `relayd`
2. `relay-wrapper`
3. `relay-install`

它们都只是对同一套 `launcher` 的兼容入口，不再是 release 用户的主产品入口。

逻辑上仍保留三层边界：

- wrapper
- server/orchestrator
- Feishu gateway/projector

只是 `server + bot` 已经合并进同一个 Go 进程。

## 3. 目录布局

当前实际目录：

```text
cmd/
  codex-remote/
  relayd/
  relay-wrapper/
  relay-install/

internal/
  adapter/
    codex/
    editor/
    feishu/
    relayws/
  app/
    adminauth/
    daemon/
    install/
    launcher/
    wrapper/
  config/
  core/
    agentproto/
    control/
    orchestrator/
    render/
    renderer/
    state/
    workspaceimport/
  debuglog/
  feishuapp/
  runtime/

testkit/
  harness/
  mockcodex/
  mockfeishu/
```

## 4. 分层职责

### 4.1 `internal/core/agentproto`

统一定义：

- wrapper <-> daemon wire envelope
- canonical command
- canonical event

### 4.2 `internal/core/control`

统一定义：

- Feishu/产品侧输入动作 `Action`
- server 输出给 projector 的 `UIEvent`
- snapshot / selection prompt / pending state / notice

当前约束：

- `Action` 的根字段主要保留共享路由元数据、消息输入载荷与少量产品动作主键
- request 卡片响应当前优先走 `ActionRequestResponse`
- upgrade / VS Code migrate owner-card follow-up 当前优先走 `ActionOwnerCardFlow`
- `UIEvent` 的 Feishu 读模型事件当前统一用 view-kind 命名：
  - `UIEventFeishuSelectionView`
  - `UIEventFeishuPageView`
  - `UIEventFeishuRequestView`
- `UIEventFeishuDirect*` 常量名仅保留兼容 alias，不再代表当前主 owner 语义

### 4.3 `internal/core/state`

领域状态：

- `InstanceRecord`
- `ThreadRecord`
- `SurfaceConsoleRecord`
- `BotCapabilitySettingsRecord`
- `QueueItemRecord`
- `StagedImageRecord`

这里不再承载 UI-owned 的瞬时卡片运行态，例如：

- active target picker
- active thread history
- active path picker

这些瞬时 gate / picker runtime 现在由 orchestrator service 自己持有，不再直接挂进 `core/state`。

### 4.4 `internal/core/orchestrator`

产品状态中心，负责：

- attach / detach
- thread routing
- queue 与 staged image
- local-priority / handoff
- model / reasoning override
- gateway 级 bot capability 字段事务与 surface 执行投影
- 将 agent event 映射成 UIEvent 和 command

另外，当前也负责 per-surface 的瞬时 UI runtime：

- target picker
- thread history
- path picker

这类状态只服务于当前进程内的交互门禁和回调继续处理，不作为领域状态根长期保存。

当前 `Service` 仍是唯一产品状态中心，但内部已经开始按 owner 收口成显式 runtime cluster，而不是继续把所有字段平铺在根 struct 上。当前稳定的第一批 cluster 包括：

- `turns`
  - 负责 queue / remote-turn runtime 热状态，包括 `pendingRemote`、`activeRemote`、`pendingSteers`、`compactTurns` 及其生命周期 helper
- `pickers`
  - 负责 target picker / path picker / thread history 及其 consumer/runtime token
- `catalog`
  - 负责 persisted catalog、snapshot query 与 catalog cache
- `progress`
  - 负责 compact notice、exec/tool progress、turn artifact 与相关派生投影

这几簇当前仍留在同包内，以减少过早拆包带来的导出污染；`Service` 自己则更接近组合根和跨簇编排点。

Feishu bot capability 的当前所有权边界是：

- gateway 级 `BotCapabilitySettingsRecord` 是 mode/backend、Codex provider、Claude profile、model/reasoning/access override 与 plan override 的唯一可变业务事实
- 合法 Feishu 私聊命令从最新 record 开始做字段级事务，并且只有这条配置入口可以在 record 缺失时按当前私聊投影首建；`/mode` 不改 provider/profile，provider/profile、prompt 与 plan 命令也不能整记录覆盖其它字段
- 同 gateway 已 materialize 的私聊和群聊 surface 都消费该 record；surface 同名字段只作为当前执行、restart、dispatch 和 route-derived snapshot 转换所需的投影
- `surface resume state` 只恢复 surface route/context 和能力执行 hint；bot store materialize 或 ingress 会重新投影 canonical record，resume entry 不能反向成为 bot 设置写源
- Claude `workspace+profile` snapshot 只保存和恢复其显式定义的 reasoning/access route-derived 值；已有 bot record 时通过 lifecycle 字段事务写回，不能覆盖 backend/provider/profile；record 缺失时只更新当前 surface 的 route-derived 执行状态，不能从群聊或生命周期路径首建整条 record
- record 同时保留 Codex provider 与 Claude profile 的已选值；active backend contract 只暴露当前一侧，切换 backend 不删除非活动选择
- canonical lookup 明确区分 not-applicable / absent / valid / invalid：只有 absent 可按既定语义使用本地 route-derived 状态；已存在但无法规范化或 map key 与 record gateway 不一致时进入 invalid gate，读、写与 dispatch 全部 fail closed，不降级为 surface 本地值。非法 identity、surface/gateway 不匹配和非 Feishu surface 则属于 not-applicable，继续保持明确的本地 owner 语义
- daemon 普通 action 收尾会同步 bot store；agent event 中唯一会改该 record 的 `request.resolved(plan_confirmation + accept)` 也在事件批末走同一 durable sync，不把高频 delta 事件带进持久化热点

### 4.4.1 `internal/core/workspaceimport`

共享 Git workspace 导入前置校验与错误契约，负责：

- 目录名推导与校验
- parent directory / destination path 预检查
- 导入失败错误码与错误载荷

它的目标是让 `orchestrator` 只依赖 core 级共享契约，而不是直接反向依赖 `internal/app/gitworkspace`。

### 4.5 `internal/core/renderer`

assistant 文本切分器，负责：

- 按 item 强边界收口
- fenced code block 识别
- 文件列表与正文切块
- 生成 append-only block

### 4.6 `internal/adapter/codex`

Codex app-server 适配层，负责：

- 观测 native `thread/turn/item`
- 观测本地 `turn/start` / `turn/steer`
- 维护最小翻译状态
- native <-> canonical 双向转换

这里不做 Feishu 产品决策。

### 4.7 `internal/adapter/relayws`

wrapper 和 daemon 之间的 websocket 传输层。

### 4.8 `internal/adapter/feishu`

Feishu 平台适配层，负责：

- 接收入站消息 / 菜单 / reaction / 图片
- 对普通 `message.receive`、`reaction.created`、`message.recalled` 做轻量归类与 gateway-local per-surface FIFO 入队
- 下载图片
- 把 `UIEvent` 投影成文本、卡片和 reaction 操作

当前内部 owner 进一步收口为三层：

- `MultiGatewayController`
  - Feishu adapter 的组合根，负责持有 gateway runtime、preview runtime 与 admin/runtime 编排
- gateway runtime
  - 只负责 inbound 归类、callback 解析、surface 路由和与 daemon/orchestrator 的协议边界
- projector / preview runtime
  - projector 只消费 `UIEvent` 做文本/卡片投影
  - preview runtime 只负责 preview 生命周期、授权与渲染辅助，不再通过宽接口横穿 gateway/projector
  - 当前 preview 具体实现已物理收口到 `internal/adapter/feishu/preview`，根包只保留稳定门面、兼容 alias 与 Feishu Drive bridge

因此 `LiveGateway` 不再承担 projector 的伪 owner 角色；daemon 主流程会直接持有 projector，而 preview 侧也通过显式 runtime 边界接入 controller。

Feishu surface identity 的跨层 contract 由零 adapter 依赖的 `internal/feishuidentity` 单独持有。gateway、preview、daemon、state 和 orchestrator 都复用同一套四段式 `feishu:<gatewayID>:<user|chat>:<scopeID>` build / parse / validate；未知 scope 或额外分段会直接拒绝，不再由各层维护 parser 或搜索字符串片段。

当前普通飞书入站的 ACK 边界已经前移：

- 轻量 command / menu / 非同步回包 card action：尽早 ACK
- 高风险 ordinary inbound（普通文本、`post`、图片、`merge_forward`，以及保持同 surface 顺序所需的 `reaction.created` / `message.recalled`）：
  - 先做最小 envelope 校验和 surface 路由
  - 成功进入 gateway-local FIFO lane 后立即 ACK
  - lane 内再继续做 quoted-input 补查、图片下载、转发树展开，以及后续 `control.Action -> orchestrator -> projector` 处理

普通消息只保留 `PlanInboundMessageEvent -> QueuedMessageWork.parseAction` 这一条 planning/parsing 主链。同步测试通过 `HandleInboundMessageEvent` 的 capture dispatcher 复用同一生产路径，不再维护另一份同步 parser，因此 command reply target、媒体下载、quote、message index 时机和 async failure 语义不会在测试实现与生产实现之间分叉。

### 4.9 `internal/adapter/editor`

编辑器接入层，负责：

- patch VS Code settings
- patch VS Code Remote 扩展 bundle 入口
- 探测当前平台可用的 VS Code extension bundle entrypoint 候选

### 4.10 `internal/app/daemon`

把这些模块组装成 daemon role：

- relay websocket server
- local Feishu MCP tool listener
- orchestrator
- renderer
- Feishu gateway
- 状态 API

`daemon` 当前更明确地作为组合根存在，而不是继续把所有运行态都堆进单个 `*App` 根对象。已经完成显式 runtime state 收口的区域包括：

- `toolRuntime`
- `managedHeadlessRuntime`
- `cronRuntime`
- `feishuRuntime`
- `surfaceResumeRuntime`
- `upgradeRuntime`

其中：

- `managedHeadlessRuntime` 负责 daemon 侧 managed headless 进程状态
- `cronRuntime` 负责 cron state、active runs、exit targets、scheduler scan 与 bitable/repo runtime 依赖
- `feishuRuntime` 负责 runtime-apply、permission gap refresh、onboarding 与 time-sensitive runtime
- `persistedStoreRuntimeState[T]` 负责 surface resume、bot capability settings、Feishu room state、Feishu bot identity、Claude workspace profile 五个 durable JSON store 的统一载入状态与可写门禁

Feishu room primary 的 durable SSOT 仍在 orchestrator room context 与 `FeishuRoomStateRecord`，但 Feishu SDK callback goroutine 不直接读取 orchestrator mutable root。daemon 在 room state materialize、primary sync 和 AppID identity cleanup 后构建 copy-on-write `chatID -> gatewayID` snapshot，adapter 的无 @ 群消息 gate 只读该 snapshot，再结合 permission cache 判定是否放行。

这些 runtime 仍留在 `internal/app/daemon` 同包内，但状态拥有者、receiver 和顶层调度边界已经分开；`App` 主要保留 lifecycle、依赖注入、跨 runtime 编排，以及少量必须集中托管的共享资源。

五个 durable JSON store 共享同一条 fail-closed load policy：文件不存在时得到 writable 空 store；成功载入时允许 materialize 和 sync；读取、JSON、schema version 或 dirty sanitation 保存失败时进入显式 read-only degraded。纯载入失败不会创建替代空 store、不会把虚假空状态 materialize 到 orchestrator，也不会清掉现有内存 recovery episode；已成功解码但 sanitation 保存失败时仍可读取和 materialize 规范化数据，但所有后续 sync 写入都会被统一门禁。修复状态文件后，下一次 runtime configure（通常是 daemon restart）会重新载入并退出 degraded。

Feishu 多 gateway 的 identity 边界进一步区分两层事实：`GatewayID` 是可复用配置槽位，`feishu-bot-identities.json` 中已提交的 AppID/generation 才是该槽位当前 bot identity。startup、admin apply 与 retry 都必须在 runtime upsert 前经过同一个 identity reconcile owner。仅 AppSecret 变化时保留业务状态；AppID 替换或配置删除时，旧 gateway generation 会先停止接收新 action 并排空已进入的 action，再在 identity store 写入 pending transition，之后以可重放事务撤销旧 surface/resume、bot capability、匹配的 room primary 与相关内存 runtime，room workspace 不随 bot identity 删除。pending transition 是失败恢复栅栏：任一 durable 清理或 identity commit 失败时，新 App runtime 不得启动；即使后续配置改回旧 AppID 或同槽位重建同 AppID，也必须先完成清理并提交新 generation，下一次 apply 从仍未提交的 identity transition 重放。清理范围包括 daemon 侧 `/bendtomywill` turn patch flow/transaction，避免旧 owner card 或异步事务在新 bot identity 下继续投递。

其中 daemon 自带的本地 Feishu tool listener 当前固定监听 `127.0.0.1:9502`，对外暴露的是 MCP-native streamable HTTP 协议面，而不再是仓库自定义 `manifest/call` 路由。它继续保留：

- loopback-only 访问边界
- bearer token 鉴权
- daemon 侧 tool 业务逻辑作为唯一 source of truth
- wrapper 发布 MCP URL 时附带调用者 instance id；daemon 在每次 tool call 时按该 instance 的 active/pending remote turn 解析当前 surface

### 4.11 `internal/app/wrapper`

把这些模块组装成 wrapper role：

- 启动真实 Codex 子进程
- 代理 stdio
- 连接 daemon
- 调用 Codex translator

### 4.12 `internal/app/install`

安装器 role，负责：

- 写统一配置 `config.json`
- 写 `install-state.json`
- patch editor settings 或 managed shim

安装器当前会复用 editor 侧的 VS Code bundle entrypoint 探测能力，而不是自己再维护一套平台候选解析逻辑。

## 5. 关键边界

### 5.1 Wrapper 不做产品语义

wrapper 只做：

- 协议翻译
- helper/internal 显式标注
- 正常 codex binary 的本地自愈选择

wrapper 不做：

- attach 语义
- queue
- 飞书渲染
- 文本切分

wrapper 当前不再直接依赖 `app/install`；如果需要探测 VS Code extension bundle 候选，会复用 `adapter/editor` 提供的共享探测能力。

### 5.2 Orchestrator 是唯一产品状态中心

所有这些都必须在 orchestrator 决策：

- 当前 surface 接管哪个 instance
- 当前消息发到哪个 thread
- 本地交互是否暂停远端 queue
- 哪些事件要渲染到 Feishu

### 5.3 Projector 不猜协议

Feishu projector 只消费 `UIEvent`，不直接理解 app-server 原生协议。

## 6. 关键运行流

### 6.1 远端 prompt

```text
Feishu inbound
  -> control.Action
  -> orchestrator enqueue / freeze route
  -> agentproto.Command(prompt.send)
  -> relayws
  -> wrapper role
  -> codex translator
  -> native Codex app-server
  -> canonical Event
  -> orchestrator / renderer
  -> UIEvent
  -> Feishu projector
```

### 6.3 Feishu MCP tool context

```text
wrapper 启动 child
  -> wrapper 读取 daemon tool service state
  -> wrapper 向 Codex/Claude 发布带 `codex_remote_instance_id` query 的 Feishu MCP URL
  -> daemon local MCP tool service 校验 loopback + bearer token，并把 caller instance 写入 MCP session context
  -> tool call 不接收公开 surface 参数
  -> daemon 按 caller instance 先查 active remote turn，再查 pending remote turn
  -> `feishu_send_im_image` / `feishu_send_im_video` / `feishu_send_im_file` 把本地产物按图片 / 视频 / 文件语义发送回该 turn 的发起 Feishu surface
  -> generic tool result 先归模型消费；只有模型显式调用 delivery tool 时，产物才会变成用户可见消息
```

### 6.2 本地 VS Code 交互

```text
VS Code / Codex UI
  -> native turn/start or turn/steer
  -> codex translator
  -> local.interaction.observed
  -> orchestrator pause_for_local / handoff_wait
```

### 6.3 状态查询

```text
HTTP /v1/status
  -> daemon snapshot
  -> current in-memory state dump
```

## 7. 当前仍然刻意不做的事

- 对外公开 control/render 协议
- 多 agent 统一插件系统
- block update/replace
- 远端 `turn.steer`
- 复杂的进程托管器抽象

这些可以以后再做，但不应影响现有三层边界。

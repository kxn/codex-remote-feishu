# 架构

> Type: `general`
> Updated: `2026-04-18`
> Summary: 对齐当前统一二进制入口、兼容 launcher 与实际目录结构，并补充 daemon 作为组合根的 runtime owner 收口、Feishu adapter 的 controller/gateway/projector/preview 边界、Feishu ordinary inbound 的 early ACK + gateway-local FIFO lane、orchestrator service-owned UI/runtime cluster，以及 editor 侧共享 VS Code bundle entrypoint 探测边界。

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

### 4.3 `internal/core/state`

领域状态：

- `InstanceRecord`
- `ThreadRecord`
- `SurfaceConsoleRecord`
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
- 将 agent event 映射成 UIEvent 和 command

另外，当前也负责 per-surface 的瞬时 UI runtime：

- target picker
- thread history
- path picker

这类状态只服务于当前进程内的交互门禁和回调继续处理，不作为领域状态根长期保存。

当前 `Service` 仍是唯一产品状态中心，但内部已经开始按 owner 收口成显式 runtime cluster，而不是继续把所有字段平铺在根 struct 上。当前稳定的第一批 cluster 包括：

- `pickers`
  - 负责 target picker / path picker / thread history 及其 consumer/runtime token
- `catalog`
  - 负责 persisted catalog、snapshot query 与 catalog cache
- `progress`
  - 负责 compact notice、exec/tool progress、turn artifact 与相关派生投影

这几簇当前仍留在同包内，以减少过早拆包带来的导出污染；`Service` 自己则更接近组合根和跨簇编排点。

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

因此 `LiveGateway` 不再承担 projector 的伪 owner 角色；daemon 主流程会直接持有 projector，而 preview 侧也通过显式 runtime 边界接入 controller。

当前普通飞书入站的 ACK 边界已经前移：

- 轻量 command / menu / 非同步回包 card action：尽早 ACK
- 高风险 ordinary inbound（普通文本、`post`、图片、`merge_forward`，以及保持同 surface 顺序所需的 `reaction.created` / `message.recalled`）：
  - 先做最小 envelope 校验和 surface 路由
  - 成功进入 gateway-local FIFO lane 后立即 ACK
  - lane 内再继续做 quoted-input 补查、图片下载、转发树展开，以及后续 `control.Action -> orchestrator -> projector` 处理

### 4.9 `internal/adapter/editor`

编辑器接入层，负责：

- patch VS Code settings
- patch VS Code Remote 扩展 bundle 入口
- 探测当前平台可用的 VS Code extension bundle entrypoint 候选

### 4.10 `internal/app/daemon`

把这些模块组装成 daemon role：

- relay websocket server
- local tool service listener
- orchestrator
- renderer
- Feishu gateway
- 状态 API

`daemon` 当前更明确地作为组合根存在，而不是继续把所有运行态都堆进单个 `*App` 根对象。已经完成第一轮显式 runtime state 收口的区域包括：

- `toolRuntime`
- `surfaceResumeRuntime`
- `upgradeRuntime`

这些 runtime 仍留在 `internal/app/daemon` 同包内，但状态拥有者、receiver 和顶层调度边界已经分开；`App` 主要保留 lifecycle、依赖注入、跨 runtime 编排，以及少量必须集中托管的共享资源。

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
normal 模式下的 workspace 排他接管
  -> daemon 在 workspace/.codex-remote/surface-context.json 写入当前 surface
  -> 本地 Feishu MCP tool description 要求 Codex 先读取该文件
  -> tool call 显式带回 surface_session_id
  -> daemon local tool service 校验并解析 surface context
  -> `feishu_send_im_file` 可继续用显式 `surface_session_id` 把本地文件发送回当前 Feishu surface
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

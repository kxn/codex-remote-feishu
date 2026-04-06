# 架构

> Type: `general`
> Updated: `2026-04-06`
> Summary: 迁移到 `docs/general` 并统一文档元信息头，内容未做实质改动。

## 1. 当前状态

当前仓库只维护 Go 版本实现。

旧的 Node.js / Rust 版本已经不再随仓库发布，也不是当前文档和测试的讨论对象。

## 2. 进程结构

当前固定为三个二进制：

1. `relayd`
   - 统一承载状态编排、Feishu 接入、wrapper websocket hub、状态 API
2. `relay-wrapper`
   - 作为 Codex 可执行包装器运行
   - 代理 VS Code / Codex 扩展和真实 `codex.real`
3. `relay-install`
   - 写配置
   - 做编辑器集成

逻辑上仍保留三层边界：

- wrapper
- server/orchestrator
- Feishu gateway/projector

只是 `server + bot` 已经合并进同一个 Go 进程。

## 3. 目录布局

当前实际目录：

```text
cmd/
  relayd/
  relay-wrapper/
  relay-install/

internal/
  adapter/
    codex/
    editor/
    feishu/
    process/
    relayws/
  app/
    daemon/
    install/
    wrapper/
  config/
  core/
    agentproto/
    control/
    orchestrator/
    render/
    renderer/
    state/
  logging/

testkit/
  harness/
  mockcodex/
  mockfeishu/
```

## 4. 分层职责

### 4.1 `internal/core/agentproto`

统一定义：

- wrapper <-> relayd wire envelope
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

### 4.4 `internal/core/orchestrator`

产品状态中心，负责：

- attach / detach
- thread routing
- queue 与 staged image
- local-priority / handoff
- model / reasoning override
- 将 agent event 映射成 UIEvent 和 command

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

wrapper 和 relayd 之间的 websocket 传输层。

### 4.8 `internal/adapter/feishu`

Feishu 平台适配层，负责：

- 接收入站消息 / 菜单 / reaction / 图片
- 下载图片
- 把 `UIEvent` 投影成文本、卡片和 reaction 操作

### 4.9 `internal/adapter/editor`

编辑器接入层，负责：

- patch VS Code settings
- patch VS Code Remote 扩展 bundle 入口

### 4.10 `internal/app/daemon`

把这些模块组装成 `relayd`：

- relay websocket server
- orchestrator
- renderer
- Feishu gateway
- 状态 API

### 4.11 `internal/app/wrapper`

把这些模块组装成 `relay-wrapper`：

- 启动真实 Codex 子进程
- 代理 stdio
- 连接 relayd
- 调用 Codex translator

### 4.12 `internal/app/install`

安装器服务层，负责：

- 写统一配置 `config.env`
- 写 `install-state.json`
- patch editor settings 或 managed shim

## 5. 关键边界

### 5.1 Wrapper 不做产品语义

wrapper 只做：

- 协议翻译
- helper/internal 显式标注

wrapper 不做：

- attach 语义
- queue
- 飞书渲染
- 文本切分

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
  -> relay-wrapper
  -> codex translator
  -> native Codex app-server
  -> canonical Event
  -> orchestrator / renderer
  -> UIEvent
  -> Feishu projector
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

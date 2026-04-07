# Relay 回压治理设计

> Type: `implemented`
> Updated: `2026-04-07`
> Summary: 完成 wrapper 削峰、transport 分级、daemon ingress 解耦、transport degraded 与错误抑制，实现 relay 回压治理第一轮闭环。

## 1. 文档定位

这份文档描述的是 **`relay-wrapper -> relayd -> Feishu` 链路在高频事件场景下的回压治理方案**。

当前代码已经按本文方案完成第一轮实现，因此本文迁移到 `docs/implemented/`，用于记录当前已落地行为与设计边界。

相关现状文档：

1. [relay-protocol-spec.md](../general/relay-protocol-spec.md)
2. [relay-error-reporting-protocol.md](../general/relay-error-reporting-protocol.md)

## 1.1 当前实施计划

以下计划用于实际分阶段落地，后续每一阶段开始前都要按当时代码状态重新复评；如果最佳顺序变化，需要先回写本节再继续编码。

### 阶段 0：流程固化与实现 issue 收敛

状态：已完成

目标：

- 把分阶段推进规则固化进仓库规则
- 基于本文创建可实施 issue，并把当前阶段切分写清楚

交付：

- `AGENTS.md` 新增分阶段推进规则
- GitHub issue 记录本文链接、范围、非目标和阶段切分

### 阶段 1：wrapper 侧削峰

状态：已完成

目标：

- 引入 `item.delta` 合并器，直接降低高频 `event_batch` 数量

交付：

- wrapper 事件发送链路支持相邻 `item.delta` 合并
- 覆盖 flush 边界与文本一致性的自动化测试

### 阶段 2：wrapper transport 分级与连接代次隔离

状态：已完成

目标：

- 防止 data flood 把 control plane 一起拖死
- 防止 reconnect 后旧 backlog / 旧 ack 污染新连接

交付：

- control/data 分级出站
- epoch-aware backlog 清理与相关测试

### 阶段 3：daemon ingress pump 解耦

状态：已完成

目标：

- 解除 websocket read path 和同步慢处理路径的直接耦合

交付：

- relay server / daemon 引入 ingress queue + pump
- 覆盖慢消费、单实例 FIFO 与 callback 异步化路径测试

### 阶段 4：退化语义、错误抑制与可观测性收口

状态：已完成

目标：

- overload 不再直接退化成普通 detach
- 避免同类链路错误在 Feishu 侧形成错误风暴
- 把必要指标落进日志，便于事故回看

交付：

- transport degraded 语义落地
- `problemReporter` 去重/限流
- 合并器 / ingress / epoch 的本地日志或统计补齐

### 当前默认范围说明

1. 本轮默认覆盖阶段 1 到阶段 4。
2. `gateway.Apply` 慢路径是否继续拆分，不作为当前默认完成条件；阶段 4 结束后再根据实测决定是否另起 issue。
3. `2026-04-07` 首次阶段复评结论：阶段 1 已按原计划完成；阶段 2 顺序保持不变，继续先做 wrapper transport 分级与连接代次隔离，再进入 daemon ingress pump。
4. `2026-04-07` 阶段 2 开始前复评结论：本阶段仍可先收敛在 `internal/adapter/relayws/client.go` 内完成，不需要提前改 `server.go` 或 daemon ingress pump；当前最佳切面仍是先做 client 内部 control/data 双队列和连接代次隔离。
5. `2026-04-07` 阶段 2 结束后复评结论：当前阶段切分依然成立；下一阶段继续按原计划进入 daemon ingress pump，把 websocket read path 和 daemon 同步慢处理解耦。
6. `2026-04-07` 阶段 3 开始前复评结论：当前最佳切面仍然是先在 `internal/app/daemon` 内引入本地 ingress pump，把 relay websocket callback 收窄成轻量入队，不提前改 overload 语义。`stale epoch work item` 与 inbound overload 处置依赖连接代次和 transport degraded 语义，应继续放在阶段 4 与退化收口一起落地。
7. `2026-04-07` 阶段 3 结束后复评结论：当前切分仍然成立。已通过 daemon 本地 ingress pump 完成 websocket callback 与同步慢处理解耦，并保持现有 `onHello/onEvents/onCommandAck/onDisconnect` 业务逻辑不变；下一阶段继续聚焦 transport degraded、同类错误抑制和必要可观测性，不在阶段 4 之外额外扩散 `gateway.Apply` 慢路径重构。
8. `2026-04-07` 阶段 4 开始前复评结论：当前最佳切面是把“连接身份、inbound overload、transport degraded、problemReporter 去重”一次性收在同一轮完成。具体做法是：`relayws.Server` 仅新增本地 `connectionID` 元数据与定向断链能力；daemon ingress item 带 `connectionID` 并引入有界 per-instance queue；orchestrator 新增非 detach 的 `transport degraded` 收口；wrapper `problemReporter` 做短窗去重和内存上限。`gateway.Apply` 慢路径拆分继续明确留在本轮之外。
9. `2026-04-07` 阶段 4 结束后复评结论：本轮默认范围已全部落地。当前实现已经具备 wrapper `item.delta` 合并、control/data 出站分级、epoch-aware backlog 隔离、daemon ingress pump、有界 per-instance inbound queue、`transport degraded` 收口、stale ingress 丢弃，以及 wrapper 同类链路错误短窗去重。`gateway.Apply` / markdown preview 慢路径拆分仍保留为后续性能类工作，不属于本文完成条件。

## 2. 事故摘要

### 2.1 用户可见症状

2026-04-07 中午，Feishu 端开始持续收到红色错误卡片：

- `链路错误 · wrapper.forward_server_events`
- `错误码：relay_send_server_events_failed`
- `调试信息：relay client outbox full`

之后只能通过手工杀掉全部 `codex remote` 进程止血。

### 2.2 已确认的运行时证据

日志中已经确认以下事实：

1. 问题实例为 `inst-headless-1775479555593649765-1`。
2. 直接触发命令是：

```text
/bin/bash -lc "unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy; rg -n -S 'tool_call_mcp_elicitation|mcp|approval|confirm' /home/dl/.local/share/codex-remote/logs/codex-remote-wrapper-*-raw.ndjson"
```

3. 该命令命中了大量 wrapper 原始日志，输出包含整行 JSON、历史 diff、配置块，导致 `call_5FEb2aEt6Ho7vtqPTzoDcQdo` 产生大量 `item/commandExecution/outputDelta`。
4. `2026-04-07 12:32:51` 左右，wrapper 开始连续记录：

```text
relay send server events failed: relay client outbox full
```

5. 之后 wrapper 又把同类失败通过 `problemReporter` 转成大量 `system.error`，relayd 将其继续投影到 Feishu，于是形成红卡风暴。

### 2.3 关键代码路径

- wrapper 读取 Codex stdout 并直接 `client.SendEvents(result.Events)`：
  - [internal/app/wrapper/app.go](/data/dl/fschannel/internal/app/wrapper/app.go#L520)
- relayws client 的 outbox 固定为 `512`，满了直接返回 `relay client outbox full`：
  - [internal/adapter/relayws/client.go](/data/dl/fschannel/internal/adapter/relayws/client.go#L48)
  - [internal/adapter/relayws/client.go](/data/dl/fschannel/internal/adapter/relayws/client.go#L141)
- relayws server 在 websocket 读循环中同步调用 `OnEvents`：
  - [internal/adapter/relayws/server.go](/data/dl/fschannel/internal/adapter/relayws/server.go#L202)
- daemon 在 `OnEvents` 中同步执行业务处理与 UI 投递：
  - [internal/app/daemon/app.go](/data/dl/fschannel/internal/app/daemon/app.go#L317)
  - [internal/app/daemon/app.go](/data/dl/fschannel/internal/app/daemon/app.go#L385)
- orchestrator 对 `item.delta` 的实际语义是把同一 item 文本追加到 buffer：
  - [internal/core/orchestrator/service.go](/data/dl/fschannel/internal/core/orchestrator/service.go#L354)
  - [internal/core/orchestrator/service_queue.go](/data/dl/fschannel/internal/core/orchestrator/service_queue.go#L302)
- wrapper 的 `problemReporter` 会把同类错误继续转成新的 `system.error`：
  - [internal/app/wrapper/app.go](/data/dl/fschannel/internal/app/wrapper/app.go#L59)

## 3. 当前链路与瓶颈

当前主链路大致为：

```text
Codex stdout
  -> wrapper translator.ObserveServer
  -> relayws client outbox
  -> websocket write
  -> relayws server read loop
  -> daemon onEvents
  -> orchestrator/service
  -> projector / markdown preview / gateway.Apply
  -> Feishu
```

这条链路目前有五个结构性问题：

1. **wrapper 侧逐帧直发**
   - 每次 `ObserveServer` 产出事件后都会立即调用 `SendEvents`
   - 高频 `item.delta` 会直接转成高频 `event_batch`

2. **wrapper outbox 是固定小缓冲且失败即报错**
   - outbox 满时不会等待、不会合并、不会限流
   - 结果是突发流量直接转成 `relay_send_server_events_failed`

3. **relayd websocket 读路径和慢处理路径没有解耦**
   - `server.go` 中 `OnEvents` 在读循环里同步执行
   - 只要 daemon 处理慢，socket 读就会跟着变慢

4. **错误上报链路会放大故障**
   - 同一个 `outbox full` 会重复生成 `system.error`
   - 这些错误本身又要占用同一条拥塞链路

5. **控制面和数据面当前没有隔离**
   - `CommandAck` 和 `EventBatch` 共用 wrapper outbox
   - 数据洪峰时，reject/accept ack 也可能被饿死

## 4. 根因判断

这次事故不是单一 bug，而是多个设计弱点叠加：

1. **触发源是一次异常大的命令输出**
   - 搜索 wrapper raw log 的命令把大量历史 JSON 和 diff 全打出来
2. **Codex 输出被拆成大量小 delta**
   - wrapper 没有进行任何合并
3. **relayd 消费速度被同步业务路径拖慢**
   - 读 socket 和处理事件是同一条同步链路
4. **系统在拥塞时没有“降载但保语义”的策略**
   - 既不合并高频 delta
   - 也不去重同类错误
   - 更没有明确的 overload 退化语义

## 5. 设计目标

### 5.1 目标

1. 在高频 `item.delta` 场景下，不再因为瞬时突发而把 wrapper outbox 打满。
2. 不静默丢失任何会改变状态机语义的 canonical event。
3. 保持 **单实例内事件顺序** 不被打乱。
4. 避免同类链路故障在 Feishu 侧形成红卡风暴。
5. overload 退化不能把 surface 直接打回“脱附且无法恢复”的更坏状态。
6. reconnect 后不能让旧连接 backlog 或旧 ack 污染新连接。
7. 修复后要能通过自动化测试稳定复现和验证。

### 5.2 非目标

1. 本次不追求“真正的 token-by-token 流式投影”。
2. 本次不重写整个 relay 协议。
3. 本次不把所有 daemon 内部处理完全并行化。
4. 本次不以“单纯调大缓冲区”作为正式方案。

## 6. 方案选型

### 6.1 仅增大 wrapper outbox

优点：

- 改动最小

问题：

- 只能延后爆炸时间，不能改变拥塞结构
- 高峰再大一点还是会满
- 错误风暴和慢消费问题完全还在

结论：

- 不接受作为正式修复

### 6.2 仅在 daemon 侧把 websocket 读路径异步化

优点：

- 能避免 socket read 被同步业务直接拖死

问题：

- wrapper 仍然会发送海量小 `event_batch`
- 内部队列只是把压力从 socket 挪到内存
- 同类错误风暴依旧存在

结论：

- 必须做，但单做不够

### 6.3 仅在 wrapper 侧合并 `item.delta`

优点：

- 对本次事故命中最直接
- 能显著降低 `event_batch` 数量

问题：

- daemon 读路径仍然耦合同步慢处理
- 未来如果出现别的高频事件源，仍可能复发

结论：

- 必须做，但单做不够

### 6.4 推荐方案

推荐组合方案：

1. **wrapper 侧做 `item.delta` 合并**
2. **wrapper 侧把控制面与数据面 transport 分级**
3. **daemon 侧把 websocket ingress 与业务处理解耦**
4. **为 reconnect / overload 引入明确的连接代次与退化语义**
5. **wrapper 侧对同类链路错误做去重/限流**

这是当前最小且闭环的可靠性修复集合。

## 7. 推荐设计

### 7.1 wrapper: 引入事件合并器

在 `translator.ObserveServer -> client.SendEvents` 之间引入一个轻量事件合并器。

#### 7.1.1 合并范围

仅允许合并以下事件：

- `item.delta`

且要求以下字段全部相同：

- `kind`
- `threadId`
- `turnId`
- `itemId`
- `itemKind`
- `trafficClass`
- `initiator`

合并方式：

- 保持事件顺序不变
- 只把多个相邻 `delta` 的 `delta` 字符串拼接成一个更大的 `delta`

#### 7.1.2 必须立即 flush 的边界

出现以下任一情况时，必须先 flush 当前待合并 `delta`：

1. 收到不同 `itemId` 的 `item.delta`
2. 收到任何非 `item.delta` 事件
3. 达到单条累计字节上限
4. 达到短时间窗口上限
5. wrapper 准备退出或连接中断

#### 7.1.3 为什么不会破坏语义

当前 orchestrator 对 `item.delta` 的使用本质上是：

- 按 `itemId` 追加文本缓存
- 等 item 边界或 turn 边界再决定是否渲染

也就是说，对同一 `itemId` 的相邻 `delta` 做字符串拼接，不会改变最终文本与状态机语义，只会降低事件数量。

### 7.2 daemon: 引入 inbound ingress pump

`relayws.Server` 的 websocket 读循环不再直接执行业务处理，而是只把入站消息交给 daemon 的 ingress pump。

改为：

```text
websocket read loop
  -> daemon ingress queue
  -> daemon ingress pump
  -> service / handleUIEvents
```

#### 7.2.1 基本要求

1. websocket 读循环只做 decode、轻量校验、入队。
2. `EventBatch` 和 `CommandAck` 都进入 ingress queue，而不是在读循环里直接执行业务回调。
3. 业务处理由独立 ingress pump 串行消费。
4. 单实例内消息必须保持 FIFO。
5. ingress pump 才允许进入 `a.mu`、orchestrator 和 UI 投递。
6. `Hello` 只有在被收窄成“最小连接登记”后才允许保留在同步路径；否则也要进入 ingress control work item。

#### 7.2.2 推荐队列模型

推荐使用 **按实例分桶、单实例 FIFO、中心调度串行消费** 的模型。队列元素是统一的 ingress work item，而不是只装 `EventBatch`：

1. 每个 instance 有独立 inbound queue。
2. websocket 连接只向自己的 instance queue 入队。
3. queue 元素至少覆盖：
   - `event_batch`
   - `command_ack`
4. daemon 的 ingress pump 以轮询或公平策略从各实例 queue 取 work item。

这样做的原因：

1. 同一实例内保序简单。
2. 单个 noisy instance 不会完全堵死其它实例的 **websocket ingress**。
3. 后续若要做 per-instance 监控或熔断，也更清晰。
4. `CommandAck` 也走同一队列后，read loop 不会再因为等待 `a.mu` 或 UI 投递而被卡住。

这里要明确一个边界：

- **按实例分桶只能解决 socket read 背压，不等于已经解决跨实例的全局慢路径 HOL blocking。**
- 只要 event pump 后面仍然串着全局 `a.mu`、markdown preview、`gateway.Apply`，单个实例的慢 UI delivery 仍然可能拖住其它实例的实际处理。
- 因此，第一版文档不再把“noisy instance 不会堵死其它实例”写成完整结论，只把它限定为 ingress 层收益。

#### 7.2.3 为什么不只异步化 EventBatch

如果只把 `EventBatch` 异步化，而把 `CommandAck` 继续留在 websocket 读循环同步处理，会留下一个结构性缺口：

1. `onCommandAck` 仍然要拿 `a.mu`。
2. 当前 `onEvents`/`onCommandAck` 路径里的 UI 处理可能继续触发 `gateway.Apply` 等慢操作。
3. 一旦 ingress pump 正在慢路径里持锁，读循环处理 `CommandAck` 仍然可能被卡住。

因此，推荐把所有真正进入 orchestrator 状态机的入站消息统一收敛到 ingress pump，只把建链 `Hello` 留在同步路径。

#### 7.2.4 `Hello` 的同步路径必须收窄

当前代码里的 `onHello` 并不轻，它会：

1. 更新 instance 状态
2. 执行 `ApplyInstanceConnected`
3. 可能触发队列恢复
4. 立刻再发 `threads_refresh`

因此如果第一版仍保留 `Hello` 的同步处理，必须把同步路径收窄成：

1. 仅做连接登记与最小必要的 `instance online` 标记
2. 把 `ApplyInstanceConnected`、队列恢复、`threads_refresh` 发送收敛成 ingress control work item

否则 reconnect 场景仍可能在 read loop 内执行较重逻辑。

### 7.3 overload 语义

内部队列不允许默默丢失 canonical event，也不允许静默跳过 `CommandAck` 这类控制面消息。

但是这里不能直接复用现有 `ApplyInstanceDisconnected` 语义，因为它会：

1. 清掉 `AttachedInstanceID`
2. 清掉 `SelectedThreadID`
3. 清掉 active remote binding
4. 让 reconnect 后不会自动恢复 detached surface

这对“过载保护”来说过重，会把用户打进更糟的状态。

当 inbound queue 满时，推荐语义是：

1. 记录一次明确的 overload 日志，并为该实例提升连接代次。
2. 关闭当前 websocket 连接，阻止旧代次继续灌入。
3. daemon 对该实例执行新的 **transport degraded** 语义，而不是 `ApplyInstanceDisconnected`：
   - 保留 surface attachment
   - 保留 selected thread
   - 保留 queued item
   - fail 当前 dispatching/running 的 active item
   - 清掉该实例的 pending/active remote binding
4. attached surface 收到单条结构化 notice，明确说明是“链路过载，当前执行中断，但会保留接管关系等待实例恢复”。
5. wrapper 用新连接代次重连；只有新代次的 ingress 才允许继续进入 daemon。
6. 同一实例在短时间内连续 overload 时，notice 和日志都要限流，避免形成第二层风暴。

原因：

1. 静默丢事件会直接污染 thread/turn/item/request 状态机。
2. 直接 detach surface 会让用户丢失上下文和恢复路径，比当前事故更糟。
3. transport degraded 语义更接近真实问题本质：坏的是当前链路，不是用户的 attach/use 关系。

#### 7.3.2 `transport degraded` 的最小清理 contract

`transport degraded` 不能只做“fail active item + 保留 attachment”，否则很容易留下旧 turn 残留，把 surface 卡在半死状态。

最小清理 contract 至少包括：

1. `inst.Online = false`
2. 清掉 `inst.ActiveTurnID`
3. 清掉该实例的 `pendingRemote` / `activeRemote`
4. 清掉与被中断 turn 相关的 request prompt、item buffer、pending turn text
5. 把 surface 的 `ActiveQueueItemID`、`ActiveTurnOrigin`、临时 handoff / pause 状态收束到可继续派发的稳定态
6. 保留：
   - `AttachedInstanceID`
   - `SelectedThreadID`
   - 未执行的 queued item

如果实现时无法安全精确定位“被中断 turn”的 thread/turn 关联，那么宁可做 **该实例范围内的瞬态状态清理**，也不能留下 `ActiveTurnID`、request、item buffer 之类的残留。

#### 7.3.3 连接代次（epoch）语义

为了避免 reconnect 后的旧 backlog/旧 ack 污染新连接，必须引入显式 connection epoch。

最低要求：

1. daemon 为每个 instance 维护当前接受中的 connection epoch。
2. wrapper transport 队列中的每条待发送 envelope 都带上生成时的 epoch。
3. 连接断开或 overload 提升 epoch 后，旧 epoch 的积压 envelope 必须被丢弃，不能在新连接上继续发送。
4. daemon ingress pump 只接受当前 epoch 的 work item；旧 epoch 的晚到消息直接丢弃并记录一次调试日志。

这意味着 overload 后不是“继续消费历史积压直到自然追平”，而是明确切断旧链路，让新链路从新的状态继续工作。

#### 7.3.4 overload 主动断链后的 `OnDisconnect` 处置

epoch 语义如果只停留在 wrapper outbox 和 daemon ingress queue 里还不够，`relayws server -> daemon` 这一跳也必须带上连接身份。

最小要求：

1. server 为每条 websocket 连接生成连接标识，并把它附着到：
   - `Hello`
   - `EventBatch`
   - `CommandAck`
   - `Disconnect`
2. daemon 在 overload 主动关链时，先把该连接标记为“已按 transport degraded 收口”。
3. 之后到来的同连接 `OnDisconnect` 只能做连接清理，**不能再回落调用普通 `ApplyInstanceDisconnected` detach surface**。
4. 只有“当前 accepted connection 的非受控断开”才走普通 disconnect 语义。

否则实现时很容易出现：

1. 先执行 transport degraded
2. 随后旧连接 `OnDisconnect` 再次进入普通 disconnect
3. 最终又把 surface 意外 detach 掉

### 7.4 wrapper: 控制面与数据面 transport 分级

仅仅合并 `item.delta` 还不够，因为 `CommandAck` 与 `EventBatch` 当前共用一个 outbox。

推荐至少满足以下之一：

1. 独立 `control outbox` 与 `data outbox`，writer 始终优先发送 control
2. 单 outbox 但保留不可被 data 占满的 control 预留容量

第一版更推荐前者，因为语义更清楚，测试也更直观。

#### 7.4.1 哪些消息属于 control plane

本轮至少把以下消息视为 control plane：

1. `CommandAck`
2. reconnect / overload 恢复所需的最小控制信号

`system.error` 不应直接升到高优先级，否则又会把诊断流量变成新的风暴源。

#### 7.4.2 设计目标

1. 数据面洪峰时，reject/accept ack 仍然有通道可发。
2. overload / reconnect 提升 epoch 时，可以独立清掉旧 data backlog，而不把 control plane 一起拖死。
3. 当前 turn 即便因为过载失败，surface 也不会因为收不到 reject/cleanup ack 而卡在模糊态。

### 7.5 wrapper: 对同类错误做去重/限流

`problemReporter` 不能在同一种链路故障下无限上报。

推荐策略：

1. 对 `(code, layer, stage, operation, details)` 做归一化 key。
2. 相同 key 在短窗口内只允许首条上报到 relay。
3. 窗口内后续重复不再继续堆积到 `pending`，仅写本地日志并增加计数。
4. 窗口结束后若仍持续，可重新上报一条带计数摘要的错误。
5. `problemReporter` 自身也要有内存上限，避免 relay 不可用时错误缓存再次无界增长。

### 7.6 本轮修复先不强行改的部分

以下问题本轮建议只纳入评估，不作为第一优先级硬改：

1. `gateway.Apply` 完全异步化
2. markdown preview 重写链路完全移出 `handleUIEvents`
3. `service` 级别去锁拆分

原因：

1. 本次主要拥塞流量来自 `item.delta`，并不是 Feishu 发送本身。
2. 先把 ingress 解耦和 delta 合并做好，已能显著降低复发概率。
3. 过早同时大改 ingress、service 锁模型、UI delivery，风险会明显上升。

但实现阶段必须重新测量：

- 第一版不会再宣称“已经解决跨实例公平性”。
- 如果 event pump 仍然被 `gateway.Apply` 明显拖慢，则下一阶段要继续把 UI delivery 从 event pump 中拆出去。

## 8. 对整体系统的潜在影响与对策

### 8.1 事件顺序风险

风险：

- 异步入队后，如果实现不严，可能打乱 `turn.started -> item.delta -> item.completed -> turn.completed` 顺序。

对策：

1. 单实例内强制 FIFO。
2. `item.delta` 只允许在同一 item 上做相邻合并，不允许跨边界重排。
3. 所有 request/turn lifecycle 事件禁止合并。

### 8.2 内存占用风险

风险：

- delta 合并器和 inbound queue 都会引入额外缓存。

对策：

1. 为单条合并 buffer 设置硬上限。
2. 为单实例 inbound queue 设置硬上限。
3. 达到上限后执行明确 overload 语义，而不是无限扩容。

### 8.3 错误可见性风险

风险：

- 做去重后，可能把真实不同错误也一起压掉。

对策：

1. 去重 key 必须包含 `details`。
2. 去重只针对短时间完全相同的错误。
3. 本地日志仍保留全部原始记录或计数摘要。

### 8.4 Feishu 投影粒度变化

风险：

- delta 合并后，若未来引入真流式展示，前端体感会改变。

对策：

1. 当前系统本来就不是 token 级流式投影。
2. 文档明确本轮非目标不是维持 token 粒度。
3. 如果将来真要做流式投影，应引入专门的 stream protocol，而不是依赖当前逐 delta 直发。

### 8.5 reconnect 后的状态恢复

风险：

- 如果直接复用 `ApplyInstanceDisconnected`，overload 会把 surface 打回脱附态，reconnect 后也不会自动恢复。

对策：

1. overload 不复用普通 disconnect 语义，而是单独定义 transport degraded 语义。
2. transport degraded 还必须清掉 `ActiveTurnID`、request、item buffer、pending turn text 等 turn 残留。
3. 只 fail 当前 active item，保留 attachment、selected thread 与 queued item。
4. reconnect 后如实例仍是同一个 attach 目标，可从保留下来的 surface 上继续工作。

### 8.6 控制面时序风险

风险：

- 如果 `CommandAck` 与 `EventBatch` 不通过同一条 FIFO ingress queue，可能出现 ack 和事件先后关系不稳定，或者 read loop 仍被同步 ack 处理拖慢。
- 如果 wrapper transport 仍让 `CommandAck` 与海量 `EventBatch` 争同一个 outbox，ack 可能根本发不出去。

对策：

1. `CommandAck` 与 `EventBatch` 对同一 instance 使用同一 FIFO queue。
2. `Hello` 之外的入站控制消息默认也按这个规则评估。
3. wrapper transport 对 `CommandAck` 提供独立 control plane 或预留容量。
4. 针对“先 ack、后 turn.started”“先 event、后 reject ack”等场景补顺序测试，明确当前协议允许的实际顺序。

### 8.7 重连抖动风险

风险：

- 如果某实例在短时间内连续触发 overload，daemon 受控断连和 wrapper 自动重连可能形成抖动。
- 如果旧 data backlog 在新连接上继续回放，还会形成重复 overload 循环。

对策：

1. 先用 wrapper `item.delta` 合并和错误去重把高频源头压下去，避免常态进入 overload。
2. overload / reconnect 提升 connection epoch，并清掉旧 epoch backlog。
3. daemon 对 overload notice、日志和断连统计做限流。
4. 验证阶段增加“连续多次大输出”场景，确认不会进入高频断连重连循环。

### 8.8 可观测性不足风险

风险：

- 如果只改行为不补指标，下次再出现拥塞时仍然难以判断堵在哪一层。

对策：

1. 为 wrapper 合并器增加至少以下调试指标或日志摘要：
   - 合并前事件数
   - 合并后事件数
   - flush 次数
   - 因大小上限触发的 flush 次数
   - 当前 data/control queue 深度
   - 因 epoch 提升而被丢弃的 envelope 数
2. 为 daemon ingress queue 增加至少以下指标或日志摘要：
   - 每实例当前 queue 深度
   - 峰值 queue 深度
   - overload 次数
   - 单实例连续 overload 次数
   - 丢弃的 stale epoch work item 数
3. 这些信息至少要进入本地日志，便于下次事故回看。

### 8.9 跨实例公平性结论被误读的风险

风险：

- 如果文档把“按实例分桶 ingress queue”直接表述成“多实例互不影响”，实现后很容易让人误以为跨实例慢路径已经解决，但当前 `a.mu` + `gateway.Apply` 仍然会串行放大阻塞。

对策：

1. 文档显式把第一版收益限定为“解除 websocket read 背压”和“把问题收束到 ingress pump 后面”。
2. 多实例真正的处理隔离，留到后续是否拆 `gateway.Apply` / preview 慢路径时再单独评估。

## 9. 建议实施顺序

如果后续开始编码，建议顺序如下：

1. 先实现 wrapper `item.delta` 合并器。
2. 再实现 wrapper control/data transport 分级，以及 epoch-aware backlog 清理。
3. 再实现 daemon inbound ingress pump，并把 `EventBatch` / `CommandAck` / reconnect 恢复工作项收敛进去。
4. 再实现 service 侧 transport degraded 语义，替代普通 disconnect detach。
5. 再实现 `problemReporter` 的去重/限流与相关可观测性。
6. 最后根据实测决定是否继续拆 `gateway.Apply` 慢路径。

这个顺序的原因是：

1. 第一步直接消掉最大流量来源。
2. 第二步先保证 control plane 不会被 data flood 一起拖死，并消除 reconnect 旧 backlog 重放。
3. 第三步把 transport read path 与慢处理解耦。
4. 第四步保证 overload 不会直接把用户打回脱附死状态。
5. 第五步阻止错误风暴自我放大，并补足回看能力。
6. 第六步属于性能/结构性增强，不必和前面捆死。

## 10. 验证计划

### 10.1 自动化测试

至少补以下测试：

1. wrapper 单元测试
   - 相邻 `item.delta` 会正确合并
   - 跨 item / 跨事件边界会正确 flush
   - flush 后文本拼接结果与原始事件序列一致

2. wrapper 错误测试
   - 连续相同 `relay_send_server_events_failed` 不会无限上报成 `system.error`
   - rate-limit 窗口内不会继续把重复错误堆进 `pending`

3. wrapper transport 测试
   - `CommandAck` 在 data flood 下仍可成功入队
   - 连接 epoch 提升后，旧 epoch 的 data backlog 不会在新连接上继续发送
   - 旧 epoch 的 ack / event 不会污染新连接

4. relay/daemon 集成测试
   - 慢消费场景下 websocket ingress 不会被同步逻辑直接拖死
   - 单实例 FIFO 仍成立
   - `CommandAck` 与 `EventBatch` 在同一 instance 内仍保持预期顺序
   - `Hello` 同步路径不会继续执行重 UI/重调度逻辑

5. overload 测试
   - inbound queue 满时不会静默丢事件
   - 会触发 transport degraded，而不是 surface detach
   - active item 会失败，queued item/attachment/selected thread 会保留
   - `ActiveTurnID`、request、item buffer、pending turn text 等残留会被清掉
   - 连续 overload 时不会刷 notice 风暴

6. reconnect / stale frame 测试
   - overload 断连后，旧连接遗留 `event_batch` 不会在新连接上重放
   - overload 断连后，旧连接遗留 reject ack 不会错误打到新状态
   - overload 主动关链后的 `OnDisconnect` 不会回落成普通 detach
   - reconnect 后同一 attached surface 可以在保留状态上继续恢复

7. 事故回归测试
   - 构造一个会产生大量 command output delta 的场景
   - 断言不再出现 `relay client outbox full`
   - 断言 Feishu 侧不会刷出成百上千条同类红卡

8. 可观测性测试
   - 合并器 flush 统计会按预期递增
   - wrapper data/control queue 与 epoch drop 统计会按预期更新
   - daemon ingress queue 深度和 overload / stale epoch 统计会按预期更新

### 10.2 手工验证

1. 在真实 Feishu surface 上跑一次大输出命令。
2. 同时观察：
   - wrapper raw log
   - relayd raw log
   - relayd 普通日志
   - Feishu 侧卡片与回复
3. 确认：
   - instance 不会卡死
   - thread 状态仍可继续使用
   - overload 后 surface 不会意外脱附
   - 没有 notice 风暴

## 11. 涉及文件

预计至少会涉及：

- `internal/app/wrapper/app.go`
- `internal/adapter/relayws/client.go`
- `internal/adapter/relayws/server.go`
- `internal/app/daemon/app.go`
- `internal/core/orchestrator/service_snapshot.go`
- `internal/core/orchestrator/service_queue.go`
- 相关 `*_test.go`

## 12. 待讨论取舍

1. transport degraded 是否需要在用户可见状态机里单独呈现一个“已接管但实例链路暂时不可用”的状态，还是先只通过 notice 表达。
2. wrapper control plane 是做独立 outbox 还是单 outbox 预留容量。当前更倾向前者，因为测试与语义更直接。
3. `gateway.Apply` 是否在这一轮一并彻底异步化。当前建议先不强绑在第一轮修复里，但实现后必须根据实测重新评估。

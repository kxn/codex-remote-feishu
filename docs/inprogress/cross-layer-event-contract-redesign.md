# 跨层事件契约激进重构方案

> Type: `inprogress`
> Updated: `2026-04-22`
> Summary: 基于主干 `1511f9b` 重新确认跨层漂移根因，改为激进方案：引入稳定事件契约层、统一 gateway target resolution，并把 followup 抑制改为显式语义。

## 背景

`#347` 在当前主干 `1511f9b` 上重新评估后，早期那类“直接编译断裂”的证据已经失效，但跨层漂移并没有消失。

当前还活着的问题集中在三类，而且三类问题共享同一个根因：

1. `MultiGatewayController` 对 gateway target resolution 没有单一契约。
2. `control.UIEvent` 仍是 `Kind + 多 payload 指针` 的单体跨层 ABI。
3. first-result / owner-card handoff 后的 followup 抑制仍靠 `Kind` / payload presence heuristic。

这意味着现在的问题已经不是单个补丁漏改，而是：

- 事件形状不稳定
- 路由语义不稳定
- handoff 语义不稳定
- 下游消费者直接绑定这些不稳定细节

如果继续按折中方案只修局部 contract，能止血，但不能真正拔掉“跨层连带破坏”的根。

## 目标

1. 建立一个稳定、显式、可迁移的跨层事件契约层，替代 `control.UIEvent` 作为 live cross-layer contract。
2. 把 gateway target resolution 收口成单一 resolver contract，覆盖 `Apply`、`SendIMFile`、`SendIMImage`、`RewriteFinalBlock`。
3. 把 first-result / owner-card handoff 的 followup 保留/抑制规则提升为显式语义，而不是 scattered heuristic。
4. 允许分阶段迁移，避免一次性重写全部 producer / consumer。
5. 为新契约建立固定 compile gate、behavior matrix 和遗留路径删除红线。

## 非目标

1. 本文不直接提交生产补丁。
2. 本文不处理 `#352` 已拆出的 package 物理拆分全局方案。
3. 本文不重开已完成的菜单/frontstage 波次；`#354`、`#360` 保持既有收口结果。
4. 本文不追求第一阶段就删除所有 legacy type alias；兼容桥接是有意保留的迁移工具。

## 当前问题确认

### 1. Gateway target resolution drift

当前 `Apply`、`SendIMFile`、`SendIMImage`、`RewriteFinalBlock` 共享同一组“目标 gateway 从哪里来”的问题，但并没有共享同一个 resolver：

- `Apply` 允许单 worker 时回落到 sole gateway，否则报错
- `SendIMFile` / `SendIMImage` 复制了几乎同一套逻辑，但错误类型又独立
- `RewriteFinalBlock` 允许 `GatewayID -> SurfaceSessionID -> no-op` 的另一套 fallback

结果不是“有一个 resolver 带不同 policy”，而是“四段逻辑恰好长得相似”。

### 2. `control.UIEvent` 是高脆弱性的单体契约

当前 `control.UIEvent` 同时承载：

- route 信息
- lifecycle / inline replace 信息
- page / selection / request / picker / notice / timeline / block 等 payload
- command side effect payload

下游再配合 `Kind` 和一堆 `nil` 判定去解释具体语义。

这类 contract 的问题不是“大”，而是任何新增 / 重命名 / 删除字段都可能在 projector、daemon、testkit、orchestrator 同时形成隐式 ABI break。

### 3. followup suppression 仍然是 heuristic

当前已有一部分显式语义基础：

- `Notice.DeliveryFamily`
- `service_verbosity.go` 的可见性分类
- `FeishuFrontstageActionContract`

但 handoff 过滤并没有真正复用这套语义，而是另起一套 heuristic：

- `event.Notice != nil`
- `event.ThreadSelection != nil`
- `event.Kind == UIEventNotice`

这会让“为什么这条事件该丢弃”无法被 contract-level 表达，也无法被集中测试。

### 4. testkit / projector 仍直接吃 legacy shape

`projector`、`testkit/mockfeishu`、`testkit/harness` 目前都直接依赖 `control.UIEvent` 及 `event.Kind` 分支。

这使测试不是 contract guard，而是 contract participant。本来应该帮我们锁 ABI 的工具，反而和 ABI 一起松耦合漂移。

## 激进方案总览

本文选择的激进方案有三根主梁：

1. 新建稳定包 `internal/core/eventcontract`
2. 新建兼容桥 `internal/core/eventcontractcompat`
3. 把 gateway target resolution 与 followup semantics 一并纳入新契约，而不是继续散落在 adapter / daemon / orchestrator

目标不是“把 `UIEvent` 复制一份”，而是把跨层 contract 拆成三部分：

- `Envelope`
  - 稳定的 route / lifecycle / semantics 元信息
- `Payload`
  - 明确类型的一组事件 payload
- `Resolver / Policy`
  - route target resolution 与 post-result followup contract

## 契约形状

### 1. Envelope

建议使用 typed envelope，而不是继续 `Kind + pointer bag`：

```go
type Event struct {
    Meta      EventMeta
    Payload   Payload
}

type EventMeta struct {
    Target            TargetRef
    SourceMessageID   string
    SourcePreview     string
    DaemonLifecycleID string
    InlineReplaceMode InlineReplaceMode
    Semantics         DeliverySemantics
}
```

这里的关键点：

- `Payload` 决定“这是什么事件”
- `Semantics` 决定“这条事件在什么 handoff / visibility 语境下怎么处理”
- `TargetRef` 决定“这条事件 / 这个请求应该发到哪个 gateway”

不再允许消费者通过 `Kind` 和 payload 是否为 `nil` 去猜。

### 2. Payload 家族

第一版 payload 家族建议固定为下面这些类型：

- `SnapshotPayload`
- `SelectionPayload`
- `PagePayload`
- `RequestPayload`
- `PathPickerPayload`
- `TargetPickerPayload`
- `ThreadHistoryPayload`
- `PendingInputPayload`
- `NoticePayload`
- `PlanUpdatePayload`
- `BlockCommittedPayload`
- `TimelineTextPayload`
- `ImageOutputPayload`
- `ExecProgressPayload`
- `AgentCommandPayload`
- `DaemonCommandPayload`

这些类型本身是稳定 API 面；之后新增类型时，必须同步所有 exhaustiveness gate。

### 3. DeliverySemantics

`DeliverySemantics` 不承载业务内容，只承载跨层调度要用到的显式语义：

```go
type DeliverySemantics struct {
    VisibilityClass       VisibilityClass
    HandoffClass          HandoffClass
    FirstResultDisposition FirstResultDisposition
    OwnerCardDisposition   OwnerCardDisposition
}
```

建议第一版最少表达这些问题：

- 这条事件是导航类、过程类还是必须始终可见
- first-result replace 之后它应继续追加还是被抑制
- owner-card processing / terminal handoff 后它是否允许继续冒泡

当前已有的 `Notice.DeliveryFamily`、`classifySurfaceVisibleEvent(...)` 和 `FeishuFrontstageActionContract` 应作为迁移输入，而不是继续并行存在。

最终目标是：

- projector 不再自己定义“可见性”
- daemon 不再自己定义“哪些 followup 要丢”
- orchestrator 不再自己定义“picker followup 过滤”

这些判断统一收口到 `DeliverySemantics`。

## TargetRef 与统一 resolver

### 1. TargetRef

gateway target resolution 不应继续作为 controller 的内部私有技巧，而应成为契约一部分：

```go
type TargetRef struct {
    GatewayID        string
    SurfaceSessionID string
    SelectionPolicy  GatewaySelectionPolicy
    FailurePolicy    GatewayFailurePolicy
}
```

推荐最少提供这些策略：

- `GatewaySelectionExplicitOnly`
- `GatewaySelectionAllowSoleGatewayFallback`
- `GatewaySelectionAllowSurfaceDerivedFallback`

以及这些失败策略：

- `GatewayFailureError`
- `GatewayFailureTypedError`
- `GatewayFailureNoop`

这样 `Apply`、`SendIMFile`、`SendIMImage`、`RewriteFinalBlock` 的差异就不再体现在各自复制实现，而是体现在调用同一个 resolver 时传入不同 policy。

### 2. 单一 resolver 的责任

统一 resolver 至少要负责：

- 规范化 `GatewayID`
- sole gateway fallback
- surface-derived gateway fallback
- gateway not running 识别
- typed error / no-op 决策
- 返回统一的 resolved target 结果

controller 层以后只允许调用 resolver，不允许再手写 target selection。

## 显式 followup 语义

### 1. 设计要求

现在要解决的不是“notice 要不要过滤”本身，而是“某条 followup 在 handoff 后应该如何处理”的显式表达。

因此激进方案里，不再保留：

- `filterNoticeUIEvents(...)`
- `filterThreadSelectionUIEvents(...)`
- `targetPickerFilteredFollowupEvents(...)`
- `pathPickerFilteredFollowupEvents(...)`

这些 helper 最终都应退化为对 `DeliverySemantics` 的统一过滤调用，甚至被完全删除。

### 2. 与 action contract 的关系

`FeishuFrontstageActionContract` 也需要从当前的布尔开关升级为策略对象。

当前：

- `DropNoticeEventsAfterResult`
- `DropThreadSelectionAfterResult`

目标形态：

```go
type FollowupPolicy struct {
    DropClasses []HandoffClass
    KeepClasses []HandoffClass
}
```

然后：

- event 提供 `HandoffClass`
- action contract 提供 `FollowupPolicy`
- daemon/orchestrator 通过统一 evaluator 判定是否保留

这样 handoff 规则就从“这个事件碰巧有 `Notice` payload”升级为“这条事件属于哪个 delivery / handoff class”。

### 3. 现有语义资产如何吸收

激进方案不是推翻当前所有语义积累，而是吸收并上提：

- `Notice.DeliveryFamily`
  - 继续作为 notice payload 的内部分类来源
- `classifySurfaceVisibleEvent(...)`
  - 迁移为 `DeliverySemantics.VisibilityClass` 的生成逻辑，再删除旧 helper
- `FeishuFrontstageActionContract`
  - 从布尔字段迁移为显式 followup policy

## 兼容桥接与迁移顺序

### 1. 兼容桥接

为了避免 `eventcontract` 与 `control` 互相形成 import cycle，桥接应放到单独包：

- `internal/core/eventcontract`
- `internal/core/eventcontractcompat`

桥接包职责：

- `FromLegacyUIEvent(control.UIEvent) (eventcontract.Event, error)`
- `ToLegacyUIEvent(eventcontract.Event) (control.UIEvent, error)`

第一阶段允许 legacy producer 继续发 `control.UIEvent`，consumer 在边界处转换。

### 2. 为什么先迁移 consumer

先迁移 consumer 的原因是：

- projector / testkit / daemon handoff 逻辑正是当前 ABI 破坏面
- producer 数量更多、分布更散，直接先动 producer 会把迁移面放大

因此顺序应是：

1. 新 contract + compat bridge
2. consumer 改吃 new contract
3. producer 改直接产出 new contract
4. 删除 legacy live path

## 分阶段实施

### 阶段 1：立新契约但不切生产输出

范围：

- `internal/core/eventcontract`
- `internal/core/eventcontractcompat`
- resolver contract 草实现
- contract compile guard

完成标准：

- typed payload、`EventMeta`、`DeliverySemantics`、`TargetRef` 定型
- 能从 `control.UIEvent` 无损转换到 `eventcontract.Event`
- 新增 contract compile smoke，锁住 payload 枚举与 interface 完整性

### 阶段 2：迁移 consumer 到新契约

范围：

- `internal/adapter/feishu/projector.go`
- `internal/app/daemon/app_ingress.go`
- `testkit/mockfeishu`
- `testkit/harness`

完成标准：

- projector 不再直接 switch `control.UIEventKind`
- mock/test harness 不再直接依赖 `control.UIEvent` shape
- first-result / owner-card handoff 过滤改读 `DeliverySemantics`

### 阶段 3：统一 controller target resolution

范围：

- `internal/adapter/feishu/controller_gateway.go`
- `internal/adapter/feishu/controller_preview.go`

完成标准：

- `Apply`、`SendIMFile`、`SendIMImage`、`RewriteFinalBlock` 全部走单一 resolver
- 差异只体现在 policy，不再体现在复制逻辑
- 有 table-driven matrix 覆盖 explicit / sole / surface-derived / missing-runtime / noop 几类行为

### 阶段 4：迁移 producer 与 action contract

范围：

- `internal/core/orchestrator/**`
- `internal/core/control/feishu_ui_lifecycle.go`
- `internal/core/control/types.go`

完成标准：

- producer 直接产出 `eventcontract.Event`
- `FeishuFrontstageActionContract` 升级为显式 followup policy
- legacy `control.UIEvent` 只剩兼容层可见

### 阶段 5：删除 legacy live path

范围：

- 删除 legacy translator / compatibility-only fields
- 收缩 `control.UIEvent`
- 删除所有 live `event.Kind` switch

完成标准：

- 生产路径不再依赖 `control.UIEvent`
- 新增 guard，禁止在非 compat 包继续引入 `control.UIEvent` 作为跨层事件契约

## 验证门

激进方案必须配套更强的固定 gate，而不是只靠全量 `go test ./...`。

### 1. compile / contract gate

固定最小集合：

- `./internal/core/eventcontract`
- `./internal/core/eventcontractcompat`
- `./internal/adapter/feishu`
- `./internal/app/daemon`
- `./internal/core/orchestrator`
- `./testkit/mockfeishu`
- `./testkit/harness`

### 2. behavior matrix

必须新增两类 table-driven matrix：

1. gateway target resolution matrix
2. first-result / owner-card followup matrix

矩阵必须直接覆盖策略，而不是只通过高层集成测试间接观察。

### 3. 遗留路径红线

在迁移期内加三条红线：

1. 非 compat 包不得新增新的 `control.UIEventKind` switch。
2. controller 层不得新增新的手写 gateway 选择逻辑。
3. handoff 过滤不得再按 `event.Notice != nil`、`event.ThreadSelection != nil` 这类 payload heuristic 扩写。

## 风险与取舍

### 1. 范围大

这是一次真实的大迁移，不是假装激进的局部重命名。它会同时影响：

- projector
- daemon ingress
- orchestrator producer
- testkit
- controller routing

因此必须阶段化推进。

### 2. 过渡期会有双轨

`control.UIEvent` 与 `eventcontract.Event` 会短期并存。这是有意设计，不是失败。

只要边界清楚，双轨期是可接受成本；真正不可接受的是无限期并存。

### 3. 不再把“typed envelope”当可选增强

本文明确把 typed envelope 当作主线，而不是“以后有空再做”的可选优化。

原因很简单：当前仍活跃的三个问题，本质上都是同一份不稳定 ABI 的不同表象。

## 建议拆分顺序

后续如果从 `#347` 继续拆实现单，建议顺序如下：

1. `eventcontract` + compat bridge + compile gate
2. consumer migration（projector / daemon ingress / testkit）
3. unified gateway target resolver
4. producer migration + followup policy 升级
5. legacy live path deletion

其中：

- 1 是全局前置依赖
- 2 与 3 可以局部并行，但不能同时改同一 consumer 文件
- 4 必须建立在 1~3 已稳定之上
- 5 是明确 close gate，不能永久拖延

## 结论

在当前主干上，激进方案不是“为了架构而架构”，而是把三个仍活跃的结构性问题还原为同一个根因，再用同一份稳定契约一次性收口：

- 用 typed event contract 收口跨层 ABI
- 用 `TargetRef + resolver` 收口 gateway target resolution
- 用 `DeliverySemantics + FollowupPolicy` 收口 handoff 过滤

折中方案可以缓解漂移；激进方案才是去掉漂移母体。

# 统一飞书调用入口设计

> Type: `draft`
> Updated: `2026-04-18`
> Summary: 初版统一飞书调用入口设计，整理当前调用面、限速风险、统一调度器分层、重试/backoff 策略与后续调研问题。

## 1. 背景

当前仓库里与飞书相关的 OpenAPI 调用已经覆盖多个业务域：

- IM 消息发送、回复、patch、reaction、图片/文件上传
- IM 入站补全读取（引用消息查询、图片资源下载）
- Drive 预览目录、文件、权限与清理
- Bitable / Cron 的表、字段、记录与权限操作
- 应用 scope 查询与权限状态刷新
- onboarding / websetup 的飞书注册直连 HTTP 请求

这些调用目前的共同问题不是“没有 SDK client”，而是：

1. 没有统一调度入口
2. 没有统一限速与优先级
3. 没有统一的 rate-limit backoff
4. 不同业务域互相看不见彼此的请求压力
5. 没有把飞书平台限制映射成仓库内部的可执行策略

与此同时，飞书平台本身已经存在多个重要限制，详见 `docs/general/feishu-card-api-constraints.md`，其中对当前仓库影响最大的包括：

- 同一用户 / 同一群消息发送 `5 QPS`
- 单消息卡片 patch `5 QPS`
- 单卡 CardKit 操作 `10 次/秒`
- 全局 OpenAPI 常见限制 `50 次/秒`、`1000 次/分钟`
- rate limit 响应返回 `x-ogw-ratelimit-reset`
- 交互回调 `3 秒` 内必须响应

因此，需要设计一个“唯一飞书调用入口”，把业务优先级、资源键限速、重试/backoff 和可观测性统一收拢。

## 2. 目标

### 2.1 设计目标

这套统一入口要解决的是：

1. **统一调度**：所有飞书 OpenAPI 调用都应经过同一套 broker，而不是到处直打 `*lark.Client`
2. **不超限**：在应用级、业务域级和资源级别上，尽量主动节流，不把请求直接打到飞书再等 429
3. **用户面优先**：当前 turn 的用户可见输出优先于后台清理、权限刷新、批量迁移
4. **统一 backoff**：一旦命中限流，不是单个 goroutine 自己 sleep，而是让对应 bucket 进入共享 cooldown
5. **安全重试**：按照请求副作用和幂等性区分不同重试策略，而不是“所有错误都重试”
6. **可观测**：能够回答“现在谁在打飞书”“哪个 lane 堵住了”“是不是在频控 backoff”
7. **便于渐进迁移**：不要求一步重构所有飞书调用点，允许分阶段接入

### 2.2 非目标

当前这份设计明确不试图在第一阶段解决：

1. 所有卡片交互都改造成 CardKit streaming
2. 所有 SDK 使用点都在一次提交里重写完毕
3. 所有飞书错误都自动恢复
4. 所有业务域共享完全相同的重试次数与节流参数

## 3. 当前调用面盘点

### 3.1 IM 出站写操作

主要位置：

- `internal/adapter/feishu/gateway_runtime.go`
- `internal/adapter/feishu/im_file.go`

主要调用：

- `im.v1.message.create`
- `im.v1.message.reply`
- `im.v1.message.patch`
- `im.v1.message.delete`
- `im.v1.image.create`
- `im.v1.file.create`
- `im.v1.message_reaction.create`
- `im.v1.message_reaction.delete`
- `im.v2.feed_card.bot_time_sensitive`

特点：

- 与用户可见体验直接相关
- 对消息发送、patch、卡片大小限制最敏感
- 当前 `LiveGateway.Apply()` 是串行直打，没有统一节流和自动 backoff

### 3.2 IM 入站补全读操作

主要位置：

- `internal/adapter/feishu/gateway_inbound_quoted_inputs.go`

主要调用：

- `im.v1.message.get`
- `im.v1.message_resource.get`

特点：

- 用于引用消息展开、图片下载
- 本身是读操作，但 burst 时会和前台出站竞争全局 API 额度

### 3.3 Drive 预览与清理

主要位置：

- `internal/adapter/feishu/markdown_preview_lark_api.go`
- `internal/adapter/feishu/markdown_preview_admin.go`
- `internal/adapter/feishu/markdown_preview_managed.go`

主要调用：

- `drive.v1.file.create_folder`
- `drive.v1.file.upload_all`
- `drive.v1.meta.batch_query`
- `drive.v1.permission_member.create`
- `drive.v1.permission_member.list`
- `drive.v1.file.list`
- `drive.v1.file.delete`

特点：

- 管理面、后台维护、Rewrite/上传可能形成短时间 burst
- 不应与 turn 输出共用同优先级

### 3.4 Bitable / Cron

主要位置：

- `internal/adapter/feishu/bitable_api.go`
- `internal/app/daemon/app_cron_*.go`

主要调用：

- `bitable.v1.app.*`
- `bitable.v1.app_table.*`
- `bitable.v1.app_table_field.*`
- `bitable.v1.app_table_record.*`
- `drive.v1.permission_member.*`

特点：

- 有分页和 batch create / batch update 循环
- 是最典型的后台批处理型调用
- 极易短时间打出大量请求

### 3.5 权限刷新

主要位置：

- `internal/adapter/feishu/scopes.go`
- `internal/app/daemon/app_feishu_permissions.go`

主要调用：

- `application.v6.scope.list`

特点：

- 单次负载低
- 周期性后台任务
- 应纳入低优先级后台 lane

### 3.6 onboarding / websetup 直连 HTTP

主要位置：

- `internal/app/daemon/admin_feishu_onboarding.go`

主要调用：

- `/open-apis/auth/v3/tenant_access_token/internal`
- `/open-apis/bot/v3/info`
- `/oauth/v1/app/registration`

特点：

- 当前绕开 SDK 和任何统一治理
- 也属于飞书调用面，后续应纳入统一 broker，但通过 raw HTTP 适配层接入

### 3.7 长连接握手与事件通道

主要位置：

- `internal/adapter/feishu/longconn.go`

特点：

- 主要是事件长连接和鉴权，不是本设计第一阶段的重点
- 但其相关 HTTP 请求和鉴权过程长期来看也应纳入统一调用治理视角

## 4. 当前主要风险

### 4.1 用户可见输出与后台任务竞争

现在的 Drive cleanup、Bitable repair、权限刷新等后台调用与当前 turn 的消息发送没有统一隔离机制。

风险是：

- 后台任务 burst 时，前台消息发送/patch 也会被飞书限流
- 但用户只能看到“机器人慢了”或“卡片 patch 失败”，看不到内部原因

### 4.2 单消息 / 单资源键限速没有建模

现在虽然知道飞书有：

- 同一用户/同一群 `5 QPS`
- 单消息 patch `5 QPS`
- 单卡 streaming `10 次/秒`

但系统当前没有把这些限制转成“资源键级别”的限速器。

### 4.3 频控后的恢复机制过弱

当前多数调用的错误处理基本是：

- 返回错误
- 由上层记录日志或降级

缺少：

- 识别 rate limit 响应头
- 共享 cooldown
- 针对资源键的 backoff

### 4.4 缺少调用域的统一观测

当前很难回答这些问题：

- 最近 30 秒是哪个 lane 在打飞书
- 是谁触发了频控
- 哪个 bucket 在 cooldown
- 哪些请求被重试了几次
- 哪些请求是被 coalesce 掉的

## 5. 总体方案

### 5.1 核心抽象：每个 Gateway 一个 Feishu Call Broker

建议引入新的核心组件：

- `FeishuCallBroker`

边界：

- **按 gateway / app 维度实例化**
- 每个飞书应用有自己独立的 broker
- 同一应用内的 IM、Drive、Bitable、Scope、Raw HTTP 调用共享 broker

直观理解：

- 不是“再包一层 Lark client”
- 而是“每个应用有一个飞书调用调度中心”

### 5.2 统一入口形式

建议统一入口同时支持 SDK 和 raw HTTP 两种调用形态：

```go
DoSDK[T any](ctx context.Context, spec CallSpec, fn func(context.Context, *lark.Client) (T, error)) (T, error)
DoHTTP[T any](ctx context.Context, spec CallSpec, fn func(context.Context, *http.Client) (T, error)) (T, error)
```

这样：

- `gateway_runtime.go`、`bitable_api.go`、`markdown_preview_lark_api.go` 走 `DoSDK`
- `admin_feishu_onboarding.go` 走 `DoHTTP`
- 两边共享同一套：
  - 排队
  - 限速
  - 重试
  - backoff
  - 指标

### 5.3 CallSpec

统一入口需要由业务层显式提供调用语义，至少包括：

```go
type CallSpec struct {
    GatewayID   string
    API         string
    Class       CallClass
    Priority    CallPriority
    ResourceKey FeishuResourceKey
    Retry       RetryPolicy
}
```

其中：

- `API`：具体飞书 API 名称，例如 `im.v1.message.create`
- `Class`：业务类别，用于 lane 和默认策略映射
- `Priority`：调度优先级
- `ResourceKey`：资源键，用于细粒度限速
- `Retry`：是否允许自动重试、最大次数、是否只对限流重试等

## 6. 分层与 lane 设计

### 6.1 设计原则

当前建议不是只搞一个全局队列，而是至少四层：

1. 应用全局总量限制
2. 业务类别 lane
3. 资源键级限速
4. 优先级调度

### 6.2 建议的 CallClass

第一版建议至少区分：

- `im_send`
- `im_patch`
- `im_read`
- `reaction_write`
- `drive_read`
- `drive_write`
- `bitable_read`
- `bitable_write`
- `app_meta_read`
- `raw_http`

这样做的原因：

- IM 发送 / patch 与 Drive/Bitable 在业务重要性上明显不同
- patch 的资源键模型和发送不一样
- Bitable 写操作通常比读操作更昂贵、更容易成批量 burst

### 6.3 建议的 Priority

建议至少三档：

- `P0Interactive`
  - 用户当前 turn 的可见输出
  - 例如发消息、reply、patch 当前卡、发送图片/文件
- `P1ReadAssist`
  - 当前用户动作的补全读取
  - 例如引用消息查询、图片下载
- `P2Background`
  - 后台清理、权限刷新、Drive inventory、Bitable repair、轮询类请求

可选第四档：

- `P3Maintenance`
  - 低价值但长批量维护任务

### 6.4 第一版的 lane 预算建议

下面的数字只是设计起点，不是最终定值：

- app 全局：保守低于飞书常见 `50 QPS`
- `im_send`：保证前台 turn 输出不被后台挤死
- `im_patch`：显式低于单消息 patch 限制，并允许按消息 ID 再限速
- `drive_write` / `bitable_write`：低并发、低优先级
- `drive_read` / `bitable_read`：可比写操作宽，但不能无限并发

这里后续需要进一步调研并确定：

- lane 数量是否要再简化
- 每个 lane 是令牌桶还是固定并发
- 不同 lane 是否允许 borrow 预算

## 7. 资源键设计

### 7.1 为什么需要资源键

飞书很多限制不是“整个应用每秒多少次”，而是更细：

- 同一用户 / 同一群发送消息 `5 QPS`
- 单消息卡片 patch `5 QPS`
- 单卡 streaming `10 次/秒`

因此 broker 必须能对“资源键”限速，而不是只看全局 API 名。

### 7.2 第一版资源键模型

```go
type FeishuResourceKey struct {
    ReceiveTarget string
    MessageID     string
    CardID        string
    DocToken      string
    TableID       string
}
```

### 7.3 不同 API 的资源键映射建议

#### `im.v1.message.create` / `reply`

主资源键：

- `ReceiveTarget`

用途：

- 贴合同一用户 / 同一群发送 `5 QPS`

#### `im.v1.message.patch`

主资源键：

- `MessageID`

用途：

- 贴合单消息 patch `5 QPS`
- 为后续 patch coalescing 做锚点

#### CardKit streaming（未来）

主资源键：

- `CardID`

用途：

- 贴合单卡 `10 次/秒`

#### Drive

主资源键：

- `DocToken`
- 某些 list 调用可回退到 folder token

用途：

- 避免同一文档/目录清理或授权连续打爆

#### Bitable

主资源键：

- `TableID`
- 必要时可进一步细化到 record batch

用途：

- 避免大批量 repair / migration 同时挤在一个表上

## 8. 请求生命周期

统一入口中的一次请求建议按如下流程执行：

1. 构造 `CallSpec`
2. 进入 broker
3. 根据 `Class` 与 `Priority` 选择 lane
4. 根据 `ResourceKey` 获取对应 limiter / cooldown bucket
5. 等待全局与资源键双重许可
6. 执行请求
7. 解析结果：
   - 成功
   - 可重试失败
   - 不可重试失败
8. 若命中频控：
   - 读取 header 或错误码
   - 设置 bucket cooldown
   - 按 retry policy 退避重试
9. 记录指标、日志、trace

## 9. 重试与 backoff 策略

### 9.1 基本原则

不是所有调用都应自动重试。

必须区分：

1. 是否幂等
2. 是否用户可见副作用
3. 是否明确命中频控
4. 是否能安全判断“上一次调用实际上没成功”

### 9.2 建议的三类策略

#### A. 默认可重试

适用：

- `GET` / `LIST` / `QUERY`
- 幂等 patch / update
- 显式带幂等键的请求

例如：

- `im.v1.message.get`
- `im.v1.message_resource.get`
- `application.v6.scope.list`
- `drive.v1.file.list`
- `drive.v1.meta.batch_query`
- 大多数 `bitable list`

#### B. 仅对 rate-limit 明确重试

适用：

- `message.create`
- `message.reply`
- `image.create`
- `file.create`

原因：

- 如果明确是 429 / `99991400`，通常可视为未成功受理
- 但如果是网络超时或连接中断，不能默认安全重放

#### C. 默认不自动重试

适用：

- 没有幂等键且重复执行会产生重复副作用的 create 操作
- 用户体验上重复执行成本很高的动作

### 9.3 rate-limit 识别策略

优先使用官方信号：

- HTTP `429`
- 旧接口 `400 + code=99991400`
- 响应头 `x-ogw-ratelimit-reset`

当前 SDK 能从 `ApiResp.Header` 取到原始 header，因此本设计在当前 SDK 版本上是可落地的。

### 9.4 backoff 策略

#### 第一优先：尊重 `x-ogw-ratelimit-reset`

如果响应头中存在：

- `x-ogw-ratelimit-reset`

则：

1. 解析 reset 秒数
2. 对对应 bucket 标记 `cooldownUntil`
3. 所有后续同 bucket 请求都等待 cooldown，而不是继续撞墙
4. 在 reset 基础上加小幅 jitter

#### 第二优先：保守指数退避

如果没有 reset header，则对可重试请求使用：

- `500ms`
- `1s`
- `2s`
- `4s`
- `8s`

并叠加 jitter。

### 9.5 为什么要共享 cooldown

如果只是让单个请求 sleep，会产生两个问题：

1. 其他 goroutine 不知道已经限流，继续撞飞书
2. 同一资源键上的请求会在 sleep 醒来后一起涌出

因此 cooldown 应该挂在：

- app 全局 bucket
- lane bucket
- 资源键 bucket

至少其中之一上，而不是只存在于单次请求栈上。

## 10. Coalescing 与 Singleflight

### 10.1 Patch Coalescing

对同一 `MessageID` 的 patch，如果前一个 patch 还在队列里，后一个 patch 到来时，第一版建议支持“保留最新值覆盖旧值”。

适用场景：

- 当前卡片状态连续更新
- 未来 live card 文本 / plan 多次 patch

收益：

- 减少无意义 patch
- 降低单消息 `5 QPS` 风险

### 10.2 Singleflight 读取合并

对一些天然可合并的读操作，建议共享请求结果：

- 同一 message ID 的 `message.get`
- 同一图片资源的 `message_resource.get`
- 同一 gateway 的 `scope.list`
- 同一 folder 的短时间 inventory scan

## 11. 可观测性设计

至少应暴露以下信息：

### 11.1 指标

- 每个 gateway 的总请求数
- 每个 `CallClass` 的请求数、成功数、失败数、重试数
- 每个 lane 当前队列长度
- 当前处于 cooldown 的 bucket 数量
- rate-limit 触发次数
- 平均等待时间 / 执行时间

### 11.2 日志

命中频控时，日志至少带上：

- `gateway_id`
- `api`
- `class`
- `priority`
- `resource_key`
- `reset_after`
- `retry_attempt`
- `request_id`

### 11.3 调试接口

后续可以考虑在 admin/debug 面板加入：

- 当前各 lane 深度
- 最近一次被限流的 API
- 哪些资源键正在 cooldown

## 12. 与现有结构的集成建议

### 12.1 不应直接替换 `*lark.Client` 为隐藏黑盒

现有代码大量直接持有 `*lark.Client`，如果一步彻底藏掉，改造面会过大。

因此第一版建议：

- `LiveGateway`、`liveBitableAPI`、`DriveMarkdownPreviewer` 等仍可持有 `*lark.Client`
- 但所有实际发请求的地方，统一改为通过 broker 执行

### 12.2 建议新增的核心位置

建议新增一个集中模块，例如：

- `internal/adapter/feishu/callbroker.go`
- `internal/adapter/feishu/callpolicy.go`
- `internal/adapter/feishu/callratelimit.go`
- `internal/adapter/feishu/callretry.go`
- `internal/adapter/feishu/callmetrics.go`

### 12.3 第一阶段接入范围

建议第一阶段只接 IM 主链路：

- `message.create`
- `message.reply`
- `message.patch`
- `image.create`
- `file.create`
- `reaction create/delete`
- `message.get`
- `message_resource.get`

原因：

- 对用户体验收益最大
- API 面足够集中
- 能先验证资源键限速、patch coalescing、rate-limit backoff 是否好用

### 12.4 第二阶段接入范围

- Drive preview
- scope refresh
- onboarding raw HTTP

### 12.5 第三阶段接入范围

- Bitable / Cron
- 更复杂的后台批处理 lane

## 13. 与卡片交互时序的边界

这套统一入口设计有一个明确例外：

- **飞书卡片回调的同步响应** 不适合走普通排队器

原因：

- 卡片回调要求 `3 秒` 内返回
- 同步 inline replace 本质上不是 OpenAPI 请求，而是 callback response payload

因此：

1. callback 的同步返回继续走快速旁路
2. callback 之后触发的异步 OpenAPI 调用应进入 broker

## 14. 当前待进一步调研的问题

这部分需要在关联 issue 中继续细化。

### 14.1 lane 的最终数量与预算

还需要明确：

- lane 是否维持 3 档 priority + 多类 class
- 还是简化成更少但更可控的 queue
- 不同 lane 的并发与 QPS 预算如何分配

### 14.2 飞书不同 API 的默认 RetryPolicy 矩阵

需要形成更具体的表：

- 哪些 API：可重试
- 哪些 API：仅 rate-limit 可重试
- 哪些 API：禁止自动重试
- 哪些 API：建议调用方显式 opt-in

### 14.3 header 与错误码的兼容性

虽然当前 SDK 暴露了 `ApiResp.Header`，但仍需进一步确认：

- 所有相关 API 是否都会稳定返回 `x-ogw-ratelimit-reset`
- 不同业务域是否都遵循相同 header 语义
- 哪些旧接口只会返回 `400 + 99991400`

### 14.4 Bitable / Drive 的专有节流面

目前对 Bitable / Drive 的限制判断仍偏经验，需要进一步调研：

- 是否存在更细的资源级限制
- 批量接口失败时是否可能部分成功
- 是否需要单独的 chunk retry 语义

### 14.5 队列里的请求是否需要取消/覆盖

例如：

- 同一条消息的旧 patch 是否要直接被新 patch 替换
- 同一 scope 刷新是否要去重
- 同一目录的 inventory scan 是否应 singleflight

### 14.6 是否需要把 broker 状态上送到 UI / admin

需要进一步判断：

- 这是纯内部调试能力
- 还是应进入 admin 状态页，帮助定位“为什么飞书回复变慢”

## 15. 建议的下一步

1. 先创建关联 issue，明确这项工作是“统一飞书调用治理基础设施”，而不是单一 bug 修复
2. 在 issue 中补齐：
   - lane 方案候选
   - RetryPolicy 矩阵草案
   - 第一阶段接入清单
   - 验证方案
3. 完成进一步调研后，再决定是否拆为：
   - 基础 broker
   - IM 主链路接入
   - Drive/Bitable 接入
   - 指标与调试面板

## 16. 相关代码位置

- `internal/adapter/feishu/gateway_runtime.go`
- `internal/adapter/feishu/gateway.go`
- `internal/adapter/feishu/im_file.go`
- `internal/adapter/feishu/gateway_inbound_quoted_inputs.go`
- `internal/adapter/feishu/markdown_preview_lark_api.go`
- `internal/adapter/feishu/bitable_api.go`
- `internal/adapter/feishu/scopes.go`
- `internal/adapter/feishu/client_timeout.go`
- `internal/app/daemon/admin_feishu_onboarding.go`
- `internal/app/daemon/app_feishu_permissions.go`
- `internal/adapter/feishu/controller_gateway.go`

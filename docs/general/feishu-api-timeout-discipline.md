# Feishu API Timeout Discipline

> Type: `general`
> Updated: `2026-04-21`
> Summary: 固化 `#226` 的当前仓库基线：所有 Feishu SDK client 统一带默认请求超时，高风险链路在外部 IO 边界派生更短 deadline，同时保留上层 cancel 语义。

## 1. 文档定位

这份文档记录的是当前仓库对 Feishu API 调用的长期纪律，而不是某一次 bug 修复的过程复盘。

它回答三个问题：

1. Feishu SDK client 现在应该怎样构造。
2. 上层 `ctx` 的 cancel 语义，和单次外部 IO 的耗时上界，当前怎样分工。
3. 哪些高风险链路已经纳入统一 bounded-call 策略。

## 2. 当前统一规则

### 2.1 所有 Lark SDK client 走统一构造入口

当前仓库不再鼓励直接调用 `lark.NewClient(...)`。

统一基线是：

1. 所有 Feishu SDK client 都通过 `internal/adapter/feishu/client_timeout.go` 中的 `NewLarkClient(...)` 构造。
2. 这个构造入口会为 SDK 请求层设置默认请求超时，避免新入口继续以“长生命周期 ctx + 无默认超时”的方式访问 Feishu。
3. `gateway`、`scopes`、`bitable`、preview admin 等 client 构造点都应复用这条入口，而不是各自散落策略。

换句话说，SDK 层默认请求超时现在是全仓库的兜底安全网，而不是调用方的可选习惯。

### 2.2 上层 cancel 与本地 timeout 分开承担职责

当前规则不是“只要上层有 ctx 就够了”，而是明确拆成两层：

1. 上层 `ctx` 继续表达用户取消、服务停机、HTTP request 结束等取消语义。
2. 真正靠近 Feishu API 或相关外部 IO 的位置，再派生一个带 deadline 的子 `ctx`，限制单次调用的绝对耗时。

因此当前不允许再把 runtime-lifetime、worker-lifetime、gateway-lifetime 这类长生命周期 `ctx` 直接原样透传到 Feishu SDK 或文件流读取链路。

### 2.3 默认 SDK timeout 是兜底，高风险链路允许更短预算

当前采用的是“两层兜底”策略：

1. SDK client 默认请求超时负责防止新调用点完全裸奔。
2. 高风险链路继续在边界处创建更短的本地 timeout，覆盖更严格的时延预算。

这样做的目的不是重复设置超时，而是把“全局兜底”和“链路级预算”区分开：

1. SDK 默认超时保证所有 Feishu 请求最终都会停下来。
2. 链路级 timeout 保证持锁路径、异步 worker、后台清理、补偿通知不会被单次慢请求拖垮。

## 3. 已纳入统一 bounded-call 的链路

### 3.1 send-file

`/send file` 当前已经收敛为：

1. Feishu 上传与发消息阶段都使用 bounded context。
2. 外部 IO 已移出 `a.mu` 持锁区，避免“大文件 + 慢网络”把 app 锁长期占住。

这条链路的重点不是“把 timeout 补上就结束”，而是同时消除“持锁外部 IO”这个放大风险。

### 3.2 异步 inbound 解析与失败补偿

异步 inbound lane、quoted input 展开、图片下载、merge-forward 展开与 failure notice 发送，当前都不再直接依赖 gateway 生命周期 `ctx`。

当前要求是：

1. 继续继承上层 cancel。
2. 真正进入 Feishu API、下载或补偿发送前，再派生 bounded 子 `ctx`。
3. failure notice 的本地超时不能再被原始长生命周期 `ctx` 覆盖掉。

### 3.3 preview admin 与后台清理

preview drive 的 admin 状态读取、手动 cleanup 与后台 maintenance，当前也纳入统一纪律：

1. HTTP request path 继续保留 `r.Context()` 的取消语义。
2. `Summary(...)`、`CleanupBefore(...)`、后台 cleanup worker 会在内部再加本地 timeout。

这样管理页单次查询、手动清理和后台维护都不会变成无界 Feishu API worker。

## 4. 当前边界

这份基线明确不等于：

1. 把仓库里所有 `context.Background()` 一次性全部替换掉。
2. 让所有链路都只剩本地 timeout，而完全切断上层 cancel。
3. 只在“正常成功路径”补 timeout，却遗漏失败通知、补偿逻辑和后台任务。

当前真正要避免的是两类坏模式：

1. 长生命周期 `ctx` 直接透传到 Feishu API 或相关外部 IO。
2. 持锁路径、异步 worker 或后台任务对慢请求没有绝对耗时上界。

## 5. 相关实现

- GitHub issue: `#226`
- `internal/adapter/feishu/client_timeout.go`
- `internal/adapter/feishu/im_file.go`
- `internal/adapter/feishu/gateway_inbound_lane.go`
- `internal/adapter/feishu/gateway_inbound_quoted_inputs.go`
- `internal/adapter/feishu/preview/markdown_preview_admin.go`
- `internal/adapter/feishu/preview/markdown_preview_managed.go`
- `internal/app/daemon/app_send_file.go`
- `internal/app/daemon/admin_storage_preview.go`

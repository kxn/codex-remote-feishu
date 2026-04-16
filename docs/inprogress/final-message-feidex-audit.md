# Final Message Feidex Audit

> Type: `inprogress`
> Updated: `2026-04-16`
> Summary: 重新梳理 Feidex 与当前实现的 final message 全链路，补充异步 markdown preview patch、reply card split 与 quiet working reuse 的关键差异。

## 1. 目标

本审计只回答一件事：

- 为什么 Feidex 的 final message 路径里更容易出现“先发出结果，再把可点击内容补上”的效果；
- 我们当前实现为什么没有这条补丁链路；
- 哪些机制可以直接借鉴，哪些必须按当前产品语义重新适配。

这次不再把 scope 限定在“文件名和 link 处理”，而是把 final message 从 turn item / turn completion 到 Feishu 最终可见消息的整条链路一起看清楚。

## 2. 审计范围

### 2.1 Feidex

- `internal/app/turn_item_payload.go`
- `internal/app/turn_stream.go`
- `internal/app/turn_item_cards.go`
- `internal/app/final_delivery.go`
- `internal/app/delivery.go`
- `internal/app/outbound_cards.go`
- `internal/app/markdown_preview.go`
- `internal/app/reply_card_split.go`
- `internal/app/turn_lifecycle.go`
- `internal/feishu/markdown_preview.go`
- `internal/feishu/adapter.go`

### 2.2 当前仓库

- `internal/core/orchestrator/service.go`
- `internal/core/orchestrator/service_queue.go`
- `internal/core/renderer/planner.go`
- `internal/app/daemon/app_ui.go`
- `internal/adapter/feishu/projector.go`
- `internal/adapter/feishu/card_renderer.go`
- `internal/adapter/feishu/final_card_markdown.go`
- `internal/adapter/feishu/gateway_runtime.go`
- `internal/adapter/feishu/markdown_preview_rewrite.go`
- `internal/adapter/feishu/markdown_preview_helpers.go`
- `internal/adapter/feishu/markdown_preview_managed.go`
- `internal/adapter/feishu/web_preview_registry.go`

## 3. Feidex 的 final message 全链路

### 3.1 final 的 source of truth 来自 completed turn item，不是 turn.completed

Feidex 当前把 completed turn item 当作用户可见输出的主要来源：

- `internal/app/turn_item_payload.go` 里，`buildTurnItemCardPayloadWithWorkspace(...)` 会把 `type=agent_message` 且 `phase=final_answer` 的 item 标成 `payload.IsFinalAnswer = true`。
- `internal/app/turn_stream.go` 里，`completeTurnItem(...)` 在 item 完成时立刻送出可见卡片；如果 payload 是 final answer，会先把 `stream.SentFinal = true` 置位，然后直接走 `sendTurnItemCardWithReuse(...)`。

这意味着 Feidex 的 final answer 不需要等到 `turn.completed` 才开始构造卡片；真正的 turn 完成事件主要负责补尾、兜底和清理，而不是首次 materialize final 正文。

### 3.2 首次发送就是 reply card，并立即拿到可 patch 的 message id

Feidex 的最终答复主路径在：

- `internal/app/turn_item_cards.go` `sendTurnItemCardWithReuse(...)`
- `internal/app/final_delivery.go` `sendFinalMessagesWithFooter(...)`
- `internal/app/delivery.go` `sendReplyCardChunksWithReuse(...)`

具体行为：

1. 先把正文切成 reply card chunks。
2. 每个 chunk 优先用 `ReplyCard(...)` 回复到 `sub.TriggerMessageID`。
3. 如果传入了 `reuseMessageID`，第一个 chunk 会先尝试 `PatchCard(...)` 复用旧卡。
4. 发送成功后立刻保存 `MessageID`，并把它放进后续 markdown preview patch 的输入里。

这里的关键不是“会不会 patch”，而是 **Feidex 从第一跳开始就把 final result 当成一个可继续 patch 的消息实体来维护**。

### 3.3 首次发送前，会先把本地 markdown link 降级成稳定文本

Feidex 在 `internal/app/outbound_cards.go` 与 `internal/app/markdown_preview.go` 里的 final card 渲染分两层：

- `renderReplyMarkdownCardWithHeaderOptions(...)`
- `prepareReplyCardMarkdown(...)`

当 `enablePreview=true` 时：

1. 先调用 `neutralizeLocalMarkdownLinks(...)`；
2. 本地 markdown link 先退化成稳定文本 / code span；
3. 远端 markdown link 保留；
4. 再做 `normalizeCardMarkdown(...)`。

这表示 Feidex 的首张 final card 不是“已经 materialize 完成的最终版本”，而是一个 **可安全落地、但仍可继续补强** 的初始版本。

### 3.4 markdown preview materialize 在后台异步执行，完成后 patch 同一张卡

Feidex 的这条链路在：

- `internal/app/markdown_preview.go` `scheduleMarkdownPreviewPatch(...)`
- `internal/app/markdown_preview.go` `rewriteMarkdownPreviewText(...)`
- `internal/feishu/markdown_preview.go` `RewriteText(...)`

完整顺序是：

1. final reply card 先通过 `ReplyCard(...)` 发出去；
2. 拿到 `messageID` 后，`scheduleMarkdownPreviewPatch(...)` 启一个 goroutine；
3. goroutine 内部先用 2 分钟超时跑 `rewriteMarkdownPreviewText(...)`；
4. 这个 rewrite 会调用 `feishu.RewriteMarkdownPreview(...)`，再进入 `DriveMarkdownPreviewer.RewriteText(...)`；
5. previewer 只扫描显式 markdown link，并且只处理本地 `.md` 目标；
6. 若 Drive materialize 成功，会把原始本地 link 改写成：
   - `` `display/path.md` [path.md](https://drive...) ``
7. 改写结果与原正文不同，则再用 15 秒超时调用 `PatchCard(...)`，直接 patch 之前那条 final card。

这条链路的核心结论：

- **Feidex 的“可点击内容”并不完全依赖第一次发送前就完成 rewrite。**
- 它允许先发一个安全的 neutralized 版本，再把真正可点击的 preview link 异步补回同一条消息。
- 由于 patch 的对象是原 message id，所以 reply 记录、回复链位置、点击 steer 入口都不会变。

### 3.5 reply card split 是 final 主路径的一部分，不是异常 fallback

Feidex 在 `internal/app/reply_card_split.go` 中把 split 做成了 final reply 的常规路径：

- 先按表格块切分，避免一个卡里塞过多 markdown tables；
- 再按 payload bytes 与 component count 检查；
- 超限时继续细分 block / paragraph / rune；
- 最后一个 chunk 才挂 footer；
- 多 chunk final 会显式显示 `最终答复 1/N`、`最终答复 2/N`。

因此 Feidex 面对大回复时的默认策略是：

- **分块发送多个 reply card**

而不是：

- 先拼成一个大卡，再让网关层截断。

### 3.6 quiet working card 可以被复用并 patch 成 final card

Feidex 还有一条与默认 normal path 不同、但同样重要的链路：

- `internal/app/turn_stream.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/quiet_working_card.go`

当 quiet/progress working card 仍悬挂时：

- final answer 或 turn finish 可以把那张“工作中”卡直接 `PatchCard(...)` 成 final card；
- 如果 turn 结束时没见到 final answer item，`finishTurn(...)` 还会走 `sendEmptyFinalCardWithReuse(...)` 做兜底。

这说明 Feidex 对“final message”并没有只绑定一种展现形态，而是允许：

- 新 reply card
- reuse 老 working card
- turn.completed 兜底空 final card

三者协同存在。

### 3.7 reply 关系不会在 patch 时被重绑，Feidex 是靠“先 reply、后 patch”规避这个限制

Feidex 的 SDK 调用很清楚：

- `ReplyCard(...)` 使用 reply 接口，创建一条新的 reply message；
- `PatchCard(...)` 只接受 `messageID + content`。

也就是说，Feidex 并不是“把一条已发出去的普通消息改成回复另一条消息”，而是：

1. 一开始就把消息发在正确的 reply 位置；
2. 后面只 patch 它的内容。

这也是它能保持 reply 记录稳定的原因。

## 4. 当前实现的 final message 全链路

### 4.1 final 正文要等到 turn.completed 才 materialize

当前仓库的最终正文来源是：

- `internal/core/orchestrator/service_queue.go` `storePendingTurnText(...)`
- `internal/core/orchestrator/service.go` `EventTurnCompleted` 分支
- `internal/core/orchestrator/service_queue.go` `flushPendingTurnTextWithSummary(...)`

流程是：

1. `agent_message` item 完成时，文本先进入 `pendingTurnText`；
2. 若 turn 还在继续，会在后续 item / plan / request 边界被作为非 final 文本提前 flush；
3. 真正 final 的 block，要等 `EventTurnCompleted` 时再由 `flushPendingTurnTextWithSummary(..., final=true, ...)` 统一产出；
4. `renderTextToSurface(...)` 这时才把 `block.Final = true` 写进 `UIEventBlockCommitted`。

所以我们当前的 final card 是一个 **turn-completed 才统一投影的结果**，不是 item 完成就立即拥有可 patch 的 final 消息。

### 4.2 preview/materialize 发生在发送前的同步热路径里

发送入口在 `internal/app/daemon/app_ui.go` `deliverUIEventWithContextMode(...)`：

1. 遇到 `UIEventBlockCommitted` 且 block 非空；
2. 先建立 `finalPreviewTimeout`，默认 90 秒；
3. 同步调用 `finalBlockPreviewer.RewriteFinalBlock(...)`；
4. rewrite 结果会直接覆写 `event.Block`；
5. 同步得到的 supplements 会立刻转成 `previewSupplementOps`；
6. 然后才走 projector 和 gateway 发送。

也就是说，当前实现只有一次“发送前 rewrite”机会：

- 成功：最终卡片第一次发出时就带着改写后的链接；
- 超时 / 失败：final 继续发送，但只保留 fallback 结果；
- 之后没有第二次异步 patch final card 的机会。

### 4.3 当前 preview rewrite 能力比 Feidex 更广，但没有 second-chance patch

我们的 preview rewrite 入口在：

- `internal/adapter/feishu/markdown_preview_rewrite.go`
- `internal/adapter/feishu/markdown_preview_helpers.go`
- `internal/adapter/feishu/web_preview_registry.go`

当前特征：

- 只扫描显式 markdown link；
- 不处理裸 URL；
- 不处理 `#224` 这类 shorthand；
- 目标不是本地文件、或以 `#` 开头、或已是 `://` 远端链接时直接跳过；
- 对本地目标会进入 handler/publisher 流程；
- drive preview 与 web preview 都可能产出 inline link；
- 支持的 artifact 范围明显宽于 Feidex：
  - `.md`
  - `.html`
  - 图片
  - `.pdf`
  - 各类文本
  - 以及 generic binary 的 web download 路径

但这条链路仍然是同步热路径：

- **没有“先发 neutralized，再后台补 clickable”的 second chance。**

### 4.4 final card 自身是 append-only send path，不保留后续 patch 所需状态

当前 final card 的投影在：

- `internal/adapter/feishu/projector.go` `projectBlock(...)`
- `internal/adapter/feishu/card_renderer.go`
- `internal/adapter/feishu/final_card_markdown.go`

行为是：

1. final assistant block 统一走 `OperationSendCard`；
2. 主体是单个 V2 `markdown` element；
3. 再附加文件修改 summary 和 final turn footer；
4. `normalizeFinalCardMarkdown(...)` 会把残留的本地 markdown target 降级成稳定文本；
5. `ReplyToMessageID` 指向触发 turn 的源消息。

但这里没有把 final card message id 记录成后续 preview patch 的 state：

- `internal/app/daemon/app_ui.go` 的 `recordUIEventDelivery(...)` 只专门记录 thread history card 和 exec progress card；
- final card 没有对应的 `RecordFinal...Message(...)`；
- 当前仓库里也没有 Feidex 那种 `MessageLink` 存储层。

因此虽然网关层已经有 `OperationUpdateCard -> message.patch` 能力，**当前 final path 本身并没有把 message id 留下来用于后续 patch**。

### 4.5 supplements 是 append-only sibling reply，不是 final card patch

当前 supplements 的投影路径是：

- `internal/app/daemon/app_ui.go` `ProjectPreviewSupplements(...)`
- `internal/adapter/feishu/projector.go` `ProjectPreviewSupplements(...)`

注意点：

- supplements 不是 patch 到 final card；
- 它们会被单独投影成新的 `OperationSendCard`；
- reply anchor 仍然是 `event.SourceMessageID`，不是刚刚发出的 final card message id；
- 这意味着我们现在的补充预览是“同轮新增一张卡”，不是“原 final card 内容增强”。

### 4.6 我们现有的 patch 能力主要用于共享过程卡，不用于 final card

当前 `message.patch` 主要用于：

- `internal/adapter/feishu/projector_exec_command_progress.go`
- `internal/adapter/feishu/projector_thread_history.go`

这些路径都有共同特征：

- 首次 send 时就会保留 message id；
- projector 会在后续 event 中切到 `OperationUpdateCard`；
- card config 会显式设置 `update_multi`。

而 final `projectBlock(...)` 现在不走这条模式。

### 4.7 后台维护只有 cleanup，不参与 final message 增强

`internal/adapter/feishu/controller.go` 会为 previewer 启动后台 maintenance goroutine，但它只做：

- Drive preview cleanup
- Web preview cache cleanup

实际逻辑在 `internal/adapter/feishu/markdown_preview_managed.go` `RunBackgroundMaintenance(...)` / `runBackgroundCleanup(...)`。

它和 final message 的后续 patch 没有直接关系。

## 5. 逐段对比结论

| 阶段 | Feidex | 当前实现 | 用户可见影响 |
| --- | --- | --- | --- |
| final 来源 | `agent_message phase=final_answer` item 完成即送出 | `turn.completed` 时统一 flush pending 文本 | Feidex 更早拿到可 patch 的 final message |
| 首次发送 | reply card，拿到 message id 后继续维护 | reply card send；send 成功后不持久化 final message id | 我们没有后续 patch final 的状态基础 |
| 首次 link 处理 | 先 neutralize 本地 link，保证安全落地 | 发送前同步 rewrite，失败则永久 fallback | 我们只有一次同步机会，没有 second chance |
| preview materialize 时机 | 后台异步 rewrite + patch 原卡 | 发送前同步 rewrite | Feidex 能把“稍后完成”的可点击内容补回原卡 |
| 大回复处理 | split 成多个 reply card | 网关层超限时截断 payload | Feidex 更少因为超限而丢内容/丢链接 |
| working card 复用 | quiet card 可 patch 成 final | shared progress card 目前不会被 final 复用 | Feidex 的 quiet path 更连贯 |
| 后台任务 | final preview patch + cleanup | 只有 cleanup | 我们当前没有 final 增强型后台链路 |
| reply 锚点稳定性 | reply 后只 patch 同一 message id | reply 失败会 fallback 成普通 create；supplement 也是 sibling append | 极端情况下我们更容易丢失统一 reply 记录 |

## 6. 可以直接借鉴的点

### 6.1 给 final card 增加 second-chance async patch

最值得直接借鉴的不是某个 markdown 正则，而是这条时序：

1. 先发可安全落地的 final card；
2. 记录 final card message id；
3. 后台继续做 preview materialize；
4. 成功后 patch 同一张卡。

这能显著降低“Drive/Web preview 一慢，最终卡就永远只能 fallback”的概率。

### 6.2 为 final card 建立 message-id 持久化/回查能力

如果要 patch 同一张 final card，至少需要：

- 发送成功后拿到 message id；
- 把它和 surface / thread / turn / final block 建立映射；
- 后台任务能回查这个 message id。

当前仓库已有 gateway 层 patch 能力，但缺 final message 自身的 message-id 生命周期管理。

### 6.3 用 split 取代“网关层超限截断”

Feidex 的 split 说明：

- final reply 体量一旦变大，真正稳妥的做法是应用层 chunking；
- 只靠网关层超限截断，会让后半段内容、链接甚至 footer 一起消失。

这点与是否采用异步 patch 无关，单独也值得做。

## 7. 必须按当前产品语义适配的点

### 7.1 不能直接照搬 Feidex 的 `.md only` preview 策略

Feidex 的 markdown preview 只处理本地 `.md`。

当前仓库已经有更广的 artifact/publisher 体系：

- drive inline link
- web preview inline link
- 多种 artifact kind
- supplement card 预留能力

因此如果引入异步 patch，应该复用现有 `FinalBlockPreviewRequest -> handler -> publisher` 框架，而不是退回成 Feidex 的 `.md only` 实现。

### 7.2 quiet working reuse 要和现有共享过程卡语义对齐

Feidex 的 quiet working card 是它自己的产品语义；当前仓库的过程卡更接近：

- shared exec progress card
- thread history card
- plan card
- 其他 selection / request / notice 卡

是否允许把“工作中”卡 patch 成 final card，需要重新评估：

- shared progress 与 final reply 是否应该共用同一 message id；
- 这会不会干扰我们当前的 verbose / normal 可见性策略；
- 这会不会让用户误把过程卡当成最终答复卡。

这点不应直接照搬。

### 7.3 若引入 final patch，需要同步审视 `update_multi`

当前仓库里会走 `OperationUpdateCard` 的卡通常会显式打开 `CardUpdateMulti`。

final `projectBlock(...)` 目前没有这层配置。如果后续要 patch final card，需要把：

- 发送包络
- message-id 持久化
- projector/gateway 的 update 路径

一起设计，而不是只加一个后台 goroutine。

## 8. 不建议把 Feidex 当成基线照搬的点

### 8.1 “只靠异步 patch” 不是更优基线

当前仓库的同步 preview rewrite 仍有价值：

- 快路径成功时，用户第一次看到 final card 就已经是可点击结果；
- 不必等待后台 patch；
- 也不必额外承担 patch 失败后的状态对账。

更合理的方向是：

- 保留当前同步快路径；
- 再叠加 second-chance async patch。

### 8.2 Feidex 不是裸 URL / issue shorthand 的实现基线

Feidex 当前也没有把这些能力做成基线：

- 裸 URL 通用 auto-linkify
- `#224` / `#227` 自动展开
- repo-aware reference expansion

因此后续若要做这类能力，应该单独以当前产品语义评估，而不是挂在“对齐 Feidex”名义下。

## 9. 当前最值得拆出的后续工作

建议把后续实现拆成至少三类：

1. `final card async patch plumbing`
   - 记录 final card message id
   - 为 final card 建立可回查的 turn 级映射
   - 后台 patch 同一 message id
2. `final reply split`
   - 把 final card 从“超限截断”升级成“应用层 chunk split”
3. `preview patch integration`
   - 复用现有 handler/publisher 体系
   - 明确 patch 时是否仅改正文，还是正文 + supplement 一起重排

## 10. 本轮审计后的稳定结论

可以把当前问题收敛成一句话：

- **Feidex 的优势不是某一个 link rewrite 细节，而是它把 final message 设计成“先 reply 一个可继续 patch 的实体，再用后台 preview materialize 去增强这条消息”。**

而当前仓库的关键短板也可以收敛成一句话：

- **我们已经有 preview materialize 能力，也已经有 `message.patch` 能力，但 final message 这条路径没有把两者接起来。**

这也是后续最值得实现的地方。

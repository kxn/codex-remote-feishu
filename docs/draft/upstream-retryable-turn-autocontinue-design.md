# 上游可重试失败 AutoContinue 设计（草稿）

> Type: `draft`
> Updated: `2026-04-27`
> Summary: 固化 turn 因上游推理异常中断后的 `AutoContinue` 方案，明确它与 `autowhip` 的拆分边界，并记录当前讨论对 `/stop`、Codex 内部 retry 与错误归因的判断；当前实现已经把触发 owner 收到 orchestrator 本地 codex/gateway error-family policy，不再直接依赖 upstream `problem.Retryable`。

## 0. 文档定位

这份文档只回答一件事：

- 当一个 turn 因上游推理侧的可重试问题中断时，系统是否应该自动继续，以及这件事应该怎样设计，才不会和现有 `autowhip`、`/stop`、Codex 自身 retry 语义混在一起。

它不是当前实现说明，也不是最终 PRD。

它的用途是把这轮讨论中已经澄清的事实、风险和取舍先固定下来，避免后面继续讨论时又回到“是不是直接改一下现有 `autowhip` backoff 就行”的局部 patch 思路。

## 1. 当前需求

当前希望增加一个新的 surface 级执行策略：

1. 默认关闭，显式开启后才生效。
2. 当 turn 因上游推理侧的可重试问题中断时，系统自动再发一次“继续”类输入。
3. 这次自动继续的语义，应接近“用户手动补发一条继续”，而不是“恢复原 turn id”。
4. 需要覆盖的失败面，优先是推理上游问题：
   - 上游明确报 `429`、`502`、`503` 等可重试错误；
   - 上游流自己断开，但 turn 最终仍以 retryable interruption 收尾，例如 `responseStreamDisconnected`。
5. 不希望误触发的场景：
   - Codex 自己在 turn 内做中途 retry，最终 turn 正常完成；
   - 用户主动 `/stop`；
   - 本地 relay / wrapper / daemon 自身的链路断开；
   - 普通 non-retryable failure。

## 2. 当前实现事实

### 2.1 `autowhip` 现在已经承接了两条不同语义

当前 surface 上的 `AutoWhipRuntimeRecord` 同时承接两条完全不同的触发链：

1. `incomplete_stop`
   - turn 正常结束，但最终文本不包含“老板不要再打我了，真的没有事情干了”这句停止口令；
   - 系统认为 Codex 可能“没把活干完”，于是补打一轮。
2. `retryable_failure`
   - turn 结束时挂着 `problem.Retryable=true`；
   - 系统认为上游不稳定，于是按 backoff 自动重试。

这条逻辑当前在这些位置：

- `internal/core/orchestrator/service_autocontinue.go`
- `internal/core/orchestrator/service_queue.go`
- `internal/core/state/types.go`

当前 backoff：

- `incomplete_stop`: `3s -> 10s -> 30s`
- `retryable_failure`: `10s -> 30s -> 90s -> 300s`

### 2.2 Codex turn 内部 retry 本身不会直接误触发当前机制

当前 translator 对 turn-bound `error` 的处理，不是收到错误就立刻判失败，而是：

1. 先把 `problem` 挂到 `pendingTurnProblems[turnID]`；
2. 等 `turn/completed` 到来时，再把该 `problem` 贴到终态 turn event 上；
3. 如果最终 `status == completed`，则这个挂起的 `problem` 会被丢掉，不会作为失败上浮。

这意味着：

- Codex 自己在 turn 内部做 retry，只要最后 turn 成功完成，当前不会触发自动恢复。

相关代码与测试：

- `internal/adapter/codex/translator_observe_server.go`
- `internal/adapter/codex/translator_internal_helper_test.go`

### 2.3 `/stop` 现在只有局部抑制，没有完整的“用户中断”终止原因

当前 `/stop` 会：

1. 向 Codex 发 `turn/interrupt`；
2. 清掉排队或暂存输入；
3. 对现有 `autowhip` 设置一次性的 `SuppressOnce`，避免本轮 turn 收尾时立刻再次自动续跑。

这意味着当前已有一层局部保护：

- 就算 turn 之后以 `interrupted` 结束，现有 `autowhip` 也会吞掉这一次 schedule。

但这层保护的局限很明显：

1. 它只是 `autowhip` 内部的局部 patch；
2. 它不是 turn 级终止原因分类；
3. 它没有在 active turn binding 上显式记录“用户主动请求过中断”。

相关代码与测试：

- `internal/core/orchestrator/service_surface_actions.go`
- `internal/core/orchestrator/service_autocontinue.go`
- `internal/core/orchestrator/service_config_prompt_test.go`

### 2.4 当前代码仍然存在“旧 retryable problem 粘到 stop 终态上”的设计风险

因为当前 `pendingTurnProblems` 是按 `turnID` 暂存，而 `/stop` 并不会：

- 清掉该 turn 上已有的 retryable problem；
- 或给该 turn 增加一个高优先级的 `user_interrupt_requested` 标记；

所以存在这样一种混淆路径：

1. turn 运行中先收到一次 retryable `problem`；
2. 用户后来点了 `/stop`；
3. 终态 `turn/completed(status=interrupted)` 到来时，旧的 retryable `problem` 仍被贴到这个终态上。

从代码上看，这类混淆现在没有被显式消解。

这也是为什么“手动 stop 后为什么还能看到 `stream disconnected before completion`”目前不能只靠读代码就精确判定来源：

1. 它可能真的是上游对 interrupt 的收尾语义；
2. 也可能是之前挂着的 retryable problem 在终态时被一起带上来了。

## 3. 当前实现为什么不适合直接补丁

### 3.1 语义已经混杂

`autowhip` 现在同时包含：

- “没干完继续催”
- “上游失败自动恢复”

这两条链的目标、提示文案、prompt 语义、backoff 节奏都不同。

如果继续在 `autowhip` 上补：

- 开关语义会越来越模糊；
- 状态字段会继续混杂；
- 用户也很难理解自己开的是“催活”还是“错误恢复”。

### 3.2 backoff 与 prompt 都不匹配当前需求

当前 retryable failure 的重试节奏过于保守：

- 第一轮要等 `10s`
- 第二轮还要等 `30s`

而用户这次明确希望的是：

1. 前两次尽量立即尝试；
2. 第三次之后再开始秒级 backoff；
3. prompt 更接近“继续”，而不是 `autowhip` 的专用补打文案。

### 3.3 `/stop` 保护现在只是 feature-local patch

当前的 `SuppressOnce` 只是在现有 `autowhip` 内部勉强避免“刚 stop 又自动继续”。

如果后面新功能直接复用 `problem.Retryable` 触发，而不补一层统一的终止原因分类，那么：

- 同样的问题还会换个名字再出现一次。

### 3.4 名字和字段也在误导实现

只要继续复用这些名字，后面的实现就很容易又滑回原来的混合语义：

- `AutoWhipRuntimeRecord`
- `AutoContinueReasonRetryableFailure`
- `RetryableFailureCount`
- `AutoWhip` notice 文案

这些命名都在暗示“错误恢复只是 autowhip 的一个分支”，这与本次需求的目标相反。

## 4. 这轮讨论的结论

### 4.1 `autowhip` 与“上游失败自动恢复”必须拆开

这是本轮最核心的结论。

`autowhip` 只保留“正常结束后的继续催活”语义。

上游失败自动恢复应成为独立能力，至少在实现层面独立：

- 独立开关
- 独立状态
- 独立 backoff
- 独立提示文案
- 独立终止原因分类

### 4.2 Codex 自己的中途 retry 不应触发新能力

这点当前实现其实已经基本满足：

- 只有 turn 终态失败时，才有资格进入自动恢复判断；
- turn 内部 retry 且最终成功，不应进入该路径。

后续新方案必须保留这个性质，不能退化成“看到 retryable error 就 schedule”。

### 4.3 `/stop` 必须升级成显式终止原因，而不是继续靠一次性 suppress

新方案不能仅依赖 `SuppressOnce`。

正确做法应是：

1. `/stop` 发出时，对当前 active turn 显式记账；
2. turn 收尾分类时，把“用户主动中断”作为高优先级终止原因；
3. 一旦命中 `user_interrupted`，即使终态上还带着 retryable problem，也不能触发自动恢复。

## 5. 设计方向

### 5.1 新能力：独立的“上游失败自动恢复”模式

建议把这次新能力定义成独立 surface 级开关，而不是 `autowhip` 的子选项。

工作方式：

1. 用户显式开启；
2. turn 终态被分类为 `autocontinue_eligible_failure` 时，系统按该模式自己的节奏 enqueue 一条恢复输入；
3. 恢复输入沿用原 queue item 的 route、thread、cwd、frozen override 与 reply anchor；
4. 只在 turn 真正结束后触发，不对 running turn 内的中途 error 直接动作。

用户可见层面，它可以挂在现有参数/配置区，但语义上不属于 model/provider 参数，而是 orchestrator 的执行恢复策略。

### 5.2 必须新增 turn 终止原因分类

建议引入一层明确的 turn terminal classification。

最少应区分：

1. `completed`
2. `user_interrupted`
3. `autocontinue_eligible_failure`
4. `nonretryable_failure`
5. `transport_lost`

建议的优先级：

1. 如果终态 `status == completed`，直接判 `completed`
2. 否则如果当前 turn 已记录 `interrupt_requested`，判 `user_interrupted`
3. 否则如果 `problem` 命中 orchestrator 本地 codex/gateway error-family policy，判 `autocontinue_eligible_failure`
4. 否则如果是 relay / wrapper / instance transport 断链，判 `transport_lost`
5. 其他都落 `nonretryable_failure`

这里的关键点不是枚举名，而是这层分类必须成为新的唯一判定入口，不能继续直接用 `problem.Retryable` 驱动产品行为。

### 5.3 `/stop` 要把“中断意图”记到 turn binding 上

建议在 active remote turn 运行时记录中增加至少这些信息：

- `InterruptRequested bool`
- `InterruptRequestedAt time.Time`

记录位置应该靠近 remote turn binding，而不是只放在 surface feature overlay 上。

原因：

1. 它是 turn 终止原因判断的输入；
2. 它不只服务 `autowhip`；
3. 它应该跟 turn 生命周期一起清理，而不是跟某个 feature 的 runtime overlay 耦合。

### 5.4 新能力的 backoff 应比现有 `retryable_failure` 更激进

建议 v1 使用更接近人工使用习惯的节奏：

1. 第 1 次：立即
2. 第 2 次：立即
3. 第 3 次：`2s`
4. 第 4 次：`5s`
5. 第 5 次：`10s`
6. 之后停止

这里的“立即”不要求在当前 call stack 里递归重入；只要通过现有 tick/queue 机制在下一拍发出即可，用户感知已经足够接近“立即”。

但这套计数不是一旦增加就永久累加。

本轮讨论后补充一条明确产品规则：

1. 只有“连续无输出即失败”的恢复 attempt，才继续累加 attempt 计数与 backoff。
2. 只要某次恢复 attempt 已经产生了任何输出，就说明这轮恢复已经真正重新跑起来。
3. 一旦真正跑起来，后续如果再次中断：
   - attempt 计数归零
   - backoff 归零
   - 下一次恢复重新从“立即重试”开始

这样做的产品含义是：

- 我们只惩罚“连续秒死”的失败；
- 不惩罚“已经恢复并继续输出过，后来又再次中断”的情况。

### 5.5 新能力的恢复 prompt 不能复用 `autowhip` 文案

`autowhip` 的专用 prompt 是“催活”语义，不适合错误恢复。

新能力应使用中性恢复 prompt，例如：

> 上一次响应因上游推理中断，请从中断处继续完成当前任务；如果其实已经完成，请直接说明结果。

最终文案可以再单独定，但原则已经明确：

- 不复用 `autowhip` 停止口令体系；
- 不把“老板不要再打我了”这一套混进错误恢复。

### 5.6 v1 范围建议

v1 只处理 turn 级、Codex 上游可重试失败，不扩大到所有错误面。

v1 建议纳入：

1. 终态 turn 挂着 `problem.Retryable=true`；
2. 已知上游断流类终态，例如 `responseStreamDisconnected`；
3. 显式的上游 `429` / `502` / `503` 这类 retryable 问题，只要最终作为 turn 失败收尾。

v1 暂不纳入：

1. relay transport degraded
2. wrapper / daemon 自身写管道失败
3. 实例 disconnect / detach
4. dispatch rejected

原因很简单：

- 这些错误面虽然也可能“能恢复”，但它们不属于“上游推理失败自动恢复”的单一问题域；
- 现在先把 turn 级上游失败做干净，比把所有错误都往一个恢复桶里塞更稳。

### 5.7 回复关系必须从“触发消息”升级成“根回复锚点”

这轮讨论里暴露出的一个重要产品问题是：

- 之前比较粗的规则是“一个 turn 自动发出来的消息，reply 到触发这个 turn 的那条消息”；
- 但对自动恢复 turn 来说，真正触发下一次 turn 的，往往是系统自己发出的恢复提示卡；
- 如果继续按这条粗规则执行，后续消息就会开始 reply 到系统卡，而不是 reply 到最初的用户请求。

这不符合产品语义。

自动恢复 turn 的正确理解应是：

- 系统在继续回答原用户请求；
- 系统提示卡只是状态说明，不是新的业务父消息。

因此这里必须引入更硬的概念：

1. `Root Reply Anchor`
   - 本轮工作真正对应的根用户消息锚点；
   - 通常是最初把这个 turn 跑起来的那条用户消息。
2. `Trigger Message`
   - 直接导致本次动作发生的消息；
   - 对自动恢复来说，它可能是系统提示卡，也可能根本不是用户消息。
3. `Owner Card Message`
   - 某张卡后续是否 patch 自己所依赖的 message id；
   - 它只决定“谁 patch 谁”，不决定“后续结果 reply 给谁”。

这里的产品规则应改成：

- turn 输出 reply 到自己的 `Root Reply Anchor`，而不是机械地 reply 到“最近一次触发消息”。

本轮讨论后，这条规则已经不再是开放问题，而是明确产品约束：

- 自动恢复提示卡可以出现；
- 但它不会成为后续业务输出的新父消息；
- 后续所有输出仍继续挂回原始用户消息。

### 5.8 自动恢复链路的 reply contract

建议把自动恢复链路的 reply 关系固定成下面这套 contract。

示例：

1. 用户消息 `U0`
2. 第一次 turn `T0`
3. `T0` 因上游失败中断
4. 系统发自动恢复提示卡 `N1`
5. 自动恢复 turn `T1`
6. `T1` 过程中如果再弹 approval / MCP / 用户补充问题卡，记为 `R1`
7. `T1` 最终答复，记为 `F1`

建议的关系是：

1. `U0` 是整个恢复链路的 `Root Reply Anchor`
2. `T0` 的错误提示如果要保留为独立消息，也 reply 到 `U0`
3. 自动恢复提示卡 `N1` reply 到 `U0`
4. `T1` 的中间输出 reply 到 `U0`
5. `T1` 的 approval / MCP / request_user_input 卡 reply 到 `U0`
6. `T1` 的最终答复 `F1` reply 到 `U0`
7. 如果 `N1` 后续需要更新 attempt 次数或 terminal 状态，走 owner-card patch，不改变 `Root Reply Anchor`

换句话说：

- 因果链可以是 `U0 -> T0 -> N1 -> T1`
- reply 链必须保持为“`N1`、`R1`、`F1` 都作为 `U0` 的同级回复挂回原请求”

更准确地写就是：

- `N1` 是 reply 给 `U0` 的一条状态消息；
- `T1` 并不是在“回复 `N1`”，它仍然是在“回复 `U0`”。

因此自动恢复实现里必须遵守：

1. 恢复提示卡的 `message_id` 不能反写成下一次恢复 turn 的 `ReplyToMessageID`
2. 恢复 turn 的 queue item 必须继承原 `Root Reply Anchor`
3. 恢复提示卡如果需要 patch 自己，单独保存自己的 owner-card `message_id`

同时补充一条与 queue 相关的明确规则：

4. 自动恢复优先级高于用户新消息和已排队用户消息
5. 当某次上游可重试失败准备触发自动恢复时：
   - 用户后续新消息不抢先派发；
   - 已在队列中的用户消息也不抢先派发；
   - 自动恢复先尝试继续把前一轮未完成的任务跑完

这里的产品判断已经明确：

- 自动恢复的语义是“前头的事还没做完，先把它续上”；
- 因此它应高于普通后续消息派发。

### 5.9 自动恢复提示卡的产品策略

当前如果直接复用现状，很容易出现这样的用户可见链路：

1. 先来一张红色 `turn_failed`
2. 紧接着又来一张“正在自动恢复”的提示卡
3. 然后又出现新的最终答复

这会让用户感知非常别扭：

- 一边像是在说“这轮已经失败”
- 一边又像是在说“其实没有失败，我已经继续了”

因此从产品角度，自动恢复开启且实际 schedule 成功时，更合理的做法是：

1. 不再把这次失败直接呈现成一个“终局红卡”
2. 而是改成一张“恢复状态卡”
3. 这张卡本身只负责表达恢复状态，不承担额外解释性长文案
4. 如果后续自动恢复成功，历史上保留这张状态卡即可，不需要再围绕它生成新的 reply 链
5. 如果后续 attempt 全部耗尽，应补一条更明确的失败答复，不能只留下状态卡悬在那里

这里建议优先采用：

- “单次恢复 episode 默认只 append 一张状态卡，后续 attempt 状态优先 patch 同一张卡”

这样可以避免：

- 每次重试都再 append 一张新提示卡
- 在同一个 root anchor 下刷出过多系统噪音

但这里有一个新增的边界条件，本轮讨论已经明确拍板：

1. 只有当恢复状态卡仍然位于当前消息尾部、没有被后续消息打断时，才允许继续 patch 它。
2. 一旦恢复状态卡后面出现了任何新消息，这张卡就视为进入历史区，不再回头 patch。
3. 如果后续恢复流程仍然需要继续向用户表达状态，则新开一张恢复状态卡。

这条规则的产品目的，是避免“回头修改历史卡片”导致时间线倒流。

### 5.10 其他产品冲突与建议

除了 reply 关系，这里还要额外注意几类容易漏掉的产品冲突。

#### 5.10.1 与现有 pending / typing / reaction 语义的冲突

当前 `autowhip` 的一个重要特征是：

- 它不伪造新的用户消息；
- `SourceMessageID` 为空；
- 因此不会对原用户消息补 `THINKING`、queue pending、`ThumbsUp` / `ThumbsDown`。

如果自动恢复也沿用这条策略，那么：

- 用户主要依赖“自动恢复提示卡”感知系统仍在继续；
- 而不是依赖原用户消息上的 reaction。

这本身是可以接受的，但它是一个明确的产品取舍，不能视为 incidental behavior。

同时，本轮讨论后还新增了一条 queue 优先级规则：

- 自动恢复派发时，不应让普通 pending 用户消息先插队执行；
- 但队列本身仍要保留，让用户知道消息没有丢，只是当前有更高优先级的恢复流程在前面。

#### 5.10.2 与 approval / MCP / 用户询问问题卡的冲突

如果自动恢复 turn 在中途又弹出 request card：

- 这些卡从业务语义上仍然是在继续处理 `U0` 对应的原任务；
- 因此也应 reply 到 `Root Reply Anchor`；
- 不能 reply 到自动恢复提示卡。

否则用户看到的会是：

- 系统卡下面再挂系统卡；
- 最后真正的业务结果离原请求越来越远。

#### 5.10.3 与最终答复标题预览的冲突

当前 final card 标题预览来自 `sourceMessagePreview`。

如果自动恢复 turn 把恢复提示卡错误地当成新的 source：

- 最终答复标题就可能变成“✅ 最后答复：正在自动恢复...”之类的错误上下文。

这也是为什么必须显式保留原 `Root Reply Anchor` 及其 preview。

#### 5.10.4 与用户显式新消息的冲突

自动恢复只应该延续“同一个原请求”，但本轮产品拍板后，这里的优先级已经明确：

1. 用户在等待自动恢复期间发了新的显式消息，这条消息进入队列。
2. 它不会抢在自动恢复前执行。
3. 自动恢复应先尝试把前一轮未完成任务续上。

因此这里不再采用“用户新消息一出现就取消 autoContinue”的方案。

#### 5.10.5 与 `/stop` 的冲突

如果用户对自动恢复出来的 turn 再次 `/stop`：

- 这次 `/stop` 应针对当前 running attempt 生效；
- 并让自动恢复 episode 进入明确的“用户手动终止”状态；
- 不能在本轮收尾后再次自动续跑。

这意味着：

- “恢复 episode”与“单个 attempt turn”虽然相关，但不能混成一个状态概念。

但恢复状态卡本身不需要额外再放一个“停止自动恢复”按钮。

本轮拍板结果是：

- 继续沿用现有 `/stop` 作为人工终止入口；
- 恢复状态卡只负责状态展示，不再承载额外中断控件。

#### 5.10.6 与观察性/追溯的冲突

如果只保留一个 reply anchor，而不额外记录“是谁触发了这次 attempt”，后面会有两个问题：

1. 用户可见线程是对的；
2. 但开发者视角无法看出这次 turn 是人工继续、自动恢复，还是别的系统动作触发的。

所以实现上最好同时保留：

1. `Root Reply Anchor`
2. `Attempt Trigger Kind`
3. `AutoContinue Notice Message ID`

前者服务产品展示，后两者服务追溯和调试。

## 6. 与 `autowhip` 的关系

这次讨论后，对 `autowhip` 的方向建议如下。

### 6.1 认同：`autowhip` 不应再处理 Codex 自身错误

对下面这个想法，本文结论是：**同意**。

> 这版方案实施后，如果效果不行，就把自动编打功能中处理 Codex 自身错误的那一条链路彻底摘掉。

更激进一点说：

- 不是“如果效果不行再摘”，而是新能力一旦接管错误恢复语义，`autowhip` 中的 `retryable_failure` 路径就不应长期保留。

原因：

1. 两套功能同时吃同一类失败，会制造新的耦合和歧义；
2. 保留双路径只会让后续排错更难；
3. 这种“新旧都留着兜底”的过渡策略，本仓库历史上已经多次证明容易留下半死不活的 legacy 行为。

### 6.2 认同：`autowhip` 只处理正常结束 turn

对下面这个想法，本文结论也是：**同意**。

> 自动编打功能只处理正常结束的 turn 情况。

更精确的说法应是：

- `autowhip` 只在 `turn terminal cause == completed` 时参与评估；
- `interrupted`、`failed`、`transport_lost` 都不再进入 `autowhip` 判断。

这样分完之后：

- `autowhip` 只剩“正常结束后要不要再催一轮”的语义；
- 错误恢复则完全由新能力负责。

### 6.3 认同：原来的错误 turn 处理链路不应保留产品语义

对下面这个想法，本文结论是：**同意，但建议复用基础设施，不复用旧产品语义**。

> 原来的处理错误 turn 的功能就不要了，相关代码可以选择删除或改造后复用。

建议的处理方式：

1. 可以复用的部分：
   - `turn.completed -> schedule pending runtime -> tick dispatch -> enqueue special queue item`
   - reply anchor 复用
   - frozen route / override 复用
   - owner-card patch 基座可复用，但不能把 owner-card `message_id` 混成 reply anchor
2. 应删除或重命名的部分：
   - `AutoContinueReasonRetryableFailure`
   - `RetryableFailureCount`
   - `AutoWhip` 错误提示文案
   - 任何把“错误恢复”解释成 `autowhip` 子能力的命名

换句话说：

- 可以复用调度骨架；
- 不应复用旧 feature 语义和命名。

## 7. 建议的数据与状态迁移方向

为了避免继续在 `AutoWhipRuntimeRecord` 上打补丁，建议最终形态至少做到：

1. `AutoWhipRuntimeRecord`
   - 只保留 `autowhip` 自己需要的运行时状态；
   - 去掉 `retryable_failure` 相关字段与计数。
2. 新增独立的 autoContinue runtime record
   - 承接“上游失败自动继续”的 enable、pending、attempt、root reply anchor、owner notice card 等状态。
3. active remote turn binding
   - 新增 `interrupt_requested` 类字段，用于 turn 终止原因分类。
4. autoContinue episode 运行时
   - 建议显式区分 `Root Reply Anchor` 与 `AutoContinue Notice Message ID`，避免把提示卡错当成后续业务消息的 reply 父节点。

如果这一步不做，只把现有 `AutoWhipRuntimeRecord` 再加几个字段，后面大概率还会再次回到“到底这算 autowhip 还是算错误恢复”的混乱状态。

## 8. 观察性要求

这类功能如果没有观察字段，很难长期维护。

建议在落地时至少补这些日志/调试面：

1. `/stop` 发出时：
   - surface / instance / thread / turn
   - 是否成功命中 active remote turn
2. turn 终态分类时：
   - terminal cause
   - status
   - problem.code
   - problem.retryable
   - interrupt_requested
3. 恢复调度时：
   - attempt count
   - due at
   - chosen prompt kind
   - root reply anchor
   - autoContinue notice message id
4. 明确记录“为什么没有恢复”：
   - completed
   - user interrupted
   - non-retryable
   - gate blocked
   - max attempts exhausted

没有这些字段，后面再碰到“为什么 stop 后它又继续了”或者“为什么明明是 upstream 断流却没恢复”，排查成本会非常高。

## 9. 当前建议的执行顺序

如果后续要进入实现，建议顺序如下：

1. 先补 turn terminal cause classification
2. 再补 `/stop` 的 explicit interrupt marker
3. 再把上游失败自动恢复从 `autowhip` 中拆出来
4. 最后再清理 `autowhip` 中的 `retryable_failure` 旧链路

不要反过来先改 backoff 或 prompt。

如果分类层没先建立，再漂亮的 backoff 都只是更激进的 patch。

## 10. 第二轮收口结论

第二轮继续往下梳理后，这个问题已经不再是“还缺少事实”，而是“要不要把收口边界写清楚”。

当前已经可以确认：`#418` 进入执行时，不应再把焦点放在 backoff、notice 文案或单个 suppress 标志上，而应先落下面五个结构。

### 10.1 先有原始 outcome，再有产品 terminal cause

当前 translator 往 orchestrator 上送的 failure，并不都是同一种 turn 终态：

1. 真正的 `turn/completed`
2. `turn/start` 失败后被折叠成 `EventTurnCompleted(status=failed)`
3. `thread/resume` / `thread/start` follow-up 失败后被折叠成 `EventTurnCompleted(status=failed)`
4. turn 中途先挂了 retryable problem，之后用户 `/stop`，最终又以 `interrupted` 收尾

如果后续仍然让各处直接读：

- `status`
- `errorMessage`
- `problem.Retryable`

就会继续把“真正跑起来后中断”和“根本没成功进入 turn”混成一类。

因此建议新增一层原始 carrier，例如 `RemoteTurnOutcome`，至少明确：

- `FailureOrigin`
- `StartAccepted`
- `StartedTurnID`
- `Problem`
- `InterruptRequested`
- `AnyOutputSeen`

然后再从这层统一映射到产品 `TerminalCause`：

- `completed`
- `user_interrupted`
- `autocontinue_eligible_failure`
- `startup_failed`
- `nonretryable_failure`
- `transport_lost`

这里的关键不是枚举名，而是：

- `problem.Retryable` 不再直接驱动产品动作
- `EventTurnCompleted` 也不再被当成唯一真实终态来源

### 10.2 需要显式消息车道契约，不能继续靠 projector 猜

第二轮确认后，当前系统真正缺的不是 reply anchor 本身，而是“这条消息该走哪条车道”的显式 contract。

现状是：

1. orchestrator 已经把 `SourceMessageID` / `replyAnchorForTurn(...)` 当成稳定事实往下传
2. 但 projector 对很多 payload family 仍直接按当前默认行为发送：
   - `RequestPayload` 顶层卡
   - `PlanUpdatePayload` 顶层卡
   - `ExecCommandProgressPayload` 顶层卡
   - `ImageOutputPayload` 顶层图片
3. daemon 还要再用 `attention` 去补偿“这张卡虽然没 reply，但需要把用户叫回来”

这说明当前是：

- 上游知道锚点是谁
- 下游不知道这条消息是否应该消费这个锚点

因此建议新增显式消息车道契约，至少拆成两维：

1. 首次发送车道
   - `reply_thread`
   - `top_level`
   - `inline_replace`
2. 后续变更策略
   - `append_only`
   - `patch_same_message`
   - `patch_same_message_tail_only`

`DeliverySemantics` 继续负责可见性 / handoff 即可，不再让它兼管消息车道。

这也给 `#418` 提供了一个不再需要额外产品拍板的收口点：

1. 自动恢复链路内被本单改到的 turn-owned 业务输出，显式走 `reply_thread`
2. 恢复状态卡显式走 `reply_thread + patch_same_message_tail_only`
3. 现有未在本单范围内变更的其它 card family，可以先显式映射到当前行为，不再靠 projector 猜

也就是说：

- 这单先消灭“靠猜”
- 不要求同一单里把全仓库所有既有 top-level 卡全部翻成 reply

### 10.3 `tail-only patch gate` 需要 surface message ledger

当前 owner-card runtime 只能回答：

- 这张卡的 `message_id` 是多少
- 当前 revision / phase 是多少

它回答不了：

- 这张卡后面是否已经 append 过别的消息
- 它现在是不是时间线尾部最后一张

而本轮拍板的产品规则是：

- 恢复状态卡只有仍位于尾部时才允许继续 patch
- 一旦后面出现任何新消息，就冻结，不再回头改历史卡

所以这里不能继续复用现有 owner-card flow 当作 tail gate。

建议新增 surface 级 append ledger，至少记录：

- `MessageID`
- `SurfaceSessionID`
- `AppendSeq`
- `Kind`
- `ReplyToMessageID`

并补一个 surface 级 `LastAppendSeq`。

判断能否继续 patch：

- `card.AppendSeq == surface.LastAppendSeq`

这样 patch 资格就变成显式状态，而不是“技术上能 patch，所以就继续 patch”。

### 10.4 自动继续应建独立 `PendingAutoContinueEpisode`

继续沿用普通 queue item 的问题已经很明确：

1. FIFO 天然不表达“恢复优先于普通消息”
2. `completeRemoteTurn(...)` 当前顺序先失败、再 dispatch、最后才 `maybeScheduleAutoContinue...`
3. 继续借 `QueueItemSourceAutoWhip` 伪装，会让恢复逻辑长期被普通队列语义绑架

因此建议显式新增 `PendingRecoveryEpisode`，挂在 surface 级 runtime，下游 dispatch 顺序改成：

1. 先看 autoContinue lane
2. 再看普通 queue

建议 episode 至少承载：

- `EpisodeID`
- `RootReplyAnchorMessageID`
- `RootReplyAnchorPreview`
- `TriggerKind`
- `NoticeMessageID`
- `NoticeAppendSeq`
- `AttemptCount`
- `ConsecutiveDryFailureCount`
- `CurrentAttemptOutputSeen`
- `PendingDueAt`
- `LastProblem`
- `State`

这样：

- 恢复优先级
- 恢复状态展示
- 恢复 backoff
- `/stop` 对 autoContinue 的终止

才不需要继续借 `autowhip` 或普通 queue 做兼容式表达。

### 10.5 失败收口必须拆成 derive / finalize 两段

当前最大的耦合点仍是 `completeRemoteTurn(...)`。

它现在同时做：

- turn 结束解释
- queue item success/failure
- 失败 notice
- `dispatchNext(...)`
- `finishSurfaceAfterWork(...)`
- auto-continue 调度

这就是“先失败、再恢复”语义冲突持续存在的根源。

建议拆成两段：

1. `deriveRemoteTurnOutcome(...)`
   - 只负责把原始 outcome 算清楚
2. `finalizeRemoteTurnOutcome(...)`
   - 再根据 `TerminalCause` 决定：
     - completed
     - enter autoContinue
     - final failure
     - user interrupted

只有明确不进入 autoContinue 时，才发最终失败答复。

### 10.6 当前已达到可开工状态

按本轮评估标准，`#418` 现在已经达到 execution closure。

原因不是“实现已经想清楚到每一行”，而是关键结构判断已经拍实：

1. turn 终态需要 `RemoteTurnOutcome -> TerminalCause` 双层收口
2. 消息车道需要独立 contract，不能继续混在 `DeliverySemantics` 或 projector heuristics 里
3. 恢复卡 tail gate 需要 surface ledger，而不是复用 owner-card 现状
4. 自动恢复需要独立 `PendingRecoveryEpisode`
5. `autowhip.retryable_failure` 不再是可长期保留的产品路径

这五件事一旦固定，后续实现就不再需要重建大范围背景，也不需要再做产品级二次拍板。

### 10.7 推荐实施顺序

进入实现后，建议按下面顺序推进：

1. `RemoteTurnOutcome` + `TerminalCause`
2. `remoteTurnBinding` 补 `InterruptRequested` / `AnyOutputSeen`
3. 显式消息车道契约
4. surface message ledger
5. `PendingRecoveryEpisode` + dispatch 优先级
6. 恢复状态卡 / 最终失败答复 / backoff 接线
7. 删除 `autowhip.retryable_failure` 旧产品语义

如果顺序反过来，最终大概率只会得到一版“更聪明的旧 patch”。

## 11. 当前 issue 的行为变更边界

为了让后续实现不再在过程中重新猜边界，这里补一条明确约束。

本 issue 进入实现后，允许且预期出现的现有行为变化包括：

1. 上游可重试失败不再直接走旧 `autowhip.retryable_failure` notice / 计数 / backoff
2. 自动继续开启且实际进入 autoContinue 时，不再先外发一张终局红色失败卡，再补提示卡
3. autoContinue 状态卡会成为单次 episode 的唯一状态卡，并受 tail-only patch gate 约束
4. autoContinue 链路里被本单接管的 turn-owned 业务输出，会显式消费 `Root Reply Anchor`，不再让 autoContinue 提示卡 message id 抢走父锚点

本 issue 当前不要求同步完成的事情：

1. 把全仓库所有既有 top-level business 卡统一翻成 reply lane
2. 把 relay / wrapper / daemon / instance 侧所有可恢复错误面并入 autoContinue
3. 在同一轮里顺手重构所有 attention 相关策略

也就是说：

- 这单会把 autoContinue 相关结构收干净
- 但不会借题发挥，把所有历史车道产品语义一次性重写

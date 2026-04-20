# Feishu Text Pipeline Governance

> Type: `draft`
> Updated: `2026-04-20`
> Summary: 从整个 Feishu 文本链路出发，梳理文本的生产点、格式化点、承接点与当前多重 markdown 解释问题，并提出统一的治理边界与分阶段收敛方案。

## 1. 文档目的

这份文档不只处理某一个 final link 渲染 bug。

它要回答的是更大的问题：

- 当前仓库里，文本到底会在哪些地方被生产出来
- 哪些地方会对文本再次做格式化或“局部改写”
- 最终这些文本会落到飞书的什么字段
- 哪些链路仍在把 `string` 同时当作“原始文本 / markdown / Feishu markdown / 中间格式”混用
- 后续如果要统一治理，应该收敛到什么边界

本文件是以下几份局部审计的上层总收口：

- [feishu-card-render-format-audit.md](./feishu-card-render-format-audit.md)
- [feishu-card-render-risk-map.md](./feishu-card-render-risk-map.md)
- [feishu-system-markdown-boundary-design.md](./feishu-system-markdown-boundary-design.md)

对应跟踪 issue：

- [#313 治理 Feishu 文本生产与格式化链路，统一 markdown/plain_text 边界](https://github.com/kxn/codex-remote-feishu/issues/313)

## 2. 当前总链路

以 Feishu 出站文本为例，当前主链路大致是：

1. 上游或本地逻辑生成文本
2. `daemon` 把它包装成 `control.UIEvent`
3. `projector` 按事件类型决定：
   - 直接发文本
   - 生成卡片 body markdown
   - 生成卡片 elements
4. `card_renderer` 把 `CardBody` 或各类 element 转成 Feishu card JSON
5. `gateway_runtime` 把 payload 发给飞书

final reply 额外多了一层：

1. `app_ui.go` 先调用 `RewriteFinalBlock(...)`
2. `projector.projectBlock(...)` 再调用 `projectFinalReplyCards(...)`
3. `final_card_markdown.go` 再做 `normalizeFinalCardMarkdown(...)`
4. 最终 `CardBody` 仍作为 `tag=markdown` 下发

关键文件：

- [internal/app/daemon/app_ui.go](../../internal/app/daemon/app_ui.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [internal/adapter/feishu/markdown_preview_rewrite.go](../../internal/adapter/feishu/markdown_preview_rewrite.go)
- [internal/adapter/feishu/final_card_markdown.go](../../internal/adapter/feishu/final_card_markdown.go)
- [internal/adapter/feishu/card_renderer.go](../../internal/adapter/feishu/card_renderer.go)

## 3. 文本生产点分类

### 3.1 上游原样文本

这类文本不是 adapter 自己拼出来的，而是来自上游事件或 runtime：

- assistant 非 final 普通文本
- assistant final 文本
- 历史记录里的输入/输出
- 最近消息 / 最近回复 / thread preview
- request/question/option/default value
- runtime error / debug text / stderr / repo URL / path / file name

特点：

- 内容最不可控
- 可能天然就带 markdown 结构
- 不能假设它只是“普通一句话”

### 3.2 adapter-owned 固定系统 copy

这类文本主要由本地系统自己拥有：

- 固定标题
- 固定提示语
- 固定按钮标签
- 固定状态说明

特点：

- 语义和结构都在本地
- 如果不混入动态值，可以保留少量 markdown

### 3.3 上游结构 + 本地拼接文本

这是当前最常见、也最容易出问题的类型：

- 上游给一堆字段
- projector / helper 在本地用 `fmt.Sprintf`、`Join`、字符串拼接把它们拼成 markdown
- 最后整体进入 `CardBody` 或 `markdown` element

当前大量存在于：

- snapshot
- request prompt
- thread history
- selection / target / path picker
- shared progress
- final footer / file summary

## 4. 当前承接位置

当前文本最终大致落在四类承接位置：

### A. Feishu `plain_text`

典型位置：

- 按钮文字
- option label
- input label / placeholder
- `div.text.tag=plain_text`

这是当前最稳定的承接位置。

### B. Feishu `markdown` element

典型位置：

- `CardBody`
- 独立 markdown 行
- 标题/分组辅助 markdown

这是当前最危险的承接位置，因为它要求传入的字符串已经是“最终可被 Feishu 正确解释的 markdown”。

### C. Feishu markdown 中的 `<text_tag>` 等平台标签

典型 helper：

- [internal/adapter/feishu/projector_inline_tags.go](../../internal/adapter/feishu/projector_inline_tags.go)

它本质上不是纯 markdown，而是“Feishu markdown 方言”。

### D. 纯文本消息

非 final assistant block 当前直接走文本消息，不进卡片 markdown 语境。

这是最安全的一条出站路径。

## 5. 当前主要模式

### 模式 1：直接文本发送

代表：

- 非 final assistant block

特点：

- 不进入 card markdown
- 不会发生 markdown 再解释

### 模式 2：structured section -> `plain_text` / 少量 markdown

代表：

- `FeishuCardTextSection`
- `appendCardTextSections(...)`

特点：

- label 用 markdown
- 动态值主体进 plain_text
- 是当前最接近正确方向的系统卡片路径

### 模式 3：adapter-owned markdown body

代表：

- snapshot
- notice body
- plan body
- request body

特点：

- `CardBody` 最终会再包进 `tag=markdown`
- 调用点需要自己保证字符串语义正确

### 模式 4：逐行 markdown element

代表：

- shared progress

特点：

- 比“整段大 body”更安全
- 单行出问题不会污染整张卡后续所有行
- 但每一行依然还是 markdown 字符串

### 模式 5：final markdown 特殊管线

代表：

- `RewriteFinalBlock(...)`
- `normalizeFinalCardMarkdown(...)`

特点：

- 是仓库里唯一明确承认“输入本身就是 markdown”的专用链路
- 但当前仍然是多次解析、多次字符串重写

### 5.1 从实际调用链看，模式 2 / 3 / 4 不应继续被视为三条业务路径

如果只看“当前代码里出现过哪些表现形态”，模式 2 / 3 / 4 可以分开讲。

但如果从“后续业务应该选哪条固定路径”来看，这三种里至少有两种并不值得继续保留为业务可感知的独立选择。

核心原因：

- 它们最终都落在同一个 `cardDocument` / `rawCardDocument(...)` 容器里
- 区别主要只是 adapter 在卡片内部用了：
  - `plain_text` block
  - 小块 markdown element
  - 或整段 `CardBody`
- 这更像是同一条 `system card lane` 内部的渲染粒度差异，不是三条平级的产品路径

代码证据：

- `appendCardTextSections(...)` 已经把大量 system card 文本收敛成：
  - label 用少量 adapter-owned markdown
  - 动态值主体进 `plain_text`
- `projectExecCommandProgress(...)` 虽然会写 `Operation.CardBody`，但真正发送时使用的是：
  - `rawCardDocument("工作中", "", cardThemeProgress, elements)`
  - 也就是说它的真实出站路径是“卡片 elements”，不是“整段 body markdown”
- `commandCatalogElements(...)`、`targetPickerElements(...)`、`pathPickerElements(...)`、`threadHistoryElements(...)` 等新一些的实现，已经不再依赖整段 raw markdown body，而是直接在同一个 card lane 里组合：
  - section
  - plain_text block
  - select / form / button
  - 少量标题 markdown

因此，更准确的结论不是“系统卡片有 3 条长期并行路径”，而是：

- 目前存在 3 种 system-card 表现变体
- 但目标上它们应收敛为 1 条 system-card 业务路径
- 其中 `raw markdown body` 与“逐行 markdown element 拼动态文本”都不应继续暴露给业务层自行选择
- 一旦正式进入实施，不应采用“先并存、后慢慢清退”的方案，而应一次性把业务入口收口到统一路径上

### 5.2 当前更接近真实情况的路径划分

如果按“真实业务应该如何选路”来重排，当前仓库的文本路径更适合收敛成下面 3 条：

1. `direct text lane`
   - 适用于普通 assistant 非 final 文本
   - 直接发送文本消息，不进入卡片 markdown 语境
2. `system structured card lane`
   - 适用于所有 system / notice / picker / request / progress / catalog / snapshot / history 一类卡片
   - 默认使用：
     - `plain_text`
     - `FeishuCardTextSection`
     - block / select / form / button
     - 少量 adapter-owned markdown 标题或状态标签
3. `final markdown lane`
   - 只服务 final answer
   - 需要单独的 markdown 输入 contract 与单次 serializer

换句话说，当前文档里列出的模式 2 / 3 / 4 更适合被理解成：

- 同一条 `system structured card lane` 的不同成熟度阶段

而不是让后续业务继续在三者之间自由选择。

## 6. 当前结构性问题

### 6.1 同一段文本会被多次重新当 markdown 解释

最典型的是 final：

1. 上游给出 markdown 字符串
2. preview rewrite 再按自己的规则扫描一遍
3. projector 再把它交给 final normalize
4. Feishu 客户端再解释一次

问题不在于“解释了三次”本身，而在于：

- 每次解释支持的语法子集不一样
- 每次解释对 code span / link / path / fenced code 的优先级也不一样

### 6.2 `string` 同时扮演了多种角色

当前一个裸 `string` 可能表示：

- 原始纯文本
- 上游 markdown
- 已带 Feishu `<text_tag>` 的 markdown
- 局部 helper 处理后的“半成品”
- 最终可以安全下发给飞书的 markdown

这些角色在类型上没有区别，只能靠调用方心里记。

### 6.3 系统卡片与 final answer 边界还不够硬

文档上已经承认 final reply 是特例，但代码上仍然存在：

- notice body 继续走 raw markdown
- snapshot 继续走 raw markdown
- request / picker 的很多局部仍在直接拼 markdown

这说明“系统卡片默认 structured/plain_text，final 单独处理”的方向还没有完全落地。

### 6.4 部分 helper 被误当成了通用 sanitizer

例如：

- `renderSystemInlineTags(...)`
- `formatNeutralTextTag(...)`
- `formatInlineCodeTextTag(...)`

它们能做的是局部格式增强，不是完整的文本语义治理。

### 6.5 同一仓库里同时存在不同收敛级别

当前已经有三种成熟度并存：

- 比较好的：`FeishuCardTextSection`
- 过渡中的：shared progress 行级 markdown
- 风险高的：final 之外仍然直接拼 raw markdown body 的分支

这会让后续实现很难保持一致风格。

## 7. 统一治理原则

### 原则 1：先区分文本 lane，再谈格式化

后续所有出站文本应先落到明确的 lane：

- `plain_text lane`
- `structured card lane`
- `final markdown lane`

不要再默认从“怎么拼 markdown 更方便”出发。

### 原则 2：系统卡片默认不接受动态值直进 raw markdown

只要文本里混入：

- 用户输入
- assistant 历史回复
- 路径 / URL / 文件名 / 错误详情
- request/question/option/default value

默认就不应该直接进入 system card 的 raw markdown body。

### 原则 3：final markdown 必须变成单一语义管线

final 不是不能保留 markdown，而是不能继续靠多轮字符串重写来维持。

目标应是：

- 输入：上游 final markdown
- 中间：统一中间表示
- 变换：preview materialize / unsupported target neutralize / split
- 输出：一次序列化成 Feishu markdown

而不是 preview rewrite 和 final normalize 各自做一套“半解析”。

### 原则 4：adapter 负责最终 markdown/plain_text 切分

orchestrator / control / daemon 负责业务结构，不负责拼 Feishu markdown 结构。

### 原则 5：局部 helper 只做局部 helper 的事

`renderSystemInlineTags(...)` 这类 helper 继续保留也可以，但定位必须收紧为：

- 已知 adapter-owned markdown 的局部修饰器

不能再把它视为：

- 任意动态文本进入 markdown 的安全兜底

## 8. 推荐统一方案

### 8.1 对系统卡片，统一向 structured/plain_text 收敛

优先级较高的收敛对象：

- snapshot
- notice
- request prompt
- thread history
- selection / target / path picker

方向：

- 保留固定标题和少量系统分组 markdown
- 动态值主体改走 `plain_text` / structured sections
- 实施时一次性移除 system-card 里不必要的 `CardBody` raw markdown 入口，而不是保留旧入口等待后续慢慢迁

这一步做完以后，业务侧不应再直接决定：

- 我这次要不要拼整段 body markdown
- 我这次要不要改成逐行 markdown element
- 我这次要不要自己混着来

这些应该都变成 adapter 内部实现细节，而不是业务层 contract。

### 8.2 对 shared progress，维持“行级 element”方向，但补明确 contract

shared progress 不一定要完全禁用 markdown，但它不应被定义成“第三条业务文本路径”。

它更适合作为 `system structured card lane` 内部的一种 renderer：

- 哪些 token 是系统拥有的
- 哪些动态值必须先转 plain text 表示
- 哪些场景不允许继续把外部字符串直接拼进 markdown 行

### 8.3 对 final，单独做 serializer 化治理

这条线不建议继续在现有字符串 helper 上叠规则。

应该单独建设：

- final markdown 输入 contract
- final Feishu markdown 子集
- 单次解析 / 单次序列化的 renderer

### 8.4 明确禁止“中间 markdown”长期存在

所谓“中间 markdown”，指的是：

- 既不是原始上游 markdown
- 也不是最终下发给 Feishu 的 markdown
- 只是某个中间 helper 改过一半的字符串

这类字符串最容易让下一层继续误判语义。

### 8.5 后续建议对业务层只暴露 3 种可选路径

如果后续希望把“文本怎么生成”固定成少数可选项，建议业务层只允许选择：

1. `SendText`
   - 单纯文本消息
2. `SystemCard`
   - 结构化系统卡片
3. `FinalCard`
   - final answer 专用卡片

不建议继续把这些作为业务层可选项暴露：

- `raw markdown body`
- `逐行 markdown element`
- “先拼半成品 markdown，再交给下游继续改写”

其中：

- `raw markdown body`
  - 在治理实施时应直接从业务层移除入口，不保留可继续使用的 legacy 分支
- `逐行 markdown element`
  - 应视为 `SystemCard` 的内部 renderer 之一，只由 adapter 决定是否使用

### 8.6 实施约束：不接受长期或阶段性双路径共存

这项治理一旦真正进入实现阶段，收尾要求必须是一次性切换完成，而不是：

- 先保留旧 helper
- 先允许新旧两条业务入口同时存在
- 先把一部分 projector 迁过去，剩下的以后再说
- 先留一个 legacy fallback 给后来人继续复用

可接受的实施方式应是：

- 先把新的 3 条业务路径和对应 helper 设计完整
- 再按一次变更或一组连续不可中断的子变更完成切换
- 在最后收尾时删除旧入口、旧 helper、旧 contract、旧调用点
- 让后续新增代码无法再通过旧路径落地

验收时应明确检查：

- 业务层是否仍能直接选择 `raw markdown body`
- 是否仍存在鼓励继续拼 system raw markdown 的 helper
- 是否仍有旧路径调用点残留在 projector / daemon / control contract 中

### 8.7 helper 收口建议：3 条路径各自拥有专属 helper 面

如果这项治理继续往前做，建议不要只统一“概念上的 3 条路径”，还要把 helper 面也固定到这 3 条上。

否则就算业务层名义上只剩 `SendText / SystemCard / FinalCard`，实现里仍然可能到处散落：

- 手写 markdown field
- 手写 hint 拼接
- 手写 `<text_tag>`
- 手写 section / block / status line

那最终还是会退化成“表面统一、内部继续分叉”。

更合适的方向是：

#### A. `SendText` helper

职责：

- 只处理普通文本消息
- 不进入卡片 markdown 语境
- 不承担格式增强、inline tag、卡片拆分等职责

适合覆盖：

- 非 final assistant block
- 普通 timeline text

这条路径几乎不需要复杂 helper，重点是边界要硬。

#### B. `SystemCard` helper

这是后续最核心的一层。

它不应该让业务代码直接拼 markdown body，而应该提供固定的结构化 helper，例如：

- `Section(label, lines...)`
- `PlainBlock(text)`
- `Field(label, value)`
- `FieldGroup(fields...)`
- `Hint(text)`
- `StatusList(items...)`
- `ActionRow(buttons...)`
- `SelectField(...)`
- `Form(...)`

其中 `Field(value)` 不能只是裸字符串，还需要有限的值语义，例如：

- `PlainValue`
- `NeutralValue`
- `CodeValue`

这样像 snapshot / history / picker 这类当前靠：

- `formatNeutralTextTag(...)`
- `formatInlineCodeTextTag(...)`
- `snapshotField(...)`
- `targetPickerFieldMarkdown(...)`

手写出来的内容，才能被真正吸收到同一套 helper 里。

同时还需要一个受限的“系统 markdown”面，只允许 adapter-owned 的固定格式，例如：

- 标题
- 分组标题
- 状态标签
- progress 行前缀

而不是允许业务代码继续自由拼一整段 raw markdown。

#### C. `FinalCard` helper

职责应保持单一：

- 输入上游 final markdown
- 处理 preview rewrite / normalize / split
- 一次序列化为最终可下发的 Feishu final card

它不应与 `SystemCard` 共用“半成品 markdown helper”。

需要的 helper 更像：

- `BuildFinalCard(rawMarkdown, extras...)`
- `SplitFinalCard(...)`
- `NormalizeFinalMarkdown(...)`

而不是通用系统卡片的 field / section / hint helper。

### 8.8 现有需求是否能被这 3 组 helper 覆盖

按当前代码看，答案是：可以，但前提是 `SystemCard` helper 面不能过窄。

现有需求大致可映射为：

- snapshot
  - `FieldGroup` + `Section` + 少量 `NeutralValue` / `CodeValue`
- notice
  - `Section` / `PlainBlock` / `Hint`
- request / mcp request
  - `PlainBlock` + `StatusList` + `Form` + `ActionRow`
- selection / path picker / target picker / thread history / command catalog
  - `Section` + `FieldGroup` + `PlainBlock` + `SelectField` + `Form` + `ActionRow`
- shared progress
  - `StatusList` 或 `ProgressList` renderer
  - 仍属于 `SystemCard` 内部，不是第四条业务路径
- final answer
  - `FinalCard` 专线

真正需要注意的是两个边界：

1. `SystemCard` helper 必须支持有限的 inline 语义
   - 否则 snapshot / history / picker 这类现有需求会被迫回退到手写 markdown
2. `SystemCard` helper 不应支持“任意上游 markdown”
   - 否则它会重新长回旧的 raw markdown body 路径

因此，当前更准确的结论是：

- 现有需求可以被 3 组 helper 覆盖
- 其中真正需要设计扎实的是 `SystemCard`
- 一旦 `SystemCard` helper 面设计完整，这次治理可以做到一次性切换并干净收尾

### 8.9 当前代码核验结果：所有实际出站文本是否都能归入 3 条路径

按当前非测试代码搜索，实际的 Feishu 出站文本生产入口主要集中在：

1. `internal/adapter/feishu/projector.go` 的 `Project(...)`
2. `internal/adapter/feishu/projector.go` 的 `ProjectPreviewSupplements(...)`
3. `internal/adapter/feishu/gateway_inbound_lane.go` 的 `deliverAsyncInboundFailure(...)`

也就是说，这轮核验已经覆盖了当前仓库里真正会构造 Feishu 出站文本的主入口，而不是只抽样检查了几个 projector helper。

核验结论如下：

| 入口 | 当前实现 | 归属路径 | 当前是否含 raw markdown body | 3 路径是否足够 | 备注 |
| --- | --- | --- | --- | --- | --- |
| `UIEventSnapshot` | `formatSnapshot(...)` -> `CardBody` | `SystemCard` | 是 | 足够 | 动态值很多，属于应优先迁出的 raw body |
| `UIEventNotice` | `projectNoticeBody/Elements(...)` | `SystemCard` | 部分是 | 足够 | 已有 `Sections` 路径，但无 section 时仍会落 raw body |
| `UIEventPlanUpdated` | `planUpdateBody/Elements(...)` | `SystemCard` | 是 | 足够 | explanation 进 body，steps 进 markdown elements |
| `UIEventFeishuDirectSelectionPrompt` | `selectionPromptElements(...)` | `SystemCard` | 否 | 足够 | 已是结构化卡片 |
| `UIEventFeishuDirectCommandCatalog` | `commandCatalogElements(...)` | `SystemCard` | 否 | 足够 | 已是结构化卡片 |
| `UIEventFeishuDirectRequestPrompt` | `requestPromptBody/Elements(...)` | `SystemCard` | 是 | 足够 | body 仍是 raw body，form/action 已结构化 |
| `UIEventTimelineText` | 文本直接发送 | `SendText` | 否 | 足够 | 无例外 |
| `UIEventFeishuPathPicker` | `pathPickerElements(...)` | `SystemCard` | 否 | 足够 | 已是结构化卡片 |
| `UIEventFeishuTargetPicker` | `targetPickerElements(...)` | `SystemCard` | 否 | 足够 | 已是结构化卡片 |
| `UIEventFeishuThreadHistory` | `threadHistoryElements(...)` | `SystemCard` | 否 | 足够 | 已是结构化卡片 |
| `UIEventBlockCommitted` 非 final | 直接发送 `block.Text` | `SendText` | 否 | 足够 | 无例外 |
| `UIEventBlockCommitted` final | `projectFinalReplyCards(...)` | `FinalCard` | 是，且预期如此 | 足够 | final 专线，属于允许的特例 |
| `UIEventExecCommandProgress` | `projectExecCommandProgress(...)` | `SystemCard` | 实际 payload 否 | 足够 | `Operation.CardBody` 只是镜像文本，真实下发走 elements |
| `UIEventThreadSelectionChange` | `projectThreadSelectionChangeCardContent(...)` | `SystemCard` | 有一个分支是 | 足够 | `new_thread_ready` 仍走固定 copy body，其余分支已是 plain block |
| `ProjectPreviewSupplements(...)` | 通用补充卡片 | 结构上是例外 | 可为任意 | 不足 | 当前生产代码未发现真实调用方生成 `Kind=card` supplement，但 API 仍允许任意 card 注入 |
| `deliverAsyncInboundFailure(...)` | 网关异步失败兜底卡 | `SystemCard` | 是 | 足够 | 固定 copy，绕过 projector，但不构成第四条路径 |

由此可以得到两层结论：

#### 结论 A：当前活跃业务场景都能被 3 条路径覆盖

只看当前真实业务场景：

- `SendText`
- `SystemCard`
- `FinalCard`

这 3 条路径已经足够，没有发现必须引入第四条业务路径的活跃场景。

也就是说：

- snapshot / notice / plan / request / picker / history / progress / thread-selection-change / async inbound failure
  - 都可以归为 `SystemCard`
- 普通文本 block / timeline text
  - 都可以归为 `SendText`
- final reply
  - 归为 `FinalCard`

#### 结论 B：当前仍存在一个结构性例外入口

真正的例外不是某个具体业务卡片，而是：

- `ProjectPreviewSupplements(...)`

这个入口当前仍允许外部补充一张任意 `card`，其中可以自行携带：

- `title`
- `body`
- `theme`
- `elements`

虽然这轮搜索没有发现非测试代码里真的在生产 `Kind=card` 的 supplement，但它在结构上仍然是：

- 一个未被 3 条路径 helper 面收口的 escape hatch

如果后续要把 3 路径模型真正做成封闭系统，这个入口也必须一起处理，不能只改主 projector 分支。

### 8.10 raw markdown 特别核验

按当前代码核验，raw markdown body 现状可分为三类：

#### A. 允许保留的 raw markdown

- `FinalCard`
  - 即 `projectFinalReplyCards(...)`
  - 这是模型里允许保留的唯一核心 raw markdown 专线

#### B. 应迁出的 system raw markdown

这些仍属于 `SystemCard`，但不应在治理完成后继续保留为 body raw markdown：

- `UIEventSnapshot`
- `UIEventNotice` 的 body 分支
- `UIEventPlanUpdated`
- `UIEventFeishuDirectRequestPrompt`
- `UIEventThreadSelectionChange` 的 `new_thread_ready` 分支
- `deliverAsyncInboundFailure(...)`

这些点共同说明：

- 当前最大的 raw markdown 债务仍然在 system-card，而不在 final

#### C. 看起来像 raw body，实际 payload 不是

- `UIEventExecCommandProgress`

这里 `Operation.CardBody` 虽然被赋值，但实际构造 `card` 时传的是：

- `rawCardDocument("工作中", "", cardThemeProgress, elements)`

所以真实下发给飞书的是：

- markdown/plain_text elements

而不是 `CardBody` body markdown。

这类点在治理时应避免误判，否则会把已经接近正确方向的路径当成旧路径重构。

## 9. 分阶段推进建议

### Phase A：盘点与边界定型

目标：

- 完成所有主要文本生产点、格式化点、承接点 inventory
- 标记每条链路属于哪一条 lane
- 明确哪些链路继续允许 raw markdown，哪些不允许

产出：

- 当前文档
- 跟踪 issue

### Phase B：system-card 收口

目标：

- 优先把仍在使用 raw markdown body 的系统卡片迁到 structured/plain_text
- 缩小 `renderSystemInlineTags(...)` 的适用面

### Phase C：final pipeline 重构

目标：

- 合并 preview rewrite 与 final normalize 的语义边界
- 建立 final 专用的单一 serializer

### Phase D：回归测试治理

目标：

- 不是只测显示结果
- 而是测文本是否进入了错误语境

建议增加三类测试：

- 动态值未回流到 system raw markdown
- final 组合语法在 serializer 下稳定
- Feishu payload 层的 markdown element / plain_text element 分配符合预期

## 10. 验收标准

治理完成后，至少应满足：

- 仓库内每条主要 Feishu 出站文本链路都能明确归属到某一条 lane
- system-card 的动态值默认不再直接进入 raw markdown body
- final markdown 不再经过多轮互不一致的字符串重解释
- helper 的职责边界能在代码和测试里说清楚
- 新增卡片实现时，默认先选 carrier，再做格式化，而不是先拼 markdown

## 11. 非目标

本轮治理不追求：

- 一次性重写整个内容系统
- 为全仓引入一个通用内容 AST 或 DTO 体系
- 把所有 markdown 全部禁掉

本轮要做的是把当前最容易反复出错的文本边界收拢清楚，并给后续实现提供一致的规则。

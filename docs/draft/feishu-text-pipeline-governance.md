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

- [app_ui.go](/data/dl/fschannel4/internal/app/daemon/app_ui.go)
- [projector.go](/data/dl/fschannel4/internal/adapter/feishu/projector.go)
- [markdown_preview_rewrite.go](/data/dl/fschannel4/internal/adapter/feishu/markdown_preview_rewrite.go)
- [final_card_markdown.go](/data/dl/fschannel4/internal/adapter/feishu/final_card_markdown.go)
- [card_renderer.go](/data/dl/fschannel4/internal/adapter/feishu/card_renderer.go)

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

- [projector_inline_tags.go](/data/dl/fschannel4/internal/adapter/feishu/projector_inline_tags.go)

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
- 其中 `raw markdown body` 与“逐行 markdown element 拼动态文本”都只是过渡形态，不应继续暴露给业务层自行选择

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
- 逐步移除不必要的 `CardBody` raw markdown

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
  - 应视为 legacy 兼容路径，目标是逐步清退
- `逐行 markdown element`
  - 应视为 `SystemCard` 的内部 renderer 之一，只由 adapter 决定是否使用

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

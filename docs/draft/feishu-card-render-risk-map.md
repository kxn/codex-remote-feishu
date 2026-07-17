# 飞书卡片渲染风险地图（临时）

> Type: `draft`
> Updated: `2026-04-18`
> Summary: 基于前一轮格式审计，梳理当前 Feishu 卡片渲染链路中最危险的内容拼接点，按风险类型和优先级标注后续最小类型化改造的切入顺序。

## 目的

前一份文档 `docs/draft/feishu-card-render-format-audit.md` 已经基本确认：

- OpenAI / Codex 协议层交付的是字符串文本和结构化 item
- 官方 Codex 客户端会把 agent/final 文本按 Markdown 渲染
- 飞书卡片明确区分 markdown-capable 字段和 `plain_text` 字段
- 当前仓库里真正脆弱的地方，是不同内容语境都被压成了 `string`

本文件不再重复证明这些前提，而是继续向前一步：回答“如果现在要降低后续改功能时继续把渲染改坏的概率，优先该看哪几处”。

## 当前渲染拓扑

### 1. 卡片外壳是相对统一的

`internal/adapter/feishu/card_renderer.go` 里，正文 `CardBody` 默认会变成 `tag=markdown` 组件：

- `rawCardDocument(...)`：`internal/adapter/feishu/card_renderer.go`
- `cardMarkdownComponent.renderCardComponent(...)`：`internal/adapter/feishu/card_renderer.go`
- `renderCardDocument(...)`：`internal/adapter/feishu/card_renderer.go`

这层本身不是最危险的地方。它的问题在于：

- 只要上层交进来的还是裸 `string`
- 这里就只能把它当 markdown content 原样下发

也就是说，真正高风险的不是“最后渲染那一步”，而是“进入 markdown 组件之前的字符串到底是什么语境”。

### 2. final reply 是当前最受控的一条链

final reply 在进卡片前会额外走：

- `projectFinalReplyCards(...)`：`internal/adapter/feishu/projector.go`
- `normalizeFinalCardMarkdown(...)`：`internal/adapter/feishu/final_card_markdown.go`

它至少做了两类保护：

- 保留 fenced code / inline code 的边界
- 中和本地相对 markdown link，避免直接按远端可点击链接渲染

所以 final reply 不是完全没风险，但它已经明显比其它 projector 分支“更像一个被认真处理过的专用语境”。

### 3. 最大的风险集中在“字符串拼接后再塞进 markdown”

当前高风险基本都属于这个模式：

1. 上游传来文本、标题、描述、历史消息、选项说明等字符串
2. 本地 projector 用 `fmt.Sprintf` / `strings.Join` / `+` 拼成一段 markdown
3. 最终作为 `tag=markdown` 下发

一旦上游字符串里自己带有：

- 列表符号
- 标题符号
- fenced code
- 反引号
- markdown link
- 飞书 `<text_tag>` / `<font>` 一类特化标签

就会直接影响外层结构。

## 风险类型

### R1. 原始字符串直接进入 markdown 正文

特征：

- 上游字段没有类型信息
- 渲染点也不做 escape / neutralize
- 直接进入 markdown 列表、段落、标题、说明区域

这是最危险的类型。

### R2. 只做“局部 inline tag 替换”，没有解决完整 markdown 语境问题

代表 helper：

- `renderSystemInlineTags(...)`：`internal/adapter/feishu/projector.go`
- `formatNeutralTextTag(...)` / `formatCommandTextTag(...)` / `formatInlineCodeTextTag(...)`：`internal/adapter/feishu/projector_inline_tags.go`

这类 helper 的价值是有的，但它们本质上只解决局部问题：

- 把某些值包成 `<text_tag>`
- 把单个反引号 token 改写成更稳的 inline tag

它们没有把“这整段字符串到底是不是安全 markdown”这个问题解决掉。

### R3. 同一函数里部分字段被 neutralize，部分字段仍然裸拼

这种类型尤其容易制造“以为这里已经处理过”的错觉。

表现通常是：

- 第一行 / 某个字段走了 `formatNeutralTextTag(...)`
- 第二行 / 补充行 / 描述行仍然原样拼进 markdown

这会让函数看起来像“已经考虑过格式安全”，但实际上只是部分安全。

### R4. 系统内部文案与上游动态内容共用同一拼接路径

有些函数一开始只服务系统内固定文案，后来逐步混入：

- thread title
- last assistant message
- request option description
- workspace/session meta

这时函数外观没变，但语义已经从“内部 copy”变成了“混合输入”。

## 高风险渲染点

### P0：thread history 详情把历史输入/输出直接拼进 markdown

位置：

- `internal/adapter/feishu/projector_thread_history.go:137`

原因：

- `detail.Inputs` 和 `detail.Outputs` 是最典型的“历史真实内容”
- 现在直接用 `fmt.Sprintf("%d. %s", ...)` 拼到 markdown 列表项里
- 没有 markdown neutralize
- 只有长度截断，没有语境转换

风险表现：

- 用户输入或 assistant 输出里只要出现列表、标题、代码块、链接，就会直接干扰详情卡片结构
- 尤其 assistant 输出本身常常就是 markdown，这里相当于“把 markdown 再嵌到 markdown 列表项里”

为什么优先级最高：

- 数据最真实、最不可控
- 用户最容易碰到
- 卡片一旦乱掉，排查时也最容易误判成其它地方坏了

### P0：request user input / MCP elicitation 问题卡把上游 question text 当 markdown 直接拼

位置：

- `internal/adapter/feishu/projector_request.go:214`
- 同一渲染路径也被 `internal/adapter/feishu/projector_request_mcp.go` 复用

原因：

- `question.Header`
- `question.Question`
- `question.DefaultValue`
- `option.Label`
- `option.Description`

这些字段全部直接拼进 `requestPromptQuestionMarkdown(...)` 的 markdown 文本块。

而前一轮外部审计已经确认：

- 上游协议把这些交互问题定义成结构化字段
- 但字段值本身仍然只是字符串，不是“安全 markdown 片段”

风险表现：

- 选项描述里带列表、代码、反引号或链接时，会影响整段问题卡的结构
- `DefaultValue` 如果本身包含 markdown 特殊字符，也会污染“当前答案：”这一行

为什么优先级最高：

- 这是明确的协议边界点
- 这里如果不收紧，后面继续补更多 request / selection / approval UI 时会反复踩坑

### P1：selectionOptionBody 只把第一段做 neutralize，剩余行继续裸拼

位置：

- `internal/adapter/feishu/projector_selection.go:177`
- 被 `internal/adapter/feishu/projector_selection_use_thread.go` 大量复用

原因：

- attach instance / attach workspace / use thread 等多个列表都走这条路径
- 第一行路径或工作区通常会被包进 `formatNeutralTextTag(...)`
- 但剩余行直接 `strings.Join(parts[1:], "\n")`
- `use_thread` 特例里也只是把第一行 `/path` 之类做 neutralize，剩余行原样带入

风险表现：

- option subtitle / meta 一旦含 markdown，会直接扩散到选择卡正文
- 同一类 UI 在不同 view mode 下显示还不一致，更难形成稳定心智模型

为什么优先级高：

- 这条路径覆盖面非常大
- 而且属于典型的 R3：看起来像处理过，实际上只处理了一部分

### P1：command catalog 只把 command/example 包成 tag，title/description/breadcrumb 仍是原始 markdown

位置：

- `internal/adapter/feishu/projector_command_catalog.go:120`

原因：

- `Commands` / `Examples` 走 `formatCommandTextTag(...)`
- 但 `Title` / `Description` / `Breadcrumbs` 仍直接拼 markdown

风险表现：

- 命令说明未来一旦改文案、加示例、引用外部文本，很容易把标题层级、列表、引用结构带进来
- 这类问题短期内不一定经常爆，但很适合在后续新需求里“顺手改坏”

为什么是 P1 而不是 P0：

- 目前这里的大部分输入还是我们自己维护的内部文案
- 但它已经是“看起来安全、实际上不完全安全”的典型样板

### P1：snapshot 状态卡把 thread 标题、最近消息、预览文本直接嵌入 markdown 字段

位置：

- `internal/adapter/feishu/projector_snapshot.go:18`

原因：

- snapshot 里很多结构化字段已经包成了 `formatNeutralTextTag(...)`
- 但以下内容仍直接进入 markdown：
  - `SelectedThreadTitle`
  - `SelectedThreadFirstUserMessage`
  - `SelectedThreadLastUserMessage`
  - `SelectedThreadLastAssistantMessage`
  - `SelectedThreadPreview`
  - `PendingHeadless.ThreadTitle`

风险表现：

- 状态卡本来是“总览卡”，但最近消息一旦带 markdown 结构，就可能把整张卡的局部层次带乱
- 用户往往会把这类问题误解成状态机或路由错误，而不是渲染问题

为什么是 P1：

- 这些字段来源真实且动态
- 但有长度压缩，所以相较 thread history 详情卡，爆炸半径略小

### P2：target picker / path picker / notice 等系统卡大量依赖 renderSystemInlineTags

位置：

- `internal/adapter/feishu/projector.go:806`
- `internal/adapter/feishu/projector_target_picker.go:116`
- `internal/adapter/feishu/projector_target_picker.go:394`
- `internal/adapter/feishu/projector_path_picker.go:82`

原因：

- 这批卡片里很多字段来自系统自己生成的提示、状态、hint、message
- 所以总体上比 thread/request/history 更安全
- 但它们大量依赖 `renderSystemInlineTags(...)`

而 `renderSystemInlineTags(...)` 的行为是：

- 只在检测到单反引号时做 inline tag 替换
- 其它 markdown 结构全部保留

风险表现：

- 一旦系统提示文案里开始混入真实动态文本，或者将来有人把带 markdown 的字符串塞进来，这层保护会明显不够
- `targetPickerMessageMarkdown(...)` 在 danger 场景下还会再包一层 `<font color='red'>...</font>`，如果输入文本本身结构复杂，更难推断最终效果

为什么是 P2：

- 当前大多数输入还是我们自己可控的系统文案
- 但这是一个很明显的技术债集中区

## 低风险或已有专门保护的区域

### A. final reply

位置：

- `internal/adapter/feishu/projector.go:521`
- `internal/adapter/feishu/final_card_markdown.go:16`

原因：

- 有专门的 final markdown normalize
- 明确区分 fenced / inline / local markdown link

它仍然不是“强类型安全”，但已经比其它分支收敛得多。

### B. form / select / button 的 plain_text 字段

位置示例：

- `internal/adapter/feishu/projector_request.go:279`
- `internal/adapter/feishu/projector_path_picker.go:191`
- `internal/adapter/feishu/projector_target_picker.go:406`

原因：

- 这些地方直接走飞书 `plain_text`
- 至少不会把输入文本解释成 markdown 结构

这批位置更适合作为后续改造里的“安全落点样板”。

## 推荐的最小改造顺序

不建议下一步直接全仓重构成完整内容类型系统，更稳的顺序是：

1. 先收口 P0：
   - `threadHistoryDetailElements(...)`
   - `requestPromptQuestionMarkdown(...)`
2. 再收口 P1：
   - `selectionOptionBody(...)`
   - `commandCatalogEntryMarkdown(...)`
   - `formatSnapshot(...)` 里动态消息相关字段
3. 最后再统一整理 P2：
   - `renderSystemInlineTags(...)` 相关系统提示链路

这样做的好处是：

- 第一阶段就能先挡住最容易炸的真实动态内容
- 第二阶段再把“部分 neutralize、部分裸拼”的 UI 列表类逻辑清理掉
- 最后才处理那些目前 mostly-internal 的系统提示路径

## 对后续类型化改造的直接启发

从这份风险地图看，最值得优先引入的不是“完美抽象”，而是最小的内容边界：

1. `PlainText`
   - 可进入 `plain_text` 字段
2. `FeishuMarkdown`
   - 已经允许进入飞书 markdown 区
3. `UnsafeExternalText`
   - 来自上游、历史记录、动态消息、question/option 等外部文本
4. `FinalAssistantMarkdown`
   - 只给 final reply 专用的 markdown 语境

第一阶段甚至不一定需要把所有函数都改掉，只要先做到：

- 高风险函数不再直接拿 `string` 拼 markdown
- 必须显式把外部文本变成某种“可放入 markdown 的安全片段”

就能明显降低后续功能改动时再次把渲染改坏的概率。

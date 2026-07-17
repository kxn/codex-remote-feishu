# 飞书卡片渲染与格式假设审计（临时）

> Type: `draft`
> Updated: `2026-04-18`
> Summary: 记录当前仓库内飞书卡片生成链路、本地对上游 Codex 内容形态与飞书卡片格式的既有判断，并补充基于 OpenAI / 飞书官方资料的二次验证结果。

## 背景

当前仓库里飞书卡片“外壳”的生成已经相对收敛，但“上游消息内容如何以正确格式嵌入到不同卡片字段”仍然比较分散。后续功能开发时，容易出现：

- Markdown 被再次嵌入 Markdown，导致结构漂移
- 反引号、列表、标题等 Markdown 语法影响外层卡片排版
- 变量值被插到不同上下文时没有按目标位置做对应 escape
- Feishu 特化标签（如 `<text_tag>` / `<font>`）被带入不适合的位置

本文件先收口当前代码内的结论，后续再结合官方外部资料做二次审计。

## 当前链路概览

### 1. UIEvent -> Projector -> Operation -> Feishu Card JSON

- `internal/app/daemon/app_ui.go`
  - final block 会先进入 `RewriteFinalBlock(...)`
  - 然后统一走 `a.projector.Project(...)`
- `internal/adapter/feishu/projector.go`
  - 按 `UIEvent.Kind` 分发到不同卡片生成路径
- `internal/adapter/feishu/card_renderer.go`
  - 将 `Operation` 转成 Feishu card payload
- `internal/adapter/feishu/gateway_runtime.go`
  - 发卡前统一做 payload 裁剪

### 2. final reply 特殊链路

- `internal/adapter/feishu/markdown_preview_rewrite.go`
  - 对 final markdown block 做本地文件/预览链接改写
- `internal/adapter/feishu/projector.go`
  - `projectBlock(...)`
  - `projectFinalReplyCards(...)`
- `internal/adapter/feishu/final_card_markdown.go`
  - `normalizeFinalCardMarkdown(...)`
  - 保留 fenced code
  - 中和不适合直接渲染的本地 markdown 链接
- `internal/app/daemon/app_final_block_patch.go`
  - 首次 preview rewrite 失败时，存在 second-chance patch 回写同一张 final 卡

## 当前代码中已识别的内容上下文

### A. Feishu markdown 元素

最常见。`CardBody` 最终会被转成：

- `tag=markdown`
- `content=<string>`

对应文件：

- `internal/adapter/feishu/card_renderer.go`

风险点：

- 任何传入这里的字符串，都会被按 Feishu markdown 解释
- 如果调用点以为它是“纯文本”，就会产生语法串扰

### B. plain_text 元素

主要用于：

- 标题
- 按钮文字
- select option 文本
- input label / placeholder

对应文件：

- `internal/adapter/feishu/card_renderer.go`

特点：

- 这类位置相对安全
- 更像“最终展示文本”，不是 markdown 上下文

### C. Feishu markdown 内嵌特化标签

代码中大量使用：

- `<text_tag color='neutral'>...`
- `<font color='red'>...`

对应文件：

- `internal/adapter/feishu/projector_inline_tags.go`
- `internal/adapter/feishu/projector_file_summary.go`
- `internal/adapter/feishu/projector_target_picker.go`

特点：

- 这不是普通 Markdown，而是“Feishu markdown + 平台特化标签”
- 一旦这些字符串被拿去做别的嵌套，容易发生上下文错位

### D. HTML 预览页

完全是另一条规则链路：

- `internal/adapter/feishu/web_preview_render.go`
- `internal/adapter/feishu/web_preview_shell.go`

这里使用：

- `BodyHTML`
- `escapePreviewText(...)`

特点：

- 这是标准 HTML 语境，不是 Feishu 卡片 markdown 语境
- 与飞书卡片渲染规则不可混用

## 当前主要卡片生成位置

### 主分发入口

- `internal/adapter/feishu/projector.go`

### 主要子模块

- 快照/状态：`internal/adapter/feishu/projector_snapshot.go`
- notice：`internal/adapter/feishu/projector.go`
- plan：`internal/adapter/feishu/projector_plan_update.go`
- 选择类卡片：`internal/adapter/feishu/projector_selection.go`
- `/use` 特殊布局：`internal/adapter/feishu/projector_selection_use_thread.go`
- 命令菜单/配置：`internal/adapter/feishu/projector_command_catalog.go`
- request / user input / approval：`internal/adapter/feishu/projector_request.go`
- MCP elicitation：`internal/adapter/feishu/projector_request_mcp.go`
- path picker：`internal/adapter/feishu/projector_path_picker.go`
- target picker：`internal/adapter/feishu/projector_target_picker.go`
- thread history：`internal/adapter/feishu/projector_thread_history.go`
- exec progress：`internal/adapter/feishu/projector_exec_command_progress.go`
- final markdown normalize：`internal/adapter/feishu/final_card_markdown.go`
- final split：`internal/adapter/feishu/projector_final_split.go`
- preview supplement card：`internal/adapter/feishu/projector.go`
- inbound 异步失败提示：`internal/adapter/feishu/gateway_inbound_lane.go`
- web preview 页面：`internal/adapter/feishu/web_preview_render.go`

## 当前判断：哪里比较稳，哪里比较脆

### 相对收敛的部分

- 卡片外壳渲染层相对统一：
  - `Project(...)`
  - `renderOperationCard(...)`
  - `renderCardDocument(...)`
  - `trimCardPayloadToFit(...)`
- final reply 的 markdown 处理是当前最专门化的一条链：
  - preview rewrite
  - final markdown normalize
  - final split

### 明显分散、容易改坏的部分

- `requestPromptQuestionMarkdown(...)`
  - 问题、默认值、选项描述直接拼 markdown
- `threadHistoryDetailElements(...)`
  - inputs / outputs 直接拼 markdown 列表
- `projectThreadSelectionChangeBody(...)`
  - 最近用户/最近回复直接拼进 markdown
- `selectionOptionBody(...)`
  - 同一段内容里，一部分包 `text_tag`，一部分直接裸拼
- `commandCatalogEntryMarkdown(...)`
  - command/example 走 `text_tag`，title/description/breadcrumb 仍是原始 markdown

## 当前根因判断

当前问题更像是“类型边界不清”，而不是单点 escape helper 错误。

代码里同时存在至少四种内容语境：

1. 原始纯文本
2. 原始 markdown
3. 已带 Feishu `<text_tag>` / `<font>` 的 markdown
4. HTML 预览文本

但这些内容在代码里大都只是 `string`，没有显式类型区分。结果就是每个 card builder 都在本地猜：

- 现在拿到的是不是原始 markdown
- 当前插入点是不是 markdown 上下文
- 是不是应该转成 `plain_text`
- 这里能不能安全地包 `<text_tag>`

这就是后续改功能时很容易“顺手改坏”的根本原因。

## 当前待外部二次验证的问题

### 1. 上游 Codex 内容假设是否准确

待验证内容：

- 上游 app-server / Codex 侧输出的 message/content，到底是：
  - 纯文本
  - Markdown
  - 受限 Markdown
  - 混合 item 结构
- 我们当前把 final assistant text 基本按 markdown 看待，假设是否成立
- 非 final 过程文本、tool/request/input 等事件，是否也允许 markdown 语义

### 2. Feishu 卡片目标格式假设是否准确

待验证内容：

- `tag=markdown` 在新卡片 schema 下的精确定义
- `plain_text` 与 `markdown` 的预期边界
- Feishu markdown 中 `<text_tag>` / `<font>` 等标签到底属于正式支持、兼容支持还是历史遗留用法
- 不同字段（标题、正文、select option、button 文本、form label）各自允许什么格式

### 3. 当前看起来“能用”的地方是否其实已经偏离平台规范

待验证内容：

- 我们现在很多渲染结果是否只是“当前客户端凑巧能显示”
- 是否存在平台未承诺的行为
- 是否有些地方实际上已经是 undefined / weakly-supported behavior

## 外部二次验证结果

### A. OpenAI / Codex 官方侧

#### 结论 1：协议层把 agent/final 文本定义成“字符串文本”，而不是“显式 Markdown 类型”

官方 app-server 文档与协议源码都把 assistant 文本建模为字符串字段：

- `agentMessage` 为 `{id, text}`
- `item/agentMessage/delta` 为流式文本增量，按顺序拼接恢复完整回复
- `plan` 为 `{id, text}`
- `reasoning` 为 `{id, summary, content}`
- `tool/requestUserInput` 是单独的结构化请求，不属于 assistant 文本的一部分

对应官方资料：

- OpenAI app-server 文档：
  - `https://developers.openai.com/codex/app-server`
- OpenAI 官方仓库（当前审计使用提交 `5bb193aa88fef0f5ef3fbbd2c6253ba93d3f6521`）：
  - `https://github.com/openai/codex/blob/5bb193aa88fef0f5ef3fbbd2c6253ba93d3f6521/codex-rs/app-server/README.md`
  - `https://github.com/openai/codex/blob/5bb193aa88fef0f5ef3fbbd2c6253ba93d3f6521/codex-rs/app-server-protocol/src/protocol/v2.rs`
  - `https://github.com/openai/codex/blob/5bb193aa88fef0f5ef3fbbd2c6253ba93d3f6521/codex-rs/docs/protocol_v1.md`

这意味着：

- 我们不能把“上游字段类型 == Markdown”当作协议保证
- 更准确的说法应是：协议交给客户端的是“文本内容”，具体按何种富文本方式展示，由客户端决定

#### 结论 2：官方 Codex 客户端实际上按 Markdown 语义渲染 agent/final 文本

虽然协议没有给 `agentMessage.text` 标注 Markdown 类型，但官方客户端实现里已经把这类文本当 Markdown 处理：

- `chatwidget.rs` 中明确缓存“Raw markdown of the most recently completed agent response”
- `history_cell.rs` 中多个历史单元通过 `append_markdown(...)` 渲染 agent / reasoning / plan 文本

对应官方资料：

- `https://github.com/openai/codex/blob/5bb193aa88fef0f5ef3fbbd2c6253ba93d3f6521/codex-rs/tui/src/chatwidget.rs`
- `https://github.com/openai/codex/blob/5bb193aa88fef0f5ef3fbbd2c6253ba93d3f6521/codex-rs/tui/src/history_cell.rs`

这意味着：

- “把 final / assistant text 作为 Markdown 渲染”与官方客户端行为是一致的
- 但这仍然应该被视为“客户端展示约定”，不是协议层强类型保证

#### 结论 3：结构化交互不能当作普通文本消息去猜

官方协议把以下内容建成了独立 item / request 结构，而不是 assistant markdown：

- `tool/requestUserInput`
- `plan`
- `reasoning`
- `commandExecution`
- `mcpToolCall`

所以如果我们把这些内容先压成字符串、再和普通消息共用一套拼 Markdown 的路径，天然就更容易出错。

#### 对当前假设的回判

- “上游最终回复通常可按 Markdown 渲染”：
  - 基本成立，但应降级为“官方客户端约定”，不是“协议保证”
- “非普通文本事件也许都能当 Markdown 混进消息渲染”：
  - 不成立，官方协议明显区分了结构化交互与普通 agent message

### B. 飞书官方卡片侧

说明：飞书开放平台公开文档页本身是前端壳，审计时同时使用了官方文档接口
`https://open.feishu.cn/api/tools/document/detail` 按 `fullPath` 拉取正文内容做交叉核对。

#### 结论 1：`plain_text` 与 markdown-capable 字段边界是明确存在的

官方卡片 JSON 2.0 文档明确区分：

- 标题 / 副标题文本对象：`tag` 可为 `plain_text` 或 `lark_md`
- 普通文本组件 `div.text.tag`：可为 `plain_text` 或 `lark_md`
- 富文本组件：组件自身 `tag = markdown`
- 但大量交互组件字段只允许 `plain_text`

对应官方资料：

- 卡片结构：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-structure`
- 普通文本：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/content-components/plain-text`
- 富文本：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/content-components/rich-text`

这意味着：

- 我们之前心里的“有些位置能吃 markdown，有些只能吃纯文本”这个大方向是对的
- 但应该更严格：不能把一个已经带 Feishu markdown/标签语义的字符串随手塞到任何文本字段

#### 结论 2：`<text_tag>` / `<font>` 不是偶然兼容，而是 markdown/rich-text 语境中的正式支持

官方富文本组件文档明确写了：

- JSON 2.0 富文本支持除 `HTMLBlock` 外的标准 Markdown 语法
- 还支持一批 HTML 风格标签，包括：
  - `<br>`
  - `<hr>`
  - `<person></person>`
  - `<local_datetime></local_datetime>`
  - `<at></at>`
  - `<a></a>`
  - `<text_tag></text_tag>`
  - `<raw></raw>`
  - `<link></link>`
  - `<font></font>`

因此：

- 我们代码里在 markdown 富文本区域使用 `<text_tag>` / `<font>`，不是“纯靠客户端凑巧兼容”
- 但它们属于 markdown-capable 上下文，不应外溢到 plain_text-only 字段

#### 结论 3：交互组件的大量文案字段就是 plain_text-only

官方文档明确给出以下字段使用 `plain_text`：

- button：
  - `text`
  - `confirm.title`
  - `confirm.text`
- input：
  - `placeholder`
  - `disabled_tips`
  - `label`
  - `confirm.title`
  - `confirm.text`
- single select / multi select：
  - `placeholder`
  - `options[].text`
  - `confirm.title`
  - `confirm.text`

对应官方资料：

- button：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/button`
- input：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input`
- single select：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/single-select-dropdown-menu`
- multi select：
  - `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/multi-select-dropdown-menu`

这意味着：

- 任何把 Markdown、`<text_tag>`、`<font>`、代码引用符号等直接喂进这些字段的做法，都不应被视为稳妥用法
- 就算当前客户端“显示得像能用”，也不应把这种行为当作可靠规范

## 当前收敛结论

### 已被证实的判断

- 卡片“外壳”生成相对集中，但内容语境判断分散，这个观察是对的
- 上游最终 assistant 文本按 Markdown 渲染，这与官方 Codex 客户端行为一致
- 飞书平台确实正式区分了 markdown-capable 与 plain_text-only 字段
- `<text_tag>` / `<font>` 在飞书 markdown/rich-text 中是正式支持能力

### 需要修正表述的判断

- 不能说“OpenAI 协议明确定义 assistant text 是 Markdown”
- 更准确的说法是：
  - 协议交付的是文本字符串
  - 官方客户端把 agent/final 文本按 Markdown 渲染
  - 我们如果跟随这一约定是合理的，但不能把它扩大到所有上游事件类型

### 当前最可信的根因判断

从这次二次验证看，问题不像是：

- “我们完全误解了飞书 markdown 规范”
- 或“上游其实根本不是 Markdown，我们一直都理解错了”

更像是：

- 我们对“上游文本”和“结构化事件”的边界处理不够严格
- 我们对“目标卡片字段到底是 markdown 语境还是 plain_text 语境”的建模不够严格
- 代码里大量内容仍然只是 `string`，导致相同字符串在不同渲染目标之间被反复复用、二次拼接、错误嵌套

这使得系统在“当前样例能显示”的情况下仍然非常脆，一旦改功能或换嵌入位置，就容易出现：

- Markdown 套 Markdown
- plain_text 字段吃进 markdown 特殊字符
- Feishu 特化标签被塞进不该出现的位置
- 最终展示结果和开发者心里想象的不一致

## 后续建议方向

如果后续要真正降低这类问题，重点不应只是补更多 escape helper，而应考虑把内容类型边界显式化，例如至少区分：

1. 原始纯文本
2. 原始 Markdown
3. 已含 Feishu 特化标签的 Markdown
4. 仅可进入 plain_text 字段的展示文本
5. HTML 预览文本

然后让不同 projector / renderer 只接收自己允许的内容类型，而不是继续依赖 `string` 猜测。

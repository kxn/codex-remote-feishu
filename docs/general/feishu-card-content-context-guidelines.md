# Feishu Card Content Context Guidelines

> Type: `general`
> Updated: `2026-04-19`
> Summary: 固化 Feishu 卡片文本内容语境边界，要求新实现默认走 structured/plain_text 路径，禁止重新引入上游预拼 markdown 和长期 legacy 双路径。

## 1. 文档定位

这份文档是当前仓库实现 Feishu 卡片文本内容时必须遵守的研发基线。

它约束的不是某一张卡片的视觉样式，而是：

- 哪些文本可以进入 Feishu markdown 语境
- 哪些文本必须留在 `plain_text` 或结构化 section 里
- 结构、动态值和 Feishu-specific 渲染职责应该落在哪一层
- 新路径替换旧路径时，仓库应如何避免长期新旧并存

## 2. 适用范围

以下变更默认都应先对照本文件：

- `internal/adapter/feishu/**` 中的 card projector / renderer / helper
- `internal/core/control/**` 中新增或修改 Feishu card DTO、summary、status、section 字段
- `internal/core/orchestrator/**` 与 `internal/app/daemon/**` 中新增或修改会投影到 Feishu 卡片的 summary、status、message、notice、catalog 内容
- 任何讨论 Feishu 卡片文本 contract、markdown/plain_text 边界、render helper 职责的设计或文档

## 3. 当前默认语境

当前仓库里，至少应明确区分下面四种文本语境：

1. `plain_text`
   - 飞书明确不会按 markdown 解释的文本字段
2. adapter-owned markdown
   - 由 adapter 明确拥有结构和格式的 Feishu markdown 文本
3. Feishu-specific markdown tag
   - 如 `<text_tag>`、`<font>` 这类只应出现在 markdown-capable 字段里的平台标签
4. final reply markdown
   - final answer 专用的 markdown 归一化链路，是当前仓库的特例，不是通用 system-card contract

如果实现里无法明确说清楚当前字符串属于哪一种语境，就说明设计还没有收敛到可安全落地的边界。

## 4. 强制规则

### 4.1 动态值不上游预拼 raw markdown

只要文本里混入了外部或动态值，就不能在上游把它预拼成一整段 raw markdown 再往下游传。

典型动态值包括：

- 用户输入、quoted text、thread title、最近消息
- runtime/debug/error 文本
- repo URL、workspace path、file name、stderr/output tail
- 运行中状态、下一步提示、命令执行结果

这些内容默认应先保留结构，再由 adapter 决定最终落到：

- `plain_text`
- `FeishuCardTextSection`
- 或少量 adapter-owned markdown 片段

### 4.2 adapter 拥有最终 markdown/plain_text 切分

orchestrator / control / daemon 层负责表达业务语义与结构，不负责拼 Feishu markdown 结构。

默认顺序应是：

1. 上游传结构和动态值
2. adapter 决定哪些部分用 markdown，哪些部分用 `plain_text`
3. adapter 决定是否使用 `<text_tag>` / `<font>` 等 Feishu-specific 标签

不要把 Feishu-specific tag 反向扩散到 orchestrator / control。

### 4.3 `renderSystemInlineTags(...)` 不是通用 sanitizer

`renderSystemInlineTags(...)` 这类 helper 只能视为 adapter-owned markdown 的局部格式工具。

它们不能被当成：

- 任意动态文本进入 markdown 的安全兜底
- “先上游拼 markdown，再下游补 neutralize” 的通用修复策略
- 新 system-card text contract 的默认边界

### 4.4 新卡片文本默认优先结构化 carrier

新增 summary / status / notice / catalog / picker 文本时，默认优先使用：

- `FeishuCardTextSection`
- `SummarySections`
- 显式 section/label/value 字段
- 飞书组件原生 `plain_text` 字段

不要优先新增“再来一个 `string` markdown body”式 contract。

### 4.5 raw system markdown 只允许留给固定系统 copy

只有同时满足以下条件，才继续允许一条链路保留 raw markdown string：

- 文本结构完全由本地系统 copy 拥有
- 不混入 runtime/debug/path/repo/output/file name 等动态值
- markdown 只是展示格式，不承担“把结构和动态值重新揉在一起”的职责

不满足这些条件时，应改成 structured/plain_text 路径。

### 4.6 新路径替换旧路径时，不保留永久双路径

如果某条 legacy markdown contract 已经有了新的 structured/plain_text 替代路径，后续应尽快删除旧 contract，而不是长期保留：

- legacy flag
- legacy renderer branch
- “以防万一”的永久 fallback

当前仓库默认不接受“新旧两套方法长期并存”的收尾方式。

### 4.7 final reply markdown 是特例，不外溢

final reply 当前有专门的 markdown normalize 与 preview rewrite 链路。

这条能力只能服务 final answer / final card，不应直接复制成：

- 通用 notice contract
- 通用 status card contract
- command catalog / picker / request / selection 的默认做法

### 4.8 新边界必须带回归测试

只要一条链路的文本 contract 发生变化，至少应补一类测试：

- 动态文本已迁出 markdown 时：
  - 断言动态值不再出现在 markdown element / markdown body
- 明确保留 adapter-owned markdown 时：
  - 断言输入确实是系统拥有的固定 copy，或已走专用 normalize/helper

不要只测“看起来能显示”，要测“有没有重新把动态值塞回 markdown”。

## 5. 推荐实现顺序

以后新做 Feishu 卡片文本相关功能，默认按这个顺序推进：

1. 先判定文本语境：
   - 这是固定系统 copy，还是混有动态值
2. 再选 carrier：
   - `plain_text` / section / explicit field / specialized markdown
3. 再由 adapter 做最终 Feishu 渲染
4. 最后补测试，证明动态值没有误回流到 markdown

如果一开始就从“怎么拼 markdown 更省事”出发，通常就是错的起点。

## 6. 当前仓库的推荐 carrier

当前仓库已经有一些可复用的安全 carrier：

- `internal/core/control/feishu_card_sections.go`
- `internal/adapter/feishu/card_text_blocks.go`
- command config / catalog 的 `SummarySections`
- 各类 button / form / input 的 `plain_text` 字段

新增文本 contract 时，优先沿用这些已有 carrier，而不是重新发明一套裸字符串边界。

## 7. 与文档和流程的关系

如果后续实现改变了这份基线，应在同一变更里同步：

- 本文档
- `AGENTS.md` 中对应的触发规则
- 必要时的 canonical product/state-machine 文档

不要把新的内容语境规则只留在 issue comment 或聊天里。


# Codex Remote Feishu 2.1.1 更新内容

## 这次更新带来的主要变化

### 不支持视觉的模型也能看图了

- 新增 `describe_image` 工具。主模型需要看图时，可以主动把本地图片交给辅助视觉模型分析，再继续完成当前任务。
- 一次最多支持 5 张图片，支持 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 和 Gemini 四种协议。
- 是否调用视觉模型由主模型决定；图片到达时不会自动触发额外推理。
- 管理页新增辅助模型配置，并可以按 Profile 标记主模型是否已经支持直接看图。支持视觉的 Profile 不会注入多余的 `describe_image` 工具。
- 没有配置视觉辅助端点时，系统不会注入一个注定失败的工具；辅助模型的 prompt 也支持自定义，留空时会使用简单的默认描述提示。

### `/access` 和 `/plan` 现在按会话生效了

- 这两个设置不再在 bot 之间共享，而是跟着当前会话走。
- 群聊里的不同会话可以各自切换 access 和 plan，不会因为一个会话改了设置而影响其他会话。
- 会话恢复后会继续沿用自己的设置，bot 上配置的值只作为默认能力。

### OpenCode 和 MCP 更稳了

- OpenCode Profile 的 admission 引用现在会持久化，headless 启动时也会补齐缺失的引用，减少恢复时配置丢失或误用旧 Profile 的情况。
- MCP server 配置改为注入完整的 TOML 表，复杂配置不再只生效一部分。
- OpenCode 权限卡会基于已经跟踪的 ACP tool call 状态补全信息，显示结果更可靠。
- DeepSeek 模型关闭了 search tool，恢复 MCP 工具在请求中的正常处理。

### 辅助模型配置反馈更清楚了

- 辅助模型保存成功后，管理页会明确显示成功提示，不用再靠刷新页面确认结果。
- 默认协议调整为 `openai_chat`，并精简了相关表单文案，配置时更容易理解各字段的作用。

## 更细的更新列表

### 功能

- 新增 profile 级视觉辅助能力和 `describe_image` MCP 工具，支持多图输入、四类模型协议和通用单次推理适配。
- 新增辅助模型配置区域，视觉辅助端点独立于 Claude、Codex 和 OpenCode 的主 Profile。
- `/access` 与 `/plan` 改为会话级设置，支持群聊中的不同会话独立切换。

### 修复

- 修复未配置视觉辅助端点时仍注入 `describe_image`，导致主模型调用后必然失败的问题。
- 修复 `describe_image` prompt 为空时缺少可用默认提示的问题。
- 修复 OpenCode headless 恢复时缺少 Profile admission ref，以及权限卡信息不完整的问题。
- 修复复杂 Codex MCP 配置注入不完整的问题。
- 修复 DeepSeek 请求中 search tool 影响 MCP 工具正常进入请求的问题。
- 修复辅助模型配置保存后没有明确成功反馈的问题。
- 修复 Release From Tracker 在全部任务都被跳过时 workflow 一直停留在 `in_progress` 的问题。

### 兼容性

- 已支持视觉的主模型不会额外注入 `describe_image`；没有视觉能力的 Profile 可以通过辅助模型配置获得按需看图能力。
- 未配置辅助视觉端点时，现有会话不会增加新的失败工具。
- 原有 Codex、Claude、OpenCode 的主模型配置和普通对话入口不需要改变。

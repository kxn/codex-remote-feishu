# Codex Remote Feishu 2.1 更新内容

## 这次更新带来的主要变化

### Goal 现在可以直接在飞书里管理了

- 在当前 Codex 会话里发送 `/goal`，就能查看目标、预算和当前用量。
- 支持用 `/goal new`、`/goal edit` 创建或修改目标，也可以直接暂停、恢复和清除 Goal。
- 当 Goal 正在执行时，普通消息会先进入队列，等 Goal 真正停稳后再继续处理，不会把一条普通消息误送进正在运行的 Goal。
- 队列处理完成后，只有确认 Goal 没有被用户或其他来源改动过，系统才会自动恢复它；状态发生变化时会停下来告诉你，不会擅自覆盖新的目标。

### Claude 和 OpenCode 也有独立的审阅会话了

- Review 会在一个单独的临时会话里执行，不会改绑当前正在使用的主会话。
- 审阅结果、失败原因和后续追问都会回到同一条审阅链路里，审阅完成后再明确回到原会话。
- Feishu 卡片会标明“临时会话 · 审阅”，不再让审阅过程看起来像普通对话，也不会在初始审阅失败后一直停在“工作中”。
- Claude、OpenCode 的审阅都使用受控的只读工具范围，减少审阅过程误改工作区的风险。

### OpenCode 的运行时控制更完整了

- 可以动态切换 OpenCode 的 plan 模式，不用为了改变工作方式重新手动整理会话。
- `/access` 和 `/reasoning` 可以分别调整运行时权限和推理强度；需要重启会话时，系统会自动切到匹配的运行环境。
- 切换 OpenCode Profile 后会启动新的匹配会话，避免继续使用旧 Profile 的配置和状态。
- OpenCode Profile 现在支持更多模型配置，包括 Gemini 类型的 API Profile，并会更准确地展示当前模型支持的推理选项。

### 过程信息终于能说明白自己在做什么了

- Codex、Claude 和 OpenCode 的探索类操作统一成了明确的进度语义。
- 飞书里的过程卡可以区分搜索、读取、分析和其他探索动作，不再把所有过程都显示成一条模糊的工具执行。
- 工具还没有产生可展示内容时不会过早刷出空进度，过程卡和最终结果的衔接更自然。

### 工作区和跨平台使用更稳了

- 群主机器人在 room 还没有绑定 workspace 时发送普通文本，会先保存原消息并打开选择卡；选好 workspace 和 session 后，原消息会继续执行。
- Windows 路径、符号链接和 workspace/cwd 的处理进一步统一，减少切换 Profile、恢复会话和创建 worktree 时找不到工作目录的问题。
- Review 的工作区信息和临时 diff 读取增加了边界限制，复杂或很大的工作区也不会让审阅流程失控。

## 更细的更新列表

### 功能

- Goal 控制面：新增 `/goal` 命令和对应的 Feishu 状态卡，支持查看、创建、编辑、暂停、恢复和清除当前 Codex Goal。
- Goal 队列互锁：普通队列会等待 Goal 完成暂停和 idle 确认后再派发，队列排空后按快照校验结果恢复 Goal，并保留重启后的互锁状态。
- Claude Review Session：支持独立审阅会话、受控审阅上下文、结果回传和后续追问。
- OpenCode Review Session：支持 OpenCode 的 review mode、审阅上下文、只读工具和审阅结果交接。
- OpenCode 运行时控制：新增动态 plan mode、runtime access relaunch 和 reasoning effort 切换，并完善 Profile 切换后的会话重建。
- 统一探索进度：把 Codex、Claude、OpenCode 的探索工具动作映射到同一套过程展示语义。

### 修复

- 修复 Review 初始 turn 失败后没有明确结果、后续文字路由错误，以及审阅完成后没有正确切回原会话的问题。
- 修复 OpenCode Profile 切换后继续复用旧 session、steer 进入不支持的会话，以及空 ACP turn 被误判为成功的问题。
- 修复 OpenCode 工具过程过早显示、探索工具名称丢失、默认 Profile 模型展示不准确和推理选项状态不同步的问题。
- 修复 Feishu 被拒绝的动作提示、detached bot 的 prompt settings，以及共享进度卡在不同来源之间显示不一致的问题。
- 修复 room primary bot 的普通文本无法进入 workspace picker、选择完成后原消息无法继续执行的问题。
- 修复 Windows extended path 污染 workspace/cwd、Profile 切换后工作目录不一致，以及 Review 读取过大 Git 元数据的问题。

### 兼容性

- Codex 的配置入口统一收口到 Profile，旧的 Provider 兼容入口不再作为长期使用路径。
- OpenCode 的权限、plan 和 reasoning 现在会根据当前 Profile、会话能力和实际运行状态分别判断；不支持的配置会明确拒绝，不会静默套用旧状态。
- 不同后端的 Review、探索进度和会话恢复继续共用现有 Feishu 工作流，原有 Codex、Claude 和 OpenCode 的普通对话入口不需要改变。

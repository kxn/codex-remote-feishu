# Codex Remote Feishu 2.0 更新内容

## 这次更新带来的主要变化

### OpenCode 现在可以直接接入飞书了

- 新增 OpenCode 模式，可以在飞书里用 `/mode opencode` 切换到 OpenCode。
- 通过 ACP 接入 OpenCode，会话、过程信息、工具输出和最终结果都能沿用现有的远程工作流。
- 支持 OpenCode 的 MCP、文件输入和会话恢复，使用方式和 Codex / Claude 的远程会话保持一致。
- 在管理页可以维护多套 OpenCode API 配置，支持端点、API Key、模型、推理强度、子代理模型和角色提示词。
- 在飞书里使用 `/opencodeprofile` 就能查看或切换当前 OpenCode Profile；本机默认配置也可以直接使用。

### Profile 配置和运行时隔离更完整了

- OpenCode Profile 会和具体会话绑定，切换配置后不会继续误用旧的运行时环境。
- 使用本机默认 OpenCode 配置时，会沿用 OpenCode 最近使用的模型；如果用户已经显式配置模型，则继续以显式配置为准。
- 配置缺少密钥、版本信息不完整或当前不可用时，会在管理页和飞书里明确显示原因。
- Codex、Claude 和 OpenCode 的 Profile 配置项和模型目录投影更统一，减少切换模式后状态不一致的问题。

### 排队的消息现在更容易看懂了

- 一条消息排队等待时，真正开始执行后会直接回复原消息，告诉你这条消息已经开始处理。
- 排队、恢复和自动继续之间的状态衔接更清楚，减少“已经发出但不知道什么时候开始”的情况。

### 安装和升级过程更稳了

- 修复跨平台升级时服务状态判断不准确的问题，Windows、macOS 和 Linux 的升级辅助进程会按实际运行平台处理。
- 修复升级前状态保存和本地升级服务探测，升级失败或 daemon 重启后更容易恢复到正确状态。
- 修复 OpenCode 运行时配置泄漏、systemd 启动时 PATH 未刷新等问题，减少升级后启动环境不完整的情况。

### 群聊和自动配置的边界更清楚了

- 群聊 room workspace 状态机进一步收口，普通消息、工作区解绑、primary bot 和同群 surface reset 的状态变化更一致。
- 群主机器人还没有绑定 workspace 时，发送普通文本会保存原消息并打开目标选择卡；选定 workspace/session 后会继续执行原消息，其他机器人或图片、文件仍会明确提示先选择 workspace。
- 飞书应用配置检查的权限提示重新整理，缺少权限时更容易知道需要补什么。
- 飞书群信息统一经过 gateway controller 获取，减少不同入口看到的群信息不一致。

## 更细的更新列表

### 功能

- OpenCode ACP 后端：新增 OpenCode headless 运行时、ACP 消息转换、工具调用、MCP、文件输入、会话恢复和统一进度投影，并接入 `/mode opencode`（[7ba8a2b](https://github.com/kxn/codex-remote-feishu/commit/7ba8a2b0), [9d61988](https://github.com/kxn/codex-remote-feishu/commit/9d619886), [4b40fc6](https://github.com/kxn/codex-remote-feishu/commit/4b40fc6), [47214cb](https://github.com/kxn/codex-remote-feishu/commit/47214cb)）
- OpenCode Profile 管理：新增本机默认配置和 API Profile，支持 Web 管理页创建、编辑、删除，以及飞书 `/opencodeprofile` 切换（[e99ff3f](https://github.com/kxn/codex-remote-feishu/commit/e99ff3f), [9d61988](https://github.com/kxn/codex-remote-feishu/commit/9d61988), [47214cb](https://github.com/kxn/codex-remote-feishu/commit/47214cb)）
- 排队消息开始执行反馈：任务真正开始时回复对应的飞书消息，并保留原始消息预览（[f43a5de](https://github.com/kxn/codex-remote-feishu/commit/f43a5de)）

### 修复

- OpenCode 运行稳定性：修复工具输出不可见、进度投影不一致、API Profile 配置串用和 ACP 文件写入边界问题（[96cad9a](https://github.com/kxn/codex-remote-feishu/commit/96cad9a), [9dd2570](https://github.com/kxn/codex-remote-feishu/commit/9dd2570), [149d01f](https://github.com/kxn/codex-remote-feishu/commit/149d01f), [8ac2d57](https://github.com/kxn/codex-remote-feishu/commit/8ac2d57)）
- OpenCode 默认 Profile：修复本机默认配置没有继承系统最近模型的问题，并按当前 headless 运行时隔离 OpenCode 的配置、数据和状态目录（[6ba60d3](https://github.com/kxn/codex-remote-feishu/commit/6ba60d3)）
- 安装升级状态：修复首次安装服务单元创建、升级前状态保存、跨平台服务状态判断、升级辅助进程启动和本地升级探测（[b73eb67](https://github.com/kxn/codex-remote-feishu/commit/b73eb67), [bcaaa6d](https://github.com/kxn/codex-remote-feishu/commit/bcaaa6d), [b2e3d08](https://github.com/kxn/codex-remote-feishu/commit/b2e3d08), [c4e1d40](https://github.com/kxn/codex-remote-feishu/commit/c4e1d40), [b25bea7](https://github.com/kxn/codex-remote-feishu/commit/b25bea7)）
- 群聊和恢复稳定性：修复 room workspace 状态收口、detach 后恢复竞态、Profile 切换后的 headless 实例收敛，以及未绑定 / 选择目标时的消息丢失（[178031a](https://github.com/kxn/codex-remote-feishu/commit/178031a), [3e68759](https://github.com/kxn/codex-remote-feishu/commit/3e68759), [51c9c84](https://github.com/kxn/codex-remote-feishu/commit/51c9c84), [f18f7b0](https://github.com/kxn/codex-remote-feishu/commit/f18f7b0)）
- 群主文本 picker 交接：修复 room 尚未绑定 workspace 时，primary bot 的普通文本无法打开 target picker、原消息无法在选择后 replay 的问题；非 primary bot 仍保持 workspace gate（[90c7980](https://github.com/kxn/codex-remote-feishu/commit/90c7980)）
- 外部访问和启动环境：加固网络模式的配置兼容和服务重载，修复 systemd 启动时 PATH 刷新以及访问配置更新后的运行时状态（[2cbe128](https://github.com/kxn/codex-remote-feishu/commit/2cbe128), [52892ba](https://github.com/kxn/codex-remote-feishu/commit/52892ba), [0cd5994](https://github.com/kxn/codex-remote-feishu/commit/0cd5994)）
- 飞书配置和路由：统一权限检查提示，修复群信息获取入口不一致和自动配置流程中的提示状态（[0f382c1](https://github.com/kxn/codex-remote-feishu/commit/0f382c1), [e39aa01](https://github.com/kxn/codex-remote-feishu/commit/e39aa01), [a4e545b](https://github.com/kxn/codex-remote-feishu/commit/a4e545b)）

### 兼容性

- 补充 OpenCode ACP 的真实会话和跨平台路径测试，修复 Windows 路径转义、macOS 符号链接和 ACP golden 文件换行差异。
- 统一 daemon、wrapper 和升级辅助进程对运行平台、服务状态和工作目录的判断，减少不同系统下行为不一致。

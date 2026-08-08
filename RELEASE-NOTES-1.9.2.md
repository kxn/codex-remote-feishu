# Codex Remote Feishu 1.9.2 更新内容

## 这次更新带来的主要变化

### 群聊工作区和会话恢复更可靠了

- 把机器人拉进群后，满足条件时就能自动开始工作，不需要再手动设置一堆关联关系。
- 群里的机器人会围绕同一个工作目录协同工作；还没有选择工作目录时，会明确告诉你下一步该怎么做。
- 选择工作目录时可以直接创建新的独立副本，适合同时处理同一个项目的不同任务。
- 修复了切换工作目录、重新接上之前的会话，以及切换模型配置后偶尔状态不一致的问题。

### 外部访问配置更完整了

- 管理页现在可以更直观地选择服务只在本机使用，还是允许局域网内的其他设备访问。
- 配置不完整或升级后有旧配置时，会给出更明确的处理结果，减少服务无法启动或意外开放访问的情况。

### MiMo Profile 更安全了

- MiMo 配置会使用适合它的模型和能力设置，不会被其他模型配置意外影响。
- 默认不会给 MiMo 开启当前不适合的联网搜索和自动修改文件能力，使用时更稳妥。

## 更细的更新列表

### 功能

- 新增 Feishu 群聊机器人进群时的 primary bot 自动 bootstrap，并补齐群信息、权限和事件处理（[fd947a26](https://github.com/kxn/codex-remote-feishu/commit/fd947a26)）
- 工作区选择器新增 Git worktree 创建流程，支持基准工作区、分支和目标目录的完整选择与校验（[6b3aa292](https://github.com/kxn/codex-remote-feishu/commit/6b3aa292)）
- 新增 external access 网络模式配置，并接入管理页、配置迁移和 web preview（[b94fca4d](https://github.com/kxn/codex-remote-feishu/commit/b94fca4d)）

### 修复

- Profile 切换后，同 bot 其他群的 headless 实例按新的 Profile 合同收敛，避免继续复用旧运行时（[44323d2c](https://github.com/kxn/codex-remote-feishu/commit/44323d2c)）
- 修复 detach 后恢复流程的竞态，避免旧状态重新接管当前 surface（[18f374c0](https://github.com/kxn/codex-remote-feishu/commit/18f374c0)）
- 收口群聊 room workspace 状态机：普通消息、workspace detach、primary bot、同群 surface reset 使用一致的状态和持久化顺序（[59b6e105](https://github.com/kxn/codex-remote-feishu/commit/59b6e105)）
- 加固 external access 网络模式的配置解析、默认值和升级兼容（[fc04168e](https://github.com/kxn/codex-remote-feishu/commit/fc04168e)）
- 修复 Codex Profile managed catalog overrides，统一 Profile、模型目录和 resume policy 的投影（[8d0eaa2f](https://github.com/kxn/codex-remote-feishu/commit/8d0eaa2f)）
- MiMo Profile 默认关闭 web search 和 freeform apply patch（[9734826a](https://github.com/kxn/codex-remote-feishu/commit/9734826a), [3ead9ed9](https://github.com/kxn/codex-remote-feishu/commit/3ead9ed9)）

### 测试和兼容性

- 补充 Profile contract reconcile、room workspace、primary bootstrap、worktree picker、detach resume 和 external access 的回归测试。
- 修复 macOS 符号链接和跨平台临时目录下的测试路径差异（[383449a2](https://github.com/kxn/codex-remote-feishu/commit/383449a2), [29488330](https://github.com/kxn/codex-remote-feishu/commit/29488330)）
- 统一 room workspace 路径断言，避免不同平台路径格式导致测试误报（[f5bdb263](https://github.com/kxn/codex-remote-feishu/commit/f5bdb263)）

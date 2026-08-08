# OpenCode Backend 风险与降级讨论

> Type: `draft`
> Updated: `2026-08-09`
> Summary: 从 OpenCode backend 完整评估中摘出需要产品/架构讨论的风险、降级和决策点。

## 1. 结论

OpenCode 可以接，但不是“实现 ACP adapter 就完事”。下面这些点需要先定产品和架构取舍；其他已验证可完整支持的能力不在本文重复。

推荐默认策略：

- 第一版按 `API key + 本地 profile overlay + ACP runtime adapter` 做。
- 不承诺 OAuth 完全隔离、OS sandbox、结构化 Plan、历史会话删除、未知 slash command 透传。
- 所有会误导用户的能力必须在启动前或发送前给明确诊断，不能静默降级。

## 2. 必须讨论

### 2.1 Profile 与 OAuth 隔离

风险：

- `OPENCODE_CONFIG_CONTENT` 和 `OPENCODE_AUTH_CONTENT` 能支持 API key 临时覆盖，但 MCP OAuth / provider OAuth 可能写 `mcp-auth.json` 或共享系统登录态。
- 如果用户以为“每个 profile 都完全隔离”，OAuth 场景会误导。

推荐方案：

- 第一版只把 API-key profile 定义为 fully supported。
- OAuth profile 分两种显式模式：
  - `shared-system-auth`：继承系统登录态，明确提示不是隔离 profile。
  - `isolated-xdg-auth`：启动时使用独立 XDG data/config/cache root，OAuth 文件只落临时/实例目录。
- MCP OAuth 默认不启用；需要单独产品开关和单独测试。

需要拍板：

- 第一版是否允许共享系统 OAuth 登录态？
- 如果允许，UI/配置里是否必须显示“共享系统登录态”？

### 2.2 Sandbox 语义

风险：

- OpenCode 没有等价 Codex/Claude 的 OS/container sandbox profile。
- 用 permission / external directory 近似 sandbox 会造成安全语义误导。

推荐方案：

- profile schema 拆成三类：`fileAccess`、`commandPermission`、`networkPolicy`。
- OpenCode 第一版只支持 permission/file-access 近似能力。
- 如果用户选择“必须 sandbox”，OpenCode backend 直接启动前失败，提示该 backend 不支持强 sandbox。

需要拍板：

- OpenCode profile 是否允许创建“无强 sandbox”的实例？
- 哪些入口需要阻止用户把它误配置成强隔离运行？

### 2.3 Plan 语义

风险：

- OpenCode `mode=plan` 只是 session mode，不是 Codex/Claude 那种结构化 `TurnPlanSnapshot`。
- 如果合成假 Plan，会破坏现有 Plan UI 和确认流程的可信度。

推荐方案：

- `mode=plan` 只显示为 backend session mode。
- OpenCode 通过 todo/plan-file/tool 输出的计划，只作为普通消息或工具结果展示。
- 不发送结构化 `TurnPlanSnapshot`，Plan UI 显示“该 backend 不提供结构化计划快照”。

需要拍板：

- OpenCode plan mode 是否要进入现有 Plan UI，还是只在普通对话中展示？

### 2.4 会话关闭与删除

风险：

- `session/close` 只关闭 ACP 内存状态并 abort backing session；黑盒验证显示历史 `session/list` 仍能看到该 session。
- 如果映射成“删除会话”，用户会误以为历史被清掉。

推荐方案：

- OpenCode `close` 只映射为 detach/stop active runtime。
- OpenCode 第一版不提供 persistent delete。
- UI/API 文案避免“删除”，用“停止/分离/关闭运行中实例”。

需要拍板：

- 产品上是否接受 OpenCode backend 暂无删除历史会话？

### 2.5 Slash Command 兼容

风险：

- `/review` 支持，但会启动 task/sub-session，和现有 Codex/Claude review surface 不同。
- `/compact` 支持，但走 summarize/compaction，prompt response 可能无 usage。
- `/patch`、`/auto-continue`、`/auto-whip`、`/sendfile` 在 OpenCode 中会被空吞，返回 `end_turn` 但没有实际动作。

推荐方案：

- command router 发送前预检：
  - 放行 OpenCode `available_commands_update` 里的命令。
  - 额外放行 `/compact`。
  - 未知命令直接返回产品诊断，不发送给 OpenCode。
- `/review` 标注 `backendShape=task_summary`，不要承诺与现有 review 字段同构。
- `/compact` 允许无 usage 完成。

需要拍板：

- `/review` 是否先作为 OpenCode 专属弱兼容能力上线？
- 未知命令是直接报错，还是提示“该 backend 不支持，可切换 Codex/Claude”？

### 2.6 MCP OAuth 与失败诊断

风险：

- local stdio MCP 已验证可用；MCP OAuth 未验证且可能写共享 auth 文件。
- MCP 注册失败时 OpenCode 可能只在日志里有 `server unavailable`，之后模型调用会变成 `invalid tool`，用户看到时已经太晚。

推荐方案：

- 第一版支持 local stdio MCP 和非 OAuth remote MCP。
- MCP OAuth 默认 gate 掉。
- `session/new` 后主动做 MCP 状态/工具可见性检查；失败时立刻返回 warning，不等模型调用。
- MCP headers/env 必须做 redaction。

需要拍板：

- 第一版是否允许 remote MCP，但禁用 OAuth？
- MCP 注册失败是阻止实例启动，还是允许启动但在会话顶部显示 warning？

### 2.7 Usage 与计费展示

风险：

- prompt response usage 是 turn-level；`usage_update` 是 context/cumulative meter，两者不能互相覆盖。
- OpenCode 会 normalize provider usage，例如 cached/reasoning 会影响 input/output 拆分。

推荐方案：

- UI/API 分开展示：
  - turn usage：来自 prompt response。
  - context meter：来自 `usage_update`。
- 不用 `usage_update` 回填当前 turn token。

需要拍板：

- 现有 UI 是否能容纳“turn usage”和“context meter”两套数字？

### 2.8 Error Taxonomy

风险：

- OpenCode 有些错误是粗粒度 `-32603 OpenCode service failure`。
- missing session、MCP failure、auth-required、invalid model 需要不同产品动作。

推荐方案：

- adapter 做 error normalizer：JSON-RPC code + method + safeMessage + stderr/log pattern -> canonical error。
- raw frame 保留到 debug trace。
- 产品面只展示归一化后的可行动错误。

需要拍板：

- 对无法归一化的 OpenCode service failure，默认显示给用户还是只进入 debug trace？

## 3. 实现顺序建议

1. 先做不会误导安全语义的部分：profile compiler、secret redaction、sandbox-required fail-fast、error normalizer。
2. 再做 ACP runtime adapter：turn buffer、load hydration、tool/permission/usage mapping。
3. 再做 command router：`/review`、`/compact`、未知 slash command 预检。
4. 最后决定 OAuth / MCP OAuth / isolated XDG 是否进入第一版。

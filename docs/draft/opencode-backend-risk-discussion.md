# OpenCode Backend 风险与降级讨论

> Type: `draft`
> Updated: `2026-08-09`
> Summary: 按 Claude 现有实现基线重新校准 OpenCode backend 需要产品/架构讨论的风险、降级和决策点。

## 1. 结论

OpenCode 可以接，但风险清单不能按 Codex 的完整能力硬套。对照当前 Claude backend 后，很多能力已经是“backend-specific 近似”或“显式不支持”，OpenCode 第一版也应采用同一标准：能明确投影就投影，不能投影就通过 backend capability / command profile 显式隐藏或拒绝。

真正需要讨论的不是“OpenCode 是否补齐 Codex 全量语义”，而是下面几类会误导用户或破坏现有契约的差异：

- profile/auth 是否隔离；
- 权限模式是否被误读为强 sandbox；
- Plan 是否有可信来源可投影；
- ACP runtime close 是否被误读为历史删除；
- MCP/OAuth 失败是否能在发送前或启动后尽早诊断；
- usage/error 是否能按现有事件契约给出可行动信息。

推荐默认策略：

- 第一版按 `API key + 本地 profile overlay + ACP runtime adapter + backend capability profile` 做。
- 不承诺 OAuth 完全隔离、OS/container sandbox、历史会话删除、未知 slash command 透传。
- Plan、usage、error 允许像 Claude 一样做 adapter synthesis，但必须有后端事件或工具结果作为来源；禁止凭普通文本猜测。
- 所有会误导用户的能力必须在启动前、发送前或会话顶部给明确诊断，不能静默降级。

## 2. Claude 基线校准

| 能力面 | Claude 现状 | 对 OpenCode 的校准结论 |
| --- | --- | --- |
| Profile / auth | 自定义 profile 继承系统环境，只移除并覆盖 `ANTHROPIC_*`、subagent model、reasoning、追加 instruction 等有限 env；默认 profile 是 inherit。没有通用配置目录/OAuth 完全隔离抽象。 | OpenCode 不需要实现通用 loader 抽象；只需声明 API key overlay 支持边界。OAuth / MCP OAuth 是单独风险。 |
| 权限 / sandbox | Claude 把 native `default`、`acceptEdits`、`plan`、`bypassPermissions` 投影到 access/plan；不是 OS/container sandbox。 | OpenCode 不应被要求补齐 Codex sandbox，但必须避免把 permission/file access 近似能力说成强隔离。 |
| Plan | Claude 有两条来源：`ExitPlanMode` control request 触发确认；`TodoWrite` tool result 投影 `TurnPlanSnapshot`。缺少 plan body 时只用固定兜底文案，不从普通文本猜。 | OpenCode 若有 todo/tool/ACP 事件可投影，就可以支持结构化 Plan；若只有 `mode=plan` 或普通文本，则只能显示 backend mode/普通消息。 |
| 命令兼容 | Claude command profile 已隐藏/拒绝 `/compact`、`/review`、`/patch`、`/auto-continue`、`/auto-whip` 等多项 Codex 命令；部分 `/new`、workspace list、steer all 是 approximation。 | OpenCode 也应做 backend command profile。未知或未验证命令发送前拒绝，不作为阻塞项。 |
| 历史会话 | Claude catalog 从本地 session store/list 读取，只有 recent/list/thread lookup 语义，没有在 catalog 层提供删除历史。 | OpenCode `session/close` 不能映射成删除；但“无 persistent delete”不是比 Claude 更差的阻塞项。 |
| Usage | Claude 从 result `usage` 合成 last/total token usage，并用 `modelUsage.contextWindow` 补 context window；这是投影，不是统一原生账单语义。 | OpenCode usage_update / prompt usage 也可投影成现有 usage 事件；需要标清 turn usage 与 context meter 来源。 |
| Error | Claude 当前失败主要归一到 `claude_turn_failed`，details 带原始 errors；不是细粒度完整 taxonomy。 | OpenCode 不必一开始细分所有错误，但 auth/session/MCP/model 这类会影响下一步操作的错误要优先 normalizer。 |

## 3. 仍需讨论的风险

### 3.1 Profile 与 OAuth 隔离

为什么仍需讨论：

- Claude 允许 inherit，所以“不是完全隔离”本身不是新 backend blocker。
- OpenCode 的特殊风险是 `OPENCODE_CONFIG_CONTENT`、`OPENCODE_AUTH_CONTENT` 可以覆盖 API key，但 OAuth provider / MCP OAuth 可能落到 `mcp-auth.json` 或系统登录态；用户容易误解为 per-profile 隔离。

推荐方案：

- 第一版只把 API-key profile 定义为 fully supported。
- OAuth profile 分两种显式模式：
  - `shared-system-auth`：继承系统登录态，明确提示不是隔离 profile。
  - `isolated-xdg-auth`：启动时使用独立 XDG data/config/cache root，OAuth 文件只落实例目录。
- MCP OAuth 默认 gate 掉；单独测试通过后再开放。

需要拍板：

- 第一版是否允许共享系统 OAuth 登录态？
- 如果允许，UI/配置里是否必须显示“共享系统登录态”？

### 3.2 权限模式与 Sandbox 文案

为什么仍需讨论：

- Claude 也只是 permission mode 投影，不提供 Codex 式强 sandbox；所以 OpenCode 不支持 OS/container sandbox 不是独有缺口。
- 风险在产品文案：如果用户选择“强 sandbox”，OpenCode 只能做 permission/file-access 近似时会造成安全误导。

推荐方案：

- backend capability 拆成 `fileAccess`、`commandPermission`、`networkPolicy`、`strongSandbox`。
- OpenCode 第一版只声明 permission/file-access 能力；`strongSandbox=false`。
- 如果 profile 要求强 sandbox，OpenCode 启动前 fail-fast。

需要拍板：

- OpenCode 是否允许创建“无强 sandbox”的实例？
- 哪些入口需要阻止用户把它误配置成强隔离运行？

### 3.3 Plan 投影来源

为什么仍需讨论：

- Claude 的 Plan 并不是单一原生字段，而是从 `ExitPlanMode` 和 `TodoWrite` 两类可信事件投影。
- OpenCode `mode=plan` 本身只说明 session mode；不能直接等价 `TurnPlanSnapshot`。

推荐方案：

- OpenCode adapter 先验证是否存在稳定 todo/tool/event 来源。
- 有可信结构化来源时，按 Claude `TodoWrite -> TurnPlanSnapshot` 的方式投影。
- 只有 `mode=plan` 或普通文本时，不进入结构化 Plan UI；只显示 backend mode 或普通消息。

需要拍板：

- 如果 OpenCode 没有可信结构化来源，是否接受 Plan UI 显示“不提供结构化计划快照”？

### 3.4 ACP Close 与历史删除

为什么仍需讨论：

- Claude 也没有统一 persistent delete，所以 OpenCode 不需要为了对齐 Claude 去实现历史删除。
- 但 ACP `session/close` 黑盒语义更像关闭 runtime/abort backing session，历史 `session/list` 仍可能看到该 session；如果映射成“删除”会误导。

推荐方案：

- OpenCode `close` 只映射为 stop/detach active runtime。
- 第一版不提供 persistent delete。
- UI/API 文案避免“删除”，使用“停止/分离/关闭运行中实例”。

需要拍板：

- 产品上是否接受 OpenCode backend 暂无删除历史会话？

### 3.5 MCP OAuth 与失败诊断

为什么仍需讨论：

- Claude 当前 profile/env 覆盖没有通用 MCP 注入抽象，OpenCode 这块必然 backend-specific。
- OpenCode local stdio MCP 已验证可用；OAuth remote MCP 可能引入共享 auth 文件和延迟失败。

推荐方案：

- 第一版支持 local stdio MCP 和非 OAuth remote MCP。
- MCP OAuth 默认 gate 掉。
- `session/new` 后主动做 MCP 状态/工具可见性检查；失败时立刻返回 warning，不等模型调用变成 `invalid tool`。
- MCP headers/env 必须做 redaction。

需要拍板：

- 第一版是否允许 remote MCP，但禁用 OAuth？
- MCP 注册失败是阻止实例启动，还是允许启动但在会话顶部显示 warning？

### 3.6 Usage 与错误归一化

为什么仍需讨论：

- Claude usage/error 也是 adapter 投影：usage 从 result 累加，error 多数先归到 `claude_turn_failed`。
- OpenCode 的差异是 ACP 可能同时有 prompt response usage 与 `usage_update` context/cumulative meter，错误也可能是粗粒度 JSON-RPC `-32603`。

推荐方案：

- usage 分两类来源：
  - turn usage：来自 prompt response 或可归因于当前 turn 的完成事件。
  - context meter：来自 `usage_update`，不回填当前 turn token。
- error normalizer 第一版只优先覆盖可行动错误：auth-required、missing session、invalid model、MCP unavailable、permission denied。
- 其他 OpenCode service failure 保留 raw frame/debug trace，并展示保守通用错误。

需要拍板：

- UI 是否需要同时展示 turn usage 与 context meter？
- 无法归一化的 service failure 默认展示给用户，还是只进入 debug trace？

## 4. 从原风险清单降级的项

- `/review`、`/compact`、`/patch`、`/auto-continue`、`/auto-whip`：按 Claude command profile 的先例，属于 backend capability/command profile 问题，不是 OpenCode 接入 blocker。
- persistent delete：Claude catalog 也没有统一删除语义，OpenCode 只需避免把 close 说成 delete。
- 完整 error taxonomy：Claude 也没有完整细粒度分类；OpenCode 第一版只要求关键可行动错误归一化。
- Codex 强 sandbox：Claude 未对齐 Codex 强 sandbox，OpenCode 也不需要补齐；需要的是 capability 文案和 fail-fast。

## 5. 实现顺序建议

1. 先做 profile compiler、secret redaction、backend capability profile、strong sandbox fail-fast。
2. 再做 ACP runtime adapter：turn buffer、load hydration、tool/permission/usage/error mapping。
3. 再做 command profile：可见/可发/近似/拒绝与诊断文案。
4. 再验证 Plan 可信来源：有结构化来源才投影 `TurnPlanSnapshot`。
5. 最后决定 OAuth / MCP OAuth / isolated XDG 是否进入第一版。

# OpenCode Backend 接入评估与黑盒测试计划

> Type: `draft`
> Updated: `2026-08-09`
> Summary: 锁定 OpenCode v1.18.15，按 backend integration playbook 完成接入评估、映射审计与真实黑盒测试，形成接入结论。

## 1. 结论

- Verdict: `candidate with known gaps`，可以进入 OpenCode ACP backend 详细设计与实现。
- 推荐接入阶段：实现 production-oriented adapter/compiler 的第一版，不做只证明可启动的最小 POC。
- 锁定版本：OpenCode `v1.18.15`。
- Release: `https://github.com/anomalyco/opencode/releases/tag/v1.18.15`
- Release published: `2026-08-07T06:49:55Z`
- Release targetCommitish: `325529761beb79a004de6d86e48b8db69cf4eba3`
- 本地源码 tag commit: `d7b115f623760e68a4749d16508a9eca350f246f`，commit message `release: v1.18.15`
- NPM package: `opencode-ai@1.18.15`，bin `opencode`。

当前结论：

- ACP 协议面足以承载我们的核心 backend lifecycle：initialize、session new/list/load/resume/fork/close、prompt/cancel、stream delta、tool lifecycle、permission request、usage、MCP injection 均已用 `opencode-ai@1.18.15` binary 黑盒验证。
- profile overlay 可以实现“少字段覆盖，其余继承”：默认建议用 `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT`，必要时配合临时 XDG；完整设计前还需要补一组黑盒验证，确认系统已有 OAuth 时显式 API profile 仍稳定使用 overlay auth/baseURL/model。
- 仍需我们侧单独实现 loader/profile compiler。ACP 只能抽象运行期协议，不能抽象 OpenCode 的配置、auth、MCP OAuth、模型目录、权限 schema 和产品命令差异。
- 主要缺口是语义退化而非不可接入：OpenCode 没有 Codex 式 sandbox profile、persistent delete、独立 thread/turn API、未知 slash command 的显式错误；Plan/usage/error 等能力应比照 Claude 做 adapter 侧承接和投影，不能把内部 carrier 差异直接暴露成用户可见“不支持”。

## 2. 调研对象

源码基线：

- Repo: `https://github.com/anomalyco/opencode`
- Tag: `v1.18.15`
- 本地源码目录：`/tmp/opencode-v1.18.15-zgic0k`
- 官方 ACP 文档：`https://opencode.ai/docs/acp/`

关键源码路径：

- ACP CLI: `packages/opencode/src/cli/cmd/acp.ts`
- ACP adapter: `packages/opencode/src/acp/agent.ts`
- ACP service: `packages/opencode/src/acp/service.ts`
- ACP session state: `packages/opencode/src/acp/session.ts`
- ACP event bridge: `packages/opencode/src/acp/event.ts`
- ACP config options: `packages/opencode/src/acp/config-option.ts`
- ACP permission bridge: `packages/opencode/src/acp/permission.ts`
- ACP tool taxonomy: `packages/opencode/src/acp/tool.ts`
- ACP usage: `packages/opencode/src/acp/usage.ts`
- Config loader: `packages/opencode/src/config/config.ts`
- Config paths: `packages/opencode/src/config/paths.ts`
- Auth store/env override: `packages/opencode/src/auth/index.ts`
- Global paths/env flags: `packages/core/src/global.ts`、`packages/core/src/flag/flag.ts`

## 3. 能力矩阵

| 能力 | 等级 | 源码证据 | 当前结论 | 必测点 |
| --- | --- | --- | --- | --- |
| ACP entrypoint | required | `AcpCommand` runs `opencode acp` over `ndJsonStream` | source supported | 真实 stdio handshake |
| initialize capabilities | required | `service.initialize` reports load/list/resume/fork/close, MCP http/sse, embeddedContext/image | source supported | 返回字段和 client capabilities |
| session new/load/list/resume/close/fork | required | `newSession`、`loadSession`、`listSessions`、`resumeSession`、`closeSession`、`forkSession` | source supported | daemon restart、process restart、cwd filter、replay |
| prompt/cancel | required | `prompt` calls `sdk.session.prompt` or `sdk.session.command`; `cancel` aborts backing session | source supported | cancel late event、stopReason、queue cleanup |
| model/effort/mode config options | required | `setSessionConfigOption` supports `model`、`effort`、`mode` | source supported | runtime set 是否影响下一 turn，失败是否半应用 |
| profile overlay | required | `OPENCODE_CONFIG`、`OPENCODE_CONFIG_DIR`、`OPENCODE_CONFIG_CONTENT`、merge order | supported with caveats | 用 inline content + auth content；不要默认用 config dir |
| auth isolation | required | `OPENCODE_AUTH_CONTENT` bypasses auth file read | supported for API auth | secret 不落盘 smoke 通过；OAuth 另算 |
| MCP publication | best_effort | `registerMcpServers` maps ACP MCP servers to OpenCode `sdk.mcp.add` | supported for local MCP | local stdio MCP list/call 通过；OAuth 未覆盖 |
| permission request | required | `ACPEvent` handles `permission.asked`; `ACPPermission.Handler` calls `requestPermission` | supported | allow once/always/reject、edit diff、fail-closed 源码成立 |
| tool taxonomy | required | `toToolKind` maps bash/read/edit/grep/task/etc. | supported with approximation | MCP tool kind 为 `other`；read/bash/edit 映射通过 |
| assistant/reasoning stream | required | `message.part.delta` maps text to `agent_message_chunk` and reasoning to `agent_thought_chunk` | supported | live delta 与 load replay 形态不同 |
| file changes | required | edit tools become ACP `edit` with diff content where possible | supported for edit/write style diffs | edit diff + `fs/write_text_file` 通过 |
| token usage | best_effort | `UsageService.buildUsage` maps tokens/cache/reasoning | supported | prompt response usage + cumulative `usage_update` 均有 |
| model catalog | required | directory snapshot uses `sdk.config.providers` | source supported | provider/model list, variants, hidden/disabled providers |
| review command | best_effort | `available_commands_update` exposes `review`; slash command calls `sdk.session.command` | supported with different shape | 会启动 task/sub-session，再把 task result 汇总 |
| compact command | best_effort | slash `/compact` calls `sdk.session.summarize` | supported with caveat | prompt response 无 usage，输出来自 compaction/title |
| turn patch / patch rollback | best_effort | no ACP-specific turn patch API found | unsupported | `/patch` 不是产品命令，只是 tool kind 兼容名 |
| workspace defaults/profile snapshots | required | no OpenCode-specific equivalent | our-side only | 由我们侧 profile snapshot/compiler 实现 |

## 4. 接入地图差异

### 4.1 Backend Identity

需要新增 backend identity，不能复用 Codex 默认值。

待设计：

- `agentproto.BackendOpenCode` 或更通用 ACP vendor identity。
- hello 必须携带 backend `opencode`、version、ACP capability summary。
- unknown backend 不允许继续默认进 Codex。

测试：

- backend normalize/display/capability table。
- wrapper hello round-trip。
- command catalog backend 不误标 Codex/Claude。

### 4.2 Entrypoint / Launcher

OpenCode 入口是 `opencode acp`。源码 `packages/opencode/src/cli/cmd/acp.ts` 会：

- 设置 `process.env.OPENCODE_CLIENT = "acp"`。
- 启动 OpenCode internal server。
- 使用 `@agentclientprotocol/sdk` 的 `ndJsonStream` 在 stdin/stdout 上跑 ACP JSON-RPC。

待设计：

- 新 wrapper mode，例如 `opencode-acp` 或 `acp-app-server --backend=opencode`。
- child binary 解析：优先显式 env，再 PATH，再 admin/runtime requirements。
- ACP wrapper 不应假设 Codex app-server JSON-RPC。

黑盒测试：

- missing binary。
- wrong binary。
- `opencode acp --cwd <repo>` initialize。
- stdout 只有 ACP frame，stderr/log 不污染 stdout。
- server auth/internal port 是否产生全局状态。

### 4.3 Profile Catalog / Loader

OpenCode 没有直接等价 Codex/Claude profile catalog。候选机制来自配置 overlay：

- `OPENCODE_CONFIG`：额外读取一个指定 config file。
- `OPENCODE_CONFIG_DIR`：额外读取目录下 `opencode.json` / `opencode.jsonc`，并且 `Global.Path.config` 会变为该目录。
- `OPENCODE_CONFIG_CONTENT`：最后加载的 virtual local config。
- `OPENCODE_AUTH_CONTENT`：auth 读取优先使用 env JSON。
- `OPENCODE_DISABLE_PROJECT_CONFIG`：禁止 project config。
- `OPENCODE_PERMISSION`：以 JSON 追加/覆盖 permission 配置。

源码 merge 顺序来自 `packages/opencode/src/config/config.ts`：

1. remote/well-known config。
2. global config。
3. `OPENCODE_CONFIG`。
4. project `opencode.json[c]`，除非 `OPENCODE_DISABLE_PROJECT_CONFIG`。
5. discovered `.opencode` and `OPENCODE_CONFIG_DIR`。
6. `OPENCODE_CONFIG_CONTENT`。
7. active account remote config / managed config / managed preferences。
8. `OPENCODE_PERMISSION`。

初步设计：

- profile compiler 生成 `OPENCODE_CONFIG_CONTENT` 用于字段级覆盖。
- auth override 用 `OPENCODE_AUTH_CONTENT`。
- 不优先用 `OPENCODE_CONFIG_DIR`，因为它会改变 `Global.Path.config`，容易失去用户系统 config 继承语义。
- `OPENCODE_CONFIG_DIR` 可作为额外目录覆盖配置，但会在该目录写 `.gitignore`，并可能成为后续 config 写入目标；默认临时 profile 不使用它。
- project config 默认继承；按 profile intent 可设置 `OPENCODE_DISABLE_PROJECT_CONFIG=1`。
- MCP OAuth 不进入第一版 profile 管理范围；源码 `packages/opencode/src/mcp/auth.ts` 使用 `Global.Path.data/mcp-auth.json`，只作为系统 inherit/OAuth 的已知限制记录。
- sandbox 没有 OpenCode 同名或等价 OS/container 隔离字段；按 Claude 基线，这不是产品拍板项，只能在 adapter/debug 侧记录权限/目录近似语义。

已黑盒证明：

- `OPENCODE_CONFIG_CONTENT` 最后覆盖 global/project。
- `OPENCODE_AUTH_CONTENT` 注入 API key 时不写 `auth.json`，临时目录扫描未发现 secret。
- `OPENCODE_CONFIG_DIR` 会替换 config path 并写 `.gitignore`；只作为隔离模式，不作为默认 profile overlay。
- `/config` 与 `/global/config` API 会写 project/global config，接入层不得用它们实现临时 profile。
- `instructions` 和 `plugin` merge 有特殊 concat/dedupe 行为，要单独测，不能按普通 object override 推断。

### 4.4 Runtime Protocol Adapter

OpenCode ACP raw update 类型和我们 `agentproto` 不同：

- `agent_message_chunk`
- `agent_thought_chunk`
- `user_message_chunk`
- `tool_call`
- `tool_call_update`
- `available_commands_update`
- usage fields in prompt response and session update

需要新增 ACP adapter 层：

- ACP request/response client。
- ACP session update subscription -> `agentproto.Event`。
- deterministic synthetic IDs for item/turn where ACP event lacks enough IDs。
- replay/hydration 标记策略。

核心风险：

- `runUntilIdle` 等待 `session.status=idle`，需要证明 prompt response 和 event stream 顺序稳定。
- replay from `loadSession` / `forkSession` 会发历史 chunks，必须标记为 hydration，不能刷 Feishu 主消息。
- tool update 可能先有 `tool_call` 再有 repeated running snapshots，需要去重。
- prompt response stopReason 不等于完整 final assistant text。
- OpenCode ACP 没有独立 `thread/*` / `turn/*` 方法或 turn lifecycle event；canonical `TurnID` 需要由我们侧 dispatch correlation 或额外 runtime evidence 生成。
- JSON-RPC request id、OpenCode permission id、tool `callID`、message id 不能混用。
- OpenCode `mode=plan` 是 session mode/config option，不是 ACP plan snapshot；应比照 Claude 的产品语义承接计划文本、确认请求或 todo/tool 事件，不从普通 mode 字段硬造 `agentproto.TurnPlanSnapshot`。

### 4.5 Surface Resume / Thread Catalog

OpenCode ACP 的 `ACPSession` 是内存 state，但 service 会通过 SDK 查询真实 sessions：

- `listSessions` calls `sdk.session.list({ directory, roots: true })`。
- `loadSession` / `resumeSession` calls `sdk.session.get` and `sdk.session.messages`。
- live ACP sessions 会和 server-backed sessions 合并。
- 持久化主存储是 SQLite `Global.Path.data/opencode.db`，底层 session metadata 包含 directory/workspace/project/parent/title/model/tokens/cost/timestamps。
- `closeSession` 只删除 ACP 进程内 session state 并 best-effort abort backing session，不等价删除持久化 session。

必须测试：

- `opencode acp` 进程重启后，旧 session 是否仍可 `listSessions` / `resumeSession`。
- `cwd` 是否是 resume 必填。
- `loadSession` replay 是否包括 user/tool/assistant/reasoning。
- session title、updatedAt、cwd 是否足够 `/list` 和 workspace picker。

### 4.6 Request / Permission

`ACPPermission.Handler` 支持：

- option id `once`、`always`、`reject`。
- missing `requestPermission` callback 时自动 reject。
- edit permission 会尝试 `writeTextFile` 发送 proposed edit。

适配风险：

- 我们 Feishu request option 需要 round-trip ACP optionId。
- `always` 的 scope 是 OpenCode native 语义，不一定等价我们 session/workspace grant。
- edit diff 内容可能要从 permission metadata 和 `writeTextFile` 双路径组合。

### 4.7 MCP

OpenCode initialize 声明 MCP http/sse capability。`registerMcpServers` 会把 ACP MCP server 转成 OpenCode MCP config：

- remote: `{ type: "remote", url, headers }`
- local: `{ type: "local", command, environment }`

必须测试：

- Feishu MCP local command 注入。
- bearer 是否只在 env/header 临时出现，不写 config。
- MCP OAuth 需要用 OpenCode 自己 `mcp auth` 还是 ACP 客户端能力，当前 unknown。

## 5. Canonical Mapping Coverage

| raw ACP / OpenCode 语义 | agentproto 目标 | 当前结论 | 必测 fixture |
| --- | --- | --- | --- |
| initialize response | capability.state / hello capability | source supported | initialize raw fixture |
| `sessionId` + cwd | thread identity / workspace key | source supported | new/list/resume |
| prompt request/response | turn lifecycle | supported with adapter buffering | prompt response has no final text |
| `agent_message_chunk` | `item.delta(agent_message)` | supported | live delta and replay aggregate differ |
| `agent_thought_chunk` | reasoning summary/content | supported | live delta and replay aggregate differ |
| `tool_call` | item.started(dynamic/tool) | supported | read/bash/edit/MCP verified |
| `tool_call_update` running/completed/failed | item.delta/completed | supported | read/bash/edit/MCP/failure verified |
| `permission.asked` -> requestPermission | request.started/resolved | supported | once/always/reject verified |
| `available_commands_update` | capability/model/command projection | supported | command catalog fixture verified |
| prompt `usage` | token usage | supported with caveat | response usage and usage_update semantics differ |
| `configOptions` | model/reasoning/mode config view | supported | dynamic model/mode verified |
| `stopReason` | turn.completed status/origin | supported | end_turn and cancelled verified |
| load/fork replay chunks | thread history / hydration | supported with caveat | replay sends aggregate chunks |
| slash `/compact` summarize | compact command lifecycle | supported with caveat | no normal prompt usage in response |
| ACP mode `plan` | session config only | best_effort | must not emit `turn.plan.updated` without real plan content |

## 6. Configuration Mapping Coverage

| 产品配置 intent | OpenCode native 目标 | launch material | observed/product projection | 当前结论 | 黑盒测试 |
| --- | --- | --- | --- | --- | --- |
| profile id/name/revision | our config only | daemon launch contract | status/admin | needs local design | compiler golden |
| base URL/provider/API key | `provider` config + auth | `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT` | provider request to local fake server | supported | fake provider config |
| main model | `model` config or ACP set model | config content / ACP config option | configOptions currentValue | source supported | model set before prompt |
| reasoning effort | model variants / ACP `effort` | config content / ACP config option | configOptions effort | source supported | variant set |
| instruction | `instructions` / agent/mode config | config content | prompt/system context only | best_effort | compiler golden |
| subagent model | agent config | config content | task/sub-session behavior | best_effort | `/review` task path observed |
| permission/access | `permission` config / `OPENCODE_PERMISSION` | env JSON | permission events | supported with caveats | edit/bash permission matrix |
| plan mode | OpenCode modes/agents | config content / ACP mode option | configOptions mode | best_effort | mode switch and plan output |
| sandbox | no native sandbox config found | none | none | unsupported | explicit unsupported diagnostic |
| context window | provider model limit | provider catalog | usage context limit | best_effort | provider limit fixture |
| MCP | ACP mcpServers -> `sdk.mcp.add` | session params | provider tool list + MCP call | supported for local stdio | remote/OAuth separate |
| project config inherit | default loader behavior | no disable env | observed config | source supported | global + project merge |
| project config disable | `OPENCODE_DISABLE_PROJECT_CONFIG=1` | env | observed config | source supported | disabled project fixture |
| auth isolation | `OPENCODE_AUTH_CONTENT` | secret env | no direct projection | source supported | no auth.json write |
| data/session isolation | XDG paths / `OPENCODE_TEST_HOME` / `OPENCODE_DB` | env | state paths | supported for process-level isolation | OAuth caveat |

## 7. 黑盒测试计划

测试安装：

- 使用 isolated npm prefix 安装 `opencode-ai@1.18.15`。
- 不修改系统 `opencode`。
- 测试 env 固定 `HOME`、`XDG_CONFIG_HOME`、`XDG_DATA_HOME`、`XDG_STATE_HOME`、`XDG_CACHE_HOME` 到临时目录。
- 每个测试记录 binary path、`opencode --version`、release tag、cwd、env 摘要。

第一批 smoke：

1. `opencode --version`。
2. `opencode acp --cwd <tmp repo>` 启动后发送 initialize。
3. initialize 返回 capabilities 和 agentInfo。
4. 缺 auth 时 newSession/prompt 的错误形态。
5. stderr/log 不污染 stdout nd-json。

第二批配置 overlay：

1. global config 设置模型 A，project config 设置模型 B，`OPENCODE_CONFIG_CONTENT` 设置模型 C，验证默认模型 C。
2. 只设置 `OPENCODE_CONFIG_CONTENT` 的少数字段，验证其他字段继承 global/project。
3. `OPENCODE_DISABLE_PROJECT_CONFIG=1` 验证 project config 不参与。
4. `OPENCODE_AUTH_CONTENT` 注入 fake API key，验证 `auth.json` 不创建或不含 secret。
5. `OPENCODE_CONFIG_DIR` 验证是否替换 global path；若替换，则禁止作为默认 profile overlay。
6. MCP OAuth 验证是否写 `mcp-auth.json`；默认按非隔离处理。
7. `/config`、`/global/config` API 验证会写文件，禁止作为临时 profile 路径。

第三批 session lifecycle：

1. newSession -> listSessions。
2. prompt text -> listSessions updatedAt/title。
3. kill ACP process -> new ACP process -> listSessions/resumeSession。
4. loadSession replay raw chunks，标记 hydration。
5. forkSession / closeSession / cancel。

## 8. 已执行黑盒测试记录

当前测试 binary：

- Install: isolated npm prefix `/tmp/opencode-bin-0kUMrD`
- Command: `/tmp/opencode-bin-0kUMrD/node_modules/.bin/opencode`
- Version output: `1.18.15`

已验证：

| 用例 | 结果 | 证据摘要 | 结论 |
| --- | --- | --- | --- |
| ACP initialize | pass | `opencode acp --cwd <tmp>` 返回 `protocolVersion: 1`、`agentInfo.version: 1.18.15`、session list/resume/fork/close、MCP http/sse、image/embeddedContext | ACP entrypoint supported |
| stdout/stderr framing | pass | initialize smoke 中 stdout 只有 nd-json response，stderr 为空 | framing smoke supported |
| session new/list | pass | `session/new` 返回 `ses_...` 和 configOptions；随后 `session/list` 返回同 session、cwd、title、updatedAt | session catalog smoke supported |
| available commands | pass | `session/update` 推送 `available_commands_update`，包含 `review`、`init`、`customize-opencode` | command catalog observable |
| invalid configured model fallback | observed | 配置 `opencode/grok-code-fast-1` 时 current model 回落 `opencode/big-pickle` | compiler 必须校验 model in provider catalog |
| global config model | pass | global `opencode.jsonc` model `opencode/longcat-2.0-free` -> ACP currentValue 同值 | global config inherited |
| project overrides global | pass | global longcat + project `opencode/mimo-v2.5-free` -> currentValue mimo | project config inherited and higher priority |
| `OPENCODE_CONFIG_CONTENT` overrides project | pass | global longcat + project mimo + content `opencode/north-mini-code-free` -> currentValue north | inline overlay supported |
| disable project config | pass | global longcat + project mimo + `OPENCODE_DISABLE_PROJECT_CONFIG=1` -> currentValue longcat | project config disable supported |
| `OPENCODE_AUTH_CONTENT` no-write smoke | pass | injected `secret-test-key`; no `auth.json`/`mcp-auth.json`; recursive temp scan found no secret; stderr empty | API auth env overlay smoke supported |
| runtime set model | pass | `session/set_config_option model=opencode/mimo-v2.5-free` -> configOptions current model updated | dynamic model supported |
| runtime set mode | pass | `session/set_config_option mode=plan` -> configOptions current mode updated | dynamic mode supported, not a plan snapshot |
| invalid runtime config | pass with diagnostic | invalid model/mode return JSON-RPC `-32602`; OpenCode also logs request/error to stderr | wrapper must capture stderr as diagnostic |
| process restart list/resume | pass | ACP process 1 `session/new`; process 2 `session/list` sees same `ses_...`; `session/resume` returns configOptions; persisted `xdg/data/opencode/opencode.db` | basic surface resume supported |
| `OPENCODE_CONFIG_DIR` overlay | pass with caveat | global longcat + `OPENCODE_CONFIG_DIR` mimo -> currentValue mimo；override dir 写入 `.gitignore` | usable but not default profile overlay path |
| unknown method / invalid params | pass with caveat | unknown method -> `-32601`；invalid params -> `-32602`；missing session resume -> `-32603 OpenCode service failure` | error taxonomy partially coarse |
| JSON-RPC response ordering | observed | negative test 中 id=2 response 早于 id=1 initialize response | adapter must correlate by id, not order |
| fork session | pass | `session/fork` returns new `ses_...` and configOptions; list includes original and fork | fork smoke supported |
| close session | caveat | `session/close` returns `{}` but subsequent `session/list` still includes closed session | close is not persistent delete |
| fake OpenAI-compatible provider | pass | `OPENCODE_CONFIG_CONTENT.provider.fake` + `@ai-sdk/openai-compatible` 调用本地 `/v1/chat/completions`；请求含 `Authorization: Bearer secret-fake-key`、`stream: true`、主请求含 tools | deterministic provider harness supported |
| title generation side request | observed | 每次 prompt 通常先发一次不带 tools 的 title/small-model 请求，再发主请求 | fixture 必须按 request body 区分 title vs main |
| assistant text delta | pass | 主请求 SSE `content: "Hello "` + `"ACP"` -> ACP `agent_message_chunk` 两段；prompt response `stopReason: end_turn` 且无 final text | adapter must buffer chunks |
| reasoning delta | pass | SSE `reasoning_content` -> ACP `agent_thought_chunk`；prompt response usage 含 `thoughtTokens` | reasoning supported |
| prompt response usage | pass | provider usage `prompt_tokens=17, completion_tokens=7, cached_tokens=5, reasoning_tokens=2` -> response usage `inputTokens=12, outputTokens=5, totalTokens=24, thoughtTokens=2, cachedReadTokens=5` | OpenCode usage is normalized, not raw provider totals |
| `usage_update` | pass | prompt 后 ACP 推送 `usage_update used=17, size=8000, cost.amount=0` | this is context/cumulative style, not exact per-turn response usage |
| load replay after prompt | pass with caveat | `session/load` replay emits `user_message_chunk`、`agent_thought_chunk`、`agent_message_chunk` as whole content (`thought-delta`, `Hello ACP`) | live delta and replay chunk shapes differ; de-dupe by message/part lifecycle |
| read tool lifecycle | pass | provider calls `read` -> ACP `tool_call pending`、`tool_call_update in_progress` with `locations:[README.md]`、`completed` with preview/rawOutput | read mapping supported |
| bash tool lifecycle | pass | provider calls `bash` -> permission request, `in_progress` with cwd, running output snapshot `probe-bash`, `completed` with exit metadata | execute mapping supported |
| edit permission + file diff | pass | provider calls `edit`; ACP sends `session/request_permission` with diff content and options; then `fs/write_text_file`; final tool update includes diff/rawOutput | edit mapping and proposed-write bridge supported |
| permission allow always | pass | client selected `always`; OpenCode continued edit and final prompt `end_turn` | adapter can expose persistent allow intent, but persistence semantics belong to OpenCode |
| permission reject | pass | client selected `reject`; tool update becomes `failed` with error text, prompt still returns `end_turn` | rejection is a failed tool, not a failed turn |
| tool execution error | pass | `read missing.txt` -> `tool_call_update failed` with raw error, prompt still returns `end_turn` after model handles tool result | tool errors are item-level |
| cancel running prompt | pass | hanging provider stream after partial text; `session/cancel` -> prompt response `stopReason: cancelled` with zero usage | cancellation supported; late partial chunks may already be emitted |
| local MCP injection | pass | `session/new` with local stdio MCP server; server received `tools/list` and `tools/call`; provider tools include `fixture_echo`; ACP emits MCP tool pending/in_progress/completed | ACP mcpServers -> OpenCode MCP tool path supported |
| MCP failed registration | observed | bad MCP fixture import caused `server unavailable` and no provider tool; model calling unavailable tool became OpenCode `invalid` tool result | wrapper should surface MCP status diagnostics if available |
| `/review` command | pass with caveat | `available_commands_update` exposes `review`; `/review` logs `command=review`, starts task/sub-session, model then summarizes `<task_result>` | supported but not equivalent to Codex/Claude native review surfaces |
| `/compact` command | pass with caveat | `/compact` calls compaction prompt (`agent=compaction`); prompt response `end_turn` without usage; emitted text can come from compaction/title path | supported, but lifecycle differs from normal prompt |
| unsupported slash commands | pass negative | `/patch`、`/auto-continue`、`/auto-whip`、`/sendfile` produce no provider call, only empty `end_turn` and available commands update | adapter should pre-filter/diagnose unsupported product commands |

未完全覆盖但不阻塞接入的风险：

- MCP OAuth / remote OAuth：源码显示会使用 `mcp-auth.json` 和 OAuth flow；第一版不做我们侧首填、刷新或多 OAuth profile 管理，只继承系统现状。
- OAuth refresh / managed account：`OPENCODE_AUTH_CONTENT` 只证明 API key 注入不落盘，不代表 OAuth token 更新不写全局状态；完整设计只需证明系统 OAuth 不影响显式 API profile。
- native runtime status：OpenCode 内部有 `session.status` idle/busy/retry，但 ACP bridge 只用 idle 解 `runUntilIdle`，不向客户端转发完整 busy/retry 状态。
- persistent delete：`session/close` 不是删除历史 session，不能映射成我们的删除语义。
- plan snapshot：`mode=plan` 只是 OpenCode mode/agent，不能还原 Codex/Claude 风格结构化 Plan。
- sandbox：未发现 OpenCode profile 级 sandbox 配置；只能表达 permission/external_directory 层面的近似约束。

## 9. Mapping 结论

raw-to-canonical adapter 需要按以下规则实现：

1. JSON-RPC response 必须按 id 关联，不能按 stdout 顺序。
2. ACP `session/update` 中 `sessionUpdate` 是主类型判别：
   - `agent_message_chunk` -> assistant text delta。
   - `agent_thought_chunk` -> reasoning/thought delta。
   - `user_message_chunk` -> load replay/hydration 的 user content。
   - `tool_call` -> tool item start，状态 pending。
   - `tool_call_update` -> tool item state transition，状态 `in_progress` / `completed` / `failed`。
   - `usage_update` -> context/cumulative usage meter，不等价于 prompt response per-turn usage。
   - `available_commands_update` -> command catalog refresh。
3. prompt response 不带 final assistant text；必须由 streamed chunks 组装文本，并以 prompt response、idle 语义或 message fetch 作为 turn close 信号。
4. live stream 与 `session/load` replay 形态不同：live 是 delta，load replay 是聚合 content chunk；adapter 要基于 hydration 状态避免重复追加。
5. tool call id、OpenCode message id、JSON-RPC id、permission request id 是不同 id 域，不能复用为 canonical turn/request id。
6. permission request 是 OpenCode 主动调客户端的 `session/request_permission`；reject/failure 归为 tool failed，而不是 backend turn failed。
7. `/review`、`/compact` 是 OpenCode 产品语义，不能假设和现有 Codex/Claude 命令同构；未知 slash 命令必须由我们侧提前拦截并给 explicit unsupported。

## 10. Existing Specialization Audit

| 现有特化面 | OpenCode 对应结论 | 处理 |
| --- | --- | --- |
| Codex/Claude backend identity | needs new identity | 新增 backend，不复用 Codex |
| binary discovery | supported by package/PATH, not yet integrated | 新 resolver + admin requirement |
| Codex capability probe | needed | ACP initialize/config probe |
| Claude local session store | different | 使用 ACP list/load/resume + OpenCode server session store |
| Codex SQLite thread catalog | different | 不读 OpenCode DB unless later needed |
| permission/access projection | best_effort | mapping to OpenCode permission + ACP request |
| plan semantics | weak mapping | mode=plan supported, no structured plan snapshot |
| tool taxonomy | supported with approximation | OpenCode taxonomy matrix + `other` fallback |
| reasoning/final reconciliation | supported but nontrivial | live delta vs replay aggregate de-dupe |
| request bridge | supported | permission request / fs write request 已验证 |
| runtime config observation | partial | ACP configOptions + our launch contract |
| workspace profile snapshots | no native | 我们侧 snapshot/compiler |
| child restart restore | supported | ACP process restart list/resume 通过 |
| review/compact/turn patch | review/compact supported; patch unsupported | command compatibility documented |
| todo / plan / delegated task | OpenCode has native todo, plan file tool, task sub-session, but ACP projects these mostly as tool updates | best-effort taxonomy, no synthetic canonical plan snapshot |
| MCP publication/OAuth | local publication supported; OAuth unknown | local stdio MCP 通过，OAuth explicit unknown |
| admin/config APIs | no native profile API | 我们侧 CRUD + compiler |
| upgrade lifecycle | optional | 默认 unsupported/hidden |

## 11. Playbook 对照结论

已对照 `docs/general/backend-integration-evaluation-playbook.md`：

- 覆盖 `4.1` backend identity：需要新增，不允许 unknown 回 Codex。
- 覆盖 `4.2` launcher/entrypoint：`opencode acp` 是独立 runtime mode。
- 覆盖 `4.3` profile catalog：OpenCode 无同构 profile，必须由我们侧 schema + compiler 实现。
- 覆盖 `4.4` loader/profile compiler：使用 `OPENCODE_CONFIG_CONTENT` / `OPENCODE_AUTH_CONTENT` 作为主候选。
- 覆盖 `4.5` configuration mapping：已有字段表和 overlay 黑盒计划。
- 覆盖 `4.6` runtime protocol adapter：ACP update 到 agentproto 需要新 adapter。
- 覆盖 `4.7` canonical mapping：已有 raw-to-canonical fixture corpus 初稿。
- 覆盖 `4.8` headless lifecycle：待后续实现设计，测试里包含 restart/failure cleanup。
- 覆盖 `4.9` surface resume：测试里包含 process restart + resume。
- 覆盖 `4.10` workspace/thread catalog：测试里包含 list/resume/cwd filter。
- 覆盖 `4.11` prompt/queue/attachments：测试里包含 text/image/file/cancel。
- 覆盖 `4.12` command/menu/config flow：产品命令 compatibility 单列。
- 覆盖 `4.13` request/permission：permission matrix 单列。
- 覆盖 `4.14` MCP/OAuth：MCP publication 和 OAuth unknown 单列。
- 覆盖 `4.15` diagnostics：smoke 记录 raw errors，后续转 error code。
- 覆盖 `4.16` admin/config/API：profile CRUD 由我们侧实现。
- 覆盖 `4.17` security/isolation：临时 XDG、secret redaction、auth no-write 单列。
- 覆盖 `4.18` existing specialization audit：已有初版对应表。

最终缺口：

- 代码落点已在差异解决策略中拆到 profile compiler、launcher、ACP adapter、diagnostics、command router 和 lifecycle state machine；下一阶段需要把这些落到具体 Go 包和测试文件。
- MCP OAuth、provider OAuth refresh、managed account 写入路径不进入第一版抽象；只记录为系统 inherit 模式的限制，显式 API profile 必须绕开它们。
- 无 auth 场景能进入 config/model/session 层，但完整 prompt 仍取决于 provider；实现时应把 auth-required 转成产品可读诊断。

## 12. 差异解决策略

这部分是后续实现的真正设计输入。原则不是把缺口简单标成 unsupported，而是先判断能否在我们侧补齐语义；只有补齐会造成误导、破坏隔离或需要 OpenCode 不存在的底层能力时，才明确降级或拒绝。

| 差异 | 影响 | 解决策略 | 实现位置 | 验收方式 |
| --- | --- | --- | --- | --- |
| OpenCode 无 native profile catalog | 我们不能像 Claude/Codex 一样直接列系统 profile | 由我们侧维护 `OpenCodeProfile` catalog；profile 只描述 override intent，不镜像 OpenCode 全配置。运行时 compiler 读取 profile + workspace intent，生成一次性 env/config overlay | daemon profile/admin 层 + OpenCode launcher | profile CRUD golden；同一机器多个 profile 并发启动互不污染 |
| 少字段覆盖并继承系统配置 | 这是必备能力；不能要求用户改全局 config | 默认只写 `OPENCODE_CONFIG_CONTENT` 和 `OPENCODE_AUTH_CONTENT`。不设置 `OPENCODE_DISABLE_PROJECT_CONFIG` 时继承 global+project；profile 要求禁用项目配置时才加该 env。`OPENCODE_CONFIG_DIR` 只作为 API profile 受系统 OAuth 干扰时的隔离候选，不作为默认路径 | OpenCode profile compiler | global/project/content 三层优先级 fixture；系统 OAuth 存在时 API overlay 仍命中 fake provider；扫描临时目录确认不写用户 config |
| API key / baseURL / custom provider 注入 | provider 配置不是 ACP 的一部分 | compiler 生成 `provider.<id>.options.baseURL/apiKey` 和 `model`；API key 优先放 `OPENCODE_AUTH_CONTENT`，只有 OpenCode schema 必须的非密字段放 config content。显式 API profile 不允许 fallback 到系统 OAuth | OpenCode profile compiler | fake provider request 捕获 authorization、model、baseURL；系统 OAuth 存在时仍捕获 profile key；secret redaction test |
| Auth/OAuth 边界 | `OPENCODE_AUTH_CONTENT` 只覆盖 API auth；OAuth/MCP OAuth 可能使用系统状态 | 第一版只支持 API-key profile 的可控覆盖。默认 profile 可以继承系统 OAuth，但我们不首填、不刷新、不隔离多个 OAuth profile；若 OAuth 会干扰 API profile，则第一版禁止 inherit/OAuth 路径 | profile schema + launcher validation | API auth no-write；系统 OAuth + API profile overlay；两个 API profiles 并发不串 key/model/baseURL |
| MCP local/remote 注入 | ACP 能注册 MCP，但 OAuth、状态诊断和命名要处理 | local/remote MCP 作为 session launch material 注入；MCP tool 名按 OpenCode `server_tool` 暴露。MCP OAuth 不进入第一版 profile 管理；注册失败优先写 debug trace/log，产品面按现有失败通道保守处理 | OpenCode ACP adapter + diagnostics | local stdio MCP list/call golden；失败 MCP debug trace；header/env redaction |
| prompt response 无 final text | 不能把 prompt response 当最终 assistant message | adapter 建 turn buffer：以 `messageId` 聚合 `agent_message_chunk` / `agent_thought_chunk`；prompt response 只关闭 turn 并附 usage/stopReason。若 turn close 时无 assistant chunk，输出空完成或命令专用结果 | ACP protocol adapter | text/reasoning delta fixture；无文本 `/patch` 负向 fixture |
| live delta 与 load replay 形态不同 | resume/load 时容易重复追加消息 | adapter 引入 hydration mode。`session/load` 期间的 chunk 作为历史快照导入，不触发“正在生成”状态；同一 `messageId` 已存在时覆盖/合并，不追加重复 delta | ACP protocol adapter + surface resume | load replay fixture：live 两段 delta，load 一段 aggregate，最终 canonical 只有一条消息 |
| OpenCode 没有独立 turn/thread API | 我们的 thread/turn 语义比 ACP 更细 | `sessionId` 映射 thread；TurnID 由我们在 prompt admission 时生成，并记录 `jsonrpc id -> turnID`、`messageId -> turnID`。OpenCode message id 只作为 backend message id 保存 | adapter state store | 并发/乱序 response fixture；id domain 不混用单测 |
| usage 两套语义 | prompt response usage 与 `usage_update` 不同 | 第一版只把可归因于当前 turn 的 usage 投影到现有 token usage；`usage_update` 作为 runtime/debug state 保留，不默认新增 context meter UI，也不覆盖本 turn token 统计 | ACP usage mapper | response usage 和 usage_update fixture；cache/reasoning token 字段 golden |
| permission reject 是 tool failed | 用户拒绝不应被显示为 backend 崩溃 | request bridge 把 `session/request_permission` 映射成现有 request card；返回 once/always/reject。reject 关闭 request 后等待 tool failed 事件；turn 仍可 end_turn | request bridge + Feishu/remote surfaces | once/always/reject golden；reject 后 card 状态和 tool failed 一致 |
| edit 会调用 `fs/write_text_file` | OpenCode 会请求客户端预览/写入 proposed content | adapter 必须实现 `fs/write_text_file` server method：在远程面只更新 proposed diff/preview，不直接绕过权限写我们自己的工作区；实际文件写入仍以 OpenCode tool execution 为准 | ACP connection method handler | edit fixture：request diff、writeTextFile、completed diff 三者一致 |
| tool taxonomy 不完全同构 | MCP tool kind 是 `other`，部分 OpenCode tool 没有现有分类 | 建 OpenCode tool taxonomy table：bash->execute、read->read、edit/write/apply_patch->edit、grep/glob->search、task/todowrite->think/plan-like、MCP unknown->other with display name。不要为了好看强行归类 MCP | tool mapper | read/bash/edit/MCP/error golden |
| plan mode 不等于 Plan snapshot | 不能从 `mode=plan` 硬造结构化计划 | 比照 Claude：`mode=plan` 只作为后续 turn 的模式/权限意图；计划正文、确认请求、todo/tool 结果按现有普通内容、确认卡或计划更新卡自然承接。有结构化 todo 才投影 `TurnPlanSnapshot`；没有时自然退化为普通内容或 backend mode | config mapper + plan surface | mode switch fixture；todo/tool/text fixture；确认不从 mode 字段发虚假的 plan snapshot |
| sandbox 缺失 | Claude 也没有 Codex 式 OS/container sandbox | 不把 OpenCode 权限/目录近似包装成强 sandbox；第一版按现有 access/permission 产品语义承接，底层差异写 debug trace | profile compiler + diagnostics | permission-only profile 可启动；debug trace 记录 OpenCode native permission/file access |
| `session/close` 不是 persistent delete | 不能把 close 按删除历史展示 | close 只映射为 detach/stop active runtime。历史删除另建产品能力，OpenCode backend 第一版不提供 delete；UI 文案避免“删除会话” | session lifecycle mapper | close 后 list 仍可见 fixture；surface 状态显示 detached/stopped |
| `/review` 语义不同 | 它会启动 task/sub-session，不等于现有 review surface | 支持 `/review` 作为 OpenCode command，但 canonical 里标记 `commandKind=review` 和 `backendShape=task_summary`。不要承诺和 Claude/Codex review 字段一致；后续可在 adapter 中提取 task result 为 review summary | command adapter | `/review` fixture：task tool lifecycle + final summary |
| `/compact` 语义不同 | 它走 summarize/compaction，prompt response 可能无 usage | 支持为 compact command；完成条件用 prompt response + observed compaction messages。usage 可为空，不回填错误值 | command adapter | `/compact` fixture：无 usage 时仍完成 |
| 未知 slash 命令空 end_turn | OpenCode 不报错会让用户误以为执行成功 | 我们侧 command registry 预检：只允许 OpenCode available commands 和显式支持的 `/compact`。未知命令在发送给 OpenCode 前返回产品诊断，不让 OpenCode 空吞 | command router before ACP prompt | `/patch`、`/auto-continue`、`/auto-whip`、`/sendfile` 负向 golden |
| error taxonomy 粗 | `-32603 OpenCode service failure` 不够产品化 | 不追求第一版完整 taxonomy。优先归一化会改变用户下一步操作的错误：missing session、invalid model、MCP failure、auth-required、permission denied；其他保留 raw frame 到 debug trace，并走 Claude 类似的保守通用失败 | diagnostics layer | missing session、invalid model、MCP failure、auth-required fixture |
| runtime busy/retry 不转发 | UI 可能缺少精细状态 | 第一版用 prompt in-flight 状态驱动 busy；cancel/close 走本地 state。retry 只从错误/usage/log 推断，不假装 native status。若后续需要，考虑直接查 OpenCode event API 或贡献 upstream ACP status update | lifecycle state machine | prompt start/end/cancel state fixture |

实现优先级：

1. 先做不会误导安全语义的底座：backend identity、launcher、profile compiler、error normalizer、secret redaction。
2. 再做 ACP runtime adapter：JSON-RPC correlation、turn buffer、load hydration、tool/permission/usage mapping。
3. 再做产品命令层：command registry 预检、`/review`、`/compact`、未知 slash 拦截。
4. 最后处理系统登录态继承边界：只证明 API profile 不受系统 OAuth 影响；MCP OAuth 和多 OAuth profile 管理不进入第一版。

## 13. 下一步执行

1. 新增 OpenCode backend identity、binary resolver 和 launch contract。
2. 实现 OpenCode profile schema/compiler：默认 `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT`，可选临时 XDG，禁止默认 `OPENCODE_CONFIG_DIR`；完整设计前先补系统 OAuth + API overlay 黑盒验证。
3. 实现 ACP protocol adapter：JSON-RPC id correlation、session/update dispatcher、stream buffer、load replay hydration、tool/permission/usage mapping。
4. 实现差异策略里的 validator/diagnostics：API overlay 失败禁止 fallback 到系统 OAuth、persistent delete 隐藏、unknown slash preflight；sandbox/MCP/OAuth 细节进入 debug trace。
5. 将本轮 `/tmp/opencode-acp-*.json` 证据中挑选脱敏 raw fixture 进入测试目录，建立 OpenCode adapter golden tests。
6. 再进入 OpenCode 详细设计与代码实现。

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
- profile overlay 可以实现“少字段覆盖，其余继承”：默认建议用 `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT`，必要时配合临时 XDG；不建议把 `OPENCODE_CONFIG_DIR` 作为默认 overlay，因为它会替换 global config path 并写 `.gitignore`。
- 仍需我们侧单独实现 loader/profile compiler。ACP 只能抽象运行期协议，不能抽象 OpenCode 的配置、auth、MCP OAuth、模型目录、权限 schema 和产品命令差异。
- 主要缺口是语义退化而非不可接入：OpenCode 没有我们的原生 `TurnPlanSnapshot`、sandbox profile、persistent delete、独立 thread/turn API、未知 slash command 的显式错误；这些应在 adapter/compiler 层显式弱化或标记 unsupported。

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
- MCP OAuth 不视为实例隔离 supported，源码 `packages/opencode/src/mcp/auth.ts` 使用 `Global.Path.data/mcp-auth.json`。
- sandbox 没有 OpenCode 同名或等价 OS/container 隔离字段，只能映射 permission / external directory 规则，不能声明为 sandbox supported。

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
- OpenCode `mode=plan` 是 session mode/config option，不是 ACP plan snapshot；不能映射成 `agentproto.TurnPlanSnapshot`。

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

- MCP OAuth / remote OAuth：源码显示会使用 `mcp-auth.json` 和 OAuth flow；默认结论是不能承诺 profile-level 完全隔离，先标 unsupported/unknown。
- OAuth refresh / managed account：`OPENCODE_AUTH_CONTENT` 只证明 API key 注入不落盘，不代表 OAuth token 更新不写全局状态。
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

- 还没有设计我们侧代码落点；这属于下一阶段 OpenCode adapter/compiler 详细设计。
- MCP OAuth、provider OAuth refresh、managed account 写入路径仍需单独设计为 unsupported/unknown，不能混入 ACP 抽象。
- 无 auth 场景能进入 config/model/session 层，但完整 prompt 仍取决于 provider；实现时应把 auth-required 转成产品可读诊断。

## 12. 下一步执行

1. 新增 OpenCode backend identity、binary resolver 和 launch contract。
2. 设计 OpenCode profile schema/compiler：默认 `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT`，可选临时 XDG，禁止默认 `OPENCODE_CONFIG_DIR`。
3. 实现 ACP protocol adapter：JSON-RPC id correlation、session/update dispatcher、stream buffer、load replay hydration、tool/permission/usage mapping。
4. 实现 explicit unsupported diagnostics：sandbox、structured plan snapshot、persistent delete、MCP OAuth、unknown slash commands、turn patch。
5. 将本轮 `/tmp/opencode-acp-*.json` 证据中挑选脱敏 raw fixture 进入测试目录，建立 OpenCode adapter golden tests。
6. 再进入 OpenCode 详细设计与代码实现。

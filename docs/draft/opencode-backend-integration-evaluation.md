# OpenCode Backend 接入评估与黑盒测试计划

> Type: `draft`
> Updated: `2026-08-09`
> Summary: 锁定 OpenCode v1.18.15，按 backend integration playbook 制定接入评估、映射审计与真实黑盒测试计划。

## 1. 结论

- Verdict: `unknown`，进入真实黑盒测试。
- 推荐接入阶段：先做 ACP backend candidate，不先承诺 production candidate。
- 锁定版本：OpenCode `v1.18.15`。
- Release: `https://github.com/anomalyco/opencode/releases/tag/v1.18.15`
- Release published: `2026-08-07T06:49:55Z`
- Release targetCommitish: `325529761beb79a004de6d86e48b8db69cf4eba3`
- 本地源码 tag commit: `d7b115f623760e68a4749d16508a9eca350f246f`，commit message `release: v1.18.15`
- NPM package: `opencode-ai@1.18.15`，bin `opencode`。

当前源码判断：

- ACP 协议面覆盖较广，值得继续测。
- 配置/profile overlay 有强候选机制，但必须黑盒证明是否满足“只覆盖少数字段，其余继承系统/项目配置，不修改用户全局配置”。
- 真正风险不在 ACP handshake，而在配置闭环、session/catalog 持久化、raw-to-canonical 事件映射、产品命令和现有 Claude/Codex 特化面的对应。

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
| profile overlay | required | `OPENCODE_CONFIG`、`OPENCODE_CONFIG_DIR`、`OPENCODE_CONFIG_CONTENT`、merge order | unknown | 继承/覆盖/不写全局配置 |
| auth isolation | required | `OPENCODE_AUTH_CONTENT` bypasses auth file read | unknown | secret 不落盘、不进 log/status |
| MCP publication | best_effort | `registerMcpServers` maps ACP MCP servers to OpenCode `sdk.mcp.add` | source supported | bearer redaction、http/sse/local、OAuth |
| permission request | required | `ACPEvent` handles `permission.asked`; `ACPPermission.Handler` calls `requestPermission` | source supported | allow once/always/reject、edit diff、missing callback fail-closed |
| tool taxonomy | required | `toToolKind` maps bash/read/edit/grep/task/etc. | source supported | agentproto item kind mapping |
| assistant/reasoning stream | required | `message.part.delta` maps text to `agent_message_chunk` and reasoning to `agent_thought_chunk` | source supported | delta/final/replay de-dupe |
| file changes | required | edit tools become ACP `edit` with diff content where possible | best_effort | structured file change path/kind/diff extraction |
| token usage | best_effort | `UsageService.buildUsage` maps tokens/cache/reasoning | source supported | per-turn vs cumulative semantics |
| model catalog | required | directory snapshot uses `sdk.config.providers` | source supported | provider/model list, variants, hidden/disabled providers |
| review command | best_effort | no ACP-specific review API found yet | unknown | command list or unsupported reject |
| compact command | best_effort | slash `/compact` calls `sdk.session.summarize` | source supported | lifecycle events and errors |
| turn patch / patch rollback | best_effort | no ACP-specific turn patch API found yet | unknown | unsupported or approximation |
| workspace defaults/profile snapshots | required | no OpenCode-specific equivalent yet | unknown | our side design required |

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

必须黑盒证明：

- `OPENCODE_CONFIG_CONTENT` 是否真正最后覆盖 global/project。
- 设置 `OPENCODE_CONFIG_CONTENT` 时不会创建/修改 global config schema 文件。
- `OPENCODE_AUTH_CONTENT` 不写 `auth.json`。
- `OPENCODE_CONFIG_DIR` 是否会完全替换 global config path；若是，则只作为隔离模式，不作为默认 profile overlay。
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
| prompt request/response | turn lifecycle | unknown | prompt raw order fixture |
| `agent_message_chunk` | `item.delta(agent_message)` | source supported | delta/final de-dupe |
| `agent_thought_chunk` | reasoning summary/content | source supported | hidden/replay reasoning |
| `tool_call` | item.started(dynamic/tool) | source supported | tool start fixture |
| `tool_call_update` running/completed/failed | item.delta/completed | source supported | bash/edit/read/web/task matrix |
| `permission.asked` -> requestPermission | request.started/resolved | source supported | approve/reject/stale |
| `available_commands_update` | capability/model/command projection | best_effort | command catalog fixture |
| prompt `usage` | token usage | source supported | per-turn/cumulative fixture |
| `configOptions` | model/reasoning/mode config view | source supported | dynamic config fixture |
| `stopReason` | turn.completed status/origin | source supported | end/cancel/max/refusal/auth |
| load/fork replay chunks | thread history / hydration | unknown | replay fixture |
| slash `/compact` summarize | compact command lifecycle | source supported | compact fixture |
| ACP mode `plan` | session config only | best_effort | must not emit `turn.plan.updated` without real plan content |

## 6. Configuration Mapping Coverage

| 产品配置 intent | OpenCode native 目标 | launch material | observed/product projection | 当前结论 | 黑盒测试 |
| --- | --- | --- | --- | --- | --- |
| profile id/name/revision | our config only | daemon launch contract | status/admin | needs local design | compiler golden |
| base URL/provider/API key | `provider` config + auth | `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT` | `configOptions` / provider list | unknown | fake provider config |
| main model | `model` config or ACP set model | config content / ACP config option | configOptions currentValue | source supported | model set before prompt |
| reasoning effort | model variants / ACP `effort` | config content / ACP config option | configOptions effort | source supported | variant set |
| instruction | `instructions` / agent/mode config | config content | unknown | unknown | prompt context capture |
| subagent model | agent config | config content | unknown | unknown | delegated task fixture |
| permission/access | `permission` config / `OPENCODE_PERMISSION` | env JSON | permission events | best_effort | edit/bash permission matrix |
| plan mode | OpenCode modes/agents | config content / ACP mode option | configOptions mode | best_effort | mode switch and plan output |
| sandbox | no native sandbox config found | none | none | unsupported | explicit unsupported diagnostic |
| context window | provider model limit | provider catalog | usage context limit | best_effort | provider limit fixture |
| MCP | ACP mcpServers -> `sdk.mcp.add` | session params | MCP events/status unknown | source supported | local/remote MCP injection |
| project config inherit | default loader behavior | no disable env | observed config | source supported | global + project merge |
| project config disable | `OPENCODE_DISABLE_PROJECT_CONFIG=1` | env | observed config | source supported | disabled project fixture |
| auth isolation | `OPENCODE_AUTH_CONTENT` | secret env | no direct projection | source supported | no auth.json write |
| data/session isolation | XDG paths / `OPENCODE_TEST_HOME` / `OPENCODE_DB` | env | state paths | unknown | temp XDG fixture |

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

仍未验证：

- prompt stream、tool lifecycle、permission request、usage。
- load replay and prompt history after restart。
- missing session / backend service failures need product-specific diagnostics because OpenCode may return coarse `-32603`。
- native runtime status has `session.status` idle/busy/retry, but ACP consumes idle internally and does not forward full runtime status to client.
- MCP server injection 和 MCP OAuth。
- OAuth refresh 是否写全局 auth。
- config overlay 对 nested provider/baseURL/API key/instructions/permission 的精确优先级。
- `OPENCODE_CONFIG_DIR` 是否替换 global path。

## 9. 后续黑盒测试批次

第一批 raw-to-canonical fixture：

1. assistant text delta + prompt response stopReason。
   - prompt response 没有 final text；adapter 必须缓冲 chunks，并用 prompt response / idle / message fetch 关闭 item。
   - `messageId` 可能是 OpenCode 自有 `msg_...` 形态，不能假设为 UUID。
2. reasoning delta。
3. bash tool pending/running/completed/error。
4. read/edit/write/file diff。
5. webfetch/websearch/grep/glob/task。
6. permission asked allow once/always/reject。
7. usage update。
8. model/effort/mode set success/fail。
9. available commands update。
10. malformed/unknown ACP update。
11. JSON-RPC id、message id、tool call id、permission id correlation，禁止混用成 canonical `TurnID` / `RequestID`。

第二批产品命令：

1. `/compact`。
2. `/review`，若 OpenCode command list 没有等价命令则 explicit unsupported。
3. `/patch` / turn patch rollback explicit unsupported unless raw evidence exists。
4. `/sendfile` local image/text file。
5. MCP OAuth unknown path。

## 10. Existing Specialization Audit

| 现有特化面 | OpenCode 对应结论 | 处理 |
| --- | --- | --- |
| Codex/Claude backend identity | needs new identity | 新增 backend，不复用 Codex |
| binary discovery | supported by package/PATH, not yet integrated | 新 resolver + admin requirement |
| Codex capability probe | needed | ACP initialize/config probe |
| Claude local session store | different | 使用 ACP list/load/resume + OpenCode server session store |
| Codex SQLite thread catalog | different | 不读 OpenCode DB unless later needed |
| permission/access projection | best_effort | mapping to OpenCode permission + ACP request |
| plan semantics | unknown | 测 mode/plan/tool/request |
| tool taxonomy | supported source | 建 OpenCode taxonomy matrix |
| reasoning/final reconciliation | supported source but risky | delta/replay/final de-dupe fixture |
| request bridge | supported source | Feishu request round-trip fixture |
| runtime config observation | partial | ACP configOptions + our launch contract |
| workspace profile snapshots | no native | 我们侧 snapshot 或 unsupported |
| child restart restore | unknown | ACP process restart restore fixture |
| review/compact/turn patch | compact source supported; review/patch unknown | command compatibility tests |
| todo / plan / delegated task | OpenCode has native todo, plan file tool, task sub-session, but ACP projects these mostly as tool updates | best-effort taxonomy, no synthetic canonical plan snapshot |
| MCP publication/OAuth | publication source supported; OAuth unknown | black-box |
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

缺口：

- 还没有真实 binary raw frame。
- 还没有验证 OpenCode config overlay 的实际优先级。
- 还没有确认无 auth 场景能否完整跑到 config/model/session 层。
- 还没有设计我们侧代码落点；这要等黑盒测试结果。

## 12. 下一步执行

1. 扩展 ACP JSON-RPC harness，捕获 prompt/session update raw frames。
2. 跑 permission/tool/usage/product command 黑盒测试。
3. 跑 process restart resume 和 MCP injection。
4. 固化 raw frames 到 `tmp` 证据文件，再挑选可进入 repo 的 fixture。
5. 根据真实结果继续更新本文能力矩阵。
6. 再进入我们侧 adapter/compiler 详细设计。

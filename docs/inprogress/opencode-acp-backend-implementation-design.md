# OpenCode ACP Backend 实现设计

> Type: `inprogress`
> Updated: `2026-08-10`
> Summary: 收敛 OpenCode profile 子代理模型设计：放弃 review model，subagentModel 投影到内置 general/explore agent model。

## 1. 结论

OpenCode backend 可以开始完整实现。第一版目标不是最小 PoC，而是把真实接入链路里的状态合同、profile compiler、launcher、ACP adapter、命令能力和测试证据一次打通。

当前锁定对象：

- GitHub parent issue：#844。
- 当前设计 issue：#845。
- 后续实现子单：#846 profile/launcher，#847 runtime adapter，#848 canonical mapping/golden tests，#849 daemon integration/e2e。
- OpenCode 版本：`opencode-ai@1.18.15`，OpenCode tag `v1.18.15`。

核心设计取舍：

- 新增具体 backend identity：`agentproto.BackendOpenCode = "opencode"`。不要把持久化 backend 写成泛化 `acp`，因为 profile/config/auth/launcher 都是 OpenCode 私有语义；可复用的只是 ACP wire adapter。
- 新增 `internal/adapter/acp` 承接 ACP JSON-RPC、session/update、permission、tool、usage、hydration 等协议段。Kimi/Goose 后续如果也接 ACP，应复用这个包，但各自另写 compiler/launcher。
- 新增 `internal/app/opencodeprofile` 承接 OpenCode profile schema 到 launch material 的编译。它不能抽象成 ACP 通用 loader。
- API-key OpenCode profile 是第一版 fully supported 目标。默认 profile 可以继承系统 OpenCode 当前状态，但我们不管理 OAuth，不首填 OAuth，不支持多个 OAuth profile。
- API-key profile overlay 已用真实 `opencode-ai@1.18.15` + fake provider 验证：模型和 `Authorization` 都来自 profile overlay。OAuth profile 管理、refresh 和多个 OAuth profile 仍不做；系统 OAuth 共存只作为默认 inherit profile 的用户本机状态处理。
- Plan、sandbox、usage context meter、persistent delete、完整 error taxonomy 都按 Claude 现有产品基线处理：能投影就投影，不能投影就自然退化到现有文本/日志/diagnostic，不新增用户可见的内部 carrier 提示。

#847 已补第一批 runtime adapter skeleton：

- `internal/adapter/acp` 已覆盖 JSON-RPC correlation、initialize、session new/list/load/resume/fork/prompt/cancel、permission request/respond、usage、tool/text/thought chunk、`session/load` hydration replay 汇总和 `fs/write_text_file`。
- `fs/write_text_file` 当前策略是：必须先收到 OpenCode permission approval，且路径必须经 workspace realpath confinement 校验，已有 symlink/junction 指向 workspace 外部时 fail-closed；成功时真实写入文件并发出 `item.file_change.patch_updated`，未批准或越界时 JSON-RPC error fail-closed。
- `fs/write_text_file` 已封住确定性的 symlink/junction escape 和 dangling symlink escape；并发 TOCTOU path swap 仍作为后续安全 hardening 记录，不阻塞 #847 skeleton close。
- `internal/app/wrapper` 已把 `opencode-acp` wrapper mode 接到真实 `opencode acp` child launch，并在返回前完成 ACP initialize bootstrap。
- 已用 `opencode-ai@1.18.15` npm binary 和本地 fake provider 跑通 Go guarded smoke：initialize -> session/new -> session/prompt -> thought/text delta -> prompt response `stopReason=end_turn` -> turn completion -> `session/load` replay 汇总成 history，且 replay 不泄漏 live delta。测试入口：`OPENCODE_ACP_SMOKE_BIN=/path/to/opencode go test ./internal/adapter/acp -run TestRealOpenCodeACPPromptSmoke -count=1 -v`。
- API-key overlay 已由 #849 真实 smoke 补证；系统 OAuth 共存不作为第一版承诺，OAuth 管理仍不进入产品面。

## 2. 范围

第一版必须完成：

- backend identity、display name、default capabilities、unknown backend 不再误回 Codex 的测试。
- OpenCode profile CRUD/schema、revision/ETag、secret handling、reference check、admin API。
- OpenCode profile compiler：`OPENCODE_CONFIG_CONTENT`、`OPENCODE_AUTH_CONTENT`、project config inherit/disable、MCP launch intent、diagnostics/redaction。
- daemon headless launch：OpenCode profile admission、launch contract、pending headless、restart compatibility、wrapper launch mode。
- wrapper runtime：`opencode acp` child launcher、ACP initialize、session lifecycle、command translation、cancel、request response、child restart restore。
- ACP canonical mapping：turn buffer、hydration replay、tool lifecycle、permission bridge、usage/error/config option/available command projection、`fs/write_text_file`。
- command profile：OpenCode visible command set、unsupported slash preflight、model/reasoning/plan/access behavior。
- fixtures/golden/unit/integration/black-box verification。

第一版不做：

- Kimi Code 接入。
- 多 OAuth profile 管理或后台代填 OAuth。
- OpenCode MCP OAuth 的我们侧登录流程。
- 把 OpenCode 权限伪装成 Codex sandbox。
- persistent session delete。
- 新增独立 context meter UI。
- 未经验证的 backend slash command passthrough。

## 3. 总体架构

| 层 | 新增/修改 | 职责 | 不放这里的内容 |
| --- | --- | --- | --- |
| backend identity | `internal/core/agentproto`、`internal/core/state`、orchestrator contract | 表达 `opencode` 是第三条 backend，实例/profile/launch 都可区分 | OpenCode 配置文件细节 |
| profile/config | `internal/config/opencode_profiles.go`、`internal/app/opencodeprofile` | 持久化 profile、校验、编译 env/config/auth/MCP launch material | ACP frame parsing |
| admin/runtime requirements | daemon admin routes、web types | profile CRUD、binary detect、profile references、状态展示 | runtime protocol translation |
| daemon launch | `app_headless.go` + OpenCode profile apply 文件 | 选择 profile revision、注入 env、选择 `opencode-acp` launch mode | JSON-RPC correlation |
| wrapper runtime | `internal/app/wrapper` | 启动 `opencode acp`，接 relay command，协调 ACP adapter | profile CRUD |
| ACP adapter | `internal/adapter/acp` | ACP initialize、request/response、session/update 到 agentproto | OpenCode loader |
| product command | `internal/core/control`、orchestrator command routing | OpenCode command visible/hidden/reject，unknown slash preflight | backend 内部 command 的完整 UI |
| tests | `testkit/mockopencode`、adapter fixtures、daemon/wrapper tests、black-box scripts | 证明映射、隔离和 e2e 行为 | 只证明能启动的 smoke-only PoC |

## 4. Backend Identity 与 State Contract

### 4.1 agentproto

新增：

- `BackendOpenCode Backend = "opencode"`。
- `NormalizeBackend` 识别 `opencode`，未知值不应静默变 Codex。兼容策略是：空值仍可按历史默认 Codex；非空未知值返回 unsupported diagnostic 或保留 unknown 并在调用点拒绝。具体实现可先加 `ParseBackend`，再逐步替换高风险入口。
- `BackendDisplayName(BackendOpenCode) == "OpenCode"`。
- `DefaultCapabilitiesForBackend(BackendOpenCode)`：
  - `ThreadsRefresh: true`
  - `TurnSteer: false`
  - `RequestRespond: true`
  - `SessionCatalog: true`
  - `ResumeByThreadID: true`
  - `RequiresCWDForResume: true`
  - `ModelCatalog: false` by default；如果 ACP `configOptions` 能稳定给出 model option，再由 runtime capability state 或 command mapper 打开局部能力。
  - `VSCodeMode: false`

OpenCode 不进入 VS Code mode。VS Code 仍固定是 Codex shape。

### 4.2 state fields

新增 profile identity：

```go
type OpenCodeProfileRef struct {
    ID       string `json:"id"`
    Revision uint64 `json:"revision"`
}

type OpenCodeAdmissionRef struct {
    ProfileRef OpenCodeProfileRef `json:"profileRef"`
}
```

扩展这些结构：

- `SurfaceBackendContract.OpenCodeProfileID`
- `InstanceBackendContract.OpenCodeProfileID`
- `HeadlessLaunchContract.OpenCodeProfileID`
- `HeadlessLaunchContract.OpenCodeAdmissionRef`
- `InstanceRecord.OpenCodeProfileID`
- `InstanceRecord.OpenCodeAdmissionRef`
- `SurfaceConsoleRecord.OpenCodeProfileID`
- `SurfaceConsoleRecord.OpenCodeAdmissionRef`
- `HeadlessLaunchRecord.OpenCodeProfileID`
- `HeadlessLaunchRecord.OpenCodeAdmissionRef`
- `BotCapabilitySettingsRecord.OpenCodeProfileID`
- `RequestPromptRecord.Backend` 已可承接，不需改字段
- `agentproto.InstanceHello.OpenCodeProfileID`
- `control.DaemonCommand.OpenCodeProfileID`
- 队列 item / pending prompt admission 如果已有 Codex admission ref 语义，需要增加 OpenCode admission ref，避免 profile 修改后 queued prompt 吃到新 secret。

新增 helper：

- `HeadlessOpenCodeSurfaceBackendContract(profileID string)`
- `OpenCodeInstanceBackendContract(profileID string)`
- `HeadlessOpenCodeLaunchContract(profileID string)`
- `EffectiveSurfaceOpenCodeProfileID`
- `NormalizeOpenCodeProfileID`
- `NormalizeOpenCodeAdmissionRef`
- `CloneOpenCodeAdmissionRef`

兼容规则：

- 旧记录没有 `OpenCodeProfileID` 时只按 Codex/Claude 旧逻辑恢复。
- `backend=opencode` 但 profile 缺失时使用 built-in default `op_default`，但仅表示 inherit 系统 OpenCode；如果 surface 要求 API profile 而 ref 缺失，应启动前失败并要求重新选择。
- `WorkspaceDefaultsStorageKey` 必须把 `opencode + profileID + workspaceKey` 作为独立 key，不能和 Codex/Claude 共用默认。

### 4.3 mode alias

`SurfaceModeAlias` 增加：

- headless + OpenCode -> `opencode`
- `/mode opencode` 是用户入口 alias。
- 持久化仍写 `ProductModeNormal + BackendOpenCode + OpenCodeProfileID`。

## 5. OpenCode Profile Schema

### 5.1 config model

新增：

```go
type OpenCodeSettings struct {
    BinaryPath string                     `json:"binaryPath,omitempty"`
    Profiles   []OpenCodeAPIProfileRecord `json:"profiles,omitempty"`
}

type OpenCodeAPIProfileRecord struct {
    ID              string                         `json:"id"`
    CurrentRevision uint64                         `json:"currentRevision"`
    Revisions       []OpenCodeAPIProfileSecretConfig `json:"revisions"`
}

type OpenCodeAPIProfileSecretConfig struct {
    ID                   string `json:"id"`
    Revision             uint64 `json:"revision"`
    CredentialGeneration uint64 `json:"credentialGeneration"`
    ConnectionGeneration uint64 `json:"connectionGeneration"`
    Name                 string `json:"name"`
    BaseURL              string `json:"baseURL"`
    APIKey               string `json:"apiKey"`
    Model                string `json:"model"`
    SmallModel           string `json:"smallModel,omitempty"`
    SubagentModel        string `json:"subagentModel,omitempty"` // projected as agent.general/explore.model, not top-level subagent_model
    Instruction          string `json:"instruction,omitempty"`
    ReasoningEffort      string `json:"reasoningEffort,omitempty"`
    ProjectConfigMode    string `json:"projectConfigMode,omitempty"`
    DataIsolationMode    string `json:"dataIsolationMode,omitempty"`
    PermissionMode       string `json:"permissionMode,omitempty"`
}
```

Built-in default profile：

- ID：`op_default`
- name：`本机默认`
- auth mode：inherit
- editable/deletable：false
- 行为：不写 `OPENCODE_CONFIG_CONTENT` / `OPENCODE_AUTH_CONTENT`，继承系统 global/project/auth。

API profile：

- ID 建议前缀 `op_`，例如 `op_team_proxy`。
- 必填：name、baseURL、apiKey、model。
- 可选：small/subagent/instruction/reasoning、project config mode、data isolation mode、permission mode。
- `subagentModel` 不是 OpenCode 顶层 `subagent_model`。OpenCode v1.18.15 的 Task tool 按 `subagent_type` 精确选择 agent，agent 没有 model 时回退父会话模型；`general.model` 不会成为其它 subagent 的 fallback。因此第一版只把 `subagentModel` 投影为内置 `agent.general.model` 和 `agent.explore.model`，自定义 subagent 继续按用户自己的 OpenCode agent 配置处理。
- `reviewModel` 不进入第一版 profile schema/WebUI/compiler。OpenCode v1.18.15 没有顶层 `review_model`；内置 `/review` 是 `subtask` command，如需指定 review model 必须覆盖 `command.review.model` 且同时承担内置 review template 版本耦合。
- 只要 apiKey/baseURL/model 不完整，`Available=false`，启动失败信息要可操作。
- API profile 不允许 fallback 到系统 OAuth。请求未命中 profile provider 时视为 compiler/runtime 错误。

第一版不接入独立 context preference store。OpenCode context window 作为 profile/provider 观测或 compiler diagnostic 表达，不新增类似 Codex 的 context preference 交互；后续需要可另加 `OpenCodeContextPreference`。

### 5.2 admin API

新增 endpoints：

- `GET /api/admin/opencode/profiles`
- `POST /api/admin/opencode/profiles`
- `PUT /api/admin/opencode/profiles/{id}`
- `DELETE /api/admin/opencode/profiles/{id}`
- `GET /api/admin/opencode/profiles/{id}/references`

使用 Codex API profile 的 revision/ETag 语义：

- update/delete 要求 `If-Match`。
- API key 空字符串表示沿用旧 secret；显式清空需要单独字段或后续设计，不用在第一版复杂化。
- references 至少扫描 bot default、surface desired、pending headless、queue item、active instance、surface resume store。

web UI 可后置到 #849，但 API 和 state 必须先到位；否则 Feishu/admin 以外无法配置 OpenCode profile。

### 5.3 runtime requirements

新增 `OPENCODE_BIN` 和 `OpenCodeSettings.BinaryPath`：

解析优先级：

1. `OPENCODE_BIN`
2. config `openCode.binaryPath`
3. PATH 上的 `opencode`

runtime requirements 页面新增 `opencode_binary` check。Ready 逻辑变为 launcher 可用且 Codex/Claude/OpenCode 任一 runtime 可用。

## 6. Profile Compiler 与 Launch Material

新增包：`internal/app/opencodeprofile`。

输入：

- `OpenCodeProfileRef`
- 当前 profile revision
- workspace root / cwd
- Feishu MCP publication intent
- profile project/data/permission intent
- runtime paths

输出：

```go
type LaunchMaterial struct {
    BinaryPath  string
    Args        []string
    Env         []string
    Session     SessionMaterial
    Diagnostics []Diagnostic
}
```

`Args` 默认：

- `acp`
- `--cwd <workspaceRoot>`，如果 OpenCode CLI 对 `--cwd` 的位置有要求，黑盒固定后按真实行为写入 tests。

env 策略：

- API profile 写 `OPENCODE_CONFIG_CONTENT`，只覆盖 provider/model/instruction/permission 等需要字段。
- API profile 写 `OPENCODE_AUTH_CONTENT={"<providerID>":{"type":"api","key":"..."}}`，注入 profile API key；旧的 `{"provider":{...}}` / `apiKey` 形状不是 OpenCode `v1.18.15` 的真实 auth schema。
- 默认 inherit profile 不写 config/auth overlay。
- `ProjectConfigMode=disable` 时写 `OPENCODE_DISABLE_PROJECT_CONFIG=1`；默认继承 project config。
- 默认不写 `OPENCODE_CONFIG_DIR`，因为它会改变 global config path 并产生 `.gitignore` 等副作用。
- `DataIsolationMode=process` 才设置临时 `XDG_DATA_HOME` / `XDG_STATE_HOME` / `XDG_CACHE_HOME`；第一版默认不启用，以免失去系统 session/OAuth 继承。
- compiler 必须清理并覆盖同名 `OPENCODE_*` env，避免 daemon 环境污染 profile。

诊断策略：

- `required` intent 无法表达：启动前 hard fail。
- `best_effort` intent 无法表达：写 diagnostic/debug trace，不阻塞。
- secret 只允许出现在 child env，不写 raw debug，不进 admin response，不落 profile summary。

OpenCode config content 的建议形状由 golden test 固定，不能靠字符串拼接；使用结构体 marshal。字段大致包括：

- `provider.<generatedProviderID>.npm = "@ai-sdk/openai-compatible"`
- `provider.<generatedProviderID>.options.baseURL`
- `provider.<generatedProviderID>.models.<model>`：必须写入当前 profile 模型的最小 metadata；`OPENCODE_DISABLE_MODELS_FETCH=1` 下不能只写 provider/model 字符串。
- `model = "<providerID>/<model>"`
- `small_model = "<providerID>/<smallModel>"`，当 profile 配置了轻量模型时写入。
- `agent.general.model = "<providerID>/<subagentModel>"` 和 `agent.explore.model = "<providerID>/<subagentModel>"`，当 profile 配置了子代理模型时写入；不要写不存在的顶层 `subagent_model`。
- instructions/agent/mode 相关字段
- `permission` 只写 OpenCode 原生 map，例如 `{"*":"ask"}` / `allow` / `deny`。产品侧 `plan` 等非 OpenCode 原生 permission intent 不写入 config，避免生成无效配置。
- `reasoningEffort` 不能写成 top-level `reasoning`，OpenCode 1.18.15 会拒绝该字段；当前只能通过模型 `variants` 做最接近的注入。
- `review_model` 不写入第一版 config overlay；如后续要支持 review model，需要单独设计 command override，不能复用 profile 顶层模型字段。

具体字段名以 `opencode-ai@1.18.15` 黑盒 fixture 为准。

OAuth/API overlay 结论：

- API profile 的 `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT` 已在真实 smoke 中压过默认环境并被 fake provider 观测到；真实 smoke 使用 production `opencodeprofile.CompileLaunchMaterial` 产物，而不是手写测试 env。
- 系统 OAuth 不由我们写入、刷新或多 profile 管理；默认 inherit profile 保持用户本机 OpenCode 状态。
- 如果后续发现系统 OAuth 会污染 API profile，第一版可直接禁止 API+OAuth 共存路径；当前 #849 只承诺 API-key overlay path。

## 7. Daemon Launch 与 Orchestrator

### 7.1 startManagedHeadless

修改 `startManagedHeadlessLocked`：

- 后端是 Codex 时走现有 `applyCodexHeadlessProviderConfigLocked`。
- 后端是 Claude 时走现有 `applyClaudeHeadlessProfileEnv`。
- 后端是 OpenCode 时走新增 `applyOpenCodeHeadlessProfileConfigLocked`。

OpenCode 分支注入：

- `CODEX_REMOTE_INSTANCE_BACKEND=opencode`
- `CODEX_REMOTE_OPENCODE_PROFILE_ID=<profileID>`
- `CODEX_REMOTE_OPENCODE_LAUNCH_JSON=<redacted-free launch material JSON>` 或仅注入 compiler 输出的 `OPENCODE_*` env。

如果 launch material 比较复杂，优先走 settings JSON env，再由 wrapper 写临时文件或展开 env；不要把长 JSON 作为命令行参数。

`headlessLaunchModeForBackend` 新增：

- `relayruntime.HeadlessLaunchModeOpenCodeACP = "opencode-acp"`

`internal/app/appserverargs` 同步识别 `opencode-acp`。

### 7.2 compatibility / restart

新增 OpenCode compatibility：

- surface desired `OpenCodeProfileID` 与 observed instance `OpenCodeProfileID` 一致才能复用。
- `OpenCodeAdmissionRef.ProfileRef.Revision` 不一致时，如果 active queue/pending prompt 指向旧 revision，不要静默升级；按 admission ref 编译旧 revision，或失败要求重新发送。
- `/mode opencode`、`/opencodeprofile`、bot default 切换都要触发和 Claude/Codex 一样的 workspace route restart / fresh workspace / exact-thread restore。

OpenCode prompt override：

- model/reasoning/plan 如果能通过 ACP `session/set_config_option` 表达，不触发 child restart。
- profile secret/baseURL/provider/project config/data isolation 改变必须重启 child。
- 如果当前 session 缺少动态 config option，命令应走 backend command profile reject/diagnostic，而不是改 surface 但不生效。

### 7.3 surface resume

surface resume store 要持久化：

- backend `opencode`
- `OpenCodeProfileID`
- `OpenCodeAdmissionRef`
- selected thread/session id
- workspace key / cwd

daemon 恢复时：

- 优先用 `session/resume` 恢复 OpenCode session。
- `session/load` 只用于 hydration/history，不作为正在执行输出。
- 找不到 session 时走现有 fresh workspace fallback，提示应和 Claude 类似，不暴露 ACP internal。

## 8. Wrapper Runtime

新增 `opencodeBackendRuntime`：

- `Backend() == agentproto.BackendOpenCode`
- `Capabilities()` 返回 OpenCode 默认能力
- `Launch()` 调用 `app.launchOpenCodeChildSession`
- `ObserveClient()` 默认只处理 parent/stdin 透传场景；headless 主要靠 relay command。
- `ObserveServer()` 委托 ACP adapter。
- `TranslateCommand()` 委托 ACP adapter，并负责本地 history/list 快捷路径如果 adapter 已缓存。
- `PrepareChildRestart()` 保存目标 session/cwd。
- `BuildChildRestartRestoreFrame()` 生成 `session/resume` 或 `session/load` 相关恢复请求。

`launchOpenCodeChildSession`：

- 解析 `OPENCODE_BIN` / launch JSON / config binary path。
- 使用 `execlaunch.CommandContext`，不得直接 `exec.CommandContext`。
- `cmd.Dir = workspaceRoot`。
- child args 为 `acp --cwd <workspaceRoot>`。
- child env 使用 `FilterEnvWithoutProxy(os.Environ()) + ChildProxyEnv + opencode launch env`。
- 不走 Codex synthetic initialize，也不走 Claude control bootstrap。
- 启动后 adapter 发送 ACP `initialize`，等待 response，确认 protocolVersion/agentInfo/capabilities；失败则 launch fail。

wrapper hello：

- hello backend/profile 使用 wrapper config 中的 OpenCode profile 字段。
- hello capabilities 使用静态 default；initialize response 的细节后续进入 runtime state/debug 或 capability events，不阻塞 hello。

## 9. ACP Adapter 设计

包名建议：`internal/adapter/acp`。

### 9.1 核心结构

```go
type Translator struct {
    instanceID string
    backendName string
    nextRequestID int
    nextSyntheticID int
    pending map[string]pendingRequest
    sessions map[string]*sessionState
    activeTurns map[string]*turnState
    pendingPermissions map[string]*permissionState
    hydration *hydrationState
}
```

id 域必须分开：

- JSON-RPC id：只用于 request/response correlation。
- ACP sessionId：映射 canonical threadId。
- OpenCode message id：只做 backend metadata / de-dupe。
- tool callID：映射 itemId。
- permission request id：映射 requestId。
- canonical turnId：由 relay command target 或 adapter synthetic id 决定。

response 必须按 JSON-RPC id 关联，不能按 stdout 顺序；黑盒已观察到 response 可乱序。

### 9.2 command mapping

| agentproto command | ACP/OpenCode 行为 | 备注 |
| --- | --- | --- |
| `threads.refresh` | `session/list` -> `threads.snapshot` | cwd/workspace metadata 必须保留 |
| `thread.history.read` | `session/load` hydration -> `thread.history.read` | replay chunk 不刷主消息 |
| `prompt.send` start new | `session/new` -> optional config set -> `session/prompt` | sessionId 即 threadId |
| `prompt.send` existing | ensure `session/resume` if needed -> optional config set -> `session/prompt` | cwd 缺失时按 capabilities fail |
| `turn.interrupt` | `session/cancel` notification | 不等待 response gate，最终靠 stopReason/turn tracker reconcile |
| `request.respond` | 回应 pending `session/request_permission` | option id 原样 round-trip |
| `model.list` | 从 `configOptions.model` 构造 catalog snapshot | 缺 option 时返回 unsupported snapshot |
| `process.child.restart` | child 重启后 `session/resume` | 成功后 emit child restart outcome |
| `process.exit` | wrapper 现有 stop/detach | 不主动发送 ACP `session/close`，避免把用户的 OpenCode session 从运行时视角关闭/删除；进程停止后的 turn reconciliation 沿用现有 tracker |
| `turn.steer` | 第一版默认不承诺同轮追加 | 若 OpenCode 后续有等价能力再开放 |
| `mcp.oauth_login.start` | 不支持 OpenCode MCP OAuth | 继承系统状态或用户本机处理 |

`prompt.send` 前置步骤：

1. 归一化 dispatch plan，确定 session/thread。
2. 如果需要新 session，发送 `session/new`，带 cwd、client capabilities、MCP servers。
3. 根据 prompt override 尝试 `session/set_config_option`：model、effort、mode。
4. 任一 required option set 失败，停止 prompt 并返回产品可读 error；不要半应用后继续。
5. 发送 `session/prompt`。
6. 建立 `jsonrpc id -> turnState`，后续 stream chunk 归入该 turn。

prompt input 映射：

- `InputText` -> ACP text content。
- `InputLocalImage` -> ACP image content，前提是 initialize capability 声明 image support 且路径在本地可读范围内。
- `InputRemoteImage` -> 先沿用现有远程面图片 staging/download 语义，再转 local image；无法 staging 时返回可操作错误。
- 其他文件附件第一版不伪装成图片或 embedded context；如果现有 Feishu 文件通道已经生成文本摘要，则作为 text content。
- initialize 未声明 image/embedded context support 时，相关输入 fail-fast，不把含图片 prompt 降级成丢图文本。

MCP session 参数：

- Feishu tool service 的 HTTP MCP server 在 `session/new` / `session/resume` / `session/fork` / `session/load` params 中以 ACP `mcpServers` 注入。
- OpenCode ACP `McpServer::Http` 要求 headers 直接出现在 JSON-RPC frame 中，不能像 Claude MCP config 那样依赖 env placeholder；因此 raw frame log 必须递归 redaction `Authorization`、token、key、secret 等敏感字段。
- MCP 注册失败不阻塞普通 prompt，但要产生 diagnostic；模型随后调用不可用工具时按 tool failed 映射。

### 9.3 session/update mapping

| ACP update | canonical event | 规则 |
| --- | --- | --- |
| `agent_message_chunk` | `item.started` + `item.delta` | 按 message/part 聚合，prompt response 不带 final text |
| `agent_thought_chunk` | `item.reasoning.summary_part_added` 或 reasoning item delta | 默认不新增用户可见特殊提示 |
| `user_message_chunk` | hydration/history item | live prompt 中通常不重复投影用户文本 |
| `tool_call` | `item.started` | tool kind 由 taxonomy 决定 |
| `tool_call_update in_progress` | `item.delta` / progress metadata | running snapshot 要去重 |
| `tool_call_update completed` | `item.completed` + file changes/progress | completed 后忽略重复 terminal |
| `tool_call_update failed` | `item.completed(status=failed)` | 不默认把 turn 标 failed |
| `usage_update` | debug/runtime usage context 或 token usage best effort | 不覆盖 prompt response per-turn usage |
| `available_commands_update` | capability/debug state | 不替代 Feishu 主菜单 |
| config option snapshot | thread settings / model catalog best effort | 只映射稳定字段 |

turn close：

- `session/prompt` response 的 `stopReason=end_turn` -> `turn.completed(status=completed)`。
- `stopReason=cancelled` -> `turn.completed(status=cancelled)`。
- JSON-RPC error -> `turn.completed(status=failed)` + normalized `system.error`。
- permission reject 后如果 OpenCode 仍 `end_turn`，turn 按 completed，tool item failed。
- child exit / process exit 时用现有 runtime turn tracker 做 reconciliation；不把 wrapper stop/detach 映射成 ACP `session/close`。

### 9.4 hydration

`session/load` / `session/fork` replay 进入 hydration mode：

- chunk 可以是 aggregate content，不一定是 live delta。
- 生成 `ThreadHistoryRecord` 或 `threads.snapshot` 更新，不触发 Feishu 正在生成卡。
- 同一 backend message id 已存在时覆盖/合并，不追加重复 assistant final。
- hydration events 标记 `TrafficClass` 或 metadata，供 projector/filter 使用。若需要新增 `TrafficClassHydration`，先在 #848 补状态/投影测试。

### 9.5 tool taxonomy

| OpenCode tool | agentproto 映射 | 备注 |
| --- | --- | --- |
| `bash` | command execution item | cwd、stdout/stderr、exit code 从 update metadata 提取 |
| `read` | read/file item | locations/preview 作为 metadata |
| `edit` / `write` / `apply_patch` | file change item | diff 优先来自 permission/update，缺失时做 preview |
| `grep` / `glob` / `list` | search/list item | 不强行伪装成 shell |
| `task` | dynamic tool item | review/agent 子任务可在 metadata 标 backend shape |
| `todowrite` / plan file tool | plan-like best effort | 有结构化 todo 才发 `turn.plan.updated` |
| MCP tools | `other` | 保留 server/tool display name，不强行归类 |
| unknown | `other` | debug trace 记录 raw kind |

Plan 策略：

- OpenCode `mode=plan` 只是 session config，不生成 `TurnPlanSnapshot`。
- 普通计划正文按 assistant text。
- todo/tool 结构稳定时才合成 plan snapshot。
- 确认请求按 request card；不展示“OpenCode 不支持 Plan snapshot”这类内部提示。

### 9.6 permission / request

ACP `session/request_permission` 映射到现有 `RequestPrompt`：

- option id 原样保存和回传。
- `once` / `always` / `reject` 只是 OpenCode option，不在我们侧自行扩展持久化语义。
- 多 option 超过 Feishu 按钮限制时转 structured form。
- unknown request kind fail-closed：返回 reject，并 emit protocol notice/debug。
- request owner routing、old-card replacement、resolved event 沿用现有 request state machine。

### 9.7 `fs/write_text_file`

OpenCode edit 流程会调用 ACP client method `fs/write_text_file`。第一版必须实现，不要让 edit 工具卡死。

处理规则：

- 只接受 workspace scope 内路径。
- 必须关联到当前 turn/tool/permission 上下文；无法关联时 fail-closed。
- 不在 permission approval 前写文件。
- 写入使用受控文件写入 helper，并记录 proposed/final diff。
- 如果 OpenCode 已在 native tool execution 中完成写入，handler 要能 idempotent 成功或返回明确冲突；具体行为用黑盒 fixture 固定。
- 任何写入失败映射为 tool failed，不映射成 daemon crash。

### 9.8 usage / errors

usage：

- prompt response usage 是 per-turn token usage 主来源。
- `usage_update` 是 context/cumulative 风格，先进入 debug/runtime metadata；不新增 context meter UI。
- cache/reasoning token 字段按现有 `ThreadTokenUsage` 最接近字段投影，字段缺失时不造值。

errors：

- 第一版只归一化影响用户下一步动作的错误：binary missing、initialize failed、auth-required、invalid model/mode、missing session、MCP registration failed、permission denied、write denied。
- 其余保留 raw code/message/details 到 debug trace，用户面走 Claude 类似的通用失败文案。
- stderr 上的 OpenCode diagnostic 不能污染 stdout；wrapper raw log/debug log 要采集并 redaction。

## 10. Product Command Profile

新增 `opencode` command display profile。第一版建议：

| command family | OpenCode 第一版 | 说明 |
| --- | --- | --- |
| `/stop` | visible/native | `session/cancel` |
| `/new` | visible/approximation | 新建 OpenCode session |
| `/list` `/use` `/history` | hidden allowed 或 visible 取决于现有菜单 | 走 session list/load/resume |
| `/workspace*` | visible/native | 产品层工作区逻辑仍归我们 |
| `/model` | visible/best_effort | 仅当 configOptions 暴露 model |
| `/reasoning` | visible/best_effort | 仅当 configOptions 暴露 effort |
| `/plan` | visible/native intent | 映射 mode=config option，不造 plan snapshot |
| `/access` | visible/approximation | 映射 OpenCode permission/mode，文案避免 sandbox 承诺 |
| `/opencodeprofile` | visible/native | 新增 profile switch |
| `/claudeprofile` `/codexprofile` | hidden reject | 指引切回对应 backend |
| `/review` | hidden/allowed only if available command | 语义是 OpenCode task/sub-session |
| `/compact` | hidden/allowed only if available command | usage 可为空 |
| `/patch` `/auto-continue` `/auto-whip` | hidden reject | OpenCode 会空吞，必须我们侧 preflight |
| `/cron` `/follow` VS Code migrate | hidden reject | 与 Claude 类似 |
| `/debug` `/status` `/menu` `/admin` | visible/native | 产品层命令 |

未知 slash command：

- 不直接送给 OpenCode。
- 只有在 `available_commands_update` 中出现，且经过 allowlist 认可，才可作为 backend command prompt。
- 否则在 wrapper/orchestrator preflight 返回明确 unsupported，避免 OpenCode 空 `end_turn` 造成误导。

## 11. 子单落点

### #846 profile and launcher

代码落点：

- `internal/config/opencode_profiles.go`
- `internal/config/opencode_profiles_test.go`
- `internal/app/opencodeprofile/*`
- `internal/app/daemon/admin_opencode_profiles.go`
- `internal/app/daemon/app_headless_opencode_profile.go`
- `internal/app/daemon/admin_runtime_requirements.go`
- `internal/runtime/headless_process.go`
- `internal/app/appserverargs/appserverargs.go`
- `internal/app/wrapper/entry.go`

完成标准：

- OpenCode profile CRUD/revision/ETag/reference check。
- compiler golden 覆盖 config/auth/project/data/permission/MCP、`small_model`、`subagentModel -> agent.general/explore.model`，并断言不生成顶层 `review_model` / `subagent_model`。
- binary resolver 和 runtime requirements 测试。
- daemon launch material 不泄漏 secret。
- API-key overlay 已由 #849 真实 smoke 补证；OAuth 管理保持 out of scope。

### #847 runtime adapter

代码落点：

- `internal/adapter/acp/*`
- `internal/app/wrapper/backend_runtime.go`
- `internal/app/wrapper/app_child_session_opencode.go`

完成标准：

- JSON-RPC id correlation：#847 已由 unit tests 覆盖。
- initialize/session list/new/resume/load/prompt/cancel：#847 已由 unit tests 覆盖，initialize/new/prompt/load replay 已由真实 OpenCode smoke 覆盖。
- `session/load` hydration：#847 已固定 OpenCode replay update 在 load response 前进入 history collector，不产生 live turn/item delta；更完整 raw JSONL golden 由 #848 承接。
- request permission/respond：#847 已由 unit tests 覆盖。
- `fs/write_text_file`：#847 已实现 permission-gated workspace write、once approval one-shot、symlink/junction escape fail-closed、越界 fail-closed 和 file patch event；真实 edit tool e2e 已由 #849 覆盖。
- config options / available commands / usage / error：#847 已覆盖 model option snapshot、available commands ignore、usage projection 和 JSON-RPC error；更完整 golden taxonomy 由 #848 承接。
- wrapper runtime integration：#847 已覆盖 runtime construction、command translation、child launch args/env 和 ACP initialize bootstrap；真实 daemon/e2e 已由 #849 承接。

### #848 canonical mapping and golden tests

代码落点：

- `internal/adapter/acp/testdata/*.jsonl`
- `internal/adapter/acp/*_test.go`
- `internal/adapter/acp/translator.go`
- `internal/adapter/acp/observe.go`
- `internal/adapter/acp/commands.go`
- `internal/adapter/acp/history.go`

执行结果：

- 已新增 raw JSONL fixture/golden：`internal/adapter/acp/testdata/canonical_session_updates.input.jsonl` -> `canonical_session_updates.golden.json`，固定 assistant/reasoning delta、tool lifecycle、todo plan snapshot、model config option 和 available commands debug-only 行为。
- 已补 tool taxonomy：`bash`/`execute` -> `command_execution`，`read`/`grep`/`glob`/`list` -> `dynamic_tool_call` + exploration metadata，`edit`/`write`/`apply_patch` -> `file_change`，`task` -> `delegated_task`，MCP -> `mcp_tool_call`，unknown -> `dynamic_tool_call` + generic metadata。
- 已补 tool lifecycle：`tool_call_update` 先到时补 `item.started`，terminal update 去重；失败工具只完成 tool item，不投影成 turn/system failure；terminal metadata 保留 command/cwd/exitCode/errorMessage。
- 已补 Plan best-effort：`todowrite` start 不进入可见 tool timeline，terminal update 有稳定 todo 结构时生成 `turn.plan.updated`；普通 plan text 仍按 assistant text，仍不暴露内部 carrier 缺口。
- 已补 permission bridge：response 仍原样 round-trip option id，同时按 option kind 识别 `allow_once`/`allow_always` 写授权；`once` 仍 one-shot，`always` 进 session-local runtime grant。
- 已补 usage/error/config 边界：prompt response usage 才投影 `thread.token_usage.updated`；ACP `usage_update` 和 `available_commands_update` 只进 debug；JSON-RPC error 分类为 missing session、invalid model、MCP failure、auth-required、permission denied 或 raw fallback；model `config_option_update` 更新 session state 并驱动 `model.list`。
- hydration 不刷主消息的 #847 测试继续保留；#848 增补了 load error 后 hydration state 清理，避免失败 load 后吞 live update。
- 验证证据：`go test ./internal/adapter/acp -count=1`、`go test ./internal/app/wrapper -count=1`、`go test ./...`、真实 OpenCode guarded smoke 和 `scripts/check/pre-commit.sh` 均通过；pre-commit 仅打印既有 cross-platform path 告警，最终 exit 0 / `pre-commit: passed`。

偏离/承接：

- `unknown slash` / command display profile 不是 ACP adapter 层能力，实际载体在 `internal/core/control`、orchestrator 和 Feishu menu；从 #848 移到 #849 实现和 e2e 验证。
- `available_commands_update` 第一版不替换产品命令菜单，只写 debug。后续若要允许 OpenCode backend slash passthrough，必须在 #849 做 allowlist 和用户可见 preflight。
- real edit tool e2e、MCP 注入和 API-key overlay 已由 #849 补证；#848 只固定 adapter 侧 raw frame 映射和受控 `fs/write_text_file` 行为。
- 2026-08-09 verifier 初次复核指出 #848 issue 完成标准仍把 command profile 当作本单交付项；已把该完成项正式纠偏为 #849 承接，#848 只要求 adapter 对 `available_commands_update` debug-only 有测试约束。

### #849 daemon integration and e2e

代码落点：

- `internal/core/orchestrator/*`
- `internal/core/control/feishu_command_display_profiles.go`
- `internal/app/daemon/*surface*` / headless restart compatibility
- web/admin types and UI if included
- black-box/e2e scripts or guarded tests for real `opencode-ai@1.18.15`

完成标准：

- `/mode opencode` / profile switch / workspace restart / exact-thread resume。
- command profile 行为和 Claude-like 降级。
- real OpenCode black-box：initialize、API-key profile overlay、prompt/cancel/resume/load、HTTP MCP、permission/edit。
- API-key profile overlay 测试完成并回写结论；OAuth 管理/refresh/多 OAuth profile 明确 out of scope。

当前执行结果：

- `/mode opencode`、`/opencodeprofile`、detached/fresh workspace/workspace-route restart 和 pending headless connect 已补 orchestrator/control/daemon 测试，OpenCode surface 不再在 connect 时退回 Codex。
- OpenCode profile catalog 会同步到 Service，profile switch 会冻结 `OpenCodeAdmissionRef`；同 profile 只有 revision 未变时才 no-op，revision 变化会刷新当前 profile。
- API profile daemon launch 会注入 `opencode-acp` launch mode、`CODEX_REMOTE_INSTANCE_BACKEND=opencode`、profile id、`OPENCODE_CONFIG_CONTENT` 和 `OPENCODE_AUTH_CONTENT`，且 config env 不泄漏 API key。
- 真实 OpenCode guarded smoke 已覆盖 prompt/thought/text/load、API-key auth/model overlay、permission/edit/file preview、cancel 和 ACP `mcpServers` HTTP MCP publication。
- raw frame log 已补递归 redaction，避免 OpenCode ACP MCP header 中的 bearer token 落盘。

首版偏离/边界：

- OpenCode persisted thread catalog 第一版不读取 Codex SQLite 历史，也暂不提供 OpenCode durable history catalog；`/use` 等历史入口只能看到在线/当前 runtime 可见会话，避免跨 backend 误曝。
- 自定义/API profile launch 必须带匹配的 `OpenCodeAdmissionRef`；缺失或 stale revision fail-closed。默认 `op_default` inherit profile 可以无 ref 启动。
- OpenCode observed config 不写回 workspace/default model/reasoning 配置，避免把某个 OpenCode 实例的 runtime snapshot 污染 Codex/Claude 默认值。
- `OPENCODE_AUTH_CONTENT` 真实 schema 是顶层 provider id map；旧 nested provider/apiKey 设计已废弃。
- `OPENCODE_CONFIG_CONTENT` 不能写 top-level `reasoning`；profile reasoning effort 只通过 provider model variant 近似注入，动态 `/reasoning` 仍取决于 OpenCode ACP `configOptions` 是否暴露 effort。
- OpenCode edit tool 的模型侧参数名是 `filePath`，不是 `path`；测试按真实 schema 固定。
- profile `permissionMode` 只映射 OpenCode 原生 `ask` / `allow` / `deny`；`plan` 等产品侧意图不写入 OpenCode config，也不对用户提示“Plan snapshot 不支持”。
- `process.exit` / stop / detach 不主动发送 ACP `session/close`；关闭仍走 wrapper child lifecycle 和 turn tracker reconciliation，不把内部 session close 暴露成用户能力。
- OAuth 管理、OAuth refresh、多 OAuth profile 和 OpenCode MCP OAuth 不进入第一版产品面；API-key overlay 已验证，系统 OAuth 只作为默认 inherit profile 的本机状态存在。

## 12. 测试矩阵

| 层 | 必跑测试 | 证据 |
| --- | --- | --- |
| state | backend normalize/display/capabilities、surface/instance/launch contract、workspace defaults、bot settings | Go unit tests |
| config | profile validation、revision/ETag、secret update、reference checks | Go unit tests |
| compiler | `OPENCODE_CONFIG_CONTENT` / `OPENCODE_AUTH_CONTENT` / project disable / data isolation / redaction | golden tests |
| daemon launch | start env, launch mode, pending headless, failure mapping | Go unit tests |
| wrapper runtime | entry args, child launch, initialize, command phases, restart restore | Go unit tests；#849 已补 daemon/e2e |
| ACP adapter | response乱序、session lifecycle、turn buffer、hydration、tool、permission、fs、usage、errors | Go unit tests；#848 补 raw JSONL golden |
| orchestrator | mode/profile switch、compatibility、restart/fresh fallback、command preflight | focused Go tests |
| Feishu projection | request card dynamic options、plan/text/tool projection不重叠 | focused projector tests |
| real OpenCode | `opencode-ai@1.18.15` black-box with fake provider/MCP | guarded Go smoke 已覆盖 prompt/load/auth overlay、permission/edit、cancel、HTTP MCP publication |
| repo gate | `git diff --check`、`scripts/check/pre-commit.sh` | command output |

黑盒测试必须记录：

- binary path、version、cwd、env 摘要。
- raw ACP frames 脱敏后保存为 fixture 候选。
- fake provider 捕获 authorization/model/baseURL。
- 系统 config/auth/data 目录扫描结果。
- stderr diagnostic 与 stdout nd-json framing。

## 13. 实现顺序

1. 先做 #846 的 backend identity + state contract + OpenCode profile schema/compiler。没有这层，后续 adapter 即使能聊天也无法证明 profile 隔离。
2. 接 #847 的 wrapper OpenCode runtime + ACP adapter。先用 mock ACP server 固定 JSON-RPC 和 mapping，再接真实 binary。
3. 接 #848，把黑盒 raw frames 收进 golden，补 hydration/tool/permission/usage/plan 的确定性测试。
4. 接 #849，把 orchestrator/Feishu/admin/e2e 串起来，并补 API-key overlay、MCP、permission/edit、cancel 的真实验证；OAuth 管理保持 out of scope。
5. 父 issue #844 关闭前再决定是否需要独立 verifier；协议/auth/cross-surface 风险未完全降到低风险前，父 issue 不关闭。

## 14. 当前未拍板项

没有阻塞实现设计的产品拍板项。

API-key overlay、HTTP MCP、permission/edit 和 cancel 已由 #849 guarded smoke 补证。OAuth 管理/refresh/多 OAuth profile 按产品决策不进入第一版；其他差异按 Claude 基线处理，不作为用户可见“不支持”项目扩散。

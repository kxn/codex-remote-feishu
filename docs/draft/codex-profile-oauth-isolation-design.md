# Codex Profile 与 OAuth 隔离设计

> Type: `draft`
> Updated: `2026-07-31`
> Summary: 统一 Codex/Claude 的用户可见 Profile 语义，设计 Codex OAuth 连接身份隔离、API Profile 运行认证隔离、可独立修改的上下文偏好、跨 Profile 会话恢复以及 Web/飞书管理交互，并明确共享同一 OS 用户时的安全边界。

## 1. 文档定位

本文定义 Codex Profile 的目标产品合同和技术实现边界。它取代旧的 `Web Codex Provider 管理设计`，并补齐当前实现尚未闭环的三部分：

1. 用户侧统一使用 `Profile`，不再暴露 `Provider` 作为产品概念。
2. 检测并保留 Codex 原生 ChatGPT OAuth 登录，将其投影为连接身份不可编辑、不可删除，但上下文偏好可写的内建 Profile。
3. Profile 切换必须真正作用于 URL、认证、模型、推理强度和上下文偏好，并能安全恢复已有会话。

本文是方案草案，不表示对应代码已经实现。当前代码中的 `/codexprovider` 和 `Codex Provider` 仍属于待迁移现状。

## 2. 背景与已确认问题

当前仓库已经具备 Codex 自定义 Provider 的主要骨架：

- Web 管理页可保存名称、Base URL、API Key、模型和推理强度；当前尚无独立上下文偏好。
- daemon/wrapper 可通过 Codex `-c` 参数和子进程环境变量投影自定义 Provider。
- surface 和 managed instance 已携带 `CodexProviderID`，不同 Provider 不会被直接当作同一个启动合同复用。
- 飞书 `/codexprovider` 已支持选择后重启当前工作区。

但当前实现存在根因级缺口：

1. Remote 生成的 `thread/resume` 没有显式携带目标 `modelProvider`。Codex 会恢复会话持久化的旧 Provider，表现为切换后仍访问旧端点，或因旧 Provider 未在新进程注册而恢复失败。
2. orchestrator 只物化 Provider ID/名称，不知道 Profile 模型与推理默认值。当前 prompt 冻结路径可能把产品默认模型再次下发，覆盖 Profile 中配置的模型。
3. Provider ID 没有修订号。用户编辑同一个 Provider 后，旧进程仍可能因 ID 相同而被误判为兼容。
4. 当前 `codex_provider_env.go` 试图自行解析 Codex 原生 Profile。上游已经改变过 Profile 文件形态，继续在 Remote 内复制上游合并规则会形成永久兼容负担；目标边界应由目标 Codex 加载原生配置，Remote 只观察其有效结果。
5. 当前产品没有区分 Codex 原生 OAuth 凭据和本系统管理的 API Key，无法向用户明确保证“自定义配置不会覆盖 OAuth”。

## 3. 目标与非目标

### 3.1 目标

- Web 和飞书统一使用 `Claude Profile`、`Codex Profile`。
- Codex Profile 可管理与 Claude Profile 对齐的连接级字段：名称、端点、凭据、主模型、辅助模型和推理强度。
- 现有 ChatGPT OAuth 登录被发现后，形成一个连接身份只读的 Codex Profile，可在机器人菜单中选择并独立设置上下文偏好。
- Claude/Codex 每个 Profile 都有独立上下文偏好；内建 Profile 的连接身份保持只读，但允许修改该运行策略。
- 自定义 API Profile 不读取、不更新、不登出 Codex 原生 OAuth 凭据。
- 多个 managed instance 可同时使用不同 Profile，进程环境和启动合同彼此隔离。
- 切换 Profile 后可继续当前会话；目标 URL、认证、模型、推理强度和上下文偏好必须与目标 Profile 一致。
- 不修改用户原有 `~/.codex/config.toml`、原生 Profile 文件或 OAuth 凭据。

### 3.2 非目标

- 不把 Codex Remote 变成多 OAuth 账号凭据保险库。
- 不复制或导出 OAuth access token、refresh token、`auth.json` 或 keyring 内容。
- 不开放任意 Codex 原生 Profile 字段，例如 MCP、hooks、sandbox、feature flags。
- 不支持运行中 turn 的热切换；Profile 切换只在当前工作空闲后通过重启/重连生效。
- 不允许 Web 创建 OAuth Profile。OAuth Profile 只能来自 Codex 原生登录探测。
- 不为每个 Profile 创建一套完整 `CODEX_HOME`。
- 不把同一 OS 用户下的 app-server、shell、hooks 或其它本地进程视为凭据保密边界。方案 C 保证产品运行认证不串用、不写坏 OAuth，不承诺抵御同用户任意代码读取 app config、进程环境或共享 `CODEX_HOME`；若需要该级别隔离，必须另行采用独立 OS 身份/凭据代理/完整 Home 隔离方案。

## 4. 方案选择

### 4.1 方案 A：每个 Profile 独立 `CODEX_HOME`

优点是认证和配置天然隔离。缺点是 Codex 会话、SQLite 状态、skills、缓存和 keyring 存储键都与 `CODEX_HOME` 相关；若复制或软链其中一部分，会依赖上游私有目录结构，并容易产生同一会话多写者问题。

结论：不采用。

### 4.2 方案 B：复制 OAuth 凭据到本系统 Profile

这种方式理论上可保留多个 OAuth 身份，但新版 Codex 可能使用系统 keyring，外部无法可靠复制；复制 refresh token 也会扩大敏感凭据的存储面和刷新竞争。

结论：不采用。

### 4.3 方案 C：共享会话 Home，按进程选择运行认证

OAuth Profile 继续只读使用 Codex 原生凭据存储。自定义 API Profile 仍共享用户 `CODEX_HOME` 中的会话可见性，但 app-server 使用进程内临时 credential store、专用 Provider 和只属于当前 Profile 的 API Key 环境，不读取或写入原生 OAuth store。

结论：采用。它满足“不同 Profile 的请求认证互不串用、API Profile 不覆盖 OAuth 凭据”的产品目标，同时保留跨 Profile 会话可见性。它不是恶意同用户进程的 secret sandbox；这个边界必须在安全合同中明确，不能用“进程隔离”一词暗示更强保证。

## 5. 用户可见合同

### 5.1 最终用户

管理当前机器 AI 连接配置，并从飞书机器人为当前工作选择连接身份的用户。

### 5.2 当前任务

- 在 Web 中查看、创建和编辑 Codex Profile。
- 识别哪个 Profile 由 Codex 登录管理，哪个 Profile 由本系统管理。
- 在飞书中为当前机器人/工作区选择 Profile。

### 5.3 允许展示

- Profile 名称和类型：`ChatGPT 登录`、`本机默认`、`API`。
- 端点地址、模型、推理强度和上下文偏好。
- API Key 是否已保存，不展示具体值。
- OAuth 登录是否被检测到、最近一次检测结果、脱敏账号提示和套餐类型（上游可用时）。
- 当前选择、切换中、切换成功、不可用原因。

### 5.4 禁止展示

- OAuth token、refresh token、完整 `auth.json`、keyring 键和值。
- API Key 明文。
- `model_provider`、`env_key`、`cli_auth_credentials_store`、`CODEX_HOME` 等实现字段。
- 内部 Profile 修订号、实例 ID、启动参数和协议 payload。

## 6. Profile 产品模型

### 6.1 Profile 类型

Codex Profile catalog 是以下三类记录的并集：

| 类型 | 来源 | 连接字段可编辑 | 上下文偏好可编辑 | 可删除 | 启动语义 |
| --- | --- | --- | --- | --- | --- |
| `native` | 用户现有 Codex 配置 | 否 | 是 | 否 | 不覆盖原生 Provider/认证/模型默认；仅按独立偏好覆盖 context |
| `oauth` | Codex 原生 ChatGPT 登录探测 | 否 | 是 | 否 | 强制使用内建 OpenAI Provider 和原生 OAuth 存储 |
| `api` | Web 创建或旧 Provider 迁移 | 是 | 是 | 是 | 使用受管 Base URL/API Key/模型配置，隔离原生认证 |

`native` Profile 固定存在，展示名为 `本机默认`。检测到 OAuth 后，额外保留固定 ID 的 `oauth` Profile，展示名为 `ChatGPT 登录`。即使本机默认当前也使用同一 OAuth，两者也不能合并：前者会继续跟随用户的 Provider、环境和配置变化，后者必须锁定内建 OpenAI Provider 与 Codex 原生持久凭据，并清除可能抢占认证的外部环境变量。

Web 必须用副文案解释这两个入口的差别，不得只展示两个看似等价的名称：

- `ChatGPT 登录`：固定使用 Codex 当前登录账号。
- `本机默认`：连接与模型默认跟随这台机器现有 Codex 配置；上下文偏好可单独设置。

### 6.2 数据结构边界

```go
type CodexProfileKind string

const (
    CodexProfileNative CodexProfileKind = "native"
    CodexProfileOAuth  CodexProfileKind = "oauth"
    CodexProfileAPI    CodexProfileKind = "api"
)

// 只用于需要精确复现历史定义的 actual/frozen 状态。
// bot default 和 route desired selection 只保存 ProfileID，不保存 Revision。
type CodexProfileRef struct {
    ID       string
    Revision uint64
}

// 上下文偏好不属于连接定义。只读 Profile 也拥有独立、可修订的偏好记录。
type CodexContextPreferenceRef struct {
    ProfileID string
    Revision  uint64
}

// admission 后的 queue/pending/active/recovery 保存这个完整精确引用。
type CodexAdmissionRef struct {
    ProfileRef           CodexProfileRef
    ContextPreferenceRef CodexContextPreferenceRef
}

type CodexContextPreference struct {
    ProfileID string
    Revision  uint64
    ETag      string
    Mode      string // codex_default | price_guard_272k | extended_1m
}

// 只用于 daemon 私有配置存储，禁止直接作为 HTTP 或状态机 DTO。
type CodexAPIProfileSecretConfig struct {
    ID                   string           `json:"id,omitempty"`
    Revision             uint64           `json:"revision,omitempty"`
    CredentialGeneration uint64           `json:"credentialGeneration,omitempty"`
    ConnectionGeneration uint64           `json:"connectionGeneration,omitempty"`
    Kind                 CodexProfileKind `json:"kind,omitempty"`
    Name                 string           `json:"name,omitempty"`
    BaseURL              string           `json:"baseURL,omitempty"`
    APIKey               string           `json:"apiKey,omitempty"`
    Model                string           `json:"model,omitempty"`
    ReviewModel          string           `json:"reviewModel,omitempty"`
    ReasoningEffort      string           `json:"reasoningEffort,omitempty"`
}

// Catalog 持久化形态：CurrentRevision 指向用户当前定义；RetainedRevisions
// 只为仍被运行态/队列引用的旧合同保留，引用释放后立即 GC。
type CodexAPIProfileRecord struct {
    ID                string
    CurrentRevision   uint64
    Revisions         []CodexAPIProfileSecretConfig
}

// Web、orchestrator 和飞书只接触这个脱敏投影。
type CodexProfileSummary struct {
    ID              string
    Revision        uint64
    ETag            string
    Kind            CodexProfileKind
    Name            string
    BaseURL         string
    Model           string
    ReviewModel     string
    ReasoningEffort string
    StatusCode      string
    Available       bool
    HasAPIKey       bool
    Editable        bool
    Deletable       bool
    ContextEditable bool
    ContextPreference CodexContextPreference
}
```

说明：

- `CodexAPIProfileSecretConfig` 和 `CodexProfileSummary` 必须是不同类型，禁止通过清空 `APIKey` 后复用 secret-bearing struct 作为响应。
- `CodexContextPreference` 是独立的非 secret 策略记录。它不能塞进 API secret definition，也不能因为 native/oauth 的连接身份只读而变成不可写。
- API Profile 创建时在旧 `CanonicalCodexProviderID` 无法生成的保留命名空间内生成稳定 opaque ID（例如 `cp_<uuid>`）；名称可编辑但 ID 永不改变，PUT path 不能修改主键。native/oauth 同样使用该不可碰撞命名空间中的固定 ID。旧 Provider ID 可原样迁移且不会与新内建/opaque ID 碰撞，新 schema 不再把新 ID 送回旧 canonicalizer。
- Profile 名称在 trim + Unicode case fold 后必须唯一；POST 重名返回冲突，不能沿用当前 Provider API“同名创建等于隐式更新”的行为。名称唯一性只服务人类识别，不参与引用身份。
- 输入上限由 Catalog 与 Web 共用同一合同：名称最多 64 Unicode code points；Base URL 最多 2048 UTF-8 bytes；model/review model 最多 256 bytes；reasoning effort 最多 64 bytes；API Key 最多 16 KiB。名称/model/review/effort 禁止换行与控制字符，去除首尾空白后保存；API Key 不做 trim 或大小写规范化。创建或实际替换 Key 时拒绝空值、NUL/CR/LF 和超限值；更新请求中的 omitted/空字符串是“保留已保存 Key”的控制语义，不进入凭据值校验，避免悄悄改写真实凭据。
- `Revision` 是 Profile 定义的单调递增版本。名称、端点、Key、模型或推理强度变化都必须递增。
- 上下文偏好变化只递增 `CodexContextPreference.Revision`，不推进 Profile Definition Revision、Credential/Connection Generation；新 admission 由两类精确 ref 共同形成新的 `ThreadPolicyID`。
- `CredentialGeneration` 只在 API Key 实际变化时递增；它是不可逆推出 Key 的 opaque 代次，用于判断 child env 是否必须重建。名称或模型编辑不能伪装成凭据变化，运行时也不能用 Key 的无盐摘要替代该代次。
- API `ConnectionGeneration` 在 Base URL、Provider 注册字段或 CredentialGeneration 变化时递增；名称/model/review/reasoning 单独变化沿用原值。resolver 再把它与 capability generation 组合成最终 Connection Contract。
- 无语义变化的更新不递增 Revision。服务端必须比较非空新 Key 与已保存 Key 后再决定是否变化，不能因为响应不回填 Key 就把每次保存都视为修改。
- definition item ETag 只由 Profile ID 与 current Definition Revision 生成，preference item ETag 只由 Profile ID 与 current Preference Revision 生成；两者都是不含 secret 或其摘要的 strong opaque validator，不能混用，也不进入用户可见文案。
- 更新不是原地覆盖 secret record：Catalog 原子写入新的不可变 Revision 并推进 `CurrentRevision`。被 queue/dispatch、route actual、pending/active child 或 recovery lease 引用的旧 Revision 连同 Key 暂时保留；最后一个引用 owner 持久提交释放后才可 GC。Web/list 只展示 current，不暴露历史 secret revision。
- 上下文偏好同样使用不可变 revision 和 lease retention；已入队动作继续使用旧偏好，最后一个 `CodexAdmissionRef` 释放后才 GC。Profile definition 与 preference 的 lease 必须作为一个 admission 事务获取或释放，不能只保住其中一半。
- OAuth/native Profile 不进入用户可写 `CodexAPIProfileSecretConfig`；API 返回时由 catalog projector 合成只读 summary。
- `ReviewModel` 对应 Codex 的 `review_model`，它是 Codex 最接近 Claude `smallModel` 的稳定辅助模型字段，但 Web 文案使用“审阅模型”，不伪装成完全相同的语义。
- API Key 的持久副本仅存在 daemon 的 secret-bearing config 中，运行时只额外进入目标 app-server 的 child env。orchestrator、surface snapshot、instance hello 和 Web summary 都不得携带它。

Profile Definition Revision 只回答“用户保存的是哪一版定义”，不能直接代表进程兼容或线程实际设置。名称变化不应重启实例，Key/端点变化必须重建连接，而模型/reasoning 变化应进入新线程策略；这三种判断分别由 `ConnectionContractID`、`CodexThreadPolicy` 和 `CodexEffectiveThreadContract` 承担，不能继续塞进一个总 Revision。

### 6.3 只读 OAuth 描述符

OAuth 探测结果保存为不含凭据的状态记录：

```go
type CodexOAuthProfileState struct {
    ProfileID          string
    Revision           uint64
    AuthGeneration     uint64
    Status             string
    AccountHint        string
    PlanType           string
    LastCheckedAt      time.Time
    LastProbeErrorCode string
    AvailabilityCode   string
}
```

- 该描述符单独持久化；“保存 OAuth Profile”只保存固定 Profile 身份、修订和脱敏探测状态，不保存任何凭据副本。
- `Status` 是 `detected / missing / unknown` 三态：识别到 ChatGPT 登录、确认没有 ChatGPT 登录、探测本身未完成。`LastProbeErrorCode` 只解释 `unknown` 的探测失败；`AvailabilityCode` 解释“已检测到但当前产品合同不可启动”，不能把两者塞进同一个错误槽。
- summary 的 `Available` 由 `Status=detected`、`AvailabilityCode` 为空且 capability preflight 通过共同派生，不得只看到账号就宣称可用。
- 不持久化 `AccountFingerprint`。仓库没有可复用且生命周期明确的机器 HMAC 密钥，上游也不返回稳定账号 ID；引入指纹会制造“可以准确识别历史账号”的错误承诺。
- `AccountHint` 最多保存脱敏邮箱；无法安全获得时留空。
- `AuthGeneration` 表示当前 daemon 已知的认证代次，不声称等同账号身份。收到 `account/updated`、daemon 新生命周期首次重新确认已有 OAuth、脱敏账号提示明确变化或 `detected <-> missing` 迁移时推进；同一生命周期内无事件且可观察结果不变的手动刷新不推进。无法稳定识别账号时允许保守多重启，不能错误复用旧合同。
- 一旦成功发现 OAuth Profile，后续 `missing` 或 `unknown` 都不会删除该 Profile；`unknown` 不能被误写成“已退出”。
- 不可用 OAuth Profile 不能静默回退到本机默认或 API Profile。
- `AuthGeneration` 推进、状态在 `detected <-> missing` 间确认迁移，或 `AvailabilityCode` 在官方可用与自定义部署不受支持之间变化时递增 OAuth `Revision`；短暂 `unknown` 与同生命周期无变化刷新不递增。由于没有稳定账号 ID，Revision 的目标是避免错误复用，不保证识别同一账号的重复登录。

## 7. OAuth 探测与保护

### 7.1 探测方式

OAuth 探测使用短生命周期 Codex app-server probe，不直接解析 `auth.json`：

1. 使用用户原始 `CODEX_HOME`。
2. 清除 `OPENAI_API_KEY`、`CODEX_API_KEY`、`CODEX_ACCESS_TOKEN`、`OPENAI_ORGANIZATION`、`OPENAI_PROJECT`、`CODEX_REFRESH_TOKEN_URL_OVERRIDE`、`CODEX_REVOKE_TOKEN_URL_OVERRIDE`、`CODEX_APP_SERVER_LOGIN_CLIENT_ID` 和本系统旧 Profile Key，避免环境认证或调试端点改变证据。
3. 临时覆盖 `model_provider="openai"` 和 `openai_base_url=""`。后者会让 built-in OpenAI Provider 回到当前 auth mode 对应的默认模型端点，不能只设置 Provider ID 后继续继承用户自定义 `openai_base_url`。
4. 完成 app-server initialize。
5. 调用 `account/read`，固定 `refreshToken=false`，取得脱敏账号摘要。
6. 通过认证证据 adapter 取得精确 auth mode：优先未来稳定协议字段；当前受支持版本调用 deprecated `getAuthStatus(includeToken=false, refreshToken=false)`。
7. 只有 `account.type=chatgpt` 且 `authMode=chatgpt` 同时成立才记为 Codex-managed OAuth。`chatgptAuthTokens`、`headers`、`agentIdentity`、`personalAccessToken` 和无法取得 auth mode 的结果都不能创建 OAuth Profile。
8. 认证证据解析完成后立即退出 probe，不保留长期进程。

选择 app-server 协议而不是文件探测的原因：Codex 可将凭据存放在 file、keyring 或 auto backend；协议边界能覆盖这些存储方式且不暴露 token。`account.type=chatgpt` 不是充分证据，因为外部 ChatGPT token、Agent Identity 和 PAT 也会投影成同一账号形态；当前必须用旧 `getAuthStatus` 补足 auth mode，后续只替换 evidence adapter，不把 deprecated 方法散落到 catalog 和 Web。

认证 probe 只判断持久认证类型，固定不主动刷新 token，也不创建 thread；检测时间不能写成“已联网验证”。不得为了补齐模型默认值额外调用 `thread/start(ephemeral=true)`：`ephemeral` 只关闭 rollout/state 持久化，Session 构造仍会加载 workspace/project config、exec policy、extensions、telemetry 并初始化/预热 MCP；`SessionStart` hook 虽然到首个 turn 才执行，但整个调用仍不是只读 probe。

OAuth 的模型策略固定为只读 `codex_default`，实际指令只在用户本来就需要的 managed `thread/start` / `thread/resume` 上形成，不提前制造一次假会话：

1. 对目标 workspace 调用 `config/read(cwd=<workspace>)`，只观察同一 child、同一 cwd 下显式的 model/reasoning/review preference；缺省字段显示“由 Codex 自动选择”，不能据此宣称已经取得隐藏默认值。
2. 真实 start/resume 始终显式传 `modelProvider=openai`，从响应取得实际 model 和可选的显式 reasoning。响应 reasoning 为 `null` 表示没有配置级 effort，不是启动失败。
3. reasoning 为 `null` 时，首个 `turn/start` 继续省略 `effort`，并在 Effective Thread Contract 中记录 `reasoningMode=codex_default`。Codex 若有该模型的 metadata，会使用模型默认；未知但合法的模型会使用 fallback metadata 并把 effort 留给 Provider 默认。两种都是上游受支持的执行语义，Remote 不得为了得到一个具体字符串而阻断 prompt，也不得把观察到的默认值重新伪装成用户显式设置。
4. `model/list` 只可作为同一 live child 的可选诊断证据：exact model 存在时可记录当时观察到的默认 effort，缺项时只保留上游 warning/provenance。它不能成为 dispatch gate、Profile 保存期校验或第三方/远端 catalog 权威。
5. review policy 采用同一 cwd 有效配置中的 `review_model`，缺省时按上游行为等于实际主模型。但 `config/read` 与 thread 建立不是原子快照，协议也不回显 review model；因此 OAuth/native 的 Effective Thread Contract 保留 `reviewModelMode=codex_config`，可附带 `config/read` 的观察值但不得冒充已回显 actual。行为正确性由受控 review fixture 验证。

当前 `model/list` 固定使用 `OnlineIfUncached`，不暴露 online/cache/bundled provenance，且共享 `models_cache.json` 没有按 Provider identity 隔离。Remote 不为补齐自动 reasoning 主动增加一次可能联网的 `model/list` 调用；已有实例目录只能用于展示、兼容性提示和诊断，不能改变 start/resume 已经形成的模型选择。

`chatgpt_base_url` 只影响登录及部分 ChatGPT 服务路由，不直接覆盖 built-in OpenAI Provider 的模型请求端点；ChatGPT auth 下且 `openai_base_url` 为空时，模型请求使用 Codex 编译期的 `CHATGPT_CODEX_BASE_URL`。因此首版 `oauth` Profile 只支持规范化后仍为官方默认值的 `chatgpt_base_url`，并固定清空 `openai_base_url`。检测到自定义 ChatGPT 部署时仍保留只读账号描述符，但标记 `oauth_deployment_unsupported`、禁止选择启动；用户仍可通过 `native` Profile 忠实沿用原生自定义配置。不能声称“保留自定义 auth URL”就等于支持该部署的模型请求。

probe 不覆盖 `forced_login_method`。app-server 初始化路径不执行 CLI/TUI 的 login restriction enforcement，该字段既不是 OAuth 证据，也不是修正 probe 的可靠手段。

### 7.2 探测时机

- daemon 启动后异步执行一次，不阻塞主服务就绪。
- Web 打开 Codex Profile 区时可显式刷新。
- 选择 OAuth Profile 启动实例前必须重新做轻量 preflight。
- 收到当前 OAuth 实例的 `account/updated` 时更新可用状态。

探测必须有独立超时和单飞控制。相同输入下的失败不能进入无限 retry；后台只在 daemon 重启、用户刷新、选择启动或 auth 事件变化时重新探测。

状态归类必须稳定：双证据成功且 `account.type=chatgpt`、`authMode=chatgpt` 为 `detected`；明确得到其它 auth mode 或空账号为 `missing`；无法证明 auth mode、进程/协议/超时/配置读取失败为 `unknown`。`detected` 后再独立判断官方部署兼容性；自定义 `chatgpt_base_url` 不得改写成 `missing/unknown`，而是保留账号证据并设置 `oauth_deployment_unsupported`。不可用 Profile 不进入正常可提交选项；旧卡、探测竞态或直接命令仍提交其 ID 时执行一次显式 preflight，失败后保留对应结构化原因，不启动错误实例。`account/updated` 提供 auth mode 但初始化时不会主动发送，只能触发重新 probe，不能替代首次证据读取。

### 7.3 OAuth 只读保证

- Web 的 definition update/delete API 对 `native`、`oauth` 固定返回只读错误；独立 context-preference PUT 允许写入。
- OAuth Profile ID 是保留值，创建/导入 API Profile 时不能占用。
- 本系统不调用 `account/login/start` 或 `account/logout` 来管理该 Profile。
- OAuth token 刷新仍由 Codex 原生 AuthManager 完成，本系统不接管刷新令牌。
- 用户在本机执行 `codex login/logout` 属于外部变更；下次 probe 更新 Profile 可用状态，而不是尝试恢复旧 token。

这里的“保存/隔离”含义是：本系统保存只读 Profile 身份和选择状态，并保证 API Profile 不读取或覆盖原生 OAuth；它不承诺保存多个历史 OAuth 账号。

## 8. 运行时投影与认证隔离

### 8.1 单一解析边界

daemon 提供唯一解析函数：

```go
ResolveCodexProfileRuntime(ref CodexAdmissionRef) (CodexProfileRuntimeProjection, error)
```

`CodexProfileRef` 与 `CodexContextPreferenceRef` 分别冻结连接定义和上下文策略；但不是所有选择状态都应保存精确 ref：

- bot default 和 route desired selection 只保存 Profile ID，表示“下次 admission 使用 current definition/preference Revision”。任一更新后不需要逐个改写这些选择。
- queue item、pending/active instance、route actual 和 recovery snapshot 保存精确 `CodexAdmissionRef`，表示“这个已经接纳的动作必须同时复现当时定义和上下文偏好”。resolver 找不到任一 Revision 时返回 `profile_revision_unavailable`，不能改取 current。
- prompt 入队前先把 desired Profile ID 解析成 current definition + preference 并冻结；两类更新与入队必须通过同一 admission/reference owner 串行化，不能在读取 current 与登记引用之间留竞态窗口。

投影分为五层：

- Profile Definition：Web 可编辑字段和 secret revision。
- Connection Contract：决定一个 app-server child 能否复用的 Provider 注册、认证代次、端点 identity 和 capability；不含名称、模型或 reasoning。
- Runtime Preference：独立保存所有 Profile 的上下文模式；连接身份只读不等于运行策略只读。
- Thread Policy / Effective Thread Contract：前者描述 Profile 默认、上下文偏好或 workspace 显式 override，后者记录真实 start/resume 后即将用于 turn 的 model/review/reasoning/context。
- Secret Launch Material：CLI overrides、需要清除的 child env 名称和只传给目标 app-server 的 secret-bearing env。

私有启动材料不能进入 orchestrator state、日志、错误 details 或 API response。

### 8.2 启动投影

| Profile | CLI 投影 | 认证环境 |
| --- | --- | --- |
| `native` | 不覆盖本机连接/模型默认；解析出实际 Provider ID，context 由 Thread Policy 单独投影 | 保留用户原生认证环境/存储 |
| `oauth` | 官方部署 preflight；`model_provider="openai"`、`openai_base_url=""` | 清除外部认证/端点覆盖环境后，使用 Codex 原生持久认证存储 |
| `api` | `model_provider`、完整 `model_providers.*`、`cli_auth_credentials_store="ephemeral"` | 移除原生 OAuth/API 认证环境，只设置当前 Profile 的专用 Key env |

API Profile 的用户可见 Profile ID 不能直接作为 Codex `model_provider`。resolver 必须生成带产品命名空间的内部 `CodexModelProviderID`，并确认它不与当前有效配置中的 Provider ID 冲突；内部 Provider 名称使用固定安全名称，不能把用户可编辑名称直接传给上游的 `OpenAI` 特殊判断。

API Profile 必须显式投影：

```text
-c model_provider="<internal-provider-id>"
-c model_providers.<internal-provider-id>.name="Codex Remote API"
-c model_providers.<internal-provider-id>.base_url="<base-url>"
-c model_providers.<internal-provider-id>.wire_api="responses"
-c model_providers.<internal-provider-id>.env_key="CODEX_REMOTE_CODEX_PROFILE_API_KEY"
-c model_providers.<internal-provider-id>.requires_openai_auth=false
-c model_providers.<internal-provider-id>.supports_websockets=false
-c cli_auth_credentials_store="ephemeral"
```

专用 env 名固定、值按实例注入，不能写入父 daemon 的全局环境；launcher 只在创建目标 app-server 的命令环境中设置它。上游 command-backed auth 虽可避免把 Key 作为 app-server env，但当前会无条件启用第三方 `/models` 后台刷新，启动后和约每 3 分钟重复请求；已移除的 `remote_models` flag 不能关闭。首版不采用 command auth，也不使用会把 Key 放入 CLI/config 的 `experimental_bearer_token`。

这里的选择是运行认证隔离，不是凭据对模型工具保密。Codex shell 默认继承父环境且默认忽略 key/token 排除，hooks 也继承 app-server 环境；API Key 本身还由同一 OS 用户持久化在 app config。实现和文案不得声称 shell/hooks/同用户进程绝对无法取得 Key。未来若把“Agent 不可见 API Key”提升为产品目标，需要独立安全设计，不能只换成可被同用户调用的 helper。

模型、review model 和 reasoning 不再写进进程 CLI；它们属于 thread policy，统一在真实 `thread/start` / `thread/resume` 和首个 `turn/start` 上显式决定。这样名称或模型默认值变化不会无理由重启认证连接，API Key/端点变化仍会因 Connection Contract 改变而重建 child。

### 8.3 模型默认值闭包

只覆盖 `model_provider` 会同时关闭 persisted thread metadata 对 Provider/model/reasoning 的整组回填，因此目标 Thread Policy 必须明确每个字段是 `explicit` 还是 `codex_default`。`codex_default` 是一种有意的策略，不是“忘了填字段后随便继承”：它只用于 native/oauth，并只允许目标 live Codex 在真实 start/resume 中解析。Resume Policy 执行 `preserve_thread_settings` 时可以额外产生 `preserved_observed` Effective mode，但不能反向改写目标 Thread Policy。

```go
type CodexThreadPolicy struct {
    ThreadPolicyID     string
    ModelMode          string // explicit | codex_default
    Model              string
    ReviewModelMode    string // explicit | same_as_main | codex_config
    ReviewModel        string
    ReasoningMode      string // explicit | codex_default
    ReasoningEffort    string
    ContextMode        string // codex_default | price_guard_272k | extended_1m
    ContextWindow      int64  // 0 | 272000 | 1000000
    AutoCompactLimit   int64  // 0 | 244800 | 900000
}
```

字段规则固定如下：

1. API Profile 的主模型和 reasoning 必须是 `explicit`；review model 显式填写时为 `explicit`，留空为 `same_as_main`。保存期只校验 model/effort 是无控制字符的非空值，不使用不可信第三方 catalog 预判模型支持档位。最新 Codex 的 reasoning wire value 是开放的非空字符串，已知值不是封闭 enum。
2. OAuth/native 没有 workspace 显式 override 时，model/reasoning 为 `codex_default`；OAuth 的 review policy 为 `codex_config`，即目标 cwd 的有效 `review_model` 优先，否则 `same_as_main`。Web 只显示显式 preference 或“自动”，不能显示尚未发生的“实际默认值”。
3. `/model`、`/reasoning` 快照只把对应字段改成 `explicit`，不会改 Profile 定义或上下文偏好。切换回没有快照的 Profile 时恢复该 Profile 自身 policy。
4. 真实 start/resume 先显式传目标 `modelProvider`。new thread 或 `apply_target_profile` 中，`explicit` 字段随同请求传入，`codex_default` 字段有意省略，因此上游不会恢复旧 thread metadata，而会使用目标 child 的当前 Codex config/catalog。`preserve_thread_settings` 是单独分支：在 Connection Contract 与 Thread Policy 均未变化时，可把上次 response 中可证明的 model 和非空 reasoning 作为 observed preservation 显式带回；这不是 Profile/用户 override，Effective Contract 必须标记 `preserved_observed` / `thread_observed`。原 reasoning 为 `null` 时没有可安全冻结的字符串，仍省略并保留 `codex_default`。
5. 响应返回实际 model 和 config snapshot 中的可选 reasoning。Remote explicit 或 preserved observed 值必须与响应一致；`apply_target_profile` 的 `codex_default` policy 下若 reasoning 非空，则记录该响应值并标记 source=`codex_config`，若为空则保留空值并标记 source=`codex_default`，首个 turn 继续省略 effort，由目标 Codex 使用模型 metadata 默认或 Provider 默认。API review policy 可闭包为 explicit/same-as-main；OAuth/native 保留 `codex_config` mode，并把非原子的 `config/read` 结果只作为 observed evidence。resume mode、model/review/reasoning mode、requested context 和 preference revision 齐全后形成可 dispatch 的 `CodexEffectiveThreadContract`；首个真实 `TurnStarted` 再补 observed effective context/clamp，不要求为取得它先制造假 turn。合同完整不等于必须猜出 Codex/Provider 内部采用的默认字符串。
6. API Profile 的 Provider 在首次请求中拒绝模型/档位组合时，保留原始 Provider 错误分类，禁止自动改档或换模型。OAuth/native 只有在 start/resume 没有返回实际 model/provider、显式值与响应冲突等协议合同不完整时才阻断；`codex_default` 本身不是错误。

首版不实现 Remote 自己维护的第三方 Provider `/models` 探测，也不把 Codex 内置/共享 cache 当作 API Profile 保存期目录。Web 可以提供 `none/minimal/low/medium/high/xhigh/max/ultra` 等已知预设和“自定义”输入，但服务端不得把这份 UI 建议列表当作封闭协议 enum。未来若增加第三方自动值，必须能证明默认模型和 reasoning 元数据来自目标 Provider；失败不得回退到内置目录或另一 Provider 的 cache。

### 8.4 上下文偏好与价格边界

Codex 使用三档下拉，不使用 checkbox：

| 用户选项 | `model_context_window` | `model_auto_compact_token_limit` | 语义 |
| --- | ---: | ---: | --- |
| `跟随 Codex` | 省略 | 省略 | 完整跟随目标 Codex 的在线目录、缓存或 bundled metadata；上游变化会改变行为 |
| `272K（费用优先）` | `272000` | `244800` | 把原始上下文预算限制为 272K，并在约 90% 时压缩，降低进入长上下文计价档的概率 |
| `1M（长上下文）` | `1000000` | `900000` | 请求 1M 本地预算；实际值仍受目标模型 metadata 的 `max_context_window` 截断 |

不增加 128K 等更多固定档：当前 Codex 默认、GPT-5.6 Sol 的价格分界和主要兼容边界都集中在 272K；额外档位没有稳定的上游产品语义。1M 使用 1,000,000 而不是模型特定的 1,050,000，保证跨模型/Profile 的用户语义一致。

截至 OpenAI Codex `3d1d26915a303c3b4765828f973f5464f8c28c5c`（2026-07-31），bundled `gpt-5.6-sol` 为：

- `context_window=272000`、`max_context_window=272000`、`auto_compact_token_limit=null`；
- 缺省 `effective_context_window_percent=95`，所以 turn 对外报告的有效窗口约为 258,400；
- `auto_compact_token_limit` 为空时按原始 context 的 90% 计算，约为 244,800。

用户记忆中的 300K+ 行为确实存在：2026-07-09 的 `3380969a29` 首次加入 GPT-5.6 系列时把 Sol/Terra/Luna 设为 372,000，约对应 353,400 有效窗口和 334,800 自动压缩阈值；2026-07-18 的 `d26a9bf671` 明确将 bundled 值调回 272,000。GPT-5.6 Sol 官方 API 当前声明 1,050,000 context、922,000 最大输入，并规定 input 超过 272K 时整次请求按 2 倍 input、1.5 倍 output 计价。因此 `跟随 Codex` 不能被描述为费用稳定策略，`1M` 必须显示可能显著增加费用的提示。

`272K（费用优先）` 也不是计费硬上限。当前上游 `run_turn` 在记录本轮 context diff、skills/plugins 注入和新用户输入之前执行 pre-turn compact，源码 TODO 明确尚未把这些 pending items 计入预估；一次突然加入的大输入仍可能让发出的 request 越过 272K。首版文案只能说“降低长上下文计价概率”。若要承诺绝不跨档，必须另做完整 request token estimator + fail-closed 拒绝/拆分，或等待上游补齐 pre-sampling 预算，不能靠当前压缩阈值冒充保证。

`model_context_window` 会被模型 metadata 的 `max_context_window` 截断。当前 bundled Sol 因 max 同为 272K，即使选择 1M 也只能得到约 258.4K 有效窗口；bundled GPT-5.4 的 max 为 1M，才可得到约 950K 有效窗口。ChatGPT OAuth 还可能从在线 `/models` 取得不同 metadata 并覆盖 bundled fallback，而 app-server `model/list` 不返回 context/max 字段，因此 Web 保存时无法可靠预判。Remote 在第一个真实 turn 的 `TurnStarted.model_context_window` 中记录 effective actual；请求 1M 但 observed effective 明显低于约 950K 时，返回一次结构化 `context_preference_clamped` 状态并在 Profile 详情展示“目标模型限制为 <actual>”，不阻断已开始的 turn，也不静默宣称 1M 已生效。

上下文偏好只进入 Thread Policy，不进入 Connection Contract。修改偏好不重启认证 child、不改 OAuth/native 连接身份，也不影响 running/已入队动作；下一次 admission 冻结新的 preference revision。`thread/start` / `thread/resume` 在 `config` 中投影上述两个字段，Effective Thread Contract 同时记录 requested mode、requested raw window、`TurnStarted` observed effective window、metadata source 是否可知以及是否发生 clamp。

### 8.5 环境清理

OAuth 和 API Profile 子进程都不能简单继承所有认证环境：

- `native` 保留原有环境，忠实跟随本机配置。
- `oauth` 先确认 `chatgpt_base_url` 是官方默认部署，再移除 7.1 列出的 API/token、组织、项目和认证端点覆盖变量，保证只读取 Codex 原生持久 OAuth；`openai_base_url` 通过 config override 清空。
- `api` 移除同一组变量和所有旧 Profile Key，再只向目标 app-server 设置当前 Profile 的专用 Key env；不得把该值写回 daemon 全局环境或其它实例。

环境验收证明的是 API A/API B/OAuth 之间不会串值，以及 API child 不会收到 OAuth/API 全局凭据；它不证明目标 app-server 的 shell/hooks 对当前 API Key 不可见。日志、错误和状态 DTO 仍禁止记录该 env 值。

### 8.6 版本门槛

`initialize` 响应只有 `userAgent`、`codexHome` 和平台字段，没有 capability 列表或独立服务端版本；`userAgent` 解析出的版本只能初筛，不能作为最终证明。

capability preflight 必须在隔离临时 `CODEX_HOME` 中运行无生产数据 fixture：

1. 用目标 strict config 启动 app-server，验证 env-key Provider、ephemeral credential store 和认证环境清理；确认启动期间不会因 Profile 功能自行探测第三方 `/models`。
2. API fixture 的 `thread/start` 成组传 typed `modelProvider`、typed `model` 以及 `config.model_reasoning_effort` / `config.review_model`，核对响应中的实际 Provider、模型和推理强度。
3. `apply_target_profile` auto-policy fixture 只传目标 `modelProvider`，证明 persisted metadata 不会回填；分别覆盖目标 config 含显式 reasoning 和未配置 reasoning 两种情况，前者记录 response value/source=`codex_config`，后者以 `codex_default` policy、空 effort/source=`codex_default` 继续运行而不伪造具体字符串；已知模型 metadata 默认和未知模型 fallback/Provider 默认都不能阻断。
4. review fixture 分别覆盖 API `explicit/same_as_main` 和 OAuth/native `codex_config`：前两者验证下发行为，后者只验证受控 review 行为并保留 mode/observed evidence，不把 `config/read` 当成 response actual。
5. 持久化临时 thread、重启 app-server，再分别验证 `preserve_thread_settings` 与 `apply_target_profile`：前者只回填上次 response 可证明的 model/非空 reasoning 并标记 `thread_observed`，原 reasoning 为 `null` 时仍省略；后者按目标 policy 解析。两者都不能恢复旧 Provider。
6. context fixture 分别覆盖三档：默认档不下发 override；272K 档观察到约 258.4K effective 并在约 244.8K 触发压缩；1M 档在 max=1M fixture 中观察到约 950K，在 max=272K fixture 中产生 `context_preference_clamped`。测试必须保留“新输入尚未进入 pre-turn compact 预算”的已知限制，不能断言 272K 档绝不产生 premium request。
7. fixture 只污染临时 Home，不读取用户会话、OAuth 或 API Key；无法证明任一能力时返回 `codex_capability_unsupported`。

不得在设计中猜测最低 Codex 版本号。发布支持矩阵只能记录通过上述 fixture 的构建；版本过旧或能力未知不能降级为 env key、共享 OAuth 存储或不完整 resume。

### 8.7 稳定架构端口

目标架构只承诺以下领域端口，不承诺当前 service、package 或文件名。并行重构稳定后，实现应先寻找最新 owner，再把这些端口落到现有边界；不能反过来把旧 `CodexProvider*` 载体当作目标设计。

| 端口 | 唯一职责 | 主要输入 | 主要输出 / 副作用 |
| --- | --- | --- | --- |
| `CodexProfileCatalog` | 管理 API Profile 定义并合成 native/oauth 只读项 | create/update/delete/list、OAuth 描述符 | secret config、redacted summary、Definition Revision |
| `CodexContextPreferenceCatalog` | 管理所有 Codex Profile 的可写上下文策略 | Profile ID、context mode、item ETag | immutable preference revision、redacted preference summary |
| `CodexOAuthProbe` | 通过 app-server 观察当前原生 ChatGPT 登录 | 显式 probe trigger、原始 `CODEX_HOME` | `detected/missing/unknown` 描述符；不写凭据、不自动重试 |
| `CodexRuntimeResolver` | 把精确 Profile 定义与上下文偏好解析成连接合同、线程 policy 和私有启动材料 | `CodexAdmissionRef`、auth/config evidence、capability fixture | connection contract、thread policy、secret launch material、稳定错误 |
| `CodexProfileSelection` | 统一修改 bot default、route desired 和 workspace+Profile 显式 override | 私聊切换、workspace route、model/reasoning 命令 | desired selection；禁止 surface 或 instance 反向写回 |
| `CodexInstanceContract` | 冻结并比较 managed child 的连接身份 | connection contract | desired/actual compatibility、restart reason |
| `CodexResumePolicy` | 根据连接迁移类型和 thread policy 生成 start/resume/turn 参数 | connection contract、thread policy、thread observed state | `preserve_thread_settings` 或 `apply_target_profile`、effective thread contract |

端口之间的依赖方向固定为：Profile Catalog/Context Preference Catalog/Probe -> Runtime Resolver -> Selection/Instance -> Resume Policy。Web 和飞书只调用 Catalog/Selection 的脱敏 DTO；wrapper/launcher 只接收 connection contract 和私有启动材料；translator 只接收已经解析的 connection/thread policy，不自行读 Catalog 或猜默认值。

`CodexRuntimeResolver` 的公共和私有输出必须是不同类型：

```go
type CodexConnectionContract struct {
    ProfileRef          CodexProfileRef
    ConnectionGeneration uint64
    ConnectionContractID string
    Kind                CodexProfileKind
    ModelProviderID     string
    ModelEndpointID     string
    ChatGPTEndpointID   string
    CapabilitySet       string
}

type CodexEffectiveThreadContract struct {
    ConnectionContractID          string
    ThreadPolicyID                string
    ResumeMode                    string // preserve_thread_settings | apply_target_profile
    ModelMode                     string // explicit | codex_default | preserved_observed
    Model                         string
    ReviewModelMode               string // explicit | same_as_main | codex_config
    ReviewModel                   string // 仅可证明具体值时非空
    ObservedReviewModel           string // 可选 config/read 证据，不冒充 response actual
    ReasoningMode                 string // explicit | codex_default | preserved_observed
    ReasoningEffort               string // response actual；未解析的 codex_default 为空
    ObservedDefaultReasoningEffort string // 可选诊断，不下发、不参与 policy 身份
    ModelSource                   string // remote_explicit | thread_observed | codex_resolved
    ReasoningSource               string // remote_explicit | thread_observed | codex_config | codex_default
    ContextMode                   string // codex_default | price_guard_272k | extended_1m
    RequestedContextWindow        int64
    RequestedAutoCompactLimit     int64
    ObservedEffectiveContextWindow int64 // TurnStarted 回显；首个真实 turn 前可为空
    ContextClamped                bool
}

type CodexSecretLaunchMaterial struct {
    CLIOverrides   []string
    ClearedEnvKeys []string
    SecretChildEnv []string
}
```

`ConnectionGeneration` 只在会改变 child 连接身份的输入变化时推进：API 的 Base URL/API Key generation，OAuth 的 auth generation/安全端点 identity，native 的有效 Provider/认证/端点 evidence，或 capability generation。Profile 名称、model、review model、reasoning 单独变化不推进它。

`ConnectionContractID` 使用版本化 canonical encoding 计算，例如 `v1:<sha256(non-secret-connection-identity)>`。编码明确排除 provenance 用的 Profile/Context Preference Revision、名称和 Thread Policy，只包含 Profile ID、认证/端点/Provider identity、对应 generation 与 capability。API Key 本身及其摘要不得进入，只使用 `CredentialGeneration`。它用于相等性判断和诊断关联，不是安全凭据、不作为跨机器身份，也不能替代精确 `CodexAdmissionRef`。

端点字段是 identity，不等于可直接展示的原始 URL。API Base URL 已禁止 userinfo/query/fragment，结构化解析后可持久化；规范化不得擅自折叠 path 或 trailing slash，除非已经用目标 Codex 的 URL join fixture 证明请求语义等价。`oauth` 只接受官方固定 ChatGPT auth/model endpoint identity。`native` 观察到的 URL 只有在同样满足安全形态时才能进入 public contract；若含 userinfo/query/fragment，原值只允许存在目标 child 的私有配置上下文，公共状态使用当前 daemon 生命周期内的 opaque endpoint generation，并在重启后保守判为未知/不兼容，不能把可能含凭据的 URL 或其可猜摘要写入状态和日志。

## 9. 实例合同与多实例隔离

### 9.1 选择作用域

Profile catalog 全局共享，但 Profile 选择不是全局开关：

- 飞书私聊中的切换与 Claude 现有行为对齐：更新当前 bot 的 Codex 默认值，并立即作用于当前 surface/workspace。
- 已经运行的其他 workspace/managed instance 保持原 Profile，不被批量重启；新建工作区继承该 bot 的当前默认值。
- workspace 恢复状态分别持久化 desired Profile ID 和上次 actual `CodexAdmissionRef`/Connection Contract，因此 daemon 重启后既能跟随 current，又能解释上次实际状态。
- 这里的“临时切换”指不修改 Profile 内容、不写用户 Codex 配置，也不影响其他实例；切回原 Profile 即恢复原连接合同。

这里存在两个不同事实，不能互相覆盖：

- `bot default`：未来新建 workspace 的默认 Profile，只由 bot capability/selection owner 持久化。
- `route desired`：当前 workspace 希望使用的 Profile ID，和 bot default 一样在下一次 admission 解析 current，但不反向改变其他 workspace。
- `route actual`：上次已经接纳/启动的精确 `CodexAdmissionRef`、Connection Contract 和状态，只用于 compatibility、recovery 与诊断，不能反写 desired。

Profile 切换命令通过 `CodexProfileSelection` 同时更新当前 bot default 和 route desired；当前没有 workspace 时只更新 bot default。surface 字段只是本次交互投影，instance hello 只是 actual state，二者都不是 desired selection 写源。恢复时先把 route desired ID 解析为 current admission ref，再用 route actual 判断 `preserve_thread_settings` 或 `apply_target_profile`；只有已经冻结的 queue/pending/recovery 动作才能要求重建旧 definition/preference Revision。

Profile definition 或上下文偏好更新都不改写 bot/route desired ID。下一条尚未 admission 的 prompt 自动使用两类 current Revision；已入队、pending 或 running 动作继续使用精确旧 admission ref。这样旧 Revision 既不会被 desired pin 永久占住，也不会在 Web 保存瞬间改写正在执行的任务。

### 9.2 运行实例合同

实例 hello 和 desired contract 从单一 ID 扩展为完整运行身份：

```text
CodexProfileID
CodexProfileRevision
CodexContextPreferenceRevision
CodexConnectionGeneration
CodexConnectionContractID
CodexModelProviderID
CodexModelEndpointID
CodexChatGPTEndpointID
CodexCapabilitySet
```

兼容判定必须同时满足：

- backend 为 Codex；
- Profile ID 相同；
- Connection Generation 和 Connection Contract ID 相同；
- 实际 Model Provider ID 与目标投影一致。
- 安全规范化或 opaque generation 表示的模型端点和 ChatGPT 服务端点与目标合同一致。
- 所需 capability 相同。

Connection Generation 来源必须覆盖三类 Profile：

- `api`：Base URL、内部 Provider 注册字段或 `CredentialGeneration` 变化时递增；名称、model/review/reasoning 单独变化不递增。
- `oauth`：认证 generation、`detected <-> missing` 状态确认迁移或 availability compatibility 变化时递增，不因短暂 `unknown` 变化；首版 endpoint identity 固定为官方 ChatGPT，检测到自定义部署直接不可用而不制造一个看似兼容的合同。由于没有稳定账号 ID，允许认证事件后保守递增。
- `native`：daemon 根据配置源代次和脱敏后的有效 Provider/认证/端点推进；若 native 依赖同一 OAuth，再纳入 OAuth auth generation。原始配置、凭据及其可离线猜测的无盐摘要都不能写入状态；无法证明合同未变化时生成新 generation，允许多一次重启但不能错误复用。

模型、review model、reasoning、context 由每个 thread 的 policy/effective contract 比较，不进入 child 兼容判定。一个兼容 child 可以承载同一连接上不同 workspace snapshot 的 thread；Profile 模型默认值或上下文偏好更新后，下一次 admission 使用新 policy，无需仅为改模型、context 或名称重启认证进程。

因此：

- 两个实例可以在不同工作区使用不同 Profile。
- 同一个 API Profile 改 Key/端点后旧 child 不再兼容；只改名称、thread 默认值或上下文偏好时，可复用连接但不能复用旧 thread policy。
- 已运行 turn 不因 Web 保存被强行中断；下一次 admission 解析两类 current Revision，并按 Connection Contract 决定复用或重建。
- newer Revision 与 active child 的 Connection Contract ID 相同时，admission 可把 child 的 launch provenance 关联到新 ref，但必须以 lease 转移事务完成：先获取新 ref、原子提交 child/recovery provenance，再释放旧 instance ref。旧 Revision 仍由真正使用旧 Thread Policy 的 running/queue/route-actual/recovery lease 保留。不能仅因 Profile ID 相同就执行这种 rebind，也不能先释放旧 ref 后再写新状态。
- 删除 API Profile 前，Catalog 必须通过统一 Reference Index 检查 bot default、route desired、active/pending instance、route actual、休眠 persisted surface/workspace、workspace+Profile override 和仍冻结在 queue item 中的精确 admission ref。任一引用存在都返回 `profile_in_use` 和脱敏引用清单，要求用户先切换或清空；不能只检查 active 进程，也不能删除后把悬空引用自动回退到 native。
- queue 中已经冻结的 prompt 不允许靠删除 Profile 改写合同。产品默认禁止删除，直到该队列项完成或取消；不通过复制 secret 到 queue 来绕过引用。

## 10. 会话恢复语义

### 10.1 根因修正

所有由 Remote 主动生成的 `thread/resume` 都必须携带目标运行时的 `modelProvider`，包括：

- 远程 prompt 恢复已有线程；
- compact 前隐式恢复；
- child restart restore；
- Profile 切换后的 exact-thread continuation。

原因是 Codex 在未显式覆盖时会优先恢复线程持久化的旧 Provider。

### 10.2 模型、推理强度与上下文

Codex 规定：只要 resume 显式传入 `modelProvider`，持久化的模型和推理强度 fallback 也会一起关闭。因此不能只补一个字段，还必须定义恢复策略。`ThreadPolicyID` 由不含 secret 的 canonical policy 计算，用于区分名称编辑与真正的线程默认变化：

1. 同一 Connection Contract 且 Thread Policy 未变化的普通重启：使用 `preserve_thread_settings`，显式传目标 `modelProvider`，同时携带 response 已证明的线程 model 和非空 reasoning；标记 source=`thread_observed`，不能冒充 Profile explicit。原 reasoning 为 `null` 时继续省略，因为 Remote 没有可证明的具体默认字符串。
2. Profile ID、Connection Contract 或 Thread Policy 任一变化：使用 `apply_target_profile`；仅 Definition Revision 因名称变化而不同，不应改变 thread settings。
3. 目标 Profile 在当前 workspace 存在用户显式 `/model`、`/reasoning` 快照：该快照覆盖 Profile 的闭包默认值。
4. API Profile 没有“自动”，在 start/resume 中提交完整显式 policy。OAuth/native 的 `codex_default` 只在真实目标 child 上解析，不注入产品硬编码默认值，也不提前创建 probe thread。

这要求 thread catalog/translator 继续携带 `modelProvider` observed state，并让 resume command 明确区分 `preserve_thread_settings` 与 `apply_target_profile`。

`CodexResumePolicy` 必须一次性决定 Provider、model、review model、reasoning 和 context 的 policy。调用方不能先补 `modelProvider`，再由 translator 无规则地猜剩余字段。显式 policy 的 start/resume 使用相同形态：

```json
{
  "modelProvider": "<resolved-provider-id>",
  "model": "<resolved-model>",
  "config": {
    "model_reasoning_effort": "<resolved-effort>",
    "review_model": "<resolved-review-model>",
    "model_context_window": 272000,
    "model_auto_compact_token_limit": 244800
  }
}
```

`modelProvider` 和 `model` 使用 typed 参数，reasoning/review/context 放入 `config`；不得把所有字段都塞入非类型化 map。context 默认档省略两个 context 字段，另外两档按 8.4 下发。每次 `thread/start` / `thread/resume` 后核对响应中的实际 Provider、model 以及 reasoning 与目标合同；context effective actual 在首个真实 `TurnStarted` 事件后补齐。`apply_target_profile` auto policy 可因目标 Codex config 返回非空 reasoning，也可返回 `null` 并继续交给 Codex 的模型 metadata/Provider 默认解析，两者都必须保留 `ReasoningMode=codex_default`，再由 `ReasoningSource` 和 response actual 区分，不能把前者伪装成 Remote 显式 override。`preserve_thread_settings` 带回的值则保留 `preserved_observed` mode。review model 若协议不回显，则在合同中保留 mode，`config/read` 只作为非原子观察证据，受控 fixture 验证行为；不能假装已由 response 证明具体 actual 或跨重启精确冻结 review model。

`apply_target_profile` auto policy 的请求仍显式传 `modelProvider`，但只提交 Remote workspace 快照中的显式字段；`codex_default` 字段有意省略。因为 modelProvider 已构成 model-group override，上游不会恢复旧 thread metadata，而是在目标 child 的当前配置/catalog 中解析。响应后以 `ThreadPolicyID`、`ResumeMode=apply_target_profile`、`ModelMode=codex_default`、实际 model、review policy mode 和 `ReasoningMode=codex_default` 形成完整 `CodexEffectiveThreadContract`；reasoning response 非空时记录为 `codex_config` source，空时记录为 `codex_default` source。首个 `turn/start` 都继续省略 effort，让 Codex 沿用刚形成的 config snapshot，或继续使用模型 metadata/Provider 默认；Remote 不把自动值转成显式 override。

目标模型不在 catalog、catalog 不可用或未声明 efforts 时，不得仅凭缺少本地 metadata 拒绝：API Profile 只做 model/effort 非空和字符边界校验，让目标 Codex/Provider 的首次请求决定组合是否支持；OAuth/native auto policy 使用 `codex_default`。只有已有同实例 catalog 明确声明该 model 不支持用户显式 effort 时，交互层才可在提交前拒绝并给出结构化原因。`preserve_thread_settings` 的 observed state 不完整时，显式 API policy 使用 Profile/override 值；auto policy 重新走目标 child 的实际解析。两者都禁止退回旧 Provider 或产品硬编码默认值。

### 10.3 Profile 级临时覆盖

- `/model`、`/reasoning` 仍是当前 workspace + Profile 下的显式运行时覆盖。
- 切换 Profile 前保存当前 Profile 快照；切换后恢复目标 Profile 快照，没有快照则使用 Profile 默认值。
- 覆盖 key 必须包含 `backend + workspaceKey + profileID`，不能跨 Profile 泄漏。
- 已入队 prompt 使用冻结值，不受后续 Profile 切换影响。
- API Profile 的 frozen definition/preference Revision 由对应 Catalog retained record 支撑，queue 不复制 Key。OAuth/native 的旧外部认证或本机配置若已变化且无法复现，dispatch 返回 `profile_revision_unavailable` 并保留可诊断目标；不得把旧队列静默重绑到当前账号/配置。上下文偏好不是 workspace 临时 override，必须通过 Profile preference API 修改并在下一次 admission 生效。

## 11. Profile 切换状态机

切换只允许在没有 running turn、dispatching request、queue item 和 workspace preparation 时开始。

1. 校验目标 Profile 存在且可用。
2. OAuth/API Profile 分别完成 auth preflight 和 Connection/Thread Policy projection；不创建 probe thread。
3. 保存当前 workspace + Profile 的显式 override 快照。
4. 提交目标 desired Profile ID，并将当前 route 标为 `switching`；本次切换 episode 同时冻结当时的 current `CodexAdmissionRef`，从这一步开始失败也不自动回退旧 Profile。
5. 停止不兼容的 managed instance，但不操作用户手动启动的 VS Code/CLI 实例。
6. 复用兼容 child 或使用目标 Connection Contract 启动新 managed instance。
7. 恢复原来的 route intent：`unbound`、`new_thread_ready` 或 exact-thread。
8. exact-thread 使用 `apply_target_profile` resume 策略；new/exact thread 都在真实 start/resume 后形成 Effective Thread Contract。
9. attach 且 effective contract 闭包成功后把 route actual 标为 `active`；失败则保留 desired 目标，把 actual 标为 `failed/unavailable` 并保存结构化根因，不发送用户 prompt、不无限重试、不回退旧 Profile。

`desired` 与 `actual` 必须分别投影。菜单勾选和“当前选择”来自 desired；状态文案来自 actual：切换中显示“正在切换”，失败显示“已选择但未生效”，成功后才显示“正在使用”。用户再次选择同一 Profile 属于显式重试，会创建新的 recovery episode；后台 tick 不能仅因为 actual 仍是 failed 自动重放。

Profile 切换失败必须区分：

- OAuth 已退出或不可读取；
- API Profile 缺少 Key/端点；
- start/resume 没有返回目标 Provider/model，或显式模型/推理合同被 Codex 拒绝；
- 已检测到 OAuth，但当前使用首版不支持的自定义 ChatGPT 部署；
- Codex 版本不支持隔离能力；
- 目标 Provider 无法注册；
- thread resume 被其他 app-server 占用；
- workspace/thread 已不存在。

## 12. WebUI 设计

### 12.1 信息架构

Admin 保持当前配置管理页面骨架，将两个区块统一命名为：

- `Claude Profile`
- `Codex Profile`

本轮不把两类 Profile 混进同一列表，也不新增营销式首页。两个区块继续复用现有列表 + 详情编辑模式，保证 backend 字段和错误不会交叉。

### 12.2 Codex Profile 列表

列表排序固定为：

1. `ChatGPT 登录`（曾成功探测到时）
2. `本机默认`
3. 用户 API Profile，按名称排序

每一行只显示：

- Profile 名称；
- 类型图标/短标签：`OAuth`、`本机`、`API`；
- 一行摘要：登录/配置状态、端点主机或主模型。

OAuth 不可用时保留在列表中并显示状态，不直接消失。列表不显示 Profile ID、配置路径和完整邮箱。

当 `本机默认` 当前也解析到同一个 ChatGPT 登录时，两项仍分别显示，但摘要必须分别写成“固定使用当前 ChatGPT 登录”和“跟随本机 Codex 配置”；不能用两个相同的“可用”摘要让用户猜差别。

### 12.3 OAuth/native 只读详情

只读详情使用与 Claude 内建 Profile 相同的详情容器，但不渲染禁用表单控件。内容包括：

- 名称；
- `由 Codex 登录管理` 或 `跟随本机 Codex 配置`；
- 当前登录/配置状态；
- 脱敏账号提示和套餐（可用时）；
- 显式模型/推理 preference；字段缺省时显示“由 Codex 自动选择”，已有 active thread 可在运行状态区显示其 actual 值，但不能回填成 Profile 定义；
- 可编辑的“上下文大小”下拉：`跟随 Codex`、`272K（费用优先）`、`1M（长上下文）`；
- `重新检测` 操作。

不显示连接定义的保存、删除和“覆盖认证”入口；上下文偏好使用独立的“保存上下文偏好”操作和 preference ETag。重新登录属于本机 Codex 任务，错误态只提供可执行提示，不在 Web 中收集 OAuth token。自定义 ChatGPT 部署显示“当前仅可通过本机默认使用此配置”，不能把它误写成退出登录，也不能提供一个必然失败的 OAuth 启动按钮。

### 12.4 API Profile 编辑

编辑字段与 Claude Profile 保持相同节奏：

1. 名称，必填；
2. 端点地址，必填；必须是绝对 `http/https` URL，不允许 userinfo、query 或 fragment，本地地址可以使用 `http`；
3. API Key，创建时必填，更新时留空表示保留；
4. 主模型：OAuth/native 只读显示显式 preference 或“自动”；API Profile 必填；
5. 审阅模型，可选，空值明确使用同一 Profile 的有效主模型；
6. 推理强度：OAuth/native 只读显示显式 preference 或“自动”；API Profile 必填。控件提供当前已知 effort 预设和“自定义”输入，最终保存非空 wire value；预设列表不是封闭协议 enum。
7. 上下文大小：所有类型都使用同一个三档下拉并保存到独立 runtime preference；切换时不改写模型字段。

API Key 只显示 `已保存` 状态，不回填。语义变化保存成功后递增 Revision，无变化保存保持原 Revision/ETag；反馈为“新配置将用于之后接纳的任务，已入队或正在运行的任务不变”。Key/端点变化会在下一次 admission 重建连接，名称或模型默认变化不应无理由重启兼容 child。

连接定义、上下文偏好和删除分别使用乐观并发：

- list JSON 中每个可写 summary 都携带独立 `etag` 字段；POST/PUT 单项响应同时返回同值的 HTTP `ETag` header。list 整体响应的 header 即使存在也只代表整份 catalog，禁止拿它更新某一项。PUT/DELETE 必须通过标准 `If-Match` 回传 item ETag；缺少前置条件返回 HTTP 428 `profile_revision_required`，ETag 不匹配返回 HTTP 412 `profile_revision_conflict`、最新 redacted summary 和最新 item `ETag` header。前端保留用户草稿并提示重新核对，不能把 HTTP 409 与 `If-Match` 语义混用。
- 创建时 API Key 必填；更新时 Key omitted 或空字符串均表示保留，非空表示替换。前端不能用占位掩码回传。
- POST 重名返回 HTTP 409 `profile_name_conflict`，不更新已有记录。
- 每个 summary 另带 `contextPreference.etag`。更新上下文偏好必须用该 ETag，缺失/冲突分别返回 428 `profile_preference_revision_required` 和 412 `profile_preference_revision_conflict`；禁止拿 definition ETag 更新 preference，反之亦然。
- 删除确认前先读取引用清单；确认时服务端必须重新检查 Revision 和引用，不能依赖前端 preflight。`profile_in_use` notice 展示可理解的 bot/workspace/任务引用，不展示内部实例 ID。

### 12.5 桌面与移动端

- 桌面保持左侧列表、右侧详情，主操作固定在详情底部。
- 移动端先显示单列列表；进入详情后使用页面级返回，不把双栏压成窄列。
- OAuth/native 详情和 API 编辑共用同一内容宽度，切换类型时页面不横向跳动。
- 错误只进入详情页现有 notice 槽位，不新增全页错误堆栈。

### 12.6 Web 状态

必须覆盖：加载中、探测中、已检测到 OAuth、OAuth 已退出、暂时无法检测、已检测到但属于不支持的自定义部署、从未发现 OAuth、Profile 保存中、上下文偏好保存中/成功/并发冲突、1M 被目标模型截断、272K 费用边界提示、删除冲突、版本过旧、读取配置失败。

Web API summary 使用结构化能力字段，例如 `editable`、`contextEditable`、`deletable`、`authKind`、`available`、`hasApiKey`；前端不能根据名称猜测只读状态。

### 12.7 Claude 上下文偏好对齐

Claude Profile 只提供两态 checkbox：未勾选为“模型默认”，勾选为“1M 上下文”。该偏好与 Codex 一样独立于连接身份和 secret definition，所以用户自建 Profile 与内建只读 `default` 都可以修改。

- 用户自建 Profile 保存不含终止 `[1m]` 的 base model；启用时只在运行投影的模型名末尾追加一个 `[1m]`，关闭时只移除末尾的 `[1m]`。规范化可折叠重复的终止后缀，但不得替换模型名中间的同名文本。
- Claude 官方只证明 `opus`、`sonnet`、`opusplan` alias 或受支持完整模型名的终止 `[1m]` 语法，没有证明 `default[1m]`。因此内建 `default` 未勾选时继续完整跟随 Claude 默认；勾选时明确固定投影为 `sonnet[1m]`，详情文案显示“1M 模式将使用 Sonnet”，不能让用户误以为仍在跟随任意默认模型。
- checkbox 变化只推进 Claude context preference revision；已入队/running 会话保持旧值，下一次 admission 冻结新值。若目标 Claude 版本、账号或模型拒绝 1M，保留上游错误并标记 `context_preference_unsupported`，不静默去掉后缀重试。

## 13. 飞书菜单与命令

### 13.1 命名

- canonical slash：`/codexprofile`
- 菜单名称：`切换 Codex Profile`
- Claude 对应文案同步为 `Claude Profile`
- `/codexprovider` 保留为 help/menu 均隐藏的兼容 alias；功能首次发布后至少保留一个完整 minor 发布周期，再在后续 minor 删除

机器人菜单只负责选择，不提供 Profile 创建、编辑、删除和 OAuth 登录。

与现有 Claude Profile 一致，切换入口只在机器人私聊中开放；群聊菜单隐藏，手输命令时提示到私聊修改 bot 默认值，避免群成员改写共享 bot 能力设置。

### 13.2 卡片交互

`/codexprofile` 继续使用现有 config-flow 参数卡：

- bare open 和菜单内进入：`keep`
- 提交 callback 只校验 lifecycle/catalog provenance、Profile ID 和当前 surface gate，并在 3 秒内 inline replace 为“切换请求已受理”或结构化校验错误；不得在 callback 同步窗口等待 OAuth probe、app-server 启停或 thread resume
- workspace 重启和恢复属于后台 owner episode；后续 actual 成功/失败通过该 owner 的普通 card create/patch 路径反馈，不依赖最多两次的 callback 延时更新 token，也不让菜单 launcher 长期承载业务状态
- stamped 卡保留现有返回菜单 footer；旧 lifecycle 卡点击必须拒绝

下拉项展示 Profile 名称和简短类型，不展示端点、邮箱和内部 ID。Profile 名称、账号提示、Provider 错误和其它动态值使用结构化字段/`plain_text` carrier，adapter 负责最后一跳格式，禁止上游预拼 raw markdown。OAuth 不可用 Profile 保留为卡片中的只读状态行，但不进入可提交下拉选项，并说明需要先在本机恢复登录或改用 `本机默认`；旧卡或探测竞态仍提交了不可用 ID 时，callback 必须同卡返回结构化原因，不能启动实例。

当前 command config form 会把全部候选直接写入普通 `select_static`，现有 path/target/thread paginator 不会自动作用于它。实现必须为 Profile 选择增加明确的 paginated-select flow：native 和 available OAuth 作为固定可选项，不可用 OAuth 只保留状态行；每页 API Profile 按稳定名称/ID 排序。翻页属于 `keep` 的 page-local action，保持当前选中值并同卡 replace。不能只调用现有 command option builder 后假设“分页已经复用”。

### 13.3 卡片预算

Profile 参数卡仍是单表单、单下拉、单 notice、分页行和单 footer，不聚合诊断详情。catalog 的产品级上限固定为 50 个 API Profile，Profile 名称最多 64 个 Unicode code points 且不能包含换行/控制字符；每一页都必须按真实 create/patch/callback-replace envelope 动态预算在 30 KB 和 200 elements 内，临界测试使用 64-code-point 多字节名称而不是短 ASCII 样例。超过上限在创建 API 时拒绝；预算不足时减小页大小，不能截断 catalog 或让某个 Profile 永远不可选择。

## 14. API、存储与迁移

### 14.1 API

新 canonical API：

```text
GET    /api/admin/codex/profiles
POST   /api/admin/codex/profiles
PUT    /api/admin/codex/profiles/{id}
PUT    /api/admin/codex/profiles/{id}/context-preference
GET    /api/admin/codex/profiles/{id}/references
DELETE /api/admin/codex/profiles/{id}
POST   /api/admin/codex/profiles/oauth/refresh
PUT    /api/admin/claude/profiles/{id}/context-preference
```

所有 list/write response 都使用 redacted summary。list body 的每个可写 definition 通过 `etag` 字段提供自己的 current strong ETag，单项 write response 再通过 HTTP `ETag` header 返回同一值；native/oauth 不伪造 definition ETag，但所有类型都返回独立 `contextPreference.etag`。definition 与 preference 的 PUT/DELETE 分别校验自己的 `If-Match`：缺少 precondition 返回 428，不匹配返回 412 并返回对应 current ETag；业务重名/占用仍使用 409。对 native/oauth 的 definition 执行 PUT/DELETE 返回稳定只读错误码，但 context-preference PUT 有效。references API 只返回脱敏引用类型、可识别名称和阻塞原因，不返回 secret、内部路径或 runtime payload。

### 14.2 配置迁移

- `codex.providers[]` 一次性迁移为 `codex.profiles[]`，每条记录 `kind=api`，ID 保持不变，初始 Revision 为 1。旧记录允许 model/reasoning 为空，而新合同要求必填；迁移必须保留 Base URL/API Key 和原 ID，并把该项标记为 `profile_definition_incomplete`，由 Web 要求补齐，managed launch fail closed，不能从本机 base config 猜值。
- bot default 与 route desired 中的 `CodexProviderID` 迁移为浮动 `CodexProfileID`；queue/pending/route actual 等已经接纳的状态迁移为 `CodexAdmissionRef{ProfileRef:{ID, Revision:1}, ContextPreferenceRef:{ProfileID:ID, Revision:1}}`。不能把所有旧字段机械替换成同一种 ref。
- 指向不存在旧 Provider 的引用迁移为 `profile_not_found` 诊断状态，保留原始 ID 供修复；不能映射到 native，也不能丢弃后把 UI 显示成默认值。
- API Profile secret config 继续进入权限受限的 app config；OAuth 只读描述符进入独立 runtime state store；两者不能写入同一用户可编辑数组。
- Codex 所有现存 Profile 迁移时创建 `codex_default` context preference Revision 1；Claude 所有现存 Profile 创建“模型默认” preference Revision 1。该迁移不改写已保存模型名；若旧 Claude 自建 Profile 的模型名已带终止 `[1m]`，迁移器原子拆成无后缀 base model + 已启用 preference，保证行为不变。
- 读取期允许旧字段作为迁移来源；新写入只写 Profile 字段，不能永久双写。
- `/codexprovider` 和旧 admin API 只作为 transport compatibility，不继续作为内部 SSOT；两者在功能首次发布后至少保留一个完整 minor 发布周期。
- 旧设计文档移入 `docs/obsoleted/`，本文成为新方案入口。

### 14.3 原生 Codex Profile 兼容

Remote 不解析或合并 `[profiles.<name>]`、`$CODEX_HOME/<name>.config.toml` 等上游原生 Profile 私有格式。固定 `native` Profile 由目标 Codex 在原始 `CODEX_HOME` 中加载自己的当前默认配置；Remote 通过 app-server `config/read` 形成连接 evidence，再通过真实 `thread/start` / `thread/resume` 形成 Effective Thread Contract，并据此推进脱敏 native generation。

若受支持 Codex 版本不能提供足够的有效配置观察证据，native Profile 仍可按原生方式启动，但不得被错误复用为已证明相同的 Connection Contract；需要 Profile-aware resume 时返回能力不支持或保守重启。Remote 不为追随上游文件格式变化维护第二套配置解析器。

### 14.4 状态所有权

| 事实 | 唯一写入 owner | 持久化 | 允许的消费者 | 禁止行为 |
| --- | --- | --- | --- | --- |
| API Profile current + retained secret revisions | Profile Catalog | app config，权限收紧 | Runtime Resolver、Reference Index/GC | secret struct 进入 HTTP/orchestrator/log；有引用时覆盖/GC 旧 Revision |
| Profile context preference current + retained revisions | Context Preference Catalog | 非敏感 runtime policy store | Runtime Resolver、Web、Reference Index/GC | 与 secret definition 共用 ETag；只冻结 definition 不冻结 preference |
| OAuth 脱敏描述符 | OAuth Probe coordinator | 独立 runtime state | Catalog、Web summary、preflight | 保存 token 或由 `unknown` 删除 Profile |
| native 连接描述符 | Runtime Resolver | 非敏感 generation/cache | Catalog、Instance Contract | 修改用户原生 config；持久化不安全端点原值 |
| bot default Profile ID | Profile Selection | bot capability/selection state | 新 workspace、菜单状态 | 保存 Definition Revision；surface/instance observed state 反向覆盖 |
| route desired Profile ID | Profile Selection | durable resume/route state | admission、菜单状态 | 固定旧 Revision；作为其他 workspace 的默认值 |
| route actual admission ref/contract | runtime manager | durable recovery/route state | recovery、compatibility、诊断 | 反向覆盖 desired；静默重绑任一 current revision |
| workspace+Profile 显式 override | Profile Selection | key=`backend+workspace+profile` | queue freeze、Resume Policy | 从 thread observed state 自动写入 |
| queue frozen admission ref/thread policy | queue owner | queue 生命周期 | dispatch、Profile Reference Index | 入队后随 definition/preference 编辑变化；复制 secret 到 queue |
| pending/active connection contract + launch provenance ref | runtime manager | runtime 生命周期/必要 recovery ref | compatibility、诊断、Reference Index | 携带 secret launch material；把 thread model 当 child identity；非原子换 ref |
| thread observed Provider/model/reasoning/effective context | translator/thread catalog | thread catalog 语义 | Resume Policy、展示 | 直接覆盖 desired selection 或反写 Profile preference |
| surface Profile 字段 | projector | 可重建投影 | 当前交互 | 成为独立 SSOT |

bot default 和 route desired 可以持有相同 Profile ID，但语义不同：前者决定未来 workspace 默认，后者决定当前 workspace 下一次 admission。route actual 才证明上次使用的精确合同。任何物理存储实现都必须通过 `CodexProfileSelection`/admission 单一 mutation owner；若并行重构后它们仍分属不同文件，需提供带代次的幂等事务和 crash recovery，不能由多个 handler 顺序裸写。

Profile Reference Index 是 definition/preference revision GC 与 Catalog 删除的唯一引用视图。每个 queue、dispatch、route actual、pending/active child 和 recovery owner 持有完整 admission lease，不能因为 child provenance 已 rebind 就释放仍在运行的旧 Thread Policy 或 preference 引用。更新事务必须先写新 Revision/current pointer，旧 Revision 默认继续保留；rebind 必须先 acquire 两类新 lease、提交 owner state，再 release 两类旧 lease；GC 只能在所有 owner 已持久提交释放后执行。daemon 启动时先从 durable queue/pending/runtime/recovery state 重建 lease，再清理孤立旧 Revision，不能按进程内计数直接删除。任一 `CurrentRevision` 永不作为 retained history 被 GC；删除整个 Profile 仍必须先通过完整引用检查。

### 14.5 Expand / migrate / cutover

迁移必须在 daemon 启动写流量开放前由单一 coordinator 执行：

1. `expand`：新增 `codex.profiles[]`、OAuth descriptor、Claude/Codex context preference、浮动 desired selection、精确 frozen admission ref、Connection Contract 和 Thread Policy schema；旧运行时仍不读取新字段。
2. `plan`：只读加载并校验 config、bot capability、surface/route resume 和 workspace override 旧状态，生成确定性迁移计划；任何损坏或冲突先进入 degraded 诊断，不边读边改。
3. `migrate`：`codex.providers[]` 一对一变成 `kind=api`、Revision=1、CredentialGeneration=1、ConnectionGeneration=1，并为所有 Claude/Codex Profile 建立 preference Revision 1；字段不完整项保持可见但不可启动。旧 `default` 只映射到 `native`，绝不因为检测到 OAuth 自动改成 `oauth`；旧 Provider ID 在 bot/route desired 中映射为 Profile ID，在已经接纳的 queue/pending/actual 中映射为 definition + preference Revision 1 精确 admission ref，override key 映射到同 ID Profile；不存在的 ID 保留为显式迁移诊断。
4. `commit`：先原子写各新 store，再最后写 migration generation/commit marker。中途崩溃时下次按同一输入幂等重算；marker 未提交前不开放 Profile mutation 或 managed Codex launch。
5. `cutover`：所有 canonical reader/writer 只使用 Profile schema；旧字段只允许迁移器读取，旧 API/命令 alias 通过 canonical Profile service 适配，不能继续写旧 SSOT。
6. `contract`：兼容窗口结束后删除旧字段读取、transport alias 和 frozen legacy evidence，并提升 schema version。

迁移冲突不能用“最后一个 surface 获胜”处理。若同一个 canonical bot+workspace 的旧状态出现多个不同 Provider ID，迁移器保留 bot default，但把该 route 标成 `profile_selection_conflict`，等待用户重新选择；不能无证据覆盖其中一个。迁移失败只降级 Codex Profile 子系统，Claude、Web 只读诊断和其它不依赖该状态的能力保持可用。

兼容窗口内允许保留一份 0600 权限的迁移前备份或 frozen legacy evidence 供回滚诊断，但新写入不能更新它。旧 daemon 降级读取到陈旧 Provider 配置不属于受支持的无损路径；发布说明必须要求使用备份回退，不能通过永久双写换取降级兼容。

旧 transport 的适配规则固定为：旧 Provider list 只投影 `native/default` 和 `api` 项，不伪造 OAuth 为可编辑 Provider；旧 create/update/delete 映射 canonical API Profile；隐藏 `/codexprovider` alias 将 ID 交给 canonical selection service。兼容层不得定义自己的验证、持久化或启动逻辑。

## 15. 安全与诊断

- 配置文件权限继续使用仅当前用户可读写模式。
- 日志只记录 Profile ID/Revision/Kind、Connection Contract ID、内部 Provider ID 和错误码；API 安全规范化端点最多记录 host，OAuth/native 不安全端点只记录 opaque generation。任何 URL userinfo/query/fragment、Key、token、完整账号都不得记录。
- app-server OAuth probe 不记录原始 response。
- Profile projection 的 secret-bearing launch material 与公共 Connection/Thread Contract 使用不同类型，避免误序列化。
- 认证失败必须保留底层错误分类，但用户主文案不暴露 token 或内部路径。
- 相同 Profile 启动失败是一次恢复 episode；只有用户重试、Profile 修改、OAuth 状态变化或目标变化才重新执行。

稳定错误码和允许重试事件如下：

| 错误码 | 含义 | 允许的再次执行触发 |
| --- | --- | --- |
| `oauth_missing` | 已确认没有 ChatGPT 登录 | 用户重新登录后刷新、再次选择 |
| `oauth_probe_unknown` | probe 超时、协议或本机配置读取失败 | 用户刷新、daemon 重启、相关配置变化 |
| `oauth_deployment_unsupported` | 已检测到自定义 ChatGPT 部署，但首版 OAuth Profile 无法把其 auth 路由映射为受支持模型端点 | 改回官方部署、使用 native Profile、Codex/产品能力升级 |
| `profile_secret_missing` | API Profile 缺少可用 Key | Profile 更新 |
| `profile_auth_rejected` | 目标服务拒绝当前 OAuth/API 凭据 | OAuth 重新登录、API Key 更新后用户重试 |
| `profile_definition_incomplete` | API Profile 缺少必需的模型或推理字段 | Profile 更新 |
| `profile_reasoning_invalid` | 推理强度为空、含非法控制字符或被目标 Codex 协议拒绝 | Profile/override 修改、Codex 升级 |
| `profile_reasoning_unsupported` | 目标 catalog 或 Provider 明确拒绝该 model/effort 组合 | Profile/override 修改 |
| `provider_request_failed` | 目标端点在 Codex 内建重试耗尽后仍返回网络、限流或服务端错误 | 用户重试、端点/Profile 修改 |
| `codex_protocol_incomplete` | start/resume 未返回目标 Provider/model，或显式响应与请求合同冲突 | Codex 升级、目标/配置变化、用户显式单次重试 |
| `profile_name_conflict` | 规范化名称与已有 Profile 重复 | Profile 名称修改 |
| `profile_revision_required` | PUT/DELETE 缺少 `If-Match` | 客户端携带当前 ETag 重新提交 |
| `profile_revision_conflict` | `If-Match` 基于旧 Revision | 用户读取最新定义后重新提交 |
| `profile_preference_revision_required` | 上下文偏好 PUT 缺少自己的 `If-Match` | 客户端携带 current preference ETag 重新提交 |
| `profile_preference_revision_conflict` | 上下文偏好 PUT 使用旧 preference Revision | 用户读取最新偏好后重新提交 |
| `profile_revision_unavailable` | 冻结合同引用的旧 Revision/外部状态已无法复现 | 用户取消/重新提交队列项或重新选择 Profile |
| `context_preference_clamped` | Codex 实际 effective context 小于请求档位 | 切换受支持模型/metadata、改为跟随或 272K；不自动重试当前 turn |
| `context_preference_unsupported` | Claude/Codex 版本、账号或模型拒绝目标上下文模式 | 修改偏好/模型、升级客户端或账号能力 |
| `profile_in_use` | Profile 仍被持久或运行态引用 | 引用切换、队列完成或取消 |
| `profile_not_found` | 选择或迁移引用不存在的 Profile | 用户重新选择或修复迁移输入 |
| `codex_capability_unsupported` | 本机 Codex 不支持隔离或 resume 合同 | Codex 升级 |
| `provider_registration_failed` | 目标 Provider 配置未被 app-server 接受 | Profile/Codex 修正、用户显式单次重试 |
| `thread_busy` | thread 被其它 app-server 占用 | 目标占用变化后用户重试 |
| `thread_missing` / `workspace_missing` | 恢复目标不存在 | 用户重新选择 route |
| `profile_selection_conflict` | 迁移发现同一 route 多个旧选择 | 用户重新选择 Profile |

后台计时器不能因为这些错误原样重复 launch/resume 或重复发通知。一次 episode 内只保留首个结构化原因和最新状态；只有表中明确的输入变化或用户动作创建新 episode。表中的“用户重试”始终只允许执行一个新 episode，不开启定时/指数退避循环；对确定性错误，UI 默认引导用户先完成表中修正，不能自动替用户点击重试。

## 16. 实施分段

1. 建立 Profile definition、独立 context preference、两类 Revision 和旧 Provider/Claude model suffix 迁移，不改变用户交互。
2. 实现 OAuth auth-only probe、只读 catalog、API Profile ephemeral credential store 与按实例专用 env 的运行认证隔离。
3. 建立 Connection Contract、浮动 desired/精确 frozen admission ref 和 definition/preference revision retention/GC。
4. 实现含 context 的 Thread Policy、真实 start/resume/TurnStarted 后的 Effective Thread Contract 和 translator resume config，修复跨 Profile exact-thread 恢复。
5. 迁移 WebUI、飞书命令和用户可见文案到 Profile，并为 Claude/Codex 接入各自 context 控件。
6. 删除内部旧 Provider SSOT 和到期兼容入口，同步 canonical 状态机文档。

阶段只表示执行顺序，不是默认停点。完整交付必须包含迁移、测试、文档和兼容清理。

## 17. 测试与验收

### 17.1 配置与安全

- 旧 Provider 的 ID、Base URL、API Key 和已有模型字段原值保留；缺 model/reasoning 的项迁移为可见但不可启动的 `profile_definition_incomplete`。
- OAuth file、keyring、auto 三种存储由 app-server probe 正确识别，并能把 Codex-managed `chatgpt` 与 `chatgptAuthTokens`、headers、Agent Identity、PAT 分开；probe 环境中的认证变量不会遮住持久登录。
- OAuth Profile child env 不含外部 API/token/auth-endpoint override；API Profile child 只含当前 Profile 的专用 Key，不含原生 OAuth/API 全局凭据，父 daemon 和其它实例环境不出现该值。
- API Profile 的 CLI 参数、生成配置、日志、错误、summary 和持久运行状态均不出现 API Key；测试不把同用户 shell/hooks 当成 secret 隔离边界。
- env-key API Provider 启动和运行不因本功能自行请求或周期重试第三方 `/models`。
- probe 超时得到 `unknown`，成功读到空账号得到 `missing`，两者不会互相覆盖语义。
- OAuth/auth refresh 不调用 `thread/start`；测试证明 Profile 浏览和后台 probe 不会初始化用户 MCP、extension lifecycle 或创建 ephemeral Session。
- 官方 `chatgpt_base_url` 可形成可用 OAuth Profile；自定义部署保留 `detected` 描述符但返回 `oauth_deployment_unsupported`，不会把 OAuth token 发往用户 `openai_base_url`，同一配置仍可由 native Profile 原样使用。
- native/oauth definition PUT/DELETE 均由后端拒绝，但 context-preference PUT 成功；definition ETag 与 preference ETag 不能混用。
- API Profile 语义编辑后 Definition Revision 递增，无变化保存不递增；只有 Key/端点/连接 capability 变化才改变 Connection Contract，名称/model/reasoning 单独变化不重启兼容 child。
- 任一 Profile 修改 context 只递增 preference Revision 和 ThreadPolicyID，不递增 Definition/Connection Generation，不重启认证 child。
- API Profile 更新后，仍有 frozen queue/pending 引用的旧 secret Revision 可按原合同解析；引用释放并经过 restart reconciliation 后被 GC。OAuth/native 旧状态不可复现时明确失败而不重绑 current。
- desired Profile ID 在 Definition/preference 更新后自动解析两类 current；已入队/active 的精确 admission ref 保持两类旧 Revision，不会永久 pin 住 route desired，也不会被任一 current 覆盖。
- native 端点含 userinfo/query/fragment 时，公共状态和日志只出现 opaque generation，原值不落入 runtime DTO；OAuth 首版只接受官方固定 endpoint identity。

### 17.2 运行时

- OAuth、API A、API B 可分别启动独立实例。
- API A 与 API B 即使使用同一专用 env 名，值也只存在各自 child env，两个实例不能串用；OAuth 实例不含该 env。
- 在 base config 预置自定义 Provider、`openai_base_url` 和认证环境后，官方 OAuth child 仍使用 built-in OpenAI 与清理后的模型端点；同 cwd model/reasoning 显式 preference 或 Codex auto policy 只在真实 start/resume 上解析。reasoning response 非空/为空分别记录 `codex_config`/`codex_default` source，均不改写 `codex_default` policy。review model 的显式配置或主模型回退通过 mode 和受控 fixture 验证，不宣称 start/resume response 回显 actual。API Profile 的模型 policy 完全来自自身定义。
- default -> API、OAuth -> API、API A -> API B、API -> OAuth 均使用目标 Provider。
- Profile 切换恢复已有 thread 时，所有 start/resume 都包含目标 typed `modelProvider`；API/显式 policy 同时包含 typed `model`、`config.model_reasoning_effort` 和 `config.review_model`，`apply_target_profile` auto policy 有意省略对应默认字段且不会回填旧 thread metadata。`preserve_thread_settings` 只带回 response 已证明的 model/非空 reasoning 并标记 `preserved_observed`。两条路径都在 prompt 前核对 Effective Thread Contract。
- `thread/start` reasoning 非空时可作为目标 Codex config snapshot 的实际值记录，但不冒充 Remote override；为 `null` 时，已知模型 metadata 默认和未知模型 fallback/Provider 默认都形成 `reasoningMode=codex_default`、source=`codex_default`。auto policy 的 `turn/start` 都不注入 effort，未知 model/list 项不会阻断 prompt，也不会回填产品硬编码默认值。
- 同 Connection Contract/Thread Policy 重启保留 response 已证明的线程 model/非空 reasoning；原 reasoning 为 `null` 时保留自动语义而不伪造字符串。跨 Profile 切换使用目标 Profile 默认或目标快照。
- Codex 三档分别验证不下发 override、`272000/244800` 和 `1000000/900000`；max=272K 的 1M 请求在首个真实 `TurnStarted` 后标记 clamp，max=1M 时观察到约 950K effective。测试不得把 272K 档断言成计费硬上限，并覆盖单次大输入未进入 pre-turn compact 预算的上游限制。
- OAuth 失效、版本过旧、Provider 未注册和 thread busy 分别产生不同稳定错误。

### 17.3 WebUI

- 桌面和移动端都能完成查看、创建、编辑、删除。
- OAuth/native 详情只有 context preference 可编辑，不出现连接/认证编辑控件；API Profile 有完整字段和同一 context 下拉。
- API Key 永不回填；保存、冲突和探测失败只进入固定 notice 槽位。已知 reasoning 预设和自定义非空值都能保存，UI 不把预设列表当封闭 enum。
- 两个浏览器并发编辑时缺少 `If-Match` 返回 428、旧 ETag 返回 412 且不覆盖新值；重名 POST 返回 409 且不隐式更新。
- list 中每个可写 item 的 `etag` 都只对应该 item；list 整体 ETag 不能误用于 PUT/DELETE，write/412 response 的 item ETag 与 body summary 一致。
- Codex context 使用三档下拉并显示价格/clamp 状态；Claude 使用 default/1M checkbox。Claude built-in default 启用后明确显示并投影 `sonnet[1m]`，custom model 的终止 `[1m]` 开关往返不会重复后缀。
- 删除前展示 bot/workspace/queue 等脱敏引用，服务端在确认时重新检查并拒绝竞态新增引用。
- 长名称、最长模型名和 50 个 Profile 不溢出布局。

### 17.4 飞书

- `/codexprofile` bare open、菜单 handoff、提交、返回和旧卡拒绝均有回归测试。
- 新菜单项继续遵守 `keep / enter_owner / enter_terminal` 统一合同；本设计保持 config-flow `keep`。
- 提交 callback 在 3 秒内完成 inline replace，不同步等待 probe/restart/resume；后台 owner episode 的成功/失败不依赖 callback delayed token 次数。
- 不可用 OAuth 只显示为状态行、不进入可提交选项；旧卡或探测竞态提交不可用 ID 时同卡拒绝，不能启动已知失败的切换。
- 群聊菜单隐藏该入口，群聊手输稳定提示到私聊修改。
- 50 个最长多字节名称 API Profile 加只读项时可通过同卡分页全部到达；每页真实 create/patch/callback envelope 均在 transport/element 预算内。
- 动态 Profile 名称、账号提示和错误文本不进入 raw markdown，adapter 投影测试断言其位于结构化/`plain_text` carrier。
- `/codexprovider` 隐藏 alias 在迁移期可用，但不出现在 help/menu。

## 18. 完成标准

以下条件全部满足才算功能完成：

1. 用户可见位置只使用 `Profile`，不再显示 `Provider`。
2. 受支持的官方 OAuth 被投影为可选择、连接身份不可编辑、不可删除的 Profile；其上下文偏好可独立修改。已检测到但不受支持的自定义部署保留只读状态和可执行提示，不伪装成可用。
3. API Profile 的 Codex Provider/AuthManager 不读取、更新、登出或覆盖原生 OAuth 存储；API Key 不进入产品日志、DTO、CLI 参数或其它实例。共享同一 OS 用户带来的本地可读性不被伪装成强隔离。
4. 多实例不同 Profile 不会被错误复用。
5. Profile 编辑后，旧 child 不会冒充新的 Key/端点连接；只改名称、thread 默认或 context 时也不会发生无意义认证重启，下一次 admission 使用新 Thread Policy。
6. 跨 Profile 恢复已有会话时，端点和认证来自目标 Connection Contract，模型、推理强度和 context 来自目标 Thread Policy/显式快照；Provider/model/reasoning 在发送 prompt 前闭包，context effective actual 在首个真实 `TurnStarted` 后补齐并显式报告 clamp。
7. 用户原有 Codex 配置、原生 Profile 文件和 OAuth 凭据没有被写入或迁移。
8. Claude/Codex 上下文偏好、Web/飞书、配置迁移、协议帧、敏感信息脱敏和状态机文档均完成验证；272K 档不做虚假的计费硬保证。

## 19. 实现参考

- `internal/config/codex_providers.go`
- `internal/config/codex_provider_env.go`
- `internal/app/daemon/app_headless_codex_provider.go`
- `internal/app/daemon/admin_codex_providers.go`
- `internal/core/state/codex_provider.go`
- `internal/core/orchestrator/service_codex_provider_command.go`
- `internal/core/orchestrator/service_surface_contract_compatibility.go`
- `internal/adapter/codex/translator_commands.go`
- `internal/adapter/codex/translator_restart_restore.go`
- `web/src/routes/admin/CodexProviderSection.tsx`
- `web/src/routes/admin/ClaudeProfileSection.tsx`
- `docs/general/config-state-storage-guidelines.md`
- `docs/general/feishu-menu-card-usage-guidelines.md`
- `docs/general/feishu-card-ui-state-machine.md`
- `docs/general/remote-surface-state-machine.md`

## 20. 上游调研依据

本设计在 OpenAI Codex `3d1d26915a303c3b4765828f973f5464f8c28c5c`（`2026-07-31` 的 `origin/main`）上复核了以下边界；实施时仍需用项目支持的最低 Codex 版本做 capability fixture，不能只按最新源码编译假设：

- `codex-rs/app-server-protocol/src/protocol/v2/account.rs`、`codex-rs/model-provider/src/provider.rs`：`account/read` 的 `chatgpt` 是账号投影而非 OAuth 充分证据，外部 ChatGPT token、Agent Identity 和 PAT 也可能投影成该类型；响应不暴露 token 或稳定账号 ID。
- `codex-rs/app-server-protocol/src/protocol/v1.rs`、`codex-rs/app-server/src/request_processors/account_processor.rs`：deprecated `getAuthStatus(includeToken=false, refreshToken=false)` 当前能区分精确 auth mode 且不返回 token；`account/updated` 虽含 auth mode，但初始化不主动发送，不能完成首次 probe。
- `codex-rs/model-provider-info/src/lib.rs`、`codex-rs/core/src/config/mod.rs`：用户 `openai_base_url` 会改变 built-in OpenAI Provider；清空后，ChatGPT auth 的模型请求使用编译期 `CHATGPT_CODEX_BASE_URL`。`chatgpt_base_url` 只控制登录及其它服务路由，不能据此支持任意自定义 OAuth 部署。
- `codex-rs/config/src/types.rs`、`codex-rs/login/src/auth/storage.rs`、`codex-rs/login/src/auth/manager.rs`：`ephemeral` 是进程内存存储，且显式选择后不会回退 file/keyring/auto；认证环境仍有独立优先级，因此必须同时清理环境变量。
- `codex-rs/app-server-protocol/src/protocol/v2/thread.rs`、`turn.rs`：`thread/start` / `thread/resume` 有 typed `modelProvider`、`model` 和非类型化 `config`，reasoning/review model 没有独立 thread 字段；`turn/start.effort` 只用于用户/Profile 的显式值，auto policy 必须继续省略。
- `codex-rs/app-server/src/request_processors/thread_processor.rs` 及其测试：只要显式覆盖 model、model provider 或 reasoning，持久化模型/Provider/推理三项就整体停止回填。因此 Resume Policy 必须成组决定每项是 explicit 还是目标 child 的 `codex_default`，不能遗漏后再恢复旧 metadata。
- `codex-rs/core/src/config/mod.rs`：模型类配置采用 override 优先、否则继承 base config 的合并方式，不能用空字符串表达“清除”。
- `codex-rs/app-server-protocol/src/protocol/v2/config.rs`、`thread.rs`：`config/read(cwd=...)` 只能返回该 cwd 有效配置里的显式 model/review/reasoning；`thread/start` response 在没有配置级 reasoning 时返回 `null`，不能由一次 response 声称存在具体 effort。
- `codex-rs/core/src/session/session.rs`、`hook_runtime.rs`：ephemeral Session 仍加载 workspace/exec policy/extensions、初始化 MCP 并产生 telemetry；SessionStart hook 到首个 turn 才执行。它不是无副作用的模型 probe。
- `codex-rs/models-manager/src/model_info.rs`、`codex-rs/core/src/client.rs`：未知模型使用 fallback metadata，reasoning effort 可继续为空并由 Provider 默认处理；缺少 catalog 项是受支持但有 warning 的路径，不是 Remote 应提前阻断的错误。
- `codex-rs/models-manager/models.json`、`model_info.rs`、`codex-rs/protocol/src/openai_models.rs`、`codex-rs/core/src/session/turn_context.rs`：当前 bundled Sol 为 272K/max 272K，config context override 会按 max 截断，effective window 默认取 95%，auto compact 缺省取 context 的 90%。ChatGPT 在线 `/models` 可替换 bundled metadata，不能把编译期值冒充每个 OAuth 实例的永久实际值。
- `codex-rs/core/src/session/turn.rs`、`context_window.rs`：pre-turn compact 在记录本轮 context 更新和用户输入前执行，当前 TODO 明确未估算 pending incoming items；因此 272K 配置是预算偏好，不是 request 计费硬上限。`TurnStarted.model_context_window` 是首个真实 turn 后可取得的 effective actual。
- Codex 历史提交 `3380969a29` / `d26a9bf671` 与 OpenAI 官方 GPT-5.6 Sol 模型页：bundled 默认曾在 2026-07-09 至 2026-07-18 间为 372K，现已回到 272K；官方服务上限为 1,050,000，而 input 超过 272K 会对整次 request 应用 2x input / 1.5x output 价格。
- `codex-rs/protocol/src/openai_models.rs`：`ReasoningEffort` 接受任意非空字符串，已知档位只是 typed variants；Remote 不能维护一个封闭枚举冒充上游协议。
- `codex-rs/app-server-protocol/src/protocol/v2/model.rs`、`codex-rs/app-server/src/models.rs`：`model/list` 返回默认标记和推理元数据，但固定使用 `OnlineIfUncached` 且不返回 catalog provenance；只能作为同一 live child 的可选观察证据，不能单独证明 OAuth/第三方服务端默认值来源或成为 dispatch gate。
- `codex-rs/login/src/auth/external_bearer.rs`、`codex-rs/app-server/src/models_refresh_worker.rs`、`codex-rs/models-manager/src/manager.rs`、`codex-rs/features/src/lib.rs`：command auth 会使 app-server 立即并周期刷新 `/models`，旧 `remote_models` flag 已移除为 no-op；首版因此继续使用 env-key Provider，而不是为较强表面隔离引入新的无动作重试。
- `codex-rs/protocol/src/config_types.rs`、`codex-rs/hooks/src/engine/command_runner.rs`：shell 默认继承全部父环境且默认忽略 secret/key/token 排除，hooks 也直接继承环境；加上 app config 与 `CODEX_HOME` 属于同一 OS 用户，方案 C 不能宣称抵御同用户任意代码读取凭据。
- `codex-rs/app-server-protocol/src/protocol/v1.rs`：initialize 响应没有 capability 列表或独立 server version，只能以 `userAgent` 初筛并用隔离行为 fixture 最终证明。
- `codex-rs/cli/src/lib.rs`：上游 Profile 加载格式已经变化，Remote 不应复制其文件解析和配置合并规则。

## 21. 产品决策记录

### API Profile 是否允许“自动”模型与推理

- 触发原因：上游 `model/list` 对普通自定义 Provider 不是可靠的保存期验证目录，并可能读取未按 Provider 隔离的共享 cache；command auth 又会引入不可关闭的后台 `/models` 请求。
- 决策：采用方案 A。API Profile 的主模型和推理强度必填；审阅模型可空并等于主模型。OAuth Profile 保留只读 `codex_default`，只在真实 start/resume 上自动解析，不建立独立 ephemeral probe。
- 首版非目标：Remote 不自行请求或适配第三方 `/models`，也不把 Codex 内置/共享缓存中的默认值当作 API Profile 自动值。
- 后续演进：只有在第三方目录能提供可验证的默认模型和推理元数据时，才允许单独设计自动探测；不支持或探测失败仍要求用户补齐字段。
- 决策时间：2026-07-31。

### OAuth Profile 是否支持自定义 ChatGPT 部署

- 触发原因：`chatgpt_base_url` 只改变认证及部分服务路由；清空 `openai_base_url` 后，built-in OpenAI Provider 的 ChatGPT 模型端点来自 Codex 编译期常量，两者没有受支持的通用映射。
- 决策：首版只把官方默认 ChatGPT 部署投影为可启动的 `oauth` Profile。自定义部署仍保留只读 detected 描述符，但以 `oauth_deployment_unsupported` 标记不可用；`native` Profile 继续忠实沿用用户原生配置。
- 安全原因：不能把 OAuth token 发送到用户 `openai_base_url` 来猜测部署关系，也不能保留自定义 auth URL 后声称模型请求已经跟随。
- 后续演进：只有 Codex 暴露稳定的部署 identity 与 auth/model endpoint 映射能力时，才扩大 OAuth Profile 支持范围。
- 决策时间：2026-07-31。

### 只读 Profile 是否允许修改上下文偏好

- 触发原因：Claude/Codex 的内建 Profile 连接身份必须防止凭据或端点被覆盖，但用户仍需要按 Profile 控制上下文；Codex 还存在上游默认从 372K 回调 272K 以及 272K 以上长上下文溢价的真实历史。
- 决策：采用“连接身份只读、运行策略可写”。Codex native/oauth 和 Claude built-in default 的名称、端点、认证、模型默认仍不可编辑，但 context preference 使用独立 revision、ETag、API 和 retention；用户自建 Profile 也走同一 preference，不把 context 混进 secret definition。
- Codex UI：三档下拉 `跟随 Codex / 272K（费用优先）/ 1M（长上下文）`。272K 档下发 `272000/244800`，但明确不承诺计费硬封顶；1M 档下发 `1000000/900000`，受模型 max clamp，并在真实 `TurnStarted` 后展示 observed effective 或 clamp 状态。
- Claude UI：两态 checkbox。custom model 只规范化终止 `[1m]`；built-in default 关闭时跟随默认，开启时固定 `sonnet[1m]`，因为官方未证明 `default[1m]`。
- 冻结语义：definition ref 与 context preference ref 共同组成 `CodexAdmissionRef`；修改偏好不影响 running/已入队动作，也不推进 Connection Generation。
- 决策时间：2026-07-31。

## 22. 安全边界结论

本设计使用“运行认证隔离”而不是“同用户 secret sandbox”作为完成标准：

- OAuth Profile 的 Codex AuthManager 使用原生持久 OAuth，并仅在官方 ChatGPT 部署合同下启动；API Profile 使用 custom Provider、ephemeral credential store 和当前实例专用 env，不调用 OAuth login/logout，也不把 API 认证写入 OAuth store。
- 只有 secret config owner 和目标 app-server child env 持有当前 API Key；daemon 全局环境、其它实例、非 secret 持久状态、HTTP DTO 和日志不能得到它。目标 app-server 及其同用户子进程不属于保密边界。
- command auth 目前既不能建立真正的同用户安全边界，又会引入每 3 分钟 `/models` 后台请求，因此不作为首版方案。
- 若未来要求 Agent shell/hooks 对 API Key 与 OAuth 文件都不可见，必须重新评估独立 `CODEX_HOME`、独立 OS 身份、系统 secret broker 和会话共享机制；不能在本设计上叠加一个 helper 后宣称已经实现。

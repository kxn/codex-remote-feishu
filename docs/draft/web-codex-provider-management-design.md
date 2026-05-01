# Web Codex Provider 管理设计

> Type: `draft`
> Updated: `2026-05-01`
> Summary: 固定共享 home 前提下的 Codex Provider 管理边界：provider 与密钥由本仓库自行管理，只放在 admin 页面，启动时用 `-c` 覆盖与子进程环境变量注入，并补齐与 `/claudeprofile` 同槽位互斥的 runtime 命令入口。

## 1. 背景

当前仓库已经支持：

- 共享用户现有 `HOME`
- 启动 Codex 子进程时补齐 detached `PATH`
- 按启动参数推导 active `model_provider`
- 按 active provider 的 `env_key` 定向补齐子进程环境变量

但当前还没有一条完整产品路径，允许用户在 admin 与 runtime 命令面中：

- 新增多套 Codex provider
- 保存某个 provider 对应的端点和认证信息
- 为当前实例切换使用哪套 provider
- 在不污染手动 `codex` / 手动 `claude` 默认配置的前提下生效

本设计的目标，是把这条路径做成与当前 `Claude 配置` / `Claude 配置切换` 相同风格的产品能力，同时继续遵守 `v1.7.0` setup/admin 页面基线，不额外扩页面骨架，也不把内部技术语义暴露到主界面。

## 2. 已确认结论

### 2.1 不做运行中无重启切 provider

本轮不设计 Codex 进程内“热切换 provider”。

切换 provider 的正式语义与当前 Claude 配置切换保持一致：

- 保存选择
- 重启当前 Codex 运行时
- 用新的 provider 配置重新拉起

### 2.2 不做独立 `CODEX_HOME`

当前需求明确要求不同实例继续共享用户现有 home。

因此本设计不为每个 daemon / provider 分配单独的 `CODEX_HOME`，也不依赖隔离 `auth.json` 作为主要机制。

### 2.3 不把密钥写进共享 `~/.codex/config.toml`

provider 的端点和密钥都不应作为共享 home 下的长期默认配置直接落到 Codex 侧：

- 不持久写 `~/.codex/config.toml` 作为主存储
- 不依赖 `~/.codex/auth.json` 承载这套自定义 provider 的密钥
- 不把密钥放进启动命令行

我们自己的配置才是这套能力的 source of truth。

### 2.4 `wire_api` 与 `requires_openai_auth` 不进入产品面

本轮不暴露下面两项：

- `wire_api`
- `requires_openai_auth`

原因：

1. 上游当前只支持 `responses`，`chat` 旧值已经移除，不再需要做用户配置。
2. `requires_openai_auth=true` 会让 Codex 走自己的 OpenAI 登录屏、登录模式和 `auth.json` 路径，不符合“我们自己保存 provider 密钥，启动时只对子进程注入”的目标。
3. 上游实现中，`requires_openai_auth` 与 `env_key` / `auth.command` 是互斥关系，不适合作为我们自定义 provider 的默认能力面。

## 3. 上游 Codex 约束

本设计依赖的上游事实来自 OpenAI 官方 Codex 文档与 `openai/codex` 开源仓库当前代码。

### 3.1 `requires_openai_auth` 的真实含义

在上游 `openai/codex` `f50c02d7bcd4c06a23173389da8ed2c68c03d81d` 中：

- `codex-rs/model-provider-info/src/lib.rs`
  - `requires_openai_auth=true` 表示 provider 走 OpenAI API key / ChatGPT 登录体系
  - 登录模式和 token / key 会进入 `auth.json`
  - 若为 `false`，provider API key 走 `env_key`
- 同文件还明确限制：
  - `requires_openai_auth` 不能和 `env_key` 同时使用
  - `requires_openai_auth` 不能和 `auth.command` 同时使用

因此我们这次自定义 provider 的正确路径是：

- 默认不使用 `requires_openai_auth`
- 按需使用 `env_key`
- 由我们自己决定是否给这个 env key 注入值

### 3.2 `OpenAI` / `openai` 有上游保留语义

上游当前同时存在两层特殊语义：

1. provider id `openai`
   - 是内建 provider catalog 的固定 id
2. provider display name `OpenAI`
   - `ModelProviderInfo::is_openai()` 直接用 `self.name == "OpenAI"` 判断
   - 该判断会影响 remote compaction 等 OpenAI 特殊行为

因此本仓库的自定义 provider 不能允许下面任一情况：

- 自定义 provider id 归一化后等于 `openai`
- 自定义 provider 名称去空白并忽略大小写后等价于 `openai`

本约束必须同时体现在：

- 后端校验
- Web 表单校验
- 配置归一化
- 导入/迁移逻辑

## 4. 产品结论

## 4.1 用户可见合同

- 最终用户：
  - 正在安装或管理当前机器 Codex 运行时的用户
- 当前任务：
  - 选择当前实例要用哪套 Codex 端点和认证
  - 管理多套可复用的 Codex provider
- 允许展示：
  - provider 名称
  - 端点地址
  - 是否已保存密钥
  - 当前实例正在使用哪一套
  - 切换后需要重启才能生效
- 不允许展示：
  - `model_provider`
  - `model_providers.<id>`
  - `env_key`
  - `requires_openai_auth`
  - `wire_api`
  - `auth.json`
  - 内部注入变量名

### 4.2 Setup 放置方式

setup 继续保持 `v1.7.0` 单步曝光向导结构。

本轮明确不在 setup 中增加任何 provider 配置入口，包括：

- `Codex Provider`
- `Claude 配置`

如果现有代码、文档或 mock 仍把任一 provider 配置放在 setup，视为陈旧设计，应在后续实现中清理掉。

### 4.3 Admin 放置方式

admin 继续保持 `v1.7.0` 主结构，不引入新的页面骨架。

本轮新增一个默认主界面区块：

1. `机器人管理`
2. `系统集成`
3. `Claude 配置`
4. `Codex Provider`
5. `存储管理`

`Codex Provider` 采用与当前 `Claude 配置` 相同的产品节奏：

- 左侧列表
- 右侧详情/编辑
- 一个内建默认项
- 一个新增入口

### 4.4 切换语义

provider 管理和 provider 选择要分开：

- provider catalog
  - 全局共享
  - 存在我们的 app config 里
- 当前实例选择
  - 不是全局默认
  - 是实例级 / 工作区级运行时选择
  - 切换后重启当前实例生效

这条规则与当前 `Claude profile` 的实例级选择边界保持一致，不做“修改一处，所有实例默认全部改掉”。

### 4.5 运行时菜单与 slash command 入口

`Codex Provider` 和 `Claude 配置` 在 runtime 的 `send_settings` 组占用同一个命令槽位。

这里沿用当前 live 代码里的既有命名：

- `Claude 配置`
- `/claudeprofile`

本轮只复用这条现有入口位，不顺带改名。

正式规则：

1. 当前 surface 是 `codex headless` 时：
   - 显示 `切换 Codex Provider`
   - slash 为 `/codexprovider`
2. 当前 surface 是 `claude headless` 时：
   - 显示 `切换 Claude 配置`
   - 继续沿用 `/claudeprofile`
3. 当前 surface 是 `vscode` 时：
   - 两者都不显示
4. 两者不能同时在 help、menu、config page 中可见。
5. 如果用户手动输入了当前 backend 不支持的那一条命令，必须返回显式报错，不做 silent fallback，也不偷偷改成另一条配置命令。
6. 这条显式报错同时适用于：
   - bare `/codexprovider` / `/claudeprofile`
   - 带参数的 `/codexprovider <id>` / `/claudeprofile <id>`

## 5. 配置模型

## 5.1 App config 新增 `CodexSettings`

建议新增：

```go
type CodexSettings struct {
    Providers []CodexProviderConfig `json:"providers,omitempty"`
}

type CodexProviderConfig struct {
    ID      string `json:"id,omitempty"`
    Name    string `json:"name,omitempty"`
    BaseURL string `json:"baseURL,omitempty"`
    APIKey  string `json:"apiKey,omitempty"`
}
```

说明：

- `Providers` 是全局共享 catalog
- `APIKey` 存在我们自己的配置里
- 本轮不引入额外 `authMode`
- 本轮不暴露模型名、轻量模型、header map、`auth.command` 等扩展面

### 5.2 内建默认项

与当前 `Claude 配置` 类似，提供一个只读内建默认项：

- `id = "default"`
- 展示名建议为 `系统默认`

语义：

- 不向 Codex 注入任何 provider 覆盖
- 完全沿用用户当前共享 home 下的默认 Codex 配置

### 5.3 自定义 provider 校验

自定义 provider 最低校验：

1. 名称必填
2. `baseURL` 必填
3. 自定义 provider 在最终持久化状态下必须有 API key
4. 创建时 API key 必填
5. 更新时若未重新输入新的 API key，必须继续保留已有 key，不允许把已保存 key 更新成空
6. 归一化后 id 不能等于 `default`
7. 归一化后 id 不能等于 `openai`
8. 名称去空白并忽略大小写后不能等价于 `openai`

## 6. 启动与切换模型

### 6.1 启动参数投影

对于自定义 provider，不持久写共享 `~/.codex/config.toml`。

而是在拉起 Codex 时，通过 CLI override 临时投影：

```text
-c model_provider="<provider-id>"
-c model_providers.<provider-id>.name="<provider-name>"
-c model_providers.<provider-id>.base_url="<base-url>"
-c model_providers.<provider-id>.env_key="CODEX_REMOTE_CODEX_PROVIDER_API_KEY"
```

本轮不传：

- `wire_api`
- `requires_openai_auth`
- `auth.command`

### 6.2 子进程环境变量注入

当前选择自定义 provider 时：

- 只给当前 Codex 子进程注入 `CODEX_REMOTE_CODEX_PROVIDER_API_KEY=<secret>`
- 不把这个值写回父进程
- 不把这个值写进共享 shell rc
- 不把这个值写进日志、错误响应和 Web 返回体

### 6.3 默认 provider 行为

当当前实例选择 `系统默认` 时：

- 不传 `model_provider` 覆盖
- 不传 `model_providers.*` 覆盖
- 不注入 provider API key

这样手动 `codex` 和受管 Codex 可以继续共享同一个 home，但只有“受管 + 选中自定义 provider”的实例会看到额外注入。

### 6.4 实例级选择状态

provider catalog 与当前实例选择分离。

建议新增一条与 `Claude workspace profile state` 同类的持久状态：

- catalog：进 `config.json`
- 当前实例选择：进独立 state store

要求：

- 不是全局默认
- daemon 重启后可恢复
- surface / workspace 恢复时仍能拿回上次选中的 provider

本轮优先复用现有 `Claude workspace profile` 的状态持久化模式与恢复节奏。

## 7. Web 交互约束

### 7.1 Setup

setup 不出现任何 provider 配置入口。

正式规则：

- 不出现 `Codex Provider`
- 不出现 `Claude 配置`
- 如果后续代码或 mock 仍在 setup 暴露 provider 管理入口，应移回 admin

### 7.2 Admin

`Codex Provider` 区块默认包含：

- 左侧 provider 列表
- 右侧详情卡
- `新增配置`
- `保存`
- `删除`
- 当前实例 `切换并重启`

表单字段只保留：

1. 名称
2. 端点地址
3. API Key

其中：

- 名称必填
- 端点地址必填
- API Key 对自定义 provider 为必填

### 7.3 错误反馈

默认用户可见错误只说任务结果，不回显内部配置字段名。

例如：

- `这个名称不能使用，请换一个名字。`
- `系统默认配置不能编辑。`
- `保存没有完成，请检查端点地址后重试。`
- `切换没有完成，当前仍有执行中的会话。`
- `当前不在 Codex 模式，暂时不能切换 Codex Provider。`
- `当前不在 Claude 模式，暂时不能切换 Claude 配置。`

不直接显示：

- `model_provider.openai`
- `requires_openai_auth`
- `env_key missing`

## 8. 安全与日志规则

1. API key 永不出现在：
   - Web 返回体
   - `/api/admin/config`
   - 页面回显
   - 日志
   - 错误 details
2. `GET /api/admin/config` 这类配置读取接口只能返回 `hasApiKey` 一类红acted 视图。
3. 切换或删除 provider 时，不能把 secret 值写进 runtime snapshot、surface snapshot 或状态页摘要。
4. 后端测试必须覆盖：
   - create/update/list/config redaction
   - 切换后启动参数投影
   - 子进程 env 注入
   - `OpenAI` / `openai` 保留名拦截

## 9. 参考落点

### 9.1 当前已同步的相关文档 / mock

本轮已经同步到当前结论的文档与 mock：

- `docs/implemented/web-admin-ui-redesign.md`
- `docs/draft/web-onboarding-admin-workflow-prd.md`
- `docs/draft/web-onboarding-admin-user-view.md`
- `docs/draft/web-setup-flow-v2.md`
- `docs/draft/web-admin-user-mock.html`
- `docs/general/remote-surface-state-machine.md`
- `docs/general/feishu-card-ui-state-machine.md`

当前仓库内最接近的现成实现路径：

- 配置存储与红acted 管理：
  - `internal/app/daemon/admin_claude_profiles.go`
  - `internal/config/claude_profiles.go`
- Web 管理交互：
  - `web/src/routes/admin/ClaudeProfileSection.tsx`
- runtime 命令入口与显示策略：
  - `internal/core/control/feishu_commands.go`
  - `internal/core/control/feishu_command_config_catalog.go`
  - `internal/core/orchestrator/service_surface_command_settings.go`
- 实例级选择与持久化：
  - `internal/app/daemon/app_claude_workspace_profile_state.go`
  - `internal/app/daemon/claudeworkspaceprofile/state.go`
- 启动时子进程环境构造：
  - `internal/config/codex_provider_env.go`
  - `internal/app/wrapper/app_process.go`

本轮实现已经按 `Claude 配置` 的形状复用了 Web 与 admin API，并把“最终如何投影到 Codex 启动参数和 child env”接到了现有 `BuildCodexChildEnv(...)` 和 wrapper 启动链路上。

# Codex / Claude Profile 子代理模型配置设计

> Type: `inprogress`
> Updated: `2026-08-08`
> Summary: 明确 Codex 子代理模型优先使用 `agents.default_subagent_model` first-class override；只有 DeepSeek/MiMo 等 catalog-backed provider 注入 provider-owned models.json，普通 GPT/OpenAI-like profile 不再生成 generic managed catalog。对应 issue #822/#839。

## 1. 背景

Codex / Claude profile 目前每个只能配置一个主模型，外加一个辅助模型：

- Codex profile：主模型 + 审阅模型（`reviewModel`，映射 Codex `review_model`）。
- Claude profile：主模型 + 轻量模型（`smallModel`，映射 `ANTHROPIC_DEFAULT_HAIKU_MODEL`）。

主模型开子代理时，Codex / Claude 子代理默认继承主模型，成本高。目标是在每个 profile 上增加一个可选“子代理模型”，让子代理默认走弱模型。

已确认的机制事实：

- Claude Code 原生支持子代理模型：agent 定义 frontmatter、Task 工具参数、全局 `CLAUDE_CODE_SUBAGENT_MODEL` 环境变量，默认 `inherit`（继承主模型）。子代理模型不需要模型目录。
- Codex 通过 `agents.default_subagent_model` 配置子代理默认模型；spawn_agent 要求模型必须存在于当前模型目录（models.json）中，否则报 `Unknown model`。
- 本仓库已有 DeepSeek / MiMo 这类 catalog-backed provider 的 managed models.json 注入机制（`model_catalog_json`），但该 catalog 是完整目录接管，不是字段级 overlay；普通 GPT/OpenAI-like profile 不应为了子代理模型伪造 generic catalog。

## 2. 目标与非目标

### 目标

1. Codex / Claude profile 各自增加一个可选“子代理模型”输入框。
2. 后端按各端语义生效：
   - Codex：启动材料注入 `agents.default_subagent_model=<model>`；只有当前 profile 命中 DeepSeek / MiMo 等 catalog-backed provider 时，才把子代理模型（连同主模型、审阅模型）并入该 provider-owned models.json。
   - Claude：启动环境注入 `CLAUDE_CODE_SUBAGENT_MODEL=<model>`。
3. 留空时行为与现状完全一致，不注入任何新配置。
4. UI 沿用现有 `form-grid` / `field` 样式体系，不新增样式类。

### 非目标

- 不做模型下拉选择器 / 模型目录编辑器。
- 不做全局（跨 profile）子代理模型设置。
- 不改变主模型、审阅模型、轻量模型、推理强度的现有语义。
- 不实现按 agent 角色配置不同子代理模型（Claude agent frontmatter / Codex agent role 配置留给未来）。

## 3. 现状与关键链路

### Codex

- `config.CodexAPIProfileSecretConfig` 持久化字段：`Model` / `ReviewModel` / `ReasoningEffort`（`internal/config/codex_profiles.go`）。
- admin API：`codexProfileWriteRequest`、`codexAPIProfileInputFromRequest`、`codexAPIProfileSummary`（`internal/app/daemon/admin_codex_profiles.go`）。
- 启动材料：`RuntimeResolver.resolveAPI` 生成 `SecretLaunchMaterial`，其中 DeepSeek / MiMo 等 catalog-backed profile 会注入 `model_catalog_json` 与 provider-owned managed models.json 文件（`internal/app/codexprofile/runtime_resolver.go`）。
- managed 目录：`codexcatalog.ManagedModelCatalogDir(stateDir)` + 内嵌 `deepseek_models.json` / `mimo_models.json`（`internal/codexcatalog`）。
- 目录生成文件写入：`EnsureLaunchManagedFiles`（`internal/app/codexprofile/runtime_resolver.go`）。
- Codex 端配置键：`agents.default_subagent_model`（codex-rs `config_toml.rs`）。

### Claude

- `config.ClaudeProfileConfig` 持久化字段：`Model` / `SmallModel` / `ReasoningEffort`（`internal/config/claude_profiles.go`）。
- 运行时 env：`ClaudeProfileRuntimeSettings` + `ApplyClaudeProfileLaunchEnv`，其中 `claudeProfileLaunchEnvKeys` 是启动时清理的 env 键列表（`internal/config/claude_runtime_settings.go`、`internal/config/claude_profiles.go`）。
- admin API：`claudeProfileWriteRequest`、`adminClaudeProfileView`、update/create 分支（`internal/app/daemon/admin_claude_profiles.go`）。

## 4. 影响面

### 4.1 后端配置层

- `internal/config/codex_profiles.go`：
  - `CodexAPIProfileSecretConfig` 增加 `SubagentModel string`（JSON `subagentModel,omitempty`）。
  - `CodexAPIProfileInput` 增加 `SubagentModel string`。
  - `PrepareCodexAPIProfileCreate` / `PrepareCodexAPIProfileUpdate`：携带新字段；update 的“无变化”比较要包含新字段。
  - `validateStoredCodexAPIProfileRevision`：如校验字段白名单，需要加入新字段（不视为非法）。
  - `MigrateLegacyCodexProviders`：新字段留空即可。
- `internal/config/claude_profiles.go`：
  - `ClaudeProfileConfig` 增加 `SubagentModel string`（JSON `subagentModel,omitempty`）。
  - `NormalizeClaudeProfiles` 增加 trim。
  - `claudeProfileLaunchEnvKeys` 增加 `CLAUDE_CODE_SUBAGENT_MODEL`（清理旧值，防止 profile 切换残留）。
- `internal/config/claude_runtime_settings.go`：
  - `ClaudeProfileRuntimeSettings` 在非空时写入 `CLAUDE_CODE_SUBAGENT_MODEL`。

### 4.2 后端 admin API

- `internal/app/daemon/admin_codex_profiles.go`：
  - `codexProfileWriteRequest` 增加 `SubagentModel *string`。
  - `codexAPIProfileInputFromRequest` 携带新字段。
  - `codexAPIProfileSummary` 输出新字段（同时补 `state.CodexProfileSummary`）。
- `internal/app/daemon/admin_codex_providers.go`（legacy `/api/admin/codex/providers`）：
  - legacy 更新请求没有新字段，但更新时必须保留 `current.SubagentModel`（与 ReviewModel 同样的 preserve 模式）。
- `internal/app/daemon/admin_claude_profiles.go`：
  - `adminClaudeProfileView` 增加 `SubagentModel`。
  - `claudeProfileWriteRequest` 增加 `SubagentModel *string`。
  - create / update 分支写入新字段；`req.SubagentModel == nil` 时保持不变。
- `internal/core/state/profile_catalog.go`：
  - `CodexProfileSummary` 增加 `SubagentModel string`。

### 4.3 Codex 启动材料

- `internal/app/codexprofile/runtime_resolver.go`：
  - `resolveAPI`：当 `profile.SubagentModel` 非空时追加 CLI override `agents.default_subagent_model=<model>`。
  - `codexcatalog.IdentifyEmbeddedCatalog` 命中 DeepSeek / MiMo 等 catalog-backed provider 时，追加 `model_catalog_json` 指向该 provider 的 managed 目录文件；若配置了子代理模型，目录生成只在 provider catalog 模板内补齐主模型/审阅模型/子代理模型。
  - 非 catalog-backed 的 GPT/OpenAI-like API profile 不生成 `managed-models-v1.json`，不接管 Codex 默认模型元数据；若 Codex 当前模型目录找不到该子代理模型，保留 Codex 原生 `Unknown model` 行为。
  - `ManagedModelCatalogDir` 缺失只在 catalog-backed provider 需要注入目录时复用 `ErrorManagedModelCatalogMissing`。
- `internal/codexcatalog/deepseek.go`（或新文件 `internal/codexcatalog/build.go`）：
  - provider catalog builder 只服务已识别 provider；不要再新增/使用 generic GPT fallback template 或 `managed-models-v1.json` 来补未知模型。
  - provider 文件名与目录内容由各 provider owner 维护，例如 `deepseek-models-v1.json` / `mimo-models-v1.json`。
- `internal/app/daemon/app_headless_codex_provider.go`：已统一走 `EnsureLaunchManagedFiles` + `ApplyLaunchMaterial`，无需改结构；新增字段自动随 `LaunchManagedFile` 写入。

### 4.4 Web UI

- `web/src/lib/types.ts`：
  - `CodexProfileSummary` / `CodexProfileWriteRequest` 增加 `subagentModel?: string`。
  - `ClaudeProfileSummary` / `ClaudeProfileWriteRequest` 增加 `subagentModel?: string`。
- `web/src/routes/admin/CodexProviderSection.tsx`：
  - draft state / createDraftFromProvider / createEmptyDraft / payload 增加 `subagentModel`。
  - 表单在“审阅模型”之后、“推理强度”之前插入 `子代理模型` 输入框（与推理强度同排成 2×2）。
  - placeholder：`留空时子代理跟随主模型`。
- `web/src/routes/admin/ClaudeProfileSection.tsx`：
  - 同样在“轻量模型”之后、“推理强度”之前插入 `子代理模型` 输入框。
- 样式：复用现有 `.form-grid` / `.field` / `.form-grid-span-2`，零新增 CSS；移动端沿用现有单列堆叠。

### 4.5 测试

后端：

- `internal/config/codex_profiles_test.go`：create/update 持久化、无变化比较、normalize。
- `internal/config/claude_profiles_test.go`、`claude_runtime_settings_test.go`：新字段 normalize、env 映射、launch env 清理。
- `internal/app/codexprofile/runtime_resolver_test.go`：子代理 override 注入、非 catalog-backed profile 不生成 managed catalog、DeepSeek/MiMo catalog-backed 分支保持、catalog-backed provider 目录缺失报错。
- `internal/app/daemon/app_headless_codex_provider_test.go`：启动 args 包含 `agents.default_subagent_model`；仅 catalog-backed provider 额外包含 `model_catalog_json`。
- `internal/app/daemon/admin_codex_profiles_test.go`、`admin_claude_profiles_test.go`：API create/update/read 新字段；legacy API 更新保留新字段。
- `internal/app/daemon/profile_catalog_migration_test.go`：旧配置无字段 → 迁移后为空，凭据不受影响。

Web：

- `web/src/routes/admin/CodexProviderSection.test.tsx`、`ClaudeProfileSection.test.tsx`：保存 payload 含 `subagentModel`、编辑回显、清空。
- `web/src/routes/AdminRoute.test.tsx` / fixtures：默认夹具补 `subagentModel`。

### 4.6 文档

- 本设计文档（`docs/inprogress/codex-claude-subagent-model-design.md`）。
- `docs/README.md` 6.3 列表增加链接。
- issue #822 body 同步本设计链接与执行上下文。

## 5. UI 布局设计

现有表单是两列 `form-grid`。新增字段后维持两列 2×2 模型块：

```
Codex:
主模型        | 审阅模型
子代理模型    | 推理强度

Claude:
主模型        | 轻量模型
子代理模型    | 推理强度
```

字段放在两个 Section 各自的“审阅模型 / 轻量模型”之后、“推理强度”之前，DOM 顺序与视觉顺序一致。不使用任何新增 class、自定义组件或内联布局 hack。

## 6. 行为约定

### Codex

- `subagentModel` 为空：不注入 `agents.default_subagent_model`，不追加 managed models.json，保持现状。
- `subagentModel` 非空：
  - CLI override：`-c agents.default_subagent_model="<model>"`。
  - 非 catalog-backed GPT/OpenAI-like profile 不生成 managed models.json；Codex 继续使用自身 bundled / fallback 模型目录。若 spawn_agent 找不到子代理模型，报错由 Codex 原生 `Unknown model ... Available models ...` 承担。
  - DeepSeek / MiMo 等 catalog-backed profile 继续使用 provider-owned 内嵌目录；若子代理模型不在内嵌目录，按该 provider catalog 模板补充条目。

### Claude

- `subagentModel` 为空：不设置 `CLAUDE_CODE_SUBAGENT_MODEL`，且启动时从 env 清理该键，避免上一个 profile 残留。
- `subagentModel` 非空：启动环境设置 `CLAUDE_CODE_SUBAGENT_MODEL=<model>`，对所有子代理生效（包括内置 explore / guide agent 的默认值会被该 env 覆盖）。

## 7. 错误处理与兼容

- catalog-backed provider 需要写 managed 目录但目录缺失：复用 `ErrorManagedModelCatalogMissing`，headless 启动路径已有对应错误映射。非 catalog-backed profile 不因子代理模型要求 managed 目录。
- 旧配置/历史 revision 无 `subagentModel` 字段：按空处理，不触发迁移写入。
- 配置迁移只读不写；不得改变既有凭据、revision、connection generation。
- legacy `/api/admin/codex/providers` 更新继续保留 `subagentModel`。

## 8. 风险与取舍

- 设置 Codex 子代理模型后，普通 GPT/OpenAI-like profile 只写 `agents.default_subagent_model`，不改变 `/model` 的动态目录来源和 Codex 默认 metadata。
- DeepSeek / MiMo 等 catalog-backed provider 会继续由 daemon 注入 provider-owned catalog；该目录只声明 provider owner 明确维护或按 provider 模板补齐的模型能力。
- 子代理模型与主模型可以相同；此时仍注入 override，语义等价于默认 inherit，但目录逻辑保持一致。
- Claude 的 `CLAUDE_CODE_SUBAGENT_MODEL` 是全局 env，会同时影响所有子代理（无法按 agent 区分）；按 agent 区分留给未来需求。

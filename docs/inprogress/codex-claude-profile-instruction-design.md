# Codex / Claude Profile instruction（角色提示词）配置设计

> Type: `inprogress`
> Updated: `2026-08-07`
> Summary: 在 Codex / Claude profile 配置中增加可选 `instruction` 字段（角色提示词）。Claude 侧通过 SDK 初始化控制消息追加 `appendSystemPrompt`，Codex 侧把 instruction 追加到 daemon 管理的 models.json 各模型 `instructions_template` 末尾；Web UI 增加全宽 textarea 与 16000 字符上限。对应 issue #823。

## 1. 背景

用户希望每个 Codex / Claude profile 都能配置一段可选的“角色提示词 / instruction”，用于预设代理的角色与行为。调研结论：

- Claude Code 原生支持追加语义：`--append-system-prompt <text>` 或 SDK 控制消息 `appendSystemPrompt`，都会追加到默认系统提示词末尾，保留 Claude Code 自身能力。完全替换语义（`--system-prompt`）不在本单范围。
- Codex 的 `model_instructions_file` 是“替换内置 instructions”语义，不适合做追加；`model_messages.instructions_template`（models.json 内每模型字段）是 Codex 实际用于组装 instructions 的载体，可以在既有模板文本末尾追加用户 instruction，实现追加语义。
- 本仓库 #822 已把 DeepSeek managed models.json 机制泛化为 `BuildManagedModelCatalog`，可复用于 instruction 注入。

## 2. 调研结论：长度上限

### 2.1 上游没有硬性长度上限

- **Claude Code**：SDK `SDKControlInitializeRequestSchema` 中 `appendSystemPrompt` 是 `z.string()`，无最大长度（`claudecode-src/src/entrypoints/sdk/controlSchemas.ts`）；CLI 侧 `--append-system-prompt` 仅受 OS 命令行参数长度（ARG_MAX）约束，源码还提供 `--append-system-prompt-file` 与 stdin 控制消息路径规避该限制（`src/main.tsx`、`src/cli/print.ts`）。官方文档未声明字符上限。
- **Codex**：`instructions_template` 是普通字符串，读取路径无大小截断；`model_instructions_file` 由 `try_read_non_empty_file` 全量读取，同样无硬上限。官方手册仅对项目说明 `AGENTS.md` 定义默认 `project_doc_max_bytes = 32768`（32 KiB），该限制不适用于模型 instructions。

### 2.2 产品上限：16,000 字符

两边机制实际只受“上下文窗口成本”和“配置/请求体大小”约束，因此需要一个统一的产品上限，前后端共用：

- 16,000 字符足够承载常见角色/风格/约束类提示词（约 4k–8k token，视语言而定）；
- UTF-8 最坏约 48 KiB，JSON 配置与 admin API 请求体仍然可控；
- 即使未来某条启动链路改为命令行参数传递，16k 也低于 Windows CreateProcess 约 32k 字符的参数上限；
- 取值远小于任何当前上下文窗口，避免 instruction 本身挤占上下文。

后端以 **Unicode 字符数**（`utf8.RuneCountInString`）校验，前端用 textarea `maxLength` + 实时计数。上限常量建议 `InstructionMaxChars = 16000`，放在 `internal/config`（Claude 与 Codex 共用），web 侧镜像同值常量。

## 3. 目标与非目标

### 目标

1. Codex API profile 与自定义 Claude profile 增加可选 `instruction` 字段，可保存、编辑、清空。
2. Claude：instruction 非空时，在 SDK 初始化控制消息中追加 `appendSystemPrompt`；留空不传。
3. Codex：instruction 非空时，为 profile 生成 managed models.json，并把 instruction 追加到目录内每个模型的 `instructions_template` 末尾；留空维持现状（不生成/不追加）。
4. Web UI：两个 Section 在模型/推理强度区域下方增加全宽 textarea，展示字符计数与上限，超限阻止保存。
5. 后端与 web 单测覆盖持久化、启动注入、留空不传、超限校验。
6. 设计文档落 `docs/inprogress/` 并更新 `docs/README.md` 索引。

### 非目标

- 不做 `--system-prompt` 完全替换语义。
- 不改内置/只读 profile（Codex native/OAuth、Claude 默认 profile）的可编辑边界。
- 不做模板变量、多段 instruction、按 agent 角色分别配置。
- 不把 instruction 视为密钥：它随 profile 摘要返回以便编辑；敏感内容仍由用户自行判断。

## 4. 现状与关键链路

### Codex

- 持久化：`config.CodexAPIProfileSecretConfig`（`internal/config/codex_profiles.go`）。
- 校验：`validateCodexAPIProfileInput` / `validateStoredCodexAPIProfileRevision`。
- admin API：`codexProfileWriteRequest`、`codexAPIProfileInputFromRequest`、`codexAPIProfileSummary`（`internal/app/daemon/admin_codex_profiles.go`），摘要类型 `state.CodexProfileSummary`（`internal/core/state/profile_catalog.go`）。
- 启动材料：`RuntimeResolver.resolveAPI` 生成 `SecretLaunchMaterial`；仅当 `subagentModel != "" || deepSeek` 时才生成 managed models.json（`internal/app/codexprofile/runtime_resolver.go`）。
- 目录生成：`codexcatalog.BuildManagedModelCatalog` + 内嵌 `deepseek_models.json`（`internal/codexcatalog/deepseek.go`）。内嵌条目已含 `model_messages.instructions_template` 基础模板。

### Claude

- 持久化：`config.ClaudeProfileConfig`（`internal/config/claude_profiles.go`）。
- admin API：`claudeProfileWriteRequest`、`adminClaudeProfileView`、create/update 分支（`internal/app/daemon/admin_claude_profiles.go`）。
- daemon → wrapper：`applyClaudeHeadlessProfileEnv` 通过 `ApplyClaudeProfileLaunchEnv` 注入环境（`internal/app/daemon/app_headless_claude_profile.go`）；wrapper 读取 `CODEX_REMOTE_CLAUDE_SETTINGS_JSON` 写入 `--settings`（`internal/app/wrapper/app_child_settings_claude.go`）。
- SDK 初始化：`claudeBootstrapInitializeFrame` 发送 `control_request`（subtype `initialize`）（`internal/app/wrapper/app_headless_claude.go`）。Claude 源码 `SDKControlInitializeRequestSchema` 明确支持该请求携带 `appendSystemPrompt`。

## 5. 设计决策

### 5.1 字段与持久化

- `CodexAPIProfileSecretConfig` 增加 `Instruction string`（JSON `instruction,omitempty`）；`CodexAPIProfileInput` 同步增加。
- `ClaudeProfileConfig` 增加 `Instruction string`（JSON `instruction,omitempty`）。
- 留空即不保存（`omitempty`）；规范化时 `TrimSpace`（仅两端空白），保留内部换行与缩进。
- 校验：`utf8.RuneCountInString(instruction) <= 16000` 且不含 `\x00`。Codex 走现有输入/存储校验；Claude 在 admin create/update 分支调用共享校验（可加 `config.ValidateClaudeProfileInstruction` 或复用通用函数）。

### 5.2 admin API

- Codex：`codexProfileWriteRequest.Instruction *string`、`codexAPIProfileInputFromRequest` 透传、`codexAPIProfileSummary` 输出 `instruction`（`state.CodexProfileSummary` 同步加字段）。
- Claude：`claudeProfileWriteRequest.Instruction *string`、`adminClaudeProfileView` 输出 `instruction,omitempty`。update 语义与其他非密钥字段一致：请求体带值即覆盖，带空串即清空；请求体缺省字段保持原值。

### 5.3 Claude 注入：SDK 初始化控制消息

不新增 CLI 参数、不写临时文件：

1. `config` 新增环境键 `CODEX_REMOTE_CLAUDE_APPEND_SYSTEM_PROMPT`（暂名），并加入 `claudeProfileLaunchEnvKeys` 清理列表。
2. `ApplyClaudeProfileLaunchEnv` 在 profile instruction 非空时写入该环境变量（built-in profile 不写、留空清掉旧值）。
3. wrapper 的 `claudeBootstrapInitializeFrame` 读取该环境变量，非空时在 initialize 请求中加入 `"appendSystemPrompt": <instruction>`。

这样指令通过 stdin JSON 控制消息传递，天然规避 ARG_MAX，且不污染 Claude 的 `--settings` 文件格式。

### 5.4 Codex 注入：managed models.json 追加 instructions_template

1. `codexcatalog.BuildManagedModelCatalog(models []string)` 增加 instruction 参数（或新增 `AppendInstruction(catalogJSON, instruction)` 辅助函数）。
2. 追加方式：对目录内每个 model 条目，取其 `model_messages.instructions_template`（缺失则视为目录生成失败，沿用现有失败语义），末尾拼接 `\n\n` + instruction，保持 `model_messages` 其他字段不变。
3. `RuntimeResolver.resolveAPI`：当 `instruction` 非空时，**强制**为 profile 生成 managed models.json（不再只依赖 subagentModel/DeepSeek 条件），并把主模型、审阅模型、子代理模型一并放入目录；既有 DeepSeek 分支选择逻辑保持不变。
4. 同一 instruction 应用到目录内全部模型（主模型 + 审阅 + 子代理），保证子代理也带上 profile 角色预设；这是 profile 级语义，也是实现上最简的一致路径。

### 5.5 Web UI 版式

两个 Section 均遵守现状：

- 位置：模型 / 子代理模型 / 推理强度区域**之后**、上下文大小控件**之前**，作为 `form-grid` 内最后一个全宽项。
- 结构：`<label className="field form-grid-span-2 stack-top">`，内含 `<span>指令 / 角色提示词（可选）</span>`、`<textarea>` 与字符计数（`{draft.instruction.length}/16000`）。
- textarea：`maxLength={16000}`、`rows` 建议 6–8、等宽或普通字体均可（沿用现有 field 样式，不新增 CSS 类）。
- 校验：`validateDraft` 增加超限提示（虽然 `maxLength` 已阻止，仍需防御性校验）；计数超限时给红色/错误态文案（沿用现有错误提示风格）。
- draft 初始值/回填：`instruction: profile.instruction?.trim() || ""`；create/update payload 携带 `instruction: draft.instruction.trim()`。

## 6. 影响面

### 6.1 后端配置层

- `internal/config/codex_profiles.go`：`CodexAPIProfileSecretConfig` / `CodexAPIProfileInput` 增加字段；create/update/迁移路径携带；`validateCodexAPIProfileInput`、`validateStoredCodexAPIProfileRevision`、`NormalizeCodexAPIProfileRecords` 增加校验/trim；update“无变化”比较纳入新字段。
- `internal/config/claude_profiles.go`：`ClaudeProfileConfig` 增加字段；`NormalizeClaudeProfiles` 增加 trim；`claudeProfileLaunchEnvKeys` 增加新环境键；`ApplyClaudeProfileLaunchEnv` 注入/清理。
- `internal/config/claude_runtime_settings.go`：不修改（instruction 不走 `--settings`）。
- 新增共享上限常量与校验函数（建议 `internal/config/instruction.go`）。

### 6.2 后端 admin API

- `internal/app/daemon/admin_codex_profiles.go`：write request、input 转换、summary。
- `internal/app/daemon/admin_claude_profiles.go`：write request、view、create/update 分支（含校验与缺省语义）。
- `internal/core/state/profile_catalog.go`：`CodexProfileSummary` 增加 `Instruction string`。

### 6.3 启动注入

- `internal/app/wrapper/app_headless_claude.go`：initialize 帧加入 `appendSystemPrompt`。
- `internal/app/codexprofile/runtime_resolver.go`：instruction 非空时生成 managed catalog。
- `internal/codexcatalog/deepseek.go`：目录生成支持 instruction 追加（及对应单测）。

### 6.4 Web

- `web/src/lib/types.ts`：`ClaudeProfileSummary` / `ClaudeProfileWriteRequest` / `CodexProfileSummary` / `CodexProfileWriteRequest` 增加 `instruction?`。
- `web/src/routes/admin/ClaudeProfileSection.tsx`：draft、textarea、计数、校验、payload。
- `web/src/routes/admin/CodexProviderSection.tsx`：同上。
- 组件测试：两个 Section 的渲染、计数、超限阻止、保存 payload。

## 7. 测试计划

- `internal/config`：字段持久化/规范化/上限校验（Codex create/update、Claude normalize）、launch env 注入与清理。
- `internal/app/codexprofile`：instruction 非空生成 managed catalog、留空不生成、目录内模板追加正确。
- `internal/codexcatalog`：append instruction 后 JSON 结构、基础模板保留、缺失模板失败语义。
- `internal/app/wrapper`：initialize 控制帧在 instruction 非空时携带 `appendSystemPrompt`，空时不携带。
- `internal/app/daemon`：admin API 创建/更新/清空、超限返回校验错误、摘要回显。
- web：两个 Section 组件测试。

## 8. 相关文档

- issue：<https://github.com/kxn/codex-remote-feishu/issues/823>
- 上一单设计：`docs/inprogress/codex-claude-subagent-model-design.md`（#822）

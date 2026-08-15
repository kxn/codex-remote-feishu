# 视觉辅助看图工具（describe_image）设计

> Type: `inprogress`
> Updated: `2026-08-15`
> Summary: 为不支持视觉的主模型提供可主动调用的 `describe_image` 工具：支持多图 + id 引用、可选 prompt，背后经可配置的 OpenAI Chat / Responses / Anthropic / Gemini 协议适配器做单次视觉推理；工具默认注入、由 profile 级开关关闭。

## 背景与目标

部分主对话模型（如 deepseek 等）不支持直接查看图片。飞书图片会 staging 到本地，以 `local_image`（本地路径）输入进模型上下文，但这类模型看不到图片内容。

目标：提供一个**由主模型主动调用**的看图辅助工具 `describe_image`。工具内部调用一个**可配置的支持视觉的模型端点**（辅助模型），把图片和提示词发过去，返回文字分析，供主模型继续回答用户。

约束：

- 不自动注入：不在图片到达时自动调用视觉模型（避免二次调用），由主模型决定何时需要看图。
- 工具只负责“看图”，不负责“拿图”：不接受 URL、不下载，图片必须是本地路径。
- 单次推理即可：不需要 agent 化、多轮、流式。
- 协议适配要覆盖主流端点：OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini。

## 工具接口契约

```jsonc
{
  "name": "describe_image",
  "description": "Describe one or more local images through a vision model and return its textual analysis. Call this tool when you cannot directly see image content in the conversation and need to know what an image shows. Useful for: reading text in images (errors, code, documents, numbers), describing UI / charts / objects, and comparing multiple images. Do NOT call this tool if you can view images directly; in that case answer from the image itself. Pass each image's local path (same reference as in the conversation inputs). For multiple images, give each a short id and refer to ids in prompt (e.g. \"compare img1 and img2\"). The tool returns plain text; use it to continue answering the user.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "images": {
        "type": "array",
        "minItems": 1,
        "maxItems": 5,
        "items": {
          "type": "object",
          "properties": {
            "id":    { "type": "string", "description": "Short identifier, e.g. img1, img2" },
            "image": { "type": "string", "description": "Local file path of the image, same reference as in the conversation inputs" }
          },
          "required": ["id", "image"]
        }
      },
      "prompt": {
        "type": "string",
        "description": "Optional question for the vision model; may reference ids, e.g. \"compare img1 and img2\". Defaults to the configured default prompt."
      }
    },
    "required": ["images"]
  }
}
```

设计要点：

- 单图即 `images` 长度为 1，模型只有一种调用形态。
- `id` 由模型自由填写，prompt 可引用，覆盖对比/指定某张图等场景。
- `maxItems: 5` 防超限；视觉模型一次看 5 张以内的对比类场景足够。
- 工具描述使用英文（工具描述是给模型读的，与系统内其它工具保持一致），用户可见文案使用中文。

## 触发与注入策略

### 默认注入 + profile 级显式关闭

- 默认注入 `describe_image`：主要用户是非视觉模型，默认可用才能开箱即用。
- 开关粒度是 **profile 级**：一个机器人可以配多种模型（不同 profile 用不同模型），有的支持视觉、有的不支持。每个 profile 声明“该 profile 使用的主模型支持直接看图”，声明后该 profile 的会话**不注入** `describe_image`（机制级硬关闭，不是只靠提示词）；未声明的 profile 默认注入。
- 工具描述中的“能直接看图就不要调用”仅作为提示词兜底，覆盖未关闭开关的视觉模型场景。

### 注入载体

- 作为新工具加入 `internal/app/daemon/tool_service.go` 的 `toolDefinitions()`，随 feishu tool-service MCP server 自动被 Codex / Claude / OpenCode 加载。
- 鉴权沿用现有 MCP caller-instance 校验。
- 关闭开关的实现：同一 daemon 下不同实例可能使用不同 profile，因此 MCP server 必须**按 caller 区分工具集**——`listTools` 根据 caller instance 解析当前生效 profile，该 profile 声明支持视觉时不返回 `describe_image`；工具调用时同样做一次防御性校验。

## 视觉模型配置（辅助模型）

管理页“对话后端 → 辅助模型”tab 中配置：

```yaml
vision_assist:
  protocol: "openai_chat"          # openai_chat | openai_responses | anthropic | gemini
  base_url: "https://api.example.com/v1"
  api_key_env: "VISION_API_KEY"
  model: "gpt-5.6-vision"
  default_prompt: "请详细描述这张图片：主要对象、界面元素、文字内容（逐字转录）、背景。包含报错、代码或数字时完整保留。"
```

配置归属：

- 端点配置（协议、base URL、API key、模型名、默认提示词）放“辅助模型”tab：它是独立的辅助服务端点，不绑定 Claude / Codex / OpenCode 任一主后端。
- “主模型支持直接看图”开关放 profile 级（三个主后端各自的 profile 配置里）：它描述的是该 profile 使用的主模型能力，与辅助端点无关。

## 协议适配层（独立通用单次推理包）

单次推理适配器**单独放一个通用包**（建议 `internal/singleturn`），不绑定视觉语义——它以后在其它地方（翻译、摘要、文本分类等单次推理）也要复用。视觉只是它的一个输入形态（消息里含图片）。

```go
// 通用单次推理接口：请求 = 模型名 + 消息列表（文本/图片），返回纯文本。
type Provider interface {
    Complete(ctx context.Context, req Request) (string, error)
}

type Request struct {
    Model    string
    Messages []Message
}

type Message struct {
    Text   string
    Images []Image // 内部已统一为 base64 + MIME
}

type Image struct {
    ID       string // 可选；多图时用于消息里声明 id 映射
    Data     []byte
    MIMEType string
}
```

协议适配：

| 协议 | 请求形状 | 图片放法 | 取文本 |
| --- | --- | --- | --- |
| OpenAI Chat Completions | `messages[{role, content:[text, image_url]}]` | `image_url.url` = data URL | `choices[0].message.content` |
| OpenAI Responses | `input[{role, content:[input_text, input_image]}]` | `input_image.image_url` | `output_text` |
| Anthropic Messages | `messages[{role, content:[text, image]}]` | `image.source` = base64 + media_type | `content[0].text` |
| Gemini | `contents[{parts:[text, inline_data]}]` | `inline_data` = base64 + mime_type | `candidates[0].content.parts[0].text` |

`describe_image` 在 daemon 侧使用该包：把用户 prompt 拼成一条文本消息 + 图片列表，调用 `Provider.Complete`。公共逻辑（读文件、id 映射声明、超时、错误转文本）在调用侧只实现一份，adapter 只负责协议形状与响应提取；包内不引入对话状态、不依赖 MCP/飞书。

## 内部流程

1. 校验 `images` 非空且 ≤ 5。
2. 按传入顺序读取本地图片文件，校验 MIME 为图片格式（png/jpeg/webp/gif 等），转 base64。
3. 按传入顺序组装协议消息，并在文本中声明 id 映射（“img1=第1张，img2=第2张，请按 ID 回答”）。
4. 调用所选协议的 adapter 单次推理。
5. 返回纯文本回答（不套 JSON）。

## 错误处理

- 图片路径不存在 / 读取失败 / 非图片格式 → 明确错误，主模型如实转述，不做自动重试。
- 视觉接口调用失败 / 超时 → 按协议解析错误信封，统一转为一句错误文本返回。
- 参数校验失败（超过 5 张、缺 id/image）→ schema 校验错误。

## 安全边界

- 工具**不接受 URL**、不做下载：模型没有网络能力时不应通过工具获得任意 URL 抓取能力；权限边界保持“模型负责拿图，工具负责看图”。
- **不限制路径读取范围**：工具接受任意本地图片路径（与模型自身文件读取能力一致——Codex exec / Claude Read 本来就能读这些文件）。视觉端点是管理员自行配置的可信端点，模型把文件内容发给它属于其能力范围内，不做额外路径白名单。
- 保留基础校验：文件不存在 / 读取失败 / 非图片格式 → 明确错误；不做任意文件读取之外的“外带”假设。
- API key 通过环境变量注入，不落管理页明文（与现有 profile 密钥处理一致）。

## 管理页 UI 位置

```
对话后端
  Claude | Codex | OpenCode | 辅助模型
                               └─ 图片描述辅助（describe_image）
                                    Base URL   [________________]
                                    API Key    [________________]
                                    模型名     [________________]
                                    协议       [ OpenAI Chat ▾ ]
                                    默认提示词 [________________]
```

- 开关“该 profile 使用的主模型支持直接看图，不注入图片描述辅助工具”放在“对话后端”各 profile 编辑器里（Claude / Codex / OpenCode profile 项各一个）。
- “辅助模型”tab 命名比“看图模型”宽，未来可承载其它辅助用途（如翻译、OCR 增强）而不新增位置。

### UI 样式约束

- **禁止创造样式**：视觉能力勾选与“辅助模型”tab 页面只使用现有样式体系——`.form-grid` / `.field` / `.field-hint` / `.card` / 现有按钮类等（`web/src/styles.css`），不新增 CSS 规则、不发明自定义组件样式。
- 勾选用 `<label className="field">` + `<input type="checkbox">` + `<span className="field-hint">` 的既有布局表达，不复刻其它页面的视觉。
- 若实现时发现现有样式无法表达 checkbox（当前 styles.css 无 checkbox 专用规则），先停下来与用户确认是否允许在现有样式体系内补充最小规则，默认不自行新增。

## 非目标

- 不做图片到达时的自动视觉注入（避免二次调用）。
- 工具不接受 URL / 不做图片下载。
- 不做 agent 化、多轮对话、流式输出。
- 不做多协议之外的自动探测（profile 的视觉能力由配置声明，不自动探测模型能力）。
- 不改变主后端（Claude / Codex / OpenCode）的工具注入机制。

## 测试计划

- 工具层：多图上限、MIME 校验、错误路径、id 映射组装。
- 协议层：四个 adapter 各自请求形状与响应提取的单元测试（用固定 fixture）。
- 注入层：profile 声明支持视觉时 `listTools` 不返回工具；未声明时返回；同一 daemon 不同 profile 的实例互不影响。
- 集成：通过 feishu tool-service MCP server 调用 `describe_image` 的端到端路径（mock 视觉端点）。
- Web：辅助模型 tab 的配置读写、协议选择、开关读写。
- `scripts/check/pre-commit.sh` + 相关包 `go test ./...`。

## 涉及代码与关键实现点（调研结论）

### 1. MCP 工具注册与分派

- `internal/app/daemon/tool_mcp.go`：`newToolRuntimeHandler` / `newToolMCPServer` / `handleMCPToolCall`。
- `internal/app/daemon/tool_service.go`：`toolDefinitions()` 增加 `describe_image` 定义（name/description/inputSchema），`handleMCPToolCall` 增加分派分支。
- 鉴权沿用 `requireToolAuth`（caller instance 已注入 request context）。

### 2. 按 caller 动态工具集

- go-sdk 的 `NewStreamableHTTPHandler(getServer func(*http.Request) *Server, ...)` 支持**按请求返回不同 server**；`getServer` 只在 MCP session 创建时调用一次，同一 session 复用固定 server。
- 实现：`newToolRuntimeHandler` 的工厂函数改为 `a.newToolMCPServerForRequest(req)`：
  1. 从 `req.URL.Query()` / request context 取 caller instance（`CallerInstanceIDQueryParam`）；
  2. `a.service.Instance(instanceID)` 读 instance record 的 `Backend` + `CodexProfileID` / `ClaudeProfileID` / `OpenCodeProfileID`（**用 instance 而非 remote turn 解析**：MCP 初始化时 remote turn 可能尚未绑定，而 instance 直接带 profile ID）；
  3. `a.loadAdminConfig()` 查对应 profile 的 `VisionSupported`：声明支持视觉 → server 不注册 `describe_image`；否则注册。
- session 复用语义：profile 切换会重启实例 → 新 wrapper → 新 MCP session → 按新 profile 重新决定工具集；daemon 重启同理。`handleMCPToolCall` 内再做一次防御性校验。

### 3. profile 视觉能力字段

- `CodexAPIProfileSecretConfig`（`internal/config/codex_profiles.go`）、`OpenCodeAPIProfileSecretConfig`（`opencode_profiles.go`）、`ClaudeProfileConfig`（`claude_profiles.go`）各加 `VisionSupported bool`。
- 对应 `CodexAPIProfileInput` / `OpenCodeAPIProfileInput` / Claude profile 写入请求、Web 三个 `ProfileSection` 的 draft 类型与表单（新增 checkbox）。
- admin handlers（`handleCodexProfileUpdate` 等）走既有 If-Match + input 流程，视觉能力变更随 revision 机制持久化。

### 4. 辅助模型端点配置

- `config.AppConfig` 新增 `VisionAssist` 配置（protocol / baseURL / apiKeyEnv / model / defaultPrompt）。
- admin API：`GET /api/admin/config` 已存在（redact 输出）；需新增写入入口（`PUT /api/admin/config` 或专用 `GET/PUT /api/admin/vision-assist`）。
- Web：`AdminRoute.tsx` 对话后端区新增第四个 tab「辅助模型」，页面读写端点配置。

### 5. 视觉推理适配器

- 新包（`internal/singleturn`）：通用单次推理 `Provider` 接口 + OpenAI Chat / Responses / Anthropic / Gemini 四个 adapter，不绑定视觉语义，未来其它单次推理复用；只做单次推理（请求组装 + 响应文本提取 + 错误归一），不引入对话状态。
- 配置读取与调用集中在 daemon 层；API key 按设计走环境变量（`apiKeyEnv`），Web 只填 env 名。

### 6. 工具内部流程与安全

- 读取本地图片文件 → 图片格式校验（png/jpeg/webp/gif 等 MIME）→ base64 组装 → id 映射声明 → 协议 adapter 调用 → 返回纯文本；不做路径白名单限制（与模型自身读取能力一致）。

### 7. 风险与实现注意

- `getServer` 在 session 创建时决定工具集，会话生命周期内不变；依赖实例重启刷新（profile 切换即重启，符合现状）。
- profile 视觉能力走 revision：旧实例沿用旧 revision，重启后生效。
- 辅助模型 API key 若与现有 profile 一致存 config，需要纳入 `redactAdminConfig` 打码；若走环境变量则无此问题（设计文档默认环境变量）。

## 开放问题

- 视觉端点鉴权除 Bearer 外是否还有其它形态（第一版只做 Bearer）。

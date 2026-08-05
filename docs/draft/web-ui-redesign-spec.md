# Web 前端（Setup / Admin）UI 重设计交接规范

> Type: `draft`
> Updated: `2026-08-02`
> Summary: 修正落地前 mock/spec 漂移：Codex 示例模型统一到 gpt-5.5，用户可见 profile 文案改为「配置」，权限检查示例 scope 对齐当前 manifest 口径；第 9 章可选增强仍仅批准「机器人权限检查」。

## 1. 这份文档是什么

这是 `web/src` 前端两个页面（Setup 向导、Admin 管理页）**整体重设计**的唯一依据文档（SSOT）。目标读者是**没有任何本项目上下文的接手开发者**。

执行规则：

- 本文没写的行为，不允许自行发明；写了"必须"的，不允许省略；写了"禁止"的，不允许出现。
- 本文与 `docs/general/web-design-guidelines.md`、`docs/general/page-mock-guidelines.md` 同时生效；冲突时以那两份通用规范为准，并先回来改本文。
- 实现过程中发现本文有漏洞或矛盾，先修订本文再写代码，不允许"先实现后补文档"。

规范用语：

- **必须** = 不做就是不合格
- **禁止** = 做了就是不合格
- **可以** = 允许自行决定，但必须在 PR 描述里说明选择

## 2. 产品背景（给零上下文的读者）

Codex Remote 是一个跑在用户自己机器上的守护进程（Go 后端 + 这套 React 前端）。它把**飞书机器人**接入本机的 **Codex / Claude 对话后端**，让用户在飞书聊天里驱动本机的编码 agent。Web 前端是 daemon 自带的本地页面，通过同源 cookie 鉴权（localhost 直连自动放行），**没有登录/登出 UI，也不要做**。

前端是一个 Vite + React SPA，入口 `web/src/main.tsx`，路由极简：路径以 `/setup` 结尾 → Setup 页，其余 → Admin 页。支持 `/g/<mount>/` 挂载前缀（`web/src/lib/paths.ts`），所有 fetch 必须继续走 `web/src/lib/api.ts` 的封装（同源 cookie、超时、错误格式化），禁止绕过它直接 `fetch`。

两个页面服务同一个人——**装这台 daemon 的机主本人**：

- **Setup（`/setup`）**：一次性安装向导。使命是"把机器人弄上线"。完成后调用 `POST /api/setup/complete`，服务端永久关闭 setup 通道。用户一生可能只见到一次。
- **Admin（其余路径）**：长期驻留控制台。用户偶尔回来，只为三件事：**还正常吗 / 改点什么 / 清理一下**。

## 3. 范围

### 3.1 在范围内

- `web/src` 下 Setup 页与 Admin 页的信息架构、布局、视觉、文案、交互、反馈
- 共享组件与 `styles.css` 的重建
- 第 8 章列出的"行为保全清单"逐条迁移
- 第 9.1 章已批准的「机器人权限检查」区块（含配套新后端端点，这是 3.2 后端禁令的唯一例外）

### 3.2 不在范围内（禁止顺手做）

- 任何 Go 后端改动（API 形状、状态机、文案 source of truth 都在后端，前端只做展示层重组）；唯一例外是第 9.1 章批准的权限检查端点
- 任何新 API 的调用，除非列在第 9 章"可选增强"且已被显式批准
- preview page、`internal/app/daemon/adminui/**`、飞书卡片相关的任何东西
- 构建工具链、依赖升级、测试框架更换（现有 vitest/playwright 保留并适配）
- 登录、登出、多用户、权限系统等不存在的概念

## 4. 全局设计系统

### 4.1 品牌与色板

品牌资产已存在：logo 在 `/branding/codex-remote-logo.svg`（`web/src/components/BrandLogo.tsx`），配色取自 logo：

| 角色 | 色值 | 用途 |
|---|---|---|
| 墨蓝 ink | `#112B39`（深色端 `#0A1824`） | Admin 侧边导航底色、正文标题 |
| 奶油 cream | `#F7EBD7` / `#E7D5B6` | Setup 页面底色、深色区上的浅文字 |
| 青碧 teal | `#20CBB8` / `#0F9588` | 主 CTA、链接、健康/成功态 |
| 暖橙 amber | `#FFB56F` / `#FFD48C` | 警示、待处理强调 |
| 危险 red | 自行取一个不刺眼的深红（如 `#C2443B`） | 危险操作、失败态 |

必须用语义化 CSS 变量（如 `--ink` `--cream` `--teal` `--amber` `--danger`），禁止在组件里写死色值。

### 4.2 两个页面两种气质

- **Setup = 安装向导**：奶油浅底，大留白，单列居中（内容 max-width ≈ 680px），一次只呈现一个任务。
- **Admin = 运维控制台**：墨蓝深色侧边导航 + 浅色内容区，信息密度更高，状态用"小圆点 + 文字"而非大色块。

两面共享同一套基础组件（按钮、徽标、notice、确认框、空态、加载态、表单控件），但**页面骨架不共享**：禁止复用现有的 `ShellScaffold` 导轨壳（`web/src/components/ui.tsx` 将整体重写）。

### 4.3 排版与双端

- 字体：系统字体栈（`-apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif`），禁止引入网络字体。
- 断点：< 768px 为移动端。双端不是"桌面做好再适配"，而是同一套信息架构的两种排布：
  - Setup：天然单列，移动端只需保证主 CTA 始终在首屏可及。
  - Admin：桌面左侧固定导航（4 项）；移动端导航变为底部 tab 栏（4 项），内容单列。
- 横竖屏切换后主流程必须成立（按 `page-mock-guidelines.md` 第 7 章验收）。

### 4.4 反馈槽位（全局只允许这五类）

1. **字段下错误**：表单校验失败，红字在字段正下方。
2. **卡片内 notice**：某个卡片/步骤内的状态横幅（good / warn / danger 三种 tone）。
3. **页面顶部 toast 条**：操作结果的短暂反馈（成功/失败），出现在页面顶部内容区上方，不遮挡导航。
4. **确认对话框**：破坏性或有外部后果的操作前（删除、发布）。统一样式，说明后果 + 确认/取消。
5. **整页状态**：仅用于首次加载中、首次加载失败（带"重新加载"按钮）。

禁止发明新的反馈位置（不允许页面级弹窗报错、不允许在按钮旁边塞行内错误文字等）。后端返回了没写明的错误类型时，落入该页面最近的卡片内 notice 或 toast，使用通用文案"操作没有完成，请重试"。

### 4.5 文案规则

- 全部 UI 文案为中文，站在"用户现在要做什么"的口径；按钮用动词短语。
- 禁止出现在用户可见区域的词：`gateway`、`surface`、`manifest`、`reconcile`、`instance`、`runtime`、`etag`、`revision`、`profile catalog`、任何错误 code 原文、任何文件系统路径（唯一例外：飞书后台外链、baseURL、App ID 这些用户自己填过或需要去飞书后台对照的值）。
- 后端错误 code → 中文文案的映射表**必须原样迁移**现有实现（见第 6、7 章各表），不允许重写措辞风格。

### 4.6 语义色 token（必须全量使用，禁止写死色值）

所有颜色必须经过下列 CSS 变量引用。组件代码、`styles.css` 中出现这些变量之外的色值即不合格（品牌 logo 图片除外）。

| token | 值 | 用途 |
|---|---|---|
| `--ink` | `#112B39` | 标题文字、深色导航上的主色块 |
| `--ink-deep` | `#0A1824` | Admin 侧边导航底色 |
| `--cream` | `#F7EBD7` | Setup 页面底色 |
| `--teal` | `#0F9588` | 主 CTA、链接、选中态 |
| `--teal-bright` | `#20CBB8` | 深色底上的强调（导航激活、成功点） |
| `--amber` | `#FFB56F` | 深色底上的警示强调 |
| `--danger` | `#C2443B` | 危险操作、失败态 |
| `--bg-page` | `#F5F2EA` | Admin 内容区底色（Setup 用 `--cream`） |
| `--bg-card` | `#FFFFFF` | 卡片、对话框 |
| `--bg-hover` | `#F3EFE4` | 列表行 hover |
| `--text-primary` | `#16242C` | 正文主文字 |
| `--text-secondary` | `#4A5B64` | 说明文字 |
| `--text-muted` | `#7C8A91` | 辅助/占位文字 |
| `--text-on-dark` | `#E7D5B6` | 深色底上的正文 |
| `--text-on-dark-muted` | `#9FB3BC` | 深色底上的辅助文字 |
| `--border` | `#E3DCCB` | 卡片/输入框描边 |
| `--border-strong` | `#CFC6B0` | 输入框 hover |
| 成功态组 | bg `#E4F5F1` / border `#A5DED3` / text `#0C6E64` | notice、badge |
| 警示态组 | bg `#FDF0DC` / border `#EFCB92` / text `#8A5A1D` | notice、badge |
| 危险态组 | bg `#FAE8E6` / border `#E5AAA3` / text `#96231B` | notice、badge |

### 4.7 字体与文字阶

- 字体栈：`-apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif`；禁止网络字体。App ID、baseURL 这类用户需要对值的字段可以用 `ui-monospace, SFMono-Regular, Menlo, monospace`。
- 文字阶（共 6 级，禁止新增级别）：

| 级别 | 字号/行高/字重 | 用途 |
|---|---|---|
| 页面标题 | 24 / 1.3 / 600 | Setup 幕标题、Admin 区标题 |
| 卡片标题 | 16 / 1.4 / 600 | 卡片头、对话框标题 |
| 正文 | 14 / 1.6 / 400 | 默认正文、按钮文字（500） |
| 辅助 | 13 / 1.5 / 400 | 摘要、说明、表单 label（500） |
| 元信息 | 12 / 1.4 / 400 | 徽标、时间、状态点文字 |
| 大数字 | 28 / 1.2 / 600 | 总览统计值 |

### 4.8 间距、圆角、描边、阴影

- 间距：4px 基准，只允许 `4 / 8 / 12 / 16 / 24 / 32 / 48`。卡片内边距桌面 24、移动 16；卡片间距 16。
- 圆角：输入框/按钮 `6`，卡片/notice `10`，对话框 `14`，徽标/状态点 `999`（全圆）。
- 描边：统一 `1px solid var(--border)`；输入框 hover 用 `--border-strong`。
- 阴影只许两级：卡片 `0 1px 2px rgba(10,24,36,.06)`；对话框/toast `0 8px 24px rgba(10,24,36,.18)`。禁止其他阴影。

### 4.9 布局尺寸

- **Setup**：内容单列居中，max-width `680px`，页面左右 padding 桌面 24 / 移动 16。顶部进度条高 `4px`，三段等宽，已填充段 `--teal`。
- **Admin**：侧边导航宽 `232px`，底色 `--ink-deep`，品牌区在顶部（logo + 产品名）。内容区 max-width `980px`，padding 桌面 28 / 移动 16。
- **移动端 Admin**：侧边导航消失，改为底部 tab 栏，高 `56px`，4 个文字 tab 等宽，激活态文字 `--teal` + 顶部 2px 指示条；内容区底部留 `56px` 安全空间。
- 触控目标（按钮、列表行、tab）高度 ≥ `40px`。

### 4.10 组件样式规格

- **主按钮**：高 36（移动 40），padding `0 16`，圆角 6，字号 14/500，底色 `--teal`、文字白；hover `#0C8075`；disabled 透明度 50% + 禁止手势。每屏主按钮只有一个。
- **次按钮**：白底、`--border` 描边、`--text-primary` 文字，其余同主按钮。**危险按钮**：底色 `--danger`。**文字按钮**（ghost）：无框，`--teal` 文字，用于低频操作。
- **状态点**：`8px` 圆点 + 12px 文字；成功 teal、警示 amber 系、危险 danger、中性 `--text-muted`。
- **徽标**：全圆角，12px 字，padding `2px 8px`，用对应语义态的 bg/text 组。
- **notice**：圆角 10，`1px` 语义态 border + 语义态 bg，padding `12 16`，14px 文字；标题加粗 500。
- **卡片**：`--bg-card` 底，圆角 10，`1px --border` 描边，卡片级阴影；头部 = 16/600 标题 + 可选 13px 辅助描述 + 右侧操作区。
- **表单**：label 在输入框上方（13/500）；输入框高 36、圆角 6、`--border` 描边，focus 时边框变 `--teal` + 2px `rgba(15,149,136,.25)` 外环；校验失败边框变 `--danger` + 字段正下方 12px `--danger` 错误文字。
- **确认对话框**：max-width `420px`，圆角 14，padding 24，遮罩 `rgba(10,24,36,.45)`；标题 16/600，正文 14，按钮组右对齐（取消 = 次按钮，确认 = 主按钮或危险按钮）。
- **toast**：内容区顶部居中，圆角 10，语义态 bg/border，14px；成功 4 秒自动消失，警示/危险驻留到下一次操作。
- **列表行**：最小高 44，hover 底色 `--bg-hover`，选中行左侧 `3px --teal` 指示条。
- **空态**：垂直居中，14px `--text-secondary` 一句话，无装饰插图。
- **加载态**：16px teal 环形 spinner + 13px 辅助文字；只在数据所在卡片内出现。

### 4.11 状态与动效

- 所有交互态（hover/focus/激活）必须有视觉反馈；focus 可见性：`2px` `--teal` 外描边，offset 2px（键盘可达性，禁止 `outline: none` 且无替代）。
- 动效只允许：`120–200ms ease-out` 的颜色/透明度过渡；对话框淡入 + `scale(.98→1)` 150ms。禁止视差、弹性、装饰性动画。`prefers-reduced-motion` 下全部关闭。
- 正文文字对比度 ≥ 4.5:1；语义态不能只用颜色区分（状态必须同时有文字或圆点+文字）。

### 4.12 图标与图片

- 禁止引入图标库/图标字体。界面不放装饰性插画。
- 允许的图形元素：品牌 logo（`/branding/codex-remote-logo.svg`）、状态色点、进度条、二维码（业务数据）。对话框关闭等确需符号时用字符 `×`。

### 4.13 样式的最终裁决

- 4.6–4.12 的数值是 mock 与实现**必须使用的基线**。mock 阶段可以微调个别数值（例如某档间距），但 mock 一经确认，其呈现即为最终视觉标准，实现不得偏离。
- 实现时发现基线不够用（缺一档字号、缺一个语义色），先回来修订本节新增条目，再使用；禁止在代码里"先加一个再说"。

## 5. Setup 页规范

### 5.1 页面合同

- 最终用户：第一次装好 daemon 的机主。
- 本页只服务一个任务：把飞书机器人接上线并完成本机收尾。
- 允许出现的信息类型：当前要做什么、当前状态、本步操作与结果、下一步入口、卡住时的原因与建议。
- 禁止出现的信息类型：内部状态名、检测路径、二进制细节、会话/token 概念、后续管理功能的预告（完成页一句"可在管理页继续调整"除外）。

### 5.2 信息架构：三幕式向导

后端状态机（7 个 stage）**不动**，前端展示层重组为三幕：

```
[1 准备环境] ─── [2 连接飞书机器人] ─── [3 本机集成] ─── ✓ 完成
                 连接 · 配置 · 菜单       自动运行 / VS Code（均可选）
```

- 顶部细进度条，三段。可回看已完成幕，禁止越过当前幕跳转（沿用现有 `currentStage` 可达性规则：index 大于当前幕且未解决不可点）。
- 主体永远只有**一张当前任务卡 + 一个主 CTA**；已完成幕折叠成一行成功摘要（可展开回看）；未到幕只显示名称。
- 幕 2 内部用子进度点（连接 → 配置 → 菜单）表达连续性：一个子步完成后自动滚动定位到下一子步。
- 幕 3 两张可选卡（自动运行 / VS Code）并排（移动端上下），各带"可选，之后可在管理页处理"标注。
- **新增展示**（后端已返回、现前端未渲染，必须接上）：
  - 卡住时主卡顶部显示 `completion.blockingReason` 的人话转写 + `guide.recommendedNextStep`。
  - `guide.remainingManualActions` 若非空，以"还需要你手动处理"列表呈现在幕 2。

### 5.3 数据来源

- `GET /api/setup/bootstrap-state`：产品名/版本（页面标题）、admin URL（完成跳转兜底）。
- `GET /api/setup/onboarding/workflow[?app=<id>]`：唯一状态机数据源。任何操作成功后重拉它刷新（`preserveDisplayedStep` 语义保留：刷新类操作留在当前幕）。

### 5.4 逐幕行为表

#### 幕 1 · 准备环境（stage: `runtime_requirements`）

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 环境检查展示 | （workflow 内 `runtimeRequirements` 字段） | ready → 成功 notice「环境正常」+ 主 CTA「继续」；否则 warn notice 显示 `summary` | 失败时只显示两类人话条目：「本机服务」（`headless_launcher` 失败）、「对话后端」（`binary_loop` 失败，或 `real_codex_binary` 与 `claude_binary` 同时失败）；其余 check 细节不上屏 |
| 重新检查 | 重拉 workflow | 环境变好时**自动晋级到幕 2** 并 toast「环境正常，已自动进入飞书连接」 | 该自动晋级行为必须保留 |

#### 幕 2 · 连接飞书机器人

子步 A · 连接（stage: `connect`）——两种模式切换（扫码创建 / 手动输入），切换时清空会话与错误。

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 扫码创建 | `POST /api/setup/feishu/onboarding/sessions` → `GET .../{id}` 轮询 → `POST .../{id}/complete` | 进入该子步且无会话时**自动创建会话**；二维码就绪前显示占位文案；`pending` 按 `pollIntervalSeconds`（与 5 取较大者）轮询；`ready` 后**自动 complete**；失败/过期显示 danger notice + 「重新扫码」；ready 但 complete 失败显示「重新验证」 | 用户零点击完成连接的全自动链路必须保留；操作进行中（busy）暂停轮询 |
| 手动输入 | `POST /api/setup/feishu/apps` 或 `PUT .../{id}` → `POST .../{id}/verify` | 表单：App ID*、App Secret*、名称（可选）；主 CTA「验证并继续」；已验证通过时顶部 good notice「当前飞书应用连接验证已通过」；名称/AppID 预填，Secret 永不回填 | 保存成功但 verify 失败：机器人已保存 + danger 提示 |
| readOnly 应用 | 同上（跳过保存直接 verify） | 由运行环境提供的机器人：三输入框全禁用 + warn notice 说明 | 必须保留 |
| `gateway_apply_failed` 恢复 | （重拉 workflow） | 配置已保存但运行态未同步 → warn notice（非 danger） | 必须保留 |

子步 B · 配置（stage: `auto_config`）——数据 `autoConfigStage.plan`，按钮完全由 `allowedActions` 驱动。

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 状态展示 | （plan 字段） | headline + 摘要 + 状态 notice；`blockingReason` 三种转写（`unsupported_application` / `application_under_review` / `apply_required_before_publish`）；「需要先解决的问题」（blockingRequirements，danger）与「可按降级继续的能力」（degradableRequirements，warn）两组清单 | plan.status 九态文案映射（clean/degraded/unsupported/apply_required/publish_required/awaiting_review/blocked/runtime_pending/loading）与 10 种 feature 中文名，**原样迁移** `web/src/routes/shared/feishuAutoConfig.ts` |
| 自动补齐 | `POST .../{id}/auto-config/apply` | allowedActions 含 `apply` 时显示 | 成功提示取 `result.summary`，tone 由 result.status 决定；HTTP 错误优先显示 `error.details` |
| 继续发布 | `POST .../{id}/auto-config/publish` | 必须先弹**确认对话框**（说明可能进入管理员审核） | 确认对话框保留；切 app 自动关闭 |
| 先按降级继续 | `POST .../{id}/onboarding-auto-config/defer` | allowedActions 含 `defer` 时显示；成功提示「已按降级继续，你后续仍可回到这里重新补齐」 | stage 变 `deferred` |
| 重新检查 | `POST .../{id}/onboarding-auto-config/reset` | **仅当 stage 已 deferred** 时替换「刷新结果」按钮 | 该分支必须保留 |
| 刷新结果 | 重拉 workflow | 非 deferred 时显示 | 留在当前子步 |
| 打开飞书后台 | 外链 `consoleLinks.auth` | 新窗口 | 保留 |

子步 C · 菜单（stage: `menu`）

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 菜单确认 | `POST .../{id}/onboarding-menu/confirm` | stage summary notice + 「打开飞书后台」外链（`consoleLinks.bot`）+ allowedActions 含 `confirm` 时显示「我已完成菜单确认」 | 完成后出现「继续」 |

#### 幕 3 · 本机集成（两张可选卡）

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 自动运行 | `POST /api/setup/autostart/apply`；`POST /api/setup/onboarding/machine-decisions/autostart` body `{decision}` | 按 allowedActions 显示：`apply`→「启用自动启动」；`record_enabled`→「保持当前状态并继续」（decision=`enabled`）；`defer`→「稍后处理」（decision=`deferred`）；不支持时显示 not_applicable 摘要 | warning 与 lingerHint 两条额外 notice 保留；检测字段（路径/manager）不上屏 |
| VS Code 集成 | `POST /api/setup/vscode/apply`（10s 超时）；`POST /api/setup/onboarding/machine-decisions/vscode` body `{decision}` | `apply`→「完成当前机器集成」（body `mode=managed_shim` + `bundleEntrypoint`）；`record_managed_shim`→「保持当前状态并继续」；`remote_only`→「留到 SSH 目标机处理」；`defer`→「稍后处理」 | **超时恢复链路必须保留**：apply 超时/失败后先重拉 workflow，若已 ready 视为成功；`request_timeout` 显示 warn「集成请求返回超时，当前还不能确认已完成」 |

#### 完成（stage: `done`）

- 仅当 `canComplete` 可达。展示「欢迎，设置已经完成」+ 主 CTA「进入管理页面」→ `POST /api/setup/complete` → 跳转返回的 `adminURL`。
- **失败兜底保留**：接口失败也跳转 `bootstrap.admin.url` 或 `/admin/`。

### 5.5 Setup 行为保全清单（逐条验收，丢一条即不合格）

1. 进入连接子步自动建扫码会话；ready 后自动 complete
2. 扫码轮询间隔取服务端 `pollIntervalSeconds` 与 5 秒的较大者；busy 时暂停
3. 环境重新检查变好后自动晋级并提示
4. deferred 态下「刷新结果」替换为「重新检查自动配置」（reset）
5. 发布确认对话框；切 app 自动关闭
6. readOnly 应用表单全锁、只验证不保存
7. `gateway_apply_failed` 走 warn 恢复而非报错
8. VS Code apply 超时后重拉确认是否已成功
9. 完成接口失败也兜底跳转 admin
10. 步骤切换滚回页面顶部
11. 认证门槛完全在服务端，前端不做 token 输入 UI；401 落到整页加载失败态 + 「重新加载」

## 6. Admin 页规范

### 6.1 页面合同

- 最终用户：已装好 daemon、偶尔回来维护的机主。
- 本页服务三件事：还正常吗（总览）、改点什么（机器人 / 对话后端）、清理一下（系统）。
- 允许出现的信息类型：各资源的状态与关键摘要、当前可执行操作、操作结果、需要处理的待办。
- 禁止出现的信息类型：内部状态名与名词（见 4.5）、etag/revision、存储根目录等文件系统路径、`rootToken`/`rootURL`、未转写的原始错误、auto-config 的 current/target/diff/publish 原始明细（observed scopes、版本号等——后端返回但禁止上屏）。

### 6.2 信息架构：四区控制台

一级导航四项：**总览 / 机器人 / 对话后端 / 系统**。桌面左侧固定导航 + 品牌区；移动端底部 tab。页面标题 = `产品名 版本 管理`（写 `document.title`）。

### 6.3 区 1 · 总览（默认落点，新增）

聚合已有状态，**不发明新数据**：

- 顶部状态摘要行：机器人 n 个（m 个连接正常）、存储占用合计、系统集成两项状态点。
- **「需要处理」聚合列表**：全系统扫一遍，把以下待办聚到一处，每条一个跳转入口到对应区：
  - 某机器人 auto-config 待补齐 / 待发布 / 待审核 / 同步中（`runtimeApply.pending`）
  - VS Code 需要修复（`vscodeIsReady` 为 false，判定逻辑原样迁移 `web/src/routes/shared/helpers.ts`）
  - 自动运行支持但未启用
  - 存储任一项可清理（文件数 > 0）
- 列表为空时显示「一切正常」空态。
- 运行时实况（实例/会话/轮次）：第 9.2 章已否决，**不实现**。

### 6.4 区 2 · 机器人

布局：列表 + 详情。「添加机器人」是列表顶部按钮，进入子视图（不再是现有那种伪装成列表项的 `"new"` 伪条目）。

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 列表 | `GET /api/admin/feishu/apps` | 每项：名称（缺省「未命名机器人」）、状态标签（九态映射同 Setup 子步 B，原样迁移） | 选中项高亮；选中即自动拉取该 app 的 auto-config plan |
| 添加 · 扫码 | `POST /api/admin/feishu/onboarding/sessions` → 轮询 → `POST .../{id}/complete` | 同 Setup 扫码状态机（自动创建、轮询、ready 自动 complete、重新扫码/重新验证分支） | complete 成功后整区刷新并选中新 app |
| 添加 · 手动 | `POST /api/admin/feishu/apps` → `POST .../{id}/verify` | 表单三字段 + 「连接并验证」；验证失败但已保存的提示保留 | `gateway_apply_failed` warn 恢复保留 |
| 详情 · 状态 | （app.status / runtimeApply） | 连接（正常/已停用/需要处理/待确认）、启用状态（只读）、最近验证时间；`runtimeApply.pending` 时 warn 横幅并禁用 auto-config 按钮 | 文案映射原样迁移 |
| 详情 · 自动配置 | `GET .../auto-config/plan`、`POST .../apply`、`POST .../publish` | 同 Setup 子步 B 的展示与按钮（apply_required→「自动补齐配置」；publish_required→「提交发布」+ 确认对话框；任意状态→「重新检查」）；plan 加载失败特判 `feishu_app_runtime_unavailable` | 确认对话框、错误 details 直出策略保留 |
| 详情 · 连接信息 | — | App ID、飞书后台外链（`consoleLinks.auth` / `consoleLinks.bot`） | 折叠进详情块，不平铺 |
| 详情 · 权限检查 | 新端点，见第 9.1 章 | 「检查权限」按钮 → 卡片内「检查中…」→ 三态结果：① 全部就绪（成功色一行「权限已就绪」）；② 有缺失（缺失 scope 逐条列出 + 「复制导入 JSON」按钮 + 指引一行「到飞书后台导入后，回到这里重新检查」）；③ 检查失败（toast + 卡片内可重试） | 缺失清单与导入 JSON 原样来自后端响应，前端禁止自行推断或拼接权限内容；scope 原文（如 `im:message.group_msg`）允许在此区块显示——它与飞书开放平台后台用词一致，属用户可操作信息，是 4.5 的登记例外；复制成功走 toast |
| 详情 · 危险区 | `DELETE /api/admin/feishu/apps/{id}` | 删除按钮 + 确认对话框（显示机器人名、「此操作不可恢复」）；`readOnly` 禁用并说明「当前机器人由运行环境提供，不能在这里删除」 | 保留 |

### 6.5 区 3 · 对话后端

两个子区 tab：**Claude** / **Codex**。现有全部约束原样迁移，本节表格即验收标准。

**Claude**（API：`GET/POST /api/admin/claude/profiles`、`PUT/DELETE .../{id}`、`PUT .../{id}/context-preference`（带 `If-Match: <contextPreference.etag>`））

- 列表卡片：名称（builtIn 显示「默认」）、摘要（builtIn「本机默认配置 [· 1M]」；自定义 baseURL 或「自定义连接配置」）。
- 编辑表单：名称*、端点地址、认证 Token（password，编辑留空=不修改）、主模型、轻量模型、推理强度（不设置/low/medium/high/max）、上下文大小（checkbox = `extended_1m` / `default`）。
- **builtIn 模式**：不显示连接表单，hero 卡说明，只能改上下文偏好，提交按钮变「保存上下文偏好」，无删除按钮。
- 保存逻辑：非 builtIn 先 PUT 连接信息，contextMode 变化再单独 PUT context-preference；builtIn 只走后者。
- 删除：builtIn 拦截提示；其余确认对话框 + DELETE。
- 错误文案保留：`profile_preference_revision_required`（页面状态已过期）、`profile_preference_revision_conflict`（已被其他窗口修改）。

**Codex**（API：`GET/POST /api/admin/codex/profiles`、`PUT/DELETE .../{id}`（带 `If-Match: <profile.etag>`）、`GET .../references`、`PUT .../context-preference`）

- kind 三态：`native` 本机默认 / `oauth` ChatGPT 登录 / `api` API。仅 `kind==="api" && editable` 连接字段可编辑。
- 列表摘要：kind 中文标签、不可用状态（六种 statusCode 映射原样迁移）、baseURL、模型、推理、上下文标签（跟随 Codex / 272K / 1M）。
- 编辑表单：名称*、端点地址*、API Key（创建必填/更新留空不改）、主模型*、审阅模型、推理强度*（input+datalist low/medium/high/xhigh，允许任意值）、上下文大小（`codex_default` / `price_guard_272k` / `extended_1m`）。
- 上下文状态 hint 五态（`contextStatusDescription`）原样迁移。
- **不可编辑模式**：表单区替换为 hero 卡（oauth / native 两种文案），提交变「保存上下文偏好」；`contextEditable=false` 时全部禁用。
- 删除保护：`deletable=false` 无删除按钮；点击删除先 `GET references`，确认对话框内列出引用（kind 中文「会话/队列/工作区/使用中」+ 净化后的 name + reason；name 含 `/ \ :` 一律丢弃）。
- 10+ 错误码友好文案映射原样迁移。

### 6.6 区 4 · 系统

| 功能 | API | 交互与反馈 | 验收标准 |
|---|---|---|---|
| 自动运行 | `GET /api/admin/autostart/detect`；`POST /api/admin/autostart/apply` | 状态文案四态（读取失败/不支持/已启用/未启用）；「启用自动运行」仅 `supported && !enabled` 出现，`!canApply` 禁用 | 保留 |
| VS Code 集成 | `GET /api/admin/vscode/detect`；`POST .../apply`；`POST .../reinstall-shim` | 状态三态（已接入/需修复/读取失败）；「重新检查并修复」：`needsShimReinstall` 且有 bundleEntrypoint 走 reinstall-shim，否则 apply（mode=managed_shim） | `vscodeIsReady` 判定原样迁移 |
| 存储维护 ×3 | 状态：`GET /api/admin/storage/image-staging`、`GET .../logs`、`GET .../preview-drive/{appId}`（逐 app 汇总）；清理：对应 `POST .../cleanup` | 每项一张卡：「N 个文件，约 X」+ 清理按钮（「清理旧图片」「清理一天前日志」（body `olderThanHours:24`）、「清理旧预览」（逐 app 并发，部分失败有专门提示）） | preview 无 app 时按钮禁用；`runAdminStorageCleanup` 的 busy 模板保留 |
| 会话信息（新增展示） | （bootstrap `session` 字段） | 只读一行：当前访问方式（本机直连 / 已认证会话 + 过期时间） | 不做登出按钮 |

### 6.7 Admin 行为保全清单（逐条验收）

1. 选中机器人自动拉取 auto-config plan；切换机器人重置发布目标与扫码流程
2. 扫码完整状态机（自动创建 → 轮询 → ready 自动 complete → 成功刷新选中）
3. 全部确认对话框 ×4：删除机器人、提交发布、删 Claude 配置、删 Codex 配置（后者内含 references 预取与"使用中"警告列表）
4. etag/If-Match 乐观锁：冲突文案保留，不发明自动重试
5. 上下文偏好总是独立的第二个请求（带各自 etag）
6. 错误 code → 中文文案的全部映射表（Codex 10+、Claude 2、auto-config details 直出）
7. preview-drive 逐 app 并发清理与部分失败提示
8. 操作反馈走 4.4 的五个槽位；禁止新增反馈位置
9. 非 JSON 响应 / 401 → 整页加载失败态 + 「重新加载」
10. `credentials: "same-origin"` 与 `lib/api.ts` 封装不被绕过
11. 权限检查三态（就绪/缺失/失败）与「复制导入 JSON」反馈符合 6.4 表；缺失清单与 JSON 不经过前端加工

## 7. 刷新策略（行为改进，非新功能）

现状是任何操作后整页重拉 8+ 请求、丢失选中态与滚动位置。重设计后：

- 列表数据变化的操作（增删改）→ 局部重拉该区域数据 + toast。
- 扫码 complete 成功、auto-config apply/publish 等会改变多区状态的操作 → 可以整区重拉，但必须保留当前选中项与所在导航区。
- 禁止引入全局 loading 遮罩打断阅读；加载态只出现在数据所在的卡片内。

## 8. 落地顺序（强制）

按 `docs/general/page-mock-guidelines.md` 执行：

1. **先做浏览器可运行的高保真 mock**（假数据、真交互、双端响应式、反馈矩阵全覆盖），顺序：Setup → Admin。mock 放 `docs/draft/`，文件名 kebab-case 含 `mock`。
2. mock 经需求方确认后，再按 mock 改 `web/src`。mock 即最终 UI 合同：除假数据换真实数据、示例反馈换真实后端反馈外，实现阶段**禁止**新增 mock 里没有的用户可见内容、文案或反馈区块。
3. 现有测试（`web/src/**/*.test.ts(x)`、`web/e2e`）必须适配并通过；新增交互补对应测试。

## 9. 可选增强（需求方已拍板，结论如下）

### 9.1 已批准：机器人权限检查（唯一纳入项）

- 内容：「机器人」详情页新增「权限检查」区块，交互与验收标准见 6.4 表。用户价值：机器人缺权限时给出缺失清单和可一键导入飞书开放平台的授权 JSON，用户导入后重新检查。
- **含后端工作——这是第 10 章第 7 条的唯一例外**：旧端点 `GET /api/admin/feishu/apps/{id}/permission-check`、`POST .../test-events`、`POST .../test-callback` 已被删除，且有测试 `TestAdminLegacyFeishuInstallTestRoutesAreRemoved`（`internal/app/daemon/admin_feishu_test.go:303`）守备这些路径不得复活。实现新端点时要么启用新路径，要么在同一改动中说明理由并同步更新该测试；禁止静默绕开。
- 新端点路径固定为 `GET /api/admin/feishu/apps/{id}/permissions/check`。响应字段：`app`、`ready`、`missingScopes[]`（`scope` / `scopeType`）、`grantJSON`、`lastCheckedAt`。`missingScopes` 与 `grantJSON` 均由后端根据 `feishuapp.DefaultManifest()` 和飞书已授权 scope 计算，前端不得维护第二份权限清单。
- 后端内部已有权限检查逻辑可参考复用：`PrimaryBotPermissionChecker`（`internal/core/orchestrator/service.go:41`，挂载于 `internal/app/daemon/app.go:251`）。当前 Admin 权限检查面向完整 manifest scope 状态，不等同于主机器人群消息权限检查；两者都必须继续以 manifest / 飞书授权结果为底层事实源，不能在前端分叉规则。
- 前端 `web/src/lib/types.ts` 复用 `FeishuAppPermissionCheckResponse` 作为该端点响应类型；已否决收发测试对应的 `FeishuAppTestStartResponse` 不得恢复。
- 落地顺序（对第 8 章的补充）：先定后端端点契约并同步本章 → Admin mock 补该区块并经需求方确认 → 前端实现。

### 9.2 已否决（实现者不得重新引入）

1. **机器人运维操作**（编辑 `PUT /api/admin/feishu/apps/{id}`、启停 `POST .../enable|disable`、重连 `POST .../reconnect`、同步重试 `POST .../retry-apply`）：否决。本产品面向非技术用户，这些操作没有对应的用户任务。后端端点保留，前端不接。
2. **收发测试**：否决。系统未记录可用于发送测试消息的单聊会话，功能无法成立。
3. **运行时实况**（`GET /api/admin/runtime-status`）：否决。机器人连接状态已在「机器人」区呈现；其余字段（实例/会话绑定/进行中与排队轮次）是内部概念，对非技术用户无可行动意义。总览区不实现。

## 10. 禁止事项汇总（防私货清单）

实现者交付物中如出现以下任何一项，视为不合格：

1. 本文未列出的新功能、新页面、新导航项、新按钮（第 9 章除外且需批准）
2. 本文未列出的用户可见文案、说明段落、免责声明、"设计说明"、空态以外的引导文
3. 第 4.4 节五个反馈槽位之外的新反馈形式（弹窗报错、行内红字、横幅新增位置等）
4. 内部名词、错误 code 原文、文件路径、协议字段名出现在用户可见区域
5. 绕过 `lib/api.ts` 的裸 `fetch`、新增的本地持久化（localStorage 等，现有代码就没有）
6. 登录/登出/token 输入等服务端已有机制之外的前端鉴权 UI
7. 后端 Go 代码的任何改动（唯一例外：第 9.1 章批准的权限检查端点，且须遵守该章对旧路由守备测试的要求）
8. 对现有交互流程语义的"优化"：如把确认对话框改成直接执行、把自动轮询改成手动刷新、把 optimistic 改成 blocking 等——流程语义以后端状态机为准，前端只改呈现
9. 引入新的运行时依赖（组件库、CSS 框架、图标库），除非 PR 中单独论证并获批准
10. 样式自由发挥：使用 4.6 之外的色值、4.7 之外的字号级别、4.8 之外的间距/圆角/阴影，或绕过 4.10 的组件规格自行发明组件外观（基线不够用时按 4.13 先修规范）

## 11. 交付验收清单

1. 第 5.5、6.7 两章行为保全清单逐条通过
2. `docs/general/web-design-guidelines.md` 第 7 章落地检查表 8 问全部回答"是"
3. 若交付物含 mock：`page-mock-guidelines.md` 第 10 章检查表 14 问全部回答"是"
4. 桌面 + 移动 + 横竖屏切换下，Setup 全流程与 Admin 四区主任务均可完成
5. `web` 下现有测试与 e2e 全绿；`make` 相关检查（含 `scripts/check/go-file-length.sh` 若涉及 Go）通过
6. PR 描述包含：行为保全清单逐条核对结果、与本文的任何偏差及理由

## 12. 附：术语与文案对照（UI 允许 / 禁止）

| 后端概念 | UI 允许说法 | 禁止说法 |
|---|---|---|
| gateway 连接状态 | 连接：正常 / 已停用 / 需要处理 / 待确认 | gateway、surface |
| auto-config plan | 自动配置：已自动完成 / 有降级 / 待补齐 / 待发布 / 待审核 / 受阻 / 同步中 | plan、manifest、reconcile |
| profile（Claude/Codex） | 配置 / 连接配置 / 对话后端 | profile catalog、etag、revision |
| context preference | 上下文大小：默认 / 272K（费用优先）/ 1M | context_preference、clamped |
| onboarding session | 扫码 / 扫码会话 | session id、token |
| storage cleanup | 清理旧预览 / 清理旧图片 / 清理一天前日志 | rootDir、staging |

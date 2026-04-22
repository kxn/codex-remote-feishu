# Backend-Aware 命令 Catalog 设计

> Type: `draft`
> Updated: `2026-04-22`
> Summary: 将当前单一 `feishuCommandSpecs` 演进为 family/variant/context resolver，支持 Codex / Claude / VS Code 并存下的同名异实现、同名异 UI 和 backend 独有命令。

## 1. 文档定位

这份文档回答的是一个具体架构问题：

1. 现在的菜单 / 命令 catalog 是否足够支撑 `codex normal`、`claude normal`、`vscode` 三套产品语义并存。
2. 如果不够，应该把“共享命令”、“同名异实现”、“同名异 UI”、“独有命令”分别落在哪一层。
3. 后续如果再接更多 backend，怎样避免 catalog 继续堆成 mode / backend 的条件分支泥团。

本文是设计草案，不代表当前代码已实现行为。

相关基线文档：

- `docs/general/feishu-menu-card-usage-guidelines.md`
- `docs/general/feishu-card-interaction-model.md`
- `docs/general/feishu-card-api-constraints.md`
- `docs/inprogress/claude-normal-mode-poc-design.md`

## 2. 背景

当前菜单系统已经支持：

- `normal`
- `vscode`

并且在产品上已经存在显著差异：

- `normal` 下，“工作会话”分组会收口成 workspace 语义
- `vscode` 下，“工作会话”分组会保留 `/list`、`/use`、`/useall`、`/follow`

这套差异目前能跑，但它主要是靠一组全局命令定义，再按 `productMode` 和少量 `menuStage` 条件投影出来的。

如果后续再加：

- `claude normal`

问题会立刻升级为三类，而不是简单的“多一个 mode”：

1. **同名命令、同一套 UI，但底层 dispatch / translator 不同**
   - 例如 `/stop`、`/status`
2. **同名命令、用户心智相同，但 UI 和流程不同**
   - 例如 `/model`、`/reasoning`、`/list`
3. **backend 独有命令**
   - 例如某些只属于 Claude、只属于 Codex、或只属于 VS Code 的功能

如果继续沿用“全局命令表 + 条件过滤 + 少量特例页”的方式，catalog 会越来越难维持 display / parse / execute 一致性。

## 3. 当前实现的结构问题

### 3.1 display 只有 `productMode`，没有 `backend`

当前 help/menu 展示的主入口仍然是一套全局定义，真正的差异主要在 `FeishuCommandDefinitionForDisplay(...)` 里硬编码过滤：

- `normal` 隐藏 `/list`、`/use`、`/useall`、`/follow`
- `vscode` 隐藏 workspace 家族

问题不是这段代码是否工作，而是它只能表达“显示或隐藏”，不能表达：

- 同一 family 在不同 backend 下展示相同标题但不同说明
- 同一 family 在不同 backend 下展示不同默认按钮
- 同一 family 在不同 backend 下进入不同 page / owner flow

### 3.2 parse 是全局无上下文 parser

当前 `ParseFeishuTextAction(...)` 和 `ParseFeishuMenuAction(...)` 只扫一套全局 `feishuCommandSpecs`。

它们不知道：

- 当前 surface backend 是什么
- 当前 mode 是什么
- 当前 attached instance 的 capability 是什么

这意味着 parser 本身并没有正式承载“同名命令在不同 backend 下解析为不同语义”。

### 3.3 config page / command page 仍按 `CommandID` 直接分叉

像 `/mode`、`/model`、`/reasoning` 这类配置页，目前还是在 orchestrator 里按 `ActionKind` / `CommandID` switch 到具体页面 builder。

这能支撑少量固定命令，但不能优雅承载：

- `model.codex.normal`
- `model.claude.normal`
- `list.normal.codex`
- `list.normal.claude`
- `list.vscode`

这些“用户看起来同名，但 flow 不一定相同”的变种。

### 3.4 状态层没有 backend 维度

当前状态层里：

- `ProductMode` 只有 `normal` / `vscode`
- `WorkspaceDefaults` 只有 `workspace -> config`
- `InstanceRecord` 只有 `Source`，没有 `Backend`

这会导致：

1. 无法把 `codex normal` 与 `claude normal` 正式区分为不同上下文。
2. `/model`、`/reasoning`、`/access` 这类 workspace 默认配置天然会串 backend。
3. catalog 只能从 `ProductMode` 推断视图，而不能从 `backend + mode + capability` 推断视图。

### 3.5 系统已经存在“专属 UI 页”先例，但没有 catalogue-level 抽象

`/upgrade`、`/cron` 已经证明当前系统并不是只能做静态 catalog entry。

它已经支持：

- 同一个 command family 下的多级页面
- 独立 breadcrumb
- 独立 back button
- 命令专属 owner card / page

所以问题不是“系统能不能做 variant UI”，而是：

- variant UI 现在没有被 catalog 正式建模
- 它只是零散存在于具体业务实现里

## 4. 设计目标

1. **把 backend 从当前 catalog 决策的一等输入里补出来。**
2. **把“用户心智上的同一命令”和“具体 backend / mode 下的实现变种”拆开。**
3. **让 display、parse、execute 三层都经过同一份上下文解析，不再各自猜。**
4. **允许共享命令复用现有实现，不要求所有命令都立刻重写。**
5. **允许某些命令继续拥有专属 UI / owner flow，但要由 catalog 正式引用，而不是零散特例。**
6. **兼容当前 `Action` / owner-flow 体系，避免第一阶段就推翻整个 command pipeline。**

## 5. 非目标

1. 本设计不重写当前菜单卡三态契约；`keep / enter_owner / enter_terminal` 仍然沿用。
2. 本设计不要求第一阶段就替换全部 `ActionKind`。
3. 本设计不把 daemon / orchestrator / adapter 的全部 provider 抽象一次性做完。
4. 本设计不把 `/cron`、`/upgrade`、`/debug` 这类命令专属页面强行迁回纯静态 catalog。

## 6. 设计原则

### 6.1 backend 与 product mode 必须正交

推荐把“用户可见模式”和“底层 backend”拆成两层：

- `Backend`
  - `codex`
  - `claude`
- `ProductMode`
  - `normal`
  - `vscode`

用户入口可以仍然保留：

- `/mode codex`
- `/mode claude`
- `/mode vscode`

但存储和路由不应再把它们当成同一维度。

建议解释：

- `/mode codex` = `Backend=codex` + `ProductMode=normal`
- `/mode claude` = `Backend=claude` + `ProductMode=normal`
- `/mode vscode` = `Backend=codex` + `ProductMode=vscode`
- `/mode normal` 继续兼容为 `/mode codex`

### 6.2 family 是用户心智，variant 是实现槽位

同一命令应先区分“用户觉得自己在用哪个命令”，再区分“系统当前实际挑中了哪个变种”。

例如：

- `model`
  - `model.codex.normal`
  - `model.claude.normal`
- `list`
  - `list.codex.normal`
  - `list.claude.normal`
  - `list.codex.vscode`

family 稳定，variant 可增长。

### 6.3 display、parse、execute 必须共用同一份上下文

一条命令如果：

- 菜单里显示 A
- 点下去 parse 成 B
- 最后执行到 C

那 catalog 设计就是坏的。

后续应确保：

- help/menu 展示什么
- slash / callback 解析成什么
- 最后由谁执行

都由同一个 `CatalogContext` 决定。

### 6.4 共享命令优先共享，差异命令再分 variant

不要因为引入 Claude 就把全部命令立刻复制成三套。

优先顺序应是：

1. 完全共享：单 variant 直接复用
2. 同名异 dispatch：共享 UI，拆 execute variant
3. 同名异 UI / flow：拆 page + execute variant
4. 独有命令：只注册在对应 backend / capability 下

### 6.5 业务 owner page 仍由业务模块拥有

像 `/cron`、`/upgrade` 这类业务页不应被 catalog 框架重新吞并成通用 builder。

catalog 只需要做到：

- 能引用它们
- 能在当前 context 下把它们作为某个 variant 的 page 入口

不需要接管其业务内部状态机。

## 7. 目标模型

## 7.1 CatalogContext

后续所有 catalog 决策都基于一个显式上下文对象：

```go
type CatalogContext struct {
    Backend       string
    ProductMode   string
    MenuStage     string
    AttachedKind  string
    WorkspaceKey  string
    InstanceID    string
    Capabilities  CapabilitySet
}
```

最关键的点：

- `Backend` 和 `ProductMode` 同时存在
- `Capabilities` 是一等输入，不只是运行时兜底拒绝
- `MenuStage` 不再承担所有组合语义，只表达菜单内部阶段

这里不建议继续增加：

- `claude_working`
- `codex_working`
- `claude_detached`

这类组合式 stage。组合维度应该通过 `Backend + ProductMode + AttachedKind + Capabilities` 表达。

## 7.2 CommandFamily

`CommandFamily` 表达用户心智上的“同一个命令”：

```go
type CommandFamily struct {
    ID          string
    GroupID     string
    Title       string
    Description string
    Rank        int
    Variants    []CommandVariant
}
```

family 要稳定：

- `mode`
- `model`
- `reasoning`
- `access`
- `list`
- `use`
- `new`
- `follow`
- `status`
- `stop`

这些 family id 才是跨 backend 的稳定产品词汇。

## 7.3 CommandVariant

`CommandVariant` 表达某个 family 在特定上下文下的具体实现：

```go
type CommandVariant struct {
    VariantID string
    FamilyID  string

    MatchesContext func(CatalogContext) bool
    Visible        func(CatalogContext) bool
    Enabled        func(CatalogContext) (bool, string)

    BuildEntry      func(CatalogContext) CommandCatalogEntry
    ParseText       func(CatalogContext, string) (ResolvedCommand, bool)
    ParseMenuAction func(CatalogContext, string) (ResolvedCommand, bool)
}
```

variant 可以共享 slash 文本，也可以共享 family 标题，但它必须有独立 `VariantID`。

## 7.4 ResolvedCommand

解析结果不应只剩一个 `ActionKind`，而应携带 catalog provenance：

```go
type ResolvedCommand struct {
    FamilyID   string
    VariantID  string
    Backend    string
    Action     Action
}
```

第一阶段不强制替换现有 `Action`，但建议给 `Action` 增加可选元数据：

- `CatalogFamilyID`
- `CatalogVariantID`
- `BackendHint`

这样后续：

- page builder
- owner flow
- dispatch adapter

都能知道这条动作最初是从哪个 variant 解析出来的。

## 7.5 Registry 与 Resolver

整体流程建议改成：

1. registry 持有所有 `CommandFamily`
2. resolver 根据 `CatalogContext` 选出当前可见 / 可解析的 variant
3. display / parse / execute 都走 resolver

### display

- 先按 group / rank 列 family
- 再在每个 family 内选当前上下文最合适的 variant
- family 不可见则整项不显示
- family 可见但当前 disabled，则允许显示 disabled 理由

### parse

- slash 文本先定位 family，再由当前 context 下可匹配的 variant 解析
- callback payload 优先走 `family + variant` 显式定位，而不是只靠 command text

### execute

- 优先读取 `ResolvedCommand.VariantID`
- 若当前实现尚未迁移，则回退到 `ActionKind`

## 7.6 菜单 / 帮助页投影规则

### family 级入口

菜单页和帮助页默认按 family 展示，而不是按 variant 展示。

原因：

- 用户不该先理解 “codex list” 和 “claude list” 才能找命令
- 用户应先看到 `查看会话` 这类稳定入口，再由 context 决定具体变种

### variant 级说明

同一个 family 在不同 variant 下可以改变：

- 说明文案
- 默认按钮
- 示例
- 表单 placeholder
- disabled reason

例如 `model`：

- `codex normal` 可以展示模型列表 + reasoning 补充说明
- `claude normal` 可以展示 Claude 支持的 model / mode 选项

### backend 独有命令

独有命令可直接注册为仅在特定 backend / capability 下可见的 family。

例如：

- `vscode-migrate`
- 将来可能存在的 `claude-session` 专属命令

## 7.7 配置页与 owner page 的变种承载

当前 `FeishuCatalogConfigView` 只带 `CommandID`，这对 variant 不够。

建议扩成：

```go
type FeishuCatalogConfigView struct {
    FamilyID    string
    VariantID   string
    CommandID   string // 兼容旧链路，逐步降级
    ...
}
```

后续页面构建改成：

- 优先按 `VariantID` 找 page builder
- 没有 variant builder 时，再回退旧 `CommandID`

这样可以承载两类变化：

1. **同名、同 UI、仅 dispatch 不同**
   - 共用 page builder，只换 execute
2. **同名、UI / flow 不同**
   - family 相同，但 page builder / owner flow 不同

## 7.8 callback payload 与旧卡一致性

如果后续同一个 family 在不同 backend 下页面结构不同，callback 不能只带：

- `action_kind`
- `command_text`

否则旧卡在 mode / backend 切换后可能被错误解释。

建议 callback payload 增加：

- `catalog_family_id`
- `catalog_variant_id`
- `catalog_backend`

旧卡点击时，如果 payload 上的 variant 与当前 context 明显不兼容，应显式拒绝并提示重新打开当前 backend 的命令页。

## 8. 三类核心场景的落地方式

## 8.1 同名、同体验、仅 translator / dispatch 不同

例子：

- `/stop`
- `/status`

落地方式：

- 保持同一个 family
- 可共用同一个 page / entry builder
- 仅在 execute / dispatch adapter 层按 backend 分 variant

这类命令不需要用户显式感知差异。

## 8.2 同名、用户心智相同，但 UI / 流程不同

例子：

- `/model`
- `/reasoning`
- `/list`
- `/use`

落地方式：

- 仍保持同一个 family
- variant 拥有各自 page builder、form shape、owner flow
- family 标题保持稳定，variant 文案说明差异

这类命令应允许：

- 同名
- 同 group
- 同 rank
- 不同 UI

## 8.3 backend 独有命令

例子：

- `/follow` 当前只属于 `vscode`
- 将来可能只属于 Claude 的 session / mode 命令

落地方式：

- 直接注册为特定 context 才可见的 family
- 其他 context 不显示，或以 disabled + capability 说明呈现

## 9. 数据模型调整

## 9.1 state 层

建议新增：

- `state.InstanceRecord.Backend`
- `state.SurfaceConsoleRecord.Backend`
- `SurfaceResumeEntry.ResumeBackend`

建议升级：

- `WorkspaceDefaults[workspaceKey] -> map[backend]BackendConfigRecord`

兼容规则：

- 缺失 backend 的旧数据默认按 `codex`

## 9.2 capability 层

catalog 不应只看 backend 名称，还要看 capability。

最少需要纳入：

- `threads_refresh`
- `turn_steer`
- `request_respond`
- `session_catalog`
- `resume_by_thread_id`
- `requires_cwd_for_resume`
- `supports_vscode_mode`

display 阶段就应该能决定：

- 完全隐藏
- 可见但 disabled
- 正常可用

而不是都拖到 execute 阶段才返回 `command_rejected`。

## 10. 推荐包边界

推荐分层如下：

- `internal/core/control`
  - 定义 `CatalogContext`、`CommandFamily`、`CommandVariant`、resolver 接口
  - 保存共享 family 定义与 display contract
- `internal/core/orchestrator`
  - 从 surface / instance 状态生成 `CatalogContext`
  - 处理 variant 到 `Action` / page / owner-flow 的绑定
- `internal/app/daemon`
  - 继续拥有 `/cron`、`/upgrade`、`/debug` 等业务命令页与 daemon command flow

不要做的事：

- 不要把全部 backend 逻辑继续塞进 `codex` translator
- 不要把所有 variant page builder 都塞回 `control` 成为一个超大 switch

## 11. 迁移顺序

### 阶段 1：补 backend 与 context 基座

1. 状态层增加 `Backend`
2. `WorkspaceDefaults` 改为 backend-aware
3. 补 `CatalogContext`
4. 保持现有 `feishuCommandSpecs` 不动

### 阶段 2：让 display 先走 resolver

1. 新 registry 包装现有 family / group
2. 先迁 help/menu 展示，不改 execute
3. 把当前 `productMode` 硬编码过滤改为 variant 选择

### 阶段 3：让 parse 走 resolver

1. slash parse 改成 context-aware
2. callback payload 增加 family / variant 元数据
3. 保持解析结果仍可回落到现有 `ActionKind`

### 阶段 4：迁移高风险 family

优先迁这些最可能在 Claude 接入时分叉的 family：

1. `mode`
2. `model`
3. `reasoning`
4. `list`
5. `use`
6. `new`
7. `follow`

### 阶段 5：新增 Claude variants

在 resolver 已稳定后，再引入：

- `*.claude.normal`

而不是先在现有全局 specs 里加第三套分支。

## 12. 风险与取舍

### 12.1 这是结构升级，不只是文案抽象

如果只改命令定义字段，不补：

- backend state
- context-aware parse
- variant provenance

那最后还是会退回多处条件分支。

### 12.2 family 不是万能共享层

有些命令即使同名，也可能只保留同一个 family，不保留同一个 canonical slash 语法说明。

因此 family 共享的是用户入口，不是所有参数语法都必须完全一致。

### 12.3 需要接受短期双轨

迁移期内会同时存在：

- 旧 `feishuCommandSpecs`
- 新 family / variant registry

这是可接受的过渡成本，但必须有明确迁移边界，不能长期并存。

## 13. 结论

当前菜单系统离“多 backend 变种 catalog”只差一步抽象，但这一步不能再只靠加字段或继续堆 `productMode` 分支。

推荐方向是：

1. 先把 `Backend` 从 `ProductMode` 里拆出来
2. 再把 catalog 从“单一命令表”改成“family + variant + context resolver”
3. 让 display、parse、execute 三层共享同一份上下文判断

这样才能同时支撑：

- 同名、同体验、仅 translator 不同
- 同名、不同 UI / 流程
- backend 独有命令

而不把 Claude 接入变成下一轮更大的菜单分支泥团。

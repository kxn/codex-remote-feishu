# 飞书菜单卡使用规约

> Type: `general`
> Updated: `2026-11-22`
> Summary: 明确持久团队菜单的页面、归属和回调规则，限定旧菜单契约的适用范围。

## 1. 持久团队菜单

菜单为用户提供工作导航。进入业务后，所选工作或业务卡承载进展、结果和可恢复的失败。按钮和反馈使用[业务卡原则](./feishu-business-card-interaction-principles.md)及[卡片交互模型](./feishu-card-interaction-model.md)中的通用呈现原则。

当前私聊菜单与业务卡使用持久 UIView、交互记录和共用 outbox。菜单按钮只引用已登记的视图及业务目标，用户、Bot 身份、来源消息和业务版本在实际回调中核对。菜单导航和分页仅在已确认送达且目的地及归属相同时原位更新；Slash 和主动通知发新消息。业务进展遵循对应视图的恢复规则。新增入口时核对按钮是否可达、来源是否可验证、重触发是否安全，以及旧卡能否给出重新进入的说明。完整规则见[卡片 UI 状态机的团队章节](./feishu-card-ui-state-machine.md#0-持久团队界面本分支的新入口)。

私聊首页使用“基本命令、参数设置、工作区与会话、常用工具、系统管理”五组。进入设置、选择／保存模型、选择工作目录、翻页、只读历史和返回都使用同一个 `private-menu:<personal-scope>` 展示归属；当前工作及业务目标另行冻结，不由这个展示 key 决定授权。停止、原生问题、决定和正式结果不合并成同一菜单状态。

私聊菜单采用旧版 compact 导航规则：面包屑与返回位于卡片顶部，各选项使用铺满可用宽度的等宽按钮，刷新、分页与常用操作留在底部。首页只放五个分组。标题注明真实对应的 Slash Command；没有独立命令的页面不伪造对应关系。参数概览在对应选项中同时显示当前值及命令，区分明确选择、固定 Profile 值与尚未观察到的默认值，不能用虚构默认模型填空。这套布局只用于私人导航，不改变群业务卡。

当前状态页保留旧版仍适用的信息：会话起点与最近输入、执行与待答复状态、队列、所选及实际执行目录、Git 分支和改动、当前进程／执行器信息、下次执行配置、自动检查与自动继续，以及本 Bot 已观察到的权限缺口。数据来自当前私人上下文及真实配置，不拼接已停用的 surface 状态，也不混入另一会话或另一个 Bot 的信息。目录探测有时限，配置读取失败时保留可读的执行事实和具体失败原因。

旧版 `/mcpoauth` 是帮助中的显式命令，原先不在工具菜单中；新版不新增“MCP 服务与登录”菜单项，保留对应显式命令。系统管理子页必须保留网络切换、自启启停和实际支持的升级操作，不能把原先可设置的页面降成只读摘要。自启入口不按 Linux／macOS 硬编码隐藏，具体操作由真实安装能力决定，包括受支持的 Windows 安装。

设置保留当前候选的下拉控件，适用命令按实际后端与工作状态显示。普通首页不提供“暂停／继续”；只有已证明是旧版私聊停止遗留的 gate 才显示明确的恢复操作。工作目录和模型由人精确选择，日常 worktree 管理由 Agent 完成。旧应用菜单的 Stop／New key 直接进入对应私聊操作，不用泛泛的帮助文案代替执行。

同卡更新依赖真实点击来源和已确认消息。旧卡／转发卡不执行动作；未知 PATCH 不重放，重新打开菜单得到新的有效入口。菜单更新不会覆盖独立评审结果或业务决定。具体 SDK 连续导航证据见 [交互整改记录](../inprogress/feishu-interaction-repair.md)。

第 2 至 7 节保留旧 surface 实现及评审清单，适用于维护仍使用旧栈的路径与迁移核对。新 UIView 使用本节和状态机中的团队规则。

## 2. 旧 surface：菜单卡三态契约

菜单相关动作必须落在三种前台处置之一，来源是 `ResolveFeishuFrontstageActionContract(...)`：

- `keep`: 仍在菜单体系内导航，只允许菜单同卡切换
- `enter_owner`: 离开菜单，进入具体业务 owner card
- `enter_terminal`: 菜单立即收口为终态结果，不再保留菜单活跃态

实现上：

- action 级入口契约由 `internal/core/control/feishu_ui_lifecycle.go` 统一定义
- 菜单运行态由 `command_menu` owner flow 承载，禁止新增平行“菜单状态容器”
- 菜单 flow role 只允许 `launcher -> owner` 单向切换，不允许回跳成 launcher
- 菜单层 `返回上一层` / `返回菜单` / 进入子页默认必须投影为 `page_local_action` 或 `page_local_submit`
- `page_action` / `page_submit` 只保留给真正需要继续走命令 provenance 的动作，不再拿来表达纯前台导航

## 3. 旧 surface：菜单卡与业务卡边界

菜单卡只负责分流，不承载业务执行状态。

具体要求：

- 菜单内可返回：仅限菜单层级（首页/分组/配置页）
- 菜单层返回/回根/进入子页：默认是当前卡内的本地导航，不重放 slash command
- 进入业务后不可返回菜单：业务未结束前，所有进度、成功、失败、取消都在业务 owner card 内收口
- `help/status` 归类为 terminal：结果即完成，不再视为可继续返回的 preview

## 4. 旧 surface：单卡推进与换卡规则

菜单来源 callback 想 replace 当前卡，必须同时满足：

- 来源卡带当前 `daemon_lifecycle_id`
- 命中 frontstage action contract 的可替换模式
- 事件流存在可投影卡（`inline_view` 或 `first_result_card`）

以下行为保持禁止：

- 恢复 `submission anchor` 回退路径
- 恢复 bare command continuation 作为菜单过渡
- 把“仅 blocker notice（如 `path_picker_active` / `target_picker_processing`）”替成新卡

## 5. 旧 surface：明确禁止项

后续实现中，以下做法视为违规：

- 重新引入旧 `Surface.MenuFlow` 或同类隐藏状态基座
- 在多个模块分散维护“菜单是否继续活跃”的独立判定
- 菜单卡同时承接业务编辑/执行态（半菜单半业务）
- 新增菜单功能却只改文案或按钮，不补契约分流与测试

## 6. 旧 surface：新增菜单项的必做清单

新增或改造菜单项时，必须按顺序完成：

1. 在 `ResolveFeishuFrontstageActionContract(...)` 明确动作归类（`keep/enter_owner/enter_terminal`）。
2. 在菜单视图层接入入口，并确认 breadcrumb / 返回行为仅发生在菜单层级；`keep` 类动作默认走本地 callback substrate，而不是命令重放。
3. 如果进入业务，确保 owner flow 全程单卡推进，终态 sealed，且不回跳菜单。
4. 为新增路径补回归：
   - `internal/core/control/inline_replacement_test.go`
   - `internal/app/daemon/app_menu_handoff_test.go`
   - 必要时补对应业务 owner card 测试（例如 workspace/picker/history 等）

未同时满足以上四项，不应合并。

## 7. 旧 surface：变更评审最小核对项

每次菜单改动至少核对：

- 是否只通过统一 action contract 决定 launcher disposition
- 是否把本地菜单导航留在 `page_local_action/page_local_submit`，而不是退回命令 provenance
- 是否仍不存在旧链路（submission anchor / bare continuation / MenuFlow substrate）
- 是否新增了“菜单与业务混住”的路径
- 是否覆盖菜单回退、业务收口、旧卡点击拒绝三类用例

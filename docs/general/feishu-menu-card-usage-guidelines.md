# 飞书菜单卡使用规约

> Type: `general`
> Updated: `2026-04-22`
> Summary: 固化菜单卡的 launcher/owner/terminal 契约与扩展清单，禁止回退到旧菜单基座或半菜单半业务混合实现。

## 1. 文档定位

本文是“菜单卡如何实现与扩展”的工程规约，约束以下内容：

- 菜单卡与业务卡的边界
- 菜单动作进入业务时的 handoff 语义
- 何时允许同卡 replace，何时必须 append-only
- 新增菜单项时必须同步修改和补测的位置

产品原则与交互模型仍以以下文档为准：

- `docs/general/feishu-business-card-interaction-principles.md`
- `docs/general/feishu-card-interaction-model.md`
- `docs/general/feishu-card-ui-state-machine.md`

## 2. 菜单卡三态契约

菜单相关动作必须落在三种前台处置之一，来源是 `ResolveFeishuFrontstageActionContract(...)`：

- `keep`: 仍在菜单体系内导航，只允许菜单同卡切换
- `enter_owner`: 离开菜单，进入具体业务 owner card
- `enter_terminal`: 菜单立即收口为终态结果，不再保留菜单活跃态

实现上：

- action 级入口契约由 `internal/core/control/feishu_ui_lifecycle.go` 统一定义
- 菜单运行态由 `command_menu` owner flow 承载，禁止新增平行“菜单状态容器”
- 菜单 flow role 只允许 `launcher -> owner` 单向切换，不允许回跳成 launcher

## 3. 菜单卡与业务卡边界

菜单卡只负责分流，不承载业务执行状态。

具体要求：

- 菜单内可返回：仅限菜单层级（首页/分组/配置页）
- 进入业务后不可返回菜单：业务未结束前，所有进度、成功、失败、取消都在业务 owner card 内收口
- `help/status` 归类为 terminal：结果即完成，不再视为可继续返回的 preview

## 4. 单卡推进与换卡规则

菜单来源 callback 想 replace 当前卡，必须同时满足：

- 来源卡带当前 `daemon_lifecycle_id`
- 命中 frontstage action contract 的可替换模式
- 事件流存在可投影卡（`inline_view` 或 `first_result_card`）

以下行为保持禁止：

- 恢复 `submission anchor` 回退路径
- 恢复 bare command continuation 作为菜单过渡
- 把“仅 blocker notice（如 `path_picker_active` / `target_picker_processing`）”替成新卡

## 5. 明确禁止项

后续实现中，以下做法视为违规：

- 重新引入旧 `Surface.MenuFlow` 或同类隐藏状态基座
- 在多个模块分散维护“菜单是否继续活跃”的独立判定
- 菜单卡同时承接业务编辑/执行态（半菜单半业务）
- 新增菜单功能却只改文案或按钮，不补契约分流与测试

## 6. 新增菜单项的必做清单

新增或改造菜单项时，必须按顺序完成：

1. 在 `ResolveFeishuFrontstageActionContract(...)` 明确动作归类（`keep/enter_owner/enter_terminal`）。
2. 在菜单视图层接入入口，并确认 breadcrumb / 返回行为仅发生在菜单层级。
3. 如果进入业务，确保 owner flow 全程单卡推进，终态 sealed，且不回跳菜单。
4. 为新增路径补回归：
   - `internal/core/control/inline_replacement_test.go`
   - `internal/app/daemon/app_menu_handoff_test.go`
   - 必要时补对应业务 owner card 测试（例如 workspace/picker/history 等）

未同时满足以上四项，不应合并。

## 7. 变更评审最小核对项

每次菜单改动至少核对：

- 是否只通过统一 action contract 决定 launcher disposition
- 是否仍不存在旧链路（submission anchor / bare continuation / MenuFlow substrate）
- 是否新增了“菜单与业务混住”的路径
- 是否覆盖菜单回退、业务收口、旧卡点击拒绝三类用例

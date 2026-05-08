# Feishu `/workspace new dir` 子目录创建检查流设计

> Type: `draft`
> Updated: `2026-05-08`
> Summary: 固定 `从目录新建` 通过同卡“检查目标目录 -> 创建并继续”支持在选定目录下创建新目录的产品合同与实现边界。

## 1. 背景

当前 `/workspace new` 已有 3 条入口：

- 从目录新建
- 从 GIT URL 新建
- 从 Worktree 新建

这次需要补的不是第 4 条入口，而是现有 `从目录新建` 里的一个新场景：

- 先选一个目录
- 可选填写一个新目录名
- 以最终目录新开 workspace

这轮已经明确的产品约束有 3 条：

1. 不增加第 4 个并列入口。
2. 飞书卡片不做假实时预览。
3. 页面文案保持最小集合，不增加解释性废话。

## 2. 产品决策

### 2.1 入口层

- `/workspace new` 继续只保留 3 个入口。
- 新场景收进现有 `/workspace new dir`。
- 不新增新的 slash 命令、菜单项或 target-picker page 类型。

### 2.2 交互层

`/workspace new dir` 继续使用现有 `FeishuTargetPickerPageLocalDirectory`，但从“单字段 + 直接确认”改成“编辑 -> 显式检查 -> 继续确认”的同卡流程。

编辑态：

- 字段 1：`目录`
- 字段 2：`在此目录下创建新目录（可选）`
- 主按钮：`检查目标目录`

检查通过后的同卡编辑态：

- 继续展示 `目录`
- 继续展示 `在此目录下创建新目录（可选）`
- 新增只读结果：`目标目录`
- 主按钮切成：
  - `接入并继续`：未填写新目录名
  - `创建并继续`：填写了新目录名

检查失败后的同卡编辑态：

- 不进入新 page
- 不进入新 phase
- 阻塞原因直接留在原卡 notice 区
- 主按钮保持 `检查目标目录`

### 2.3 状态规则

- `检查目标目录` 是显式服务端往返，不是实时预览。
- 任何会影响最终目标路径的编辑，都会使上一次检查结果失效：
  - 重新选择目录
  - 修改 `在此目录下创建新目录（可选）`
- 检查结果失效后：
  - `目标目录` 隐藏
  - 主按钮退回 `检查目标目录`
- 没有有效检查结果时，不能出现 `接入并继续` 或 `创建并继续`。

### 2.4 文案合同

这张卡允许出现的新增用户可见文案仅限：

- `在此目录下创建新目录（可选）`
- `检查目标目录`
- `目标目录`
- `创建并继续`

其余文案继续复用现有 target picker 合同，不新增说明性长段落。

## 3. 为什么不用其他方案

### 3.1 不增加第 4 个入口

原因：

- `/workspace new` 现在已经有 3 个入口，再加一条会明显增加选择负担。
- 当前代码和运行时模型本来就是围绕 `local_directory / git / worktree` 3 条显式分支搭的，不是一个抽象的“任意新建来源”卡。

### 3.2 不做实时预览

原因：

- 飞书卡片不是实时表单。
- 边输入边显示 `最终目录` 会制造“前端实时计算”的错觉。
- 这类结果必须以服务端确认后的卡片回写为准。

### 3.3 不新增确认页或新 phase

原因：

- 新 page / 新 phase 会让 `local_directory` 从当前最简单的一条分支变成状态机例外。
- 当前 `git / worktree` 已经有成熟的“submit-time validation + 同卡回写”模型，这次应顺着它扩展，而不是再长一套局部特例。

## 4. 当前实现调研结论

### 4.1 现有 `/workspace new dir` 过于简单

当前 `local_directory` 页只保存并显示一个 `LocalDirectoryPath`，然后直接在 confirm 时进入：

- 校验目录
- 接入工作区
- 准备新会话

这意味着：

- 还没有“父目录 + 新目录名 + 检查结果”这组结构化状态
- 当前 projector 也还不是 inline form

### 4.2 `git / worktree` 已经提供了可复用模式

当前 `git / worktree` 分支已经具备：

- inline form
- submit-time validation
- 同卡回写阻塞原因
- 目标路径展示

因此 `local_directory` 的新场景应复用这条模式，而不是发明新的 owner-flow 语义。

### 4.3 path picker 不需要学会 mkdir

现有 path picker 只负责浏览和选择路径，不支持“边选边创建目录”。

这轮不应把“新建目录”责任塞给 path picker；正确归属是：

- path picker 负责选目录
- target picker 在 `检查目标目录` 时计算最终路径并校验
- 真正创建目录发生在 `创建并继续` 后端执行阶段

### 4.4 不能把“父目录选择”直接当成“最终工作区目录选择”

这是这轮最重要的实现边界。

当前 `/workspace new dir` 的很多逻辑默认把选中的目录直接当成最终 workspace 目录，包括：

- busy workspace 判断
- known workspace 复用提示
- confirm 后直接进入 `enterTargetPickerNewThread(...)`

但新场景里：

- 选中的目录可能只是父目录
- 最终 workspace 目录可能是 `父目录 + 新目录名`

所以实现上必须把“父目录草稿”和“检查后的最终目录”拆开，不能继续把同一个字段同时承担两种语义。

## 5. 实现方向

### 5.1 运行时与 view model

`activeTargetPickerRecord` / `FeishuTargetPickerView` 至少需要补出本地目录分支自己的结构化字段，例如：

- 父目录草稿
- 新目录名草稿
- 最近一次检查通过后的最终目录
- 当前检查结果是否仍然有效

这里不要求字面字段名必须完全照抄本文，但语义上必须分开：

- `目录草稿`
- `检查后的最终目录`

### 5.2 本地目录页改成 inline form

建议把 `local_directory` 页收口成和 `git` 更接近的形式：

- 一行目录字段 + `选择目录`
- 一个文本输入：`在此目录下创建新目录（可选）`
- 同卡 footer 动作区

这样可以自然承接：

- `检查目标目录`
- `接入并继续`
- `创建并继续`

### 5.3 显式检查动作

`检查目标目录` 的职责固定为：

1. 读取目录草稿与新目录名草稿
2. 算出最终目标目录
3. 执行阻塞校验
4. 把结果回写到同一张 owner card

建议这一步继续留在 `StageEditing`，不要引入额外 phase。

### 5.4 confirm 只消费“已检查通过的最终目录”

`接入并继续` / `创建并继续` 不应再次从“目录草稿 + 新目录名草稿”即时拼装结果，而应只消费：

- 最近一次检查通过后固化的最终目录

这样可以避免：

- 用户改了字段但没重新检查
- 服务端拿旧结果还是新草稿不一致

### 5.5 父目录与最终目录的校验边界

建议这轮按下面规则收口：

- 父目录阶段只做“路径存在 / 可访问 / 可选”判断
- known workspace / busy workspace / 目标目录已存在 / 目录名非法 这类和最终目标路径相关的语义，统一放到 `检查目标目录`

也就是说，不能继续把当前 `local_directory` 那套“把选中目录当成最终目录”的判断原样复用到父目录草稿上。

## 6. 完成标准建议

实现完成后，至少满足：

1. `/workspace new` 仍然只有 3 个入口。
2. `/workspace new dir` 支持：
   - 只选目录直接接入
   - 选目录并填写新目录名后创建并接入
3. 飞书卡片不做实时预览。
4. `检查目标目录` 通过同卡回写展示 `目标目录`。
5. 目录或目录名一旦变化，旧检查结果立即失效。
6. 复用已有工作区、目标目录冲突、busy workspace、非法目录名等结果都通过同卡 notice 给出明确提示。
7. 不新增新的 workspace new 入口。
8. `docs/general/feishu-card-ui-state-machine.md` 与实现同步更新。

## 7. 主要影响面

- `internal/core/control/feishu_target_picker.go`
- `internal/core/orchestrator/service_ui_runtime.go`
- `internal/core/orchestrator/service_target_picker.go`
- `internal/core/orchestrator/service_target_picker_add_workspace.go`
- `internal/core/orchestrator/service_target_picker_status.go`
- `internal/core/orchestrator/service_target_picker_contract.go`
- `internal/adapter/feishu/projector/target_picker.go`
- `internal/adapter/feishu/projector_target_picker_test.go`
- `internal/core/orchestrator/service_target_picker_test.go`
- `docs/general/feishu-card-ui-state-machine.md`

## 8. 暂不在本设计内扩展的内容

- 不让 path picker 直接创建目录
- 不增加新的 `/workspace new ...` 入口
- 不额外引入实时预览
- 不在这轮顺手改造整个 target picker 状态机
- 不在没有证据的前提下新增“嵌套 workspace 一律禁止”的新产品政策；若后续实现验证表明这里需要额外限制，再单独补 follow-up

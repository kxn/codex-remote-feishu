# Claude Plan Confirmation Permission Panel Design

> Type: `inprogress`
> Updated: `2026-05-26`
> Summary: 为 `#664` 收口 Feishu `plan_confirmation` 的复杂权限面板设计、Claude native `updatedPermissions` bridge contract、以及当前实现面临的关键技术难点。

## 1. 文档定位

这份文档服务于 `#664`。

它只解决一件事：

- 当 Claude 在 `ExitPlanMode` / `plan_confirmation` 上请求确认时，Feishu 端如何以 **复杂权限面板** 的方式，承接 accept-side permission updates，并把用户选择稳定映射到 Claude native `updatedPermissions`。

这份文档不覆盖：

- `#663` 已处理的 same-request `revise`
- generic `approval_can_use_tool` 的 session grant 改造
- 跨会话持久化权限记忆
- 任意自由文本权限规则编辑器

当前实现与状态机的 source of truth 仍然是：

- [relay-protocol-spec.md](../general/relay-protocol-spec.md)
- [feishu-card-ui-state-machine.md](../general/feishu-card-ui-state-machine.md)
- [remote-surface-state-machine.md](../general/remote-surface-state-machine.md)

## 2. 结论摘要

`#664` 的产品方向已经定稿：

1. `plan_confirmation` 需要承接 accept-side permission updates。
2. 用户看到的是 **两段式交互**：
   - 第一张卡先做快速决策
   - 第二张卡才进入复杂权限面板
3. 更长期的授权仅限 **当前会话**
   - 不做跨会话记忆
4. 权限面板必须是 **结构化点选**
   - 不要求用户手写规则
   - 候选项尽量由当前 request 上下文自动生成
5. `v1` 明确 fail-closed：
   - 不暴露全局 `bypassPermissions`
   - 不支持手写路径/规则
   - 不支持编辑或撤销历史 session grant
   - 不支持把未知 native affordance 静默压扁成现有 `confirm/full_access`

更重要的实现结论是：

- `v1` 不应把“raw permission mode dropdown”直接暴露给用户。
- 用户面展示为“授权级别 / 目录范围 / 规则范围”三块。
- bridge 层只在 translator 里把这些结构化选择编译成 Claude native `updatedPermissions`。

原因是 Claude native documented modes 过于粗粒度，而用户要求的目录范围控制需要更细的 rule-level contract；如果直接把用户选择翻成 `acceptEdits` / `bypassPermissions`，很容易做出语义错误的“伪等价”。

## 3. Native 事实与当前代码现实

### 3.1 Claude native 已确认事实

基于官方文档与 source preview，当前至少可以确认：

1. permission callback 的 allow path 可以回 `updatedPermissions`
2. SDK/CLI 的权限体系至少分为：
   - permission mode
   - allow / ask / deny rules
   - additional directories
   - destination（`session` / `localSettings` / `projectSettings` / `userSettings`）
3. 文档已公开的 mode 至少包括：
   - `default`
   - `plan`
   - `acceptEdits`
   - `bypassPermissions`
4. source preview 里的 `PermissionUpdate` 已明显 richer than current repo assumption，至少包含：
   - `setMode`
   - `addRules`
   - `replaceRules`
   - `removeRules`
   - `addDirectories`
   - `removeDirectories`

需要特别注意两条 native 语义：

1. `acceptEdits` 会自动批准文件修改和常见文件系统操作，这是一种 **粗粒度 session mode**
2. allow / ask / deny rules 具备更细粒度的 Bash 前缀匹配与文件路径 glob 语义

### 3.2 当前仓库现实

当前本地实现还远没到能正确承接上面这些语义的程度。

#### 3.2.1 本地 access model 过粗

当前只有：

- `confirm`
- `full_access`
- `planMode on/off`

对应代码：

- [internal/core/agentproto/access.go](../../internal/core/agentproto/access.go)
- [internal/adapter/claude/permission_mode.go](../../internal/adapter/claude/permission_mode.go)

这意味着：

- `acceptEdits` 没有本地 carrier
- `dontAsk` 没有本地 carrier
- rules / directories 完全没有本地 carrier

#### 3.2.2 `plan_confirmation` allow path 仍硬编码空权限更新

当前 Claude translator 的 allow path 仍固定发：

- `updatedPermissions = []`

对应代码：

- [internal/adapter/claude/commands.go](../../internal/adapter/claude/commands.go)

#### 3.2.3 当前 Feishu request card 还不具备复杂权限面板 carrier

当前 request card 的现实能力是：

- approval：按钮组
- request_user_input / MCP form：单题 direct-response，或者单个文本输入框

当前并没有现成的：

- 多字段 request form
- `select_static` request field
- `multi_select_static` request field
- approval subphase 下的复杂面板

对应代码：

- [internal/adapter/feishu/projector/request.go](../../internal/adapter/feishu/projector/request.go)
- [internal/core/orchestrator/service_request_presentation.go](../../internal/core/orchestrator/service_request_presentation.go)
- [internal/core/control/types.go](../../internal/core/control/types.go)

#### 3.2.4 当前 question/state 模型默认每题单值

现有 carrier：

- `RequestQuestion`
- `RequestPromptQuestionRecord`
- `DraftAnswers map[string]string`

都默认“一题一个字符串答案”。

对应代码：

- [internal/core/agentproto/types.go](../../internal/core/agentproto/types.go)
- [internal/core/state/types.go](../../internal/core/state/types.go)
- [internal/core/orchestrator/service_request.go](../../internal/core/orchestrator/service_request.go)

#### 3.2.5 gateway 虽然能收数组，但现有 request form helper 会压平多选值

当前 `requestAnswersFromMap(...)` 能保留 `[]string`，但 `requestAnswersFromFormValue(...)` 和 `selectflow.FormValue(...)` 都按“取第一个值”处理。

这意味着：

- 飞书表单原生多选不是完全无路
- 但当前 request form helper 会把多选答案压成单值

对应代码：

- [internal/adapter/feishu/gateway/routing.go](../../internal/adapter/feishu/gateway/routing.go)
- [internal/adapter/feishu/selectflow/contract.go](../../internal/adapter/feishu/selectflow/contract.go)

#### 3.2.6 `plan_confirmation` 观察链还没把 native permission suggestions 带下来

当前 `can_use_tool` 路径已经会保留 `permissionSuggestions` metadata，但 `plan_confirmation` 没有同类 sidecar。

对应代码：

- [internal/adapter/claude/observe.go](../../internal/adapter/claude/observe.go)

这意味着如果复杂面板想“尽量复用 native 已经建议的规则/目录”，前面还缺一段 observe-side carrier。

## 4. 用户可见设计

### 4.1 第一张卡：快速决策

第一张卡保持轻量，不直接展开完整复杂面板。

用户看到：

- `允许一次并执行`
- `配置本会话授权`
- `拒绝`
- `告诉 Claude 怎么做`

卡片正文增加一段只读摘要：

- 当前计划将修改哪些文件或目录
- 当前计划是否涉及文件创建/移动/删除
- 当前计划是否明显需要额外目录访问

这一步的原则是：

- 普通用户仍然一键 `允许一次`
- 只有明确需要 session-scoped 结构化授权的用户才进入第二张卡

### 4.2 第二张卡：复杂权限面板

点击 `配置本会话授权` 后，同一条 pending request inline replace 成复杂权限面板。

卡片顶部固定说明：

- `仅当前会话有效`
- `未展示或未勾选的权限默认不授予`
- `这不是全局永久授权`

面板分三块。

#### 4.2.1 授权级别

用户看到的是用户语义，不是 raw native mode 名称：

- `仅按下面选中的范围自动允许`
- `本会话自动允许文件修改（更快）`
- `本会话自动允许文件修改和常见文件系统操作（更激进）`

推荐默认第一项。

设计意图：

- 第一项优先走 rules/directories
- 后两项才允许落到更粗的 native mode / bundle

#### 4.2.2 目录范围

候选目录由系统自动生成，默认选中“当前计划涉及的目录”。

典型候选：

- `internal/adapter/claude`
- `internal/core/orchestrator`
- `docs/general`
- `整个当前工作区`

用户通过结构化多选点选，不手填路径。

#### 4.2.3 规则范围

规则范围也不暴露原始 rule string，而是规则族：

- `编辑现有文件`
- `创建新文件`
- `重命名或移动文件`
- `删除计划中涉及的文件`
- `执行常见文件系统命令`

其中只有明确能稳定映射的规则族才允许在 `v1` 中出现。

### 4.3 面板底部动作

底部动作固定为：

- `按以上授权继续`
- `返回`
- `拒绝`

`返回` 回到第一张卡，不丢弃当前 pending request。

### 4.4 提交后的封口态

提交后不会直接“什么都不提示就继续执行”，而是先把卡片 seal 为摘要态：

- `已按本会话授权继续`
- `授权级别：...`
- `目录范围：...`
- `规则范围：...`
- `有效期：当前会话`

然后再向 Claude 派发真正的 request response。

这样用户能清楚知道：

- 我授权了什么
- 这次不是“没同意也执行”
- 当前授权只活到当前会话结束

## 5. 支持矩阵与 fail-closed 边界

### 5.1 `v1` 明确支持

`v1` 支持下面这些能力：

1. 当前会话内的结构化授权
2. 目录候选自动生成 + 多选
3. 规则族自动生成 + 多选
4. 提交后将结构化选择编译成 Claude native `updatedPermissions`
5. `accept` / `decline` / `revise` 继续共存，不破坏 `#663`

### 5.2 `v1` 明确不支持

`v1` fail-closed 不做：

1. 跨会话记忆
2. 全局 `bypassPermissions` 直出按钮
3. 手写规则
4. 手写路径
5. 编辑或撤销历史 session grant
6. `replaceRules` / `removeRules`
7. `removeDirectories`
8. 网络、WebFetch、MCP server 这类更宽权限的自动授权
9. 任何我们无法稳定翻译的 native affordance

### 5.3 对 permission mode 的定稿

这个 issue 的关键结论之一是：

- `v1` **不把 raw permission mode 作为主用户心智暴露**

原因：

1. `acceptEdits` / `bypassPermissions` 是粗粒度 session mode
2. 用户要求的是“当前计划相关目录/操作”的结构化授权
3. 直接把两者绑定会制造错误承诺

因此 `v1` 的策略是：

1. 用户侧主语义是：
   - 授权级别
   - 目录范围
   - 规则范围
2. translator 侧再根据这三个维度，决定是否：
   - 只发 `addRules`
   - `addRules + addDirectories`
   - 或在非常保守的特定组合下附带 `setMode(acceptEdits)`

`bypassPermissions` 不进 `v1` 面板。

## 6. 本地 bridge contract 设计

### 6.1 不建议把 raw Claude `PermissionUpdate` 暴露到 orchestrator

不建议让 orchestrator / Feishu projector 直接持有上游原生 `PermissionUpdate[]`。

原因：

1. 产品层不该直接拼 Claude 私有协议
2. 这样会把 UI 设计和 translator 实现绑死
3. 后续如果 native schema 变动，产品层会被迫一起改

### 6.2 推荐新增本地结构化 carrier

推荐在 request response 上新增一个 **本地结构化权限选择 carrier**，仅对 `plan_confirmation` 生效。

建议概念模型：

- `scope`
  - `session`
- `grant_level`
  - `scoped_rules`
  - `session_file_edits`
  - `session_file_edits_and_fs_ops`
- `directories`
  - `[]string`
- `rule_classes`
  - `[]string`

示意：

```json
{
  "type": "approval",
  "decision": "accept",
  "feedback": "Approved. Execute the plan.",
  "permissionSelection": {
    "scope": "session",
    "grant_level": "scoped_rules",
    "directories": [
      "internal/adapter/claude",
      "internal/core/orchestrator"
    ],
    "rule_classes": [
      "edit_existing_files",
      "create_new_files",
      "rename_or_move_files"
    ]
  }
}
```

然后由 Claude translator 单点把它翻成 native `updatedPermissions`。

### 6.3 推荐新增 request-local form carrier

当前 `Questions` 模型不适合承载这块复杂面板。

推荐新增 `request-local structured form` carrier：

- 多字段
- 字段级 kind
- 字段级多选默认值
- 字段候选项
- 同 request revision / pending gate / sealed lifecycle

推荐字段类型至少支持：

- `text`
- `select_static`
- `multi_select_static`

这条 carrier 先只给 request family 用，不强行把 command catalog 的单字段 form 扩成万能表单。

### 6.4 推荐把复杂面板视为 `plan_confirmation` 的本地 subphase

不建议把第二张卡建成脱离 pending request 的 owner page。

推荐把它定义成 `plan_confirmation` 自己的本地 subphase：

- `quick_decision`
- `permission_configuring`
- `waiting_dispatch`
- `sealed`

这样可以继续复用：

- request gate
- request revision
- old-card reject
- same-request dispatch

## 7. Claude native 映射设计

### 7.1 总原则

translator 负责编译本地 `permissionSelection -> updatedPermissions[]`。

编译原则：

1. 优先 additive
2. 优先窄授权
3. 无法证明安全等价时，宁可少发，不多发
4. 不支持的能力 fail-closed，不 silent drop

### 7.2 `v1` 建议映射

推荐 `v1` 以 rules 为主，mode 为辅。

#### 7.2.1 `scoped_rules`

默认组合：

- `setMode` 不发，保持 `default`
- 对选中的规则族生成 `addRules`
- 对超出当前 cwd 的目录生成 `addDirectories`

这条路径最符合“目录范围 + 规则范围”的用户心智。

#### 7.2.2 `session_file_edits`

保守建议：

- 当前实现仍走 `default + addRules`
- `v1` 不把这档直接编译成 `setMode(acceptEdits)`

原因：

- `acceptEdits` 本身不是目录级约束
- `acceptEdits` 还会顺带放开常见文件系统 Bash 命令，这会超过“只自动允许文件修改”的用户心智
- 如果用户只选了两个子目录，却翻成全 cwd 的 `acceptEdits`，语义就变了

#### 7.2.3 `session_file_edits_and_fs_ops`

保守建议：

- 只有在这三个条件同时成立时，才允许编译成 `setMode(acceptEdits)`：
  - 目录范围等于整个当前工作区
  - 规则范围已经覆盖 edit/create/rename/delete/common fs ops 全套
  - translator 不需要再用 `acceptEdits` 去补未知宽度的额外语义
- 其余组合仍然编译成 curated `addRules`
- 不直接暴露 `bypassPermissions`

允许的规则族必须是显式白名单，例如：

- 文件修改相关工具
- 常见文件系统 Bash 前缀

但需要明确：

- Bash prefix rule 很难像 Edit/Write 一样自然带目录边界
- 对“只选了子目录”的 aggressive 组合，当前实现必须 fail-closed 为更窄的 path-scoped edit/create 自动授权，并让其余 fs ops 继续按需弹 prompt

### 7.3 `v1` 不做 remove/replace

当前没有稳定的 session permission snapshot。

因此 `v1` 不做：

- `replaceRules`
- `removeRules`
- `removeDirectories`

只做 additive grant。

这意味着：

- 复杂面板的语义是“给当前会话再加一组自动允许”
- 不是“编辑整个当前 session 的完整权限配置”

## 8. 技术难点与实现风险

### 8.1 高风险：本地 permission model 与 native model 不等价

当前本地只有 `confirm/full_access + plan`，而 native 明显 richer。

风险：

- 如果继续复用旧 access model，会把新面板压扁成错误语义

结论：

- 这次不能只修 translator
- 必须引入新的 request-local permission carrier

### 8.2 高风险：现有 request question 模型是单值模型

当前：

- `DraftAnswers map[string]string`
- `buildRequestUserInputResponse(...)` 按单值处理

风险：

- 多选目录和多选规则族没有自然承载面

结论：

- 复杂面板不应硬塞进旧 `Questions` 模型
- 推荐新增 structured form carrier

### 8.3 高风险：gateway form helper 当前会压扁多选答案

当前 `selectflow.FormValue(...)` 与 `requestAnswersFromFormValue(...)` 默认只取第一个值。

风险：

- 就算飞书表单用了 `multi_select_static`，回到 request response 也会只剩一个值

结论：

- gateway / selectflow helper 必须一并改

### 8.4 高风险：`acceptEdits` 是粗粒度 mode，和目录范围天然冲突

这是本设计最容易被误判的一点。

用户要的是：

- “只对当前计划涉及的目录自动放行”

但 `acceptEdits` 的语义更像：

- “本会话内文件编辑与常见文件系统操作自动批准”

这不是同一个东西。

结论：

- `v1` 不能把面板默认翻成 `acceptEdits`
- 必须优先以 `addRules` 驱动

### 8.5 高风险：当前没有 session permission snapshot，因此无法安全做 revoke/edit

没有本地 snapshot 时，就无法稳定判断：

- 现在到底已经有哪些 allow rules
- 某个目录是不是之前已经 grant 过
- 这次“取消勾选”到底意味着 no-op，还是应该 remove

结论：

- `v1` 只支持 additive grant

### 8.6 中风险：plan_confirmation 目前未观察到 permission suggestions sidecar

`can_use_tool` 已有 `permissionSuggestions` metadata，但 `plan_confirmation` 没有。

风险：

- 复杂面板的候选规则只能靠本地 plan 语义和保守 bundle 推导
- 无法直接复用 native 已建议的 update 列表

结论：

- 需要补一轮 observe-side research / blackbox 验证
- 若 native 真实能给 suggestion，应优先把它投影到 metadata

### 8.7 中风险：飞书卡片体积和候选项数量

目录和规则候选如果太多，会碰到：

- 卡片体积上限
- 元素数量上限
- 手机端可读性下降

结论：

- 目录候选必须做预算
- 超额时优先只展示“计划涉及目录”与“整个工作区”
- 更长目录集要有分页或折叠降级，而不是塞满一张卡

### 8.8 中风险：不能破坏现有 `revise -> same-request guidance` 语义

`#663` 已恢复 `revise` 正确语义。

复杂面板必须保证：

- `revise` 仍然是 first-card 入口
- 不和 `permission_configuring` 子流程混淆
- 不出现“点了配置授权却误进 revise capture”的回归

## 9. 推荐拆分

当前不建议把 `#664` 直接作为单 worker 执行单元继续编码。

推荐拆成两个执行子单：

### 9.1 子单 A：Feishu request structured form / complex panel

负责：

- request-local structured form carrier
- `plan_confirmation` subphase
- `multi_select_static` request field render
- gateway request form multi-select 回传
- inline replace / sealed summary card

主要涉及：

- `internal/core/control/**`
- `internal/core/state/**`
- `internal/core/orchestrator/**`
- `internal/adapter/feishu/**`

### 9.2 子单 B：Claude permission bridge / native mapping

负责：

- `permissionSelection` local carrier
- translator 编译 `updatedPermissions`
- native permission mode / rules / directories mapping
- additive-only contract
- focused translator tests

主要涉及：

- `internal/adapter/claude/**`
- `internal/core/agentproto/**`
- `internal/core/orchestrator/**`

这两个子单可以在 design contract 固定后弱并行，但以 A 先落 carrier、B 跟进 mapping 更稳。

## 10. 验证建议

至少覆盖下面几类测试：

1. first-card `允许一次 / 配置本会话授权 / 拒绝 / 告诉 Claude 怎么做`
2. 复杂面板 inline replace 与返回
3. `multi_select_static` form submit 能保留全部选中值
4. request revision / old-card reject
5. `revise` 语义不回归
6. translator 对 `permissionSelection` 的 `updatedPermissions` 编译
7. additive-only contract：`v1` 不产生 remove/replace
8. 超预算目录候选的降级行为

## 11. 外部参考

- Claude user input:
  - https://code.claude.com/docs/en/agent-sdk/user-input
- Claude SDK permissions:
  - https://docs.anthropic.com/en/docs/claude-code/sdk/sdk-permissions
- Claude Code settings / rules:
  - https://docs.anthropic.com/en/docs/claude-code/settings
- Claude Code IAM / permission rules:
  - https://docs.anthropic.com/en/docs/claude-code/iam
- Claude source preview:
  - https://claude-code-info.vercel.app/docs/claude-src/file/types/permissions.ts
- Feishu card form container:
  - https://open.feishu.cn/document/feishu-cards/card-json-v2-components/containers/form-container
- Feishu static select:
  - https://open.feishu.cn/document/feishu-cards/card-json-v2-components/selection-controls/select-static
- Feishu multi static select:
  - https://open.feishu.cn/document/feishu-cards/card-json-v2-components/selection-controls/multi-select-static
